//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

var (
	fixedNow          = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	fixedCohorte      = time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	fixedUltimaCompra = time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
)

func validParams() domain.CrearCohorteClienteParams {
	return domain.CrearCohorteClienteParams{
		ClienteID:             24037,
		Nombre:                "Minerva López",
		Telefono:              "238 123 4567",
		Segmento:              domain.SegmentoRecienLiquidado,
		EnControl:             false,
		FueContactado:         false,
		CohorteFecha:          fixedCohorte,
		FechaUltimaCompraBase: fixedUltimaCompra,
		Saldo:                 decimal.Zero,
		PorLiquidarPct:        decimal.Zero,
		Now:                   fixedNow,
	}
}

func TestCrearCohorteCliente_Success(t *testing.T) {
	t.Parallel()
	c, err := domain.CrearCohorteCliente(validParams())
	require.NoError(t, err)

	assert.NotEqual(t, uuid.Nil, c.ID())
	assert.Equal(t, 24037, c.ClienteID())
	assert.Equal(t, "Minerva López", c.Nombre())
	assert.Equal(t, "238 123 4567", c.Telefono())
	assert.Equal(t, domain.SegmentoRecienLiquidado, c.Segmento())
	assert.False(t, c.EnControl())
	assert.False(t, c.FueContactado())
	assert.Equal(t, fixedCohorte, c.CohorteFecha())
	assert.Equal(t, fixedUltimaCompra, c.FechaUltimaCompraBase())
	assert.True(t, c.Saldo().IsZero())
	assert.True(t, c.PorLiquidarPct().IsZero())
	assert.Equal(t, fixedNow, c.CreatedAt())
	assert.Equal(t, fixedNow, c.UpdatedAt())
}

func TestCrearCohorteCliente_PreservesFlags(t *testing.T) {
	t.Parallel()
	p := validParams()
	p.EnControl = true
	p.FueContactado = true
	p.Segmento = domain.SegmentoPorLiquidarHueco
	p.Saldo = decimal.RequireFromString("1200.50")
	p.PorLiquidarPct = decimal.RequireFromString("12.75")

	c, err := domain.CrearCohorteCliente(p)
	require.NoError(t, err)
	assert.True(t, c.EnControl())
	assert.True(t, c.FueContactado())
	assert.Equal(t, domain.SegmentoPorLiquidarHueco, c.Segmento())
	assert.Equal(t, "1200.5", c.Saldo().String())
	assert.Equal(t, "12.75", c.PorLiquidarPct().String())
}

func TestCrearCohorteCliente_ZeroUltimaCompraStaysZero(t *testing.T) {
	t.Parallel()
	p := validParams()
	p.FechaUltimaCompraBase = time.Time{}
	c, err := domain.CrearCohorteCliente(p)
	require.NoError(t, err)
	assert.True(t, c.FechaUltimaCompraBase().IsZero())
}

func TestCrearCohorteCliente_NormalizesToUTC(t *testing.T) {
	t.Parallel()
	loc := time.FixedZone("MX", -6*3600)
	p := validParams()
	p.CohorteFecha = time.Date(2026, 7, 20, 6, 0, 0, 0, loc)
	p.FechaUltimaCompraBase = time.Date(2026, 5, 10, 6, 0, 0, 0, loc)

	c, err := domain.CrearCohorteCliente(p)
	require.NoError(t, err)
	assert.Equal(t, time.UTC, c.CohorteFecha().Location())
	assert.Equal(t, time.UTC, c.FechaUltimaCompraBase().Location())
	assert.True(t, c.CohorteFecha().Equal(time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)))
}

func TestCrearCohorteCliente_Invariants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(p *domain.CrearCohorteClienteParams)
		wantErr error
	}{
		{
			name:    "cliente_id_zero",
			mutate:  func(p *domain.CrearCohorteClienteParams) { p.ClienteID = 0 },
			wantErr: domain.ErrCohorteClienteIDInvalido,
		},
		{
			name:    "cliente_id_negative",
			mutate:  func(p *domain.CrearCohorteClienteParams) { p.ClienteID = -5 },
			wantErr: domain.ErrCohorteClienteIDInvalido,
		},
		{
			name:    "segmento_invalido",
			mutate:  func(p *domain.CrearCohorteClienteParams) { p.Segmento = domain.Segmento("x") },
			wantErr: domain.ErrSegmentoInvalido,
		},
		{
			name:    "saldo_negative",
			mutate:  func(p *domain.CrearCohorteClienteParams) { p.Saldo = decimal.RequireFromString("-1") },
			wantErr: domain.ErrCohorteSaldoInvalido,
		},
		{
			name:    "cohorte_fecha_zero",
			mutate:  func(p *domain.CrearCohorteClienteParams) { p.CohorteFecha = time.Time{} },
			wantErr: domain.ErrCohorteFechaInvalida,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validParams()
			tc.mutate(&p)
			c, err := domain.CrearCohorteCliente(p)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, c)
		})
	}
}

func TestHydrateCohorteCliente(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	created := time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)

	c := domain.HydrateCohorteCliente(domain.HydrateCohorteClienteParams{
		ID:                    id,
		ClienteID:             99,
		Nombre:                "Cliente Hidratado",
		Telefono:              "238 999 0000",
		Segmento:              domain.SegmentoPorLiquidarHueco,
		EnControl:             true,
		FueContactado:         true,
		CohorteFecha:          fixedCohorte,
		FechaUltimaCompraBase: fixedUltimaCompra,
		Saldo:                 decimal.RequireFromString("500.00"),
		PorLiquidarPct:        decimal.RequireFromString("8.00"),
		CreatedAt:             created,
		UpdatedAt:             updated,
	})

	assert.Equal(t, id, c.ID())
	assert.Equal(t, 99, c.ClienteID())
	assert.Equal(t, "Cliente Hidratado", c.Nombre())
	assert.Equal(t, "238 999 0000", c.Telefono())
	assert.Equal(t, domain.SegmentoPorLiquidarHueco, c.Segmento())
	assert.True(t, c.EnControl())
	assert.True(t, c.FueContactado())
	assert.Equal(t, fixedCohorte, c.CohorteFecha())
	assert.Equal(t, fixedUltimaCompra, c.FechaUltimaCompraBase())
	assert.Equal(t, "500", c.Saldo().String())
	assert.Equal(t, "8", c.PorLiquidarPct().String())
	assert.Equal(t, created, c.CreatedAt())
	assert.Equal(t, updated, c.UpdatedAt())
}
