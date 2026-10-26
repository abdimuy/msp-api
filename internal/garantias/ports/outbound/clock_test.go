package outbound_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

func TestProductionClock_Now(t *testing.T) {
	t.Parallel()
	antes := time.Now().UTC()
	got := outbound.ProductionClock{}.Now()
	despues := time.Now().UTC()

	if got.Location() != time.UTC {
		t.Errorf("Location = %v, want UTC", got.Location())
	}
	if got.Before(antes) || got.After(despues) {
		t.Errorf("Now() = %v, fuera de [%v, %v]", got, antes, despues)
	}
}

func TestUUIDGenerator_Nuevo(t *testing.T) {
	t.Parallel()
	gen := outbound.UUIDGenerator{}

	primero := gen.Nuevo()
	if primero == uuid.Nil {
		t.Error("Nuevo() = uuid.Nil")
	}
	if v := primero.Version(); v != 4 {
		t.Errorf("Version() = %d, want 4", v)
	}

	// Unique across a batch: a generator that repeats would silently collide
	// primary keys, and the Firebird PK constraint would surface it as an
	// opaque constraint violation in production instead of here.
	const n = 1000
	vistos := make(map[uuid.UUID]struct{}, n)
	for range n {
		id := gen.Nuevo()
		if id == uuid.Nil {
			t.Fatal("Nuevo() = uuid.Nil")
		}
		if _, dup := vistos[id]; dup {
			t.Fatalf("Nuevo() repitió %v en %d intentos", id, n)
		}
		vistos[id] = struct{}{}
	}
}

// TestPuertos_CompilanContraLasImplementaciones is a compile-time check that
// the production implementations satisfy the interfaces the app layer will
// depend on. var _ is the assertion; the test body documents why it exists.
func TestPuertos_CompilanContraLasImplementaciones(t *testing.T) {
	t.Parallel()
	var (
		_ outbound.Clock       = outbound.ProductionClock{}
		_ outbound.IDGenerator = outbound.UUIDGenerator{}
	)
}
