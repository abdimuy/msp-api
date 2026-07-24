//nolint:misspell // Spanish vocabulary (Articulo, Precios, Categoria) per project convention.
package microsip

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
)

func TestParsePrecios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want map[string]string // name → expected decimal string ("" absent handled per-case)
	}{
		{
			name: "two lists in canonical order",
			raw:  "MUEBLERIAS:8500.00,CONTADO:7000.00",
			want: map[string]string{"MUEBLERIAS": "8500", "CONTADO": "7000"},
		},
		{
			name: "reversed order — parsed by name, not position",
			raw:  "CONTADO:7000.00,MUEBLERIAS:8500.00",
			want: map[string]string{"MUEBLERIAS": "8500", "CONTADO": "7000"},
		},
		{
			name: "single list present",
			raw:  "MUEBLERIAS:8500.00",
			want: map[string]string{"MUEBLERIAS": "8500"},
		},
		{
			name: "empty string yields empty map",
			raw:  "",
			want: map[string]string{},
		},
		{
			name: "malformed entry without colon is skipped",
			raw:  "MUEBLERIAS:8500.00,GARBAGE,CONTADO:7000.00",
			want: map[string]string{"MUEBLERIAS": "8500", "CONTADO": "7000"},
		},
		{
			name: "non-numeric price is skipped",
			raw:  "MUEBLERIAS:abc,CONTADO:7000.00",
			want: map[string]string{"CONTADO": "7000"},
		},
		{
			name: "empty name is skipped",
			raw:  ":8500.00,CONTADO:7000.00",
			want: map[string]string{"CONTADO": "7000"},
		},
		{
			name: "surrounding whitespace tolerated",
			raw:  " MUEBLERIAS : 8500.00 ",
			want: map[string]string{"MUEBLERIAS": "8500"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := parsePrecios(tc.raw)
			require.Len(t, got, len(tc.want))
			for name, wantStr := range tc.want {
				v, ok := got[name]
				require.True(t, ok, "expected list %q present", name)
				assert.True(t, v.Equal(decimal.RequireFromString(wantStr)),
					"list %q: got %s want %s", name, v, wantStr)
			}
		})
	}
}

func TestArticuloCatalogoFromDomain(t *testing.T) {
	t.Parallel()

	got := ArticuloCatalogoFromDomain(domain.ArticuloAlmacen{
		ArticuloID:      1234,
		Articulo:        "Refrigerador Hisense de 11 pies",
		Existencias:     7,
		LineaArticuloID: 42,
		LineaArticulo:   "Línea Blanca",
		Precios:         "MUEBLERIAS:8500.00,CONTADO:7000.00",
	})

	assert.Equal(t, 1234, got.ArticuloID)
	assert.Equal(t, 42, got.LineaArticuloID)
	assert.Equal(t, "Refrigerador Hisense de 11 pies", got.Nombre)
	assert.Equal(t, "Línea Blanca", got.Categoria)
	assert.Equal(t, int64(7), got.Existencias)
	require.Contains(t, got.Precios, "MUEBLERIAS")
	assert.True(t, got.Precios["MUEBLERIAS"].Equal(decimal.RequireFromString("8500")))
}
