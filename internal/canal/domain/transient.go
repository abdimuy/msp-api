package domain

import (
	"errors"
	"fmt"
)

// TransientError wraps a forwarding failure that is safe to retry: a
// transport-level failure (timeout, connection refused, DNS), an HTTP 5xx,
// or an HTTP 429 from the store's on-premise server.
//
// outbound.Forwarder.Reenviar returns a bare error, so nothing in the port
// itself tells the forwarding worker what is worth retrying, and app/ cannot
// import infra to ask. The infra adapter that implements Forwarder (the HTTP
// client) wraps exactly these failures in TransientError; everything else —
// a permanent 4xx, a malformed response — is left unwrapped and treated as
// permanent, so the worker sends it straight to MarcarFallido without
// burning a retry. Mirrors the shape internal/platform/whatsapp already uses
// for the same purpose.
type TransientError struct {
	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *TransientError) Error() string {
	return fmt.Sprintf("canal: transient forward error: %v", e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As traversal.
func (e *TransientError) Unwrap() error { return e.Cause }

// IsTransient reports whether err (or any error in its chain) is a
// *TransientError. Transient errors are safe to retry with backoff;
// anything else is permanent and should not be retried.
//
// Use IsTransient(err) rather than a type assertion: it traverses the error
// chain, so an error wrapped again with fmt.Errorf("...: %w", err) still
// resolves correctly.
func IsTransient(err error) bool {
	var t *TransientError
	return errors.As(err, &t)
}
