package imageprocessor_test

import (
	"bytes"
	"image/jpeg"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

// decodeJPEGDims reads a JPEG payload and returns its dimensions via the
// header-only decoder (cheap and exercises the same code paths the
// processor's PreserveSmallImages branch uses).
func decodeJPEGDims(t *testing.T, payload []byte) (int, int) {
	t.Helper()
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(payload))
	require.NoError(t, err)
	return cfg.Width, cfg.Height
}

func TestResize_LongSideCap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		w, h, cap int
		wantW     int
		wantH     int
	}{
		{"landscape_scales_to_cap", 2000, 1000, 800, 800, 400},
		{"portrait_scales_to_cap", 1000, 2000, 800, 400, 800},
		{"square_scales_to_cap", 1500, 1500, 800, 800, 800},
		{"already_within_cap", 600, 400, 800, 600, 400},
		{"exact_cap_landscape", 800, 600, 800, 800, 600},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := makeJPEG(t, tc.w, tc.h, 80)
			opts := imageprocessor.DefaultOptions()
			opts.MaxLongSidePx = tc.cap
			opts.PreserveSmallImages = false
			p := imageprocessor.NewStandardProcessor(opts)

			out, err := p.Process(t.Context(), imageprocessor.Input{
				Body:        bytes.NewReader(src),
				ContentType: imageprocessor.MimeJPEG,
				SizeBytes:   int64(len(src)),
			})
			require.NoError(t, err)
			assert.Equal(t, tc.wantW, out.Width, "result width")
			assert.Equal(t, tc.wantH, out.Height, "result height")
			body, err := readAll(out.Body)
			require.NoError(t, err)
			gotW, gotH := decodeJPEGDims(t, body)
			assert.Equal(t, tc.wantW, gotW, "decoded width matches metadata")
			assert.Equal(t, tc.wantH, gotH, "decoded height matches metadata")
		})
	}
}

func TestResize_MaxLongSideZeroSkipsResize(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 100, 80, 80)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 0
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Equal(t, 100, out.Width)
	assert.Equal(t, 80, out.Height)
}
