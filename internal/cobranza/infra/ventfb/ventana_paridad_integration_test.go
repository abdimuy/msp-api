//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

// Pruebas de integración de la VENTANA de cobranza y de la paridad entre los
// tres canales que la aplican.
//
// El defecto que cubren (D2): los pagos tenían filtro propio (`p.FECHA >=
// desde`) en vez de derivar de la venta. El pago que SALDA una venta la deja
// en SALDO = 0 y con ese filtro se borraba a sí mismo del sync —junto con
// todo el historial anterior de esa venta, cuyas fechas son aún más viejas—.
// El cobrador dejaba de ver el abono, concluía que no se registró y volvía a
// cobrar.
//
// Además, cada canal aplicaba (o no) su propio filtro: /sync, el inventario
// (/digest + /ids) y /by-ids podían responder tres conjuntos distintos para la
// misma zona y la misma ventana. El reconciliador compara uno contra otro, así
// que cualquier diferencia hace que borre como fantasma lo que el sync acaba
// de entregar.
//
// AISLAMIENTO: Digest/ListIDs abren una transacción REPEATABLE READ, que solo
// ve filas COMMITTED. Por eso los fixtures se insertan con pool.ExecContext
// (auto-commit) y se borran en t.Cleanup, igual que en
// digest_query_integration_test.go, en vez de vivir dentro de una transacción
// con rollback.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

// conceptoCobranzaEnRuta es el concepto que el cobrador ve en su app; 155 es
// el abono interno que MSP_RECOMPUTE_SALDO_VENTA cuenta para FECHA_ULT_PAGO
// pero que /sync/pagos excluye.
const (
	conceptoCobranzaEnRuta = 87327
	conceptoAbonoInterno   = 155
)

// folioCorto genera un FOLIO que quepa en el CHAR(9) de DOCTOS_CC.
func folioCorto(prefijo string) string {
	return fmt.Sprintf("%s%X", prefijo, time.Now().UnixNano()&0xFFFFF)
}

// insertCargoVentana inserta un cargo (DOCTOS_CC + IMPORTES_DOCTOS_CC
// TIPO_IMPTE='C') con auto-commit y registra su limpieza. El trigger recalcula
// MSP_SALDOS_VENTAS.
func insertCargoVentana(t *testing.T, pool *firebird.Pool, clienteID int, importe decimal.Decimal) int {
	t.Helper()
	ctx := context.Background()

	var cargoID, impteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&cargoID))
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID))

	now := time.Now()
	_, err := pool.ExecContext(ctx,
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, ?, ?, 'C',
		        225490, ?, ?, '0001',
		        1, 'Cargo fixture ventana E2E',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		cargoID, conceptoCobranzaEnRuta, folioCorto("VW"), now, clienteID)
	require.NoError(t, err, "insertCargoVentana: INSERT DOCTOS_CC")

	_, err = pool.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID, IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'C', NULL, ?, 0, 'N', 'N', 'N')`,
		impteID, cargoID, now, importe)
	require.NoError(t, err, "insertCargoVentana: INSERT IMPORTES_DOCTOS_CC cargo")

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, cargoID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID = ?`, cargoID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_SALDOS_CHANGELOG WHERE DOCTO_CC_ID = ?`, cargoID)
	})
	return cargoID
}

// insertPagoVentana inserta un abono con su PROPIO header DOCTOS_CC —como en
// Microsip real— acreditado a cargoID. La fecha del header es la que termina
// en MSP_PAGOS_VENTAS.FECHA y la que alimenta MSP_SALDOS_VENTAS.FECHA_ULT_PAGO,
// así que es la palanca con la que estas pruebas separan "fecha del pago" de
// "fecha del último pago de la venta". Devuelve el IMPTE_DOCTO_CC_ID.
func insertPagoVentana(
	t *testing.T, pool *firebird.Pool,
	clienteID, cargoID int, importe decimal.Decimal, fecha time.Time, conceptoID int,
) int {
	t.Helper()
	ctx := context.Background()

	var headerID, impteID int
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&headerID))
	require.NoError(t, pool.QueryRowContext(ctx, `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&impteID))

	_, err := pool.ExecContext(ctx,
		`INSERT INTO DOCTOS_CC
		  (DOCTO_CC_ID, CONCEPTO_CC_ID, FOLIO, NATURALEZA_CONCEPTO,
		   SUCURSAL_ID, FECHA, CLIENTE_ID, CLAVE_CLIENTE,
		   TIPO_CAMBIO, DESCRIPCION,
		   SISTEMA_ORIGEN, APLICADO, ESTATUS, ESTATUS_ANT,
		   CONTABILIZADO_GYP, ES_CFD, TIENE_ANTICIPO, CFDI_CERTIFICADO, ENVIADO,
		   INTEG_BA, CONTABILIZADO_BA, CANCELADO)
		VALUES (?, ?, ?, 'R',
		        225490, ?, ?, '0001',
		        1, 'Abono fixture ventana E2E',
		        'CC', 'S', 'N', 'N',
		        'N', 'N', 'N', 'N', 'N',
		        'N', 'N', 'N')`,
		headerID, conceptoID, folioCorto("PW"), fecha, clienteID)
	require.NoError(t, err, "insertPagoVentana: INSERT DOCTOS_CC abono")

	_, err = pool.ExecContext(ctx,
		`INSERT INTO IMPORTES_DOCTOS_CC
		  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, FECHA,
		   TIPO_IMPTE, DOCTO_CC_ACR_ID, IMPORTE, IMPUESTO,
		   APLICADO, ESTATUS, CANCELADO)
		VALUES (?, ?, ?, 'R', ?, ?, 0, 'N', 'N', 'N')`,
		impteID, headerID, fecha, cargoID, importe)
	require.NoError(t, err, "insertPagoVentana: INSERT IMPORTES_DOCTOS_CC abono")

	// El sync de pagos excluye filas escritas por transacciones aún en vuelo
	// (p.TX_ID < watermark, ADR-0007). Cualquier transacción ajena abierta en
	// la BD compartida —otro test, el contenedor de dev— baja el watermark por
	// debajo del TX_ID de estas filas recién escritas y las sacaría de la
	// página, haciendo fallar la prueba por el entorno y no por el predicado.
	// Forzar TX_ID = 1 las deja siempre por debajo del watermark. Mismo truco
	// que forcePagoTxID en cursor_sync_watermark_integration_test.go.
	_, err = pool.ExecContext(ctx,
		`UPDATE MSP_PAGOS_VENTAS SET TX_ID = 1 WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	require.NoError(t, err, "insertPagoVentana: forzar TX_ID")

	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx, `DELETE FROM IMPORTES_DOCTOS_CC WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM DOCTOS_CC WHERE DOCTO_CC_ID = ?`, headerID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
		_, _ = pool.ExecContext(ctx, `DELETE FROM MSP_PAGOS_CHANGELOG WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	})
	return impteID
}

