package imageprocessor

import (
	"fmt"
	"image/png"
)

// Default knob values. Exported so tests and callers can refer to them by
// name when building partial Options structs.
const (
	// DefaultMaxLongSidePx is the default cap on the longer image side
	// after resize. Chosen so the resulting JPEG comfortably fits common
	// review screens and 4G upload budgets.
	DefaultMaxLongSidePx = 1920
	// DefaultJPEGQuality is the default JPEG quality (1-100). 85 is the
	// common quality/size sweet spot for photographic content.
	DefaultJPEGQuality = 85
	// DefaultMaxInputBytes is the default cap on the source body. 15 MB
	// accommodates phone-camera JPEGs without enabling DoS by oversized
	// uploads.
	DefaultMaxInputBytes int64 = 15 * 1024 * 1024
	// DefaultSmallImageBytes is the threshold below which an input is
	// considered already-compact and the [Options.PreserveSmallImages]
	// short-circuit returns it unchanged.
	DefaultSmallImageBytes int64 = 512 * 1024
)

// Options drive a [StandardProcessor]. The zero value is not valid; use
// [DefaultOptions] as a starting point or build one explicitly.
type Options struct {
	// MaxLongSidePx caps the longer image dimension after resize. Set to
	// zero only in unit tests that explicitly want to skip the resize
	// step; production callers MUST set a sane cap.
	MaxLongSidePx int
	// JPEGQuality is the quality knob for JPEG output (1-100).
	JPEGQuality int
	// PNGCompression maps directly to [png.CompressionLevel].
	// [png.DefaultCompression] (-1) is the typical choice.
	PNGCompression png.CompressionLevel
	// MaxInputBytes caps the source body BEFORE decode to avoid decoding
	// huge inputs into RAM. Must be positive.
	MaxInputBytes int64
	// RecompressWebPToJPEG re-encodes WebP inputs to JPEG. Required for
	// any client that needs to read back the asset because
	// golang.org/x/image/webp is decode-only.
	RecompressWebPToJPEG bool
	// PreserveSmallImages skips decode+encode when the input is already
	// under [DefaultSmallImageBytes] and its header-only dimension probe
	// shows the long side already ≤ MaxLongSidePx. Avoids needless
	// recompression of phone-side-compressed uploads.
	PreserveSmallImages bool
	// SmallImageBytes overrides [DefaultSmallImageBytes] for the
	// PreserveSmallImages short-circuit. Zero falls back to the default.
	SmallImageBytes int64
}

// DefaultOptions returns a fully populated, validated Options. Callers
// override individual fields after the call.
func DefaultOptions() Options {
	return Options{
		MaxLongSidePx:        DefaultMaxLongSidePx,
		JPEGQuality:          DefaultJPEGQuality,
		PNGCompression:       png.DefaultCompression,
		MaxInputBytes:        DefaultMaxInputBytes,
		RecompressWebPToJPEG: true,
		PreserveSmallImages:  true,
		SmallImageBytes:      DefaultSmallImageBytes,
	}
}

// Validate enforces the value ranges documented on each field. The error
// chain wraps [ErrInvalidOptions] so callers can match with [errors.Is].
func (o Options) Validate() error {
	if o.MaxLongSidePx < 0 {
		return fmt.Errorf("%w: max_long_side_px must be >= 0 (got %d)", ErrInvalidOptions, o.MaxLongSidePx)
	}
	if o.JPEGQuality < 1 || o.JPEGQuality > 100 {
		return fmt.Errorf("%w: jpeg_quality must be in [1,100] (got %d)", ErrInvalidOptions, o.JPEGQuality)
	}
	if o.MaxInputBytes < 1 {
		return fmt.Errorf("%w: max_input_bytes must be >= 1 (got %d)", ErrInvalidOptions, o.MaxInputBytes)
	}
	switch o.PNGCompression {
	case png.DefaultCompression, png.NoCompression, png.BestSpeed, png.BestCompression:
	default:
		return fmt.Errorf(
			"%w: png_compression must be one of {-1, 0, -2, -3} (got %d)",
			ErrInvalidOptions, int(o.PNGCompression),
		)
	}
	if o.SmallImageBytes < 0 {
		return fmt.Errorf("%w: small_image_bytes must be >= 0 (got %d)", ErrInvalidOptions, o.SmallImageBytes)
	}
	return nil
}

// smallImageCap returns the effective threshold for the
// PreserveSmallImages short-circuit, applying the default when the field
// is left at zero.
func (o Options) smallImageCap() int64 {
	if o.SmallImageBytes <= 0 {
		return DefaultSmallImageBytes
	}
	return o.SmallImageBytes
}
