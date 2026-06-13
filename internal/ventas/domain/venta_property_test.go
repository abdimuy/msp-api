package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// TestGPSCoords_Property checks that NewGPSCoords accepts any (lat,lng) pair
// within bounds and rejects anything outside them.
func TestGPSCoords_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		lat := rapid.Float64Range(-90, 90).Draw(t, "lat")
		lng := rapid.Float64Range(-180, 180).Draw(t, "lng")
		g, err := domain.NewGPSCoords(lat, lng)
		if err != nil {
			t.Fatalf("expected valid gps (%v,%v): %v", lat, lng, err)
		}
		if g.Latitud() != lat || g.Longitud() != lng {
			t.Fatalf("round-trip mismatch: in=(%v,%v) out=(%v,%v)", lat, lng, g.Latitud(), g.Longitud())
		}
	})
}

func TestGPSCoords_RejectsOutOfBounds_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		// Either lat or lng is out of bounds.
		latOut := rapid.OneOf(
			rapid.Float64Range(-1000, -90.0001),
			rapid.Float64Range(90.0001, 1000),
		).Draw(t, "lat_out")
		if _, err := domain.NewGPSCoords(latOut, 0); err == nil {
			t.Fatalf("expected error for lat=%v", latOut)
		}
		lngOut := rapid.OneOf(
			rapid.Float64Range(-1000, -180.0001),
			rapid.Float64Range(180.0001, 1000),
		).Draw(t, "lng_out")
		if _, err := domain.NewGPSCoords(0, lngOut); err == nil {
			t.Fatalf("expected error for lng=%v", lngOut)
		}
	})
}

// TestDiaCobranzaMes_Property checks domain ∈ [1,31] iff accepted.
func TestDiaCobranzaMes_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		day := rapid.IntRange(-100, 100).Draw(t, "day")
		_, err := domain.NewDiaCobranzaMes(day)
		inRange := day >= 1 && day <= 31
		if inRange && err != nil {
			t.Fatalf("expected day=%d accepted, got: %v", day, err)
		}
		if !inRange && err == nil {
			t.Fatalf("expected day=%d rejected, got nil err", day)
		}
	})
}

// TestPlanCredito_RoundTrip_Property checks that any valid PlanCredito built
// from random inputs satisfies the field invariants on the way out.
func TestPlanCredito_RoundTrip_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		plazo := rapid.IntRange(1, 120).Draw(t, "plazo")
		enganche := rapid.Int64Range(0, 1_000_000).Draw(t, "enganche")
		parcialidad := rapid.Int64Range(0, 1_000_000).Draw(t, "parcialidad")
		frecs := []domain.FrecPago{domain.FrecPagoSemanal, domain.FrecPagoQuincenal, domain.FrecPagoMensual}
		frec := rapid.SampledFrom(frecs).Draw(t, "frec")

		p, err := domain.NewPlanCredito(plazo, decimal.NewFromInt(enganche), decimal.NewFromInt(parcialidad), frec)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.PlazoMeses() != plazo {
			t.Fatalf("plazo mismatch")
		}
		if !p.Enganche().Equal(decimal.NewFromInt(enganche)) {
			t.Fatalf("enganche mismatch")
		}
		if !p.Parcialidad().Equal(decimal.NewFromInt(parcialidad)) {
			t.Fatalf("parcialidad mismatch")
		}
		if p.FrecPago() != frec {
			t.Fatalf("frec mismatch")
		}
	})
}

// TestCrearVenta_RandomValidContado_Property generates random valid CONTADO
// params and asserts that CrearVenta accepts them with the expected
// invariants (no plan, no diaCobranza, has at least one producto/vendedor,
// event emitted).
func TestCrearVenta_RandomValidContado_Property(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		numProductos := rapid.IntRange(1, 5).Draw(t, "num_productos")
		numVendedores := rapid.IntRange(1, 3).Draw(t, "num_vendedores")
		anual := rapid.Int64Range(0, 999999).Draw(t, "anual")
		corto := rapid.Int64Range(0, 999999).Draw(t, "corto")
		contado := rapid.Int64Range(0, 999999).Draw(t, "contado")
		lat := rapid.Float64Range(-90, 90).Draw(t, "lat")
		lng := rapid.Float64Range(-180, 180).Draw(t, "lng")
		almOrig := rapid.IntRange(1, 100).Draw(t, "almOrig")
		almDest := almOrig + 1

		nom, _ := domain.NewNombreCliente("Cliente")
		cliente, _ := domain.NewClienteSnapshot(domain.NewClienteSnapshotParams{Nombre: nom})
		dir, _ := domain.NewDireccion(domain.NewDireccionParams{
			Calle: "C", Colonia: "Co", Poblacion: "P", Ciudad: "Cd",
		})
		gps, _ := domain.NewGPSCoords(lat, lng)
		montos, _ := domain.NewMontoSnapshot(
			decimal.NewFromInt(anual),
			decimal.NewFromInt(corto),
			decimal.NewFromInt(contado),
		)

		productos := make([]domain.CrearVentaProductoInput, 0, numProductos)
		for range numProductos {
			productos = append(productos, domain.CrearVentaProductoInput{
				ID: uuid.New(), ArticuloID: 1, Articulo: "art",
				Cantidad: decimal.NewFromInt(1), Precios: montos,
				AlmacenOrigen: &almOrig, AlmacenDestino: &almDest,
			})
		}
		vendedores := make([]domain.CrearVentaVendedorInput, 0, numVendedores)
		for range numVendedores {
			vendedores = append(vendedores, domain.CrearVentaVendedorInput{
				ID: uuid.New(), UsuarioID: uuid.New(), Email: "v@x.com", Nombre: "V",
			})
		}

		v, err := domain.CrearVenta(domain.CrearVentaParams{
			ID:         uuid.New(),
			Cliente:    cliente,
			Direccion:  dir,
			GPS:        gps,
			FechaVenta: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			TipoVenta:  domain.TipoVentaContado,
			Productos:  productos,
			Vendedores: vendedores,
			CreatedBy:  uuid.New(),
			Now:        time.Now(),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.PlanCredito() != nil || v.DiaCobranza() != nil {
			t.Fatalf("contado must not carry plan/dia_cobranza")
		}
		if v.ProductosCount() != numProductos {
			t.Fatalf("productos count mismatch: got=%d want=%d", v.ProductosCount(), numProductos)
		}
		if v.VendedoresCount() != numVendedores {
			t.Fatalf("vendedores count mismatch: got=%d want=%d", v.VendedoresCount(), numVendedores)
		}
		evs := v.PendingEvents()
		if len(evs) != 1 || evs[0].EventType() != "venta.creada" {
			t.Fatalf("expected exactly one venta.creada event")
		}
	})
}
