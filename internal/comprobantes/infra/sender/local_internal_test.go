package sender

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// notDirInfo is an os.FileInfo whose IsDir() is false. It satisfies the full
// interface (only IsDir is read by the constructor) so the defensive branch
// that rejects a non-directory base path is reachable in tests.
type notDirInfo struct{}

func (notDirInfo) Name() string       { return "file" }
func (notDirInfo) Size() int64        { return 0 }
func (notDirInfo) Mode() os.FileMode  { return 0o600 }
func (notDirInfo) ModTime() time.Time { return time.Time{} }
func (notDirInfo) Sys() interface{}   { return nil }
func (notDirInfo) IsDir() bool        { return false }

func TestNewLocalSender_StatError_Unreadable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	statFn := func(string) (os.FileInfo, error) {
		return nil, errors.New("stat failed")
	}

	_, err := newLocalSender(dir, statFn)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "sender_basedir_unreadable", appErr.Code)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
}

func TestNewLocalSender_NotDirectory_Rejected(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "outbox")
	statFn := func(string) (os.FileInfo, error) {
		return notDirInfo{}, nil
	}

	_, err := newLocalSender(dir, statFn)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "sender_basedir_not_directory", appErr.Code)
	assert.Equal(t, apperror.KindValidation, appErr.Kind)
}
