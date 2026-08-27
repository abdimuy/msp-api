package authhttp

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// ─── noopIdempotencyStore ──────────────────────────────────────────────────
//
// noopIdempotencyStore satisfies idempotency.Store without persisting anything.
// HTTP handler tests in this package don't exercise the replay path — they
// just need a non-nil Store so MountRouter can install the middleware.

type noopIdempotencyStore struct{}

func (noopIdempotencyStore) Get(_ context.Context, _ string) (*idempotency.Record, error) {
	return nil, nil //nolint:nilnil // (nil, nil) means "not found", matches Store contract
}

func (noopIdempotencyStore) Save(_ context.Context, _ idempotency.Record) error { return nil }

// newNoopIdempotencyStore returns a Store that always reports cache misses.
func newNoopIdempotencyStore() idempotency.Store { return noopIdempotencyStore{} }

// ─── fixedClock ─────────────────────────────────────────────────────────────

type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

// ─── fakeOutbox ─────────────────────────────────────────────────────────────

type fakeOutbox struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeOutbox) Enqueue(_ context.Context, _ string, _ uuid.UUID, _ string, _ any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return nil
}

// ─── fakeFirebase ───────────────────────────────────────────────────────────

type fakeFirebase struct {
	mu    sync.Mutex
	Token *outbound.FirebaseToken
	Err   error
}

func (f *fakeFirebase) VerifyIDToken(_ context.Context, _ string) (*outbound.FirebaseToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Token, nil
}

func (f *fakeFirebase) DisableUser(_ context.Context, _ string) error { return nil }
func (f *fakeFirebase) EnableUser(_ context.Context, _ string) error  { return nil }

// ─── fakeUsuarioRepo ────────────────────────────────────────────────────────

type fakeUsuarioRepo struct {
	mu      sync.Mutex
	ByID    map[uuid.UUID]*domain.Usuario
	ByFUID  map[string]*domain.Usuario
	ByEmail map[string]*domain.Usuario
	// indexed records, per row id, the keys the row is CURRENTLY indexed
	// under. Needed because this fake stores (and returns) the entity
	// POINTER: by the time Update runs, the caller already mutated the object
	// the map holds, so the "old" email/uid are unrecoverable from it. Without
	// this snapshot a soft delete leaves the row reachable by its original
	// email and firebase_uid, while Firebird frees both UNIQUE slots.
	indexed   map[uuid.UUID]usuarioKeys
	RoleLinks map[uuid.UUID]map[uuid.UUID]struct{}
	Permisos  map[uuid.UUID][]domain.Permission
	// Roles resolves rol IDs (as tracked in RoleLinks) into full *domain.Rol
	// values for RolesFor. Wired by newTestRig after both fakes exist —
	// fakeUsuarioRepo cannot own the canonical Rol objects itself since
	// fakeRolRepo is a sibling fake, not a dependency.
	Roles *fakeRolRepo
}

func newFakeUsuarioRepo() *fakeUsuarioRepo {
	return &fakeUsuarioRepo{
		ByID:      map[uuid.UUID]*domain.Usuario{},
		ByFUID:    map[string]*domain.Usuario{},
		ByEmail:   map[string]*domain.Usuario{},
		indexed:   map[uuid.UUID]usuarioKeys{},
		RoleLinks: map[uuid.UUID]map[uuid.UUID]struct{}{},
		Permisos:  map[uuid.UUID][]domain.Permission{},
	}
}

func (f *fakeUsuarioRepo) Save(_ context.Context, u *domain.Usuario) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !u.FirebaseUID().IsZero() {
		if _, ok := f.ByFUID[u.FirebaseUID().Value()]; ok {
			return domain.ErrUsuarioYaExiste
		}
	}
	if _, ok := f.ByEmail[u.Email().Value()]; ok {
		return domain.ErrUsuarioYaExiste
	}
	f.ByID[u.ID()] = u
	f.reindex(u)
	return nil
}

// usuarioKeys is the pair of UNIQUE keys a row is indexed under.
type usuarioKeys struct {
	email string
	fuid  string
}

