package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/comprobantes/infra/storage"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// failingReader returns a fixed error on every Read.
type failingReader struct{ err error }

func (f failingReader) Read(_ []byte) (int, error) { return 0, f.err }

// partialFailingReader returns up to prefix bytes from underlying then
// returns err on the next Read. Mimics ENOSPC or a network drop mid-payload.
type partialFailingReader struct {
	underlying io.Reader
	prefix     int64
	read       int64
	err        error
}

func (r *partialFailingReader) Read(p []byte) (int, error) {
	if r.read >= r.prefix {
		return 0, r.err
	}
	remaining := r.prefix - r.read
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := r.underlying.Read(p)
	r.read += int64(n)
	if err != nil {
		return n, err
	}
	return n, nil
}

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
	payload := []byte("%PDF-1.7 mock receipt")
	require.NoError(t, p.Store(ctx, "comprobantes/2026/venta-1.pdf", "application/pdf",
		int64(len(payload)), bytes.NewReader(payload)))

	obj, err := p.Get(ctx, "comprobantes/2026/venta-1.pdf")
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.Equal(t, "application/pdf", obj.ContentType)
	assert.Equal(t, int64(len(payload)), obj.SizeBytes)
}

func TestFilesystemProvider_Store_LargeBlob_RoundTrip(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	payload := bytes.Repeat([]byte{0xAA}, 2<<20) // 2 MB, pdf-with-images size
	require.NoError(t, p.Store(ctx, "comprobantes/big.pdf", "application/pdf",
		int64(len(payload)), bytes.NewReader(payload)))

	obj, err := p.Get(ctx, "comprobantes/big.pdf")
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
	assert.Equal(t, int64(len(payload)), obj.SizeBytes)
}

func TestFilesystemProvider_Store_Overwrite(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/overwrite.pdf"

	require.NoError(t, p.Store(ctx, key, "application/pdf", 3, bytes.NewReader([]byte("aaa"))))
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

func TestFilesystemProvider_Get_NotFound(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	_, err := p.Get(context.Background(), "does/not/exist.pdf")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_object_not_found", appErr.Code)
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
}

func TestFilesystemProvider_Delete_Idempotent(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	assert.NoError(t, p.Delete(ctx, "missing/key.pdf"))
	assert.NoError(t, p.Delete(ctx, "missing/key.pdf"))
}

func TestFilesystemProvider_Delete_RemovesBlobAndSidecar(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/123/pago.pdf"
	require.NoError(t, p.Store(ctx, key, "application/pdf", 4, bytes.NewReader([]byte("abcd"))))

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

// invalidKeyCases is the full catalog of keys the provider must reject.
// The thermometer of this deliverable: every case must ALSO leave the base
// directory untouched — returning an error is not enough, a provider that
// rejects the key after writing the temp file would pass and leave garbage.
var invalidKeyCases = []struct {
	name string
	key  string
}{
	{"double_dot", "../escape.pdf"},
	{"embedded_double_dot", "foo/../bar.pdf"},
	{"absolute_path", "/etc/passwd"},
	{"null_byte", "ok\x00bad.pdf"},
	{"backslash", `windows\path.pdf`},
	{"empty", ""},
	{"whitespace_only", "   "},
	{"too_long", strings.Repeat("a", 501)},
}

func TestFilesystemProvider_Store_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	for _, tc := range invalidKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := p.Store(context.Background(), tc.key, "application/pdf", 1, bytes.NewReader([]byte("x")))
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
	for _, tc := range invalidKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := p.Get(context.Background(), tc.key)
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "storage_invalid_key", appErr.Code)
		})
	}
}

func TestFilesystemProvider_Delete_PathTraversal_Rejected(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	for _, tc := range invalidKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := p.Delete(context.Background(), tc.key)
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "storage_invalid_key", appErr.Code)
		})
	}
}

