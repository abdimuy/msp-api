package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestNewComprobantePago_HappyPath(t *testing.T) {
	t.Parallel()
	fecha := time.Date(2026, 8, 10, 15, 30, 0, 0, time.UTC)
	c, err := domain.NewComprobantePago(domain.NewComprobantePagoParams{
		Folio:         "1-15",
		Fecha:         fecha,
		ClienteNombre: "JUAN PEREZ",
		Monto:         decimal.NewFromInt(7500),
		FormaCobro:    "efectivo",
		VentaFolio:    "VENTA-1001",
		SaldoRestante: decimal.NewFromInt(0),
		Cobrador:      "LUIS MARTINEZ",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.Folio() != "1-15" {
		t.Fatalf("Folio() = %q, want 1-15", c.Folio())
	}
	if !c.Fecha().Equal(fecha) {
		t.Fatalf("Fecha() = %v, want %v", c.Fecha(), fecha)
	}
	if c.ClienteNombre() != "JUAN PEREZ" {
		t.Fatalf("ClienteNombre() = %q", c.ClienteNombre())
	}
	if c.Monto().Cmp(decimal.NewFromInt(7500)) != 0 {
		t.Fatalf("Monto() = %v", c.Monto())
	}
	if c.FormaCobro() != "efectivo" {
		t.Fatalf("FormaCobro() = %q", c.FormaCobro())
	}
	if c.VentaFolio() != "VENTA-1001" {
		t.Fatalf("VentaFolio() = %q", c.VentaFolio())
	}
	if c.SaldoRestante().Cmp(decimal.NewFromInt(0)) != 0 {
		t.Fatalf("SaldoRestante() = %v", c.SaldoRestante())
	}
	if c.Cobrador() != "LUIS MARTINEZ" {
		t.Fatalf("Cobrador() = %q", c.Cobrador())
	}
}

func TestNewComprobantePago_Validation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  domain.NewComprobantePagoParams
		want error
	}{
		{
			name: "folio vacio",
			got: domain.NewComprobantePagoParams{
				ClienteNombre: "JUAN PEREZ",
				VentaFolio:    "VENTA-1001",
			},
			want: domain.ErrComprobantePagoFolioRequerido,
		},
		{
			name: "cliente vacio",
			got: domain.NewComprobantePagoParams{
				Folio:      "1-15",
				VentaFolio: "VENTA-1001",
			},
			want: domain.ErrComprobantePagoClienteRequerido,
		},
		{
			name: "venta folio vacio",
			got: domain.NewComprobantePagoParams{
				Folio:         "1-15",
				ClienteNombre: "JUAN PEREZ",
			},
			want: domain.ErrComprobantePagoVentaFolioRequerido,
		},
		{
			name: "monto negativo",
			got: domain.NewComprobantePagoParams{
				Folio:         "1-15",
				ClienteNombre: "JUAN PEREZ",
				VentaFolio:    "VENTA-1001",
				Monto:         decimal.NewFromInt(-1),
			},
			want: domain.ErrComprobantePagoMontoNegativo,
		},
		{
			name: "saldo restante negativo",
			got: domain.NewComprobantePagoParams{
				Folio:         "1-15",
				ClienteNombre: "JUAN PEREZ",
				VentaFolio:    "VENTA-1001",
				SaldoRestante: decimal.NewFromInt(-1),
			},
			want: domain.ErrComprobantePagoSaldoRestanteNegativo,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := domain.NewComprobantePago(tc.got)
			if !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestHydrateComprobantePago_BypassesValidation(t *testing.T) {
	t.Parallel()
	c := domain.HydrateComprobantePago(domain.HydrateComprobantePagoParams{
		Folio:         "",
		ClienteNombre: "",
		VentaFolio:    "",
		Monto:         decimal.NewFromInt(-999),
	})
	if c.Folio() != "" {
		t.Fatalf("Folio() = %q", c.Folio())
	}
	if c.Monto().Cmp(decimal.NewFromInt(-999)) != 0 {
		t.Fatalf("Monto() = %v", c.Monto())
	}
}
