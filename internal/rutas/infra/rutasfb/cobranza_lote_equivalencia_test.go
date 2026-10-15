//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutasfb_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/rutas"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// loteAhora: último día denso de la base de desarrollo (18-feb-2026 20:00
// CDMX). La base de dev termina en feb-2026: con time.Now() casi no habría
// abonos en la ventana y la equivalencia no probaría nada.
var loteAhora = time.Date(2026, 2, 19, 2, 0, 0, 0, time.UTC)

func todasLasZonas(t *testing.T, pool *firebird.Pool) []int {
	t.Helper()
	rows, err := pool.QueryContext(context.Background(),
		`SELECT DISTINCT ZONA_CLIENTE_ID FROM MSP_SALDOS_VENTAS WHERE ZONA_CLIENTE_ID IS NOT NULL ORDER BY 1`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var zs []int
	for rows.Next() {
		var z int
		require.NoError(t, rows.Scan(&z))
		zs = append(zs, z)
	}
	require.NoError(t, rows.Err())
	require.NotEmpty(t, zs, "la base de desarrollo no tiene ventas: la equivalencia no probaría nada")
	return zs
}

// iniciosDePrueba: hace 6 días, hace 2 días a medianoche, HOY a las 10:00 (la
// semana que empieza a media jornada) y hace 30 días, todo en CDMX.
func iniciosDePrueba(ahora time.Time) []time.Time {
	loc := firebird.BusinessTZ()
	h := ahora.In(loc)
	medianoche := time.Date(h.Year(), h.Month(), h.Day(), 0, 0, 0, 0, loc)
	return []time.Time{
		ahora.Add(-6 * 24 * time.Hour),
		medianoche.AddDate(0, 0, -2).UTC(),
		medianoche.Add(10 * time.Hour).UTC(),
		ahora.Add(-30 * 24 * time.Hour),
	}
}

func fechaNula(t *time.Time) string {
	if t == nil {
		return "nil"
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// describirVenta escribe TODOS los campos de la fila; dos filas son
// equivalentes si su descripción es idéntica. Los decimales se comparan por
// valor (String recorta ceros: 100.00 y 100 son el mismo importe).
func describirVenta(v rutasdomain.VentaCobranza) string {
	return fmt.Sprintf("venta=%d cliente=%d zona=%d nombre=%q folio=%q pv=%d parc=%s frec=%s abono=%s venc=%s aporte=%s saldo=%s fup=%s cargo=%s aplica=%t total=%s",
		v.VentaID, v.ClienteID, v.ZonaID, v.ClienteNombre, v.Folio, v.DoctoPVID, v.Parcialidad, v.Frecuencia,
		v.AbonoSemana, v.Vencidas, v.Aporte, v.Saldo, fechaNula(v.FechaUltPago),
		v.FechaCargo.UTC().Format(time.RFC3339Nano), v.AplicaPonderado, v.TotalImporte)
}

func describirVentas(vs []rutasdomain.VentaCobranza) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = describirVenta(v)
	}
	return out
}

// TestCobranzaRepo_VentasPorZonas_EquivaleZonaPorZona compara la lectura por
// lote contra VentasPorZona (la consulta de GET /v2/rutas/*, que no cambia)
// para TODAS las zonas y cuatro inicios de semana, fila por fila y campo por
// campo, en el mismo orden.
func TestCobranzaRepo_VentasPorZonas_EquivaleZonaPorZona(t *testing.T) { //nolint:paralleltest // recorre la base compartida.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()
	repo := rutasfb.NewCobranzaRepo(pool)
	zonas := todasLasZonas(t, pool)

	for _, hasta := range []time.Time{loteAhora, time.Now().UTC()} {
		var consultas []outbound.ConsultaVentasZona
		for _, z := range zonas {
			for _, d := range iniciosDePrueba(hasta) {
				consultas = append(consultas, outbound.ConsultaVentasZona{ZonaID: z, Desde: d})
			}
		}
		lote, err := repo.VentasPorZonas(ctx, consultas, hasta)
		require.NoError(t, err)
		require.Len(t, lote, len(consultas))

		filas, saldadasConAbono, conAbono := 0, 0, 0
		for i, c := range consultas {
			una, err := repo.VentasPorZona(ctx, c.ZonaID, c.Desde, hasta)
			require.NoError(t, err)
			require.Equal(t, describirVentas(una), describirVentas(lote[i]),
				"zona %d desde %s hasta %s", c.ZonaID, c.Desde.Format(time.RFC3339), hasta.Format(time.RFC3339))
			filas += len(una)
			for _, v := range una {
				if v.AbonoSemana.IsPositive() {
					conAbono++
					if !v.Saldo.IsPositive() {
						saldadasConAbono++
					}
				}
			}
		}
		t.Logf("hasta=%s: %d consultas (%d zonas × %d inicios), %d filas idénticas; %d con abono en la ventana, %d de ellas saldadas",
			hasta.Format(time.RFC3339), len(consultas), len(zonas), len(iniciosDePrueba(hasta)), filas, conAbono, saldadasConAbono)
		if hasta.Equal(loteAhora) {
			// Control positivo: la ventana densa DEBE ejercer las dos ramas
			// (saldo > 0 y saldada con abono); si no, la igualdad no prueba la regla.
			require.Positive(t, conAbono, "sin abonos en la ventana: la equivalencia del abono no se probó")
			require.Positive(t, saldadasConAbono, "sin ventas saldadas con abono: la rama de saldadas no se probó")
		}
	}
}

// calendarioFijo devuelve una lista fija de usuarios (sin Firestore).
type calendarioFijo struct{ us []outbound.UsuarioCobrador }

func (c calendarioFijo) FechaInicioPorCobrador(context.Context) (map[int]time.Time, error) {
	m := map[int]time.Time{}
	for _, u := range c.us {
		m[u.CobradorID] = u.FechaInicio
	}
	return m, nil
}

func (c calendarioFijo) ListarCobradores(context.Context) ([]outbound.UsuarioCobrador, error) {
	return c.us, nil
}

func describirCobro(c rutas.VentaCobro) string {
	return fmt.Sprintf("venta=%d cliente=%d parc=%s abono=%s saldo=%s venc=%s aporte=%s aplica=%t",
		c.VentaID, c.ClienteID, c.Parcialidad, c.AbonoSemana, c.Saldo, c.Vencidas, c.Aporte, c.AplicaPonderado)
}

func describirCobranza(c rutas.CobranzaUsuario) []string {
	out := []string{fmt.Sprintf("usuario=%s pct=%v", c.Usuario.UID, pct(c))}
	for _, v := range c.Ventas {
		out = append(out, describirCobro(v))
	}
	return out
}

func pct(c rutas.CobranzaUsuario) string {
	if c.PctPonderado == nil {
		return "nil"
	}
	return c.PctPonderado.String()
}

// TestCobranzaDeUsuarios_EquivaleACobranzaDeUsuarioYAlDesglose: el lote del
// contrato da, usuario por usuario, lo mismo que CobranzaDeUsuario y que
// DesglosePorUsuario (lo que sirve GET /v2/rutas/usuarios/{uid}/cobranza).
// Las tres usan time.Now() por dentro: la ventana de abonos llega hasta "ahora".
func TestCobranzaDeUsuarios_EquivaleACobranzaDeUsuarioYAlDesglose(t *testing.T) { //nolint:paralleltest // recorre la base compartida.
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()
	zonas := todasLasZonas(t, pool)

	var cal []outbound.UsuarioCobrador
	var us []rutas.UsuarioCobrador
	add := func(uid string, cob, zona int, ini *time.Time) {
		u := rutas.UsuarioCobrador{UID: uid, Nombre: uid, CobradorID: cob, ZonaID: zona, InicioSemana: ini}
		us = append(us, u)
		cu := outbound.UsuarioCobrador{UID: uid, Nombre: uid, CobradorID: cob, ZonaID: zona}
		if ini != nil {
			cu.FechaInicio = *ini
		}
		cal = append(cal, cu)
	}
	// Dos inicios por zona: uno de la ventana densa de la base (hay abonos
	// aunque la ventana llegue hasta hoy) y uno de esta semana.
	for i, z := range zonas {
		a := loteAhora.Add(-6 * 24 * time.Hour)
		b := time.Now().UTC().Add(-3 * 24 * time.Hour)
		add(fmt.Sprintf("lote-a-%d", z), 1000+i, z, &a)
		add(fmt.Sprintf("lote-b-%d", z), 2000+i, z, &b)
	}
	add("lote-sin-zona", 3001, 0, nil)
	add("lote-sin-inicio", 3002, zonas[0], nil)

	svc := rutasapp.NewService(rutasfb.NewRutasRepo(pool), rutasfb.NewCobranzaRepo(pool), calendarioFijo{us: cal})
	lote, err := svc.CobranzaDeUsuarios(ctx, us)
	require.NoError(t, err)
	require.Len(t, lote, len(us))

	ventas, conAbono := 0, 0
	for i, u := range us {
		una, err := svc.CobranzaDeUsuario(ctx, u)
		require.NoError(t, err)
		require.Equal(t, describirCobranza(una), describirCobranza(lote[i]), "usuario %s", u.UID)
		ventas += len(una.Ventas)
		for _, v := range una.Ventas {
			if v.AbonoSemana.IsPositive() {
				conAbono++
			}
		}
		if u.ZonaID <= 0 || u.InicioSemana == nil {
			require.Empty(t, lote[i].Ventas)
			continue
		}
		desglose, _, _, err := svc.DesglosePorUsuario(ctx, u.UID)
		require.NoError(t, err)
		var dd []string
		for _, v := range desglose {
			dd = append(dd, describirCobro(rutas.VentaCobro{
				VentaID: v.VentaID, ClienteID: v.ClienteID, Parcialidad: v.Parcialidad, AbonoSemana: v.AbonoSemana,
				Saldo: v.Saldo, Vencidas: v.Vencidas, Aporte: v.Aporte, AplicaPonderado: v.AplicaPonderado,
			}))
		}
		require.Equal(t, strings.Join(dd, "\n"), strings.Join(describirCobranza(lote[i])[1:], "\n"), "desglose de %s", u.UID)
	}
	t.Logf("%d usuarios, %d ventas idénticas entre lote, CobranzaDeUsuario y DesglosePorUsuario; %d con abono", len(us), ventas, conAbono)
	require.Positive(t, conAbono, "sin abonos: la equivalencia del abono no se probó")
}
