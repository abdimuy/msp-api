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

	"github.com/abdimuy/msp-api/internal/garantias/infra/storage"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// entryCount returns the number of directory entries directly under dir.
// Used to assert that a rejected Store call left zero filesystem artifacts.
func entryCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	return len(entries)
}

func newProvider(t *testing.T) (*storage.FilesystemProvider, string) {
	t.Helper()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)
	require.NotNil(t, p)
	return p, dir
}

func TestFilesystemProvider_StoreThenGet_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	ctx := context.Background()
	key := "evidencia/abc123.jpg"
	content := []byte("contenido de la imagen")

	err = p.Store(ctx, key, "image/jpeg", int64(len(content)), bytes.NewReader(content))
	require.NoError(t, err)

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	defer func() { _ = obj.Body.Close() }()

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)

	assert.Equal(t, content, got)
	assert.Equal(t, "image/jpeg", obj.ContentType)
	assert.Equal(t, int64(len(content)), obj.SizeBytes)
}

func TestFilesystemProvider_Store_Overwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	ctx := context.Background()
	key := "evidencia/overwrite.png"

	first := []byte("primer contenido")
	err = p.Store(ctx, key, "image/png", int64(len(first)), bytes.NewReader(first))
	require.NoError(t, err)

	second := []byte("segundo contenido, más largo que el primero")
	err = p.Store(ctx, key, "image/png", int64(len(second)), bytes.NewReader(second))
	require.NoError(t, err)

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	defer func() { _ = obj.Body.Close() }()

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)

	assert.Equal(t, second, got)
	assert.Equal(t, int64(len(second)), obj.SizeBytes)
}

func TestFilesystemProvider_Get_NotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	_, err = p.Get(context.Background(), "no/existe.jpg")
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_object_not_found", appErr.Code)
	assert.Equal(t, apperror.KindNotFound, appErr.Kind)
}

func TestFilesystemProvider_Delete_ExistingKey_RemovesBlobAndMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	ctx := context.Background()
	key := "evidencia/delete-me.jpg"
	content := []byte("borrame")

	require.NoError(t, p.Store(ctx, key, "image/jpeg", int64(len(content)), bytes.NewReader(content)))

	blobPath := filepath.Join(dir, key)
	metaPath := blobPath + ".meta"
	_, statErr := os.Stat(blobPath)
	require.NoError(t, statErr)
	_, statErr = os.Stat(metaPath)
	require.NoError(t, statErr)

	require.NoError(t, p.Delete(ctx, key))

	_, statErr = os.Stat(blobPath)
	assert.True(t, os.IsNotExist(statErr))
	_, statErr = os.Stat(metaPath)
	assert.True(t, os.IsNotExist(statErr))

	_, err = p.Get(ctx, key)
	require.Error(t, err)
}

func TestFilesystemProvider_Delete_MissingKey_IsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	err = p.Delete(context.Background(), "nunca/existio.jpg")
	assert.NoError(t, err)
}

func TestFilesystemProvider_Store_InvalidKeys_RejectedAndNoFilesCreated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{"path_traversal", "../escape.jpg"},
		{"embedded_double_dot", "foo/../bar.jpg"},
		{"whitespace_only", "   "},
		{"null_byte", "evidencia/\x00nullbyte.jpg"},
		{"absolute_path", "/etc/passwd"},
		{"backslash", `evidencia\windows.jpg`},
		{"empty", ""},
		{"too_long", strings.Repeat("a", 501)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p, err := storage.NewFilesystemProvider(dir)
			require.NoError(t, err)

			content := []byte("no debería escribirse")
			err = p.Store(context.Background(), tc.key, "image/jpeg", int64(len(content)), bytes.NewReader(content))
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "storage_invalid_key", appErr.Code)
			assert.Equal(t, apperror.KindValidation, appErr.Kind)
			assert.Equal(t, 0, entryCount(t, dir), "no debería crearse ningún archivo en el directorio base")
		})
	}
}

