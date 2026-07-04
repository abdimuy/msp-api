//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildBuscarVentasInput_HappyPath asserts every query parameter — the
// pre-existing Firebird-fallback filters plus the new Meili-only fields —
// parses into the matching app-layer field.
func TestBuildBuscarVentasInput_HappyPath(t *testing.T) {
	t.Parallel()

	vendedorUsuarioID := uuid.NewString()
	in := &ListarVentasInput{
		Cursor:            "o10",
		Limit:             20,
		Desde:             "2026-01-01T00:00:00Z",
		Hasta:             "2026-02-01T00:00:00Z",
		VendedorUsuarioID: vendedorUsuarioID,
		ClienteID:         "42",
		TipoVenta:         "CONTADO",
		Situacion:         "aprobada",
		Sincronizacion:    "aplicada",
		IncluirCanceladas: true,
		Q:                 "juan perez",
		ZonaClienteID:     "7",
		VendedorEmail:     "vendedor@example.com",
		PrecioMin:         "100.5",
		PrecioMax:         "9000",
		SortBy:            "precio_total",
		SortOrder:         "desc",
	}

	got, err := buildBuscarVentasInput(in)
	require.NoError(t, err)

	assert.Equal(t, "juan perez", got.Q)
	require.NotNil(t, got.Desde)
	assert.Equal(t, "2026-01-01T00:00:00Z", got.Desde.Format("2006-01-02T15:04:05Z"))
	require.NotNil(t, got.Hasta)
	assert.Equal(t, "2026-02-01T00:00:00Z", got.Hasta.Format("2006-01-02T15:04:05Z"))
	require.NotNil(t, got.VendedorUsuarioID)
	assert.Equal(t, vendedorUsuarioID, got.VendedorUsuarioID.String())
	require.NotNil(t, got.ClienteID)
	assert.Equal(t, 42, *got.ClienteID)
	assert.Equal(t, "CONTADO", got.TipoVenta)
	assert.Equal(t, "aprobada", got.Situacion)
	assert.Equal(t, "aplicada", got.Sincronizacion)
	assert.True(t, got.IncluirCanceladas)
	require.NotNil(t, got.ZonaClienteID)
	assert.Equal(t, 7, *got.ZonaClienteID)
	require.NotNil(t, got.VendedorEmail)
	assert.Equal(t, "vendedor@example.com", *got.VendedorEmail)
	require.NotNil(t, got.PrecioMin)
	assert.True(t, got.PrecioMin.Equal(decimal.RequireFromString("100.5")))
	require.NotNil(t, got.PrecioMax)
	assert.True(t, got.PrecioMax.Equal(decimal.RequireFromString("9000")))
	assert.Equal(t, "precio_total", got.SortBy)
	assert.Equal(t, "desc", got.SortOrder)
	assert.Equal(t, "o10", got.Cursor)
	assert.Equal(t, 20, got.Limit)
}

// TestBuildBuscarVentasInput_AllOptionalFieldsEmpty asserts every optional
// filter defaults to nil/zero when its query param is absent.
func TestBuildBuscarVentasInput_AllOptionalFieldsEmpty(t *testing.T) {
	t.Parallel()

	got, err := buildBuscarVentasInput(&ListarVentasInput{})
	require.NoError(t, err)

	assert.Nil(t, got.Desde)
	assert.Nil(t, got.Hasta)
	assert.Nil(t, got.VendedorUsuarioID)
	assert.Nil(t, got.ClienteID)
	assert.Nil(t, got.ZonaClienteID)
	assert.Nil(t, got.VendedorEmail)
	assert.Nil(t, got.PrecioMin)
	assert.Nil(t, got.PrecioMax)
}

// TestBuildBuscarVentasInput_InvalidParams asserts each malformed query
// parameter yields a validation apperror rather than panicking or silently
// defaulting.
func TestBuildBuscarVentasInput_InvalidParams(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   ListarVentasInput
	}{
		{"bad_desde", ListarVentasInput{Desde: "not-a-date"}},
		{"bad_hasta", ListarVentasInput{Hasta: "not-a-date"}},
		{"bad_vendedor_usuario_id", ListarVentasInput{VendedorUsuarioID: "not-a-uuid"}},
		{"bad_cliente_id_non_numeric", ListarVentasInput{ClienteID: "abc"}},
		{"bad_cliente_id_zero", ListarVentasInput{ClienteID: "0"}},
		{"bad_cliente_id_negative", ListarVentasInput{ClienteID: "-5"}},
		{"bad_zona_cliente_id_non_numeric", ListarVentasInput{ZonaClienteID: "abc"}},
		{"bad_zona_cliente_id_zero", ListarVentasInput{ZonaClienteID: "0"}},
		{"bad_precio_min", ListarVentasInput{PrecioMin: "not-a-number"}},
		{"bad_precio_max", ListarVentasInput{PrecioMax: "not-a-number"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := tc.in
			_, err := buildBuscarVentasInput(&in)
			require.Error(t, err, "expected a validation apperror for %s", tc.name)
		})
	}
}

// TestBuildBuscarVentasInput_PrecioRoundsToTwoDecimals guards the money
// parsing path used by precio_min/precio_max — same HALF_UP rounding as the
// rest of the monetary fields (montoDecimals).
func TestBuildBuscarVentasInput_PrecioRoundsToTwoDecimals(t *testing.T) {
	t.Parallel()
	got, err := buildBuscarVentasInput(&ListarVentasInput{PrecioMin: "100.125"})
	require.NoError(t, err)
	require.NotNil(t, got.PrecioMin)
	assert.True(t, got.PrecioMin.Equal(decimal.RequireFromString("100.13")))
}
