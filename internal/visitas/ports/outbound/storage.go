package outbound

import (
	"context"
	"io"
)

// StorageObject is the result of a Get call from a StorageProvider.
//
// The caller MUST close Body. ContentType and SizeBytes mirror what was
// passed to Store; providers persist them as sidecar metadata.
type StorageObject struct {
	// Body is the opaque blob payload, ready to stream.
	Body io.ReadCloser
	// ContentType is the MIME type provided at Store time.
	ContentType string
	// SizeBytes is the number of bytes in Body.
	SizeBytes int64
}

// StorageProvider abstracts the binary blob backing store for the photos a
// cobrador attaches to a visita.
//
// There is exactly one concrete implementation in the codebase —
// ventas/infra/storage.FilesystemProvider, writing under STORAGE_DIR — and
// visitas reuses it through a thin adapter at the composition root
// (cmd/api/visitas_wiring.go). Blob storage is local disk only; no cloud
// backend, no selector (CLAUDE.md §6, ADR-0003).
//
// The port is nonetheless declared module-locally, exactly as cobranza and
// ventas each declare their own: a module never imports another module's
// ports (CLAUDE.md §2, enforced by depguard).
//
// Implementations must reject keys containing path-traversal segments
// (".."), NUL bytes, absolute paths, or backslashes. This module builds keys
// as "visitas/<visita_id>/<imagen_id><ext>" and validates them in the domain
// (domain.NewImagenStorage) before they ever reach a provider.
type StorageProvider interface {
	// Store writes a new blob under the given key. If a blob already exists
	// at the same key it is overwritten — callers ensure key uniqueness via
	// uuid segments. SizeBytes is supplied by the caller because a multipart
	// part does not expose a cheap length check.
	Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error

	// Get fetches a blob by key. The caller MUST close obj.Body.
	Get(ctx context.Context, key string) (StorageObject, error)

	// Delete removes the blob at the given key. Idempotent: returns nil when
	// the key is already absent. This is what makes the rollback path safe —
	// a blob written before a failed transaction is deleted, and deleting it
	// twice is not an error.
	Delete(ctx context.Context, key string) error
}