// fixtureVentana es el escenario compartido por las pruebas de este archivo.
// Cubre los cuatro casos que separan un predicado correcto de uno que "pasa
// por casualidad":
//
//	cargoActivo            SALDO > 0                       → dentro
//	cargoSaldadoDentro     SALDO = 0, FECHA_ULT_PAGO hoy   → dentro (el caso del defecto)
//	cargoSaldadoFuera      SALDO = 0, FECHA_ULT_PAGO -60d  → fuera
//	cargoConAbonoInterno   SALDO > 0, abono concepto 155   → la venta dentro, su abono NO
type fixtureVentana struct {
	zonaID int
	desde  time.Time

	cargoActivo          int
	cargoSaldadoDentro   int
	cargoSaldadoFuera    int
	cargoConAbonoInterno int

	pagoActivo       int
	pagoViejoDentro  int // FECHA del pago fuera de la ventana; su venta se saldó dentro
	pagoQueSalda     int
	pagoFuera        int
	pagoAbonoInterno int
}

// cargosEsperados es el conjunto de ventas que los tres canales deben
// devolver para la ventana del fixture.
func (f fixtureVentana) cargosEsperados() map[int]bool {
	return map[int]bool{f.cargoActivo: true, f.cargoSaldadoDentro: true, f.cargoConAbonoInterno: true}
}

// cargosDelFixture son todos los ids que las pruebas conocen; se usa para
// filtrar el ruido de las miles de filas preexistentes de la zona.
func (f fixtureVentana) cargosDelFixture() []int {
	return []int{f.cargoActivo, f.cargoSaldadoDentro, f.cargoSaldadoFuera, f.cargoConAbonoInterno}
}

// pagosEsperados es el conjunto de pagos que los tres canales deben devolver.
func (f fixtureVentana) pagosEsperados() map[int]bool {
	return map[int]bool{f.pagoActivo: true, f.pagoViejoDentro: true, f.pagoQueSalda: true}
}

func (f fixtureVentana) pagosDelFixture() []int {
	return []int{f.pagoActivo, f.pagoViejoDentro, f.pagoQueSalda, f.pagoFuera, f.pagoAbonoInterno}
}

