//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
)

// Periodo del tablero (contrato §2).
type Periodo string

// Periodos.
const (
	PeriodoHoy    Periodo = "hoy"
	PeriodoSemana Periodo = "semana"
)

// TableroInput lleva los parámetros crudos de GET /desempeno/usuarios.
type TableroInput struct {
	Periodo   string
	PausaMin  string
	DesdeHora string
	HastaHora string
}

// Tablero tiene una fila por usuario, sin totales del equipo (D14).
type Tablero struct {
	GeneradoAt time.Time
	Periodo    Periodo
	PausaMin   int
	Filas      []FilaTablero
}

// PagosPorForma cuenta solo efectivo, transferencia y cheque (no existe "depósito").
type PagosPorForma struct {
	Efectivo      domain.ConteoImporte
	Transferencia domain.ConteoImporte
	Cheque        domain.ConteoImporte
}

// CoberturaPorSegmento cuenta de la semana por segmento + sin nada.
type CoberturaPorSegmento struct {
	SinVisitar int
	Volver     int
	Despues    int
	Pagados    int
	SinNada    int
}

// UltimaActividad es `ultima {id, tipo, hora, cliente_nombre}`.
type UltimaActividad struct {
	ID            string
	Tipo          domain.TipoActividad
	Hora          time.Time
	ClienteNombre string
}

// FilaTablero lleva las cifras de UN usuario. Pagos, visitas y pausas son del
// periodo y solo de lo que ESTE usuario registró; cobertura y cobro son de la
// semana del usuario (contrato §2) sobre toda la actividad de su zona
// (decisión D-API-5).
type FilaTablero struct {
	Usuario             domain.UsuarioCobranza
	Configurado         bool
	MotivoNoConfigurado string
	VentasUniverso      int
	EstadoAhora         domain.EstadoAhora
	EstadoDesde         *time.Time
	Ultima              *UltimaActividad
	PrimeraActividad    *time.Time
	UltimaActividadHora *time.Time
	MinutosActivos      int
	Pausas              domain.ResumenPausas
	Pagos               domain.ConteoImporte
	PagosPorForma       PagosPorForma
	Condonaciones       domain.ConteoImporte
	NoAplicados         domain.ConteoImporte
	Visitas             int
	CoberturaConAlgo    int
	CoberturaTotal      int
	PorSegmento         CoberturaPorSegmento
	Cobro               domain.Cobro
	Lineas              []domain.LineaDelDia
}

func parsePeriodo(s string) (Periodo, error) {
	switch Periodo(s) {
	case "", PeriodoHoy:
		return PeriodoHoy, nil
	case PeriodoSemana:
		return PeriodoSemana, nil
	}
	return "", domain.ErrParametroInvalido.WithField("parametro", "periodo")
}

// Tablero arma una fila por usuario. Las lecturas son por lote y no por
// usuario ni por zona: una de actividades (tres sentencias), una de universo
// (una sentencia) y una de cobro de rutas (dos sentencias) para TODA la flota,
// las tres a la vez,
// sea cual sea el número de zonas; el reparto por zona, inicio de semana y
// usuario se hace en Go con las mismas reglas de ventana de la lectura por
// zona (E-INF-014: antes 220 sentencias y 8–9 s por petición con 44 zonas
// en dev; por lote 6 sentencias y 1.6–1.9 s). Un error de Firebird falla la consulta
// completa: el tablero nunca muestra cifras a medias.
func (s *Service) Tablero(ctx context.Context, in TableroInput) (Tablero, error) {
	periodo, err := parsePeriodo(in.Periodo)
	if err != nil {
		return Tablero{}, err
	}
	p, err := domain.NuevosParametrosJornada(in.PausaMin, in.DesdeHora, in.HastaHora)
	if err != nil {
		return Tablero{}, err
	}
	us, err := s.usuarios.ListarUsuarios(ctx)
	if err != nil {
		return Tablero{}, err
	}
	ix := domain.NuevoIndiceUsuarios(us)
	ahora := s.clock.Now()
	semanas, err := s.semanasDelTablero(ctx, us, ix, ahora)
	if err != nil {
		return Tablero{}, err
	}
	t := Tablero{GeneradoAt: ahora, Periodo: periodo, PausaMin: p.PausaMin, Filas: make([]FilaTablero, len(us))}
	for i, u := range us {
		t.Filas[i] = s.filaTablero(semanas, contexto{usuario: u, indice: ix, ahora: ahora}, periodo, p)
	}
	sort.SliceStable(t.Filas, func(i, j int) bool {
		a, b := t.Filas[i].Usuario, t.Filas[j].Usuario
		if a.ZonaNombre != b.ZonaNombre {
			return a.ZonaNombre < b.ZonaNombre
		}
		return a.Nombre < b.Nombre
	})
	return t, nil
}

