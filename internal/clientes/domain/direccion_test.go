//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

func TestHydrateDireccion_GetterRoundTrip(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "Av. Juárez 45",
		Colonia:   "Centro Histórico",
		Poblacion: "Guadalajara",
		Estado:    "Jalisco",
	})

	assert.Equal(t, "Av. Juárez 45", d.Calle())
	assert.Equal(t, "Centro Histórico", d.Colonia())
	assert.Equal(t, "Guadalajara", d.Poblacion())
	assert.Equal(t, "Jalisco", d.Estado())
}

func TestHydrateDireccion_ZeroValues(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{})
	assert.Empty(t, d.Calle())
	assert.Empty(t, d.Colonia())
	assert.Empty(t, d.Poblacion())
	assert.Empty(t, d.Estado())
}

func TestDireccion_Corta_AllPresent(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "Calle Hidalgo 12",
		Colonia:   "Jardines",
		Poblacion: "Zapopan",
		Estado:    "Jalisco",
	})
	assert.Equal(t, "Calle Hidalgo 12, Jardines, Zapopan", d.Corta())
}

func TestDireccion_Corta_MissingColonia(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "Calle Morelos 5",
		Colonia:   "",
		Poblacion: "Tonalá",
	})
	assert.Equal(t, "Calle Morelos 5, Tonalá", d.Corta())
}

func TestDireccion_Corta_MissingCalle(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "",
		Colonia:   "El Mirador",
		Poblacion: "Tlaquepaque",
	})
	assert.Equal(t, "El Mirador, Tlaquepaque", d.Corta())
}

func TestDireccion_Corta_AllEmpty(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{})
	assert.Empty(t, d.Corta())
}

func TestDireccion_Corta_WhitespaceOnlySkipped(t *testing.T) {
	t.Parallel()
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "   ",
		Colonia:   "Américas",
		Poblacion: "\t",
	})
	// Only "Américas" is non-whitespace.
	assert.Equal(t, "Américas", d.Corta())
}

func TestDireccion_Corta_TrimsNonEmptyComponents(t *testing.T) {
	t.Parallel()
	// Firebird CHAR columns may pad with trailing spaces; Corta must trim each
	// component so the joined label has no stray internal whitespace.
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "  Calle Hidalgo 12  ",
		Colonia:   "\tJardines\t",
		Poblacion: " Zapopan ",
	})
	assert.Equal(t, "Calle Hidalgo 12, Jardines, Zapopan", d.Corta())
}

func TestDireccion_Corta_EstadoNotIncluded(t *testing.T) {
	t.Parallel()
	// Corta() must NOT include Estado — it's only exposed via Estado().
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "Av. López Mateos",
		Colonia:   "Chapalita",
		Poblacion: "Guadalajara",
		Estado:    "Jalisco",
	})
	corta := d.Corta()
	assert.NotContains(t, corta, "Jalisco")
	assert.Equal(t, "Av. López Mateos, Chapalita, Guadalajara", corta)
}

func TestDireccion_GettersRoundTripAllFields(t *testing.T) {
	t.Parallel()
	// Direccion is an immutable value type (no setters); verify every getter
	// returns exactly what was hydrated.
	d := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "Av. Hidalgo 123",
		Colonia:   "Centro",
		Poblacion: "Guadalajara",
		Estado:    "Jalisco",
	})
	assert.Equal(t, "Av. Hidalgo 123", d.Calle())
	assert.Equal(t, "Centro", d.Colonia())
	assert.Equal(t, "Guadalajara", d.Poblacion())
	assert.Equal(t, "Jalisco", d.Estado())
}
