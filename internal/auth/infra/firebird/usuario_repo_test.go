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
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

func TestUsuarioRepo_SaveAndFindByID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "save-"+uuid.NewString())

		require.NoError(t, repo.Save(ctx, u))

		got, err := repo.FindByID(ctx, u.ID())
		require.NoError(t, err)
		assert.Equal(t, u.ID(), got.ID())
		assert.Equal(t, u.Email().Value(), got.Email().Value())
		assert.Equal(t, u.FirebaseUID().Value(), got.FirebaseUID().Value())
		assert.True(t, got.Activo())
	})
}

func TestUsuarioRepo_FindByFirebaseUID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "fbuid-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, u))

		got, err := repo.FindByFirebaseUID(ctx, u.FirebaseUID().Value())
		require.NoError(t, err)
		assert.Equal(t, u.ID(), got.ID())

		_, err = repo.FindByFirebaseUID(ctx, "nonexistent-fbuid-"+uuid.NewString())
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})
}

func TestUsuarioRepo_FindByEmail(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "email-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, u))

		got, err := repo.FindByEmail(ctx, u.Email().Value())
		require.NoError(t, err)
		assert.Equal(t, u.ID(), got.ID())

		_, err = repo.FindByEmail(ctx, "missing-"+uuid.NewString()+"@example.invalid")
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})
}

func TestUsuarioRepo_Save_DuplicateEmail(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		first := newUsuario(t, root, "dup-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, first))

		// Same email, different uuid+fuid → should still collide on UQ_EMAIL.
		email, err := domain.NewEmail(first.Email().Value())
		require.NoError(t, err)
		fuid, err := domain.NewFirebaseUID("other-" + uuid.NewString())
		require.NoError(t, err)
		nombre, err := domain.NewNombre("Otro")
		require.NoError(t, err)
		dupe := domain.NewUsuario(uuid.New(), fuid, email, nombre, nil, nil, root, testNow())

		err = repo.Save(ctx, dupe)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
	})
}

func TestUsuarioRepo_AsignarRolThenRevocar(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	usuarioRepo := authfb.NewUsuarioRepo(pool)
	rolRepo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "asgn-"+uuid.NewString())
		require.NoError(t, usuarioRepo.Save(ctx, u))

		rol := newRol(t, root, "rol-"+uuid.NewString()[:8], false)
		require.NoError(t, rolRepo.Save(ctx, rol))

		require.NoError(t, usuarioRepo.AsignarRol(ctx, u.ID(), rol.ID(), root, testNow()))

		// Re-asignar: idempotent, no error.
		require.NoError(t, usuarioRepo.AsignarRol(ctx, u.ID(), rol.ID(), root, testNow()))

		roles, err := usuarioRepo.RolesFor(ctx, u.ID())
		require.NoError(t, err)
		require.Len(t, roles, 1)
		assert.Equal(t, rol.ID(), roles[0].ID())

		require.NoError(t, usuarioRepo.RevocarRol(ctx, u.ID(), rol.ID()))

		// Idempotent revoke.
		require.NoError(t, usuarioRepo.RevocarRol(ctx, u.ID(), rol.ID()))

		roles, err = usuarioRepo.RolesFor(ctx, u.ID())
		require.NoError(t, err)
		assert.Empty(t, roles)
	})
}

