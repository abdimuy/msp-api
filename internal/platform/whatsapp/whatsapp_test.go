package whatsapp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// messageIDResponse returns an http.HandlerFunc that writes a fixed
// Graph-API-style "message sent" response containing the given wamid.
func messageIDResponse(wamid string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"messaging_product": "whatsapp",
			"messages":          []map[string]any{{"id": wamid}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// metaErrorResponse returns an http.HandlerFunc that writes a Graph-API
// error body carrying the given Meta application error code, under the
// given HTTP status.
func metaErrorResponse(statusCode, code int, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		resp := map[string]any{
			"error": map[string]any{
				"message": message,
				"type":    "OAuthException",
				"code":    code,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// newEnabledClient builds a realClient pointed at srv.URL.
func newEnabledClient(t *testing.T, srv *httptest.Server) whatsapp.Client {
	t.Helper()
	return whatsapp.NewClient(config.WhatsApp{
		Enabled:           true,
		Token:             "test-token",
		PhoneNumberID:     "1234567890",
		BusinessAccountID: "waba-1",
		APIVersion:        "v21.0",
		BaseURL:           srv.URL,
		Timeout:           5 * time.Second,
	})
}

// ─── disabled client ─────────────────────────────────────────────────────────

func TestDisabledClient_AllMethods_ReturnErrWhatsAppDisabled(t *testing.T) {
	t.Parallel()
	c := whatsapp.NewClient(config.WhatsApp{Enabled: false})

	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if !errors.Is(err, whatsapp.ErrWhatsAppDisabled) {
		t.Errorf("SendText: want ErrWhatsAppDisabled, got %v", err)
	}

	_, err = c.SendTemplate(context.Background(), "5215500000000", whatsapp.Template{Name: "greeting", LanguageCode: "es_MX"})
	if !errors.Is(err, whatsapp.ErrWhatsAppDisabled) {
		t.Errorf("SendTemplate: want ErrWhatsAppDisabled, got %v", err)
	}

	_, err = c.SendDocumentByMediaID(context.Background(), "5215500000000", "media-1", "invoice.pdf", "tu recibo")
	if !errors.Is(err, whatsapp.ErrWhatsAppDisabled) {
		t.Errorf("SendDocumentByMediaID: want ErrWhatsAppDisabled, got %v", err)
	}

	_, err = c.UploadMedia(context.Background(), whatsapp.Media{Reader: strings.NewReader("x"), Filename: "f.pdf", MIMEType: "application/pdf"})
	if !errors.Is(err, whatsapp.ErrWhatsAppDisabled) {
		t.Errorf("UploadMedia: want ErrWhatsAppDisabled, got %v", err)
	}
}

// ─── real client happy paths ──────────────────────────────────────────────────

func TestRealClient_SendText_HappyPath(t *testing.T) {
	t.Parallel()
	const wantWamid = "wamid.TEXT123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v21.0/1234567890/messages" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %q", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("unexpected Authorization header: %q", got)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["type"] != "text" {
			t.Errorf("unexpected type: %v", body["type"])
		}
		if body["to"] != "5215500000000" {
			t.Errorf("unexpected to: %v", body["to"])
		}
		text, ok := body["text"].(map[string]any)
		if !ok || text["body"] != "hola mundo" {
			t.Errorf("unexpected text.body: %v", body["text"])
		}

		messageIDResponse(wantWamid)(w, r)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	got, err := c.SendText(context.Background(), "5215500000000", "hola mundo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantWamid {
		t.Errorf("want %q, got %q", wantWamid, got)
	}
}

func TestRealClient_SendTemplate_HappyPath(t *testing.T) {
	t.Parallel()
	const wantWamid = "wamid.TEMPLATE123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["type"] != "template" {
			t.Errorf("unexpected type: %v", body["type"])
		}
		tmpl, ok := body["template"].(map[string]any)
		if !ok {
			t.Fatal("expected template object")
		}
		if tmpl["name"] != "payment_reminder" {
			t.Errorf("unexpected template.name: %v", tmpl["name"])
		}
		lang, ok := tmpl["language"].(map[string]any)
		if !ok || lang["code"] != "es_MX" {
			t.Errorf("unexpected template.language: %v", tmpl["language"])
		}
		comps, ok := tmpl["components"].([]any)
		if !ok || len(comps) != 1 {
			t.Fatalf("expected one component, got %v", tmpl["components"])
		}
		comp := comps[0].(map[string]any)
		params, ok := comp["parameters"].([]any)
		if !ok || len(params) != 2 {
			t.Fatalf("expected two parameters, got %v", comp["parameters"])
		}
		p0 := params[0].(map[string]any)
		if p0["type"] != "text" || p0["text"] != "Juan" {
			t.Errorf("unexpected parameter[0]: %v", p0)
		}
		p1 := params[1].(map[string]any)
		if p1["type"] != "text" || p1["text"] != "$500" {
			t.Errorf("unexpected parameter[1]: %v", p1)
		}

		messageIDResponse(wantWamid)(w, r)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	got, err := c.SendTemplate(context.Background(), "5215500000000", whatsapp.Template{
		Name:         "payment_reminder",
		LanguageCode: "es_MX",
		BodyParams:   []string{"Juan", "$500"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantWamid {
		t.Errorf("want %q, got %q", wantWamid, got)
	}
}

func TestRealClient_SendDocumentByMediaID_HappyPath(t *testing.T) {
	t.Parallel()
	const wantWamid = "wamid.DOC123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["type"] != "document" {
			t.Errorf("unexpected type: %v", body["type"])
		}
		doc, ok := body["document"].(map[string]any)
		if !ok {
			t.Fatal("expected document object")
		}
		if doc["id"] != "media-abc" {
			t.Errorf("unexpected document.id: %v", doc["id"])
		}
		if doc["filename"] != "recibo.pdf" {
			t.Errorf("unexpected document.filename: %v", doc["filename"])
		}
		if doc["caption"] != "tu recibo de pago" {
			t.Errorf("unexpected document.caption: %v", doc["caption"])
		}

		messageIDResponse(wantWamid)(w, r)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	got, err := c.SendDocumentByMediaID(context.Background(), "5215500000000", "media-abc", "recibo.pdf", "tu recibo de pago")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantWamid {
		t.Errorf("want %q, got %q", wantWamid, got)
	}
}

func TestRealClient_UploadMedia_HappyPath(t *testing.T) {
	t.Parallel()
	const wantMediaID = "media-xyz"
	const fileContent = "%PDF-1.4 fake pdf content"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v21.0/1234567890/media" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}

		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Fatalf("expected multipart content type, got %q (%v)", r.Header.Get("Content-Type"), err)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		form, err := mr.ReadForm(1 << 20)
		if err != nil {
			t.Fatalf("read multipart form: %v", err)
		}
		if got := form.Value["messaging_product"]; len(got) != 1 || got[0] != "whatsapp" {
			t.Errorf("unexpected messaging_product field: %v", got)
		}
		if got := form.Value["type"]; len(got) != 1 || got[0] != "application/pdf" {
			t.Errorf("unexpected type field: %v", got)
		}
		fileHeaders, ok := form.File["file"]
		if !ok || len(fileHeaders) != 1 {
			t.Fatalf("expected one file part, got %v", form.File)
		}
		if fileHeaders[0].Filename != "recibo.pdf" {
			t.Errorf("unexpected filename: %q", fileHeaders[0].Filename)
		}
		f, err := fileHeaders[0].Open()
		if err != nil {
			t.Fatalf("open file part: %v", err)
		}
		defer func() { _ = f.Close() }()
		got, err := io.ReadAll(f)
		if err != nil {
			t.Fatalf("read file part: %v", err)
		}
		if string(got) != fileContent {
			t.Errorf("unexpected file content: %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": wantMediaID})
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	got, err := c.UploadMedia(context.Background(), whatsapp.Media{
		Reader:   strings.NewReader(fileContent),
		Filename: "recibo.pdf",
		MIMEType: "application/pdf",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantMediaID {
		t.Errorf("want %q, got %q", wantMediaID, got)
	}
}

// ─── HTTP-status error classification ────────────────────────────────────────

func TestRealClient_HTTP429_IsTransient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !whatsapp.IsTransient(err) {
		t.Errorf("want transient error for HTTP 429, got: %v", err)
	}
}

func TestRealClient_HTTP500_IsTransient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !whatsapp.IsTransient(err) {
		t.Errorf("want transient error for HTTP 500, got: %v", err)
	}
}

func TestRealClient_HTTP400_IsPermanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if whatsapp.IsTransient(err) {
		t.Errorf("want permanent error for HTTP 400, got transient: %v", err)
	}
}

func TestRealClient_ErrorSnippet_IsBounded(t *testing.T) {
	t.Parallel()
	hugeBody := strings.Repeat("x", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, hugeBody, http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(err.Error()) > 700 {
		t.Errorf("expected bounded error message, got %d bytes", len(err.Error()))
	}
}

// ─── Meta application-level error codes ──────────────────────────────────────

func TestRealClient_MetaWindowClosed_IsPermanentAndMatchesSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(metaErrorResponse(http.StatusBadRequest, 131047, "message window closed"))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, whatsapp.ErrWindowClosed) {
		t.Errorf("want ErrWindowClosed, got: %v", err)
	}
	if whatsapp.IsTransient(err) {
		t.Errorf("want permanent error, got transient: %v", err)
	}
}

func TestRealClient_MetaInvalidNumber_IsPermanentAndMatchesSentinel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(metaErrorResponse(http.StatusBadRequest, 131026, "invalid number"))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, whatsapp.ErrInvalidNumber) {
		t.Errorf("want ErrInvalidNumber, got: %v", err)
	}
	if whatsapp.IsTransient(err) {
		t.Errorf("want permanent error, got transient: %v", err)
	}
}

func TestRealClient_MetaRateLimited_IsTransientAndMatchesSentinel(t *testing.T) {
	t.Parallel()
	// Meta returns 130429 under an HTTP 400, not a 429 — the classification
	// must key off the application code, not the HTTP status.
	srv := httptest.NewServer(metaErrorResponse(http.StatusBadRequest, 130429, "rate limit hit"))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, whatsapp.ErrRateLimited) {
		t.Errorf("want ErrRateLimited, got: %v", err)
	}
	if !whatsapp.IsTransient(err) {
		t.Errorf("want transient error, got permanent: %v", err)
	}
}

