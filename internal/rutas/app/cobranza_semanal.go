//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// DesglosePorZona returns the per-sale breakdown for the given zona's
// reporting window. Returns the ventas slice and the fechaInicioSemana
// for the zona's cobrador (nil when no calendar entry exists).
//
// The caller (HTTP handler) uses fechaInicioSemana to populate the
// response; nil means the cobrador has no Firestore entry.
func (s *Service) DesglosePorZona(
	ctx context.Context, zonaID int,
) ([]rutasdomain.VentaCobranza, *time.Time, error) {
	// Resolve cobrador for this zona.
	rutas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, nil, err
	}
	var cobradorID *int
	for _, r := range rutas {
		if r.ZonaID == zonaID {
			cobradorID = r.CobradorID
			break
		}
	}
	if cobradorID == nil {
		// Zona exists but has no cobrador — return empty breakdown.
		return []rutasdomain.VentaCobranza{}, nil, nil
	}

	calendario, err := s.calendario.FechaInicioPorCobrador(ctx)
	if err != nil {
		calendario = map[int]time.Time{}
	}

	fechaInicio, ok := calendario[*cobradorID]
	if !ok {
		return []rutasdomain.VentaCobranza{}, nil, nil
	}

	now := time.Now().UTC()
	ventas, err := s.cobranza.VentasPorZona(ctx, zonaID, fechaInicio, now)
	if err != nil {
		return nil, nil, err
	}

	// Enrich: compute Plazos, Vencidas, and Aporte now that we have fechaInicio.
	enrichVentas(ventas, fechaInicio)

	fi := fechaInicio
	return ventas, &fi, nil
}

// enrichVentas populates Aporte and Vencidas on each venta using fechaInicio.
// This is called after the repo returns raw rows (repo does not have fechaInicio).
// NOTE: This function mutates the slice in-place.
func enrichVentas(ventas []rutasdomain.VentaCobranza, fechaInicio time.Time) {
	for i := range ventas {
		v := &ventas[i]
		cadencia := rutasdomain.CadenciaDias(v.Frecuencia)
		windowDays := fechaInicio.UTC().Sub(v.FechaCargo.UTC()).Hours() / 24.0
		plazos := decimal.NewFromFloat(windowDays / float64(cadencia))

		in := rutasdomain.AporteInput{
			Parcialidad:  v.Parcialidad,
			Plazos:       plazos,
			TotalImporte: v.TotalImporte,
			AbonoSemana:  v.AbonoSemana,
			SaldoHoy:     v.Saldo,
		}
		v.Aporte = rutasdomain.CalcAporte(in)

		// Compute vencidas for the DTO (informational).
		if v.Parcialidad.IsPositive() {
			saldoAlInicio := v.Saldo.Add(v.AbonoSemana)
			pagadoAntes := v.TotalImporte.Sub(saldoAlInicio)
			expectedDebt := v.Parcialidad.Mul(plazos)
			debia := decimal.Min(expectedDebt, v.TotalImporte)
			diff := debia.Sub(pagadoAntes)
			v.Vencidas = decimal.Max(decimal.Zero, diff.Div(v.Parcialidad))
		}
	}
}
