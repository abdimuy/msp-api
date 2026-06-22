package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ─── LLM configuration ───────────────────────────────────────────────────────
//
// These tests follow the same t.Setenv pattern as the Meilisearch tests.
// They must NOT be parallel because t.Setenv mutates process-wide state.

func TestLoad_LLM_Defaults(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.LLM.Enabled)
	assert.Equal(t, "qwen2.5:7b-instruct", cfg.LLM.Model)
	assert.Equal(t, 30*time.Second, cfg.LLM.Timeout)
	assert.Empty(t, cfg.LLM.BaseURL)
}

func TestLoad_LLM_EnabledWithBaseURL_Valid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("LLM_ENABLED", "true")
	t.Setenv("LLM_BASE_URL", "http://localhost:11434")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.LLM.Enabled)
	assert.Equal(t, "http://localhost:11434", cfg.LLM.BaseURL)
}

func TestLoad_LLM_EnabledWithoutBaseURL_Fails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("LLM_ENABLED", "true")
	// LLM_BASE_URL intentionally unset.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LLM_BASE_URL")
}

func TestLoad_LLM_DisabledWithoutBaseURL_Valid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("LLM_ENABLED", "false")
	// LLM_BASE_URL intentionally unset — must be fine when disabled.
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.LLM.Enabled)
	assert.Empty(t, cfg.LLM.BaseURL)
}
