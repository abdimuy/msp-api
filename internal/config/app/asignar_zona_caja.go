package app

import (
	"context"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// AsignarZonaCaja validates and upserts the zona→caja/cajero/vendedor/
// cobrador mapping for zonaClienteID.
//
// Order of checks: (1) domain.NewZonaCajaConfig validates every id is either
// strictly positive or the SinMapeoZonaCaja (-1) sentinel; (2) the zona must
// exist in ZONAS_CLIENTES, else NotFound; (3) every non-sentinel slot must
// exist in its own Microsip catalog, else a validation error naming that
// slot — -1 slots skip this check entirely; (4) the config is upserted.
func (s *Service) AsignarZonaCaja(ctx context.Context, zonaClienteID, cajaID, cajeroID, vendedorID, cobradorID int) error {
	cfg, err := configdomain.NewZonaCajaConfig(zonaClienteID, cajaID, cajeroID, vendedorID, cobradorID)
	if err != nil {
		return err
	}

	existe, err := s.zonaCajaExistente.ZonaExiste(ctx, cfg.ZonaClienteID)
	if err != nil {
		return err
	}
	if !existe {
		return errZonaNoExiste
	}

	checks := []struct {
		id      int
		exists  func(context.Context, int) (bool, error)
		errFunc error
	}{
		{cfg.CajaID, s.zonaCajaExistente.CajaExiste, errCajaNoExiste},
		{cfg.CajeroID, s.zonaCajaExistente.CajeroExiste, errCajeroNoExiste},
		{cfg.VendedorID, s.zonaCajaExistente.VendedorExiste, errVendedorNoExisteCatalogo},
		{cfg.CobradorID, s.zonaCajaExistente.CobradorExiste, errCobradorNoExiste},
	}
	for _, c := range checks {
		if c.id == configdomain.SinMapeoZonaCaja {
			continue
		}
		ok, cerr := c.exists(ctx, c.id)
		if cerr != nil {
			return cerr
		}
		if !ok {
			return c.errFunc
		}
	}

	return s.repo.UpsertZonaCajaConfig(ctx, cfg)
}
