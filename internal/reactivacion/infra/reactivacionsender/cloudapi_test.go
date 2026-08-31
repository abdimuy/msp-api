//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionsender_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
	platformwhatsapp "github.com/abdimuy/msp-api/internal/platform/whatsapp"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionsender"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// newCloudAPIClient builds a platformwhatsapp.Client (the real, enabled
// implementation) pointed at srv — no test in this file ever touches the
// real network.
func newCloudAPIClient(srv *httptest.Server) platformwhatsapp.Client {
	return platformwhatsapp.NewClient(config.WhatsApp{
		Enabled:           true,
		Token:             "test-token",
		PhoneNumberID:     "1234567890",
		BusinessAccountID: "waba-1",
		APIVersion:        "v21.0",
		BaseURL:           srv.URL,
		Timeout:           5 * time.Second,
	})
}

// metaErrorServer starts an httptest.Server that always answers a Graph API
// error body carrying the given Meta application error code, under the
// given HTTP status.
func metaErrorServer(t *testing.T, statusCode, metaCode int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		resp := map[string]any{
			"error": map[string]any{
				"message": "meta rejected the message",
				"type":    "OAuthException",
				"code":    metaCode,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCloudAPISender_Enviar_HappyPath(t *testing.T) {
	t.Parallel()

	var gotTo, gotBody string
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		decodeErr = json.NewDecoder(r.Body).Decode(&req)
		gotTo, _ = req["to"].(string)
		text, _ := req["text"].(map[string]any)
		gotBody, _ = text["body"].(string)

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"messages": []map[string]any{{"id": "wamid.HAPPY123"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)

	s := reactivacionsender.NewCloudAPISender(newCloudAPIClient(srv), nil)
	dest := outbound.Destino{ClienteID: 101, Telefono: "5212381112222"}

	err := s.Enviar(context.Background(), dest, "hola, este es el mensaje de reactivación")
	require.NoError(t, err)
	require.NoError(t, decodeErr)
	assert.Equal(t, "5212381112222", gotTo)
	assert.Equal(t, "hola, este es el mensaje de reactivación", gotBody)
}

func TestCloudAPISender_Enviar_VentanaCerrada(t *testing.T) {
	t.Parallel()
	srv := metaErrorServer(t, http.StatusBadRequest, 131047)

	s := reactivacionsender.NewCloudAPISender(newCloudAPIClient(srv), nil)
	err := s.Enviar(context.Background(), outbound.Destino{ClienteID: 101, Telefono: "5212381112222"}, "cuerpo")
	require.Error(t, err)

	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "reactivacion_cloudapi_ventana_cerrada", ae.Code)
	assert.Contains(t, err.Error(), "ventana de 24 horas")
	assert.Contains(t, err.Error(), "131047")
	require.ErrorIs(t, err, platformwhatsapp.ErrWindowClosed)
}

func TestCloudAPISender_Enviar_NumeroInvalido(t *testing.T) {
	t.Parallel()
	srv := metaErrorServer(t, http.StatusBadRequest, 131026)

	s := reactivacionsender.NewCloudAPISender(newCloudAPIClient(srv), nil)
	err := s.Enviar(context.Background(), outbound.Destino{ClienteID: 101, Telefono: "5212381112222"}, "cuerpo")
	require.Error(t, err)

	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "reactivacion_cloudapi_numero_invalido", ae.Code)
	assert.Contains(t, err.Error(), "no tiene whatsapp")
	assert.Contains(t, err.Error(), "131026")
	require.ErrorIs(t, err, platformwhatsapp.ErrInvalidNumber)
}

// TestCloudAPISender_Enviar_VentanaVsNumero_DistinctCodes pins the exact
// requirement from the task brief: window-closed (131047) and
// invalid-number (131026) must be told apart, not collapsed into one
// generic "meta rejected it" error — the difference is retry-later vs.
// never-retry.
func TestCloudAPISender_Enviar_VentanaVsNumero_DistinctCodes(t *testing.T) {
	t.Parallel()

	ventanaSrv := metaErrorServer(t, http.StatusBadRequest, 131047)
	numeroSrv := metaErrorServer(t, http.StatusBadRequest, 131026)

	dest := outbound.Destino{ClienteID: 101, Telefono: "5212381112222"}

	ventanaErr := reactivacionsender.NewCloudAPISender(newCloudAPIClient(ventanaSrv), nil).
		Enviar(context.Background(), dest, "cuerpo")
	numeroErr := reactivacionsender.NewCloudAPISender(newCloudAPIClient(numeroSrv), nil).
		Enviar(context.Background(), dest, "cuerpo")

	require.Error(t, ventanaErr)
	require.Error(t, numeroErr)
	assert.NotEqual(t, ventanaErr.Error(), numeroErr.Error())

	var ventanaAE, numeroAE *apperror.Error
	require.ErrorAs(t, ventanaErr, &ventanaAE)
	require.ErrorAs(t, numeroErr, &numeroAE)
	assert.NotEqual(t, ventanaAE.Code, numeroAE.Code)
	require.ErrorIs(t, ventanaErr, platformwhatsapp.ErrWindowClosed)
	require.NotErrorIs(t, ventanaErr, platformwhatsapp.ErrInvalidNumber)
	require.ErrorIs(t, numeroErr, platformwhatsapp.ErrInvalidNumber)
	require.NotErrorIs(t, numeroErr, platformwhatsapp.ErrWindowClosed)
}

func TestCloudAPISender_Enviar_LimiteExcedido(t *testing.T) {
	t.Parallel()
	srv := metaErrorServer(t, http.StatusTooManyRequests, 130429)

	s := reactivacionsender.NewCloudAPISender(newCloudAPIClient(srv), nil)
	err := s.Enviar(context.Background(), outbound.Destino{ClienteID: 101, Telefono: "5212381112222"}, "cuerpo")
	require.Error(t, err)

	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "reactivacion_cloudapi_limite_excedido", ae.Code)
	// 429, not 500: apperror.Kind's own doc names 130429 as exactly the
	// intended use of KindTooManyRequests — a caller that should back off
	// and retry, not a server-side failure.
	assert.Equal(t, apperror.KindTooManyRequests, ae.Kind)
	assert.Contains(t, err.Error(), "130429")
	require.ErrorIs(t, err, platformwhatsapp.ErrRateLimited)
	assert.True(t, platformwhatsapp.IsTransient(err))
}

func TestCloudAPISender_Enviar_FalloGenerico(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	t.Cleanup(srv.Close)

	s := reactivacionsender.NewCloudAPISender(newCloudAPIClient(srv), nil)
	err := s.Enviar(context.Background(), outbound.Destino{ClienteID: 101, Telefono: "5212381112222"}, "cuerpo")
	require.Error(t, err)

	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "reactivacion_cloudapi_fallo_envio", ae.Code)
	assert.Contains(t, err.Error(), "no se pudo enviar el mensaje por whatsapp")
}

func TestCloudAPISender_Kind(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)

	s := reactivacionsender.NewCloudAPISender(newCloudAPIClient(srv), nil)
	assert.Equal(t, domain.SenderReal, s.Kind())
}
