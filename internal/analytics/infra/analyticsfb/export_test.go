// export_test.go exposes package-internal symbols to the external test package
// (analyticsfb_test) for targeted testing. Compiled only during testing.
package analyticsfb

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// ExportCoalesceUltimoPago exposes coalesceUltimoPago for unit testing the
// FechaUltimoPago fallback (bug #2) without a database.
func ExportCoalesceUltimoPago(saldoCache, pagosVentas time.Time) time.Time {
	return coalesceUltimoPago(saldoCache, pagosVentas)
}

// ExportLeerCobranzaSignals exposes leerCobranzaSignals so integration tests can
// assert the cobranza materialization directly (e.g. single-payment inclusion).
func (r *Repo) ExportLeerCobranzaSignals(ctx context.Context, cutoff time.Time) (map[int]outbound.CobranzaSignals, error) {
	return r.leerCobranzaSignals(ctx, cutoff)
}
