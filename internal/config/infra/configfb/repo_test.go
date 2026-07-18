// Package configfb_test contains Firebird integration tests for the config
// repo. Tests skip cleanly when FB_DATABASE is not set; every write runs
// inside fbtestutil.WithTestTransaction (rollback-only) so the shared dev DB
// never accumulates state.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/config/infra/configfb/...
//
//nolint:paralleltest // serial: shares rollback-only tx.
package configfb_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/config/infra/configfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireFBEnv skips the test when FB_DATABASE is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

func intPtr(v int) *int { return &v }

// existingUsuarioID discovers a real MSP_USUARIOS.ID to satisfy the
// FK_MSP_CFG_VEND_MSIP_USUARIO constraint. Fails loudly (t.Fatalf) when the
// dev DB has no usuarios at all, so a depleted seed never produces a
// false-green run.
func existingUsuarioID(ctx context.Context, t *testing.T, q firebird.Querier) string {
	t.Helper()
	var id string
	err := q.QueryRowContext(ctx, `SELECT FIRST 1 ID FROM MSP_USUARIOS`).Scan(&id)
	require.NoError(t, err, "dev DB must have at least one MSP_USUARIOS row")
	return id
}

func TestConfigRepo_UpsertVendedorMapping_InsertsThenUpdates(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		usuarioIDStr := existingUsuarioID(ctx, t, q)
		repo := configfb.NewConfigRepo(pool)

		mapping, err := configdomain.NewVendedorMapping(mustParseUUID(t, usuarioIDStr), intPtr(111), intPtr(222), intPtr(333))
		require.NoError(t, err)

		// First call: INSERT path.
		require.NoError(t, repo.UpsertVendedorMapping(ctx, mapping))

		got, err := repo.ListarVendedorMappings(ctx)
		require.NoError(t, err)
		found := findMapping(got, mapping.UsuarioID)
		require.NotNil(t, found)
		require.NotNil(t, found.ListaID1)
		assert.Equal(t, 111, *found.ListaID1)
		require.NotNil(t, found.ListaID2)
		assert.Equal(t, 222, *found.ListaID2)
		require.NotNil(t, found.ListaID3)
		assert.Equal(t, 333, *found.ListaID3)

		// Second call with a NULL third slot: UPDATE path.
		updated, err := configdomain.NewVendedorMapping(mapping.UsuarioID, intPtr(999), intPtr(222), nil)
		require.NoError(t, err)
		require.NoError(t, repo.UpsertVendedorMapping(ctx, updated))

		got, err = repo.ListarVendedorMappings(ctx)
		require.NoError(t, err)
		found = findMapping(got, mapping.UsuarioID)
		require.NotNil(t, found)
		require.NotNil(t, found.ListaID1)
		assert.Equal(t, 999, *found.ListaID1)
		require.NotNil(t, found.ListaID2)
		assert.Equal(t, 222, *found.ListaID2)
		assert.Nil(t, found.ListaID3, "third slot must round-trip as NULL, not -1")
	})
}

func TestConfigRepo_DeleteVendedorMapping_RemovesRow(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		usuarioIDStr := existingUsuarioID(ctx, t, q)
		repo := configfb.NewConfigRepo(pool)

		mapping, err := configdomain.NewVendedorMapping(mustParseUUID(t, usuarioIDStr), intPtr(1), nil, nil)
		require.NoError(t, err)
		require.NoError(t, repo.UpsertVendedorMapping(ctx, mapping))

		require.NoError(t, repo.DeleteVendedorMapping(ctx, mapping.UsuarioID))

		got, err := repo.ListarVendedorMappings(ctx)
		require.NoError(t, err)
		assert.Nil(t, findMapping(got, mapping.UsuarioID))
	})
}

func TestConfigRepo_ResolverNombresLista_And_ListarIdentidadesMicrosip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := configfb.NewConfigRepo(pool)

		identidades, err := repo.ListarIdentidadesMicrosip(ctx)
		require.NoError(t, err)
		t.Logf("ListarIdentidadesMicrosip returned %d identidades", len(identidades))

		if len(identidades) == 0 {
			t.Skip("dev DB has no LISTAS_ATRIBUTOS rows for atributos 19985/86/87 — roster not seeded")
		}

		// Resolve the first identidad's populated lista ids back to names —
		// each must round-trip to the identidad's own Nombre.
		first := identidades[0]
		ids := make([]int, 0, 3)
		for _, id := range []*int{first.V1ListaID, first.V2ListaID, first.V3ListaID} {
			if id != nil {
				ids = append(ids, *id)
			}
		}
		require.NotEmpty(t, ids)

		nombres, err := repo.ResolverNombresLista(ctx, ids)
		require.NoError(t, err)
		for _, id := range ids {
			assert.Equal(t, first.Nombre, nombres[id])
		}
	})
}

func TestConfigRepo_ListaIDPerteneceAtributo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := configfb.NewConfigRepo(pool)

		identidades, err := repo.ListarIdentidadesMicrosip(ctx)
		require.NoError(t, err)
		if len(identidades) == 0 || identidades[0].V1ListaID == nil {
			t.Skip("dev DB has no atributo 19985 rows to exercise this check")
		}

		ok, err := repo.ListaIDPerteneceAtributo(ctx, *identidades[0].V1ListaID, 19985)
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = repo.ListaIDPerteneceAtributo(ctx, *identidades[0].V1ListaID, 19986)
		require.NoError(t, err)
		assert.False(t, ok, "a slot-1 lista id must not belong to attribute 19986")
	})
}

func findMapping(mappings []configdomain.VendedorMapping, usuarioID uuid.UUID) *configdomain.VendedorMapping {
	for i := range mappings {
		if mappings[i].UsuarioID == usuarioID {
			return &mappings[i]
		}
	}
	return nil
}

func mustParseUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(s)
	require.NoError(t, err)
	return id
}
