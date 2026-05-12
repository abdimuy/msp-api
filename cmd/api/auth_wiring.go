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
	idempotencypg "github.com/abdimuy/msp-api/internal/platform/idempotency/postgres"
	"github.com/abdimuy/msp-api/internal/platform/postgres"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
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

// provideIdempotencyStore builds the Postgres-backed idempotency.Store. The
// store is shared by every module's HTTP middleware so a single table tracks
// keys across the whole API surface.
func provideIdempotencyStore(p *postgres.Pool) idempotency.Store {
	return idempotencypg.New(p.Pool)
}

// provideAuthOutboxEnqueuer builds the auth-module wrapper around the platform
// outbox, using the Postgres transaction manager.
func provideAuthOutboxEnqueuer(txMgr *transaction.Manager) outbound.OutboxEnqueuer {
	return authoutbox.NewEnqueuer(txMgr)
}

// provideAuthService assembles the auth application service.
func provideAuthService(
	usuarios outbound.UsuarioRepo,
	roles outbound.RolRepo,
	permisos outbound.PermisoRepo,
	clock outbound.Clock,
	outbox outbound.OutboxEnqueuer,
	fb outbound.FirebaseClient,
	fbTxMgr *firebird.TxManager,
) *app.Service {
	return app.NewService(usuarios, roles, permisos, clock, outbox, fb, fbTxMgr)
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
