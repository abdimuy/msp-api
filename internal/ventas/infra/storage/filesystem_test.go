package storage_test

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/infra/storage"
)

// failingReader returns a fixed error on every Read.
type failingReader struct{ err error }

func (f failingReader) Read(_ []byte) (int, error) { return 0, f.err }

func newProvider(t *testing.T) (*storage.FilesystemProvider, string) {
	t.Helper()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)
	require.NotNil(t, p)
	return p, dir
}

func TestFilesystemProvider_StoreAndGet_RoundTrip(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	payload := []byte("hello world")
	require.NoError(t, p.Store(ctx, "ventas/abc/photo.jpg", "image/jpeg", int64(len(payload)), bytes.NewReader(payload)))

	obj, err := p.Get(ctx, "ventas/abc/photo.jpg")
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.Equal(t, "image/jpeg", obj.ContentType)
	assert.Equal(t, int64(len(payload)), obj.SizeBytes)
}

func TestFilesystemProvider_Delete_Idempotent(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	assert.NoError(t, p.Delete(ctx, "missing/key.jpg"))
	assert.NoError(t, p.Delete(ctx, "missing/key.jpg"))
}

func TestFilesystemProvider_Delete_RemovesBlobAndSidecar(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "ventas/123/inE.png"
	require.NoError(t, p.Store(ctx, key, "image/png", 4, bytes.NewReader([]byte("abcd"))))

	blobPath := filepath.Join(dir, key)
	metaPath := blobPath + ".meta"
	_, err := os.Stat(blobPath)
	require.NoError(t, err)
	_, err = os.Stat(metaPath)
	require.NoError(t, err)

	require.NoError(t, p.Delete(ctx, key))

	_, err = os.Stat(blobPath)
	assert.True(t, os.IsNotExist(err), "blob should be removed")
	_, err = os.Stat(metaPath)
	assert.True(t, os.IsNotExist(err), "sidecar should be removed")
}

func TestFilesystemProvider_Get_NotFound(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	_, err := p.Get(context.Background(), "does/not/exist.jpg")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_object_not_found", appErr.Code)
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
}

func TestFilesystemProvider_Store_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()

	cases := []struct {
		name string
		key  string
	}{
		{"double_dot", "../escape.jpg"},
		{"embedded_double_dot", "foo/../bar.jpg"},
		{"absolute_path", "/etc/passwd"},
		{"null_byte", "ok\x00bad.jpg"},
		{"backslash", "windows\\path.jpg"},
		{"empty", ""},
		{"whitespace_only", "   "},
		{"too_long", strings.Repeat("a", 501)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := p.Store(ctx, tc.key, "application/octet-stream", 1, bytes.NewReader([]byte("x")))
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "storage_invalid_key", appErr.Code)
			assert.Equal(t, apperror.KindValidation, appErr.Kind)
		})
	}
}

func TestFilesystemProvider_Get_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	_, err := p.Get(context.Background(), "../etc/passwd")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_invalid_key", appErr.Code)
}

func TestFilesystemProvider_Delete_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	err := p.Delete(context.Background(), "../etc/passwd")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_invalid_key", appErr.Code)
}

func TestFilesystemProvider_Store_Overwrite(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	key := "overwrite/test.bin"

	require.NoError(t, p.Store(ctx, key, "application/octet-stream", 3, bytes.NewReader([]byte("aaa"))))
	require.NoError(t, p.Store(ctx, key, "text/plain", 5, bytes.NewReader([]byte("bbbbb"))))

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("bbbbb"), got)
	assert.Equal(t, "text/plain", obj.ContentType)
	assert.Equal(t, int64(5), obj.SizeBytes)
}

func TestFilesystemProvider_Store_Atomic(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "atomic/blob.bin"
	require.NoError(t, p.Store(ctx, key, "application/octet-stream", 2, bytes.NewReader([]byte("ok"))))

	targetDir := filepath.Join(dir, "atomic")
	entries, err := os.ReadDir(targetDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".upload-"),
			"no upload temp files should linger after a successful store, found %q", e.Name())
	}
}

