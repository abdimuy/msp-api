package storage

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// filesystemBlobMode is the permission bits used for blob files. 0o600 keeps
// the bytes readable only by the service user.
const filesystemBlobMode os.FileMode = 0o600

// filesystemDirMode is the permission bits used for directories created by the
// provider.
const filesystemDirMode os.FileMode = 0o700

// maxKeyLength is the upper bound on storage keys. Keys are caller-supplied
// (uuid-prefixed) so 500 chars is far above the realistic shape.
const maxKeyLength = 500

// metaSuffix is appended to a key to derive the sidecar metadata file path.
const metaSuffix = ".meta"

// uploadTempPattern is the os.CreateTemp pattern used for atomic writes.
const uploadTempPattern = ".upload-*"

// errStorageInvalidKey is the sentinel returned by validateKey on any malformed
// key. Constructed once so the err113 linter is satisfied and call sites
// share a single apperror code.
var errStorageInvalidKey = apperror.NewValidation(
	"storage_invalid_key",
	"clave de almacenamiento inválida",
)

// FilesystemProvider stores blobs under a local base directory. Each blob has
// a sidecar `<key>.meta` file holding the content-type and size declared at
// Store time.
//
// Path traversal is rejected before any I/O happens — see [validateKey].
type FilesystemProvider struct {
	baseDir string
}

// NewFilesystemProvider constructs a FilesystemProvider rooted at baseDir.
// It resolves baseDir to an absolute path, creates the directory tree if it
// does not exist, and verifies the directory is writable.
func NewFilesystemProvider(baseDir string) (*FilesystemProvider, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, apperror.NewValidation(
			"storage_basedir_required",
			"directorio base de almacenamiento requerido",
		)
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, apperror.NewInternal(
			"storage_basedir_invalid",
			"directorio base de almacenamiento inválido",
		).WithError(err).WithSource("storage.filesystem")
	}
	if mkErr := os.MkdirAll(abs, filesystemDirMode); mkErr != nil {
		return nil, apperror.NewInternal(
			"storage_basedir_unwritable",
			"directorio base de almacenamiento no se puede crear",
		).WithError(mkErr).WithSource("storage.filesystem").WithField("dir", abs)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, apperror.NewInternal(
			"storage_basedir_unreadable",
			"directorio base de almacenamiento no accesible",
		).WithError(err).WithSource("storage.filesystem").WithField("dir", abs)
	}
	if !info.IsDir() {
		return nil, apperror.NewValidation(
			"storage_basedir_not_directory",
			"la ruta base de almacenamiento no es un directorio",
		).WithField("dir", abs)
	}
	return &FilesystemProvider{baseDir: abs}, nil
}

var _ outbound.StorageProvider = (*FilesystemProvider)(nil)

// Store writes body atomically under key. Behavior:
//
//  1. The key is validated (no `..`, no leading `/`, no null bytes, no
//     backslashes, length ≤ 500).
//  2. The parent directory is created if missing.
//  3. The payload is streamed to a temp file in the target directory, fsynced,
//     closed, then renamed into place.
//  4. A sidecar `<key>.meta` file is written with `content_type=` and
//     `size_bytes=` lines.
//
// If the body fails to copy or the rename fails, any temp artifact is removed
// before returning.
func (p *FilesystemProvider) Store(
	_ context.Context, key, contentType string, sizeBytes int64, body io.Reader,
) error {
	if err := validateKey(key); err != nil {
		return err
	}
	target := filepath.Join(p.baseDir, key)
	if err := os.MkdirAll(filepath.Dir(target), filesystemDirMode); err != nil {
		return apperror.NewInternal(
			"storage_mkdir_failed",
			"no se pudo crear el directorio destino",
		).WithError(err).WithSource("storage.filesystem").WithField("key", key)
	}
	if err := writeAtomic(target, body); err != nil {
		return err
	}
	if err := writeMetaSidecar(target+metaSuffix, contentType, sizeBytes); err != nil {
		// Best-effort cleanup of the blob if the sidecar fails; the pair is
		// only meaningful together.
		_ = os.Remove(target)
		return err
	}
	return nil
}

// Get opens the blob at key for streaming. The caller must close obj.Body.
// If a sidecar exists it is parsed for content-type and size; otherwise we
// fall back to "application/octet-stream" and the file's stat-reported size.
func (p *FilesystemProvider) Get(_ context.Context, key string) (outbound.StorageObject, error) {
	if err := validateKey(key); err != nil {
		return outbound.StorageObject{}, err
	}
	target := filepath.Join(p.baseDir, key)
	file, err := os.Open(target) //nolint:gosec // path is rooted under baseDir and key is validated.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return outbound.StorageObject{}, apperror.NewNotFound(
				"storage_object_not_found",
				"objeto de almacenamiento no encontrado",
			).WithField("key", key)
		}
		return outbound.StorageObject{}, apperror.NewInternal(
			"storage_open_failed",
			"no se pudo abrir el objeto de almacenamiento",
		).WithError(err).WithSource("storage.filesystem").WithField("key", key)
	}
	meta, metaErr := readMetaSidecar(target + metaSuffix)
	if metaErr != nil {
		_ = file.Close()
		return outbound.StorageObject{}, metaErr
	}
	if !meta.exists || meta.contentType == "" || meta.sizeBytes < 0 {
		stat, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return outbound.StorageObject{}, apperror.NewInternal(
				"storage_stat_failed",
				"no se pudo leer el tamaño del objeto",
			).WithError(statErr).WithSource("storage.filesystem").WithField("key", key)
		}
		if meta.contentType == "" {
			meta.contentType = "application/octet-stream"
		}
		if !meta.exists || meta.sizeBytes < 0 {
			meta.sizeBytes = stat.Size()
		}
	}
	return outbound.StorageObject{
		Body:        file,
		ContentType: meta.contentType,
		SizeBytes:   meta.sizeBytes,
	}, nil
}

