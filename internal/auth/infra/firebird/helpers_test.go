package firebird_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	authfb "github.com/abdimuy/msp-api/internal/auth/infra/firebird"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// testNow returns a stable instant used across the integration tests so
// timestamps round-trip cleanly through ScanUTCTime regardless of process
// timezone.
func testNow() time.Time {
	return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
}

// seedRootUsuario inserts a self-referential root usuario inside the active
// test tx. Returns the usuario's ID for downstream CREATED_BY/UPDATED_BY
// references. The CREATED_BY column on MSP_USUARIOS is a self-FK so the very
// first usuario must point at itself.
func seedRootUsuario(ctx context.Context, t *testing.T, pool *firebird.Pool) uuid.UUID {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	id := uuid.New()
	now := testNow()
	suffix := id.String()
	_, err := q.ExecContext(
		ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'root', TRUE, ?, ?, ?, ?)`,
		id.String(), "fbtest-root-"+suffix, "root-"+suffix+"@example.invalid",
		now, now, id.String(), id.String(),
	)
	require.NoError(t, err, "seed root usuario")
	return id
}

// newUsuario constructs a Usuario entity ready for insertion with unique
// identifiers, attributed to createdBy.
func newUsuario(t *testing.T, createdBy uuid.UUID, suffix string) *domain.Usuario {
	t.Helper()
	email, err := domain.NewEmail("user-" + suffix + "@example.invalid")
	require.NoError(t, err)
	fuid, err := domain.NewFirebaseUID("fb-" + suffix)
	require.NoError(t, err)
	nombre, err := domain.NewNombre("Usuario " + suffix)
	require.NoError(t, err)
	return domain.NewUsuario(uuid.New(), fuid, email, nombre, nil, nil, createdBy, testNow())
}

// newRol constructs a Rol entity ready for insertion.
func newRol(t *testing.T, createdBy uuid.UUID, nombre string, inmutable bool) *domain.Rol {
	t.Helper()
	rol, err := domain.NewRol(uuid.New(), nombre, nil, inmutable, createdBy, testNow())
	require.NoError(t, err)
	return rol
}

// seedPermisoCatalog inserts a small set of test-scoped permission codes
// into MSP_PERMISOS inside the active tx so SyncPermisos / AsignarPermiso
// tests have valid FK targets. Each call uses a unique suffix so parallel
// tests do not contend on the same catalog keys (Firebird serializes writes
// to the same primary key even when the transactions are eventually rolled
// back, which would otherwise produce 10-second lock waits).
//
// The returned codes are deliberately not the same as domain.AllPermissions();
// tests that need to assert "the canonical catalog is present" should call
// UpsertCatalog directly with domain.AllPermissions() (and accept that they
// must run alone — see TestPermisoRepo_UpsertCatalog_InsertsThenUpdates).
func seedPermisoCatalog(ctx context.Context, t *testing.T, pool *firebird.Pool) []domain.Permission {
	t.Helper()
	repo := authfb.NewPermisoRepo(pool)
	suffix := uuid.NewString()[:8]
	perms := []domain.PermissionMeta{
		{Code: domain.Permission("test:" + suffix + ":a"), Description: "perm a", Categoria: "test"},
		{Code: domain.Permission("test:" + suffix + ":b"), Description: "perm b", Categoria: "test"},
		{Code: domain.Permission("test:" + suffix + ":c"), Description: "perm c", Categoria: "test"},
	}
	require.NoError(t, repo.UpsertCatalog(ctx, perms))
	codes := make([]domain.Permission, len(perms))
	for i, p := range perms {
		codes[i] = p.Code
	}
	return codes
}
