//nolint:misspell // Spanish domain vocabulary per project convention.
package reactivacionllm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	platformllm "github.com/abdimuy/msp-api/internal/platform/llm"

	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Generator implements outbound.CopilotoLLM using a pluggable LLM client. It
// builds a Spanish, safety-anchored prompt per method and parses the
// structured JSON response. Temperature is fixed at 0 for determinism —
// mirrors internal/analytics/infra/llm.Generator.
type Generator struct {
	client platformllm.Client
	model  string
}

// NewGenerator constructs a Generator with the given LLM client and model name.
func NewGenerator(client platformllm.Client, model string) *Generator {
	return &Generator{client: client, model: model}
}

// Compile-time assertion.
var _ outbound.CopilotoLLM = (*Generator)(nil)

// analizarSystemPrompt anchors the copiloto's §8 safety rails: it is an
// internal SALES copilot for reactivación, never a cobranza (collections)
// agent, and it only PROPOSES — the deterministic policy in the app layer
// decides whether to auto-send or escalate.
const analizarSystemPrompt = `Eres el copiloto interno de VENTAS para la reactivación de clientes de una mueblería. NO eres un agente de cobranza.

Reglas estrictas que debes seguir siempre:
- Puedes OFRECER solamente los productos o planes de catálogo que se te indiquen explícitamente en los datos del cliente. Nunca inventes productos, precios ni fechas que no se te hayan dado.
- Puedes AFIRMAR en positivo el estatus de pago cuando el dato lo confirme (por ejemplo "ya terminó de pagar su compra"), pero JAMÁS menciones un monto de deuda o saldo pendiente, ni en positivo ni en negativo.
- JAMÁS menciones la palabra "cobranza" ni sugieras que este mensaje es un recordatorio de pago.
- JAMÁS cites ni repitas la nota del cobrador; solo puedes usar el CONTEXTO DE VENTA ya destilado que se te entrega.
- Los montos de enganche y parcialidad, si se te dan, son los ÚNICOS montos que puedes mencionar — nunca inventes otros ni HAGAS ARITMÉTICA con ellos (no calcules un "total al mes", no sumes ni multipliques): enuncia solo el enganche y la parcialidad tal como se te dieron.
- Tú PROPONES: intención, señales, una acción (responder o escalar), un borrador de respuesta (si propones responder) y evidencia que respalde tu lectura. Una capa de política determinista en el sistema decide si tu propuesta se envía o se escala — tú nunca decides el envío final. Cuando el cliente solo muestra interés o pregunta por productos, precios o planes, PROPÓN responder con un borrador que venda (ofrece el producto y el plan de pago permitidos). PROPÓN escalar únicamente ante intención de cierre, deuda, solicitud de humano, enojo, algo fuera del allowlist, o confianza baja.

Cómo redactar el "borrador" cuando vendes (español de México, cálido y cercano pero profesional, sin coloquialismos forzados):
- Vende la PARCIALIDAD, no el precio: el pago pequeño y frecuente ("desde $X a la semana") es la cifra protagonista y el enganche es el paso de entrada; NUNCA lideres con el precio total ni con una cifra grande.
- Ofrece UN solo producto dirigido (el siguiente mejor producto que se te indique), no una lista larga; solo si el cliente pide otras opciones, ofrécele alternativas.
- Mensajes cortos (2 a 4 líneas), con calidez, con un solo objetivo o una sola pregunta por mensaje; usa el nombre del cliente; evita el tono corporativo o de notificación automática.
- Maneja las objeciones sin presionar: si dice que está caro, reencuadra a la parcialidad accesible (no bajes el precio ni inventes descuentos); si dice que lo consulta con su pareja, respeta e incluye a quien decide y deja un seguimiento suave; si duda o dice que no es el momento, propón un siguiente paso chico y deja la puerta abierta.
- Sé cálido y humano en el tono, pero no prometas ni exageres nada que no se te haya dado.

Debes levantar, en el arreglo "senales", exactamente los siguientes valores en snake_case cuando apliquen (puedes levantar varios o ninguno):
- "deuda": el cliente pregunta por su saldo, monto que debe, o menciona pagos pendientes.
- "senal_compra": el cliente muestra interés o pregunta por productos, precios o planes. TÚ le respondes y vendes; esto NO se escala.
- "senal_cierre": el cliente ya decidió comprar y quiere concretar la compra ahora (por ejemplo "lo quiero", "¿cómo le hago?", "¿cómo pago?", "sí le entro"). Esto SÍ se escala a un humano para cerrar.
- "pide_humano": el cliente pide explícitamente hablar con una persona.
- "enojo_loop": el cliente muestra enojo o está repitiendo el mismo reclamo sin resolución.
- "fuera_allowlist": el cliente pregunta por algo fuera del catálogo permitido que se te dio.
- "confianza_baja": tu propia lectura de la intención del cliente es ambigua o poco clara.

Asigna un valor numérico de "confianza" entre 0 y 100 sobre qué tan seguro estás de tu lectura de la intención.

Responde ÚNICAMENTE con un objeto JSON con esta forma exacta, sin texto adicional:
{"intencion": "...", "confianza": 0, "senales": ["..."], "accion": "responder|escalar", "borrador": "...", "evidencia": ["..."], "razon_escalamiento": "..."}
"borrador" debe ir vacío si "accion" es "escalar". "razon_escalamiento" debe ir vacío si "accion" es "responder".`

