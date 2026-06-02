//nolint:misspell // Spanish vocabulary (cobranza, pago, etc.) by convention.
package cobranzahttp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
)

// cobranzaStorageE2EAdapter wraps a ventas FilesystemProvider to satisfy the
// cobranza StorageProvider port — mirrors the production cobranzaStorageAdapter
// in cmd/api but kept local to the test so we don't pull cmd into infra tests.
type cobranzaStorageE2EAdapter struct {
	inner *storage.FilesystemProvider
}

func (a *cobranzaStorageE2EAdapter) Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error {
	return a.inner.Store(ctx, key, contentType, sizeBytes, body)
}

func (a *cobranzaStorageE2EAdapter) Get(ctx context.Context, key string) (cobranzaoutbound.StorageObject, error) {
	obj, err := a.inner.Get(ctx, key)
	if err != nil {
		return cobranzaoutbound.StorageObject{}, err
	}
	return cobranzaoutbound.StorageObject{
		Body:        obj.Body,
		ContentType: obj.ContentType,
		SizeBytes:   obj.SizeBytes,
	}, nil
}

func (a *cobranzaStorageE2EAdapter) Delete(ctx context.Context, key string) error {
	return a.inner.Delete(ctx, key)
}

// recordingMicrosipWriter records calls to Aplicar. Used so the E2E test can
// verify the post-commit fast-path fired without exercising the real Microsip
// writer (which has its own integration suite).
type recordingMicrosipWriter struct {
	mu        sync.Mutex
	callCount int
	result    cobranzaoutbound.MicrosipPagoResult
	err       error
}

func (r *recordingMicrosipWriter) Aplicar(_ context.Context, _ cobranzaoutbound.MicrosipPagoInput) (cobranzaoutbound.MicrosipPagoResult, error) {
	r.mu.Lock()
	r.callCount++
	r.mu.Unlock()
	return r.result, r.err
}

func (r *recordingMicrosipWriter) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.callCount
}

// e2eRequireCargo ensures cargo 1001 exists in DOCTOS_CC + MSP_SALDOS_VENTAS
// (test fixture used by every E2E cobranza write test). Skips if absent so
// runs on machines without the seed survive.
func e2eRequireCargo(ctx context.Context, t *testing.T, q firebird.Querier, cargoID int) {
	t.Helper()
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID).Scan(&n)
	if err != nil || n == 0 {
		t.Skipf("cargo %d / MSP_SALDOS_VENTAS cache not seeded; skipping — run 'make fb-seed-cobranza'", cargoID)
	}
}

// TestE2E_CrearPagoConImagenes_FullCycle exercises the new POST /pagos
// multipart endpoint against a real Firebird transaction + a temp-dir
// filesystem storage. Verifies:
//
//  1. POST returns 200 with a valid PagoRecibidoDTO.
//  2. MSP_PAGOS_RECIBIDOS has one row for the pago UUID.
//  3. MSP_PAGOS_IMAGENES has N rows linked to the pago.
//  4. N blob files exist on disk under STORAGE_DIR/pagos/<pago_id>/.
//  5. The post-commit AplicarPago fast-path fired exactly once.
//
// Wrapped in the rollback-only outer tx (WithTestTransaction); the inner
// runInTx the service uses is composed re-entrantly so everything rolls back
// at test end and the shared DB stays clean.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_CrearPagoConImagenes_FullCycle(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("1500.00")
		cargoID := e2eInsertCargo(t, q, clienteID, "E2E-MP-01", importe)
		e2eRequireMigration000010(t, q)
		e2eRequireCargo(ctx, t, q, cargoID)

		// Storage rooted at t.TempDir() so blob writes are isolated to this test.
		storageDir := t.TempDir()
		fsProv, err := storage.NewFilesystemProvider(storageDir)
		require.NoError(t, err)

		repo := cobranzaventfb.NewPagosRecibidosRepo(pool)
		writer := &recordingMicrosipWriter{
			result: cobranzaoutbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "E2E-MP-001"},
		}
		txMgr := firebird.NewTxManager(pool.DB)

		svc := cobranzaapp.NewService(
			cobranzaventfb.NewSaldosRepo(pool),
			cobranzaventfb.NewPagosRepo(pool),
			cobranzaventfb.NewVentasRepo(pool),
			cobranzaoutbound.ProductionClock{},
			repo, // pagosRecibidos
			repo, // pagosImagenes (same struct satisfies both)
			writer,
			&cobranzaStorageE2EAdapter{inner: fsProv},
			nil, // imageProc — PDFs bypass it
			txMgr,
		)

		cu := pagoUser()
		// buildReadRouter (defined in e2e_firebird_test.go) splices the test tx ctx.
		router := buildReadRouter(ctx, svc, cu)

		// Build a multipart with 2 comprobantes (both PDF so processor stays out).
		pagoID := uuid.New()
		imgID1 := uuid.New()
		imgID2 := uuid.New()
		fechaRFC3339 := "2026-06-01T09:30:00Z"
		datos := `{"id":"` + pagoID.String() + `",` +
			`"cargo_docto_cc_id":` + itoa(cargoID) + `,` +
			`"cliente_id":` + itoa(clienteID) + `,` +
			`"cobrador_id":42,` +
			`"cobrador":"Ramírez García, Jorge",` +
			`"importe":"` + importe.StringFixed(2) + `",` +
			`"forma_cobro_id":87327,` +
			`"fecha_hora_pago":"` + fechaRFC3339 + `"}`

		body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
			{
				Filename: "recibo-1.pdf",
				Mime:     "application/pdf",
				Body:     []byte("%PDF-1.4 e2e content 1"),
				ID:       imgID1.String(),
			},
			{
				Filename: "recibo-2.pdf",
				Mime:     "application/pdf",
				Body:     []byte("%PDF-1.4 e2e content 2"),
				ID:       imgID2.String(),
			},
		})

		req := httptest.NewRequest(http.MethodPost, "/pagos", body)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var dto cobranzahttp.PagoRecibidoDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
		assert.Equal(t, pagoID.String(), dto.ID)

		// 1) Pago row.
		var nPago int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`, pagoID.String(),
		).Scan(&nPago))
		assert.Equal(t, 1, nPago, "exactly one MSP_PAGOS_RECIBIDOS row")

		// 2) Imagen rows.
		var nImg int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES WHERE PAGO_ID = ?`, pagoID.String(),
		).Scan(&nImg))
		assert.Equal(t, 2, nImg, "exactly two MSP_PAGOS_IMAGENES rows")

		// 3) Blob files on disk.
		for _, imgID := range []uuid.UUID{imgID1, imgID2} {
			blobPath := filepath.Join(storageDir, "pagos", pagoID.String(), imgID.String()+".pdf")
			_, statErr := os.Stat(blobPath)
			require.NoError(t, statErr, "blob must exist on disk at %s", blobPath)
		}

		// 4) Microsip apply fast-path fired once.
		assert.Equal(t, 1, writer.calls(), "post-commit AplicarPago must have fired exactly once")
	})
}

