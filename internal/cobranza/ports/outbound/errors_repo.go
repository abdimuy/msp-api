package outbound

import (
	"context"
	"time"
)

// SaldoError is a read projection of one MSP_SALDOS_ERRORS row.
type SaldoError struct {
	ErrorID  int
	CargoID  int
	ErrorMsg string
	ErrorAt  time.Time
}

// ErrorsRepo reads error records written by the MSP_SALDOS_VENTAS triggers and
// the recompute procedure when they encounter unexpected failures.
type ErrorsRepo interface {
	// Recent returns up to limit error rows ordered by ERROR_AT descending.
	// Pass since as zero time to return all errors within the limit.
	Recent(ctx context.Context, limit int) ([]SaldoError, error)
}
