//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var (
	conversacionFixedNow  = time.Date(2026, 7, 21, 9, 0, 0, 0, time.UTC)
	conversacionFixedNow2 = time.Date(2026, 7, 21, 9, 5, 0, 0, time.UTC)
)

// mustCrearConversacion is a test helper that fails the test immediately if
// CrearConversacion returns an error, so the many transition tests below
// (which only care about a valid starting conversation) don't each repeat
// the same error check.
func mustCrearConversacion(t *testing.T, clienteID int, now time.Time) *domain.Conversacion {
	t.Helper()
	c, err := domain.CrearConversacion(clienteID, now)
	require.NoError(t, err)
	return c
}

func TestCrearConversacion_Success(t *testing.T) {
	t.Parallel()
	c, err := domain.CrearConversacion(24037, conversacionFixedNow)
	require.NoError(t, err)

	assert.NotEmpty(t, c.ID())
	assert.Equal(t, 24037, c.ClienteID())
	assert.Equal(t, domain.EstadoContactado, c.Estado())
	assert.Empty(t, c.AsignadoA())
	assert.Empty(t, c.ResumenMemoria())
	assert.Empty(t, c.ContextoNota())
	assert.NotNil(t, c.Banderas())
	assert.Empty(t, c.Banderas())
	assert.Empty(t, c.NotaHash())
	assert.Equal(t, conversacionFixedNow, c.CreatedAt())
	assert.Equal(t, conversacionFixedNow, c.UpdatedAt())
}

func TestCrearConversacion_UniqueIDs(t *testing.T) {
	t.Parallel()
	a := mustCrearConversacion(t, 1, conversacionFixedNow)
	b := mustCrearConversacion(t, 1, conversacionFixedNow)
	assert.NotEqual(t, a.ID(), b.ID())
}

func TestCrearConversacion_Invariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		clienteID int
	}{
		{"cliente_id_zero", 0},
		{"cliente_id_negative", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := domain.CrearConversacion(tc.clienteID, conversacionFixedNow)
			require.ErrorIs(t, err, domain.ErrConversacionClienteIDInvalido)
			assert.Nil(t, c)
		})
	}
}

func TestConversacion_MarcarRespondio(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	err := c.MarcarRespondio(conversacionFixedNow2)
	require.NoError(t, err)
	assert.Equal(t, domain.EstadoRespondio, c.Estado())
	assert.Equal(t, conversacionFixedNow2, c.UpdatedAt())
}

func TestConversacion_MarcarRespondio_InvalidFrom(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(c *domain.Conversacion)
	}{
		{"desde_respondio", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
		}},
		{"desde_conversando", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
			require.NoError(t, c.MarcarConversando(conversacionFixedNow2))
		}},
		{"desde_escalado", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarEscalada("op1", conversacionFixedNow2))
		}},
		{"desde_enganche", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarEnganche(conversacionFixedNow2))
		}},
		{"desde_descartado", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarDescartado(conversacionFixedNow2))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			tc.setup(c)
			err := c.MarcarRespondio(conversacionFixedNow2.Add(time.Minute))
			require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
		})
	}
}

func TestConversacion_MarcarConversando_FromRespondioOrConversando(t *testing.T) {
	t.Parallel()

	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
	require.NoError(t, c.MarcarConversando(conversacionFixedNow2.Add(time.Minute)))
	assert.Equal(t, domain.EstadoConversando, c.Estado())

	// idempotent-ish: conversando -> conversando is allowed.
	require.NoError(t, c.MarcarConversando(conversacionFixedNow2.Add(2*time.Minute)))
	assert.Equal(t, domain.EstadoConversando, c.Estado())
}

func TestConversacion_MarcarConversando_InvalidFromContactado(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	err := c.MarcarConversando(conversacionFixedNow2)
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestConversacion_MarcarEscalada_AllowedStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(c *domain.Conversacion)
	}{
		{"desde_contactado", func(c *domain.Conversacion) {}},
		{"desde_respondio", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
		}},
		{"desde_conversando", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
			require.NoError(t, c.MarcarConversando(conversacionFixedNow2))
		}},
		{"desde_escalado_reescala", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarEscalada("op1", conversacionFixedNow2))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			tc.setup(c)
			when := conversacionFixedNow2.Add(time.Hour)
			err := c.MarcarEscalada("op2", when)
			require.NoError(t, err)
			assert.Equal(t, domain.EstadoEscalado, c.Estado())
			assert.Equal(t, "op2", c.AsignadoA())
			assert.Equal(t, when, c.UpdatedAt())
		})
	}
}

func TestConversacion_MarcarEscalada_InvalidFromTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(c *domain.Conversacion)
	}{
		{"desde_enganche", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarEnganche(conversacionFixedNow2))
		}},
		{"desde_descartado", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarDescartado(conversacionFixedNow2))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			tc.setup(c)
			err := c.MarcarEscalada("op1", conversacionFixedNow2.Add(time.Minute))
			require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
		})
	}
}

