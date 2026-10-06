package app_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type fakeTxRunner struct {
	mu       sync.Mutex
	inTx     bool
	calls    int
	errOnRun error
}

func (f *fakeTxRunner) RunInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	f.calls++
	f.inTx = true
	errOnRun := f.errOnRun
	f.mu.Unlock()
	if errOnRun != nil {
		return errOnRun
	}
	err := fn(ctx)
	f.mu.Lock()
	f.inTx = false
	f.mu.Unlock()
	return err
}

// enTx reports whether a transaction is currently open.
func (f *fakeTxRunner) enTx() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inTx
}

type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time {
	if f.now.IsZero() {
		return time.Now().UTC()
	}
	return f.now
}

type fakeIDGen struct {
	ids []uuid.UUID
	i   int
	mu  sync.Mutex
}

func (f *fakeIDGen) Nuevo() uuid.UUID {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.i < len(f.ids) {
		id := f.ids[f.i]
		f.i++
		return id
	}
	return uuid.New()
}

type fakeIdentity struct {
	usuario outbound.Usuario
	denegar *domain.Permiso
	errUser error
	errPerm error
}

func (f *fakeIdentity) UsuarioActual(ctx context.Context) (outbound.Usuario, error) {
	if f.errUser != nil {
		return outbound.Usuario{}, f.errUser
	}
	return f.usuario, nil
}

func (f *fakeIdentity) TienePermiso(ctx context.Context, p domain.Permiso) (bool, error) {
	if f.errPerm != nil {
		return false, f.errPerm
	}
	if f.denegar != nil && *f.denegar == p {
		return false, nil
	}
	return true, nil
}

// seedFolio builds a real aggregate with the domain and stores it in the fake
// repo, so the commands under test read a genuine entity rather than a stub.
func seedFolio(t *testing.T, repo *fakeGarantiaRepo, now time.Time, nArts int) *domain.Garantia {
	t.Helper()
	folio, err := domain.NewFolio(2001 + repo.seeded)
	repo.seeded++
	if err != nil {
		t.Fatalf("NewFolio: %v", err)
	}
	g, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
		Folio:       folio,
		Origen:      domain.OrigenFolioPiso,
		Description: "silla de oficina desarmada",
		AbiertoPor:  "Juan",
		Now:         now,
		Actor: domain.ActorParams{
			Usuario:           "Juan",
			ClaveIdempotencia: "seed-" + folio.String(),
			DeviceCreatedAt:   now,
		},
	})
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	for i := range nArts {
		clave := string(rune('A' + i))
		if err := g.AgregarArticulo(domain.AgregarArticuloParams{
			Clave:       clave,
			Description: "articulo " + clave,
		}, domain.ActorParams{
			Usuario:           "Juan",
			ClaveIdempotencia: "seed-art-" + clave,
			DeviceCreatedAt:   now,
		}, now); err != nil {
			t.Fatalf("AgregarArticulo: %v", err)
		}
	}
	repo.obtained[g.ID()] = g
	repo.forUpdate[g.ID()] = g
	repo.byFolio[g.Folio()] = g
	return g
}

// firstArticuloID returns the id of the first article of the folio.
func firstArticuloID(t *testing.T, g *domain.Garantia) uuid.UUID {
	t.Helper()
	for a := range g.Articulos() {
		return a.ID()
	}
	t.Fatal("folio sin articulos")
	return uuid.Nil
}

// hydrateEvento builds a persisted-shape event for the idempotency fake.
func hydrateEvento(garantiaID uuid.UUID, clave string, now time.Time) *domain.Evento {
	return domain.HydrateEvento(domain.HydrateEventoParams{
		ID:                uuid.New(),
		GarantiaID:        garantiaID,
		Tipo:              domain.TipoEventoArticuloAgregado,
		Usuario:           "Juan",
		CreatedAt:         now,
		DeviceCreatedAt:   now,
		ClaveIdempotencia: clave,
	})
}

// uuidAleatorio returns an id no folio will ever carry.
func uuidAleatorio() uuid.UUID {
	return uuid.New()
}
