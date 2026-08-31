// Package reactivacionsender implements outbound.MessageSender for the
// reactivación channel: FakeSender (simulated, always succeeds) is the safe
// default; CloudAPISender is the real channel over Meta's WhatsApp Cloud
// API (internal/platform/whatsapp), selected via REACTIVACION_SENDER=cloudapi
// per docs/adr/0010-whatsapp-cloud-api-and-the-always-on-edge.md. There is
// no whatsmeow-backed sender: ADR-0010 rejects whatsmeow outright for the
// pilot, and the earlier stub for it was deleted rather than kept around
// for a phase that will not use it.
//
//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionsender

import (
	"context"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// FakeSender simulates delivering a message: it logs the send and always
// succeeds. It never reaches a real WhatsApp number — safe to run against
// the live cohorte without spending the piloto's channel budget.
type FakeSender struct {
	logger *slog.Logger
}

// NewFakeSender builds a FakeSender. A nil logger falls back to slog.Default().
func NewFakeSender(logger *slog.Logger) *FakeSender {
	if logger == nil {
		logger = slog.Default()
	}
	return &FakeSender{logger: logger}
}

// Enviar logs the simulated send and always returns nil.
func (f *FakeSender) Enviar(ctx context.Context, dest outbound.Destino, cuerpo string) error {
	f.logger.InfoContext(
		ctx, "reactivacion_fake_sender.enviar",
		slog.Int("cliente_id", dest.ClienteID),
		slog.String("telefono", dest.Telefono),
		slog.Int("cuerpo_len", len(cuerpo)),
	)
	return nil
}

// Kind identifies FakeSender as domain.SenderSimulado — the measurement-
// integrity tag that keeps a simulated send from counting as a real contact.
func (f *FakeSender) Kind() domain.SenderKind { return domain.SenderSimulado }

var _ outbound.MessageSender = (*FakeSender)(nil)
