//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"sort"
	"time"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// ListarReporteUsuarios returns the weekly cobranza report keyed by USER (one row
// per Firestore cobrador), not by zona. Each user is evaluated over its OWN window
// (FECHA_CARGA_INICIAL), so two users sharing a COBRADOR_ID/zona get independent
// rows — fixing the old per-zona ambiguity where a duplicated COBRADOR_ID resolved
// to an arbitrary single window.
//
// Only active cobradores are included: CobradorID > 0 AND FechaInicio set. The
// zona cartera (NumClientes/SaldoTotal/ZonaNombre) is shared across users of the
// same zona; only the percentages differ by window. Rows are sorted by zona name
// then user name. A per-user venta-fetch error is non-fatal: it logs and leaves
// that row's percentages nil.
func (s *Service) ListarReporteUsuarios(ctx context.Context) ([]rutasdomain.ReporteUsuario, error) {
	rutas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, err
	}
	rutaPorZona := make(map[int]rutasdomain.RutaResumen, len(rutas))
	for _, r := range rutas {
		rutaPorZona[r.ZonaID] = r
	}

	usuarios, err := s.calendario.ListarCobradores(ctx)
	if err != nil {
		// Non-fatal: without the calendar there is nothing to report. Surface an
		// empty list rather than failing the request (mirrors ListarRutas).
		slog.WarnContext(ctx, "rutas.reporte_usuarios_calendario_error", "error", err)
		return []rutasdomain.ReporteUsuario{}, nil
	}

	now := time.Now().UTC()
	out := make([]rutasdomain.ReporteUsuario, 0, len(usuarios))
	for _, u := range usuarios {
		// Active cobradores only: real COBRADOR_ID and a window start.
		if u.CobradorID <= 0 || u.FechaInicio.IsZero() {
			continue
		}

		ruta := rutaPorZona[u.ZonaID] // zero value when zona unknown
		row := rutasdomain.ReporteUsuario{
			UID:         u.UID,
			Nombre:      u.Nombre,
			Email:       u.Email,
			CobradorID:  u.CobradorID,
			ZonaID:      u.ZonaID,
			ZonaNombre:  ruta.ZonaNombre,
			NumClientes: ruta.NumClientes,
			SaldoTotal:  ruta.SaldoTotal,
			FechaInicio: u.FechaInicio,
		}

		ventas, verr := s.cobranza.VentasPorZona(ctx, u.ZonaID, u.FechaInicio, now)
		if verr != nil {
			slog.WarnContext(ctx, "rutas.reporte_usuarios_ventas_error",
				"uid", u.UID, "zona_id", u.ZonaID, "error", verr)
			out = append(out, row) // percentages stay nil
			continue
		}
		enrichVentas(ventas, u.FechaInicio, now)
		reporte := calcReporteZona(u.ZonaID, ventas)
		row.PctCoberturaSemanal = reporte.PctCoberturaSemanal
		row.PctPonderadoSemanal = reporte.PctPonderadoSemanal
		row.CoberturaNum = reporte.CoberturaNum
		row.CoberturaDen = reporte.CoberturaDen
		row.PonderadoDen = reporte.PonderadoDen
		out = append(out, row)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ZonaNombre != out[j].ZonaNombre {
			return out[i].ZonaNombre < out[j].ZonaNombre
		}
		return out[i].Nombre < out[j].Nombre
	})
	return out, nil
}
