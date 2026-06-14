// export_test.go exposes package-internal symbols to the external test package
// (app_test) for white-box unit testing. This file is compiled ONLY during
// testing; it does not appear in the production binary.
package app

import (
	"time"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// ExportComputeSegmentoScore exposes the internal computeSegmentoScore function
// for table-driven tests in the app_test package.
func ExportComputeSegmentoScore(c *domain.WinbackCandidato, now time.Time) (domain.Segmento, domain.ScoreWinback, int) {
	return computeSegmentoScore(c, now)
}

// ExportDeterministicControl exposes deterministicControl for property tests.
func ExportDeterministicControl(clienteID int) bool {
	return deterministicControl(clienteID)
}
