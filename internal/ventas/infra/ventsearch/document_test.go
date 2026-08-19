package ventsearch_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventsearchmeili "github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// ── DefaultIndexConfig ────────────────────────────────────────────────────

func TestDefaultIndexConfig_UIDAndPrimaryKey(t *testing.T) {
	t.Parallel()
	cfg := ventsearchmeili.DefaultIndexConfig("my-index")
	assert.Equal(t, "my-index", cfg.UID)
	assert.Equal(t, "id", cfg.PrimaryKey)
}

func TestDefaultIndexConfig_SearchableAttributes(t *testing.T) {
	t.Parallel()
	cfg := ventsearchmeili.DefaultIndexConfig("ventas")
	require.NotEmpty(t, cfg.SearchableAttributes)
	for _, expected := range []string{"nombre_cliente", "telefono", "direccion", "folio", "vendedor"} {
		assert.Contains(t, cfg.SearchableAttributes, expected,
			"searchable must include %q", expected)
	}
}

func TestDefaultIndexConfig_FilterableAttributes(t *testing.T) {
	t.Parallel()
	cfg := ventsearchmeili.DefaultIndexConfig("ventas")
	require.NotEmpty(t, cfg.FilterableAttributes)
	for _, expected := range []string{
		"tipo_venta", "situacion", "sincronizacion", "zona_cliente_id",
		"vendedor_emails", "cliente_id", "estado", "fecha_venta_ts", "precio_total",
	} {
		assert.True(t, slices.Contains(cfg.FilterableAttributes, expected),
			"filterable must include %q", expected)
	}
}

func TestDefaultIndexConfig_SortableAttributes(t *testing.T) {
	t.Parallel()
	cfg := ventsearchmeili.DefaultIndexConfig("ventas")
	require.NotEmpty(t, cfg.SortableAttributes)
	for _, expected := range []string{"fecha_venta_ts", "precio_total", "nombre_cliente", "created_at_ts"} {
		assert.Contains(t, cfg.SortableAttributes, expected,
			"sortable must include %q", expected)
	}
}

func TestDefaultIndexConfig_RankingRules(t *testing.T) {
	t.Parallel()
	cfg := ventsearchmeili.DefaultIndexConfig("ventas")
	require.NotEmpty(t, cfg.RankingRules)
	for _, r := range []string{"words", "typo", "proximity", "attribute", "sort", "exactness"} {
		assert.Contains(t, cfg.RankingRules, r, "ranking rules must include %q", r)
	}
}

func TestDefaultIndexConfig_Pagination(t *testing.T) {
	t.Parallel()
	cfg := ventsearchmeili.DefaultIndexConfig("ventas")
	assert.Equal(t, int64(outbound.MaxTotalHitsVentas), cfg.PaginationMaxTotalHits)
}

// TestDefaultIndexConfig_IsPlatformType verifies the return type is the
// platform-level IndexConfig (not a ventas-specific type). The assertion is
// structural: passing the result to a function that accepts
// platformmeili.IndexConfig causes a compile error if the types diverge.
func TestDefaultIndexConfig_IsPlatformType(t *testing.T) {
	t.Parallel()
	acceptsPlatformType(ventsearchmeili.DefaultIndexConfig("x"))
}

func acceptsPlatformType(cfg platformmeili.IndexConfig) {
	_ = cfg.UID // reference to avoid unused-variable linting on cfg
}

// ── mapDoc ────────────────────────────────────────────────────────────────

func TestMapDoc_PrimaryKeyAndIdentityFields(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	doc := outbound.VentaSearchDoc{
		ID:            id,
		NombreCliente: "JUAN PEREZ",
		Telefono:      "3311112222",
		Direccion:     "INSURGENTES CENTRO GUADALAJARA JALISCO",
		Folio:         "F00123",
		Vendedor:      "MARIA LOPEZ, JOSE RAMIREZ",
	}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.Equal(t, id.String(), got.ID)
	assert.Equal(t, "JUAN PEREZ", got.NombreCliente)
	assert.Equal(t, "3311112222", got.Telefono)
	assert.Equal(t, "INSURGENTES CENTRO GUADALAJARA JALISCO", got.Direccion)
	assert.Equal(t, "F00123", got.Folio)
	assert.Equal(t, "MARIA LOPEZ, JOSE RAMIREZ", got.Vendedor)
}

func TestMapDoc_FilterableFields(t *testing.T) {
	t.Parallel()
	doc := outbound.VentaSearchDoc{
		ID:             uuid.New(),
		TipoVenta:      "CREDITO",
		Situacion:      "aprobada",
		Sincronizacion: "pendiente",
		ZonaClienteID:  7,
		VendedorEmails: []string{"vendedor@muebleriamsp.mx", "socio@muebleriamsp.mx"},
		ClienteID:      42,
		Estado:         "active",
	}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.Equal(t, "CREDITO", got.TipoVenta)
	assert.Equal(t, "aprobada", got.Situacion)
	assert.Equal(t, "pendiente", got.Sincronizacion)
	assert.Equal(t, 7, got.ZonaClienteID)
	assert.Equal(t, []string{"vendedor@muebleriamsp.mx", "socio@muebleriamsp.mx"}, got.VendedorEmails)
	assert.Equal(t, 42, got.ClienteID)
	assert.Equal(t, "active", got.Estado)
}

