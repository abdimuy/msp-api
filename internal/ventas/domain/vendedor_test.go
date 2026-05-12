package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestHydrateVendedor(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	userID := uuid.New()
	creator := uuid.New()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	snap, err := domain.NewVendedorSnapshot(domain.NewVendedorSnapshotParams{
		UsuarioID: userID, Email: "x@y.com", Nombre: "Foo",
	})
	require.NoError(t, err)

	v := domain.HydrateVendedor(domain.HydrateVendedorParams{
		ID:        id,
		Snapshot:  snap,
		CreatedAt: now,
		UpdatedAt: now,
		CreatedBy: creator,
		UpdatedBy: creator,
	})

	assert.Equal(t, id, v.ID())
	assert.Equal(t, userID, v.UsuarioID())
	assert.Equal(t, "x@y.com", v.Snapshot().Email())
	a := v.Audit()
	assert.Equal(t, creator, a.CreatedBy())
}

func TestVendedor_ViaCrearVenta_RejectsBadSnapshot(t *testing.T) {
	t.Parallel()
	params := validCrearVentaParams(t)
	params.Vendedores = []domain.CrearVentaVendedorInput{{
		ID: uuid.New(), UsuarioID: uuid.New(), Email: "", Nombre: "Foo",
	}}
	_, err := domain.CrearVenta(params)
	require.Error(t, err)
}
