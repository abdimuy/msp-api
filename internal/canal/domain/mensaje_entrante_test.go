package domain_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

func fixedNow() time.Time {
	return time.Date(2026, 8, 31, 10, 30, 0, 0, time.UTC)
}

func validParams() domain.NewMensajeEntranteParams {
	now := fixedNow()
	return domain.NewMensajeEntranteParams{
		Wamid:         "wamid.HBgLMTIzNDU2Nzg5MDAVAgARGBI5QTNDQTVCM0Q0RUQ2QTIzRDQA",
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "text",
		Contenido:     "Hola, ¿tienen refrigeradores en existencia?",
		TimestampMeta: now.Add(-2 * time.Second),
		RecibidoEn:    now,
	}
}

func TestNewMensajeEntrante_HappyPath(t *testing.T) {
	t.Parallel()
	p := validParams()
	m, err := domain.NewMensajeEntrante(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.ID() == uuid.Nil {
		t.Fatal("expected a generated non-nil id")
	}
	if m.Wamid() != p.Wamid {
		t.Errorf("Wamid = %q, want %q", m.Wamid(), p.Wamid)
	}
	if m.Remitente() != p.Remitente {
		t.Errorf("Remitente = %q, want %q", m.Remitente(), p.Remitente)
	}
	if m.PhoneNumberID() != p.PhoneNumberID {
		t.Errorf("PhoneNumberID = %q, want %q", m.PhoneNumberID(), p.PhoneNumberID)
	}
	if m.Tipo() != p.Tipo {
		t.Errorf("Tipo = %q, want %q", m.Tipo(), p.Tipo)
	}
	if m.Contenido() != p.Contenido {
		t.Errorf("Contenido = %q, want %q", m.Contenido(), p.Contenido)
	}
	if !m.TimestampMeta().Equal(p.TimestampMeta) {
		t.Errorf("TimestampMeta = %v, want %v", m.TimestampMeta(), p.TimestampMeta)
	}
	if !m.RecibidoEn().Equal(p.RecibidoEn) {
		t.Errorf("RecibidoEn = %v, want %v", m.RecibidoEn(), p.RecibidoEn)
	}
	if m.Estado() != domain.EstadoReenvioPendiente {
		t.Errorf("Estado = %q, want pendiente", m.Estado())
	}
	if m.MotivoFallo() != "" {
		t.Errorf("MotivoFallo should be empty on creation, got %q", m.MotivoFallo())
	}
	if !m.CreatedAt().Equal(p.RecibidoEn) {
		t.Errorf("CreatedAt = %v, want %v", m.CreatedAt(), p.RecibidoEn)
	}
	if !m.UpdatedAt().Equal(p.RecibidoEn) {
		t.Errorf("UpdatedAt = %v, want %v", m.UpdatedAt(), p.RecibidoEn)
	}

	evs := m.PendingEvents()
	if len(evs) != 1 {
		t.Fatalf("expected exactly 1 pending event, got %d", len(evs))
	}
	if evs[0].EventType() != domain.EventTypeMensajeEntranteRecibido {
		t.Errorf("event type = %q, want %q", evs[0].EventType(), domain.EventTypeMensajeEntranteRecibido)
	}
	if evs[0].AggregateID() != m.ID() {
		t.Errorf("event AggregateID = %v, want %v", evs[0].AggregateID(), m.ID())
	}
	if !evs[0].OccurredAt().Equal(p.RecibidoEn) {
		t.Errorf("event OccurredAt = %v, want %v", evs[0].OccurredAt(), p.RecibidoEn)
	}
	payload := evs[0].Payload()
	if payload["wamid"] != p.Wamid || payload["remitente"] != p.Remitente ||
		payload["phone_number_id"] != p.PhoneNumberID || payload["tipo"] != p.Tipo {
		t.Errorf("unexpected payload: %+v", payload)
	}
}

func TestNewMensajeEntrante_TrimsAndNormalizesText(t *testing.T) {
	t.Parallel()
	p := validParams()
	p.Wamid = "  " + p.Wamid + "  "
	p.Remitente = "  " + p.Remitente + " "
	p.Contenido = "  con espacios  "
	m, err := domain.NewMensajeEntrante(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Wamid() != strings.TrimSpace(p.Wamid) {
		t.Errorf("Wamid not trimmed: %q", m.Wamid())
	}
	if m.Remitente() != strings.TrimSpace(p.Remitente) {
		t.Errorf("Remitente not trimmed: %q", m.Remitente())
	}
	if m.Contenido() != "con espacios" {
		t.Errorf("Contenido not trimmed: %q", m.Contenido())
	}
}

func TestNewMensajeEntrante_AllowsEmptyContenido(t *testing.T) {
	t.Parallel()
	p := validParams()
	p.Contenido = "   "
	m, err := domain.NewMensajeEntrante(p)
	if err != nil {
		t.Fatalf("unexpected error for blank contenido: %v", err)
	}
	if m.Contenido() != "" {
		t.Errorf("Contenido = %q, want empty", m.Contenido())
	}
}

func TestNewMensajeEntrante_RejectsInvalid(t *testing.T) {
	t.Parallel()

	tooLong := func(n int) string { return strings.Repeat("a", n) }

	cases := []struct {
		name    string
		mutate  func(p *domain.NewMensajeEntranteParams)
		wantErr error
	}{
		{
			name:    "wamid vacío",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Wamid = "" },
			wantErr: domain.ErrMensajeEntranteWamidRequerido,
		},
		{
			name:    "wamid solo espacios",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Wamid = "   " },
			wantErr: domain.ErrMensajeEntranteWamidRequerido,
		},
		{
			name:    "wamid demasiado largo",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Wamid = tooLong(256) },
			wantErr: domain.ErrMensajeEntranteWamidDemasiadoLargo,
		},
		{
			name:    "wamid con NUL",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Wamid = "wamid\x00malo" },
			wantErr: domain.ErrMensajeEntranteCaracteresInvalidos,
		},
		{
			name: "wamid con UTF-8 inválido",
			mutate: func(p *domain.NewMensajeEntranteParams) {
				p.Wamid = "wamid" + string([]byte{0xff, 0xfe})
			},
			wantErr: domain.ErrMensajeEntranteCaracteresInvalidos,
		},
		{
			name:    "remitente vacío",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Remitente = "" },
			wantErr: domain.ErrMensajeEntranteRemitenteRequerido,
		},
		{
			name:    "remitente demasiado largo",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Remitente = tooLong(33) },
			wantErr: domain.ErrMensajeEntranteRemitenteDemasiadoLargo,
		},
		{
			name:    "phone_number_id vacío",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.PhoneNumberID = "" },
			wantErr: domain.ErrMensajeEntrantePhoneNumberIDRequerido,
		},
		{
			name:    "phone_number_id demasiado largo",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.PhoneNumberID = tooLong(33) },
			wantErr: domain.ErrMensajeEntrantePhoneNumberIDDemasiadoLargo,
		},
		{
			name:    "tipo vacío",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Tipo = "" },
			wantErr: domain.ErrMensajeEntranteTipoRequerido,
		},
		{
			name:    "tipo demasiado largo",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Tipo = tooLong(65) },
			wantErr: domain.ErrMensajeEntranteTipoDemasiadoLargo,
		},
		{
			name:    "contenido demasiado largo",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Contenido = tooLong(4097) },
			wantErr: domain.ErrMensajeEntranteContenidoDemasiadoLargo,
		},
		{
			name:    "contenido con caracter de control",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.Contenido = "hola\x01mundo" },
			wantErr: domain.ErrMensajeEntranteCaracteresInvalidos,
		},
		{
			name:    "timestamp de meta cero",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.TimestampMeta = time.Time{} },
			wantErr: domain.ErrMensajeEntranteTimestampMetaRequerido,
		},
		{
			name:    "recibido_en cero",
			mutate:  func(p *domain.NewMensajeEntranteParams) { p.RecibidoEn = time.Time{} },
			wantErr: domain.ErrMensajeEntranteRecibidoEnRequerido,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := validParams()
			tc.mutate(&p)
			m, err := domain.NewMensajeEntrante(p)
			if err == nil {
				t.Fatalf("expected error, got nil (m=%+v)", m)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
			if m != nil {
				t.Fatalf("expected nil entity on error, got %+v", m)
			}
		})
	}
}

