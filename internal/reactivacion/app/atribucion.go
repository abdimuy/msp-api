//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package app

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// AtribucionResult carries the treatment-vs-control measurement summary returned
// by [Service.Atribucion].
type AtribucionResult struct {
	// TreatmentTotal is the number of contacted clientes (FUE_CONTACTADO = 1).
	TreatmentTotal int
	// TreatmentConvertidos is the number of contacted clientes who converted
	// (made a purchase after their cohort date).
	TreatmentConvertidos int
	// ControlTotal is the number of control clientes (EN_CONTROL = 1).
	ControlTotal int
	// ControlConvertidos is the number of control clientes who converted.
	ControlConvertidos int
	// TasaTreatment is the conversion rate of the treatment group [0, 1].
	TasaTreatment decimal.Decimal
	// TasaControl is the conversion rate of the control group [0, 1].
	TasaControl decimal.Decimal
	// Uplift is TasaTreatment - TasaControl. May be negative.
	Uplift decimal.Decimal
}

// Atribucion measures the incremental impact of the reactivación campaign by
// comparing the conversion rate of contacted clientes (treatment) against the
// control group.
//
// A cliente is considered converted ("enganchó") when its last purchase is
// strictly after its cohort date — i.e. it bought again after entering the
// cohort.
//
// Group assignment:
//   - control    = EN_CONTROL = 1
//   - treatment  = FUE_CONTACTADO = 1
//
// Treatment clientes that were never contacted (EN_CONTROL = 0 AND
// FUE_CONTACTADO = 0) are EXCLUDED from both denominators — the piloto compares
// contactados vs control, not the full tratable universe. In Fase 1 no cliente
// is contacted yet, so TreatmentTotal is 0 and the tasas are 0.
func (s *Service) Atribucion(ctx context.Context) (AtribucionResult, error) {
	const source = "reactivacion.Atribucion"

	cohorte, err := s.repo.ListarCohorte(ctx, outbound.ListarCohorteParams{
		Segmento:        "",
		SoloTratamiento: false, // must include both groups
	})
	if err != nil {
		return AtribucionResult{}, apperror.NewInternal("atribucion_list_failed", "error al listar la cohorte para atribución").
			WithSource(source).WithError(err)
	}

	var (
		treatmentTotal, treatmentConv int
		controlTotal, controlConv     int
	)

	for _, c := range cohorte {
		convirtio := !c.FechaUltimaCompraBase().IsZero() && c.FechaUltimaCompraBase().After(c.CohorteFecha())

		switch {
		case c.EnControl():
			controlTotal++
			if convirtio {
				controlConv++
			}
		case c.FueContactado():
			treatmentTotal++
			if convirtio {
				treatmentConv++
			}
		default:
			// Treatment cliente not (yet) contacted — excluded from attribution.
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

// safeDivide computes conv/total as a decimal. Returns zero when total is 0 to
// guard against divide-by-zero.
func safeDivide(conv, total int) decimal.Decimal {
	if total == 0 {
		return decimal.Zero
	}
	return decimal.NewFromInt(int64(conv)).Div(decimal.NewFromInt(int64(total)))
}