func filaVacia(u domain.UsuarioCobranza, motivo string) FilaTablero {
	cero := domain.ConteoImporte{Importe: decimal.Zero}
	return FilaTablero{
		Usuario: u, MotivoNoConfigurado: motivo, EstadoAhora: domain.EstadoAhoraNoHaEmpezado,
		Pagos: cero, Condonaciones: cero, NoAplicados: cero,
		PagosPorForma: PagosPorForma{Efectivo: cero, Transferencia: cero, Cheque: cero},
		Cobro:         domain.Cobro{Cobrado: decimal.Zero, Esperado: decimal.Zero},
		Lineas:        []domain.LineaDelDia{},
	}
}

// claveSemana identifica una semana compartible: la misma zona con el mismo
// inicio da el mismo universo, las mismas actividades y el mismo cobro de rutas
// (rutas app/cobranza_usuarios.go CobranzaDeUsuario solo usa ZonaID e
// InicioSemana del usuario).
type claveSemana struct {
	zona   int
	inicio int64
}

func claveSemanaDe(u domain.UsuarioCobranza) claveSemana {
	return claveSemana{zona: u.ZonaID, inicio: u.InicioSemana.UnixNano()}
}

// semanaDeZona es de solo lectura una vez armada: cada fila filtra con
// registradasPor, que copia.
type semanaDeZona struct {
	sem   semana
	cobro domain.Cobro
	hoy   []domain.Actividad // actividades de hoy de TODA la zona
}

// semanasDelTablero arma la semana de cada (zona, inicio) de los usuarios
// configurados con tres lecturas por lote. Las de hoy: si la semana empezó
// antes de hoy ya están en la semana; si empezó hoy a media jornada, se piden
// desde las 00:00 como una ventana más del mismo lote.
func (s *Service) semanasDelTablero(
	ctx context.Context, us []domain.UsuarioCobranza, ix domain.IndiceUsuarios, ahora time.Time,
) (map[claveSemana]*semanaDeZona, error) {
	inicioHoy := domain.DiaDe(ahora, s.loc).InicioDelDia(s.loc)
	var (
		claves    []claveSemana
		ventanas  []domain.Ventana
		repres    []domain.UsuarioCobranza
		consAct   []outbound.ConsultaActividades
		consUni   []outbound.ConsultaUniverso
		hoyAparte = map[int]int{} // índice de clave → índice de su consulta de hoy
		vistas    = map[claveSemana]bool{}
	)
	for _, u := range us {
		if ok, _ := u.ConfiguracionEn(ahora); !ok {
			continue
		}
		k := claveSemanaDe(u)
		if vistas[k] {
			continue
		}
		vistas[k] = true
		v, err := ventanaDe(contexto{usuario: u, indice: ix, ahora: ahora})
		if err != nil {
			return nil, err
		}
		claves, ventanas, repres = append(claves, k), append(ventanas, v), append(repres, u)
		consUni = append(consUni, outbound.ConsultaUniverso{ZonaID: u.ZonaID, InicioSemana: v.Inicio()})
		consAct = append(consAct, outbound.ConsultaActividades{ZonaID: u.ZonaID, Desde: v.Inicio(), Hasta: v.Fin()})
	}
	for i, v := range ventanas {
		if v.Inicio().After(inicioHoy) {
			hoyAparte[i] = len(consAct)
			consAct = append(consAct, outbound.ConsultaActividades{ZonaID: claves[i].zona, Desde: inicioHoy, Hasta: ahora})
		}
	}
	out := make(map[claveSemana]*semanaDeZona, len(claves))
	if len(claves) == 0 {
		return out, nil
	}
	// Las tres lecturas van a la vez, cada una en su conexión (E-INF-014,
	// TestMedirLoteParalelo con 55 usuarios / 44 zonas: en serie 3.1 s, en
	// paralelo 1.6 s). Si una falla, la consulta completa falla.
	var (
		universos [][]domain.CuentaUniverso
		acts      [][]domain.Actividad
		cobros    []domain.CobroSemana
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		var err error
		universos, err = s.universoLote(gctx, consUni)
		return err
	})
	g.Go(func() error {
		var err error
		acts, err = s.actividadesLote(gctx, ix, consAct)
		return err
	})
	g.Go(func() error {
		var err error
		cobros, err = s.cobrosLote(gctx, repres)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}
	for i, k := range claves {
		e := &semanaDeZona{sem: armarSemana(ventanas[i], universos[i], acts[i]), cobro: domain.CalcularCobro(cobros[i])}
		e.hoy = e.sem.actividades
		if j, ok := hoyAparte[i]; ok {
			e.hoy = acts[j]
		}
		out[k] = e
	}
	return out, nil
}

