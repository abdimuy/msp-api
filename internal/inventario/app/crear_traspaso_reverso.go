//nolint:misspell // domain vocabulary is Spanish (traspaso, reverso, etc.) per project convention.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// errMultiplesTraspasosdirectos is returned when ListByVentaID returns more
// than one non-reverso traspaso, making automatic reversal ambiguous.
var errMultiplesTraspasosdirectos = apperror.NewInternal(
	"multiple_traspasos_directos",
	"la venta tiene múltiples traspasos directos, no se puede reversar automáticamente",
)

// CrearTraspasoReverso creates a reversal traspaso for the single direct
// traspaso linked to ventaID. Fails with ErrTraspasoNoEncontrado when none
// exists, or with errMultiplesTraspasosdirectos when more than one exists.
func (s *Service) CrearTraspasoReverso(ctx context.Context, ventaID, by uuid.UUID) (*domain.Traspaso, int, error) {
	all, err := s.traspasos.ListByVentaID(ctx, ventaID)
	if err != nil {
		return nil, 0, err
	}

	// Find the single direct (non-reverso) traspaso.
	var directs []*domain.Traspaso
	for _, tr := range all {
		if !tr.TipoReverso() {
			directs = append(directs, tr)
		}
	}
	switch len(directs) {
	case 0:
		return nil, 0, domain.ErrTraspasoNoEncontrado
	case 1:
		// proceed below
	default:
		return nil, 0, errMultiplesTraspasosdirectos
	}

	original := directs[0]

	// Mint a fresh folio for the reversal.
	newFolio, err := s.folioMinter.MintFolio(ctx)
	if err != nil {
		return nil, 0, err
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
		return reversed.MarcarAplicado(id)
	}); err != nil {
		return nil, 0, err
	}

	s.drainEvents(ctx, reversed)
	return reversed, doctoInID, nil
}
