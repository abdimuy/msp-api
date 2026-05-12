package app

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
)

// ─── FixedClock ────────────────────────────────────────────────────────────

// FixedClock is an outbound.Clock that always returns T. Tests use it to
// freeze time so assertions on audit timestamps are deterministic.
type FixedClock struct{ T time.Time }

// Now returns the fixed instant T.
func (c FixedClock) Now() time.Time { return c.T }

// ─── FakeOutbox ────────────────────────────────────────────────────────────

// OutboxCall records a single Enqueue invocation.
type OutboxCall struct {
	Aggregate   string
	AggregateID uuid.UUID
	EventType   string
	Payload     any
}

// FakeOutbox captures every enqueue call. Concurrency-safe.
type FakeOutbox struct {
	mu    sync.Mutex
	Calls []OutboxCall
	Err   error
}

// Enqueue appends the call and returns the configured error.
func (f *FakeOutbox) Enqueue(_ context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, OutboxCall{Aggregate: aggregate, AggregateID: aggregateID, EventType: eventType, Payload: payload})
	return f.Err
}

// EventTypes returns the ordered slice of event types captured so far.
func (f *FakeOutbox) EventTypes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = c.EventType
	}
	return out
}

// ─── FakeFirebaseClient ────────────────────────────────────────────────────

// FakeFirebaseClient returns a pre-configured token and counts invocations.
type FakeFirebaseClient struct {
	mu       sync.Mutex
	Token    *outbound.FirebaseToken
	Err      error
	Verified int
}

// VerifyIDToken records a call and returns the configured token/error.
func (f *FakeFirebaseClient) VerifyIDToken(_ context.Context, _ string) (*outbound.FirebaseToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Verified++
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Token, nil
}

// ─── FakeUsuarioRepo ───────────────────────────────────────────────────────

// FakeUsuarioRepo is an in-memory implementation of outbound.UsuarioRepo.
// All maps are keyed by their natural identifier; the repo never mutates the
// supplied entities except through the Save/Update entry points.
type FakeUsuarioRepo struct {
	mu         sync.Mutex
	ByID       map[uuid.UUID]*domain.Usuario
	ByFUID     map[string]*domain.Usuario
	ByEmail    map[string]*domain.Usuario
	RoleLinks  map[uuid.UUID]map[uuid.UUID]struct{} // usuarioID → set of rolID
	Permisos   map[uuid.UUID][]domain.Permission
	SaveErr    error
	UpdateErr  error
	FindErr    error
	ListErr    error
	AsignarErr error
	RevocarErr error
}

// NewFakeUsuarioRepo constructs an empty repo.
func NewFakeUsuarioRepo() *FakeUsuarioRepo {
	return &FakeUsuarioRepo{
		ByID:      map[uuid.UUID]*domain.Usuario{},
		ByFUID:    map[string]*domain.Usuario{},
		ByEmail:   map[string]*domain.Usuario{},
		RoleLinks: map[uuid.UUID]map[uuid.UUID]struct{}{},
		Permisos:  map[uuid.UUID][]domain.Permission{},
	}
}

// Save adds a new usuario, rejecting collisions on FUID/email.
func (f *FakeUsuarioRepo) Save(_ context.Context, u *domain.Usuario) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		return f.SaveErr
	}
	if _, exists := f.ByFUID[u.FirebaseUID().Value()]; exists {
		return domain.ErrUsuarioYaExiste
	}
	if _, exists := f.ByEmail[u.Email().Value()]; exists {
		return domain.ErrUsuarioYaExiste
	}
	f.ByID[u.ID()] = u
	f.ByFUID[u.FirebaseUID().Value()] = u
	f.ByEmail[u.Email().Value()] = u
	return nil
}

// Update rewrites the indexes to reflect any FUID/email change.
func (f *FakeUsuarioRepo) Update(_ context.Context, u *domain.Usuario) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
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

// FindByID looks up by primary key.
func (f *FakeUsuarioRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Usuario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindErr != nil {
		return nil, f.FindErr
	}
	u, ok := f.ByID[id]
	if !ok {
		return nil, domain.ErrUsuarioNotFound
	}
	return u, nil
}

// FindByFirebaseUID looks up by Firebase uid.
func (f *FakeUsuarioRepo) FindByFirebaseUID(_ context.Context, fuid string) (*domain.Usuario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindErr != nil {
		return nil, f.FindErr
	}
	u, ok := f.ByFUID[fuid]
	if !ok {
		return nil, domain.ErrUsuarioNotFound
	}
	return u, nil
}

// FindByEmail looks up by email.
func (f *FakeUsuarioRepo) FindByEmail(_ context.Context, email string) (*domain.Usuario, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindErr != nil {
		return nil, f.FindErr
	}
	u, ok := f.ByEmail[email]
	if !ok {
		return nil, domain.ErrUsuarioNotFound
	}
	return u, nil
}

