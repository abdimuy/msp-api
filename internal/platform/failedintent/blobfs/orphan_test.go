package blobfs_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
)

// fakeRef is a PathReferencer with canned output.
type fakeRef struct {
	paths []string
	err   error
}

func (f *fakeRef) ReferencedPaths(_ context.Context) ([]string, error) {
	return f.paths, f.err
}

func TestSweepOrphans_DeletesOnlyUnreferenced(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := context.Background()

	// One blob that will be referenced.
	keepID := uuid.New()
	keepPath, err := store.Save(ctx, keepID, strings.NewReader("keep"), 1024)
	require.NoError(t, err)

	// Two unreferenced blobs.
	orphan1ID := uuid.New()
	orphan1Path, err := store.Save(ctx, orphan1ID, strings.NewReader("orphan1"), 1024)
	require.NoError(t, err)

	orphan2ID := uuid.New()
	orphan2Path, err := store.Save(ctx, orphan2ID, strings.NewReader("orphan2"), 1024)
	require.NoError(t, err)

	ref := &fakeRef{paths: []string{keepPath}}
	report, err := blobfs.SweepOrphans(ctx, store, ref)
	require.NoError(t, err)
	assert.Equal(t, 3, report.Scanned)
	assert.Equal(t, 2, report.Deleted)

	// keep must still exist.
	_, statErr := os.Stat(keepPath)
	require.NoError(t, statErr)

	// orphans must be gone.
	_, statErr = os.Stat(orphan1Path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	_, statErr = os.Stat(orphan2Path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestSweepOrphans_SkipsTempFiles(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := context.Background()

	// Simulate an in-flight upload by creating a .upload-XXX.tmp file.
	tmp, err := os.CreateTemp(store.BaseDir(), ".upload-*.tmp")
	require.NoError(t, err)
	tmpName := tmp.Name()
	require.NoError(t, tmp.Close())

	// Sanity: the temp file must use the prefix the sweep skips.
	assert.True(t, strings.HasPrefix(filepath.Base(tmpName), ".upload-"))

	report, err := blobfs.SweepOrphans(ctx, store, &fakeRef{})
	require.NoError(t, err)
	assert.Equal(t, 0, report.Scanned, "in-flight temp must not be scanned as a blob")
	assert.Equal(t, 0, report.Deleted)

	// Temp must still exist.
	_, statErr := os.Stat(tmpName)
	assert.NoError(t, statErr, "in-flight temp must survive a sweep")
}

func TestSweepOrphans_IgnoresForeignFiles(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	ctx := context.Background()

	// A foreign file (e.g. a README dropped by ops) must be left alone.
	foreign := filepath.Join(store.BaseDir(), "README.md")
	require.NoError(t, os.WriteFile(foreign, []byte("ops notes"), 0o600))

	report, err := blobfs.SweepOrphans(ctx, store, &fakeRef{})
	require.NoError(t, err)
	assert.Equal(t, 0, report.Scanned)
	assert.Equal(t, 0, report.Deleted)

	_, statErr := os.Stat(foreign)
	assert.NoError(t, statErr, "foreign files must survive the sweep")
}

func TestSweepOrphans_RefErrorPropagates(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	bad := &fakeRef{err: errors.New("db down")}
	_, err := blobfs.SweepOrphans(context.Background(), store, bad)
	require.Error(t, err)
}

func TestSweepOrphans_NilArgsRejected(t *testing.T) {
	t.Parallel()

	store := newStore(t)
	_, err := blobfs.SweepOrphans(context.Background(), nil, &fakeRef{})
	require.Error(t, err)

	_, err = blobfs.SweepOrphans(context.Background(), store, nil)
	require.Error(t, err)
}
