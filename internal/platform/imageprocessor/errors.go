package imageprocessor

import "errors"

// Sentinel errors. Callers wrap these into domain apperror values to map
// to stable HTTP status codes (typically 422 Unprocessable Entity for the
// validation-flavored errors and 500 for ErrEncodeFailed).
var (
	// ErrUnsupportedMIME is returned when the content-sniffed MIME is not
	// one of JPEG, PNG, GIF, or WebP.
	ErrUnsupportedMIME = errors.New("imageprocessor: unsupported MIME type")
	// ErrInputTooLarge is returned when the source body exceeds
	// [Options.MaxInputBytes].
	ErrInputTooLarge = errors.New("imageprocessor: input exceeds max bytes")
	// ErrDecodeFailed wraps any decoder error from image/jpeg, image/png,
	// image/gif, or golang.org/x/image/webp.
	ErrDecodeFailed = errors.New("imageprocessor: decode failed")
	// ErrEncodeFailed wraps any encoder error from image/jpeg, image/png,
	// or image/gif.
	ErrEncodeFailed = errors.New("imageprocessor: encode failed")
	// ErrInvalidOptions is returned by [Options.Validate] when a knob is
	// out of range or internally inconsistent.
	ErrInvalidOptions = errors.New("imageprocessor: invalid options")
)
