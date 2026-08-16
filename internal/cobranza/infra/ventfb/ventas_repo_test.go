//nolint:misspell // Spanish vocabulary by project convention.
package ventfb_test

import (
	"context"
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

// findVenta scans page items for the given cargo ID. Returns nil when not
// present in the page.
func findVenta(items []domain.Venta, doctoCCID int) *domain.Venta {
	for i := range items {
		if items[i].DoctoCCID() == doctoCCID {
			return &items[i]
		}
	}
	return nil
}

// seedZonedCliente returns a CLIENTE_ID + ZONA_CLIENTE_ID usable by the
// sync-by-zona tests. Skips the test when no such cliente exists.
//
// El cliente tiene que cumplir las MISMAS condiciones que el sync exige para
// mostrarlo en la ruta (ver ventaClienteFilter): zona no nula, ESTATUS 'A' y
// domicilio principal. Si no, la prueba se monta sobre un cliente que el
// backend filtra a proposito y todo falla por el fixture, no por el codigo.
//
// Paso justo eso: el helper tomaba `FIRST 1 ... ORDER BY CLIENTE_ID`, que en
// la base de desarrollo devuelve al cliente 11511 — ESTATUS 'B', o sea uno
// que oficina dio de baja. Los tres tests de tombstone rompieron por eso.
func seedZonedCliente(t *testing.T, q firebird.Querier) (int, int) {
	t.Helper()
	const preferredID = 11486
	var (
		preferredZona *int
		clienteID     int
		zonaID        int
	)
	err := q.QueryRowContext(context.Background(),
		`SELECT ZONA_CLIENTE_ID FROM CLIENTES c
		 WHERE c.CLIENTE_ID = ?
		   AND c.ESTATUS = 'A'
		   AND EXISTS (SELECT 1 FROM DIRS_CLIENTES d
		               WHERE d.CLIENTE_ID = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S')`,
		preferredID).Scan(&preferredZona)
	if err == nil && preferredZona != nil {
		return preferredID, *preferredZona
	}
	err = q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 c.CLIENTE_ID, c.ZONA_CLIENTE_ID FROM CLIENTES c
		 WHERE c.ZONA_CLIENTE_ID IS NOT NULL
		   AND c.ESTATUS = 'A'
		   AND EXISTS (SELECT 1 FROM DIRS_CLIENTES d
		               WHERE d.CLIENTE_ID = c.CLIENTE_ID AND d.ES_DIR_PPAL = 'S')
		 ORDER BY c.CLIENTE_ID`).Scan(&clienteID, &zonaID)
	if err != nil {
		t.Skipf("no zoned cliente available: %v", err)
	}
	return clienteID, zonaID
}

// TestE2E_VentasRepo_SyncPorZona_ReturnsEnrichedRow inserts a fresh cargo for
// a known cliente in a known zona and verifies the SyncPorZona JOIN query
// returns the row hydrated with cliente / zona / cobrador / dirección /
// contrato fields.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_SyncPorZona_ReturnsEnrichedRow(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("4200.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "VENT-001", importe)

		repo := cobranzaventfb.NewVentasRepo(pool)

		// Wait out the clock-skew margin (syncClockSkewSeconds = 1 s).
		// 2 s is sufficient; the old 5 s wait covered the prior syncLagSeconds window.
		time.Sleep(2 * time.Second)

		// Read the saldo to learn the cargo's UPDATED_AT; use it minus 1s
		// as cursor so the page contains the new cargo without paginating
		// through the entire zone.
		saldoRepo := cobranzaventfb.NewSaldosRepo(pool)
		saldo, err := saldoRepo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		cursor := saldo.UpdatedAt().Add(-time.Second)

		page, err := repo.SyncPorZona(ctx, zonaID, cursor, 0, 5000, time.Time{})
		require.NoError(t, err)

		v := findVenta(page.Items, cargoID)
		require.NotNil(t, v, "expected cargo %d in sync page for zona %d", cargoID, zonaID)

		assert.Equal(t, cargoID, v.DoctoCCID())
		assert.Equal(t, clienteID, v.ClienteID())
		require.NotNil(t, v.ZonaClienteID())
		assert.Equal(t, zonaID, *v.ZonaClienteID())
		assert.True(t, importe.Equal(v.PrecioTotal()), "PrecioTotal mismatch")
		assert.False(t, v.CargoCancelado())

		assert.NotEmpty(t, v.ClienteNombre(), "ClienteNombre debería venir hidratado desde CLIENTES")

		t.Logf("cargo %d enriquecido: cliente=%q zona=%d cobrador=%q",
			cargoID, v.ClienteNombre(), zonaID, v.NombreCobrador())
	})
}

// TestE2E_VentasRepo_SyncPorZona_SaldadaConDesde verifica el contrato del
// parámetro `desde` (el filtro depende SOLO de desde, no del cursor):
//
// Contrato vigente: la venta que se saldó DENTRO de la ventana sigue
// viajando. Es el pago que la saldó el que la deja en SALDO = 0, y si la
// venta desaparece ese mismo día el cobrador no ve el abono, cree que no se
// registró y vuelve a cobrar. Sin `desde` el filtro es SALDO > 0 estricto,
// pero por HTTP `desde` nunca falta: app.ResolveSyncDesde le pone el default
// de servidor.
//
//  1. cursor=zero, desde=zero  → saldada NO viaja (sin ventana).
//  2. cursor=zero, desde<FUP   → saldada SÍ viaja.
//  3. cursor!=zero, desde<FUP  → saldada SÍ viaja al paginar.
//  4. cursor!=zero, desde=zero → saldada NO viaja.
//  5. cursor=zero, desde>FUP   → saldada FUERA de la ventana NO viaja.
//  6. cursor=zero, desde<FUP, venta saldada con CONCEPTO_CC_ID=155:
//     viaja igual. FECHA_ULT_PAGO cuenta 87327 y 155 (000011/000023),
//     así que una venta saldada solo por ese concepto entra por la
//     ventana aunque su abono no aparezca en /sync/pagos, que filtra
//     (87327, 27969). Es una asimetría conocida de los catálogos, no
//     del predicado: preferimos que el cobrador vea la venta saldada
//     sin uno de sus abonos a que la venta desaparezca de su ruta.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_SyncPorZona_SaldadaConDesde(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("1500.00")
		// FOLIO en DOCTOS_CC es CHAR(9); "VENT-SLD" cabe exacto.
		cargoID := insertCargoDoctosCC(t, q, clienteID, "VENT-SLD", importe)
		// Pago completo: SALDO queda en 0, FECHA_ULT_PAGO ≈ now.
		insertPagoImporte(t, q, cargoID, importe)

		saldoRepo := cobranzaventfb.NewSaldosRepo(pool)
		saldo, err := saldoRepo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		require.True(t, saldo.Saldo().IsZero(),
			"prerequisito: saldo debe quedar en 0 tras el pago completo; got=%s", saldo.Saldo())

		// Wait out the clock-skew margin (syncClockSkewSeconds = 1 s).
		time.Sleep(2 * time.Second)

		repo := cobranzaventfb.NewVentasRepo(pool)
		cursor := saldo.UpdatedAt().Add(-time.Second)
		desde := time.Now().Add(-24 * time.Hour)

		// Caso 1: sync inicial legacy (sin desde) — saldada no debe aparecer.
		pageLegacy, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, findVenta(pageLegacy.Items, cargoID),
			"sin desde, la venta saldada no debería aparecer en sync inicial")

		// Caso 2: con `desde`, la saldada dentro de la ventana SÍ viaja.
		// Es la rama que el defecto D2 había retirado: sin ella el pago que
		// salda la venta se borra a sí mismo del sync el día que se cobra.
		pageConDesde, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)
		assert.NotNil(t, findVenta(pageConDesde.Items, cargoID),
			"con desde, la venta saldada dentro de la ventana debe viajar")

		// Caso 3: lo mismo al paginar con cursor.
		pageIncrConDesde, err := repo.SyncPorZona(ctx, zonaID, cursor, 0, 5000, desde)
		require.NoError(t, err)
		assert.NotNil(t, findVenta(pageIncrConDesde.Items, cargoID),
			"con desde, la saldada debe seguir viajando al paginar")

		// Caso 5: ventana que empieza DESPUÉS del último pago — la saldada
		// queda fuera y no viaja. Sin esta mitad, un `OR TRUE` accidental
		// pasaría el caso 2 sin que nadie se entere.
		desdeFuturo := time.Now().Add(24 * time.Hour)
		pageFuera, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desdeFuturo)
		require.NoError(t, err)
		assert.Nil(t, findVenta(pageFuera.Items, cargoID),
			"con la ventana empezando mañana, la saldada de hoy no debe viajar")

		// Caso 4: paginación legacy sin desde — saldada NO viaja
		// (protege que las saldadas históricas no se cuelen en páginas 2+).
		pageIncrLegacy, err := repo.SyncPorZona(ctx, zonaID, cursor, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, findVenta(pageIncrLegacy.Items, cargoID),
			"sin desde, las saldadas no deben colarse en paginación")

		// Caso 6: venta saldada por un abono de CONCEPTO_CC_ID=155.
		// FECHA_ULT_PAGO cuenta 87327 y 155, así que entra por la ventana
		// aunque su abono no salga en /sync/pagos (filtro 87327, 27969).
		// Asimetría conocida entre los dos catálogos de conceptos; se deja
		// documentada aquí en vez de resolverla con un EXISTS carísimo
		// (7.5 s / 1.3M lecturas medidos contra 0.285 s del predicado).
		importeAdm := decimal.RequireFromString("750.00")
		cargoAdm := insertCargoDoctosCC(t, q, clienteID, "VENT-ADM", importeAdm)
		insertPagoNoEnRutaImporte(t, q, clienteID, cargoAdm, importeAdm)

		saldoAdm, err := saldoRepo.PorCargo(ctx, cargoAdm)
		require.NoError(t, err)
		require.True(t, saldoAdm.Saldo().IsZero(),
			"prerequisito caso 6: saldo debe ser 0 tras el abono 155; got=%s", saldoAdm.Saldo())
		require.NotNil(t, saldoAdm.FechaUltPago(),
			"prerequisito caso 6: FECHA_ULT_PAGO debe quedar set (el concepto 155 cuenta para FUP)")
		require.True(t, saldoAdm.FechaUltPago().After(desde),
			"prerequisito caso 6: FECHA_ULT_PAGO debe caer dentro de la ventana `desde`")

		// Esperar de nuevo el margen de clock-skew: cargoAdm se acaba de
		// escribir y su UPDATED_AT quedaría por encima de la cota superior
		// (server_now - 1 s). Sin esta espera el caso pasaría por el motivo
		// equivocado — la fila queda fuera de la página por tiempo, no por el
		// predicado.
		time.Sleep(2 * time.Second)

		pageAdm, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)
		assert.NotNil(t, findVenta(pageAdm.Items, cargoAdm),
			"la saldada por concepto 155 viaja: FECHA_ULT_PAGO la cuenta y la ventana manda")
	})
}

// TestE2E_VentasRepo_SyncPorZona_Tombstone inserta un cargo, lo cancela y
// verifica que el sync devuelve cargo_cancelado=true para propagar la
// cancelación al móvil.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_SyncPorZona_Tombstone(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		cargoID := insertCargoDoctosCC(t, q, clienteID, "VENT-002",
			decimal.RequireFromString("1000.00"))

		_, err := q.ExecContext(ctx,
			`UPDATE DOCTOS_CC SET CANCELADO = 'S' WHERE DOCTO_CC_ID = ?`, cargoID)
		require.NoError(t, err)

		// Wait out the clock-skew margin (syncClockSkewSeconds = 1 s).
		time.Sleep(2 * time.Second)

		saldoRepo := cobranzaventfb.NewSaldosRepo(pool)
		saldo, err := saldoRepo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		cursor := saldo.UpdatedAt().Add(-time.Second)

		repo := cobranzaventfb.NewVentasRepo(pool)
		page, err := repo.SyncPorZona(ctx, zonaID, cursor, 0, 5000, time.Time{})
		require.NoError(t, err)

		v := findVenta(page.Items, cargoID)
		require.NotNil(t, v, "tombstone debe seguir en el page para que el cliente lo propague")
		assert.True(t, v.CargoCancelado(), "cargo_cancelado debe ser true")
	})
}

// TestE2E_VentasRepo_SyncPorZona_TombstonePorFechaCancelacion verifica que,
// con `desde` set, los tombstones se filtran por la fecha real de
// cancelación en Microsip (DOCTOS_CC.FECHA_HORA_CANCELACION), no por el
// UPDATED_AT del cache (que se mueve cada vez que un backfill toca la fila).
//
// Sin este filtro, cualquier migración que recompute MSP_SALDOS_VENTAS
// resucita tombstones de cancelaciones de 2018-2025 cada vez que el
// cobrador hace pm clear — ruido sin valor porque el cliente borra en
// no-op silencioso.
//
//  1. Tombstone con FECHA_HORA_CANCELACION ANTES de `desde` → NO viaja.
//  2. Tombstone con FECHA_HORA_CANCELACION DENTRO de `desde` → SÍ viaja.
//  3. Tombstone con FECHA_HORA_CANCELACION NULL → SÍ viaja (defensivo:
//     mejor un delete espurio que perder una cancelación legítima).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_VentasRepo_SyncPorZona_TombstonePorFechaCancelacion(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)
		desde := time.Now().Add(-7 * 24 * time.Hour)
		fechaVieja := desde.Add(-30 * 24 * time.Hour)
		fechaReciente := desde.Add(24 * time.Hour)

		cargoViejo := insertCargoDoctosCC(t, q, clienteID, "TOMB-OLD", decimal.RequireFromString("100.00"))
		cargoReciente := insertCargoDoctosCC(t, q, clienteID, "TOMB-NEW", decimal.RequireFromString("100.00"))
		cargoNull := insertCargoDoctosCC(t, q, clienteID, "TOMB-NUL", decimal.RequireFromString("100.00"))

		// El trigger BEFORE UPDATE de Microsip (DOCTOS_CC_BEFUPD_0) setea
		// NEW.FECHA_HORA_CANCELACION = 'NOW' cuando CANCELADO cambia, así
		// que no podemos pasar la fecha que queremos en el mismo UPDATE.
		// Workaround: dos UPDATEs — primero CANCELADO (dispara el trigger
		// con 'NOW'), después FECHA_HORA_CANCELACION (sin tocar CANCELADO,
		// trigger no se mete).
		cancelarConFecha := func(t *testing.T, cargoID int, fecha *time.Time) {
			t.Helper()
			_, err := q.ExecContext(ctx,
				`UPDATE DOCTOS_CC SET CANCELADO = 'S' WHERE DOCTO_CC_ID = ?`, cargoID)
			require.NoError(t, err, "fase 1: SET CANCELADO=S")

			if fecha == nil {
				_, err = q.ExecContext(ctx,
					`UPDATE DOCTOS_CC SET FECHA_HORA_CANCELACION = NULL WHERE DOCTO_CC_ID = ?`,
					cargoID)
			} else {
				_, err = q.ExecContext(ctx,
					`UPDATE DOCTOS_CC SET FECHA_HORA_CANCELACION = ? WHERE DOCTO_CC_ID = ?`,
					firebird.ToWallClock(*fecha), cargoID)
			}
			require.NoError(t, err, "fase 2: SET FECHA_HORA_CANCELACION")
		}

		cancelarConFecha(t, cargoViejo, &fechaVieja)
		cancelarConFecha(t, cargoReciente, &fechaReciente)
		cancelarConFecha(t, cargoNull, nil)

		// Wait out the clock-skew margin (syncClockSkewSeconds = 1 s).
		time.Sleep(2 * time.Second)

		repo := cobranzaventfb.NewVentasRepo(pool)

		// Con desde: el viejo NO debe viajar; el reciente Y el NULL sí.
		page, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)

		assert.Nil(t, findVenta(page.Items, cargoViejo),
			"tombstone con cancelación anterior a `desde` NO debe propagarse")

		vReciente := findVenta(page.Items, cargoReciente)
		require.NotNil(t, vReciente,
			"tombstone con cancelación dentro de `desde` debe propagarse")
		assert.True(t, vReciente.CargoCancelado())

		vNull := findVenta(page.Items, cargoNull)
		require.NotNil(t, vNull,
			"tombstone con FECHA_HORA_CANCELACION NULL debe propagarse (defensivo)")
		assert.True(t, vNull.CargoCancelado())

		// Sin desde (sync legacy admin): los tres tombstones viajan, sin
		// filtrar por fecha — preserva el comportamiento histórico para
		// flujos sin ventana del cobrador.
		pageLegacy, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.NotNil(t, findVenta(pageLegacy.Items, cargoViejo),
			"sin desde, el tombstone viejo también debe propagarse (rama legacy)")
		assert.NotNil(t, findVenta(pageLegacy.Items, cargoReciente))
		assert.NotNil(t, findVenta(pageLegacy.Items, cargoNull))
	})
}
