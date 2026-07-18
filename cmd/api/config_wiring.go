package main

import (
	authapp "github.com/abdimuy/msp-api/internal/auth/app"
	configapp "github.com/abdimuy/msp-api/internal/config/app"
	"github.com/abdimuy/msp-api/internal/config/infra/clients"
	configfb "github.com/abdimuy/msp-api/internal/config/infra/configfb"
	configoutbound "github.com/abdimuy/msp-api/internal/config/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// provideConfigRepo builds the Firebird-backed ConfigRepo for the config
// module. The concrete type also satisfies outbound.CatalogoReader, so fx
// supplies this single value for both port parameters of provideConfigService.
func provideConfigRepo(pool *firebird.Pool) *configfb.ConfigRepo {
	return configfb.NewConfigRepo(pool)
}

// provideConfigUsuariosReader wires the cross-module adapter so fx can inject
// it as the configoutbound.UsuariosReader port. authapp.Service satisfies
// auth.UsuariosLister.
func provideConfigUsuariosReader(authSvc *authapp.Service) configoutbound.UsuariosReader {
	return clients.NewAuthUsuariosClient(authSvc)
}

// provideConfigService assembles the config module's Service.
func provideConfigService(
	repo *configfb.ConfigRepo,
	usuarios configoutbound.UsuariosReader,
) *configapp.Service {
	return configapp.NewService(repo, repo, usuarios)
}
