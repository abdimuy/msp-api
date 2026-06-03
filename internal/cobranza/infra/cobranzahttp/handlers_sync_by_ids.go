//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// maxByIDsLimit is the maximum number of IDs accepted per by-ids request.
// Enforced to keep URL length well below the 8 KB practical limit of most
// HTTP servers and proxies (~4 KB for 500 int IDs of up to 7 digits each).
const maxByIDsLimit = 500

// byIDsHandlers holds the repos needed for the by-ids endpoints.
// These are raw chi handlers (not Huma) because the response is a flat
// JSON array — no envelope — and the endpoints are latency-critical.
type byIDsHandlers struct {
	pagosRepo  outbound.PagosRepo
	saldosRepo outbound.SaldosRepo
	logger     *slog.Logger
}

// newByIDsHandlers constructs a byIDsHandlers.
func newByIDsHandlers(pagos outbound.PagosRepo, saldos outbound.SaldosRepo, logger *slog.Logger) *byIDsHandlers {
	return &byIDsHandlers{pagosRepo: pagos, saldosRepo: saldos, logger: logger}
}

// getPagosByIDs handles GET /v2/cobranza/sync/pagos/by-ids.
//
// Query params:
//   - zona_id (int, required) — must match the user's zona access scope.
//   - ids     (string, required) — comma-separated integer list.
//
// Returns 200 with []PagoDTO, or an apperror-shaped JSON error on failure.
// No watermark filtering is applied — the caller obtained these PKs from the
// SSE listener, which only publishes committed rows.
//
//nolint:dupl // mirrors getSaldosByIDs; diverges at perm + repo + marshal — abstraction not worth it
func (h *byIDsHandlers) getPagosByIDs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		writePlainErrorCobranza(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		writePlainErrorCobranza(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return
	}
	zonaID, ids, ok := parseByIDsRequest(w, r)
	if !ok {
		return
	}
	if len(ids) == 0 {
		writeByIDsJSON(w, marshalPagoDTOs(nil))
		return
	}
	pagos, err := h.pagosRepo.ByIDs(ctx, zonaID, ids)
	if err != nil {
		h.logger.ErrorContext(ctx, "cobranza.by_ids_pagos_failed",
			slog.Int("zona_id", zonaID), slog.Int("ids_count", len(ids)), slog.Any("error", err))
		writeAppErrorCobranza(w, err)
		return
	}
	writeByIDsJSON(w, marshalPagoDTOs(pagosToDTOs(pagos)))
}

// getSaldosByIDs handles GET /v2/cobranza/sync/saldos/by-ids.
//
// Identical contract to getPagosByIDs but returns []SaldoDTO and requires
// PermCobranzaVerSaldos. The implementation mirrors getPagosByIDs; the dupl
// linter suppression is justified because the two paths diverge at the
// permission check, repo call, marshal function, and log key — extracting
// them into a generic closure would trade concrete readability for abstraction.
//
//nolint:dupl // mirrors getPagosByIDs; diverges at perm + repo + marshal — abstraction not worth it
func (h *byIDsHandlers) getSaldosByIDs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		writePlainErrorCobranza(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return
	}
	if err := requirePerm(cu, auth.PermCobranzaVerSaldos); err != nil {
		writePlainErrorCobranza(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return
	}
	zonaID, ids, ok := parseByIDsRequest(w, r)
	if !ok {
		return
	}
	if len(ids) == 0 {
		writeByIDsJSON(w, marshalSaldoDTOs(nil))
		return
	}
	saldos, err := h.saldosRepo.ByIDs(ctx, zonaID, ids)
	if err != nil {
		h.logger.ErrorContext(ctx, "cobranza.by_ids_saldos_failed",
			slog.Int("zona_id", zonaID), slog.Int("ids_count", len(ids)), slog.Any("error", err))
		writeAppErrorCobranza(w, err)
		return
	}
	writeByIDsJSON(w, marshalSaldoDTOs(saldosToDTOs(saldos)))
}

// ─── Shared parse logic ────────────────────────────────────────────────────────

