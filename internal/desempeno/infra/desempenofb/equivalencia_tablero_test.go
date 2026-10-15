//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb_test

// Arnés de EQUIVALENCIA del módulo desempeno contra el Firebird de
// desarrollo (datos reales). Graba la salida completa de las consultas del
// servicio (tablero hoy y semana, jornada, actividad paginada completa y
// cuentas) para todas las zonas reales y un conjunto de usuarios sintéticos, y
// la compara campo por campo contra una grabación previa.
//
// Uso (CLAUDE.md §8: la grabación se hace con el código ANTERIOR al cambio):
//
//	EQUIV_GRABAR=/ruta/fuera/del/repo/antes.json   go test -run TestEquivalenciaDesempeno ...
//	EQUIV_COMPARAR=/ruta/fuera/del/repo/antes.json go test -run TestEquivalenciaDesempeno ...
//
// Con EQUIV_SOLO=tablero solo se calculan y comparan los dos casos del
// tablero (minutos en vez de cuarenta).
//
// La grabación contiene nombres de clientes de la base de desarrollo: va
// SIEMPRE fuera del repositorio. Sin ninguna de las dos variables la prueba se
// salta (CI corre sin FB_DATABASE).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	desempenoclients "github.com/abdimuy/msp-api/internal/desempeno/infra/clients"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/rutas"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
)

// equivAhora: último día denso de la base de desarrollo (18-feb-2026 20:00
// CDMX), el mismo reloj que la medición del tablero.
var equivAhora = time.Date(2026, 2, 19, 2, 0, 0, 0, time.UTC)

type relojEquiv struct{ t time.Time }

func (r relojEquiv) Now() time.Time { return r.t }

// lectorSintetico es el lector de rutas real con la lista de usuarios
// reemplazada por la sintética (sin Firestore). Embebe *rutasapp.Service:
// cualquier otro método del contrato (incluido uno por lote) es el real.
type lectorSintetico struct {
	*rutasapp.Service
	us []rutas.UsuarioCobrador
}

func (l lectorSintetico) UsuariosCobradores(context.Context) ([]rutas.UsuarioCobrador, error) {
	return l.us, nil
}

type registrador struct {
	uid string
	cob int
}

// zonasYRegistradores: todas las zonas con ventas y, por zona, el uid de app
// que más pagos capturó y el COBRADOR_ID que más visitas registró en los 30
// días previos a equivAhora.
func zonasYRegistradores(t *testing.T, pool *firebird.Pool) ([]int, map[int]registrador, map[int]int) {
	t.Helper()
	ctx := context.Background()
	desde, hasta := firebird.ToWallClock(equivAhora.Add(-30*24*time.Hour)), firebird.ToWallClock(equivAhora)
	var zonas []int
	rows, err := pool.QueryContext(ctx, `SELECT DISTINCT ZONA_CLIENTE_ID FROM MSP_SALDOS_VENTAS
		WHERE ZONA_CLIENTE_ID IS NOT NULL ORDER BY 1`)
	require.NoError(t, err)
	for rows.Next() {
		var z int
		require.NoError(t, rows.Scan(&z))
		zonas = append(zonas, z)
	}
	require.NoError(t, rows.Err())
	_ = rows.Close()

	porUID := map[int]registrador{}
	rows, err = pool.QueryContext(ctx, `
SELECT s.ZONA_CLIENTE_ID, u.FIREBASE_UID, pr.COBRADOR_ID, COUNT(*)
FROM MSP_PAGOS_RECIBIDOS pr
JOIN MSP_SALDOS_VENTAS s ON s.DOCTO_CC_ID = pr.CARGO_DOCTO_CC_ID
JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE pr.FECHA >= ? AND pr.FECHA <= ? AND u.FIREBASE_UID IS NOT NULL
GROUP BY 1, 2, 3 ORDER BY 4 DESC, 2`, desde, hasta)
	require.NoError(t, err)
	for rows.Next() {
		var z, n int
		var uid string
		var cob *int
		require.NoError(t, rows.Scan(&z, &uid, &cob, &n))
		if _, ok := porUID[z]; ok {
			continue
		}
		r := registrador{uid: strings.TrimSpace(uid)}
		if cob != nil {
			r.cob = *cob
		}
		porUID[z] = r
	}
	require.NoError(t, rows.Err())
	_ = rows.Close()

	porCob := map[int]int{}
	rows, err = pool.QueryContext(ctx, `
SELECT ZONA_CLIENTE_ID, COBRADOR_ID, COUNT(*) FROM MSP_VISITAS
WHERE FECHA >= ? AND FECHA <= ? GROUP BY 1, 2 ORDER BY 3 DESC, 2`, desde, hasta)
	require.NoError(t, err)
	for rows.Next() {
		var z, cob, n int
		require.NoError(t, rows.Scan(&z, &cob, &n))
		if _, ok := porCob[z]; !ok {
			porCob[z] = cob
		}
	}
	require.NoError(t, rows.Err())
	_ = rows.Close()
	return zonas, porUID, porCob
}