// AnalizarInput carries every fact the copiloto LLM needs to analyze one
// inbound cliente message and propose a Decision.

// Analizar reads an inbound cliente message plus conversation context and
// proposes an intent, confidence, signals, and either a reply draft or an
// escalation reason.
func (g *Generator) Analizar(ctx context.Context, in outbound.AnalizarInput) (outbound.AnalizarOutput, error) {
	content, err := g.chatJSON(ctx, analizarSystemPrompt, buildAnalizarUserMessage(in))
	if err != nil {
		return outbound.AnalizarOutput{}, err
	}

	var parsed struct {
		Intencion string `json:"intencion"`
		// Confianza is json.Number (not int) so a provider that returns the
		// confidence on a 0-1 scale (e.g. 0.85) does not fail to unmarshal and
		// abort the whole call. normalizarConfianza maps it back to 0-100.
		Confianza         json.Number `json:"confianza"`
		Senales           []string    `json:"senales"`
		Accion            string      `json:"accion"`
		Borrador          string      `json:"borrador"`
		Evidencia         []string    `json:"evidencia"`
		RazonEscalamiento string      `json:"razon_escalamiento"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return outbound.AnalizarOutput{}, fmt.Errorf("reactivacionllm: parse analizar response: %w", err)
	}

	return outbound.AnalizarOutput{
		Intencion:         parsed.Intencion,
		Confianza:         normalizarConfianza(parsed.Confianza),
		Senales:           parsed.Senales,
		Accion:            parsed.Accion,
		Borrador:          parsed.Borrador,
		Evidencia:         parsed.Evidencia,
		RazonEscalamiento: parsed.RazonEscalamiento,
	}, nil
}

// normalizarConfianza maps the LLM's raw confidence onto the 0-100 scale the
// domain expects (triar compares it against umbralConfianzaBaja). Some
// providers answer on a 0-1 scale despite the prompt; a fractional value ≤ 1
// (e.g. 0.85, or 1.0 meaning "certain") is scaled ×100, while an integer is
// taken at face value. A non-numeric value yields 0 (which triar treats as
// low confidence → escala, the safe default).
func normalizarConfianza(raw json.Number) int {
	f, err := raw.Float64()
	if err != nil {
		return 0
	}
	// Only a value written with a decimal point AND ≤ 1 is a 0-1 scale reading;
	// a bare integer like "1" is taken as 1/100, never rescaled to 100.
	if strings.Contains(raw.String(), ".") && f <= 1.0 {
		f *= 100
	}
	return int(f + 0.5)
}

// buildAnalizarUserMessage assembles the structured Spanish prompt from in.
//
//nolint:revive // writes to strings.Builder never fail.
func buildAnalizarUserMessage(in outbound.AnalizarInput) string {
	var sb strings.Builder

	_, _ = fmt.Fprintf(&sb, "=== CLIENTE ===\n")
	_, _ = fmt.Fprintf(&sb, "Nombre: %s  |  Segmento: %s\n", in.Nombre, in.Segmento)

	if in.NextBestProduct != "" {
		_, _ = fmt.Fprintf(&sb, "Producto/plan sugerido de catálogo: %s\n", in.NextBestProduct)
	}
	if in.Enganche != "" || in.Parcialidad != "" || in.Cadencia != "" {
		_, _ = fmt.Fprintf(&sb, "Enganche: %s  |  Parcialidad: %s  |  Cadencia: %s\n",
			in.Enganche, in.Parcialidad, in.Cadencia)
	}

	if in.ContextoNota != "" {
		_, _ = fmt.Fprintf(&sb, "\n=== CONTEXTO DE VENTA (destilado, privado, no citar) ===\n%s\n", in.ContextoNota)
	}
	if len(in.Banderas) > 0 {
		_, _ = fmt.Fprintf(&sb, "Banderas de venta: %s\n", strings.Join(in.Banderas, ", "))
	}

	if in.Allowlist != "" {
		_, _ = fmt.Fprintf(&sb, "\n=== TEMAS PERMITIDOS ===\n%s\n", in.Allowlist)
	}

	if in.ResumenMemoria != "" {
		_, _ = fmt.Fprintf(&sb, "\n=== RESUMEN DE LA CONVERSACIÓN HASTA AHORA ===\n%s\n", in.ResumenMemoria)
	}

	_, _ = fmt.Fprintf(&sb, "\n=== MENSAJE ENTRANTE DEL CLIENTE ===\n%s\n", in.MensajeEntrante)

	return sb.String()
}

// notaSystemPrompt anchors DestilarNota's rails: the note is distilled into a
// short PRIVATE operational context — the cliente's own words are never
// quoted back, only the tone/substance the copiloto can act on.
const notaSystemPrompt = `Eres un asistente interno que destila la nota del cobrador sobre un cliente en contexto de venta breve y privado para el copiloto de reactivación.

Reglas estrictas:
- NUNCA cites ni reproduzcas las palabras textuales del cliente o del cobrador; informa el tono y la sustancia operativa, no las palabras.
- El contexto que produces es SOLO para uso interno del copiloto — nunca se muestra al cliente.
- Si la nota está vacía o no aporta nada operativo relevante para la venta, deja "contexto" como cadena vacía "" y "banderas" como arreglo vacío [].
- Las "banderas" son señales operativas cortas en snake_case (por ejemplo: "prefiere_tarde", "domicilio_compartido", "ya_contactado_antes").

Responde ÚNICAMENTE con un objeto JSON con esta forma exacta, sin texto adicional:
{"contexto": "...", "banderas": ["..."]}`

// DestilarNota distills a cobrador's free-text note into a short operational
// context plus flags, cached on the Conversacion so it is not re-computed on
// every turn.
func (g *Generator) DestilarNota(ctx context.Context, in outbound.NotaInput) (outbound.NotaOutput, error) {
	content, err := g.chatJSON(ctx, notaSystemPrompt, buildNotaUserMessage(in))
	if err != nil {
		return outbound.NotaOutput{}, err
	}

	var parsed struct {
		Contexto string   `json:"contexto"`
		Banderas []string `json:"banderas"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return outbound.NotaOutput{}, fmt.Errorf("reactivacionllm: parse destilar_nota response: %w", err)
	}

	return outbound.NotaOutput{
		Contexto: parsed.Contexto,
		Banderas: parsed.Banderas,
	}, nil
}

