package firebird_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestRolRepo_SaveAndFindByID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		rol := newRol(t, root, "save-rol-"+uuid.NewString()[:8], false)

		require.NoError(t, repo.Save(ctx, rol))

		got, err := repo.FindByID(ctx, rol.ID())
		require.NoError(t, err)
		assert.Equal(t, rol.ID(), got.ID())
		assert.Equal(t, rol.Nombre(), got.Nombre())
		assert.False(t, got.Inmutable())
		assert.True(t, got.Activo())

		// Find by nombre too.
		gotByName, err := repo.FindByNombre(ctx, rol.Nombre())
		require.NoError(t, err)
		assert.Equal(t, rol.ID(), gotByName.ID())
	})
}

func TestRolRepo_FindByID_NotFound(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.FindByID(ctx, uuid.New())
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRolNotFound)
	})
}

func TestRolRepo_Save_DuplicateNombre(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		nombre := "dup-rol-" + uuid.NewString()[:8]
		first := newRol(t, root, nombre, false)
		require.NoError(t, repo.Save(ctx, first))

		dupe := newRol(t, root, nombre, false)
		err := repo.Save(ctx, dupe)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRolYaExiste)
	})
}

func TestRolRepo_UpsertInmutableByName_FirstInserts(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		nombre := "seed-" + uuid.NewString()[:8]
		seed := newRol(t, root, nombre, true)

		require.NoError(t, repo.UpsertInmutableByName(ctx, seed))

		got, err := repo.FindByNombre(ctx, nombre)
		require.NoError(t, err)
		assert.True(t, got.Inmutable())

		// Second call: no-op — should not error.
		again := newRol(t, root, nombre, true)
		require.NoError(t, repo.UpsertInmutableByName(ctx, again))
	})
}

func TestRolRepo_UpsertInmutableByName_RefusesNonInmutableShadow(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		nombre := "shadow-" + uuid.NewString()[:8]
		user := newRol(t, root, nombre, false)
		require.NoError(t, repo.Save(ctx, user))

		seed := newRol(t, root, nombre, true)
		err := repo.UpsertInmutableByName(ctx, seed)
		require.Error(t, err)
	})
}

func TestRolRepo_AsignarYRevocarPermiso(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		codes := seedPermisoCatalog(ctx, t, pool)
		require.NotEmpty(t, codes)

		rol := newRol(t, root, "asgnp-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, rol))

		require.NoError(t, repo.AsignarPermiso(ctx, rol.ID(), codes[0], root, testNow()))
		// Idempotent.
		require.NoError(t, repo.AsignarPermiso(ctx, rol.ID(), codes[0], root, testNow()))

		got, err := repo.PermisosFor(ctx, rol.ID())
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, codes[0], got[0])

		require.NoError(t, repo.RevocarPermiso(ctx, rol.ID(), codes[0]))
		// Idempotent.
		require.NoError(t, repo.RevocarPermiso(ctx, rol.ID(), codes[0]))

		got, err = repo.PermisosFor(ctx, rol.ID())
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestRolRepo_AsignarPermiso_UnknownCode_ReturnsErrPermisoNotFound(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		rol := newRol(t, root, "asgnp-bad-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, rol))

		err := repo.AsignarPermiso(ctx, rol.ID(), domain.Permission("does:not:exist"), root, testNow())
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrPermisoNotFound)
	})
}

func TestRolRepo_SyncPermisos_ReplacesSet(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		codes := seedPermisoCatalog(ctx, t, pool)
		require.GreaterOrEqual(t, len(codes), 3)

		rol := newRol(t, root, "sync-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, rol))

		// Initial set.
		require.NoError(t, repo.SyncPermisos(ctx, rol.ID(),
			[]domain.Permission{codes[0], codes[1]}, root, testNow()))
		got, err := repo.PermisosFor(ctx, rol.ID())
		require.NoError(t, err)
		assert.ElementsMatch(t, []domain.Permission{codes[0], codes[1]}, got)

		// Replace with a different set.
		require.NoError(t, repo.SyncPermisos(ctx, rol.ID(),
			[]domain.Permission{codes[2]}, root, testNow()))
		got, err = repo.PermisosFor(ctx, rol.ID())
		require.NoError(t, err)
		assert.ElementsMatch(t, []domain.Permission{codes[2]}, got)

		// Empty set → all gone.
		require.NoError(t, repo.SyncPermisos(ctx, rol.ID(), nil, root, testNow()))
		got, err = repo.PermisosFor(ctx, rol.ID())
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestRolRepo_SyncPermisos_UnknownCode_ReturnsErrPermisoNotFound(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		seedPermisoCatalog(ctx, t, pool)
		rol := newRol(t, root, "syncbad-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, rol))

		err := repo.SyncPermisos(ctx, rol.ID(),
			[]domain.Permission{domain.Permission("does:not:exist")}, root, testNow())
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrPermisoNotFound)
	})
}

