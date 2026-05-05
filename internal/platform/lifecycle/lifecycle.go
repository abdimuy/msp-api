// Package lifecycle helps wire startup/shutdown hooks via uber-fx.
package lifecycle

import (
	"context"
	"log/slog"

	"go.uber.org/fx"
)

// Hooks is the minimal interface a long-running component must implement to
// participate in the application lifecycle.
type Hooks interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Append registers a Hooks implementation with the fx lifecycle, logging
// each phase with the given component name.
func Append(lc fx.Lifecycle, name string, h Hooks) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			slog.InfoContext(ctx, "lifecycle: starting", "component", name)
			if err := h.Start(ctx); err != nil {
				slog.ErrorContext(ctx, "lifecycle: start failed", "component", name, "error", err)
				return err
			}
			slog.InfoContext(ctx, "lifecycle: started", "component", name)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			slog.InfoContext(ctx, "lifecycle: stopping", "component", name)
			if err := h.Stop(ctx); err != nil {
				slog.ErrorContext(ctx, "lifecycle: stop failed", "component", name, "error", err)
				return err
			}
			slog.InfoContext(ctx, "lifecycle: stopped", "component", name)
			return nil
		},
	})
}
