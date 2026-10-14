//nolint:paralleltest // serial: comparte una transacción que se revierte.
package desempenohttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// TestHumo_ContraFirebird recorre el camino completo HTTP → servicio →
// repositorios Firebird reales con datos sembrados en una transacción que
// siempre se revierte (CLAUDE.md §7). Solo Firestore es un fake: no se puede
// sembrar dentro de la transacción. Semana del lunes 18 de marzo de 2024 para
// no cruzarse con actividad viva de la base de desarrollo; las aserciones se
// limitan al cliente sembrado.
func TestHumo_ContraFirebird(t *testing.T) {
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
	pool := fbtestutil.NewTestFirebirdPool(t)
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "JUANA ANDRADE NÚÑEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{Total: decimal.NewFromInt(8800)})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(733), // = parcialidad (8800 / 12)
			Fecha:       time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC),
			ConCobrador: true, ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
		})
		visitaID := uuid.New()
		horaVisita := time.Date(2024, 3, 20, 17, 15, 0, 0, time.UTC)
		_, err := q.ExecContext(ctx,
			`INSERT INTO MSP_VISITAS (ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG, NOTA,
			  TIPO_VISITA, ZONA_CLIENTE_ID, CLIENTE_ID, IMPTE_DOCTO_CC_ID)
			 VALUES (?, 'JUAN CARLOS MÉNDEZ', ?, ?, 0, 18.4617, -97.3928, 'Vecina dice que regresa el lunes',
			  'No se encontraba', ?, ?, NULL)`,
			visitaID.String(), abono.CobradorID, firebird.ToWallClock(horaVisita), zona, cliente)
		require.NoError(t, err, "sembrar MSP_VISITAS")

		ini := time.Date(2024, 3, 18, 14, 0, 0, 0, time.UTC)
		usuarios := fakeUsuarios{us: []domain.UsuarioCobranza{{
			UID: "fb-humo-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
			CobradorID: abono.CobradorID, ZonaID: zona, ZonaNombre: "ZONA SEMBRADA", InicioSemana: &ini,
		}}}
		svc := desempenoapp.NewService(usuarios, desempenofb.NewActividadesRepo(pool), desempenofb.NewCuentasRepo(pool),
			reloj{time.Date(2024, 3, 21, 18, 0, 0, 0, time.UTC)}, loc)
		cu := usuarioCon(auth.PermDesempenoVer)
		h := routerCon(&cu, svc)

		pedir := func(path string) map[string]any {
			t.Helper()
			rec := httptest.NewRecorder()
			// El contexto de la transacción viaja en la petición: los repos
			// toman la tx con firebird.GetQuerier.
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx))
			require.Equal(t, http.StatusOK, rec.Code, "%s: %s", path, rec.Body.String())
			var m map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
			return m
		}

		// Actividad de la zona, filtrada al cliente sembrado por su nombre.
		act := pedir("/desempeno/usuarios/fb-humo-juan/actividad?q=juana+andrade+nunez&limit=500")
		claves(t, act, clavesPagina, "actividad")
		var pago, visita map[string]any
		for _, it := range act["items"].([]any) {
			a := it.(map[string]any)
			if a["cliente_id"] != float64(cliente) {
				continue
			}
			revisarActividad(t, a, "items[]")
			switch a["tipo"] {
			case "pago":
				pago = a
			case "visita":
				visita = a
			}
		}
		require.NotNil(t, pago, "el abono sembrado sale en la actividad")
		require.NotNil(t, visita, "la visita sembrada sale en la actividad")
		assert.Equal(t, "733.00", pago["pago"].(map[string]any)["importe"])
		assert.Equal(t, "efectivo", pago["pago"].(map[string]any)["forma_cobro"])
		assert.InDelta(t, venta.CargoCCID, pago["venta_id"], 0)
		assert.Equal(t, venta.Folio, pago["folio"])
		assert.Equal(t, "JUANA ANDRADE NÚÑEZ", pago["cliente_nombre"], "UTF-8 de punta a punta")
		assert.Equal(t, "visita:"+visitaID.String(), visita["id"])
		assert.Equal(t, "no_estaba", visita["visita"].(map[string]any)["estado"])

		// Cuentas: la venta sembrada, pagada esta semana.
		cs := pedir("/desempeno/usuarios/fb-humo-juan/cuentas?q=juana+andrade+nunez")
		claves(t, cs, clavesCuentas, "cuentas")
		var cuenta map[string]any
		for _, c := range cs["cuentas"].([]any) {
			if cm := c.(map[string]any); cm["venta_id"] == float64(venta.CargoCCID) {
				cuenta = cm
			}
		}
		require.NotNil(t, cuenta, "la venta sembrada está en el universo de la semana")
		claves(t, cuenta, clavesCuenta, "cuenta")
		assert.Equal(t, "pago", cuenta["estado"])
		assert.Equal(t, "pagados", cuenta["segmento"])
		assert.Equal(t, "733.00", cuenta["abono_semana"])

		// Jornada del 20 de marzo: la visita la registró este usuario
		// (COBRADOR_ID único).
		j := pedir("/desempeno/usuarios/fb-humo-juan/jornada?fecha=2024-03-20")
		claves(t, j, clavesJornada, "jornada")
		ids := map[string]bool{}
		for _, a := range j["actividades"].([]any) {
			ids[a.(map[string]any)["id"].(string)] = true
		}
		assert.True(t, ids["visita:"+visitaID.String()], "la visita sembrada está en la jornada: %v", ids)

		// Tablero de la semana: la fila del usuario, configurada, con la venta
		// sembrada en su universo.
		tb := pedir("/desempeno/usuarios?periodo=semana")
		claves(t, tb, clavesTablero, "tablero")
		u := claves(t, primero(t, tb["usuarios"], "usuarios"), clavesUsuario, "usuarios[0]")
		assert.Equal(t, true, u["configurado"])
		assert.InDelta(t, zona, u["zona_id"], 0)
		assert.GreaterOrEqual(t, u["ventas_universo"].(float64), 1.0)
		assert.Len(t, u["lineas"], 4, "lunes 18 → jueves 21: "+strconv.Itoa(len(u["lineas"].([]any)))+" días")
	})
}
