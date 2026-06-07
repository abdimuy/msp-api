package failedintenthttp_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// makeReplayMultipartRequest builds the operator → admin request body
// (a multipart form with __manifest + zero or more file uploads).
type uploadFixture struct {
	formName    string
	filename    string
	contentType string
	body        []byte
}

func makeReplayMultipartRequest(
	t *testing.T,
	manifest map[string]any,
	uploads []uploadFixture,
) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	if manifest != nil {
		manifestJSON, err := json.Marshal(manifest)
		require.NoError(t, err)
		require.NoError(t, w.WriteField("__manifest", string(manifestJSON)))
	}
	for _, up := range uploads {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition",
			`form-data; name="`+up.formName+`"; filename="`+up.filename+`"`)
		hdr.Set("Content-Type", up.contentType)
		pw, err := w.CreatePart(hdr)
		require.NoError(t, err)
		_, err = pw.Write(up.body)
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes(), w.FormDataContentType()
}

// ─── happy path: keep + field + upload ────────────────────────────────────────

func TestReplayWithMultipart_HappyPath_DispatchesReassembledBody(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	userID := uuid.New()

	// Original blob: JSON field with wrong cliente + one INE photo.
	originalJPG := bytes.Repeat([]byte{0xFF, 0xD8}, 200)
	originalBody, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/replay"
	intent.BodyContentType = ct
	intent.UsuarioID = &userID
	seedIntent(t, store, intent)
	blobs.put("/blob/replay", originalBody)
	_ = originalJPG

	dispatcher := &fakeDispatcher{respondStatus: http.StatusCreated, respondBody: []byte(`{"ok":true}`)}
	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: userID}}
	svc := failedintenthttp.NewService(store, dispatcher, lookup, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Manifest: edit venta_json, keep INE, replace evidencia with a fresh upload.
	freshFirma := bytes.Repeat([]byte{0xCA, 0xFE}, 50)
	manifest := map[string]any{
		"parts": []map[string]any{
			{
				"name":         "venta_json",
				"content_type": "application/json",
				"source": map[string]any{
					"kind":  "field",
					"value": base64.StdEncoding.EncodeToString([]byte(`{"cliente":"Juan FIXED","monto":9500}`)),
				},
			},
			{
				"name":     "ine_frente",
				"filename": "ine.jpg",
				"source": map[string]any{
					"kind":           "keep",
					"original_index": 1,
				},
			},
			{
				"name":     "evidencia",
				"filename": "firma_v2.png",
				"source": map[string]any{
					"kind":         "upload",
					"upload_field": "file_evidencia",
				},
			},
		},
	}
	reqBody, reqCT := makeReplayMultipartRequest(t, manifest, []uploadFixture{
		{
			formName: "file_evidencia", filename: "firma_v2.png",
			contentType: "image/png", body: freshFirma,
		},
	})

	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "retried_ok", resp.Outcome)
	assert.Equal(t, http.StatusCreated, resp.ReplayHTTPStatus)

	// Verify dispatcher saw a reassembled multipart body.
	require.Equal(t, 1, dispatcher.callCount())
	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched)
	assert.True(t, strings.HasPrefix(dispatched.Header.Get("Content-Type"),
		"multipart/form-data"))

	// Parse the dispatched body and check the three sections.
	dispatchedBody := dispatcher.lastBody()
	parts := parseDispatched(t, dispatchedBody, dispatched.Header.Get("Content-Type"))
	require.Len(t, parts, 3)
	assert.Equal(t, "venta_json", parts[0].name)
	assert.JSONEq(t, `{"cliente":"Juan FIXED","monto":9500}`, string(parts[0].body))
	assert.Equal(t, "ine_frente", parts[1].name)
	assert.Equal(t, "evidencia", parts[2].name)
	assert.Equal(t, freshFirma, parts[2].body)
}

type dispatchedPart struct {
	name     string
	filename string
	body     []byte
}

func parseDispatched(t *testing.T, body []byte, contentType string) []dispatchedPart {
	t.Helper()
	_, params, err := parseMediaTypeShim(contentType)
	require.NoError(t, err)
	boundary, ok := params["boundary"]
	require.True(t, ok)
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	var out []dispatchedPart
	for {
		p, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		buf, err := io.ReadAll(p)
		require.NoError(t, err)
		out = append(out, dispatchedPart{
			name:     p.FormName(),
			filename: p.FileName(),
			body:     buf,
		})
		_ = p.Close()
	}
	return out
}

func parseMediaTypeShim(ct string) (string, map[string]string, error) {
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
		v := strings.Trim(kv[eq+1:], `"`)
		out[strings.ToLower(kv[:eq])] = v
	}
	return strings.TrimSpace(parts[0]), out, nil
}

// ─── validation paths ─────────────────────────────────────────────────────────

func TestReplayWithMultipart_MissingManifest_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	userID := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/m"
	intent.BodyContentType = ct
	intent.UsuarioID = &userID
	seedIntent(t, store, intent)
	blobs.put("/blob/m", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	reqBody, reqCT := makeReplayMultipartRequest(t, nil, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "replay_manifest_required", p.Code)
}

