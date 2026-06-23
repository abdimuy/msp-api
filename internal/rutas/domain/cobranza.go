//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// VentaCobranza is the per-sale read-model used by the cobranza breakdown
// endpoint (GET /v2/rutas/{zona_id}/cobranza).
type VentaCobranza struct {
	// VentaID is MSP_SALDOS_VENTAS.DOCTO_CC_ID.
	VentaID int
	// ClienteID is MSP_SALDOS_VENTAS.CLIENTE_ID.
	ClienteID int
	// ZonaID is MSP_SALDOS_VENTAS.ZONA_CLIENTE_ID.
	ZonaID int
	// ClienteNombre from CLIENTES.NOMBRE (legacy table; NFC in Go).
	ClienteNombre string
	// Folio de la venta PV (DOCTOS_PV.FOLIO), "" si no se resuelve.
	Folio string
	// DoctoPVID is the linked PV sale id (DOCTOS_ENTRE_SIS bridge), 0 si no se resuelve.
	// Used by the FE to fetch products lazily via the clientes endpoint.
	DoctoPVID int
	// Parcialidad from LIBRES_CARGOS_CC.
	Parcialidad decimal.Decimal
	// Frecuencia resolved from LISTAS_ATRIBUTOS via LIBRES_CARGOS_CC.FORMA_DE_PAGO.
	Frecuencia Frecuencia
	// AbonoSemana is the sum of valid payments in the reporting window.
	AbonoSemana decimal.Decimal
	// Vencidas is the overdue quota count used in the aporte calculation.
	Vencidas decimal.Decimal
	// Aporte is the calculated contribution (CalcAporte result).
	Aporte decimal.Decimal
	// Saldo is MSP_SALDOS_VENTAS.SALDO (current outstanding balance).
	Saldo decimal.Decimal
	// FechaUltPago from MSP_SALDOS_VENTAS (retained for compatibility; no longer
	// drives ponderado logic — use AplicaPonderado instead).
	FechaUltPago *time.Time
	// FechaCargo from MSP_SALDOS_VENTAS (used for plazos computation).
	FechaCargo time.Time
	// AplicaPonderado indicates whether this venta counts in the ponderado
	// denominator (there is a calendar due-date within the reporting window).
	// Populated by enrichVentas after the repo returns raw rows.
	AplicaPonderado bool
	// TotalImporte from MSP_SALDOS_VENTAS.
	TotalImporte decimal.Decimal
}

// ReporteZona aggregates the two weekly metrics for a single zona.
// Nil pointers mean Firestore had no calendar entry for the cobrador
// (dev mode or unconfigured) — the API returns JSON null for those fields.
type ReporteZona struct {
	// ZonaID matches RutaResumen.ZonaID.
	ZonaID int
	// PctCoberturaSemanal is numerador/denominador×100 for coverage.
	// Nil when FechaInicioSemana is unknown.
	PctCoberturaSemanal *decimal.Decimal
	// PctPonderadoSemanal is SUM(aporte)/denominadorPonderado×100.
	// Nil when FechaInicioSemana is unknown.
	PctPonderadoSemanal *decimal.Decimal
	// FechaInicioSemana is the cobrador's week-start timestamp from Firestore.
	// Nil when not available.
	FechaInicioSemana *time.Time
}