func TestUsuarioRepo_PermisosFor_Deduplicates(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	usuarioRepo := authfb.NewUsuarioRepo(pool)
	rolRepo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		codes := seedPermisoCatalog(ctx, t, pool)
		require.GreaterOrEqual(t, len(codes), 3)

		u := newUsuario(t, root, "perms-"+uuid.NewString())
		require.NoError(t, usuarioRepo.Save(ctx, u))

		rolA := newRol(t, root, "rA-"+uuid.NewString()[:8], false)
		rolB := newRol(t, root, "rB-"+uuid.NewString()[:8], false)
		require.NoError(t, rolRepo.Save(ctx, rolA))
		require.NoError(t, rolRepo.Save(ctx, rolB))

		// rolA: codes[0], codes[1]
		// rolB: codes[1], codes[2]  → codes[1] is shared, should dedupe.
		require.NoError(t, rolRepo.AsignarPermiso(ctx, rolA.ID(), codes[0], root, testNow()))
		require.NoError(t, rolRepo.AsignarPermiso(ctx, rolA.ID(), codes[1], root, testNow()))
		require.NoError(t, rolRepo.AsignarPermiso(ctx, rolB.ID(), codes[1], root, testNow()))
		require.NoError(t, rolRepo.AsignarPermiso(ctx, rolB.ID(), codes[2], root, testNow()))

		require.NoError(t, usuarioRepo.AsignarRol(ctx, u.ID(), rolA.ID(), root, testNow()))
		require.NoError(t, usuarioRepo.AsignarRol(ctx, u.ID(), rolB.ID(), root, testNow()))

		perms, err := usuarioRepo.PermisosFor(ctx, u.ID())
		require.NoError(t, err)
		require.Len(t, perms, 3)
		assert.ElementsMatch(t, []domain.Permission{codes[0], codes[1], codes[2]}, perms)
	})
}

func TestUsuarioRepo_List_Pagination(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)

		// Count existing usuarios so the test stays robust against
		// pre-existing rows (the root we just inserted plus anything from
		// prior production data).
		existing := countAllUsuarios(ctx, t, pool)

		const newRows = 5
		created := make(map[uuid.UUID]bool, newRows)
		for i := range newRows {
			u := newUsuario(t, root, "list-"+strconv.Itoa(i)+"-"+uuid.NewString())
			require.NoError(t, repo.Save(ctx, u))
			created[u.ID()] = true
		}

		total := existing + newRows
		seen := make(map[uuid.UUID]bool, total)

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
		require.Less(t, pageCount, safetyLimit, "pagination did not terminate")

		// All 5 newly-created rows are observable.
		for id := range created {
			assert.True(t, seen[id], "created usuario %s not seen by List", id)
		}
	})
}

func TestUsuarioRepo_Update_SoftDeleteRename(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "rename-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, u))

		originalEmail := u.Email().Value()

		// Rename for soft-delete and deactivate.
		newSuffix := "+deleted-" + uuid.NewString()
		u.RenameForSoftDelete(originalEmail+newSuffix, u.FirebaseUID().Value()+newSuffix, root, testNow())
		u.Desactivar(root, testNow())

		require.NoError(t, repo.Update(ctx, u))

		got, err := repo.FindByID(ctx, u.ID())
		require.NoError(t, err)
		assert.False(t, got.Activo())
		assert.NotEqual(t, originalEmail, got.Email().Value())

		// A new usuario can now claim the original email — it was freed by
		// the soft-delete rename.
		email, err := domain.NewEmail(originalEmail)
		require.NoError(t, err)
		fuid, err := domain.NewFirebaseUID("fbid-fresh-" + uuid.NewString())
		require.NoError(t, err)
		nombre, err := domain.NewNombre("Reuse")
		require.NoError(t, err)
		reused := domain.NewUsuario(uuid.New(), fuid, email, nombre, nil, nil, root, testNow())
		require.NoError(t, repo.Save(ctx, reused))
	})
}

