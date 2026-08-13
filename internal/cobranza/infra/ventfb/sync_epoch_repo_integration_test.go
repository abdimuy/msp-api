//nolint:misspell // Spanish vocabulary (recurso, zona, ventas, pagos, epoch) by project convention.
package ventfb_test

// Integration tests for sync_epoch_repo.go against the real Firebird dev DB
// (migration 000055 must be applied).
//
// ISOLATION
// =========
// Every test runs inside fbtestutil.WithTestTransaction, so all its
// INSERT/UPDATE/DELETE on MSP_CFG_SYNC_EPOCH roll back. firebird.RunInReadTx
// is re-entrant (it reuses the tx injected in the context), so the repository
// under test reads the same uncommitted state the test just wrote.
//
// The tests never assume the seeded rows hold any particular EPOCH: each one
// sets the values it needs first.

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	cobranzaoutbound "github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Zonas ficticias usadas por estas pruebas. No necesitan existir en
// ZONAS_CLIENTES: MSP_CFG_SYNC_EPOCH no tiene FK hacia allá.
const (
	epochZonaA = 912271
	epochZonaB = 912272
)

// requireMigration000055 skips the test when MSP_CFG_SYNC_EPOCH does not
// exist (migration 000055 not applied).
func requireMigration000055(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(context.Background(), `
SELECT COUNT(*) FROM RDB$RELATIONS WHERE RDB$RELATION_NAME = 'MSP_CFG_SYNC_EPOCH'`).Scan(&n)
	require.NoError(t, err, "consultar RDB$RELATIONS")
	if n == 0 {
		t.Skip("MSP_CFG_SYNC_EPOCH no existe; aplica la migracion 000055")
	}
}

// epochFixture writes MSP_CFG_SYNC_EPOCH rows inside the test transaction.
// It is a struct rather than plain functions so `ctx` can stay the first
// parameter (revive's context-as-argument) while `t` remains available for
// require assertions.
type epochFixture struct {
	t    *testing.T
	pool *firebird.Pool
}

// newEpochFixture builds a fixture and skips the test when migration 000055
// has not been applied.
func newEpochFixture(t *testing.T, ctx context.Context, pool *firebird.Pool) epochFixture { //nolint:revive // t must stay first for thelper
	t.Helper()
	requireMigration000055(t, firebird.GetQuerier(ctx, pool.DB))
	return epochFixture{t: t, pool: pool}
}

// set upserts one MSP_CFG_SYNC_EPOCH row. UPDATE-then-INSERT instead of
// MERGE: nakagami/firebirdsql mishandles parameters in MERGE (see
// reference_firebirdsql_merge_param_bug).
func (f epochFixture) set(ctx context.Context, recurso domain.RecursoSync, zonaID, epoch int) {
	f.t.Helper()
	q := firebird.GetQuerier(ctx, f.pool.DB)
	now := firebird.ToWallClock(time.Now().UTC())

	res, err := q.ExecContext(ctx, `
UPDATE MSP_CFG_SYNC_EPOCH SET EPOCH = ?, MOTIVO = ?, UPDATED_AT = ?
WHERE RECURSO = ? AND ZONA_CLIENTE_ID = ?`,
		epoch, "prueba de integracion", now, recurso.String(), zonaID)
	require.NoError(f.t, err, "UPDATE MSP_CFG_SYNC_EPOCH")

	affected, err := res.RowsAffected()
	require.NoError(f.t, err)
	if affected > 0 {
		return
	}

	_, err = q.ExecContext(ctx, `
INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT)
VALUES (?, ?, ?, ?, ?)`,
		recurso.String(), zonaID, epoch, "prueba de integracion", now)
	require.NoError(f.t, err, "INSERT MSP_CFG_SYNC_EPOCH")
}

