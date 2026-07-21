//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var decisionFixedNow = time.Date(2026, 7, 21, 11, 0, 0, 0, time.UTC)

func validDecisionParams() domain.CrearDecisionParams {
	return domain.CrearDecisionParams{
		ClienteID:         24037,
		TurnoRef:          "turno-1",
		Intencion:         "quiere comprar de contado",
		Confianza:         85,
		Senales:           []string{domain.SenalCompra.String()},
		Accion:            domain.AccionResponder,
		Borrador:          "claro, tenemos la sala en rojo disponible",
		Evidencia:         []string{"mensaje: quiero la sala roja"},
		RazonEscalamiento: "",
		Resultado:         domain.ResultadoPropuesto,
		Now:               decisionFixedNow,
	}
}

func TestCrearDecision_Success(t *testing.T) {
	t.Parallel()
	d, err := domain.CrearDecision(validDecisionParams())
	require.NoError(t, err)

	assert.NotEmpty(t, d.ID())
	assert.Equal(t, 24037, d.ClienteID())
	assert.Equal(t, "turno-1", d.TurnoRef())
	assert.Equal(t, "quiere comprar de contado", d.Intencion())
	assert.Equal(t, 85, d.Confianza())
	assert.Equal(t, []string{domain.SenalCompra.String()}, d.Senales())
	assert.Equal(t, domain.AccionResponder, d.AccionPropuesta())
	assert.Equal(t, "claro, tenemos la sala en rojo disponible", d.Borrador())
	assert.Equal(t, []string{"mensaje: quiero la sala roja"}, d.Evidencia())
	assert.Empty(t, d.RazonEscalamiento())
	assert.Equal(t, domain.ResultadoPropuesto, d.Resultado())
	assert.Equal(t, decisionFixedNow, d.CreatedAt())
}

func TestCrearDecision_DefaultsSlicesToNonNilEmpty(t *testing.T) {
	t.Parallel()
	p := validDecisionParams()
	p.Senales = nil
	p.Evidencia = nil

	d, err := domain.CrearDecision(p)
	require.NoError(t, err)

	assert.NotNil(t, d.Senales())
	assert.Empty(t, d.Senales())
	assert.NotNil(t, d.Evidencia())
	assert.Empty(t, d.Evidencia())
}

func TestCrearDecision_Invariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(p *domain.CrearDecisionParams)
		wantErr error
	}{
		{
			name:    "accion_invalida",
			mutate:  func(p *domain.CrearDecisionParams) { p.Accion = domain.Accion("x") },
			wantErr: domain.ErrAccionInvalido,
		},
		{
			name:    "resultado_invalido",
			mutate:  func(p *domain.CrearDecisionParams) { p.Resultado = domain.ResultadoDecision("x") },
			wantErr: domain.ErrResultadoDecisionInvalido,
		},
		{
			name:    "confianza_negativa",
			mutate:  func(p *domain.CrearDecisionParams) { p.Confianza = -1 },
			wantErr: domain.ErrDecisionConfianzaInvalida,
		},
		{
			name:    "confianza_mayor_100",
			mutate:  func(p *domain.CrearDecisionParams) { p.Confianza = 101 },
			wantErr: domain.ErrDecisionConfianzaInvalida,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validDecisionParams()
			tc.mutate(&p)
			d, err := domain.CrearDecision(p)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, d)
		})
	}
}

func TestCrearDecision_ConfianzaBoundaries(t *testing.T) {
	t.Parallel()
	for _, c := range []int{0, 100} {
		p := validDecisionParams()
		p.Confianza = c
		d, err := domain.CrearDecision(p)
		require.NoError(t, err)
		assert.Equal(t, c, d.Confianza())
	}
}

func TestDecision_SetResultado(t *testing.T) {
	t.Parallel()
	d, err := domain.CrearDecision(validDecisionParams())
	require.NoError(t, err)

	d.SetResultado(domain.ResultadoAprobado)
	assert.Equal(t, domain.ResultadoAprobado, d.Resultado())
}

func TestDecision_Senales_DefensiveCopy(t *testing.T) {
	t.Parallel()
	d, err := domain.CrearDecision(validDecisionParams())
	require.NoError(t, err)

	got := d.Senales()
	got[0] = "mutated"
	assert.Equal(t, []string{domain.SenalCompra.String()}, d.Senales())
}

func TestDecision_Evidencia_DefensiveCopy(t *testing.T) {
	t.Parallel()
	d, err := domain.CrearDecision(validDecisionParams())
	require.NoError(t, err)

	got := d.Evidencia()
	got[0] = "mutated"
	assert.Equal(t, []string{"mensaje: quiero la sala roja"}, d.Evidencia())
}

func TestHydrateDecision(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	d := domain.HydrateDecision(domain.HydrateDecisionParams{
		ID:                "dec-1",
		ClienteID:         99,
		TurnoRef:          "turno-9",
		Intencion:         "quiere escalar",
		Confianza:         40,
		Senales:           []string{domain.SenalPideHumano.String()},
		AccionPropuesta:   domain.AccionEscalar,
		Borrador:          "",
		Evidencia:         []string{"quiero hablar con alguien"},
		RazonEscalamiento: "el cliente pide un humano",
		Resultado:         domain.ResultadoEscalado,
		CreatedAt:         created,
	})

	assert.Equal(t, "dec-1", d.ID())
	assert.Equal(t, 99, d.ClienteID())
	assert.Equal(t, "turno-9", d.TurnoRef())
	assert.Equal(t, "quiere escalar", d.Intencion())
	assert.Equal(t, 40, d.Confianza())
	assert.Equal(t, []string{domain.SenalPideHumano.String()}, d.Senales())
	assert.Equal(t, domain.AccionEscalar, d.AccionPropuesta())
	assert.Empty(t, d.Borrador())
	assert.Equal(t, []string{"quiero hablar con alguien"}, d.Evidencia())
	assert.Equal(t, "el cliente pide un humano", d.RazonEscalamiento())
	assert.Equal(t, domain.ResultadoEscalado, d.Resultado())
	assert.Equal(t, created, d.CreatedAt())
}
