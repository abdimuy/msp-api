package clientespdf

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/go-pdf/fpdf"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// TestNewPagoCols_PreservesImporteWidth locks in that narrowing CONCEPTO and
// widening DESCRIPCIÓN keeps their sum (and thus the IMPORTE remainder) equal to
// the previous layout (66 + 52 == 118).
func TestNewPagoCols_PreservesImporteWidth(t *testing.T) {
	t.Parallel()

	c := newPagoCols()

	require.InDelta(t, 118.0, c.concepto+c.descripcion, 1e-9,
		"concepto+descripcion must stay 118 so importe (remainder) is unchanged")
	require.Greater(t, c.descripcion, c.concepto,
		"descripcion must be clearly wider than concepto")

	// importe equals the remainder under the previous concepto=66, cobrador=52.
	oldImporte := bodyW - c.mes - c.fecha - 66.0 - 52.0
	require.InDelta(t, oldImporte, c.importe, 1e-9)
}

// TestRowHeight covers the max(baseH, nLines*lineH) math used for pagination.
func TestRowHeight(t *testing.T) {
	t.Parallel()

	const baseH = 4.5 // pagoLineH is 4.0

	require.InDelta(t, baseH, rowHeight(0, baseH), 1e-9, "0 lines clamps to at least 1 line, then baseH")
	require.InDelta(t, baseH, rowHeight(1, baseH), 1e-9, "single line keeps base height")
	require.InDelta(t, 8.0, rowHeight(2, baseH), 1e-9)
	require.InDelta(t, 12.0, rowHeight(3, baseH), 1e-9)
}

// TestPagoRowHeight_MultiLineGrowsRow verifies a long descripcion produces a
// taller row than a short one at the descripcion column width.
func TestPagoRowHeight_MultiLineGrowsRow(t *testing.T) {
	t.Parallel()

	pdf := fpdf.New("P", "mm", "Letter", "")
	require.NoError(t, loadFonts(pdf))
	pdf.AddPage()

	cols := newPagoCols()
	tp := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	mk := func(desc string) outbound.ReportePago {
		return outbound.ReportePago{Fecha: tp("2024-03-15"), Cobrador: desc, Importe: decimal.NewFromInt(1)}
	}

	short := pagoRowHeight(pdf, mk("RUTA 36"), cols)
	require.InDelta(t, cols.rowH, short, 1e-9, "short text stays a single-line row")

	long := pagoRowHeight(pdf, mk("CONDONACIÓN POR ANTIGÜEDAD APROBADA POR LA GERENCIA DE COBRANZA POR CLIENTE EN SITUACIÓN VULNERABLE Y DE LARGO PLAZO"), cols)
	require.Greater(t, long, cols.rowH, "long text must wrap to more than one line")
	require.InDelta(t, 0.0, long-float64(int(long/pagoLineH))*pagoLineH, 1e-9,
		"tall row height is a whole number of line heights")
}