// TestConversacion_MarcarEscalada_InvalidFromInteresado covers a business-rule
// exclusion, NOT a terminal state — interesado is a perfectly normal
// non-terminal estado, it simply isn't in MarcarEscalada's allowed-from set.
func TestConversacion_MarcarEscalada_InvalidFromInteresado(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
	require.NoError(t, c.MarcarConversando(conversacionFixedNow2))
	require.NoError(t, c.MarcarInteresado(conversacionFixedNow2))

	err := c.MarcarEscalada("op1", conversacionFixedNow2.Add(time.Minute))
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestConversacion_MarcarEscalada_InvalidFromUnknownEstado(t *testing.T) {
	t.Parallel()
	// A defensive default branch: an estado value outside the canonical set
	// (which should never happen via Crear/transition methods, but could
	// arrive via a corrupted Hydrate) must still be rejected.
	c := domain.HydrateConversacion(domain.HydrateConversacionParams{
		ID:        "conv-x",
		ClienteID: 1,
		Estado:    domain.EstadoConversacion("desconocido"),
		Banderas:  []string{},
		CreatedAt: conversacionFixedNow,
		UpdatedAt: conversacionFixedNow,
	})
	err := c.MarcarEscalada("op1", conversacionFixedNow2)
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestConversacion_MarcarInteresado_AllowedStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(c *domain.Conversacion)
	}{
		{"desde_conversando", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
			require.NoError(t, c.MarcarConversando(conversacionFixedNow2))
		}},
		{"desde_escalado", func(c *domain.Conversacion) {
			require.NoError(t, c.MarcarEscalada("op1", conversacionFixedNow2))
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			tc.setup(c)
			when := conversacionFixedNow2.Add(time.Hour)
			require.NoError(t, c.MarcarInteresado(when))
			assert.Equal(t, domain.EstadoInteresado, c.Estado())
			assert.Equal(t, when, c.UpdatedAt())
		})
	}
}

func TestConversacion_MarcarInteresado_InvalidFromContactado(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	err := c.MarcarInteresado(conversacionFixedNow2)
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestConversacion_MarcarEnganche_FromAnyNonTerminal(t *testing.T) {
	t.Parallel()
	estados := []func() *domain.Conversacion{
		func() *domain.Conversacion { return mustCrearConversacion(t, 1, conversacionFixedNow) },
		func() *domain.Conversacion {
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
			return c
		},
		func() *domain.Conversacion {
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			require.NoError(t, c.MarcarEscalada("op1", conversacionFixedNow2))
			return c
		},
		func() *domain.Conversacion {
			c := mustCrearConversacion(t, 1, conversacionFixedNow)
			require.NoError(t, c.MarcarRespondio(conversacionFixedNow2))
			require.NoError(t, c.MarcarConversando(conversacionFixedNow2))
			require.NoError(t, c.MarcarInteresado(conversacionFixedNow2))
			return c
		},
	}
	for _, build := range estados {
		c := build()
		when := conversacionFixedNow2.Add(time.Hour)
		require.NoError(t, c.MarcarEnganche(when))
		assert.Equal(t, domain.EstadoEnganche, c.Estado())
		assert.Equal(t, when, c.UpdatedAt())
	}
}

func TestConversacion_MarcarEnganche_InvalidFromTerminal(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	require.NoError(t, c.MarcarDescartado(conversacionFixedNow2))
	err := c.MarcarEnganche(conversacionFixedNow2.Add(time.Minute))
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestConversacion_MarcarDescartado_FromAnyNonTerminal(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	when := conversacionFixedNow2.Add(time.Hour)
	require.NoError(t, c.MarcarDescartado(when))
	assert.Equal(t, domain.EstadoDescartado, c.Estado())
	assert.Equal(t, when, c.UpdatedAt())
}

func TestConversacion_MarcarDescartado_InvalidFromTerminal(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	require.NoError(t, c.MarcarEnganche(conversacionFixedNow2))
	err := c.MarcarDescartado(conversacionFixedNow2.Add(time.Minute))
	require.ErrorIs(t, err, domain.ErrConversacionTransicionInvalida)
}

func TestConversacion_SetResumenMemoria(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	c.SetResumenMemoria("resumen breve", conversacionFixedNow2)
	assert.Equal(t, "resumen breve", c.ResumenMemoria())
	assert.Equal(t, conversacionFixedNow2, c.UpdatedAt())
}

func TestConversacion_SetContextoNota(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	c.SetContextoNota("cliente puntual", []string{"vip"}, "hash123", conversacionFixedNow2)
	assert.Equal(t, "cliente puntual", c.ContextoNota())
	assert.Equal(t, []string{"vip"}, c.Banderas())
	assert.Equal(t, "hash123", c.NotaHash())
	assert.Equal(t, conversacionFixedNow2, c.UpdatedAt())
}

func TestConversacion_Banderas_DefensiveCopy(t *testing.T) {
	t.Parallel()
	c := mustCrearConversacion(t, 1, conversacionFixedNow)
	c.SetContextoNota("", []string{"vip"}, "", conversacionFixedNow2)

	got := c.Banderas()
	got[0] = "mutated"

	assert.Equal(t, []string{"vip"}, c.Banderas())
}

func TestHydrateConversacion(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateConversacion(domain.HydrateConversacionParams{
		ID:             "conv-1",
		ClienteID:      24037,
		Estado:         domain.EstadoEscalado,
		AsignadoA:      "op1",
		ResumenMemoria: "resumen",
		ContextoNota:   "contexto",
		Banderas:       []string{"vip", "moroso"},
		NotaHash:       "hash",
		CreatedAt:      created,
		UpdatedAt:      updated,
	})

	assert.Equal(t, "conv-1", c.ID())
	assert.Equal(t, 24037, c.ClienteID())
	assert.Equal(t, domain.EstadoEscalado, c.Estado())
	assert.Equal(t, "op1", c.AsignadoA())
	assert.Equal(t, "resumen", c.ResumenMemoria())
	assert.Equal(t, "contexto", c.ContextoNota())
	assert.Equal(t, []string{"vip", "moroso"}, c.Banderas())
	assert.Equal(t, "hash", c.NotaHash())
	assert.Equal(t, created, c.CreatedAt())
	assert.Equal(t, updated, c.UpdatedAt())
}
