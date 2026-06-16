// Package apperror defines the typed error model used across the API.
//
// Domain layers create sentinel errors with the New* constructors; service
// and repository layers attach context via WithSource and WithError without
// mutating the original sentinel.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

// Kind classifies errors so the HTTP layer can map them to status codes.
type Kind int

const (
	// KindUnknown is the zero value; it maps to 500.
	KindUnknown Kind = iota
	// KindValidation maps to 422 Unprocessable Entity.
	KindValidation
	// KindNotFound maps to 404 Not Found.
	KindNotFound
	// KindConflict maps to 409 Conflict.
	KindConflict
	// KindUnauthorized maps to 401 Unauthorized.
	KindUnauthorized
	// KindForbidden maps to 403 Forbidden.
	KindForbidden
	// KindInternal maps to 500 Internal Server Error.
	KindInternal
	// KindServiceUnavailable maps to 503 Service Unavailable.
	// Used when a required downstream dependency (e.g. Meilisearch) is
	// temporarily unavailable and there is no fallback.
	KindServiceUnavailable
)

// HTTPStatus returns the HTTP status code matching this kind.
func (k Kind) HTTPStatus() int {
	switch k {
	case KindValidation:
		return http.StatusUnprocessableEntity
	case KindNotFound:
		return http.StatusNotFound
	case KindConflict:
		return http.StatusConflict
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindForbidden:
		return http.StatusForbidden
	case KindServiceUnavailable:
		return http.StatusServiceUnavailable
	case KindInternal, KindUnknown:
		return http.StatusInternalServerError
	}
	return http.StatusInternalServerError
}

// Error is the canonical error type produced by domain and platform code.
type Error struct {
	Kind    Kind
	Code    string         // stable, snake_case, English. e.g. "cliente_not_found"
	Message string         // user-facing, Spanish.
	Source  string         // optional: where in the stack the error was attached.
	Cause   error          // wrapped underlying error.
	Fields  map[string]any // optional: extra context for logging.
}

// Error returns a human-readable representation. Callers should not parse this.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the wrapped error for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Is matches errors by Code, allowing errors.Is(err, ErrXxxSentinel).
func (e *Error) Is(target error) bool {
	var t *Error
	if !errors.As(target, &t) {
		return false
	}
	return e.Code == t.Code
}

// WithSource returns a copy with the source location attached.
// The original sentinel is not mutated.
func (e *Error) WithSource(source string) *Error {
	c := *e
	c.Source = source
	return &c
}

// WithError returns a copy that wraps the given error as cause.
func (e *Error) WithError(cause error) *Error {
	c := *e
	c.Cause = cause
	return &c
}

// WithField returns a copy with one extra context field attached.
func (e *Error) WithField(key string, value any) *Error {
	c := *e
	if c.Fields == nil {
		c.Fields = map[string]any{}
	} else {
		// Copy to avoid mutating the parent map.
		copied := make(map[string]any, len(c.Fields)+1)
		for k, v := range c.Fields {
			copied[k] = v
		}
		c.Fields = copied
	}
	c.Fields[key] = value
	return &c
}

// Constructors — used at package level to declare sentinels.

// NewValidation creates a validation error (422).
func NewValidation(code, message string) *Error {
	return &Error{Kind: KindValidation, Code: code, Message: message}
}

// NewNotFound creates a not-found error (404).
func NewNotFound(code, message string) *Error {
	return &Error{Kind: KindNotFound, Code: code, Message: message}
}

// NewConflict creates a conflict error (409).
func NewConflict(code, message string) *Error {
	return &Error{Kind: KindConflict, Code: code, Message: message}
}

// NewUnauthorized creates an unauthorized error (401).
func NewUnauthorized(code, message string) *Error {
	return &Error{Kind: KindUnauthorized, Code: code, Message: message}
}

// NewForbidden creates a forbidden error (403).
func NewForbidden(code, message string) *Error {
	return &Error{Kind: KindForbidden, Code: code, Message: message}
}

// NewInternal creates an internal error (500).
func NewInternal(code, message string) *Error {
	return &Error{Kind: KindInternal, Code: code, Message: message}
}

// NewServiceUnavailable creates a service-unavailable error (503).
// Use when a required downstream dependency is temporarily unreachable and
// there is no local fallback (e.g. Meilisearch not configured or transient).
func NewServiceUnavailable(code, message string) *Error {
	return &Error{Kind: KindServiceUnavailable, Code: code, Message: message}
}

// As extracts an *Error from any error in the chain.
// Returns the *Error and true if found, otherwise zero values.
func As(err error) (*Error, bool) {
	if err == nil {
		return nil, false
	}
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
