package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/canal/domain"
)

func TestMensajeEntranteRecibidoEvent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	now := time.Date(2026, 8, 31, 9, 0, 0, 0, time.UTC)
	ev := domain.NewMensajeEntranteRecibidoEvent(id, "wamid.X", "5219981234567", "109876543210123", "text", now)

	if ev.EventType() != domain.EventTypeMensajeEntranteRecibido {
		t.Errorf("EventType = %q, want %q", ev.EventType(), domain.EventTypeMensajeEntranteRecibido)
	}
	if ev.EventType() != "canal.mensaje_entrante_recibido" {
		t.Errorf("EventType wire value = %q", ev.EventType())
	}
	if ev.AggregateID() != id {
		t.Errorf("AggregateID = %v, want %v", ev.AggregateID(), id)
	}
	if !ev.OccurredAt().Equal(now) {
		t.Errorf("OccurredAt = %v, want %v", ev.OccurredAt(), now)
	}
	want := map[string]any{
		"wamid":           "wamid.X",
		"remitente":       "5219981234567",
		"phone_number_id": "109876543210123",
		"tipo":            "text",
	}
	got := ev.Payload()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("payload[%q] = %v, want %v", k, got[k], v)
		}
	}
}

func TestMensajeReenviadoEvent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	now := time.Date(2026, 8, 31, 9, 5, 0, 0, time.UTC)
	ev := domain.NewMensajeReenviadoEvent(id, "wamid.X", now)

	if ev.EventType() != "canal.mensaje_reenviado" {
		t.Errorf("EventType = %q", ev.EventType())
	}
	if ev.AggregateID() != id {
		t.Errorf("AggregateID = %v, want %v", ev.AggregateID(), id)
	}
	if !ev.OccurredAt().Equal(now) {
		t.Errorf("OccurredAt = %v, want %v", ev.OccurredAt(), now)
	}
	if ev.Payload()["wamid"] != "wamid.X" {
		t.Errorf("payload wamid = %v", ev.Payload()["wamid"])
	}
}

func TestReenvioFallidoEvent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	now := time.Date(2026, 8, 31, 9, 10, 0, 0, time.UTC)
	ev := domain.NewReenvioFallidoEvent(id, "wamid.X", "timeout", now)

	if ev.EventType() != "canal.reenvio_fallido" {
		t.Errorf("EventType = %q", ev.EventType())
	}
	if ev.AggregateID() != id {
		t.Errorf("AggregateID = %v, want %v", ev.AggregateID(), id)
	}
	if !ev.OccurredAt().Equal(now) {
		t.Errorf("OccurredAt = %v, want %v", ev.OccurredAt(), now)
	}
	payload := ev.Payload()
	if payload["wamid"] != "wamid.X" || payload["motivo"] != "timeout" {
		t.Errorf("unexpected payload: %+v", payload)
	}
}
