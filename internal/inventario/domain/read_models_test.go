package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

func TestNewAlmacen_Constructable(t *testing.T) {
	t.Parallel()
	a := domain.NewAlmacen(3, "Almacen Central")
	if a.ID != 3 {
		t.Fatalf("expected ID=3, got %d", a.ID)
	}
	if a.Nombre != "Almacen Central" {
		t.Fatalf("expected Nombre='Almacen Central', got %q", a.Nombre)
	}
}

func TestAlmacen_DirectConstruction(t *testing.T) {
	t.Parallel()
	a := domain.Almacen{ID: 5, Nombre: "Bodega Norte"}
	if a.ID != 5 {
		t.Fatalf("expected ID=5, got %d", a.ID)
	}
}

func TestExistencia_Constructable(t *testing.T) {
	t.Parallel()
	e := domain.Existencia{
		ArticuloID: 100,
		AlmacenID:  2,
		Cantidad:   decimal.NewFromInt(50),
	}
	if e.ArticuloID != 100 {
		t.Fatalf("expected ArticuloID=100, got %d", e.ArticuloID)
	}
	if e.AlmacenID != 2 {
		t.Fatalf("expected AlmacenID=2, got %d", e.AlmacenID)
	}
	if !e.Cantidad.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("expected Cantidad=50")
	}
}

func TestExistencia_NegativeQuantityAllowed(t *testing.T) {
	t.Parallel()
	// Stock projections can hold negative values (oversold scenario).
	e := domain.Existencia{
		ArticuloID: 1,
		AlmacenID:  1,
		Cantidad:   decimal.NewFromInt(-5),
	}
	if e.Cantidad.Sign() >= 0 {
		t.Fatal("negative existencia should be allowed")
	}
}