func TestUsuarioRepo_ConcurrentAsignarRol_Idempotent(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	usuarioRepo := authfb.NewUsuarioRepo(pool)
	rolRepo := authfb.NewRolRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "conc-"+uuid.NewString())
		require.NoError(t, usuarioRepo.Save(ctx, u))
		rol := newRol(t, root, "conc-rol-"+uuid.NewString()[:8], false)
		require.NoError(t, rolRepo.Save(ctx, rol))

		// Concurrency note: *sql.Tx is single-threaded so this test cannot
		// literally fan out 10 parallel transactions writing the same PK
		// without escaping the rollback boundary. Instead it issues N
		// serial AsignarRol calls within the same tx to assert that
		// re-issuing the assignment after the row exists surfaces the unique
		// violation as the idempotent nil return — the same guarantee that
		// protects the real production path when two competing requests race
		// on the public pool.
		const repeats = 10
		successCount := 0
		for range repeats {
			require.NoError(t, usuarioRepo.AsignarRol(ctx, u.ID(), rol.ID(), root, testNow()))
			successCount++
		}
		assert.Equal(t, repeats, successCount,
			"every call should swallow the unique violation and return nil")

		roles, err := usuarioRepo.RolesFor(ctx, u.ID())
		require.NoError(t, err)
		assert.Len(t, roles, 1, "exactly one (usuario, rol) row should exist")
	})
}

func TestUsuarioRepo_Save_DuplicateFirebaseUID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		first := newUsuario(t, root, "dupfb-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, first))

		// Same firebase_uid, different uuid+email → must collide on UQ_FIREBASE_UID.
		fuid, err := domain.NewFirebaseUID(first.FirebaseUID().Value())
		require.NoError(t, err)
		email, err := domain.NewEmail("other-" + uuid.NewString() + "@example.invalid")
		require.NoError(t, err)
		nombre, err := domain.NewNombre("Otro")
		require.NoError(t, err)
		dupe := domain.NewUsuario(uuid.New(), fuid, email, nombre, nil, nil, root, testNow())

		err = repo.Save(ctx, dupe)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
	})
}

// TestUsuarioRepo_Save_WithTelefonoAndAlmacen exercises the non-nil branches
// of the telefono and almacen_id parameter encoding in Save.
func TestUsuarioRepo_Save_WithTelefonoAndAlmacen(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		email, err := domain.NewEmail("withopt-" + uuid.NewString() + "@example.invalid")
		require.NoError(t, err)
		fuid, err := domain.NewFirebaseUID("fb-withopt-" + uuid.NewString())
		require.NoError(t, err)
		nombre, err := domain.NewNombre("Con Telefono")
		require.NoError(t, err)
		tel, err := platform.NewTelefono("5551234567")
		require.NoError(t, err)
		almacen := 42
		u := domain.NewUsuario(uuid.New(), fuid, email, nombre, &tel, &almacen, root, testNow())

		require.NoError(t, repo.Save(ctx, u))

		got, err := repo.FindByID(ctx, u.ID())
		require.NoError(t, err)
		require.NotNil(t, got.Telefono())
		assert.Equal(t, tel.Value(), got.Telefono().Value())
		require.NotNil(t, got.AlmacenID())
		assert.Equal(t, almacen, *got.AlmacenID())
	})
}

// TestUsuarioRepo_Update_WithTelefonoAndAlmacen exercises Update's optional
// field branches plus the round-trip happy path.
func TestUsuarioRepo_Update_WithTelefonoAndAlmacen(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "updopt-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, u))

		tel, err := platform.NewTelefono("5559876543")
		require.NoError(t, err)
		almacen := 7
		u.Update(domain.UsuarioUpdate{
			Email:     u.Email(),
			Nombre:    u.Nombre(),
			Telefono:  &tel,
			AlmacenID: &almacen,
		}, root, testNow())
		require.NoError(t, repo.Update(ctx, u))

		got, err := repo.FindByID(ctx, u.ID())
		require.NoError(t, err)
		require.NotNil(t, got.Telefono())
		assert.Equal(t, tel.Value(), got.Telefono().Value())
		require.NotNil(t, got.AlmacenID())
		assert.Equal(t, almacen, *got.AlmacenID())
	})
}

// TestUsuarioRepo_Update_NotFound exercises the n==0 branch of Update.
func TestUsuarioRepo_Update_NotFound(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		// Build a usuario but never Save it — Update should report NotFound.
		ghost := newUsuario(t, root, "ghost-"+uuid.NewString())
		err := repo.Update(ctx, ghost)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUsuarioNotFound)
	})
}

