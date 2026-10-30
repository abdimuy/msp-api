package garhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

type pruebaIdempotenciaRequest struct {
	EncabezadoIdempotencia
}

type pruebaIdempotenciaResponse struct {
	Body struct {
		Clave string `json:"clave"`
	}
}

// El encabezado del contrato: obligatorio y UUID, validado por Huma antes del
// handler. Si se quita required o format, uno de los dos casos 422 se pone
// rojo.
func TestEncabezadoIdempotencia(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	api := humachi.New(r, huma.DefaultConfig("prueba", "v0"))
	llamadas := 0
	huma.Register(api, huma.Operation{
		OperationID: "prueba-idempotencia",
		Method:      http.MethodPost,
		Path:        "/prueba",
	}, func(_ context.Context, in *pruebaIdempotenciaRequest) (*pruebaIdempotenciaResponse, error) {
		llamadas++
		out := &pruebaIdempotenciaResponse{}
		out.Body.Clave = in.IdempotencyKey
		return out, nil
	})

	casos := []struct {
		nombre string
		clave  string
		status int
	}{
		{"sin_encabezado", "", http.StatusUnprocessableEntity},
		{"no_es_uuid", "reintento-1", http.StatusUnprocessableEntity},
		{"uuid", "0b5f3c2e-8d3a-4c47-9e3b-2f6a1d7c9e10", http.StatusOK},
	}
	for _, c := range casos {
		req := httptest.NewRequest(http.MethodPost, "/prueba", nil)
		if c.clave != "" {
			req.Header.Set("Idempotency-Key", c.clave)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, c.status, rec.Code, "%s: %s", c.nombre, rec.Body.String())
	}
	require.Equal(t, 1, llamadas, "el handler sólo corre con un encabezado válido")
}