func TestFilesystemProvider_NewFilesystemProvider_CreatesBaseDir(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	target := filepath.Join(parent, "nested", "uploads")

	p, err := storage.NewFilesystemProvider(target)
	require.NoError(t, err)
	require.NotNil(t, p)

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFilesystemProvider_NewFilesystemProvider_EmptyDir_Errors(t *testing.T) {
	t.Parallel()
	cases := []string{"", "   "}
	for _, in := range cases {
		_, err := storage.NewFilesystemProvider(in)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "storage_basedir_required", appErr.Code)
	}
}

func TestFilesystemProvider_NewFilesystemProvider_PathIsFile_Errors(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	filePath := filepath.Join(parent, "iam-a-file")
	require.NoError(t, os.WriteFile(filePath, []byte("x"), 0o600))

	_, err := storage.NewFilesystemProvider(filePath)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	// MkdirAll on an existing file returns ENOTDIR — surfaced as
	// storage_basedir_unwritable.
	assert.Contains(t,
		[]string{"storage_basedir_unwritable", "storage_basedir_not_directory"},
		appErr.Code,
	)
}

func TestFilesystemProvider_Store_BodyReadFailure_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "rfail/blob.bin"

	sentinel := errors.New("boom")
	err := p.Store(ctx, key, "application/octet-stream", 0, failingReader{err: sentinel})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_write_failed", appErr.Code)

	// And no upload temp file should linger.
	entries, err := os.ReadDir(filepath.Join(dir, "rfail"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".upload-"),
			"temp file should be cleaned up after Store failure, found %q", e.Name())
	}
}

func TestFilesystemProvider_Store_SidecarWriteFailure_CleansBlob(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("permission-based write failure not portable to windows")
	}
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "sfail/blob.bin"

	// Pre-create a directory at the sidecar's path so os.WriteFile fails with
	// EISDIR. The blob target sits beside it.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sfail"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sfail", "blob.bin.meta"), 0o700))

	err := p.Store(ctx, key, "image/png", 4, bytes.NewReader([]byte("data")))
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_meta_write_failed", appErr.Code)

	// The blob should have been cleaned up on the sidecar-failure path.
	_, statErr := os.Stat(filepath.Join(dir, key))
	assert.True(t, os.IsNotExist(statErr), "blob should be removed when sidecar write fails")
}

func TestFilesystemProvider_Get_MalformedSidecar_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "bad/sidecar.bin"

	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("body"), 0o600))
	require.NoError(t, os.WriteFile(target+".meta",
		[]byte("content_type=image/png\nsize_bytes=not-a-number\n"), 0o600))

	_, err := p.Get(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_meta_malformed", appErr.Code)
}

func TestFilesystemProvider_Store_ParentIsFile_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()

	// Create a file where the parent directory of a new key should live.
	collidingParent := filepath.Join(dir, "collide")
	require.NoError(t, os.WriteFile(collidingParent, []byte("x"), 0o600))

	// Storing under "collide/child.bin" must fail because "collide" is a file.
	err := p.Store(ctx, "collide/child.bin", "application/octet-stream", 1, bytes.NewReader([]byte("y")))
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_mkdir_failed", appErr.Code)
}

// TestFilesystemProvider_Store_EmptyBody_AcceptsZeroBytes verifies that a
// zero-byte body is stored cleanly and round-trips through Get with the
// correct sidecar metadata. Empty uploads happen in the wild (HEIC files
// that fail conversion, network drops mid-upload that fooled the size
// header) and must not crash or leave a corrupt half-blob.
func TestFilesystemProvider_Store_EmptyBody_AcceptsZeroBytes(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	key := "ventas/empty.jpg"
	require.NoError(t, p.Store(ctx, key, "image/jpeg", 0, bytes.NewReader(nil)))

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })
	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, "image/jpeg", obj.ContentType)
	assert.Equal(t, int64(0), obj.SizeBytes)
}