// parseByIDsRequest parses zona_id and ids from r. Returns (zonaID, ids, ok).
// On any error, the response is written and ok=false is returned.
func parseByIDsRequest(w http.ResponseWriter, r *http.Request) (int, []int, bool) {
	zonaID, ok := parseByIDsZonaID(w, r)
	if !ok {
		return 0, nil, false
	}
	ids, ok := parseByIDsParam(w, r)
	if !ok {
		return 0, nil, false
	}
	return zonaID, ids, true
}

// parseByIDsZonaID extracts and validates the zona_id query parameter.
// On error it writes the response and returns ok=false.
func parseByIDsZonaID(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := r.URL.Query().Get("zona_id")
	if raw == "" {
		ae := apperror.NewValidation("zona_id_required", "el parámetro zona_id es obligatorio")
		writePlainErrorCobranza(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
		return 0, false
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		ae := apperror.NewValidation("zona_id_invalid", "zona_id debe ser un entero positivo")
		writePlainErrorCobranza(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
		return 0, false
	}
	return id, true
}

// parseByIDsParam extracts the comma-separated ids query parameter.
// Returns the parsed IDs and ok=true. On any error writes the response and
// returns (nil, false). An absent or empty ids parameter is an error.
func parseByIDsParam(w http.ResponseWriter, r *http.Request) ([]int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("ids"))
	if raw == "" {
		ae := apperror.NewValidation("ids_invalid", "parámetro ids es obligatorio")
		writePlainErrorCobranza(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
		return nil, false
	}

	parts := strings.Split(raw, ",")
	if len(parts) > maxByIDsLimit {
		ae := apperror.NewValidation("ids_too_many", "máximo 500 ids por request")
		writePlainErrorCobranza(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
		return nil, false
	}

	ids := make([]int, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			ae := apperror.NewValidation("ids_invalid", "ids contiene un valor no numérico")
			writePlainErrorCobranza(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
			return nil, false
		}
		ids = append(ids, n)
	}
	return ids, true
}

// ─── DTO projection helpers ────────────────────────────────────────────────────

func pagosToDTOs(pagos []domain.Pago) []PagoDTO {
	dtos := make([]PagoDTO, len(pagos))
	for i, p := range pagos {
		dtos[i] = toPagoDTO(p)
	}
	return dtos
}

func saldosToDTOs(saldos []domain.Saldo) []SaldoDTO {
	dtos := make([]SaldoDTO, len(saldos))
	for i, s := range saldos {
		dtos[i] = toSaldoDTO(s)
	}
	return dtos
}

// ─── Response helpers ──────────────────────────────────────────────────────────

// writeByIDsJSON writes pre-marshalled JSON bytes with status 200.
// The Content-Type matches the rest of the cobranza module.
func writeByIDsJSON(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

// marshalPagoDTOs serialises a []PagoDTO to JSON bytes.
// Separated from writeByIDsJSON so the concrete type is visible to errchkjson.
func marshalPagoDTOs(dtos []PagoDTO) []byte {
	if dtos == nil {
		dtos = []PagoDTO{}
	}
	b, err := json.Marshal(dtos)
	if err != nil {
		// PagoDTO contains only plain types; this branch is unreachable in
		// practice. Return [] as a safe degradation so the caller can proceed.
		slog.Warn("cobranza.by_ids_marshal_pago_failed", "error", err)
		return []byte("[]")
	}
	return b
}

// marshalSaldoDTOs serialises a []SaldoDTO to JSON bytes.
func marshalSaldoDTOs(dtos []SaldoDTO) []byte {
	if dtos == nil {
		dtos = []SaldoDTO{}
	}
	b, err := json.Marshal(dtos)
	if err != nil {
		slog.Warn("cobranza.by_ids_marshal_saldo_failed", "error", err)
		return []byte("[]")
	}
	return b
}

// mountByIDs registers the two by-ids endpoints onto r.
// Both are raw chi routes (not Huma) — their response is a flat JSON array
// with no schema envelope, and they need zona_id as a query parameter rather
// than a path segment so batching multiple IDs stays in the URL.
func mountByIDs(r chi.Router, h *byIDsHandlers) {
	r.Get("/sync/pagos/by-ids", h.getPagosByIDs)
	r.Get("/sync/saldos/by-ids", h.getSaldosByIDs)
}
