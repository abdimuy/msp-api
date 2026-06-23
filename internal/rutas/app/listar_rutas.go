//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
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
			// Non-fatal per-zona: leave metrics nil and continue.
			continue
		}
		reporte := calcReporteZona(r.ZonaID, ventas, fechaInicio, now)
		fi := fechaInicio
		rutas[i].FechaInicioSemana = &fi
		rutas[i].PctCoberturaSemanal = reporte.PctCoberturaSemanal
		rutas[i].PctPonderadoSemanal = reporte.PctPonderadoSemanal
	}
	return rutas, nil
}

// calcReporteZona computes the two weekly metrics for a set of ventas.
// This function is pure (no I/O) and is exercised by unit tests.
func calcReporteZona(
	zonaID int,
	ventas []rutasdomain.VentaCobranza,
	fechaInicio, now time.Time,
) rutasdomain.ReporteZona {
	var (
		coberturaNum int
		coberturaDen int
		aporteSum    decimal.Decimal
		aporteDen    int
	)

	for _, v := range ventas {
		// Denominador cobertura: ventas activas (SALDO > 0 OR pagó en ventana).
		coberturaDen++

		// Numerador cobertura: pagó algo en la ventana.
		if v.AbonoSemana.IsPositive() {
			coberturaNum++
		}

		// Ponderado: cadencia determina si la venta "aplica".
		// SEMANAL: siempre aplica.
		// QUINCENAL/MENSUAL: solo si su próximo vencimiento cae en la ventana.
		// NOTE: La regla de quincenal/mensual es un supuesto a confirmar en
		// producción. El próximo vencimiento se infiere como
		// FechaUltPago + cadenciaDias. Si FechaUltPago es nil (nunca pagó)
		// se usa FechaCargo como base.
		if v.Frecuencia == rutasdomain.Semanal || ventaAplicaEnVentana(v, fechaInicio, now) {
			aporteDen++
			aporteSum = aporteSum.Add(v.Aporte)
		}
	}

	reporte := rutasdomain.ReporteZona{ZonaID: zonaID}

	if coberturaDen > 0 {
		pct := decimal.NewFromInt(int64(coberturaNum)).
			Div(decimal.NewFromInt(int64(coberturaDen))).
			Mul(decimal.NewFromInt(100))
		reporte.PctCoberturaSemanal = &pct
	}

	if aporteDen > 0 {
		pct := aporteSum.
			Div(decimal.NewFromInt(int64(aporteDen))).
			Mul(decimal.NewFromInt(100))
		reporte.PctPonderadoSemanal = &pct
	}

	return reporte
}

// ventaAplicaEnVentana returns true when a QUINCENAL or MENSUAL venta has
// its next expected payment falling within [fechaInicio, now].
// The next due date is inferred as FechaUltPago + cadenciaDias; when
// FechaUltPago is nil, FechaCargo is used as the base.
func ventaAplicaEnVentana(v rutasdomain.VentaCobranza, fechaInicio, now time.Time) bool {
	cadencia := rutasdomain.CadenciaDias(v.Frecuencia)
	base := v.FechaCargo
	if v.FechaUltPago != nil {
		base = *v.FechaUltPago
	}
	nextDue := base.AddDate(0, 0, cadencia)
	return !nextDue.Before(fechaInicio) && !nextDue.After(now)
}
