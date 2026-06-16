//nolint:misspell // Spanish domain vocabulary by project convention.
package analyticsfb

import "time"

// computeProxPago derives the estimated next payment date from the client's
// last payment date and their payment cadence.
//
// Returns zero time.Time when either input is absent (cadenciaDias == 0 or
// ultimaFecha.IsZero()), since a next-payment estimate requires both facts.
//
// Per CLAUDE.md rule #1: derived fields live in Go, not SQL. The previous
// DATEADD(DAY, cadencia, ultima_fecha) SQL expression is replaced by this.
func computeProxPago(ultimaFecha time.Time, cadenciaDias int) time.Time {
	if ultimaFecha.IsZero() || cadenciaDias <= 0 {
		return time.Time{}
	}
	return ultimaFecha.AddDate(0, 0, cadenciaDias)
}
