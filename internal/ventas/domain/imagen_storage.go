package domain

import "strings"

// Maximum widths for image storage fields.
const (
	maxStorageKeyLength = 500
)

// StorageKind enumerates where an imagen is physically stored.
type StorageKind string

// StorageKind enum values matching MSP_VENTAS_IMAGENES.STORAGE_KIND.
//
// The DB CHECK constraint also allows "R2" for forward compatibility, but
// the Go code only ever writes FILESYSTEM. If cloud storage is ever added,
// declare the new enum value here and the writer code that produces it.
const (
	// StorageKindFilesystem stores the binary on the API server's local
	// filesystem under a relative path.
	StorageKindFilesystem StorageKind = "FILESYSTEM"
)

// ParseStorageKind parses a string into a StorageKind or returns
// ErrStorageKindInvalido.
func ParseStorageKind(s string) (StorageKind, error) {
	k := StorageKind(s)
	if !k.IsValid() {
		return "", ErrStorageKindInvalido
	}
	return k, nil
}

// IsValid reports whether k is a recognized StorageKind.
func (k StorageKind) IsValid() bool {
	return k == StorageKindFilesystem
}

// String returns the canonical string representation.
func (k StorageKind) String() string { return string(k) }

// Allowed image MIME types matching MSP_VENTAS_IMAGENES.MIME.
const (
	// MimeJPEG is the canonical mime for JPEG images.
	MimeJPEG = "image/jpeg"
	// MimePNG is the canonical mime for PNG images.
	MimePNG = "image/png"
	// MimeGIF is the canonical mime for GIF images.
	MimeGIF = "image/gif"
	// MimeWebP is the canonical mime for WebP images.
	MimeWebP = "image/webp"
)

// IsAllowedMime reports whether m is one of the allowed image mime types.
func IsAllowedMime(m string) bool {
	switch m {
	case MimeJPEG, MimePNG, MimeGIF, MimeWebP:
		return true
	}
	return false
}

// ImagenStorage couples a StorageKind with the storage-specific key (a path
// for FILESYSTEM, an object key for R2). The key is validated to be
// non-empty, length-bounded, free of NUL bytes, free of path-traversal
// segments, and without a leading slash.
type ImagenStorage struct {
	kind StorageKind
	key  string
}

// NewImagenStorage validates and constructs an ImagenStorage.
func NewImagenStorage(kind StorageKind, key string) (ImagenStorage, error) {
	if !kind.IsValid() {
		return ImagenStorage{}, ErrStorageKindInvalido
	}
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return ImagenStorage{}, ErrStorageKeyInvalida
	}
	if len(trimmed) > maxStorageKeyLength {
		return ImagenStorage{}, ErrStorageKeyInvalida
	}
	if !isSafeStorageKey(trimmed) {
		return ImagenStorage{}, ErrStorageKeyInvalida
	}
	return ImagenStorage{kind: kind, key: trimmed}, nil
}

// HydrateImagenStorage rebuilds an ImagenStorage from persistence without
// validation.
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

// isSafeStorageKey checks the key for the simple, structural unsafe patterns:
// NUL bytes, leading '/' (we always want relative paths), and the ".."
// traversal segment.
func isSafeStorageKey(key string) bool {
	if strings.ContainsRune(key, 0) {
		return false
	}
	if strings.HasPrefix(key, "/") {
		return false
	}
	if strings.Contains(key, "..") {
		return false
	}
	return true
}
