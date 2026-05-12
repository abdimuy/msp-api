package imageprocessor_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

func TestDetectActualMIME(t *testing.T) {
	t.Parallel()

	jpeg := makeJPEG(t, 4, 4, 90)
	pngImg := makePNG(t, 4, 4)
	gif := makeGIF(t, 4, 4)
	webp := smallWebPBytes()

	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"jpeg", jpeg, imageprocessor.MimeJPEG},
		{"png", pngImg, imageprocessor.MimePNG},
		{"gif", gif, imageprocessor.MimeGIF},
		{"webp", webp, imageprocessor.MimeWebP},
		{"text_rejected", []byte("hello, world"), ""},
		{"empty_rejected", []byte{}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, imageprocessor.DetectActualMIME(tc.in))
		})
	}
}