// TestFilesystemProvider_InvalidKey_LeavesNoFiles is the thermometer: for
// every rejected key the base directory must have zero entries afterwards.
func TestFilesystemProvider_InvalidKey_LeavesNoFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range invalidKeyCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p, err := storage.NewFilesystemProvider(dir)
			require.NoError(t, err)

			require.Error(t, p.Store(ctx, tc.key, "application/pdf", 1, bytes.NewReader([]byte("x"))))

			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			assert.Empty(t, entries, "no file may be created for invalid key %q", tc.key)
		})
	}
}

func TestFilesystemProvider_Store_Atomic(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/atomic.pdf"
	require.NoError(t, p.Store(ctx, key, "application/pdf", 2, bytes.NewReader([]byte("ok"))))

	targetDir := filepath.Join(dir, "comprobantes")
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
	target := filepath.Join(parent, "nested", "receipts")

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
	key := "comprobantes/rfail.pdf"

	sentinel := errors.New("boom")
	err := p.Store(ctx, key, "application/pdf", 0, failingReader{err: sentinel})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_write_failed", appErr.Code)

	entries, err := os.ReadDir(filepath.Join(dir, "comprobantes"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".upload-"),
			"temp file should be cleaned up after Store failure, found %q", e.Name())
	}
}

// TestFilesystemProvider_Store_PartialBodyAfterNBytes_NoOrphan simulates an
// ENOSPC-style failure: the reader hands out the first 64 bytes of a 256
// byte payload then errors. Store must (a) surface the error, (b) leave no
// upload temp file behind, (c) leave no .meta sidecar, and (d) leave no
// partial blob at the target key.
func TestFilesystemProvider_Store_PartialBodyAfterNBytes_NoOrphan(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/2026/09/partial.pdf"

	full := bytes.Repeat([]byte{0xAA}, 256)
	body := &partialFailingReader{
		underlying: bytes.NewReader(full),
		prefix:     64,
		err:        errors.New("simulated ENOSPC"),
	}
	err := p.Store(ctx, key, "application/pdf", int64(len(full)), body)
	require.Error(t, err, "Store must surface a mid-stream read failure")
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_write_failed", appErr.Code)

	parent := filepath.Join(dir, "comprobantes/2026/09")
	entries, err := os.ReadDir(parent)
	if err == nil {
		for _, e := range entries {
			assert.False(t, strings.HasPrefix(e.Name(), ".upload-"),
				"temp file must be cleaned up after partial-body failure: %q", e.Name())
		}
	}
	_, statErr := os.Stat(filepath.Join(dir, key+".meta"))
	assert.True(t, os.IsNotExist(statErr), "no .meta sidecar may exist when the payload write failed")
	_, statErr = os.Stat(filepath.Join(dir, key))
	assert.True(t, os.IsNotExist(statErr), "no partial blob may exist at the target when Store failed mid-stream")
}

func TestFilesystemProvider_Store_SidecarWriteFailure_CleansBlob(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/sfail.pdf"

	// Pre-create a directory at the sidecar's path so os.WriteFile fails
	// (EISDIR on unix, access denied on windows) — no permission tricks needed.
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "comprobantes"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "comprobantes", "sfail.pdf.meta"), 0o700))

	err := p.Store(ctx, key, "application/pdf", 4, bytes.NewReader([]byte("data")))
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_meta_write_failed", appErr.Code)

	_, statErr := os.Stat(filepath.Join(dir, key))
	assert.True(t, os.IsNotExist(statErr), "blob should be removed when sidecar write fails")
}

func TestFilesystemProvider_Get_MalformedSidecar_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/bad.pdf"

	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("body"), 0o600))
	require.NoError(t, os.WriteFile(target+".meta",
		[]byte("content_type=application/pdf\nsize_bytes=not-a-number\n"), 0o600))

	_, err := p.Get(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_meta_malformed", appErr.Code)
}

