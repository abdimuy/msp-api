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

// EstatusMicrosipDeCliente fetches the cliente's current ESTATUS from
// Microsip (A/B/V/C) as a best-effort, NON-blocking hint. It must NEVER fail
// the venta detail read, so every adverse condition degrades to nil:
//   - reader not wired → nil
//   - venta has no cliente link → nil
//   - ANY reader error (cliente not found in Microsip, transient I/O, …) →
//     nil — degrade gracefully rather than failing the read
//   - ESTATUS is an empty string → nil — no useful info
func (s *Service) EstatusMicrosipDeCliente(ctx context.Context, v *domain.Venta) *string {
	if s.estatusReader == nil || v.ClienteID() == nil {
		return nil
	}
	est, err := s.estatusReader.EstatusDeCliente(ctx, *v.ClienteID())
	if err != nil {
		// Best-effort hint: degrade on ANY error (not-found, transient I/O, …)
		// rather than failing the venta detail read.
		return nil
	}
	if est == "" {
		return nil
	}
	return &est
}

// NombreMicrosipDeCliente fetches the cliente's current NOMBRE from Microsip
// as a best-effort, NON-blocking hint — the desktop uses it to show the
// venta's cliente name as read-only when a cliente_id is linked, guarding
// against the local snapshot drifting from the linked cliente (see the venta
// applied to the wrong person after only the name was edited). It must NEVER
// fail the venta detail read, so every adverse condition degrades to nil:
//   - reader not wired → nil
//   - venta has no cliente link → nil
//   - ANY reader error (cliente not found in Microsip, transient I/O, …) →
//     nil — degrade gracefully rather than failing the read
//   - NOMBRE is an empty string → nil — no useful info
//
// This is a READ HINT for the UI, NOT a guard: unlike
// validarEstatusClienteMicrosipPreExistente in aplicar_venta.go — which fails
// AplicarVenta closed when the cliente's estatus blocks the venta — this
// method never blocks anything. A Microsip outage or a not-found cliente
// simply means the desktop falls back to showing the locally-captured name.
func (s *Service) NombreMicrosipDeCliente(ctx context.Context, v *domain.Venta) *string {
	if s.nombreReader == nil || v.ClienteID() == nil {
		return nil
	}
	nombre, err := s.nombreReader.NombreDeCliente(ctx, *v.ClienteID())
	if err != nil {
		// Best-effort hint: degrade on ANY error (not-found, transient I/O, …)
		// rather than failing the venta detail read.
		return nil
	}
	if nombre == "" {
		return nil
	}
	return &nombre
}
