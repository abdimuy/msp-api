//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import "context"

// NotaReader reads the cobrador's free-text note for a cliente — the source
// material CopilotoLLM.DestilarNota distills into Conversacion's cached
// ContextoNota/Banderas.
type NotaReader interface {
	// GetNotaCliente returns the current note for clienteID, or an empty
	// string when the cliente has none.
	GetNotaCliente(ctx context.Context, clienteID int) (string, error)
}
