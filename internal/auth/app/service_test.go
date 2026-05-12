package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/domain"
)

// testHarness aggregates a Service plus the fakes it depends on so tests
// can assert side-effects without rebuilding the wiring on every case.
type testHarness struct {
	svc      *Service
	usuarios *FakeUsuarioRepo
	roles    *FakeRolRepo
	permisos *FakePermisoRepo
	outbox   *FakeOutbox
	firebase *FakeFirebaseClient
	clock    FixedClock
}

// newHarness builds a fresh harness. `seedPermisos` controls whether the
// permission catalog is pre-populated with domain.AllPermissions() — most
// tests want this so AsignarPermisoARol can find the code.
func newHarness(t *testing.T, seedPermisos bool) *testHarness {
	t.Helper()
	clock := FixedClock{T: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)}
	usuarios := NewFakeUsuarioRepo()
	roles := NewFakeRolRepo()
	permisos := NewFakePermisoRepo()
	outbox := &FakeOutbox{}
	firebase := &FakeFirebaseClient{}

	if seedPermisos {
		require.NoError(t, permisos.UpsertCatalog(t.Context(), domain.AllPermissions()))
		// Reset the counter so tests that assert UpsertCalls start at zero.
		permisos.UpsertCalls = 0
	}

	svc := NewService(usuarios, roles, permisos, clock, outbox, firebase, nil)
	return &testHarness{
		svc:      svc,
		usuarios: usuarios,
		roles:    roles,
		permisos: permisos,
		outbox:   outbox,
		firebase: firebase,
		clock:    clock,
	}
}

// seedUsuario creates a valid usuario, persists it, and returns it.
func (h *testHarness) seedUsuario(t *testing.T) *domain.Usuario {
	t.Helper()
	id := uuid.New()
	fuid, err := domain.NewFirebaseUID("fuid-" + id.String())
	require.NoError(t, err)
	email, err := domain.NewEmail("user-" + id.String() + "@example.com")
	require.NoError(t, err)
	nombre, err := domain.NewNombre("Juan Perez")
	require.NoError(t, err)
	u := domain.NewUsuario(id, fuid, email, nombre, nil, nil, id, h.clock.T)
	require.NoError(t, h.usuarios.Save(t.Context(), u))
	return u
}

// seedRol creates a mutable rol with the given name and persists it.
func (h *testHarness) seedRol(t *testing.T, nombre string) *domain.Rol {
	t.Helper()
	rol, err := domain.NewRol(uuid.New(), nombre, nil, false, uuid.New(), h.clock.T)
	require.NoError(t, err)
	require.NoError(t, h.roles.Save(t.Context(), rol))
	return rol
}

// seedInmutableRol creates an inmutable rol persisted directly.
func (h *testHarness) seedInmutableRol(t *testing.T, nombre string) *domain.Rol {
	t.Helper()
	rol, err := domain.NewRol(uuid.New(), nombre, nil, true, uuid.New(), h.clock.T)
	require.NoError(t, err)
	require.NoError(t, h.roles.Save(t.Context(), rol))
	return rol
}

// TestNewService asserts the constructor wires every collaborator.
func TestNewService(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	require.NotNil(t, h.svc)
	require.NotNil(t, h.svc.usuarios)
	require.NotNil(t, h.svc.roles)
	require.NotNil(t, h.svc.permisos)
	require.NotNil(t, h.svc.outbox)
	require.NotNil(t, h.svc.firebase)
	require.NotNil(t, h.svc.clock)
}
