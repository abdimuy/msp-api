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

// fakeZonaNombreResolver maps Microsip zona ids to display names, counting
// calls so the listing test can assert the resolver runs ONCE per page rather
// than once per venta.
type fakeZonaNombreResolver struct {
	nombres map[int]string
	calls   int
}

func (f *fakeZonaNombreResolver) NombresPorID(_ context.Context, ids []int) (map[int]string, error) {
	f.calls++
	out := make(map[int]string, len(ids))
	for _, id := range ids {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// TestObtenerVenta_ResolvesZonaClienteNombre verifies GET /ventas/{id} carries
// the zona NAME next to its id, so the desktop does not have to resolve it
// against a local catalog.
func TestObtenerVenta_ResolvesZonaClienteNombre(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	svc.WithZonaNombreResolver(&fakeZonaNombreResolver{nombres: map[int]string{
		21563: "TEHUACAN NORTE",
	}})
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	body.Direccion.ZonaClienteID = intPtr(21563)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, crearVentaMultipartRequest(t, body))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil))
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	require.NotNil(t, got.Direccion.ZonaCliente)
	assert.Equal(t, "TEHUACAN NORTE", *got.Direccion.ZonaCliente)
}

// TestObtenerVenta_OmitsZonaClienteWithoutResolver verifies the field stays
// absent (omitempty) when no resolver is wired — the read must not break.
func TestObtenerVenta_OmitsZonaClienteWithoutResolver(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	body.Direccion.ZonaClienteID = intPtr(21563)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, crearVentaMultipartRequest(t, body))
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil))
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	assert.NotContains(t, getRec.Body.String(), `"zona_cliente"`)

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	assert.Nil(t, got.Direccion.ZonaCliente)
	require.NotNil(t, got.Direccion.ZonaClienteID)
}

// TestListarVentas_ResolvesZonaClienteNombreOncePerPage verifies GET /ventas
// decorates every item with its zona name and resolves the whole page in a
// SINGLE resolver call.
func TestListarVentas_ResolvesZonaClienteNombreOncePerPage(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	zonas := &fakeZonaNombreResolver{nombres: map[int]string{
		21563: "TEHUACAN NORTE",
		99:    "TEHUACAN SUR",
	}}
	svc.WithZonaNombreResolver(zonas)
	r := buildRouter(t, svc, cu)

	for _, zona := range []int{21563, 21563, 99} {
		body := validCreateBody()
		body.Direccion.ZonaClienteID = intPtr(zona)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, crearVentaMultipartRequest(t, body))
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	}

	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, httptest.NewRequest(http.MethodGet, "/ventas", nil))
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())

	var page struct {
		Items []venthttp.VentaDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &page))
	require.Len(t, page.Items, 3)

	nombres := make([]string, 0, len(page.Items))
	for _, item := range page.Items {
		require.NotNil(t, item.Direccion.ZonaCliente, "every listed venta must carry its zona name")
		nombres = append(nombres, *item.Direccion.ZonaCliente)
	}
	assert.ElementsMatch(t, []string{"TEHUACAN NORTE", "TEHUACAN NORTE", "TEHUACAN SUR"}, nombres)
	assert.Equal(t, 1, zonas.calls, "the page must be resolved in a single batched call")
}
