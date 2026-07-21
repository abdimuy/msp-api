//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import "context"

// ClienteFacts carries the per-cliente facts the copiloto needs to build an
// AnalizarInput and to enqueue an approved draft via the Fase 2 channel. In
// Fase 3a these come from the MSP_RX_COHORTE snapshot (nombre/segmento/telefono);
// the amount fields are reserved for later ([POR CONFIRMAR]) and stay empty for
// now.
type ClienteFacts struct {
	Nombre   string
	Segmento string
	Telefono string
}

// ClienteFactsReader reads the facts for a single cliente. Returns (nil, nil)
// when the cliente is not in the cohorte snapshot.
type ClienteFactsReader interface {
	GetFacts(ctx context.Context, clienteID int) (*ClienteFacts, error)
}
