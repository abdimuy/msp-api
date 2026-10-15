//nolint:misspell,paralleltest // medición local contra la base de desarrollo; vocabulario español.
package desempenofb

// Medición del tablero contra el Firebird de desarrollo. Solo corre con
// MEDIR_TABLERO=1 (y la base de desarrollo en el entorno); en CI se salta.
//
// Reloj fijo: 18-feb-2026 20:00 CDMX, el último día denso de la base de
// desarrollo. 55 usuarios sintéticos (MEDIR_USUARIOS) repartidos en las zonas
// con pagos de la semana, inicio de semana hace 6 días.
//
// Cuenta las sentencias con un driver que envuelve al de firebirdsql y cuenta
// cada Prepare (database/sql prepara cada sentencia que ejecuta cuando la
// conexión no expone QueryerContext), y cronometra cada lectura por lote.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	desempenoclients "github.com/abdimuy/msp-api/internal/desempeno/infra/clients"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/rutas"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

var medAhora = time.Date(2026, 2, 19, 2, 0, 0, 0, time.UTC)

// ─── contador de sentencias ──────────────────────────────────────────────────

const nombreContador = "firebirdsql-contador-desempeno"

var (
	sentencias      atomic.Int64
	registroDriver  sync.Once
	errSinDriverMed error
)

type driverContador struct{ base driver.Driver }

func (d driverContador) Open(dsn string) (driver.Conn, error) {
	c, err := d.base.Open(dsn)
	if err != nil {
		return nil, err
	}
	return connContador{Conn: c}, nil
}

type connContador struct{ driver.Conn }

func (c connContador) Prepare(q string) (driver.Stmt, error) {
	sentencias.Add(1)
	return c.Conn.Prepare(q)
}

func (c connContador) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	sentencias.Add(1)
	if pc, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return pc.PrepareContext(ctx, q)
	}
	return c.Conn.Prepare(q)
}

func (c connContador) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if b, ok := c.Conn.(driver.ConnBeginTx); ok {
		return b.BeginTx(ctx, opts)
	}
	return c.Conn.Begin() //nolint:staticcheck // respaldo de un driver sin BeginTx.
}

// poolContador abre un pool contra la base de desarrollo cuyas sentencias se
// cuentan en `sentencias`.
func poolContador(t *testing.T) *firebird.Pool {
	t.Helper()
	registroDriver.Do(func() {
		tmp, err := sql.Open("firebirdsql", "")
		if err != nil {
			errSinDriverMed = err
			return
		}
		base := tmp.Driver()
		_ = tmp.Close()
		sql.Register(nombreContador, driverContador{base: base})
	})
	require.NoError(t, errSinDriverMed)
	db, err := sql.Open(nombreContador, fbtestutil.TestFirebirdConfig(t).DSN())
	require.NoError(t, err)
	db.SetMaxOpenConns(10)
	t.Cleanup(func() { _ = db.Close() })
	return &firebird.Pool{DB: db}
}

// ─── cronómetro por fase ─────────────────────────────────────────────────────

type fase struct {
	nombre     string
	duracion   time.Duration
	sentencias int64
	filas      int
}

type cronometro struct {
	mu    sync.Mutex
	fases []fase
}

func (c *cronometro) medir(nombre string, filas func() int, f func() error) error {
	s0, t0 := sentencias.Load(), time.Now()
	err := f()
	c.mu.Lock()
	c.fases = append(c.fases, fase{nombre: nombre, duracion: time.Since(t0), sentencias: sentencias.Load() - s0, filas: filas()})
	c.mu.Unlock()
	return err
}

func (c *cronometro) informe(t *testing.T, titulo string, total time.Duration, totalSent int64) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "\n=== %s — total %v, %d sentencias\n", titulo, total.Round(time.Millisecond), totalSent)
	_, _ = fmt.Fprintf(&b, "%-34s %10s %11s %8s\n", "fase", "tiempo", "sentencias", "filas")
	for _, f := range c.fases {
		_, _ = fmt.Fprintf(&b, "%-34s %10v %11d %8d\n", f.nombre, f.duracion.Round(time.Millisecond), f.sentencias, f.filas)
	}
	t.Log(b.String())
	c.fases = nil
}

