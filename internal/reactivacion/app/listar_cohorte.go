//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ListarCohorteParams groups the input parameters for [Service.ListarCohorte].
type ListarCohorteParams struct {
	// Segmento restricts results to one segmento. Empty string = no filter.
	// A non-empty value that is not a canonical segmento yields a validation error.
	Segmento string

	// SoloTratamiento returns only the treatment group (EN_CONTROL = 0) when true.
	SoloTratamiento bool
}

// ListarCohorte returns the cohorte rows matching p.
func (s *Service) ListarCohorte(ctx context.Context, p ListarCohorteParams) ([]*domain.CohorteCliente, error) {
	const source = "reactivacion.ListarCohorte"

	var seg domain.Segmento
	if p.Segmento != "" {
		parsed, err := domain.ParseSegmento(p.Segmento)
		if err != nil {
			return nil, err
		}
		seg = parsed
	}

	cohorte, err := s.repo.ListarCohorte(ctx, outbound.ListarCohorteParams{
		Segmento:        seg,
		SoloTratamiento: p.SoloTratamiento,
	})
	if err != nil {
		return nil, apperror.NewInternal("cohorte_list_failed", "error al listar la cohorte de reactivación").
			WithSource(source).WithError(err)
	}
	return cohorte, nil
}