func (s *Service) filaTablero(
	semanas map[claveSemana]*semanaDeZona, c contexto, periodo Periodo, p domain.ParametrosJornada,
) FilaTablero {
	u := c.usuario
	if ok, motivo := u.ConfiguracionEn(c.ahora); !ok {
		return filaVacia(u, motivo)
	}
	fila := filaVacia(u, "")
	fila.Configurado = true

	hoy := domain.DiaDe(c.ahora, s.loc)
	e := semanas[claveSemanaDe(u)]
	sem := e.sem
	fila.Cobro = e.cobro
	s.llenarCobertura(&fila, sem, c.ahora)

	propiasSemana := registradasPor(sem.actividades, u)
	propiasHoy := registradasPor(e.hoy, u)
	jHoy := domain.CalcularJornada(propiasHoy, p, hoy, s.loc, c.ahora)
	fila.EstadoAhora, fila.EstadoDesde = domain.CalcularEstadoAhora(jHoy, p, c.ahora)

	if periodo == PeriodoHoy {
		fila.Lineas = []domain.LineaDelDia{jHoy.Linea}
		acumularResumen(&fila, jHoy.Resumen)
		fila.Pausas = jHoy.Resumen.Pausas
		llenarPeriodo(&fila, jHoy.Actividades)
		return fila
	}
	for d := domain.DiaDe(sem.ventana.Inicio(), s.loc); !d.Despues(hoy); d = d.Siguiente() {
		j := domain.CalcularJornada(propiasSemana, p, d, s.loc, c.ahora)
		fila.Lineas = append(fila.Lineas, j.Linea)
		acumularResumen(&fila, j.Resumen)
		fila.Pausas.N += j.Resumen.Pausas.N
		fila.Pausas.Minutos += j.Resumen.Pausas.Minutos
		fila.Pausas.EnCurso = fila.Pausas.EnCurso || j.Resumen.Pausas.EnCurso
	}
	domain.OrdenarActividades(propiasSemana)
	llenarPeriodo(&fila, propiasSemana)
	return fila
}

func (s *Service) llenarCobertura(fila *FilaTablero, sem semana, ahora time.Time) {
	hoy := domain.DiaDe(ahora, s.loc)
	fila.VentasUniverso = len(sem.universo)
	fila.CoberturaTotal = len(sem.universo)
	for _, r := range sem.derivacion.PorVenta {
		if r.ConAlgo {
			fila.CoberturaConAlgo++
		} else {
			fila.PorSegmento.SinNada++
		}
		seg, ok := domain.SegmentoDe(r, hoy)
		if !ok {
			continue
		}
		switch seg {
		case domain.SegmentoSinVisitar:
			fila.PorSegmento.SinVisitar++
		case domain.SegmentoVolver:
			fila.PorSegmento.Volver++
		case domain.SegmentoDespues:
			fila.PorSegmento.Despues++
		case domain.SegmentoPagados:
			fila.PorSegmento.Pagados++
		}
	}
}

func acumularResumen(fila *FilaTablero, r domain.ResumenJornada) {
	fila.MinutosActivos += r.MinutosActivos
	fila.Pagos.N += r.Pagos.N
	fila.Pagos.Importe = fila.Pagos.Importe.Add(r.Pagos.Importe)
	fila.Condonaciones.N += r.Condonaciones.N
	fila.Condonaciones.Importe = fila.Condonaciones.Importe.Add(r.Condonaciones.Importe)
	fila.NoAplicados.N += r.NoAplicados.N
	fila.NoAplicados.Importe = fila.NoAplicados.Importe.Add(r.NoAplicados.Importe)
	fila.Visitas += r.Visitas
}

// llenarPeriodo llena primera/última actividad, `ultima` y pagos por forma sobre las
// actividades propias del periodo (ya ordenadas).
func llenarPeriodo(fila *FilaTablero, acts []domain.Actividad) {
	if len(acts) == 0 {
		return
	}
	primera, ultima := acts[0], acts[len(acts)-1]
	fila.PrimeraActividad = &primera.Hora
	fila.UltimaActividadHora = &ultima.Hora
	fila.Ultima = &UltimaActividad{ID: ultima.ID, Tipo: ultima.Tipo, Hora: ultima.Hora, ClienteNombre: ultima.ClienteNombre}
	for _, a := range acts {
		if !a.CuentaComoDinero() {
			continue
		}
		var dst *domain.ConteoImporte
		switch a.Pago.FormaCobroID {
		case domain.FormaCobroEfectivo:
			dst = &fila.PagosPorForma.Efectivo
		case domain.FormaCobroTransferencia:
			dst = &fila.PagosPorForma.Transferencia
		case domain.FormaCobroCheque:
			dst = &fila.PagosPorForma.Cheque
		default:
			// Inalcanzable hoy: CuentaComoDinero ya exige 157/158/52569. Un pago
			// aplicado con forma "otra" queda fuera del dinero y de no_aplicados
			// (ver resumir en domain/jornada.go y la medición C0, consulta M3).
			continue
		}
		dst.N++
		dst.Importe = dst.Importe.Add(a.Importe())
	}
}
