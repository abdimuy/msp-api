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

// UsuarioCobrador is one Firestore `users` document projected to the fields the
// per-user cobranza report needs. Each user carries its OWN window
// (FechaInicio = FECHA_CARGA_INICIAL), so two users sharing a COBRADOR_ID/zona
// are reported independently — no arbitrary collision.
type UsuarioCobrador struct {
	// UID is the Firestore document id.
	UID string
	// Nombre is NOMBRE.
	Nombre string
	// Email is EMAIL.
	Email string
	// CobradorID is COBRADOR_ID (> 0 for real cobradores).
	CobradorID int
	// ZonaID is ZONA_CLIENTE_ID (the ruta the user covers).
	ZonaID int
	// FechaInicio is FECHA_CARGA_INICIAL (UTC). Zero when the field is absent.
	FechaInicio time.Time
}

// CalendarioCobradorClient is the read port for the Firestore-backed cobrador
// calendar (the `users` collection).
//
// Implementations must be safe for concurrent use. A Firestore-unavailable
// environment (dev mode, unconfigured) should return empty results without error.
type CalendarioCobradorClient interface {
	// FechaInicioPorCobrador returns a map from COBRADOR_ID to the week-start
	// FECHA_CARGA_INICIAL. NOTE: when several users share a COBRADOR_ID the map
	// keeps an arbitrary one — use ListarCobradores for the per-user report.
	FechaInicioPorCobrador(ctx context.Context) (map[int]time.Time, error)
	// ListarCobradores returns one entry per user document that has a COBRADOR_ID,
	// each with its own FECHA_CARGA_INICIAL window.
	ListarCobradores(ctx context.Context) ([]UsuarioCobrador, error)
}
