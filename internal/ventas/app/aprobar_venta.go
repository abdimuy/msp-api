package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// EnviarARevision transitions the venta identified by ventaID from borrador
// to revisada. The aggregate is loaded, mutated, the header row is updated,
// and the resulting domain event is best-effort enqueued onto the outbox.
func (s *Service) EnviarARevision(ctx context.Context, ventaID, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return nil, err
	}
	if err := venta.EnviarARevision(by, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.Update(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}

// Aprobar transitions the venta identified by ventaID from revisada to
// aprobada, recording the approval. The aggregate is loaded, mutated, the
// header row is updated, and the resulting domain event is best-effort
// enqueued onto the outbox.
func (s *Service) Aprobar(ctx context.Context, ventaID, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return nil, err
	}
	if err := venta.Aprobar(by, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.Update(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}

// RegresarABorrador transitions the venta identified by ventaID back to
// borrador from either revisada or aprobada, clearing any recorded
// Aprobacion so the header becomes editable again. Rejected if the venta has
// already been materialized in Microsip (sincronizacion=aplicada). The
// aggregate is loaded, mutated, the header row is updated, and the resulting
// domain event is best-effort enqueued onto the outbox.
func (s *Service) RegresarABorrador(ctx context.Context, ventaID, by uuid.UUID) (*domain.Venta, error) {
	venta, err := s.ventas.FindByID(ctx, ventaID)
	if err != nil {
		return nil, err
	}
	if err := venta.RegresarABorrador(by, s.clock.Now()); err != nil {
		return nil, err
	}
	if err := s.runInTx(ctx, func(ctx context.Context) error {
		return s.ventas.Update(ctx, venta)
	}); err != nil {
		return nil, err
	}
	s.drainEvents(ctx, venta)
	return venta, nil
}
