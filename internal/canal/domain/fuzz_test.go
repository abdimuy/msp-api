package domain

import (
	"testing"
	"time"
)

// FuzzParseEstadoReenvio exercises ParseEstadoReenvio with arbitrary strings.
// The contract: never panic, and every accepted value is one of the three
// known states and round-trips through String().
func FuzzParseEstadoReenvio(f *testing.F) {
	seeds := []string{"", "pendiente", "reenviado", "fallido", "Pendiente", "PENDIENTE", "pendiente ", "x\x00y"}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		e, err := ParseEstadoReenvio(s)
		if err != nil {
			return
		}
		if !e.IsValid() {
			t.Fatalf("accepted but not valid: %q", s)
		}
		if e.String() != s {
			t.Fatalf("accepted value does not round-trip: in=%q out=%q", s, e.String())
		}
	})
}

// FuzzNewMensajeEntrante exercises NewMensajeEntrante's text validation with
// arbitrary strings for every text field, holding the two timestamps fixed.
// The contract: never panic, and every accepted entity satisfies the
// documented length bounds (measured in codepoints).
func FuzzNewMensajeEntrante(f *testing.F) {
	seeds := []struct {
		wamid, remitente, phoneNumberID, tipo, contenido string
	}{
		{"wamid.OK", "5219981234567", "109876543210123", "text", "hola"},
		{"", "", "", "", ""},
		{"a\x00b", "5219981234567", "109876543210123", "text", "hola"},
		{"wamid.OK", "5219981234567", "109876543210123", "text", "línea\ncon\tsaltos"},
		{"wamid.OK", "5219981234567", "109876543210123", "text", "café"},
	}
	for _, s := range seeds {
		f.Add(s.wamid, s.remitente, s.phoneNumberID, s.tipo, s.contenido)
	}
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, wamid, remitente, phoneNumberID, tipo, contenido string) {
		m, err := NewMensajeEntrante(NewMensajeEntranteParams{
			Wamid:         wamid,
			Remitente:     remitente,
			PhoneNumberID: phoneNumberID,
			Tipo:          tipo,
			Contenido:     contenido,
			TimestampMeta: now,
			RecibidoEn:    now,
		})
		if err != nil {
			return
		}
		if codepointCount(m.Wamid()) == 0 || codepointCount(m.Wamid()) > maxWamidLength {
			t.Fatalf("accepted wamid outside bounds: %q", m.Wamid())
		}
		if codepointCount(m.Remitente()) == 0 || codepointCount(m.Remitente()) > maxRemitenteLength {
			t.Fatalf("accepted remitente outside bounds: %q", m.Remitente())
		}
		if codepointCount(m.PhoneNumberID()) == 0 || codepointCount(m.PhoneNumberID()) > maxPhoneNumberIDLength {
			t.Fatalf("accepted phoneNumberID outside bounds: %q", m.PhoneNumberID())
		}
		if codepointCount(m.Tipo()) == 0 || codepointCount(m.Tipo()) > maxTipoLength {
			t.Fatalf("accepted tipo outside bounds: %q", m.Tipo())
		}
		if codepointCount(m.Contenido()) > maxContenidoLength {
			t.Fatalf("accepted contenido outside bounds: %q", m.Contenido())
		}
		if m.Estado() != EstadoReenvioPendiente {
			t.Fatalf("newly constructed entity must start pendiente, got %s", m.Estado())
		}
	})
}

// FuzzMarcarFallido exercises MarcarFallido's motivo validation with
// arbitrary strings. The contract: never panic, and an accepted motivo is
// bounded and non-empty.
func FuzzMarcarFallido(f *testing.F) {
	seeds := []string{"", "   ", "timeout", "x\x00y", "café con NUL\x00"}
	for _, s := range seeds {
		f.Add(s)
	}
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, motivo string) {
		m, err := NewMensajeEntrante(NewMensajeEntranteParams{
			Wamid:         "wamid.FUZZ",
			Remitente:     "5219981234567",
			PhoneNumberID: "109876543210123",
			Tipo:          "text",
			Contenido:     "hola",
			TimestampMeta: now,
			RecibidoEn:    now,
		})
		if err != nil {
			t.Fatalf("unexpected setup error: %v", err)
		}
		if err := m.MarcarFallido(motivo, now); err != nil {
			return
		}
		if m.MotivoFallo() == "" {
			t.Fatalf("accepted empty motivo")
		}
		if codepointCount(m.MotivoFallo()) > maxMotivoFalloLength {
			t.Fatalf("accepted motivo outside bounds: %q", m.MotivoFallo())
		}
	})
}
