//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// PagosPorVenta handles GET /cobranza/pagos/venta/{docto_cc_id}.
func (h *Handlers) PagosPorVenta(ctx context.Context, in *PagosPorVentaInput) (*PagosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	pagos, err := h.svc.PagosPorVenta(ctx, in.DoctoCCID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &PagosOutput{Body: mapPagos(pagos)}, nil
}

// PagosPorCliente handles GET /cobranza/pagos/cliente/{cliente_id}.
func (h *Handlers) PagosPorCliente(ctx context.Context, in *PagosPorClienteInput) (*PagosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	pagos, err := h.svc.PagosPorCliente(ctx, in.ClienteID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &PagosOutput{Body: mapPagos(pagos)}, nil
}

// PagosPorZona handles GET /cobranza/pagos/zona/{zona_id}.
func (h *Handlers) PagosPorZona(ctx context.Context, in *PagosPorZonaInput) (*PagosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	desde, err := parseOptionalDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	pagos, err := h.svc.PagosEnRutaPorZona(ctx, in.ZonaID, desde, optionalVentanaDias(in.VentanaDias))
	if err != nil {
		return nil, mapAppError(err)
	}
	return &PagosOutput{Body: mapPagos(pagos)}, nil
}

// SyncSaldosPorZona handles GET /cobranza/sync/saldos/zona/{zona_id}.
func (h *Handlers) SyncSaldosPorZona(ctx context.Context, in *SyncSaldosInput) (*SyncSaldosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	cursor, err := parseCursor(in.Cursor)
	if err != nil {
		return nil, mapAppError(err)
	}
	page, err := h.svc.SyncSaldosPorZona(ctx, in.ZonaID, cursor, in.AfterID, in.Limit)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &SyncSaldosOutput{Body: toSyncSaldosBody(page)}, nil
}

// SyncPagosPorZona handles GET /cobranza/sync/pagos/zona/{zona_id}.
func (h *Handlers) SyncPagosPorZona(ctx context.Context, in *SyncPagosInput) (*SyncPagosOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	cursor, err := parseCursor(in.Cursor)
	if err != nil {
		return nil, mapAppError(err)
	}
	desde, err := parseOptionalDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	page, err := h.svc.SyncPagosPorZona(ctx, in.ZonaID, cursor, in.AfterID, in.Limit, desde)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &SyncPagosOutput{Body: toSyncPagosBody(page)}, nil
}

// parseCursor parses the sync cursor — RFC3339 timestamp. Empty input returns
// the zero time (treated as "from the beginning" by the repo).
func parseCursor(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, domain.ErrCursorInvalido
}

// mapPagos projects each domain.Pago into its DTO.
func mapPagos(pagos []domain.Pago) []PagoDTO {
	out := make([]PagoDTO, 0, len(pagos))
	for _, p := range pagos {
		out = append(out, toPagoDTO(p))
	}
	return out
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var (
	_ func(context.Context, *PagosPorVentaInput) (*PagosOutput, error)   = (*Handlers)(nil).PagosPorVenta
	_ func(context.Context, *PagosPorClienteInput) (*PagosOutput, error) = (*Handlers)(nil).PagosPorCliente
	_ func(context.Context, *PagosPorZonaInput) (*PagosOutput, error)    = (*Handlers)(nil).PagosPorZona
	_ func(context.Context, *SyncSaldosInput) (*SyncSaldosOutput, error) = (*Handlers)(nil).SyncSaldosPorZona
	_ func(context.Context, *SyncPagosInput) (*SyncPagosOutput, error)   = (*Handlers)(nil).SyncPagosPorZona
)
