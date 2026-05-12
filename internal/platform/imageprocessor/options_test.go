package imageprocessor_test

import (
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

func TestOptions_Validate(t *testing.T) {
	t.Parallel()

	base := imageprocessor.DefaultOptions()

	cases := []struct {
		name    string
		mutate  func(o *imageprocessor.Options)
		wantErr bool
	}{
		{"defaults_ok", func(_ *imageprocessor.Options) {}, false},
		{"max_long_side_zero_ok", func(o *imageprocessor.Options) { o.MaxLongSidePx = 0 }, false},
		{"max_long_side_negative", func(o *imageprocessor.Options) { o.MaxLongSidePx = -1 }, true},
		{"jpeg_quality_zero", func(o *imageprocessor.Options) { o.JPEGQuality = 0 }, true},
		{"jpeg_quality_too_high", func(o *imageprocessor.Options) { o.JPEGQuality = 101 }, true},
		{"jpeg_quality_lower_bound", func(o *imageprocessor.Options) { o.JPEGQuality = 1 }, false},
		{"jpeg_quality_upper_bound", func(o *imageprocessor.Options) { o.JPEGQuality = 100 }, false},
		{"max_input_bytes_zero", func(o *imageprocessor.Options) { o.MaxInputBytes = 0 }, true},
		{"max_input_bytes_negative", func(o *imageprocessor.Options) { o.MaxInputBytes = -1 }, true},
		{"png_default_ok", func(o *imageprocessor.Options) { o.PNGCompression = png.DefaultCompression }, false},
		{"png_best_compression_ok", func(o *imageprocessor.Options) { o.PNGCompression = png.BestCompression }, false},
		{"png_invalid", func(o *imageprocessor.Options) { o.PNGCompression = png.CompressionLevel(42) }, true},
		{"small_image_bytes_negative", func(o *imageprocessor.Options) { o.SmallImageBytes = -1 }, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := base
			tc.mutate(&o)
			err := o.Validate()
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, imageprocessor.ErrInvalidOptions)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestDefaultOptions_PassesValidate(t *testing.T) {
	t.Parallel()
	require.NoError(t, imageprocessor.DefaultOptions().Validate())
}

func TestErrInvalidOptions_IsSentinel(t *testing.T) {
	t.Parallel()
	o := imageprocessor.Options{JPEGQuality: 200, MaxInputBytes: 1}
	err := o.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrInvalidOptions)
}
