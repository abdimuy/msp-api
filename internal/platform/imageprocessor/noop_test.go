package imageprocessor_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
)

func TestNoOp_PassthroughBytes(t *testing.T) {
	t.Parallel()
	src := []byte("\x89PNG\r\n\x1a\n-some-bytes-")
	out, err := imageprocessor.NoOpProcessor{}.Process(t.Context(), imageprocessor.Input{
		Body:        bytes.NewReader(src),
		ContentType: "image/png",
		SizeBytes:   int64(len(src)),
	})
	require.NoError(t, err)
	assert.Equal(t, "image/png", out.ContentType)
	assert.Equal(t, int64(len(src)), out.SizeBytes)
	got, err := io.ReadAll(out.Body)
	require.NoError(t, err)
	assert.Equal(t, src, got)
}

// errReader returns a fixed error on every Read so the NoOp passthrough
// surfaces read failures from the source body.
type errReader struct{ err error }

func (r errReader) Read(_ []byte) (int, error) { return 0, r.err }

func TestNoOp_PropagatesReadError(t *testing.T) {
	t.Parallel()
	boom := io.ErrUnexpectedEOF
	_, err := imageprocessor.NoOpProcessor{}.Process(t.Context(), imageprocessor.Input{
		Body:        errReader{err: boom},
		ContentType: "image/jpeg",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, boom)
}

func TestNoOp_EmptyBodyIsValid(t *testing.T) {
	t.Parallel()
	out, err := imageprocessor.NoOpProcessor{}.Process(context.Background(), imageprocessor.Input{
		Body: strings.NewReader(""),
	})
	require.NoError(t, err)
	assert.Zero(t, out.SizeBytes)
}
