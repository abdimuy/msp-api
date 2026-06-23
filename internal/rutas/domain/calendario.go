//nolint:misspell // rutas vocabulary is Spanish per project convention.
package domain

import "time"

// dateUTC truncates t to midnight UTC (civil date, no DST concern).
func dateUTC(t time.Time) time.Time {
	y, m, day := t.UTC().Date()
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

// daysBetween returns the number of civil days between a and b
// (dateUTC(b) − dateUTC(a)), which may be negative.
// Safe over UTC midnights: Sub(...).Hours()/24 is exact when both
// times are midnight UTC (no DST in UTC).
func daysBetween(a, b time.Time) int {
	return int(dateUTC(b).Sub(dateUTC(a)).Hours() / 24)
}

// ultimoDiaDeMes returns midnight UTC on the last day of the month of t.
// Go normalises day 0 of month+1 to the last day of the original month,
// handling 28/29/30/31 and leap years automatically.
func ultimoDiaDeMes(t time.Time) time.Time {
	y, m, _ := t.UTC().Date()
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC)
}

// VencimientosVencidos returns the number of scheduled payment dates that are
// STRICTLY BEFORE fechaInicio AND whose grace period has already elapsed.
// It replaces the old Plazos inference based on FECHA_ULT_PAGO + cadencia.
//
//   - SEMANAL:   floor(daysBetween(fechaCargo, fechaInicio) / 7), min 0.
//     Grace is ignored for weekly (whole-week grace already embedded in the formula).
//   - QUINCENAL: counts day-15 and last-day-of-month dates v where
//     dateUTC(fechaCargo) < v  AND  v + graceDias < dateUTC(fechaInicio).
//   - MENSUAL:   counts day-1 dates v where
//     dateUTC(fechaCargo) < v  AND  v + graceDias < dateUTC(fechaInicio).
//   - Unknown frecuencia → treated as SEMANAL (matches CadenciaDias default).
func VencimientosVencidos(frec Frecuencia, fechaCargo, fechaInicio time.Time, graceDias int) int {
	cargo := dateUTC(fechaCargo)
	inicio := dateUTC(fechaInicio)

	// weeklyVencidos is the common path for SEMANAL and any unknown frecuencia.
	weeklyVencidos := func() int {
		d := daysBetween(fechaCargo, fechaInicio)
		if d < 0 {
			return 0
		}
		return d / 7
	}

	switch frec {
	case Quincenal:
		return contarVencidosQuincenal(cargo, inicio, graceDias)
	case Mensual:
		return contarVencidosMensual(cargo, inicio, graceDias)
	case Semanal:
		return weeklyVencidos()
	default:
		return weeklyVencidos()
	}
}

// contarVencidosMensual iterates day-1 candidates from the month after fechaCargo
// up to the month of fechaInicio, counting those that satisfy the grace filter.
func contarVencidosMensual(cargo, inicio time.Time, graceDias int) int {
	count := 0
	// Start at the first day-1 that is strictly after cargo.
	// The first candidate is day 1 of the month following cargo's month,
	// unless cargo is before the 1st of its own month (not possible since
	// cargo is always day-1-based; but we use strict > check anyway).
	y, m, _ := cargo.Date()
	// First candidate: 1st of cargo's own month — but only if > cargo.
	candidate := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
	if !candidate.After(cargo) {
		// Advance to next month's 1st.
		candidate = time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
	}

	for !candidate.After(inicio) {
		// v + graceDias < inicio (strict)
		withGrace := candidate.AddDate(0, 0, graceDias)
		if withGrace.Before(inicio) {
			count++
		}
		// Next month's 1st.
		y2, m2, _ := candidate.Date()
		candidate = time.Date(y2, m2+1, 1, 0, 0, 0, 0, time.UTC)
	}
	return count
}

// contarVencidosQuincenal iterates day-15 and last-day-of-month candidates
// from the month containing cargo through the month containing inicio,
// counting those that satisfy the grace filter.
func contarVencidosQuincenal(cargo, inicio time.Time, graceDias int) int {
	count := 0
	y, m, _ := cargo.Date()
	yEnd, mEnd, _ := inicio.Date()

	// Iterate months from cargo's month up to inicio's month using a monotonic
	// month index. When cargo's month is AFTER inicio's month (e.g. a venta
	// created in a later month than the cobrador's week-start), idx > idxEnd and
	// the loop body never runs — returning 0 instead of spinning forever.
	idx := y*12 + int(m) - 1
	idxEnd := yEnd*12 + int(mEnd) - 1

	for ; idx <= idxEnd; idx++ {
		yy := idx / 12
		mm := time.Month(idx%12 + 1)
		day15 := time.Date(yy, mm, 15, 0, 0, 0, 0, time.UTC)
		lastDay := ultimoDiaDeMes(time.Date(yy, mm, 1, 0, 0, 0, 0, time.UTC))

		for _, v := range []time.Time{day15, lastDay} {
			// Must be strictly > cargo.
			if !v.After(cargo) {
				continue
			}
			// Must not exceed inicio (candidate itself must be ≤ inicio to be relevant).
			if v.After(inicio) {
				continue
			}
			// v + graceDias < inicio (strict)
			withGrace := v.AddDate(0, 0, graceDias)
			if withGrace.Before(inicio) {
				count++
			}
		}
	}
	return count
}