func TestReplayWithMultipart_MalformedManifestJSON_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	userID := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/m2"
	intent.BodyContentType = ct
	intent.UsuarioID = &userID
	seedIntent(t, store, intent)
	blobs.put("/blob/m2", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	// Craft a request where __manifest is invalid JSON.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	require.NoError(t, w.WriteField("__manifest", "{ not valid"))
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "invalid_replay_manifest", p.Code)
}

func TestReplayWithMultipart_JSONIntent_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	// JSON intent — no blob.
	seedIntent(t, store, makeIntent(id, time.Now().UTC()))

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	reqBody, reqCT := makeReplayMultipartRequest(t,
		map[string]any{
			"parts": []map[string]any{
				{"name": "x", "source": map[string]any{"kind": "field", "value": ""}},
			},
		}, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_no_blob", p.Code)
}

func TestReplayWithMultipart_UploadFieldReferencedButMissing_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	userID := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/upload-missing"
	intent.BodyContentType = ct
	intent.UsuarioID = &userID
	seedIntent(t, store, intent)
	blobs.put("/blob/upload-missing", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{},
		&stubUsuarioLookup{user: auth.CurrentUser{ID: userID}}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	manifest := map[string]any{
		"parts": []map[string]any{
			{
				"name": "evidencia",
				"source": map[string]any{
					"kind":         "upload",
					"upload_field": "file_NOT_SENT",
				},
			},
		},
	}
	reqBody, reqCT := makeReplayMultipartRequest(t, manifest, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "manifest_upload_missing", p.Code)
}

func TestReplayWithMultipart_KeepIndexOutOfRange_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	userID := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/oor"
	intent.BodyContentType = ct
	intent.UsuarioID = &userID
	seedIntent(t, store, intent)
	blobs.put("/blob/oor", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{},
		&stubUsuarioLookup{user: auth.CurrentUser{ID: userID}}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	idx := 42
	manifest := map[string]any{
		"parts": []map[string]any{
			{
				"name": "ine",
				"source": map[string]any{
					"kind":           "keep",
					"original_index": idx,
				},
			},
		},
	}
	reqBody, reqCT := makeReplayMultipartRequest(t, manifest, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "manifest_keep_index_invalid", p.Code)
}

func TestReplayWithMultipart_IntentNotFound_Returns404(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	missing := uuid.New().String()
	manifest := map[string]any{
		"parts": []map[string]any{
			{"name": "x", "source": map[string]any{"kind": "field", "value": ""}},
		},
	}
	reqBody, reqCT := makeReplayMultipartRequest(t, manifest, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+missing+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "failed_intent_not_found", p.Code)
}

func TestReplayWithMultipart_IntentMissingUsuario_Returns422(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/no-usr"
	intent.BodyContentType = ct
	intent.UsuarioID = nil
	seedIntent(t, store, intent)
	blobs.put("/blob/no-usr", body)

	svc := failedintenthttp.NewService(store, &fakeDispatcher{}, &stubUsuarioLookup{}, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	manifest := map[string]any{
		"parts": []map[string]any{
			{"name": "x", "source": map[string]any{"kind": "field", "value": ""}},
		},
	}
	reqBody, reqCT := makeReplayMultipartRequest(t, manifest, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
	p := parseProblem(t, rec.Body.Bytes())
	assert.Equal(t, "intent_has_no_usuario", p.Code)
}

// TestReplayWithMultipart_FreshIdempotencyKey is the regression guard
// that the new path also mints a fresh Idempotency-Key — reusing the
// captured one would 409 against the idempotency middleware.
func TestReplayWithMultipart_FreshIdempotencyKey(t *testing.T) {
	t.Parallel()

	store := newMemoryStore()
	blobs := newMemoryBlobs()
	id := uuid.New()
	userID := uuid.New()
	body, ct, _ := buildMultipartFixture(t)
	intent := makeIntent(id, time.Now().UTC())
	intent.BodyBlobPath = "/blob/idem"
	intent.BodyContentType = ct
	intent.IdempotencyKey = "user-original-key"
	intent.UsuarioID = &userID
	seedIntent(t, store, intent)
	blobs.put("/blob/idem", body)

	dispatcher := &fakeDispatcher{respondStatus: http.StatusOK}
	lookup := &stubUsuarioLookup{user: auth.CurrentUser{ID: userID}}
	svc := failedintenthttp.NewService(store, dispatcher, lookup, blobs, nil, nil)
	cu := defaultCU()
	r := newRouter(t, svc, &cu)

	manifest := map[string]any{
		"parts": []map[string]any{
			{
				"name":         "venta_json",
				"content_type": "application/json",
				"source": map[string]any{
					"kind":  "field",
					"value": base64.StdEncoding.EncodeToString([]byte(`{}`)),
				},
			},
		},
	}
	reqBody, reqCT := makeReplayMultipartRequest(t, manifest, nil)
	req := httptest.NewRequest(http.MethodPost,
		"/"+id.String()+"/replay-with-multipart", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", reqCT)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	dispatched := dispatcher.lastRequest()
	require.NotNil(t, dispatched)
	fresh := dispatched.Header.Get("Idempotency-Key")
	assert.NotEmpty(t, fresh)
	assert.NotEqual(t, intent.IdempotencyKey, fresh,
		"replay-with-multipart must mint a fresh Idempotency-Key, never reuse the captured one")
}