// TestFilesystemProvider_Get_SidecarLineWithoutEquals_Skipped verifies a
// stray line without a "=" separator in the sidecar is skipped, not fatal.
func TestFilesystemProvider_Get_SidecarLineWithoutEquals_Skipped(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/legacy-line.pdf"

	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("body"), 0o600))
	require.NoError(t, os.WriteFile(target+".meta", []byte(
		"garbage-without-equals\ncontent_type=application/pdf\nsize_bytes=4\n"), 0o600))

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })
	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, []byte("body"), got)
	assert.Equal(t, "application/pdf", obj.ContentType)
	assert.Equal(t, int64(4), obj.SizeBytes)
}

// TestFilesystemProvider_Get_ScannerTooLong_Errors forces bufio.Scanner to
// hit its max token size (a >64KB sidecar line) so the reader surfaces
// storage_meta_read_failed.
func TestFilesystemProvider_Get_ScannerTooLong_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/huge-line.pdf"

	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("body"), 0o600))
	longLine := strings.Repeat("x", 70<<10)
	require.NoError(t, os.WriteFile(target+".meta",
		[]byte("content_type="+longLine+"\n"), 0o600))

	_, err := p.Get(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_meta_read_failed", appErr.Code)
}

func TestFilesystemProvider_Delete_BlobRemoveFails_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	// A non-empty directory at the blob path makes os.Remove fail (ENOTEMPTY
	// on unix, access denied on windows) — portably triggerable.
	key := "comprobantes/delfail"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, key, "child"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, key, "child", "x"), []byte("x"), 0o600))

	err := p.Delete(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_delete_failed", appErr.Code)
}

func TestFilesystemProvider_Delete_MetaRemoveFails_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/metafail.pdf"
	// Hand-crafted blob (no sidecar) plus a blocking non-empty directory at
	// the would-be sidecar path, so Delete removes the blob but fails on the
	// sidecar.
	blobPath := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(blobPath), 0o700))
	require.NoError(t, os.WriteFile(blobPath, []byte("x"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, key+".meta", "child"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, key+".meta", "child", "x"), []byte("x"), 0o600))

	err := p.Delete(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_delete_meta_failed", appErr.Code)
}

// TestFilesystemProvider_Store_RenameOverDirectory_Errors replaces the target
// with a pre-existing directory so the final rename of the temp file onto it
// fails (EISDIR on unix, access denied on windows): the temp is cleaned up.
func TestFilesystemProvider_Store_RenameOverDirectory_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/renfail.pdf"

	targetDir := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(targetDir, 0o700))

	err := p.Store(ctx, key, "application/pdf", 4, bytes.NewReader([]byte("data")))
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_rename_failed", appErr.Code)

	entries, err := os.ReadDir(filepath.Join(dir, "comprobantes"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".upload-"),
			"temp file must be cleaned up after rename failure, found %q", e.Name())
	}
}

func TestFilesystemProvider_Store_ParentIsFile_Errors(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()

	collidingParent := filepath.Join(dir, "collide")
	require.NoError(t, os.WriteFile(collidingParent, []byte("x"), 0o600))

	err := p.Store(ctx, "collide/child.pdf", "application/pdf", 1, bytes.NewReader([]byte("y")))
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_mkdir_failed", appErr.Code)
}

func TestFilesystemProvider_Store_EmptyBody_AcceptsZeroBytes(t *testing.T) {
	t.Parallel()
	p, _ := newProvider(t)
	ctx := context.Background()
	key := "comprobantes/empty.pdf"
	require.NoError(t, p.Store(ctx, key, "application/pdf", 0, bytes.NewReader(nil)))

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = obj.Body.Close() })
	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, "application/pdf", obj.ContentType)
	assert.Equal(t, int64(0), obj.SizeBytes)
}

func TestFilesystemProvider_Get_MissingSidecar_DefaultsApplied(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()
	key := "legacy/no-sidecar.pdf"

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
