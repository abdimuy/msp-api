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

// findPagoByCargo busca un pago acreditado al cargo dado dentro de la página.
func findPagoByCargo(items []domain.Pago, doctoCCAcrID int) *domain.Pago {
	for i := range items {
		if items[i].DoctoCCAcrID() == doctoCCAcrID {
			return &items[i]
		}
	}
	return nil
}

// TestE2E_PagosRepo_SyncPorZona_SaldadaConDesde cubre las tres ramas del
// filtro de saldo en /sync/pagos:
//
//  1. cursor=zero, desde=zero → pago de venta saldada NO viaja (legacy).
//  2. cursor=zero, desde<pago.fecha → pago SÍ viaja aunque la venta esté
//     saldada (extiende ventana del cobrador).
//  3. cursor!=zero → pago SÍ viaja sin importar saldo (incremental propaga
//     el pago final que terminó saldando la venta).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRepo_SyncPorZona_SaldadaConDesde(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		requireMigration000010(t, q)

		clienteID, zonaID := seedZonedCliente(t, q)

		importe := decimal.RequireFromString("2300.00")
		cargoID := insertCargoDoctosCC(t, q, clienteID, "PAG-SLD-1", importe)
		// Pago completo: SALDO=0, deja un row en MSP_PAGOS_VENTAS por trigger.
		insertPagoImporte(t, q, cargoID, importe)

		saldoRepo := cobranzaventfb.NewSaldosRepo(pool)
		saldo, err := saldoRepo.PorCargo(ctx, cargoID)
		require.NoError(t, err)
		require.True(t, saldo.Saldo().IsZero(),
			"prerequisito: saldo debe quedar en 0 tras el pago completo; got=%s", saldo.Saldo())

		// Wait out the sync lag window.
		time.Sleep(6 * time.Second)

		repo := cobranzaventfb.NewPagosRepo(pool)

		// Caso 1: sync inicial sin desde — el pago de la venta saldada NO viaja.
		pageLegacy, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.Nil(t, findPagoByCargo(pageLegacy.Items, cargoID),
			"sin desde, los pagos de ventas saldadas no deben aparecer en sync inicial")

		// Caso 2: sync inicial con desde anterior al pago — el pago SÍ viaja.
		desde := time.Now().Add(-24 * time.Hour)
		pageConDesde, err := repo.SyncPorZona(ctx, zonaID, time.Time{}, 0, 5000, desde)
		require.NoError(t, err)
		p := findPagoByCargo(pageConDesde.Items, cargoID)
		require.NotNil(t, p, "con desde<pago.fecha, el pago debe aparecer aunque la venta esté saldada")
		assert.True(t, importe.Equal(p.Importe()),
			"importe del pago debe coincidir; want=%s got=%s", importe, p.Importe())

		// Caso 3: sync incremental sin desde — el filtro de saldo se quita
		// y el pago debe propagarse aunque la venta haya quedado saldada.
		cursor := p.UpdatedAt().Add(-time.Second)
		pageIncr, err := repo.SyncPorZona(ctx, zonaID, cursor, 0, 5000, time.Time{})
		require.NoError(t, err)
		assert.NotNil(t, findPagoByCargo(pageIncr.Items, cargoID),
			"en incremental, el pago final que saldó la venta debe viajar")
	})
}