// TestE2E_CrearPagoConImagenes_IdempotentReplay POSTs the same multipart
// twice and verifies: same pago returned, exactly one pago row, no duplicate
// imagenes, and the second request's blobs were cleaned up from disk.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_CrearPagoConImagenes_IdempotentReplay(t *testing.T) {
	e2eRequireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)

		clienteID := e2eClienteID(t, q)
		importe := decimal.RequireFromString("1500.00")
		cargoID := e2eInsertCargo(t, q, clienteID, "E2E-MP-02", importe)
		e2eRequireMigration000010(t, q)
		e2eRequireCargo(ctx, t, q, cargoID)

		storageDir := t.TempDir()
		fsProv, err := storage.NewFilesystemProvider(storageDir)
		require.NoError(t, err)

		repo := cobranzaventfb.NewPagosRecibidosRepo(pool)
		writer := &recordingMicrosipWriter{
			result: cobranzaoutbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "E2E-MP-002"},
		}
		txMgr := firebird.NewTxManager(pool.DB)

		svc := cobranzaapp.NewService(
			cobranzaventfb.NewSaldosRepo(pool),
			cobranzaventfb.NewPagosRepo(pool),
			cobranzaventfb.NewVentasRepo(pool),
			cobranzaoutbound.ProductionClock{},
			repo, repo, writer,
			&cobranzaStorageE2EAdapter{inner: fsProv},
			nil, txMgr,
		)
		router := buildReadRouter(ctx, svc, pagoUser())

		pagoID := uuid.New()
		imgID1 := uuid.New()
		datos := `{"id":"` + pagoID.String() + `",` +
			`"cargo_docto_cc_id":` + itoa(cargoID) + `,` +
			`"cliente_id":` + itoa(clienteID) + `,` +
			`"cobrador_id":42,"cobrador":"Ramírez García, Jorge",` +
			`"importe":"` + importe.StringFixed(2) + `",` +
			`"forma_cobro_id":87327,"fecha_hora_pago":"2026-06-01T09:30:00Z"}`

		// First POST.
		body1, ct1 := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
			{Filename: "a.pdf", Mime: "application/pdf", Body: []byte("first"), ID: imgID1.String()},
		})
		req1 := httptest.NewRequest(http.MethodPost, "/pagos", body1)
		req1.Header.Set("Content-Type", ct1)
		rec1 := httptest.NewRecorder()
		router.ServeHTTP(rec1, req1)
		require.Equal(t, http.StatusOK, rec1.Code, "first: %s", rec1.Body.String())

		// Second POST with same datos.id but a different imagenID.
		imgID2 := uuid.New()
		body2, ct2 := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
			{Filename: "b.pdf", Mime: "application/pdf", Body: []byte("second"), ID: imgID2.String()},
		})
		req2 := httptest.NewRequest(http.MethodPost, "/pagos", body2)
		req2.Header.Set("Content-Type", ct2)
		rec2 := httptest.NewRecorder()
		router.ServeHTTP(rec2, req2)
		require.Equal(t, http.StatusOK, rec2.Code, "second: %s", rec2.Body.String())

		// One pago row, one imagen row.
		var nPago, nImg int
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`, pagoID.String(),
		).Scan(&nPago))
		require.NoError(t, q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES WHERE PAGO_ID = ?`, pagoID.String(),
		).Scan(&nImg))

		assert.Equal(t, 1, nPago, "exactly one pago after replay")
		assert.Equal(t, 1, nImg, "winner's single imagen; replay imagen rejected via idempotency")

		// First blob still present, second blob cleaned up by the cleanup-on-replay path.
		first := filepath.Join(storageDir, "pagos", pagoID.String(), imgID1.String()+".pdf")
		second := filepath.Join(storageDir, "pagos", pagoID.String(), imgID2.String()+".pdf")
		_, statFirst := os.Stat(first)
		_, statSecond := os.Stat(second)
		require.NoError(t, statFirst, "winner's blob persists")
		assert.True(t, os.IsNotExist(statSecond), "replay blob must have been cleaned up; stat=%v", statSecond)
	})
}

// itoa wraps strconv.Itoa for inline use in JSON string concatenation.
func itoa(i int) string {
	return strconv.Itoa(i)
}
