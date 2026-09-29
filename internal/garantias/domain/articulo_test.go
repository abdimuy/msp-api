package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestHydrateArticulo_Garbage drills a purposely broken row through the
// constructor: hydration must not validate a thing (invalid stages, nil
// pointers), the repository only calls it with rows it already wrote.
func TestHydrateArticulo_Garbage(t *testing.T) {
	t.Parallel()
	id := uuid.New()
	garID := uuid.New()
	reemplazaA := uuid.New()
	articuloID := 42
	ruta := domain.RutaReparacionTaller
	dictamen := domain.DictamenAceptada
	desenlace := domain.DesenlaceMerma
	cerrado := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	created := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	updated := created.Add(4 * time.Hour)

	a := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:          id,
		GarantiaID:  garID,
		Rol:         domain.RolArticuloReemplazo,
		ReemplazaA:  &reemplazaA,
		ArticuloID:  &articuloID,
		Clave:       "ART-1",
		Description: "silla",
		Ruta:        &ruta,
		Etapa:       domain.Etapa("etapa_basura"),
		Ubicacion:   domain.UbicacionBaja,
		Dictamen:    &dictamen,
		Desenlace:   &desenlace,
		CerradoEn:   &cerrado,
		CreatedAt:   created,
		UpdatedAt:   updated,
	})

	if a.ID() != id || a.GarantiaID() != garID {
		t.Errorf("ID/GarantiaID = %v/%v, want %v/%v", a.ID(), a.GarantiaID(), id, garID)
	}
	if a.Rol() != domain.RolArticuloReemplazo {
		t.Errorf("Rol() = %q, want \"reemplazo\"", a.Rol())
	}
	if a.ReemplazaA() == nil || *a.ReemplazaA() != reemplazaA {
		t.Errorf("ReemplazaA() = %v, want %v", a.ReemplazaA(), reemplazaA)
	}
	if a.ArticuloID() == nil || *a.ArticuloID() != articuloID {
		t.Errorf("ArticuloID() = %v, want %v", a.ArticuloID(), articuloID)
	}
	if a.Clave() != "ART-1" {
		t.Errorf("Clave() = %q, want \"ART-1\"", a.Clave())
	}
	if a.Description() != "silla" {
		t.Errorf("Description() = %q, want \"silla\"", a.Description())
	}
	if a.Ruta() == nil || *a.Ruta() != ruta {
		t.Errorf("Ruta() = %v, want %v", a.Ruta(), ruta)
	}
	if a.Etapa() != domain.Etapa("etapa_basura") {
		t.Errorf("Etapa() = %q, want \"etapa_basura\"", a.Etapa())
	}
	if a.Ubicacion() != domain.UbicacionBaja {
		t.Errorf("Ubicacion() = %q, want \"baja\"", a.Ubicacion())
	}
	if a.Dictamen() == nil || *a.Dictamen() != dictamen {
		t.Errorf("Dictamen() = %v, want %v", a.Dictamen(), dictamen)
	}
	if a.Desenlace() == nil || *a.Desenlace() != desenlace {
		t.Errorf("Desenlace() = %v, want %v", a.Desenlace(), desenlace)
	}
	if a.CerradoEn() == nil || !a.CerradoEn().Equal(cerrado) {
		t.Errorf("CerradoEn() = %v, want %v", a.CerradoEn(), cerrado)
	}
	if !a.CreatedAt().Equal(created) || !a.UpdatedAt().Equal(updated) {
		t.Errorf("CreatedAt/UpdatedAt = %v/%v, want %v/%v", a.CreatedAt(), a.UpdatedAt(), created, updated)
	}
}

func TestHydrateArticulo_NilPointers(t *testing.T) {
	t.Parallel()
	a := domain.HydrateArticulo(domain.HydrateArticuloParams{
		ID:         uuid.New(),
		GarantiaID: uuid.New(),
		Rol:        domain.RolArticuloOriginal,
		Etapa:      domain.EtapaRegistrado,
		Ubicacion:  domain.UbicacionAlmacenRevision,
	})
	if a.ReemplazaA() != nil {
		t.Errorf("ReemplazaA() = %v, want nil", a.ReemplazaA())
	}
	if a.ArticuloID() != nil {
		t.Errorf("ArticuloID() = %v, want nil", a.ArticuloID())
	}
	if a.Ruta() != nil || a.Dictamen() != nil || a.Desenlace() != nil || a.CerradoEn() != nil {
		t.Errorf("pointer fields not nil: ruta=%v dictamen=%v desenlace=%v cerradoEn=%v",
			a.Ruta(), a.Dictamen(), a.Desenlace(), a.CerradoEn())
	}
}
