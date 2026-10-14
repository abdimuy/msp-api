//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"sort"
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
// Cada fila hace hasta tres lecturas de Firebird y una de rutas; ocho a la vez
// no satura el pool. La latencia real se mide en C4 (Apéndice 1).
const LimiteParaleloTablero = 8

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
	t := Tablero{GeneradoAt: ahora, Periodo: periodo, PausaMin: p.PausaMin, Filas: make([]FilaTablero, len(us))}
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(LimiteParaleloTablero)
	for i, u := range us {
		g.Go(func() error {
			fila, err := s.filaTablero(gctx, contexto{usuario: u, indice: ix, ahora: ahora}, periodo, p)
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

func (s *Service) filaTablero(ctx context.Context, c contexto, periodo Periodo, p domain.ParametrosJornada) (FilaTablero, error) {
	u := c.usuario
	if ok, motivo := u.Configuracion(); !ok {
		return filaVacia(u, motivo), nil
	}
	fila := filaVacia(u, "")
	fila.Configurado = true

	sem, err := s.calcularSemana(ctx, c)
	if err != nil {
		return FilaTablero{}, err
	}
	cs, err := s.usuarios.CobroSemana(ctx, u)
	if err != nil {
		return FilaTablero{}, err
	}
	fila.Cobro = domain.CalcularCobro(cs)
	s.llenarCobertura(&fila, sem, c.ahora)

	hoy := domain.DiaDe(c.ahora, s.loc)
	propiasSemana := registradasPor(sem.actividades, u)
	propiasHoy, err := s.actividadesDeHoy(ctx, c, sem, hoy)
	if err != nil {
		return FilaTablero{}, err
	}
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

// actividadesDeHoy resuelve las de hoy: si la semana empezó antes de hoy, las de hoy ya están en
// la semana; si empezó hoy a media jornada, se consultan desde las 00:00.
func (s *Service) actividadesDeHoy(ctx context.Context, c contexto, sem semana, hoy domain.Fecha) ([]domain.Actividad, error) {
	inicioHoy := hoy.InicioDelDia(s.loc)
	if !sem.ventana.Inicio().After(inicioHoy) {
		return registradasPor(sem.actividades, c.usuario), nil
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, inicioHoy, c.ahora)
	if err != nil {
		return nil, err
	}
	return registradasPor(acts, c.usuario), nil
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
			continue
		}
		dst.N++
		dst.Importe = dst.Importe.Add(a.Importe())
	}
}
