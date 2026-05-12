package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// ─── In-memory fakes ──────────────────────────────────────────────────────

type fakeUsuarioRepo struct {
	saved        []*domain.Usuario
	existing     []*domain.Usuario
	rolesByUser  map[uuid.UUID][]uuid.UUID
	listErr      error
	saveErr      error
	assignRolErr error
}

func newFakeUsuarioRepo() *fakeUsuarioRepo {
	return &fakeUsuarioRepo{rolesByUser: map[uuid.UUID][]uuid.UUID{}}
}

func (f *fakeUsuarioRepo) Save(_ context.Context, u *domain.Usuario) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, u)
	return nil
}

func (f *fakeUsuarioRepo) Update(_ context.Context, _ *domain.Usuario) error {
	return errors.New("not implemented for bootstrap test")
}

func (f *fakeUsuarioRepo) FindByID(_ context.Context, _ uuid.UUID) (*domain.Usuario, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeUsuarioRepo) FindByFirebaseUID(_ context.Context, _ string) (*domain.Usuario, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeUsuarioRepo) FindByEmail(_ context.Context, _ string) (*domain.Usuario, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeUsuarioRepo) List(_ context.Context, _ outbound.ListParams) (outbound.Page[*domain.Usuario], error) {
	if f.listErr != nil {
		return outbound.Page[*domain.Usuario]{}, f.listErr
	}
	return outbound.Page[*domain.Usuario]{Items: f.existing}, nil
}

func (f *fakeUsuarioRepo) AsignarRol(_ context.Context, usuarioID, rolID, _ uuid.UUID, _ time.Time) error {
	if f.assignRolErr != nil {
		return f.assignRolErr
	}
	f.rolesByUser[usuarioID] = append(f.rolesByUser[usuarioID], rolID)
	return nil
}

func (f *fakeUsuarioRepo) RevocarRol(_ context.Context, _, _ uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeUsuarioRepo) PermisosFor(_ context.Context, _ uuid.UUID) ([]domain.Permission, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeUsuarioRepo) RolesFor(_ context.Context, _ uuid.UUID) ([]*domain.Rol, error) {
	return nil, errors.New("not implemented")
}

type fakeRolRepo struct {
	upserted        *domain.Rol
	foundByName     *domain.Rol
	syncedPermisos  map[uuid.UUID][]domain.Permission
	upsertErr       error
	findErr         error
	syncPermisosErr error
}

func newFakeRolRepo() *fakeRolRepo {
	return &fakeRolRepo{syncedPermisos: map[uuid.UUID][]domain.Permission{}}
}

func (f *fakeRolRepo) Save(_ context.Context, _ *domain.Rol) error {
	return errors.New("not implemented")
}

func (f *fakeRolRepo) Update(_ context.Context, _ *domain.Rol) error {
	return errors.New("not implemented")
}

func (f *fakeRolRepo) FindByID(_ context.Context, _ uuid.UUID) (*domain.Rol, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeRolRepo) FindByNombre(_ context.Context, _ string) (*domain.Rol, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.foundByName != nil {
		return f.foundByName, nil
	}
	if f.upserted != nil {
		return f.upserted, nil
	}
	return nil, domain.ErrRolNotFound
}

func (f *fakeRolRepo) List(_ context.Context, _ outbound.ListParams) (outbound.Page[*domain.Rol], error) {
	return outbound.Page[*domain.Rol]{}, errors.New("not implemented")
}

func (f *fakeRolRepo) UpsertInmutableByName(_ context.Context, r *domain.Rol) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = r
	return nil
}

func (f *fakeRolRepo) AsignarPermiso(_ context.Context, _ uuid.UUID, _ domain.Permission, _ uuid.UUID, _ time.Time) error {
	return errors.New("not implemented")
}

func (f *fakeRolRepo) RevocarPermiso(_ context.Context, _ uuid.UUID, _ domain.Permission) error {
	return errors.New("not implemented")
}