func TestFilesystemProvider_Get_InvalidKeys_Rejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{"path_traversal", "../escape.jpg"},
		{"embedded_double_dot", "foo/../bar.jpg"},
		{"whitespace_only", "   "},
		{"null_byte", "evidencia/\x00nullbyte.jpg"},
		{"absolute_path", "/etc/passwd"},
		{"backslash", `evidencia\windows.jpg`},
		{"empty", ""},
		{"too_long", strings.Repeat("a", 501)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p, err := storage.NewFilesystemProvider(dir)
			require.NoError(t, err)

			_, err = p.Get(context.Background(), tc.key)
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "storage_invalid_key", appErr.Code)
			assert.Equal(t, apperror.KindValidation, appErr.Kind)
		})
	}
}

func TestFilesystemProvider_Delete_InvalidKeys_Rejected(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{"path_traversal", "../escape.jpg"},
		{"embedded_double_dot", "foo/../bar.jpg"},
		{"whitespace_only", "   "},
		{"null_byte", "evidencia/\x00nullbyte.jpg"},
		{"absolute_path", "/etc/passwd"},
		{"backslash", `evidencia\windows.jpg`},
		{"empty", ""},
		{"too_long", strings.Repeat("a", 501)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			p, err := storage.NewFilesystemProvider(dir)
			require.NoError(t, err)

			err = p.Delete(context.Background(), tc.key)
			require.Error(t, err)
			appErr, ok := apperror.As(err)
			require.True(t, ok)
			assert.Equal(t, "storage_invalid_key", appErr.Code)
			assert.Equal(t, apperror.KindValidation, appErr.Kind)
		})
	}
}

func TestNewFilesystemProvider_EmptyBaseDir_ReturnsError(t *testing.T) {
	t.Parallel()
	_, err := storage.NewFilesystemProvider("")
	require.Error(t, err)

	_, err = storage.NewFilesystemProvider("   ")
	require.Error(t, err)
}

func TestNewFilesystemProvider_CreatesMissingDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "nested", "does", "not", "exist", "yet")

	_, statErr := os.Stat(target)
	require.True(t, os.IsNotExist(statErr))

	p, err := storage.NewFilesystemProvider(target)
	require.NoError(t, err)
	require.NotNil(t, p)

	info, statErr := os.Stat(target)
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestNewFilesystemProvider_BaseDirIsAFile_ReturnsError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	filePath := filepath.Join(root, "soy-un-archivo")
	require.NoError(t, os.WriteFile(filePath, []byte("contenido"), 0o600))

	_, err := storage.NewFilesystemProvider(filePath)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_basedir_unwritable", appErr.Code)
	// La rama '!info.IsDir()' es defensiva y no se alcanza en este caso porque
	// MkdirAll falla antes; queda cubierta por la propia lógica de os.MkdirAll.
}

func TestFilesystemProvider_Store_CreatesNestedDirectories(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	ctx := context.Background()
	key := "a/b/c/nested.jpg"
	content := []byte("nested content")

	err = p.Store(ctx, key, "image/jpeg", int64(len(content)), bytes.NewReader(content))
	require.NoError(t, err)

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	defer func() { _ = obj.Body.Close() }()

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestFilesystemProvider_Get_MissingSidecar_FallsBackToDefaults(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	key := "sin-sidecar.bin"
	content := []byte("blob sin metadata")
	require.NoError(t, os.WriteFile(filepath.Join(dir, key), content, 0o600))

	obj, err := p.Get(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = obj.Body.Close() }()

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)

	assert.Equal(t, content, got)
	assert.Equal(t, "application/octet-stream", obj.ContentType)
	assert.Equal(t, int64(len(content)), obj.SizeBytes)
}

func TestFilesystemProvider_Get_MalformedSidecar_ReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	key := "con-sidecar-roto.bin"
	require.NoError(t, os.WriteFile(filepath.Join(dir, key), []byte("blob"), 0o600))
	badMeta := "content_type=image/jpeg\nsize_bytes=no-es-un-numero\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, key+".meta"), []byte(badMeta), 0o600))

	_, err = p.Get(context.Background(), key)
	require.Error(t, err)
}

