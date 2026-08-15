//nolint:misspell // Spanish vocabulary (ciudad, acentos) per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestNormalizeCiudad(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ya normalizada", "TEHUACAN", "TEHUACAN"},
		{"minúsculas", "tehuacan", "TEHUACAN"},
		{"acento", "Tehuacán", "TEHUACAN"},
		{"eñe", "Cañada Morelos", "CANADA MORELOS"},
		{"acento y eñe", "San Antonio Cañada", "SAN ANTONIO CANADA"},
		// The production catalog really does store these with a trailing space.
		{"espacio final del catálogo", "COYOMEAPAN ", "COYOMEAPAN"},
		{"espacio inicial", "  ESPERANZA", "ESPERANZA"},
		{"espacios internos colapsados", "SAN  JUAN   ATENCO", "SAN JUAN ATENCO"},
		{"tabulador y salto de línea", "SAN\tGABRIEL\nCHILAC", "SAN GABRIEL CHILAC"},
		{"vacía", "", ""},
		{"sólo espacios", "   ", ""},
		{"diéresis", "Müller", "MULLER"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, domain.NormalizeCiudad(tc.in))
		})
	}
}

// What the vendor types must fold onto what the catalog stores.
func TestNormalizeCiudad_CapturaCoincideConCatalogo(t *testing.T) {
	t.Parallel()

	pairs := []struct{ capturado, catalogo string }{
		{"tehuacán", "TEHUACAN"},
		{"Coyomeapan", "COYOMEAPAN "},
		{"esperanza", "ESPERANZA "},
		{"cañada morelos", "CAÑADA MORELOS"},
		{"San José Miahuatlán", "SAN JOSE MIAHUATLAN"},
	}

	for _, p := range pairs {
		assert.Equal(
			t, domain.NormalizeCiudad(p.catalogo), domain.NormalizeCiudad(p.capturado),
			"%q debe resolver a %q", p.capturado, p.catalogo,
		)
	}
}

// Distinct cities must NOT collapse onto each other. The normalizer folds
// accents and spacing — never punctuation or suffixes, because "TLACHICHUCA"
// and "TLACHICHUCA, PUE" are two separate catalog rows and picking one for the
// other writes a cliente into a row nobody chose.
func TestNormalizeCiudad_NoColapsaCiudadesDistintas(t *testing.T) {
	t.Parallel()

	distintas := [][2]string{
		{"TLACHICHUCA", "TLACHICHUCA, PUE"},
		{"TECAMACHALCO", "TECAMACHALCO, PUE."},
		{"CIUDAD DE HIDALGO", "CIUDAD HIDALGO"},
		{"OAXACA", "OAXACA DE JUAREZ"},
	}

	for _, d := range distintas {
		assert.NotEqual(
			t, domain.NormalizeCiudad(d[0]), domain.NormalizeCiudad(d[1]),
			"%q y %q son filas distintas del catálogo y no deben colapsarse", d[0], d[1],
		)
	}
}
