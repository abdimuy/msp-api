//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ReindexVenta upserts a single venta's search document into the wired
// VentaSearchIndex. No-op when no index is wired (s.searchIndex == nil) —
// dev environments without Meilisearch configured never touch the index.
//
// Called by the ventoutbox incremental reindex handler after every venta
// mutation. Idempotent and safe to invoke more than once for the same id —
// the outbox dispatcher is at-least-once. When the venta no longer exists
// in Firebird the corresponding document is purged from the index
// defensively (a soft-cancel is never the not-found path — cancellation
// keeps the row; this only fires if the row is truly gone).
func (s *Service) ReindexVenta(ctx context.Context, id uuid.UUID) error {
	if s.searchIndex == nil {
		return nil
	}
	v, err := s.ventas.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrVentaNotFound) {
			return s.searchIndex.Eliminar(ctx, id)
		}
		return err
	}
	return s.searchIndex.IndexarUno(ctx, VentaToSearchDoc(v))
}