// TestNewMensajeEntrante_BoundaryLengthAccepted verifies the max-length
// constants are inclusive: a field at exactly the limit is accepted.
func TestNewMensajeEntrante_BoundaryLengthAccepted(t *testing.T) {
	t.Parallel()
	p := validParams()
	p.Wamid = strings.Repeat("a", 255)
	p.Remitente = strings.Repeat("1", 32)
	p.PhoneNumberID = strings.Repeat("2", 32)
	p.Tipo = strings.Repeat("t", 64)
	p.Contenido = strings.Repeat("c", 4096)
	m, err := domain.NewMensajeEntrante(p)
	if err != nil {
		t.Fatalf("unexpected error at boundary length: %v", err)
	}
	if len([]rune(m.Wamid())) != 255 {
		t.Errorf("wamid length = %d, want 255", len([]rune(m.Wamid())))
	}
}

func TestRehydrateMensajeEntrante_RoundTrips(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	id := uuid.New()
	p := domain.RehydrateMensajeEntranteParams{
		ID:            id,
		Wamid:         "wamid.REHYDRATED",
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "image",
		Contenido:     "media-id-abc123",
		TimestampMeta: now.Add(-time.Hour),
		RecibidoEn:    now.Add(-time.Hour).Add(2 * time.Second),
		Estado:        domain.EstadoReenvioFallido,
		MotivoFallo:   "el servidor de la tienda no respondió",
		CreatedAt:     now.Add(-time.Hour),
		UpdatedAt:     now,
	}
	m := domain.RehydrateMensajeEntrante(p)

	if m.ID() != p.ID {
		t.Errorf("ID = %v, want %v", m.ID(), p.ID)
	}
	if m.Wamid() != p.Wamid {
		t.Errorf("Wamid = %q, want %q", m.Wamid(), p.Wamid)
	}
	if m.Estado() != p.Estado {
		t.Errorf("Estado = %q, want %q", m.Estado(), p.Estado)
	}
	if m.MotivoFallo() != p.MotivoFallo {
		t.Errorf("MotivoFallo = %q, want %q", m.MotivoFallo(), p.MotivoFallo)
	}
	if !m.CreatedAt().Equal(p.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", m.CreatedAt(), p.CreatedAt)
	}
	if !m.UpdatedAt().Equal(p.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", m.UpdatedAt(), p.UpdatedAt)
	}
	if evs := m.PendingEvents(); len(evs) != 0 {
		t.Errorf("expected no pending events after rehydrate, got %d", len(evs))
	}
}

