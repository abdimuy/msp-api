package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/flota/domain"
)

// intPtr is a local helper for the roster's nullable camioneta field.
func intPtr(n int) *int { return &n }

// TestSinCamioneta_EsElValorCero pins that the unassigned value is the zero
// value. Several places in the module rely on it implicitly.
func TestSinCamioneta_EsElValorCero(t *testing.T) {
	t.Parallel()

	var cero domain.Camioneta
	assert.True(t, domain.SinCamioneta().Igual(cero))
	assert.False(t, domain.SinCamioneta().Asignada())
	assert.Equal(t, 0, domain.SinCamioneta().ID())
}

// TestNuevaCamioneta_RechazaNoPositivos verifies the strict constructor.
func TestNuevaCamioneta_RechazaNoPositivos(t *testing.T) {
	t.Parallel()

	for _, id := range []int{0, -1, -11341} {
		_, err := domain.NuevaCamioneta(id)
		require.ErrorIs(t, err, domain.ErrCamionetaInvalida, "id %d", id)
	}

	c, err := domain.NuevaCamioneta(11341)
	require.NoError(t, err)
	assert.True(t, c.Asignada())
	assert.Equal(t, 11341, c.ID())
}

// TestCamionetaDesdeRoster_TodasLasAusencias covers the three shapes the
// roster genuinely produces for "no camioneta". Measured on the dev project
// on 2026-08-28: 26 documents without the field, 1 with an explicit null,
// and the console lets anybody type a zero.
func TestCamionetaDesdeRoster_TodasLasAusencias(t *testing.T) {
	t.Parallel()

	casos := map[string]*int{
		"campo ausente o null": nil,
		"cero":                 intPtr(0),
		"negativo":             intPtr(-3),
	}
	for nombre, raw := range casos {
		t.Run(nombre, func(t *testing.T) {
			t.Parallel()
			c := domain.CamionetaDesdeRoster(raw)
			assert.False(t, c.Asignada(), "debe leerse como sin camioneta")
			assert.True(t, c.Igual(domain.SinCamioneta()))
		})
	}
}

// TestCamionetaDesdeRoster_ValorReal verifies the happy path with an id
// actually observed in the roster.
func TestCamionetaDesdeRoster_ValorReal(t *testing.T) {
	t.Parallel()

	c := domain.CamionetaDesdeRoster(intPtr(3051068))
	assert.True(t, c.Asignada())
	assert.Equal(t, 3051068, c.ID())
}

// TestCamioneta_Igual verifies the equality that the whole comparison rests
// on — including that two unassigned values are equal, which is what keeps a
// person without a camioneta from being reported as changing every pass.
func TestCamioneta_Igual(t *testing.T) {
	t.Parallel()

	a := domain.CamionetaDesdeRoster(intPtr(11341))
	b := domain.CamionetaDesdeRoster(intPtr(11341))
	otra := domain.CamionetaDesdeRoster(intPtr(11342))

	assert.True(t, a.Igual(b))
	assert.False(t, a.Igual(otra))
	assert.False(t, a.Igual(domain.SinCamioneta()))
	assert.True(t, domain.SinCamioneta().Igual(domain.SinCamioneta()))
}
