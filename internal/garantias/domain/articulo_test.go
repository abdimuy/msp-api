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

// articuloHidratadoCon builds an article straight from a persisted row, the
// same way the repository does, so these getter tests do not depend on walking
// the stage machine. The pointer it stores is a private copy: the test only
// looks at what the getter hands back.
func articuloHidratadoCon(p domain.HydrateArticuloParams) *domain.Articulo {
	p.ID = uuid.New()
	p.GarantiaID = uuid.New()
	p.Rol = domain.RolArticuloOriginal
	p.Etapa = domain.EtapaEnTaller
	p.Ubicacion = domain.UbicacionTaller
	return domain.HydrateArticulo(p)
}

// TestArticulo_Ruta_DevuelveCopia: mutar el valor devuelto por Ruta() no debe
// tocar el agregado. Si el getter devolviera el puntero interno, el segundo
// Ruta() leería la mutación (mutante T2).
func TestArticulo_Ruta_DevuelveCopia(t *testing.T) {
	t.Parallel()
	ruta := domain.RutaReparacionTaller
	a := articuloHidratadoCon(domain.HydrateArticuloParams{Ruta: &ruta})

	devuelta := a.Ruta()
	if devuelta == nil || *devuelta != domain.RutaReparacionTaller {
		t.Fatalf("Ruta() = %v, want taller", devuelta)
	}
	*devuelta = domain.RutaReparacionProveedor

	if got := a.Ruta(); got == nil || *got != domain.RutaReparacionTaller {
		t.Fatalf("Ruta() tras mutar el valor devuelto = %v, want taller", got)
	}
}

// TestArticulo_Dictamen_DevuelveCopia: mutar el valor devuelto por Dictamen()
// no debe tocar el agregado (mutante T1).
func TestArticulo_Dictamen_DevuelveCopia(t *testing.T) {
	t.Parallel()
	dictamen := domain.DictamenAceptada
	a := articuloHidratadoCon(domain.HydrateArticuloParams{Dictamen: &dictamen})

	devuelto := a.Dictamen()
	if devuelto == nil || *devuelto != domain.DictamenAceptada {
		t.Fatalf("Dictamen() = %v, want aceptada", devuelto)
	}
	*devuelto = domain.DictamenRechazada

	if got := a.Dictamen(); got == nil || *got != domain.DictamenAceptada {
		t.Fatalf("Dictamen() tras mutar el valor devuelto = %v, want aceptada", got)
	}
}

// TestArticulo_Desenlace_DevuelveCopia: mutar el valor devuelto por
// Desenlace() no debe tocar el agregado (mutante T3).
func TestArticulo_Desenlace_DevuelveCopia(t *testing.T) {
	t.Parallel()
	desenlace := domain.DesenlaceMerma
	a := articuloHidratadoCon(domain.HydrateArticuloParams{Desenlace: &desenlace})

	devuelto := a.Desenlace()
	if devuelto == nil || *devuelto != domain.DesenlaceMerma {
		t.Fatalf("Desenlace() = %v, want merma", devuelto)
	}
	*devuelto = domain.DesenlaceDevuelto

	if got := a.Desenlace(); got == nil || *got != domain.DesenlaceMerma {
		t.Fatalf("Desenlace() tras mutar el valor devuelto = %v, want merma", got)
	}
}
