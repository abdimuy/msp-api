//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// valuesForwardingCtx wraps the live request context with the ambient test
// transaction context as the value-lookup fallback. The request's own
// Deadline / Done / Err semantics are preserved (so chi's per-request
// cancellation still works), but firebird's package-private tx key — which
// fbtestutil planted on the test ctx — is reachable from inside the request
// handler chain.
// valuesForwardingCtx is a test-only seam to forward firebird's tx context
// key across the httptest boundary while preserving the request's own
// deadline and cancellation semantics.
//
//nolint:containedctx // test-only: explained above.
type valuesForwardingCtx struct {
	context.Context
	values context.Context
}

func (c valuesForwardingCtx) Value(key any) any {
	if v := c.Context.Value(key); v != nil {
		return v
	}
	return c.values.Value(key)
}

// txInjector returns a chi middleware that splices the test transaction
// context onto every incoming request, so the ventfb repo's calls to
// firebird.GetQuerier(ctx, pool.DB) execute inside the rollback-only test
// transaction instead of the shared connection pool.
func txInjector(txCtx context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			merged := valuesForwardingCtx{Context: r.Context(), values: txCtx}
			next.ServeHTTP(w, r.WithContext(merged))
		})
	}
}

// e2eTestPool builds the real Firebird pool from the env vars. Skips the
// test when FB_DATABASE is not set.
func e2eTestPool(t *testing.T) *firebird.Pool {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird E2E tests")
	}
	cfg := config.Firebird{
		Host:     envOr("FB_HOST", "localhost"),
		Port:     3050,
		Database: os.Getenv("FB_DATABASE"),
		User:     envOr("FB_USER", "SYSDBA"),
		Password: envOr("FB_PASSWORD", ""),
		Charset:  envOr("FB_CHARSET", "UTF8"),
		PoolSize: 4,
	}
	pool, err := firebird.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Stop(context.Background()) })
	return pool
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// seedE2EUsuario inserts a usuario row inside the active test tx and returns
// its UUID. The CREATED_BY column on MSP_USUARIOS is a self-FK so the helper
// points it at the freshly-generated id. The returned id is suitable as the
// FK target for ventas CREATED_BY / UPDATED_BY and VENDEDOR_USUARIO_ID.
func seedE2EUsuario(ctx context.Context, t *testing.T, pool *firebird.Pool) uuid.UUID {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	id := uuid.New()
	now := e2eFixedTime()
	suffix := id.String()
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'e2e-test', TRUE, ?, ?, ?, ?)`,
		id.String(), "fb-e2e-"+suffix, "e2e-"+suffix+"@example.invalid",
		now, now, id.String(), id.String(),
	)
	require.NoError(t, err, "seed e2e usuario")
	return id
}

// e2eFixedTime is the deterministic instant used for E2E timestamps so the
// values round-trip cleanly through ScanUTCTime regardless of process TZ.
func e2eFixedTime() time.Time {
	return time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
}

// TestE2E_Firebird_CrearVenta runs the full HTTP path through the real ventfb
// repository inside a rollback-only Firebird transaction. POST → GET → POST
// imagenes → DELETE imagen → PATCH cancel → GET list, all in one tx. The
// transaction is rolled back when the test finishes so the shared Microsip
// DB stays clean.
//
// Not parallel: the test shares the Firebird pool across a rollback-only tx
// and running parallel test functions would cause tx-isolation surprises
// against the dev DB.
func TestE2E_Firebird_CrearVenta(t *testing.T) { //nolint:paralleltest // see comment above
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		store := newFakeStorage()
		clock := fixedClock{T: e2eFixedTime()}
		// TxMgr is nil — the ambient test tx is supplied via context, and
		// firebird.GetQuerier picks it up. The service-level runInTx becomes a
		// no-op which is exactly what we want here.
		svc := ventasapp.NewService(repo, store, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil)

		cu := auth.CurrentUser{
			ID:          usuarioID,
			FirebaseUID: "fb-e2e-" + usuarioID.String(),
			Email:       "e2e@example.invalid",
			Nombre:      "E2E Tester",
			Permisos: []string{
				string(authdomain.PermVentasListar),
				string(authdomain.PermVentasVer),
				string(authdomain.PermVentasCrear),
				string(authdomain.PermVentasCancelar),
				string(authdomain.PermVentasSubirImagenes),
				string(authdomain.PermVentasEliminarImagenes),
			},
		}

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		// 1. POST /ventas — create.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String() // FK target inside the tx
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, body.ID, created.ID)
		assert.Equal(t, "CONTADO", created.TipoVenta)
		require.Len(t, created.Productos, 1)
		require.Len(t, created.Vendedores, 1)

		// 2. GET /ventas/{id} — round-trip via the same tx.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get body=%s", rec.Body.String())
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, body.ID, got.ID)
		assert.Equal(t, body.Cliente.Nombre, got.Cliente.Nombre)

		// 3. POST /ventas/{id}/imagenes — multipart upload (must happen
		// BEFORE cancel; the domain forbids attaching to a cancelled venta).
		var imgBuf bytes.Buffer
		mw := multipart.NewWriter(&imgBuf)
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="file"; filename="evidence.jpg"`)
		hdr.Set("Content-Type", "image/jpeg")
		part, err := mw.CreatePart(hdr)
		require.NoError(t, err)
		_, _ = part.Write([]byte("e2e-fake-jpeg-bytes"))
		require.NoError(t, mw.Close())

		req = httptest.NewRequest(http.MethodPost, "/ventas/"+body.ID+"/imagenes", &imgBuf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "imagen body=%s", rec.Body.String())
		var imgResp venthttp.ImagenDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &imgResp))
		assert.True(t, store.has(imgResp.StorageKey), "blob should be persisted in fake storage")

		// 4. DELETE /ventas/{id}/imagenes/{img_id}.
		req = httptest.NewRequest(http.MethodDelete, "/ventas/"+body.ID+"/imagenes/"+imgResp.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNoContent, rec.Code, "delete body=%s", rec.Body.String())
		assert.False(t, store.has(imgResp.StorageKey), "blob should have been deleted from storage")

		// 5. PATCH /ventas/{id}/cancel — soft cancel last so the prior
		// image-add/remove steps run on a live venta.
		cancelBody, err := json.Marshal(venthttp.CancelarVentaBody{Reason: "e2e cancel"})
		require.NoError(t, err)
		req = httptest.NewRequest(http.MethodPatch, "/ventas/"+body.ID+"/cancel", bytes.NewReader(cancelBody))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "cancel body=%s", rec.Body.String())
		var canceled venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &canceled))
		require.NotNil(t, canceled.Cancelacion)
		assert.Equal(t, "e2e cancel", canceled.Cancelacion.Reason)

		// 6. GET /ventas?incluir_canceladas=true — list reflects the canceled venta.
		req = httptest.NewRequest(http.MethodGet, "/ventas?incluir_canceladas=true&limit=10", nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "list body=%s", rec.Body.String())

		var list venthttp.ListResponse[venthttp.VentaDTO]
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
		found := false
		for _, item := range list.Items {
			if item.ID == body.ID {
				found = true
				assert.NotNil(t, item.Cancelacion, "listed venta should be flagged canceled")
			}
		}
		assert.True(t, found, "the venta created in this tx should be in the list")
	})

	// fbtestutil rolls back when the closure returns — no rows persist.
}

