//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// countingFaseResolver answers with a fixed map and counts calls, so the
// listing test can prove the whole page is resolved in one round trip.
type countingFaseResolver struct {
	fases   map[uuid.UUID]ventasoutbound.FaseDeVenta
	calls   int
	lastIDs []uuid.UUID
}

func (f *countingFaseResolver) FasesPorVenta(
	_ context.Context, ids []uuid.UUID,
) (map[uuid.UUID]ventasoutbound.FaseDeVenta, error) {
	f.calls++
	f.lastIDs = append([]uuid.UUID(nil), ids...)
	out := make(map[uuid.UUID]ventasoutbound.FaseDeVenta, len(ids))
	for _, id := range ids {
		if t, ok := f.fases[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}

// TestObtenerVenta_CarriesFases verifies GET /ventas/{id} surfaces WHEN the
// venta entered its current fase (RFC3339 UTC) and the highest fase it ever
// reached.
func TestObtenerVenta_CarriesFases(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, crearVentaMultipartRequest(t, body))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	ventaID := uuid.MustParse(body.ID)
	entroEn := time.Date(2026, 8, 12, 17, 30, 0, 0, time.UTC)
	svc.WithFaseResolver(&countingFaseResolver{
		fases: map[uuid.UUID]ventasoutbound.FaseDeVenta{
			ventaID: {Desde: entroEn, Alcanzada: 3},
		},
	})

	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil))
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	require.NotNil(t, got.FaseDesde)
	assert.Equal(t, "2026-08-12T17:30:00Z", *got.FaseDesde)
	require.NotNil(t, got.FaseAlcanzada)
	assert.Equal(t, 3, *got.FaseAlcanzada)
}

// TestListarVentas_ResolvesFasesOncePerPage verifies the listing carries
// fase_desde and fase_alcanzada on the ventas that have them, omits both on
// those that do not, and resolves the page in a SINGLE call.
func TestListarVentas_ResolvesFasesOncePerPage(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	conFase := crearVenta(t, r)
	sinFase := crearVenta(t, r)

	entroEn := time.Date(2026, 8, 13, 9, 0, 0, 0, time.UTC)
	fases := &countingFaseResolver{fases: map[uuid.UUID]ventasoutbound.FaseDeVenta{
		uuid.MustParse(conFase): {Desde: entroEn, Alcanzada: 2},
	}}
	svc.WithFaseResolver(fases)

	items := listVentas(t, r)
	require.Len(t, items, 2)
	assert.Equal(t, 1, fases.calls, "one resolver call for the whole page (no N+1)")
	assert.ElementsMatch(t,
		[]uuid.UUID{uuid.MustParse(conFase), uuid.MustParse(sinFase)}, fases.lastIDs)

	byID := make(map[string]venthttp.VentaDTO, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	require.NotNil(t, byID[conFase].FaseDesde)
	assert.Equal(t, "2026-08-13T09:00:00Z", *byID[conFase].FaseDesde)
	require.NotNil(t, byID[conFase].FaseAlcanzada)
	assert.Equal(t, 2, *byID[conFase].FaseAlcanzada,
		"the listing carries the highest fase the venta ever reached")
	assert.Nil(t, byID[sinFase].FaseDesde,
		"a venta with no phase event carries no fase_desde — never an invented date")
	assert.Nil(t, byID[sinFase].FaseAlcanzada,
		"…and no fase_alcanzada either — never an invented arc")
}

// TestListarVentas_OmitsFasesWithoutResolver verifies both fields are absent
// from the JSON entirely when no resolver is wired, so the frontend's
// "sin dato" branch is what it actually sees.
func TestListarVentas_OmitsFasesWithoutResolver(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))
	crearVenta(t, r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ventas", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `"fase_desde"`)
	assert.NotContains(t, rec.Body.String(), `"fase_alcanzada"`)
}
