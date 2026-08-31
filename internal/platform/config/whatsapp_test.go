package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ─── WhatsApp configuration ──────────────────────────────────────────────────
//
// These tests follow the same t.Setenv pattern as the LLM and Meilisearch
// tests. They must NOT be parallel because t.Setenv mutates process-wide
// state.

func TestLoad_WhatsApp_Defaults(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.WhatsApp.Enabled)
	assert.Equal(t, "v21.0", cfg.WhatsApp.APIVersion)
	assert.Equal(t, 15*time.Second, cfg.WhatsApp.Timeout)
	assert.Empty(t, cfg.WhatsApp.Token)
	assert.Empty(t, cfg.WhatsApp.PhoneNumberID)
	assert.Empty(t, cfg.WhatsApp.BusinessAccountID)
	assert.Empty(t, cfg.WhatsApp.AppSecret)
	assert.Empty(t, cfg.WhatsApp.BaseURL)
}

func TestLoad_WhatsApp_EnabledWithFullCreds_Valid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("WHATSAPP_ENABLED", "true")
	t.Setenv("WHATSAPP_TOKEN", "test-token")
	t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "1234567890")
	t.Setenv("WHATSAPP_BUSINESS_ACCOUNT_ID", "waba-1")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.True(t, cfg.WhatsApp.Enabled)
	assert.Equal(t, "test-token", cfg.WhatsApp.Token)
	assert.Equal(t, "1234567890", cfg.WhatsApp.PhoneNumberID)
	assert.Equal(t, "waba-1", cfg.WhatsApp.BusinessAccountID)
}

func TestLoad_WhatsApp_EnabledWithoutToken_Fails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("WHATSAPP_ENABLED", "true")
	t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "1234567890")
	t.Setenv("WHATSAPP_BUSINESS_ACCOUNT_ID", "waba-1")
	// WHATSAPP_TOKEN intentionally unset.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WHATSAPP_TOKEN")
}

func TestLoad_WhatsApp_EnabledWithoutPhoneNumberID_Fails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("WHATSAPP_ENABLED", "true")
	t.Setenv("WHATSAPP_TOKEN", "test-token")
	t.Setenv("WHATSAPP_BUSINESS_ACCOUNT_ID", "waba-1")
	// WHATSAPP_PHONE_NUMBER_ID intentionally unset.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WHATSAPP_PHONE_NUMBER_ID")
}

func TestLoad_WhatsApp_EnabledWithoutBusinessAccountID_Fails(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("WHATSAPP_ENABLED", "true")
	t.Setenv("WHATSAPP_TOKEN", "test-token")
	t.Setenv("WHATSAPP_PHONE_NUMBER_ID", "1234567890")
	// WHATSAPP_BUSINESS_ACCOUNT_ID intentionally unset.
	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WHATSAPP_BUSINESS_ACCOUNT_ID")
}

func TestLoad_WhatsApp_DisabledWithoutCreds_Valid(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("WHATSAPP_ENABLED", "false")
	// No WhatsApp credentials set — must be fine when disabled.
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.False(t, cfg.WhatsApp.Enabled)
}

func TestLoad_WhatsApp_BaseURLOverride(t *testing.T) { //nolint:paralleltest // uses t.Setenv
	setMinimal(t)
	t.Setenv("WHATSAPP_BASE_URL", "http://127.0.0.1:9999")
	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:9999", cfg.WhatsApp.BaseURL)
}
