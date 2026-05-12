package imageprocessor_test

import (
	"bytes"
	"testing"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

// BenchmarkProcess_JPEG_1600x1200 measures the cost of processing a
// modest phone-camera-shaped JPEG with the default options. Reported in
// run logs only; not a hard gate.
func BenchmarkProcess_JPEG_1600x1200(b *testing.B) {
	src := makeJPEG(b, 1600, 1200, 90)
	opts := imageprocessor.DefaultOptions()
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		if _, err := p.Process(b.Context(), imageprocessor.Input{
			Body:        bytes.NewReader(src),
			ContentType: imageprocessor.MimeJPEG,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcess_NoOp is the baseline: bytes through the passthrough.
// The standard pipeline is expected to be ~10-50x slower for a 5 MB JPEG
// because it decodes + resizes + re-encodes in pure Go.
func BenchmarkProcess_NoOp(b *testing.B) {
	src := makeJPEG(b, 1600, 1200, 90)
	p := imageprocessor.NoOpProcessor{}

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		if _, err := p.Process(b.Context(), imageprocessor.Input{
			Body:        bytes.NewReader(src),
			ContentType: imageprocessor.MimeJPEG,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
