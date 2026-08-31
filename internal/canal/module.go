// Package canal exposes the sealed canal module's fx composition as a
// single fx.Option (Module), per ADR-0009 decision 3: a sealed module owns
// its own wiring rather than appearing piecemeal in a composition root's
// flat provider list — that flat list is exactly what would turn
// extraction into archaeology. cmd/winback (Task 8) includes Module() as
// one line: fx.New(canal.Module(), ...).
//
// This file is the one place in the module that touches every layer at
// once (app, infra/canalsqlite, infra/canalhttp, ports/outbound) and the
// third-party/platform packages that assemble them. It contains no
// business logic — only construction, port-typed returns, and lifecycle
// registration. See docs/adr/0009-asistencia-sealed-module.md.
package canal

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalhttp"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalsqlite"
	canaloutbound "github.com/abdimuy/msp-api/internal/canal/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// Module bundles every provider and lifecycle registration the canal
// module needs. The composition root must separately supply, via its own
// fx.Provide/fx.Supply: *config.Config, *slog.Logger, and a chi.Router
// (registerRoutes mounts canal's whole HTTP surface onto it — see that
// function's doc comment for the one hard constraint on that router).
// Nothing else.
func Module() fx.Option {
	return fx.Options(
		fx.Provide(
			provideClock,
			provideBuzonRepo,
			provideBuzonRepoPort,
			provideForwarder,
			provideSender,
			provideService,
			provideReenvioWorker,
		),
		fx.Invoke(
			registerBuzonRepoLifecycle,
			registerReenvioWorkerLifecycle,
			registerRoutes,
		),
	)
}

// provideClock returns the production UTC clock for the canal module.
func provideClock() canaloutbound.Clock {
	return canaloutbound.ProductionClock{}
}

// provideBuzonRepo opens the durable SQLite mailbox at cfg.Canal.SQLitePath,
// creating the file and ensuring its schema if they do not yet exist.
//
// This mirrors internal/platform/firebird.New: the connection (and here,
// the schema) is established when this provider runs — fx builds the
// dependency graph, calling every provider an fx.Invoke needs, before
// app.Start() ever fires — and registerBuzonRepoLifecycle's Stop hook is
// what actually closes it. The concrete *canalsqlite.Repo is exposed
// separately from its port (provideBuzonRepoPort) so the lifecycle
// registration below can reach Close, which the outbound.BuzonRepo
// interface does not expose.
func provideBuzonRepo(cfg *config.Config) (*canalsqlite.Repo, error) {
	repo, err := canalsqlite.Open(cfg.Canal.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("canal: open sqlite mailbox at %q: %w", cfg.Canal.SQLitePath, err)
	}
	return repo, nil
}

// provideBuzonRepoPort exposes *canalsqlite.Repo as the module's BuzonRepo
// port. fx resolves by exact type, so this makes the interface resolution
// explicit — mirrors cmd/api/flota_wiring.go's provideFlotaTxRunner, and is
// what keeps canal's app layer depending on outbound.BuzonRepo rather than
// a concrete canalsqlite type.
func provideBuzonRepoPort(r *canalsqlite.Repo) canaloutbound.BuzonRepo {
	return r
}

// provideForwarder builds the HTTP-based outbound.Forwarder that pushes
// mailboxed entrantes to the store's on-premise server, authenticated by
// the shared token described on config.Canal.SharedToken.
func provideForwarder(cfg *config.Config) canaloutbound.Forwarder {
	return canalhttp.NewForwarderClient(canalhttp.ForwarderConfig{
		URL:     cfg.Canal.ForwarderURL,
		Token:   cfg.Canal.SharedToken,
		Timeout: cfg.Canal.ForwarderTimeout,
	})
}

// provideSender builds the outbound.Sender the send path (EnviarSaliente)
// uses to push one WhatsApp message on the store's behalf.
// whatsapp.Client satisfies Sender structurally (see that port's doc
// comment), so no adapter is written here — this assigns the interface
// value directly. When WHATSAPP_ENABLED is false, whatsapp.NewClient
// returns a client whose every call fails with whatsapp.ErrWhatsAppDisabled
// rather than attempting a network call; the module still wires cleanly,
// EnviarSaliente simply reports that error until an operator enables it.
func provideSender(cfg *config.Config) canaloutbound.Sender {
	return whatsapp.NewClient(cfg.WhatsApp)
}

