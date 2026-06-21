//nolint:misspell // Spanish domain vocabulary per project convention.
package llmfake

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// Generator is a deterministic outbound.NarrativeGenerator for tests.
// It records every call in Inputs and returns the configured Out/Err.
type Generator struct {
	Out    outbound.NarrativeOutput
	Err    error
	Inputs []outbound.NarrativeInput
}

// Compile-time assertion.
var _ outbound.NarrativeGenerator = (*Generator)(nil)

// Generar records the input and returns the configured Out/Err pair.
func (g *Generator) Generar(_ context.Context, in outbound.NarrativeInput) (outbound.NarrativeOutput, error) {
	g.Inputs = append(g.Inputs, in)
	return g.Out, g.Err
}
