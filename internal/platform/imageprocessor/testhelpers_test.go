package imageprocessor_test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// makeJPEG generates a synthetic JPEG of the requested pixel dimensions.
// Each pixel is filled with a deterministic gradient so encoder behavior
// is stable across runs.
func makeJPEG(tb testing.TB, w, h, quality int) []byte {
	tb.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		tb.Fatalf("makeJPEG: %v", err)
	}
	return buf.Bytes()
}

// makePNG generates a synthetic PNG of the requested pixel dimensions.
func makePNG(tb testing.TB, w, h int) []byte {
	tb.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8((x + y) % 256), G: 200, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatalf("makePNG: %v", err)
	}
	return buf.Bytes()
}

// makeGIF generates a single-frame synthetic GIF.
func makeGIF(tb testing.TB, w, h int) []byte {
	tb.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), []color.Color{
		color.RGBA{0, 0, 0, 255},
		color.RGBA{255, 255, 255, 255},
	})
	for y := range h {
		for x := range w {
			if (x+y)%2 == 0 {
				img.SetColorIndex(x, y, 1)
			}
		}
	}
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		tb.Fatalf("makeGIF: %v", err)
	}
	return buf.Bytes()
}

// smallWebPBytes returns a valid 1-bit-per-pixel lossless WebP. The
// payload is the gopher-doc.1bpp fixture from golang.org/x/image, copied
// here as a hex literal so tests do not need binary blobs in testdata.
func smallWebPBytes() []byte {
	const hexPayload = smallWebPHex
	buf := make([]byte, len(hexPayload)/2)
	for i := range buf {
		buf[i] = hexNibble(hexPayload[2*i])<<4 | hexNibble(hexPayload[2*i+1])
	}
	return buf
}

func hexNibble(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

const smallWebPHex = "52494646b2010000574542505650384ca50100002f4ac018000f30fff33ffff31f7890246d7bda486ee6f10dc67d848125e930433b66fc8719960c279962269f604aeda16606d9d58abeaaffff153a4144ff19b86da4c8bbc738f00ac4a3af81df314a6259f7a6a0a5482297d1b7a015301714e2d71d2c85f1c08d719106e0ecb0b80e0a5557c90a202b53b18080923cfa524ffce28c4ff7c10237af83571807b615905b9681ada5c8f8b92341c5cb9613a56207834459a649e24555bda1d1c028ec28b16b8e19dc48ca7d8ebda083be183fc1ee93c1a74f04f6ea055e7c32c2e6309f3266739693c491cf837e428c8f2fe3276a6cccbdc135ac7344afdd45f462993d551c4bdc3b3e1847dfab2e07da8f7986ffa0b93a72e4e2274c0e2b79b987570a8d6e8455909830aeddc5c28205d80ff4790aafd82400ed8ff0629919655d2006ad41afb5203a6deaaca8ad5c1dcb4d71756f0991f93ac63117995410f8741d16be8e2a120ddf87575aad3ed2aafa10948279e54b1fdfa0bc64cbcaa33ae4f438e228739535f140a8ca6c0bec857822afb2e297dc382f66ef3327268d072a5da3023ba065636f22f8538bcdb7c8d6f12ac40868b6870000"
