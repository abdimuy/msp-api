// Package httptesting hosts reusable test doubles for HTTP composition
// tests — tests that wire the real chi router + middleware chain and only
// stub at the outermost adapter boundary (Firebase, Microsip, persistence).
//
// The fakes here are intentionally shared so each new composition test does
// not re-invent its own FakeFirebase / FakeUsuarioRepo / in-memory
// idempotency store. The first such test lived in
// internal/platform/failedintent/http/e2e_test.go; everything reusable was
// lifted into this package.
//
// Scope: only inbound adapters and ports the *chain* depends on. Anything
// specific to a single module's wiring (intent store, dispatcher, stub
// handlers) stays in that module's test file.
package httptesting

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

// ── FakeFirebase ─────────────────────────────────────────────────────────────

// FakeFirebase is an in-memory outbound.FirebaseClient stub. By default it
// returns the same FirebaseToken for every VerifyIDToken call. Tests that
// need per-token behaviour can set Verify directly.
type FakeFirebase struct {
	mu     sync.Mutex
	Token  *outbound.FirebaseToken
	Verify func(token string) (*outbound.FirebaseToken, error)
}

// NewFakeFirebase returns a FakeFirebase that resolves any token to a
// FirebaseToken with the given UID.
func NewFakeFirebase(uid string) *FakeFirebase {
	return &FakeFirebase{Token: &outbound.FirebaseToken{UID: uid}}
}

// VerifyIDToken returns the configured token (or routes through Verify when
// set). The fake ignores the token *content* and accepts any non-empty
// value — composition tests use a fixed "e2e-token" Bearer string.
func (f *FakeFirebase) VerifyIDToken(_ context.Context, token string) (*outbound.FirebaseToken, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.Verify != nil {
		return f.Verify(token)
	}
	return f.Token, nil
}

// DisableUser is a no-op; composition tests don't exercise disable flows.
func (f *FakeFirebase) DisableUser(context.Context, string) error { return nil }

// EnableUser is a no-op; composition tests don't exercise enable flows.
func (f *FakeFirebase) EnableUser(context.Context, string) error { return nil }

// ── FakeUsuarioRepo ──────────────────────────────────────────────────────────

// FakeUsuarioRepo is an in-memory outbound.UsuarioRepo for composition
// tests. It supports multiple usuarios, configurable permissions per
// usuario, and indexes by ID + Firebase UID + email.
//
// The repo is intentionally permissive: writes (Save/Update/AsignarRol/
// RevocarRol) are no-ops because composition tests assert middleware/route
// behaviour, not persistence. If a test needs to assert a write happened
// it should use a different fake.
type FakeUsuarioRepo struct {
	mu        sync.Mutex
	byID      map[uuid.UUID]*authdomain.Usuario
	byFB      map[string]uuid.UUID
	byEmail   map[string]uuid.UUID
	perms     map[uuid.UUID][]authdomain.Permission
	listOrder []uuid.UUID
}

// NewFakeUsuarioRepo returns an empty repo. Use AddUsuario to populate.
func NewFakeUsuarioRepo() *FakeUsuarioRepo {
	return &FakeUsuarioRepo{
		byID:    map[uuid.UUID]*authdomain.Usuario{},
		byFB:    map[string]uuid.UUID{},
		byEmail: map[string]uuid.UUID{},
		perms:   map[uuid.UUID][]authdomain.Permission{},
	}
}

// AddUsuarioParams describes a usuario to plant in the repo.
type AddUsuarioParams struct {
	ID          uuid.UUID
	FirebaseUID string
	Email       string
	Nombre      string
	Activo      bool
	Permissions []authdomain.Permission
}

// AddUsuario hydrates and stores a usuario. Defaults: Activo=true, Email
// "tester+{shortID}@example.invalid", Nombre "E2E Tester". Returns the
// hydrated Usuario so the test can reference its VO output.
func (r *FakeUsuarioRepo) AddUsuario(p AddUsuarioParams) *authdomain.Usuario {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	if p.Email == "" {
		p.Email = "tester+" + p.ID.String()[:8] + "@example.invalid"
	}
	if p.Nombre == "" {
		p.Nombre = "E2E Tester"
	}
	now := time.Now().UTC()
	u := authdomain.HydrateUsuario(authdomain.HydrateUsuarioParams{
		ID:          p.ID,
		FirebaseUID: authdomain.HydrateFirebaseUID(p.FirebaseUID),
		Email:       authdomain.HydrateEmail(p.Email),
		Nombre:      authdomain.HydrateNombre(p.Nombre),
		Activo:      p.Activo,
		CreatedAt:   now,
		UpdatedAt:   now,
		CreatedBy:   p.ID,
		UpdatedBy:   p.ID,
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID[p.ID] = u
	if p.FirebaseUID != "" {
		r.byFB[p.FirebaseUID] = p.ID
	}
	if p.Email != "" {
		r.byEmail[p.Email] = p.ID
	}
	r.perms[p.ID] = append([]authdomain.Permission(nil), p.Permissions...)
	r.listOrder = append(r.listOrder, p.ID)
	return u
}

// SetPermissions replaces the permission set for a usuario; useful for
// "with vs. without permission" composition tests.
func (r *FakeUsuarioRepo) SetPermissions(id uuid.UUID, perms []authdomain.Permission) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.perms[id] = append([]authdomain.Permission(nil), perms...)
}

// Save is a no-op.
func (r *FakeUsuarioRepo) Save(context.Context, *authdomain.Usuario) error { return nil }

// Update is a no-op.
func (r *FakeUsuarioRepo) Update(context.Context, *authdomain.Usuario) error { return nil }

// FindByID returns the usuario with the given ID or ErrUsuarioNotFound.
func (r *FakeUsuarioRepo) FindByID(_ context.Context, id uuid.UUID) (*authdomain.Usuario, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, authdomain.ErrUsuarioNotFound
	}
	return u, nil
}