func (f *fakeRolRepo) SyncPermisos(_ context.Context, rolID uuid.UUID, codes []domain.Permission, _ uuid.UUID, _ time.Time) error {
	if f.syncPermisosErr != nil {
		return f.syncPermisosErr
	}
	f.syncedPermisos[rolID] = codes
	return nil
}

func (f *fakeRolRepo) PermisosFor(_ context.Context, _ uuid.UUID) ([]domain.Permission, error) {
	return nil, errors.New("not implemented")
}

type fakePermisoRepo struct {
	upsertedCatalog []domain.PermissionMeta
	upsertErr       error
}

func (f *fakePermisoRepo) UpsertCatalog(_ context.Context, perms []domain.PermissionMeta) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upsertedCatalog = perms
	return nil
}

func (f *fakePermisoRepo) FindByCodigo(_ context.Context, _ domain.Permission) (*domain.Permiso, error) {
	return nil, errors.New("not implemented")
}

func (f *fakePermisoRepo) FindAll(_ context.Context) ([]*domain.Permiso, error) {
	return nil, errors.New("not implemented")
}

func (f *fakePermisoRepo) FindOrphans(_ context.Context, _ []domain.Permission) ([]domain.Permission, error) {
	return nil, errors.New("not implemented")
}

// inlineTxRunner just runs fn directly — no real transaction needed for the
// bootstrap algorithm test since the fakes are not transactional themselves.
type inlineTxRunner struct{ runErr error }

func (r *inlineTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	if r.runErr != nil {
		return r.runErr
	}
	return fn(ctx)
}

// fixedNowAndIDs supplies a deterministic clock + UUID source. The bootstrap
// algorithm requests two UUIDs (admin id, rol id); we return them in order.
func fixedNowAndIDs(now time.Time, ids ...uuid.UUID) (func() time.Time, func() uuid.UUID) {
	i := 0
	return func() time.Time { return now },
		func() uuid.UUID {
			if i >= len(ids) {
				return uuid.Nil
			}
			id := ids[i]
			i++
			return id
		}
}

func buildTestDeps() (bootstrapDeps, *fakeUsuarioRepo, *fakeRolRepo, *fakePermisoRepo, *inlineTxRunner) {
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	adminID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	rolID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	nowFn, idFn := fixedNowAndIDs(now, adminID, rolID)

	usuarios := newFakeUsuarioRepo()
	roles := newFakeRolRepo()
	permisos := &fakePermisoRepo{}
	runner := &inlineTxRunner{}

	deps := bootstrapDeps{
		Usuarios: usuarios,
		Roles:    roles,
		Permisos: permisos,
		TxRunner: runner,
		Now:      nowFn,
		NewID:    idFn,
	}
	return deps, usuarios, roles, permisos, runner
}

// ─── Tests ────────────────────────────────────────────────────────────────

func TestBootstrap_HappyPath_CreatesAdminAndAttachesSuperAdmin(t *testing.T) {
	t.Parallel()

	deps, usuarios, roles, permisos, _ := buildTestDeps()

	var out bytes.Buffer
	err := bootstrapAuth(context.Background(), deps, "alice-uid", "alice@example.com", "Alice", &out)
	require.NoError(t, err)

	require.Len(t, usuarios.saved, 1, "exactly one usuario must be saved")
	assert.Equal(t, "alice@example.com", usuarios.saved[0].Email().Value())

	require.NotNil(t, roles.upserted, "super_admin rol must be upserted")
	assert.Equal(t, "super_admin", roles.upserted.Nombre())
	assert.True(t, roles.upserted.Inmutable())

	// All permissions assigned to super_admin.
	assert.Len(t, roles.syncedPermisos[roles.upserted.ID()], len(domain.AllPermissions()))

	// Admin got the super_admin rol attached.
	assert.Contains(t, usuarios.rolesByUser[usuarios.saved[0].ID()], roles.upserted.ID())

	// Permission catalog upserted with the full canonical list.
	assert.Len(t, permisos.upsertedCatalog, len(domain.AllPermissions()))

	assert.Contains(t, out.String(), "auth-bootstrap: created usuario")
	assert.Contains(t, out.String(), "alice@example.com")
}

