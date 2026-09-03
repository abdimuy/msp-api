//nolint:misspell // visitas vocabulary is Spanish per project convention.
package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ─── StorageKind ──────────────────────────────────────────────────────────────

func TestStorageKind_IsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.StorageKindFilesystem.IsValid())
	assert.False(t, domain.StorageKind("R2").IsValid(),
		"cloud storage is not a thing in this codebase (CLAUDE.md §6)")
	assert.False(t, domain.StorageKind("").IsValid())
	assert.False(t, domain.StorageKind("filesystem").IsValid(),
		"the comparison is exact: lower-case is not the enum value")
}

func TestStorageKind_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "FILESYSTEM", domain.StorageKindFilesystem.String())
	assert.Empty(t, domain.StorageKind("").String())
}

// ─── IsAllowedMime ────────────────────────────────────────────────────────────

func TestIsAllowedMime_AcceptsTheWholeWhitelist(t *testing.T) {
	t.Parallel()

	// Every member spelled out literally. If someone edits the whitelist,
	// this test tells them exactly which member moved.
	for _, m := range []string{
		"image/jpeg", "image/png", "image/gif", "image/webp", "application/pdf",
	} {
		assert.True(t, domain.IsAllowedMime(m), "expected %q on the whitelist", m)
	}
}

func TestIsAllowedMime_RejectsEverythingElse(t *testing.T) {
	t.Parallel()

	for _, m := range []string{
		"",
		"image/bmp",
		"image/svg+xml",
		"image/heic",
		"text/html",
		"application/octet-stream",
		"application/x-msdownload",
		"IMAGE/JPEG",            // case matters at this level; the transport lower-cases first
		"image/jpeg; charset=x", // parameters must already be stripped
		" image/jpeg",           // no trimming happens here either
	} {
		assert.False(t, domain.IsAllowedMime(m), "expected %q rejected", m)
	}
}

func TestMimeConstants_MatchTheDeclaredWhitelist(t *testing.T) {
	t.Parallel()

	// The constants are what the HTTP layer's contentType tag is built from,
	// so a typo here would silently open or close the gate.
	assert.Equal(t, "image/jpeg", domain.MimeJPEG)
	assert.Equal(t, "image/png", domain.MimePNG)
	assert.Equal(t, "image/gif", domain.MimeGIF)
	assert.Equal(t, "image/webp", domain.MimeWebP)
	assert.Equal(t, "application/pdf", domain.MimePDF)
}

// ─── NewImagenStorage ─────────────────────────────────────────────────────────

func TestNewImagenStorage_HappyPath(t *testing.T) {
	t.Parallel()

	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "visitas/abc/def.jpg")
	require.NoError(t, err)
	assert.Equal(t, domain.StorageKindFilesystem, s.Kind())
	assert.Equal(t, "visitas/abc/def.jpg", s.Key())
}

func TestNewImagenStorage_TrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "  visitas/a/b.png\t")
	require.NoError(t, err)
	assert.Equal(t, "visitas/a/b.png", s.Key())
}

func TestNewImagenStorage_RejectsInvalidKind(t *testing.T) {
	t.Parallel()

	_, err := domain.NewImagenStorage(domain.StorageKind("S3"), "visitas/a/b.jpg")
	require.ErrorIs(t, err, domain.ErrImagenStorageKindInvalido)
}

func TestNewImagenStorage_RejectsUnsafeKeys(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":             "",
		"blank":             "   ",
		"leading slash":     "/etc/passwd",
		"traversal":         "visitas/../../etc/passwd",
		"traversal only":    "..",
		"nul byte":          "visitas/a\x00b.jpg",
		"windows separator": `visitas\a\b.jpg`,
		"too long":          strings.Repeat("a", 501),
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
			require.ErrorIs(t, err, domain.ErrImagenStorageKeyInvalida)
		})
	}
}

func TestNewImagenStorage_AcceptsKeyAtExactlyTheColumnWidth(t *testing.T) {
	t.Parallel()

	// 500 is the column width, so 500 must pass and 501 must not — the
	// boundary itself, not just "long" vs "short".
	key := strings.Repeat("a", 500)
	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
	require.NoError(t, err)
	assert.Len(t, s.Key(), 500)
}

// ─── HydrateImagenStorage ─────────────────────────────────────────────────────

func TestHydrateImagenStorage_DoesNotValidate(t *testing.T) {
	t.Parallel()

	// The read path accepts whatever the row holds — same rule as
	// RehydrateVisita for TipoVisita. A key that NewImagenStorage would
	// reject still hydrates.
	s := domain.HydrateImagenStorage(domain.StorageKind("R2"), "/absolute/../unsafe")
	assert.Equal(t, domain.StorageKind("R2"), s.Kind())
	assert.Equal(t, "/absolute/../unsafe", s.Key())
}

// ─── Equals ───────────────────────────────────────────────────────────────────

func TestImagenStorage_Equals(t *testing.T) {
	t.Parallel()

	a, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "visitas/x/y.jpg")
	require.NoError(t, err)
	b, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "visitas/x/y.jpg")
	require.NoError(t, err)
	c, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "visitas/x/z.jpg")
	require.NoError(t, err)

	assert.True(t, a.Equals(b))
	assert.False(t, a.Equals(c), "different key")
	assert.False(t, a.Equals(domain.HydrateImagenStorage(domain.StorageKind("R2"), "visitas/x/y.jpg")),
		"different kind")
}