// reindex drops whatever keys u was last indexed under and installs its
// current ones. Callers hold f.mu.
func (f *fakeUsuarioRepo) reindex(u *domain.Usuario) {
	if prev, ok := f.indexed[u.ID()]; ok {
		delete(f.ByEmail, prev.email)
		if prev.fuid != "" {
			delete(f.ByFUID, prev.fuid)
		}
	}
	keys := usuarioKeys{email: u.Email().Value()}
	f.ByEmail[keys.email] = u
	if !u.FirebaseUID().IsZero() {
		keys.fuid = u.FirebaseUID().Value()
		f.ByFUID[keys.fuid] = u
	}
	f.indexed[u.ID()] = keys
}

// Update mirrors Firebird's UPDATE semantics, UNIQUE indexes included: a
// key already owned by a DIFFERENT row is rejected with
// domain.ErrUsuarioYaExiste (409) instead of being silently stolen.
func (f *fakeUsuarioRepo) Update(_ context.Context, u *domain.Usuario) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ByID[u.ID()]; !ok {
		return domain.ErrUsuarioNotFound
	}
	if !u.FirebaseUID().IsZero() {
		if owner, taken := f.ByFUID[u.FirebaseUID().Value()]; taken && owner.ID() != u.ID() {
			return domain.ErrUsuarioYaExiste
		}
	}
	if owner, taken := f.ByEmail[u.Email().Value()]; taken && owner.ID() != u.ID() {
		return domain.ErrUsuarioYaExiste
	}
	f.ByID[u.ID()] = u
	f.reindex(u)
	return nil
}

func (f *fakeUsuarioRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Usuario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.ByID[id]
	if !ok {
		return nil, domain.ErrUsuarioNotFound
	}
	return u, nil
}

func (f *fakeUsuarioRepo) FindByFirebaseUID(_ context.Context, fuid string) (*domain.Usuario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.ByFUID[fuid]
	if !ok {
		return nil, domain.ErrUsuarioNotFound
	}
	return u, nil
}

func (f *fakeUsuarioRepo) FindByEmail(_ context.Context, email string) (*domain.Usuario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.ByEmail[email]
	if !ok {
		return nil, domain.ErrUsuarioNotFound
	}
	return u, nil
}

func (f *fakeUsuarioRepo) List(_ context.Context, p outbound.ListParams) (outbound.Page[*domain.Usuario], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]*domain.Usuario, 0, len(f.ByID))
	for _, u := range f.ByID {
		items = append(items, u)
	}
	if p.PageSize > 0 && len(items) > p.PageSize {
		items = items[:p.PageSize]
	}
	return outbound.Page[*domain.Usuario]{Items: items}, nil
}

func (f *fakeUsuarioRepo) AsignarRol(_ context.Context, usuarioID, rolID, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set, ok := f.RoleLinks[usuarioID]
	if !ok {
		set = map[uuid.UUID]struct{}{}
		f.RoleLinks[usuarioID] = set
	}
	set[rolID] = struct{}{}
	return nil
}

func (f *fakeUsuarioRepo) RevocarRol(_ context.Context, usuarioID, rolID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if set, ok := f.RoleLinks[usuarioID]; ok {
		delete(set, rolID)
	}
	return nil
}

func (f *fakeUsuarioRepo) PermisosFor(_ context.Context, usuarioID uuid.UUID) ([]domain.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Permisos[usuarioID], nil
}

func (f *fakeUsuarioRepo) RolesFor(_ context.Context, usuarioID uuid.UUID) ([]*domain.Rol, error) {
	f.mu.Lock()
	rolIDs := make([]uuid.UUID, 0, len(f.RoleLinks[usuarioID]))
	for rolID := range f.RoleLinks[usuarioID] {
		rolIDs = append(rolIDs, rolID)
	}
	roles := f.Roles
	f.mu.Unlock()

	if roles == nil {
		return nil, nil
	}
	out := make([]*domain.Rol, 0, len(rolIDs))
	for _, rolID := range rolIDs {
		if r, err := roles.FindByID(context.Background(), rolID); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

// ─── fakeRolRepo ────────────────────────────────────────────────────────────

type fakeRolRepo struct {
	mu       sync.Mutex
	ByID     map[uuid.UUID]*domain.Rol
	ByNombre map[string]*domain.Rol
	Perms    map[uuid.UUID]map[domain.Permission]struct{}
}

func newFakeRolRepo() *fakeRolRepo {
	return &fakeRolRepo{
		ByID:     map[uuid.UUID]*domain.Rol{},
		ByNombre: map[string]*domain.Rol{},
		Perms:    map[uuid.UUID]map[domain.Permission]struct{}{},
	}
}

func (f *fakeRolRepo) Save(_ context.Context, r *domain.Rol) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ByNombre[r.Nombre()]; ok {
		return domain.ErrRolYaExiste
	}
	f.ByID[r.ID()] = r
	f.ByNombre[r.Nombre()] = r
	return nil
}

func (f *fakeRolRepo) Update(_ context.Context, r *domain.Rol) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.ByID[r.ID()]
	if !ok {
		return domain.ErrRolNotFound
	}
	delete(f.ByNombre, existing.Nombre())
	f.ByID[r.ID()] = r
	f.ByNombre[r.Nombre()] = r
	return nil
}

