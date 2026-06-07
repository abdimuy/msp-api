package failedintent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// Reuse memoryBlobStorage from blob_parts_test.go (same package).

func makeUpload(filename, contentType string, body []byte) failedintent.Upload {
	return failedintent.Upload{
		Filename:    filename,
		ContentType: contentType,
		Open: func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(body)), nil
		},
	}
}

// parseReassembled parses the reassembler output back into a map
// keyed by part name so assertions stay readable.
type parsedPart struct {
	name        string
	filename    string
	contentType string
	body        []byte
}

func parseReassembled(t *testing.T, body []byte, contentType string) []parsedPart {
	t.Helper()
	_, params, err := mimeParseMediaType(contentType)
	require.NoError(t, err)
	boundary, ok := params["boundary"]
	require.True(t, ok)

	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	out := []parsedPart{}
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		buf, err := io.ReadAll(p)
		require.NoError(t, err)
		out = append(out, parsedPart{
			name:        p.FormName(),
			filename:    p.FileName(),
			contentType: p.Header.Get("Content-Type"),
			body:        buf,
		})
		_ = p.Close()
	}
	return out
}

// mimeParseMediaType is a tiny indirection so tests don't import "mime"
// just for this one call.
func mimeParseMediaType(ct string) (string, map[string]string, error) {
	parts := strings.SplitN(ct, ";", 2)
	if len(parts) == 1 {
		return parts[0], map[string]string{}, nil
	}
	pairs := strings.Split(parts[1], ";")
	out := map[string]string{}
	for _, kv := range pairs {
		kv = strings.TrimSpace(kv)
		eq := strings.Index(kv, "=")
		if eq <= 0 {
			continue
		}
		v := kv[eq+1:]
		v = strings.Trim(v, `"`)
		out[strings.ToLower(kv[:eq])] = v
	}
	return strings.TrimSpace(parts[0]), out, nil
}

// ─── happy paths ──────────────────────────────────────────────────────────────

func TestReassemble_KeepField_RoundTrip(t *testing.T) {
	t.Parallel()

	jpgBytes := bytes.Repeat([]byte{0xFF, 0xD8}, 1000)
	pngBytes := bytes.Repeat([]byte{0x89, 0x50}, 500)
	original, ct := buildMultipart(t, []multipartPart{
		{name: "venta_json", contentType: "application/json", body: []byte(`{"cliente":"Carlos"}`)},
		{name: "ine_frente", filename: "ine.jpg", contentType: "image/jpeg", body: jpgBytes},
		{name: "evidencia", filename: "firma.png", contentType: "image/png", body: pngBytes},
	})

	store := newMemoryBlobStorage()
	store.put("/blob/o", original)
	reas := failedintent.NewReassembler(store)

	var out bytes.Buffer
	newCT, err := reas.Reassemble(t.Context(), failedintent.ReassembleInput{
		OriginalBlobPath:    "/blob/o",
		OriginalContentType: ct,
		Parts: []failedintent.ManifestPart{
			{
				Name:        "venta_json",
				ContentType: "application/json",
				Source: failedintent.ManifestSource{
					Kind:  failedintent.ManifestSourceField,
					Value: []byte(`{"cliente":"Juan FIXED","monto":9500}`),
				},
			},
			{
				Name:     "ine_frente",
				Filename: "ine.jpg",
				Source: failedintent.ManifestSource{
					Kind:          failedintent.ManifestSourceKeep,
					OriginalIndex: 1,
				},
			},
			{
				Name:     "evidencia",
				Filename: "firma.png",
				Source: failedintent.ManifestSource{
					Kind:          failedintent.ManifestSourceKeep,
					OriginalIndex: 2,
				},
			},
		},
	}, &out)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(newCT, "multipart/form-data; boundary="),
		"new content type must announce a fresh boundary")
	assert.NotEqual(t, ct, newCT, "new boundary must NOT match the original")

	parts := parseReassembled(t, out.Bytes(), newCT)
	require.Len(t, parts, 3)

	assert.Equal(t, "venta_json", parts[0].name)
	assert.Empty(t, parts[0].filename)
	assert.Equal(t, "application/json", parts[0].contentType)
	assert.JSONEq(t, `{"cliente":"Juan FIXED","monto":9500}`, string(parts[0].body))

	assert.Equal(t, "ine_frente", parts[1].name)
	assert.Equal(t, "ine.jpg", parts[1].filename)
	assert.Equal(t, "image/jpeg", parts[1].contentType)
	assert.Equal(t, jpgBytes, parts[1].body)

	assert.Equal(t, "evidencia", parts[2].name)
	assert.Equal(t, "firma.png", parts[2].filename)
	assert.Equal(t, "image/png", parts[2].contentType)
	assert.Equal(t, pngBytes, parts[2].body)
}

