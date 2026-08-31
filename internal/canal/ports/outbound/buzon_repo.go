package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

// BuzonRepo persists the durable SQLite mailbox of inbound WhatsApp
// messages: canal's whole reason to exist is that the VPS answers Meta's
// webhook with 200 before the store's on-premise server has necessarily
// seen the message, so every entrante must survive a VPS restart until it is
// forwarded.
type BuzonRepo interface {
	// Guardar persists a newly-arrived MensajeEntrante idempotently, keyed
	// by Wamid — Meta's own message id. A row that already carries the same
	// Wamid is left untouched: Meta delivers webhooks at-least-once, and a
	// retried delivery must not duplicate the row or clobber whatever state
	// the mailbox has already moved it to. inserted reports whether this
	// call created a new row (false means the row already existed and this
	// call was a no-op).
	Guardar(ctx context.Context, m *domain.MensajeEntrante) (inserted bool, err error)

	// ListarPendientes returns up to limite entrantes in
	// domain.EstadoReenvioPendiente, ordered by RecibidoEn ascending — the
	// order the mailbox retries by (see MensajeEntrante's doc comment on
	// RecibidoEn vs TimestampMeta).
	ListarPendientes(ctx context.Context, limite int) ([]*domain.MensajeEntrante, error)

	// MarcarReenviado moves the entrante identified by id to
	// domain.EstadoReenvioReenviado.
	MarcarReenviado(ctx context.Context, id uuid.UUID, now time.Time) error

	// MarcarFallido moves the entrante identified by id to
	// domain.EstadoReenvioFallido, recording motivo as the failure reason a
	// human later reads.
	MarcarFallido(ctx context.Context, id uuid.UUID, motivo string, now time.Time) error

	// ContarPendientes counts entrantes in domain.EstadoReenvioPendiente.
	// Feeds the module's /salud endpoint (Task 6): a growing count is the
	// signal that forwarding to the store has stalled.
	ContarPendientes(ctx context.Context) (int, error)
}
