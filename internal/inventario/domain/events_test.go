package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

func TestTraspasoCreadoEvent_Payload(t *testing.T) {
	t.Parallel()
	traspasoID := uuid.New()
	vid := uuid.New()
	ev := domain.NewTraspasoCreadoEvent(
		traspasoID, "MST000001", 1, 2, &vid, false, 3, fixedNow,
	)

	if ev.EventType() != domain.EventTypeTraspasoCreado {
		t.Fatalf("EventType mismatch: got %q", ev.EventType())
	}
	if ev.AggregateID() != traspasoID {
		t.Fatalf("AggregateID mismatch")
	}
	if ev.OccurredAt() != fixedNow {
		t.Fatalf("OccurredAt mismatch")
	}

	p := ev.Payload()
	require.Equal(t, "MST000001", p["folio"])
	require.Equal(t, 1, p["almacen_origen"])
	require.Equal(t, 2, p["almacen_destino"])
	require.Equal(t, false, p["tipo_reverso"])
	require.Equal(t, 3, p["detalles_count"])
	require.Equal(t, vid.String(), p["venta_id"])
}

func TestTraspasoCreadoEvent_Payload_NilVentaID(t *testing.T) {
	t.Parallel()
	ev := domain.NewTraspasoCreadoEvent(
		uuid.New(), "MST000001", 1, 2, nil, false, 1, fixedNow,
	)
	p := ev.Payload()
	if p["venta_id"] != nil {
		t.Fatalf("expected venta_id=nil, got %v", p["venta_id"])
	}
}

func TestTraspasoReversadoEvent_Payload(t *testing.T) {
	t.Parallel()
	traspasoID := uuid.New()
	ev := domain.NewTraspasoReversadoEvent(
		traspasoID, "MST000002", 2, 1, nil, true, 2, fixedNow,
	)

	if ev.EventType() != domain.EventTypeTraspasoReversado {
		t.Fatalf("EventType mismatch: got %q", ev.EventType())
	}
	if ev.AggregateID() != traspasoID {
		t.Fatalf("AggregateID mismatch")
	}
	if ev.OccurredAt() != fixedNow {
		t.Fatalf("OccurredAt mismatch")
	}

	p := ev.Payload()
	require.Equal(t, "MST000002", p["folio"])
	require.Equal(t, 2, p["almacen_origen"])
	require.Equal(t, 1, p["almacen_destino"])
	require.Equal(t, true, p["tipo_reverso"])
	require.Equal(t, 2, p["detalles_count"])
	if p["venta_id"] != nil {
		t.Fatalf("expected venta_id=nil, got %v", p["venta_id"])
	}
}

func TestTraspasoReversadoEvent_Payload_WithVentaID(t *testing.T) {
	t.Parallel()
	vid := uuid.New()
	ev := domain.NewTraspasoReversadoEvent(
		uuid.New(), "MST000003", 3, 4, &vid, true, 1, fixedNow,
	)
	p := ev.Payload()
	require.Equal(t, vid.String(), p["venta_id"])
}

func TestEvents_ViaCrearTraspaso_EmitsTraspasoCreadoEvent(t *testing.T) {
	t.Parallel()
	tr, err := domain.CrearTraspaso(validCrearParams(t))
	require.NoError(t, err)

	evs := tr.PendingEvents()
	require.Len(t, evs, 1)

	ev := evs[0]
	if ev.EventType() != domain.EventTypeTraspasoCreado {
		t.Fatalf("expected %q, got %q", domain.EventTypeTraspasoCreado, ev.EventType())
	}
	if ev.AggregateID() != tr.ID() {
		t.Fatalf("AggregateID mismatch")
	}
	p := ev.Payload()
	if p["folio"] != "MST000001" {
		t.Fatalf("payload folio mismatch: got %v", p["folio"])
	}
}

func TestEvents_Reversar_EmitsTraspasoReversadoEvent(t *testing.T) {
	t.Parallel()
	original, _ := domain.CrearTraspaso(validCrearParams(t))
	newFolio, _ := domain.NewFolio("MST000002")
	reversed, err := original.Reversar(fixedNow, uuid.New(), uuid.New(), newFolio)
	require.NoError(t, err)

	evs := reversed.PendingEvents()
	require.Len(t, evs, 1)
	if evs[0].EventType() != domain.EventTypeTraspasoReversado {
		t.Fatalf("expected traspaso.reversado event, got %q", evs[0].EventType())
	}
}
