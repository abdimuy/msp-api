package canalhttp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalhttp"
)

// newTestEntrante builds a valid domain.MensajeEntrante for ForwarderClient
// tests — its own construction is out of scope here, only that
// ForwarderClient forwards its fields correctly.
func newTestEntrante(t *testing.T) *domain.MensajeEntrante {
	t.Helper()
	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         "wamid.forward-1",
		Remitente:     "5215511112222",
		PhoneNumberID: "1234567890",
		Tipo:          "text",
		Contenido:     "hola tienda",
		TimestampMeta: fixedNow,
		RecibidoEn:    fixedNow,
	})
	require.NoError(t, err)
	return m
}

func TestForwarderClient_Success_SendsTokenAndPayload(t *testing.T) {
	t.Parallel()
	var gotToken string
	var gotPayload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// assert, not require: this handler runs on a goroutine the test
		// framework does not manage, and require.NoError would call
		// t.FailNow() off the test's own goroutine.
		gotToken = r.Header.Get("X-Canal-Token")
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.NoError(t, json.Unmarshal(body, &gotPayload))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := canalhttp.NewForwarderClient(canalhttp.ForwarderConfig{
		URL:   server.URL,
		Token: testSharedToken,
	})

	err := client.Reenviar(context.Background(), newTestEntrante(t))
	require.NoError(t, err)

	assert.Equal(t, testSharedToken, gotToken)
	assert.Equal(t, "wamid.forward-1", gotPayload["wamid"])
	assert.Equal(t, "5215511112222", gotPayload["remitente"])
	assert.Equal(t, "1234567890", gotPayload["phone_number_id"])
	assert.Equal(t, "hola tienda", gotPayload["contenido"])
}

func TestForwarderClient_Permanent4xx_NotTransient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := canalhttp.NewForwarderClient(canalhttp.ForwarderConfig{URL: server.URL, Token: testSharedToken})
	err := client.Reenviar(context.Background(), newTestEntrante(t))

	require.Error(t, err)
	assert.False(t, domain.IsTransient(err), "a 400 must be permanent, not retried")
}

func TestForwarderClient_5xx_IsTransient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := canalhttp.NewForwarderClient(canalhttp.ForwarderConfig{URL: server.URL, Token: testSharedToken})
	err := client.Reenviar(context.Background(), newTestEntrante(t))

	require.Error(t, err)
	assert.True(t, domain.IsTransient(err), "a 500 must be transient, safe to retry")
}

func TestForwarderClient_429_IsTransient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := canalhttp.NewForwarderClient(canalhttp.ForwarderConfig{URL: server.URL, Token: testSharedToken})
	err := client.Reenviar(context.Background(), newTestEntrante(t))

	require.Error(t, err)
	assert.True(t, domain.IsTransient(err))
}

func TestForwarderClient_TransportFailure_IsTransient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	unreachableURL := server.URL
	server.Close() // closed before use: connection refused.

	client := canalhttp.NewForwarderClient(canalhttp.ForwarderConfig{
		URL:     unreachableURL,
		Token:   testSharedToken,
		Timeout: 2 * time.Second,
	})
	err := client.Reenviar(context.Background(), newTestEntrante(t))

	require.Error(t, err)
	assert.True(t, domain.IsTransient(err), "a connection failure must be transient")
}