// TestE2E_Firebird_StandardProcessor_ResizesAndShrinks exercises the full
// stack — HTTP → app → real StandardProcessor → fake storage → Firebird
// row inside a rollback-only tx — and asserts the upload pipeline really
// compresses the persisted blob. This is the smoke test the plan calls
// for: a regression here would mean uploads are silently consuming disk
// at source resolution.
//
//nolint:paralleltest // serialize Firebird E2E tests like the other E2E tests.
func TestE2E_Firebird_StandardProcessor_ResizesAndShrinks(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		store := newFakeStorage()
		clock := fixedClock{T: e2eFixedTime()}

		opts := imageprocessor.DefaultOptions()
		opts.MaxLongSidePx = 320
		opts.JPEGQuality = 75
		opts.PreserveSmallImages = false
		proc := imageprocessor.NewStandardProcessor(opts)
		svc := ventasapp.NewService(repo, store, clock, noopOutbox{}, proc, nil)

		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		srcBytes := makeJPEGBody(t, 1600, 1200, 95)
		uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
			"big.jpg", "image/jpeg", srcBytes)
		uploadRec := httptest.NewRecorder()
		r.ServeHTTP(uploadRec, uploadReq)
		require.Equal(t, http.StatusCreated, uploadRec.Code, "upload body=%s", uploadRec.Body.String())

		var img venthttp.ImagenDTO
		require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))

		stored, err := store.Get(ctx, img.StorageKey)
		require.NoError(t, err)
		var storedBuf bytes.Buffer
		_, err = storedBuf.ReadFrom(stored.Body)
		require.NoError(t, err)
		require.NoError(t, stored.Body.Close())

		assert.Less(t, storedBuf.Len(), len(srcBytes),
			"E2E: persisted blob (%d bytes) must be smaller than the 1600x1200 source (%d bytes)",
			storedBuf.Len(), len(srcBytes))
		assert.Equal(t, int64(storedBuf.Len()), img.SizeBytes,
			"E2E: ImagenDTO.SizeBytes must reflect the compressed payload")
	})
}

