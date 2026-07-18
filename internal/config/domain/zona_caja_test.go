package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/config/domain"
)

func TestNewZonaCajaConfig_Valido_TodosLosSlotsAsignados(t *testing.T) {
	t.Parallel()
	cfg, err := domain.NewZonaCajaConfig(12271, 12151, 22368, 87340, 11294)

	require.NoError(t, err)
	assert.Equal(t, 12271, cfg.ZonaClienteID)
	assert.Equal(t, 12151, cfg.CajaID)
	assert.Equal(t, 22368, cfg.CajeroID)
	assert.Equal(t, 87340, cfg.VendedorID)
	assert.Equal(t, 11294, cfg.CobradorID)
}

func TestNewZonaCajaConfig_Valido_SentinelaSinMapeoEnCadaSlot(t *testing.T) {
	t.Parallel()
	cfg, err := domain.NewZonaCajaConfig(12271, -1, -1, -1, -1)

	require.NoError(t, err)
	assert.Equal(t, -1, cfg.CajaID)
	assert.Equal(t, -1, cfg.CajeroID)
	assert.Equal(t, -1, cfg.VendedorID)
	assert.Equal(t, -1, cfg.CobradorID)
}

func TestNewZonaCajaConfig_ZonaClienteIDInvalido(t *testing.T) {
	t.Parallel()
	cases := []int{0, -1, -5}
	for _, zonaID := range cases {
		_, err := domain.NewZonaCajaConfig(zonaID, 12151, 22368, 87340, 11294)
		require.ErrorIs(t, err, domain.ErrZonaCajaIDInvalido)
	}
}

func TestNewZonaCajaConfig_SlotCero_Rechazado(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                             string
		caja, cajero, vendedor, cobrador int
	}{
		{"caja cero", 0, 22368, 87340, 11294},
		{"cajero cero", 12151, 0, 87340, 11294},
		{"vendedor cero", 12151, 22368, 0, 11294},
		{"cobrador cero", 12151, 22368, 87340, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewZonaCajaConfig(12271, tc.caja, tc.cajero, tc.vendedor, tc.cobrador)
			require.ErrorIs(t, err, domain.ErrZonaCajaIDInvalido)
		})
	}
}

func TestNewZonaCajaConfig_SlotNegativoDistintoDeSentinela_Rechazado(t *testing.T) {
	t.Parallel()
	_, err := domain.NewZonaCajaConfig(12271, -2, 22368, 87340, 11294)
	require.ErrorIs(t, err, domain.ErrZonaCajaIDInvalido)
}
