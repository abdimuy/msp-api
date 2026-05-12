package imageprocessor_test

import (
	"bytes"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }

func TestStandard_JPEGRoundTrip(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 100, 80, 95)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 50
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeJPEG, out.ContentType)
	assert.Equal(t, 50, out.Width)
	assert.Equal(t, 40, out.Height)
	body, err := readAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, int64(len(body)), out.SizeBytes)
	assert.Positive(t, out.SizeBytes)
}

func TestStandard_PNGStaysPNG(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 80, 60)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 40
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimePNG,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimePNG, out.ContentType)
	assert.Equal(t, 40, out.Width)
	assert.Equal(t, 30, out.Height)
}

func TestStandard_GIFStaysGIF(t *testing.T) {
	t.Parallel()
	src := makeGIF(t, 60, 40)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 30
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeGIF,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeGIF, out.ContentType)
	assert.Equal(t, 30, out.Width)
	assert.Equal(t, 20, out.Height)
}

func TestStandard_WebPToJPEG(t *testing.T) {
	t.Parallel()
	opts := imageprocessor.DefaultOptions()
	opts.RecompressWebPToJPEG = true
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)
	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(smallWebPBytes()),
		ContentType: imageprocessor.MimeWebP,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeJPEG, out.ContentType, "WebP must be re-encoded to JPEG")
	assert.Positive(t, out.Width)
	assert.Positive(t, out.Height)
	assert.Positive(t, out.SizeBytes)
}

func TestStandard_PreserveSmallImagesShortCircuit(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 200, 200, 85)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 1000
	opts.PreserveSmallImages = true
	opts.SmallImageBytes = int64(len(src)) + 1024
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	got, err := readAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, src, got, "short-circuit must return the input bytes verbatim")
	assert.Equal(t, int64(len(src)), out.SizeBytes)
	assert.Equal(t, 200, out.Width)
	assert.Equal(t, 200, out.Height)
}

func TestStandard_MIMEMismatchTrustsSniff(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 32, 32)
	opts := imageprocessor.DefaultOptions()
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: "image/jpeg", // wrong on purpose
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimePNG, out.ContentType, "sniffed PNG wins over declared JPEG")
}

func TestStandard_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	p := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	// Pre-build payload outside the goroutines so makeJPEG (which uses
	// require) runs only on the test goroutine.
	src := makeJPEG(t, 64, 64, 80)
	errs := make(chan error, n)
	for range n {
		go func() {
			defer wg.Done()
			_, err := p.Process(t.Context(), imageprocessor.Input{
				Body:        bytes.NewReader(src),
				ContentType: imageprocessor.MimeJPEG,
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestStandard_PreserveSmallShortCircuit_PNG(t *testing.T) {
	t.Parallel()
	src := makePNG(t, 100, 100)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 500
	opts.PreserveSmallImages = true
	opts.SmallImageBytes = int64(len(src)) + 1
	p := imageprocessor.NewStandardProcessor(opts)
	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimePNG,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimePNG, out.ContentType)
	assert.Equal(t, 100, out.Width)
}

func TestStandard_PreserveSmallShortCircuit_GIF(t *testing.T) {
	t.Parallel()
	src := makeGIF(t, 50, 50)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 500
	opts.PreserveSmallImages = true
	opts.SmallImageBytes = int64(len(src)) + 1
	p := imageprocessor.NewStandardProcessor(opts)
	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeGIF,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeGIF, out.ContentType)
	assert.Equal(t, 50, out.Width)
}

func TestStandard_PreserveSmallShortCircuit_WebP_NoTranscode(t *testing.T) {
	t.Parallel()
	src := smallWebPBytes()
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 1000
	opts.RecompressWebPToJPEG = false // keep WebP so the short-circuit applies
	opts.PreserveSmallImages = true
	opts.SmallImageBytes = int64(len(src)) + 1
	p := imageprocessor.NewStandardProcessor(opts)
	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeWebP,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeWebP, out.ContentType)
}

func TestStandard_SmallImageCapDefaults(t *testing.T) {
	t.Parallel()
	// Set SmallImageBytes=0 to exercise the default branch in smallImageCap.
	src := makeJPEG(t, 32, 32, 85)
	opts := imageprocessor.DefaultOptions()
	opts.SmallImageBytes = 0
	opts.MaxLongSidePx = 500
	p := imageprocessor.NewStandardProcessor(opts)
	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Equal(t, imageprocessor.MimeJPEG, out.ContentType)
}

// TestStandard_BoundaryExactlyAtCap verifies a body whose length is
// exactly MaxInputBytes is accepted. readBounded reads cap+1; only N+1
// bytes triggers the cap. Off-by-one bugs here would silently reject
// every upload at the exact threshold.
func TestStandard_BoundaryExactlyAtCap(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 32, 32, 80)
	opts := imageprocessor.DefaultOptions()
	opts.MaxInputBytes = int64(len(src)) // exactly at cap
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Positive(t, out.SizeBytes)
}

