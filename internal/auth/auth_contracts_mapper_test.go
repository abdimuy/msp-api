package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/auth/domain"
)

func TestToContract_AllFieldsCopied(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	fuid, err := domain.NewFirebaseUID("alice-uid")
	require.NoError(t, err)
	email, err := domain.NewEmail("alice@example.com")
	require.NoError(t, err)
	nombre, err := domain.NewNombre("Alice")
	require.NoError(t, err)
	almacen := 7

	u := domain.NewUsuario(id, fuid, email, nombre, nil, &almacen, id, time.Now())
	perms := []domain.Permission{domain.PermUsuariosListar, domain.PermRolesListar}

	got := auth.ToContract(u, perms)

	assert.Equal(t, id, got.ID)
	assert.Equal(t, "alice-uid", got.FirebaseUID)
	assert.Equal(t, "alice@example.com", got.Email)
	assert.Equal(t, "Alice", got.Nombre)
	require.NotNil(t, got.AlmacenID)
	assert.Equal(t, 7, *got.AlmacenID)
	assert.Equal(t, []string{"usuarios:listar", "roles:listar"}, got.Permisos)
}

func TestToContract_NilAlmacen_PreservedAsNil(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	fuid, _ := domain.NewFirebaseUID("bob-uid")
	email, _ := domain.NewEmail("bob@example.com")
	nombre, _ := domain.NewNombre("Bob")
	u := domain.NewUsuario(id, fuid, email, nombre, nil, nil, id, time.Now())

	got := auth.ToContract(u, nil)

	assert.Nil(t, got.AlmacenID)
	assert.Empty(t, got.Permisos)
}

func TestToContract_PermsSliceIsFreshAllocation(t *testing.T) {
	t.Parallel()

	id := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	fuid, _ := domain.NewFirebaseUID("eve-uid")
	email, _ := domain.NewEmail("eve@example.com")
	nombre, _ := domain.NewNombre("Eve")
	u := domain.NewUsuario(id, fuid, email, nombre, nil, nil, id, time.Now())

	perms := []domain.Permission{domain.PermUsuariosListar}
	got := auth.ToContract(u, perms)
	require.Len(t, got.Permisos, 1)

	// Mutating the result must not affect the source.
	got.Permisos[0] = "tampered"
	assert.Equal(t, domain.PermUsuariosListar, perms[0],
		"ToContract must allocate a fresh slice for Permisos")
}
