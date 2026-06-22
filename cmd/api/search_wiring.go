package main

import (
	"context"
	"errors"
	"log/slog"

	"go.uber.org/fx"

	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// ── Meilisearch provider ──────────────────────────────────────────────────────

// provideMeilisearchClient constructs the Meilisearch client (real or
// not-configured) based on the loaded config.
func provideMeilisearchClient(cfg *config.Config) (platformmeili.Client, error) {
	return platformmeili.NewMeilisearchClient(cfg.Meilisearch)
}

// ── Lifecycle: idempotent index bootstrap ─────────────────────────────────────

// meilisearchBootstrap holds the Meilisearch client and config needed to
// bootstrap the clientes index on application start.
type meilisearchBootstrap struct {
	client    platformmeili.Client
	indexName string
}

// Start kicks off the index bootstrap (create + settings) and returns
// immediately, running EnsureIndex detached in the background.
//
// Why detached: applying settings on an already-populated index can make
// Meilisearch trigger a full reindex that runs far longer than fx's
// StartTimeout. Doing it inline made Start exceed the deadline, so fx aborted
// boot with "context deadline exceeded" — and because the reindex kept running
// server-side, every retry re-queued the same settings task and the app could
// never boot. Detaching (the same pattern the background workers use) lets boot
// complete immediately while settings converge in the background. Serving
// before settings are applied is safe: the directory index is unusable until
// the reconcile worker's first tick anyway.
func (b *meilisearchBootstrap) Start(ctx context.Context) error {
	idxCfg := clientessearchmeili.DefaultIndexConfig(b.indexName)
	// Drop fx's start-phase cancellation+deadline but keep ctx's values; the
	// goroutine outlives Start. Bounded internally by meilisearch taskTimeout.
	bgCtx := context.WithoutCancel(ctx)
	go func() {
		if err := b.client.EnsureIndex(bgCtx, idxCfg); err != nil {
			// NotConfiguredClient returns ErrMeilisearchNotConfigured — log and
			// continue. The directory falls back to SQL sort until configured.
			if isNotConfiguredErr(err) {
				slog.WarnContext(bgCtx, "meilisearch.bootstrap_skipped: not configured")
				return
			}
			slog.ErrorContext(bgCtx, "meilisearch.bootstrap_failed", "error", err.Error())
			return
		}
		slog.InfoContext(bgCtx, "meilisearch.bootstrap_done", "index", b.indexName)
	}()
	return nil
}

// Stop closes the Meilisearch client connection pool.
func (b *meilisearchBootstrap) Stop(_ context.Context) error {
	b.client.Close()
	return nil
}

// isNotConfiguredErr reports whether err is ErrMeilisearchNotConfigured.
func isNotConfiguredErr(err error) bool {
	return errors.Is(err, platformmeili.ErrMeilisearchNotConfigured)
}

// registerMeilisearchBootstrapLifecycle hooks the index bootstrap into the fx
// lifecycle so EnsureIndex runs on application start and Close runs on stop.
func registerMeilisearchBootstrapLifecycle(
	lc fx.Lifecycle,
	client platformmeili.Client,
	cfg *config.Config,
) {
	b := &meilisearchBootstrap{
		client:    client,
		indexName: cfg.Meilisearch.IndexName,
	}
	lifecycle.Append(lc, "meilisearch-bootstrap", b)
}

// ── Compile-time assertion ────────────────────────────────────────────────────

var _ lifecycle.Hooks = (*meilisearchBootstrap)(nil)
