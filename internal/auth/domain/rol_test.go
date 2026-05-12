package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func strp(s string) *string { return &s }

func TestNewRol_AcceptsValid(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	creator := uuid.New()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)

	r, err := domain.NewRol(id, "  admin  ", strp(" administrador "), true, creator, now)
	require.NoError(t, err)

	assert.Equal(t, id, r.ID())
	assert.Equal(t, "admin", r.Nombre(), "nombre must be trimmed")
	require.NotNil(t, r.Description())
	assert.Equal(t, "administrador", *r.Description())
	assert.True(t, r.Inmutable())
	assert.True(t, r.Activo())
	assert.Equal(t, now, r.CreatedAt())
	assert.Equal(t, now, r.UpdatedAt())
	assert.Equal(t, creator, r.CreatedBy())
}

func TestNewRol_NilOrEmptyDescripcionCollapsesToNil(t *testing.T) {
	t.Parallel()
	r1, err := domain.NewRol(uuid.New(), "x", nil, false, uuid.New(), time.Now().UTC())
	require.NoError(t, err)
	assert.Nil(t, r1.Description())

	r2, err := domain.NewRol(uuid.New(), "x", strp("   "), false, uuid.New(), time.Now().UTC())
	require.NoError(t, err)
	assert.Nil(t, r2.Description())
}

func TestNewRol_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		nombre      string
		description *string
		code        string
	}{
		{"empty_nombre", "", nil, "rol_nombre_required"},
		{"whitespace_nombre", "   ", nil, "rol_nombre_required"},
		{"too_long_nombre", strings.Repeat("x", 51), nil, "rol_nombre_too_long"},
		{"too_long_description", "x", strp(strings.Repeat("d", 256)), "rol_description_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewRol(uuid.New(), tc.nombre, tc.description, false, uuid.New(), time.Now().UTC())
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.code, ae.Code)
		})
	}
}

func TestHydrateRol_BypassesValidation(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	r := domain.HydrateRol(domain.HydrateRolParams{
		ID:          id,
		Nombre:      "anything that would fail validation including being long",
		Description: nil,
		Inmutable:   false,
		Activo:      false,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		CreatedBy:   uuid.New(),
		UpdatedBy:   uuid.New(),
	})
	assert.Equal(t, id, r.ID())
	assert.False(t, r.Activo())
}

func TestRol_Update_MutableRol(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRol(uuid.New(), "old", strp("vieja"), false, uuid.New(), time.Now().UTC())
	require.NoError(t, err)

	updater := uuid.New()
	require.NoError(t, r.Update("new", strp("nueva"), updater, time.Now()))
	assert.Equal(t, "new", r.Nombre())
	require.NotNil(t, r.Description())
	assert.Equal(t, "nueva", *r.Description())
	assert.Equal(t, updater, r.UpdatedBy())
}

func TestRol_Update_ValidatesNewValues(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRol(uuid.New(), "ok", nil, false, uuid.New(), time.Now().UTC())
	require.NoError(t, err)
	err = r.Update("", nil, uuid.New(), time.Now())
	require.Error(t, err)
	ae, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "rol_nombre_required", ae.Code)
}

func TestRol_Update_InmutableRefusesMutation(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRol(uuid.New(), "admin", strp("desc"), true, uuid.New(), time.Now().UTC())
	require.NoError(t, err)

	err = r.Update("changed", strp("changed"), uuid.New(), time.Now())
	require.ErrorIs(t, err, domain.ErrRolInmutable)
	assert.Equal(t, "admin", r.Nombre(), "name must not have changed")

	err = r.Desactivar(uuid.New(), time.Now())
	require.ErrorIs(t, err, domain.ErrRolInmutable)
	assert.True(t, r.Activo(), "must still be active after refused desactivar")

	err = r.Reactivar(uuid.New(), time.Now())
	require.ErrorIs(t, err, domain.ErrRolInmutable)
}

func TestRol_DesactivarReactivar_MutableRol(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRol(uuid.New(), "x", nil, false, uuid.New(), time.Now().UTC())
	require.NoError(t, err)
	require.True(t, r.Activo())

	require.NoError(t, r.Desactivar(uuid.New(), time.Now()))
	assert.False(t, r.Activo())

	require.NoError(t, r.Reactivar(uuid.New(), time.Now()))
	assert.True(t, r.Activo())
}

func TestRol_AuditCopy(t *testing.T) {
	t.Parallel()
	r, err := domain.NewRol(uuid.New(), "x", nil, false, uuid.New(), time.Now().UTC())
	require.NoError(t, err)
	a := r.Audit()
	a.MarkUpdated(uuid.New())
	assert.Equal(t, r.CreatedBy(), r.UpdatedBy(), "audit accessor must return a copy")
}