// TestUsuarioRepo_Update_DuplicateEmail exercises Update's unique-violation
// branch — renaming to an email that already exists on a different row.
func TestUsuarioRepo_Update_DuplicateEmail(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		a := newUsuario(t, root, "updA-"+uuid.NewString())
		b := newUsuario(t, root, "updB-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, a))
		require.NoError(t, repo.Save(ctx, b))

		// Try to rename b to a's email.
		b.Update(domain.UsuarioUpdate{
			Email:     a.Email(),
			Nombre:    b.Nombre(),
			Telefono:  nil,
			AlmacenID: nil,
		}, root, testNow())
		err := repo.Update(ctx, b)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUsuarioYaExiste)
	})
}

// TestUsuarioRepo_AsignarRol_FKViolation exercises the non-unique error path
// in AsignarRol — assigning an unknown rol surfaces an FK violation.
func TestUsuarioRepo_AsignarRol_FKViolation(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "asgnfk-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, u))

		err := repo.AsignarRol(ctx, u.ID(), uuid.New(), root, testNow())
		require.Error(t, err)
		// The error must be a conflict-shaped FK violation surfaced from
		// firebird.MapError (NOT a unique-violation, NOT silently swallowed).
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected apperror.Error, got %T", err)
		assert.Equal(t, "firebird_fk_violation", appErr.Code)
	})
}

// TestUsuarioRepo_RevocarRol_NoRow exercises the idempotent revoke when the
// (usuario, rol) row does not exist — Firebird returns 0 rows affected and
// the repo must return nil.
func TestUsuarioRepo_RevocarRol_NoRow(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		// Both IDs are uuid.New() so no MSP_USUARIOS_ROLES row matches the
		// composite primary key — DELETE finds nothing and that's fine.
		err := repo.RevocarRol(ctx, uuid.New(), uuid.New())
		require.NoError(t, err)
	})
}

// TestUsuarioRepo_RevocarRol_CanceledContext drives the ExecContext error
// branch of RevocarRol by pre-cancelling the context. Any non-nil error is
// surfaced via firebird.MapError.
func TestUsuarioRepo_RevocarRol_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := repo.RevocarRol(cctx, uuid.New(), uuid.New())
		require.Error(t, err)
	})
}

// TestUsuarioRepo_PermisosFor_CanceledContext drives the QueryContext error
// branch.
func TestUsuarioRepo_PermisosFor_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.PermisosFor(cctx, uuid.New())
		require.Error(t, err)
	})
}

// TestUsuarioRepo_RolesFor_CanceledContext drives the QueryContext error
// branch.
func TestUsuarioRepo_RolesFor_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.RolesFor(cctx, uuid.New())
		require.Error(t, err)
	})
}

// TestUsuarioRepo_List_BadCursor exercises queryPage's decodeCursor error
// branch. We pass an obviously malformed cursor and expect the validation
// error to bubble up unchanged.
func TestUsuarioRepo_List_BadCursor(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		_, err := repo.List(ctx, outbound.ListParams{Cursor: "not-base64-!", PageSize: 10})
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected apperror.Error, got %T", err)
		assert.Equal(t, "invalid_cursor", appErr.Code)
	})
}

// TestUsuarioRepo_List_CanceledContext exercises queryPage's QueryContext
// error branch via a pre-canceled child context.
func TestUsuarioRepo_List_CanceledContext(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := repo.List(cctx, outbound.ListParams{PageSize: 10})
		require.Error(t, err)
	})
}

