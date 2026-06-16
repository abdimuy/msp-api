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

// Start calls EnsureIndex so the clientes index is created and configured
// before the HTTP server begins accepting traffic.
func (b *meilisearchBootstrap) Start(ctx context.Context) error {
	idxCfg := clientessearchmeili.DefaultIndexConfig(b.indexName)
	if err := b.client.EnsureIndex(ctx, idxCfg); err != nil {
		// NotConfiguredClient returns ErrMeilisearchNotConfigured — log and
		// continue. The directory will fall back to SQL sort / Bleve until the
		// reconcile worker (Task A3) is in place.
		if isNotConfiguredErr(err) {
			slog.WarnContext(ctx, "meilisearch.bootstrap_skipped: not configured")
			return nil
		}
		return err
	}
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
