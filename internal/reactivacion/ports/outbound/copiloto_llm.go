//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import "context"

// AnalizarInput carries every fact the copiloto LLM needs to analyze one
// inbound cliente message and propose a Decision. Implementations live in
// internal/reactivacion/infra/reactivacionllm (later slice).
type AnalizarInput struct {
	ResumenMemoria, MensajeEntrante   string
	Nombre, Segmento, NextBestProduct string
	Enganche, Parcialidad, Cadencia   string
	ContextoNota                      string
	Banderas                          []string
	Allowlist                         string
}

// AnalizarOutput is the raw LLM output for Analizar. The app layer (Slice B)
// validates the enum-shaped fields (Accion, Senales) before turning this
// into a domain.Decision — CopilotoLLM must never assume its callers trust
// the LLM's output blindly.
type AnalizarOutput struct {
	Intencion         string
	Confianza         int
	Senales           []string
	Accion            string
	Borrador          string
	Evidencia         []string
	RazonEscalamiento string
}

// NotaInput carries the cobrador's raw note plus enough cliente context for
// DestilarNota to produce a short, relevant distillation.
type NotaInput struct {
	Nota, Nombre, Segmento string
}

// NotaOutput is the distilled version of a cobrador's note: a short
// operational context plus flags the copiloto can act on.
type NotaOutput struct {
	Contexto string
	Banderas []string
}

// RedactarInput carries what a human operator dictated (the "Dictar" action)
// plus enough cliente/conversation context for Redactar to draft a message
// consistent with the copiloto's other outputs.
type RedactarInput struct {
	Intencion      string // what the operator wants to say, in their words
	ResumenMemoria string
	Nombre         string
	Segmento       string
	ContextoNota   string
	Banderas       []string
	Allowlist      string
}

// CopilotoLLM is the port to the language model backing the reactivación
// copiloto. Implementations live in internal/reactivacion/infra/reactivacionllm
// (later slice) and call an OpenAI-compatible hosted endpoint — never local
// inference (see feedback_llm_local_satura_server).
type CopilotoLLM interface {
	// Analizar reads an inbound cliente message plus conversation context and
	// proposes an intent, confidence, signals, and either a reply draft or an
	// escalation reason.
	Analizar(ctx context.Context, in AnalizarInput) (AnalizarOutput, error)

	// DestilarNota distills a cobrador's free-text note into a short
	// operational context plus flags, cached on the Conversacion so it is not
	// re-computed on every turn.
	DestilarNota(ctx context.Context, in NotaInput) (NotaOutput, error)

	// Redactar drafts an outbound message from a HUMAN operator's stated intent
	// (the "Dictar" action) — distinct from Analizar, which reacts to a
	// customer's inbound message. Returns the draft body. Errors (incl.
	// llm.ErrLLMDisabled) propagate to the caller.
	Redactar(ctx context.Context, in RedactarInput) (string, error)
}