// deleteAll removes every row of a recurso inside the test transaction.
func (f epochFixture) deleteAll(ctx context.Context, recurso domain.RecursoSync) {
	f.t.Helper()
	q := firebird.GetQuerier(ctx, f.pool.DB)
	_, err := q.ExecContext(ctx, `DELETE FROM MSP_CFG_SYNC_EPOCH WHERE RECURSO = ?`, recurso.String())
	require.NoError(f.t, err, "DELETE MSP_CFG_SYNC_EPOCH")
}

// ─── Cálculo del epoch contra datos reales ───────────────────────────────────

// TestSyncEpochRepo_Efectivo_SinFilasDaCero es el requisito de seguridad: si
// la tabla no tiene filas para el recurso, el epoch efectivo es 0 y no hay
// error. El sync tiene que seguir funcionando igual.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Efectivo_SinFilasDaCero(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

		epoch, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err, "la ausencia de filas no es un error")
		assert.Equal(t, 0, epoch)
	})
}

// TestSyncEpochRepo_Efectivo_SoloGlobal: con solo la fila global, el epoch
// efectivo de cualquier zona es el del global.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Efectivo_SoloGlobal(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 3)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

		epochA, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		assert.Equal(t, 3, epochA)

		epochB, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaB)
		require.NoError(t, err)
		assert.Equal(t, 3, epochB, "el global aplica a toda zona sin fila propia")
	})
}

// TestSyncEpochRepo_Efectivo_GlobalMasZonaSeSuman: con ambas filas presentes,
// el epoch efectivo es la suma.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Efectivo_GlobalMasZonaSeSuman(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 3)
		fx.set(ctx, domain.RecursoSyncVentas, epochZonaA, 5)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

		epoch, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		assert.Equal(t, 8, epoch)
	})
}

// TestSyncEpochRepo_Efectivo_UpdateGlobalMueveTodasLasZonas: un UPDATE de la
// fila global sube el epoch de TODAS las zonas — incluso una que ya traía
// bump propio.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Efectivo_UpdateGlobalMueveTodasLasZonas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 1)
		fx.set(ctx, domain.RecursoSyncVentas, epochZonaA, 4) // zona con bump propio

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

		antesA, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		antesB, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaB)
		require.NoError(t, err)
		require.Equal(t, 5, antesA)
		require.Equal(t, 1, antesB)

		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 2) // bump global

		despuesA, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		despuesB, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaB)
		require.NoError(t, err)

		assert.Equal(t, 6, despuesA)
		assert.Equal(t, 2, despuesB)
		assert.Greater(t, despuesA, antesA, "el bump global debe mover la zona con bump propio")
		assert.Greater(t, despuesB, antesB, "el bump global debe mover la zona sin fila propia")
	})
}

// TestSyncEpochRepo_Efectivo_UpdatePorZonaMueveSoloEsaZona es la contraparte
// del anterior y la razón de ser de la fila por zona.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Efectivo_UpdatePorZonaMueveSoloEsaZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 1)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

		antesA, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		antesB, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaB)
		require.NoError(t, err)
		require.Equal(t, 1, antesA)
		require.Equal(t, 1, antesB)

		fx.set(ctx, domain.RecursoSyncVentas, epochZonaA, 9) // bump SOLO de la zona A

		despuesA, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		despuesB, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaB)
		require.NoError(t, err)

		assert.Equal(t, 10, despuesA, "zona A = global 1 + zona 9")
		assert.Equal(t, antesB, despuesB, "la zona B no se debe mover")
	})
}

// TestSyncEpochRepo_Efectivo_RecursosAislados: mover 'pagos' no puede afectar
// a 'ventas'. Son dos streams de sync independientes.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Efectivo_RecursosAislados(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.deleteAll(ctx, domain.RecursoSyncPagos)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 2)
		fx.set(ctx, domain.RecursoSyncPagos, domain.ZonaEpochGlobal, 11)
		fx.set(ctx, domain.RecursoSyncPagos, epochZonaA, 6)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

		ventas, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		pagos, err := repo.Efectivo(ctx, domain.RecursoSyncPagos, epochZonaA)
		require.NoError(t, err)

		assert.Equal(t, 2, ventas, "ventas no ve las filas de pagos")
		assert.Equal(t, 17, pagos)
	})
}

