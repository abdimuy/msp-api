package main

import (
	"github.com/abdimuy/msp-api/internal/microsip"
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

// provideMicrosipCiudadRepo builds the Firebird-backed CiudadRepo.
func provideMicrosipCiudadRepo(p *firebird.Pool) microsipoutbound.CiudadRepo {
	return microsipfb.NewCiudadRepo(p)
}

// provideMicrosipService assembles the microsip application service.
func provideMicrosipService(
	almacenes microsipoutbound.AlmacenRepo,
	zonas microsipoutbound.ZonaClienteRepo,
	ciudades microsipoutbound.CiudadRepo,
) *microsipapp.Service {
	return microsipapp.NewService(almacenes, zonas, ciudades)
}

// provideMicrosipCatalogo exposes the microsip service as the cross-module
// Catalogo contract (consumed by the reactivación next-best-product reader).
func provideMicrosipCatalogo(svc *microsipapp.Service) microsip.Catalogo {
	return microsip.NewServiceAdapter(svc)
}
