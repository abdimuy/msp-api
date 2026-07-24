//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"

	"github.com/shopspring/decimal"
)

// NextBestProduct is the single, targeted product the copiloto should offer a
// cliente — a real, in-stock catalog item with its price. The app layer turns
// the price into a deterministic PlanPago (enganche + parcialidad) before it
// reaches the LLM; the LLM only enunciates the amounts it is given.
type NextBestProduct struct {
	// Nombre is the product's display name (as it exists in the catalog).
	Nombre string
	// Precio is the product's list price (used to compute the payment plan).
	Precio decimal.Decimal
}

// NextBestProductReader resolves the suggested next-best-product for a cliente.
// Implementations live in infra (later slice: a Microsip-backed reader, then a
// personalized association-rules engine). The reader is OPTIONAL on the copiloto
// — when absent or when it returns (nil, nil), ProcesarMensajeEntrante degrades
// gracefully and the copiloto simply does not offer a specific product/plan.
type NextBestProductReader interface {
	// GetNBP returns the suggested product for clienteID, or (nil, nil) when
	// there is no suitable suggestion (e.g. nothing in stock above the price
	// floor). A non-nil error is a real infra failure.
	GetNBP(ctx context.Context, clienteID int) (*NextBestProduct, error)
}
