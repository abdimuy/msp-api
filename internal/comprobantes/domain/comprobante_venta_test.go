package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func fechaPrueba() time.Time {
	return time.Date(2026, 8, 10, 15, 30, 0, 0, time.UTC)
}

func articuloValido() domain.ArticuloComprobante {
	a, err := domain.NewArticuloComprobante(domain.NewArticuloComprobanteParams{
		Descripcion:    "Refrigerador 12 pies",
		Cantidad:       decimal.NewFromInt(1),
		PrecioUnitario: decimal.NewFromInt(12500),
		Importe:        decimal.NewFromInt(12500),
	})
	if err != nil {
		panic(err)
	}
	return a
}

func TestNewComprobanteVenta_HappyPath(t *testing.T) {
	t.Parallel()
	c, err := domain.NewComprobanteVenta(domain.NewComprobanteVentaParams{
		Folio:            "VENTA-1001",
		Fecha:            fechaPrueba(),
		ClienteNombre:    "JUAN PEREZ",
		ClienteDomicilio: "Calle 1 #23, Colonia Centro",
		Articulos:        []domain.ArticuloComprobante{articuloValido()},
		Total:            decimal.NewFromInt(12500),
		Enganche:         decimal.NewFromInt(5000),
		Saldo:            decimal.NewFromInt(7500),
		PlanPago:         "12 meses sin intereses",
		Vendedor:         "MARIA GARCIA",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.Folio() != "VENTA-1001" {
		t.Fatalf("Folio() = %q, want VENTA-1001", c.Folio())
	}
	if !c.Fecha().Equal(fechaPrueba()) {
		t.Fatalf("Fecha() = %v, want %v", c.Fecha(), fechaPrueba())
	}
	if c.ClienteNombre() != "JUAN PEREZ" {
		t.Fatalf("ClienteNombre() = %q, want JUAN PEREZ", c.ClienteNombre())
	}
	if c.ClienteDomicilio() != "Calle 1 #23, Colonia Centro" {
		t.Fatalf("ClienteDomicilio() = %q", c.ClienteDomicilio())
	}
	if got := c.Articulos(); len(got) != 1 || got[0].Descripcion() != "Refrigerador 12 pies" {
		t.Fatalf("Articulos() = %+v, want 1 article", got)
	}
	if c.Total().Cmp(decimal.NewFromInt(12500)) != 0 {
		t.Fatalf("Total() = %v", c.Total())
	}
	if c.Enganche().Cmp(decimal.NewFromInt(5000)) != 0 {
		t.Fatalf("Enganche() = %v", c.Enganche())
	}
	if c.Saldo().Cmp(decimal.NewFromInt(7500)) != 0 {
		t.Fatalf("Saldo() = %v", c.Saldo())
	}
	if c.PlanPago() != "12 meses sin intereses" {
		t.Fatalf("PlanPago() = %q", c.PlanPago())
	}
	if c.Vendedor() != "MARIA GARCIA" {
		t.Fatalf("Vendedor() = %q", c.Vendedor())
	}
}

func TestNewComprobanteVenta_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  domain.NewComprobanteVentaParams
		want error
	}{
		{
			name: "folio vacio",
			got: domain.NewComprobanteVentaParams{
				Folio:         "  ",
				ClienteNombre: "JUAN PEREZ",
				Articulos:     []domain.ArticuloComprobante{articuloValido()},
			},
			want: domain.ErrComprobanteVentaFolioRequerido,
		},
		{
			name: "cliente vacio",
			got: domain.NewComprobanteVentaParams{
				Folio:         "VENTA-1001",
				ClienteNombre: " ",
				Articulos:     []domain.ArticuloComprobante{articuloValido()},
			},
			want: domain.ErrComprobanteVentaClienteRequerido,
		},
		{
			name: "total negativo",
			got: domain.NewComprobanteVentaParams{
				Folio:         "VENTA-1001",
				ClienteNombre: "JUAN PEREZ",
				Articulos:     []domain.ArticuloComprobante{articuloValido()},
				Total:         decimal.NewFromInt(-1),
			},
			want: domain.ErrComprobanteVentaTotalNegativo,
		},
		{
			name: "enganche negativo",
			got: domain.NewComprobanteVentaParams{
				Folio:         "VENTA-1001",
				ClienteNombre: "JUAN PEREZ",
				Articulos:     []domain.ArticuloComprobante{articuloValido()},
				Enganche:      decimal.NewFromInt(-5),
			},
			want: domain.ErrComprobanteVentaEngancheNegativo,
		},
		{
			name: "saldo negativo",
			got: domain.NewComprobanteVentaParams{
				Folio:         "VENTA-1001",
				ClienteNombre: "JUAN PEREZ",
				Articulos:     []domain.ArticuloComprobante{articuloValido()},
				Saldo:         decimal.NewFromInt(-10),
			},
			want: domain.ErrComprobanteVentaSaldoNegativo,
		},
		{
			name: "sin articulos",
			got: domain.NewComprobanteVentaParams{
				Folio:         "VENTA-1001",
				ClienteNombre: "JUAN PEREZ",
				Articulos:     nil,
			},
			want: domain.ErrComprobanteVentaSinArticulos,
		},
		{
			name: "articulo sin descripcion",
			got: domain.NewComprobanteVentaParams{
				Folio:         "VENTA-1001",
				ClienteNombre: "JUAN PEREZ",
				Articulos: []domain.ArticuloComprobante{
					domain.HydrateArticuloComprobante(domain.HydrateArticuloComprobanteParams{
						Descripcion:    " ",
						Cantidad:       decimal.NewFromInt(1),
						PrecioUnitario: decimal.NewFromInt(100),
						Importe:        decimal.NewFromInt(100),
					}),
				},
			},
			want: domain.ErrArticuloDescripcionRequerida,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewComprobanteVenta(tc.got)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestComprobanteVenta_ArticulosDefensiveCopy(t *testing.T) {
	t.Parallel()
	c, err := domain.NewComprobanteVenta(domain.NewComprobanteVentaParams{
		Folio:         "VENTA-1001",
		ClienteNombre: "JUAN PEREZ",
		Articulos:     []domain.ArticuloComprobante{articuloValido()},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	got := c.Articulos()
	got[0] = domain.HydrateArticuloComprobante(domain.HydrateArticuloComprobanteParams{Descripcion: "MUTADO"})
	if c.Articulos()[0].Descripcion() == "MUTADO" {
		t.Fatal("Articulos() returned the backing slice; mutation leaked")
	}
}

func TestHydrateComprobanteVenta_BypassesValidation(t *testing.T) {
	t.Parallel()
	c := domain.HydrateComprobanteVenta(domain.HydrateComprobanteVentaParams{
		Folio:         "",
		ClienteNombre: "",
		Total:         decimal.NewFromInt(-999),
		Articulos:     nil,
	})
	if c.Folio() != "" {
		t.Fatalf("Folio() = %q, want empty", c.Folio())
	}
	if c.Total().Cmp(decimal.NewFromInt(-999)) != 0 {
		t.Fatalf("Total() = %v", c.Total())
	}
	if c.Articulos() != nil {
		t.Fatalf("Articulos() = %+v, want nil", c.Articulos())
	}
}

func TestNewArticuloComprobante_HappyPath(t *testing.T) {
	t.Parallel()
	a, err := domain.NewArticuloComprobante(domain.NewArticuloComprobanteParams{
		Descripcion:    "Lavadora 20 kg",
		Cantidad:       decimal.NewFromInt(2),
		PrecioUnitario: decimal.NewFromInt(8900),
		Importe:        decimal.NewFromInt(17800),
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if a.Descripcion() != "Lavadora 20 kg" {
		t.Fatalf("Descripcion() = %q", a.Descripcion())
	}
	if a.Cantidad().Cmp(decimal.NewFromInt(2)) != 0 {
		t.Fatalf("Cantidad() = %v", a.Cantidad())
	}
	if a.PrecioUnitario().Cmp(decimal.NewFromInt(8900)) != 0 {
		t.Fatalf("PrecioUnitario() = %v", a.PrecioUnitario())
	}
	if a.Importe().Cmp(decimal.NewFromInt(17800)) != 0 {
		t.Fatalf("Importe() = %v", a.Importe())
	}
}

func TestNewArticuloComprobante_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  domain.NewArticuloComprobanteParams
		want error
	}{
		{
			name: "descripcion vacia",
			got: domain.NewArticuloComprobanteParams{
				Descripcion:    "",
				Cantidad:       decimal.NewFromInt(1),
				PrecioUnitario: decimal.NewFromInt(1),
				Importe:        decimal.NewFromInt(1),
			},
			want: domain.ErrArticuloDescripcionRequerida,
		},
		{
			name: "cantidad negativa",
			got: domain.NewArticuloComprobanteParams{
				Descripcion:    "Sofa",
				Cantidad:       decimal.NewFromInt(-1),
				PrecioUnitario: decimal.NewFromInt(1),
				Importe:        decimal.NewFromInt(1),
			},
			want: domain.ErrArticuloCantidadNegativa,
		},
		{
			name: "precio unitario negativo",
			got: domain.NewArticuloComprobanteParams{
				Descripcion:    "Sofa",
				Cantidad:       decimal.NewFromInt(1),
				PrecioUnitario: decimal.NewFromInt(-1),
				Importe:        decimal.NewFromInt(1),
			},
			want: domain.ErrArticuloPrecioUnitarioNegativo,
		},
		{
			name: "importe negativo",
			got: domain.NewArticuloComprobanteParams{
				Descripcion:    "Sofa",
				Cantidad:       decimal.NewFromInt(1),
				PrecioUnitario: decimal.NewFromInt(1),
				Importe:        decimal.NewFromInt(-1),
			},
			want: domain.ErrArticuloImporteNegativo,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewArticuloComprobante(tc.got)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestHydrateArticuloComprobante_BypassesValidation(t *testing.T) {
	t.Parallel()
	a := domain.HydrateArticuloComprobante(domain.HydrateArticuloComprobanteParams{
		Descripcion:    "",
		Cantidad:       decimal.NewFromInt(-3),
		PrecioUnitario: decimal.NewFromInt(-7),
		Importe:        decimal.NewFromInt(-11),
	})
	if a.Cantidad().Cmp(decimal.NewFromInt(-3)) != 0 {
		t.Fatalf("Cantidad() = %v", a.Cantidad())
	}
	if a.Descripcion() != "" {
		t.Fatalf("Descripcion() = %q", a.Descripcion())
	}
}
