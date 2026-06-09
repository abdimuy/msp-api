package main

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/auth/app"
	"github.com/abdimuy/msp-api/internal/auth/infra/authoutbox"
	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	idempotencyfb "github.com/abdimuy/msp-api/internal/platform/idempotency/firebird"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

// provideAuthUsuarioRepo builds the Firebird-backed UsuarioRepo.
func provideAuthUsuarioRepo(p *firebird.Pool) outbound.UsuarioRepo {
	return authfb.NewUsuarioRepo(p)
}

// provideAuthRolRepo builds the Firebird-backed RolRepo.
func provideAuthRolRepo(p *firebird.Pool) outbound.RolRepo {
	return authfb.NewRolRepo(p)
}

// provideAuthPermisoRepo builds the Firebird-backed PermisoRepo.
func provideAuthPermisoRepo(p *firebird.Pool) outbound.PermisoRepo {
	return authfb.NewPermisoRepo(p)
}

// provideAuthClock returns the production clock used by every auth service.
func provideAuthClock() outbound.Clock { return outbound.ProductionClock{} }

// provideAuthFirebase selects the FirebaseClient implementation based on the
// runtime config.
func provideAuthFirebase(cfg *config.Config) (outbound.FirebaseClient, error) {
	return firebase.NewFirebaseClient(cfg.Firebase, cfg.App.Env)
}

// provideIdempotencyStore builds the Firebird-backed idempotency.Store. The
// store is shared by every module's HTTP middleware so a single table
// (MSP_IDEMPOTENCY_KEYS) tracks keys across the whole API surface. Per
// ADR-0008 the store lives in Firebird so a snapshot/restore captures
// idempotency state alongside business data.
func provideIdempotencyStore(p *firebird.Pool) idempotency.Store {
	return idempotencyfb.New(p)
}

// provideIdempotencyStoreConcrete exposes the concrete *idempotencyfb.Store
// so the janitor wiring can call its PurgeExpired method without widening
// the idempotency.Store interface.
func provideIdempotencyStoreConcrete(p *firebird.Pool) *idempotencyfb.Store {
	return idempotencyfb.New(p)
}

// provideIdempotencyJanitor wires the background purge of expired
// MSP_IDEMPOTENCY_KEYS rows.
func provideIdempotencyJanitor(s *idempotencyfb.Store) *idempotencyfb.Janitor {
	return idempotencyfb.NewJanitor(idempotencyfb.JanitorConfig{Store: s})
}

// provideAuthOutboxEnqueuer builds the auth-module wrapper around the platform
// outbox. Backed by Firebird per ADR-0008: the event row is INSERTed into
// MSP_OUTBOX_EVENTS inside the same firebird tx as the business write, so
// the COMMIT covers both atomically and a tx rollback takes the event with
// it.
func provideAuthOutboxEnqueuer(p *firebird.Pool) outbound.OutboxEnqueuer {
	return authoutbox.NewEnqueuer(p)
}

// provideAuthService assembles the auth application service.
func provideAuthService(
	usuarios outbound.UsuarioRepo,
	roles outbound.RolRepo,
	permisos outbound.PermisoRepo,
	clock outbound.Clock,
	outboxEnq outbound.OutboxEnqueuer,
	fb outbound.FirebaseClient,
	fbTxMgr *firebird.TxManager,
) *app.Service {
	return app.NewService(usuarios, roles, permisos, clock, outboxEnq, fb, fbTxMgr)
}

// provideUserDeactivatedHandler constructs the outbox handler that
// propagates user.deactivated events to Firebase Auth.
func provideUserDeactivatedHandler(fb outbound.FirebaseClient) *authoutbox.UserDeactivatedHandler {
	return authoutbox.NewUserDeactivatedHandler(fb)
}

// registerAuthOutboxHandlers registers every auth-module outbox handler
// on the shared registry. Must run before registerOutboxLifecycle so the
// dispatcher sees the handlers when it starts.
func registerAuthOutboxHandlers(reg *outboxfb.HandlerRegistry, h *authoutbox.UserDeactivatedHandler) {
	reg.Register(h)
}

// invokeAuthCatalogSync runs at startup AFTER firebird is up.
//
// It performs two operations:
//
//  1. SyncPermissionCatalog — UPSERTs MSP_PERMISOS rows for every code declared
//     in the domain so the catalog stays in sync with code.
//  2. SyncRolesCatalog — UPSERTs the inmutable "super_admin" rol (and any
//     other future inmutable roles); skips assignment when no usuario exists
//     yet (the bootstrap CLI handles that case).
//
// Failures are logged but never block boot. This keeps a freshly-provisioned
// database able to come up before the operator runs `msp-api auth-bootstrap`.
func invokeAuthCatalogSync(lc fx.Lifecycle, svc *app.Service) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := svc.SyncPermissionCatalog(ctx); err != nil {
				slog.ErrorContext(ctx, "auth.permission_catalog_sync_failed", "error", err)
				return nil
			}
			if err := svc.SyncRolesCatalog(ctx, uuid.Nil); err != nil {
				slog.ErrorContext(ctx, "auth.roles_catalog_sync_failed", "error", err)
				return nil
			}
			return nil
		},
	})
}
