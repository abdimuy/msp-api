//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// reconcileVentasPageSize is the page size used by ReconciliarVentas' cursor
// loop over VentaRepo.List. Large enough to keep the number of round-trips
// low for a full-catalog reconcile while staying well under the ventsearch
// upsert batch size (10_000 — see ventsearch.upsertBatchSize).
const reconcileVentasPageSize = 1000

// ReconciliarVentas materializes every venta — including canceled ones —
// into the Meilisearch index. Called by VentasReconcileWorker (warm-up +
// periodic drift recovery) and by the manual refresh endpoint
// (POST /ventas/_search/refresh). No-op when no index is wired
// (s.searchIndex == nil).
//
// Canceled ventas MUST be reconciled too: cancellation is modeled as a
// soft-delete upsert (situacion="cancelada"), never a hard delete from the
// index, so a search with incluir_canceladas=true keeps finding them.
//
// Returns the total number of documents pushed to the index.
func (s *Service) ReconciliarVentas(ctx context.Context) (int, error) {
	if s.searchIndex == nil {
		return 0, nil
	}

	total := 0
	cursor := ""
	for {
		page, err := s.ventas.List(ctx,
			outbound.ListParams{Cursor: cursor, PageSize: reconcileVentasPageSize},
			outbound.ListVentasFilters{IncluirCanceladas: true},
		)
		if err != nil {
			return total, err
		}

		if len(page.Items) > 0 {
			docs := make([]outbound.VentaSearchDoc, len(page.Items))
			for i, v := range page.Items {
				docs[i] = VentaToSearchDoc(v)
			}
			if err := s.searchIndex.Reconciliar(ctx, docs); err != nil {
				return total, err
			}
			total += len(docs)
		}

		if page.NextCursor == "" {
			return total, nil
		}
		cursor = page.NextCursor
	}
}
