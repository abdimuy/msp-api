//nolint:misspell // Spanish domain vocabulary per project convention.
package reactivacionllm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

type stubClient struct {
	ChatFunc func(ctx context.Context, req platformllm.ChatReq) (string, error)
}

func (s *stubClient) Chat(ctx context.Context, req platformllm.ChatReq) (string, error) {
	return s.ChatFunc(ctx, req)
}

func assertDeterministicJSONReq(t *testing.T, req platformllm.ChatReq) {
	t.Helper()
	require.NotNil(t, req.Temperature, "Temperature must be explicit (pointer), not the zero value")
	assert.InDelta(t, 0.0, *req.Temperature, 0, "Temperature must be 0 for determinism")
	require.NotNil(t, req.ResponseFormat)
	assert.Equal(t, "json_object", req.ResponseFormat.Type)
}

// ─── Analizar ───────────────────────────────────────────────────────────────────

func TestGenerator_Analizar_HappyParse(t *testing.T) {
	t.Parallel()

	var captured platformllm.ChatReq
	responseJSON := `{"intencion":"pregunta por saldo","confianza":72,"senales":["deuda","senal_compra"],` +
		`"accion":"escalar","borrador":"","evidencia":["preguntó monto exacto"],"razon_escalamiento":"pide saldo"}`
	stub := &stubClient{ChatFunc: func(_ context.Context, req platformllm.ChatReq) (string, error) {
		captured = req
		return responseJSON, nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	out, err := gen.Analizar(context.Background(), outbound.AnalizarInput{
		Nombre: "María López", Segmento: "recien_liquidado", MensajeEntrante: "¿Cuánto debo?",
	})
	require.NoError(t, err)

	assert.Equal(t, "pregunta por saldo", out.Intencion)
	assert.Equal(t, 72, out.Confianza)
	assert.Equal(t, []string{"deuda", "senal_compra"}, out.Senales)
	assert.Equal(t, "escalar", out.Accion)
	assert.Equal(t, []string{"preguntó monto exacto"}, out.Evidencia)
	assert.Equal(t, "pide saldo", out.RazonEscalamiento)

	assertDeterministicJSONReq(t, captured)
	require.GreaterOrEqual(t, len(captured.Messages), 2)
	assert.Contains(t, captured.Messages[1].Content, "María López")
}

func TestGenerator_Analizar_ConfianzaScale(t *testing.T) {
	t.Parallel()

	// Some providers return "confianza" on a 0-1 scale (e.g. 0.85) instead of
	// the 0-100 the prompt asks for. Before the fix, `Confianza int` failed to
	// unmarshal a float and the WHOLE Analizar call errored. Analizar must now
	// normalize the 0-1 case to 0-100 and take integers at face value.
	cases := []struct {
		name     string
		rawConf  string
		expected int
	}{
		{"int_0_100", "72", 72},
		{"float_0_1", "0.85", 85},
		{"float_half", "0.5", 50},
		{"float_high", "0.99", 99},
		{"float_one_is_certain", "1.0", 100},
		{"int_one_is_low", "1", 1},
		{"float_already_0_100", "85.0", 85},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			response := `{"intencion":"interesado","confianza":` + tc.rawConf + `,"senales":[],` +
				`"accion":"responder","borrador":"¡Claro!","evidencia":[],"razon_escalamiento":""}`
			stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
				return response, nil
			}}
			gen := reactivacionllm.NewGenerator(stub, "test-model")
			out, err := gen.Analizar(context.Background(), outbound.AnalizarInput{
				Nombre: "Ana", Segmento: "recien_liquidado",
			})
			require.NoError(t, err, "confianza %q debe parsear sin error", tc.rawConf)
			assert.Equal(t, tc.expected, out.Confianza)
		})
	}
}

func TestGenerator_Analizar_ThinkWrapped(t *testing.T) {
	t.Parallel()

	response := "<think>razonando...</think>\n" +
		`{"intencion":"saluda","confianza":50,"senales":[],"accion":"responder","borrador":"¡Hola!",` +
		`"evidencia":[],"razon_escalamiento":""}`
	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return response, nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	out, err := gen.Analizar(context.Background(), outbound.AnalizarInput{})
	require.NoError(t, err)
	assert.Equal(t, "saluda", out.Intencion)
	assert.Equal(t, "¡Hola!", out.Borrador)
}

