package meilisearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"time"

	meili "github.com/meilisearch/meilisearch-go"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// taskWaitInterval is the polling interval when waiting for asynchronous
// Meilisearch tasks (index creation, settings update) to complete.
const taskWaitInterval = 100 * time.Millisecond

// taskTimeout caps the total wait for any single index-management task
// (create + settings). 30 s is generous; the operations are fast in practice.
const taskTimeout = 30 * time.Second

// RealClient is the production Meilisearch client wrapping the meilisearch-go
// SDK. It satisfies the Client interface.
type RealClient struct {
	sdk meili.ServiceManager
}

// Compile-time assertion.
var _ Client = (*RealClient)(nil)

// NewRealClient constructs a RealClient from config. The SDK client is created
// eagerly; connectivity is NOT verified here — that happens at EnsureIndex time
// so the app can start even if Meilisearch is temporarily unreachable.
func NewRealClient(cfg config.Meilisearch) (*RealClient, error) {
	if cfg.URL == "" {
		return nil, apperror.NewInternal(
			"meilisearch_url_required",
			"la url de meilisearch es requerida",
		)
	}
	opts := []meili.Option{}
	if cfg.APIKey != "" {
		opts = append(opts, meili.WithAPIKey(cfg.APIKey))
	}
	c := meili.New(cfg.URL, opts...)
	slog.Info("meilisearch.real_client_ready", "url", cfg.URL, "index", cfg.IndexName)
	return &RealClient{sdk: c}, nil
}

