package blobfs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
)

func newStore(t *testing.T) *blobfs.Store {
	t.Helper()
	store, err := blobfs.New(t.TempDir())
	require.NoError(t, err)
	return store
}

func TestNew_RequiresBaseDir(t *testing.T) {
	t.Parallel()

	_, err := blobfs.New("")
	assert.Error(t, err)

	_, err = blobfs.New("   ")
	assert.Error(t, err)
}

func TestNew_CreatesMissingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "nested", "blobs")

	store, err := blobfs.New(target)
	require.NoError(t, err)
	assert.Equal(t, target, store.BaseDir())

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNew_RejectsExistingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	file := filepath.Join(root, "not-a-dir")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	_, err := blobfs.New(file)
	assert.Error(t, err)
}

func TestSave_WritesAndCommitsAtomically(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	id := uuid.New()
	body := bytes.NewReader([]byte("multipart payload"))

	path, err := store.Save(context.Background(), id, body, 1024)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, id.String()+".bin"))

	got, err := os.ReadFile(path) //nolint:gosec // test-only
	require.NoError(t, err)
	assert.Equal(t, "multipart payload", string(got))

	// Mode should be 0o600 on platforms that honour it.
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestSave_RejectsOverflow(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	id := uuid.New()
	body := bytes.NewReader(bytes.Repeat([]byte("x"), 2048))

	_, err := store.Save(context.Background(), id, body, 1024)
	require.Error(t, err)
	assert.ErrorIs(t, err, failedintent.ErrBlobTooLarge)

	// No artifact left behind.
	entries, readErr := os.ReadDir(store.BaseDir())
	require.NoError(t, readErr)
	for _, entry := range entries {
		assert.False(t, strings.HasSuffix(entry.Name(), ".bin"),
			"overflow must not leave a committed .bin")
	}
}

func TestSave_RejectsNilID(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	_, err := store.Save(context.Background(), uuid.Nil, strings.NewReader("x"), 1024)
	assert.Error(t, err)
}

func TestSave_RejectsNonPositiveLimit(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	_, err := store.Save(context.Background(), uuid.New(), strings.NewReader("x"), 0)
	assert.Error(t, err)

	_, err = store.Save(context.Background(), uuid.New(), strings.NewReader("x"), -1)
	assert.Error(t, err)
}

func TestOpen_ReturnsBlobBytes(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	id := uuid.New()
	path, err := store.Save(context.Background(), id, strings.NewReader("hola"), 1024)
	require.NoError(t, err)

	rc, err := store.Open(context.Background(), path)
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, "hola", string(got))
}

func TestOpen_MissingFile_ReturnsErrBlobNotFound(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	bogus := filepath.Join(store.BaseDir(), uuid.NewString()+".bin")

	_, err := store.Open(context.Background(), bogus)
	require.Error(t, err)
	assert.ErrorIs(t, err, failedintent.ErrBlobNotFound)
}

func TestOpen_PathOutsideBaseDir_ReturnsErrBlobNotFound(t *testing.T) {
	t.Parallel()

	store := newStore(t)

	// Existing file outside baseDir: an attacker-controlled body_blob_path
	// must not be openable.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	_, err := store.Open(context.Background(), outside)
	require.Error(t, err)
	assert.ErrorIs(t, err, failedintent.ErrBlobNotFound)
}

func TestOpen_EmptyPath_ReturnsErrBlobNotFound(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	_, err := store.Open(context.Background(), "")
	assert.ErrorIs(t, err, failedintent.ErrBlobNotFound)
}

func TestDelete_RemovesBlob(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	id := uuid.New()
	path, err := store.Save(context.Background(), id, strings.NewReader("x"), 1024)
	require.NoError(t, err)

	require.NoError(t, store.Delete(context.Background(), path))

	_, err = os.Stat(path)
	assert.True(t, errors.Is(err, os.ErrNotExist), "blob must be gone")
}

func TestDelete_MissingFile_NoError(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	missing := filepath.Join(store.BaseDir(), uuid.NewString()+".bin")
	assert.NoError(t, store.Delete(context.Background(), missing))
}

func TestDelete_PathOutsideBaseDir_NoOp(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	outside := filepath.Join(t.TempDir(), "untouched.txt")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))

	require.NoError(t, store.Delete(context.Background(), outside))

	// File must still exist — Delete must not touch paths outside baseDir.
	_, err := os.Stat(outside)
	assert.NoError(t, err, "Delete must not reach outside baseDir")
}
