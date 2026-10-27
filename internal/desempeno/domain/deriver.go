//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Ventana es el periodo de cobro [Inicio, Fin], AMBOS extremos incluidos
// (msp-app-kt@44fe4593 core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/VentanaCobro.kt:44 `contiene`, y rutas cobranza_repo.go:
// FECHA >= ? AND FECHA <= ?).
type Ventana struct {
	inicio time.Time
	fin    time.Time
}

// NuevaVentana rechaza una ventana invertida (VentanaCobro.kt:37-41, `require`).
func NuevaVentana(inicio, fin time.Time) (Ventana, error) {
	if fin.Before(inicio) {
		return Ventana{}, ErrParametroInvalido.WithField("ventana", "fin anterior a inicio")
	}
	return Ventana{inicio: inicio.UTC(), fin: fin.UTC()}, nil
}

// Inicio del periodo.
func (v Ventana) Inicio() time.Time { return v.inicio }

// Fin del periodo.
func (v Ventana) Fin() time.Time { return v.fin }

// Contiene es inclusivo en los dos extremos.
func (v Ventana) Contiene(t time.Time) bool { return !t.Before(v.inicio) && !t.After(v.fin) }

// CuentaPeriodo es una venta a crédito del universo de la semana.
type CuentaPeriodo struct {
	VentaID     int
	ClienteID   int
	Parcialidad decimal.Decimal
}

// PagoPeriodo es un pago (o condonación) de la zona. ActividadID enlaza con la
// Actividad que lo originó ("pago:..."); en el juego de casos va vacío.
type PagoPeriodo struct {
	VentaID      int
	Importe      decimal.Decimal
	FormaCobroID int
	FechaHora    time.Time
	Rechazado    bool
	ActividadID  string
}

// VisitaPeriodo es una visita de la zona. VentaID nil = sin venta ligada
// (MSP_VISITAS.IMPTE_DOCTO_CC_ID nulo o 0). FechaPromesa, FechaCita y HoraCita
// existen para el juego de casos y la fase 2; hoy el servidor no tiene esas
// columnas y siempre llegan nil (spec §9).
type VisitaPeriodo struct {
	ClienteID    int
	VentaID      *int
	TipoVisita   string
	FechaHora    time.Time
	FechaPromesa *Fecha
	FechaCita    *Fecha
	HoraCita     *HoraDelDia
	ActividadID  string
}

// ResultadoCuenta es el estado derivado de una venta y lo que lo justifica.
type ResultadoCuenta struct {
	Estado       EstadoCuenta
	AbonoVentana decimal.Decimal
	Parcialidad  decimal.Decimal
	FechaPromesa *Fecha
	FechaCita    *Fecha
	HoraCita     *HoraDelDia
	// ConAlgo: al menos un pago de cualquier forma (rechazado y condonación
	// incluidos) o una visita que le aplique, dentro de la ventana (spec §2).
	ConAlgo bool
	// DeterminadoPor es el ActividadID que fijó el estado: el pago de dinero más
	// reciente o la visita aplicable más reciente. Vacío en SIN_TOCAR.
	DeterminadoPor string
	// UltimaActividadID / UltimaActividadHora: lo más reciente que le aplica
	// (cualquier pago de la venta o visita aplicable). Vacío si ConAlgo es false.
	UltimaActividadID   string
	UltimaActividadHora time.Time
}

// Incidencias espeja IncidenciaCobranza.kt: conteos sin PII.
type Incidencias struct {
	TipoVisitaFueraDeCatalogo int
	ParcialidadNoPositiva     int
	VisitaSinVenta            int
}

// Derivacion trae una entrada por cada cuenta de la entrada, siempre.
type Derivacion struct {
	PorVenta    map[int]ResultadoCuenta
	Incidencias Incidencias
}

