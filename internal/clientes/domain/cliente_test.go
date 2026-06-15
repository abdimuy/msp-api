package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
)

func TestHydrateCliente_AllGettersRoundTrip(t *testing.T) {
	t.Parallel()
	limite := decimal.NewFromFloat(15000.50)
	dir := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Calle:     "Av. Revolución 100",
		Colonia:   "Centro",
		Poblacion: "Guadalajara",
		Estado:    "Jalisco",
	})
	p := domain.HydrateClienteParams{
		ClienteID:      42,
		Nombre:         "Juan Pérez García",
		LimiteCredito:  limite,
		Notas:          "cliente frecuente",
		Estatus:        "A",
		ZonaClienteID:  7,
		ZonaNombre:     "Zona Norte",
		CobradorID:     3,
		CobradorNombre: "Roberto López",
		Direccion:      dir,
		Telefono:       "3312345678",
	}

	c := domain.HydrateCliente(p)

	assert.Equal(t, 42, c.ClienteID())
	assert.Equal(t, "Juan Pérez García", c.Nombre())
	assert.True(t, limite.Equal(c.LimiteCredito()), "LimiteCredito round-trip")
	assert.Equal(t, "cliente frecuente", c.Notas())
	assert.Equal(t, "A", c.Estatus())
	assert.Equal(t, 7, c.ZonaClienteID())
	assert.Equal(t, "Zona Norte", c.ZonaNombre())
	assert.Equal(t, 3, c.CobradorID())
	assert.Equal(t, "Roberto López", c.CobradorNombre())
	assert.Equal(t, "Av. Revolución 100", c.Direccion().Calle())
	assert.Equal(t, "Centro", c.Direccion().Colonia())
	assert.Equal(t, "Guadalajara", c.Direccion().Poblacion())
	assert.Equal(t, "Jalisco", c.Direccion().Estado())
	assert.Equal(t, "3312345678", c.Telefono())
}

func TestHydrateCliente_ZeroValues(t *testing.T) {
	t.Parallel()
	c := domain.HydrateCliente(domain.HydrateClienteParams{})

	assert.Zero(t, c.ClienteID())
	assert.Empty(t, c.Nombre())
	assert.True(t, decimal.Zero.Equal(c.LimiteCredito()))
	assert.Empty(t, c.Notas())
	assert.Empty(t, c.Estatus())
	assert.Zero(t, c.ZonaClienteID())
	assert.Empty(t, c.ZonaNombre())
	assert.Zero(t, c.CobradorID())
	assert.Empty(t, c.CobradorNombre())
	assert.Empty(t, c.Direccion().Calle())
	assert.Empty(t, c.Direccion().Colonia())
	assert.Empty(t, c.Direccion().Poblacion())
	assert.Empty(t, c.Direccion().Estado())
	assert.Empty(t, c.Telefono())
}

func TestHydrateCliente_DecimalPrecision(t *testing.T) {
	t.Parallel()
	// Verify that high-precision decimal values are preserved exactly.
	limite := decimal.NewFromFloat(99999.99)
	c := domain.HydrateCliente(domain.HydrateClienteParams{
		LimiteCredito: limite,
	})
	assert.True(t, limite.Equal(c.LimiteCredito()), "decimal precision preserved")
}

func TestHydrateCliente_NegativeLimiteCredito(t *testing.T) {
	t.Parallel()
	// Hydrate performs zero validation — negative values must pass through.
	limite := decimal.NewFromFloat(-100.00)
	c := domain.HydrateCliente(domain.HydrateClienteParams{
		LimiteCredito: limite,
	})
	assert.True(t, limite.Equal(c.LimiteCredito()))
}

func TestHydrateCliente_ReturnsPointer(t *testing.T) {
	t.Parallel()
	c := domain.HydrateCliente(domain.HydrateClienteParams{ClienteID: 1})
	assert.NotNil(t, c)
}

func TestHydrateCliente_UnicodeStrings(t *testing.T) {
	t.Parallel()
	// Spanish accents, em-dash, and other UTF-8 content must round-trip byte-equal.
	dir := domain.HydrateDireccion(domain.HydrateDireccionParams{
		Poblacion: "México",
		Estado:    "Michoacán",
	})
	c := domain.HydrateCliente(domain.HydrateClienteParams{
		Nombre:    "José María Ñoño — el \"mejor\" cliente",
		Notas:     "observación: paga puntual ✓",
		Direccion: dir,
	})
	assert.Equal(t, "José María Ñoño — el \"mejor\" cliente", c.Nombre())
	assert.Equal(t, "observación: paga puntual ✓", c.Notas())
	assert.Equal(t, "México", c.Direccion().Poblacion())
	assert.Equal(t, "Michoacán", c.Direccion().Estado())
}