func TestFilesystemProvider_Store_EmptyBody(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	ctx := context.Background()
	key := "vacio.jpg"

	err = p.Store(ctx, key, "image/jpeg", 0, bytes.NewReader(nil))
	require.NoError(t, err)

	obj, err := p.Get(ctx, key)
	require.NoError(t, err)
	defer func() { _ = obj.Body.Close() }()

	got, err := io.ReadAll(obj.Body)
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Equal(t, int64(0), obj.SizeBytes)
}

// errReader always fails on Read, used to exercise Store's copy-error path.
type errReader struct{}

func (errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

func TestFilesystemProvider_Store_BodyReadError_NoArtifactLeftBehind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p, err := storage.NewFilesystemProvider(dir)
	require.NoError(t, err)

	key := "fallara.jpg"
	err = p.Store(context.Background(), key, "image/jpeg", 10, errReader{})
	require.Error(t, err)

	assert.Equal(t, 0, entryCount(t, dir), "no debe quedar archivo temporal ni blob tras un fallo de lectura")
}

// TestFilesystemProvider_Store_RenameFailsWhenTargetIsDirectory verifica que
// si ya existe un directorio en la ruta del blob, el rename falle y se
// devuelva storage_rename_failed.
func TestFilesystemProvider_Store_RenameFailsWhenTargetIsDirectory(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)

	// Crear un directorio que coincida con la ruta del blob.
	key := "dir-collision/blob.bin"
	targetDir := filepath.Join(dir, filepath.Dir(key), "blob.bin")
	require.NoError(t, os.MkdirAll(targetDir, 0o700))

	ctx := context.Background()
	payload := []byte("no debe escribirse")
	err := p.Store(ctx, key, "application/octet-stream",
		int64(len(payload)), bytes.NewReader(payload))
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_rename_failed", appErr.Code)

	// Asegurarse de que no quedó ningún archivo (el temporal debe limpiarse).
	entries, err := os.ReadDir(filepath.Join(dir, "dir-collision"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".upload-"),
			"no debe quedar temporal tras fallo de rename: %q", e.Name())
	}
}

func TestFilesystemProvider_Get_OpenFailsWithPermissionDenied(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("permission-based open failure not portable to windows")
	}
	p, dir := newProvider(t)
	ctx := context.Background()

	key := "no-read/file.bin"
	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o000))

	_, err := p.Get(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_open_failed", appErr.Code)
}

func TestFilesystemProvider_Delete_RemoveFailsWhenParentReadOnly(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("permission-based delete failure not portable to windows")
	}
	p, dir := newProvider(t)
	ctx := context.Background()

	key := "readonly-parent/blob.bin"
	target := filepath.Join(dir, key)
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o600))

	parent := filepath.Dir(target)
	require.NoError(t, os.Chmod(parent, 0o555))
	t.Cleanup(func() {
		_ = os.Chmod(parent, 0o700) // restore para que TempDir pueda limpiar
	})

	err := p.Delete(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_delete_failed", appErr.Code)
}

// TestFilesystemProvider_Get_MalformedSidecarLineTooLong cubre la rama
// donde bufio.Scanner falla por línea demasiado larga (>64KB).
func TestFilesystemProvider_Get_MalformedSidecarLineTooLong(t *testing.T) {
	t.Parallel()
	p, dir := newProvider(t)
	ctx := context.Background()

	key := "long-line/blob.bin"
	target := filepath.Join(dir, key)
	metaPath := target + ".meta"

	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	require.NoError(t, os.WriteFile(target, []byte("data"), 0o600))

	// Línea de más de 64KB (64*1024 + 1)
	longLine := "content_type=" + strings.Repeat("a", 65*1024) + "\n"
	require.NoError(t, os.WriteFile(metaPath, []byte(longLine), 0o600))

	_, err := p.Get(ctx, key)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "storage_meta_read_failed", appErr.Code)
}
