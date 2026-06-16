package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ─── Meilisearch conditional validation ──────────────────────────────────────
//
// These tests follow the same t.Setenv pattern as the Firebase tests above.
// They must NOT be parallel because t.Setenv mutates process-wide state.

func TestLoad_MeilisearchURLRequiredInProduction(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	// FIREBASE also requires its config in production; supply a project so
	// we isolate the Meilisearch error.
	clearAmbientEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("FIREBASE_PROJECT_ID", "prod-project")
	t.Setenv("STORAGE_DIR", "/var/data")
	// MEILISEARCH_URL intentionally unset, no AllowUnconfigured.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MEILISEARCH_URL")
}

func TestLoad_MeilisearchURLRequiredInProduction_AllowUnconfiguredHasNoEscapeHatch(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	// AllowUnconfigured should NOT bypass the production requirement.
	clearAmbientEnv(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("FIREBASE_PROJECT_ID", "prod-project")
	t.Setenv("STORAGE_DIR", "/var/data")
	t.Setenv("MEILISEARCH_ALLOW_UNCONFIGURED", "true")
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MEILISEARCH_URL")
}

func TestLoad_MeilisearchURLSet_AlwaysValid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("MEILISEARCH_URL", "http://localhost:7700")
	t.Setenv("MEILISEARCH_ALLOW_UNCONFIGURED", "") // clear the escape hatch
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:7700", cfg.Meilisearch.URL)
}

func TestLoad_MeilisearchAllowUnconfigured_DevelopmentValid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	clearAmbientEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("FIREBASE_PROJECT_ID", "dev-project")
	t.Setenv("MEILISEARCH_ALLOW_UNCONFIGURED", "true")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.Meilisearch.AllowUnconfigured)
	assert.Empty(t, cfg.Meilisearch.URL)
}

func TestLoad_MeilisearchAllowUnconfigured_StagingValid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	clearAmbientEnv(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("FIREBASE_ALLOW_UNCONFIGURED", "true")
	t.Setenv("MEILISEARCH_ALLOW_UNCONFIGURED", "true")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.Meilisearch.AllowUnconfigured)
}

func TestLoad_MeilisearchMissing_StagingFails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	clearAmbientEnv(t)
	t.Setenv("APP_ENV", "staging")
	t.Setenv("FIREBASE_ALLOW_UNCONFIGURED", "true")
	// No MEILISEARCH_URL and no AllowUnconfigured.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MEILISEARCH_URL")
}

func TestLoad_MeilisearchIndexNameDefaults(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "clientes", cfg.Meilisearch.IndexName)
}

func TestLoad_MeilisearchSyncIntervalDefaults(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "5m0s", cfg.Meilisearch.SyncInterval.String())
}