// List returns at most p.PageSize usuarios in unspecified order.
func (f *FakeUsuarioRepo) List(_ context.Context, p outbound.ListParams) (outbound.Page[*domain.Usuario], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return outbound.Page[*domain.Usuario]{}, f.ListErr
	}
	items := make([]*domain.Usuario, 0, len(f.ByID))
	for _, u := range f.ByID {
		items = append(items, u)
	}
	if p.PageSize > 0 && len(items) > p.PageSize {
		items = items[:p.PageSize]
	}
	return outbound.Page[*domain.Usuario]{Items: items}, nil
}

// AsignarRol adds a usuario→rol link.
func (f *FakeUsuarioRepo) AsignarRol(_ context.Context, usuarioID, rolID, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AsignarErr != nil {
		return f.AsignarErr
	}
	set, ok := f.RoleLinks[usuarioID]
	if !ok {
		set = map[uuid.UUID]struct{}{}
		f.RoleLinks[usuarioID] = set
	}
	set[rolID] = struct{}{}
	return nil
}

// RevocarRol removes a usuario→rol link.
func (f *FakeUsuarioRepo) RevocarRol(_ context.Context, usuarioID, rolID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.RevocarErr != nil {
		return f.RevocarErr
	}
	if set, ok := f.RoleLinks[usuarioID]; ok {
		delete(set, rolID)
	}
	return nil
}

// PermisosFor returns the configured permission slice.
func (f *FakeUsuarioRepo) PermisosFor(_ context.Context, usuarioID uuid.UUID) ([]domain.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Permisos[usuarioID], nil
}

// RolesFor returns an empty slice; tests that need data may extend this.
func (f *FakeUsuarioRepo) RolesFor(_ context.Context, _ uuid.UUID) ([]*domain.Rol, error) {
	return nil, nil
}

// ─── FakeRolRepo ───────────────────────────────────────────────────────────

// FakeRolRepo is an in-memory implementation of outbound.RolRepo.
type FakeRolRepo struct {
	mu           sync.Mutex
	ByID         map[uuid.UUID]*domain.Rol
	ByNombre     map[string]*domain.Rol
	PermsByRol   map[uuid.UUID]map[domain.Permission]struct{}
	SaveErr      error
	UpdateErr    error
	FindByIDErr  error
	FindByNomErr error
	ListErr      error
	UpsertErr    error
	AsignarErr   error
	RevocarErr   error
	SyncErr      error
	UpsertCalls  int
	SyncCalls    int
}

// NewFakeRolRepo constructs an empty repo.
func NewFakeRolRepo() *FakeRolRepo {
	return &FakeRolRepo{
		ByID:       map[uuid.UUID]*domain.Rol{},
		ByNombre:   map[string]*domain.Rol{},
		PermsByRol: map[uuid.UUID]map[domain.Permission]struct{}{},
	}
}

// Save persists a new rol.
func (f *FakeRolRepo) Save(_ context.Context, r *domain.Rol) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.SaveErr != nil {
		return f.SaveErr
	}
	if _, ok := f.ByNombre[r.Nombre()]; ok {
		return domain.ErrRolYaExiste
	}
	f.ByID[r.ID()] = r
	f.ByNombre[r.Nombre()] = r
	return nil
}

// Update overwrites the indexed rol.
func (f *FakeRolRepo) Update(_ context.Context, r *domain.Rol) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.UpdateErr != nil {
		return f.UpdateErr
	}
	existing, ok := f.ByID[r.ID()]
	if !ok {
		return domain.ErrRolNotFound
	}
	delete(f.ByNombre, existing.Nombre())
	f.ByID[r.ID()] = r
	f.ByNombre[r.Nombre()] = r
	return nil
}

// FindByID looks up by primary key.
func (f *FakeRolRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.Rol, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindByIDErr != nil {
		return nil, f.FindByIDErr
	}
	r, ok := f.ByID[id]
	if !ok {
		return nil, domain.ErrRolNotFound
	}
	return r, nil
}

// FindByNombre looks up by unique name.
func (f *FakeRolRepo) FindByNombre(_ context.Context, nombre string) (*domain.Rol, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindByNomErr != nil {
		return nil, f.FindByNomErr
	}
	r, ok := f.ByNombre[nombre]
	if !ok {
		return nil, domain.ErrRolNotFound
	}
	return r, nil
}

// List returns at most p.PageSize roles.
func (f *FakeRolRepo) List(_ context.Context, p outbound.ListParams) (outbound.Page[*domain.Rol], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return outbound.Page[*domain.Rol]{}, f.ListErr
	}
	items := make([]*domain.Rol, 0, len(f.ByID))
	for _, r := range f.ByID {
		items = append(items, r)
	}
	if p.PageSize > 0 && len(items) > p.PageSize {
		items = items[:p.PageSize]
	}
	return outbound.Page[*domain.Rol]{Items: items}, nil
}