// TestMapDoc_VendedorEmails_NilBecomesEmptyArrayOnWire pins the wire shape:
// a nil slice marshals to JSON `null`, which Meilisearch stores as a null
// attribute rather than an empty array. The document must always carry an
// array so `vendedor_emails = "x"` behaves consistently across documents.
func TestMapDoc_VendedorEmails_NilBecomesEmptyArrayOnWire(t *testing.T) {
	t.Parallel()
	got := ventsearchmeili.MapDocForTest(outbound.VentaSearchDoc{ID: uuid.New()})

	raw, err := json.Marshal(got)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"vendedor_emails":[]`)
}

func TestMapDoc_MoneyRoundTripExact(t *testing.T) {
	t.Parallel()
	// Value chosen so the float64 binary representation is NOT exact —
	// proving the string field (not the numeric precio_total) is what
	// preserves precision for display.
	doc := outbound.VentaSearchDoc{
		ID:          uuid.New(),
		PrecioTotal: decimal.RequireFromString("12345.67"),
	}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.InEpsilon(t, 12345.67, got.PrecioTotal, 0.001, "numeric sort/filter key")
	assert.Equal(t, "12345.67", got.PrecioTotalStr, "exact display value")
}

func TestMapDoc_FechaVenta_EpochAndDisplay(t *testing.T) {
	t.Parallel()
	fecha := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC)
	doc := outbound.VentaSearchDoc{
		ID:         uuid.New(),
		FechaVenta: fecha,
	}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.Equal(t, fecha.Unix(), got.FechaVentaTs, "epoch-seconds sortable/filterable field")
	assert.Equal(t, "2026-07-15T10:30:00Z", got.FechaVenta, "RFC3339 display string")
}

func TestMapDoc_FechaVenta_ZeroTime(t *testing.T) {
	t.Parallel()
	doc := outbound.VentaSearchDoc{ID: uuid.New()}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.Equal(t, int64(0), got.FechaVentaTs, "zero time → 0 epoch")
	assert.Empty(t, got.FechaVenta, "zero time → empty display string")
}

func TestMapDoc_CreatedAt_EpochAndDisplay(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	doc := outbound.VentaSearchDoc{
		ID:        uuid.New(),
		CreatedAt: created,
	}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.Equal(t, created.Unix(), got.CreatedAtTs)
	assert.Equal(t, "2026-01-01T00:00:00Z", got.CreatedAt)
}

func TestMapDoc_CreatedAt_ZeroTime_SortsLast(t *testing.T) {
	t.Parallel()
	doc := outbound.VentaSearchDoc{ID: uuid.New()}

	got := ventsearchmeili.MapDocForTest(doc)
	assert.Equal(t, int64(0), got.CreatedAtTs, "zero time → 0 epoch (omitempty sorts last)")
	assert.Empty(t, got.CreatedAt, "zero time → empty display string")
}

// ── wire format (JSON marshaling) ───────────────────────────────────────────

// TestMapDoc_ZeroTimes_OmitFromWire proves that when both FechaVenta and
// CreatedAt are zero, the marshaled JSON has NEITHER "fecha_venta_ts" NOR
// "created_at_ts" keys — omitempty must drop both symmetrically so
// Meilisearch treats absent-attribute docs as sorting last in both asc and
// desc, instead of an explicit 0 sorting first under asc.
func TestMapDoc_ZeroTimes_OmitFromWire(t *testing.T) {
	t.Parallel()
	doc := outbound.VentaSearchDoc{ID: uuid.New()}

	got := ventsearchmeili.MapDocForTest(doc)
	raw, err := json.Marshal(got)
	require.NoError(t, err)

	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))

	assert.NotContains(t, wire, "fecha_venta_ts", "zero fecha_venta must be absent, not 0")
	assert.NotContains(t, wire, "created_at_ts", "zero created_at must be absent, not 0")
}

// TestMapDoc_RealTimes_PresentOnWire proves that when both FechaVenta and
// CreatedAt are set to real times, the marshaled JSON DOES contain both keys
// with the correct epoch-seconds values.
func TestMapDoc_RealTimes_PresentOnWire(t *testing.T) {
	t.Parallel()
	fecha := time.Date(2026, 7, 15, 10, 30, 0, 0, time.UTC)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	doc := outbound.VentaSearchDoc{
		ID:         uuid.New(),
		FechaVenta: fecha,
		CreatedAt:  created,
	}

	got := ventsearchmeili.MapDocForTest(doc)
	raw, err := json.Marshal(got)
	require.NoError(t, err)

	var wire map[string]any
	require.NoError(t, json.Unmarshal(raw, &wire))

	require.Contains(t, wire, "fecha_venta_ts")
	require.Contains(t, wire, "created_at_ts")
	assert.InDelta(t, float64(fecha.Unix()), wire["fecha_venta_ts"], 0, "epoch-seconds on wire")
	assert.InDelta(t, float64(created.Unix()), wire["created_at_ts"], 0, "epoch-seconds on wire")
}