func TestGenerator_Analizar_NoJSONReturnsErrNoJSONInResponse(t *testing.T) {
	t.Parallel()

	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return "no soy json", nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	_, err := gen.Analizar(context.Background(), outbound.AnalizarInput{})
	assert.ErrorIs(t, err, reactivacionllm.ErrNoJSONInResponse)
}

func TestGenerator_Analizar_ChatErrorPropagates(t *testing.T) {
	t.Parallel()

	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return "", platformllm.ErrLLMDisabled
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	_, err := gen.Analizar(context.Background(), outbound.AnalizarInput{})
	assert.ErrorIs(t, err, platformllm.ErrLLMDisabled)
}

// ─── DestilarNota ───────────────────────────────────────────────────────────────

func TestGenerator_DestilarNota_HappyParse(t *testing.T) {
	t.Parallel()

	var captured platformllm.ChatReq
	responseJSON := `{"contexto":"paga los viernes, prefiere contacto por la tarde","banderas":["prefiere_tarde"]}`
	stub := &stubClient{ChatFunc: func(_ context.Context, req platformllm.ChatReq) (string, error) {
		captured = req
		return responseJSON, nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	out, err := gen.DestilarNota(context.Background(), outbound.NotaInput{
		Nota:   "la señora dijo que paga los viernes y que le hablen en la tarde",
		Nombre: "Rosa Martínez", Segmento: "por_liquidar_hueco",
	})
	require.NoError(t, err)
	assert.Equal(t, "paga los viernes, prefiere contacto por la tarde", out.Contexto)
	assert.Equal(t, []string{"prefiere_tarde"}, out.Banderas)
	assertDeterministicJSONReq(t, captured)
}

func TestGenerator_DestilarNota_ErrorPropagates(t *testing.T) {
	t.Parallel()

	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return "", platformllm.ErrLLMDisabled
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	_, err := gen.DestilarNota(context.Background(), outbound.NotaInput{})
	assert.ErrorIs(t, err, platformllm.ErrLLMDisabled)
}

func TestGenerator_DestilarNota_MalformedJSON(t *testing.T) {
	t.Parallel()

	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return "not json", nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	_, err := gen.DestilarNota(context.Background(), outbound.NotaInput{})
	assert.ErrorIs(t, err, reactivacionllm.ErrNoJSONInResponse)
}

// ─── Redactar ───────────────────────────────────────────────────────────────────

func TestGenerator_Redactar_HappyParse(t *testing.T) {
	t.Parallel()

	var captured platformllm.ChatReq
	responseJSON := `{"borrador":"Hola Rosa, le comparto que ya puede aprovechar nuestra promoción vigente."}`
	stub := &stubClient{ChatFunc: func(_ context.Context, req platformllm.ChatReq) (string, error) {
		captured = req
		return responseJSON, nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	borrador, err := gen.Redactar(context.Background(), outbound.RedactarInput{
		Intencion: "ofrecerle la promoción de temporada", Nombre: "Rosa Martínez",
	})
	require.NoError(t, err)
	assert.Equal(t, "Hola Rosa, le comparto que ya puede aprovechar nuestra promoción vigente.", borrador)
	assertDeterministicJSONReq(t, captured)
	assert.Contains(t, captured.Messages[1].Content, "ofrecerle la promoción de temporada")
}

func TestGenerator_Redactar_ChatErrorPropagates(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return "", sentinel
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	_, err := gen.Redactar(context.Background(), outbound.RedactarInput{})
	assert.ErrorIs(t, err, sentinel)
}

func TestGenerator_Redactar_NoJSONInResponse(t *testing.T) {
	t.Parallel()

	stub := &stubClient{ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
		return "", nil
	}}

	gen := reactivacionllm.NewGenerator(stub, "test-model")
	_, err := gen.Redactar(context.Background(), outbound.RedactarInput{})
	assert.ErrorIs(t, err, reactivacionllm.ErrNoJSONInResponse)
}
