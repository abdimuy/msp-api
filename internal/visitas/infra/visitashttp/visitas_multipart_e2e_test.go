// TestE2E_VisitasMultipart_* drive POST /v2/visitas end to end against a REAL
// Firebird database and a REAL filesystem blob store: HTTP → visitashttp
// handler → app.RegistrarVisitaConImagenes → visitasfb → MSP_VISITAS +
// MSP_VISITAS_IMAGENES, with the photo bytes landing on disk under a temp
// STORAGE_DIR.
//
// The point of doing it for real rather than with fakes is the pair of
// invariants the fakes cannot prove: that the visita row and its imagen rows
// commit inside ONE Firebird transaction, and that the file on disk and the
// row in the table agree after a retry.
//
// Every test runs inside fbtestutil.WithTestTransaction (rollback-only), so
// no row survives; the blobs live in t.TempDir() and go with it.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test -run TestE2E ./internal/visitas/...
//
//nolint:misspell // visitas vocabulary is Spanish (imagen, descripcion) per project convention.
package visitashttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
	visitasoutbound "github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// visitasE2EStorageAdapter re-packs the ventas FilesystemProvider as the
// visitas StorageProvider port — the same adapter cmd/api wires in
// production, kept local so an infra test does not import cmd.
type visitasE2EStorageAdapter struct {
	inner *storage.FilesystemProvider
}

func (a *visitasE2EStorageAdapter) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	return a.inner.Store(ctx, key, contentType, sizeBytes, body)
}

func (a *visitasE2EStorageAdapter) Get(ctx context.Context, key string) (visitasoutbound.StorageObject, error) {
	obj, err := a.inner.Get(ctx, key)
	if err != nil {
		return visitasoutbound.StorageObject{}, err
	}
	return visitasoutbound.StorageObject{
		Body:        obj.Body,
		ContentType: obj.ContentType,
		SizeBytes:   obj.SizeBytes,
	}, nil
}

func (a *visitasE2EStorageAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(ctx, key)
}

// multipartE2EEnv is everything a multipart E2E test needs: the mounted
// router, the storage root on disk, and the pool for raw verification queries.
type multipartE2EEnv struct {
	router     http.Handler
	storageDir string
	pool       *firebird.Pool
}

// newMultipartE2EEnv assembles the production composition for the photo path:
// real repo, real filesystem storage, real Firebird TxManager.
func newMultipartE2EEnv(txCtx context.Context, t *testing.T, pool *firebird.Pool, cu auth.CurrentUser) multipartE2EEnv {
	t.Helper()

	dir := t.TempDir()
	fsProvider, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	repo := visitasfb.New(pool)
	svc := app.NewService(
		repo,
		visitasoutbound.ProductionClock{},
		app.WithImagenes(repo, &visitasE2EStorageAdapter{inner: fsProvider}, firebird.NewTxManager(pool.DB)),
	)

	r := chi.NewRouter()
	r.Use(visitasTxInjector(txCtx))
	r.Use(planter(cu))
	visitashttp.MountRouter(r, svc)

	return multipartE2EEnv{router: r, storageDir: dir, pool: pool}
}

// e2eVisitaJSON builds a visita document tied to the shared test cliente and
// tagged with the E2E marker so residual rows are identifiable.
func e2eVisitaJSON(id uuid.UUID, clienteID int) string {
	fecha := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	return `{` +
		`"id":"` + id.String() + `",` +
		`"cliente_id":` + strconv.Itoa(clienteID) + `,` +
		`"cobrador_id":42,` +
		`"cobrador":"Ramírez García, Jorge (` + visitasE2EMarker + `-MULTIPART)",` +
		`"forma_cobro_id":87327,` +
		`"lat":19.432608,` +
		`"lng":-99.133209,` +
		`"nota":"María salió, vuelve mañana",` +
		`"tipo_visita":"No se encontraba",` +
		`"zona_cliente_id":7,` +
		`"fecha":"` + fecha + `"` +
		`}`
}