// AplicaEnVentana reports whether there is a scheduled payment date within
// [desde, hasta] (inclusive, D2) that is STRICTLY after fechaCargo (D3).
// Grace does NOT apply here (D5).
//
//   - SEMANAL:   exists k≥1 such that fechaCargo + 7k falls in [desde, hasta].
//   - QUINCENAL: exists a day-15 or last-day-of-month in [desde, hasta] and > fechaCargo.
//   - MENSUAL:   exists a day-1 in [desde, hasta] and > fechaCargo.
//   - Unknown frecuencia → treated as SEMANAL.
func AplicaEnVentana(frec Frecuencia, fechaCargo, desde, hasta time.Time) bool {
	cargo := dateUTC(fechaCargo)
	lo := dateUTC(desde)
	hi := dateUTC(hasta)

	switch frec {
	case Quincenal:
		return aplicaQuincenalEnVentana(cargo, lo, hi)
	case Mensual:
		return aplicaMensualEnVentana(cargo, lo, hi)
	case Semanal:
		return aplicaSemanalEnVentana(cargo, lo, hi)
	default:
		return aplicaSemanalEnVentana(cargo, lo, hi)
	}
}

// aplicaSemanalEnVentana checks whether any múltiplo-de-7 offset from cargo
// (k≥1) lands in [lo, hi].
//
// In offset-from-cargo space:
//
//	offsetLo = daysBetween(cargo, lo)
//	offsetHi = daysBetween(cargo, hi)
//
// We need a multiple of 7 in [max(offsetLo, 7), offsetHi] (k≥1 forces ≥7).
// If offsetHi < 7 → false (D6: no full week elapsed within window).
func aplicaSemanalEnVentana(cargo, lo, hi time.Time) bool {
	offsetLo := daysBetween(cargo, lo)
	offsetHi := daysBetween(cargo, hi)

	if offsetHi < 7 {
		return false
	}
	// Smallest offset we care about: max(offsetLo, 7).
	minOffset := offsetLo
	if minOffset < 7 {
		minOffset = 7
	}
	// Is there a multiple of 7 in [minOffset, offsetHi]?
	// The smallest multiple of 7 that is >= minOffset:
	// firstMult = ((minOffset + 6) / 7) * 7  (integer ceiling to multiple of 7)
	firstMult := ((minOffset + 6) / 7) * 7
	return firstMult <= offsetHi
}

// aplicaMensualEnVentana checks whether any day-1 in [lo, hi] is > cargo.
func aplicaMensualEnVentana(cargo, lo, hi time.Time) bool {
	y, m, _ := lo.Date()
	yEnd, mEnd, _ := hi.Date()

	for {
		candidate := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
		if candidate.After(hi) {
			break
		}
		if !candidate.Before(lo) && candidate.After(cargo) {
			return true
		}
		if y == yEnd && m == mEnd {
			break
		}
		m++
		if m > 12 {
			m = 1
			y++
		}
	}
	return false
}

// aplicaQuincenalEnVentana checks whether any day-15 or last-day-of-month
// in [lo, hi] is > cargo.
func aplicaQuincenalEnVentana(cargo, lo, hi time.Time) bool {
	y, m, _ := lo.Date()
	yEnd, mEnd, _ := hi.Date()

	// Monotonic month index from lo's month to hi's month. A degenerate window
	// (lo's month after hi's month) yields idx > idxEnd → no iterations → false,
	// instead of looping forever.
	idx := y*12 + int(m) - 1
	idxEnd := yEnd*12 + int(mEnd) - 1

	for ; idx <= idxEnd; idx++ {
		yy := idx / 12
		mm := time.Month(idx%12 + 1)
		day15 := time.Date(yy, mm, 15, 0, 0, 0, 0, time.UTC)
		lastDay := ultimoDiaDeMes(time.Date(yy, mm, 1, 0, 0, 0, 0, time.UTC))

		for _, v := range []time.Time{day15, lastDay} {
			if v.After(hi) {
				continue
			}
			if !v.Before(lo) && v.After(cargo) {
				return true
			}
		}
	}
	return false
}