// minimo devuelve el menor de los ids, para acotar las consultas paginadas al
// rango que creó la prueba.
func minimo(ids []int) int {
	m := ids[0]
	for _, id := range ids[1:] {
		if id < m {
			m = id
		}
	}
	return m
}

// buildFixtureVentana arma el escenario y espera el margen de clock-skew del
// sync (syncClockSkewSeconds = 1 s) para que las filas recién escritas entren
// en la cota superior de UPDATED_AT.
func buildFixtureVentana(t *testing.T, pool *firebird.Pool) fixtureVentana {
	t.Helper()

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)
		requireMigration000019(t, q)
	})

	clienteID, zonaID := seedZonedClienteFromPool(t, pool)

	hoy := time.Now()
	viejo := hoy.AddDate(0, 0, -60)
	mil := decimal.RequireFromString("1000.00")

	f := fixtureVentana{zonaID: zonaID, desde: hoy.AddDate(0, 0, -7)}

	f.cargoActivo = insertCargoVentana(t, pool, clienteID, mil)
	f.pagoActivo = insertPagoVentana(t, pool, clienteID, f.cargoActivo,
		decimal.RequireFromString("200.00"), hoy, conceptoCobranzaEnRuta)

	f.cargoSaldadoDentro = insertCargoVentana(t, pool, clienteID, mil)
	f.pagoViejoDentro = insertPagoVentana(t, pool, clienteID, f.cargoSaldadoDentro,
		decimal.RequireFromString("400.00"), viejo, conceptoCobranzaEnRuta)
	f.pagoQueSalda = insertPagoVentana(t, pool, clienteID, f.cargoSaldadoDentro,
		decimal.RequireFromString("600.00"), hoy, conceptoCobranzaEnRuta)

	f.cargoSaldadoFuera = insertCargoVentana(t, pool, clienteID, mil)
	f.pagoFuera = insertPagoVentana(t, pool, clienteID, f.cargoSaldadoFuera,
		mil, viejo, conceptoCobranzaEnRuta)

	f.cargoConAbonoInterno = insertCargoVentana(t, pool, clienteID, mil)
	f.pagoAbonoInterno = insertPagoVentana(t, pool, clienteID, f.cargoConAbonoInterno,
		decimal.RequireFromString("100.00"), hoy, conceptoAbonoInterno)

	// Prerrequisitos: si los triggers no dejaron el escenario como se espera,
	// la prueba no está midiendo el predicado.
	saldos := cobranzaventfb.NewSaldosRepo(pool)
	ctx := context.Background()
	requireSaldo(ctx, t, saldos, f.cargoActivo, false)
	requireSaldo(ctx, t, saldos, f.cargoSaldadoDentro, true)
	requireSaldo(ctx, t, saldos, f.cargoSaldadoFuera, true)
	requireSaldo(ctx, t, saldos, f.cargoConAbonoInterno, false)

	time.Sleep(2 * time.Second)
	return f
}

// requireSaldo verifica el estado del cargo en el cache: saldado o con saldo.
func requireSaldo(ctx context.Context, t *testing.T, repo *cobranzaventfb.SaldosRepo, cargoID int, saldado bool) {
	t.Helper()
	s, err := repo.PorCargo(ctx, cargoID)
	require.NoError(t, err, "prerrequisito: el cache debe tener el cargo %d", cargoID)
	require.Equal(t, saldado, s.Saldo().IsZero(),
		"prerrequisito: cargo %d saldado=%v; saldo=%s", cargoID, saldado, s.Saldo())
}

// idsDePagos proyecta los PKs de una página de pagos, quedándose solo con los
// del fixture.
func idsDePagos(items []domain.Pago, delFixture []int) map[int]bool {
	conocidos := make(map[int]bool, len(delFixture))
	for _, id := range delFixture {
		conocidos[id] = true
	}
	out := make(map[int]bool)
	for _, p := range items {
		if conocidos[p.ImpteDoctoCCID()] {
			out[p.ImpteDoctoCCID()] = true
		}
	}
	return out
}

// idsDeVentas hace lo mismo para ventas.
func idsDeVentas(items []domain.Venta, delFixture []int) map[int]bool {
	conocidos := make(map[int]bool, len(delFixture))
	for _, id := range delFixture {
		conocidos[id] = true
	}
	out := make(map[int]bool)
	for _, v := range items {
		if conocidos[v.DoctoCCID()] {
			out[v.DoctoCCID()] = true
		}
	}
	return out
}