// buildNotaUserMessage assembles the structured Spanish prompt from in.
//
//nolint:revive // writes to strings.Builder never fail.
func buildNotaUserMessage(in outbound.NotaInput) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "=== CLIENTE ===\n")
	_, _ = fmt.Fprintf(&sb, "Nombre: %s  |  Segmento: %s\n", in.Nombre, in.Segmento)
	_, _ = fmt.Fprintf(&sb, "\n=== NOTA DEL COBRADOR ===\n%s\n", in.Nota)
	return sb.String()
}

// redactarSystemPrompt anchors Redactar's rails: it drafts an outbound
// message realizing a HUMAN operator's stated intent (the "Dictar" action),
// under the same §8 rails as Analizar — distinct from Analizar because it
// reacts to a human operator's instruction, not to a customer's message.
const redactarSystemPrompt = `Eres el copiloto interno de VENTAS para la reactivación de clientes de una mueblería. NO eres un agente de cobranza.

Un operador humano te dictó, en sus propias palabras, la intención de un mensaje que quiere enviarle al cliente. Tu trabajo es redactar el mensaje final en español neutro y profesional, cálido pero breve, realizando esa intención.

Reglas estrictas (idénticas a las del análisis de mensajes entrantes):
- Puedes OFRECER solamente los productos o planes de catálogo que se te indiquen explícitamente. Nunca inventes productos, precios ni fechas.
- Puedes AFIRMAR en positivo el estatus de pago cuando el dato lo confirme, pero JAMÁS menciones un monto de deuda o saldo pendiente.
- JAMÁS menciones la palabra "cobranza".
- JAMÁS cites ni repitas la nota del cobrador; solo puedes usar el CONTEXTO DE VENTA ya destilado que se te entrega.

Responde ÚNICAMENTE con un objeto JSON con esta forma exacta, sin texto adicional:
{"borrador": "..."}`