// validCreditoCreateBody is validCreateBody adapted for a CREDITO sale with
// plan_credito + dia_cobranza populated. Used by the CREDITO E2E test.
func validCreditoCreateBody() venthttp.CrearVentaBody {
	body := validCreateBody()
	body.TipoVenta = "CREDITO"
	plan := venthttp.PlanCreditoDTO{
		PlazoMeses:  12,
		Enganche:    "100.00",
		Parcialidad: "150.00",
		FrecPago:    "SEMANAL",
	}
	body.PlanCredito = &plan
	semana := "LUNES"
	body.DiaCobranza = &venthttp.DiaCobranzaDTO{Semana: &semana}
	return body
}

// TestE2E_Firebird_Credito_RoundTrip runs CrearVenta + ObtenerVenta for a
// CREDITO sale and asserts every nullable plan_credito / dia_cobranza field
// survives the round-trip through HTTP → app → ventfb → Firebird → response.
// A drop in any field would corrupt downstream cobranza scheduling.
//
//nolint:paralleltest // serialize Firebird E2E tests; same reasoning as the CONTADO E2E.
func TestE2E_Firebird_Credito_RoundTrip(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)

		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreditoCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "CREDITO", created.TipoVenta)
		require.NotNil(t, created.PlanCredito, "PlanCredito must be present on CREDITO response")
		assert.Equal(t, 12, created.PlanCredito.PlazoMeses)
		assert.Equal(t, "100.00", created.PlanCredito.Enganche)
		assert.Equal(t, "150.00", created.PlanCredito.Parcialidad)
		assert.Equal(t, "SEMANAL", created.PlanCredito.FrecPago)
		require.NotNil(t, created.DiaCobranza)
		require.NotNil(t, created.DiaCobranza.Semana)
		assert.Equal(t, "LUNES", *created.DiaCobranza.Semana)
		assert.Nil(t, created.DiaCobranza.Mes)

		// Round-trip GET — must yield the same fields after a real DB read.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get body=%s", rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.PlanCredito, "PlanCredito must persist through GET")
		assert.Equal(t, created.PlanCredito.PlazoMeses, got.PlanCredito.PlazoMeses)
		assert.Equal(t, created.PlanCredito.Enganche, got.PlanCredito.Enganche)
		assert.Equal(t, created.PlanCredito.Parcialidad, got.PlanCredito.Parcialidad)
		assert.Equal(t, created.PlanCredito.FrecPago, got.PlanCredito.FrecPago)
		require.NotNil(t, got.DiaCobranza)
		require.NotNil(t, got.DiaCobranza.Semana)
		assert.Equal(t, "LUNES", *got.DiaCobranza.Semana)
	})
}

