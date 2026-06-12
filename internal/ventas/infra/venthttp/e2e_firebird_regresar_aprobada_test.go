//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// TestE2E_Firebird_Aprobada_Regresar_Edit_Reaprobar exercises the full
// edit-window-reopen lifecycle: create CRÉDITO → revisar → aprobar →
// regresar-borrador → PATCH /ventas/{id} to change the plan → revisar again
// → aprobar again. The point is to confirm that once the regress succeeds the
// venta becomes editable again (so the dueño can fix a captured-wrong plazo,
// which is what motivated this whole feature) and can complete the flow up
// through aprobada a second time.
//
//nolint:paralleltest // E2E tests share a tx and must run serially.
func TestE2E_Firebird_Aprobada_Regresar_Edit_Reaprobar(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eAllPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.TipoVenta = "CREDITO"
		body.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses:  8,
			Enganche:    "100",
			Parcialidad: "120",
			FrecPago:    "QUINCENAL",
		}
		mes := 15
		body.DiaCobranza = &venthttp.DiaCobranzaDTO{Mes: &mes}

		// 1. CREAR (status borrador)
		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create: %s", rec.Body.String())

		// 2. REVISAR
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/revisar", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "revisar: %s", rec.Body.String())

		// 3. APROBAR
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/aprobar", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "aprobar: %s", rec.Body.String())
		var aprobada venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &aprobada))
		require.Equal(t, "aprobada", aprobada.Situacion)
		require.NotNil(t, aprobada.Aprobacion, "aprobacion must be set after aprobar")

		// 4. REGRESAR A BORRADOR — the new path (was: only legal from revisada).
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/regresar-borrador", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "regresar from aprobada: %s", rec.Body.String())
		var regresada venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &regresada))
		assert.Equal(t, "borrador", regresada.Situacion)
		assert.Nil(t, regresada.Aprobacion, "aprobacion must be cleared after regress")

		// 5. EDITAR el plan (cambia plazo 8 → 9). El header debe ser editable.
		newMes := 15
		editBody := venthttp.ActualizarHeaderBody{
			Direccion:  body.Direccion,
			GPS:        body.GPS,
			FechaVenta: body.FechaVenta,
			Montos:     body.Montos,
			PlanCredito: &venthttp.PlanCreditoDTO{
				PlazoMeses:  9,
				Enganche:    "100",
				Parcialidad: "120",
				FrecPago:    "QUINCENAL",
			},
			DiaCobranza: &venthttp.DiaCobranzaDTO{Mes: &newMes},
		}
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID, editBody))
		require.Equal(t, http.StatusOK, rec.Code, "edit header after regress: %s", rec.Body.String())
		var edited venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &edited))
		require.NotNil(t, edited.PlanCredito)
		assert.Equal(t, 9, edited.PlanCredito.PlazoMeses,
			"plazo must reflect the post-regress edit (was 8 before)")

		// 6. RE-REVISAR
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/revisar", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "re-revisar: %s", rec.Body.String())

		// 7. RE-APROBAR
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/aprobar", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "re-aprobar: %s", rec.Body.String())
		var reaprobada venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reaprobada))
		assert.Equal(t, "aprobada", reaprobada.Situacion)
		require.NotNil(t, reaprobada.PlanCredito)
		assert.Equal(t, 9, reaprobada.PlanCredito.PlazoMeses,
			"the re-approved venta must carry the edited plazo, not the original")
		assert.NotNil(t, reaprobada.Aprobacion, "aprobacion must be set again")
	})
}

// TestE2E_Firebird_Aplicada_Regresar_Rejected is the load-bearing safety
// test: a venta already materialized in Microsip (sincronizacion=aplicada)
// must NEVER regress to borrador, otherwise we orphan the DOCTOS_PV row. We
// drive the venta to APROBADA via the public surface and then forcibly mark
// it APLICADA at the domain layer + persist the row (the full AplicarVenta
// wiring requires a Microsip-mapped catalog that this slim test harness
// doesn't have). The handler must still 409.
//
//nolint:paralleltest // E2E tests share a tx and must run serially.
func TestE2E_Firebird_Aplicada_Regresar_Rejected(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eAllPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()

		req := crearVentaMultipartRequest(t, body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create: %s", rec.Body.String())

		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/revisar", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "revisar: %s", rec.Body.String())
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/aprobar", struct{}{}))
		require.Equal(t, http.StatusOK, rec.Code, "aprobar: %s", rec.Body.String())

		// Forcibly mark the persisted venta as aplicada via a direct write —
		// the AplicarVenta machinery would need a full Microsip catalog
		// setup that's tested elsewhere (e2e_firebird_extra_test.go) and
		// orthogonal to this safety check.
		q := firebird.GetQuerier(ctx, pool.DB)
		_, err := q.ExecContext(ctx,
			`UPDATE MSP_VENTAS
			   SET SINCRONIZACION = 'aplicada',
			       MICROSIP_DOCTO_PV_ID = ?,
			       MICROSIP_FOLIO = ?,
			       MICROSIP_APLICADA_AT = ?
			 WHERE ID = ?`,
			15239197,
			"Y00099999",
			e2eFixedTime(),
			body.ID,
		)
		require.NoError(t, err, "force-mark aplicada via direct UPDATE")

		// Now the regress must 409 — and the row must remain aprobada/aplicada.
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/regresar-borrador", struct{}{}))
		require.Equal(t, http.StatusConflict, rec.Code, "regresar after aplicada: %s", rec.Body.String())
		assert.Contains(t, rec.Body.String(), "venta_no_regresable_a_borrador")

		// Read back via GET and confirm nothing was mutated.
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "aprobada", got.Situacion, "situacion must NOT change on a rejected regress")
		assert.Equal(t, "aplicada", got.Sincronizacion, "sincronizacion must remain aplicada")
		require.NotNil(t, got.MicrosipDoctoPVID)
		assert.Equal(t, 15239197, *got.MicrosipDoctoPVID)

		// And verify the path with an invalid UUID still does the right thing
		// even for the new code path — uuid.NewString gives us a well-formed
		// UUID that doesn't match any row → 404.
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, jsonRequest(t, http.MethodPost, "/ventas/"+uuid.NewString()+"/regresar-borrador", struct{}{}))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}
