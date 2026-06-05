package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
)

func mustEmail(t *testing.T, s string) domain.Email {
	t.Helper()
	e, err := domain.NewEmail(s)
	require.NoError(t, err)
	return e
}

func mustFirebaseUID(t *testing.T, s string) domain.FirebaseUID {
	t.Helper()
	f, err := domain.NewFirebaseUID(s)
	require.NoError(t, err)
	return f
}

func mustNombre(t *testing.T, s string) domain.Nombre {
	t.Helper()
	n, err := domain.NewNombre(s)
	require.NoError(t, err)
	return n
}

func mustTelefono(t *testing.T, s string) *platform.Telefono {
	t.Helper()
	tel, err := platform.NewTelefono(s)
	require.NoError(t, err)
	return &tel
}

func TestNewUsuario_InitializesFields(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	creator := uuid.New()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	almacen := 7

	u := domain.NewUsuario(
		id,
		mustFirebaseUID(t, "abc123"),
		mustEmail(t, "foo@bar.com"),
		mustNombre(t, "Foo Bar"),
		mustTelefono(t, "+15551234567"),
		&almacen,
		creator,
		now,
	)

	assert.Equal(t, id, u.ID())
	assert.Equal(t, "abc123", u.FirebaseUID().Value())
	assert.Equal(t, "foo@bar.com", u.Email().Value())
	assert.Equal(t, "Foo Bar", u.Nombre().Value())
	require.NotNil(t, u.Telefono())
	assert.Equal(t, "+15551234567", u.Telefono().Value())
	require.NotNil(t, u.AlmacenID())
	assert.Equal(t, 7, *u.AlmacenID())
	assert.True(t, u.Activo())
	assert.Equal(t, now, u.CreatedAt())
	assert.Equal(t, now, u.UpdatedAt())
	assert.Equal(t, creator, u.CreatedBy())
	assert.Equal(t, creator, u.UpdatedBy())
}

func TestNewUsuario_NilOptionalFields(t *testing.T) {
	t.Parallel()
	u := domain.NewUsuario(
		uuid.New(),
		mustFirebaseUID(t, "abc"),
		mustEmail(t, "x@y.com"),
		mustNombre(t, "Solo"),
		nil,
		nil,
		uuid.New(),
		time.Now().UTC(),
	)
	assert.Nil(t, u.Telefono())
	assert.Nil(t, u.AlmacenID())
}

func TestHydrateUsuario_BypassesValidation(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	createdBy := uuid.New()
	updatedBy := uuid.New()
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)

	u := domain.HydrateUsuario(domain.HydrateUsuarioParams{
		ID:          id,
		FirebaseUID: domain.HydrateFirebaseUID("anything goes"),
		Email:       domain.HydrateEmail("not-a-valid-email"),
		Nombre:      domain.HydrateNombre("@@@"),
		Telefono:    nil,
		AlmacenID:   nil,
		Activo:      false,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	})
	assert.Equal(t, id, u.ID())
	assert.False(t, u.Activo())
	assert.Equal(t, createdAt, u.CreatedAt())
	assert.Equal(t, updatedAt, u.UpdatedAt())
	assert.Equal(t, createdBy, u.CreatedBy())
	assert.Equal(t, updatedBy, u.UpdatedBy())
}

func TestUsuario_Update(t *testing.T) {
	t.Parallel()
	creator := uuid.New()
	updater := uuid.New()
	createdAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	u := domain.NewUsuario(
		uuid.New(),
		mustFirebaseUID(t, "abc"),
		mustEmail(t, "old@x.com"),
		mustNombre(t, "Old"),
		nil,
		nil,
		creator,
		createdAt,
	)
	beforeUpdate := time.Now()
	newAlmacen := 42
	u.Update(domain.UsuarioUpdate{
		Email:     mustEmail(t, "new@x.com"),
		Nombre:    mustNombre(t, "New"),
		Telefono:  mustTelefono(t, "+15559998888"),
		AlmacenID: &newAlmacen,
	}, updater, time.Now())

	assert.Equal(t, "new@x.com", u.Email().Value())
	assert.Equal(t, "New", u.Nombre().Value())
	require.NotNil(t, u.Telefono())
	assert.Equal(t, "+15559998888", u.Telefono().Value())
	require.NotNil(t, u.AlmacenID())
	assert.Equal(t, 42, *u.AlmacenID())
	assert.Equal(t, updater, u.UpdatedBy())
	assert.Equal(t, creator, u.CreatedBy(), "createdBy must not change")
	assert.GreaterOrEqual(t, u.UpdatedAt().UnixNano(), beforeUpdate.UnixNano())
}

