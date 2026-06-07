// Package blobfs implements failedintent.BlobStorage backed by the local
// filesystem. Blobs are atomic (tmp + rename + fsync), 0o600, and named after
// the intent UUID so the boot-time orphan sweep can detect leaks by joining
// against failed_intents.body_blob_path.
package blobfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// blobFileMode is the permission bits used for committed blobs. 0o600 keeps
// the bytes readable only by the service user.
const blobFileMode os.FileMode = 0o600

// dirMode is the permission bits used for the blob directory.
const dirMode os.FileMode = 0o700

// blobSuffix is appended to the intent UUID to form the on-disk file name.
const blobSuffix = ".bin"

// tempPattern is the os.CreateTemp pattern used for in-flight uploads. The
// orphan sweep skips files matching this prefix so it can never race a Save
// goroutine and delete a partially-written file.
const tempPattern = ".upload-*.tmp"

// tempPrefix mirrors tempPattern (stripped of the random/extension suffix)
// for SweepOrphans to detect in-flight uploads via prefix check.
const tempPrefix = ".upload-"

var (
	errBaseDirRequired  = errors.New("failedintent.blobfs: base directory is required")
	errBaseDirNotDir    = errors.New("failedintent.blobfs: base dir is not a directory")
	errNilIntentID      = errors.New("failedintent.blobfs: intent id must not be nil")
	errNonPositiveLimit = errors.New("failedintent.blobfs: limitBytes must be positive")
)

// Store implements failedintent.BlobStorage rooted at a base directory.
type Store struct {
	baseDir string
}

// New constructs a Store rooted at baseDir. It resolves baseDir to an absolute
// path, creates the directory tree if missing, and verifies it is a directory.
func New(baseDir string) (*Store, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, errBaseDirRequired
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failedintent.blobfs: resolve base dir: %w", err)
	}
	if mkErr := os.MkdirAll(abs, dirMode); mkErr != nil {
		return nil, fmt.Errorf("failedintent.blobfs: create base dir: %w", mkErr)
	}
	info, statErr := os.Stat(abs)
	if statErr != nil {
		return nil, fmt.Errorf("failedintent.blobfs: stat base dir: %w", statErr)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %s", errBaseDirNotDir, abs)
	}
	return &Store{baseDir: abs}, nil
}

var _ failedintent.BlobStorage = (*Store)(nil)

// BaseDir returns the absolute base directory. Used by SweepOrphans.
func (s *Store) BaseDir() string { return s.baseDir }

// Save streams body into a new blob keyed by intentID. The write is atomic:
// bytes flow into a temp file in baseDir, fsync, chmod 0o600, then rename
// into <baseDir>/<intentID>.bin. On overflow (> limitBytes) the temp file is
// removed and ErrBlobTooLarge is returned.
func (s *Store) Save(
	_ context.Context, intentID uuid.UUID, body io.Reader, limitBytes int64,
) (string, error) {
	if intentID == uuid.Nil {
		return "", errNilIntentID
	}
	if limitBytes <= 0 {
		return "", errNonPositiveLimit
	}

	target := filepath.Join(s.baseDir, intentID.String()+blobSuffix)

	tmp, err := os.CreateTemp(s.baseDir, tempPattern)
	if err != nil {
		return "", fmt.Errorf("failedintent.blobfs: create temp: %w", err)
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpName)
		}
	}()

	// LimitReader caps at limitBytes; +1 sentinel lets us detect overflow.
	n, copyErr := io.Copy(tmp, io.LimitReader(body, limitBytes+1))
	if copyErr != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("failedintent.blobfs: write body: %w", copyErr)
	}
	if n > limitBytes {
		_ = tmp.Close()
		return "", failedintent.ErrBlobTooLarge
	}
	if syncErr := tmp.Sync(); syncErr != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("failedintent.blobfs: fsync: %w", syncErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return "", fmt.Errorf("failedintent.blobfs: close temp: %w", closeErr)
	}
	if chmodErr := os.Chmod(tmpName, blobFileMode); chmodErr != nil {
		return "", fmt.Errorf("failedintent.blobfs: chmod: %w", chmodErr)
	}
	if renameErr := os.Rename(tmpName, target); renameErr != nil {
		return "", fmt.Errorf("failedintent.blobfs: rename: %w", renameErr)
	}
	committed = true
	return target, nil
}

// Open returns a reader for the blob at path. The path must live under
// baseDir; any attempt to escape returns ErrBlobNotFound rather than leaking
// filesystem geometry to the caller.
func (s *Store) Open(_ context.Context, path string) (io.ReadCloser, error) {
	if err := s.validatePath(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path) //nolint:gosec // path is validated to live under baseDir.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, failedintent.ErrBlobNotFound
		}
		return nil, fmt.Errorf("failedintent.blobfs: open: %w", err)
	}
	return file, nil
}

// Delete removes the blob at path. A missing blob is not an error
// (idempotent — janitor + orphan sweep may race).
func (s *Store) Delete(_ context.Context, path string) error {
	if err := s.validatePath(path); err != nil {
		if errors.Is(err, failedintent.ErrBlobNotFound) {
			return nil
		}
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failedintent.blobfs: delete: %w", err)
	}
	return nil
}

// validatePath rejects paths outside baseDir. Returns ErrBlobNotFound so the
// caller cannot distinguish "escape attempt" from "really missing".
func (s *Store) validatePath(path string) error {
	if path == "" {
		return failedintent.ErrBlobNotFound
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return failedintent.ErrBlobNotFound
	}
	rel, err := filepath.Rel(s.baseDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == ".." {
		return failedintent.ErrBlobNotFound
	}
	return nil
}
