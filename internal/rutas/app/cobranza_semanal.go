//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
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
		return nil, nil, err
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

	// Enrich: compute Plazos, Vencidas, Aporte and AplicaPonderado now that we have fechaInicio.
	enrichVentas(ventas, fechaInicio, now)

	fi := fechaInicio
	return ventas, &fi, nil
}

// DesglosePorUsuario returns the per-sale breakdown for a single USER (cobrador),
// computed over THAT user's own window (FECHA_CARGA_INICIAL) — consistent with the
// per-user report. This avoids the duplicated-COBRADOR_ID ambiguity of
// DesglosePorZona (which resolves the window via the COBRADOR_ID→fecha map and can
// pick an arbitrary one when several users share a COBRADOR_ID).
//
// Returns the enriched ventas, the user's window start, and the user's zona id.
// An unknown/inactive uid yields an empty breakdown (not an error).
func (s *Service) DesglosePorUsuario(
	ctx context.Context, uid string,
) ([]rutasdomain.VentaCobranza, *time.Time, int, error) {
	usuarios, err := s.calendario.ListarCobradores(ctx)
	if err != nil {
		return nil, nil, 0, err
	}

	var u *outbound.UsuarioCobrador
	for i := range usuarios {
		if usuarios[i].UID == uid {
			u = &usuarios[i]
			break
		}
	}
	if u == nil || u.CobradorID <= 0 || u.FechaInicio.IsZero() {
		return []rutasdomain.VentaCobranza{}, nil, 0, nil
	}

	now := time.Now().UTC()
	ventas, err := s.cobranza.VentasPorZona(ctx, u.ZonaID, u.FechaInicio, now)
	if err != nil {
		return nil, nil, u.ZonaID, err
	}
	enrichVentas(ventas, u.FechaInicio, now)

	fi := u.FechaInicio
	return ventas, &fi, u.ZonaID, nil
}

// enrichVentas populates Aporte, Vencidas, and AplicaPonderado on each venta
// using fechaInicio (week start) and now (current time = window end).
// This is called after the repo returns raw rows (repo does not have fechaInicio).
// NOTE: This function mutates the slice in-place.
func enrichVentas(ventas []rutasdomain.VentaCobranza, fechaInicio, now time.Time) {
	for i := range ventas {
		v := &ventas[i]

		// Cash sales (de contado) are not credit collection: they contribute
		// zero aporte and never count in the cobertura/ponderado denominators.
		// The repo already filters them out; this is defence in depth so a
		// stray contado row can never inflate a financial metric.
		if v.Frecuencia.EsContado() {
			v.AplicaPonderado = false
			v.Aporte = decimal.Zero
			v.Vencidas = decimal.Zero
			continue
		}

		grace := 0
		if v.Frecuencia == rutasdomain.Quincenal || v.Frecuencia == rutasdomain.Mensual {
			grace = 2
		}
		plazos := decimal.NewFromInt(int64(
			rutasdomain.VencimientosVencidos(v.Frecuencia, v.FechaCargo, fechaInicio, grace),
		))
		v.AplicaPonderado = rutasdomain.AplicaEnVentana(v.Frecuencia, v.FechaCargo, fechaInicio, now)

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