// provideService assembles canal's application surface.
//
// txMgr is deliberately nil: every canalsqlite.Repo method is already a
// single atomic SQLite statement (Task 5/6's report), so there is no
// cross-call transaction for a TxRunner to coordinate, and canalsqlite has
// no TxRunner implementation to provide — Service.runInTx already handles a
// nil txMgr by invoking fn directly (see internal/canal/app/service.go).
//
// cfg is a zero-value ReenvioConfig: NewService's own applyDefaults fills
// in reliability.DefaultRetry/DefaultCircuit. canal has no env-configurable
// override for retry/circuit tuning today — add one to config.Canal if
// operating the module in production shows the defaults wrong.
func provideService(
	repo canaloutbound.BuzonRepo,
	forwarder canaloutbound.Forwarder,
	sender canaloutbound.Sender,
	clock canaloutbound.Clock,
	logger *slog.Logger,
) *canalapp.Service {
	return canalapp.NewService(repo, forwarder, sender, clock, nil, canalapp.ReenvioConfig{}, logger)
}

// provideReenvioWorker builds the background worker that drains the
// mailbox to the store on a fixed cadence. cfg is a zero-value
// ReenvioWorkerConfig: NewReenvioWorker's own applyDefaults fills in the
// 30s interval / 50-message batch documented on that type — canal has no
// env-configurable override for these today, matching provideService's
// ReenvioConfig above.
func provideReenvioWorker(svc *canalapp.Service, logger *slog.Logger) *canalapp.ReenvioWorker {
	return canalapp.NewReenvioWorker(svc, canalapp.ReenvioWorkerConfig{}, logger)
}

// registerBuzonRepoLifecycle hooks the SQLite mailbox into the fx
// lifecycle so its file handle is released on shutdown instead of leaked.
func registerBuzonRepoLifecycle(lc fx.Lifecycle, r *canalsqlite.Repo) {
	lifecycle.Append(lc, "canal-buzon-sqlite", buzonRepoLifecycle{repo: r})
}

// buzonRepoLifecycle adapts *canalsqlite.Repo to lifecycle.Hooks.
// canalsqlite.Open already established the connection and ensured the
// schema when provideBuzonRepo ran, so Start has nothing left to do; Stop
// closes the underlying *sql.DB.
type buzonRepoLifecycle struct {
	repo *canalsqlite.Repo
}

// Start is a no-op — see buzonRepoLifecycle's doc comment.
func (buzonRepoLifecycle) Start(context.Context) error { return nil }

// Stop closes the mailbox's underlying database connection.
func (l buzonRepoLifecycle) Stop(context.Context) error {
	return l.repo.Close()
}

// registerReenvioWorkerLifecycle hooks the forwarding worker into the fx
// lifecycle so it starts with the application and drains on shutdown. The
// worker's own Start already detaches from fx's OnStart context before
// entering its ticker loop (internal/canal/app/reenvio_worker.go) — this
// call passes fx's ctx straight through and lets it do that; nothing here
// re-solves that problem.
func registerReenvioWorkerLifecycle(lc fx.Lifecycle, w *canalapp.ReenvioWorker) {
	lifecycle.Append(lc, "canal-reenvio-worker", w)
}

// registerRoutes mounts canal's whole HTTP surface — Meta's raw webhook
// plus the internal Huma API — onto the chi.Router the composition root
// supplies.
//
// 🔴 r must not already carry, nor ever gain from anything mounted above it
// in the chain, middleware that reads and replaces the request body: Meta's
// HMAC-SHA256 signature is verified against the exact request bytes, and
// any decode/re-encode invalidates it. See
// internal/canal/infra/canalhttp/webhook.go's mountWebhook doc comment —
// this is the composition-root wiring step that doc comment warns is most
// likely to get this silently wrong.
func registerRoutes(r chi.Router, cfg *config.Config, svc *canalapp.Service, clock canaloutbound.Clock, logger *slog.Logger) {
	canalhttp.MountRouter(r, canalhttp.Deps{
		Svc:   svc,
		Clock: clock,
		Cfg: canalhttp.Config{
			AppSecret:   cfg.WhatsApp.AppSecret,
			VerifyToken: cfg.Canal.WebhookVerifyToken,
			SharedToken: cfg.Canal.SharedToken,
		},
		Logger: logger,
	})
}
