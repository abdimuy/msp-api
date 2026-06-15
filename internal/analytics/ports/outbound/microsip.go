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
