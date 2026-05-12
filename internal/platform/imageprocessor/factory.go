package imageprocessor

import (
	"image/png"
	"log/slog"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// New returns the [Processor] selected by cfg. When cfg.Enabled is false a
// [NoOpProcessor] is returned and the pipeline is effectively disabled at
// runtime; this is the operator escape hatch. Otherwise the [Options] are
// validated and a [StandardProcessor] is built.
//
// Failure paths:
//
//   - cfg.Enabled=true with invalid knobs returns the wrapped
//     [ErrInvalidOptions]; the caller (typically cmd/api wiring) treats
//     this as a fatal boot error.
func New(cfg config.ImageProcessor) (Processor, error) {
	if !cfg.Enabled {
		slog.Info("imageprocessor.disabled", "reason", "config_opt_out")
		return NoOpProcessor{}, nil
	}
	opts := Options{
		MaxLongSidePx:        cfg.MaxLongSidePx,
		JPEGQuality:          cfg.JPEGQuality,
		PNGCompression:       png.CompressionLevel(cfg.PNGCompression),
		MaxInputBytes:        cfg.MaxInputBytes,
		RecompressWebPToJPEG: cfg.RecompressWebPToJPEG,
		PreserveSmallImages:  cfg.PreserveSmallImages,
		SmallImageBytes:      cfg.SmallImageBytes,
	}
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	slog.Info("imageprocessor.enabled",
		"max_long_side_px", opts.MaxLongSidePx,
		"jpeg_quality", opts.JPEGQuality,
		"max_input_bytes", opts.MaxInputBytes,
		"webp_to_jpeg", opts.RecompressWebPToJPEG,
		"preserve_small", opts.PreserveSmallImages,
	)
	return NewStandardProcessor(opts), nil
}