// usuariosEquivalencia arma varios usuarios por zona con inicios distintos:
//   - a: uid real de app (si la zona tiene) con su COBRADOR_ID, inicio hace 6 días;
//   - b: COBRADOR_ID de visitas, inicio hace 2 días a medianoche;
//   - c: mismo COBRADOR_ID que b (lo comparten, D-API-4), inicio HOY a las 10:00;
//   - d: (una de cada tres zonas) inicio hace 30 días;
//   - e: (una de cada cinco) mismo inicio que a, para compartir semana;
//
// más tres sin configurar (sin zona, sin inicio, inicio futuro).
func usuariosEquivalencia(zonas []int, porUID map[int]registrador, porCob map[int]int, nombres map[int]string) []rutas.UsuarioCobrador {
	loc := firebird.BusinessTZ()
	hoy := equivAhora.In(loc)
	medianoche := time.Date(hoy.Year(), hoy.Month(), hoy.Day(), 0, 0, 0, 0, loc)
	ptr := func(t time.Time) *time.Time { u := t.UTC(); return &u }
	var us []rutas.UsuarioCobrador
	add := func(uid string, cob, zona int, ini *time.Time) {
		us = append(us, rutas.UsuarioCobrador{
			UID: uid, Nombre: "Cobrador " + uid, Email: uid + "@muebleriamsp.mx",
			CobradorID: cob, ZonaID: zona, ZonaNombre: nombres[zona], InicioSemana: ini,
		})
	}
	for i, z := range zonas {
		ra := porUID[z]
		uidA := ra.uid
		if uidA == "" {
			uidA = fmt.Sprintf("equiv-a-%d", z)
		}
		cobA := ra.cob
		if cobA <= 0 {
			cobA = 800000 + z
		}
		add(uidA, cobA, z, ptr(equivAhora.Add(-6*24*time.Hour)))
		cobB := porCob[z]
		if cobB <= 0 {
			cobB = 900000 + z
		}
		add(fmt.Sprintf("equiv-b-%d", z), cobB, z, ptr(medianoche.AddDate(0, 0, -2)))
		add(fmt.Sprintf("equiv-c-%d", z), cobB, z, ptr(medianoche.Add(10*time.Hour)))
		if i%3 == 0 {
			add(fmt.Sprintf("equiv-d-%d", z), 700000+z, z, ptr(equivAhora.Add(-30*24*time.Hour)))
		}
		if i%5 == 0 {
			add(fmt.Sprintf("equiv-e-%d", z), 600000+z, z, ptr(equivAhora.Add(-6*24*time.Hour)))
		}
	}
	add("equiv-sin-zona", 1, 0, ptr(equivAhora.Add(-6*24*time.Hour)))
	add("equiv-sin-inicio", 2, zonas[0], nil)
	add("equiv-inicio-futuro", 3, zonas[0], ptr(equivAhora.Add(24*time.Hour)))
	return us
}

// grabacion junta la salida de cada caso, serializada.
type grabacion map[string]string

func (g grabacion) poner(t *testing.T, nombre string, v any, err error) {
	t.Helper()
	if err != nil {
		g[nombre] = "ERROR: " + err.Error()
		return
	}
	b, jerr := json.MarshalIndent(v, "", " ")
	require.NoError(t, jerr, nombre)
	g[nombre] = string(b)
}

