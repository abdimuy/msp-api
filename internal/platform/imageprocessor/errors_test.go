package imageprocessor_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

func TestStandard_UnsupportedMIME(t *testing.T) {
	t.Parallel()
	p := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader([]byte("plain text, not an image")),
		ContentType: "text/plain",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrUnsupportedMIME)
}

func TestStandard_InputTooLarge(t *testing.T) {
	t.Parallel()
	opts := imageprocessor.DefaultOptions()
	opts.MaxInputBytes = 1024
	p := imageprocessor.NewStandardProcessor(opts)

	jumbo := bytes.Repeat([]byte{0xFF}, 2048)
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(jumbo),
		ContentType: "image/jpeg",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrInputTooLarge)
}

func TestStandard_DecodeFailedOnTruncatedJPEG(t *testing.T) {
	t.Parallel()
	// Valid JPEG SOI marker so DetectContentType returns image/jpeg, but
	// the rest is garbage so jpeg.Decode rejects it. We disable the
	// PreserveSmallImages fast-path so decode is forced.
	corrupt := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x00}, 64)...)
	opts := imageprocessor.DefaultOptions()
	opts.PreserveSmallImages = false
	p := imageprocessor.NewStandardProcessor(opts)
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(corrupt),
		ContentType: "image/jpeg",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, imageprocessor.ErrDecodeFailed)
}

// errReaderHalf returns n bytes successfully then errs out on the second
// Read so io.Copy surfaces a non-EOF error.
type errReaderHalf struct {
	once bool
	err  error
}

func (r *errReaderHalf) Read(p []byte) (int, error) {
	if !r.once {
		r.once = true
		p[0] = 0xFF
		return 1, nil
	}
	return 0, r.err
}

func TestStandard_ReadError(t *testing.T) {
	t.Parallel()
	p := imageprocessor.NewStandardProcessor(imageprocessor.DefaultOptions())
	boom := errors.New("disk read failed")
	_, err := p.Process(t.Context(), imageprocessor.Input{
		Body: &errReaderHalf{err: boom},
	})
	require.Error(t, err)
	// readBounded wraps the underlying io error.
	assert.ErrorIs(t, err, boom)
}

// Use io.EOF discard so the linter does not flag an unused import in the
// rare configuration where this test is compiled without other io usage.
var _ = io.EOF