// EstadoPorDinero es EstadoCuentaDeriver.estadoPorDinero
// (msp-app-kt@44fe4593 core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt:79-84, LEÍDO 2026-10-14):
// abono <= 0 → nada; parcialidad <= 0 → ABONO_PARCIAL; abono >= parcialidad →
// PAGO (borde `>=`); si no, ABONO_PARCIAL. La comparación es por valor
// (compareTo de BigDecimal en Kotlin = Cmp de decimal): la escala no cuenta.
// ok=false significa "no entró dinero": quien pregunta debe seguir con las visitas.
func EstadoPorDinero(abono, parcialidad decimal.Decimal) (EstadoCuenta, bool) {
	switch {
	case abono.Sign() <= 0:
		return "", false
	case parcialidad.Sign() <= 0:
		return EstadoAbonoParcial, true
	case abono.GreaterThanOrEqual(parcialidad):
		return EstadoPago, true
	default:
		return EstadoAbonoParcial, true
	}
}

// Derivar es el port de EstadoCuentaDeriver.derivar (EstadoCuentaDeriver.kt:98-120,
// resolver :143-175) más la cobertura (CoberturaDelPeriodo.ventasConAlgo del plan
// Android; el teléfono no tiene `con_algo`). Precedencia:
//  1. El dinero manda: suma de pagos de la ventana con forma 157/158/52569 y
//     no rechazados.
//  2. Sin dinero, la visita más reciente de la ventana que le aplique (las de
//     alcance venta con esa venta + las de alcance cliente de su cliente; en
//     empate exacto gana la de venta, y entre iguales la primera de la entrada).
//  3. SIN_TOCAR.
func Derivar(cuentas []CuentaPeriodo, pagos []PagoPeriodo, visitas []VisitaPeriodo, v Ventana) Derivacion {
	sp := sumarPagos(pagos, v)
	delPeriodo := make([]VisitaPeriodo, 0, len(visitas))
	for _, vis := range visitas {
		if v.Contiene(vis.FechaHora) {
			delPeriodo = append(delPeriodo, vis)
		}
	}
	ix := indexarVisitas(delPeriodo)

	out := Derivacion{PorVenta: make(map[int]ResultadoCuenta, len(cuentas))}
	for _, c := range cuentas {
		out.PorVenta[c.VentaID] = resolverCuenta(c, sp, ix)
	}
	out.Incidencias = contarIncidencias(cuentas, sp, delPeriodo, ix)
	return out
}

// pagosSumados: lo que la derivación necesita de los pagos de la ventana.
type pagosSumados struct {
	abonos           map[int]decimal.Decimal
	ultimoDinero     map[int]PagoPeriodo
	ultimoCualquiera map[int]PagoPeriodo
}

func masReciente(m map[int]PagoPeriodo, p PagoPeriodo) {
	if prev, ok := m[p.VentaID]; !ok || p.FechaHora.After(prev.FechaHora) {
		m[p.VentaID] = p
	}
}

// sumarPagos es EstadoCuentaDeriver.abonosPorVenta (EstadoCuentaDeriver.kt:134-141):
// suma los pagos de la ventana con forma en FORMAS_COBRO_COBRANZA
// (VentanaCobro.kt:65, {157, 158, 52569}). El rechazado se excluye del abono
// igual que el teléfono excluye los marcados DOCTO_CC_ID = -1 antes de derivar
// (feature/pagos/.../data/adapter/RoomPagosAdapter.kt:105-109); a diferencia
// del teléfono, sí cuenta para `con_algo` (spec §2).
func sumarPagos(pagos []PagoPeriodo, v Ventana) pagosSumados {
	sp := pagosSumados{
		abonos:           map[int]decimal.Decimal{},
		ultimoDinero:     map[int]PagoPeriodo{},
		ultimoCualquiera: map[int]PagoPeriodo{},
	}
	for _, p := range pagos {
		if !v.Contiene(p.FechaHora) {
			continue
		}
		masReciente(sp.ultimoCualquiera, p)
		if p.Rechazado || !EsFormaDeDinero(p.FormaCobroID) {
			continue
		}
		sp.abonos[p.VentaID] = sp.abonos[p.VentaID].Add(p.Importe)
		masReciente(sp.ultimoDinero, p)
	}
	return sp
}

