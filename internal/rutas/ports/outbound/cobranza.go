//nolint:misspell // rutas vocabulary is Spanish per project convention.
package outbound

import (
	"context"
	"time"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// CobranzaRepo is the read port for cobranza weekly metrics and breakdown.
// Implemented by rutasfb.CobranzaRepo; fakes used in app-layer tests.
type CobranzaRepo interface {
	// VentasPorZona returns the enriched venta rows for a single zona,
	// restricted to active sales (CARGO_CANCELADO <> 'S', SALDO > 0 OR
	// paid in window). The caller provides the reporting window so the
	// query can filter pagos without a second round-trip.
	VentasPorZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]rutasdomain.VentaCobranza, error)
}

// CalendarioCobradorClient is the read port for the Firestore-backed
// cobrador calendar. Returns a map from COBRADOR_ID to the week-start
// FECHA_CARGA_INICIAL. Missing cobradores in the map → nil FechaInicioSemana
// (the service treats them as "no calendar entry").
//
// Implementations must be safe for concurrent use. A Firestore-unavailable
// environment (dev mode, unconfigured) should return an empty map without error.
type CalendarioCobradorClient interface {
	FechaInicioPorCobrador(ctx context.Context) (map[int]time.Time, error)
}
