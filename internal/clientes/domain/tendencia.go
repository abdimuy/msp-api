// Package domain — tendencia.go implements least-squares linear-trend
// computation over a monthly series. Pure; no I/O; no time.Now.
//
//nolint:misspell // domain vocabulary is Spanish per project convention.
package domain

import "math"

// Tendencia describes the linear trend of a monthly series plus a change flag.
type Tendencia struct {
	Slope     float64 // least-squares slope (amount per month step); 0 when n<2
	Direccion string  // DireccionMejorando | DireccionEstable | DireccionEmpeorando
	Cambio    bool    // true when the last point deviates notably from the prior mean
}

const (
	// DireccionMejorando indicates a positive (upward) trend in the series.
	DireccionMejorando = "mejorando"
	// DireccionEstable indicates a flat trend in the series.
	DireccionEstable = "estable"
	// DireccionEmpeorando indicates a negative (downward) trend in the series.
	DireccionEmpeorando = "empeorando"
)

// CalcularTendencia computes the trend of a monthly series. valores are the
// per-month amounts in chronological order. Pure; deterministic; no I/O.
//
//   - n < 2          → {Slope:0, Direccion:"estable", Cambio:false}
//   - Slope          = least-squares slope of valores over index 0..n-1
//   - Direccion      = "mejorando" if Slope > umbral, "empeorando" if Slope < -umbral,
//     else "estable"; umbral = 0.05 * max(mediaAbs, 1.0) (5% of series scale)
//   - Cambio         = |ultimo - mediaPrevia| > 0.20 * max(mediaPrevia, 1.0)
//     where mediaPrevia = mean of valores[0..n-2]
func CalcularTendencia(valores []float64) Tendencia {
	n := len(valores)
	if n < 2 {
		return Tendencia{Slope: 0, Direccion: DireccionEstable, Cambio: false}
	}

	// Compute mean of y values.
	meanY := 0.0
	for _, v := range valores {
		meanY += v
	}
	meanY /= float64(n)

	// Mean of indices 0..n-1 is (n-1)/2.
	meanI := float64(n-1) / 2.0

	// Least-squares slope: Σ((i-meanI)(y-meanY)) / Σ((i-meanI)²).
	num := 0.0
	den := 0.0
	for i, v := range valores {
		di := float64(i) - meanI
		dy := v - meanY
		num += di * dy
		den += di * di
	}

	slope := 0.0
	if den != 0 {
		slope = num / den
	}
	if math.IsNaN(slope) || math.IsInf(slope, 0) {
		slope = 0
	}

	// umbral = 5% of the series scale (mean absolute value, floored at 1).
	mediaAbs := 0.0
	for _, v := range valores {
		mediaAbs += math.Abs(v)
	}
	mediaAbs /= float64(n)
	umbral := 0.05 * math.Max(mediaAbs, 1.0)

	var dir string
	switch {
	case slope > umbral:
		dir = DireccionMejorando
	case slope < -umbral:
		dir = DireccionEmpeorando
	default:
		dir = DireccionEstable
	}

	// Cambio: does the last point deviate notably from the prior mean?
	mediaPrevia := 0.0
	for _, v := range valores[:n-1] {
		mediaPrevia += v
	}
	mediaPrevia /= float64(n - 1)

	ultimo := valores[n-1]
	cambio := math.Abs(ultimo-mediaPrevia) > 0.20*math.Max(mediaPrevia, 1.0)

	return Tendencia{
		Slope:     slope,
		Direccion: dir,
		Cambio:    cambio,
	}
}