func resolverCuenta(c CuentaPeriodo, sp pagosSumados, ix indiceVisitas) ResultadoCuenta {
	abono := sp.abonos[c.VentaID]
	r := ResultadoCuenta{AbonoVentana: abono, Parcialidad: c.Parcialidad, Estado: EstadoSinTocar}
	visita, hayVisita := ix.masRecientePara(c)
	if e, ok := EstadoPorDinero(abono, c.Parcialidad); ok {
		r.Estado = e
		r.DeterminadoPor = sp.ultimoDinero[c.VentaID].ActividadID
	} else if hayVisita {
		r.Estado = EstadoDeTipoVisita(visita.TipoVisita, visita.FechaCita != nil)
		r.FechaPromesa, r.FechaCita, r.HoraCita = visita.FechaPromesa, visita.FechaCita, visita.HoraCita
		r.DeterminadoPor = visita.ActividadID
	}

	pago, hayPago := sp.ultimoCualquiera[c.VentaID]
	r.ConAlgo = hayPago || hayVisita
	switch {
	case hayVisita && (!hayPago || visita.FechaHora.After(pago.FechaHora)):
		r.UltimaActividadID, r.UltimaActividadHora = visita.ActividadID, visita.FechaHora
	case hayPago:
		r.UltimaActividadID, r.UltimaActividadHora = pago.ActividadID, pago.FechaHora
	}
	return r
}

// contarIncidencias es EstadoCuentaDeriver.incidencias (EstadoCuentaDeriver.kt:177-197).
func contarIncidencias(cuentas []CuentaPeriodo, sp pagosSumados, delPeriodo []VisitaPeriodo, ix indiceVisitas) Incidencias {
	inc := Incidencias{VisitaSinVenta: ix.huerfanas}
	for _, vis := range delPeriodo {
		if !EsTipoVisitaConocido(vis.TipoVisita) {
			inc.TipoVisitaFueraDeCatalogo++
		}
	}
	for _, c := range cuentas {
		if c.Parcialidad.Sign() <= 0 && sp.abonos[c.VentaID].Sign() > 0 {
			inc.ParcialidadNoPositiva++
		}
	}
	return inc
}

// indiceVisitas es EstadoCuentaDeriver.IndiceVisitas (EstadoCuentaDeriver.kt:209-247):
// visitas del periodo por cómo se propagan; la partición es la de `de` (:230-246).
type indiceVisitas struct {
	porVenta   map[int][]VisitaPeriodo
	porCliente map[int][]VisitaPeriodo
	huerfanas  int
}

func indexarVisitas(delPeriodo []VisitaPeriodo) indiceVisitas {
	ix := indiceVisitas{porVenta: map[int][]VisitaPeriodo{}, porCliente: map[int][]VisitaPeriodo{}}
	for _, vis := range delPeriodo {
		switch {
		case AlcanceDeTipoVisita(vis.TipoVisita, vis.FechaCita != nil) == AlcanceCliente:
			ix.porCliente[vis.ClienteID] = append(ix.porCliente[vis.ClienteID], vis)
		case vis.VentaID != nil:
			ix.porVenta[*vis.VentaID] = append(ix.porVenta[*vis.VentaID], vis)
		default:
			ix.huerfanas++
		}
	}
	return ix
}

// masRecientePara es masRecienteFor (EstadoCuentaDeriver.kt:223-227): propias
// primero y luego las del cliente; gana el PRIMER máximo (maxByOrNull de
// Kotlin), así que en empate gana la de venta.
func (ix indiceVisitas) masRecientePara(c CuentaPeriodo) (VisitaPeriodo, bool) {
	var mejor VisitaPeriodo
	hay := false
	for _, lista := range [][]VisitaPeriodo{ix.porVenta[c.VentaID], ix.porCliente[c.ClienteID]} {
		for _, vis := range lista {
			if !hay || vis.FechaHora.After(mejor.FechaHora) {
				mejor, hay = vis, true
			}
		}
	}
	return mejor, hay
}
