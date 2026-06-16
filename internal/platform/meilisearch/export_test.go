package meilisearch

import "github.com/abdimuy/msp-api/internal/platform/config"

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
