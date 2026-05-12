package auth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/auth"
)

func TestCurrentUserFromContext_Missing_ReturnsFalse(t *testing.T) {
	t.Parallel()

	got, ok := auth.CurrentUserFromContext(context.Background())
	assert.False(t, ok)
	assert.Equal(t, auth.CurrentUser{}, got)
}

func TestPlantCurrentUser_RoundTrip(t *testing.T) {
	t.Parallel()

	almacen := 42
	want := auth.CurrentUser{
		ID:          uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		FirebaseUID: "alice",
		Email:       "alice@example.com",
		Nombre:      "Alice",
		AlmacenID:   &almacen,
		Permisos:    []string{string(auth.PermUsuariosListar), string(auth.PermRolesListar)},
	}

	ctx := auth.PlantCurrentUser(context.Background(), want)
	got, ok := auth.CurrentUserFromContext(ctx)

	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestPlantCurrentUser_DoesNotMutateBaseContext(t *testing.T) {
	t.Parallel()

	base := context.Background()
	_ = auth.PlantCurrentUser(base, auth.CurrentUser{FirebaseUID: "alice"})

	// The base context must remain user-less after planting on a derived ctx.
	_, ok := auth.CurrentUserFromContext(base)
	assert.False(t, ok)
}

func TestCurrentUserFromContext_WrongTypeValue_ReturnsFalse(t *testing.T) {
	t.Parallel()

	// A different context key with the same shape must not collide. We can't
	// construct the unexported key from outside the package, but we can ensure
	// that an unrelated string-keyed value does not leak.
	type unrelatedKey struct{}
	ctx := context.WithValue(context.Background(), unrelatedKey{}, "alice")
	_, ok := auth.CurrentUserFromContext(ctx)
	assert.False(t, ok)
}