func TestUsuario_DesactivarReactivar(t *testing.T) {
	t.Parallel()
	updater := uuid.New()
	u := domain.NewUsuario(
		uuid.New(),
		mustFirebaseUID(t, "abc"),
		mustEmail(t, "x@y.com"),
		mustNombre(t, "X"),
		nil, nil,
		uuid.New(),
		time.Now().UTC(),
	)
	require.True(t, u.Activo())

	u.Desactivar(updater, time.Now())
	assert.False(t, u.Activo())
	assert.Equal(t, updater, u.UpdatedBy())

	// Idempotent — calling again on an already-inactive usuario must not
	// produce an error and must keep activo=false.
	other := uuid.New()
	u.Desactivar(other, time.Now())
	assert.False(t, u.Activo())
	assert.Equal(t, other, u.UpdatedBy())

	u.Reactivar(updater, time.Now())
	assert.True(t, u.Activo())
}

func TestUsuario_RenameForSoftDelete(t *testing.T) {
	t.Parallel()
	updater := uuid.New()
	u := domain.NewUsuario(
		uuid.New(),
		mustFirebaseUID(t, "abc"),
		mustEmail(t, "x@y.com"),
		mustNombre(t, "X"),
		nil, nil,
		uuid.New(),
		time.Now().UTC(),
	)
	u.RenameForSoftDelete("x@y.com.deleted.123", "abc.deleted.123", updater, time.Now())
	assert.Equal(t, "x@y.com.deleted.123", u.Email().Value())
	assert.Equal(t, "abc.deleted.123", u.FirebaseUID().Value())
	assert.Equal(t, updater, u.UpdatedBy())
}

func TestUsuario_AuditCopy(t *testing.T) {
	t.Parallel()
	u := domain.NewUsuario(
		uuid.New(),
		mustFirebaseUID(t, "abc"),
		mustEmail(t, "x@y.com"),
		mustNombre(t, "X"),
		nil, nil,
		uuid.New(),
		time.Now().UTC(),
	)
	a := u.Audit()
	// Audit() returns a value: callers cannot mutate the entity through it.
	a.MarkUpdated(uuid.New())
	assert.Equal(t, u.CreatedBy(), u.UpdatedBy(), "internal audit must be untouched")
}

func TestNewUsuario_SetsEstatusFirebaseUser(t *testing.T) {
	t.Parallel()
	u := domain.NewUsuario(
		uuid.New(),
		mustFirebaseUID(t, "abc123"),
		mustEmail(t, "vendor@muebleriamsp.mx"),
		mustNombre(t, "Juan Pérez"),
		nil,
		nil,
		uuid.New(),
		time.Now().UTC(),
	)
	assert.Equal(t, domain.EstatusFirebaseUser, u.Estatus())
}

func TestNewVendedorUsuario_SetsEstatusVendedorOnly(t *testing.T) {
	t.Parallel()
	u := domain.NewVendedorUsuario(
		uuid.New(),
		mustEmail(t, "vendedor@muebleriamsp.mx"),
		mustNombre(t, "María López"),
		uuid.New(),
		time.Now().UTC(),
	)
	assert.Equal(t, domain.EstatusVendedorOnly, u.Estatus())
}

func TestNewVendedorUsuario_FirebaseUIDIsZero(t *testing.T) {
	t.Parallel()
	u := domain.NewVendedorUsuario(
		uuid.New(),
		mustEmail(t, "carlos@muebleriamsp.mx"),
		mustNombre(t, "Carlos Ramírez"),
		uuid.New(),
		time.Now().UTC(),
	)
	assert.True(t, u.FirebaseUID().IsZero(), "vendedor-only usuario must have zero FirebaseUID")
	assert.Nil(t, u.Telefono())
	assert.Nil(t, u.AlmacenID())
	assert.True(t, u.Activo())
}

func TestNewVendedorUsuario_AuditSeededFromNow(t *testing.T) {
	t.Parallel()
	createdBy := uuid.New()
	now := time.Date(2026, 6, 4, 10, 0, 0, 0, time.UTC)

	u := domain.NewVendedorUsuario(
		uuid.New(),
		mustEmail(t, "lucia@muebleriamsp.mx"),
		mustNombre(t, "Lucía Torres"),
		createdBy,
		now,
	)
	assert.Equal(t, now, u.CreatedAt())
	assert.Equal(t, now, u.UpdatedAt())
	assert.Equal(t, createdBy, u.CreatedBy())
	assert.Equal(t, createdBy, u.UpdatedBy())
}

func TestHydrateUsuario_PreservesEstatus(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	createdBy := uuid.New()
	updatedBy := uuid.New()
	now := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)

	u := domain.HydrateUsuario(domain.HydrateUsuarioParams{
		ID:          id,
		FirebaseUID: domain.HydrateFirebaseUID(""),
		Email:       domain.HydrateEmail("vendedor@muebleriamsp.mx"),
		Nombre:      domain.HydrateNombre("Ana García"),
		Telefono:    nil,
		AlmacenID:   nil,
		Activo:      true,
		Estatus:     domain.EstatusVendedorOnly,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	})
	assert.Equal(t, domain.EstatusVendedorOnly, u.Estatus())
}
