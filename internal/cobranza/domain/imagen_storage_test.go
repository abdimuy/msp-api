//nolint:misspell // Spanish vocabulary by convention.
package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// ─── ParseStorageKind ─────────────────────────────────────────────────────────

func TestParseStorageKind_ValidFilesystem(t *testing.T) {
	t.Parallel()

	k, err := domain.ParseStorageKind("FILESYSTEM")

	require.NoError(t, err)
	assert.Equal(t, domain.StorageKindFilesystem, k)
}

func TestParseStorageKind_RejectsR2(t *testing.T) {
	t.Parallel()

	k, err := domain.ParseStorageKind("R2")

	assert.Equal(t, domain.StorageKind(""), k)
	require.ErrorIs(t, err, domain.ErrStorageKindInvalido)
}

func TestParseStorageKind_RejectsEmpty(t *testing.T) {
	t.Parallel()

	_, err := domain.ParseStorageKind("")

	require.ErrorIs(t, err, domain.ErrStorageKindInvalido)
}

// ParseStorageKind is case-sensitive — lowercase must be rejected.
func TestParseStorageKind_CaseSensitive(t *testing.T) {
	t.Parallel()

	_, err := domain.ParseStorageKind("filesystem")

	require.ErrorIs(t, err, domain.ErrStorageKindInvalido)
}

// ─── StorageKind.IsValid / String ─────────────────────────────────────────────

func TestStorageKind_IsValid(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.StorageKindFilesystem.IsValid())
}

func TestStorageKind_IsValid_ReturnsFalseForUnknown(t *testing.T) {
	t.Parallel()

	assert.False(t, domain.StorageKind("FOO").IsValid())
	assert.False(t, domain.StorageKind("").IsValid())
	assert.False(t, domain.StorageKind("R2").IsValid())
}

func TestStorageKind_String(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "FILESYSTEM", domain.StorageKindFilesystem.String())
}

// ─── NewImagenStorage happy path ──────────────────────────────────────────────

func TestNewImagenStorage_HappyPath(t *testing.T) {
	t.Parallel()

	key := "pagos/2026/05/img.jpg"
	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)

	require.NoError(t, err)
	assert.Equal(t, domain.StorageKindFilesystem, s.Kind())
	assert.Equal(t, key, s.Key())
}

// ─── NewImagenStorage key validation ─────────────────────────────────────────

func TestNewImagenStorage_KeyValidation(t *testing.T) {
	t.Parallel()

	key501 := strings.Repeat("a", 501)
	key500 := strings.Repeat("a", 500)

	tests := []struct {
		name      string
		key       string
		wantErrIs error
		wantOK    bool
	}{
		{
			name:      "empty",
			key:       "",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "whitespace_only",
			key:       "   ",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "leading_slash",
			key:       "/abs/path/img.jpg",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "dotdot_segment",
			key:       "foo/../bar",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "dotdot_alone",
			key:       "..",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "dotdot_relative_prefix",
			key:       "../sneaky/file.jpg",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "dotdot_in_filename",
			key:       "a/b..c",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "nul_byte",
			key:       "foo\x00bar",
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:      "exceeds_500_chars",
			key:       key501,
			wantErrIs: domain.ErrStorageKeyInvalida,
		},
		{
			name:   "exactly_500_chars",
			key:    key500,
			wantOK: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, tc.key)
			if tc.wantOK {
				require.NoError(t, err)
				assert.Equal(t, tc.key, s.Key())
			} else {
				require.ErrorIs(t, err, tc.wantErrIs)
			}
		})
	}
}

// ─── NewImagenStorage trims whitespace ───────────────────────────────────────

func TestNewImagenStorage_TrimsKey(t *testing.T) {
	t.Parallel()

	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "  foo/bar.jpg  ")

	require.NoError(t, err)
	assert.Equal(t, "foo/bar.jpg", s.Key())
}

// ─── NewImagenStorage invalid kind ───────────────────────────────────────────

func TestNewImagenStorage_InvalidKind(t *testing.T) {
	t.Parallel()

	_, err := domain.NewImagenStorage(domain.StorageKind("R2"), "pagos/img.jpg")

	require.ErrorIs(t, err, domain.ErrStorageKindInvalido)
}

// ─── HydrateImagenStorage skips validation ────────────────────────────────────

func TestHydrateImagenStorage_SkipsValidation(t *testing.T) {
	t.Parallel()

	// An absolute path and an invalid kind are normally rejected by NewImagenStorage,
	// but HydrateImagenStorage is intended for persistence reconstruction and
	// must store what it receives verbatim.
	s := domain.HydrateImagenStorage(domain.StorageKindFilesystem, "/abs/path/img.jpg")

	assert.Equal(t, "/abs/path/img.jpg", s.Key())
	assert.Equal(t, domain.StorageKindFilesystem, s.Kind())
}

// ─── ImagenStorage.Equals ─────────────────────────────────────────────────────

func TestImagenStorage_Equals_SameKindAndKey(t *testing.T) {
	t.Parallel()

	s1, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/img.jpg")
	require.NoError(t, err)
	s2, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/img.jpg")
	require.NoError(t, err)

	assert.True(t, s1.Equals(s2))
}

func TestImagenStorage_Equals_DifferentKey(t *testing.T) {
	t.Parallel()

	s1, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/img1.jpg")
	require.NoError(t, err)
	s2, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/img2.jpg")
	require.NoError(t, err)

	assert.False(t, s1.Equals(s2))
}

func TestImagenStorage_Equals_ZeroValueNotEqualToNonEmpty(t *testing.T) {
	t.Parallel()

	var zero domain.ImagenStorage
	s, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "pagos/img.jpg")
	require.NoError(t, err)

	assert.False(t, zero.Equals(s))
}
