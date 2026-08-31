package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
)

// defaultGraphAPIBaseURL is used when config.WhatsApp.BaseURL is empty.
const defaultGraphAPIBaseURL = "https://graph.facebook.com"

// defaultTimeout is used when config.WhatsApp.Timeout is <= 0.
const defaultTimeout = 15 * time.Second

// defaultAPIVersion is used when config.WhatsApp.APIVersion is empty.
const defaultAPIVersion = "v21.0"

// errSnippetMaxBytes bounds how much of a non-2xx response body is embedded
// in an error message, so a diagnosable snippet never turns into an
// unbounded log line.
const errSnippetMaxBytes = 500

// Meta application-level error codes surfaced in the `error.code` field of a
// non-2xx Graph API response body.
const (
	metaCodeWindowClosed  = 131047
	metaCodeInvalidNumber = 131026
	metaCodeRateLimited   = 130429
)

// ErrWhatsAppHTTP is the sentinel wrapped into errors for non-2xx Graph API
// responses whose body carries no recognized Meta error code. Use
// fmt.Errorf("...: %w", ErrWhatsAppHTTP) so err113 is satisfied while the
// dynamic status code and body snippet are still captured in the message.
var ErrWhatsAppHTTP = errors.New("whatsapp: http error")

// ErrWhatsAppEmptyResponse is returned when a 2xx response carries no usable
// payload (an empty messages array, or an empty media id). This is a
// permanent error — the server accepted the request but gave nothing back.
var ErrWhatsAppEmptyResponse = errors.New("whatsapp: empty response payload")

// ErrWindowClosed is Meta error code 131047: the 24-hour customer service
// window is closed and only a template message may be sent. Permanent —
// retrying the same free-form message will not succeed.
var ErrWindowClosed = errors.New("whatsapp: 24h customer service window closed")

// ErrInvalidNumber is Meta error code 131026: the destination number is
// invalid or has no WhatsApp account. Permanent.
var ErrInvalidNumber = errors.New("whatsapp: invalid number or no WhatsApp account")

// ErrRateLimited is Meta error code 130429: Meta's own application-level
// rate limit, distinct from an HTTP 429. Transient — safe to retry with
// backoff, like any rate limit.
var ErrRateLimited = errors.New("whatsapp: meta rate limit exceeded")

// TransientError wraps errors that are safe to retry: network/timeout/ctx
// errors, HTTP 429 (rate-limited), HTTP 5xx (server-side failures), and
// Meta's own 130429 application rate limit regardless of HTTP status.
//
// Use IsTransient(err) rather than type-asserting directly; it traverses the
// error chain so wrapping with fmt.Errorf("%w", ...) still works.
type TransientError struct {
	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *TransientError) Error() string {
	return fmt.Sprintf("whatsapp: transient error: %v", e.Cause)
}

// Unwrap exposes the underlying cause for errors.Is / errors.As traversal.
func (e *TransientError) Unwrap() error { return e.Cause }

// IsTransient reports whether err (or any error in its chain) is a
// TransientError. Transient errors are safe to retry; permanent errors
// (4xx except 429, JSON-decode failures) should not be retried.
func IsTransient(err error) bool {
	var t *TransientError
	return errors.As(err, &t)
}

// transient wraps cause in a TransientError.
func transient(cause error) error { return &TransientError{Cause: cause} }

// ─── wire types ──────────────────────────────────────────────────────────────

// graphMessageRequest is the JSON wire body POSTed to
// /{phone_number_id}/messages. Exactly one of Text, Template or Document is
// set, matching Type.
type graphMessageRequest struct {
	MessagingProduct string         `json:"messaging_product"`
	To               string         `json:"to"`
	Type             string         `json:"type"`
	Text             *graphText     `json:"text,omitempty"`
	Template         *graphTemplate `json:"template,omitempty"`
	Document         *graphDocument `json:"document,omitempty"`
}

// graphText is the wire shape of a free-form text message body.
type graphText struct {
	Body string `json:"body"`
}

// graphTemplate is the wire shape of a template-message invocation.
type graphTemplate struct {
	Name       string              `json:"name"`
	Language   graphTemplateLang   `json:"language"`
	Components []graphTemplateComp `json:"components,omitempty"`
}

// graphTemplateLang carries the template's approved language code.
type graphTemplateLang struct {
	Code string `json:"code"`
}

// graphTemplateComp is one component (e.g. "body") of a template invocation.
type graphTemplateComp struct {
	Type       string               `json:"type"`
	Parameters []graphTemplateParam `json:"parameters"`
}

// graphTemplateParam is one positional parameter substituted into a
// template component.
type graphTemplateParam struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// graphDocument is the wire shape of a document-by-media-id message.
type graphDocument struct {
	ID       string `json:"id"`
	Filename string `json:"filename,omitempty"`
	Caption  string `json:"caption,omitempty"`
}