// ─── Caché TTL ───────────────────────────────────────────────────────────────

// TestSyncEpochRepo_Cache_SirveValorCacheado documenta el comportamiento del
// TTL: dentro de la ventana, un UPDATE no se ve. Es el precio aceptado para
// no pedir una conexión del pool por cada página de sync.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Cache_SirveValorCacheado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 1)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, time.Minute)

		primera, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		require.Equal(t, 1, primera)

		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 99)

		segunda, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		assert.Equal(t, 1, segunda, "dentro del TTL se sirve el valor cacheado")
	})
}

// TestSyncEpochRepo_Cache_ExpiraYReleeElValorNuevo: pasado el TTL, la
// siguiente llamada vuelve a Firebird.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Cache_ExpiraYReleeElValorNuevo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 1)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 20*time.Millisecond)

		primera, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		require.Equal(t, 1, primera)

		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 99)
		time.Sleep(40 * time.Millisecond)

		segunda, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		assert.Equal(t, 99, segunda, "vencido el TTL se relee de Firebird")
	})
}

// TestSyncEpochRepo_Cache_EntradasIndependientesPorZonaYRecurso: la clave del
// caché es (recurso, zona); cachear una zona no puede contaminar a otra.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpochRepo_Cache_EntradasIndependientesPorZonaYRecurso(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.deleteAll(ctx, domain.RecursoSyncPagos)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 1)
		fx.set(ctx, domain.RecursoSyncVentas, epochZonaA, 4)
		fx.set(ctx, domain.RecursoSyncPagos, domain.ZonaEpochGlobal, 8)

		repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, time.Minute)

		ventasA, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
		ventasB, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaB)
		require.NoError(t, err)
		pagosA, err := repo.Efectivo(ctx, domain.RecursoSyncPagos, epochZonaA)
		require.NoError(t, err)

		assert.Equal(t, 5, ventasA)
		assert.Equal(t, 1, ventasB)
		assert.Equal(t, 8, pagosA)
	})
}

// ─── Latencia ────────────────────────────────────────────────────────────────

// TestSyncEpochRepo_Efectivo_Latencia mide el costo real del lookup, que es
// lo que justifica (o no) el caché. Reporta dos números:
//
//   - por consulta dentro de una tx ya abierta: el costo puro del SELECT.
//   - por llamada sin caché desde el pool: SELECT + BEGIN/COMMIT + adquirir
//     conexión, que es lo que pagaría CADA página de sync sin el caché.
//
// No falla por umbral (la máquina de desarrollo no es un banco de pruebas);
// deja el dato en el log para poder revisar la decisión.
//
//nolint:paralleltest // comparte el pool
func TestSyncEpochRepo_Efectivo_Latencia(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	ctx := context.Background()
	requireMigration000055(t, firebird.GetQuerier(ctx, pool.DB))

	const iteraciones = 50
	repo := cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0)

	inicioPool := time.Now()
	for range iteraciones {
		_, err := repo.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
	}
	porLlamadaPool := time.Since(inicioPool) / iteraciones

	var porLlamadaEnTx time.Duration
	fbtestutil.WithTestTransaction(t, pool, func(txCtx context.Context) {
		inicio := time.Now()
		for range iteraciones {
			_, err := repo.Efectivo(txCtx, domain.RecursoSyncVentas, epochZonaA)
			require.NoError(t, err)
		}
		porLlamadaEnTx = time.Since(inicio) / iteraciones
	})

	cacheado := cobranzaventfb.NewSyncEpochRepo(pool)
	_, err := cacheado.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
	require.NoError(t, err)
	inicioCache := time.Now()
	for range iteraciones {
		_, err := cacheado.Efectivo(ctx, domain.RecursoSyncVentas, epochZonaA)
		require.NoError(t, err)
	}
	porLlamadaCacheada := time.Since(inicioCache) / iteraciones

	t.Logf("epoch lookup: %v/llamada sin cache (pool) | %v/consulta dentro de tx | %v/llamada cacheada",
		porLlamadaPool, porLlamadaEnTx, porLlamadaCacheada)

	assert.Less(t, porLlamadaCacheada, porLlamadaPool,
		"el caché debe ser mas barato que ir a Firebird")
}