// Redactar drafts an outbound message from a HUMAN operator's stated intent
// (the "Dictar" action) — distinct from Analizar, which reacts to a
// customer's inbound message. Returns the draft body.
func (g *Generator) Redactar(ctx context.Context, in outbound.RedactarInput) (string, error) {
	content, err := g.chatJSON(ctx, redactarSystemPrompt, buildRedactarUserMessage(in))
	if err != nil {
		return "", err
	}

	var parsed struct {
		Borrador string `json:"borrador"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return "", fmt.Errorf("reactivacionllm: parse redactar response: %w", err)
	}
	return parsed.Borrador, nil
}

// buildRedactarUserMessage assembles the structured Spanish prompt from in.
//
//nolint:revive // writes to strings.Builder never fail.
func buildRedactarUserMessage(in outbound.RedactarInput) string {
	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "=== CLIENTE ===\n")
	_, _ = fmt.Fprintf(&sb, "Nombre: %s  |  Segmento: %s\n", in.Nombre, in.Segmento)

	if in.ContextoNota != "" {
		_, _ = fmt.Fprintf(&sb, "\n=== CONTEXTO DE VENTA (destilado, privado, no citar) ===\n%s\n", in.ContextoNota)
	}
	if len(in.Banderas) > 0 {
		_, _ = fmt.Fprintf(&sb, "Banderas de venta: %s\n", strings.Join(in.Banderas, ", "))
	}
	if in.Allowlist != "" {
		_, _ = fmt.Fprintf(&sb, "\n=== TEMAS PERMITIDOS ===\n%s\n", in.Allowlist)
	}
	if in.ResumenMemoria != "" {
		_, _ = fmt.Fprintf(&sb, "\n=== RESUMEN DE LA CONVERSACIÓN HASTA AHORA ===\n%s\n", in.ResumenMemoria)
	}

	_, _ = fmt.Fprintf(&sb, "\n=== INTENCIÓN DICTADA POR EL OPERADOR ===\n%s\n", in.Intencion)

	return sb.String()
}

// chatJSON sends system+user as a deterministic (Temperature=0), json_object
// chat request and returns the extracted JSON object string. The Chat error
// (including platformllm.ErrLLMDisabled) is returned unwrapped so callers
// can errors.Is it; a response with no balanced JSON object yields
// ErrNoJSONInResponse.
func (g *Generator) chatJSON(ctx context.Context, systemPrompt, userMsg string) (string, error) {
	req := platformllm.ChatReq{
		Messages: []platformllm.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMsg},
		},
		// Pointer required: bare 0 is the Go zero value and would be omitted.
		Temperature:    platformllm.Float64(0),
		ResponseFormat: &platformllm.ResponseFormat{Type: "json_object"},
	}

	content, err := g.client.Chat(ctx, req)
	if err != nil {
		return "", err
	}

	jsonStr, ok := extractJSON(content)
	if !ok {
		return "", ErrNoJSONInResponse
	}
	return jsonStr, nil
}