// Decoradores de los puertos por lote (los que usa el tablero).

type actividadesMedidas struct {
	*ActividadesRepo
	c *cronometro
}

func (r actividadesMedidas) ActividadesZonas(ctx context.Context, cs []outbound.ConsultaActividades) ([][]domain.Actividad, error) {
	var out [][]domain.Actividad
	err := r.c.medir(fmt.Sprintf("actividades (%d ventanas)", len(cs)), func() int { return contarFilas(out) }, func() error {
		var err error
		out, err = r.ActividadesRepo.ActividadesZonas(ctx, cs)
		return err
	})
	return out, err
}

type cuentasMedidas struct {
	*CuentasRepo
	c *cronometro
}

func (r cuentasMedidas) UniversoZonas(ctx context.Context, cs []outbound.ConsultaUniverso) ([][]domain.CuentaUniverso, error) {
	var out [][]domain.CuentaUniverso
	err := r.c.medir(fmt.Sprintf("universo (%d semanas)", len(cs)), func() int { return contarFilas(out) }, func() error {
		var err error
		out, err = r.CuentasRepo.UniversoZonas(ctx, cs)
		return err
	})
	return out, err
}

type usuariosMedidos struct {
	outbound.UsuariosCobranza
	us []domain.UsuarioCobranza
	c  *cronometro
}

func (u usuariosMedidos) ListarUsuarios(context.Context) ([]domain.UsuarioCobranza, error) {
	return u.us, nil
}

func (u usuariosMedidos) CobrosSemana(ctx context.Context, us []domain.UsuarioCobranza) ([]domain.CobroSemana, error) {
	var out []domain.CobroSemana
	err := u.c.medir(fmt.Sprintf("rutas cobro (%d semanas)", len(us)), func() int {
		n := 0
		for _, c := range out {
			n += len(c.Ventas)
		}
		return n
	}, func() error {
		var err error
		out, err = u.UsuariosCobranza.(outbound.CobrosLote).CobrosSemana(ctx, us)
		return err
	})
	return out, err
}

func contarFilas[T any](xs [][]T) int {
	n := 0
	for _, x := range xs {
		n += len(x)
	}
	return n
}

type relojMed struct{ t time.Time }

func (r relojMed) Now() time.Time { return r.t }

type lectorMed struct {
	*rutasapp.Service
}

func (lectorMed) UsuariosCobradores(context.Context) ([]rutas.UsuarioCobrador, error) {
	return nil, nil
}

// ─── escenario ───────────────────────────────────────────────────────────────

func zonasConPagos(t *testing.T, pool *firebird.Pool, desde, hasta time.Time) []int {
	t.Helper()
	rows, err := pool.QueryContext(context.Background(), `
SELECT p.ZONA_CLIENTE_ID, COUNT(*) FROM MSP_PAGOS_VENTAS p
WHERE p.FECHA >= ? AND p.FECHA <= ? AND p.ZONA_CLIENTE_ID IS NOT NULL GROUP BY 1 ORDER BY 2 DESC, 1`,
		firebird.ToWallClock(desde), firebird.ToWallClock(hasta))
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var zs []int
	for rows.Next() {
		var z, n int
		require.NoError(t, rows.Scan(&z, &n))
		zs = append(zs, z)
	}
	require.NoError(t, rows.Err())
	return zs
}

type escenarioMed struct {
	c   *cronometro
	us  []domain.UsuarioCobranza
	svc *desempenoapp.Service
}