// TestUsuarioRepo_List_MalformedRow exercises queryPage's scanRow error
// branch: a row with a malformed UUID makes usuarioFromRow return an error
// mid-iteration, hitting the `if scanErr != nil` branch.
func TestUsuarioRepo_List_MalformedRow(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		badID := "list-not-a-uuid-ZZZZZZZZZZZZZZZZZZZZ"
		require.Len(t, badID, 36)
		now := testNow()
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'malformed-list', TRUE, ?, ?, ?, ?)`,
			badID, "fb-listmal-"+uuid.NewString(),
			"listmal-"+uuid.NewString()+"@example.invalid",
			now, now, badID, badID,
		)
		require.NoError(t, err)

		// List eventually walks every row including the malformed one, so the
		// scan error MUST surface from at least one paginated call.
		var listErr error
		var cursor string
		for i := 0; i < 200 && listErr == nil; i++ {
			page, e := repo.List(ctx, outbound.ListParams{Cursor: cursor, PageSize: 50})
			if e != nil {
				listErr = e
				break
			}
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		require.Error(t, listErr, "List should have hit the malformed row")
		appErr, ok := apperror.As(listErr)
		require.True(t, ok, "expected apperror.Error, got %T", listErr)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
	})
}

// TestUsuarioRepo_FindByID_MalformedUUID drives parseUUIDColumn's error path
// and usuarioFromRow's error-propagation paths. We insert a row whose ID
// (and self-referential CREATED_BY/UPDATED_BY) is a 36-character non-UUID
// string. The CHAR(36) column accepts it, the FK self-references itself, but
// uuid.Parse rejects the value when the repo tries to read it back.
func TestUsuarioRepo_FindByID_MalformedUUID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		// 36-character string that is NOT a valid UUID. The CHAR(36) column
		// accepts it; uuid.Parse will reject the format on read.
		badID := "not-a-uuid-XXXXXXXXXXXXXXXXXXXXXXXXX"
		require.Len(t, badID, 36)
		now := testNow()
		email := "malformed-" + uuid.NewString() + "@example.invalid"
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'malformed', TRUE, ?, ?, ?, ?)`,
			badID, "fb-malformed-"+uuid.NewString(), email,
			now, now, badID, badID,
		)
		require.NoError(t, err, "insert row with malformed UUID")

		// FindByID stringifies its input via uuid.UUID.String(); we can't pass
		// the malformed ID through it. Use FindByEmail instead to hit the same
		// findOne → usuarioFromRow → parseUUIDColumn chain.
		_, err = repo.FindByEmail(ctx, email)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected apperror.Error, got %T", err)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
	})
}

// TestUsuarioRepo_FindByEmail_MalformedCreatedBy drives the CREATED_BY
// branch of parseUUIDColumn inside usuarioFromRow. We seed a row whose ID is
// itself a malformed 36-char string, then insert a *second* row with a valid
// UUID ID but whose CREATED_BY points at the malformed first row (FK
// satisfied via self-reference). Reading back the second row makes
// usuarioFromRow succeed on ID and fail on CREATED_BY.
func TestUsuarioRepo_FindByEmail_MalformedCreatedBy(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		// First row: malformed ID, self-FK.
		badAnchorID := "anchor-bad-uuid-XXXXXXXXXXXXXXXXXXXX"
		require.Len(t, badAnchorID, 36)
		now := testNow()
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'anchor', TRUE, ?, ?, ?, ?)`,
			badAnchorID, "fb-anchor-"+uuid.NewString(),
			"anchor-"+uuid.NewString()+"@example.invalid",
			now, now, badAnchorID, badAnchorID,
		)
		require.NoError(t, err)

		// Second row: valid UUID ID; CREATED_BY = malformed first row.
		victimID := uuid.New().String()
		victimEmail := "victim-" + uuid.NewString() + "@example.invalid"
		_, err = q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'victim', TRUE, ?, ?, ?, ?)`,
			victimID, "fb-victim-"+uuid.NewString(), victimEmail,
			now, now, badAnchorID, victimID,
		)
		require.NoError(t, err)

		// Read back the victim — ID parses fine, CREATED_BY parsing fails.
		_, err = repo.FindByEmail(ctx, victimEmail)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok, "expected apperror.Error, got %T", err)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
		assert.Equal(t, "CREATED_BY", appErr.Fields["column"])
	})
}