// EnsureIndex creates the index if it does not exist, then applies (or
// re-applies) the settings. The operation is idempotent: repeated calls on an
// already-configured index are safe and converge to the desired state.
func (c *RealClient) EnsureIndex(ctx context.Context, cfg IndexConfig) error {
	// GetOrCreateIndex is a helper in the SDK — it is NOT a standard method.
	// We use GetIndex first and create only when the index is absent.
	idx := c.sdk.Index(cfg.UID)

	// Probe whether the index already exists.
	_, err := idx.FetchInfo()
	if err != nil {
		// Index does not exist (or unreachable) — try to create it.
		task, createErr := c.sdk.CreateIndex(&meili.IndexConfig{
			Uid:        cfg.UID,
			PrimaryKey: cfg.PrimaryKey,
		})
		if createErr != nil {
			return classifyError("meilisearch_create_index_failed",
				"no se pudo crear el índice de meilisearch", createErr)
		}
		waitCtx, cancel := context.WithTimeout(ctx, taskTimeout)
		defer cancel()
		if waitErr := c.waitForTask(waitCtx, task.TaskUID); waitErr != nil {
			return waitErr
		}
		slog.InfoContext(ctx, "meilisearch.index_created", "index", cfg.UID)
	}

	// Apply settings (idempotent — safe to re-apply on every boot).
	settings := c.buildSettings(cfg)
	task, err := idx.UpdateSettings(settings)
	if err != nil {
		return classifyError("meilisearch_update_settings_failed",
			"no se pudieron actualizar las configuraciones del índice", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()
	if err := c.waitForTask(waitCtx, task.TaskUID); err != nil {
		return err
	}
	slog.InfoContext(ctx, "meilisearch.settings_applied", "index", cfg.UID)
	return nil
}

// buildSettings maps IndexConfig fields to the SDK's Settings type.
func (c *RealClient) buildSettings(cfg IndexConfig) *meili.Settings {
	s := &meili.Settings{}
	if len(cfg.SearchableAttributes) > 0 {
		s.SearchableAttributes = cfg.SearchableAttributes
	}
	if len(cfg.FilterableAttributes) > 0 {
		s.FilterableAttributes = cfg.FilterableAttributes
	}
	if len(cfg.SortableAttributes) > 0 {
		s.SortableAttributes = cfg.SortableAttributes
	}
	if len(cfg.RankingRules) > 0 {
		s.RankingRules = cfg.RankingRules
	}
	if cfg.FacetingMaxValuesPerFacet > 0 {
		s.Faceting = &meili.Faceting{
			MaxValuesPerFacet: cfg.FacetingMaxValuesPerFacet,
		}
	}
	if cfg.PaginationMaxTotalHits > 0 {
		s.Pagination = &meili.Pagination{
			MaxTotalHits: cfg.PaginationMaxTotalHits,
		}
	}
	return s
}

// UpsertDocs bulk-adds or replaces documents. docs must be JSON-serializable.
func (c *RealClient) UpsertDocs(_ context.Context, indexUID string, docs any) error {
	task, err := c.sdk.Index(indexUID).AddDocuments(docs, nil)
	if err != nil {
		return classifyError("meilisearch_upsert_docs_failed",
			"no se pudieron indexar los documentos", err)
	}
	slog.Debug("meilisearch.upsert_docs_enqueued",
		"index", indexUID, "task_uid", task.TaskUID)
	return nil
}

// DeleteDocs removes documents by primary-key string values.
func (c *RealClient) DeleteDocs(_ context.Context, indexUID string, ids []string) error {
	task, err := c.sdk.Index(indexUID).DeleteDocuments(ids, nil)
	if err != nil {
		return classifyError("meilisearch_delete_docs_failed",
			"no se pudieron eliminar los documentos", err)
	}
	slog.Debug("meilisearch.delete_docs_enqueued",
		"index", indexUID, "task_uid", task.TaskUID)
	return nil
}

// Search executes a search request and returns a generic SearchResult.
// FacetDistribution values are returned as int64 counts (Meilisearch returns
// them as float64 in JSON; we convert).
func (c *RealClient) Search(_ context.Context, indexUID string, params SearchParams) (SearchResult, error) {
	req := &meili.SearchRequest{
		Filter: params.Filter,
		Sort:   params.Sort,
		Offset: params.Offset,
		Limit:  params.Limit,
		Facets: params.Facets,
	}

	raw, err := c.sdk.Index(indexUID).SearchRaw(params.Query, req)
	if err != nil {
		return SearchResult{}, classifyError("meilisearch_search_failed",
			"la búsqueda en meilisearch falló", err)
	}

	return decodeSearchResult(raw)
}

// Close releases resources held by the SDK (idle HTTP connections, etc.).
func (c *RealClient) Close() {
	c.sdk.Close()
}

// waitForTask polls until the task identified by taskUID reaches a terminal
// state or the context is cancelled.
func (c *RealClient) waitForTask(ctx context.Context, taskUID int64) error {
	done, err := c.sdk.WaitForTaskWithContext(ctx, taskUID, taskWaitInterval)
	if err != nil {
		return classifyError("meilisearch_task_wait_failed",
			"el task de meilisearch no completó", err)
	}
	if done.Status == meili.TaskStatusFailed {
		return apperror.NewInternal(
			"meilisearch_task_failed",
			"el task de meilisearch falló",
		).WithField("task_uid", taskUID).WithField("error", done.Error)
	}
	return nil
}

// rawSearchResponse is the minimal shape we decode from SearchRaw output.
// The JSON field names use the Meilisearch camelCase convention; tagliatelle
// is suppressed here because we are bound to the external API's format.
//
//nolint:tagliatelle // Meilisearch API returns camelCase field names.
type rawSearchResponse struct {
	Hits               []json.RawMessage         `json:"hits"`
	FacetDistribution  map[string]map[string]any `json:"facetDistribution"`
	EstimatedTotalHits int64                     `json:"estimatedTotalHits"`
}

// decodeSearchResult decodes the SDK's raw JSON search response into
// our SearchResult type, converting float64 facet counts to int64.
func decodeSearchResult(raw *json.RawMessage) (SearchResult, error) {
	if raw == nil {
		return SearchResult{}, nil
	}
	var resp rawSearchResponse
	if err := json.Unmarshal(*raw, &resp); err != nil {
		return SearchResult{}, apperror.NewInternal(
			"meilisearch_decode_failed",
			"no se pudo decodificar la respuesta de meilisearch",
		).WithError(err)
	}

	// Convert facet distribution float64 → int64.
	dist := make(map[string]map[string]int64, len(resp.FacetDistribution))
	for facet, vals := range resp.FacetDistribution {
		inner := make(map[string]int64, len(vals))
		for k, v := range vals {
			switch t := v.(type) {
			case float64:
				inner[k] = int64(t)
			case int64:
				inner[k] = t
			default:
				inner[k] = 0
			}
		}
		dist[facet] = inner
	}

	return SearchResult{
		Hits:               resp.Hits,
		FacetDistribution:  dist,
		EstimatedTotalHits: resp.EstimatedTotalHits,
	}, nil
}

// classifyError maps SDK and transport errors to apperror types.
// Network-level failures and server 5xx are wrapped with ErrMeilisearchTransient
// so callers can decide to retry. Everything else is an internal error.
func classifyError(code, msg string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return &transientError{cause: err, code: code, msg: msg}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return &transientError{cause: err, code: code, msg: msg}
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &transientError{cause: err, code: code, msg: msg}
	}
	// Classify SDK error codes that indicate a server-side transient condition.
	var meiliErr *meili.Error
	if errors.As(err, &meiliErr) {
		if meiliErr.StatusCode >= 500 {
			return &transientError{cause: err, code: code, msg: msg}
		}
		return apperror.NewInternal(code, msg).
			WithError(err).
			WithField("status_code", meiliErr.StatusCode).
			WithField("meili_code", meiliErr.MeilisearchApiError.Code)
	}
	return apperror.NewInternal(code, msg).WithError(err)
}

// transientError wraps a Meilisearch failure so errors.Is(err,
// ErrMeilisearchTransient) is true for callers that want to retry.
type transientError struct {
	code  string
	msg   string
	cause error
}

func (e *transientError) Error() string {
	return fmt.Sprintf("meilisearch: transient [%s]: %v", e.code, e.cause)
}

func (e *transientError) Unwrap() error { return e.cause }

// Is satisfies the errors.Is contract for ErrMeilisearchTransient.
func (e *transientError) Is(target error) bool {
	return target == ErrMeilisearchTransient
}