func armarEscenarioMed(t *testing.T) escenarioMed {
	t.Helper()
	if os.Getenv("MEDIR_TABLERO") == "" {
		t.Skip("MEDIR_TABLERO no definido")
	}
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE no definido")
	}
	pool := poolContador(t)
	c := &cronometro{}
	inicio := medAhora.Add(-6 * 24 * time.Hour)
	zonas := zonasConPagos(t, pool, inicio, medAhora)
	n := 55
	if v := os.Getenv("MEDIR_USUARIOS"); v != "" {
		_, _ = fmt.Sscanf(v, "%d", &n)
	}
	us := make([]domain.UsuarioCobranza, 0, n)
	for i := range n {
		z := zonas[i%len(zonas)]
		ini := inicio
		us = append(us, domain.UsuarioCobranza{
			UID: fmt.Sprintf("med-%02d", i), Nombre: fmt.Sprintf("Cobrador %02d", i),
			Email: fmt.Sprintf("cobrador%02d@muebleriamsp.mx", i), CobradorID: 900000 + i,
			ZonaID: z, ZonaNombre: fmt.Sprintf("ZONA %d", z), InicioSemana: &ini,
		})
	}
	if os.Getenv("MEDIR_GRUPOS") != "" {
		// Peor caso: cada usuario con su propio inicio, a 25 h del anterior
		// (más allá de la tolerancia de agrupación): un grupo por zona.
		for i := range us {
			ini := inicio.Add(-time.Duration(i) * 25 * time.Hour)
			us[i].InicioSemana = &ini
		}
	}
	if os.Getenv("MEDIR_UNO_VIEJO") != "" {
		// Un usuario con la semana de hace 30 días: no debe ensanchar la
		// lectura de las demás zonas.
		viejo := medAhora.Add(-30 * 24 * time.Hour)
		us[0].InicioSemana = &viejo
	}
	rutasSvc := rutasapp.NewService(rutasfb.NewRutasRepo(pool), rutasfb.NewCobranzaRepo(pool), nil)
	usu := usuariosMedidos{UsuariosCobranza: desempenoclients.NewRutasUsuariosClient(lectorMed{rutasSvc}), us: us, c: c}
	svc := desempenoapp.NewService(usu, actividadesMedidas{NewActividadesRepo(pool), c},
		cuentasMedidas{NewCuentasRepo(pool), c}, relojMed{medAhora}, firebird.BusinessTZ())
	t.Logf("escenario: %d usuarios en %d zonas, inicio=%s ahora=%s",
		len(us), len(zonas), inicio.Format(time.RFC3339), medAhora.Format(time.RFC3339))
	return escenarioMed{c: c, us: us, svc: svc}
}

// TestMedirTablero corre el tablero hoy y semana, dos veces cada uno
// (frío/tibio), con tiempo y sentencias por fase.
func TestMedirTablero(t *testing.T) {
	e := armarEscenarioMed(t)
	ctx := context.Background()
	for _, corrida := range []string{"1", "2"} {
		for _, periodo := range []string{"hoy", "semana"} {
			s0, t0 := sentencias.Load(), time.Now()
			tab, err := e.svc.Tablero(ctx, desempenoapp.TableroInput{Periodo: periodo})
			require.NoError(t, err)
			e.c.informe(t, fmt.Sprintf("Tablero periodo=%s corrida=%s filas=%d", periodo, corrida, len(tab.Filas)),
				time.Since(t0), sentencias.Load()-s0)
		}
	}
}

// TestMedirActividad mide la consulta de actividad (todas las páginas) y
// Cuentas de un usuario de cada una de las cinco zonas más densas.
func TestMedirActividad(t *testing.T) {
	e := armarEscenarioMed(t)
	ctx := context.Background()
	vistas := map[int]bool{}
	var filas []string
	for _, u := range e.us {
		if vistas[u.ZonaID] || len(vistas) == 5 {
			continue
		}
		vistas[u.ZonaID] = true
		for _, rango := range []struct{ nombre, desde string }{
			{"semana", ""}, {"92 días", medAhora.Add(-92 * 24 * time.Hour).Format(time.RFC3339)},
		} {
			s0, t0 := sentencias.Load(), time.Now()
			in := desempenoapp.FiltroActividadInput{Desde: rango.desde}
			paginas, items := 0, 0
			var primera time.Duration
			for {
				p, err := e.svc.Actividad(ctx, u.UID, in)
				require.NoError(t, err)
				if paginas == 0 {
					primera = time.Since(t0)
				}
				paginas++
				items += len(p.Items)
				if p.SiguienteCursor == "" {
					break
				}
				in.Cursor = p.SiguienteCursor
			}
			filas = append(filas, fmt.Sprintf("actividad zona=%d rango=%s: %d páginas, %d items, primera página %v, todas %v, %d sentencias",
				u.ZonaID, rango.nombre, paginas, items, primera.Round(time.Millisecond),
				time.Since(t0).Round(time.Millisecond), sentencias.Load()-s0))
		}
		s0, t0 := sentencias.Load(), time.Now()
		cs, err := e.svc.Cuentas(ctx, u.UID, desempenoapp.CuentasInput{})
		require.NoError(t, err)
		filas = append(filas, fmt.Sprintf("cuentas zona=%d: %d cuentas, %v, %d sentencias",
			u.ZonaID, len(cs.Cuentas), time.Since(t0).Round(time.Millisecond), sentencias.Load()-s0))
	}
	sort.Strings(filas)
	t.Log("\n" + strings.Join(filas, "\n"))
}

