//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// ReporteUsuario is the read-model for one cobrador USER's weekly report. Unlike
// RutaResumen (one row per zona), this is one row per Firestore user: the same
// zona cartera (NumClientes/SaldoTotal) is evaluated over THAT user's own window
// (FechaInicio = su FECHA_CARGA_INICIAL). Two users on the same COBRADOR_ID/zona
// produce two independent rows, each with its own window — which removes the
// ambiguity of the old per-zona path (where a duplicated COBRADOR_ID resolved to
// an arbitrary single FECHA_CARGA_INICIAL).
type ReporteUsuario struct {
	// UID is the Firestore users document id.
	UID string
	// Nombre is the user's display name (NOMBRE).
	Nombre string
	// Email is the user's login email (EMAIL).
	Email string
	// CobradorID is the Microsip COBRADOR_ID this user collects for.
	CobradorID int
	// ZonaID is the user's ZONA_CLIENTE_ID (the ruta they cover).
	ZonaID int
	// ZonaNombre is the display name of the zona; empty when the zona is unknown.
	ZonaNombre string
	// NumClientes is the active-client count of the zona (per-zona, shared across
	// users of the same zona).
	NumClientes int
	// SaldoTotal is the zona's outstanding balance (per-zona, shared).
	SaldoTotal decimal.Decimal
	// PctCoberturaSemanal is coverage over THIS user's window. Nil when the venta
	// fetch failed.
	PctCoberturaSemanal *decimal.Decimal
	// PctPonderadoSemanal is the weighted percentage over THIS user's window.
	PctPonderadoSemanal *decimal.Decimal
	// FechaInicio is the user's FECHA_CARGA_INICIAL (window start, UTC).
	FechaInicio time.Time
}
