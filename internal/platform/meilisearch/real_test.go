package meilisearch_test

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// TestNotConfiguredClient verifies the not-configured client returns the
// expected sentinel on every method call.
func TestNotConfiguredClient(t *testing.T) {
	t.Parallel()
	c := platformmeili.NewNotConfiguredClient()

	t.Run("EnsureIndex", func(t *testing.T) {
		t.Parallel()
		err := c.EnsureIndex(context.Background(), platformmeili.IndexConfig{UID: "x"})
		assert.ErrorIs(t, err, platformmeili.ErrMeilisearchNotConfigured)
	})
	t.Run("UpsertDocs", func(t *testing.T) {
		t.Parallel()
		err := c.UpsertDocs(context.Background(), "x", nil)
		assert.ErrorIs(t, err, platformmeili.ErrMeilisearchNotConfigured)
	})
	t.Run("DeleteDocs", func(t *testing.T) {
		t.Parallel()
		err := c.DeleteDocs(context.Background(), "x", []string{"1"})
		assert.ErrorIs(t, err, platformmeili.ErrMeilisearchNotConfigured)
	})
	t.Run("Search", func(t *testing.T) {
		t.Parallel()
		_, err := c.Search(context.Background(), "x", platformmeili.SearchParams{})
		assert.ErrorIs(t, err, platformmeili.ErrMeilisearchNotConfigured)
	})
}

// TestTransientErrorClassification verifies that network-level and
// context-cancellation errors are classified as transient.
// Uses ClassifyErrorForTest exported via export_test.go.
func TestTransientErrorClassification(t *testing.T) {
	t.Parallel()

	t.Run("net_OpError_is_transient", func(t *testing.T) {
		t.Parallel()
		cause := &net.OpError{Op: "dial", Err: errors.New("connection refused")}
		wrapped := platformmeili.ClassifyErrorForTest("test_code", "test msg", cause)
		require.ErrorIs(t, wrapped, platformmeili.ErrMeilisearchTransient,
			"net.OpError must be classified as transient")
		require.ErrorIs(t, wrapped, cause,
			"cause must be reachable through Unwrap chain")
	})

	t.Run("url_Error_is_transient", func(t *testing.T) {
		t.Parallel()
		cause := &url.Error{Op: "Get", URL: "http://nowhere", Err: errors.New("timeout")}
		wrapped := platformmeili.ClassifyErrorForTest("test_code", "test msg", cause)
		assert.ErrorIs(t, wrapped, platformmeili.ErrMeilisearchTransient)
	})

	t.Run("context_deadline_is_transient", func(t *testing.T) {
		t.Parallel()
		wrapped := platformmeili.ClassifyErrorForTest("test_code", "test msg",
			context.DeadlineExceeded)
		assert.ErrorIs(t, wrapped, platformmeili.ErrMeilisearchTransient)
	})

	t.Run("context_cancelled_is_transient", func(t *testing.T) {
		t.Parallel()
		wrapped := platformmeili.ClassifyErrorForTest("test_code", "test msg",
			context.Canceled)
		assert.ErrorIs(t, wrapped, platformmeili.ErrMeilisearchTransient)
	})

	t.Run("generic_error_is_not_transient", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("some permanent configuration error")
		wrapped := platformmeili.ClassifyErrorForTest("test_code", "test msg", cause)
		assert.NotErrorIs(t, wrapped, platformmeili.ErrMeilisearchTransient,
			"generic error must NOT be classified as transient")
	})

	t.Run("nil_error_returns_nil", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, platformmeili.ClassifyErrorForTest("code", "msg", nil))
	})
}

// TestRealClientConstructor_NoURL verifies that NewRealClient returns an
// error when no URL is provided.
func TestRealClientConstructor_NoURL(t *testing.T) {
	t.Parallel()
	_, err := platformmeili.NewRealClient(platformmeili.NewTestConfig(""))
	require.Error(t, err)
}

// TestRealClientConstructor_WithURL verifies that NewRealClient succeeds
// when a URL is provided (constructor does not dial).
func TestRealClientConstructor_WithURL(t *testing.T) {
	t.Parallel()
	c, err := platformmeili.NewRealClient(platformmeili.NewTestConfig("http://127.0.0.1:19999"))
	require.NoError(t, err)
	defer c.Close()
}