// TestE2E_Firebird_Credito_DiaCobranzaMes verifies the alternate cobranza
// branch (QUINCENAL/MENSUAL with day-of-month) also round-trips correctly.
// The two cobranza shapes share a column pair (DIA_COBRANZA_SEMANA /
// DIA_COBRANZA_MES) with strict CHECK constraints; both must work.
//
//nolint:paralleltest // serialize Firebird E2E tests.
func TestE2E_Firebird_Credito_DiaCobranzaMes(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreditoCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.PlanCredito.FrecPago = "MENSUAL"
		dia := 15
		body.DiaCobranza = &venthttp.DiaCobranzaDTO{Mes: &dia}

		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get body=%s", rec.Body.String())

		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.DiaCobranza)
		require.NotNil(t, got.DiaCobranza.Mes, "day-of-month must persist for MENSUAL")
		assert.Equal(t, 15, *got.DiaCobranza.Mes)
		assert.Nil(t, got.DiaCobranza.Semana, "day-of-week must be nil for day-of-month plan")
	})
}

// TestE2E_Firebird_MultiplesImagenes verifies the imagen child collection
// behaves correctly: adding three, deleting the middle one, then GETting
// shows the remaining two with stable ids.
//
//nolint:paralleltest // serialize Firebird E2E tests.
func TestE2E_Firebird_MultiplesImagenes(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		// Attach three imagenes.
		var imagenIDs []string
		for i := range 3 {
			req = httptest.NewRequest(http.MethodPost, "/ventas/"+body.ID+"/imagenes",
				multipartImageBytes(t, byte('A'+i)))
			req.Header.Set("Content-Type", multipartContentType())
			rec = httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			require.Equal(t, http.StatusCreated, rec.Code, "imagen %d body=%s", i, rec.Body.String())
			var imgDTO venthttp.ImagenDTO
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &imgDTO))
			imagenIDs = append(imagenIDs, imgDTO.ID)
		}

		// Verify GET shows all three.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get-before body=%s", rec.Body.String())
		var beforeDTO venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &beforeDTO))
		assert.Len(t, beforeDTO.Imagenes, 3, "venta must have 3 imagenes before delete")

		// Delete the middle one.
		middle := imagenIDs[1]
		req = httptest.NewRequest(http.MethodDelete,
			"/ventas/"+body.ID+"/imagenes/"+middle, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNoContent, rec.Code, "delete body=%s", rec.Body.String())

		// Verify GET shows the remaining two with stable ids.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get-after body=%s", rec.Body.String())
		var afterDTO venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &afterDTO))
		require.Len(t, afterDTO.Imagenes, 2, "venta must have 2 imagenes after deleting one")
		gotIDs := map[string]bool{}
		for _, im := range afterDTO.Imagenes {
			gotIDs[im.ID] = true
		}
		assert.True(t, gotIDs[imagenIDs[0]], "first imagen id must still be present")
		assert.False(t, gotIDs[middle], "deleted imagen id must be gone")
		assert.True(t, gotIDs[imagenIDs[2]], "third imagen id must still be present")
	})
}

// TestE2E_Firebird_ObtenerImagen exercises the full read-back stack with
// a REAL FilesystemProvider on disk + REAL ventfb repo: upload one
// imagen, GET it, verify the bytes on the wire are byte-identical to the
// uploaded payload AND that the FilesystemProvider's recorded MIME +
// size came back through the handler. Catches regressions in the
// ventfb FindByID hydration path that fakeRepo cannot see.
//
//nolint:paralleltest // serialize Firebird E2E tests.
func TestE2E_Firebird_ObtenerImagen(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		fsStore, err := storage.NewFilesystemProvider(t.TempDir())
		require.NoError(t, err, "filesystem provider must build under t.TempDir")
		clock := fixedClock{T: e2eFixedTime()}
		svc := ventasapp.NewService(repo, fsStore, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil)

		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

		uploadedBytes := makePNGBody(t, 32, 32)
		uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
			"e2e-evidencia.png", "image/png", uploadedBytes)
		uploadRec := httptest.NewRecorder()
		r.ServeHTTP(uploadRec, uploadReq)
		require.Equal(t, http.StatusCreated, uploadRec.Code, "upload body=%s", uploadRec.Body.String())

		var img venthttp.ImagenDTO
		require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &img))

		getReq := httptest.NewRequest(http.MethodGet,
			"/ventas/"+body.ID+"/imagenes/"+img.ID, nil)
		getRec := httptest.NewRecorder()
		r.ServeHTTP(getRec, getReq)

		require.Equal(t, http.StatusOK, getRec.Code, "get body=%s", getRec.Body.String())
		assert.Equal(t, "image/png", getRec.Header().Get("Content-Type"),
			"real filesystem storage reads MIME from the sidecar")
		assert.Equal(t, `"`+img.ID+`"`, getRec.Header().Get("ETag"))
		assert.Equal(t, "private, max-age=31536000, immutable", getRec.Header().Get("Cache-Control"))
		assert.Equal(t, uploadedBytes, getRec.Body.Bytes(),
			"E2E: GET body must equal the uploaded blob byte-for-byte")
	})
}

