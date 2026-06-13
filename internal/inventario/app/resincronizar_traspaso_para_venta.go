//nolint:misspell // domain vocabulary is Spanish (traspaso, venta, etc.) per project convention.
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// detalleMultiset represents an articuloID → summed-cantidad multiset used to
// compare the net effect of two sets of detalles regardless of ordering or
// duplicate lines.
type detalleMultiset map[int]decimal.Decimal

// buildDetalleMultiset sums cantidades by articuloID from raw input lines.
func buildDetalleMultiset(detalles []CrearTraspasoDetalleInput) detalleMultiset {
	m := make(detalleMultiset, len(detalles))
	for _, d := range detalles {
		m[d.ArticuloID] = m[d.ArticuloID].Add(d.Cantidad)
	}
	return m
}

// buildDetalleMultisetFromTraspaso extracts the articuloID → cantidad multiset
// from a persisted Traspaso. Duplicate articuloIDs are summed.
func buildDetalleMultisetFromTraspaso(t *domain.Traspaso) detalleMultiset {
	m := make(detalleMultiset)
	for det := range t.Detalles() {
		m[det.ArticuloID()] = m[det.ArticuloID()].Add(det.Cantidad().Value())
	}
	return m
}

// detalleMultisetsEqual returns true iff a and b have identical keys and
// values (using decimal.Equal for value comparison).
func detalleMultisetsEqual(a, b detalleMultiset) bool {
	if len(a) != len(b) {
		return false
	}
	for artID, cantA := range a {
		cantB, ok := b[artID]
		if !ok || !cantA.Equal(cantB) {
			return false
		}
	}
	return true
}

// sameNetEffect reports whether the proposed params would produce a directo
// traspaso with the same net inventory effect as the currently-active one.
// Returns false when active is nil.
func sameNetEffect(active *domain.Traspaso, p CrearTraspasoParaVentaParams) bool {
	if active == nil {
		return false
	}
	if active.AlmacenOrigen() != p.AlmacenOrigen || active.AlmacenDestino() != p.AlmacenDestino {
		return false
	}
	return detalleMultisetsEqual(
		buildDetalleMultisetFromTraspaso(active),
		buildDetalleMultiset(p.Detalles),
	)
}

// ResincronizarTraspasoParaVenta keeps the inventory reservation for a venta
// consistent with the current set of detalles. All writes execute in a single
// transaction.
//
// Semantics:
//   - No active directo + non-empty new detalles → create a new directo.
//   - Active directo + identical net effect → no-op; returns the active directo.
//   - Active directo + different detalles or almacenes → reverse the active,
//     then create a new directo (when new detalles are non-empty).
//   - Empty new detalles + active directo → reverse only; no new directo.
//   - Empty new detalles + no active directo → no-op; returns nil, 0, nil.
func (s *Service) ResincronizarTraspasoParaVenta(ctx context.Context, p CrearTraspasoParaVentaParams) (*domain.Traspaso, int, error) {
	var (
		reverso    *domain.Traspaso
		newDirecto *domain.Traspaso
		doctoInID  int
		// noopResult captures the active traspaso and its DoctoInID when the
		// fast-path (sameNetEffect) fires inside the transaction.
		noopActive    *domain.Traspaso
		noopDoctoInID int
		isNoop        bool
	)

	if err := s.runInTx(ctx, func(ctx context.Context) error {
		// Resolve the active directo inside the transaction so the read
		// participates in the same snapshot as any subsequent writes.
		active, err := s.activeDirect(ctx, p.VentaID)
		if err != nil && !errors.Is(err, domain.ErrTraspasoNoEncontrado) {
			return err
		}
		if errors.Is(err, domain.ErrTraspasoNoEncontrado) {
			active = nil
		}

		// Fast path: nothing to do — read-only tx commits trivially.
		if sameNetEffect(active, p) {
			if active.DoctoInID() == nil {
				return apperror.NewInternal(
					"traspaso_directo_sin_docto_in_id",
					"el traspaso directo no tiene un id de microsip asignado",
				)
			}
			isNoop = true
			noopActive = active
			noopDoctoInID = *active.DoctoInID()
			return nil
		}
		if len(p.Detalles) == 0 && active == nil {
			isNoop = true
			return nil
		}

		// Guard: if we need to reverse the active, it must have a DoctoInID.
		if active != nil && active.DoctoInID() == nil {
			return apperror.NewInternal(
				"traspaso_directo_sin_docto_in_id",
				"el traspaso directo no tiene un id de microsip asignado",
			)
		}

		// Reverse the active directo if one exists.
		if active != nil {
			newFolio, folioErr := s.folioMinter.MintFolio(ctx)
			if folioErr != nil {
				return folioErr
			}
			rev, revErr := active.Reversar(s.clock.Now(), p.CreatedBy, uuid.New(), newFolio)
			if revErr != nil {
				return revErr
			}
			revID, saveErr := s.traspasos.Save(ctx, rev)
			if saveErr != nil {
				return saveErr
			}
			if err := rev.MarcarAplicado(revID); err != nil {
				return err
			}
			if err := s.traspasos.MarcarDirectoReversado(ctx, *active.DoctoInID()); err != nil {
				return err
			}
			reverso = rev
		}

		// Create a new directo when the caller supplied detalles.
		if len(p.Detalles) > 0 {
			nd, ndID, ndErr := s.crearDirecto(ctx, p)
			if ndErr != nil {
				return ndErr
			}
			newDirecto = nd
			doctoInID = ndID
		}
		return nil
	}); err != nil {
		return nil, 0, err
	}

	if isNoop {
		return noopActive, noopDoctoInID, nil
	}

	// Drain outbox events for every aggregate we created.
	if reverso != nil {
		s.drainEvents(ctx, reverso)
	}
	if newDirecto != nil {
		s.drainEvents(ctx, newDirecto)
	}
	return newDirecto, doctoInID, nil
}
