package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// TestCrearTraspaso_RandomValid_Property verifies that for any valid set of
// parameters (almacen_origen ≠ almacen_destino, at least one detalle with
// cantidad > 0), CrearTraspaso succeeds and the aggregate's invariants hold.
func TestCrearTraspaso_RandomValid_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		almOrig := rapid.IntRange(1, 50).Draw(t, "alm_orig")
		almDestOffset := rapid.IntRange(1, 50).Draw(t, "alm_dest_offset")
		almDest := almOrig + almDestOffset // guaranteed != almOrig

		numDetalles := rapid.IntRange(1, 5).Draw(t, "num_detalles")
		articuloIDs := make([]int, numDetalles)
		for i := range numDetalles {
			articuloIDs[i] = rapid.IntRange(1, 9999).Draw(t, "articulo_id")
		}
		cantidades := make([]int64, numDetalles)
		for i := range numDetalles {
			cantidades[i] = rapid.Int64Range(1, 1000).Draw(t, "cantidad")
		}

		folioStr := "MST" + rapid.StringMatching(`[0-9]{6}`).Draw(t, "folio_digits")
		folio, err := domain.NewFolio(folioStr)
		if err != nil {
			t.Fatalf("folio generation failed for %q: %v", folioStr, err)
		}

		detalleInputs := make([]domain.CrearTraspasoDetalleInput, numDetalles)
		for i := range numDetalles {
			c, cerr := domain.NewCantidad(decimal.NewFromInt(cantidades[i]))
			if cerr != nil {
				t.Fatalf("cantidad construction failed: %v", cerr)
			}
			detalleInputs[i] = domain.CrearTraspasoDetalleInput{
				ID:         uuid.New(),
				ArticuloID: articuloIDs[i],
				Cantidad:   c,
			}
		}

		tr, terr := domain.CrearTraspaso(domain.CrearTraspasoParams{
			ID:             uuid.New(),
			Folio:          folio,
			AlmacenOrigen:  almOrig,
			AlmacenDestino: almDest,
			Fecha:          time.Date(2026, 6, 8, 18, 0, 0, 0, time.UTC),
			Descripcion:    "property test",
			Detalles:       detalleInputs,
			CreatedBy:      uuid.New(),
			Now:            time.Date(2026, 6, 8, 18, 0, 0, 0, time.UTC),
		})
		if terr != nil {
			t.Fatalf("unexpected CrearTraspaso error: %v", terr)
		}

		// Invariants.
		if tr.AlmacenOrigen() == tr.AlmacenDestino() {
			t.Fatalf("almacen_origen must not equal almacen_destino")
		}
		if tr.TipoReverso() {
			t.Fatal("new traspaso must not be tipoReverso=true")
		}
		if tr.DoctoInID() != nil {
			t.Fatal("new traspaso must have nil DoctoInID")
		}

		// Detalles count via iterator.
		count := 0
		for range tr.Detalles() {
			count++
		}
		if count != numDetalles {
			t.Fatalf("detalles count mismatch: got=%d want=%d", count, numDetalles)
		}

		// Exactly one event of type traspaso.creado.
		evs := tr.PendingEvents()
		if len(evs) != 1 || evs[0].EventType() != domain.EventTypeTraspasoCreado {
			t.Fatalf("expected exactly one traspaso.creado event, got %v", evs)
		}
	})
}