func (f *fakeRolRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Rol, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.ByID[id]
	if !ok {
		return nil, domain.ErrRolNotFound
	}
	return r, nil
}

func (f *fakeRolRepo) FindByNombre(_ context.Context, nombre string) (*domain.Rol, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.ByNombre[nombre]
	if !ok {
		return nil, domain.ErrRolNotFound
	}
	return r, nil
}

func (f *fakeRolRepo) List(_ context.Context, p outbound.ListParams) (outbound.Page[*domain.Rol], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := make([]*domain.Rol, 0, len(f.ByID))
	for _, r := range f.ByID {
		items = append(items, r)
	}
	if p.PageSize > 0 && len(items) > p.PageSize {
		items = items[:p.PageSize]
	}
	return outbound.Page[*domain.Rol]{Items: items}, nil
}

func (f *fakeRolRepo) UpsertInmutableByName(_ context.Context, r *domain.Rol) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ByNombre[r.Nombre()]; ok {
		return nil
	}
	f.ByID[r.ID()] = r
	f.ByNombre[r.Nombre()] = r
	return nil
}

func (f *fakeRolRepo) AsignarPermiso(_ context.Context, rolID uuid.UUID, codigo domain.Permission, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set, ok := f.Perms[rolID]
	if !ok {
		set = map[domain.Permission]struct{}{}
		f.Perms[rolID] = set
	}
	set[codigo] = struct{}{}
	return nil
}

func (f *fakeRolRepo) RevocarPermiso(_ context.Context, rolID uuid.UUID, codigo domain.Permission) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if set, ok := f.Perms[rolID]; ok {
		delete(set, codigo)
	}
	return nil
}

func (f *fakeRolRepo) SyncPermisos(_ context.Context, rolID uuid.UUID, codigos []domain.Permission, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := map[domain.Permission]struct{}{}
	for _, c := range codigos {
		set[c] = struct{}{}
	}
	f.Perms[rolID] = set
	return nil
}

func (f *fakeRolRepo) PermisosFor(_ context.Context, rolID uuid.UUID) ([]domain.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := f.Perms[rolID]
	out := make([]domain.Permission, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	return out, nil
}

// ─── fakePermisoRepo ────────────────────────────────────────────────────────

type fakePermisoRepo struct {
	mu     sync.Mutex
	ByCode map[domain.Permission]domain.PermissionMeta
}

func newFakePermisoRepo() *fakePermisoRepo {
	return &fakePermisoRepo{ByCode: map[domain.Permission]domain.PermissionMeta{}}
}

func (f *fakePermisoRepo) UpsertCatalog(_ context.Context, perms []domain.PermissionMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range perms {
		f.ByCode[p.Code] = p
	}
	return nil
}

func (f *fakePermisoRepo) FindByCodigo(_ context.Context, codigo domain.Permission) (*domain.Permiso, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.ByCode[codigo]
	if !ok {
		return nil, domain.ErrPermisoNotFound
	}
	p := domain.HydratePermiso(m.Code, m.Description, m.Categoria)
	return &p, nil
}

func (f *fakePermisoRepo) FindAll(_ context.Context) ([]*domain.Permiso, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*domain.Permiso, 0, len(f.ByCode))
	for _, m := range f.ByCode {
		p := domain.HydratePermiso(m.Code, m.Description, m.Categoria)
		out = append(out, &p)
	}
	// Mirror the production repo's ORDER BY CODIGO so callers (e.g.
	// Service.enrichPermisos) see the same deterministic ordering in tests
	// as in Firebird.
	sort.Slice(out, func(i, j int) bool { return out[i].Codigo() < out[j].Codigo() })
	return out, nil
}

func (f *fakePermisoRepo) FindOrphans(_ context.Context, _ []domain.Permission) ([]domain.Permission, error) {
	return nil, nil
}