// ─── El epoch viaja en la respuesta ──────────────────────────────────────────

// fakeSyncVentasRepo es un cobranzaoutbound.VentasRepo vacío: estas pruebas
// verifican el metadato de la página, no sus items.
type fakeSyncVentasRepo struct{}

func (fakeSyncVentasRepo) SyncPorZona(_ context.Context, _ int, cursor time.Time, _, _ int, _ time.Time) (cobranzaoutbound.SyncPage[domain.Venta], error) {
	return cobranzaoutbound.SyncPage[domain.Venta]{
		Items:        nil,
		MaxUpdatedAt: cursor.UTC(),
		ServerNow:    time.Now().UTC(),
		HasMore:      false,
	}, nil
}

func (fakeSyncVentasRepo) ByIDs(_ context.Context, _ int, _ []int) ([]domain.Venta, error) {
	return nil, nil
}

func (fakeSyncVentasRepo) ProductosByPVIDs(_ context.Context, _ []int) (map[int][]domain.ProductoVenta, error) {
	return map[int][]domain.ProductoVenta{}, nil
}

// fakeSyncPagosRepo es el equivalente para pagos. Solo SyncPorZona se ejerce.
type fakeSyncPagosRepo struct{}

func (fakeSyncPagosRepo) PorVenta(_ context.Context, _ int) ([]domain.Pago, error) { return nil, nil }
func (fakeSyncPagosRepo) PorCliente(_ context.Context, _ int) ([]domain.Pago, error) {
	return nil, nil
}

func (fakeSyncPagosRepo) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Pago, error) {
	return nil, nil
}

func (fakeSyncPagosRepo) SyncPorZona(_ context.Context, _ int, cursor time.Time, _, _ int, _ time.Time) (cobranzaoutbound.SyncPage[domain.Pago], error) {
	return cobranzaoutbound.SyncPage[domain.Pago]{
		Items:        nil,
		MaxUpdatedAt: cursor.UTC(),
		ServerNow:    time.Now().UTC(),
		HasMore:      false,
	}, nil
}

func (fakeSyncPagosRepo) ByIDs(_ context.Context, _ int, _ []int) ([]domain.Pago, error) {
	return nil, nil
}

// epochHandlers arma la cadena real repo → Service → Handlers con el
// SyncEpochRepo de Firebird y repos falsos para ventas/pagos.
func epochHandlers(t *testing.T, pool *firebird.Pool) *cobranzahttp.Handlers {
	t.Helper()
	svc := cobranzaapp.NewService(
		nil, fakeSyncPagosRepo{}, fakeSyncVentasRepo{}, cobranzaoutbound.ProductionClock{},
		nil, nil, nil, nil, nil, nil,
	)
	svc.WithSyncEpochRepo(cobranzaventfb.NewSyncEpochRepoWithTTL(pool, 0), nil)
	return cobranzahttp.NewHandlers(svc, nil, nil)
}

// epochUserCtx planta un principal con los permisos de lectura de cobranza.
func epochUserCtx(ctx context.Context) context.Context {
	return auth.PlantCurrentUser(ctx, auth.CurrentUser{
		ID:       uuid.New(),
		Email:    "gabriel.roque@muebleriamsp.mx",
		Permisos: []string{string(auth.PermCobranzaVerSaldos), string(auth.PermCobranzaVerPagos)},
	})
}

