// Package response renders JSON responses and RFC 9457 Problem Details errors.
package response

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/logger"
)

// ContentTypeJSON is the standard JSON content type.
const ContentTypeJSON = "application/json; charset=utf-8"

// ContentTypeProblem is the RFC 9457 Problem Details content type.
const ContentTypeProblem = "application/problem+json; charset=utf-8"

// JSON writes v as JSON with the given status code.
//
// On encoding failure it falls back to a 500 Problem Details response and logs
// the encoding error.
func JSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", ContentTypeJSON)
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode JSON response", "error", err)
	}
}

// NoContent writes a 204 with no body.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Problem represents an RFC 9457 Problem Details document.
//
// The five canonical fields (type, title, status, detail, instance) come from
// the spec; Code, RequestID, and Errors are extension members allowed by §3.2.
type Problem struct {
	Type      string         `json:"type"`               // URI identifying the problem class
	Title     string         `json:"title"`              // short, human-readable summary
	Status    int            `json:"status"`             // HTTP status code
	Detail    string         `json:"detail,omitempty"`   // human-readable explanation
	Instance  string         `json:"instance,omitempty"` // URI of this specific occurrence (request path)
	Code      string         `json:"code,omitempty"`     // machine-readable, snake_case
	RequestID string         `json:"request_id,omitempty"`
	Errors    []FieldError   `json:"errors,omitempty"` // for 422 validation
	Fields    map[string]any `json:"fields,omitempty"` // extra context attached by apperror.WithField
}

// FieldError describes a single failed field in a validation error.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Error renders an error as a Problem Details document.
//
// Wraps the error in apperror.Error if it isn't already one (treating it as
// a 500 internal). Logs the underlying cause when present.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	appErr, ok := apperror.As(err)
	if !ok {
		appErr = apperror.NewInternal("internal_error", "ocurrió un error interno").WithError(err)
	}

	status := appErr.Kind.HTTPStatus()
	requestID := logger.RequestIDFrom(r.Context())

	// Always log internal errors at error level; client errors at info.
	if status >= http.StatusInternalServerError {
		slog.ErrorContext(
			r.Context(), "request failed",
			"code", appErr.Code,
			"status", status,
			"error", errors.Unwrap(appErr),
		)
	} else {
		slog.InfoContext(
			r.Context(), "request rejected",
			"code", appErr.Code,
			"status", status,
		)
	}

	problem := Problem{
		Type:      "about:blank",
		Title:     http.StatusText(status),
		Status:    status,
		Detail:    appErr.Message,
		Instance:  r.URL.Path,
		Code:      appErr.Code,
		RequestID: requestID,
		Fields:    appErr.Fields,
	}

	w.Header().Set("Content-Type", ContentTypeProblem)
	w.WriteHeader(status)
	if encodeErr := json.NewEncoder(w).Encode(problem); encodeErr != nil {
		slog.ErrorContext(r.Context(), "failed to encode problem response", "error", encodeErr)
	}
}

// ValidationError renders a 422 Problem with a list of field errors.
func ValidationError(w http.ResponseWriter, r *http.Request, fields []FieldError) {
	requestID := logger.RequestIDFrom(r.Context())
	problem := Problem{
		Type:      "about:blank",
		Title:     http.StatusText(http.StatusUnprocessableEntity),
		Status:    http.StatusUnprocessableEntity,
		Detail:    "uno o más campos no son válidos",
		Instance:  r.URL.Path,
		Code:      "validation_failed",
		RequestID: requestID,
		Errors:    fields,
	}
	w.Header().Set("Content-Type", ContentTypeProblem)
	w.WriteHeader(http.StatusUnprocessableEntity)
	if err := json.NewEncoder(w).Encode(problem); err != nil {
		slog.ErrorContext(r.Context(), "failed to encode validation problem", "error", err)
	}
}
