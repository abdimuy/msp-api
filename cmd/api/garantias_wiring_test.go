package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/garantias"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
)

func TestGarantiasWiring_ModuloEnGrafo(t *testing.T) {
	t.Parallel()

	opts := append(
		appOptions(),
		fx.Invoke(func(garhttp.Deps) {}),
	)

	require.NoError(t, fx.ValidateApp(opts...))
}

func TestGarantiasWiring_UsuarioDesdeAuth(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	ctx := auth.PlantCurrentUser(context.Background(), auth.CurrentUser{
		ID:       usuarioID,
		Nombre:   "Rubén",
		Permisos: []string{"garantias:leer"},
	})

	id, nombre, ok := provideGarantiasUsuarioFn()(ctx)

	require.True(t, ok)
	require.Equal(t, usuarioID.String(), id)
	require.Equal(t, "Rubén", nombre)
}

func TestGarantiasWiring_UsuarioAnonimo(t *testing.T) {
	t.Parallel()

	id, nombre, ok := provideGarantiasUsuarioFn()(context.Background())

	require.False(t, ok)
	require.Empty(t, id)
	require.Empty(t, nombre)
}

func TestGarantiasWiring_PermisosDesdeAuth(t *testing.T) {
	t.Parallel()

	ctx := auth.PlantCurrentUser(context.Background(), auth.CurrentUser{
		ID:       uuid.New(),
		Nombre:   "Rubén",
		Permisos: []string{"garantias:leer", "garantias:actualizar"},
	})

	permiso := provideGarantiasPermisoFn()

	tiene, err := permiso(ctx, "garantias:leer")
	require.NoError(t, err)
	require.True(t, tiene)

	tiene, err = permiso(ctx, "garantias:actualizar")
	require.NoError(t, err)
	require.True(t, tiene)

	tiene, err = permiso(ctx, "garantias:cerrar")
	require.NoError(t, err)
	require.False(t, tiene)
}

func TestGarantiasWiring_PermisoSinUsuario(t *testing.T) {
	t.Parallel()

	tiene, err := provideGarantiasPermisoFn()(
		context.Background(),
		"garantias:leer",
	)

	require.NoError(t, err)
	require.False(t, tiene)
}

func TestGarantiasWiring_PermisosCoincidenConCatalogoAuth(t *testing.T) {
	t.Parallel()

	esperados := garantias.Permisos()
	actuales := make(map[string]authdomain.PermissionMeta)

	for _, permiso := range authdomain.AllPermissions() {
		if permiso.Categoria == "garantias" {
			actuales[permiso.Code.String()] = permiso
		}
	}

	require.Len(t, actuales, len(esperados))

	for _, esperado := range esperados {
		actual, ok := actuales[esperado.Codigo]
		require.Truef(
			t,
			ok,
			"falta permiso de garantías %q en auth",
			esperado.Codigo,
		)
		require.Equal(t, esperado.Description, actual.Description)
		require.Equal(t, "garantias", actual.Categoria)
	}
}