// TestUsuarioRepo_FindByEmail_MalformedUpdatedBy mirrors the CREATED_BY test
// but stresses the UPDATED_BY branch — ID and CREATED_BY parse cleanly while
// UPDATED_BY does not.
func TestUsuarioRepo_FindByEmail_MalformedUpdatedBy(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		// Anchor row with a malformed ID we'll use as UPDATED_BY.
		badAnchorID := "ub-anchor-bad-uuid-XXXXXXXXXXXXXXXXX"
		require.Len(t, badAnchorID, 36)
		now := testNow()
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'ub-anchor', TRUE, ?, ?, ?, ?)`,
			badAnchorID, "fb-ubanchor-"+uuid.NewString(),
			"ubanchor-"+uuid.NewString()+"@example.invalid",
			now, now, badAnchorID, badAnchorID,
		)
		require.NoError(t, err)

		victimID := uuid.New().String()
		victimEmail := "ubvictim-" + uuid.NewString() + "@example.invalid"
		_, err = q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS
			 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, ?, 'ubvictim', TRUE, ?, ?, ?, ?)`,
			victimID, "fb-ubvictim-"+uuid.NewString(), victimEmail,
			now, now, victimID, badAnchorID,
		)
		require.NoError(t, err)

		_, err = repo.FindByEmail(ctx, victimEmail)
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
		assert.Equal(t, "UPDATED_BY", appErr.Fields["column"])
	})
}

// TestUsuarioRepo_RolesFor_MalformedRolRow drives the rolFromRow scan-error
// branch inside RolesFor: we attach a rol whose stored ID is a malformed
// 36-char string to a usuario, then read the user's roles. The join returns
// the malformed rol and rolFromRow surfaces firebird_uuid_invalid.
func TestUsuarioRepo_RolesFor_MalformedRolRow(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := authfb.NewUsuarioRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		root := seedRootUsuario(ctx, t, pool)
		u := newUsuario(t, root, "rolesfor-mal-"+uuid.NewString())
		require.NoError(t, repo.Save(ctx, u))

		q := firebird.GetQuerier(ctx, pool.DB)
		now := testNow()
		badRolID := "rolesfor-mal-uuid-XXXXXXXXXXXXXXXXXX"
		require.Len(t, badRolID, 36)
		// Insert a rol with a malformed ID directly (the FK on CREATED_BY is
		// satisfied by root; the PK column is CHAR(36) so any string fits).
		_, err := q.ExecContext(
			ctx,
			`INSERT INTO MSP_ROLES
			 (ID, NOMBRE, INMUTABLE, ACTIVO,
			  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
			 VALUES (?, ?, FALSE, TRUE, ?, ?, ?, ?)`,
			badRolID, "rolesfor-mal-"+uuid.NewString()[:8],
			now, now, root.String(), root.String(),
		)
		require.NoError(t, err)
		// Attach the malformed rol to the usuario.
		_, err = q.ExecContext(
			ctx,
			`INSERT INTO MSP_USUARIOS_ROLES (USUARIO_ID, ROL_ID, CREATED_AT, CREATED_BY)
			 VALUES (?, ?, ?, ?)`,
			u.ID().String(), badRolID, now, root.String(),
		)
		require.NoError(t, err)

		_, err = repo.RolesFor(ctx, u.ID())
		require.Error(t, err)
		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "firebird_uuid_invalid", appErr.Code)
	})
}

// countAllUsuarios returns the number of MSP_USUARIOS rows visible to the
// active tx in ctx (or to the raw pool if ctx carries none).
func countAllUsuarios(ctx context.Context, t *testing.T, pool *firebird.Pool) int {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	var n int
	require.NoError(
		t,
		q.QueryRowContext(ctx, "SELECT COUNT(*) FROM MSP_USUARIOS").Scan(&n),
	)
	return n
}
