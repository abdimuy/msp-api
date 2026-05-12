package imageprocessor

import (
	"context"
	"io"
)

// Processor is the port. All consumers depend on this interface, not on a
// concrete type. Implementations live in this package:
//
//   - [StandardProcessor] runs the real decode/resize/encode pipeline.
//   - [NoOpProcessor] is a passthrough — returns input unchanged. Used by
//     tests and when the operator sets IMAGEPROCESSOR_ENABLED=false.
type Processor interface {
	// Process consumes in.Body in full and returns a processed Output.
	// The returned Output.Body is always backed by an in-memory buffer
	// (never a streaming reader) so callers can safely seek, retry, or
	// hand it to a storage provider.
	Process(ctx context.Context, in Input) (Output, error)
}

// Input is the source image plus its declared metadata. The Body stream
// is consumed in full and is the caller's responsibility to close.
type Input struct {
	// Body is the raw upload stream. It is read up to
	// [Options.MaxInputBytes]+1 bytes; bodies above the cap are rejected
	// with [ErrInputTooLarge] before any decode.
	Body io.Reader
	// ContentType is the MIME declared by the caller (e.g. a multipart
	// header). Treated as advisory; the real MIME is sniffed from the
	// first 512 bytes of Body.
	ContentType string
	// SizeBytes is the declared byte length, used for fast-path
	// short-circuit when [Options.PreserveSmallImages] is true. Set to
	// zero (or a negative value) when unknown.
	SizeBytes int64
}

// Output is the processed result. Body is always backed by an in-memory
// buffer (callers do not need to close it). ContentType may differ from
// the input — for example a WebP input re-encoded to JPEG.
type Output struct {
	// Body is a reader over the fully-buffered processed payload. Safe to
	// re-read by wrapping with [bytes.NewReader] of the buffered bytes,
	// though typical callers consume it once.
	Body io.Reader
	// ContentType is the resulting MIME (image/jpeg, image/png, image/gif).
	ContentType string
	// SizeBytes is the exact byte length of Body.
	SizeBytes int64
	// Width is the pixel width of the resulting image. Zero when the
	// processor short-circuits without decoding (NoOp; PreserveSmallImages
	// fast-path with disabled dimension probe).
	Width int
	// Height is the pixel height of the resulting image. Zero under the
	// same conditions as Width.
	Height int
}
