package outbound

import (
	"context"
	"io"
)

// Destino identifies who receives a rendered receipt.
type Destino struct {
	// ClienteID is the Microsip client id. Opaque to this module.
	ClienteID int
	// Telefono is the destination phone number, already validated as usable.
	Telefono string
}

// Documento is the rendered receipt to deliver.
type Documento struct {
	// Nombre is the file name the recipient sees, e.g. "comprobante-A123.pdf".
	Nombre string
	// ContentType is the MIME type, e.g. "application/pdf".
	ContentType string
	// SizeBytes is the payload length.
	SizeBytes int64
	// Body streams the payload. Senders MUST NOT close it; the caller does.
	Body io.Reader
}

// Sender delivers one Documento to Destino over a channel.
//
// The message body is NOT free text: the WhatsApp Business API only allows
// pre-approved templates for business-initiated messages, so implementations
// receive a template name plus its variables and never compose prose.
type Sender interface {
	// Enviar delivers doc to dest using the named template. Variables are
	// substituted into the template placeholders in order. It returns the
	// channel's message id when the channel accepts the message.
	//
	// A returned error means the channel rejected it; there is no partial
	// success.
	Enviar(ctx context.Context, dest Destino, doc Documento, plantilla string, variables []string) (string, error)

	// Canal identifies which implementation answered. It is persisted on the
	// delivery record so a simulated send can never be counted as a real one.
	Canal() string
}
