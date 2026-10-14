//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// semana es el cálculo de la semana del usuario que comparten el tablero
// (cobertura) y Cuentas: universo, actividades de TODA la zona en la ventana y
// la derivación del estado por venta.
type semana struct {
	ventana     domain.Ventana
	universo    []domain.CuentaUniverso
	actividades []domain.Actividad
	derivacion  domain.Derivacion
}

// calcularSemana exige usuario configurado. La ventana es
// [FECHA_CARGA_INICIAL, ahora], inclusiva (VentanaCobro.kt).
func (s *Service) calcularSemana(ctx context.Context, c contexto) (semana, error) {
	if err := c.usuario.ExigirConfigurado(); err != nil {
		return semana{}, err
	}
	v, err := domain.NuevaVentana(*c.usuario.InicioSemana, c.ahora)
	if err != nil {
		return semana{}, err
	}
	universo, err := s.cuentas.UniversoSemana(ctx, c.usuario.ZonaID, v.Inicio())
	if err != nil {
		return semana{}, err
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, v.Inicio(), v.Fin())
	if err != nil {
		return semana{}, err
	}
	pagos, visitas := aPeriodo(acts)
	d := domain.Derivar(cuentasPeriodo(universo), pagos, visitas, v)
	return semana{ventana: v, universo: universo, actividades: acts, derivacion: d}, nil
}

func cuentasPeriodo(universo []domain.CuentaUniverso) []domain.CuentaPeriodo {
	out := make([]domain.CuentaPeriodo, 0, len(universo))
	for _, c := range universo {
		out = append(out, domain.CuentaPeriodo{VentaID: c.VentaID, ClienteID: c.ClienteID, Parcialidad: c.Parcialidad})
	}
	return out
}

// aPeriodo traduce actividades a las entradas del deriver. Una condonación
// siempre entra con la forma 137026, aunque Microsip le haya puesto otra forma:
// así nunca cuenta como dinero (spec §2). Un pago aún no aplicado entra igual
// que uno aplicado (D-API-8, resolución del arquitecto): el teléfono deriva con
// lo capturado en Room sin esperar a Microsip (msp-app-kt
// feature/pagos/.../application/DerivarEstadoDelPeriodo.kt:73,105-110); solo el
// rechazado queda fuera del dinero (RoomPagosAdapter.kt:105-109).
func aPeriodo(acts []domain.Actividad) ([]domain.PagoPeriodo, []domain.VisitaPeriodo) {
	pagos := make([]domain.PagoPeriodo, 0, len(acts))
	visitas := make([]domain.VisitaPeriodo, 0, len(acts))
	for _, a := range acts {
		switch {
		case a.Pago != nil && a.VentaID > 0:
			forma := a.Pago.FormaCobroID
			if a.Tipo == domain.TipoCondonacion {
				forma = domain.FormaCobroCondonacion
			}
			pagos = append(pagos, domain.PagoPeriodo{
				VentaID: a.VentaID, Importe: a.Pago.Importe, FormaCobroID: forma,
				FechaHora: a.Hora, Rechazado: a.Pago.Rechazado, ActividadID: a.ID,
			})
		case a.Visita != nil:
			vp := domain.VisitaPeriodo{
				ClienteID: a.ClienteID, TipoVisita: a.Visita.TipoVisita, FechaHora: a.Hora, ActividadID: a.ID,
			}
			if a.VentaID > 0 {
				id := a.VentaID
				vp.VentaID = &id
			}
			visitas = append(visitas, vp)
		}
	}
	return pagos, visitas
}
