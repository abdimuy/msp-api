package outbound

import (
	"context"

	"github.com/shopspring/decimal"
)

// SaldoReader answers what the client still owes on a sale AFTER a payment is
// applied. It is the number the client looks for first, so it is read at
// render time and never computed inside the domain.
//
// SaldoRestante must return the balance AFTER the payment being receipted.
// Spec §7 still lists MSP_SALDOS_VENTAS as the source, and that table is a
// cache that Firebird procedures recalculate — spec §11: it is acceptable
// only if it is already updated in the same transaction that applies the
// payment, and whoever reads it must demonstrate that, not assume it. A stale
// balance on a receipt is a PDF the client keeps and complains with.
type SaldoReader interface {
	SaldoRestante(ctx context.Context, ventaID int) (decimal.Decimal, error)
}
