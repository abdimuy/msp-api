package imageprocessor

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/webp"
)

// readBounded reads up to max+1 bytes from r. Returns [ErrInputTooLarge]
// when the body exceeds max, otherwise the full buffer. Allocates one
// bytes.Buffer sized to the cap so adversarial uploads cannot cause
// runaway allocation.
func readBounded(r io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(r, maxBytes+1)
	var buf bytes.Buffer
	n, err := io.Copy(&buf, limited)
	if err != nil {
		return nil, fmt.Errorf("imageprocessor.read: %w", err)
	}
	if n > maxBytes {
		return nil, ErrInputTooLarge
	}
	return buf.Bytes(), nil
}

// decode dispatches to the right decoder based on the sniffed MIME. The
// returned image always carries the full pixel data — the decoders here
// do not stream.
func decode(payload []byte, mime string) (image.Image, error) {
	r := bytes.NewReader(payload)
	switch mime {
	case MimeJPEG:
		img, err := jpeg.Decode(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return img, nil
	case MimePNG:
		img, err := png.Decode(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return img, nil
	case MimeGIF:
		img, err := gif.Decode(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return img, nil
	case MimeWebP:
		img, err := webp.Decode(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return img, nil
	}
	return nil, ErrUnsupportedMIME
}

// decodeConfig reads only the header to extract dimensions, used by the
// PreserveSmallImages fast-path to avoid decoding the full image when the
// dimensions already fit. WebP headers are decoded via the dedicated
// decoder so the stdlib registry does not have to learn about WebP.
func decodeConfig(payload []byte, mime string) (image.Config, error) {
	r := bytes.NewReader(payload)
	switch mime {
	case MimeJPEG:
		cfg, err := jpeg.DecodeConfig(r)
		if err != nil {
			return image.Config{}, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return cfg, nil
	case MimePNG:
		cfg, err := png.DecodeConfig(r)
		if err != nil {
			return image.Config{}, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return cfg, nil
	case MimeGIF:
		cfg, err := gif.DecodeConfig(r)
		if err != nil {
			return image.Config{}, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return cfg, nil
	case MimeWebP:
		cfg, err := webp.DecodeConfig(r)
		if err != nil {
			return image.Config{}, fmt.Errorf("%w: %w", ErrDecodeFailed, err)
		}
		return cfg, nil
	}
	return image.Config{}, fmt.Errorf("%w: decodeConfig", ErrUnsupportedMIME)
}
