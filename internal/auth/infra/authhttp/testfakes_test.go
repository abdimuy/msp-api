package authhttp

import (
	"context"
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

// ─── fakeUsuarioRepo ────────────────────────────────────────────────────────

type fakeUsuarioRepo struct {
	mu        sync.Mutex
	ByID      map[uuid.UUID]*domain.Usuario
	ByFUID    map[string]*domain.Usuario
	ByEmail   map[string]*domain.Usuario
	RoleLinks map[uuid.UUID]map[uuid.UUID]struct{}
	Permisos  map[uuid.UUID][]domain.Permission
}

func newFakeUsuarioRepo() *fakeUsuarioRepo {
	return &fakeUsuarioRepo{
		ByID:      map[uuid.UUID]*domain.Usuario{},
		ByFUID:    map[string]*domain.Usuario{},
		ByEmail:   map[string]*domain.Usuario{},
		RoleLinks: map[uuid.UUID]map[uuid.UUID]struct{}{},
		Permisos:  map[uuid.UUID][]domain.Permission{},
	}
}

func (f *fakeUsuarioRepo) Save(_ context.Context, u *domain.Usuario) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.ByFUID[u.FirebaseUID().Value()]; ok {
		return domain.ErrUsuarioYaExiste
	}
	if _, ok := f.ByEmail[u.Email().Value()]; ok {
		return domain.ErrUsuarioYaExiste
	}
	f.ByID[u.ID()] = u
	f.ByFUID[u.FirebaseUID().Value()] = u
	f.ByEmail[u.Email().Value()] = u
	return nil
}

func (f *fakeUsuarioRepo) Update(_ context.Context, u *domain.Usuario) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing, ok := f.ByID[u.ID()]
	if !ok {
		return domain.ErrUsuarioNotFound
	}
	delete(f.ByFUID, existing.FirebaseUID().Value())
	delete(f.ByEmail, existing.Email().Value())
	f.ByID[u.ID()] = u
	f.ByFUID[u.FirebaseUID().Value()] = u
	f.ByEmail[u.Email().Value()] = u
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

func (f *fakeUsuarioRepo) RolesFor(_ context.Context, _ uuid.UUID) ([]*domain.Rol, error) {
	return nil, nil
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
	return out, nil
}

func (f *fakePermisoRepo) FindOrphans(_ context.Context, _ []domain.Permission) ([]domain.Permission, error) {
	return nil, nil
}
