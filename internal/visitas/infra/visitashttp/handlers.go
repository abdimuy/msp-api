//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"context"
	"time"

	"github.com/google/uuid"

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

// CrearVisita handles POST /visitas.
//
// Idempotency: the client-generated UUID in body.id is the idempotency key
// end-to-end (see visitasapp.Service.RegistrarVisita) — a retried request
// with the same id returns the already-stored visita instead of an error.
//
// The Idempotency-Key header must match body.id for real clients (the
// mobile app sets both to the visita UUID). An internal replay — a desk
// operator re-applying a captured request via the failed-intent replay
// path — mints a fresh transport key by design, so the cross-check is
// skipped for those (mirrors cobranza's CrearPago).
func (h *Handlers) CrearVisita(ctx context.Context, in *CrearVisitaInput) (*CrearVisitaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}

	visitaID, err := uuid.Parse(in.Body.ID)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("visita_id_invalido", "el id de la visita no es un UUID válido").WithError(err),
		)
	}

	if in.IdempotencyKey != "" && in.IdempotencyKey != in.Body.ID && !httpdispatch.IsInternal(ctx) {
		return nil, mapAppError(
			apperror.NewValidation("idempotency_key_mismatch", "Idempotency-Key debe coincidir con body.id"),
		)
	}

	fecha, err := time.Parse(time.RFC3339, in.Body.Fecha)
	if err != nil {
		return nil, mapAppError(
			apperror.NewValidation("visita_fecha_invalida", "fecha no es una fecha-hora RFC3339 válida").WithError(err),
		)
	}

	appIn := visitasapp.RegistrarVisitaInput{
		ID:             visitaID,
		Cobrador:       in.Body.Cobrador,
		CobradorID:     in.Body.CobradorID,
		Fecha:          fecha.UTC(),
		FormaCobroID:   in.Body.FormaCobroID,
		Lat:            in.Body.Lat,
		Lng:            in.Body.Lng,
		Nota:           in.Body.Nota,
		TipoVisita:     in.Body.TipoVisita,
		ZonaClienteID:  in.Body.ZonaClienteID,
		ClienteID:      in.Body.ClienteID,
		ImpteDoctoCCID: in.Body.ImpteDoctoCCID,
	}

	visita, err := h.svc.RegistrarVisita(ctx, appIn, cu.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CrearVisitaOutput{Body: toVisitaDTO(visita)}, nil
}

// ─── Compile-time handler signature checks ────────────────────────────────────

var _ func(context.Context, *CrearVisitaInput) (*CrearVisitaOutput, error) = (*Handlers)(nil).CrearVisita