// countImagenes reads MSP_VISITAS_IMAGENES inside the test transaction.
func countImagenes(ctx context.Context, t *testing.T, pool *firebird.Pool, visitaID uuid.UUID) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MSP_VISITAS_IMAGENES WHERE VISITA_ID = ?", visitaID.String()).Scan(&n))
	return n
}

func countVisitas(ctx context.Context, t *testing.T, pool *firebird.Pool, visitaID uuid.UUID) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var n int
	require.NoError(t, q.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM MSP_VISITAS WHERE ID = ?", visitaID.String()).Scan(&n))
	return n
}

// ─── Coexistence, end to end ───────────────────────────────────────────────

// TestE2E_VisitasMultipart_LosDosContentTypesContraFirebird is the
// coexistence test at full stack: the same route, both wire formats, both
// reaching MSP_VISITAS.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasMultipart_LosDosContentTypesContraFirebird(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clienteID := visitasE2EClienteID(t, firebird.GetQuerier(ctx, pool.DB))
		env := newMultipartE2EEnv(ctx, t, pool, visitasE2EFullUser())

		// ── JSON ──────────────────────────────────────────────────────────
		jsonID := uuid.New()
		reqJSON := httptest.NewRequest(http.MethodPost, "/visitas",
			bytes.NewBufferString(e2eVisitaJSON(jsonID, clienteID)))
		reqJSON.Header.Set("Content-Type", "application/json")
		recJSON := httptest.NewRecorder()
		env.router.ServeHTTP(recJSON, reqJSON)

		require.Equal(t, http.StatusCreated, recJSON.Code,
			"the legacy JSON contract must still reach Firebird; body=%s", recJSON.Body.String())
		assert.Equal(t, 1, countVisitas(ctx, t, pool, jsonID))
		assert.Equal(t, 0, countImagenes(ctx, t, pool, jsonID))

		// ── Multipart ─────────────────────────────────────────────────────
		mpID := uuid.New()
		imgID := uuid.NewString()
		body, ct := buildMultipart(t, e2eVisitaJSON(mpID, clienteID),
			[]testImagen{jpeg(imgID, "candado en la puerta")})
		reqMP := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewReader(body))
		reqMP.Header.Set("Content-Type", ct)
		recMP := httptest.NewRecorder()
		env.router.ServeHTTP(recMP, reqMP)

		require.Equal(t, http.StatusCreated, recMP.Code, "body=%s", recMP.Body.String())
		assert.Equal(t, 1, countVisitas(ctx, t, pool, mpID))
		assert.Equal(t, 1, countImagenes(ctx, t, pool, mpID))

		// ── The blob is really on disk, at the documented key ─────────────
		key := "visitas/" + mpID.String() + "/" + imgID + ".jpg"
		onDisk := filepath.Join(env.storageDir, filepath.FromSlash(key))
		data, err := os.ReadFile(onDisk) //nolint:gosec // path is built from a temp dir + two UUIDs.
		require.NoError(t, err, "the photo must exist at %s", key)
		assert.Equal(t, []byte("\xff\xd8\xff\xe0JFIF-bytes"), data, "byte-for-byte what was uploaded")

		// ── And the row points at exactly that key ────────────────────────
		q := firebird.GetQuerier(ctx, pool.DB)
		var storedKey, storedMime, storedKind string
		var storedSize int64
		require.NoError(t, q.QueryRowContext(ctx,
			"SELECT STORAGE_KEY, MIME, STORAGE_KIND, SIZE_BYTES FROM MSP_VISITAS_IMAGENES WHERE VISITA_ID = ?",
			mpID.String()).Scan(&storedKey, &storedMime, &storedKind, &storedSize))
		assert.Equal(t, key, storedKey)
		assert.Equal(t, "image/jpeg", storedMime)
		assert.Equal(t, "FILESYSTEM", strings.TrimSpace(storedKind))
		assert.Equal(t, int64(len(data)), storedSize)
	})
}

