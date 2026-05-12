package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestNewPermiso_AcceptsValid(t *testing.T) {
	t.Parallel()
	p, err := domain.NewPermiso(domain.PermUsuariosListar, " listar usuarios ", "  usuarios  ")
	require.NoError(t, err)
	assert.Equal(t, domain.PermUsuariosListar, p.Codigo())
	assert.Equal(t, "listar usuarios", p.Description())
	assert.Equal(t, "usuarios", p.Categoria())
}

func TestNewPermiso_RejectsInvalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		codigo      domain.Permission
		description string
		categoria   string
		errCode     string
	}{
		{"empty_codigo", "", "desc", "cat", "permiso_codigo_required"},
		{"too_long_codigo", domain.Permission(strings.Repeat("x", 61)), "desc", "cat", "permiso_codigo_too_long"},
		{"empty_description", "u:l", "", "cat", "permiso_description_required"},
		{"whitespace_description", "u:l", "   ", "cat", "permiso_description_required"},
		{"too_long_description", "u:l", strings.Repeat("d", 256), "cat", "permiso_description_too_long"},
		{"empty_categoria", "u:l", "desc", "", "permiso_categoria_required"},
		{"too_long_categoria", "u:l", "desc", strings.Repeat("c", 31), "permiso_categoria_too_long"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewPermiso(tc.codigo, tc.description, tc.categoria)
			require.Error(t, err)
			ae, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, tc.errCode, ae.Code)
		})
	}
}

func TestHydratePermiso_NoValidation(t *testing.T) {
	t.Parallel()
	p := domain.HydratePermiso("", "", "")
	assert.Equal(t, domain.Permission(""), p.Codigo())
	assert.Empty(t, p.Description())
	assert.Empty(t, p.Categoria())
}

func TestPermiso_EqualsAndToMeta(t *testing.T) {
	t.Parallel()
	a, err := domain.NewPermiso(domain.PermUsuariosListar, "listar usuarios", "usuarios")
	require.NoError(t, err)
	b, err := domain.NewPermiso(domain.PermUsuariosListar, "listar usuarios", "usuarios")
	require.NoError(t, err)
	c, err := domain.NewPermiso(domain.PermRolesListar, "listar roles", "roles")
	require.NoError(t, err)

	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c))

	meta := a.ToMeta()
	assert.Equal(t, domain.PermUsuariosListar, meta.Code)
	assert.Equal(t, "listar usuarios", meta.Description)
	assert.Equal(t, "usuarios", meta.Categoria)
}