// UpsertInmutableByName creates or updates the rol matched by name.
func (f *FakeRolRepo) UpsertInmutableByName(_ context.Context, r *domain.Rol) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpsertCalls++
	if f.UpsertErr != nil {
		return f.UpsertErr
	}
	if existing, ok := f.ByNombre[r.Nombre()]; ok {
		f.ByID[existing.ID()] = existing
		return nil
	}
	f.ByID[r.ID()] = r
	f.ByNombre[r.Nombre()] = r
	return nil
}

// AsignarPermiso links a code to a rol.
func (f *FakeRolRepo) AsignarPermiso(_ context.Context, rolID uuid.UUID, codigo domain.Permission, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.AsignarErr != nil {
		return f.AsignarErr
	}
	set, ok := f.PermsByRol[rolID]
	if !ok {
		set = map[domain.Permission]struct{}{}
		f.PermsByRol[rolID] = set
	}
	set[codigo] = struct{}{}
	return nil
}

// RevocarPermiso unlinks a code from a rol.
func (f *FakeRolRepo) RevocarPermiso(_ context.Context, rolID uuid.UUID, codigo domain.Permission) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.RevocarErr != nil {
		return f.RevocarErr
	}
	if set, ok := f.PermsByRol[rolID]; ok {
		delete(set, codigo)
	}
	return nil
}

// SyncPermisos replaces the rol's permission set atomically.
func (f *FakeRolRepo) SyncPermisos(_ context.Context, rolID uuid.UUID, codigos []domain.Permission, _ uuid.UUID, _ time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.SyncCalls++
	if f.SyncErr != nil {
		return f.SyncErr
	}
	set := map[domain.Permission]struct{}{}
	for _, c := range codigos {
		set[c] = struct{}{}
	}
	f.PermsByRol[rolID] = set
	return nil
}

// PermisosFor returns the rol's permission codes.
func (f *FakeRolRepo) PermisosFor(_ context.Context, rolID uuid.UUID) ([]domain.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	set := f.PermsByRol[rolID]
	out := make([]domain.Permission, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	return out, nil
}

// ─── FakePermisoRepo ───────────────────────────────────────────────────────

// FakePermisoRepo is an in-memory implementation of outbound.PermisoRepo.
type FakePermisoRepo struct {
	mu             sync.Mutex
	ByCode         map[domain.Permission]domain.PermissionMeta
	Orphans        []domain.Permission
	UpsertErr      error
	FindByCodeErr  error
	FindAllErr     error
	FindOrphansErr error
	UpsertCalls    int
	LastUpsert     []domain.PermissionMeta
}

// NewFakePermisoRepo constructs an empty repo.
func NewFakePermisoRepo() *FakePermisoRepo {
	return &FakePermisoRepo{ByCode: map[domain.Permission]domain.PermissionMeta{}}
}

// UpsertCatalog overwrites the in-memory catalog.
func (f *FakePermisoRepo) UpsertCatalog(_ context.Context, perms []domain.PermissionMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpsertCalls++
	f.LastUpsert = perms
	if f.UpsertErr != nil {
		return f.UpsertErr
	}
	for _, p := range perms {
		f.ByCode[p.Code] = p
	}
	return nil
}

// FindByCodigo returns the stored permiso or ErrPermisoNotFound.
func (f *FakePermisoRepo) FindByCodigo(_ context.Context, codigo domain.Permission) (*domain.Permiso, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindByCodeErr != nil {
		return nil, f.FindByCodeErr
	}
	m, ok := f.ByCode[codigo]
	if !ok {
		return nil, domain.ErrPermisoNotFound
	}
	p := domain.HydratePermiso(m.Code, m.Description, m.Categoria)
	return &p, nil
}

// FindAll returns every cataloged permiso.
func (f *FakePermisoRepo) FindAll(_ context.Context) ([]*domain.Permiso, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindAllErr != nil {
		return nil, f.FindAllErr
	}
	out := make([]*domain.Permiso, 0, len(f.ByCode))
	for _, m := range f.ByCode {
		p := domain.HydratePermiso(m.Code, m.Description, m.Categoria)
		out = append(out, &p)
	}
	return out, nil
}

// FindOrphans returns the configured orphan slice ignoring `known`. Tests
// that need orphan-specific logic should set Orphans explicitly.
func (f *FakePermisoRepo) FindOrphans(_ context.Context, _ []domain.Permission) ([]domain.Permission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.FindOrphansErr != nil {
		return nil, f.FindOrphansErr
	}
	return f.Orphans, nil
}
