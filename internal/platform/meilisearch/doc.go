// Package meilisearch provides a generic transport client for the Meilisearch
// search engine.
//
// Three implementations are shipped here:
//
//   - NotConfiguredClient: default fallback used when MEILISEARCH_URL is unset
//     and MEILISEARCH_ALLOW_UNCONFIGURED=true. Every method returns
//     ErrMeilisearchNotConfigured.
//   - RealClient: production client wrapping the meilisearch-go SDK. Initialized
//     at boot when MEILISEARCH_URL is set.
//
// Selection happens in the factory (NewMeilisearchClient) at boot. The config
// layer (see internal/platform/config) gates which selection is legal.
//
// This package is GENERIC: it must not import anything from internal/clientes
// or any other module. Clientes-specific document shapes and index settings live
// in internal/clientes/infra/clientessearch.
package meilisearch