// actividadCompleta recorre todas las páginas de una consulta de actividad.
func actividadCompleta(ctx context.Context, svc *desempenoapp.Service, uid string, in desempenoapp.FiltroActividadInput,
) ([]desempenoapp.PaginaActividad, error) {
	var paginas []desempenoapp.PaginaActividad
	for range 1000 {
		p, err := svc.Actividad(ctx, uid, in)
		if err != nil {
			return paginas, err
		}
		paginas = append(paginas, p)
		if p.SiguienteCursor == "" {
			return paginas, nil
		}
		in.Cursor = p.SiguienteCursor
	}
	return paginas, errors.New("más de 1000 páginas")
}

func grabarTodo(t *testing.T, svc *desempenoapp.Service, us []rutas.UsuarioCobrador) grabacion {
	t.Helper()
	ctx := context.Background()
	g := grabacion{}
	for _, periodo := range []string{"hoy", "semana"} {
		tab, err := svc.Tablero(ctx, desempenoapp.TableroInput{Periodo: periodo})
		g.poner(t, "tablero/"+periodo, tab, err)
	}
	if os.Getenv("EQUIV_SOLO") == "tablero" {
		return g
	}
	zonasVistas := map[int]int{}
	for _, u := range us {
		uid := u.UID
		for _, fecha := range []string{"", "2026-02-16"} {
			j, err := svc.Jornada(ctx, uid, desempenoapp.JornadaInput{Fecha: fecha})
			g.poner(t, "jornada/"+uid+"/"+fecha, j, err)
		}
		cs, err := svc.Cuentas(ctx, uid, desempenoapp.CuentasInput{})
		g.poner(t, "cuentas/"+uid, cs, err)
		cs, err = svc.Cuentas(ctx, uid, desempenoapp.CuentasInput{Segmento: "sin_nada", Orden: "saldo", Dir: "desc"})
		g.poner(t, "cuentas-sin-nada/"+uid, cs, err)

		// Actividad: la consulta por omisión completa para el primer usuario de
		// cada zona; todos los órdenes y algunos filtros para las tres primeras.
		n := zonasVistas[u.ZonaID]
		zonasVistas[u.ZonaID]++
		if n > 0 {
			continue
		}
		pags, err := actividadCompleta(ctx, svc, uid, desempenoapp.FiltroActividadInput{})
		g.poner(t, "actividad/"+uid+"/omision", pags, err)
		if len(zonasVistas) > 3 {
			continue
		}
		for _, orden := range []string{"hora", "cliente", "monto", "tipo", "registrado_por"} {
			for _, dir := range []string{"asc", "desc"} {
				pags, err := actividadCompleta(ctx, svc, uid, desempenoapp.FiltroActividadInput{Orden: orden, Dir: dir, Limit: "150"})
				g.poner(t, "actividad/"+uid+"/"+orden+"-"+dir, pags, err)
			}
		}
		for nombre, f := range map[string]desempenoapp.FiltroActividadInput{
			"visitas":   {Tipos: []string{"visita"}, Limit: "25"},
			"oficina":   {RegistradoPor: []string{"oficina"}, Orden: "monto"},
			"q":         {Q: "ma", Orden: "cliente", Dir: "asc", Limit: "20"},
			"fotos-gps": {ConFotos: "true", ConGPS: "true"},
			"monto":     {MontoMin: "100", MontoMax: "500", FormasCobro: []string{"efectivo", "transferencia"}},
			"rango":     {Desde: "2026-01-20T06:00:00Z", Hasta: "2026-02-10T06:00:00Z", Limit: "500"},
		} {
			pags, err := actividadCompleta(ctx, svc, uid, f)
			g.poner(t, "actividad/"+uid+"/filtro-"+nombre, pags, err)
		}
	}
	return g
}

