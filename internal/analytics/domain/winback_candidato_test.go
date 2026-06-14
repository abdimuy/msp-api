package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// now is a deterministic timestamp used across all tests in this file.
var now = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)

// validParams returns a fully-valid CrearWinbackCandidatoParams.
// Tests that need to violate an invariant copy and mutate this struct.
func validParams() domain.CrearWinbackCandidatoParams {
	return domain.CrearWinbackCandidatoParams{
		ClienteID:         42,
		Nombre:            "María López",
		Zona:              "Norte",
		Telefono:          "5512345678",
		FechaUltimaCompra: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Frecuencia:        3,
		Monetary:          decimal.NewFromFloat(15000.50),
		Saldo:             decimal.NewFromFloat(500.00),
		PorLiquidarPct:    decimal.NewFromFloat(3.33),
		NextBestProduct:   "Sala Esquinera",
		EnControl:         false,
		CohorteFecha:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Now:               now,
	}
}

func TestCrearWinbackCandidato(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		mutate    func(*domain.CrearWinbackCandidatoParams)
		wantErr   error
		wantErrIs bool
	}{
		{
			name:   "valid input returns entity without error",
			mutate: nil,
		},
		{
			name: "frecuencia negative returns frecuencia sentinel",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.Frecuencia = -1
			},
			wantErr:   domain.ErrWinbackCandidatoFrecuenciaInvalida,
			wantErrIs: true,
		},
		{
			name: "frecuencia zero is valid",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.Frecuencia = 0
			},
		},
		{
			name: "monetary negative returns monto sentinel",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.Monetary = decimal.NewFromFloat(-0.01)
			},
			wantErr:   domain.ErrWinbackCandidatoMontoInvalido,
			wantErrIs: true,
		},
		{
			name: "monetary zero is valid",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.Monetary = decimal.Zero
			},
		},
		{
			name: "saldo negative returns saldo sentinel",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.Saldo = decimal.NewFromFloat(-100)
			},
			wantErr:   domain.ErrWinbackCandidatoSaldoInvalido,
			wantErrIs: true,
		},
		{
			name: "saldo zero is valid",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.Saldo = decimal.Zero
			},
		},
		{
			name: "cohorte fecha zero returns cohorte sentinel",
			mutate: func(p *domain.CrearWinbackCandidatoParams) {
				p.CohorteFecha = time.Time{}
			},
			wantErr:   domain.ErrWinbackCandidatoCohorteInvalida,
			wantErrIs: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := validParams()
			if tc.mutate != nil {
				tc.mutate(&p)
			}

			got, err := domain.CrearWinbackCandidato(p)

			if tc.wantErrIs {
				require.Error(t, err)
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, got)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)

			// Verify every getter returns the value passed in.
			assert.NotEqual(t, uuid.Nil, got.ID(), "id must be non-zero uuid")
			assert.Equal(t, p.ClienteID, got.ClienteID())
			assert.Equal(t, p.Nombre, got.Nombre())
			assert.Equal(t, p.Zona, got.Zona())
			assert.Equal(t, p.Telefono, got.Telefono())
			assert.Equal(t, p.FechaUltimaCompra.UTC(), got.FechaUltimaCompra())
			assert.Equal(t, p.Frecuencia, got.Frecuencia())
			assert.True(t, p.Monetary.Equal(got.Monetary()), "monetary mismatch")
			assert.True(t, p.Saldo.Equal(got.Saldo()), "saldo mismatch")
			assert.True(t, p.PorLiquidarPct.Equal(got.PorLiquidarPct()), "porLiquidarPct mismatch")
			assert.Equal(t, p.NextBestProduct, got.NextBestProduct())
			assert.Equal(t, p.EnControl, got.EnControl())
			assert.Equal(t, p.CohorteFecha.UTC(), got.CohorteFecha())

			// Audit timestamps come from p.Now.
			assert.Equal(t, now, got.CreatedAt())
			assert.Equal(t, now, got.UpdatedAt())
		})
	}
}

func TestHydrateWinbackCandidato(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2025, 11, 1, 8, 0, 0, 0, time.UTC)
	updatedAt := time.Date(2026, 1, 10, 9, 30, 0, 0, time.UTC)
	id := uuid.New()

	p := domain.HydrateWinbackCandidatoParams{
		ID:                id,
		ClienteID:         99,
		Nombre:            "Pedro García",
		Zona:              "Sur",
		Telefono:          "5598765432",
		FechaUltimaCompra: time.Date(2025, 10, 15, 0, 0, 0, 0, time.UTC),
		Frecuencia:        7,
		Monetary:          decimal.NewFromFloat(35000),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		NextBestProduct:   "",
		EnControl:         true,
		CohorteFecha:      time.Date(2025, 10, 1, 0, 0, 0, 0, time.UTC),
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
	}

	got := domain.HydrateWinbackCandidato(p)
	require.NotNil(t, got)

	assert.Equal(t, id, got.ID())
	assert.Equal(t, p.ClienteID, got.ClienteID())
	assert.Equal(t, p.Nombre, got.Nombre())
	assert.Equal(t, p.Zona, got.Zona())
	assert.Equal(t, p.Telefono, got.Telefono())
	assert.Equal(t, p.FechaUltimaCompra, got.FechaUltimaCompra())
	assert.Equal(t, p.CohorteFecha, got.CohorteFecha())
	assert.Equal(t, p.Frecuencia, got.Frecuencia())
	assert.True(t, p.Monetary.Equal(got.Monetary()))
	assert.True(t, p.Saldo.Equal(got.Saldo()))
	assert.True(t, p.PorLiquidarPct.Equal(got.PorLiquidarPct()))
	assert.Equal(t, p.NextBestProduct, got.NextBestProduct())
	assert.True(t, got.EnControl())
	assert.Equal(t, createdAt, got.CreatedAt())
	assert.Equal(t, updatedAt, got.UpdatedAt())
}
