//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package cobranzahttp

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

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
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerSaldos); err != nil {
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
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	saldos, err := h.svc.AbiertasPorCliente(ctx, in.ClienteID)
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]SaldoDTO, 0, len(saldos))
	for _, s := range saldos {
		items = append(items, toSaldoDTO(s))
	}
	return &SaldosOutput{Body: items}, nil
}

// PorZona handles GET /cobranza/saldos/zona/{zona_id}?ventana_dias=7.
func (h *Handlers) PorZona(ctx context.Context, in *PorZonaInput) (*SaldosOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	saldos, err := h.svc.EnRutaPorZona(ctx, in.ZonaID, in.VentanaDias)
	if err != nil {
		return nil, mapAppError(err)
	}
	items := make([]SaldoDTO, 0, len(saldos))
	for _, s := range saldos {
		items = append(items, toSaldoDTO(s))
	}
	return &SaldosOutput{Body: items}, nil
}

// ResumenZonas handles GET /cobranza/resumen-zonas.
func (h *Handlers) ResumenZonas(ctx context.Context, _ *ResumenZonasInput) (*ResumenZonasOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerSaldos); err != nil {
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
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaReconciliar); err != nil {
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
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaBackfill); err != nil {
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
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaReconciliar); err != nil {
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