// TestStandard_BoundaryOneByteOverCap verifies a body whose length is
// MaxInputBytes+1 is rejected. Pair with BoundaryExactlyAtCap to pin the
// exact threshold.
func TestStandard_BoundaryOneByteOverCap(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 64, 64, 90)
	opts := imageprocessor.DefaultOptions()
	opts.MaxInputBytes = int64(len(src)) - 1 // input is exactly cap+1
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrInputTooLarge)
}

// TestStandard_EmptyBodyRejected verifies an empty body cannot pass the
// MIME sniff. An empty upload is a buggy client, not a valid image, and
// must surface a clear error rather than crashing the decoder.
func TestStandard_EmptyBodyRejected(t *testing.T) {
	t.Parallel()
	p := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(nil),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrUnsupportedMIME)
}

// TestStandard_SequentialProcessingIsIdempotent verifies the same
// StandardProcessor instance can process multiple inputs back-to-back
// without state leaking between calls.
func TestStandard_SequentialProcessingIsIdempotent(t *testing.T) {
	t.Parallel()
	p := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	src := makeJPEG(t, 100, 100, 85)

	var sizes []int64
	for range 5 {
		out, err := p.Process(t.Context(), imageprocessor.Input{
			Body:        bytes.NewReader(src),
			ContentType: imageprocessor.MimeJPEG,
		})
		require.NoError(t, err)
		sizes = append(sizes, out.SizeBytes)
	}
	for i := 1; i < len(sizes); i++ {
		assert.Equal(t, sizes[0], sizes[i], "every run on the same input must produce a byte-identical output size")
	}
}

// TestStandard_OutputBodyMatchesSizeBytes verifies the Output.SizeBytes
// field is exactly the number of bytes the caller can read from
// Output.Body. The storage provider relies on this contract to write the
// correct Content-Length sidecar.
func TestStandard_OutputBodyMatchesSizeBytes(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 80, 60, 85)
	opts := imageprocessor.DefaultOptions()
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	body, err := readAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, out.SizeBytes, int64(len(body)),
		"Output.SizeBytes must match the byte count actually readable from Output.Body")
}

// TestStandard_WebPNotRecompressedWhenFlagFalse verifies the WebP→JPEG
// re-encode is opt-in.
func TestStandard_WebPNotRecompressedWhenFlagFalse(t *testing.T) {
	t.Parallel()
	opts := imageprocessor.DefaultOptions()
	opts.RecompressWebPToJPEG = false
	opts.PreserveSmallImages = false
	opts.MaxLongSidePx = 4096 // do not resize the 75x100 fixture
	p := imageprocessor.NewStandardProcessor(opts)

	// With RecompressWebPToJPEG=false the encoder dispatcher is asked to
	// emit WebP, which is unsupported — we expect ErrUnsupportedMIME so
	// callers know they need to enable re-encode.
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(smallWebPBytes()),
		ContentType: imageprocessor.MimeWebP,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrUnsupportedMIME,
		"WebP encode is unsupported; require RecompressWebPToJPEG=true to handle WebP uploads")
}

// TestStandard_DimensionsExactlyAtCap verifies that an image whose long
// side equals the cap is accepted without resize.
func TestStandard_DimensionsExactlyAtCap(t *testing.T) {
	t.Parallel()
	longSide := 256
	src := makeJPEG(t, longSide, longSide/2, 85)
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = longSide
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Equal(t, longSide, out.Width)
	assert.Equal(t, longSide/2, out.Height)
}

// TestStandard_HEICRejected verifies that an unsupported format like
// HEIC (whose first bytes do not match any of the four allowed sniffs)
// surfaces ErrUnsupportedMIME cleanly. Production protection against
// iOS clients that forget to transcode.
func TestStandard_HEICRejected(t *testing.T) {
	t.Parallel()
	// "ftypheic" ISO base media file type box magic, padded so
	// DetectContentType has enough bytes to inspect.
	heicMagic := append([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'h', 'e', 'i', 'c'}, bytes.Repeat([]byte{0}, 64)...)
	p := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(heicMagic),
		ContentType: "image/heic",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrUnsupportedMIME)
}

func TestStandard_LargePhotoActuallyShrinks(t *testing.T) {
	t.Parallel()
	src := makeJPEG(t, 1600, 1200, 95) // 95-quality phone-camera-ish JPEG
	opts := imageprocessor.DefaultOptions()
	opts.MaxLongSidePx = 800
	opts.JPEGQuality = 80
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	out, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: imageprocessor.MimeJPEG,
	})
	require.NoError(t, err)
	assert.Less(t, out.SizeBytes, int64(len(src)), "processed payload must be smaller than the input")
	assert.LessOrEqual(t, out.Width, 800)
	assert.LessOrEqual(t, out.Height, 800)
}
