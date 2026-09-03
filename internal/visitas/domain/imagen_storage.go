package domain

import "strings"

// maxStorageKeyLength is the width of MSP_VISITAS_IMAGENES.STORAGE_KEY.
const maxStorageKeyLength = 500

// StorageKind enumerates where an imagen de visita is physically stored.
//
// Duplicated from internal/cobranza/domain rather than imported: domain
// packages are the innermost layer of a vertical slice and never depend on
// another module (CLAUDE.md §2, enforced by depguard). Same reasoning as
// safe_string.go's header.
type StorageKind string

// StorageKind values matching MSP_VISITAS_IMAGENES.STORAGE_KIND. Go only ever
// writes FILESYSTEM — blob storage is local disk only (CLAUDE.md §6, ADR-0003).
const (
	// StorageKindFilesystem stores the binary on the API server's local
	// filesystem under a relative path below STORAGE_DIR.
	StorageKindFilesystem StorageKind = "FILESYSTEM"
)

// IsValid reports whether k is a recognized StorageKind.
func (k StorageKind) IsValid() bool { return k == StorageKindFilesystem }

// String returns the canonical string representation.
func (k StorageKind) String() string { return string(k) }

// Allowed MIME types for a visita photo. The list is the SAME whitelist
// cobranza applies to comprobantes de pago — deliberately duplicated, not
// imported, for the vertical-slice reason above. Keep the two in sync by
// intent, not by compiler.
//
// The Android app photographs the door / the client / a receipt; it never
// uploads anything else. application/pdf is on the list because the same
// multipart contract is reused verbatim from cobranza, where a recibo SAT
// legitimately arrives as a PDF.
const (
	// MimeJPEG is the canonical mime for JPEG images.
	MimeJPEG = "image/jpeg"
	// MimePNG is the canonical mime for PNG images.
	MimePNG = "image/png"
	// MimeGIF is the canonical mime for GIF images.
	MimeGIF = "image/gif"
	// MimeWebP is the canonical mime for WebP images.
	MimeWebP = "image/webp"
	// MimePDF is the canonical mime for PDF files.
	MimePDF = "application/pdf"
)

// IsAllowedMime reports whether m is one of the allowed mime types. The
// comparison is exact and case-sensitive: the transport lower-cases the
// declared Content-Type before it reaches the domain.
func IsAllowedMime(m string) bool {
	switch m {
	case MimeJPEG, MimePNG, MimeGIF, MimeWebP, MimePDF:
		return true
	}
	return false
}

// ImagenStorage couples a StorageKind with its storage-specific key. For
// FILESYSTEM the key is a path relative to STORAGE_DIR, by convention
// "visitas/<visita_id>/<imagen_id><ext>".
type ImagenStorage struct {
	kind StorageKind
	key  string
}

// NewImagenStorage validates and constructs an ImagenStorage. The key must be
// non-empty, within the column width, and free of the structural patterns
// that would let a caller escape STORAGE_DIR.
func NewImagenStorage(kind StorageKind, key string) (ImagenStorage, error) {
	if !kind.IsValid() {
		return ImagenStorage{}, ErrImagenStorageKindInvalido
	}
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ImagenStorage{}, ErrImagenStorageKeyInvalida
	}
	if len(trimmed) > maxStorageKeyLength {
		return ImagenStorage{}, ErrImagenStorageKeyInvalida
	}
	if !isSafeStorageKey(trimmed) {
		return ImagenStorage{}, ErrImagenStorageKeyInvalida
	}
	return ImagenStorage{kind: kind, key: trimmed}, nil
}

// HydrateImagenStorage rebuilds an ImagenStorage from persistence WITHOUT
// validation — the read path accepts whatever the row holds, exactly as
// RehydrateVisita does for TipoVisita.
func HydrateImagenStorage(kind StorageKind, key string) ImagenStorage {
	return ImagenStorage{kind: kind, key: key}
}

// Kind returns the storage kind.
func (s ImagenStorage) Kind() StorageKind { return s.kind }

// Key returns the storage-specific key.
func (s ImagenStorage) Key() string { return s.key }

// Equals reports whether two ImagenStorage values are identical.
func (s ImagenStorage) Equals(other ImagenStorage) bool {
	return s.kind == other.kind && s.key == other.key
}

// isSafeStorageKey rejects the structural patterns that make a key unsafe to
// join onto a base directory: NUL bytes, a leading '/' (keys are always
// relative), a backslash (a Windows separator — this API ships to Windows
// Server, CLAUDE.md §5), and the ".." traversal segment.
func isSafeStorageKey(key string) bool {
	if strings.ContainsRune(key, 0) {
		return false
	}
	if strings.HasPrefix(key, "/") {
		return false
	}
	if strings.Contains(key, `\`) {
		return false
	}
	if strings.Contains(key, "..") {
		return false
	}
	return true
}