func TestMensajeEntrante_MarcarReenviado(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	m.ClearPendingEvents()

	reenviadoEn := now.Add(5 * time.Second)
	if err := m.MarcarReenviado(reenviadoEn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Estado() != domain.EstadoReenvioReenviado {
		t.Errorf("Estado = %q, want reenviado", m.Estado())
	}
	if !m.UpdatedAt().Equal(reenviadoEn) {
		t.Errorf("UpdatedAt = %v, want %v", m.UpdatedAt(), reenviadoEn)
	}
	evs := m.PendingEvents()
	if len(evs) != 1 || evs[0].EventType() != domain.EventTypeMensajeReenviado {
		t.Fatalf("expected exactly one canal.mensaje_reenviado event, got %+v", evs)
	}
	if evs[0].Payload()["wamid"] != m.Wamid() {
		t.Errorf("event payload wamid mismatch: %+v", evs[0].Payload())
	}

	// Terminal: a second MarcarReenviado must fail and not mutate anything.
	beforeUpdatedAt := m.UpdatedAt()
	if err := m.MarcarReenviado(reenviadoEn.Add(time.Second)); !errors.Is(err, domain.ErrMensajeEntranteTransicionInvalida) {
		t.Fatalf("expected ErrMensajeEntranteTransicionInvalida, got %v", err)
	}
	if m.UpdatedAt() != beforeUpdatedAt {
		t.Error("rejected transition mutated UpdatedAt")
	}
}

func TestMensajeEntrante_MarcarFallido(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	m.ClearPendingEvents()

	falloEn := now.Add(3 * time.Second)
	if err := m.MarcarFallido("  el servidor de la tienda respondió 500  ", falloEn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Estado() != domain.EstadoReenvioFallido {
		t.Errorf("Estado = %q, want fallido", m.Estado())
	}
	if m.MotivoFallo() != "el servidor de la tienda respondió 500" {
		t.Errorf("MotivoFallo = %q", m.MotivoFallo())
	}
	evs := m.PendingEvents()
	if len(evs) != 1 || evs[0].EventType() != domain.EventTypeReenvioFallido {
		t.Fatalf("expected exactly one canal.reenvio_fallido event, got %+v", evs)
	}
	if evs[0].Payload()["motivo"] != m.MotivoFallo() {
		t.Errorf("event payload motivo mismatch: %+v", evs[0].Payload())
	}
}

func TestMensajeEntrante_MarcarFallido_RequiresMotivo(t *testing.T) {
	t.Parallel()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	before := m.Estado()

	if err := m.MarcarFallido("   ", fixedNow()); !errors.Is(err, domain.ErrMensajeEntranteMotivoFalloRequerido) {
		t.Fatalf("expected ErrMensajeEntranteMotivoFalloRequerido, got %v", err)
	}
	if m.Estado() != before {
		t.Error("empty motivo must not mutate state")
	}

	tooLong := strings.Repeat("x", 501)
	if err := m.MarcarFallido(tooLong, fixedNow()); !errors.Is(err, domain.ErrMensajeEntranteMotivoFalloDemasiadoLargo) {
		t.Fatalf("expected ErrMensajeEntranteMotivoFalloDemasiadoLargo, got %v", err)
	}
}

func TestMensajeEntrante_MarcarFallido_InvalidTransition(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := m.MarcarReenviado(now); err != nil {
		t.Fatalf("setup MarcarReenviado: %v", err)
	}
	// reenviado is terminal — MarcarFallido must fail even with a valid motivo.
	if err := m.MarcarFallido("motivo válido", now.Add(time.Second)); !errors.Is(err, domain.ErrMensajeEntranteTransicionInvalida) {
		t.Fatalf("expected ErrMensajeEntranteTransicionInvalida, got %v", err)
	}
}

func TestMensajeEntrante_MarcarPendiente_RetryAfterFallo(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := m.MarcarFallido("timeout", now); err != nil {
		t.Fatalf("setup MarcarFallido: %v", err)
	}
	m.ClearPendingEvents()

	retryEn := now.Add(time.Minute)
	if err := m.MarcarPendiente(retryEn); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Estado() != domain.EstadoReenvioPendiente {
		t.Errorf("Estado = %q, want pendiente", m.Estado())
	}
	// The last failure reason is retained as diagnostic history.
	if m.MotivoFallo() != "timeout" {
		t.Errorf("MotivoFallo should be retained across retry, got %q", m.MotivoFallo())
	}
	if evs := m.PendingEvents(); len(evs) != 0 {
		t.Errorf("MarcarPendiente must not buffer an event, got %d", len(evs))
	}

	// From a fresh pendiente entity, retrying immediately is invalid.
	fresh, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup fresh: %v", err)
	}
	if err := fresh.MarcarPendiente(now); !errors.Is(err, domain.ErrMensajeEntranteTransicionInvalida) {
		t.Fatalf("expected ErrMensajeEntranteTransicionInvalida, got %v", err)
	}
}

func TestMensajeEntrante_ClearPendingEvents(t *testing.T) {
	t.Parallel()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if len(m.PendingEvents()) == 0 {
		t.Fatal("expected at least one pending event after construction")
	}
	m.ClearPendingEvents()
	if evs := m.PendingEvents(); len(evs) != 0 {
		t.Fatalf("expected no pending events after clear, got %d", len(evs))
	}
}

// TestMensajeEntrante_PendingEvents_DefensiveCopy verifies mutating the
// returned slice does not affect the entity's internal buffer.
func TestMensajeEntrante_PendingEvents_DefensiveCopy(t *testing.T) {
	t.Parallel()
	m, err := domain.NewMensajeEntrante(validParams())
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	evs := m.PendingEvents()
	evs[0] = nil
	if again := m.PendingEvents(); again[0] == nil {
		t.Fatal("mutating the returned slice affected the entity's internal buffer")
	}
}
