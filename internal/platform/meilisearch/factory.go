package meilisearch

import (
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// NewMeilisearchClient selects the Client implementation based on config.
//
// Selection matrix:
//
//	URL set                         → RealClient (meilisearch-go SDK)
//	URL unset + AllowUnconfigured   → NotConfiguredClient
//	URL unset + !AllowUnconfigured  → error (caught earlier by config.validate)
func NewMeilisearchClient(cfg config.Meilisearch) (Client, error) {
	if cfg.URL != "" {
		return NewRealClient(cfg)
	}
	if cfg.AllowUnconfigured {
		return NewNotConfiguredClient(), nil
	}
	return nil, apperror.NewInternal(
		"meilisearch_no_client_selectable",
		"meilisearch config no selecciona ningún cliente",
	)
}
