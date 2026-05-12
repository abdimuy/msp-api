package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

func TestVentaCreadaEvent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	by := uuid.New()
	now := time.Now()
	e := domain.NewVentaCreadaEvent(id, domain.TipoVentaCredito, by, now)
	assert.Equal(t, "venta.creada", e.EventType())
	assert.Equal(t, id, e.AggregateID())
	assert.Equal(t, now, e.OccurredAt())
	p := e.Payload()
	assert.Equal(t, id.String(), p["venta_id"])
	assert.Equal(t, "CREDITO", p["tipo_venta"])
	assert.Equal(t, by.String(), p["created_by"])
}

func TestVentaCanceladaEvent(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	by := uuid.New()
	now := time.Now()
	e := domain.NewVentaCanceladaEvent(id, by, "razon", now)
	assert.Equal(t, "venta.cancelada", e.EventType())
	assert.Equal(t, id, e.AggregateID())
	assert.Equal(t, now, e.OccurredAt())
	p := e.Payload()
	assert.Equal(t, id.String(), p["venta_id"])
	assert.Equal(t, by.String(), p["canceled_by"])
	assert.Equal(t, "razon", p["reason"])
}

func TestImagenAdjuntadaEvent(t *testing.T) {
	t.Parallel()
	ventaID := uuid.New()
	imgID := uuid.New()
	now := time.Now()
	e := domain.NewImagenAdjuntadaEvent(domain.NewImagenAdjuntadaEventParams{
		VentaID: ventaID, ImagenID: imgID, StorageKey: "k.jpg",
		Mime: "image/jpeg", SizeBytes: 42, Now: now,
	})
	assert.Equal(t, "venta.imagen_adjuntada", e.EventType())
	assert.Equal(t, ventaID, e.AggregateID())
	assert.Equal(t, now, e.OccurredAt())
	p := e.Payload()
	assert.Equal(t, ventaID.String(), p["venta_id"])
	assert.Equal(t, imgID.String(), p["imagen_id"])
	assert.Equal(t, "k.jpg", p["storage_key"])
	assert.Equal(t, "image/jpeg", p["mime"])
	assert.Equal(t, int64(42), p["size_bytes"])
}

func TestImagenEliminadaEvent(t *testing.T) {
	t.Parallel()
	ventaID := uuid.New()
	imgID := uuid.New()
	now := time.Now()
	e := domain.NewImagenEliminadaEvent(ventaID, imgID, now)
	assert.Equal(t, "venta.imagen_eliminada", e.EventType())
	assert.Equal(t, ventaID, e.AggregateID())
	assert.Equal(t, now, e.OccurredAt())
	p := e.Payload()
	assert.Equal(t, ventaID.String(), p["venta_id"])
	assert.Equal(t, imgID.String(), p["imagen_id"])
}
