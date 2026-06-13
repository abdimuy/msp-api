//nolint:misspell // domain vocabulary is Spanish (traspaso, reverso, etc.) per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// errMultiplesTraspasosdirectos is returned when ListByVentaID returns more
// than one active (non-reverso, non-reversado) directo traspaso, making
// automatic reversal ambiguous.
var errMultiplesTraspasosdirectos = apperror.NewInternal(
	"multiple_traspasos_directos",
	"la venta tiene múltiples traspasos directos, no se puede reversar automáticamente",
)

// activeDirect returns the single active directo traspaso linked to ventaID.
// An active directo is one where TipoReverso()==false AND Reversado()==false.
//
//   - 0 matches → domain.ErrTraspasoNoEncontrado
//   - 1 match   → returns it
//   - >1 matches → errMultiplesTraspasosdirectos
func (s *Service) activeDirect(ctx context.Context, ventaID uuid.UUID) (*domain.Traspaso, error) {
	all, err := s.traspasos.ListByVentaID(ctx, ventaID)
	if err != nil {
		return nil, err
	}
	var directs []*domain.Traspaso
	for _, tr := range all {
		if !tr.TipoReverso() && !tr.Reversado() {
			directs = append(directs, tr)
		}
	}
	switch len(directs) {
	case 0:
		return nil, domain.ErrTraspasoNoEncontrado
	case 1:
		return directs[0], nil
	default:
		return nil, errMultiplesTraspasosdirectos
	}
}

// CrearTraspasoReverso creates a reversal traspaso for the single active
// directo traspaso linked to ventaID. Fails with ErrTraspasoNoEncontrado when
// none exists, or with errMultiplesTraspasosdirectos when more than one active
// directo exists. Already-reversed directos are ignored — only truly active
// (non-reversado) directos count.
func (s *Service) CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (*domain.Traspaso, int, error) {
	original, err := s.activeDirect(ctx, ventaID)
	if err != nil {
		return nil, 0, err
	}

	// Mint a fresh folio for the reversal outside the tx (minting is
	// idempotent-safe and should not be inside the write tx).
	newFolio, err := s.folioMinter.MintFolio(ctx)
	if err != nil {
		return nil, 0, err
	}

	// Guard: the original must have a DoctoInID (it was persisted).
	if original.DoctoInID() == nil {
		return nil, 0, apperror.NewInternal(
			"traspaso_directo_sin_docto_in_id",
			"el traspaso directo no tiene un id de microsip asignado",
		)
	}

	reversed, err := original.Reversar(s.clock.Now(), by, uuid.New(), newFolio)
	if err != nil {
		return nil, 0, err
	}

	var doctoInID int
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		id, saveErr := s.traspasos.Save(ctx, reversed)
		if saveErr != nil {
			return saveErr
		}
		doctoInID = id
		if err := reversed.MarcarAplicado(id); err != nil {
			return err
		}
		return s.traspasos.MarcarDirectoReversado(ctx, *original.DoctoInID())
	}); err != nil {
		return nil, 0, err
	}

	s.drainEvents(ctx, reversed)
	return reversed, doctoInID, nil
}
