//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/shopspring/decimal"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// Service is the rutas module's query surface.
type Service struct {
	repo       outbound.RutasRepo
	cobranza   outbound.CobranzaRepo
	calendario outbound.CalendarioCobradorClient
}

// NewService builds a Service wired against the given dependencies.
func NewService(
	repo outbound.RutasRepo,
	cobranza outbound.CobranzaRepo,
	calendario outbound.CalendarioCobradorClient,
) *Service {
	return &Service{repo: repo, cobranza: cobranza, calendario: calendario}
}

// ListarRutas returns all zonas with cobrador, client count, total balance,
// and weekly cobranza metrics (pct_cobertura_semanal, pct_ponderado_semanal).
// Zones whose cobrador has no Firestore calendar entry return nil metrics.
func (s *Service) ListarRutas(ctx context.Context) ([]rutasdomain.RutaResumen, error) {
	rutas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, err
	}

	// Fetch cobrador calendar. A missing/empty map is non-fatal: zones
	// without a calendar entry will have nil metrics.
	calendario, err := s.calendario.FechaInicioPorCobrador(ctx)
	if err != nil {
		// Non-fatal: log is surfaced upstream; return rutas without metrics.
		calendario = map[int]time.Time{}
	}

	now := time.Now().UTC()

	for i, r := range rutas {
		if r.CobradorID == nil {
			continue
		}
		fechaInicio, ok := calendario[*r.CobradorID]
		if !ok {
			continue
		}
		// Fetch ventas for this zona within the reporting window.
		ventas, verr := s.cobranza.VentasPorZona(ctx, r.ZonaID, fechaInicio, now)
		if verr != nil {
			// Non-fatal per-zona: log and leave metrics nil, continue.
			slog.WarnContext(ctx, "rutas.cobranza_metricas_zona_error",
				"zona_id", r.ZonaID, "error", verr)
			continue
		}
		enrichVentas(ventas, fechaInicio, now)
		reporte := calcReporteZona(r.ZonaID, ventas)
		fi := fechaInicio
		rutas[i].FechaInicioSemana = &fi
		rutas[i].PctCoberturaSemanal = reporte.PctCoberturaSemanal
		rutas[i].PctPonderadoSemanal = reporte.PctPonderadoSemanal
	}
	return rutas, nil
}

// calcReporteZona computes the two weekly metrics for a set of ventas.
// AplicaPonderado must already be set on each venta by enrichVentas before
// calling this function. This function is pure (no I/O) and is exercised by
// unit tests.
func calcReporteZona(
	zonaID int,
	ventas []rutasdomain.VentaCobranza,
) rutasdomain.ReporteZona {
	var (
		coberturaNum int
		coberturaDen int
	)

	// Cobertura: count ventas and those that paid something in the window.
	for _, v := range ventas {
		coberturaDen++
		if v.AbonoSemana.IsPositive() {
			coberturaNum++
		}
	}

	// Ponderado: delegate to the shared aggregate so listing and modal agree.
	resumen := rutasdomain.CalcularResumenPonderado(ventas)

	reporte := rutasdomain.ReporteZona{ZonaID: zonaID}
	if coberturaDen > 0 {
		pct := decimal.NewFromInt(int64(coberturaNum)).
			Div(decimal.NewFromInt(int64(coberturaDen))).
			Mul(decimal.NewFromInt(100))
		reporte.PctCoberturaSemanal = &pct
	}
	reporte.PctPonderadoSemanal = resumen.Pct
	return reporte
}