func TestBootstrap_RefusesIfUsuarioAlreadyExists(t *testing.T) {
	t.Parallel()

	deps, usuarios, _, _, _ := buildTestDeps()
	// Seed an existing usuario so List() returns 1 item.
	fuid, _ := domain.NewFirebaseUID("existing-uid")
	em, _ := domain.NewEmail("existing@example.com")
	nm, _ := domain.NewNombre("Existing")
	usuarios.existing = []*domain.Usuario{
		domain.NewUsuario(uuid.New(), fuid, em, nm, nil, nil, uuid.New(), time.Now()),
	}

	err := bootstrapAuth(context.Background(), deps, "alice-uid", "alice@example.com", "Alice", &bytes.Buffer{})
	require.ErrorIs(t, err, errBootstrapAlreadyDone)
	assert.Empty(t, usuarios.saved)
}

func TestBootstrap_ListError_Propagates(t *testing.T) {
	t.Parallel()

	deps, usuarios, _, _, _ := buildTestDeps()
	usuarios.listErr = errors.New("firebird down")

	err := bootstrapAuth(context.Background(), deps, "alice-uid", "alice@example.com", "Alice", &bytes.Buffer{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, errBootstrapAlreadyDone)
}

func TestBootstrap_InvalidEmail_Rejected(t *testing.T) {
	t.Parallel()

	deps, _, _, _, _ := buildTestDeps()
	err := bootstrapAuth(context.Background(), deps, "alice-uid", "not-an-email", "Alice", &bytes.Buffer{})
	require.Error(t, err)
}

func TestBootstrap_InvalidFirebaseUID_Rejected(t *testing.T) {
	t.Parallel()

	deps, _, _, _, _ := buildTestDeps()
	err := bootstrapAuth(context.Background(), deps, "", "alice@example.com", "Alice", &bytes.Buffer{})
	require.Error(t, err)
}

func TestBootstrap_EmptyNombre_Rejected(t *testing.T) {
	t.Parallel()

	deps, _, _, _, _ := buildTestDeps()
	err := bootstrapAuth(context.Background(), deps, "alice-uid", "alice@example.com", "   ", &bytes.Buffer{})
	require.Error(t, err)
}

func TestBootstrap_TxFailure_Propagates(t *testing.T) {
	t.Parallel()

	deps, _, _, _, runner := buildTestDeps()
	runner.runErr = errors.New("tx begin failed")

	err := bootstrapAuth(context.Background(), deps, "alice-uid", "alice@example.com", "Alice", &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx begin failed")
}

func TestBootstrap_PermisoCatalogFailure_StopsBeforeUsuario(t *testing.T) {
	t.Parallel()

	deps, usuarios, _, permisos, _ := buildTestDeps()
	permisos.upsertErr = errors.New("permiso upsert failed")

	err := bootstrapAuth(context.Background(), deps, "alice-uid", "alice@example.com", "Alice", &bytes.Buffer{})
	require.Error(t, err)
	assert.Empty(t, usuarios.saved, "usuario must not be saved when an earlier write fails")
}

// ─── CLI surface tests ─────────────────────────────────────────────────────

func TestAuthBootstrapCmd_RegistersRequiredFlags(t *testing.T) {
	t.Parallel()

	c := authBootstrapCmd()
	for _, name := range []string{"firebase-uid", "email", "nombre"} {
		flag := c.Flags().Lookup(name)
		require.NotNil(t, flag, "flag %q must be registered", name)
	}
}

func TestAuthBootstrapCmd_RunE_MissingFlags_ReturnsSentinel(t *testing.T) {
	t.Parallel()

	c := authBootstrapCmd()
	err := c.RunE(c, nil)
	require.ErrorIs(t, err, errBootstrapMissingFlags)
}
