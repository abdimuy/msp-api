package app

import (
	"context"

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
// Microsip and reports whether it mismatches the venta's own zona. The
// mismatch is a best-effort, NON-blocking warning: it must NEVER fail the
// venta detail read, so every adverse condition degrades to "no mismatch
// info" (nil, false):
//   - reader not wired → (nil, false)
//   - venta has no cliente link → (nil, false)
//   - ANY reader error (cliente not found in Microsip, transient I/O, …) →
//     (nil, false) — degrade gracefully rather than failing the read
//   - cliente zona NULL → (nil, false) — no constraint
//
// mismatch is true only when BOTH zonas are present AND they differ. Because
// the method never propagates an error, it returns no error value.
func (s *Service) ZonaMicrosipDeVenta(ctx context.Context, v *domain.Venta) (*int, bool) {
	if s.zonaReader == nil || v.ClienteID() == nil {
		return nil, false
	}
	z, err := s.zonaReader.ZonaDeCliente(ctx, *v.ClienteID())
	if err != nil {
		// Best-effort warning: degrade on ANY error (not-found, transient
		// I/O, …) rather than failing the venta detail read.
		return nil, false
	}
	if z == nil {
		return nil, false
	}
	ventaZona := v.Direccion().ZonaClienteID()
	mismatch := ventaZona != nil && *ventaZona != *z
	return z, mismatch
}
