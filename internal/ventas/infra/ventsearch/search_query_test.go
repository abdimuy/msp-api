package ventsearch_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	ventsearchmeili "github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }

// ── buildFilter: each filter nil vs set ──────────────────────────────────

func TestBuildFilter_NoFilters_ExcludesCanceladasOnly(t *testing.T) {
	t.Parallel()
	filter := ventsearchmeili.BuildFilterForTest(outbound.VentasSearchQuery{})
	assert.Equal(t, `situacion != "cancelada"`, filter)
}

func TestBuildFilter_TipoVenta_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{TipoVenta: strp("CREDITO"), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, `tipo_venta = "CREDITO"`, filter)
}

func TestBuildFilter_TipoVenta_Nil_Omits(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "tipo_venta")
}

func TestBuildFilter_Situacion_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{Situacion: strp("aprobada"), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `situacion = "aprobada"`)
}

func TestBuildFilter_Sincronizacion_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{Sincronizacion: strp("aplicada"), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, `sincronizacion = "aplicada"`, filter)
}

func TestBuildFilter_ZonaClienteID_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{ZonaClienteID: intp(7), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, "zona_cliente_id = 7", filter)
}

func TestBuildFilter_ZonaClienteID_Nil_Omits(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "zona_cliente_id")
}

func TestBuildFilter_VendedorEmail_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{VendedorEmail: strp("v@muebleriamsp.mx"), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	// Meilisearch matches an array attribute with `=` when ANY element equals
	// the value, so a venta with several vendedores is found by any of them.
	assert.Equal(t, `vendedor_emails = "v@muebleriamsp.mx"`, filter)
}

func TestBuildFilter_ClienteID_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{ClienteID: intp(42), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, "cliente_id = 42", filter)
}

func TestBuildFilter_Estado_Set(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{Estado: strp("active"), IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, `estado = "active"`, filter)
}

// ── buildFilter: incluir_canceladas ───────────────────────────────────────

func TestBuildFilter_IncluirCanceladas_False_ExcludesCancelada(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{IncluirCanceladas: false}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `situacion != "cancelada"`)
}

// Asking explicitly for situacion="cancelada" must not be silently negated by
// the canceladas guard: `situacion = "cancelada" AND situacion != "cancelada"`
// matches nothing, so the screen shows an empty list with no error. The
// Firebird fallback already carries this coherence rule in
// appendCanceladasFilter; both paths must agree.
func TestBuildFilter_SituacionCancelada_WithoutIncluirCanceladas_OmitsExclusion(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{Situacion: strp("cancelada"), IncluirCanceladas: false}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, `situacion = "cancelada"`, filter)
	assert.NotContains(t, filter, `!=`)
}

// The guard still applies when the caller filters by any other situacion:
// "aprobada" and "cancelada" are mutually exclusive, so keeping the exclusion
// costs nothing and preserves the default of hiding canceladas.
func TestBuildFilter_SituacionAprobada_WithoutIncluirCanceladas_KeepsExclusion(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{Situacion: strp("aprobada"), IncluirCanceladas: false}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `situacion = "aprobada"`)
	assert.Contains(t, filter, `situacion != "cancelada"`)
}

func TestBuildFilter_IncluirCanceladas_True_OmitsExclusion(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "cancelada")
}

// ── buildFilter: fecha/precio ranges ──────────────────────────────────────

func TestBuildFilter_FechaDesde_Set(t *testing.T) {
	t.Parallel()
	fecha := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	q := outbound.VentasSearchQuery{FechaDesde: &fecha, IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, "fecha_venta_ts >= 1767225600", filter)
}

func TestBuildFilter_FechaHasta_Set(t *testing.T) {
	t.Parallel()
	fecha := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	q := outbound.VentasSearchQuery{FechaHasta: &fecha, IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, "fecha_venta_ts < 1798675200", filter)
}

func TestBuildFilter_FechaRange_Nil_Omits(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "fecha_venta_ts")
}

func TestBuildFilter_PrecioMin_Set(t *testing.T) {
	t.Parallel()
	precioMin := decimal.RequireFromString("1500.50")
	q := outbound.VentasSearchQuery{PrecioMin: &precioMin, IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, "precio_total >= 1500.5", filter)
}

func TestBuildFilter_PrecioMax_Set(t *testing.T) {
	t.Parallel()
	precioMax := decimal.RequireFromString("9999.99")
	q := outbound.VentasSearchQuery{PrecioMax: &precioMax, IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Equal(t, "precio_total < 9999.99", filter)
}

func TestBuildFilter_PrecioRange_Nil_Omits(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{IncluirCanceladas: true}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.NotContains(t, filter, "precio_total")
}

// ── buildFilter: multiple clauses combine with AND ────────────────────────

func TestBuildFilter_CombinesMultipleClausesWithAND(t *testing.T) {
	t.Parallel()
	q := outbound.VentasSearchQuery{
		TipoVenta:     strp("CONTADO"),
		ZonaClienteID: intp(3),
	}
	filter := ventsearchmeili.BuildFilterForTest(q)
	assert.Contains(t, filter, `tipo_venta = "CONTADO"`)
	assert.Contains(t, filter, "zona_cliente_id = 3")
	assert.Contains(t, filter, `situacion != "cancelada"`)
	assert.Contains(t, filter, " AND ")
}

// ── buildSort: each SortBy/SortOrder ───────────────────────────────────────

func TestBuildSort_FechaVenta_Desc(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{SortBy: "fecha_venta", SortOrder: "desc"})
	assert.Equal(t, []string{"fecha_venta_ts:desc"}, sort)
}

func TestBuildSort_FechaVenta_DefaultsToAsc(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{SortBy: "fecha_venta"})
	assert.Equal(t, []string{"fecha_venta_ts:asc"}, sort)
}

func TestBuildSort_PrecioTotal_Desc(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{SortBy: "precio_total", SortOrder: "desc"})
	assert.Equal(t, []string{"precio_total:desc"}, sort)
}

func TestBuildSort_PrecioTotal_Asc(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{SortBy: "precio_total", SortOrder: "asc"})
	assert.Equal(t, []string{"precio_total:asc"}, sort)
}

func TestBuildSort_NombreCliente_Asc(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{SortBy: "nombre_cliente", SortOrder: "asc"})
	assert.Equal(t, []string{"nombre_cliente:asc"}, sort)
}

func TestBuildSort_UnknownSortBy_ReturnsNil(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{SortBy: "unknown_field"})
	assert.Nil(t, sort)
}

// ── buildSort: default (no SortBy) ────────────────────────────────────────

func TestBuildSort_NoSortBy_NoQuery_DefaultsToFechaVentaDesc(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{})
	assert.Equal(t, []string{"fecha_venta_ts:desc"}, sort)
}

func TestBuildSort_NoSortBy_WithQuery_UsesRelevance(t *testing.T) {
	t.Parallel()
	sort := ventsearchmeili.BuildSortForTest(outbound.VentasSearchQuery{Q: "juan perez"})
	assert.Nil(t, sort)
}
