package main

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/auth"
	authoutbound "github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	apperror "github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
	failedintentpg "github.com/abdimuy/msp-api/internal/platform/failedintent/postgres"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	"github.com/abdimuy/msp-api/internal/platform/postgres"
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

// provideFailedIntentStore builds the Postgres-backed Store.
func provideFailedIntentStore(p *postgres.Pool) failedintent.Store {
	return failedintentpg.New(p.Pool)
}

// provideFailedIntentCaptureConfig assembles the CaptureMiddleware config
// with defaults (POST/PATCH/PUT on /v2/ventas, 256 KiB body cap).
func provideFailedIntentCaptureConfig(store failedintent.Store) failedintent.Config {
	return failedintent.Config{Store: store}
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

// provideFailedIntentHTTPService wires the admin handlers.
func provideFailedIntentHTTPService(
	store failedintent.Store,
	dispatcher failedintent.ReplayDispatcher,
	usuarios failedintenthttp.UsuarioLookup,
) *failedintenthttp.Service {
	return failedintenthttp.NewService(store, dispatcher, usuarios, nil, nil, nil)
}

// provideFailedIntentJanitor builds the background purge component.
func provideFailedIntentJanitor(store failedintent.Store) *failedintent.Janitor {
	return failedintent.NewJanitor(failedintent.JanitorConfig{Store: store})
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
