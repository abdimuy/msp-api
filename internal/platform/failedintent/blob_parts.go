package failedintent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
)

// BlobPartKind classifies a parsed multipart section. Fields carry inline
// text (form values, JSON snippets, etc.) that the UI surfaces editable.
// Files reference upload payloads whose bytes only flow through the
// /:index/download endpoint — keeping them out of inspection responses
// keeps that payload light even for sales with several photos.
type BlobPartKind string

// BlobPartKindField / BlobPartKindFile are the only two valid kinds.
const (
	BlobPartKindField BlobPartKind = "field"
	BlobPartKindFile  BlobPartKind = "file"
)

// Sentinel errors for blob part inspection. Defined as package-level
// variables so callers can wrap and tests can errors.Is against them.
var (
	errBlobPartNegativeIndex = errors.New("failedintent: blob part index is negative")
	// ErrBlobPartOutOfRange is returned by DownloadPart when the requested
	// index exceeds the parts count in the multipart body. Exported so the
	// HTTP layer can translate it into a 422 with a stable apperror code.
	ErrBlobPartOutOfRange  = errors.New("failedintent: blob part index out of range")
	errBlobMissingBoundary = errors.New("failedintent: missing boundary in content type")
)

// MaxInlineFieldBytes caps the inline `Value` returned for a field part.
// Anything larger is reclassified as a "file" so the inspect response stays
// bounded — the UI can always pull the full bytes via the download
// endpoint if it really needs them.
const MaxInlineFieldBytes = 64 * 1024 // 64 KiB

// BlobPart describes one section of a captured multipart body. The struct
// is the unit returned by BlobPartsInspector both for the admin /blob-parts
// endpoint and (without `Value`) for the replay-with-multipart reassembler.
type BlobPart struct {
	// Index is the zero-based position of the part in the original body.
	// It is the manifest reference used by replay-with-multipart `keep`
	// directives.
	Index int

	// Name is the form field name from Content-Disposition (e.g.
	// "venta_json", "ine_frente"). Empty if the original part omitted it.
	Name string

	// Kind classifies the part — see BlobPartKind.
	Kind BlobPartKind

	// ContentType is the parsed media type of the part. Defaults to
	// "application/octet-stream" when the original part omitted Content-Type.
	ContentType string

	// Filename comes from the Content-Disposition `filename=` parameter.
	// Files always carry this; fields never do.
	Filename string

	// SizeBytes is the byte length of the part's body. For fields it equals
	// len(Value). For files it is the total size on disk and is what the
	// download endpoint would stream.
	SizeBytes int64

	// Value carries the inline bytes of a field part, only when Kind ==
	// BlobPartKindField. Always nil for files. For fields whose body exceeds
	// MaxInlineFieldBytes the part is reclassified as a file (no inline value).
	Value []byte
}

// ErrBlobNotMultipart signals that the recorded body content type is not a
// multipart/form-data envelope. Callers should map this to a 422
// (`blob_intent_not_multipart`) — the intent has a blob but it isn't a
// form upload that can be split into parts.
var ErrBlobNotMultipart = errors.New("failedintent: blob is not multipart/form-data")

// BlobPartsInspector reads a stored multipart blob and parses it into
// BlobPart records. It does not retain file part bytes — those stream
// through DownloadPart on demand.
type BlobPartsInspector struct {
	storage BlobStorage
}

// NewBlobPartsInspector wires the inspector against the blob storage port.
// The storage must be the same instance the capture middleware writes to;
// in production both come from the same FilesystemProvider.
func NewBlobPartsInspector(storage BlobStorage) *BlobPartsInspector {
	if storage == nil {
		panic("failedintent: NewBlobPartsInspector requires a non-nil BlobStorage")
	}
	return &BlobPartsInspector{storage: storage}
}

