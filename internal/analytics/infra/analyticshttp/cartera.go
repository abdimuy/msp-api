// Package analyticshttp — cartera.go contains the HTTP handlers for the cartera
// dashboard: salud, aging, cosechas, cobradores, cuentas-riesgo, roll-rate.
// All handlers require PermAnalyticsCarteraRead ("analytics:cartera_ver").
//
//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/auth"
)

// parsePeriodo parses a "YYYY-MM" period string into the [desde, hasta) UTC
// range used for CEI window calculations:
//
//	desde = first day of the given month, 00:00:00 UTC.
//	hasta = first day of the following month, 00:00:00 UTC (exclusive end).
//
// Returns zero times on empty input — the service then applies its default
// 30-day lookback window. Returns a 422 huma error on invalid format.
func parsePeriodo(s string) (time.Time, time.Time, error) {
	t, err := time.Parse("2006-01", s)
	if err != nil {
		return time.Time{}, time.Time{},
			huma.NewError(http.StatusUnprocessableEntity,
				fmt.Sprintf("periodo debe ser YYYY-MM, se recibió %q", s))
	}
	desde := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return desde, desde.AddDate(0, 1, 0), nil
}

// parseCarteraParams builds a CarteraParams from the cartera query input.
// Returns a 422 huma error if zona/cobrador are non-empty but not positive
// integers, or if periodo is malformed.
func parseCarteraParams(input *CarteraQueryInput) (analyticsapp.CarteraParams, error) {
	p := analyticsapp.CarteraParams{}

	if input.Zona != "" {
		id, err := strconv.Atoi(input.Zona)
		if err != nil || id <= 0 {
			return analyticsapp.CarteraParams{}, huma.NewError(http.StatusUnprocessableEntity,
				fmt.Sprintf("zona debe ser un ID numérico positivo, se recibió %q", input.Zona))
		}
		p.ZonaClienteID = &id
	}

	if input.Cobrador != "" {
		id, err := strconv.Atoi(input.Cobrador)
		if err != nil || id <= 0 {
			return analyticsapp.CarteraParams{}, huma.NewError(http.StatusUnprocessableEntity,
				fmt.Sprintf("cobrador debe ser un ID numérico positivo, se recibió %q", input.Cobrador))
		}
		p.CobradorID = &id
	}

	if input.Periodo != "" {
		desde, hasta, err := parsePeriodo(input.Periodo)
		if err != nil {
			return analyticsapp.CarteraParams{}, err
		}
		p.Desde = desde
		p.Hasta = hasta
	}
	return p, nil
}

// SaludCartera handles GET /cartera/salud.
func (h *Handlers) SaludCartera(ctx context.Context, input *CarteraQueryInput) (*SaludCarteraOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsCarteraRead); err != nil {
		return nil, err
	}
	p, err := parseCarteraParams(input)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.ObtenerSaludCartera(ctx, p)
	if err != nil {
		return nil, mapAppError(err)
	}
	out := &SaludCarteraOutput{}
	out.Body = toSaludCarteraDTO(result)
	return out, nil
}

// AgingCartera handles GET /cartera/aging.
func (h *Handlers) AgingCartera(ctx context.Context, input *CarteraQueryInput) (*AgingOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsCarteraRead); err != nil {
		return nil, err
	}
	p, err := parseCarteraParams(input)
	if err != nil {
		return nil, err
	}
	buckets, err := h.svc.ObtenerAging(ctx, p)
	if err != nil {
		return nil, mapAppError(err)
	}
	dtos := make([]AgingBucketDTO, 0, len(buckets))
	for _, b := range buckets {
		dtos = append(dtos, toAgingBucketDTO(b))
	}
	out := &AgingOutput{}
	out.Body.Items = dtos
	return out, nil
}

// CosechasCartera handles GET /cartera/cosechas.
func (h *Handlers) CosechasCartera(ctx context.Context, input *CarteraQueryInput) (*CosechasOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsCarteraRead); err != nil {
		return nil, err
	}
	p, err := parseCarteraParams(input)
	if err != nil {
		return nil, err
	}
	cosechas, err := h.svc.ObtenerCosechas(ctx, p)
	if err != nil {
		return nil, mapAppError(err)
	}
	dtos := make([]CosechaDTO, 0, len(cosechas))
	for _, c := range cosechas {
		dtos = append(dtos, toCosechaDTO(c))
	}
	out := &CosechasOutput{}
	out.Body.Items = dtos
	return out, nil
}

// CobradorRanking handles GET /cartera/cobradores.
func (h *Handlers) CobradorRanking(ctx context.Context, input *CarteraQueryInput) (*CobradorRankingOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsCarteraRead); err != nil {
		return nil, err
	}
	p, err := parseCarteraParams(input)
	if err != nil {
		return nil, err
	}
	ranking, err := h.svc.ObtenerRankingCobradores(ctx, p)
	if err != nil {
		return nil, mapAppError(err)
	}
	dtos := make([]CobradorPerformanceDTO, 0, len(ranking))
	for _, r := range ranking {
		dtos = append(dtos, toCobradorPerformanceDTO(r))
	}
	out := &CobradorRankingOutput{}
	out.Body.Items = dtos
	return out, nil
}

// CuentasRiesgo handles GET /cartera/cuentas-riesgo.
// Both zona and cobrador params are validated (zona as a positive int ID, cobrador
// as a positive int ID) but neither is forwarded to ListarCuentasRiesgo in v1:
//   - zona: ListarCuentasRiesgo filters by zone name string (WinbackCandidato.Zona()),
//     not by numeric zone ID. No ID→name lookup exists in v1; the endpoint returns
//     the whole portfolio and the client filters locally.
//   - cobrador: ListarCuentasRiesgo operates at portfolio level and has no cobrador
//     filter; the candidatos read-model does not carry a cobrador ID per account.
func (h *Handlers) CuentasRiesgo(ctx context.Context, input *CarteraQueryInput) (*CuentasRiesgoOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsCarteraRead); err != nil {
		return nil, err
	}
	// Validate periodo format even though CuentasRiesgo ignores CEI dates.
	if _, err := parseCarteraParams(input); err != nil {
		return nil, err
	}
	result, err := h.svc.ListarCuentasRiesgo(ctx, analyticsapp.CarteraParams{})
	if err != nil {
		return nil, mapAppError(err)
	}
	dtos := make([]CuentaRiesgoDTO, 0, len(result))
	for _, c := range result {
		dtos = append(dtos, toCuentaRiesgoDTO(c))
	}
	out := &CuentasRiesgoOutput{}
	out.Body.Items = dtos
	return out, nil
}

// RollRate handles GET /cartera/roll-rate.
// zona/cobrador/periodo query params are accepted for interface consistency
// but not forwarded to ObtenerRollRate, which is always portfolio-level
// (compares snapshot cuts across all zones).
func (h *Handlers) RollRate(ctx context.Context, input *CarteraQueryInput) (*RollRateOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermAnalyticsCarteraRead); err != nil {
		return nil, err
	}
	// Validate periodo format (no-op if empty; roll-rate ignores the dates).
	if _, err := parseCarteraParams(input); err != nil {
		return nil, err
	}
	result, err := h.svc.ObtenerRollRate(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}
	out := &RollRateOutput{}
	out.Body = toRollRateDTO(result)
	return out, nil
}