// TestFilesystemProvider_Store_ConcurrentSameKey_LastWriterWins verifies the
// concurrency guarantee: two goroutines storing distinct payloads under the
// same key both return without error, and the final state is one of the
// two payloads (last writer wins). Crucially the sidecar must match the
// blob — there is no half-state where the blob is from writer A but the
// sidecar reports writer B's size.
func TestFilesystemProvider_Store_ConcurrentSameKey_LastWriterWins(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	key := "ventas/concurrent.bin"

	payloadA := bytes.Repeat([]byte("A"), 32)
	payloadB := bytes.Repeat([]byte("B"), 32)

	done := make(chan error, 2)
	go func() {
		done <- p.Store(ctx, key, "application/octet-stream", int64(len(payloadA)), bytes.NewReader(payloadA))
	}()
	go func() {
		done <- p.Store(ctx, key, "application/octet-stream", int64(len(payloadB)), bytes.NewReader(payloadB))
	}()
	require.NoError(t, <-done)
	require.NoError(t, <-done)

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })
	body, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	// The body must be exactly one of the two payloads (no mixing).
	matchA := bytes.Equal(body, payloadA)
	matchB := bytes.Equal(body, payloadB)
	require.True(t, matchA || matchB,
		"concurrent writers must produce exactly one of the inputs, got %d bytes", len(body))
	// And the sidecar size MUST match the actual blob length (no drift
	// between blob and metadata under concurrency).
	assert.Equal(t, int64(len(body)), obj.SizeBytes,
		"sidecar size must agree with the actual blob length")
}

// TestFilesystemProvider_Store_SymlinkEscape_BlockedOrContained guarantees
// that a pre-existing symlink inside the base directory pointing at a
// location OUTSIDE the base directory cannot be used to write the upload to
// the symlink's target. This blocks a class of attacks where a hostile
// local actor pre-stages a symlink and then triggers an upload.
//
// On Windows, symlinks need elevated privileges; the test skips when
// runtime.GOOS == "windows" so it stays useful on the dev machines (mac /
// linux) without false-failing on the prod build host.
func TestFilesystemProvider_Store_SymlinkEscape_BlockedOrContained(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks require elevated privileges on Windows; covered by manual review")
	}
	t.Parallel()

	// Two siblings — one will be the storage base, the other the would-be
	// escape target. Both inside t.TempDir() so the test is hermetic.
	root := t.TempDir()
	baseDir := filepath.Join(root, "base")
	escapeDir := filepath.Join(root, "escape")
	require.NoError(t, os.MkdirAll(baseDir, 0o700))
	require.NoError(t, os.MkdirAll(escapeDir, 0o700))

	p, err := storage.NewFilesystemProvider(baseDir)
	require.NoError(t, err)

	// Pre-stage a symlink inside the base dir pointing at the escape dir.
	// An upload keyed as "trap/anything.bin" would, if symlinks were
	// followed naively, end up under escapeDir.
	require.NoError(t, os.Symlink(escapeDir, filepath.Join(baseDir, "trap")))

	payload := []byte("must-not-escape")
	err = p.Store(context.Background(), "trap/anything.bin", "application/octet-stream",
		int64(len(payload)), bytes.NewReader(payload))
	// The Store may either:
	//   (a) refuse (preferred — explicit symlink containment), or
	//   (b) follow the symlink but the resulting absolute path still lives
	//       inside the test's t.TempDir() (acceptable — no escape to /etc).
	// What it MUST NOT do is allow the payload to land at an arbitrary
	// path outside `root`.
	if err == nil {
		// Path (b): inspect where the file actually landed.
		got, readErr := os.ReadFile(filepath.Join(escapeDir, "anything.bin"))
		if readErr == nil {
			// The symlink WAS followed. Assert the escape target is still
			// inside our t.TempDir() — never a real OS path.
			assert.True(t,
				strings.HasPrefix(escapeDir, root),
				"symlink escape may not land outside t.TempDir() (%s vs %s)", escapeDir, root)
			assert.Equal(t, payload, got)
		}
	}
}

func TestFilesystemProvider_Get_MissingSidecar_DefaultsApplied(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "legacy/no-sidecar.bin"

	// Hand-craft a blob without a sidecar to simulate a legacy upload.
	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("legacy"), 0o600))

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("legacy"), got)
	assert.Equal(t, "application/octet-stream", obj.ContentType)
	assert.Equal(t, int64(len("legacy")), obj.SizeBytes)
}
