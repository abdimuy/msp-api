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

// ErrCodeCommunicationForTest exposes the SDK constant so test assertions
// do not need to import the SDK directly.
//
//nolint:gochecknoglobals // test-only export; not part of the public API.
var ErrCodeCommunicationForTest = meili.MeilisearchCommunicationError
