package canalhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	canaldomain "github.com/abdimuy/msp-api/internal/canal/domain"
	canaloutbound "github.com/abdimuy/msp-api/internal/canal/ports/outbound"
)

// defaultForwarderTimeout is used when ForwarderConfig.Timeout is <= 0.
const defaultForwarderTimeout = 15 * time.Second

// errForwarderHTTP is the sentinel wrapped into errors for a non-2xx
// response from the store's on-premise server. Use
// fmt.Errorf("...: %w", errForwarderHTTP) so err113 is satisfied while the
// dynamic status code is still captured in the message.
var errForwarderHTTP = errors.New("canalhttp: forwarder http error")

// ForwarderConfig configures ForwarderClient. All three fields come from
// internal/platform/config's Canal section — never hardcode a URL or a
// token, never log a ForwarderConfig.
type ForwarderConfig struct {
	// URL is the store's on-premise server endpoint that receives a pushed
	// entrante — a full URL, not a base a path gets appended to. Task 10
	// owns the store-side contract and may need this pointed at whatever
	// path the real endpoint ends up using.
	URL string
	// Token is the shared secret sent on sharedTokenHeader, authenticating
	// this VPS to the store.
	Token string
	// Timeout bounds each forwarding HTTP call. <= 0 uses
	// defaultForwarderTimeout.
	Timeout time.Duration
}

func (c *ForwarderConfig) applyDefaults() {
	if c.Timeout <= 0 {
		c.Timeout = defaultForwarderTimeout
	}
}

// ForwarderClient implements outbound.Forwarder over HTTP: it pushes one
// MensajeEntrante to the store's on-premise server, authenticated by the
// shared token, and classifies the outcome so internal/canal/app's worker
// (Task 4) knows whether to retry.
//
// Classification mirrors internal/platform/whatsapp's realClient
// (Task 1's transport): a transport-level failure (timeout, connection
// refused, request build failure) or an HTTP 5xx/429 response is wrapped in
// canaldomain.TransientError; every other non-2xx status is permanent.
type ForwarderClient struct {
	url        string
	token      string
	httpClient *http.Client
}

// Compile-time assertion: ForwarderClient must satisfy outbound.Forwarder.
var _ canaloutbound.Forwarder = (*ForwarderClient)(nil)

// NewForwarderClient builds a ForwarderClient from cfg. No network
// connection is attempted at construction time.
func NewForwarderClient(cfg ForwarderConfig) *ForwarderClient {
	cfg.applyDefaults()
	return &ForwarderClient{
		url:   cfg.URL,
		token: cfg.Token,
		httpClient: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// forwarderPayload is the wire shape pushed to the store for one entrante.
// Field names and shape are canal's own choice — see the type doc on
// ForwarderConfig.URL — Task 10 verifies this against the real store
// endpoint and adjusts this struct if the DTO differs.
type forwarderPayload struct {
	Wamid         string `json:"wamid"`
	Remitente     string `json:"remitente"`
	PhoneNumberID string `json:"phone_number_id"`
	Tipo          string `json:"tipo"`
	Contenido     string `json:"contenido"`
	TimestampMeta string `json:"timestamp_meta"`
	RecibidoEn    string `json:"recibido_en"`
}

// toForwarderPayload builds the wire payload from what MensajeEntrante
// exposes through its getters — canal is sealed and ForwarderClient may not
// reach into the entity's private fields.
func toForwarderPayload(m *canaldomain.MensajeEntrante) forwarderPayload {
	return forwarderPayload{
		Wamid:         m.Wamid(),
		Remitente:     m.Remitente(),
		PhoneNumberID: m.PhoneNumberID(),
		Tipo:          m.Tipo(),
		Contenido:     m.Contenido(),
		TimestampMeta: m.TimestampMeta().UTC().Format(time.RFC3339Nano),
		RecibidoEn:    m.RecibidoEn().UTC().Format(time.RFC3339Nano),
	}
}

// Reenviar pushes m to the store's on-premise server. See the type doc for
// how the outcome is classified transient vs permanent.
func (f *ForwarderClient) Reenviar(ctx context.Context, m *canaldomain.MensajeEntrante) error {
	body, err := json.Marshal(toForwarderPayload(m))
	if err != nil {
		return fmt.Errorf("canalhttp: marshal forwarder payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url, bytes.NewReader(body))
	if err != nil {
		return &canaldomain.TransientError{Cause: fmt.Errorf("canalhttp: build forwarder request: %w", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sharedTokenHeader, f.token)

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return &canaldomain.TransientError{Cause: fmt.Errorf("canalhttp: forwarder http do: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection is eligible for keep-alive reuse; the body's
	// content is never inspected or logged.
	_, _ = io.Copy(io.Discard, resp.Body)

	return classifyForwarderStatus(resp.StatusCode)
}

// classifyForwarderStatus turns a store response status into nil (success),
// a canaldomain.TransientError (5xx, 429 — safe to retry), or a bare
// permanent error (every other non-2xx).
func classifyForwarderStatus(statusCode int) error {
	if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
		return nil
	}
	if statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError {
		return &canaldomain.TransientError{Cause: fmt.Errorf("%w: status %d", errForwarderHTTP, statusCode)}
	}
	return fmt.Errorf("%w: status %d", errForwarderHTTP, statusCode)
}
