package firebase

import (
	"log/slog"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// NewFirebaseClient selects the outbound.FirebaseClient implementation
// based on the loaded config. The selection matrix:
//
//	APP_ENV=development + DevMode=true     → DevModeClient
//	any env + ProjectID set                → RealClient (not yet
//	                                           implemented — falls back to
//	                                           NotConfigured with a WARN)
//	any env + AllowUnconfigured=true       → NotConfiguredClient (intentional
//	                                           opt-in to permanent 401)
//	anything else                          → error
//
// config.Load() is expected to have already enforced the legal combinations
// — this factory's job is to map a validated config to a concrete client.
func NewFirebaseClient(cfg config.Firebase, env config.Environment) (outbound.FirebaseClient, error) {
	if cfg.DevMode {
		return NewDevModeClient(env)
	}
	if cfg.ProjectID != "" {
		// Real client deferred — see ADR-0002. Fall back to NotConfigured
		// with a startup WARN so the operator notices.
		slog.Warn("auth.firebase_real_client_not_implemented",
			"fallback", "NotConfiguredClient",
			"project_id", cfg.ProjectID)
		return NewNotConfiguredClient(), nil
	}
	if cfg.AllowUnconfigured {
		return NewNotConfiguredClient(), nil
	}
	return nil, apperror.NewInternal(
		"firebase_no_client_selectable",
		"firebase config no selecciona ningún cliente",
	)
}
