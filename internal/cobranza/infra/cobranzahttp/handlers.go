//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// parseDesde accepts either an RFC3339 timestamp or a YYYY-MM-DD date string
// and returns the parsed time in UTC. Empty input returns (zero, nil) — the
// caller treats that as "not supplied".
func parseDesde(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, domain.ErrDesdeInvalido
}

// Handlers groups every Huma handler for the cobranza module.
type Handlers struct {
	svc        *cobranzaapp.Service
	reconciler *cobranzaapp.Reconciler
	errorsRepo outbound.ErrorsRepo
}

// NewHandlers wires a Handlers with its application dependencies.
func NewHandlers(svc *cobranzaapp.Service, reconciler *cobranzaapp.Reconciler, errorsRepo outbound.ErrorsRepo) *Handlers {
	return &Handlers{svc: svc, reconciler: reconciler, errorsRepo: errorsRepo}
}

// PorVenta handles GET /cobranza/saldos/venta/{id}.
func (h *Handlers) PorVenta(ctx context.Context, in *PorVentaInput) (*SaldoOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	saldo, err := h.svc.PorVenta(ctx, in.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &SaldoOutput{Body: toSaldoDTO(*saldo)}, nil
}

// PorCliente handles GET /cobranza/saldos/cliente/{cliente_id}.
func (h *Handlers) PorCliente(ctx context.Context, in *PorClienteInput) (*SaldosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	saldos, err := h.svc.AbiertasPorCliente(ctx, in.ClienteID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &SaldosOutput{Body: mapSaldos(saldos)}, nil
}

// PorZona handles GET /cobranza/saldos/zona/{zona_id}.
//
// Accepts ?desde=YYYY-MM-DD (or RFC3339) for a deterministic cutoff, or
// ?ventana_dias=N for a relative window. Defaults to ventana_dias=7 when
// neither is supplied. Returns 422 cobranza_parametros_excluyentes when both
// are present.
func (h *Handlers) PorZona(ctx context.Context, in *PorZonaInput) (*SaldosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	desde, err := parseOptionalDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	saldos, err := h.svc.EnRutaPorZona(ctx, in.ZonaID, desde, optionalVentanaDias(in.VentanaDias))
	if err != nil {
		return nil, mapAppError(err)
	}
	return &SaldosOutput{Body: mapSaldos(saldos)}, nil
}

// ResumenZonas handles GET /cobranza/resumen-zonas.
func (h *Handlers) ResumenZonas(ctx context.Context, _ *ResumenZonasInput) (*ResumenZonasOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	resumenes, err := h.svc.ResumenZonas(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]ResumenZonaDTO, 0, len(resumenes))
	for _, r := range resumenes {
		items = append(items, toResumenZonaDTO(r))
	}
	return &ResumenZonasOutput{Body: items}, nil
}

// Reconcile handles POST /_admin/saldos/reconcile.
func (h *Handlers) Reconcile(ctx context.Context, _ *ReconcileInput) (*ReconcileOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaReconciliar); err != nil {
		return nil, err
	}
	report, err := h.reconciler.Run(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ReconcileOutput{Body: toReconcileReportDTO(report)}, nil
}

// Backfill handles POST /_admin/saldos/backfill.
// It re-runs the reconciler unconditionally (FixDrift always true), which
// effectively recomputes every cargo in the cache. This is the same logic as
// the migration's EXECUTE BLOCK backfill, available as an HTTP endpoint for
// re-runs after migration issues or data repairs.
func (h *Handlers) Backfill(ctx context.Context, _ *BackfillInput) (*BackfillOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaBackfill); err != nil {
		return nil, err
	}
	report, err := h.reconciler.Run(ctx)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &BackfillOutput{Body: toReconcileReportDTO(report)}, nil
}

// Errors handles GET /_admin/saldos/errors.
func (h *Handlers) Errors(ctx context.Context, in *ErrorsInput) (*ErrorsOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaReconciliar); err != nil {
		return nil, err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 100
	}
	errs, err := h.errorsRepo.Recent(ctx, limit)
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]SaldoErrorDTO, 0, len(errs))
	for _, e := range errs {
		items = append(items, toErrorDTO(e))
	}
	return &ErrorsOutput{Body: items}, nil
}

// parseOptionalDesde resolves the optional ?desde= query parameter. Empty
// input yields a nil pointer (treated by the service as "not supplied"); a
// parse error is translated through mapAppError.
func parseOptionalDesde(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil //nolint:nilnil // nil = not supplied, by handler contract.
	}
	t, err := parseDesde(raw)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &t, nil
}

// optionalVentanaDias maps the wire-level sentinel (-1 = not supplied) to the
// service's nil-pointer contract.
func optionalVentanaDias(v int) *int {
	if v < 0 {
		return nil
	}
	return &v
}

// mapSaldos projects each domain.Saldo into its DTO. Used by every list-style
// handler that returns []domain.Saldo.
func mapSaldos(saldos []domain.Saldo) []SaldoDTO {
	out := make([]SaldoDTO, 0, len(saldos))
	for _, s := range saldos {
		out = append(out, toSaldoDTO(s))
	}
	return out
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var (
	_ func(context.Context, *PorVentaInput) (*SaldoOutput, error)            = (*Handlers)(nil).PorVenta
	_ func(context.Context, *PorClienteInput) (*SaldosOutput, error)         = (*Handlers)(nil).PorCliente
	_ func(context.Context, *PorZonaInput) (*SaldosOutput, error)            = (*Handlers)(nil).PorZona
	_ func(context.Context, *ResumenZonasInput) (*ResumenZonasOutput, error) = (*Handlers)(nil).ResumenZonas
	_ func(context.Context, *ReconcileInput) (*ReconcileOutput, error)       = (*Handlers)(nil).Reconcile
	_ func(context.Context, *BackfillInput) (*BackfillOutput, error)         = (*Handlers)(nil).Backfill
	_ func(context.Context, *ErrorsInput) (*ErrorsOutput, error)             = (*Handlers)(nil).Errors
)