// idsDeLista filtra una lista de PKs dejando solo los del fixture.
func idsDeLista(ids, delFixture []int) map[int]bool {
	conocidos := make(map[int]bool, len(delFixture))
	for _, id := range delFixture {
		conocidos[id] = true
	}
	out := make(map[int]bool)
	for _, id := range ids {
		if conocidos[id] {
			out[id] = true
		}
	}
	return out
}

// ─── D2: el pago deriva de la venta, no de su propia fecha ───────────────────

// TestE2E_SyncPagos_LaVentanaEsDeLaVenta_NoDelPago es la prueba del arreglo en
// pagos_repo.go. Con el filtro viejo (`p.FECHA >= desde`) el pago viejo de una
// venta saldada dentro de la ventana desaparecía: su FECHA es de hace 60 días
// y su venta ya no tiene saldo. Con el predicado correcto
// (`s.FECHA_ULT_PAGO >= desde`) viaja el historial completo de esa venta.
//
//nolint:paralleltest // serial: fixtures committed en la BD compartida.
func TestE2E_SyncPagos_LaVentanaEsDeLaVenta_NoDelPago(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	f := buildFixtureVentana(t, pool)

	repo := cobranzaventfb.NewPagosRepo(pool)
	afterID := minimo(f.pagosDelFixture()) - 1

	page, err := repo.SyncPorZona(context.Background(), f.zonaID, time.Time{}, afterID, 5000, f.desde)
	require.NoError(t, err)
	got := idsDePagos(page.Items, f.pagosDelFixture())

	assert.True(t, got[f.pagoQueSalda], "el pago que saldó la venta debe viajar")
	assert.True(t, got[f.pagoViejoDentro],
		"el pago viejo de una venta saldada DENTRO de la ventana también debe viajar: "+
			"si no, el cobrador ve la venta sin la mitad de sus abonos y vuelve a cobrar")
	assert.True(t, got[f.pagoActivo], "el pago de una venta con saldo debe viajar siempre")
	assert.False(t, got[f.pagoFuera],
		"el pago de una venta saldada FUERA de la ventana no debe viajar")
	assert.False(t, got[f.pagoAbonoInterno],
		"el abono de concepto 155 no es cobranza en ruta; el filtro de concepto lo excluye")
}

// ─── D2: el inventario mira exactamente lo mismo que el sync ─────────────────

// TestE2E_DigestPagos_MismaVentanaQueElSync cubre el arreglo en
// digest_query.go. Si el inventario usara otro predicado, el reconciliador
// borraría del teléfono justo los pagos que el sync acaba de entregar.
//
//nolint:paralleltest // serial: fixtures committed en la BD compartida.
func TestE2E_DigestPagos_MismaVentanaQueElSync(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	f := buildFixtureVentana(t, pool)

	repo := cobranzaventfb.NewPagosRepo(pool)
	after := minimo(f.pagosDelFixture()) - 1

	ids, _, err := repo.ListIDs(context.Background(), f.zonaID, after, 5000, f.desde)
	require.NoError(t, err)
	got := idsDeLista(ids, f.pagosDelFixture())

	assert.True(t, got[f.pagoViejoDentro],
		"el inventario debe listar el pago viejo de la venta saldada dentro de la ventana")
	assert.True(t, got[f.pagoQueSalda], "el inventario debe listar el pago que saldó la venta")
	assert.True(t, got[f.pagoActivo], "el inventario debe listar el pago de la venta con saldo")
	assert.False(t, got[f.pagoFuera], "el inventario no debe listar pagos fuera de la ventana")
}

// ─── D2: by-ids dejó de ser el canal permisivo ───────────────────────────────