// graphMessageResponse is the top-level JSON response from
// /{phone_number_id}/messages on success.
type graphMessageResponse struct {
	Messages []struct {
		ID string `json:"id"`
	} `json:"messages"`
}

// graphMediaUploadResponse is the top-level JSON response from
// /{phone_number_id}/media on success.
type graphMediaUploadResponse struct {
	ID string `json:"id"`
}

// graphErrorResponse is the top-level JSON response shape Meta uses for
// non-2xx responses.
type graphErrorResponse struct {
	Error struct {
		Message      string `json:"message"`
		Type         string `json:"type"`
		Code         int    `json:"code"`
		ErrorSubcode int    `json:"error_subcode"`
		FBTraceID    string `json:"fbtrace_id"`
	} `json:"error"`
}

// ─── realClient ──────────────────────────────────────────────────────────────

// realClient is the production WhatsApp Cloud API client. It POSTs to Meta's
// Graph API using raw net/http (no third-party SDK) so it cross-compiles
// cleanly for GOOS=windows GOARCH=amd64 CGO_ENABLED=0.
type realClient struct {
	baseURL       string
	apiVersion    string
	phoneNumberID string
	token         string
	httpClient    *http.Client
}

// Compile-time assertion: realClient must satisfy Client.
var _ Client = (*realClient)(nil)

// newRealClient constructs a realClient from config. No network connection
// is attempted at construction time; the first Send/Upload call validates
// reachability.
func newRealClient(cfg config.WhatsApp) *realClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = defaultAPIVersion
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = defaultGraphAPIBaseURL
	}
	slog.Info("whatsapp.real_client_ready", "base_url", baseURL, "phone_number_id", cfg.PhoneNumberID)
	return &realClient{
		baseURL:       strings.TrimRight(baseURL, "/"),
		apiVersion:    apiVersion,
		phoneNumberID: cfg.PhoneNumberID,
		token:         cfg.Token,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// messagesEndpoint returns the /{phone_number_id}/messages URL.
func (c *realClient) messagesEndpoint() string {
	return c.baseURL + "/" + c.apiVersion + "/" + c.phoneNumberID + "/messages"
}

// mediaEndpoint returns the /{phone_number_id}/media URL.
func (c *realClient) mediaEndpoint() string {
	return c.baseURL + "/" + c.apiVersion + "/" + c.phoneNumberID + "/media"
}

// SendText sends a free-form text message and returns the resulting wamid.
func (c *realClient) SendText(ctx context.Context, to, body string) (string, error) {
	return c.sendMessage(ctx, graphMessageRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "text",
		Text:             &graphText{Body: body},
	})
}

// SendTemplate sends a template message and returns the resulting wamid.
func (c *realClient) SendTemplate(ctx context.Context, to string, tmpl Template) (string, error) {
	return c.sendMessage(ctx, graphMessageRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "template",
		Template: &graphTemplate{
			Name:       tmpl.Name,
			Language:   graphTemplateLang{Code: tmpl.LanguageCode},
			Components: templateComponents(tmpl.BodyParams),
		},
	})
}

// templateComponents builds the "components" wire array from positional
// body parameters. Returns nil when there are none, so the JSON field is
// omitted rather than sent as an empty array.
func templateComponents(bodyParams []string) []graphTemplateComp {
	if len(bodyParams) == 0 {
		return nil
	}
	params := make([]graphTemplateParam, len(bodyParams))
	for i, p := range bodyParams {
		params[i] = graphTemplateParam{Type: "text", Text: p}
	}
	return []graphTemplateComp{{Type: "body", Parameters: params}}
}

// SendDocumentByMediaID sends a document previously uploaded via UploadMedia
// and returns the resulting wamid.
func (c *realClient) SendDocumentByMediaID(ctx context.Context, to, mediaID, filename, caption string) (string, error) {
	return c.sendMessage(ctx, graphMessageRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "document",
		Document: &graphDocument{
			ID:       mediaID,
			Filename: filename,
			Caption:  caption,
		},
	})
}