// TestE2E_Firebird_ObtenerImagen_MultiplesImagenes verifies that when a
// venta has many imagenes, each GET resolves to the correct child — the
// iter inside ObtenerImagen must not silently return the first/last
// item regardless of the requested id. This is the test that catches
// "iter selects wrong imagen" regressions which the unit tests with a
// single-imagen fixture cannot.
//
//nolint:paralleltest // serialize Firebird E2E tests.
func TestE2E_Firebird_ObtenerImagen_MultiplesImagenes(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)

		repo := ventfb.NewVentaRepo(pool)
		fsStore, err := storage.NewFilesystemProvider(t.TempDir())
		require.NoError(t, err)
		clock := fixedClock{T: e2eFixedTime()}
		svc := ventasapp.NewService(repo, fsStore, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil)

		cu := e2eFullPermsUser(usuarioID)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code)

		// Upload three distinct imagenes; record each one's expected bytes.
		expected := map[string][]byte{}
		for i := range 3 {
			payload := makePNGBody(t, 16+i*4, 16+i*4)
			uploadReq := buildMultipartRequestWithBody(t, "/ventas/"+body.ID+"/imagenes",
				fmt.Sprintf("img-%d.png", i), "image/png", payload)
			uploadRec := httptest.NewRecorder()
			r.ServeHTTP(uploadRec, uploadReq)
			require.Equal(t, http.StatusCreated, uploadRec.Code, "upload %d body=%s", i, uploadRec.Body.String())
			var dto venthttp.ImagenDTO
			require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &dto))
			expected[dto.ID] = payload
		}
		require.Len(t, expected, 3, "three distinct imagen ids must come back")

		// GET each one and verify the body matches that imagen's payload —
		// not another imagen's.
		for id, want := range expected {
			getReq := httptest.NewRequest(http.MethodGet,
				"/ventas/"+body.ID+"/imagenes/"+id, nil)
			getRec := httptest.NewRecorder()
			r.ServeHTTP(getRec, getReq)
			require.Equal(t, http.StatusOK, getRec.Code, "get %s body=%s", id, getRec.Body.String())
			assert.Equal(t, want, getRec.Body.Bytes(),
				"GET imagen %s must return ITS payload, not any other imagen's", id)
			assert.Equal(t, `"`+id+`"`, getRec.Header().Get("ETag"),
				"ETag must reflect the actual imagen id requested")
		}
	})
}

