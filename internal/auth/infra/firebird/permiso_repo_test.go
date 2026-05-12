package firebird_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

func TestPermisoRepo_UpsertCatalog_Empty_NoError(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Empty slice → loop body never runs → returns nil.
		require.NoError(t, repo.UpsertCatalog(ctx, nil))
		require.NoError(t, repo.UpsertCatalog(ctx, []domain.PermissionMeta{}))
	})
}

// TestPermisoRepo_UpsertCatalog_ExecError drives the ExecContext error path
// via a canceled context.
func TestPermisoRepo_UpsertCatalog_ExecError(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.UpsertCatalog(cctx, []domain.PermissionMeta{
			{Code: domain.Permission("x:y"), Description: "d", Categoria: "test"},
		})
		require.Error(t, err)
	})
}

// TestPermisoRepo_FindByCodigo_CanceledContext drives the non-NoRows error
// path in FindByCodigo.
func TestPermisoRepo_FindByCodigo_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.FindByCodigo(cctx, domain.Permission("any:code"))
		require.Error(t, err)
		require.NotErrorIs(t, err, domain.ErrPermisoNotFound)
	})
}

// TestPermisoRepo_FindAll covers PermisoRepo.FindAll (currently 0%).
func TestPermisoRepo_FindAll(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		suffix := uuid.NewString()[:8]
		want := []domain.Permission{
			domain.Permission("test:" + suffix + ":all1"),
			domain.Permission("test:" + suffix + ":all2"),
		}
		require.NoError(t, repo.UpsertCatalog(ctx, []domain.PermissionMeta{
			{Code: want[0], Description: "u", Categoria: "test"},
			{Code: want[1], Description: "d", Categoria: "test"},
		}))

		all, err := repo.FindAll(ctx)
		require.NoError(t, err)

		codes := make([]domain.Permission, 0, len(all))
		for _, p := range all {
			codes = append(codes, p.Codigo())
		}
		for _, w := range want {
			assert.Contains(t, codes, w)
		}
	})
}

// TestPermisoRepo_FindAll_CanceledContext drives FindAll's QueryContext error
// branch.
func TestPermisoRepo_FindAll_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.FindAll(cctx)
		require.Error(t, err)
	})
}

// TestPermisoRepo_FindOrphans_CanceledContext drives both QueryContext error
// branches in FindOrphans — one with an empty `known` slice (the IN-LIST-less
// branch) and one with a populated slice (the dynamic IN-LIST branch).
func TestPermisoRepo_FindOrphans_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()

		_, err := repo.FindOrphans(cctx, nil)
		require.Error(t, err)

		_, err = repo.FindOrphans(cctx, []domain.Permission{domain.Permission("x:y")})
		require.Error(t, err)
	})
}

func TestPermisoRepo_UpsertCatalog_InsertsThenUpdates(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Use test-scoped codes so parallel test transactions do not contend
		// on the same primary key (which would trigger 10s lock waits).
		suffix := uuid.NewString()[:8]
		perms := []domain.PermissionMeta{
			{Code: domain.Permission("test:" + suffix + ":one"), Description: "uno", Categoria: "test"},
			{Code: domain.Permission("test:" + suffix + ":two"), Description: "dos", Categoria: "test"},
		}

		require.NoError(t, repo.UpsertCatalog(ctx, perms))

		got, err := repo.FindByCodigo(ctx, perms[0].Code)
		require.NoError(t, err)
		assert.Equal(t, perms[0].Description, got.Description())

		// Second call with mutated descriptions — should UPDATE, not error.
		mutated := make([]domain.PermissionMeta, len(perms))
		copy(mutated, perms)
		mutated[0].Description = "descripcion actualizada"
		require.NoError(t, repo.UpsertCatalog(ctx, mutated))

		got, err = repo.FindByCodigo(ctx, perms[0].Code)
		require.NoError(t, err)
		assert.Equal(t, "descripcion actualizada", got.Description())
	})
}

func TestPermisoRepo_FindByCodigo_NotFound(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.FindByCodigo(ctx, domain.Permission("nope:nope"))
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrPermisoNotFound)
	})
}

func TestPermisoRepo_FindOrphans(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Use test-scoped codes so parallel tests do not contend on the same
		// catalog keys (which would trigger 10s Firebird lock waits even when
		// both transactions are eventually rolled back).
		suffix := uuid.NewString()[:8]
		known := []domain.Permission{
			domain.Permission("test:" + suffix + ":known1"),
			domain.Permission("test:" + suffix + ":known2"),
		}
		orphanCode := domain.Permission("test:" + suffix + ":orphan")

		require.NoError(t, repo.UpsertCatalog(ctx, []domain.PermissionMeta{
			{Code: known[0], Description: "known one", Categoria: "test"},
			{Code: known[1], Description: "known two", Categoria: "test"},
			{Code: orphanCode, Description: "orphan", Categoria: "test"},
		}))

		orphans, err := repo.FindOrphans(ctx, known)
		require.NoError(t, err)
		assert.Contains(t, orphans, orphanCode)
		for _, k := range known {
			assert.NotContains(t, orphans, k)
		}
	})
}

func TestPermisoRepo_FindOrphans_EmptyKnown_ReturnsAll(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewPermisoRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		suffix := uuid.NewString()[:8]
		code := domain.Permission("test:" + suffix + ":findall")
		require.NoError(t, repo.UpsertCatalog(ctx, []domain.PermissionMeta{
			{Code: code, Description: "x", Categoria: "test"},
		}))

		orphans, err := repo.FindOrphans(ctx, nil)
		require.NoError(t, err)
		assert.Contains(t, orphans, code, "with empty known, the test-scoped row is an orphan")
	})
}
