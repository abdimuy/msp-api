//nolint:misspell // Spanish domain vocabulary per project convention.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"

	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ErrNoJSONInResponse is returned when the LLM response contains no balanced JSON object.
var ErrNoJSONInResponse = errors.New("llm generator: no JSON object found in response")

// Generator implements outbound.NarrativeGenerator using a pluggable LLM client.
// It builds a fully-anchored prompt from computed facts and parses the structured
// JSON response. Temperature is fixed at 0 for determinism.
type Generator struct {
	client platformllm.Client
	model  string
}

// NewGenerator constructs a Generator with the given LLM client and model name.
func NewGenerator(client platformllm.Client, model string) *Generator {
	return &Generator{client: client, model: model}
}

// Compile-time assertion.
var _ outbound.NarrativeGenerator = (*Generator)(nil)

const systemPrompt = `Eres un analista interno de cartera. Tu análisis es para uso interno de oficina, nunca para el cliente.
Se te proporcionan hechos ya calculados sobre un cliente. NO inventes números ni contradigas las bandas o el nivel de riesgo que se te indica.
Redacta UN solo párrafo en español neutro (máximo 4 frases) que sintetice los tres ejes (crédito, recompra, valor) y cierre con UNA acción interna recomendada.
Además, elige entre 1 y 3 rasgos del catálogo, usando EXCLUSIVAMENTE sus códigos exactos.
Responde SOLO en el formato JSON indicado, sin texto adicional fuera del objeto JSON.`

// Generar builds an anchored prompt from in, calls the LLM, and parses the JSON response.
func (g *Generator) Generar(ctx context.Context, in outbound.NarrativeInput) (outbound.NarrativeOutput, error) {
	userMsg := buildUserMessage(in)

	codes := make([]any, len(in.Catalogo))
	for i, r := range in.Catalogo {
		codes[i] = r.Codigo
	}

	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"narrativa": map[string]any{"type": "string"},
			"rasgos": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
					"enum": codes,
				},
				"minItems": 1,
				"maxItems": 3,
			},
		},
		"required":             []any{"narrativa", "rasgos"},
		"additionalProperties": false,
	}

	req := platformllm.ChatReq{
		Messages: []platformllm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
		// Pointer required: bare 0 is the Go zero value and would be omitted.
		Temperature: platformllm.Float64(0),
		ResponseFormat: &platformllm.ResponseFormat{
			Type:   "json_schema",
			Schema: schema,
			Name:   "analyst_reading",
		},
	}

	content, err := g.client.Chat(ctx, req)
	if err != nil {
		return outbound.NarrativeOutput{}, err
	}

	jsonStr, ok := extractJSON(content)
	if !ok {
		return outbound.NarrativeOutput{}, ErrNoJSONInResponse
	}

	var parsed struct {
		Narrativa string   `json:"narrativa"`
		Rasgos    []string `json:"rasgos"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return outbound.NarrativeOutput{}, fmt.Errorf("llm generator: parse response: %w", err)
	}

	return outbound.NarrativeOutput{
		Narrativa: parsed.Narrativa,
		Rasgos:    parsed.Rasgos,
	}, nil
}

// buildUserMessage assembles the structured Spanish prompt from computed facts.
//
//nolint:revive // writes to strings.Builder never fail.
func buildUserMessage(in outbound.NarrativeInput) string {
	var sb strings.Builder

	_, _ = fmt.Fprintf(&sb, "=== DATOS DEL CLIENTE ===\n")
	_, _ = fmt.Fprintf(&sb, "Nombre: %s  |  Zona: %s\n", in.Nombre, in.Zona)
	_, _ = fmt.Fprintf(&sb, "Segmento: %s  |  Tier de riesgo: %s  |  Estado de pago: %s\n",
		in.Segmento, in.TierRiesgo, in.EstadoPago)

	_, _ = fmt.Fprintf(&sb, "\n=== CRÉDITO ===\n")
	_, _ = fmt.Fprintf(&sb, "Banda: %s  |  Score: %d\n", in.BandaCredito, in.ScoreCredito)
	_, _ = fmt.Fprintf(&sb, "Saldo: $%s  |  %% pagos a tiempo: %s%%  |  Días atraso prom: %d\n",
		in.Saldo.String(), in.PctPagosATiempo.StringFixed(1), in.DiasAtrasoProm)
	_, _ = fmt.Fprintf(&sb, "Titular: %s\n", in.CreditoResumen)
	_, _ = fmt.Fprintf(&sb, "Factores: %s\n", strings.Join(in.CreditoDrivers, "; "))

	_, _ = fmt.Fprintf(&sb, "\n=== RECOMPRA ===\n")
	_, _ = fmt.Fprintf(&sb, "Banda: %s  |  Score: %d\n", in.BandaRecompra, in.ScoreRecompra)
	_, _ = fmt.Fprintf(&sb, "Frecuencia: %d compras  |  Recencia: %d días  |  Cadencia: %d días\n",
		in.Frecuencia, in.RecenciaDias, in.CadenciaDias)
	_, _ = fmt.Fprintf(&sb, "Titular: %s\n", in.RecompraResumen)
	_, _ = fmt.Fprintf(&sb, "Factores: %s\n", strings.Join(in.RecompraDrivers, "; "))

	_, _ = fmt.Fprintf(&sb, "\n=== VALOR (CLV) ===\n")
	_, _ = fmt.Fprintf(&sb, "Banda: %s  |  CLV: $%s  |  Monetary: $%s\n",
		in.BandaCLV, in.MontoCLV.String(), in.Monetary.String())
	_, _ = fmt.Fprintf(&sb, "Titular: %s\n", in.CLVResumen)
	_, _ = fmt.Fprintf(&sb, "Factores: %s\n", strings.Join(in.CLVDrivers, "; "))

	_, _ = fmt.Fprintf(&sb, "\n=== CATÁLOGO DE RASGOS ===\n")
	for _, r := range in.Catalogo {
		_, _ = fmt.Fprintf(&sb, "  %s — %s: %s\n", r.Codigo, r.Etiqueta, r.Definicion)
	}

	return sb.String()
}

// extractJSON strips <think>...</think> blocks and markdown fences, then finds
// and returns the first balanced JSON object in s.
func extractJSON(s string) (string, bool) {
	// Remove <think>...</think> blocks (some reasoning models emit these).
	// An unclosed <think> is stripped to end-of-string so stray '{' inside
	// a malformed block can't be mis-parsed as a JSON object.
	for {
		start := strings.Index(s, "<think>")
		if start == -1 {
			break
		}
		end := strings.Index(s, "</think>")
		if end == -1 {
			s = s[:start]
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}

	// Find first '{'.
	i := strings.Index(s, "{")
	if i == -1 {
		return "", false
	}

	depth := 0
	inStr := false
	for j := i; j < len(s); j++ {
		ch := s[j]
		if inStr {
			if ch == '\\' {
				j++ // skip escaped char
				continue
			}
			if ch == '"' {
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[i : j+1], true
			}
		}
	}
	return "", false
}
