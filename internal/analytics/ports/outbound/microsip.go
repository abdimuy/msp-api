//nolint:misspell // Microsip table and column names are Spanish identifiers (e.g. CLIENTES, DOCTOS_PV).
package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AnclaCliente holds the per-cliente anchor facts read from Microsip.
// These values are the raw inputs the app layer uses to build a
// domain.WinbackCandidato — they are never stored directly.
type AnclaCliente struct {
	// ClienteID is the Microsip primary key for the client (CLIENTES.ID_CLIENTE).
	ClienteID int

	// Nombre is the client's display name from CLIENTES.NOMBRE.
	Nombre string

	// Zona is the sales zone assigned to the client (may be empty if unmapped).
	Zona string

	// Telefono is the primary contact phone number (may be empty).
	Telefono string

	// FechaUltimaCompra is the UTC timestamp of the client's most recent
	// purchase in DOCTOS_PV. Zero if the client has no purchase history.
	FechaUltimaCompra time.Time

	// Frecuencia is the total count of completed purchase documents in
	// DOCTOS_PV for this client. Always >= 0.
	Frecuencia int

	// Monetary is the total invoiced amount across all purchase documents.
	// Expressed in MXN. Always >= 0.
	Monetary decimal.Decimal

	// Saldo is the outstanding balance owed by the client (from DOCTOS_CC).
	// Always >= 0.
	Saldo decimal.Decimal

	// PorLiquidarPct is the percentage of the saldo still pending payment,
	// as projected from DOCTOS_CC, expressed in [0, 100] (e.g. 45.50 = 45.5%)
	// to match the NUMERIC(5,2) MSP_AN_WINBACK_CANDIDATOS.POR_LIQUIDAR_PCT
	// column. May be zero when fully paid.
	PorLiquidarPct decimal.Decimal

	// NextBestProduct is the SKU or description of the recommended next
	// product for this client, derived from purchase-pattern analysis.
	// May be empty when no recommendation is available.
	NextBestProduct string

	// FechaUltimoPago is the most recent payment date across the client's open
	// cargos in MSP_SALDOS_VENTAS (CARGO_CANCELADO='N'). Derived as
	// MAX(sv.FECHA_ULT_PAGO) per CLIENTE_ID. Zero if the client has never made
	// a payment.
	FechaUltimoPago time.Time

	// ─── Cobranza intelligence facts ─────────────────────────────────────────────
	// Computed from MSP_PAGOS_VENTAS (CANCELADO='N' AND APLICADO='S') using a
	// windowed LAG aggregation. All values are zero when insufficient data exists
	// (< 2 consecutive payments for cadencia-based metrics).

	// NumPagos is the total count of applied payments.
	NumPagos int

	// CadenciaDias is the average days between consecutive payments.
	// Zero when fewer than 2 payments exist.
	CadenciaDias int

	// DiasAtrasoProm is the average of max(0, gap − cadencia) over all gaps.
	// Zero when insufficient data.
	DiasAtrasoProm int

	// PctPagosATiempo is the percentage of payment gaps within cadencia + 7 days.
	// Zero when insufficient data.
	PctPagosATiempo decimal.Decimal

	// FechaProxPago is the estimated next payment date (last payment + cadencia).
	// Zero when no cadencia is available.
	FechaProxPago time.Time

	// MontoProxPago is the average payment amount used as the installment proxy.
	// Zero when no payment history is available.
	MontoProxPago decimal.Decimal
}

// CobranzaSignals holds the per-client cadence and punctuality facts computed
// from MSP_PAGOS_VENTAS. Returned alongside AnclaCliente anchors by the
// extended LeerAnclasDesde implementation.
//
// All integer fields use 0 as the "not available" sentinel (they are always
// non-negative when computed). Decimal fields use decimal.Zero similarly.
type CobranzaSignals struct {
	ClienteID       int
	NumPagos        int
	CadenciaDias    int
	DiasAtrasoProm  int
	PctPagosATiempo decimal.Decimal
	FechaProxPago   time.Time
	MontoProxPago   decimal.Decimal
}

// MicrosipReader is the analytics module's read-only view of Microsip.
// It MUST NOT issue any writes to Microsip tables.
// The single concrete implementation lives in internal/analytics/infra/analyticsfb.
type MicrosipReader interface {
	// LeerAnclasDesde returns the anchor facts for all lapsed clients.
	//
	// since == nil triggers a full read (all clients regardless of last
	// activity timestamp). A non-nil since limits results to clients whose
	// last purchase or account update occurred at or after that timestamp;
	// the caller is responsible for applying an overlap window to avoid
	// missing rows that landed between runs.
	LeerAnclasDesde(ctx context.Context, since *time.Time) ([]AnclaCliente, error)
}
