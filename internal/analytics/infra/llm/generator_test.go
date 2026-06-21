//nolint:misspell // Spanish domain vocabulary per project convention.
package llm_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	analyticsllm "github.com/abdimuy/msp-api/internal/analytics/infra/llm"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"
)

type stubClient struct {
	ChatFunc func(ctx context.Context, req platformllm.ChatReq) (string, error)
}

func (s *stubClient) Chat(ctx context.Context, req platformllm.ChatReq) (string, error) {
	return s.ChatFunc(ctx, req)
}

func sampleCatalog() []domain.Rasgo {
	return []domain.Rasgo{
		{Codigo: "loyal_but_stagnant", Etiqueta: "Leal pero estancado", Definicion: "Cliente con alta frecuencia histórica pero sin crecimiento reciente."},
		{Codigo: "churn_risk", Etiqueta: "Riesgo de abandono", Definicion: "Señales de que el cliente podría dejar de comprar pronto."},
	}
}

func sampleInput() outbound.NarrativeInput {
	return outbound.NarrativeInput{
		ClienteID:       42,
		Nombre:          "Juan Pérez García",
		Zona:            "Zona Norte",
		Segmento:        "DIAMANTE",
		TierRiesgo:      "BAJO",
		EstadoPago:      "AL_CORRIENTE",
		BandaCredito:    "RIESGO_BAJO",
		ScoreCredito:    85,
		BandaRecompra:   "ALTA",
		ScoreRecompra:   78,
		BandaCLV:        "PREMIUM",
		Saldo:           decimal.NewFromFloat(1200.50),
		Monetary:        decimal.NewFromFloat(45000.00),
		MontoCLV:        decimal.NewFromFloat(80000.00),
		Frecuencia:      12,
		RecenciaDias:    15,
		CadenciaDias:    30,
		DiasAtrasoProm:  2,
		PctPagosATiempo: decimal.NewFromFloat(98.5),
		CreditoResumen:  "Pagador puntual con saldo bajo",
		RecompraResumen: "Comprador frecuente y leal",
		CLVResumen:      "Cliente de alto valor proyectado",
		CreditoDrivers:  []string{"Saldo < 20% del límite", "Historial limpio 24 meses"},
		RecompraDrivers: []string{"12 compras en 12 meses", "Cadencia regular de 30 días"},
		CLVDrivers:      []string{"CLV proyectado $80,000", "Monetary promedio $3,750/compra"},
		Catalogo:        sampleCatalog(),
	}
}

func TestGenerar_HappyParse(t *testing.T) {
	t.Parallel()

	responseJSON := `{"narrativa":"Cliente puntual con alta frecuencia de compra.","rasgos":["loyal_but_stagnant","churn_risk"]}`

	stub := &stubClient{
		ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
			return responseJSON, nil
		},
	}

	gen := analyticsllm.NewGenerator(stub, "test-model")
	out, err := gen.Generar(context.Background(), sampleInput())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if out.Narrativa != "Cliente puntual con alta frecuencia de compra." {
		t.Errorf("unexpected narrativa: %q", out.Narrativa)
	}
	if len(out.Rasgos) != 2 {
		t.Fatalf("expected 2 rasgos, got %d", len(out.Rasgos))
	}
	if out.Rasgos[0] != "loyal_but_stagnant" || out.Rasgos[1] != "churn_risk" {
		t.Errorf("unexpected rasgos: %v", out.Rasgos)
	}
}

