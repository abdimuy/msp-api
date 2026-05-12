package storage_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
)

func TestNew_ReturnsFilesystemProvider(t *testing.T) {
	t.Parallel()
	cfg := config.Storage{Dir: t.TempDir()}
	provider, err := storage.New(cfg)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Equal(t, "*storage.FilesystemProvider", fmt.Sprintf("%T", provider))
}

func TestNew_EmptyDir_Errors(t *testing.T) {
	t.Parallel()
	cfg := config.Storage{Dir: ""}
	provider, err := storage.New(cfg)
	require.Error(t, err)
	assert.Nil(t, provider)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_basedir_required", appErr.Code)
}
