//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var turnoFixedNow = time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)

func validTurnoParams() domain.CrearTurnoParams {
	return domain.CrearTurnoParams{
		ClienteID: 24037,
		Direccion: domain.DireccionEntrante,
		Autor:     domain.AutorCliente,
		Cuerpo:    "hola, ¿tienen la sala en rojo?",
		Now:       turnoFixedNow,
	}
}

func TestCrearTurno_Success(t *testing.T) {
	t.Parallel()
	tn, err := domain.CrearTurno(validTurnoParams())
	require.NoError(t, err)

	assert.NotEmpty(t, tn.ID())
	assert.Equal(t, 24037, tn.ClienteID())
	assert.Equal(t, domain.DireccionEntrante, tn.Direccion())
	assert.Equal(t, domain.AutorCliente, tn.Autor())
	assert.Equal(t, "hola, ¿tienen la sala en rojo?", tn.Cuerpo())
	assert.Empty(t, tn.MensajeRef())
	assert.Equal(t, turnoFixedNow, tn.CreatedAt())
}

func TestCrearTurno_Invariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(p *domain.CrearTurnoParams)
		wantErr error
	}{
		{
			name:    "cliente_id_zero",
			mutate:  func(p *domain.CrearTurnoParams) { p.ClienteID = 0 },
			wantErr: domain.ErrTurnoClienteIDInvalido,
		},
		{
			name:    "cliente_id_negative",
			mutate:  func(p *domain.CrearTurnoParams) { p.ClienteID = -1 },
			wantErr: domain.ErrTurnoClienteIDInvalido,
		},
		{
			name:    "direccion_invalida",
			mutate:  func(p *domain.CrearTurnoParams) { p.Direccion = domain.DireccionTurno("x") },
			wantErr: domain.ErrDireccionTurnoInvalido,
		},
		{
			name:    "autor_invalido",
			mutate:  func(p *domain.CrearTurnoParams) { p.Autor = domain.Autor("x") },
			wantErr: domain.ErrAutorInvalido,
		},
		{
			name:    "cuerpo_vacio",
			mutate:  func(p *domain.CrearTurnoParams) { p.Cuerpo = "" },
			wantErr: domain.ErrTurnoCuerpoRequerido,
		},
		{
			name:    "cuerpo_solo_espacios",
			mutate:  func(p *domain.CrearTurnoParams) { p.Cuerpo = "   \t\n  " },
			wantErr: domain.ErrTurnoCuerpoRequerido,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validTurnoParams()
			tc.mutate(&p)
			tn, err := domain.CrearTurno(p)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, tn)
		})
	}
}

func TestTurno_SetMensajeRef(t *testing.T) {
	t.Parallel()
	tn, err := domain.CrearTurno(validTurnoParams())
	require.NoError(t, err)

	tn.SetMensajeRef("msg-123")
	assert.Equal(t, "msg-123", tn.MensajeRef())
}

func TestHydrateTurno(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)

	tn := domain.HydrateTurno(domain.HydrateTurnoParams{
		ID:         "turno-1",
		ClienteID:  99,
		Direccion:  domain.DireccionSaliente,
		Autor:      domain.AutorIA,
		Cuerpo:     "cuerpo hidratado",
		MensajeRef: "msg-999",
		CreatedAt:  created,
	})

	assert.Equal(t, "turno-1", tn.ID())
	assert.Equal(t, 99, tn.ClienteID())
	assert.Equal(t, domain.DireccionSaliente, tn.Direccion())
	assert.Equal(t, domain.AutorIA, tn.Autor())
	assert.Equal(t, "cuerpo hidratado", tn.Cuerpo())
	assert.Equal(t, "msg-999", tn.MensajeRef())
	assert.Equal(t, created, tn.CreatedAt())
}
