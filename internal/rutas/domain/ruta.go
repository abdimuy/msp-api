// Package domain contains the rutas module's read-models and value objects.
//
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// RutaResumen is the read-model for a single zona (ruta) summary. It is the
// canonical type shared between the outbound port and the app service layer,
// avoiding an import cycle that would arise if the type lived in app/.
type RutaResumen struct {
	// ZonaID is the Microsip ZONA_CLIENTE_ID primary key.
	ZonaID int
	// ZonaNombre is the display name of the zona, decoded from Windows-1252.
	ZonaNombre string
	// CobradorID is the assigned cobrador, or nil when the zona has no cobrador
	// (cfg row absent or COBRADOR_ID = -1).
	CobradorID *int
	// CobradorNombre is the display name of the cobrador; empty when CobradorID is nil.
	CobradorNombre string
	// NumClientes is the count of active clients (ESTATUS IN ('A','B')) in the zona.
	NumClientes int
	// SaldoTotal is the total outstanding balance across all open sales in the zona,
	// sourced from MSP_SALDOS_VENTAS (CARGO_CANCELADO <> 'S').
	SaldoTotal decimal.Decimal
	// PctCoberturaSemanal is the coverage percentage for the current week.
	// Nil when the cobrador has no Firestore calendar entry (dev/unconfigured).
	PctCoberturaSemanal *decimal.Decimal
	// PctPonderadoSemanal is the weighted payment percentage for the current week.
	// Nil when the cobrador has no Firestore calendar entry.
	PctPonderadoSemanal *decimal.Decimal
	// FechaInicioSemana is the cobrador's week-start timestamp from Firestore.
	// Nil when not available.
	FechaInicioSemana *time.Time
}
