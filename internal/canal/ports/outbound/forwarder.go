package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

// Forwarder pushes one inbound WhatsApp message to the store's on-premise
// server. It is the module's outbox in spirit (see the plan's "desviaciones
// conscientes" table: canal has no MSP_OUTBOX_EVENTS row to enqueue into,
// because that table lives in the on-premise Firebird the VPS cannot reach —
// the retry queue kept by BuzonRepo plays the same role instead).
//
// Forwarder does not touch the mailbox: it only attempts the push and
// reports success or failure. Recording the outcome — MarcarReenviado or
// MarcarFallido — is the caller's job (Task 4's worker).
type Forwarder interface {
	// Reenviar pushes m to the store's on-premise server. A non-nil error
	// means the push did not land; the caller decides whether and when to
	// retry.
	Reenviar(ctx context.Context, m *domain.MensajeEntrante) error
}