// TestE2E_ByIDs_AplicaLaVentana cubre el arreglo en PagosRepo.ByIDs y
// VentasRepo.ByIDs. Antes el WHERE era solo zona + PK: pedir un id devolvía la
// fila aunque el sync y el inventario la excluyeran.
//
//nolint:paralleltest // serial: fixtures committed en la BD compartida.
func TestE2E_ByIDs_AplicaLaVentana(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	f := buildFixtureVentana(t, pool)
	ctx := context.Background()

	pagos := cobranzaventfb.NewPagosRepo(pool)
	filas, err := pagos.ByIDs(ctx, f.zonaID, f.pagosDelFixture(), f.desde)
	require.NoError(t, err)
	got := idsDePagos(filas, f.pagosDelFixture())

	assert.True(t, got[f.pagoQueSalda], "by-ids debe devolver el pago que saldó la venta")
	assert.True(t, got[f.pagoViejoDentro], "by-ids debe devolver el historial de esa venta")
	assert.False(t, got[f.pagoFuera],
		"by-ids no puede devolver un pago que el sync y el inventario excluyen")
	assert.False(t, got[f.pagoAbonoInterno],
		"by-ids tampoco puede saltarse el filtro de concepto")

	ventas := cobranzaventfb.NewVentasRepo(pool)
	filasV, err := ventas.ByIDs(ctx, f.zonaID, f.cargosDelFixture(), f.desde)
	require.NoError(t, err)
	gotV := idsDeVentas(filasV, f.cargosDelFixture())

	assert.True(t, gotV[f.cargoSaldadoDentro], "by-ids debe devolver la venta saldada dentro de la ventana")
	assert.True(t, gotV[f.cargoActivo], "by-ids debe devolver la venta con saldo")
	assert.False(t, gotV[f.cargoSaldadoFuera],
		"by-ids no puede devolver una venta saldada fuera de la ventana")
}

// ─── La prueba que faltaba: paridad de los tres canales ──────────────────────

// TestE2E_ParidadCanales_Pagos exige que /sync (sin tombstones), el inventario
// (/ids) y /by-ids devuelvan el MISMO conjunto de ids para la misma zona y la
// misma ventana. La ausencia de esta prueba es lo que dejó que los tres
// predicados se separaran sin que nada fallara.
//
//nolint:paralleltest // serial: fixtures committed en la BD compartida.
func TestE2E_ParidadCanales_Pagos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	f := buildFixtureVentana(t, pool)
	ctx := context.Background()

	repo := cobranzaventfb.NewPagosRepo(pool)
	after := minimo(f.pagosDelFixture()) - 1

	page, err := repo.SyncPorZona(ctx, f.zonaID, time.Time{}, after, 5000, f.desde)
	require.NoError(t, err)
	desdeSync := idsDePagos(page.Items, f.pagosDelFixture())

	ids, _, err := repo.ListIDs(ctx, f.zonaID, after, 5000, f.desde)
	require.NoError(t, err)
	desdeInventario := idsDeLista(ids, f.pagosDelFixture())

	filas, err := repo.ByIDs(ctx, f.zonaID, f.pagosDelFixture(), f.desde)
	require.NoError(t, err)
	desdeByIDs := idsDePagos(filas, f.pagosDelFixture())

	esperado := f.pagosEsperados()
	assert.Equal(t, esperado, desdeSync, "sync devuelve un conjunto distinto al esperado")
	assert.Equal(t, esperado, desdeInventario, "el inventario no coincide con el sync")
	assert.Equal(t, esperado, desdeByIDs, "by-ids no coincide con el sync")
}

// TestE2E_ParidadCanales_Ventas es la misma exigencia para el recurso
// ventas/saldos: /sync/ventas, el inventario de saldos (/ids) y
// /sync/saldos/by-ids —que responde por VentasRepo.ByIDs— tienen que decir lo
// mismo. Cubre además la restitución de la rama de saldadas en
// queryVentaSyncPage y el predicado equivalente en saldoDigestSaldoFilter.
//
//nolint:paralleltest // serial: fixtures committed en la BD compartida.
func TestE2E_ParidadCanales_Ventas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	f := buildFixtureVentana(t, pool)
	ctx := context.Background()

	ventas := cobranzaventfb.NewVentasRepo(pool)
	saldos := cobranzaventfb.NewSaldosRepo(pool)
	after := minimo(f.cargosDelFixture()) - 1

	page, err := ventas.SyncPorZona(ctx, f.zonaID, time.Time{}, after, 5000, f.desde)
	require.NoError(t, err)
	desdeSync := idsDeVentas(page.Items, f.cargosDelFixture())

	ids, _, err := saldos.ListIDs(ctx, f.zonaID, after, 5000, f.desde)
	require.NoError(t, err)
	desdeInventario := idsDeLista(ids, f.cargosDelFixture())

	filas, err := ventas.ByIDs(ctx, f.zonaID, f.cargosDelFixture(), f.desde)
	require.NoError(t, err)
	desdeByIDs := idsDeVentas(filas, f.cargosDelFixture())

	esperado := f.cargosEsperados()
	assert.Equal(t, esperado, desdeSync,
		"sync de ventas: la saldada dentro de la ventana debe estar y la de fuera no")
	assert.Equal(t, esperado, desdeInventario, "el inventario de saldos no coincide con el sync")
	assert.Equal(t, esperado, desdeByIDs, "by-ids de saldos no coincide con el sync")
}
