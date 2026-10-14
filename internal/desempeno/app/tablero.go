//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
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

// LimiteParaleloTablero acota cuántas filas del tablero se calculan a la vez
// (resolución del arquitecto del 2026-10-14: paralelo acotado con errgroup).
// El pool de Firebird es de 10 conexiones y lo comparte todo el API; con la
// semana calculada una vez por zona (semanasPorZona), una fila nueva hace a lo
// más tres lecturas de Firebird y una de rutas. Cuatro a la vez deja aire al
// resto del API (revisión del carril A). La latencia real se mide en C4.
const LimiteParaleloTablero = 4

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

// Tablero arma una fila por usuario, calculadas en paralelo con a lo más
// LimiteParaleloTablero a la vez. Un error de Firebird en cualquier fila
// falla la consulta completa (y cancela las demás): el tablero nunca muestra
// cifras a medias.
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
	cache := &semanasPorZona{m: map[claveSemana]*semanaDeZona{}}
	t := Tablero{GeneradoAt: ahora, Periodo: periodo, PausaMin: p.PausaMin, Filas: make([]FilaTablero, len(us))}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(LimiteParaleloTablero)
	for i, u := range us {
		g.Go(func() error {
			fila, err := s.filaTablero(gctx, cache, contexto{usuario: u, indice: ix, ahora: ahora}, periodo, p)
			if err != nil {
				return err
			}
			t.Filas[i] = fila // cada goroutine escribe solo su índice
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return Tablero{}, err
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

// semanaDeZona se calcula una sola vez por petición del tablero (sync.Once),
// la pida la fila que la pida primero; las demás esperan y la reutilizan. Es de
// solo lectura una vez calculada: cada fila filtra con registradasPor, que
// copia.
type semanaDeZona struct {
	once  sync.Once
	sem   semana
	cobro domain.Cobro
	hoy   []domain.Actividad // actividades de hoy de TODA la zona
	err   error
}

type semanasPorZona struct {
	mu sync.Mutex
	m  map[claveSemana]*semanaDeZona
}

func (sz *semanasPorZona) de(u domain.UsuarioCobranza) *semanaDeZona {
	k := claveSemana{zona: u.ZonaID, inicio: u.InicioSemana.UnixNano()}
	sz.mu.Lock()
	defer sz.mu.Unlock()
	e, ok := sz.m[k]
	if !ok {
		e = &semanaDeZona{}
		sz.m[k] = e
	}
	return e
}

// semanaCompartida devuelve la semana de la zona del usuario, calculándola si
// es la primera fila que la pide.
func (s *Service) semanaCompartida(ctx context.Context, cache *semanasPorZona, c contexto, hoy domain.Fecha) *semanaDeZona {
	e := cache.de(c.usuario)
	e.once.Do(func() {
		if e.sem, e.err = s.calcularSemana(ctx, c); e.err != nil {
			return
		}
		var cs domain.CobroSemana
		if cs, e.err = s.usuarios.CobroSemana(ctx, c.usuario); e.err != nil {
			return
		}
		e.cobro = domain.CalcularCobro(cs)
		e.hoy, e.err = s.actividadesDeHoy(ctx, c, e.sem, hoy)
	})
	return e
}

func (s *Service) filaTablero(
	ctx context.Context, cache *semanasPorZona, c contexto, periodo Periodo, p domain.ParametrosJornada,
) (FilaTablero, error) {
	u := c.usuario
	if ok, motivo := u.ConfiguracionEn(c.ahora); !ok {
		return filaVacia(u, motivo), nil
	}
	fila := filaVacia(u, "")
	fila.Configurado = true

	hoy := domain.DiaDe(c.ahora, s.loc)
	e := s.semanaCompartida(ctx, cache, c, hoy)
	if e.err != nil {
		return FilaTablero{}, e.err
	}
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
		return fila, nil
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
	return fila, nil
}

// actividadesDeHoy resuelve las de hoy de toda la zona: si la semana empezó antes de hoy,
// las de hoy ya están en la semana; si empezó hoy a media jornada, se consultan desde las 00:00.
func (s *Service) actividadesDeHoy(ctx context.Context, c contexto, sem semana, hoy domain.Fecha) ([]domain.Actividad, error) {
	inicioHoy := hoy.InicioDelDia(s.loc)
	if !sem.ventana.Inicio().After(inicioHoy) {
		return sem.actividades, nil
	}
	return s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, inicioHoy, c.ahora)
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