func TestReassemble_UploadReplacesFile(t *testing.T) {
	t.Parallel()

	original, ct := buildMultipart(t, []multipartPart{
		{
			name: "ine", filename: "old.jpg", contentType: "image/jpeg",
			body: bytes.Repeat([]byte{0xAA}, 100),
		},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/r", original)
	reas := failedintent.NewReassembler(store)

	newPhoto := bytes.Repeat([]byte{0xBB}, 1234)
	var out bytes.Buffer
	newCT, err := reas.Reassemble(t.Context(), failedintent.ReassembleInput{
		OriginalBlobPath:    "/blob/r",
		OriginalContentType: ct,
		Parts: []failedintent.ManifestPart{
			{
				Name: "ine",
				Source: failedintent.ManifestSource{
					Kind:        failedintent.ManifestSourceUpload,
					UploadField: "file_0",
				},
			},
		},
		Uploads: map[string]failedintent.Upload{
			"file_0": makeUpload("new.jpg", "image/jpeg", newPhoto),
		},
	}, &out)
	require.NoError(t, err)

	parts := parseReassembled(t, out.Bytes(), newCT)
	require.Len(t, parts, 1)
	assert.Equal(t, "new.jpg", parts[0].filename)
	assert.Equal(t, "image/jpeg", parts[0].contentType)
	assert.Equal(t, newPhoto, parts[0].body)
}

func TestReassemble_RemoveOriginalPart_NotInManifest(t *testing.T) {
	t.Parallel()

	// Original has 3 parts; manifest only keeps 2.
	original, ct := buildMultipart(t, []multipartPart{
		{name: "a", contentType: "text/plain", body: []byte("aaa")},
		{name: "b", contentType: "text/plain", body: []byte("bbb")},
		{name: "c", contentType: "text/plain", body: []byte("ccc")},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/rm", original)
	reas := failedintent.NewReassembler(store)

	var out bytes.Buffer
	newCT, err := reas.Reassemble(t.Context(), failedintent.ReassembleInput{
		OriginalBlobPath:    "/blob/rm",
		OriginalContentType: ct,
		Parts: []failedintent.ManifestPart{
			{Name: "a", Source: failedintent.ManifestSource{Kind: failedintent.ManifestSourceKeep, OriginalIndex: 0}},
			{Name: "c", Source: failedintent.ManifestSource{Kind: failedintent.ManifestSourceKeep, OriginalIndex: 2}},
		},
	}, &out)
	require.NoError(t, err)

	parts := parseReassembled(t, out.Bytes(), newCT)
	require.Len(t, parts, 2)
	assert.Equal(t, "a", parts[0].name)
	assert.Equal(t, "c", parts[1].name)
	assert.Equal(t, "aaa", string(parts[0].body))
	assert.Equal(t, "ccc", string(parts[1].body))
}

func TestReassemble_AddBrandNewFile(t *testing.T) {
	t.Parallel()

	// Original has only a JSON field; manifest keeps it and adds a brand
	// new photo that the operator just attached.
	original, ct := buildMultipart(t, []multipartPart{
		{name: "venta_json", contentType: "application/json", body: []byte(`{"a":1}`)},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/add", original)
	reas := failedintent.NewReassembler(store)

	newPhoto := bytes.Repeat([]byte{0xDE, 0xAD, 0xBE, 0xEF}, 250)
	var out bytes.Buffer
	newCT, err := reas.Reassemble(t.Context(), failedintent.ReassembleInput{
		OriginalBlobPath:    "/blob/add",
		OriginalContentType: ct,
		Parts: []failedintent.ManifestPart{
			{
				Name: "venta_json", ContentType: "application/json",
				Source: failedintent.ManifestSource{Kind: failedintent.ManifestSourceKeep, OriginalIndex: 0},
			},
			{
				Name:   "nueva_evidencia",
				Source: failedintent.ManifestSource{Kind: failedintent.ManifestSourceUpload, UploadField: "file_0"},
			},
		},
		Uploads: map[string]failedintent.Upload{
			"file_0": makeUpload("brand_new.bin", "application/octet-stream", newPhoto),
		},
	}, &out)
	require.NoError(t, err)

	parts := parseReassembled(t, out.Bytes(), newCT)
	require.Len(t, parts, 2)
	assert.Equal(t, "brand_new.bin", parts[1].filename)
	assert.Equal(t, newPhoto, parts[1].body)
}

// ─── validation ───────────────────────────────────────────────────────────────

func TestReassemble_EmptyManifest_ErrManifestEmpty(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	_, err := reas.Reassemble(context.Background(),
		failedintent.ReassembleInput{}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrManifestEmpty)
}

func TestReassemble_PartWithoutName_Rejected(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	_, err := reas.Reassemble(context.Background(), failedintent.ReassembleInput{
		Parts: []failedintent.ManifestPart{
			{Source: failedintent.ManifestSource{Kind: failedintent.ManifestSourceField, Value: []byte("x")}},
		},
	}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrManifestPartNameEmpty)
}

func TestReassemble_UnknownKind_Rejected(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	_, err := reas.Reassemble(context.Background(), failedintent.ReassembleInput{
		Parts: []failedintent.ManifestPart{
			{Name: "a", Source: failedintent.ManifestSource{Kind: "garbage"}},
		},
	}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrManifestPartKindInvalid)
}

func TestReassemble_UploadFieldMissing_Rejected(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	_, err := reas.Reassemble(context.Background(), failedintent.ReassembleInput{
		Parts: []failedintent.ManifestPart{
			{Name: "x", Source: failedintent.ManifestSource{
				Kind: failedintent.ManifestSourceUpload, UploadField: "missing",
			}},
		},
		Uploads: map[string]failedintent.Upload{
			"file_other": makeUpload("a", "b", []byte("c")),
		},
	}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrManifestUploadMissing)
}

func TestReassemble_UploadFieldEmpty_Rejected(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	_, err := reas.Reassemble(context.Background(), failedintent.ReassembleInput{
		Parts: []failedintent.ManifestPart{
			{Name: "x", Source: failedintent.ManifestSource{
				Kind: failedintent.ManifestSourceUpload, UploadField: "",
			}},
		},
	}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrManifestUploadMissing)
}

func TestReassemble_KeepIndexOutOfRange_Rejected(t *testing.T) {
	t.Parallel()

	body, ct := buildMultipart(t, []multipartPart{
		{name: "a", contentType: "text/plain", body: []byte("hi")},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/k", body)
	reas := failedintent.NewReassembler(store)

	_, err := reas.Reassemble(t.Context(), failedintent.ReassembleInput{
		OriginalBlobPath:    "/blob/k",
		OriginalContentType: ct,
		Parts: []failedintent.ManifestPart{
			{Name: "x", Source: failedintent.ManifestSource{
				Kind: failedintent.ManifestSourceKeep, OriginalIndex: 99,
			}},
		},
	}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrManifestKeepIndexInvalid)
}

func TestReassemble_NilStorage_Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { failedintent.NewReassembler(nil) })
}

// ─── error propagation ────────────────────────────────────────────────────────

func TestReassemble_UploadOpenFails_PropagatesError(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	wantErr := assert.AnError
	_, err := reas.Reassemble(context.Background(), failedintent.ReassembleInput{
		Parts: []failedintent.ManifestPart{
			{Name: "x", Source: failedintent.ManifestSource{
				Kind: failedintent.ManifestSourceUpload, UploadField: "f",
			}},
		},
		Uploads: map[string]failedintent.Upload{
			"f": {
				Filename:    "a",
				ContentType: "b",
				Open: func() (io.ReadCloser, error) {
					return nil, wantErr
				},
			},
		},
	}, io.Discard)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestReassemble_OriginalBlobMissing_PropagatesNotFound(t *testing.T) {
	t.Parallel()
	reas := failedintent.NewReassembler(newMemoryBlobStorage())
	_, err := reas.Reassemble(context.Background(), failedintent.ReassembleInput{
		OriginalBlobPath:    "/missing",
		OriginalContentType: "multipart/form-data; boundary=---x",
		Parts: []failedintent.ManifestPart{
			{Name: "x", Source: failedintent.ManifestSource{
				Kind: failedintent.ManifestSourceKeep, OriginalIndex: 0,
			}},
		},
	}, io.Discard)
	assert.ErrorIs(t, err, failedintent.ErrBlobNotFound)
}

// silenced import to avoid unused linter — uuid is needed by the BlobStorage
// signature transitively, but the test file doesn't construct any UUIDs.
var _ = uuid.Nil
