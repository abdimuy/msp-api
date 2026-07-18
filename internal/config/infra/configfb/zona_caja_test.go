//nolint:paralleltest // serial: shares rollback-only tx.
package configfb_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/config/infra/configfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// existingZonaClienteID discovers a real ZONAS_CLIENTES.ZONA_CLIENTE_ID to
// exercise the upsert round-trip without depending on a fixed dev-DB id.
func existingZonaClienteID(ctx context.Context, t *testing.T, q firebird.Querier) int {
	t.Helper()
	var id int
	err := q.QueryRowContext(ctx, `SELECT FIRST 1 ZONA_CLIENTE_ID FROM ZONAS_CLIENTES`).Scan(&id)
	require.NoError(t, err, "dev DB must have at least one ZONAS_CLIENTES row")
	return id
}

func findZonaCajaConfig(configs []configdomain.ZonaCajaConfig, zonaClienteID int) *configdomain.ZonaCajaConfig {
	for i := range configs {
		if configs[i].ZonaClienteID == zonaClienteID {
			return &configs[i]
		}
	}
	return nil
}

func TestConfigRepo_UpsertZonaCajaConfig_InsertsThenUpdates(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zonaClienteID := existingZonaClienteID(ctx, t, q)
		repo := configfb.NewConfigRepo(pool)

		cfg, err := configdomain.NewZonaCajaConfig(zonaClienteID, 111, 222, 333, 444)
		require.NoError(t, err)

		// First call: INSERT path (the rolled-back tx sees the row as new even
		// though a config for this zona may already exist in the shared dev DB
		// outside the tx — MSP_CFG_ZONA_CAJA has ZONA_CLIENTE_ID as PK, so the
		// UPDATE-first path always fires when a row exists; either path must
		// round-trip correctly).
		require.NoError(t, repo.UpsertZonaCajaConfig(ctx, cfg))

		got, err := repo.ListarZonaCajaConfigs(ctx)
		require.NoError(t, err)
		found := findZonaCajaConfig(got, zonaClienteID)
		require.NotNil(t, found)
		assert.Equal(t, 111, found.CajaID)
		assert.Equal(t, 222, found.CajeroID)
		assert.Equal(t, 333, found.VendedorID)
		assert.Equal(t, 444, found.CobradorID)

		// Second call with a -1 slot: UPDATE path.
		updated, err := configdomain.NewZonaCajaConfig(zonaClienteID, 999, 222, -1, 444)
		require.NoError(t, err)
		require.NoError(t, repo.UpsertZonaCajaConfig(ctx, updated))

		got, err = repo.ListarZonaCajaConfigs(ctx)
		require.NoError(t, err)
		found = findZonaCajaConfig(got, zonaClienteID)
		require.NotNil(t, found)
		assert.Equal(t, 999, found.CajaID)
		assert.Equal(t, 222, found.CajeroID)
		assert.Equal(t, -1, found.VendedorID, "sentinel must round-trip as -1, not NULL")
		assert.Equal(t, 444, found.CobradorID)
	})
}

func TestConfigRepo_CatalogListsReturnRows(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := configfb.NewConfigRepo(pool)

		zonas, err := repo.ListarZonas(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, zonas, "dev DB must have ZONAS_CLIENTES rows")

		cajas, err := repo.ListarCajas(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cajas, "dev DB must have CAJAS rows")

		cajeros, err := repo.ListarCajeros(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cajeros, "dev DB must have CAJEROS rows")
		t.Logf("first cajero resolved: id=%d nombre=%q", cajeros[0].ID, cajeros[0].Nombre)

		vendedores, err := repo.ListarVendedoresCatalogo(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, vendedores, "dev DB must have VENDEDORES rows")

		cobradores, err := repo.ListarCobradores(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cobradores, "dev DB must have COBRADORES rows")
	})
}

func TestConfigRepo_ExisteEnCatalogo_TrueYFalse(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := configfb.NewConfigRepo(pool)

		zonas, err := repo.ListarZonas(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, zonas)

		ok, err := repo.ZonaExiste(ctx, zonas[0].ID)
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = repo.ZonaExiste(ctx, -999999)
		require.NoError(t, err)
		assert.False(t, ok)

		cajas, err := repo.ListarCajas(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cajas)
		ok, err = repo.CajaExiste(ctx, cajas[0].ID)
		require.NoError(t, err)
		assert.True(t, ok)

		cajeros, err := repo.ListarCajeros(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cajeros)
		ok, err = repo.CajeroExiste(ctx, cajeros[0].ID)
		require.NoError(t, err)
		assert.True(t, ok)

		vendedores, err := repo.ListarVendedoresCatalogo(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, vendedores)
		ok, err = repo.VendedorExiste(ctx, vendedores[0].ID)
		require.NoError(t, err)
		assert.True(t, ok)

		cobradores, err := repo.ListarCobradores(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, cobradores)
		ok, err = repo.CobradorExiste(ctx, cobradores[0].ID)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}
