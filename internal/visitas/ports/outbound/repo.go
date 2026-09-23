package outbound

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// VisitasRepo is the writable side of the visitas module — it backs
// MSP_VISITAS, a preexisting legacy table with no audit columns (see the
// domain.Visita doc comment).
type VisitasRepo interface {
	// Insert persists a new Visita. Returns [domain.ErrVisitaYaExiste] if the
	// UUID collides with an existing row (idempotency key — the mobile app
	// generates the ID offline and may safely retry the request).
	Insert(ctx context.Context, v *domain.Visita) error

	// FindByID loads a single Visita by its UUID. Returns
	// [domain.ErrVisitaNoEncontrada] on miss.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Visita, error)

	// FindExistingIDs returns the subset of ids that already exist in
	// MSP_VISITAS, in no particular order. An id the table does not hold is
	// simply absent from the result — never an error; that is the expected,
	// common case for the caller (the visits reconciler channel, GET
	// /v2/visitas/by-ids, which answers "which of these ids — that the phone
	// itself generated and uploaded — do you already have?"). An empty ids
	// slice returns an empty slice without issuing a query.
	FindExistingIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}

// VisitasImagenesRepo is the write side of MSP_VISITAS_IMAGENES (migration
// 000063), the child collection of the Visita aggregate.
//
// It is a SEPARATE interface from VisitasRepo, not extra methods on it, so a
// caller that only registers visits without photos keeps depending on the
// smaller surface — and so a test can supply one without the other.
type VisitasImagenesRepo interface {
	// InsertImagen persists one imagen belonging to visitaID. It is expected
	// to run inside the caller's transaction (the Firebird repos are
	// tx-aware via firebird.GetQuerier), so a failure here rolls back the
	// parent visita too.
	//
	// Returns [domain.ErrImagenYaExiste] when the imagen UUID already exists.
	// That is not an error condition for the caller: it means this exact
	// image was already stored by an earlier attempt of the same request.
	InsertImagen(ctx context.Context, visitaID uuid.UUID, img *domain.Imagen) error

	// FindImagenIDs returns the ids of the images already attached to
	// visitaID, in no particular order. Used by the idempotent-replay path to
	// decide which images of a retried request are genuinely new.
	FindImagenIDs(ctx context.Context, visitaID uuid.UUID) ([]uuid.UUID, error)
}
