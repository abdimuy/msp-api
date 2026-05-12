package imageprocessor

import "net/http"

// Canonical MIME constants. Centralized so callers and tests agree on the
// exact spelling (the stdlib returns these strings verbatim).
const (
	MimeJPEG = "image/jpeg"
	MimePNG  = "image/png"
	MimeGIF  = "image/gif"
	MimeWebP = "image/webp"
)

// DetectActualMIME sniffs the leading bytes of an image body and returns
// the canonical MIME if it is one of the supported formats, otherwise the
// empty string. Wraps [net/http.DetectContentType] which is pure Go and
// reads up to 512 bytes.
func DetectActualMIME(prefix []byte) string {
	sniffed := http.DetectContentType(prefix)
	switch sniffed {
	case MimeJPEG, MimePNG, MimeGIF, MimeWebP:
		return sniffed
	}
	return ""
}
