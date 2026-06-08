package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// newValidDetalle is a test helper that creates a TraspasoDetalle via
// CrearTraspaso (since newDetalle is package-private). It uses a minimal
// single-detalle traspaso and returns the first detalle.
func newValidDetalle(t *testing.T, articuloID int, cantidad domain.Cantidad) *domain.TraspasoDetalle {
	t.Helper()
	folio, err := domain.NewFolio("MST000001")
	require.NoError(t, err)
	traspaso, err := domain.CrearTraspaso(domain.CrearTraspasoParams{
		ID:             uuid.New(),
		Folio:          folio,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "test",
		Detalles: []domain.CrearTraspasoDetalleInput{
			{ID: uuid.New(), ArticuloID: articuloID, Cantidad: cantidad},
		},
		CreatedBy: uuid.New(),
		Now:       fixedNow,
	})
	require.NoError(t, err)
	var det *domain.TraspasoDetalle
	for d := range traspaso.Detalles() {
		det = d
		break
	}
	require.NotNil(t, det)
	return det
}

func TestTraspasoDetalle_Accessors(t *testing.T) {
	t.Parallel()
	cantidad, _ := domain.NewCantidad(decimal.NewFromInt(3))
	detID := uuid.New()

	// Use HydrateDetalle to exercise the public hydrator path.
	det := domain.HydrateDetalle(domain.HydrateDetalleParams{
		ID:         detID,
		ArticuloID: 42,
		Cantidad:   cantidad,
	})
	if det.ID() != detID {
		t.Fatalf("ID mismatch: want %v got %v", detID, det.ID())
	}
	if det.ArticuloID() != 42 {
		t.Fatalf("ArticuloID mismatch: want 42 got %d", det.ArticuloID())
	}
	if !det.Cantidad().Equals(cantidad) {
		t.Fatalf("Cantidad mismatch")
	}
}

func TestTraspasoDetalle_ViaCrearTraspaso_HappyPath(t *testing.T) {
	t.Parallel()
	cantidad, _ := domain.NewCantidad(decimal.NewFromInt(5))
	det := newValidDetalle(t, 100, cantidad)
	if det.ArticuloID() != 100 {
		t.Fatalf("ArticuloID mismatch: want 100 got %d", det.ArticuloID())
	}
	if !det.Cantidad().Equals(cantidad) {
		t.Fatalf("Cantidad mismatch")
	}
}

func TestCrearTraspaso_RejectsInvalidArticuloID(t *testing.T) {
	t.Parallel()
	cantidad, _ := domain.NewCantidad(decimal.NewFromInt(1))
	folio, _ := domain.NewFolio("MST000001")
	_, err := domain.CrearTraspaso(domain.CrearTraspasoParams{
		ID:             uuid.New(),
		Folio:          folio,
		AlmacenOrigen:  1,
		AlmacenDestino: 2,
		Fecha:          fixedNow,
		Descripcion:    "test",
		Detalles: []domain.CrearTraspasoDetalleInput{
			{ID: uuid.New(), ArticuloID: 0, Cantidad: cantidad},
		},
		CreatedBy: uuid.New(),
		Now:       fixedNow,
	})
	if err == nil {
		t.Fatal("expected error for articuloID=0")
	}
}

func TestTraspasoDetalle_HydrateDetalle(t *testing.T) {
	t.Parallel()
	cantidad, _ := domain.NewCantidad(decimal.RequireFromString("2.5"))
	id := uuid.New()
	det := domain.HydrateDetalle(domain.HydrateDetalleParams{
		ID:         id,
		ArticuloID: 7,
		Cantidad:   cantidad,
	})
	if det.ID() != id {
		t.Fatalf("ID mismatch")
	}
	if det.ArticuloID() != 7 {
		t.Fatalf("ArticuloID mismatch")
	}
}