// TestSyncEpoch_ViajaEnLaRespuestaDeVentas recorre la cadena completa: la
// fila de Firebird tiene que aparecer como `sync_epoch` en el JSON de
// /sync/ventas/zona/{id}, y moverse cuando se mueve la fila.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpoch_ViajaEnLaRespuestaDeVentas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 2)
		fx.set(ctx, domain.RecursoSyncVentas, epochZonaA, 5)

		h := epochHandlers(t, pool)
		authCtx := epochUserCtx(ctx)

		out, err := h.SyncVentasPorZona(authCtx, &cobranzahttp.SyncVentasInput{ZonaID: epochZonaA})
		require.NoError(t, err)
		assert.Equal(t, 7, out.Body.SyncEpoch, "global 2 + zona 5")

		raw, err := json.Marshal(out.Body)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"sync_epoch":7`)

		// Un bump global se refleja en la siguiente página.
		fx.set(ctx, domain.RecursoSyncVentas, domain.ZonaEpochGlobal, 3)
		out, err = h.SyncVentasPorZona(authCtx, &cobranzahttp.SyncVentasInput{ZonaID: epochZonaA})
		require.NoError(t, err)
		assert.Equal(t, 8, out.Body.SyncEpoch)

		// Otra zona solo ve el global.
		out, err = h.SyncVentasPorZona(authCtx, &cobranzahttp.SyncVentasInput{ZonaID: epochZonaB})
		require.NoError(t, err)
		assert.Equal(t, 3, out.Body.SyncEpoch)
	})
}

// TestSyncEpoch_ViajaEnLaRespuestaDePagos es el mismo recorrido para
// /sync/pagos/zona/{id}, con su propio recurso.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpoch_ViajaEnLaRespuestaDePagos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncPagos)
		fx.set(ctx, domain.RecursoSyncPagos, domain.ZonaEpochGlobal, 4)
		fx.set(ctx, domain.RecursoSyncPagos, epochZonaB, 6)

		h := epochHandlers(t, pool)
		authCtx := epochUserCtx(ctx)

		out, err := h.SyncPagosPorZona(authCtx, &cobranzahttp.SyncPagosInput{ZonaID: epochZonaB})
		require.NoError(t, err)
		assert.Equal(t, 10, out.Body.SyncEpoch, "global 4 + zona 6")

		raw, err := json.Marshal(out.Body)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"sync_epoch":10`)

		out, err = h.SyncPagosPorZona(authCtx, &cobranzahttp.SyncPagosInput{ZonaID: epochZonaA})
		require.NoError(t, err)
		assert.Equal(t, 4, out.Body.SyncEpoch, "zona sin fila propia solo ve el global")
	})
}

// TestSyncEpoch_SinFilasLaRespuestaSigueSaliendo es el requisito duro de
// disponibilidad: sin filas en MSP_CFG_SYNC_EPOCH el sync responde igual, con
// sync_epoch en 0.
//
//nolint:paralleltest // comparte el pool y escribe en una tx que hace rollback
func TestSyncEpoch_SinFilasLaRespuestaSigueSaliendo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		fx := newEpochFixture(t, ctx, pool)
		fx.deleteAll(ctx, domain.RecursoSyncVentas)
		fx.deleteAll(ctx, domain.RecursoSyncPagos)

		h := epochHandlers(t, pool)
		authCtx := epochUserCtx(ctx)

		outV, err := h.SyncVentasPorZona(authCtx, &cobranzahttp.SyncVentasInput{ZonaID: epochZonaA})
		require.NoError(t, err)
		assert.Equal(t, 0, outV.Body.SyncEpoch)

		outP, err := h.SyncPagosPorZona(authCtx, &cobranzahttp.SyncPagosInput{ZonaID: epochZonaA})
		require.NoError(t, err)
		assert.Equal(t, 0, outP.Body.SyncEpoch)
	})
}