// Delete removes the blob and its sidecar. Idempotent: a missing blob or
// sidecar is not an error.
func (p *FilesystemProvider) Delete(_ context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	target := filepath.Join(p.baseDir, key)
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperror.NewInternal(
			"storage_delete_failed",
			"no se pudo eliminar el objeto de almacenamiento",
		).WithError(err).WithSource("storage.filesystem").WithField("key", key)
	}
	if err := os.Remove(target + metaSuffix); err != nil && !errors.Is(err, os.ErrNotExist) {
		return apperror.NewInternal(
			"storage_delete_meta_failed",
			"no se pudo eliminar el sidecar del objeto",
		).WithError(err).WithSource("storage.filesystem").WithField("key", key)
	}
	return nil
}

// writeAtomic streams body to a temp file in the target directory, fsyncs it,
// then renames it over targetPath. The temp file is removed if anything fails
// before the rename succeeds.
func writeAtomic(targetPath string, body io.Reader) error {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, uploadTempPattern)
	if err != nil {
		return apperror.NewInternal(
			"storage_tempfile_failed",
			"no se pudo crear archivo temporal",
		).WithError(err).WithSource("storage.filesystem")
	}
	tmpName := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(tmpName)
		}
	}()

	if _, copyErr := io.Copy(tmp, body); copyErr != nil {
		_ = tmp.Close()
		return apperror.NewInternal(
			"storage_write_failed",
			"no se pudo escribir el objeto de almacenamiento",
		).WithError(copyErr).WithSource("storage.filesystem")
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		return apperror.NewInternal(
			"storage_fsync_failed",
			"no se pudo sincronizar el archivo temporal",
		).WithError(syncErr).WithSource("storage.filesystem")
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return apperror.NewInternal(
			"storage_tempclose_failed",
			"no se pudo cerrar el archivo temporal",
		).WithError(closeErr).WithSource("storage.filesystem")
	}
	if chmodErr := os.Chmod(tmpName, filesystemBlobMode); chmodErr != nil {
		return apperror.NewInternal(
			"storage_chmod_failed",
			"no se pudo asignar permisos al archivo",
		).WithError(chmodErr).WithSource("storage.filesystem")
	}
	if renameErr := os.Rename(tmpName, targetPath); renameErr != nil {
		return apperror.NewInternal(
			"storage_rename_failed",
			"no se pudo mover el archivo temporal a destino",
		).WithError(renameErr).WithSource("storage.filesystem")
	}
	renamed = true
	return nil
}

// writeMetaSidecar writes a two-line sidecar describing the blob.
func writeMetaSidecar(path, contentType string, sizeBytes int64) error {
	content := fmt.Sprintf("content_type=%s\nsize_bytes=%d\n", contentType, sizeBytes)
	if err := os.WriteFile(path, []byte(content), filesystemBlobMode); err != nil {
		return apperror.NewInternal(
			"storage_meta_write_failed",
			"no se pudo escribir el sidecar del objeto",
		).WithError(err).WithSource("storage.filesystem")
	}
	return nil
}

// metaSidecar is the decoded form of a `<key>.meta` file.
type metaSidecar struct {
	contentType string
	sizeBytes   int64
	exists      bool
}

// readMetaSidecar reads the sidecar at path. If the sidecar does not exist
// it returns a zero metaSidecar with exists=false so the caller can apply
// defaults. A malformed sidecar returns an apperror.
func readMetaSidecar(path string) (metaSidecar, error) {
	file, err := os.Open(path) //nolint:gosec // path is rooted under baseDir; key was validated upstream.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return metaSidecar{}, nil
		}
		return metaSidecar{}, apperror.NewInternal(
			"storage_meta_open_failed",
			"no se pudo abrir el sidecar del objeto",
		).WithError(err).WithSource("storage.filesystem")
	}
	defer func() { _ = file.Close() }()

	meta := metaSidecar{exists: true, sizeBytes: -1}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		switch key {
		case "content_type":
			meta.contentType = value
		case "size_bytes":
			n, parseErr := strconv.ParseInt(value, 10, 64)
			if parseErr != nil {
				return metaSidecar{}, apperror.NewInternal(
					"storage_meta_malformed",
					"sidecar del objeto malformado",
				).WithError(parseErr).WithSource("storage.filesystem")
			}
			meta.sizeBytes = n
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return metaSidecar{}, apperror.NewInternal(
			"storage_meta_read_failed",
			"no se pudo leer el sidecar del objeto",
		).WithError(scanErr).WithSource("storage.filesystem")
	}
	return meta, nil
}

// validateKey rejects keys that would let a caller escape baseDir or otherwise
// confuse the host filesystem.
func validateKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return errStorageInvalidKey.WithField("reason", "empty")
	}
	if len(key) > maxKeyLength {
		return errStorageInvalidKey.WithField("reason", "too_long")
	}
	if strings.Contains(key, "..") {
		return errStorageInvalidKey.WithField("reason", "path_traversal")
	}
	if strings.ContainsRune(key, 0x00) {
		return errStorageInvalidKey.WithField("reason", "null_byte")
	}
	if strings.Contains(key, `\`) {
		return errStorageInvalidKey.WithField("reason", "backslash")
	}
	if strings.HasPrefix(key, "/") {
		return errStorageInvalidKey.WithField("reason", "absolute_path")
	}
	return nil
}
