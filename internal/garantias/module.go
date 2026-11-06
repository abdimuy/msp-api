// Package garantias compone el módulo sellado de garantías.
package garantias

import (
	"path/filepath"

	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/infra/identidad"
	"github.com/abdimuy/msp-api/internal/garantias/infra/storage"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Module construye todas las dependencias internas del módulo de garantías.
//
// El composition root únicamente debe proveer:
//   - *config.Config
//   - *firebird.Pool
//   - identidad.UsuarioFn
//   - identidad.PermisoFn
//
// El montaje HTTP se realiza desde cmd/api/server.go.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			provideGarantiaRepo,
			provideBandejaRepo,
			provideEventoRepo,
			provideImagenRepo,
			provideFolioGenerator,
			provideTxRunner,
			provideClock,
			provideStorage,
			provideIdentity,
			provideService,
			provideConsultas,
			provideHTTPDeps,
		),
	)
}

func provideGarantiaRepo(pool *firebird.Pool) outbound.GarantiaRepo {
	return garfb.NewGarantiaRepo(pool)
}

func provideBandejaRepo(pool *firebird.Pool) outbound.BandejaRepo {
	return garfb.NewBandejaRepo(pool)
}

func provideEventoRepo(pool *firebird.Pool) outbound.EventoRepo {
	return garfb.NewEventoRepo(pool)
}

func provideImagenRepo(pool *firebird.Pool) outbound.ImagenRepo {
	return garfb.NewImagenRepo(pool)
}

func provideFolioGenerator(pool *firebird.Pool) outbound.FolioGenerator {
	return garfb.NewFolioGenerator(pool)
}

func provideTxRunner(pool *firebird.Pool) outbound.TxRunner {
	return firebird.NewTxManager(pool.DB)
}

func provideClock() outbound.Clock {
	return outbound.ProductionClock{}
}

func provideStorage(cfg *config.Config) (outbound.StorageProvider, error) {
	baseDir := filepath.Join(cfg.Storage.Dir, "garantias")

	provider, err := storage.NewFilesystemProvider(baseDir)
	if err != nil {
		return nil, err
	}

	return provider, nil
}

func provideIdentity(
	usuario identidad.UsuarioFn,
	permiso identidad.PermisoFn,
) outbound.Identity {
	return identidad.New(usuario, permiso)
}

func provideService(
	garantias outbound.GarantiaRepo,
	eventos outbound.EventoRepo,
	imagenes outbound.ImagenRepo,
	folios outbound.FolioGenerator,
	tx outbound.TxRunner,
	identity outbound.Identity,
	clock outbound.Clock,
	storageProvider outbound.StorageProvider,
) *app.Service {
	return app.NewService(app.Deps{
		GarantiaRepo:   garantias,
		EventoRepo:     eventos,
		ImagenRepo:     imagenes,
		FolioGenerator: folios,
		TxRunner:       tx,
		Identity:       identity,
		Clock:          clock,
		Storage:        storageProvider,
	})
}

func provideConsultas(
	garantias outbound.GarantiaRepo,
	bandeja outbound.BandejaRepo,
	eventos outbound.EventoRepo,
	imagenes outbound.ImagenRepo,
	storageProvider outbound.StorageProvider,
	identity outbound.Identity,
) *app.Consultas {
	return app.NewConsultas(app.ConsultasDeps{
		Garantias: garantias,
		Bandeja:   bandeja,
		Eventos:   eventos,
		Imagenes:  imagenes,
		Storage:   storageProvider,
		Identity:  identity,
	})
}

func provideHTTPDeps(
	consultas *app.Consultas,
	service *app.Service,
) garhttp.Deps {
	return garhttp.Deps{
		Consultas: consultas,
		Comandos:  service, //nolint:misspell // nombre existente del contrato de garhttp.
	}
}
