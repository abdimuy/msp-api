package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/auth"
	authoutbound "github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentblobfs "github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	"github.com/abdimuy/msp-api/internal/platform/response"
)

// SettableReplayDispatcher wraps the root chi router but allows the router to
// be assigned AFTER construction. It breaks the natural dependency cycle:
//
//	router → admin handler → dispatcher → router
//
// The fx providers build the dispatcher first with a nil handler; once the
// router has been assembled, an fx.Invoke calls Set to publish it. Loads use
// atomic.Pointer so Dispatch is safe to call concurrently with Set.
type SettableReplayDispatcher struct {
	h atomic.Pointer[http.Handler]
}

// NewSettableReplayDispatcher builds an unwired dispatcher. Use Set to
// publish the chi router once it has been assembled.
func NewSettableReplayDispatcher() *SettableReplayDispatcher {
	return &SettableReplayDispatcher{}
}

// Set publishes h as the dispatcher's target. Safe to call any number of
// times; the latest call wins.
func (d *SettableReplayDispatcher) Set(h http.Handler) {
	d.h.Store(&h)
}

// Dispatch forwards r to the published handler. When called before the
// router has been wired (Set never invoked) it responds with a stable 5xx
// apperror so the failure is observable in logs instead of panicking.
func (d *SettableReplayDispatcher) Dispatch(w http.ResponseWriter, r *http.Request) {
	ptr := d.h.Load()
	if ptr == nil {
		response.Error(w, r, apperror.NewInternal(
			"failedintent_replay_unwired",
			"el dispatcher de replay aún no está listo",
		))
		return
	}
	(*ptr).ServeHTTP(w, r)
}

// usuarioLookup adapts the auth UsuarioRepo into the narrow port that
// failedintenthttp.Service depends on. Lives in cmd/api so the http subpackage
// stays decoupled from the auth domain.
type usuarioLookup struct {
	repo authoutbound.UsuarioRepo
}

// BuildCurrentUserByID reconstructs the auth.CurrentUser for the original
// requester. Mirrors the projection performed by authhttp.AuthnMiddleware so
// replays observe the exact same effective permission set.
func (u *usuarioLookup) BuildCurrentUserByID(
	ctx context.Context, id uuid.UUID,
) (auth.CurrentUser, error) {
	usuario, err := u.repo.FindByID(ctx, id)
	if err != nil {
		return auth.CurrentUser{}, err
	}
	if !usuario.Activo() {
		return auth.CurrentUser{}, apperror.NewForbidden(
			"user_inactive",
			"el usuario asociado al intento se encuentra inactivo",
		)
	}
	perms, err := u.repo.PermisosFor(ctx, usuario.ID())
	if err != nil {
		return auth.CurrentUser{}, err
	}
	return auth.ToContract(usuario, perms), nil
}

var _ failedintenthttp.UsuarioLookup = (*usuarioLookup)(nil)

// provideFailedIntentStore builds the Firebird-backed Store. The concrete
// type is exposed alongside the interface so the orphan-sweep wiring can
// consume the ReferencedPaths method without dragging it into the Store
// interface from cross-package callers.
func provideFailedIntentStore(p *firebird.Pool) *failedintentfb.Store {
	return failedintentfb.New(p)
}

// provideFailedIntentStoreInterface narrows the concrete *firebird.Store to
// the Store interface that consumers depend on.
func provideFailedIntentStoreInterface(s *failedintentfb.Store) failedintent.Store {
	return s
}

// provideFailedIntentBlobStorage builds the filesystem-backed BlobStorage
// adapter rooted at the resolved blob dir (defaults to
// STORAGE_DIR/failed-intents).
func provideFailedIntentBlobStorage(cfg *config.Config) (*failedintentblobfs.Store, error) {
	return failedintentblobfs.New(cfg.FailedIntentBlobDir())
}

// provideFailedIntentBlobStorageInterface exposes the concrete blobfs.Store
// as the BlobStorage interface that the http subpackage and the middleware
// consume.
func provideFailedIntentBlobStorageInterface(s *failedintentblobfs.Store) failedintent.BlobStorage {
	return s
}

// provideFailedIntentCaptureConfig assembles the CaptureMiddleware config
// wiring the configured MaxMultipartBytes plus the blob storage so
// multipart /v2/ventas bodies opt into capture.
func provideFailedIntentCaptureConfig(
	store failedintent.Store,
	blob failedintent.BlobStorage,
	cfg *config.Config,
) failedintent.Config {
	return failedintent.Config{
		Store:             store,
		Blob:              blob,
		MaxMultipartBytes: cfg.FailedIntent.MaxMultipartBytes,
	}
}

// provideSettableReplayDispatcher constructs the cycle-breaking dispatcher.
func provideSettableReplayDispatcher() *SettableReplayDispatcher {
	return NewSettableReplayDispatcher()
}

// provideReplayDispatcher exposes the settable dispatcher as the interface
// the http subpackage consumes.
func provideReplayDispatcher(d *SettableReplayDispatcher) failedintent.ReplayDispatcher {
	return d
}

// provideFailedIntentUsuarioLookup adapts the auth UsuarioRepo for replay.
func provideFailedIntentUsuarioLookup(repo authoutbound.UsuarioRepo) failedintenthttp.UsuarioLookup {
	return &usuarioLookup{repo: repo}
}

// provideFailedIntentHTTPService wires the admin handlers. The blob storage
// is required so /replay can stream multipart bodies from disk back through
// the dispatcher byte-exact.
func provideFailedIntentHTTPService(
	store failedintent.Store,
	dispatcher failedintent.ReplayDispatcher,
	usuarios failedintenthttp.UsuarioLookup,
	blobs failedintent.BlobStorage,
) *failedintenthttp.Service {
	return failedintenthttp.NewService(store, dispatcher, usuarios, blobs, nil, nil)
}

// provideFailedIntentJanitor builds the background purge component wired to
// delete blobs alongside their parent rows.
func provideFailedIntentJanitor(
	store failedintent.Store, blobs failedintent.BlobStorage,
) *failedintent.Janitor {
	return failedintent.NewJanitor(failedintent.JanitorConfig{
		Store: store,
		Blob:  blobs,
	})
}

// wireReplayDispatcher publishes the assembled root handler to the
// dispatcher. Runs as an fx.Invoke AFTER all providers have built — at that
// point the chi router exists and Dispatch can safely forward to it.
func wireReplayDispatcher(d *SettableReplayDispatcher, root RootHandler) {
	d.Set(root)
}

// registerFailedIntentJanitorLifecycle hooks the janitor into the fx
// lifecycle so it starts at boot and drains at shutdown.
func registerFailedIntentJanitorLifecycle(lc fx.Lifecycle, j *failedintent.Janitor) {
	lifecycle.Append(lc, "failedintent-janitor", j)
}

// invokeFailedIntentOrphanSweep registers a boot-time sweep that removes
// .bin files left behind by a previous run (crashed after rename, dropped
// row via a manual migration, etc.). Failures are logged, not fatal — the
// service still boots when the sweep cannot run.
func invokeFailedIntentOrphanSweep(
	lc fx.Lifecycle,
	store *failedintentfb.Store,
	blobs *failedintentblobfs.Store,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			report, err := failedintentblobfs.SweepOrphans(ctx, blobs, store)
			if err != nil {
				slog.WarnContext(
					ctx,
					"failedintent: orphan sweep failed at boot",
					"error", err,
				)
				return nil
			}
			slog.InfoContext(
				ctx,
				"failedintent: orphan sweep complete",
				"scanned", report.Scanned,
				"deleted", report.Deleted,
			)
			return nil
		},
	})
}