// ─── Idempotency with a photo, end to end ──────────────────────────────────

// TestE2E_VisitasMultipart_ReintentoConFotoNoDuplicaNiHuerfana replays the
// identical multipart request and checks the three things that can go wrong
// against real storage: a second visita row, a second imagen row, and a blob
// that the cleanup deleted out from under a committed row.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasMultipart_ReintentoConFotoNoDuplicaNiHuerfana(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clienteID := visitasE2EClienteID(t, firebird.GetQuerier(ctx, pool.DB))
		env := newMultipartE2EEnv(ctx, t, pool, visitasE2EFullUser())

		visitaID := uuid.New()
		imgID := uuid.NewString()
		body, ct := buildMultipart(t, e2eVisitaJSON(visitaID, clienteID),
			[]testImagen{jpeg(imgID, "puerta")})
		key := "visitas/" + visitaID.String() + "/" + imgID + ".jpg"
		onDisk := filepath.Join(env.storageDir, filepath.FromSlash(key))

		post := func() *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewReader(body))
			req.Header.Set("Content-Type", ct)
			rec := httptest.NewRecorder()
			env.router.ServeHTTP(rec, req)
			return rec
		}

		first := post()
		require.Equal(t, http.StatusCreated, first.Code, "body=%s", first.Body.String())
		require.FileExists(t, onDisk)

		second := post()
		require.Equal(t, http.StatusCreated, second.Code,
			"a replay answers 201 with the stored visita; body=%s", second.Body.String())

		var dto1, dto2 visitashttp.VisitaDTO
		require.NoError(t, json.Unmarshal(first.Body.Bytes(), &dto1))
		require.NoError(t, json.Unmarshal(second.Body.Bytes(), &dto2))
		assert.Equal(t, dto1.ID, dto2.ID)

		assert.Equal(t, 1, countVisitas(ctx, t, pool, visitaID), "ONE visita row")
		assert.Equal(t, 1, countImagenes(ctx, t, pool, visitaID), "ONE imagen row")

		// The blob the committed row points at must still be there. This is
		// the assertion that would fail if the replay re-uploaded and then
		// cleaned up: the row would survive and the file would not.
		require.FileExists(t, onDisk,
			"the replay must not delete the blob a committed row references")
		data, err := os.ReadFile(onDisk) //nolint:gosec // path is built from a temp dir + two UUIDs.
		require.NoError(t, err)
		assert.Equal(t, []byte("\xff\xd8\xff\xe0JFIF-bytes"), data)
	})
}

// TestE2E_VisitasMultipart_VisitaInvalidaNoDejaBlobHuerfano proves the
// rollback path for real: a request whose tipo_visita is outside the closed
// catalogue writes neither a row nor a file.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VisitasMultipart_VisitaInvalidaNoDejaBlobHuerfano(t *testing.T) {
	visitasE2ERequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		clienteID := visitasE2EClienteID(t, firebird.GetQuerier(ctx, pool.DB))
		env := newMultipartE2EEnv(ctx, t, pool, visitasE2EFullUser())

		visitaID := uuid.New()
		datos := strings.Replace(e2eVisitaJSON(visitaID, clienteID),
			`"tipo_visita":"No se encontraba"`, `"tipo_visita":"inventado por el cliente"`, 1)
		body, ct := buildMultipart(t, datos, []testImagen{jpeg(uuid.NewString(), "")})

		req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewReader(body))
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "visita_tipo_invalido")
		assert.Equal(t, 0, countVisitas(ctx, t, pool, visitaID))
		assert.Equal(t, 0, countImagenes(ctx, t, pool, visitaID))

		entries, err := os.ReadDir(filepath.Join(env.storageDir, "visitas"))
		if err == nil {
			assert.Empty(t, entries, "no blob directory should have been created")
		}
	})
}
