package failedintent_test

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ─── memoryBlobStorage ────────────────────────────────────────────────────────

// memoryBlobStorage is a tiny in-memory implementation of BlobStorage
// for the parser tests — pure unit tests should not touch disk.
type memoryBlobStorage struct {
	blobs map[string][]byte
}

func newMemoryBlobStorage() *memoryBlobStorage {
	return &memoryBlobStorage{blobs: map[string][]byte{}}
}

func (m *memoryBlobStorage) Save(
	_ context.Context, _ uuid.UUID, _ io.Reader, _ int64,
) (string, error) {
	panic("not used in these tests")
}

func (m *memoryBlobStorage) Open(_ context.Context, path string) (io.ReadCloser, error) {
	b, ok := m.blobs[path]
	if !ok {
		return nil, failedintent.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *memoryBlobStorage) Delete(_ context.Context, path string) error {
	delete(m.blobs, path)
	return nil
}

func (m *memoryBlobStorage) put(path string, b []byte) {
	m.blobs[path] = b
}

// ─── builders ─────────────────────────────────────────────────────────────────

// buildMultipart writes a multipart/form-data body using textproto so the
// parts have exactly the headers we want — including missing ones.
func buildMultipart(t *testing.T, parts []multipartPart) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		hdr := textproto.MIMEHeader{}
		disp := `form-data; name="` + p.name + `"`
		if p.filename != "" {
			disp += `; filename="` + p.filename + `"`
		}
		hdr.Set("Content-Disposition", disp)
		if p.contentType != "" {
			hdr.Set("Content-Type", p.contentType)
		}
		pw, err := w.CreatePart(hdr)
		require.NoError(t, err)
		_, err = pw.Write(p.body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes(), "multipart/form-data; boundary=" + w.Boundary()
}

type multipartPart struct {
	name        string
	filename    string
	contentType string
	body        []byte
}

// ─── ListParts ────────────────────────────────────────────────────────────────

func TestListParts_ParsesFieldAndFile(t *testing.T) {
	t.Parallel()

	body, ct := buildMultipart(t, []multipartPart{
		{name: "venta_json", contentType: "application/json", body: []byte(`{"cliente":"Carlos"}`)},
		{name: "ine_frente", filename: "ine.jpg", contentType: "image/jpeg", body: bytes.Repeat([]byte{0xFF}, 1000)},
		{name: "evidencia", filename: "firma.png", contentType: "image/png", body: bytes.Repeat([]byte{0x89}, 250)},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/intent-1", body)
	insp := failedintent.NewBlobPartsInspector(store)

	parts, err := insp.ListParts(t.Context(), "/blob/intent-1", ct)
	require.NoError(t, err)
	require.Len(t, parts, 3)

	assert.Equal(t, 0, parts[0].Index)
	assert.Equal(t, "venta_json", parts[0].Name)
	assert.Equal(t, failedintent.BlobPartKindField, parts[0].Kind)
	assert.Equal(t, "application/json", parts[0].ContentType)
	assert.Empty(t, parts[0].Filename)
	assert.Equal(t, int64(len(`{"cliente":"Carlos"}`)), parts[0].SizeBytes)
	assert.JSONEq(t, `{"cliente":"Carlos"}`, string(parts[0].Value))

	assert.Equal(t, 1, parts[1].Index)
	assert.Equal(t, "ine_frente", parts[1].Name)
	assert.Equal(t, failedintent.BlobPartKindFile, parts[1].Kind)
	assert.Equal(t, "ine.jpg", parts[1].Filename)
	assert.Equal(t, "image/jpeg", parts[1].ContentType)
	assert.Equal(t, int64(1000), parts[1].SizeBytes)
	assert.Nil(t, parts[1].Value, "file parts must not carry inline bytes")

	assert.Equal(t, 2, parts[2].Index)
	assert.Equal(t, "evidencia", parts[2].Name)
	assert.Equal(t, "firma.png", parts[2].Filename)
	assert.Equal(t, int64(250), parts[2].SizeBytes)
}

func TestListParts_EmptyMultipart_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	body, ct := buildMultipart(t, nil)
	store := newMemoryBlobStorage()
	store.put("/blob/e", body)
	insp := failedintent.NewBlobPartsInspector(store)

	parts, err := insp.ListParts(t.Context(), "/blob/e", ct)
	require.NoError(t, err)
	assert.Empty(t, parts)
}

func TestListParts_OversizeField_ReclassifiesAsFile(t *testing.T) {
	t.Parallel()

	// 1 byte over the inline cap: the parser should treat it as a file.
	oversize := bytes.Repeat([]byte("a"), failedintent.MaxInlineFieldBytes+1)
	body, ct := buildMultipart(t, []multipartPart{
		{name: "huge_field", contentType: "text/plain", body: oversize},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/big", body)
	insp := failedintent.NewBlobPartsInspector(store)

	parts, err := insp.ListParts(t.Context(), "/blob/big", ct)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, failedintent.BlobPartKindFile, parts[0].Kind,
		"oversize field must reclassify as file so inspect payload stays bounded")
	assert.Equal(t, int64(len(oversize)), parts[0].SizeBytes)
	assert.Nil(t, parts[0].Value)
}

func TestListParts_MissingContentTypeDefaultsToOctetStream(t *testing.T) {
	t.Parallel()

	body, ct := buildMultipart(t, []multipartPart{
		{name: "unknown", filename: "blob.bin", body: []byte{0x01, 0x02, 0x03}},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/x", body)
	insp := failedintent.NewBlobPartsInspector(store)

	parts, err := insp.ListParts(t.Context(), "/blob/x", ct)
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Equal(t, "application/octet-stream", parts[0].ContentType)
}

func TestListParts_NotMultipartContentType_ReturnsErrBlobNotMultipart(t *testing.T) {
	t.Parallel()

	store := newMemoryBlobStorage()
	store.put("/blob/x", []byte("{}"))
	insp := failedintent.NewBlobPartsInspector(store)

	_, err := insp.ListParts(t.Context(), "/blob/x", "application/json")
	assert.ErrorIs(t, err, failedintent.ErrBlobNotMultipart)
}

func TestListParts_MissingBoundary_ReturnsError(t *testing.T) {
	t.Parallel()

	store := newMemoryBlobStorage()
	store.put("/blob/x", []byte("--xx"))
	insp := failedintent.NewBlobPartsInspector(store)

	_, err := insp.ListParts(t.Context(), "/blob/x", "multipart/form-data")
	assert.ErrorContains(t, err, "missing boundary")
}

func TestListParts_BlobNotFound_PropagatesStorageError(t *testing.T) {
	t.Parallel()

	insp := failedintent.NewBlobPartsInspector(newMemoryBlobStorage())
	_, err := insp.ListParts(t.Context(), "/no/such",
		"multipart/form-data; boundary=---boundary")
	assert.ErrorIs(t, err, failedintent.ErrBlobNotFound)
}

func TestListParts_NilStorage_Panics(t *testing.T) {
	t.Parallel()
	assert.Panics(t, func() { failedintent.NewBlobPartsInspector(nil) })
}

func TestListParts_CorruptHeader_ReturnsParseError(t *testing.T) {
	t.Parallel()

	// Construct a body where a part's header line is malformed (no colon
	// in the Content-Disposition value). Go's mime/multipart.NextPart()
	// rejects this; the parser surfaces the wrapped error.
	const boundary = "ZZZcorruptZZZ"
	corrupt := strings.Join([]string{
		"--" + boundary,
		"This is not a valid header line",
		"",
		"body bytes",
		"--" + boundary + "--",
		"",
	}, "\r\n")
	store := newMemoryBlobStorage()
	store.put("/blob/corrupt", []byte(corrupt))
	insp := failedintent.NewBlobPartsInspector(store)

	_, err := insp.ListParts(t.Context(), "/blob/corrupt",
		"multipart/form-data; boundary="+boundary)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse part")
}

// ─── DownloadPart ─────────────────────────────────────────────────────────────

func TestDownloadPart_StreamsRequestedPart(t *testing.T) {
	t.Parallel()

	wantBytes := bytes.Repeat([]byte{0xAB}, 5000)
	body, ct := buildMultipart(t, []multipartPart{
		{name: "venta_json", contentType: "application/json", body: []byte(`{"a":1}`)},
		{name: "ine", filename: "ine.jpg", contentType: "image/jpeg", body: wantBytes},
		{name: "firma", filename: "firma.png", contentType: "image/png", body: []byte{1, 2, 3}},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/d", body)
	insp := failedintent.NewBlobPartsInspector(store)

	var out bytes.Buffer
	meta, err := insp.DownloadPart(t.Context(), "/blob/d", ct, 1, nil, &out)
	require.NoError(t, err)
	assert.Equal(t, wantBytes, out.Bytes())
	assert.Equal(t, "ine.jpg", meta.Filename)
	assert.Equal(t, "image/jpeg", meta.ContentType)
	assert.Equal(t, int64(len(wantBytes)), meta.SizeBytes)
}

func TestDownloadPart_OutOfRange_ReturnsError(t *testing.T) {
	t.Parallel()

	body, ct := buildMultipart(t, []multipartPart{
		{name: "a", contentType: "text/plain", body: []byte("hi")},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/r", body)
	insp := failedintent.NewBlobPartsInspector(store)

	_, err := insp.DownloadPart(t.Context(), "/blob/r", ct, 5, nil, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestDownloadPart_OnLocated_FiresBeforeBodyWrite(t *testing.T) {
	t.Parallel()

	wantBytes := bytes.Repeat([]byte{0xAB}, 1024)
	body, ct := buildMultipart(t, []multipartPart{
		{name: "venta_json", contentType: "application/json", body: []byte(`{"a":1}`)},
		{name: "ine", filename: "ine.jpg", contentType: "image/jpeg", body: wantBytes},
	})
	store := newMemoryBlobStorage()
	store.put("/blob/cb", body)
	insp := failedintent.NewBlobPartsInspector(store)

	// observer logs the order in which onLocated and writes interleave.
	var calls []string
	observer := &observingWriter{
		write: func(p []byte) {
			calls = append(calls, "write")
			_ = p
		},
	}
	onLocated := func(p failedintent.BlobPart) {
		calls = append(calls, "located:"+p.Filename)
	}

	_, err := insp.DownloadPart(t.Context(), "/blob/cb", ct, 1, onLocated, observer)
	require.NoError(t, err)

	require.NotEmpty(t, calls)
	assert.Equal(t, "located:ine.jpg", calls[0],
		"onLocated must fire before any write call so the HTTP handler can set headers")
}

// observingWriter is an io.Writer that records every Write call via a hook.
type observingWriter struct {
	write func(p []byte)
}

func (o *observingWriter) Write(p []byte) (int, error) {
	if o.write != nil {
		o.write(p)
	}
	return len(p), nil
}

func TestDownloadPart_NegativeIndex_Rejected(t *testing.T) {
	t.Parallel()
	insp := failedintent.NewBlobPartsInspector(newMemoryBlobStorage())
	_, err := insp.DownloadPart(t.Context(), "x",
		"multipart/form-data; boundary=---x", -1, nil, io.Discard)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "negative")
}
