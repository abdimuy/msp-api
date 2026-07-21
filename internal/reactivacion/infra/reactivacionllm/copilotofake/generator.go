//nolint:misspell // Spanish domain vocabulary per project convention.
package copilotofake

import (
	"context"

	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// Generator is a deterministic outbound.CopilotoLLM for tests. It records
// every call's input and returns the configured output/error for that
// method — mirrors internal/analytics/infra/llm/llmfake.Generator.
type Generator struct {
	AnalizarOut outbound.AnalizarOutput
	AnalizarErr error
	AnalizarIn  []outbound.AnalizarInput

	NotaOut outbound.NotaOutput
	NotaErr error
	NotaIn  []outbound.NotaInput

	RedactarOut string
	RedactarErr error
	RedactarIn  []outbound.RedactarInput
}

// Compile-time assertion.
var _ outbound.CopilotoLLM = (*Generator)(nil)

// Analizar records in and returns the configured AnalizarOut/AnalizarErr pair.
func (g *Generator) Analizar(_ context.Context, in outbound.AnalizarInput) (outbound.AnalizarOutput, error) {
	g.AnalizarIn = append(g.AnalizarIn, in)
	return g.AnalizarOut, g.AnalizarErr
}

// DestilarNota records in and returns the configured NotaOut/NotaErr pair.
func (g *Generator) DestilarNota(_ context.Context, in outbound.NotaInput) (outbound.NotaOutput, error) {
	g.NotaIn = append(g.NotaIn, in)
	return g.NotaOut, g.NotaErr
}

// Redactar records in and returns the configured RedactarOut/RedactarErr pair.
func (g *Generator) Redactar(_ context.Context, in outbound.RedactarInput) (string, error) {
	g.RedactarIn = append(g.RedactarIn, in)
	return g.RedactarOut, g.RedactarErr
}