// TestRolRepo_Save_WithDescription exercises the non-nil description branch
// of rolInsertArgs (the existing tests pass nil so only the nil branch is
// covered).
func TestRolRepo_Save_WithDescription(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		desc := "rol con descripcion"
		rol, err := domain.NewRol(uuid.New(), "desc-"+uuid.NewString()[:8], &desc, false, root, testNow())
		require.NoError(t, err)

		require.NoError(t, repo.Save(ctx, rol))

		got, err := repo.FindByID(ctx, rol.ID())
		require.NoError(t, err)
		require.NotNil(t, got.Description())
		assert.Equal(t, desc, *got.Description())
	})
}

// TestRolRepo_Update_HappyPath covers the entire Update flow — currently 0%.
func TestRolRepo_Update_HappyPath(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		rol := newRol(t, root, "upd-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, rol))

		// Mutate name + description via the domain mutator.
		newDesc := "actualizada"
		require.NoError(t, rol.Update(rol.Nombre()+"-v2", &newDesc, root, testNow()))
		require.NoError(t, repo.Update(ctx, rol))

		got, err := repo.FindByID(ctx, rol.ID())
		require.NoError(t, err)
		require.NotNil(t, got.Description())
		assert.Equal(t, "actualizada", *got.Description())
	})
}

// TestRolRepo_Update_NotFound exercises the n==0 branch in Update.
func TestRolRepo_Update_NotFound(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		// Build but never Save.
		ghost := newRol(t, root, "ghost-"+uuid.NewString()[:8], false)
		err := repo.Update(ctx, ghost)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRolNotFound)
	})
}

// TestRolRepo_Update_DuplicateNombre exercises the unique-violation branch
// in Update.
func TestRolRepo_Update_DuplicateNombre(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		a := newRol(t, root, "updA-"+uuid.NewString()[:8], false)
		b := newRol(t, root, "updB-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, a))
		require.NoError(t, repo.Save(ctx, b))

		require.NoError(t, b.Update(a.Nombre(), nil, root, testNow()))
		err := repo.Update(ctx, b)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrRolYaExiste)
	})
}

// TestRolRepo_List_Pagination covers RolRepo.List (currently 0%).
func TestRolRepo_List_Pagination(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)

		const newRows = 5
		created := make(map[uuid.UUID]bool, newRows)
		for i := range newRows {
			rol := newRol(t, root, "list-"+strconv.Itoa(i)+"-"+uuid.NewString()[:8], false)
			require.NoError(t, repo.Save(ctx, rol))
			created[rol.ID()] = true
		}

		seen := make(map[uuid.UUID]bool)
		var cursor string
		pageCount := 0
		const pageSize = 2
		const safetyLimit = 1000
		for pageCount < safetyLimit {
			page, err := repo.List(ctx, outbound.ListParams{Cursor: cursor, PageSize: pageSize})
			require.NoError(t, err)
			require.LessOrEqual(t, len(page.Items), pageSize)
			for _, item := range page.Items {
				assert.False(t, seen[item.ID()], "duplicate id across pages: %s", item.ID())
				seen[item.ID()] = true
			}
			pageCount++
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		require.Less(t, pageCount, safetyLimit)
		for id := range created {
			assert.True(t, seen[id], "rol %s not seen by List", id)
		}
	})
}

