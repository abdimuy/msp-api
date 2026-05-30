package ventfb

import (
	"context"

	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: ErrorsRepo satisfies the outbound port.
var _ outbound.ErrorsRepo = (*ErrorsRepo)(nil)

// ErrorsRepo implements outbound.ErrorsRepo backed by MSP_SALDOS_ERRORS.
type ErrorsRepo struct {
	pool *firebird.Pool
}

// NewErrorsRepo builds an ErrorsRepo wired to the given pool.
func NewErrorsRepo(pool *firebird.Pool) *ErrorsRepo {
	return &ErrorsRepo{pool: pool}
}

// Recent returns up to limit error rows from MSP_SALDOS_ERRORS ordered by
// ERROR_AT descending (most recent first).
func (r *ErrorsRepo) Recent(ctx context.Context, limit int) ([]outbound.SaldoError, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(
		ctx, `
SELECT FIRST ? ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT
FROM MSP_SALDOS_ERRORS
ORDER BY ERROR_AT DESC`,
		limit,
	)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []outbound.SaldoError
	for rows.Next() {
		var (
			se         outbound.SaldoError
			errorAtRaw any
			msgRaw     *string
		)
		if scanErr := rows.Scan(&se.ErrorID, &se.CargoID, &msgRaw, &errorAtRaw); scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		if msgRaw != nil {
			se.ErrorMsg = *msgRaw
		}
		t, parseErr := firebird.ScanUTCTime(errorAtRaw)
		if parseErr != nil {
			return nil, parseErr
		}
		se.ErrorAt = t
		result = append(result, se)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}
