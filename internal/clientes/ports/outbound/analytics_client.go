//nolint:misspell // Spanish domain vocabulary (clientes, pulso, etc.) by project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics"
)

// AnalyticsClient is the clientes module's outbound port to the analytics module.
// It fetches the pre-computed scoring pulse for one or more clients so the
// clientes hub can enrich directory and ficha responses without recomputing scores.
type AnalyticsClient interface {
	// ObtenerPulso returns the analytics pulse for a single client.
	// found is false (and pulso is zero-valued) when the client has no
	// materialized winback candidato row (e.g. zero purchase history).
	ObtenerPulso(ctx context.Context, clienteID int) (pulso analytics.ClientePulsoContract, found bool, err error)

	// ObtenerPulsos returns a map of clienteID → pulse for the given IDs.
	// Clients with no materialized row are absent from the map (no error).
	// An empty input returns an empty map.
	ObtenerPulsos(ctx context.Context, clienteIDs []int) (map[int]analytics.ClientePulsoContract, error)
}