// TestRolRepo_FindByID_MalformedUUID drives parseUUIDColumn's error path
// through rolFromRow. We insert a row in MSP_ROLES whose CREATED_BY is a
// real seeded usuario (FK constraint) but whose ID is a 36-char non-UUID.
func TestRolRepo_FindByID_MalformedUUID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)

		badID := "rol-not-a-uuid-YYYYYYYYYYYYYYYYYYYYY"
		require.Len(t, badID, 36)
		nombre := "malformed-rol-" + uuid.NewString()[:8]
		now := testNow()
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_ROLES
			 (ID, NOMBRE, INMUTABLE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, FALSE, TRUE, ?, ?, ?, ?)`,
			badID, nombre, now, now, root.String(), root.String(),
		)
		require.NoError(t, err)

		_, err = repo.FindByNombre(ctx, nombre)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected apperror.Error, got %T", err)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
	})
}

// TestRolRepo_SyncPermisos_DuplicateInBatch_ReturnsMapped exercises the
// final "return mapped" branch of SyncPermisos: the initial DELETE succeeds
// but the second INSERT in the batch hits a unique-violation (same code
// twice in the input). The error is neither FK nor swallowed-as-nil, so the
// repo returns it as-is.
func TestRolRepo_SyncPermisos_DuplicateInBatch_ReturnsMapped(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		codes := seedPermisoCatalog(ctx, t, pool)
		require.NotEmpty(t, codes)
		rol := newRol(t, root, "syncdupe-"+uuid.NewString()[:8], false)
		require.NoError(t, repo.Save(ctx, rol))

		// Same code twice — first insert succeeds, second triggers
		// firebird_unique_violation. SyncPermisos returns it as-is (not as
		// ErrPermisoNotFound).
		err := repo.SyncPermisos(ctx, rol.ID(),
			[]domain.Permission{codes[0], codes[0]}, root, testNow())
		require.Error(t, err)
		require.NotErrorIs(t, err, domain.ErrPermisoNotFound)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_unique_violation", appErr.Code)
	})
}

// TestRolRepo_FindByNombre_MalformedCreatedBy drives rolFromRow's CREATED_BY
// parseUUIDColumn error branch. Strategy: seed a usuario with a malformed
// 36-char ID (anchor), then insert a MSP_ROLES row pointing CREATED_BY at
// that anchor. The FK to MSP_USUARIOS is satisfied (anchor exists) but
// uuid.Parse rejects the CREATED_BY value on read.
func TestRolRepo_FindByNombre_MalformedCreatedBy(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)
		now := testNow()

		// Anchor usuario with malformed ID.
		badAnchor := "rol-anchor-not-uuid-AAAAAAAAAAAAAAAA"
		require.Len(t, badAnchor, 36)
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'rol-anchor', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
			badAnchor, "fb-rolanchor-"+uuid.NewString(),
			"rolanchor-"+uuid.NewString()+"@example.invalid",
			now, now, badAnchor, badAnchor,
		)
		require.NoError(t, err)

		nombre := "rol-cbad-" + uuid.NewString()[:8]
		_, err = q.ExecContext(
			ctx,
			`INSERT INTO MSP_ROLES
			 (ID, NOMBRE, INMUTABLE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, FALSE, TRUE, ?, ?, ?, ?)`,
			uuid.NewString(), nombre, now, now, badAnchor, root.String(),
		)
		require.NoError(t, err)

		_, err = repo.FindByNombre(ctx, nombre)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
		assert.Equal(t, "CREATED_BY", appErr.Fields["column"])
	})
}

// TestRolRepo_FindByNombre_MalformedUpdatedBy is the UPDATED_BY counterpart.
func TestRolRepo_FindByNombre_MalformedUpdatedBy(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		q := firebird.GetQuerier(ctx, pool.DB)
		now := testNow()

		badAnchor := "rol-ub-not-uuid-BBBBBBBBBBBBBBBBBBBB"
		require.Len(t, badAnchor, 36)
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'rol-ub-anchor', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
			badAnchor, "fb-roluban-"+uuid.NewString(),
			"roluban-"+uuid.NewString()+"@example.invalid",
			now, now, badAnchor, badAnchor,
		)
		require.NoError(t, err)

		nombre := "rol-ubad-" + uuid.NewString()[:8]
		_, err = q.ExecContext(
			ctx,
			`INSERT INTO MSP_ROLES
			 (ID, NOMBRE, INMUTABLE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, FALSE, TRUE, ?, ?, ?, ?)`,
			uuid.NewString(), nombre, now, now, root.String(), badAnchor,
		)
		require.NoError(t, err)

		_, err = repo.FindByNombre(ctx, nombre)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
		assert.Equal(t, "UPDATED_BY", appErr.Fields["column"])
	})
}

// TestRolRepo_UpsertInmutableByName_PropagatesUnexpectedError drives the
// "FindByNombre returned non-NotFound error" branch by pre-cancelling the
// context — the inner SELECT fails with context.Canceled.
func TestRolRepo_UpsertInmutableByName_PropagatesUnexpectedError(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		seed := newRol(t, root, "upsbad-"+uuid.NewString()[:8], true)

		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.UpsertInmutableByName(cctx, seed)
		require.Error(t, err)
		require.NotErrorIs(t, err, domain.ErrRolNotFound)
		require.NotErrorIs(t, err, domain.ErrRolYaExiste)
	})
}

// TestRolRepo_AsignarPermiso_GenericError exercises the final "return mapped"
// branch of AsignarPermiso (neither unique nor FK violation). Driven by a
// canceled context.
func TestRolRepo_AsignarPermiso_GenericError(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.AsignarPermiso(cctx, uuid.New(), domain.Permission("any:perm"), uuid.New(), testNow())
		require.Error(t, err)
	})
}

// TestRolRepo_RevocarPermiso_CanceledContext drives the ExecContext error
// branch of RevocarPermiso.
func TestRolRepo_RevocarPermiso_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.RevocarPermiso(cctx, uuid.New(), domain.Permission("x:y"))
		require.Error(t, err)
	})
}

// TestRolRepo_SyncPermisos_InitialDeleteError drives the error path in
// SyncPermisos when the upfront DELETE fails — exercised via a canceled
// context.
func TestRolRepo_SyncPermisos_InitialDeleteError(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.SyncPermisos(cctx, uuid.New(),
			[]domain.Permission{domain.Permission("x:y")}, uuid.New(), testNow())
		require.Error(t, err)
	})
}

// TestRolRepo_PermisosFor_CanceledContext drives the QueryContext error
// branch.
func TestRolRepo_PermisosFor_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.PermisosFor(cctx, uuid.New())
		require.Error(t, err)
	})
}