func TestGenerar_PromptAnchoring(t *testing.T) {
	t.Parallel()

	var capturedReq platformllm.ChatReq

	stub := &stubClient{
		ChatFunc: func(_ context.Context, req platformllm.ChatReq) (string, error) {
			capturedReq = req
			return `{"narrativa":"Texto.","rasgos":["code_a"]}`, nil
		},
	}

	in := outbound.NarrativeInput{
		BandaCredito:   "RIESGO_BAJO",
		CreditoResumen: "Pagador puntual",
		Catalogo: []domain.Rasgo{
			{Codigo: "code_a", Etiqueta: "Etiqueta A", Definicion: "Definición A."},
			{Codigo: "code_b", Etiqueta: "Etiqueta B", Definicion: "Definición B."},
		},
	}

	gen := analyticsllm.NewGenerator(stub, "test-model")
	_, err := gen.Generar(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(capturedReq.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(capturedReq.Messages))
	}

	userContent := capturedReq.Messages[1].Content

	if !strContains(userContent, "RIESGO_BAJO") {
		t.Error("user message should contain BandaCredito 'RIESGO_BAJO'")
	}
	if !strContains(userContent, "Pagador puntual") {
		t.Error("user message should contain CreditoResumen 'Pagador puntual'")
	}
	if !strContains(userContent, "code_a") {
		t.Error("user message should contain catalog code 'code_a'")
	}
	if !strContains(userContent, "code_b") {
		t.Error("user message should contain catalog code 'code_b'")
	}

	if capturedReq.ResponseFormat == nil {
		t.Fatal("ResponseFormat should not be nil")
	}
	if capturedReq.ResponseFormat.Type != "json_schema" {
		t.Errorf("ResponseFormat.Type: got %q, want %q", capturedReq.ResponseFormat.Type, "json_schema")
	}
	if capturedReq.ResponseFormat.Name != "analyst_reading" {
		t.Errorf("ResponseFormat.Name: got %q, want %q", capturedReq.ResponseFormat.Name, "analyst_reading")
	}

	props, ok := capturedReq.ResponseFormat.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("schema properties not a map[string]any")
	}
	rasgos, ok := props["rasgos"].(map[string]any)
	if !ok {
		t.Fatal("schema rasgos not a map[string]any")
	}
	items, ok := rasgos["items"].(map[string]any)
	if !ok {
		t.Fatal("schema rasgos.items not a map[string]any")
	}
	enum, ok := items["enum"].([]any)
	if !ok {
		t.Fatal("schema rasgos.items.enum not a []any")
	}
	if len(enum) != 2 {
		t.Fatalf("expected 2 enum values, got %d", len(enum))
	}
	if enum[0] != "code_a" || enum[1] != "code_b" {
		t.Errorf("unexpected enum values: %v", enum)
	}
}

func TestGenerar_MarkdownFence(t *testing.T) {
	t.Parallel()

	response := "```json\n{\"narrativa\":\"Texto de prueba.\",\"rasgos\":[\"code_a\"]}\n```"

	stub := &stubClient{
		ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
			return response, nil
		},
	}

	in := outbound.NarrativeInput{
		Catalogo: []domain.Rasgo{
			{Codigo: "code_a", Etiqueta: "Etiqueta A", Definicion: "Definición A."},
		},
	}

	gen := analyticsllm.NewGenerator(stub, "test-model")
	out, err := gen.Generar(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if out.Narrativa != "Texto de prueba." {
		t.Errorf("unexpected narrativa: %q", out.Narrativa)
	}
	if len(out.Rasgos) != 1 || out.Rasgos[0] != "code_a" {
		t.Errorf("unexpected rasgos: %v", out.Rasgos)
	}
}

func TestGenerar_ThinkWrapper(t *testing.T) {
	t.Parallel()

	response := "<think>Razonamiento interno del modelo.</think>\n{\"narrativa\":\"Texto.\",\"rasgos\":[\"code_a\"]}"

	stub := &stubClient{
		ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
			return response, nil
		},
	}

	in := outbound.NarrativeInput{
		Catalogo: []domain.Rasgo{
			{Codigo: "code_a", Etiqueta: "Etiqueta A", Definicion: "Definición A."},
		},
	}

	gen := analyticsllm.NewGenerator(stub, "test-model")
	out, err := gen.Generar(context.Background(), in)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if out.Narrativa != "Texto." {
		t.Errorf("unexpected narrativa: %q", out.Narrativa)
	}
	if len(out.Rasgos) != 1 || out.Rasgos[0] != "code_a" {
		t.Errorf("unexpected rasgos: %v", out.Rasgos)
	}
}

func TestGenerar_BadJSON(t *testing.T) {
	t.Parallel()

	stub := &stubClient{
		ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
			return "not json at all", nil
		},
	}

	gen := analyticsllm.NewGenerator(stub, "test-model")
	_, err := gen.Generar(context.Background(), sampleInput())
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestGenerar_ClientError(t *testing.T) {
	t.Parallel()

	stub := &stubClient{
		ChatFunc: func(_ context.Context, _ platformllm.ChatReq) (string, error) {
			return "", platformllm.ErrLLMDisabled
		},
	}

	gen := analyticsllm.NewGenerator(stub, "test-model")
	_, err := gen.Generar(context.Background(), sampleInput())
	if !errors.Is(err, platformllm.ErrLLMDisabled) {
		t.Errorf("expected ErrLLMDisabled, got: %v", err)
	}
}

func strContains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
