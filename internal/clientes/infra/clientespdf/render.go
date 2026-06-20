//nolint:misspell // Spanish domain vocabulary by project convention.
package clientespdf

import (
	"bytes"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

//go:embed fonts/*.ttf
var fontsFS embed.FS

// palette constants — RGB tuples.
const (
	inkR, inkG, inkB             = 26, 26, 26
	grayR, grayG, grayB          = 120, 120, 120
	hairR, hairG, hairB          = 229, 231, 235
	slateR, slateG, slateB       = 51, 65, 85
	greenR, greenG, greenB       = 22, 163, 74
	amberR, amberG, amberB       = 217, 119, 6
	violetR, violetG, violetB    = 124, 77, 196 // condonación (matches the app's violet)
	redR, redG, redB             = 200, 50, 50  // pérdida / fuga / mal cliente
	altFillR, altFillG, altFillB = 248, 249, 250
)

// Payment categories that are NOT real collected money (shown apart, in color).
const (
	catCondonacion = "condonacion"
	catPerdida     = "perdida"
)

// pagoColor returns the ink color for a payment row by category: violet for
// condonación, red for pérdida, normal ink for collected money.
func pagoColor(p outbound.ReportePago) (int, int, int) {
	if p.EsIngreso {
		return inkR, inkG, inkB
	}
	if p.Categoria == catPerdida {
		return redR, redG, redB
	}
	return violetR, violetG, violetB
}

// subtotalLine is one line of a venta's payment subtotal (label + tinted amount).
type subtotalLine struct {
	label   string
	amount  decimal.Decimal
	r, g, b int
}

// buildSubtotales returns the subtotal lines for a venta: ABONADO always, plus
// CONDONADO / PÉRDIDA only when those movements are present.
func buildSubtotales(ingreso, condon, perdida decimal.Decimal) []subtotalLine {
	lines := []subtotalLine{{"ABONADO", ingreso, inkR, inkG, inkB}}
	if condon.IsPositive() {
		lines = append(lines, subtotalLine{"CONDONADO", condon, violetR, violetG, violetB})
	}
	if perdida.IsPositive() {
		lines = append(lines, subtotalLine{"PÉRDIDA", perdida, redR, redG, redB})
	}
	return lines
}

// sumarCategorias totals payments across all ventas, split into collected income,
// condonación, and pérdida.
func sumarCategorias(ventas []outbound.ReporteVenta) (decimal.Decimal, decimal.Decimal, decimal.Decimal) {
	var ingreso, condon, perdida decimal.Decimal
	for _, v := range ventas {
		for _, p := range v.Pagos {
			switch {
			case p.EsIngreso:
				ingreso = ingreso.Add(p.Importe)
			case p.Categoria == catPerdida:
				perdida = perdida.Add(p.Importe)
			default:
				condon = condon.Add(p.Importe)
			}
		}
	}
	return ingreso, condon, perdida
}

// Letter size in mm.
const (
	pageW  = 215.9
	pageH  = 279.4
	margin = 18.0
	bodyW  = pageW - 2*margin
	// bottomLimit is the lowest Y content may reach before a manual page break,
	// leaving room for the footer (drawn at SetY(-14)).
	bottomLimit = pageH - 20
)

var meses = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

// Render produces the PDF bytes for a client report.
// gen is the generation timestamp (injected for deterministic tests).
func Render(rep outbound.ReporteCliente, gen time.Time) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(margin, margin, margin)
	// Pagination is managed manually (see drawVenta / drawPagosTable) so venta
	// headers are never orphaned and long tables repeat their context on
	// continuation pages. fpdf's automatic break would split sections blindly.
	pdf.SetAutoPageBreak(false, margin)
	pdf.AliasNbPages("{nb}")

	if err := loadFonts(pdf); err != nil {
		return nil, fmt.Errorf("cargar fuentes: %w", err)
	}

	pdf.SetFooterFunc(func() {
		drawFooter(pdf, gen)
	})

	pdf.AddPage()

	ingreso, condon, perdida := sumarCategorias(rep.Ventas)

	drawMasthead(pdf, gen)
	drawClienteBlock(pdf, rep.Cliente)
	drawResumenBand(pdf, rep.Resumen, ingreso, condon.Add(perdida))
	drawVentas(pdf, rep.Ventas)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("generar pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// loadFonts registers all project fonts using AddUTF8FontFromBytes.
func loadFonts(pdf *fpdf.Fpdf) error {
	fonts := []struct {
		family string
		file   string
	}{
		{"Poppins", "fonts/Poppins-Regular.ttf"},
		{"PoppinsSB", "fonts/Poppins-SemiBold.ttf"},
		{"PoppinsB", "fonts/Poppins-Bold.ttf"},
		{"PlexMono", "fonts/IBMPlexMono-Regular.ttf"},
		{"PlexMonoMed", "fonts/IBMPlexMono-Medium.ttf"},
	}
	for _, f := range fonts {
		b, err := fontsFS.ReadFile(f.file)
		if err != nil {
			return fmt.Errorf("leer fuente %s: %w", f.file, err)
		}
		pdf.AddUTF8FontFromBytes(f.family, "", b)
		if pdf.Error() != nil {
			return fmt.Errorf("registrar fuente %s: %w", f.family, pdf.Error())
		}
	}
	return nil
}

// drawMasthead renders the top header: company name, report title, date and rule.
func drawMasthead(pdf *fpdf.Fpdf, gen time.Time) {
	// Row 1: company name (left) + generation date (right)
	pdf.SetTextColor(slateR, slateG, slateB)
	pdf.SetFont("PoppinsSB", "", 7.5)
	pdf.CellFormat(bodyW/2, 5, "MUEBLERÍA MSP", "", 0, "L", false, 0, "")
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.SetFont("PlexMono", "", 7.5)
	pdf.CellFormat(bodyW/2, 5, formatFechaHora(gen), "", 1, "R", false, 0, "")

	// Row 2: report title
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.SetFont("PoppinsB", "", 22)
	pdf.CellFormat(bodyW, 11, "Reporte de cliente", "", 1, "L", false, 0, "")

	// Slate horizontal rule
	pdf.SetDrawColor(slateR, slateG, slateB)
	pdf.SetLineWidth(0.6)
	y := pdf.GetY() + 1
	pdf.Line(margin, y, pageW-margin, y)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(hairR, hairG, hairB)
	pdf.Ln(5)
}

// drawClienteBlock renders the client identity section.
func drawClienteBlock(pdf *fpdf.Fpdf, c outbound.ReporteClienteDatos) {
	// Name
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.SetFont("PoppinsSB", "", 14)
	pdf.CellFormat(bodyW, 8, c.Nombre, "", 1, "L", false, 0, "")
	pdf.Ln(1)

	// 2-column key/value pairs
	colW := bodyW / 2
	type kv struct{ k, v string }
	pairs := []kv{
		{"ID", strconv.Itoa(c.ID)},
		{"Teléfono", c.Telefono},
		{"Zona", c.Zona},
		{"Cobrador", c.Cobrador},
	}

	rowH := 5.5
	for i := 0; i < len(pairs); i += 2 {
		left := pairs[i]
		// label
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.SetFont("PlexMono", "", 7)
		pdf.CellFormat(colW/3, rowH, strings.ToUpper(left.k), "", 0, "L", false, 0, "")
		// value
		pdf.SetTextColor(inkR, inkG, inkB)
		pdf.SetFont("Poppins", "", 9)
		if i+1 < len(pairs) {
			right := pairs[i+1]
			pdf.CellFormat(colW-colW/3, rowH, left.v, "", 0, "L", false, 0, "")
			// label right
			pdf.SetTextColor(grayR, grayG, grayB)
			pdf.SetFont("PlexMono", "", 7)
			pdf.CellFormat(colW/3, rowH, strings.ToUpper(right.k), "", 0, "L", false, 0, "")
			// value right
			pdf.SetTextColor(inkR, inkG, inkB)
			pdf.SetFont("Poppins", "", 9)
			pdf.CellFormat(colW-colW/3, rowH, right.v, "", 1, "L", false, 0, "")
		} else {
			pdf.CellFormat(bodyW-colW/3, rowH, left.v, "", 1, "L", false, 0, "")
		}
	}

	// Dirección — full width. Label width matches the key/value rows above
	// (colW/3 == bodyW/6) so the value left-aligns with ID/Zona, not centered.
	labelW := colW / 3
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.SetFont("PlexMono", "", 7)
	pdf.CellFormat(labelW, rowH, "DIRECCIÓN", "", 0, "L", false, 0, "")
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.SetFont("Poppins", "", 9)
	pdf.CellFormat(bodyW-labelW, rowH, c.Direccion, "", 1, "L", false, 0, "")

	pdf.Ln(3)
}

// drawResumenBand renders the 6-metric financial summary strip. abonadoIngreso
// is collected money only (excludes condonación/pérdida); noCobrado is the sum
// of forgiven debt + write-offs — so ABONADO here means real cash, coherent with
// the per-venta subtotals and the rest of the app.
func drawResumenBand(pdf *fpdf.Fpdf, r outbound.ResumenFicha, abonadoIngreso, noCobrado decimal.Decimal) {
	bandH := 16.0
	colW := bodyW / 6
	y := pdf.GetY()

	// Top and bottom hairlines
	pdf.SetDrawColor(hairR, hairG, hairB)
	pdf.SetLineWidth(0.2)
	pdf.Line(margin, y, pageW-margin, y)
	pdf.Line(margin, y+bandH, pageW-margin, y+bandH)

	type metric struct {
		label string
		value string
	}
	metrics := []metric{
		{"COMPRADO", formatMXN(r.TotalComprado)},
		{"ABONADO", formatMXN(abonadoIngreso)},
		{"NO COBRADO", formatMXN(noCobrado)},
		{"SALDO", formatMXN(r.SaldoTotal)},
		{"% LIQUIDADO", r.PctLiquidado.StringFixed(1) + "%"},
		{"# VENTAS", strconv.Itoa(r.NumVentas)},
	}

	for i, m := range metrics {
		x := margin + float64(i)*colW
		// Vertical separator (skip first)
		if i > 0 {
			pdf.Line(x, y+2, x, y+bandH-2)
		}
		// Label
		pdf.SetFont("PlexMono", "", 6.5)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.SetXY(x, y+2.5)
		pdf.CellFormat(colW, 5, m.label, "", 0, "C", false, 0, "")
		// Value
		pdf.SetFont("PlexMonoMed", "", 10.5)
		pdf.SetTextColor(inkR, inkG, inkB)
		pdf.SetXY(x, y+7.5)
		pdf.CellFormat(colW, 6, m.value, "", 0, "C", false, 0, "")
	}

	pdf.SetXY(margin, y+bandH+4)
}

// drawVentas renders per-venta sections with payment tables.
func drawVentas(pdf *fpdf.Fpdf, ventas []outbound.ReporteVenta) {
	for _, v := range ventas {
		drawVenta(pdf, v)
	}
}

// drawVenta renders one venta section with its payment table.
func drawVenta(pdf *fpdf.Fpdf, v outbound.ReporteVenta) {
	// Keep the venta header with the start of its table: if too little room
	// remains, start on a fresh page so the folio is never orphaned at the
	// bottom of a page, separated from its rows.
	const ventaHeaderBlock = 42.0
	if pdf.GetY()+ventaHeaderBlock > bottomLimit {
		pdf.AddPage()
	}
	pdf.Ln(2)

	// Venta header row
	drawVentaHeader(pdf, v)

	// Payment table
	drawPagosTable(pdf, v)

	pdf.Ln(4)
}

// drawContinuation re-establishes which venta a table belongs to at the top of
// a continuation page.
func drawContinuation(pdf *fpdf.Fpdf, folio string) {
	pdf.SetFont("PoppinsSB", "", 9)
	pdf.SetTextColor(slateR, slateG, slateB)
	pdf.CellFormat(bodyW, 6, folio+"  (continúa)", "", 1, "L", false, 0, "")
	pdf.Ln(1)
}

// fitText truncates s with an ellipsis so it never overflows maxW under the
// current font. UTF-8 safe (trims by rune).
func fitText(pdf *fpdf.Fpdf, s string, maxW float64) string {
	const pad = 1.5 // mm safety so the text never touches the next column
	avail := maxW - pad
	if pdf.GetStringWidth(s) <= avail {
		return s
	}
	r := []rune(s)
	for len(r) > 0 {
		cand := strings.TrimRight(string(r), " ") + "…"
		if pdf.GetStringWidth(cand) <= avail {
			return cand
		}
		r = r[:len(r)-1]
	}
	return "…"
}

func drawVentaHeader(pdf *fpdf.Fpdf, v outbound.ReporteVenta) {
	// Hairline above venta
	pdf.SetDrawColor(hairR, hairG, hairB)
	pdf.SetLineWidth(0.3)
	y := pdf.GetY()
	pdf.Line(margin, y, pageW-margin, y)
	pdf.Ln(3)

	// Left side: folio + date + almacen
	startY := pdf.GetY()
	pdf.SetFont("PoppinsSB", "", 10.5)
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.CellFormat(80, 6, v.Folio, "", 0, "L", false, 0, "")

	// Right side: total + status chip
	// Determine chip text and colors
	var chipText string
	var chipR, chipG, chipB int
	if v.Liquidada {
		chipText = "LIQUIDADA"
		chipR, chipG, chipB = greenR, greenG, greenB
	} else {
		chipText = "DEBE " + formatMXN(v.Saldo)
		chipR, chipG, chipB = amberR, amberG, amberB
	}

	// Total
	pdf.SetFont("PlexMonoMed", "", 10.5)
	pdf.SetTextColor(inkR, inkG, inkB)
	totalStr := formatMXN(v.Total)
	pdf.CellFormat(bodyW-80-40, 6, totalStr, "", 0, "R", false, 0, "")

	// Chip: draw bordered rect + text
	chipW := 38.0
	chipX := pageW - margin - chipW
	chipY := startY
	pdf.SetDrawColor(chipR, chipG, chipB)
	pdf.SetLineWidth(0.4)
	pdf.Rect(chipX, chipY, chipW, 6, "D")
	pdf.SetFont("PlexMono", "", 7.5)
	pdf.SetTextColor(chipR, chipG, chipB)
	pdf.SetXY(chipX, chipY)
	pdf.CellFormat(chipW, 6, chipText, "", 1, "C", false, 0, "")

	// Second line: date + almacen
	pdf.SetXY(margin, startY+7)
	pdf.SetFont("Poppins", "", 8.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	dateAlm := formatFecha(v.Fecha) + "   " + v.Almacen
	pdf.CellFormat(bodyW, 5, dateAlm, "", 1, "L", false, 0, "")

	pdf.Ln(2)
}

// drawPagosTable renders the payment table for a venta, paginating manually so
// long tables repeat their column header (and the venta folio) on each page.
func drawPagosTable(pdf *fpdf.Fpdf, v outbound.ReporteVenta) {
	pagos := v.Pagos
	if len(pagos) == 0 {
		pdf.SetFont("Poppins", "", 8.5)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.CellFormat(bodyW, 5, "Sin pagos registrados", "", 1, "L", false, 0, "")
		return
	}

	// Column widths
	colFecha := 28.0
	colConcepto := 75.0
	colCobrador := 60.0
	colImporte := bodyW - colFecha - colConcepto - colCobrador

	rowH := 5.5

	drawColumnHeader := func() {
		pdf.SetFont("PlexMono", "", 6.5)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.SetFillColor(hairR, hairG, hairB)
		headers := []struct {
			text  string
			width float64
			align string
		}{
			{"FECHA", colFecha, "L"},
			{"CONCEPTO", colConcepto, "L"},
			{"COBRADOR", colCobrador, "L"},
			{"IMPORTE", colImporte, "R"},
		}
		for _, h := range headers {
			pdf.CellFormat(h.width, rowH, h.text, "", 0, h.align, true, 0, "")
		}
		pdf.Ln(rowH)
	}

	drawColumnHeader()

	// Payment rows — break manually, repeating folio + column header.
	// Condonación/pérdida rows are colored and totaled apart from real money.
	var ingreso, condon, perdida decimal.Decimal
	cols := pagoCols{colFecha, colConcepto, colCobrador, colImporte, rowH}
	for i, p := range pagos {
		if pdf.GetY()+rowH > bottomLimit {
			pdf.AddPage()
			drawContinuation(pdf, v.Folio)
			drawColumnHeader()
		}
		drawPagoRow(pdf, p, i%2 == 1, cols)
		switch {
		case p.EsIngreso:
			ingreso = ingreso.Add(p.Importe)
		case p.Categoria == catPerdida:
			perdida = perdida.Add(p.Importe)
		default:
			condon = condon.Add(p.Importe)
		}
	}

	lines := buildSubtotales(ingreso, condon, perdida)
	// Keep the whole subtotal block with the table.
	if pdf.GetY()+float64(len(lines))*rowH > bottomLimit {
		pdf.AddPage()
		drawContinuation(pdf, v.Folio)
		drawColumnHeader()
	}
	labelArea := colFecha + colConcepto + colCobrador
	for i, ln := range lines {
		drawSubtotalRow(pdf, labelArea, colImporte, rowH, ln.label, ln.amount, ln.r, ln.g, ln.b, i == 0)
	}
}

// pagoCols carries the payment-table column widths and row height.
type pagoCols struct {
	fecha, concepto, cobrador, importe, rowH float64
}

// drawPagoRow renders one payment row, tinting concepto + importe by category.
func drawPagoRow(pdf *fpdf.Fpdf, p outbound.ReportePago, fill bool, c pagoCols) {
	if fill {
		pdf.SetFillColor(altFillR, altFillG, altFillB)
	} else {
		pdf.SetFillColor(255, 255, 255)
	}
	cr, cg, cb := pagoColor(p)

	pdf.SetFont("PlexMono", "", 8)
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.CellFormat(c.fecha, c.rowH, formatFecha(p.Fecha), "", 0, "L", fill, 0, "")

	pdf.SetFont("Poppins", "", 8.5)
	pdf.SetTextColor(cr, cg, cb)
	pdf.CellFormat(c.concepto, c.rowH, fitText(pdf, p.Concepto, c.concepto), "", 0, "L", fill, 0, "")

	pdf.SetFont("Poppins", "", 8.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.CellFormat(c.cobrador, c.rowH, fitText(pdf, p.Cobrador, c.cobrador), "", 0, "L", fill, 0, "")

	pdf.SetFont("PlexMonoMed", "", 8.5)
	pdf.SetTextColor(cr, cg, cb)
	pdf.CellFormat(c.importe, c.rowH, formatMXN(p.Importe), "", 0, "R", fill, 0, "")

	pdf.Ln(c.rowH)
}

// drawSubtotalRow renders one subtotal line (label + amount) beneath a payment
// table. topRule draws the slate separator (used only on the first line). The
// label and amount are tinted r/g/b so condonación/pérdida totals read in color.
func drawSubtotalRow(pdf *fpdf.Fpdf, labelArea, colImporte, rowH float64, label string, total decimal.Decimal, r, g, b int, topRule bool) {
	if topRule {
		pdf.SetDrawColor(slateR, slateG, slateB)
		pdf.SetLineWidth(0.4)
		y := pdf.GetY()
		pdf.Line(margin, y, pageW-margin, y)
		pdf.SetLineWidth(0.2)
		pdf.SetDrawColor(hairR, hairG, hairB)
	}

	pdf.SetFillColor(255, 255, 255)
	pdf.SetFont("PlexMono", "", 7)
	pdf.SetTextColor(r, g, b)
	// Reserve a gap between the right-aligned label and the amount so they never collide.
	const subtotalGap = 4.0
	pdf.CellFormat(labelArea-subtotalGap, rowH, label, "", 0, "R", false, 0, "")
	pdf.CellFormat(subtotalGap, rowH, "", "", 0, "R", false, 0, "")
	pdf.SetFont("PlexMonoMed", "", 9)
	pdf.SetTextColor(r, g, b)
	pdf.CellFormat(colImporte, rowH, formatMXN(total), "", 1, "R", false, 0, "")
}

// drawFooter renders the page footer (called by fpdf on every page).
func drawFooter(pdf *fpdf.Fpdf, _ time.Time) {
	pdf.SetY(-14)
	pdf.SetDrawColor(hairR, hairG, hairB)
	pdf.SetLineWidth(0.2)
	pdf.Line(margin, pdf.GetY(), pageW-margin, pdf.GetY())
	pdf.Ln(2)
	pdf.SetFont("Poppins", "", 7)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.CellFormat(bodyW/2, 4, "Mueblería MSP", "", 0, "L", false, 0, "")
	pdf.SetFont("PlexMono", "", 7)
	pdf.CellFormat(bodyW/2, 4, "Página "+strconv.Itoa(pdf.PageNo())+" de {nb}", "", 0, "R", false, 0, "")
}

// formatMXN formats a decimal as "$1,200.00".
func formatMXN(d decimal.Decimal) string {
	s := d.StringFixed(2)
	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	// Thousands separators
	var result []byte
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	dec := ""
	if len(parts) > 1 {
		dec = "." + parts[1]
	}
	formatted := "$" + string(result) + dec
	if neg {
		return "-" + formatted
	}
	return formatted
}

// formatFecha formats a date as "12 mar 2024".
func formatFecha(t time.Time) string {
	return fmt.Sprintf("%d %s %d", t.Day(), meses[t.Month()-1], t.Year())
}

// formatFechaHora formats a datetime as "20 jun 2026, 14:30".
func formatFechaHora(t time.Time) string {
	return fmt.Sprintf("%d %s %d, %02d:%02d", t.Day(), meses[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}
