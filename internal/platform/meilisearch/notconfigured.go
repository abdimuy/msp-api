package meilisearch

import (
	"context"
	"log/slog"
)

// NotConfiguredClient is the safe fallback when MEILISEARCH_ALLOW_UNCONFIGURED
// is true and no URL is provided. Every method returns
// ErrMeilisearchNotConfigured so callers can degrade gracefully (e.g. skip
// indexing, fall back to SQL search).
type NotConfiguredClient struct{}

// Compile-time assertion.
var _ Client = (*NotConfiguredClient)(nil)

// NewNotConfiguredClient constructs the always-error client.
func NewNotConfiguredClient() *NotConfiguredClient {
	slog.Warn("meilisearch.not_configured: search features degraded")
	return &NotConfiguredClient{}
}

// EnsureIndex is a no-op for the not-configured client.
func (c *NotConfiguredClient) EnsureIndex(_ context.Context, _ IndexConfig) error {
	return ErrMeilisearchNotConfigured
}

// UpsertDocs is a no-op for the not-configured client.
func (c *NotConfiguredClient) UpsertDocs(_ context.Context, _ string, _ any) error {
	return ErrMeilisearchNotConfigured
}

// DeleteDocs is a no-op for the not-configured client.
func (c *NotConfiguredClient) DeleteDocs(_ context.Context, _ string, _ []string) error {
	return ErrMeilisearchNotConfigured
}

// Search is a no-op for the not-configured client.
func (c *NotConfiguredClient) Search(_ context.Context, _ string, _ SearchParams) (SearchResult, error) {
	return SearchResult{}, ErrMeilisearchNotConfigured
}

// Close is a no-op for the not-configured client.
func (c *NotConfiguredClient) Close() {}
