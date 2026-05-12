package imageprocessor

import (
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncode_UnsupportedMIME hits the default branch of the encode
// dispatcher. The public API filters MIMEs before calling encode, so this
// path is only reachable from inside the package — covering it here so
// that a future refactor cannot accidentally drop the guard.
func TestEncode_UnsupportedMIME(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	_, err := encode(img, "image/heic", DefaultOptions())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedMIME)
}

// TestDecodeConfig_UnsupportedMIME hits the default branch of the
// decodeConfig dispatcher.
func TestDecodeConfig_UnsupportedMIME(t *testing.T) {
	t.Parallel()
	_, err := decodeConfig([]byte{0x00}, "image/heic")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedMIME)
}

// TestDecode_UnsupportedMIME hits the default branch of the decode
// dispatcher.
func TestDecode_UnsupportedMIME(t *testing.T) {
	t.Parallel()
	_, err := decode([]byte{0x00}, "image/heic")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnsupportedMIME)
}

// TestDecode_FailedPerFormat covers the decoder error branches for each
// supported MIME by feeding the right magic bytes followed by garbage.
func TestDecode_FailedPerFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mime string
		body []byte
	}{
		{"jpeg_truncated", MimeJPEG, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0}},
		{"png_truncated", MimePNG, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}},
		{"gif_truncated", MimeGIF, []byte("GIF87a")},
		{"webp_truncated", MimeWebP, []byte("RIFF\x00\x00\x00\x00WEBP")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := decode(tc.body, tc.mime)
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrDecodeFailed)
		})
	}
}

// TestDecodeConfig_FailedPerFormat covers the per-format header-probe
// error branches.
func TestDecodeConfig_FailedPerFormat(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mime string
		body []byte
	}{
		{"jpeg", MimeJPEG, []byte{0xFF, 0xD8}},
		{"png", MimePNG, []byte{0x89, 0x50}},
		{"gif", MimeGIF, []byte("GIF")},
		{"webp", MimeWebP, []byte("RIFF")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeConfig(tc.body, tc.mime)
			require.Error(t, err)
		})
	}
}

// TestResize_ExtremeAspectClampsMinDimension ensures the floor-to-1
// guard kicks in when the aspect ratio would otherwise scale to 0.
func TestResize_ExtremeAspectClampsMinDimension(t *testing.T) {
	t.Parallel()
	src := image.NewRGBA(image.Rect(0, 0, 2000, 1))
	dst := resize(src, 100)
	b := dst.Bounds()
	assert.Equal(t, 100, b.Dx())
	assert.Equal(t, 1, b.Dy(), "tall-thin image must keep at least 1 px on the short side")

	src2 := image.NewRGBA(image.Rect(0, 0, 1, 2000))
	dst2 := resize(src2, 100)
	b2 := dst2.Bounds()
	assert.Equal(t, 1, b2.Dx())
	assert.Equal(t, 100, b2.Dy())
}
