package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestHydrateEvento_Garbage drills a purposely broken row through the
// constructor: hydration must not validate a thing, the repository only
// calls it with rows it already wrote.
func TestHydrateEvento_Garbage(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	garID := uuid.New()
	articuloID := uuid.New()
	desde := domain.Etapa("etapa_basura")
	hasta := domain.EtapaEnTaller
	rol := domain.RolDecisorOficina
	lat, lon := 19.427, -99.17
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)

	e := domain.HydrateEvento(domain.EventoParamsOut{
		ID:                id,
		GarantiaID:        garID,
		ArticuloRef:       &articuloID,
		Tipo:              domain.TipoEvento("tipo_basura"),
		Description:       "comentario",
		EtapaDesde:        &desde,
		EtapaHasta:        &hasta,
		Usuario:           "juan",
		RolDecisor:        &rol,
		GPSLat:            &lat,
		GPSLon:            &lon,
		CreatedAt:         now,
		DeviceCreatedAt:   now.Add(time.Hour),
		ClaveIdempotencia: "clave-1",
	})

	if e.ID() != id {
		t.Errorf("ID() = %v, want %v", e.ID(), id)
	}
	if e.GarantiaID() != garID {
		t.Errorf("GarantiaID() = %v, want %v", e.GarantiaID(), garID)
	}
	if e.ArticuloRef() == nil || *e.ArticuloRef() != articuloID {
		t.Errorf("ArticuloRef() = %v, want %v", e.ArticuloRef(), articuloID)
	}
	if e.Tipo() != domain.TipoEvento("tipo_basura") {
		t.Errorf("Tipo() = %q, want \"tipo_basura\"", e.Tipo())
	}
	if e.Description() != "comentario" {
		t.Errorf("Description() = %q, want \"comentario\"", e.Description())
	}
	if e.EtapaDesde() == nil || *e.EtapaDesde() != desde {
		t.Errorf("EtapaDesde() = %v, want %v", e.EtapaDesde(), desde)
	}
	if e.EtapaHasta() == nil || *e.EtapaHasta() != hasta {
		t.Errorf("EtapaHasta() = %v, want %v", e.EtapaHasta(), hasta)
	}
	if e.Usuario() != "juan" {
		t.Errorf("Usuario() = %q, want \"juan\"", e.Usuario())
	}
	if e.RolDecisor() == nil || *e.RolDecisor() != rol {
		t.Errorf("RolDecisor() = %v, want %v", e.RolDecisor(), rol)
	}
	if e.GPSLat() == nil || *e.GPSLat() != lat {
		t.Errorf("GPSLat() = %v, want %v", e.GPSLat(), lat)
	}
	if e.GPSLon() == nil || *e.GPSLon() != lon {
		t.Errorf("GPSLon() = %v, want %v", e.GPSLon(), lon)
	}
	if !e.CreatedAt().Equal(now) {
		t.Errorf("CreatedAt() = %v, want %v", e.CreatedAt(), now)
	}
	if !e.DeviceCreatedAt().Equal(now.Add(time.Hour)) {
		t.Errorf("DeviceCreatedAt() = %v, want %v", e.DeviceCreatedAt(), now.Add(time.Hour))
	}
	if e.ClaveIdempotencia() != "clave-1" {
		t.Errorf("ClaveIdempotencia() = %q, want \"clave-1\"", e.ClaveIdempotencia())
	}
}

func TestHydrateEvento_NilPointers(t *testing.T) {
	t.Parallel()
	e := domain.HydrateEvento(domain.EventoParamsOut{
		ID:         uuid.New(),
		GarantiaID: uuid.New(),
		Tipo:       domain.TipoEventoNota,
		Usuario:    "juan",
		CreatedAt:  time.Now(),
	})
	if e.ArticuloRef() != nil {
		t.Errorf("ArticuloRef() = %v, want nil", e.ArticuloRef())
	}
	if e.EtapaDesde() != nil {
		t.Errorf("EtapaDesde() = %v, want nil", e.EtapaDesde())
	}
	if e.EtapaHasta() != nil {
		t.Errorf("EtapaHasta() = %v, want nil", e.EtapaHasta())
	}
	if e.RolDecisor() != nil {
		t.Errorf("RolDecisor() = %v, want nil", e.RolDecisor())
	}
	if e.GPSLat() != nil || e.GPSLon() != nil {
		t.Errorf("GPSLat()/GPSLon() = %v/%v, want nil/nil", e.GPSLat(), e.GPSLon())
	}
}
