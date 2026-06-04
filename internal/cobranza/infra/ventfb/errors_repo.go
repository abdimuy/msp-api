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
	var result []outbound.SaldoError
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(
			ctx, `
SELECT FIRST ? ERROR_ID, CARGO_ID, ERROR_MSG, ERROR_AT
FROM MSP_SALDOS_ERRORS
ORDER BY ERROR_AT DESC`,
			limit,
		)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				se         outbound.SaldoError
				errorAtRaw any
				msgRaw     *string
			)
			if scanErr := rows.Scan(&se.ErrorID, &se.CargoID, &msgRaw, &errorAtRaw); scanErr != nil {
				return firebird.MapError(scanErr)
			}
			if msgRaw != nil {
				se.ErrorMsg = *msgRaw
			}
			t, parseErr := firebird.ScanUTCTime(errorAtRaw)
			if parseErr != nil {
				return parseErr
			}
			se.ErrorAt = t
			result = append(result, se)
		}
		if serr := rows.Err(); serr != nil {
			return firebird.MapError(serr)
		}
		return nil
	})
	return result, err
}
