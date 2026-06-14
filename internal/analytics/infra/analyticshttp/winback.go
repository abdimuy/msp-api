//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	"context"
	"time"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/auth"
)

// moneyScale is the number of decimal places enforced for every monetary
// field in the analytics response.
const moneyScale int32 = 2

// Handlers holds the analytics HTTP handlers wired against the app service.
type Handlers struct {
	svc *analyticsapp.Service
}

// NewHandlers builds a Handlers wired against svc.
func NewHandlers(svc *analyticsapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// ListarWinback handles GET /winback.
func (h *Handlers) ListarWinback(ctx context.Context, input *ListWinbackInput) (*ListWinbackOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsWinbackRead); err != nil {
		return nil, err
	}

	items, err := h.svc.ListarWinback(ctx, analyticsapp.ListarWinbackParams{
		Segmento:       input.Segmento,
		Zona:           input.Zona,
		Limit:          input.Limit,
		IncluirControl: input.IncluirControl,
	})
	if err != nil {
		return nil, mapAppError(err)
	}

	dtos := make([]WinbackItemDTO, 0, len(items))
	for _, item := range items {
		c := item.Candidato
		dtos = append(dtos, WinbackItemDTO{
			ClienteID:         c.ClienteID(),
			Nombre:            c.Nombre(),
			Zona:              c.Zona(),
			Telefono:          c.Telefono(),
			FechaUltimaCompra: formatTime(c.FechaUltimaCompra()),
			RecenciaDias:      item.RecenciaDias,
			Frecuencia:        c.Frecuencia(),
			Monetary:          c.Monetary().StringFixed(moneyScale),
			Saldo:             c.Saldo().StringFixed(moneyScale),
			PorLiquidarPct:    c.PorLiquidarPct().StringFixed(moneyScale),
			NextBestProduct:   c.NextBestProduct(),
			Segmento:          item.Segmento.String(),
			Score:             item.Score.Int(),
			EnControl:         c.EnControl(),
		})
	}

	out := &ListWinbackOutput{}
	out.Body.Items = dtos
	return out, nil
}

// Atribucion handles GET /winback/attribution.
func (h *Handlers) Atribucion(ctx context.Context, input *AttributionInput) (*AttributionOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsWinbackRead); err != nil {
		return nil, err
	}

	result, err := h.svc.Atribucion(ctx, analyticsapp.AtribucionParams{
		Zona: input.Zona,
	})
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &AttributionOutput{}
	out.Body.TreatmentTotal = result.TreatmentTotal
	out.Body.TreatmentConvertidos = result.TreatmentConvertidos
	out.Body.ControlTotal = result.ControlTotal
	out.Body.ControlConvertidos = result.ControlConvertidos
	out.Body.TasaTreatment = result.TasaTreatment.StringFixed(moneyScale)
	out.Body.TasaControl = result.TasaControl.StringFixed(moneyScale)
	out.Body.Uplift = result.Uplift.StringFixed(moneyScale)
	return out, nil
}

// RefrescarCandidatos handles POST /winback/refresh.
func (h *Handlers) RefrescarCandidatos(ctx context.Context, input *RefreshInput) (*RefreshOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsRefresh); err != nil {
		return nil, err
	}

	result, err := h.svc.RefrescarCandidatos(ctx, input.Body.Full)
	if err != nil {
		return nil, mapAppError(err)
	}

	out := &RefreshOutput{}
	out.Body.Procesados = result.Procesados
	out.Body.Watermark = formatTime(result.Watermark)
	return out, nil
}

// formatTime renders a timestamp as RFC3339Nano in UTC. Zero values map to
// the empty string so optional fields remain clear in the JSON response.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
