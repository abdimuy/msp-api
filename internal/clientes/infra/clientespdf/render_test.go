package clientespdf_test

import (
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

func TestRenderSample(t *testing.T) {
	t.Parallel()

	sample := buildSample()
	gen := time.Date(2026, 6, 20, 14, 30, 0, 0, time.UTC)

	got, err := clientespdf.Render(sample, gen)
	require.NoError(t, err)
	require.Greater(t, len(got), 5000, "pdf demasiado pequeño: %d bytes", len(got))
	require.Equal(t, "%PDF", string(got[:4]))

	// Idempotency check
	got2, err := clientespdf.Render(sample, gen)
	require.NoError(t, err)
	require.Len(t, got2, len(got), "tamaño no idempotente")

	if out := os.Getenv("REPORTE_PDF_OUT"); out != "" {
		require.NoError(t, os.WriteFile(out, got, 0o644))
	}
}

func buildSample() outbound.ReporteCliente {
	d := func(s string) decimal.Decimal { v, _ := decimal.NewFromString(s); return v }
	tp := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	// pago helpers — collected money vs forgiven/write-off, with category set.
	ing := func(fecha, concepto, cobrador, importe, cat string) outbound.ReportePago {
		return outbound.ReportePago{Fecha: tp(fecha), Concepto: concepto, Cobrador: cobrador, Importe: d(importe), EsIngreso: true, Categoria: cat}
	}
	noing := func(fecha, concepto, cobrador, importe, cat string) outbound.ReportePago {
		return outbound.ReportePago{Fecha: tp(fecha), Concepto: concepto, Cobrador: cobrador, Importe: d(importe), EsIngreso: false, Categoria: cat}
	}

	return outbound.ReporteCliente{
		Cliente: outbound.ReporteClienteDatos{
			ID:        4821,
			Nombre:    "BEATRIZ ELENA MORFÍN TORRES",
			Direccion: "Calle Independencia 347, Col. Centro, Zapopan",
			Telefono:  "331-204-5876",
			Zona:      "Zona Norte",
			Cobrador:  "RUTA 36 - OSCAR ROQUE",
			Notas:     "CASA DE DOS PISOS CON PORTÓN NEGRO. 26-06-2024 SE DEJARON MÁS CUENTAS AQUÍ A NOMBRE DE MÓNICA Y JULIA. CLIENTA SE COMPROMETE A PONERSE AL CORRIENTE LA PRÓXIMA SEMANA. ESTUVO GASTADA EN GASTOS MÉDICOS.",
		},
		Resumen: outbound.ResumenFicha{
			TotalComprado: d("48500.00"),
			TotalAbonado:  d("31200.00"),
			SaldoTotal:    d("17300.00"),
			PctLiquidado:  d("64.3"),
			NumVentas:     2,
			NumPagos:      14,
		},
		Ventas: []outbound.ReporteVenta{
			{
				DoctoPvID: 10231,
				Folio:     "V-10231",
				Fecha:     tp("2024-03-15"),
				Almacen:   "Almacén Central",
				Total:     d("18500.00"),
				Saldo:     d("0.00"),
				Liquidada: true,
				Pagos: []outbound.ReportePago{
					ing("2024-03-15", "Enganche", "RUTA 36 - OSCAR ROQUE", "3700.00", "enganche"),
					ing("2024-04-10", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "2000.00", "pago"),
					ing("2024-05-08", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "2000.00", "pago"),
					ing("2024-06-12", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "2000.00", "pago"),
					ing("2024-07-09", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "2000.00", "pago"),
					ing("2024-08-14", "Abono especial", "CAJA - LAURA JIMÉNEZ", "6800.00", "pago"),
				},
			},
			{
				DoctoPvID: 11047,
				Folio:     "V-11047",
				Fecha:     tp("2025-01-20"),
				Almacen:   "Almacén Central",
				Total:     d("30000.00"),
				Saldo:     d("17300.00"),
				Liquidada: false,
				Pagos: []outbound.ReportePago{
					ing("2025-01-20", "Enganche", "RUTA 36 - OSCAR ROQUE", "6000.00", "enganche"),
					ing("2025-02-18", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "1500.00", "pago"),
					ing("2025-03-19", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "1500.00", "pago"),
					ing("2025-04-16", "Cobranza en ruta", "RUTA 36 - OSCAR ROQUE", "1500.00", "pago"),
					noing("2025-05-14", "Condonaciones", "CONDONACION POR ANTIGÜEDAD", "1500.00", "condonacion"),
					noing("2025-06-11", "Mal cliente", "RUTA 36 - OSCAR ROQUE", "700.00", "perdida"),
				},
			},
		},
	}
}
