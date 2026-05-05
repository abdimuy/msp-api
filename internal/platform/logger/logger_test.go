package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/logger"
)

func TestNew_DefaultsToTextHandler(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Output: &buf}) // empty Format → text
	l.Info("hello")
	assert.Contains(t, buf.String(), "hello")
}

func TestNew_JSONHandler(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Format: "json", Output: &buf})
	l.Info("hello", "key", "value")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, "hello", record["msg"])
	assert.Equal(t, "value", record["key"])
}

func TestNew_RespectsLogLevel(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Level: "warn", Output: &buf})
	l.Debug("debug msg")
	l.Info("info msg")
	l.Warn("warn msg")
	out := buf.String()
	assert.NotContains(t, out, "debug msg")
	assert.NotContains(t, out, "info msg")
	assert.Contains(t, out, "warn msg")
}

func TestContextHandler_AttachesRequestID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Format: "json", Output: &buf})

	ctx := logger.WithRequestID(context.Background(), "abc-123")
	l.InfoContext(ctx, "x")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, "abc-123", record[string(logger.RequestIDKey)])
}

func TestContextHandler_AttachesUserID(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Format: "json", Output: &buf})

	ctx := logger.WithUserID(context.Background(), "user-42")
	l.InfoContext(ctx, "x")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	assert.Equal(t, "user-42", record[string(logger.UserIDKey)])
}

func TestContextHandler_OmitsEmptyValues(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Format: "json", Output: &buf})
	// no request_id / user_id in ctx.
	l.InfoContext(context.Background(), "x")

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))
	_, hasRID := record[string(logger.RequestIDKey)]
	_, hasUID := record[string(logger.UserIDKey)]
	assert.False(t, hasRID)
	assert.False(t, hasUID)
}

func TestRequestIDFrom_EmptyWhenAbsent(t *testing.T) {
	t.Parallel()
	assert.Empty(t, logger.RequestIDFrom(context.Background()))
}

func TestUserIDFrom_EmptyWhenAbsent(t *testing.T) {
	t.Parallel()
	assert.Empty(t, logger.UserIDFrom(context.Background()))
}

func TestWithGroup_PrefixesAttrs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	l := logger.New(logger.Options{Format: "json", Output: &buf})

	l.WithGroup("http").Info("request", "method", "GET")
	out := buf.String()
	assert.Contains(t, out, "http")
}
