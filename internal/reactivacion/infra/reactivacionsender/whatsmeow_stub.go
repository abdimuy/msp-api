//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionsender

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// WhatsmeowSender is a stub for the real whatsmeow-backed channel. Fase 2 has
// no WhatsApp number to pair with, so Enviar always fails with a clear
// "not configured" error — Fase 3 replaces this file's body (QR pairing,
// actual send) behind the same MessageSender signature; nothing else changes.
type WhatsmeowSender struct{}

// NewWhatsmeowSender builds a WhatsmeowSender stub.
func NewWhatsmeowSender() *WhatsmeowSender { return &WhatsmeowSender{} }

// Enviar always fails: the real whatsmeow channel is not wired up yet.
func (w *WhatsmeowSender) Enviar(_ context.Context, _ outbound.Destino, _ string) error {
	return apperror.NewInternal(
		"whatsmeow_no_configurado",
		"el canal de whatsapp aún no está configurado",
	).WithSource("reactivacionsender.WhatsmeowSender")
}

// Kind identifies WhatsmeowSender as domain.SenderReal — the measurement-
// integrity tag reserved for a genuinely delivered message once Fase 3 wires
// up the real channel.
func (w *WhatsmeowSender) Kind() domain.SenderKind { return domain.SenderReal }

var _ outbound.MessageSender = (*WhatsmeowSender)(nil)
