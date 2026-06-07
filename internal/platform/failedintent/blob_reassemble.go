package failedintent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/textproto"
)

// ManifestPart describes one section of the reassembled multipart body.
// The Source discriminator picks between three modes: keep the bytes of
// an original part, inline a new field value, or pull bytes from a
// caller-supplied upload.
type ManifestPart struct {
	// Name is the new form field name. Required.
	Name string

	// ContentType overrides the section's Content-Type header. When empty,
	// the reassembler defaults: for kept/upload parts it inherits from the
	// source; for field parts it falls back to "text/plain; charset=utf-8".
	ContentType string

	// Filename, when non-empty, populates the filename= parameter in the
	// Content-Disposition header — the marker that turns the section into
	// a file upload from the receiver's perspective.
	Filename string

	// Source decides where the body bytes come from.
	Source ManifestSource
}

// ManifestSourceKind enumerates the three modes the reassembler supports.
type ManifestSourceKind string

const (
	// ManifestSourceKeep copies the bytes of an original part by index.
	// The original Content-Type is reused unless ContentType overrides it.
	ManifestSourceKeep ManifestSourceKind = "keep"

	// ManifestSourceField inlines a new value. Used to edit text fields,
	// JSON payloads, etc. Filename should be empty for fields.
	ManifestSourceField ManifestSourceKind = "field"

	// ManifestSourceUpload pulls bytes from a caller-supplied upload keyed
	// by UploadField. Used when the operator replaces an old file or adds
	// a new one.
	ManifestSourceUpload ManifestSourceKind = "upload"
)

// ManifestSource selects the body bytes for a ManifestPart. Exactly one
// of OriginalIndex, Value, UploadField is meaningful per Kind.
type ManifestSource struct {
	Kind ManifestSourceKind

	// OriginalIndex points to a section of the captured blob (kind=keep).
	OriginalIndex int

	// Value carries the bytes inlined into the new body (kind=field).
	Value []byte

	// UploadField references an entry in the Uploads map (kind=upload).
	UploadField string
}

// Upload is a stream of bytes contributed by the caller (in practice, a
// multipart.FileHeader from the incoming admin request). Open is called
// exactly once per upload referenced by the manifest.
type Upload struct {
	Filename    string
	ContentType string
	Open        func() (io.ReadCloser, error)
}

// ReassembleInput bundles everything the reassembler needs in a single
// argument so tests can build inputs inline without long parameter lists.
type ReassembleInput struct {
	OriginalBlobPath    string
	OriginalContentType string
	Parts               []ManifestPart
	Uploads             map[string]Upload
}

// Sentinel errors. Each maps to a stable apperror code at the HTTP edge.
var (
	// ErrManifestEmpty is returned when the manifest has zero parts.
	ErrManifestEmpty = errors.New("failedintent: manifest is empty")

	// ErrManifestPartNameEmpty is returned when a manifest part omits Name.
	ErrManifestPartNameEmpty = errors.New("failedintent: manifest part name is required")

	// ErrManifestPartKindInvalid is returned for an unknown Kind value.
	ErrManifestPartKindInvalid = errors.New("failedintent: manifest part kind is invalid")

	// ErrManifestKeepIndexInvalid is returned when a keep refers to an index
	// that is out of range for the original blob.
	ErrManifestKeepIndexInvalid = errors.New("failedintent: manifest keep index out of range")

	// ErrManifestUploadMissing is returned when a upload references a
	// missing form field.
	ErrManifestUploadMissing = errors.New("failedintent: manifest upload not found")
)

// Reassembler builds a fresh multipart body from a captured blob, an
// edit manifest, and a set of new uploads.
type Reassembler struct {
	storage BlobStorage
}

// NewReassembler wires the reassembler against the same storage the
// capture middleware writes to.
func NewReassembler(storage BlobStorage) *Reassembler {
	if storage == nil {
		panic("failedintent: NewReassembler requires a non-nil BlobStorage")
	}
	return &Reassembler{storage: storage}
}

// Reassemble walks the manifest in order, writes a fresh multipart body
// to w using a new boundary, and returns the Content-Type header to set
// on the dispatched request.
//
// The function is one-shot: every error aborts the partial output. The
// caller is expected to write into a buffer first and only forward the
// bytes downstream once Reassemble returns nil.
func (r *Reassembler) Reassemble(
	ctx context.Context, in ReassembleInput, w io.Writer,
) (string, error) {
	if len(in.Parts) == 0 {
		return "", ErrManifestEmpty
	}

	// Validate manifest shape up front so we never half-write a body.
	if err := validateManifest(in); err != nil {
		return "", err
	}

	// Plan keep indexes so we can fetch each original part exactly once.
	keepPlan, err := r.planKeeps(ctx, in)
	if err != nil {
		return "", err
	}

	mw := multipart.NewWriter(w)
	for i, p := range in.Parts {
		if writeErr := r.writeManifestPart(mw, p, in.Uploads, keepPlan); writeErr != nil {
			return "", fmt.Errorf("failedintent: write part %d (%q): %w", i, p.Name, writeErr)
		}
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("failedintent: close multipart writer: %w", err)
	}
	return "multipart/form-data; boundary=" + mw.Boundary(), nil
}

// validateManifest checks structural rules that don't require I/O.
func validateManifest(in ReassembleInput) error {
	for i, p := range in.Parts {
		if p.Name == "" {
			return fmt.Errorf("%w: index %d", ErrManifestPartNameEmpty, i)
		}
		switch p.Source.Kind {
		case ManifestSourceKeep, ManifestSourceField, ManifestSourceUpload:
		default:
			return fmt.Errorf("%w: index %d (%q): kind=%q",
				ErrManifestPartKindInvalid, i, p.Name, p.Source.Kind)
		}
		if p.Source.Kind == ManifestSourceUpload {
			if p.Source.UploadField == "" {
				return fmt.Errorf("%w: index %d (%q): missing upload_field",
					ErrManifestUploadMissing, i, p.Name)
			}
			if _, ok := in.Uploads[p.Source.UploadField]; !ok {
				return fmt.Errorf("%w: index %d (%q): upload_field=%q",
					ErrManifestUploadMissing, i, p.Name, p.Source.UploadField)
			}
		}
	}
	return nil
}

