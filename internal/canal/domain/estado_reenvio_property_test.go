package domain_test

import (
	"errors"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

// allEstados lists every EstadoReenvio value the generators below draw from.
var allEstados = []domain.EstadoReenvio{
	domain.EstadoReenvioPendiente,
	domain.EstadoReenvioReenviado,
	domain.EstadoReenvioFallido,
}

// estadoGen draws a random known EstadoReenvio.
func estadoGen(t *rapid.T, label string) domain.EstadoReenvio {
	return allEstados[rapid.IntRange(0, len(allEstados)-1).Draw(t, label)]
}

// TestProperty_EstadoReenvio_TransitionMapIsExhaustive verifies, for every
// pair of known states, that CanTransitionTo agrees with a hand-written
// reimplementation of the transition table — i.e. the map in
// estado_reenvio.go is the only source of truth and nothing falls outside
// it silently.
func TestProperty_EstadoReenvio_TransitionMapIsExhaustive(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		from := estadoGen(t, "from")
		to := estadoGen(t, "to")

		want := false
		switch from {
		case domain.EstadoReenvioPendiente:
			want = to == domain.EstadoReenvioReenviado || to == domain.EstadoReenvioFallido
		case domain.EstadoReenvioFallido:
			want = to == domain.EstadoReenvioPendiente
		case domain.EstadoReenvioReenviado:
			want = false
		}

		if got := from.CanTransitionTo(to); got != want {
			t.Fatalf("CanTransitionTo(%s -> %s) = %v, want %v", from, to, got, want)
		}
	})
}

// buildEntranteInEstado constructs a MensajeEntrante and drives it into the
// requested state via the entity's own transition methods, so the property
// test below exercises the real production path rather than poking private
// fields.
func buildEntranteInEstado(t *rapid.T, estado domain.EstadoReenvio, now time.Time) *domain.MensajeEntrante {
	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         "wamid.PROPERTY",
		Remitente:     "5219981234567",
		PhoneNumberID: "1029384756",
		Tipo:          "text",
		Contenido:     "hola",
		TimestampMeta: now,
		RecibidoEn:    now,
	})
	if err != nil {
		t.Fatalf("NewMensajeEntrante: %v", err)
	}
	switch estado {
	case domain.EstadoReenvioPendiente:
		// Already there.
	case domain.EstadoReenvioReenviado:
		if err := m.MarcarReenviado(now); err != nil {
			t.Fatalf("MarcarReenviado setup: %v", err)
		}
	case domain.EstadoReenvioFallido:
		if err := m.MarcarFallido("fallo de preparación", now); err != nil {
			t.Fatalf("MarcarFallido setup: %v", err)
		}
	}
	return m
}

// TestProperty_MensajeEntrante_TransitionsObeyTheMap verifies, for a
// MensajeEntrante starting in any known state, that every transition method
// succeeds exactly when EstadoReenvio.CanTransitionTo says it should, and
// that a rejected call leaves the entity's state completely untouched
// (invariant preserved: no partial transition on failure).
func TestProperty_MensajeEntrante_TransitionsObeyTheMap(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
		from := estadoGen(t, "from")
		target := estadoGen(t, "target")

		m := buildEntranteInEstado(t, from, now)
		before := m.Estado()
		beforeUpdatedAt := m.UpdatedAt()

		var err error
		switch target {
		case domain.EstadoReenvioReenviado:
			err = m.MarcarReenviado(now.Add(time.Minute))
		case domain.EstadoReenvioFallido:
			err = m.MarcarFallido("algo salió mal", now.Add(time.Minute))
		case domain.EstadoReenvioPendiente:
			err = m.MarcarPendiente(now.Add(time.Minute))
		}

		if before.CanTransitionTo(target) {
			assertTransitionSucceeded(t, m, err, before, target, beforeUpdatedAt)
		} else {
			assertTransitionRejected(t, m, err, before, target, beforeUpdatedAt)
		}
	})
}

// assertTransitionSucceeded checks the postconditions of an allowed
// transition: no error, the entity landed in target, and UpdatedAt advanced.
func assertTransitionSucceeded(
	t *rapid.T, m *domain.MensajeEntrante, err error,
	before, target domain.EstadoReenvio, beforeUpdatedAt time.Time,
) {
	if err != nil {
		t.Fatalf("expected %s -> %s to succeed, got %v", before, target, err)
	}
	if m.Estado() != target {
		t.Fatalf("expected state %s after transition, got %s", target, m.Estado())
	}
	if !m.UpdatedAt().After(beforeUpdatedAt) {
		t.Fatalf("expected UpdatedAt to advance on a successful transition")
	}
}

// assertTransitionRejected checks the postconditions of a disallowed
// transition: the sentinel error, and no mutation at all — state and
// UpdatedAt both untouched.
func assertTransitionRejected(
	t *rapid.T, m *domain.MensajeEntrante, err error,
	before, target domain.EstadoReenvio, beforeUpdatedAt time.Time,
) {
	if err == nil {
		t.Fatalf("expected %s -> %s to fail, got nil error", before, target)
	}
	if !errors.Is(err, domain.ErrMensajeEntranteTransicionInvalida) {
		t.Fatalf("expected ErrMensajeEntranteTransicionInvalida, got %v", err)
	}
	if m.Estado() != before {
		t.Fatalf("rejected transition mutated state: before=%s after=%s", before, m.Estado())
	}
	if m.UpdatedAt() != beforeUpdatedAt {
		t.Fatalf("rejected transition mutated UpdatedAt")
	}
}