// consultasDelTablero arma las ventanas que pide el tablero para e.us (una por
// zona, mismo inicio).
func consultasDelTablero(e escenarioMed) ([]outbound.ConsultaActividades, []outbound.ConsultaUniverso, []domain.UsuarioCobranza) {
	var acts []outbound.ConsultaActividades
	var uni []outbound.ConsultaUniverso
	var reps []domain.UsuarioCobranza
	vistas := map[int]bool{}
	for _, u := range e.us {
		if vistas[u.ZonaID] {
			continue
		}
		vistas[u.ZonaID] = true
		acts = append(acts, outbound.ConsultaActividades{ZonaID: u.ZonaID, Desde: *u.InicioSemana, Hasta: medAhora})
		uni = append(uni, outbound.ConsultaUniverso{ZonaID: u.ZonaID, InicioSemana: *u.InicioSemana})
		reps = append(reps, u)
	}
	return acts, uni, reps
}

// TestMedirLoteParalelo compara las tres lecturas por lote del tablero en
// serie contra las tres a la vez (tres conexiones), y cada sentencia de
// actividades por separado.
func TestMedirLoteParalelo(t *testing.T) {
	e := armarEscenarioMed(t)
	ctx := context.Background()
	pool := poolContador(t)
	ar, cr := NewActividadesRepo(pool), NewCuentasRepo(pool)
	rutasSvc := rutasapp.NewService(rutasfb.NewRutasRepo(pool), rutasfb.NewCobranzaRepo(pool), nil)
	cl := desempenoclients.NewRutasUsuariosClient(lectorMed{rutasSvc}).(outbound.CobrosLote)
	acts, uni, reps := consultasDelTablero(e)
	leerTodo := func(paralelo bool) time.Duration {
		t0 := time.Now()
		fs := []func(){
			func() { _, err := cr.UniversoZonas(ctx, uni); require.NoError(t, err) },
			func() { _, err := ar.ActividadesZonas(ctx, acts); require.NoError(t, err) },
			func() { _, err := cl.CobrosSemana(ctx, reps); require.NoError(t, err) },
		}
		var wg sync.WaitGroup
		for _, f := range fs {
			if !paralelo {
				f()
				continue
			}
			wg.Add(1)
			go func() { defer wg.Done(); f() }()
		}
		wg.Wait()
		return time.Since(t0)
	}
	for i := range 3 {
		t.Logf("corrida %d: en serie %v, en paralelo %v", i+1,
			leerTodo(false).Round(time.Millisecond), leerTodo(true).Round(time.Millisecond))
	}
	grupos, hasta := cubrirConsultas(acts)
	for i, ls := range lecturasActividades(grupos, hasta) {
		t0, n := time.Now(), 0
		for _, l := range ls {
			filas, err := leer(ctx, pool, l.sql, l.args, l.scan)
			require.NoError(t, err)
			n += len(filas)
		}
		t.Logf("%s (%d zonas, %d sentencias): %v, %d filas", []string{"aplicados", "pendientes", "visitas"}[i],
			len(uni), len(ls), time.Since(t0).Round(time.Millisecond), n)
	}
	t0 := time.Now()
	_, err := cr.UniversoZonas(ctx, uni)
	require.NoError(t, err)
	t.Logf("sentencia universo (%d zonas): %v", len(uni), time.Since(t0).Round(time.Millisecond))
}
