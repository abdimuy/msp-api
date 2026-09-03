//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/httpdispatch"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
)

// Handlers holds the visitas HTTP handlers.
type Handlers struct {
	svc *visitasapp.Service
}

// NewHandlers builds a Handlers wired against the given service.
func NewHandlers(svc *visitasapp.Service) *Handlers {
	return &Handlers{svc: svc}
}

// CrearVisita handles POST /visitas in BOTH supported wire formats.
//
// # Coexistence
//
// The route accepts application/json — the shape the phones already in the
// field send — and multipart/form-data, which carries that same JSON in a
// `datos` field plus 0..N `imagen` parts. Both land here, take the same
// validation path, and reach the same service call. A build that predates
// photos keeps working with no change and no feature flag; see
// [CrearVisitaInput] for why the body is read as raw bytes to make that
// possible.
//
// This is NOT how cobranza handled its own JSON→multipart move. There the
// change was a deliberate breaking one — POST /cobranza/pagos rejects a JSON
// Content-Type outright, and a test pins that rejection — which was
// affordable because that endpoint had no production consumers. Visitas does
// not get that luxury, so the coexistence is built here rather than copied.
//
// # Idempotency
//
// The client-generated UUID in the body is the idempotency key end-to-end
// (see visitasapp.Service.RegistrarVisitaConImagenes). A retried request with
// the same id returns the already-stored visita — 201, not 409 — and, when it
// carries photos, neither duplicates them nor orphans their blobs.
//
// The Idempotency-Key header must match the body id for real clients (the
// mobile app sets both to the visita UUID). An internal replay — a desk
// operator re-applying a captured request via the failed-intent replay path —
// mints a fresh transport key by design, so the cross-check is skipped for
// those (mirrors cobranza's CrearPago).
//
// # Failure modes
//
// Everything a client can get wrong — a malformed multipart envelope, a
// missing boundary, an image type outside the whitelist, a tipo_visita
// outside the closed catalogue — answers a 4xx with its own code. None of it
// reaches a 500: on this route a 500 would look like the server silently
// losing a cobrador's visit.
func (h *Handlers) CrearVisita(ctx context.Context, in *CrearVisitaInput) (*CrearVisitaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	decoded, decodeErr := decodeCrearVisitaRequest(in.ContentType, in.RawBody)
	// Closing every part the decoder opened is not optional: without it the
	// request leaks a file descriptor per image, which only surfaces under
	// load. The defer is registered BEFORE the error check on purpose — a
	// decode that failed halfway still returns the handles it opened.
	defer func() {
		for _, c := range decoded.Closers {
			_ = c.Close()
		}
	}()
	if decodeErr != nil {
		return nil, mapAppError(decodeErr)
	}

	visitaID, err := parseVisitaID(decoded.Body.ID)
	if err != nil {
		return nil, mapAppError(err)
	}

	if in.IdempotencyKey != "" && in.IdempotencyKey != decoded.Body.ID && !httpdispatch.IsInternal(ctx) {
		return nil, mapAppError(
			apperror.NewValidation("idempotency_key_mismatch", "Idempotency-Key debe coincidir con body.id"),
		)
	}

	fecha, err := time.Parse(time.RFC3339, decoded.Body.Fecha)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("visita_fecha_invalida", "fecha no es una fecha-hora RFC3339 válida").WithError(err),
		)
	}

	appIn := visitasapp.RegistrarVisitaInput{
		ID:             visitaID,
		Cobrador:       decoded.Body.Cobrador,
		CobradorID:     decoded.Body.CobradorID,
		Fecha:          fecha.UTC(),
		FormaCobroID:   decoded.Body.FormaCobroID,
		Lat:            decoded.Body.Lat,
		Lng:            decoded.Body.Lng,
		Nota:           decoded.Body.Nota,
		TipoVisita:     decoded.Body.TipoVisita,
		ZonaClienteID:  decoded.Body.ZonaClienteID,
		ClienteID:      decoded.Body.ClienteID,
		ImpteDoctoCCID: decoded.Body.ImpteDoctoCCID,
	}

	visita, err := h.svc.RegistrarVisitaConImagenes(ctx, appIn, decoded.Uploads, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CrearVisitaOutput{Body: toVisitaDTO(visita)}, nil
}

// ObtenerVisita handles GET /visitas/{id}.
//
// Loads a single visita from MSP_VISITAS by UUID. Returns 404
// (visita_not_found) when no row matches, and 422 when the path segment is
// not a UUID — never a 500.
//
// Requires the same permission as the write side (cobranza:ver_pagos): a
// cobrador who may record a visita may read it back.
//
// Reads are catalogue-agnostic. A row whose TIPO_VISITA predates the closed
// catalogue — a retired literal, or the empty string on the 200 oldest rows —
// loads and serializes normally, because the repo rehydrates without
// validating (domain.RehydrateVisita). Enforcing the catalogue here would
// break exactly the legacy rows this endpoint exists to expose.
func (h *Handlers) ObtenerVisita(ctx context.Context, in *ObtenerVisitaInput) (*ObtenerVisitaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	visitaID, err := parseVisitaID(in.ID)
	if err != nil {
		return nil, mapAppError(err)
	}

	visita, err := h.svc.ObtenerVisita(ctx, visitaID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ObtenerVisitaOutput{Body: toVisitaDTO(visita)}, nil
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var (
	_ func(context.Context, *CrearVisitaInput) (*CrearVisitaOutput, error)     = (*Handlers)(nil).CrearVisita
	_ func(context.Context, *ObtenerVisitaInput) (*ObtenerVisitaOutput, error) = (*Handlers)(nil).ObtenerVisita
)
