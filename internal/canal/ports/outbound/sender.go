package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// Sender is the port app.Service.EnviarSaliente uses to push one outbound
// WhatsApp message — text or template — through whichever transport is
// wired at composition root (Task 7). Deliberately narrow: SendText and
// SendTemplate are the only two operations the send path needs; document
// upload (whatsapp.Client.UploadMedia/SendDocumentByMediaID) is a distinct
// concern outside this port's scope.
//
// internal/platform/whatsapp.Client already satisfies Sender structurally
// — same two method signatures, same parameter and return types — so
// production wiring (Task 7) can pass a whatsapp.Client value directly as
// a Sender with no adapter to write; see the compile-time assertion below.
//
// Referencing whatsapp.Template here rather than redeclaring an equivalent
// struct is a deliberate choice, not an oversight: internal/platform/… is
// unconditionally allowed through the seal (ADR-0009), so depending on it
// from ports/outbound costs nothing extraction-wise beyond what this
// module's domain and infra layers already assume elsewhere (e.g.
// internal/platform/config, internal/platform/apperror). Extracting canal
// still means "copy the internal/canal directory, plus internal/platform"
// — that was already true before this port existed.
type Sender interface {
	// SendText sends a free-form text message and returns the resulting
	// wamid. Only legal inside WhatsApp's 24h customer-service window —
	// see whatsapp.ErrWindowClosed.
	SendText(ctx context.Context, to, body string) (string, error)

	// SendTemplate sends a pre-approved template message and returns the
	// resulting wamid. Legal at any time.
	SendTemplate(ctx context.Context, to string, tmpl whatsapp.Template) (string, error)
}

// Compile-time assertion: whatsapp.Client (both the real and disabled
// implementations) satisfies Sender with no adapter required — see the
// type doc above.
var _ Sender = whatsapp.Client(nil)
