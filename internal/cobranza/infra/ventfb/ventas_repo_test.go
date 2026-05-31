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

// seedZonedCliente returns a CLIENTE_ID + ZONA_CLIENTE_ID where the cliente
// has a non-null zona, suitable for sync-by-zona tests. Skips the test when
// no such cliente exists. Prefers the well-known test cliente 11486 when it
// happens to be zoned; otherwise picks the first zoned cliente by PK.
func seedZonedCliente(t *testing.T, q firebird.Querier) (int, int) {
	t.Helper()
	const preferredID = 11486
	var (
		preferredZona *int
		clienteID     int
		zonaID        int
	)
	err := q.QueryRowContext(context.Background(),
		`SELECT ZONA_CLIENTE_ID FROM CLIENTES WHERE CLIENTE_ID = ?`, preferredID).Scan(&preferredZona)
	if err == nil && preferredZona != nil {
		return preferredID, *preferredZona
	}
	err = q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 CLIENTE_ID, ZONA_CLIENTE_ID FROM CLIENTES
		 WHERE ZONA_CLIENTE_ID IS NOT NULL ORDER BY CLIENTE_ID`).Scan(&clienteID, &zonaID)
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

		// The sync query excludes rows within a 5-second lag window so
		// in-flight commits don't disappear between queries. Wait it out.
		time.Sleep(6 * time.Second)

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
// nuevo parámetro `desde` en sync inicial (cursor zero):
//
//  1. cursor=zero, desde=zero → venta saldada NO viaja (legacy admin).
//  2. cursor=zero, desde<fechaUltPago → venta saldada SÍ viaja.
//  3. cursor!=zero → venta saldada SÍ viaja (filtro de saldo removido en
//     incremental para propagar el pago final).
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
		cargoID := insertCargoDoctosCC(t, q, clienteID, "VENT-SLD-1", importe)
		// Pago completo: SALDO queda en 0, FECHA_ULT_PAGO ≈ now.
		insertPagoImporte(t, q, cargoID, importe)

		saldoRepo := cobranzaventfb.NewSaldosRepo(pool)
		saldo, err := saldoRepo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		require.True(t, saldo.Saldo().IsZero(),
			"prerequisito: saldo debe quedar en 0 tras el pago completo; got=%s", saldo.Saldo())

		// Wait out the sync lag window so the row is visible.
		time.Sleep(6 * time.Second)

		repo := cobranzaventfb.NewVentasRepo(pool)

		// Caso 1: sync inicial legacy (sin desde) — saldada no debe aparecer.
		pageLegacy, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, findVenta(pageLegacy.Items, cargoID),
			"sin desde, la venta saldada no debería aparecer en sync inicial")

		// Caso 2: sync inicial con desde anterior al pago — saldada SÍ viaja.
		desde := time.Now().Add(-24 * time.Hour)
		pageConDesde, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)
		v := findVenta(pageConDesde.Items, cargoID)
		require.NotNil(t, v, "con desde<fechaUltPago, la venta saldada debe aparecer")
		assert.True(t, v.Saldo().IsZero(), "venta debe traer saldo=0")

		// Caso 3: sync incremental (cursor antes del UPDATED_AT del saldo) —
		// el filtro de saldo está removido y la venta saldada debe propagarse.
		cursor := saldo.UpdatedAt().Add(-time.Second)
		pageIncr, err := repo.SyncPorZona(ctx, zonaID, cursor, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.NotNil(t, findVenta(pageIncr.Items, cargoID),
			"en incremental, la venta recién saldada debe viajar al cliente")
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

		// Wait out the sync lag window so the tombstone is visible.
		time.Sleep(6 * time.Second)

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
