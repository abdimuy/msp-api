package outbound

import (
	"context"

	"github.com/shopspring/decimal"
)

// SaldoReader answers what the client still owes on a sale AFTER a payment is
// applied. It is the number the client looks for first, so it is read at
// render time and never computed inside the domain.
type SaldoReader interface {
	SaldoRestante(ctx context.Context, ventaID int) (decimal.Decimal, error)
}
