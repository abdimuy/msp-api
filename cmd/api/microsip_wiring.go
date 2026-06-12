package main

import (
	microsipapp "github.com/abdimuy/msp-api/internal/microsip/app"
	"github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb"
	microsipoutbound "github.com/abdimuy/msp-api/internal/microsip/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// provideMicrosipAlmacenRepo builds the Firebird-backed AlmacenRepo with
// the configured price-list IDs interpolated into the article query.
func provideMicrosipAlmacenRepo(cfg *config.Config, p *firebird.Pool) microsipoutbound.AlmacenRepo {
	return microsipfb.NewAlmacenRepo(p, cfg.Microsip.PriceListIDs)
}

// provideMicrosipZonaRepo builds the Firebird-backed ZonaClienteRepo.
func provideMicrosipZonaRepo(p *firebird.Pool) microsipoutbound.ZonaClienteRepo {
	return microsipfb.NewZonaRepo(p)
}

// provideMicrosipService assembles the microsip application service.
func provideMicrosipService(
	almacenes microsipoutbound.AlmacenRepo,
	zonas microsipoutbound.ZonaClienteRepo,
) *microsipapp.Service {
	return microsipapp.NewService(almacenes, zonas)
}
