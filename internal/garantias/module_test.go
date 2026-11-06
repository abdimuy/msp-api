package garantias

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/infra/identidad"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestModule_ConstruyeProviders(t *testing.T) {
	t.Parallel()

	pool := &firebird.Pool{}

	garantias := provideGarantiaRepo(pool)
	bandeja := provideBandejaRepo(pool)
	eventos := provideEventoRepo(pool)
	imagenes := provideImagenRepo(pool)
	folios := provideFolioGenerator(pool)
	tx := provideTxRunner(pool)
	clock := provideClock()

	require.NotNil(t, garantias)
	require.NotNil(t, bandeja)
	require.NotNil(t, eventos)
	require.NotNil(t, imagenes)
	require.NotNil(t, folios)
	require.NotNil(t, tx)
	require.False(t, clock.Now().IsZero())

	cfg := &config.Config{}
	cfg.Storage.Dir = t.TempDir()

	storageProvider, err := provideStorage(cfg)
	require.NoError(t, err)
	require.NotNil(t, storageProvider)

	usuarioFn := identidad.UsuarioFn(
		func(context.Context) (string, string, bool) {
			return "usuario-1", "Usuario Uno", true
		},
	)

	permisoFn := identidad.PermisoFn(
		func(context.Context, string) (bool, error) {
			return true, nil
		},
	)

	identity := provideIdentity(usuarioFn, permisoFn)
	require.NotNil(t, identity)

	service := provideService(
		garantias,
		eventos,
		imagenes,
		folios,
		tx,
		identity,
		clock,
		storageProvider,
	)
	require.NotNil(t, service)

	consultas := provideConsultas(
		garantias,
		bandeja,
		eventos,
		imagenes,
		storageProvider,
		identity,
	)
	require.NotNil(t, consultas)

	httpDeps := provideHTTPDeps(consultas, service)
	require.Same(t, consultas, httpDeps.Consultas)
	require.Same(t, service, httpDeps.Comandos)

	require.NotNil(t, Module())
}