// ─── malformed / empty 2xx responses ─────────────────────────────────────────

func TestRealClient_SendText_MalformedJSONResponse_IsPermanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("this is not json {{{"))
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if whatsapp.IsTransient(err) {
		t.Errorf("want permanent error for bad JSON, got transient: %v", err)
	}
}

func TestRealClient_SendText_EmptyMessagesArray_ReturnsErrWhatsAppEmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"messaging_product": "whatsapp",
			"messages":          []any{},
		})
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.SendText(context.Background(), "5215500000000", "hola")
	if !errors.Is(err, whatsapp.ErrWhatsAppEmptyResponse) {
		t.Errorf("want ErrWhatsAppEmptyResponse, got: %v", err)
	}
}

func TestRealClient_UploadMedia_EmptyID_ReturnsErrWhatsAppEmptyResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": ""})
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.UploadMedia(context.Background(), whatsapp.Media{
		Reader:   strings.NewReader("x"),
		Filename: "f.pdf",
		MIMEType: "application/pdf",
	})
	if !errors.Is(err, whatsapp.ErrWhatsAppEmptyResponse) {
		t.Errorf("want ErrWhatsAppEmptyResponse, got: %v", err)
	}
}

func TestRealClient_UploadMedia_ReaderError_IsPermanent(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("server should not be contacted when the reader fails before the request is built")
	}))
	defer srv.Close()

	c := newEnabledClient(t, srv)
	_, err := c.UploadMedia(context.Background(), whatsapp.Media{
		Reader:   iotest.ErrReader(errors.New("disk read failed")),
		Filename: "f.pdf",
		MIMEType: "application/pdf",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if whatsapp.IsTransient(err) {
		t.Errorf("want permanent error for a broken reader, got transient: %v", err)
	}
}

// ─── construction defaults ────────────────────────────────────────────────────

func TestRealClient_ZeroValueConfig_AppliesDefaults(t *testing.T) {
	t.Parallel()
	const wantWamid = "wamid.DEFAULTS123"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// APIVersion was left empty in config — the default "v21.0" must
		// still land in the path.
		if r.URL.Path != "/v21.0/1234567890/messages" {
			t.Errorf("unexpected path: %q", r.URL.Path)
		}
		messageIDResponse(wantWamid)(w, r)
	}))
	defer srv.Close()

	c := whatsapp.NewClient(config.WhatsApp{
		Enabled:           true,
		Token:             "test-token",
		PhoneNumberID:     "1234567890",
		BusinessAccountID: "waba-1",
		BaseURL:           srv.URL,
		// APIVersion and Timeout intentionally left at their zero values.
	})
	got, err := c.SendText(context.Background(), "5215500000000", "hola")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantWamid {
		t.Errorf("want %q, got %q", wantWamid, got)
	}
}

// ─── TransientError ────────────────────────────────────────────────────────────

func TestTransientError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	err := fmt.Errorf("wrapped: %w", &whatsapp.TransientError{Cause: cause})

	if !whatsapp.IsTransient(err) {
		t.Fatal("expected IsTransient to be true")
	}
	if !errors.Is(err, cause) {
		t.Error("expected errors.Is to reach the wrapped cause via Unwrap")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected Error() to mention the cause, got %q", err.Error())
	}
}

func TestIsTransient_PlainError_IsFalse(t *testing.T) {
	t.Parallel()
	if whatsapp.IsTransient(errors.New("plain error")) {
		t.Error("want false for a plain error")
	}
}
