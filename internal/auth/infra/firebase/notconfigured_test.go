package firebase_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/infra/firebase"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func TestNotConfiguredClient_AlwaysUnauthorized(t *testing.T) {
	t.Parallel()
	c := firebase.NewNotConfiguredClient()
	_, err := c.VerifyIDToken(context.Background(), "anything")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "firebase_not_configured", appErr.Code)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
}

func TestNotConfiguredClient_EmptyTokenAlsoUnauthorized(t *testing.T) {
	t.Parallel()
	c := firebase.NewNotConfiguredClient()
	tok, err := c.VerifyIDToken(context.Background(), "")
	require.Error(t, err)
	assert.Nil(t, tok)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindUnauthorized, appErr.Kind)
}

func TestNotConfiguredClient_DisableUser_ReturnsInternalAppError(t *testing.T) {
	t.Parallel()
	c := firebase.NewNotConfiguredClient()
	err := c.DisableUser(context.Background(), "any-uid")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
	assert.Equal(t, "firebase_not_configured", appErr.Code)
}

func TestNotConfiguredClient_EnableUser_ReturnsInternalAppError(t *testing.T) {
	t.Parallel()
	c := firebase.NewNotConfiguredClient()
	err := c.EnableUser(context.Background(), "any-uid")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
	assert.Equal(t, "firebase_not_configured", appErr.Code)
}