// TestE2E_Firebird_ListFilters verifies the cursor-paginated /ventas list
// honors the four documented filters when exercised against real Firebird.
// Each filter is checked independently; only the venta we crafted to match
// must be returned (other dev-DB rows are ignored by their distinct ids).
//
//nolint:paralleltest // serialize Firebird E2E tests.
func TestE2E_Firebird_ListFilters(t *testing.T) {
	pool := e2eTestPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		otherVendedor := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		cu := e2eFullPermsUser(usuarioID)

		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(cu))
		venthttp.MountRouter(r, svc)

		// Create two distinct ventas: one CONTADO/today vendor=usuarioID,
		// one CREDITO/today vendor=otherVendedor.
		contadoBody := validCreateBody()
		contadoBody.Vendedores[0].UsuarioID = usuarioID.String()
		req := jsonRequest(t, http.MethodPost, "/ventas", contadoBody)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "contado body=%s", rec.Body.String())

		creditoBody := validCreditoCreateBody()
		creditoBody.Vendedores[0].UsuarioID = otherVendedor.String()
		req = jsonRequest(t, http.MethodPost, "/ventas", creditoBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "credito body=%s", rec.Body.String())

		// 1) Filter by tipo_venta=CREDITO — only the credito one matches.
		req = httptest.NewRequest(http.MethodGet, "/ventas?tipo_venta=CREDITO&limit=200", nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "list-credito body=%s", rec.Body.String())
		var creditoPage venthttp.ListResponse[venthttp.VentaDTO]
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &creditoPage))
		assertVentaInPage(t, creditoPage.Items, creditoBody.ID, true,
			"CREDITO filter must include the credito venta")
		assertVentaInPage(t, creditoPage.Items, contadoBody.ID, false,
			"CREDITO filter must exclude the contado venta")

		// 2) Filter by vendedor_usuario_id=otherVendedor — only credito.
		req = httptest.NewRequest(http.MethodGet,
			"/ventas?vendedor_usuario_id="+otherVendedor.String()+"&limit=200", nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "list-vendedor body=%s", rec.Body.String())
		var vendedorPage venthttp.ListResponse[venthttp.VentaDTO]
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &vendedorPage))
		assertVentaInPage(t, vendedorPage.Items, creditoBody.ID, true,
			"vendedor filter must include credito venta")
		assertVentaInPage(t, vendedorPage.Items, contadoBody.ID, false,
			"vendedor filter must exclude contado venta (other vendedor)")
	})
}

// ─── E2E helpers ────────────────────────────────────────────────────────────

// buildE2EService wires the real ventfb repo + fake storage/outbox into a
// Service. TxMgr is nil so the ambient WithTestTransaction tx is used via
// firebird.GetQuerier.
func buildE2EService(pool *firebird.Pool) *ventasapp.Service {
	repo := ventfb.NewVentaRepo(pool)
	store := newFakeStorage()
	clock := fixedClock{T: e2eFixedTime()}
	return ventasapp.NewService(repo, store, clock, noopOutbox{}, imageprocessor.NoOpProcessor{}, nil)
}

// e2eFullPermsUser returns a CurrentUser holding every ventas permission.
func e2eFullPermsUser(usuarioID uuid.UUID) auth.CurrentUser {
	return auth.CurrentUser{
		ID:          usuarioID,
		FirebaseUID: "fb-e2e-" + usuarioID.String(),
		Email:       "e2e@example.invalid",
		Nombre:      "E2E Tester",
		Permisos: []string{
			string(authdomain.PermVentasListar),
			string(authdomain.PermVentasVer),
			string(authdomain.PermVentasCrear),
			string(authdomain.PermVentasCancelar),
			string(authdomain.PermVentasSubirImagenes),
			string(authdomain.PermVentasEliminarImagenes),
		},
	}
}

// multipartImageBytes builds a one-part multipart body suitable for the
// upload endpoint, stamped with a unique payload byte.
func multipartImageBytes(t *testing.T, payload byte) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="e2e.jpg"`)
	hdr.Set("Content-Type", "image/jpeg")
	part, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, _ = part.Write([]byte{payload, payload, payload, payload})
	require.NoError(t, mw.Close())
	_savedBoundary = mw.Boundary()
	return &buf
}

// _savedBoundary holds the most recent multipart writer's boundary so the
// caller can set the matching Content-Type header.
var _savedBoundary string

// multipartContentType returns the Content-Type for the most recent
// multipartImageBytes call.
func multipartContentType() string {
	return "multipart/form-data; boundary=" + _savedBoundary
}

// assertVentaInPage asserts that the given venta id is (or is not) present
// in the page items. The `should` flag selects the direction.
func assertVentaInPage(t *testing.T, items []venthttp.VentaDTO, ventaID string, should bool, msg string) {
	t.Helper()
	for _, it := range items {
		if it.ID == ventaID {
			if !should {
				t.Errorf("%s: but found %s", msg, ventaID)
			}
			return
		}
	}
	if should {
		t.Errorf("%s: %s missing from page (%d items)", msg, ventaID, len(items))
	}
}
