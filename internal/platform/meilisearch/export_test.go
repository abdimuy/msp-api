package meilisearch

import (
	"context"
	"time"

	meili "github.com/meilisearch/meilisearch-go"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ClassifyErrorForTest exposes the private classifyError function so
// unit tests in meilisearch_test can verify the transient/permanent
// classification logic without going through the network.
func ClassifyErrorForTest(code, msg string, err error) error {
	return classifyError(code, msg, err)
}

// NewTestConfig returns a config.Meilisearch pre-filled with the given URL
// for use in unit tests (e.g. http://127.0.0.1:19999 to trigger a reliable
// connection-refused without reaching a real server).
func NewTestConfig(rawURL string) config.Meilisearch {
	return config.Meilisearch{
		URL:       rawURL,
		IndexName: "test",
	}
}

// UpsertDocsAndWaitForTest calls AddDocuments and then waits synchronously for
// the indexing task to complete. Intended for integration tests that need
// deterministic indexing without a time.Sleep.
func (c *RealClient) UpsertDocsAndWaitForTest(ctx context.Context, indexUID string, docs any, interval time.Duration) error {
	task, err := c.sdk.Index(indexUID).AddDocuments(docs, nil)
	if err != nil {
		return classifyError("meilisearch_upsert_docs_failed",
			"no se pudieron indexar los documentos", err)
	}
	_, err = c.sdk.WaitForTaskWithContext(ctx, task.TaskUID, interval)
	return err
}

// DeleteIndexForTest removes the entire index identified by uid. Used in
// integration tests to clean up after themselves.
func (c *RealClient) DeleteIndexForTest(ctx context.Context, uid string) error {
	task, err := c.sdk.DeleteIndex(uid)
	if err != nil {
		return err
	}
	_, err = c.sdk.WaitForTaskWithContext(ctx, task.TaskUID, 100*time.Millisecond)
	return err
}

// IndexInfoForTest carries the primary key and document count for a
// Meilisearch index, combined from FetchInfo + GetStats so integration
// tests can assert both without importing the SDK directly.
type IndexInfoForTest struct {
	PrimaryKey        string
	NumberOfDocuments int64
}

// FetchIndexInfoForTest fetches the current primary key and document count
// for uid. Used by integration tests to assert the outcome of a UpsertDocs
// call that ends up auto-creating the index (the primary-key race).
func (c *RealClient) FetchIndexInfoForTest(ctx context.Context, uid string) (IndexInfoForTest, error) {
	idx := c.sdk.Index(uid)
	info, err := idx.FetchInfoWithContext(ctx)
	if err != nil {
		return IndexInfoForTest{}, err
	}
	stats, err := idx.GetStatsWithContext(ctx, nil)
	if err != nil {
		return IndexInfoForTest{}, err
	}
	return IndexInfoForTest{
		PrimaryKey:        info.PrimaryKey,
		NumberOfDocuments: stats.NumberOfDocuments,
	}, nil
}

// ErrCodeCommunicationForTest exposes the SDK constant so test assertions
// do not need to import the SDK directly.
//
//nolint:gochecknoglobals // test-only export; not part of the public API.
var ErrCodeCommunicationForTest = meili.MeilisearchCommunicationError
