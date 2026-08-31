//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// Destino identifies the recipient of an outbound reactivación message.
type Destino struct {
	// ClienteID is the Microsip cliente ID.
	ClienteID int
	// Telefono is the destination phone number.
	Telefono string
}

// MessageSender delivers the body of one message to dest over a channel.
// Implementations live in internal/reactivacion/infra/reactivacionsender —
// a FakeSender that simulates success (the safe default), and a
// CloudAPISender that delivers over Meta's WhatsApp Cloud API, the real
// channel per docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md,
// which rejects whatsmeow outright — there is no whatsmeow-backed
// implementation.
type MessageSender interface {
	// Enviar delivers cuerpo to dest. Returns an error if the channel rejects
	// the message (never returns a partial success).
	Enviar(ctx context.Context, dest Destino, cuerpo string) error

	// Kind identifies which implementation is answering — the measurement-
	// integrity tag persisted on Mensaje.SenderKind so a simulated send can
	// never be counted as a real contact in the attribution.
	Kind() domain.SenderKind
}
