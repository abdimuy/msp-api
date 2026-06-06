package failedintent

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
)

// DefaultMaxMultipartBytes caps each multipart blob at 50 MiB. The cap exists
// so a runaway upload cannot saturate disk; the failure mode when exceeded
// is symmetric with BodyTruncated for inline bodies — the intent is still
// persisted, BodyTruncated=true, BodyBlobPath="".
const DefaultMaxMultipartBytes int64 = 50 * 1024 * 1024

// BlobStorage is the outbound port for streaming multipart bodies to a
// durable side store. Implementations must atomically commit (tmp + rename
// + fsync) so the boot-time orphan sweep never sees half-written files.
type BlobStorage interface {
	// Save streams body into a new blob keyed by intentID. limitBytes caps
	// the total bytes accepted; exceeding it must abort the write, remove
	// the temp artifact, and return ErrBlobTooLarge. On success it returns
	// the absolute path to the committed blob.
	Save(ctx context.Context, intentID uuid.UUID, body io.Reader, limitBytes int64) (path string, err error)

	// Open returns a reader over the blob at path. The caller closes it.
	// A missing blob must return ErrBlobNotFound.
	Open(ctx context.Context, path string) (io.ReadCloser, error)

	// Delete removes the blob at path. A missing blob is not an error.
	Delete(ctx context.Context, path string) error
}

// ErrBlobNotFound is returned by BlobStorage.Open when the requested path
// has no corresponding blob.
var ErrBlobNotFound = errors.New("failedintent: blob not found")

// ErrBlobTooLarge is returned by BlobStorage.Save when the streamed body
// would exceed the configured limit.
var ErrBlobTooLarge = errors.New("failedintent: blob exceeds size limit")
