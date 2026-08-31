package whatsapp

import (
	"context"
	"io"
)

// Template describes a single WhatsApp template-message invocation: the
// template's registered name, the language code Meta approved it under, and
// the positional variables substituted into the template's body component.
//
// Only body-component positional parameters are supported. Header and
// button parameters are out of scope for this client — see Client for the
// four-operation budget this package deliberately stays within.
type Template struct {
	// Name is the template's name as registered in WhatsApp Manager (e.g.
	// "payment_reminder").
	Name string
	// LanguageCode is the language code the template was approved under
	// (e.g. "es_MX").
	LanguageCode string
	// BodyParams are the positional variables substituted into the
	// template's body component, in order ({{1}}, {{2}}, ...). Nil or empty
	// means the template's body has no variables.
	BodyParams []string
}

// Media is the payload for an UploadMedia call.
type Media struct {
	// Reader streams the file content. The caller retains ownership: a
	// Client implementation reads it to completion but never closes it.
	Reader io.Reader
	// Filename is the file name reported to Meta.
	Filename string
	// MIMEType is the media's MIME type (e.g. "application/pdf"), required
	// by Meta's multipart upload contract.
	MIMEType string
}

// Client is the interface satisfied by both realClient and disabledClient.
// It covers the four operations the WhatsApp Cloud API transport needs:
// free-form text, template messages, sending a document already uploaded to
// Meta by its media id, and uploading new media. Kept deliberately small
// (interfacebloat is on) — domain-specific message composition belongs in
// the consuming module, not here.
//
// Every Send* method returns the WhatsApp message id (wamid) on success.
// UploadMedia returns the media id used by a later SendDocumentByMediaID
// call.
//
// Network errors, timeouts, HTTP 429, and HTTP 5xx responses are wrapped in
// a TransientError and are safe to retry. HTTP 4xx (except 429) and
// JSON-decode failures are permanent and should not be retried. Meta's
// application error codes 131047, 131026 and 130429 are additionally
// exposed as sentinels so a caller can errors.Is against them regardless of
// the wrapping HTTP status.
type Client interface {
	// SendText sends a free-form text message. Only legal inside the 24h
	// customer service window; outside it Meta responds with the error
	// mapped to ErrWindowClosed.
	SendText(ctx context.Context, to, body string) (string, error)

	// SendTemplate sends a pre-approved template message. Legal at any
	// time, including outside the 24h customer service window.
	SendTemplate(ctx context.Context, to string, tmpl Template) (string, error)

	// SendDocumentByMediaID sends a document previously uploaded via
	// UploadMedia, identified by its media id.
	SendDocumentByMediaID(ctx context.Context, to, mediaID, filename, caption string) (string, error)

	// UploadMedia uploads a file to Meta's media store and returns its
	// media id for later use with SendDocumentByMediaID.
	UploadMedia(ctx context.Context, media Media) (string, error)
}
