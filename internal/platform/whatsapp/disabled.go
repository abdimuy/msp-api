package whatsapp

import (
	"context"
	"errors"
	"log/slog"
)

// ErrWhatsAppDisabled is returned by disabledClient on every call when
// WHATSAPP_ENABLED is false. Callers may check with
// errors.Is(err, ErrWhatsAppDisabled) to degrade gracefully (e.g. queue the
// message for later, skip the send, surface a clear operator message).
var ErrWhatsAppDisabled = errors.New("whatsapp: disabled")

// disabledClient is the safe fallback used when WHATSAPP_ENABLED=false (the
// default). Every method returns ErrWhatsAppDisabled immediately without
// attempting any network connection.
type disabledClient struct{}

// Compile-time assertion: disabledClient must satisfy Client.
var _ Client = (*disabledClient)(nil)

// newDisabledClient constructs the always-error client and logs a warning so
// operators know the WhatsApp channel is running in degraded mode.
func newDisabledClient() *disabledClient {
	slog.Warn("whatsapp.disabled: WhatsApp channel degraded; set WHATSAPP_ENABLED=true to activate")
	return &disabledClient{}
}

// SendText returns ErrWhatsAppDisabled without making any network request.
func (c *disabledClient) SendText(_ context.Context, _, _ string) (string, error) {
	return "", ErrWhatsAppDisabled
}

// SendTemplate returns ErrWhatsAppDisabled without making any network request.
func (c *disabledClient) SendTemplate(_ context.Context, _ string, _ Template) (string, error) {
	return "", ErrWhatsAppDisabled
}

// SendDocumentByMediaID returns ErrWhatsAppDisabled without making any
// network request.
func (c *disabledClient) SendDocumentByMediaID(_ context.Context, _, _, _, _ string) (string, error) {
	return "", ErrWhatsAppDisabled
}

// UploadMedia returns ErrWhatsAppDisabled without making any network request.
func (c *disabledClient) UploadMedia(_ context.Context, _ Media) (string, error) {
	return "", ErrWhatsAppDisabled
}
