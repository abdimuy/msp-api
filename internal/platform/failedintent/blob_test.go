package failedintent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// TestSentinels_AreDistinct guards against accidental aliasing of the two
// blob-related sentinels — callers branch on errors.Is so they must compare
// inequal.
func TestSentinels_AreDistinct(t *testing.T) {
	t.Parallel()

	assert.NotEqual(t, failedintent.ErrBlobNotFound, failedintent.ErrBlobTooLarge)
	assert.NotNil(t, failedintent.ErrBlobNotFound)
	assert.NotNil(t, failedintent.ErrBlobTooLarge)
}

func TestDefaultMaxMultipartBytes_Is50MiB(t *testing.T) {
	t.Parallel()
	assert.Equal(t, int64(50*1024*1024), failedintent.DefaultMaxMultipartBytes)
}
