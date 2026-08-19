//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"context"
	"strconv"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── Input DTOs ───────────────────────────────────────────────────────────────

// SyncDigestInput is the path + query parameters for the digest endpoints.
// Desde, cuando viene, debe ser RFC3339 UTC y tiene que ser el MISMO que el
// cliente manda al /sync: el inventario existe para compararse contra lo que
// el sync entregó, y dos ventanas distintas hacen que el reconciliador
// declare fantasma lo que el sync acaba de mandar. Cuando falta, el servidor
// resuelve el default (app.ResolveSyncDesde) — el mismo que resuelve el sync,
// así que omitirlo en ambos lados sigue siendo consistente.
type SyncDigestInput struct {
	ZonaID int    `path:"zona_id"                                                                             doc:"ID de la zona"`
	Desde  string `query:"desde"  doc:"Ventana RFC3339 UTC; debe ser la MISMA que usa el sync. Si se omite, el servidor aplica su ventana por defecto (7 días)"`
}

// SyncListIDsInput contains the path and query params for the IDs endpoints.
type SyncListIDsInput struct {
	ZonaID int    `path:"zona_id"                                          doc:"ID de la zona"`
	After  int    `query:"after"  minimum:"0"             default:"0"       doc:"Paginación: devuelve IDs > after. Default 0 (inicio)"`
	Limit  int    `query:"limit"  minimum:"1" maximum:"10000" default:"5000" doc:"Máximo de IDs por página. Default 5000, máximo 10000"`
	Desde  string `query:"desde"  doc:"Ventana RFC3339 UTC; debe ser la MISMA que usa el sync. Si se omite, el servidor aplica su ventana por defecto (7 días)"`
}

// ─── Response DTOs ────────────────────────────────────────────────────────────

// DigestBody is the JSON body returned by both digest endpoints.
type DigestBody struct {
	CountActivos int        `json:"count_activos"            doc:"Número de filas activas en la zona"`
	IDsXor       string     `json:"ids_xor"                  doc:"XOR de todos los PKs activos como int64 (base 10)"`
	IDsSum       string     `json:"ids_sum"                  doc:"Suma de todos los PKs activos como int64 (base 10)"`
	MaxUpdatedAt *time.Time `json:"max_updated_at,omitempty" doc:"UPDATED_AT más alto del conjunto activo (RFC3339 UTC). Omitido cuando no hay filas"`
}

// DigestOutput wraps a DigestBody.
type DigestOutput struct {
	Body DigestBody
}

// ListIDsBody is the JSON body returned by both list-ids endpoints.
type ListIDsBody struct {
	IDs     []int `json:"ids"      doc:"IDs activos para la zona, ordenados ascendente"`
	HasMore bool  `json:"has_more" doc:"true si quedan más IDs (paginar con after=último_id)"`
}

// ListIDsOutput wraps a ListIDsBody.
type ListIDsOutput struct {
	Body ListIDsBody
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// SyncPagosDigest handles GET /v2/cobranza/sync/pagos/zona/{zona_id}/digest.
func (h *Handlers) SyncPagosDigest(ctx context.Context, in *SyncDigestInput) (*DigestOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	desde, err := parseReconcileDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.DigestPagosPorZona(ctx, in.ZonaID, desde)
	if err != nil {
		return nil, mapAppError(err)
	}
	return toDigestOutput(result), nil
}

// SyncPagosIDs handles GET /v2/cobranza/sync/pagos/zona/{zona_id}/ids.
func (h *Handlers) SyncPagosIDs(ctx context.Context, in *SyncListIDsInput) (*ListIDsOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	desde, err := parseReconcileDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	ids, hasMore, err := h.svc.ListIDsPagosPorZona(ctx, in.ZonaID, in.After, in.Limit, desde)
	if err != nil {
		return nil, mapAppError(err)
	}
	if ids == nil {
		ids = []int{}
	}
	return &ListIDsOutput{Body: ListIDsBody{IDs: ids, HasMore: hasMore}}, nil
}

// SyncSaldosDigest handles GET /v2/cobranza/sync/saldos/zona/{zona_id}/digest.
func (h *Handlers) SyncSaldosDigest(ctx context.Context, in *SyncDigestInput) (*DigestOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	desde, err := parseReconcileDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	result, err := h.svc.DigestSaldosPorZona(ctx, in.ZonaID, desde)
	if err != nil {
		return nil, mapAppError(err)
	}
	return toDigestOutput(result), nil
}

// SyncSaldosIDs handles GET /v2/cobranza/sync/saldos/zona/{zona_id}/ids.
func (h *Handlers) SyncSaldosIDs(ctx context.Context, in *SyncListIDsInput) (*ListIDsOutput, error) {
	if err := authorize(ctx, auth.PermCobranzaVerSaldos); err != nil {
		return nil, err
	}
	desde, err := parseReconcileDesde(in.Desde)
	if err != nil {
		return nil, err
	}
	ids, hasMore, err := h.svc.ListIDsSaldosPorZona(ctx, in.ZonaID, in.After, in.Limit, desde)
	if err != nil {
		return nil, mapAppError(err)
	}
	if ids == nil {
		ids = []int{}
	}
	return &ListIDsOutput{Body: ListIDsBody{IDs: ids, HasMore: hasMore}}, nil
}

// parseReconcileDesde parses the optional ?desde= query parameter for the
// digest/ids reconcile endpoints. Unlike parseOptionalDesde (which also
// accepts YYYY-MM-DD), these endpoints require a full RFC3339 UTC timestamp
// because the window must be deterministic across calls.
//
// Empty/missing input devuelve el zero time, que NO significa "sin ventana":
// el servicio lo pasa por app.ResolveSyncDesde y resuelve el default de
// servidor, el mismo que aplica el sync. La resolución vive ahí y no aquí
// porque el default necesita el reloj inyectado y porque así hay UNA sola
// implementación para los tres canales — dos copias en dos handlers es
// exactamente como se separaron la primera vez.
func parseReconcileDesde(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, mapAppError(domain.ErrDesdeReconcileInvalido)
}

// toDigestOutput projects an outbound.DigestResult into a DigestOutput DTO.
// Extracted to avoid repeating the MaxUpdatedAt nil-check in each handler.
func toDigestOutput(result outbound.DigestResult) *DigestOutput {
	body := DigestBody{
		CountActivos: result.CountActivos,
		IDsXor:       strconv.FormatInt(result.IDsXor, 10),
		IDsSum:       strconv.FormatInt(result.IDsSum, 10),
	}
	if !result.MaxUpdatedAt.IsZero() {
		t := result.MaxUpdatedAt.UTC()
		body.MaxUpdatedAt = &t
	}
	return &DigestOutput{Body: body}
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var (
	_ func(context.Context, *SyncDigestInput) (*DigestOutput, error)   = (*Handlers)(nil).SyncPagosDigest
	_ func(context.Context, *SyncListIDsInput) (*ListIDsOutput, error) = (*Handlers)(nil).SyncPagosIDs
	_ func(context.Context, *SyncDigestInput) (*DigestOutput, error)   = (*Handlers)(nil).SyncSaldosDigest
	_ func(context.Context, *SyncListIDsInput) (*ListIDsOutput, error) = (*Handlers)(nil).SyncSaldosIDs
)
