package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ObtenerVenta loads a venta by its ID. Returns ErrVentaNotFound on miss.
// Pure pass-through to the repository — the query layer adds no logic over
// the read path today.
func (s *Service) ObtenerVenta(ctx context.Context, ventaID uuid.UUID) (*domain.Venta, error) {
	return s.ventas.FindByID(ctx, ventaID)
}

// ZonaMicrosipDeVenta fetches the cliente's current ZONA_CLIENTE_ID from
// Microsip and reports whether it mismatches the venta's own zona. It is
// intentionally non-blocking:
//   - reader not wired → (nil, false, nil)
//   - venta has no cliente link → (nil, false, nil)
//   - cliente not found in Microsip → (nil, false, nil) — degrade gracefully
//   - cliente zona NULL → (nil, false, nil) — no constraint
//   - any other reader error → (nil, false, err)
//
// mismatch is true only when BOTH zonas are present AND they differ.
func (s *Service) ZonaMicrosipDeVenta(ctx context.Context, v *domain.Venta) (*int, bool, error) {
	if s.zonaReader == nil || v.ClienteID() == nil {
		return nil, false, nil
	}
	z, err := s.zonaReader.ZonaDeCliente(ctx, *v.ClienteID())
	if err != nil {
		if errors.Is(err, domain.ErrClienteNotFoundInMicrosip) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if z == nil {
		return nil, false, nil
	}
	ventaZona := v.Direccion().ZonaClienteID()
	mismatch := ventaZona != nil && *ventaZona != *z
	return z, mismatch, nil
}
