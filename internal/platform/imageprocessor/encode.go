package imageprocessor

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
)

// encode writes img to a bytes.Buffer in the requested output MIME. The
// caller decides the target MIME via outMime — it can differ from the
// input (e.g. WebP→JPEG re-encode).
func encode(img image.Image, outMime string, opts Options) ([]byte, error) {
	var buf bytes.Buffer
	switch outMime {
	case MimeJPEG:
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: opts.JPEGQuality}); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEncodeFailed, err)
		}
	case MimePNG:
		enc := png.Encoder{CompressionLevel: opts.PNGCompression}
		if err := enc.Encode(&buf, img); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEncodeFailed, err)
		}
	case MimeGIF:
		if err := gif.Encode(&buf, img, nil); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrEncodeFailed, err)
		}
	default:
		return nil, fmt.Errorf("%w: encode", ErrUnsupportedMIME)
	}
	return buf.Bytes(), nil
}
