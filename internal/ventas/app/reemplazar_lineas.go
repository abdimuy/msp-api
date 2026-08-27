//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ReemplazarLineasInput is the request DTO for replacing BOTH line-item
// collections of a venta in one shot.
type ReemplazarLineasInput struct {
	VentaID   uuid.UUID
	Combos    []CrearVentaComboInput
	Productos []CrearVentaProductoInput
}

// ReemplazarLineas replaces the venta's combos AND productos in a single
// transaction, validating the producto → combo references against the final
// state of both collections.
//
// It exists because the two single-collection commands cannot express the
// edit the desktop actually performs. The capture screen does not let a user
// change what is inside a combo, so changing a combo means deleting it and
// creating a new one — with a new id. Neither order of the two endpoints
// survives that:
//
//   - combos first  → the surviving productos still point at the deleted
//     combo id, so the domain rejects with producto_combo_referencia_invalida
//   - productos first → the new productos point at a combo that does not
//     exist yet, same rejection
//
// Inventory: ResincronizarTraspasoParaVenta runs EXACTLY ONCE per call, after
// both collections are persisted, against the final line-item set. The two
// single-collection commands each run their own resync, so the old two-request
// edit reserved stock twice — once against a half-updated venta. Folding both
// writes into one command removes that intermediate state entirely; see
// resincronizarTraspaso for why stock validation lives inside the inventario
// module (post-reversal) instead of here.
func (s *Service) ReemplazarLineas(ctx context.Context, in ReemplazarLineasInput, by uuid.UUID) (*domain.Venta, error) {
	now := s.clock.Now()
	venta, err := s.ventas.FindByID(ctx, in.VentaID)
	if err != nil {
		return nil, err
	}
	productos, err := buildProductoInputs(in.Productos)
	if err != nil {
		return nil, err
	}
	if err := venta.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos:    buildComboInputs(in.Combos),
		Productos: productos,
		By:        by,
		Now:       now,
	}); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		if err := s.ventas.ReplaceLineas(ctx, venta); err != nil {
			return err
		}
		return s.resincronizarTraspaso(ctx, venta, by, now)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
