package blobfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

var (
	errSweepNilStore = errors.New("failedintent.blobfs: nil store")
	errSweepNilRef   = errors.New("failedintent.blobfs: nil referencer")
)

// PathReferencer is the narrow port SweepOrphans uses to learn which on-disk
// blobs still have a database row pointing at them. Implementations should
// return absolute paths.
type PathReferencer interface {
	ReferencedPaths(ctx context.Context) ([]string, error)
}

// SweepReport summarises a single orphan-sweep cycle.
type SweepReport struct {
	Scanned int
	Deleted int
}

// SweepOrphans removes every .bin file under store.BaseDir() that is not in
// the referenced-paths set returned by ref. Files matching the temp prefix
// (still being written by an in-flight Save) are skipped so the sweep can
// never race a writer and delete a half-committed blob.
//
// Errors during readdir or unlink are logged but never fatal: the sweep's
// goal is to free space, not to enforce strict consistency. The function
// returns a SweepReport for observability and any terminal error.
func SweepOrphans(ctx context.Context, store *Store, ref PathReferencer) (SweepReport, error) {
	if store == nil {
		return SweepReport{}, errSweepNilStore
	}
	if ref == nil {
		return SweepReport{}, errSweepNilRef
	}

	referenced, err := ref.ReferencedPaths(ctx)
	if err != nil {
		return SweepReport{}, fmt.Errorf("failedintent.blobfs: referenced paths: %w", err)
	}
	known := make(map[string]struct{}, len(referenced))
	for _, p := range referenced {
		known[p] = struct{}{}
	}

	entries, err := os.ReadDir(store.BaseDir())
	if err != nil {
		return SweepReport{}, fmt.Errorf("failedintent.blobfs: read dir: %w", err)
	}

	var report SweepReport
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, tempPrefix) {
			// In-flight upload — skip so we never race Save.
			continue
		}
		if !strings.HasSuffix(name, blobSuffix) {
			// Foreign file (logs, sidecar, README): leave it alone.
			continue
		}
		full := filepath.Join(store.BaseDir(), name)
		report.Scanned++
		if _, kept := known[full]; kept {
			continue
		}
		if rmErr := os.Remove(full); rmErr != nil {
			slog.WarnContext(
				ctx,
				"failedintent.blobfs.sweep: unlink failed",
				"error", rmErr, "path", full,
			)
			continue
		}
		report.Deleted++
	}
	slog.InfoContext(
		ctx,
		"failedintent.blobfs.sweep: complete",
		"scanned", report.Scanned,
		"deleted", report.Deleted,
		"base_dir", store.BaseDir(),
	)
	return report, nil
}
