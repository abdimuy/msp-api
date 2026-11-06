package identidad_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/identidad"
)

func TestIdentidad_New_RechazaUsuarioNil(t *testing.T) {
	t.Parallel()

	permiso := func(context.Context, string) (bool, error) {
		return true, nil
	}

	require.PanicsWithValue(
		t,
		"identidad: UsuarioFn no puede ser nil",
		func() {
			identidad.New(nil, permiso)
		},
	)
}

func TestIdentidad_New_RechazaPermisoNil(t *testing.T) {
	t.Parallel()

	usuario := func(context.Context) (string, string, bool) {
		return "usuario-1", "Ruben", true
	}

	require.PanicsWithValue(
		t,
		"identidad: PermisoFn no puede ser nil",
		func() {
			identidad.New(usuario, nil)
		},
	)
}

func TestIdentidad_UsuarioActual_CaminoFeliz(t *testing.T) {
	t.Parallel()

	usuario := func(context.Context) (string, string, bool) {
		return "usuario-1", "Ruben", true
	}
	permiso := func(context.Context, string) (bool, error) {
		return true, nil
	}

	adapter := identidad.New(usuario, permiso)

	actual, err := adapter.UsuarioActual(context.Background())

	require.NoError(t, err)
	require.Equal(t, "usuario-1", actual.ID)
	require.Equal(t, "Ruben", actual.Nombre)
}

func TestIdentidad_UsuarioActual_SinUsuario(t *testing.T) {
	t.Parallel()

	usuario := func(context.Context) (string, string, bool) {
		return "", "", false
	}
	permiso := func(context.Context, string) (bool, error) {
		return true, nil
	}

	adapter := identidad.New(usuario, permiso)

	_, err := adapter.UsuarioActual(context.Background())

	require.ErrorIs(t, err, domain.ErrUsuarioNoAutenticado)
}

func TestIdentidad_UsuarioActual_NombreVacio(t *testing.T) {
	t.Parallel()

	usuario := func(context.Context) (string, string, bool) {
		return "usuario-1", "", true
	}
	permiso := func(context.Context, string) (bool, error) {
		return true, nil
	}

	adapter := identidad.New(usuario, permiso)

	_, err := adapter.UsuarioActual(context.Background())

	require.ErrorIs(t, err, domain.ErrUsuarioNoAutenticado)
}

func TestIdentidad_TienePermiso_CaminoFeliz(t *testing.T) {
	t.Parallel()

	usuario := func(context.Context) (string, string, bool) {
		return "usuario-1", "Ruben", true
	}

	var codigoRecibido string
	permiso := func(_ context.Context, codigo string) (bool, error) {
		codigoRecibido = codigo
		return true, nil
	}

	adapter := identidad.New(usuario, permiso)

	permitido, err := adapter.TienePermiso(
		context.Background(),
		domain.PermisoLeer,
	)

	require.NoError(t, err)
	require.True(t, permitido)
	require.Equal(t, "garantias:leer", codigoRecibido)
}

func TestIdentidad_TienePermiso_InvalidoNoLlamaFuncion(t *testing.T) {
	t.Parallel()

	usuario := func(context.Context) (string, string, bool) {
		return "usuario-1", "Ruben", true
	}

	llamadas := 0
	permiso := func(context.Context, string) (bool, error) {
		llamadas++
		return true, nil
	}

	adapter := identidad.New(usuario, permiso)

	permitido, err := adapter.TienePermiso(
		context.Background(),
		domain.Permiso("permiso-invalido"),
	)

	require.ErrorIs(t, err, domain.ErrPermisoInvalido)
	require.False(t, permitido)
	require.Zero(t, llamadas)
}

func TestIdentidad_TienePermiso_PropagaError(t *testing.T) {
	t.Parallel()

	errPermiso := errors.New("fallo consultando permisos")

	usuario := func(context.Context) (string, string, bool) {
		return "usuario-1", "Ruben", true
	}
	permiso := func(context.Context, string) (bool, error) {
		return false, errPermiso
	}

	adapter := identidad.New(usuario, permiso)

	permitido, err := adapter.TienePermiso(
		context.Background(),
		domain.PermisoLeer,
	)

	require.ErrorIs(t, err, errPermiso)
	require.False(t, permitido)
}
