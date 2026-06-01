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

// StorageProvider abstracts the binary blob backing store for cobranza
// comprobantes (transferencia bancaria PDFs, fotos de cheques, etc.). The
// only implementation is FilesystemProvider, which writes blobs under a
// local directory. Cobranza shares the same concrete implementation as
// ventas (one FilesystemProvider rooted at STORAGE_DIR) — the port is
// declared module-locally to preserve the vertical-slice boundary.
//
// Implementations must reject keys that contain path-traversal segments
// (`..`), null bytes, absolute paths, or backslashes. The key shape is
// caller-defined and stable across reads/writes.
type StorageProvider interface {
	// Store writes a new blob under the given key. If a blob already exists
	// at the same key it is overwritten — callers ensure key uniqueness via
	// uuid prefixes. SizeBytes is supplied by the caller because some
	// upstream sources (multipart) do not expose a cheap length check.
	Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error

	// Get fetches a blob by key. The caller MUST close obj.Body.
	Get(ctx context.Context, key string) (StorageObject, error)

	// Delete removes the blob at the given key. Idempotent: returns nil when
	// the key is already absent.
	Delete(ctx context.Context, key string) error
}
