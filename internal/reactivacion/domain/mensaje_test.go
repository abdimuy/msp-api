//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var (
	mensajeFixedNow      = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	mensajeFixedEnviado  = time.Date(2026, 7, 20, 12, 5, 0, 0, time.UTC)
	mensajeFixedEncolado = time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
)

func validMensajeParams() domain.CrearMensajeParams {
	return domain.CrearMensajeParams{
		ClienteID: 24037,
		Segmento:  domain.SegmentoRecienLiquidado,
		Telefono:  "238 123 4567",
		Cuerpo:    "Hola Minerva, le saluda Mueblería MSP. ¡Felicidades por completar su pago!",
		Now:       mensajeFixedNow,
	}
}

func TestCrearMensaje_Success(t *testing.T) {
	t.Parallel()
	m, err := domain.CrearMensaje(validMensajeParams())
	require.NoError(t, err)

	assert.NotEqual(t, uuid.Nil, m.ID())
	assert.Equal(t, 24037, m.ClienteID())
	assert.Equal(t, domain.SegmentoRecienLiquidado, m.Segmento())
	assert.Equal(t, "238 123 4567", m.Telefono())
	assert.Contains(t, m.Cuerpo(), "Minerva")
	assert.Equal(t, domain.EstadoEncolado, m.Estado())
	assert.Empty(t, m.SenderKind().String())
	assert.Equal(t, mensajeFixedNow, m.EncoladoEn())
	assert.True(t, m.EnviadoEn().IsZero())
	assert.Empty(t, m.Error())
	assert.Equal(t, mensajeFixedNow, m.CreatedAt())
	assert.Equal(t, mensajeFixedNow, m.UpdatedAt())
}

func TestCrearMensaje_Invariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(p *domain.CrearMensajeParams)
		wantErr error
	}{
		{
			name:    "cliente_id_zero",
			mutate:  func(p *domain.CrearMensajeParams) { p.ClienteID = 0 },
			wantErr: domain.ErrMensajeClienteIDInvalido,
		},
		{
			name:    "cliente_id_negative",
			mutate:  func(p *domain.CrearMensajeParams) { p.ClienteID = -1 },
			wantErr: domain.ErrMensajeClienteIDInvalido,
		},
		{
			name:    "segmento_invalido",
			mutate:  func(p *domain.CrearMensajeParams) { p.Segmento = domain.Segmento("x") },
			wantErr: domain.ErrSegmentoInvalido,
		},
		{
			name:    "telefono_vacio",
			mutate:  func(p *domain.CrearMensajeParams) { p.Telefono = "" },
			wantErr: domain.ErrMensajeTelefonoRequerido,
		},
		{
			name:    "cuerpo_vacio",
			mutate:  func(p *domain.CrearMensajeParams) { p.Cuerpo = "" },
			wantErr: domain.ErrMensajeCuerpoRequerido,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validMensajeParams()
			tc.mutate(&p)
			m, err := domain.CrearMensaje(p)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, m)
		})
	}
}

func TestMensaje_MarcarEnviado_FromEncolado(t *testing.T) {
	t.Parallel()
	m, err := domain.CrearMensaje(validMensajeParams())
	require.NoError(t, err)

	err = m.MarcarEnviado(domain.SenderSimulado, mensajeFixedEnviado)
	require.NoError(t, err)

	assert.Equal(t, domain.EstadoEnviado, m.Estado())
	assert.Equal(t, domain.SenderSimulado, m.SenderKind())
	assert.Equal(t, mensajeFixedEnviado, m.EnviadoEn())
	assert.Equal(t, mensajeFixedEnviado, m.UpdatedAt())
}

func TestMensaje_MarcarEnviado_FromNonEncolado(t *testing.T) {
	t.Parallel()
	m, err := domain.CrearMensaje(validMensajeParams())
	require.NoError(t, err)
	require.NoError(t, m.MarcarEnviado(domain.SenderSimulado, mensajeFixedEnviado))

	err = m.MarcarEnviado(domain.SenderSimulado, mensajeFixedEnviado.Add(time.Minute))
	require.ErrorIs(t, err, domain.ErrMensajeTransicionInvalida)
}

func TestMensaje_MarcarFallido(t *testing.T) {
	t.Parallel()
	m, err := domain.CrearMensaje(validMensajeParams())
	require.NoError(t, err)

	when := mensajeFixedNow.Add(time.Minute)
	m.MarcarFallido("número no válido en whatsapp", when)

	assert.Equal(t, domain.EstadoFallido, m.Estado())
	assert.Equal(t, "número no válido en whatsapp", m.Error())
	assert.Equal(t, when, m.UpdatedAt())
	// A failed send must never claim a sender kind or a sent timestamp.
	assert.Empty(t, m.SenderKind().String())
	assert.True(t, m.EnviadoEn().IsZero())
}

func TestMensaje_MarcarBloqueado(t *testing.T) {
	t.Parallel()
	m, err := domain.CrearMensaje(validMensajeParams())
	require.NoError(t, err)

	when := mensajeFixedNow.Add(2 * time.Minute)
	m.MarcarBloqueado("tope diario alcanzado", when)

	assert.Equal(t, domain.EstadoBloqueado, m.Estado())
	assert.Equal(t, "tope diario alcanzado", m.Error())
	assert.Equal(t, when, m.UpdatedAt())
}

func TestHydrateMensaje(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	created := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	m := domain.HydrateMensaje(domain.HydrateMensajeParams{
		ID:         id,
		ClienteID:  99,
		Segmento:   domain.SegmentoPorLiquidarHueco,
		Telefono:   "238 999 0000",
		Cuerpo:     "cuerpo hidratado",
		Estado:     domain.EstadoEnviado,
		SenderKind: domain.SenderReal,
		EncoladoEn: mensajeFixedEncolado,
		EnviadoEn:  mensajeFixedEnviado,
		Error:      "",
		CreatedAt:  created,
		UpdatedAt:  updated,
	})

	assert.Equal(t, id, m.ID())
	assert.Equal(t, 99, m.ClienteID())
	assert.Equal(t, domain.SegmentoPorLiquidarHueco, m.Segmento())
	assert.Equal(t, "238 999 0000", m.Telefono())
	assert.Equal(t, "cuerpo hidratado", m.Cuerpo())
	assert.Equal(t, domain.EstadoEnviado, m.Estado())
	assert.Equal(t, domain.SenderReal, m.SenderKind())
	assert.Equal(t, mensajeFixedEncolado, m.EncoladoEn())
	assert.Equal(t, mensajeFixedEnviado, m.EnviadoEn())
	assert.Empty(t, m.Error())
	assert.Equal(t, created, m.CreatedAt())
	assert.Equal(t, updated, m.UpdatedAt())
}

func TestHydrateMensaje_WithError(t *testing.T) {
	t.Parallel()
	m := domain.HydrateMensaje(domain.HydrateMensajeParams{
		ID:         uuid.New(),
		ClienteID:  1,
		Segmento:   domain.SegmentoRecienLiquidado,
		Telefono:   "238 000 1111",
		Cuerpo:     "cuerpo",
		Estado:     domain.EstadoFallido,
		SenderKind: "",
		EncoladoEn: mensajeFixedEncolado,
		Error:      "canal no configurado",
		CreatedAt:  mensajeFixedEncolado,
		UpdatedAt:  mensajeFixedEncolado,
	})
	assert.Equal(t, domain.EstadoFallido, m.Estado())
	assert.Equal(t, "canal no configurado", m.Error())
	assert.Empty(t, m.SenderKind().String())
	assert.True(t, m.EnviadoEn().IsZero())
}
