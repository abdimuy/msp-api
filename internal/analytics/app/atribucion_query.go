//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// AtribucionParams groups the input parameters for [Service.Atribucion].
type AtribucionParams struct {
	// Zona restricts the candidate set to a specific sales zone.
	// Empty string means no zone filter (all zones).
	Zona string
}

// AtribucionResult carries the A/B measurement summary returned by
// [Service.Atribucion].
type AtribucionResult struct {
	// TreatmentTotal is the total number of treatment-group candidates.
	TreatmentTotal int
	// TreatmentConvertidos is the number of treatment candidates who converted
	// (i.e. made a purchase after their cohort date).
	TreatmentConvertidos int
	// ControlTotal is the total number of control-group candidates.
	ControlTotal int
	// ControlConvertidos is the number of control candidates who converted.
	ControlConvertidos int
	// TasaTreatment is the conversion rate for the treatment group [0, 1].
	TasaTreatment decimal.Decimal
	// TasaControl is the conversion rate for the control group [0, 1].
	TasaControl decimal.Decimal
	// Uplift is TasaTreatment - TasaControl. May be negative.
	Uplift decimal.Decimal
}

// Atribucion measures the incremental impact of the winback campaign by
// comparing conversion rates between the treatment and control groups.
//
// A candidate is considered converted ("convirtió") when it has a
// FechaUltimaCompra strictly after its CohorteFecha — i.e. it made at least
// one purchase after it entered the cohort.
//
// Both groups (treatment and control) are included in the read so the
// denominator of each tasa is correct; control exclusion must NOT be applied
// here.
func (s *Service) Atribucion(ctx context.Context, p AtribucionParams) (AtribucionResult, error) {
	const source = "analytics.Atribucion"

	page, err := s.repo.ListCandidatos(ctx, outbound.ListWinbackParams{
		Zona:           p.Zona,
		ExcluirControl: false, // must include both groups
		Limit:          0,
	})
	if err != nil {
		return AtribucionResult{}, apperror.NewInternal("atribucion_list_failed", "error al listar candidatos para atribución").
			WithSource(source).WithError(err)
	}

	var (
		treatmentTotal, treatmentConv int
		controlTotal, controlConv     int
	)

	for _, c := range page.Items {
		// Converted: has a purchase dated after cohort entry.
		convirtio := !c.FechaUltimaCompra().IsZero() && c.FechaUltimaCompra().After(c.CohorteFecha())

		if c.EnControl() {
			controlTotal++
			if convirtio {
				controlConv++
			}
		} else {
			treatmentTotal++
			if convirtio {
				treatmentConv++
			}
		}
	}

	tasaTreatment := safeDivide(treatmentConv, treatmentTotal)
	tasaControl := safeDivide(controlConv, controlTotal)

	return AtribucionResult{
		TreatmentTotal:       treatmentTotal,
		TreatmentConvertidos: treatmentConv,
		ControlTotal:         controlTotal,
		ControlConvertidos:   controlConv,
		TasaTreatment:        tasaTreatment,
		TasaControl:          tasaControl,
		Uplift:               tasaTreatment.Sub(tasaControl),
	}, nil
}

// safeDivide computes conv/total as a decimal. Returns zero when total is 0
// to guard against divide-by-zero.
func safeDivide(conv, total int) decimal.Decimal {
	if total == 0 {
		return decimal.Zero
	}
	return decimal.NewFromInt(int64(conv)).Div(decimal.NewFromInt(int64(total)))
}
