//nolint:misspell // ventas vocabulary is Spanish (vendedor, nombre) per project convention.
package domain_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestSanitizarNombreVendedor pins the transformation itself.
func TestSanitizarNombreVendedor(t *testing.T) {
	t.Parallel()

	casos := []struct {
		desc   string
		entra  string
		quiere string
	}{
		{"nombre normal pasa igual", "BETO SUAREZ", "BETO SUAREZ"},
		{"recorta espacios", "  BETO  ", "BETO"},
		{"vacio sigue vacio", "", ""},
		{"solo espacios queda vacio", "   \t\n ", ""},
		{"elimina el NUL", "BETO\x00SUAREZ", "BETOSUAREZ"},
		{"elimina control chars", "BETO\x07\x1bSUAREZ", "BETOSUAREZ"},
		{"elimina DEL", "BETO\x7fSUAREZ", "BETOSUAREZ"},
		{"tab, LF y CR se vuelven espacio", "BETO\tS\nU\rAREZ", "BETO S U AREZ"},
		{"UTF-8 invalido se descarta", "BETO\xffSUAREZ", "BETOSUAREZ"},
		{"acentos y ñ sobreviven", "IÑIGO PÉREZ", "IÑIGO PÉREZ"},
		{"trunca a 200 runas", strings.Repeat("A", 300), strings.Repeat("A", 200)},
		{
			"trunca contando RUNAS, no bytes",
			strings.Repeat("é", 300),
			strings.Repeat("é", 200),
		},
	}
	for _, c := range casos {
		t.Run(c.desc, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.quiere, domain.SanitizarNombreVendedor(c.entra))
		})
	}
}

// TestSanitizarNombreVendedor_SiempreAceptadoPorElSnapshot is the property
// that actually matters, stated against the constructor rather than against a
// restatement of its rules: whatever SanitizarNombreVendedor returns non-empty
// must be a name NewVendedorSnapshot accepts.
//
// If the domain's bound or its safe-character rule ever changes, this fails
// instead of letting a roster document become a rejected venta again.
func TestSanitizarNombreVendedor_SiempreAceptadoPorElSnapshot(t *testing.T) {
	t.Parallel()

	entradas := []string{
		"BETO SUAREZ",
		strings.Repeat("A", 300),
		strings.Repeat("é", 500),
		"BETO\x00SUAREZ",
		"BETO\x07SUAREZ",
		"BETO\tSUAREZ",
		"BETO\xffSUAREZ",
		"IÑIGO PÉREZ",
		"  márgenes  ",
	}
	for _, e := range entradas {
		nombre := domain.SanitizarNombreVendedor(e)
		require.NotEmpty(t, nombre, "input %q must survive with something usable", e)
		_, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
			UsuarioID: uuid.New(),
			Email:     "beto@muebleriamsp.mx",
			Nombre:    nombre,
		})
		require.NoError(t, err, "sanitized %q must be constructible", nombre)
	}
}
