package outbound

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// DatosPago is what a receipt needs from an applied payment.
type DatosPago struct {
	Folio      string
	Fecha      time.Time
	Monto      decimal.Decimal
	FormaCobro string
	VentaID    int
	VentaFolio string
	Cobrador   string
}

// CambioPago is one entry of the payment changelog: the cursor walks these to
// discover what to enqueue. DoctoCCID is the grouping key — spec §2.3: one
// payment that credits three charges must produce ONE receipt, not three.
type CambioPago struct {
	SeqID     int64
	DoctoCCID int
}

// PagoReader reads applied payments and the changelog that announces them.
//
// Datos reads from DOCTOS_CC, the source — NOT from the MSP_PAGOS_VENTAS
// cache. That distinction is not a preference: reading payment data from the
// cache was a real defect in cobranza, fixed in f665c62 (2026-08-13), and it
// is what left stale coordinates on the phones and forced the sync_epoch
// lever of migration 000055. The cache is for the cursor, not for the data.
type PagoReader interface {
	// Cambios returns changelog entries after seqID, oldest first, at most
	// limite of them.
	Cambios(ctx context.Context, desdeSeqID int64, limite int) ([]CambioPago, error)

	// Datos reads the payment identified by its DOCTOS_CC id.
	Datos(ctx context.Context, doctoCCID int) (DatosPago, error)
}
