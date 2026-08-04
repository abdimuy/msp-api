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
}