// sendMessage marshals req, POSTs it to the messages endpoint, and returns
// choices[0].id — the wamid — from the response.
func (c *realClient) sendMessage(ctx context.Context, req graphMessageRequest) (string, error) {
	body, err := json.Marshal(req)
	if err != nil {
		// JSON-encode failure is permanent (bad input).
		return "", fmt.Errorf("whatsapp: marshal request: %w", err)
	}

	respBody, err := c.doRequest(ctx, c.messagesEndpoint(), "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	var parsed graphMessageResponse
	if jsonErr := json.Unmarshal(respBody, &parsed); jsonErr != nil {
		// Malformed JSON from the server is a permanent error.
		return "", fmt.Errorf("whatsapp: decode response: %w", jsonErr)
	}
	if len(parsed.Messages) == 0 {
		return "", ErrWhatsAppEmptyResponse
	}
	return parsed.Messages[0].ID, nil
}

// UploadMedia uploads media.Reader to Meta's media store and returns the
// resulting media id.
func (c *realClient) UploadMedia(ctx context.Context, media Media) (string, error) {
	body, contentType, err := buildMediaUploadBody(media)
	if err != nil {
		return "", fmt.Errorf("whatsapp: build multipart body: %w", err)
	}

	respBody, err := c.doRequest(ctx, c.mediaEndpoint(), contentType, body)
	if err != nil {
		return "", err
	}

	var parsed graphMediaUploadResponse
	if jsonErr := json.Unmarshal(respBody, &parsed); jsonErr != nil {
		return "", fmt.Errorf("whatsapp: decode media upload response: %w", jsonErr)
	}
	if parsed.ID == "" {
		return "", ErrWhatsAppEmptyResponse
	}
	return parsed.ID, nil
}

// buildMediaUploadBody encodes media as the multipart/form-data body Meta's
// /{phone_number_id}/media endpoint expects: a "messaging_product" field, a
// "type" field carrying the MIME type, and a "file" part.
func buildMediaUploadBody(media Media) (*bytes.Buffer, string, error) {
	mimeType := media.MIMEType
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	if err := mw.WriteField("messaging_product", "whatsapp"); err != nil {
		return nil, "", fmt.Errorf("write messaging_product field: %w", err)
	}
	if err := mw.WriteField("type", mimeType); err != nil {
		return nil, "", fmt.Errorf("write type field: %w", err)
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, media.Filename))
	header.Set("Content-Type", mimeType)
	part, err := mw.CreatePart(header)
	if err != nil {
		return nil, "", fmt.Errorf("create file part: %w", err)
	}
	if _, err := io.Copy(part, media.Reader); err != nil {
		return nil, "", fmt.Errorf("copy file content: %w", err)
	}

	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}
	return &buf, mw.FormDataContentType(), nil
}

// doRequest POSTs body to url with the given Content-Type header and the
// bearer token, and returns the raw response body on a 2xx response. Non-2xx
// responses are classified by classifyGraphError.
func (c *realClient) doRequest(ctx context.Context, url, contentType string, body io.Reader) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, transient(fmt.Errorf("whatsapp: build http request: %w", err))
	}
	httpReq.Header.Set("Content-Type", contentType)
	httpReq.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		// Network/timeout/ctx cancellation errors are transient.
		return nil, transient(fmt.Errorf("whatsapp: http do: %w", err))
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, transient(fmt.Errorf("whatsapp: read response body: %w", err))
	}

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, classifyGraphError(resp.StatusCode, respBody)
	}
	return respBody, nil
}

// classifyGraphError turns a non-2xx Graph API response into a classified
// error. Meta's own error.code, when present, takes precedence over the
// HTTP status: 130429 is transient even when Meta returns it under an HTTP
// 400, and 131047/131026 are permanent even if they ever arrived under a
// 5xx.
func classifyGraphError(statusCode int, respBody []byte) error {
	snippet := snippetOf(respBody, errSnippetMaxBytes)

	switch metaErrorCode(respBody) {
	case metaCodeWindowClosed:
		return fmt.Errorf("whatsapp: status %d %s: %w", statusCode, snippet, ErrWindowClosed)
	case metaCodeInvalidNumber:
		return fmt.Errorf("whatsapp: status %d %s: %w", statusCode, snippet, ErrInvalidNumber)
	case metaCodeRateLimited:
		return transient(fmt.Errorf("whatsapp: status %d %s: %w", statusCode, snippet, ErrRateLimited))
	}

	if statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError {
		return transient(fmt.Errorf("whatsapp: status %d %s: %w", statusCode, snippet, ErrWhatsAppHTTP))
	}
	// 4xx (except 429) are permanent: bad request, auth failure, etc.
	return fmt.Errorf("whatsapp: status %d %s: %w", statusCode, snippet, ErrWhatsAppHTTP)
}

// metaErrorCode extracts error.code from a Graph API error body. Returns 0
// when the body is not JSON or carries no recognizable error code.
func metaErrorCode(respBody []byte) int {
	var parsed graphErrorResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return 0
	}
	return parsed.Error.Code
}

// snippetOf trims and bounds body to at most maxBytes so error messages
// stay diagnosable without becoming unbounded log lines.
func snippetOf(body []byte, maxBytes int) string {
	s := strings.TrimSpace(string(body))
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "..."
}
