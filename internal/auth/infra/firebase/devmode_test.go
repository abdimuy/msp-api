package firebase

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

func TestParse_HappyPath_UIDOnly(t *testing.T) {
	t.Parallel()
	uid, email, err := parseDevModeToken("dev:alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", uid)
	assert.Empty(t, email)
}

func TestParse_HappyPath_WithEmail(t *testing.T) {
	t.Parallel()
	uid, email, err := parseDevModeToken("dev:alice:alice@x.com")
	require.NoError(t, err)
	assert.Equal(t, "alice", uid)
	assert.Equal(t, "alice@x.com", email)
}

func TestParse_RefusesMissingPrefix(t *testing.T) {
	t.Parallel()
	_, _, err := parseDevModeToken("alice")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
	assert.Equal(t, "firebase_token_invalid", appErr.Code)
}

func TestParse_RefusesMissingPrefix_Empty(t *testing.T) {
	t.Parallel()
	_, _, err := parseDevModeToken("")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
}

func TestParse_RefusesEmptyUID_BareDev(t *testing.T) {
	t.Parallel()
	_, _, err := parseDevModeToken("dev:")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
	assert.Equal(t, "firebase_token_invalid", appErr.Code)
}

func TestParse_RefusesEmptyUID_ColonEmail(t *testing.T) {
	t.Parallel()
	_, _, err := parseDevModeToken("dev::alice@x.com")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
}

func TestParse_RefusesNonPrintableUID(t *testing.T) {
	t.Parallel()
	// space (0x20) is below the printable range we accept.
	_, _, err := parseDevModeToken("dev:al ice")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
	assert.Equal(t, "firebase_token_invalid", appErr.Code)

	// control character.
	_, _, err = parseDevModeToken("dev:al\x01ice")
	require.Error(t, err)

	// DEL (0x7F) is above the printable range.
	_, _, err = parseDevModeToken("dev:al\x7Fice")
	require.Error(t, err)
}

func TestVerify_ReturnsTokenWithExpiry(t *testing.T) {
	t.Parallel()
	c, err := NewDevModeClient(config.EnvDevelopment)
	require.NoError(t, err)
	tok, err := c.VerifyIDToken(context.Background(), "dev:alice:alice@x.com")
	require.NoError(t, err)
	require.NotNil(t, tok)
	assert.Equal(t, "alice", tok.UID)
	assert.Equal(t, "alice@x.com", tok.Email)
	assert.True(t, tok.ExpiresAt.After(tok.IssuedAt),
		"ExpiresAt (%s) must be after IssuedAt (%s)", tok.ExpiresAt, tok.IssuedAt)
}

func TestVerify_RejectsMalformedToken(t *testing.T) {
	t.Parallel()
	c, err := NewDevModeClient(config.EnvDevelopment)
	require.NoError(t, err)
	_, err = c.VerifyIDToken(context.Background(), "not-a-dev-token")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
}

func TestNewDevModeClient_RefusesProduction(t *testing.T) {
	t.Parallel()
	c, err := NewDevModeClient(config.EnvProduction)
	require.Error(t, err)
	assert.Nil(t, c)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
	assert.Equal(t, "firebase_devmode_not_permitted", appErr.Code)
}

func TestNewDevModeClient_RefusesStaging(t *testing.T) {
	t.Parallel()
	c, err := NewDevModeClient(config.EnvStaging)
	require.Error(t, err)
	assert.Nil(t, c)
}

func TestNewDevModeClient_RefusesTest(t *testing.T) {
	t.Parallel()
	c, err := NewDevModeClient(config.EnvTest)
	require.Error(t, err)
	assert.Nil(t, c)
}

func TestNewDevModeClient_AcceptsDevelopment(t *testing.T) {
	t.Parallel()
	c, err := NewDevModeClient(config.EnvDevelopment)
	require.NoError(t, err)
	require.NotNil(t, c)
}
