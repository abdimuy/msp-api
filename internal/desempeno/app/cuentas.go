//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Órdenes de Cuentas (contrato §5).
const (
	OrdenCuentasCliente         = "cliente"
	OrdenCuentasSaldo           = "saldo"
	OrdenCuentasAtrasos         = "atrasos"
	OrdenCuentasUltimaActividad = "ultima_actividad"
	segmentoSinNada             = "sin_nada"
)

// CuentasInput lleva los parámetros crudos de GET /desempeno/usuarios/{uid}/cuentas.
type CuentasInput struct {
	Segmento string
	Q        string
	Orden    string
	Dir      string
}

// ConteosCuentas cuenta TODO el universo, antes de filtrar (pastillas).
type ConteosCuentas struct {
	SinVisitar int
	Volver     int
	Despues    int
	Pagados    int
	SinNada    int
	Total      int
}

// ActividadDeCuenta es `ultima_actividad {id, tipo, hora}`.
type ActividadDeCuenta struct {
	ID   string
	Tipo domain.TipoActividad
	Hora time.Time
}

// CuentaVista es una venta del universo con su estado de la semana.
type CuentaVista struct {
	Cuenta          domain.CuentaUniverso
	Resultado       domain.ResultadoCuenta
	Trato           domain.Trato
	Etiqueta        string
	Detalle         string
	Segmento        *domain.Segmento
	UltimaActividad *ActividadDeCuenta
	NumAtrasos      int
	Nueva           bool
}

// VistaCuentas es la respuesta de Cuentas.
type VistaCuentas struct {
	InicioSemana time.Time
	Conteos      ConteosCuentas
	Cuentas      []CuentaVista
}

type filtroCuentas struct {
	segmento string
	q        string
	orden    string
	desc     bool
}

func parseFiltroCuentas(in CuentasInput) (filtroCuentas, error) {
	f := filtroCuentas{segmento: in.Segmento, q: normalizar(in.Q), orden: OrdenCuentasCliente}
	switch in.Segmento {
	case "", string(domain.SegmentoSinVisitar), string(domain.SegmentoVolver),
		string(domain.SegmentoDespues), string(domain.SegmentoPagados), segmentoSinNada:
	default:
		return f, invalido("segmento")
	}
	switch in.Orden {
	case "":
	case OrdenCuentasCliente, OrdenCuentasSaldo, OrdenCuentasAtrasos, OrdenCuentasUltimaActividad:
		f.orden = in.Orden
	default:
		return f, invalido("orden")
	}
	switch in.Dir {
	case "", "asc":
	case "desc":
		f.desc = true
	default:
		return f, invalido("dir")
	}
	return f, nil
}

func (f filtroCuentas) acepta(c CuentaVista) bool {
	switch {
	case f.segmento == segmentoSinNada && c.Resultado.ConAlgo:
		return false
	case f.segmento != "" && f.segmento != segmentoSinNada &&
		(c.Segmento == nil || string(*c.Segmento) != f.segmento):
		return false
	}
	if f.q == "" {
		return true
	}
	return strings.Contains(normalizar(c.Cuenta.ClienteNombre), f.q) || strings.Contains(normalizar(c.Cuenta.Folio), f.q)
}

// Cuentas calcula el estado por venta de la semana del usuario, igual que el teléfono
// pero con todas las visitas de la zona (spec §2). D-API-8: para el ESTADO un pago
// pendiente de aplicar sí cuenta como dinero (como en el teléfono, que deriva
// con lo capturado en Room); el rechazado no.
func (s *Service) Cuentas(ctx context.Context, uid string, in CuentasInput) (VistaCuentas, error) {
	f, err := parseFiltroCuentas(in)
	if err != nil {
		return VistaCuentas{}, err
	}
	c, err := s.resolverUsuario(ctx, uid)
	if err != nil {
		return VistaCuentas{}, err
	}
	sem, err := s.calcularSemana(ctx, c)
	if err != nil {
		return VistaCuentas{}, err
	}
	tipoPorID := make(map[string]domain.TipoActividad, len(sem.actividades))
	for _, a := range sem.actividades {
		tipoPorID[a.ID] = a.Tipo
	}

	hoy := domain.DiaDe(c.ahora, s.loc)
	vista := VistaCuentas{InicioSemana: sem.ventana.Inicio(), Cuentas: []CuentaVista{}}
	for _, cu := range sem.universo {
		r := sem.derivacion.PorVenta[cu.VentaID]
		cv := CuentaVista{
			Cuenta: cu, Resultado: r, Trato: domain.TratoDe(r),
			Etiqueta: domain.EtiquetaDe(r), Detalle: domain.DetalleDe(r),
			// D-API-7 (resolución del arquitecto): num_atrasos es la fórmula de
			// overdue_payments_view del teléfono (domain.NumAtrasos), no Vencidas de
			// rutas; nueva es saldo == total − enganche (OrdenDeCobranza.kt:103).
			NumAtrasos: domain.NumAtrasos(cu, c.ahora), Nueva: cu.EsNueva(),
		}
		if seg, ok := domain.SegmentoDe(r, hoy); ok {
			cv.Segmento = &seg
			contarSegmento(&vista.Conteos, seg)
		}
		if !r.ConAlgo {
			vista.Conteos.SinNada++
		}
		if r.UltimaActividadID != "" {
			cv.UltimaActividad = &ActividadDeCuenta{ID: r.UltimaActividadID, Tipo: tipoPorID[r.UltimaActividadID], Hora: r.UltimaActividadHora}
		}
		vista.Conteos.Total++
		if f.acepta(cv) {
			vista.Cuentas = append(vista.Cuentas, cv)
		}
	}
	ordenarCuentas(vista.Cuentas, f)
	return vista, nil
}

func contarSegmento(c *ConteosCuentas, seg domain.Segmento) {
	switch seg {
	case domain.SegmentoSinVisitar:
		c.SinVisitar++
	case domain.SegmentoVolver:
		c.Volver++
	case domain.SegmentoDespues:
		c.Despues++
	case domain.SegmentoPagados:
		c.Pagados++
	}
}

func ordenarCuentas(cs []CuentaVista, f filtroCuentas) {
	sort.SliceStable(cs, func(i, j int) bool {
		c := compararCuentas(cs[i], cs[j], f.orden)
		if f.desc {
			return c > 0
		}
		return c < 0
	})
}

func compararCuentas(a, b CuentaVista, orden string) int {
	var c int
	switch orden {
	case OrdenCuentasSaldo:
		c = a.Cuenta.Saldo.Cmp(b.Cuenta.Saldo)
	case OrdenCuentasAtrasos:
		c = a.NumAtrasos - b.NumAtrasos
	case OrdenCuentasUltimaActividad:
		c = horaDe(a).Compare(horaDe(b))
	default:
		c = strings.Compare(normalizar(a.Cuenta.ClienteNombre), normalizar(b.Cuenta.ClienteNombre))
	}
	if c != 0 {
		return c
	}
	return a.Cuenta.VentaID - b.Cuenta.VentaID
}

// horaDe devuelve la hora; sin actividad ordena como el instante cero (primero en asc).
func horaDe(c CuentaVista) time.Time {
	if c.UltimaActividad == nil {
		return time.Time{}
	}
	return c.UltimaActividad.Hora
}
