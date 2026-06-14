package domain_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

const (
	maxPermisoCodigoLen      = 60
	maxPermisoDescriptionLen = 255
	maxPermisoCategoriaLen   = 30
)

func TestPermission_StringAndCodeAndEquals(t *testing.T) {
	t.Parallel()
	p := domain.PermUsuariosListar
	assert.Equal(t, "usuarios:listar", p.Code())
	assert.Equal(t, "usuarios:listar", p.String())
	assert.True(t, p.Equals(domain.PermUsuariosListar))
	assert.False(t, p.Equals(domain.PermRolesListar))
}

func TestAllPermissions_NonEmpty(t *testing.T) {
	t.Parallel()
	perms := domain.AllPermissions()
	require.NotEmpty(t, perms)
	// Spot-check that the expected codes are present.
	expected := []domain.Permission{
		domain.PermUsuariosListar,
		domain.PermUsuariosVer,
		domain.PermUsuariosActualizar,
		domain.PermUsuariosDesactivar,
		domain.PermUsuariosAsignarRol,
		domain.PermRolesListar,
		domain.PermRolesCrear,
		domain.PermRolesActualizar,
		domain.PermRolesAsignarPermiso,
		domain.PermPermisosListar,
		domain.PermVentasListar,
		domain.PermVentasVer,
		domain.PermVentasCrear,
		domain.PermVentasCancelar,
		domain.PermVentasSubirImagenes,
		domain.PermVentasEliminarImagenes,
		domain.PermInventarioVer,
		domain.PermTraspasosVer,
		domain.PermStockConsultar,
		domain.PermAnalyticsWinbackRead,
		domain.PermAnalyticsRefresh,
	}
	got := make(map[domain.Permission]struct{}, len(perms))
	for _, p := range perms {
		got[p.Code] = struct{}{}
	}
	for _, e := range expected {
		_, ok := got[e]
		assert.Truef(t, ok, "missing permission %q from catalog", e)
	}
}

func TestAllPermissions_SortedDeterministically(t *testing.T) {
	t.Parallel()
	a := domain.AllPermissions()
	b := domain.AllPermissions()
	assert.Equal(t, a, b)

	// Returned slice is sorted by Code ascending.
	codes := make([]string, len(a))
	for i, p := range a {
		codes[i] = string(p.Code)
	}
	assert.True(t, sort.StringsAreSorted(codes), "AllPermissions must be sorted by Code")
}

func TestAllPermissions_UniqueByCode(t *testing.T) {
	t.Parallel()
	perms := domain.AllPermissions()
	seen := make(map[domain.Permission]struct{}, len(perms))
	for _, p := range perms {
		_, dup := seen[p.Code]
		assert.Falsef(t, dup, "duplicate code %q in catalog", p.Code)
		seen[p.Code] = struct{}{}
	}
}

func TestAllPermissions_FitFirebirdColumnWidths(t *testing.T) {
	t.Parallel()
	for _, p := range domain.AllPermissions() {
		assert.LessOrEqualf(t, len(string(p.Code)), maxPermisoCodigoLen,
			"code %q exceeds MSP_PERMISOS.CODIGO width", p.Code)
		assert.LessOrEqualf(t, len(p.Description), maxPermisoDescriptionLen,
			"description for %q exceeds MSP_PERMISOS.DESCRIPCION width", p.Code)
		assert.LessOrEqualf(t, len(p.Categoria), maxPermisoCategoriaLen,
			"categoria for %q exceeds MSP_PERMISOS.CATEGORIA width", p.Code)
		assert.NotEmptyf(t, p.Description, "empty description for %q", p.Code)
		assert.NotEmptyf(t, p.Categoria, "empty categoria for %q", p.Code)
	}
}

func TestAllPermissions_ReturnsFreshSlice(t *testing.T) {
	t.Parallel()
	a := domain.AllPermissions()
	a[0].Code = "mutated"
	b := domain.AllPermissions()
	assert.NotEqual(t, "mutated", string(b[0].Code), "AllPermissions must not share backing storage between calls")
}
