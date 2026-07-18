package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/config/domain"
)

func intPtr(v int) *int { return &v }

func TestNewVendedorMapping_AllNil_OK(t *testing.T) {
	t.Parallel()
	usuarioID := uuid.New()

	m, err := domain.NewVendedorMapping(usuarioID, nil, nil, nil)

	require.NoError(t, err)
	assert.Equal(t, usuarioID, m.UsuarioID)
	assert.Nil(t, m.ListaID1)
	assert.Nil(t, m.ListaID2)
	assert.Nil(t, m.ListaID3)
}

func TestNewVendedorMapping_ValidIDs_OK(t *testing.T) {
	t.Parallel()
	usuarioID := uuid.New()

	m, err := domain.NewVendedorMapping(usuarioID, intPtr(10), intPtr(20), intPtr(30))

	require.NoError(t, err)
	require.NotNil(t, m.ListaID1)
	require.NotNil(t, m.ListaID2)
	require.NotNil(t, m.ListaID3)
	assert.Equal(t, 10, *m.ListaID1)
	assert.Equal(t, 20, *m.ListaID2)
	assert.Equal(t, 30, *m.ListaID3)
}

func TestNewVendedorMapping_PartialSlots_OK(t *testing.T) {
	t.Parallel()

	m, err := domain.NewVendedorMapping(uuid.New(), intPtr(5), nil, nil)

	require.NoError(t, err)
	require.NotNil(t, m.ListaID1)
	assert.Equal(t, 5, *m.ListaID1)
	assert.Nil(t, m.ListaID2)
	assert.Nil(t, m.ListaID3)
}

func TestNewVendedorMapping_RejectsZeroAndNegative(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		l1, l2, l3 *int
	}{
		{"slot1 zero", intPtr(0), nil, nil},
		{"slot1 negative", intPtr(-1), nil, nil},
		{"slot2 zero", nil, intPtr(0), nil},
		{"slot2 negative", nil, intPtr(-5), nil},
		{"slot3 zero", nil, nil, intPtr(0)},
		{"slot3 negative", nil, nil, intPtr(-1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewVendedorMapping(uuid.New(), tc.l1, tc.l2, tc.l3)
			require.ErrorIs(t, err, domain.ErrVendedorListaIDInvalido)
		})
	}
}
