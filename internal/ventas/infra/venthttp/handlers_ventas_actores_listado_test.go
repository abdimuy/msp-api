//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// countingNombreResolver maps usuario ids to display names and counts calls,
// so the listing tests can prove the page is resolved ONCE instead of once
// per venta (the N+1 this feature must not introduce).
type countingNombreResolver struct {
	nombres map[uuid.UUID]string
	calls   int
	lastIDs []uuid.UUID
}

func (f *countingNombreResolver) NombresPorID(
	_ context.Context, ids []uuid.UUID,
) (map[uuid.UUID]string, error) {
	f.calls++
	f.lastIDs = append([]uuid.UUID(nil), ids...)
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// listVentas issues GET /ventas through r and decodes the page.
func listVentas(t *testing.T, r http.Handler) []venthttp.VentaDTO {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ventas", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var page struct {
		Items []venthttp.VentaDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	return page.Items
}

// crearVenta posts a fresh venta through r and returns its id.
func crearVenta(t *testing.T, r http.Handler) string {
	t.Helper()
	body := validCreateBody()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, crearVentaMultipartRequest(t, body))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	return body.ID
}

// TestListarVentas_ResolvesActorNombresOncePerPage is the regression test for
// the listing showing raw UUIDs: GET /ventas must carry created_by_nombre and
// updated_by_nombre on every item, resolved for the whole page in a SINGLE
// call.
func TestListarVentas_ResolvesActorNombresOncePerPage(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	creador := uuid.New()
	nombres := &countingNombreResolver{nombres: map[uuid.UUID]string{
		creador: "Ana Vendedora",
	}}
	svc.WithUsuarioResolver(nombres)
	r := buildRouter(t, svc, fullPerms(creador))

	for range 3 {
		crearVenta(t, r)
	}

	items := listVentas(t, r)
	require.Len(t, items, 3)
	for _, item := range items {
		assert.Equal(t, "Ana Vendedora", item.CreatedByNombre,
			"the listing must show people, not UUIDs")
		assert.Equal(t, "Ana Vendedora", item.UpdatedByNombre)
	}
	assert.Equal(t, 1, nombres.calls,
		"the whole page must be resolved in one batched call (no N+1)")
	assert.ElementsMatch(t, []uuid.UUID{creador}, nombres.lastIDs,
		"repeated actor ids must be collapsed before the call")
}

// TestListarVentas_UnresolvedActorLeavesNombreAbsent verifies an actor the
// resolver cannot name degrades to an absent field instead of breaking the
// response.
func TestListarVentas_UnresolvedActorLeavesNombreAbsent(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	desconocido := uuid.New()
	// The resolver knows somebody else entirely.
	svc.WithUsuarioResolver(&countingNombreResolver{nombres: map[uuid.UUID]string{
		uuid.New(): "Otra Persona",
	}})
	r := buildRouter(t, svc, fullPerms(desconocido))
	crearVenta(t, r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ventas", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `"created_by_nombre"`,
		"an unresolved actor leaves the field out, it does not emit an empty string")

	var page struct {
		Items []venthttp.VentaDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	assert.Empty(t, page.Items[0].CreatedByNombre)
	assert.Equal(t, desconocido.String(), page.Items[0].CreatedBy,
		"the raw id is still there for support")
}

// TestListarVentas_ResolvesAprobacionYCancelacionNombres verifies the nested
// aprobacion / cancelacion records also carry by_nombre in the LISTING, not
// only in the detail read.
func TestListarVentas_ResolvesAprobacionYCancelacionNombres(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	creador := uuid.New()
	supervisor := uuid.New()
	nombres := &countingNombreResolver{nombres: map[uuid.UUID]string{
		creador:    "Ana Vendedora",
		supervisor: "Beto Supervisor",
	}}
	svc.WithUsuarioResolver(nombres)
	rCreador := buildRouter(t, svc, fullPerms(creador))
	rSupervisor := buildRouter(t, svc, fullPerms(supervisor))

	aprobada := crearVenta(t, rCreador)
	require.Equal(t, http.StatusOK, do(t, rSupervisor,
		jsonRequest(t, http.MethodPost, "/ventas/"+aprobada+"/revisar", struct{}{})).Code)
	require.Equal(t, http.StatusOK, do(t, rSupervisor,
		jsonRequest(t, http.MethodPost, "/ventas/"+aprobada+"/aprobar", struct{}{})).Code)

	cancelada := crearVenta(t, rCreador)
	require.Equal(t, http.StatusOK, do(t, rSupervisor,
		jsonRequest(t, http.MethodPatch, "/ventas/"+cancelada+"/cancel",
			venthttp.CancelarVentaBody{Reason: "cliente no localizable"})).Code)

	nombres.calls = 0
	items := listVentas(t, rCreador)
	require.Len(t, items, 2)
	assert.Equal(t, 1, nombres.calls, "still one call for the whole page")

	byID := make(map[string]venthttp.VentaDTO, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	ap := byID[aprobada]
	require.NotNil(t, ap.Aprobacion)
	assert.Equal(t, "Beto Supervisor", ap.Aprobacion.ByNombre)

	ca := byID[cancelada]
	require.NotNil(t, ca.Cancelacion)
	assert.Equal(t, "Beto Supervisor", ca.Cancelacion.ByNombre)
	assert.Equal(t, "Ana Vendedora", ca.CreatedByNombre,
		"created_by still resolves to the original capturer")
}
