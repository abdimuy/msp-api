// Package logger configures the structured logger (slog) for the application.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// ContextKey is the type used for logger context keys.
type ContextKey string

const (
	// RequestIDKey is the context key under which the per-request ID is stored.
	RequestIDKey ContextKey = "request_id"
	// UserIDKey is the context key under which the local user UUID is stored.
	UserIDKey ContextKey = "user_id"
)

// Options configures the logger setup.
type Options struct {
	Level  string // "debug" | "info" | "warn" | "error"
	Format string // "text" | "json"
	Output io.Writer
}

// New builds an *slog.Logger with the given options.
// Output defaults to os.Stdout when nil.
func New(opts Options) *slog.Logger {
	if opts.Output == nil {
		opts.Output = os.Stdout
	}

	level := parseLevel(opts.Level)
	handlerOpts := &slog.HandlerOptions{
		Level:     level,
		AddSource: level == slog.LevelDebug,
	}

	var handler slog.Handler
	if strings.EqualFold(opts.Format, "json") {
		handler = slog.NewJSONHandler(opts.Output, handlerOpts)
	} else {
		handler = slog.NewTextHandler(opts.Output, handlerOpts)
	}

	return slog.New(&contextHandler{Handler: handler})
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// contextHandler enriches every log record with request_id and user_id from
// context, when present.
type contextHandler struct{ slog.Handler }

// Handle adds context-scoped attributes (request_id, user_id) to the record
// before forwarding to the wrapped handler.
func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if v, ok := ctx.Value(RequestIDKey).(string); ok && v != "" {
		r.AddAttrs(slog.String(string(RequestIDKey), v))
	}
	if v, ok := ctx.Value(UserIDKey).(string); ok && v != "" {
		r.AddAttrs(slog.String(string(UserIDKey), v))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs returns a child handler with attrs applied.
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a child handler with name as the group prefix.
func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}

// WithRequestID returns a context with the given request ID attached.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, RequestIDKey, id)
}

// RequestIDFrom returns the request ID stored in ctx, or "" when missing.
func RequestIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(RequestIDKey).(string)
	return v
}

// WithUserID returns a context with the given local user ID attached.
func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, UserIDKey, id)
}

// UserIDFrom returns the user ID stored in ctx, or "" when missing.
func UserIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}
