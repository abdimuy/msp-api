package render_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
	"github.com/abdimuy/msp-api/internal/comprobantes/infra/render"
)

func date(s string) time.Time {
	v, _ := time.Parse("2006-01-02 15:04:05", s)
	return v
}

func ventaFixture() domain.ComprobanteVenta {
	art1, err := domain.NewArticuloComprobante(domain.NewArticuloComprobanteParams{
		Descripcion:    "Refrigerador 16 pies",
		Cantidad:       decimal.NewFromInt(1),
		PrecioUnitario: decimal.NewFromFloat(9999.50),
		Importe:        decimal.NewFromFloat(9999.50),
	})
	if err != nil {
		panic(err)
	}
	art2, err := domain.NewArticuloComprobante(domain.NewArticuloComprobanteParams{
		Descripcion:    "Estufa 4 hornillas",
		Cantidad:       decimal.NewFromInt(1),
		PrecioUnitario: decimal.NewFromFloat(4999.99),
		Importe:        decimal.NewFromFloat(4999.99),
	})
	if err != nil {
		panic(err)
	}
	total := decimal.NewFromFloat(14999.49)
	enganche := decimal.NewFromFloat(3000)
	v, err := domain.NewComprobanteVenta(domain.NewComprobanteVentaParams{
		Folio:            "V-2026-09-000123",
		Fecha:            date("2026-09-01 12:00:00"),
		ClienteNombre:    "María José Niña Hernández López",
		ClienteDomicilio: "Av. Juárez 123, Col. Centro, Villahermosa, Tabasco",
		Articulos:        []domain.ArticuloComprobante{art1, art2},
		Total:            total,
		Enganche:         enganche,
		Saldo:            total.Sub(enganche),
		PlanPago:         "12 mensualidades de $1,000.00 sin intereses",
		Vendedor:         "Carmen López García",
	})
	if err != nil {
		panic(err)
	}
	return v
}

func pagoFixture() domain.ComprobantePago {
	p, err := domain.NewComprobantePago(domain.NewComprobantePagoParams{
		Folio:         "P-2026-09-000456",
		Fecha:         date("2026-09-02 09:00:00"),
		ClienteNombre: "María José Niña Hernández López",
		Monto:         decimal.NewFromFloat(1000),
		FormaCobro:    "Efectivo",
		VentaFolio:    "V-2026-09-000123",
		SaldoRestante: decimal.NewFromFloat(13999.49),
		Cobrador:      "Luis Ramírez Pérez",
	})
	if err != nil {
		panic(err)
	}
	return p
}

func TestVenta_Renders(t *testing.T) {
	t.Parallel()
	got, err := render.NewPDFRenderer().Venta(context.Background(), ventaFixture())
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(got, []byte("%PDF-")), "must start with %PDF-")
	require.Greater(t, len(got), 2000, "a real document is more than a few KB")
}

func TestPago_Renders(t *testing.T) {
	t.Parallel()
	got, err := render.NewPDFRenderer().Pago(context.Background(), pagoFixture())
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(got, []byte("%PDF-")), "must start with %PDF-")
	require.Greater(t, len(got), 2000, "a real document is more than a few KB")
}

// TestVenta_Deterministic locks that rendering the same model twice yields
// identical bytes: the creation/modification date is pinned to the model's own
// timestamp, never time.Now(), so the document is reproducible.
func TestVenta_Deterministic(t *testing.T) {
	t.Parallel()
	r := render.NewPDFRenderer()
	ctx := context.Background()
	fx := ventaFixture()
	b1, err := r.Venta(ctx, fx)
	require.NoError(t, err)
	b2, err := r.Venta(ctx, fx)
	require.NoError(t, err)
	assert.Equal(t, b1, b2, "rendering the same model twice must be byte-identical")
}

func TestVenta_LongAccentsAndNye(t *testing.T) {
	t.Parallel()
	got, err := render.NewPDFRenderer().Venta(context.Background(), ventaFixture())
	require.NoError(t, err)
	require.Greater(t, len(got), 3000)
}

func TestPago_Long(t *testing.T) {
	t.Parallel()
	got, err := render.NewPDFRenderer().Pago(context.Background(), pagoFixture())
	require.NoError(t, err)
	require.Greater(t, len(got), 2000)
}

// TestVenta_20Articulos locks the brief requirement that a sale with many
// articles — twenty — renders without crashing and without error.
func TestVenta_20Articulos(t *testing.T) {
	t.Parallel()
	const n = 20
	articulos := make([]domain.ArticuloComprobante, 0, n)
	total := decimal.Zero
	for i := range n {
		importe := decimal.NewFromInt(1000 + int64(i))
		art, err := domain.NewArticuloComprobante(domain.NewArticuloComprobanteParams{
			Descripcion:    "Artículo de prueba número " + string(rune('A'+i%26)) + " con descripción larga que fluye en la tabla",
			Cantidad:       decimal.NewFromInt(int64(i%3 + 1)),
			PrecioUnitario: importe,
			Importe:        importe.Mul(decimal.NewFromInt(int64(i%3 + 1))),
		})
		require.NoError(t, err)
		articulos = append(articulos, art)
		total = total.Add(importe.Mul(decimal.NewFromInt(int64(i%3 + 1))))
	}
	v, err := domain.NewComprobanteVenta(domain.NewComprobanteVentaParams{
		Folio:            "V-2026-09-000777",
		Fecha:            date("2026-09-03 12:00:00"),
		ClienteNombre:    "Cliente con veinte artículos S.A. de C.V.",
		ClienteDomicilio: "Calle Larga 456, Col. Centro, Villahermosa, Tabasco",
		Articulos:        articulos,
		Total:            total,
		Enganche:         decimal.NewFromInt(5000),
		Saldo:            total.Sub(decimal.NewFromInt(5000)),
		PlanPago:         "24 mensualidades sin intereses",
		Vendedor:         "Vendedora de prueba",
	})
	require.NoError(t, err)

	got, err := render.NewPDFRenderer().Venta(context.Background(), v)
	require.NoError(t, err, "a sale with twenty articles must render without error")
	require.True(t, bytes.HasPrefix(got, []byte("%PDF-")), "must start with %PDF-")
	require.Greater(t, len(got), 2000, "a twenty-article document is not a few hundred bytes")
}

// TestPago_SaldoRestanteCero locks the brief requirement that a payment
// receipt with zero remaining balance — the client who just settled in full —
// renders just the same, without error.
func TestPago_SaldoRestanteCero(t *testing.T) {
	t.Parallel()
	fx := pagoFixture()
	zeroPago, err := domain.NewComprobantePago(domain.NewComprobantePagoParams{
		Folio:         "P-2026-09-000900",
		Fecha:         fx.Fecha(),
		ClienteNombre: fx.ClienteNombre(),
		Monto:         fx.Monto(),
		FormaCobro:    fx.FormaCobro(),
		VentaFolio:    fx.VentaFolio(),
		SaldoRestante: decimal.Zero,
		Cobrador:      fx.Cobrador(),
	})
	require.NoError(t, err)

	got, err := render.NewPDFRenderer().Pago(context.Background(), zeroPago)
	require.NoError(t, err, "a payment receipt with zero remaining balance renders without error")
	require.True(t, bytes.HasPrefix(got, []byte("%PDF-")), "must start with %PDF-")
}
