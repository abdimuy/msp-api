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

// rateScale is the number of decimal places enforced for rate/ratio fields
// (values in [0, 1] such as TasaTreatment, TasaControl, Uplift). Four decimal
// places preserve meaningful precision for small uplifts (e.g. 0.0050 instead
// of 0.00).
const rateScale int32 = 4

// refresh estado constants used in RefreshOutput.Body.
const (
	estadoIniciado     = "iniciado"
	estadoYaEnProgreso = "ya_en_progreso"
)

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
		dtos = append(dtos, toWinbackItemDTO(item))
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
	fillAttributionOutput(out, result)
	return out, nil
}

// RefrescarCandidatos handles POST /winback/refresh.
//
// The refresh is dispatched as a background goroutine so the endpoint returns
// immediately with HTTP 202 Accepted. The actual work (reading Microsip anclas
// + upserting candidatos) takes ~50s for a full rebuild and would exceed the
// HTTP write timeout if run synchronously.
//
// A single-flight guard in the app layer prevents duplicate concurrent runs: if
// a refresh is already in progress, the response body carries estado =
// "ya_en_progreso" but the status code is still 202 (the request was accepted;
// the desired end-state — a fresh projection — is already being worked on).
func (h *Handlers) RefrescarCandidatos(ctx context.Context, input *RefreshInput) (*RefreshOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsRefresh); err != nil {
		return nil, err
	}

	out := &RefreshOutput{}
	//nolint:contextcheck // intentional: RefrescarEnSegundoPlano uses context.Background() internally
	// so that the background goroutine is not cancelled by the HTTP request context.
	if h.svc.RefrescarEnSegundoPlano(input.Body.Full) {
		out.Body.Estado = estadoIniciado
		out.Body.Mensaje = "refresco iniciado en segundo plano"
	} else {
		out.Body.Estado = estadoYaEnProgreso
		out.Body.Mensaje = "ya hay un refresco en progreso"
	}
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
