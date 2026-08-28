//nolint:misspell // ventas vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestNormalizarEmail pins the canonical form both sides of the vendedor
// comparison must reach. The capitalized case is the defect that made a
// vendedor disappear from a venta without a log.
func TestNormalizarEmail(t *testing.T) {
	t.Parallel()

	casos := map[string]struct{ entrada, quiere string }{
		"ya canonico":       {"ana@muebleriamsp.mx", "ana@muebleriamsp.mx"},
		"mayusculas":        {"Ana.Laura@MuebleriaMSP.MX", "ana.laura@muebleriamsp.mx"},
		"espacios":          {"  ana@muebleriamsp.mx\t", "ana@muebleriamsp.mx"},
		"ambos":             {" ANA@MSP.COM ", "ana@msp.com"},
		"vacio":             {"", ""},
		"solo espacios":     {"   ", ""},
		"sin arroba pasa":   {"NoEsUnCorreo", "noesuncorreo"},
		"idempotente dos v": {"ana@muebleriamsp.mx", "ana@muebleriamsp.mx"},
	}
	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			got := domain.NormalizarEmail(c.entrada)
			assert.Equal(t, c.quiere, got)
			assert.Equal(t, got, domain.NormalizarEmail(got), "normalization must be idempotent")
		})
	}
}
