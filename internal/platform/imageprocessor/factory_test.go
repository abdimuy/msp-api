package imageprocessor_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

func TestNew_DisabledReturnsNoOp(t *testing.T) {
	t.Parallel()
	cfg := config.ImageProcessor{Enabled: false}
	p, err := imageprocessor.New(cfg)
	require.NoError(t, err)
	_, ok := p.(imageprocessor.NoOpProcessor)
	assert.True(t, ok, "Enabled=false must yield NoOpProcessor (got %T)", p)
}

func TestNew_EnabledReturnsStandard(t *testing.T) {
	t.Parallel()
	cfg := config.ImageProcessor{
		Enabled:              true,
		MaxLongSidePx:        1920,
		JPEGQuality:          85,
		PNGCompression:       -1,
		MaxInputBytes:        1024 * 1024,
		RecompressWebPToJPEG: true,
		PreserveSmallImages:  true,
		SmallImageBytes:      256 * 1024,
	}
	p, err := imageprocessor.New(cfg)
	require.NoError(t, err)
	_, ok := p.(*imageprocessor.StandardProcessor)
	assert.True(t, ok, "Enabled=true must yield *StandardProcessor (got %T)", p)
}

func TestNew_InvalidOptionsReturnsError(t *testing.T) {
	t.Parallel()

	base := config.ImageProcessor{
		Enabled:        true,
		MaxLongSidePx:  1920,
		JPEGQuality:    85,
		PNGCompression: -1,
		MaxInputBytes:  1024,
	}
	cases := []struct {
		name   string
		mutate func(c *config.ImageProcessor)
	}{
		{"jpeg_quality_zero", func(c *config.ImageProcessor) { c.JPEGQuality = 0 }},
		{"jpeg_quality_too_high", func(c *config.ImageProcessor) { c.JPEGQuality = 101 }},
		{"max_input_bytes_zero", func(c *config.ImageProcessor) { c.MaxInputBytes = 0 }},
		{"max_long_side_negative", func(c *config.ImageProcessor) { c.MaxLongSidePx = -1 }},
		{"png_compression_invalid", func(c *config.ImageProcessor) { c.PNGCompression = 42 }},
		{"small_image_bytes_negative", func(c *config.ImageProcessor) { c.SmallImageBytes = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := base
			tc.mutate(&c)
			_, err := imageprocessor.New(c)
			require.Error(t, err)
			assert.ErrorIs(t, err, imageprocessor.ErrInvalidOptions)
		})
	}
}

// TestNew_ConstructedProcessorProducesValidOutput is a smoke test that
// the factory's product can actually process a real image end-to-end
// using the production-default knobs. Catches regressions where the
// factory drifts from the constructor's expectations.
func TestNew_ConstructedProcessorProducesValidOutput(t *testing.T) {
	t.Parallel()
	cfg := config.ImageProcessor{
		Enabled:              true,
		MaxLongSidePx:        1920,
		JPEGQuality:          85,
		PNGCompression:       -1,
		MaxInputBytes:        15 * 1024 * 1024,
		RecompressWebPToJPEG: true,
		PreserveSmallImages:  true,
		SmallImageBytes:      512 * 1024,
	}
	p, err := imageprocessor.New(cfg)
	require.NoError(t, err)

	src := makeJPEG(t, 200, 200, 90)
	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeJPEG, out.ContentType)
	assert.Positive(t, out.SizeBytes)
}
