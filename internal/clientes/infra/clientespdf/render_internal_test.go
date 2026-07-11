package clientespdf

import (
	"strings"
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

// TestEstatusBadge covers the status-code → Spanish badge mapping: only V and C
// produce a badge; A/B and unknown codes do not. Input is normalized.
func TestEstatusBadge(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in       string
		wantText string
		wantOK   bool
	}{
		{"V", "VETADO", true},
		{"C", "CANCELADO", true},
		{" v ", "VETADO", true},
		{"c", "CANCELADO", true},
		{"A", "", false},
		{"B", "", false},
		{"", "", false},
		{"X", "", false},
	}
	for _, tc := range cases {
		text, ok := estatusBadge(tc.in)
		require.Equal(t, tc.wantOK, ok, "estatusBadge(%q) ok", tc.in)
		require.Equal(t, tc.wantText, text, "estatusBadge(%q) text", tc.in)
	}
}

// TestRowHeight covers the max(baseH, nLines*pagoLineH) math used for pagination.
func TestRowHeight(t *testing.T) {
	t.Parallel()

	baseH := newPagoCols().rowH // derive from code so the test can't drift

	require.InDelta(t, baseH, rowHeight(0, baseH), 1e-9, "0 lines clamps to at least 1 line, then baseH")
	require.InDelta(t, baseH, rowHeight(1, baseH), 1e-9, "single line keeps base height")
	require.InDelta(t, 2*pagoLineH, rowHeight(2, baseH), 1e-9)
	require.InDelta(t, 3*pagoLineH, rowHeight(3, baseH), 1e-9)
}

// TestMaxLinesForRowHeight covers the line budget derived from a row height.
func TestMaxLinesForRowHeight(t *testing.T) {
	t.Parallel()

	require.Equal(t, 1, maxLinesForRowHeight(0), "never below one line")
	require.Equal(t, 1, maxLinesForRowHeight(newPagoCols().rowH), "base height is a single line")
	require.Equal(t, 2, maxLinesForRowHeight(2*pagoLineH))
	require.Equal(t, 3, maxLinesForRowHeight(3*pagoLineH))
	require.Equal(t, 60, maxLinesForRowHeight(60*pagoLineH))
}

// TestClampDescForHeight_TruncatesOnlyWhenNeeded verifies the descripcion clamp
// leaves short text alone and shortens over-tall text to exactly maxLines lines
// with an ellipsis marker.
func TestClampDescForHeight_TruncatesOnlyWhenNeeded(t *testing.T) {
	t.Parallel()

	pdf := fpdf.New("P", "mm", "Letter", "")
	require.NoError(t, loadFonts(pdf))
	pdf.AddPage()
	pdf.SetFont("Poppins", "", 7.5)

	cols := newPagoCols()

	// Fits within budget → returned verbatim.
	short := "RUTA 36 - OSCAR ROQUE"
	require.Equal(t, short, clampDescForHeight(pdf, short, cols.descripcion, 3))

	// Needs many lines but capped to 2 → exactly 2 lines, last one ellipsized.
	long := strings.Repeat("PALABRA ", 400)
	got := clampDescForHeight(pdf, long, cols.descripcion, 2)
	require.NotEqual(t, long, got, "over-tall text must be shortened")
	require.Len(t, pdf.SplitText(got, cols.descripcion), 2, "clamped text must render in exactly maxLines lines")
	require.Contains(t, got, "…", "truncation must be signalled with an ellipsis")
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

// TestDrawPagoRows_PathologicalRowNeverOverprintsFooter guards the unbounded
// tall-row overflow: a single payment whose descripcion wraps to more than a
// full page must not render past bottomLimit into the footer band. If the loop's
// height cap were removed, the cursor would end far below bottomLimit and this
// would fail (it also asserts the render does not panic).
func TestDrawPagoRows_PathologicalRowNeverOverprintsFooter(t *testing.T) {
	t.Parallel()

	pdf := fpdf.New("P", "mm", "Letter", "")
	require.NoError(t, loadFonts(pdf))
	pdf.SetAutoPageBreak(false, margin)
	pdf.AddPage()

	cols := newPagoCols()
	drawColHdr := makePagosColumnHeader(pdf, cols)
	drawColHdr()
	card := newVentaCard(pdf, false)

	tp := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	// A description that wraps to far more than a full page of lines (a full
	// usable page fits ~60 lines; this is well beyond that, so the loop's cap
	// must engage).
	huge := strings.Repeat("PALABRA ", 2000)
	pagos := []outbound.ReportePago{
		{Fecha: tp("2024-03-15"), Concepto: "Cobranza", Cobrador: huge, Importe: decimal.NewFromInt(100), EsIngreso: true},
	}

	require.NotPanics(t, func() {
		drawPagoRows(pdf, "V-1", pagos, cols, drawColHdr, card)
	})
	require.LessOrEqual(t, pdf.GetY(), bottomLimit+0.01,
		"tall row must be capped so the cursor never lands past the footer limit")
}