func armarEquivalencia(t *testing.T) (*desempenoapp.Service, []rutas.UsuarioCobrador) {
	t.Helper()
	pool := fbtestutil.NewTestFirebirdPool(t)
	zonas, porUID, porCob := zonasYRegistradores(t, pool)
	rs, err := rutasfb.NewRutasRepo(pool).ListarRutas(context.Background())
	require.NoError(t, err)
	nombres := map[int]string{}
	for _, r := range rs {
		nombres[r.ZonaID] = r.ZonaNombre
	}
	us := usuariosEquivalencia(zonas, porUID, porCob, nombres)
	rutasSvc := rutasapp.NewService(rutasfb.NewRutasRepo(pool), rutasfb.NewCobranzaRepo(pool), nil)
	lector := lectorSintetico{Service: rutasSvc, us: us}
	svc := desempenoapp.NewService(desempenoclients.NewRutasUsuariosClient(lector),
		desempenofb.NewActividadesRepo(pool), desempenofb.NewCuentasRepo(pool),
		relojEquiv{equivAhora}, firebird.BusinessTZ())
	t.Logf("equivalencia: %d zonas, %d usuarios", len(zonas), len(us))
	return svc, us
}

//nolint:paralleltest // recorre la base compartida de punta a punta; no se mezcla con otras.
func TestEquivalenciaDesempeno(t *testing.T) {
	grabar, comparar := os.Getenv("EQUIV_GRABAR"), os.Getenv("EQUIV_COMPARAR")
	if grabar == "" && comparar == "" {
		t.Skip("EQUIV_GRABAR / EQUIV_COMPARAR no definidos")
	}
	requireFBEnv(t)
	svc, us := armarEquivalencia(t)
	t0 := time.Now()
	g := grabarTodo(t, svc, us)
	t.Logf("%d casos en %v", len(g), time.Since(t0).Round(time.Millisecond))
	if grabar != "" {
		b, err := json.Marshal(g)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(grabar, b, 0o600))
		return
	}
	b, err := os.ReadFile(comparar)
	require.NoError(t, err)
	var antes grabacion
	require.NoError(t, json.Unmarshal(b, &antes))
	if solo := os.Getenv("EQUIV_SOLO"); solo != "" {
		// Corrida corta (solo el tablero), p. ej. para el control positivo.
		for k := range antes {
			if !strings.HasPrefix(k, solo+"/") {
				delete(antes, k)
			}
		}
	}
	compararGrabaciones(t, antes, g)
}

// compararGrabaciones falla ante cualquier caso faltante, sobrante o con una
// sola línea distinta, y muestra la primera diferencia de cada caso.
func compararGrabaciones(t *testing.T, antes, despues grabacion) {
	t.Helper()
	nombres := map[string]bool{}
	for k := range antes {
		nombres[k] = true
	}
	for k := range despues {
		nombres[k] = true
	}
	orden := make([]string, 0, len(nombres))
	for k := range nombres {
		orden = append(orden, k)
	}
	sort.Strings(orden)
	diferentes, lineas, errores := 0, 0, 0
	for _, k := range orden {
		a, okA := antes[k]
		d, okD := despues[k]
		switch {
		case !okA:
			t.Errorf("caso nuevo que no estaba en la grabación: %s", k)
			diferentes++
			continue
		case !okD:
			t.Errorf("caso de la grabación que ya no sale: %s", k)
			diferentes++
			continue
		}
		if strings.HasPrefix(a, "ERROR:") {
			errores++
		}
		la, ld := strings.Split(a, "\n"), strings.Split(d, "\n")
		lineas += len(la)
		if a == d {
			continue
		}
		diferentes++
		for i := 0; i < len(la) || i < len(ld); i++ {
			var x, y string
			if i < len(la) {
				x = la[i]
			}
			if i < len(ld) {
				y = ld[i]
			}
			if x != y {
				desde := max(0, i-6)
				t.Errorf("%s difiere en la línea %d:\n  antes:   %q\n  después: %q\n  contexto antes:\n%s",
					k, i+1, x, y, strings.Join(la[desde:min(len(la), i+1)], "\n"))
				break
			}
		}
	}
	t.Logf("comparados %d casos (%d líneas de JSON, %d casos que son error esperado); %d diferentes",
		len(orden), lineas, errores, diferentes)
}
