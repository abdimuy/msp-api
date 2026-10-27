// GET /v2/visitas/by-ids — the visits reconciler channel.
//
// Shape mirrors internal/cobranza/infra/cobranzahttp/handlers_sync_by_ids.go
// (named cap constant, cap validated BEFORE any element is parsed, a
// distinguishable ids_too_many 422) with two deliberate departures:
//
//  1. No zona_id. Cobranza's by-ids doubles as a zone-scoped rescue channel —
//     a phone that missed rows can re-download them, scoped to its zone. This
//     endpoint answers a narrower question: "of these ids — which the phone
//     itself generated and uploaded — which do you already have?" The phone
//     never downloads a visita it did not itself create, so there is no
//     zone-scoped download to protect and nothing for zona_id to scope.
//
//  2. Huma, not raw chi. Cobranza dropped to a raw chi handler because a
//     Huma Output.Body must be a struct field, and it wanted a bare JSON
//     array with no envelope. That constraint turns out not to hold: Huma
//     supports a slice Output.Body natively for GET operations (it special-
//     cases them as "list" operations — see huma.GenerateOperationID, which
//     switches on `deref(body.Type).Kind() == reflect.Slice`), so a bare
//     array response is available without leaving Huma. Every other visitas
//     route already goes through Huma; the module has no multipart upload,
//     streaming, or custom-header need that would justify a second
//     transport mechanism inside the same package. Consistency with the
//     rest of internal/visitas won by not fighting Huma over a response
//     shape it already supports.
//
// The cap's justification is URL length, not any client-side array-parameter
// limit — see maxIDsPorRequest's doc comment for the exact arithmetic.

//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// maxIDsPorRequest is the maximum number of ids accepted per
// GET /visitas/by-ids request.
//
// The justification is URL length, not any SQLite/client-side bound
// (COBRANZA-SYNC.md's "an unbounded IN (...) is a bomb" passage is about the
// Android client's SQLite 999-parameter limit — that is a DIFFERENT
// concern, and irrelevant to this server-side cap). Each id on the wire is a
// 36-character UUID string plus a comma separator (37 bytes). At the cap:
//
//	100 * 37 = 3,700 bytes
//
// — comfortably under the practical ~8 KB request-line/header ceiling most
// HTTP servers and proxies enforce (IIS's default maxQueryStringLength is
// 2,048 characters and maxUrlLength 4,096 — msp-api's own Windows Server
// 2016 target, see the root CLAUDE.md §5 — which is the tighter of the two
// ceilings that matter here), leaving headroom for the path, the "ids="
// key, and everything else on the request line. Cobranza's own by-ids
// channel (handlers_sync_by_ids.go) computes the same ceiling for
// integer ids (~7 digits, 8 bytes each) and lands at 500 — a UUID string is
// roughly 4.6x longer than a 7-digit int, so the proportionally safe count
// here is smaller, not the same number blindly copied.
const maxIDsPorRequest = 100

// VisitasExistentesPorIDs handles GET /visitas/by-ids.
//
// Requires the same permission as the rest of the module's read/write
// surface (cobranza:ver_pagos) — a cobrador who may record and read a visita
// may also confirm which of the ones it uploaded already landed.
//
// Behaviour, settled and tested at the exact boundary:
//   - Missing or blank ids           → 422 ids_required.
//   - More than maxIDsPorRequest ids → 422 ids_too_many, checked on the raw
//     comma-split BEFORE any element is parsed as a UUID.
//   - One malformed UUID among otherwise valid ones → 422 ids_invalid; the
//     whole request is rejected, not silently narrowed to the valid subset
//     (mirrors cobranza's handling of a non-numeric id in its own by-ids).
//   - Duplicate ids                  → accepted as-is; MSP_VISITAS.ID is the
//     primary key, so a duplicate in the request cannot produce a duplicate
//     in the response — nothing to dedupe.
//   - An id the server has never seen → simply absent from the response,
//     never an error. That is the expected, common case this endpoint
//     exists to answer.
func (h *Handlers) VisitasExistentesPorIDs(
	ctx context.Context, in *VisitasExistentesPorIDsInput,
) (*VisitasExistentesPorIDsOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	ids, err := parseIDsParam(in.IDs)
	if err != nil {
		return nil, mapAppError(err)
	}

	existentes, err := h.svc.VisitasExistentesPorIDs(ctx, ids)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &VisitasExistentesPorIDsOutput{Body: idsToStrings(existentes)}, nil
}

// parseIDsParam parses the comma-separated "ids" query parameter into a
// slice of UUIDs.
//
// The cap is enforced on the raw comma-split count BEFORE any element is
// parsed as a UUID — rejecting an oversized list costs one strings.Split and
// a length check, never proportional to the list size the caller sent.
//
// Empty segments (e.g. a trailing comma from a naive client-side join) are
// skipped rather than treated as an error. If every segment turns out empty,
// parseIDsParam returns a non-nil, zero-length slice and no error — the
// handler then reaches the service and repo with an empty ids slice, both of
// which short-circuit to an empty result without a query. That differs from
// a wholly blank ids parameter, which is rejected up front as ids_required:
// "no ids parameter at all" is a caller bug worth surfacing distinctly from
// "an ids parameter that evaluates to nothing".
func parseIDsParam(raw string) ([]uuid.UUID, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, apperror.NewValidation("ids_required", "el parámetro ids es obligatorio")
	}

	parts := strings.Split(trimmed, ",")
	if len(parts) > maxIDsPorRequest {
		return nil, apperror.NewValidation(
			"ids_too_many", "máximo "+strconv.Itoa(maxIDsPorRequest)+" ids por request",
		)
	}

	ids := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s == "" {
			continue
		}
		id, perr := uuid.Parse(s)
		if perr != nil {
			return nil, apperror.NewValidation("ids_invalid", "ids contiene un uuid no válido").WithError(perr)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// idsToStrings projects a []uuid.UUID into its wire representation.
func idsToStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// ─── Compile-time handler signature check ──────────────────────────────────

var _ func(context.Context, *VisitasExistentesPorIDsInput) (*VisitasExistentesPorIDsOutput, error) = (*Handlers)(nil).VisitasExistentesPorIDs