// ListParts opens the blob at blobPath, parses every section using the
// boundary parsed out of contentType, and returns the structural summary.
// File bytes are NOT read into memory — only metadata. Field bytes up to
// MaxInlineFieldBytes are inlined; oversize fields degrade to file kind.
func (i *BlobPartsInspector) ListParts(
	ctx context.Context, blobPath, contentType string,
) ([]BlobPart, error) {
	boundary, err := extractBoundary(contentType)
	if err != nil {
		return nil, err
	}

	rc, err := i.storage.Open(ctx, blobPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	mr := multipart.NewReader(rc, boundary)
	parts := []BlobPart{}
	for index := 0; ; index++ {
		part, perr := mr.NextPart()
		if errors.Is(perr, io.EOF) {
			break
		}
		if perr != nil {
			return nil, fmt.Errorf("failedintent: parse part %d: %w", index, perr)
		}
		bp, parseErr := readPart(part, index)
		_ = part.Close()
		if parseErr != nil {
			return nil, fmt.Errorf("failedintent: read part %d: %w", index, parseErr)
		}
		parts = append(parts, bp)
	}
	return parts, nil
}

// DownloadPart opens the blob and streams the bytes of the part at the
// given index back through w. onLocated, if non-nil, is invoked once the
// target part has been found and its headers parsed but BEFORE any body
// bytes flow through w — the HTTP handler uses it to set Content-Type
// and Content-Disposition before the stream begins. The returned
// BlobPart has SizeBytes populated only after the stream completes
// (chunked transfer; no Content-Length is set up front).
//
// The implementation reads through earlier parts but discards their
// bytes — multipart is not random-access.
func (i *BlobPartsInspector) DownloadPart(
	ctx context.Context,
	blobPath, contentType string,
	targetIndex int,
	onLocated func(BlobPart),
	w io.Writer,
) (BlobPart, error) {
	boundary, err := extractBoundary(contentType)
	if err != nil {
		return BlobPart{}, err
	}
	if targetIndex < 0 {
		return BlobPart{}, fmt.Errorf("%w: %d", errBlobPartNegativeIndex, targetIndex)
	}

	rc, err := i.storage.Open(ctx, blobPath)
	if err != nil {
		return BlobPart{}, err
	}
	defer func() { _ = rc.Close() }()

	mr := multipart.NewReader(rc, boundary)
	for index := 0; ; index++ {
		part, perr := mr.NextPart()
		if errors.Is(perr, io.EOF) {
			return BlobPart{}, fmt.Errorf("%w: %d", ErrBlobPartOutOfRange, targetIndex)
		}
		if perr != nil {
			return BlobPart{}, fmt.Errorf("failedintent: parse part %d: %w", index, perr)
		}
		if index != targetIndex {
			// Discard the body and move on.
			if _, ierr := io.Copy(io.Discard, part); ierr != nil {
				_ = part.Close()
				return BlobPart{}, fmt.Errorf("failedintent: discard part %d: %w", index, ierr)
			}
			_ = part.Close()
			continue
		}
		// Found the requested part. Stream it to w while computing size.
		meta := partMeta(part, index)
		if onLocated != nil {
			onLocated(meta)
		}
		n, copyErr := io.Copy(w, part)
		_ = part.Close()
		if copyErr != nil {
			return BlobPart{}, fmt.Errorf("failedintent: stream part %d: %w", index, copyErr)
		}
		meta.SizeBytes = n
		return meta, nil
	}
}

// readPart consumes the part's body into memory (capped at
// MaxInlineFieldBytes for fields; capped at MaxInlineFieldBytes+1 for files
// to compute exact size for small files, or rolled into io.Copy(io.Discard,
// rest) when the file is larger).
func readPart(part *multipart.Part, index int) (BlobPart, error) {
	meta := partMeta(part, index)
	if meta.Kind != BlobPartKindField {
		return readFilePart(part, meta)
	}
	return readFieldPart(part, meta)
}

// readFieldPart reads up to MaxInlineFieldBytes+1 bytes into memory. If
// the part overflows we reclassify it as a file and drain the rest just
// to compute the exact byte count.
func readFieldPart(part *multipart.Part, meta BlobPart) (BlobPart, error) {
	buf := make([]byte, MaxInlineFieldBytes+1)
	n, err := io.ReadFull(part, buf)
	if err != nil &&
		!errors.Is(err, io.ErrUnexpectedEOF) &&
		!errors.Is(err, io.EOF) {
		return BlobPart{}, err
	}
	if n > MaxInlineFieldBytes {
		extra, drainErr := io.Copy(io.Discard, part)
		if drainErr != nil {
			return BlobPart{}, drainErr
		}
		meta.Kind = BlobPartKindFile
		meta.SizeBytes = int64(n) + extra
		return meta, nil
	}
	meta.SizeBytes = int64(n)
	meta.Value = append([]byte(nil), buf[:n]...)
	return meta, nil
}

// readFilePart counts the body bytes without retaining them.
func readFilePart(part *multipart.Part, meta BlobPart) (BlobPart, error) {
	size, err := io.Copy(io.Discard, part)
	if err != nil {
		return BlobPart{}, err
	}
	meta.SizeBytes = size
	return meta, nil
}

// partMeta builds the BlobPart skeleton from a multipart.Part's headers,
// minus SizeBytes/Value which the caller fills in.
func partMeta(part *multipart.Part, index int) BlobPart {
	ct := part.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	filename := part.FileName()
	kind := BlobPartKindField
	if filename != "" {
		kind = BlobPartKindFile
	}
	return BlobPart{
		Index:       index,
		Name:        part.FormName(),
		Kind:        kind,
		ContentType: ct,
		Filename:    filename,
	}
}

// extractBoundary returns the `boundary` parameter from a
// multipart/form-data content type. Returns ErrBlobNotMultipart when the
// media type is anything else.
func extractBoundary(contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return "", fmt.Errorf("failedintent: parse content type %q: %w", contentType, err)
	}
	if !strings.HasPrefix(mediaType, "multipart/") {
		return "", ErrBlobNotMultipart
	}
	boundary := params["boundary"]
	if boundary == "" {
		return "", fmt.Errorf("%w: %q", errBlobMissingBoundary, contentType)
	}
	return boundary, nil
}
