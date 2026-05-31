//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"

	"github.com/abdimuy/msp-api/internal/auth"
)

// SyncVentasPorZona handles GET /cobranza/sync/ventas/zona/{zona_id}.
//
// Devuelve un page de ventas enriquecidas (saldo + cliente + dirección +
// contrato) modificadas desde el cursor server_ts. Mismo cursor que
// /sync/saldos/zona (MSP_SALDOS_VENTAS.UPDATED_AT); el join agrega los
// campos estáticos que necesita el cobrador en el móvil.
func (h *Handlers) SyncVentasPorZona(ctx context.Context, in *SyncVentasInput) (*SyncVentasOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	cursor, err := parseCursor(in.Cursor)
	if err != nil {
		return nil, mapAppError(err)
	}
	page, err := h.svc.SyncVentasPorZona(ctx, in.ZonaID, cursor, in.AfterID, in.Limit)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &SyncVentasOutput{Body: toSyncVentasBody(page)}, nil
}

// ─── Compile-time handler signature check ────────────────────────────────────

var _ func(context.Context, *SyncVentasInput) (*SyncVentasOutput, error) = (*Handlers)(nil).SyncVentasPorZona