// planKeeps reads through the original blob ONCE, retaining the bytes of
// every part referenced by a keep directive. The map is keyed by index;
// values are the part bytes plus their original Content-Type.
//
// Reading all kept bytes into memory is simpler than the alternative
// (re-parse per keep with seek-discard, which is O(n²) in the part
// count). Multipart blobs are already capped at MaxMultipartBytes
// (50 MiB by default), so the memory ceiling is bounded.
func (r *Reassembler) planKeeps(
	ctx context.Context, in ReassembleInput,
) (map[int]keptPart, error) {
	wanted := map[int]struct{}{}
	for _, p := range in.Parts {
		if p.Source.Kind == ManifestSourceKeep {
			wanted[p.Source.OriginalIndex] = struct{}{}
		}
	}
	if len(wanted) == 0 {
		return map[int]keptPart{}, nil
	}

	boundary, err := extractBoundary(in.OriginalContentType)
	if err != nil {
		return nil, err
	}

	rc, err := r.storage.Open(ctx, in.OriginalBlobPath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	mr := multipart.NewReader(rc, boundary)
	kept := map[int]keptPart{}
	for index := 0; ; index++ {
		part, perr := mr.NextPart()
		if errors.Is(perr, io.EOF) {
			break
		}
		if perr != nil {
			return nil, fmt.Errorf("failedintent: keep plan parse part %d: %w", index, perr)
		}
		if _, want := wanted[index]; !want {
			if _, derr := io.Copy(io.Discard, part); derr != nil {
				_ = part.Close()
				return nil, fmt.Errorf("failedintent: keep plan discard part %d: %w", index, derr)
			}
			_ = part.Close()
			continue
		}
		body, rerr := io.ReadAll(part)
		_ = part.Close()
		if rerr != nil {
			return nil, fmt.Errorf("failedintent: keep plan read part %d: %w", index, rerr)
		}
		ct := part.Header.Get("Content-Type")
		if ct == "" {
			ct = "application/octet-stream"
		}
		kept[index] = keptPart{contentType: ct, body: body, filename: part.FileName()}
	}

	// Surface keep indexes that were requested but never seen.
	for idx := range wanted {
		if _, ok := kept[idx]; !ok {
			return nil, fmt.Errorf("%w: %d", ErrManifestKeepIndexInvalid, idx)
		}
	}
	return kept, nil
}

type keptPart struct {
	contentType string
	body        []byte
	filename    string
}

// writeManifestPart writes a single manifest entry into the multipart
// writer.
func (r *Reassembler) writeManifestPart(
	mw *multipart.Writer,
	p ManifestPart,
	uploads map[string]Upload,
	keepPlan map[int]keptPart,
) error {
	hdr := textproto.MIMEHeader{}

	switch p.Source.Kind {
	case ManifestSourceField:
		setDisposition(hdr, p.Name, p.Filename)
		setContentType(hdr, p.ContentType, "text/plain; charset=utf-8")
		w, err := mw.CreatePart(hdr)
		if err != nil {
			return err
		}
		_, err = w.Write(p.Source.Value)
		return err

	case ManifestSourceKeep:
		kept := keepPlan[p.Source.OriginalIndex]
		filename := p.Filename
		if filename == "" {
			filename = kept.filename
		}
		setDisposition(hdr, p.Name, filename)
		setContentType(hdr, p.ContentType, kept.contentType)
		w, err := mw.CreatePart(hdr)
		if err != nil {
			return err
		}
		_, err = w.Write(kept.body)
		return err

	case ManifestSourceUpload:
		up := uploads[p.Source.UploadField]
		filename := p.Filename
		if filename == "" {
			filename = up.Filename
		}
		setDisposition(hdr, p.Name, filename)
		setContentType(hdr, p.ContentType, up.ContentType)
		w, err := mw.CreatePart(hdr)
		if err != nil {
			return err
		}
		rc, err := up.Open()
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()
		_, err = io.Copy(w, rc)
		return err
	}
	// Already validated up front.
	return fmt.Errorf("%w: %q", ErrManifestPartKindInvalid, p.Source.Kind)
}

func setDisposition(hdr textproto.MIMEHeader, name, filename string) {
	if filename == "" {
		hdr.Set("Content-Disposition",
			`form-data; name="`+escapeQuotedString(name)+`"`)
		return
	}
	hdr.Set("Content-Disposition",
		`form-data; name="`+escapeQuotedString(name)+
			`"; filename="`+escapeQuotedString(filename)+`"`)
}

func setContentType(hdr textproto.MIMEHeader, override, fallback string) {
	ct := override
	if ct == "" {
		ct = fallback
	}
	if ct == "" {
		ct = "application/octet-stream"
	}
	hdr.Set("Content-Type", ct)
}

// escapeQuotedString escapes the small set of bytes RFC 2616 forbids in
// quoted-string headers. The multipart library does this too, but we
// build the header by hand for clarity.
func escapeQuotedString(s string) string {
	// Replace backslash first to avoid double-escaping the inserted ones.
	out := make([]byte, 0, len(s))
	for i := range len(s) {
		switch s[i] {
		case '\\', '"':
			out = append(out, '\\')
		}
		out = append(out, s[i])
	}
	return string(out)
}
