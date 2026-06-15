//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"sort"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// WinbackListItem is the enriched result type returned by ListarWinback.
// It carries the raw candidato alongside the values computed at read time
// (segmento, score, recency) so callers never have to re-derive them.
type WinbackListItem struct {
	Candidato    *domain.WinbackCandidato
	Segmento     domain.Segmento
	Score        domain.ScoreWinback
	RecenciaDias int
	// EstadoPago is the payment-solvency classification computed at read time
	// from the candidato's saldo and FechaUltimoPago.
	EstadoPago domain.EstadoPago
}

// ListarWinbackParams groups the filtering options for [Service.ListarWinback].
type ListarWinbackParams struct {
	// Segmento restricts results to a specific RFM segment.
	// Empty string means no segment filter.
	// If non-empty it must be a valid domain.Segmento value.
	Segmento string

	// Zona restricts results to a specific sales zone.
	// Empty string means no zone filter.
	Zona string

	// Limit caps the final sorted list. 0 means no cap.
	Limit int

	// IncluirControl, when true, includes control-group candidates in the
	// results. When false (default), control candidates are excluded.
	IncluirControl bool
}

// ListarWinback returns winback candidates scored and sorted by score
// descending. Segmento filtering and sorting happen AFTER scoring in the app
// layer (not the repo) because both are computed fields.
//
// Flow:
//  1. Fetch all candidates for the given zone from the repo (Limit=0 so the
//     repo returns everything matching the zone; we never truncate before scoring).
//  2. Compute segmento+score for each via computeSegmentoScore.
//  3. Filter by segmento if p.Segmento is set.
//  4. Sort by Score DESC; tie-break by Monetary DESC, then ClienteID ASC.
//  5. Truncate to p.Limit when > 0.
func (s *Service) ListarWinback(ctx context.Context, p ListarWinbackParams) ([]WinbackListItem, error) {
	const source = "analytics.ListarWinback"

	// Validate segmento filter early so we return a typed error before any I/O.
	var segFilter domain.Segmento
	if p.Segmento != "" {
		seg, err := domain.ParseSegmento(p.Segmento)
		if err != nil {
			appErr, ok := apperror.As(err)
			if ok {
				return nil, appErr.WithSource(source)
			}
			return nil, apperror.NewValidation("segmento_invalido", "el segmento no es válido").
				WithSource(source).WithError(err)
		}
		segFilter = seg
	}

	// Fetch all candidates; Limit=0 so the repo returns everything matching
	// the zone — we must sort by score before any truncation.
	page, err := s.repo.ListCandidatos(ctx, outbound.ListWinbackParams{
		Zona:           p.Zona,
		ExcluirControl: !p.IncluirControl,
		Limit:          0,
	})
	if err != nil {
		return nil, apperror.NewInternal("winback_list_failed", "error al listar candidatos winback").
			WithSource(source).WithError(err)
	}

	now := s.clock.Now()

	// Score and optionally filter.
	items := make([]WinbackListItem, 0, len(page.Items))
	for _, c := range page.Items {
		seg, score, recencia := computeSegmentoScore(c, now)
		if segFilter != "" && seg != segFilter {
			continue
		}
		ep := estadoPagoFor(c.Saldo(), c.FechaUltimoPago(), now)
		items = append(items, WinbackListItem{
			Candidato:    c,
			Segmento:     seg,
			Score:        score,
			RecenciaDias: recencia,
			EstadoPago:   ep,
		})
	}

	// Sort: score DESC, monetary DESC, clienteID ASC (deterministic).
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score.Int() != items[j].Score.Int() {
			return items[i].Score.Int() > items[j].Score.Int()
		}
		cmpM := items[i].Candidato.Monetary().Cmp(items[j].Candidato.Monetary())
		if cmpM != 0 {
			return cmpM > 0
		}
		return items[i].Candidato.ClienteID() < items[j].Candidato.ClienteID()
	})

	// Truncate after sorting.
	if p.Limit > 0 && len(items) > p.Limit {
		items = items[:p.Limit]
	}

	return items, nil
}
