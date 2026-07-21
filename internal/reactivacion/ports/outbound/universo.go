//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// ClienteUniverso is a flat, transport-free record of one tratable cliente in
// the Tehuacán piloto universe, read from Microsip. It carries only what the
// cohort builder needs: identity, contact, segment, balance and the baseline
// last-purchase date. It is intentionally NOT a domain entity — the app layer
// turns it into a domain.CohorteCliente.
type ClienteUniverso struct {
	// ClienteID is the Microsip cliente ID. Always > 0.
	ClienteID int
	// Nombre is the cliente display name (CLIENTES.NOMBRE), UTF-8.
	Nombre string
	// Telefono is the primary address phone (DIRS_CLIENTES.TELEFONO1); the reader
	// only returns clientes whose trimmed phone is at least 7 chars long.
	Telefono string
	// Segmento is the pre-classified universe slice for this cliente.
	Segmento domain.Segmento
	// Saldo is the current outstanding balance from MSP_SALDOS_VENTAS. >= 0.
	Saldo decimal.Decimal
	// PorLiquidarPct is SUM(SALDO)/SUM(PRECIO_TOTAL)*100 (0–100); 0 for
	// recien_liquidado clientes.
	PorLiquidarPct decimal.Decimal
	// FechaUltimaCompra is MAX(DOCTOS_PV.FECHA); zero when unknown.
	FechaUltimaCompra time.Time
}

// UniversoReader reads the tratable universe of the reactivación piloto from
// the Microsip read-model. It is read-only and must never write.
type UniversoReader interface {
	// LeerUniversoTehuacan returns every tratable cliente in Tehuacán
	// (DIRS_CLIENTES.CIUDAD_ID = 338, ES_DIR_PPAL='S', phone >= 7 chars) that
	// falls into one of the two segmentos over MSP_SALDOS_VENTAS
	// (CARGO_CANCELADO='N'): recien_liquidado (SALDO=0) or por_liquidar_hueco
	// (0 < SALDO < 20% of PRECIO_TOTAL). A cliente qualifying for both slices is
	// returned once, tagged por_liquidar_hueco.
	LeerUniversoTehuacan(ctx context.Context) ([]ClienteUniverso, error)
}
