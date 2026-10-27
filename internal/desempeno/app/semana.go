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

// calcularSemana exige usuario configurado y con inicio de semana no futuro
// (contrato v2.1: 409, nunca el 422 de una ventana invertida). La ventana es
// [FECHA_CARGA_INICIAL, ahora], inclusiva (VentanaCobro.kt).
func (s *Service) calcularSemana(ctx context.Context, c contexto) (semana, error) {
	v, err := ventanaDe(c)
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
	return armarSemana(v, universo, acts), nil
}

// ventanaDe valida la configuración del usuario y arma su ventana de semana.
func ventanaDe(c contexto) (domain.Ventana, error) {
	if err := c.usuario.ExigirConfiguradoEn(c.ahora); err != nil {
		return domain.Ventana{}, err
	}
	return domain.NuevaVentana(*c.usuario.InicioSemana, c.ahora)
}

// armarSemana deriva la semana con lo ya leído: el universo de la zona para
// el inicio de la ventana y TODAS las actividades de la zona en la ventana
// (ya completadas). Lo comparten Cuentas (lectura por zona) y el tablero
// (lectura por lote).
func armarSemana(v domain.Ventana, universo []domain.CuentaUniverso, acts []domain.Actividad) semana {
	universo = sinVentasRepetidas(universo)
	pagos, visitas := aPeriodo(acts)
	d := domain.Derivar(cuentasPeriodo(universo), pagos, visitas, v)
	return semana{ventana: v, universo: universo, actividades: acts, derivacion: d}
}

// sinVentasRepetidas deja la primera aparición de cada VentaID. Derivar indexa
// por venta, así que una repetida (un JOIN del repositorio que duplique filas)
// haría que total ≠ entradas derivadas y rompería sin_nada == total − con_algo.
func sinVentasRepetidas(universo []domain.CuentaUniverso) []domain.CuentaUniverso {
	vistas := make(map[int]bool, len(universo))
	out := make([]domain.CuentaUniverso, 0, len(universo))
	for _, c := range universo {
		if vistas[c.VentaID] {
			continue
		}
		vistas[c.VentaID] = true
		out = append(out, c)
	}
	return out
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