// FindByFirebaseUID looks up by Firebase UID.
func (r *FakeUsuarioRepo) FindByFirebaseUID(_ context.Context, fuid string) (*authdomain.Usuario, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byFB[fuid]
	if !ok {
		return nil, authdomain.ErrUsuarioNotFound
	}
	return r.byID[id], nil
}

// FindByEmail looks up by email.
func (r *FakeUsuarioRepo) FindByEmail(_ context.Context, email string) (*authdomain.Usuario, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byEmail[email]
	if !ok {
		return nil, authdomain.ErrUsuarioNotFound
	}
	return r.byID[id], nil
}

// List returns all planted usuarios (composition tests typically only need
// a degenerate listing).
func (r *FakeUsuarioRepo) List(_ context.Context, _ outbound.ListParams) (outbound.Page[*authdomain.Usuario], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*authdomain.Usuario, 0, len(r.listOrder))
	for _, id := range r.listOrder {
		items = append(items, r.byID[id])
	}
	return outbound.Page[*authdomain.Usuario]{Items: items}, nil
}

// AsignarRol is a no-op.
func (r *FakeUsuarioRepo) AsignarRol(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) error {
	return nil
}

// RevocarRol is a no-op.
func (r *FakeUsuarioRepo) RevocarRol(context.Context, uuid.UUID, uuid.UUID) error { return nil }

// PermisosFor returns the configured permission slice for the usuario, or
// an empty slice if none configured.
func (r *FakeUsuarioRepo) PermisosFor(_ context.Context, id uuid.UUID) ([]authdomain.Permission, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	perms := r.perms[id]
	out := make([]authdomain.Permission, len(perms))
	copy(out, perms)
	return out, nil
}

// RolesFor returns nil; composition tests don't exercise role listing.
func (r *FakeUsuarioRepo) RolesFor(context.Context, uuid.UUID) ([]*authdomain.Rol, error) {
	return nil, nil
}

// ── InMemoryIdempotencyStore ─────────────────────────────────────────────────

// InMemoryIdempotencyStore is a thread-safe map-backed idempotency.Store
// for composition tests. It honours the *contract* (Get returns (nil,nil)
// on miss; Save is last-writer-wins) but skips persistence concerns like
// expiry filtering — tests don't need TTL.
type InMemoryIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]*idempotency.Record
}

// NewInMemoryIdempotencyStore returns an empty store.
func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{records: map[string]*idempotency.Record{}}
}

// Get returns the stored record for the key or (nil, nil) on miss.
func (s *InMemoryIdempotencyStore) Get(_ context.Context, key string) (*idempotency.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, nil //nolint:nilnil // contract
	}
	return rec, nil
}

// Save stores a record. Last writer wins; matches the contract the
// production store implements via UPSERT.
func (s *InMemoryIdempotencyStore) Save(_ context.Context, rec idempotency.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.Key] = &rec
	return nil
}

// ── NewE2ERequest ────────────────────────────────────────────────────────────

// E2ERequestOpt customises a request built by NewE2ERequest.
type E2ERequestOpt func(*http.Request)

// WithBearer sets the Authorization header to "Bearer <token>". Default is
// "Bearer e2e-token". Use this only when the test needs a specific token.
func WithBearer(token string) E2ERequestOpt {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+token) }
}

// NoBearer removes the Authorization header so the request looks
// anonymous — useful for endpoints that the chain leaves unauthenticated
// (e.g. POST /auth/login).
func NoBearer() E2ERequestOpt {
	return func(r *http.Request) { r.Header.Del("Authorization") }
}

// WithIdempotencyKey sets the Idempotency-Key header.
func WithIdempotencyKey(key string) E2ERequestOpt {
	return func(r *http.Request) { r.Header.Set("Idempotency-Key", key) }
}

// WithHeader sets an arbitrary header.
func WithHeader(name, value string) E2ERequestOpt {
	return func(r *http.Request) { r.Header.Set(name, value) }
}

// NewE2ERequest builds an httptest request with sensible composition-test
// defaults:
//
//   - Authorization: Bearer e2e-token (override with WithBearer / NoBearer)
//   - Content-Type: application/json when body is non-empty
//
// The body is taken as a string for ergonomic JSON literals; pass "" for
// no body. The request carries context.Background() so the standard chi
// router context can be attached as the test exercises the chain.
func NewE2ERequest(method, path, body string, opts ...E2ERequestOpt) *http.Request {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequestWithContext(context.Background(), method, path, rdr)
	req.Header.Set("Authorization", "Bearer e2e-token")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, opt := range opts {
		opt(req)
	}
	return req
}
