//nolint:misspell // Spanish domain vocabulary by project convention.
package clientespdf

import (
	"bytes"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// notaTokenRe matches the structural cues in a Microsip note: ****EMPHASIS****
// markers, DD-MM-YYYY follow-up dates, and single-* account markers. Used to
// highlight them (bold dates/markers, bullets for *) instead of dumping raw text.
var notaTokenRe = regexp.MustCompile(`(\*{2,}\s*[^*]+?\s*\*{2,})|(\b\d{1,2}-\d{1,2}-\d{4}\b)|(\*)`)

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
	margin = 15.0
	bodyW  = pageW - 2*margin
	// bottomLimit is the lowest Y content may reach before a manual page break,
	// leaving room for the two-line footer (drawn starting at SetY(-16)).
	bottomLimit = pageH - 20
)

var meses = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}

var mesesLargo = [...]string{"ENERO", "FEBRERO", "MARZO", "ABRIL", "MAYO", "JUNIO", "JULIO", "AGOSTO", "SEPTIEMBRE", "OCTUBRE", "NOVIEMBRE", "DICIEMBRE"}

// Render produces the PDF bytes for a client report.
// gen is the generation timestamp (injected for deterministic tests).
// generadoPor is the display name of the user who generated the report,
// shown in the footer of every page.
func Render(rep outbound.ReporteCliente, gen time.Time, generadoPor string) ([]byte, error) {
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

	impresas := len(rep.Ventas)
	total := rep.TotalVentas
	pdf.SetFooterFunc(func() {
		drawFooter(pdf, gen, generadoPor, impresas, total)
	})

	pdf.AddPage()

	ingreso, condon, perdida := sumarCategorias(rep.Ventas)

	drawMasthead(pdf)
	drawClienteBlock(pdf, rep.Cliente)
	drawResumenBand(pdf, rep.Resumen, ingreso, condon.Add(perdida), rep.TotalVentas)
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

// drawMasthead renders the top header: company name, report title and rule.
// The generation date was moved to the footer (Cambio 2).
func drawMasthead(pdf *fpdf.Fpdf) {
	// Row 1: company name (left only; generation date moved to footer)
	pdf.SetTextColor(slateR, slateG, slateB)
	pdf.SetFont("PoppinsSB", "", 7)
	pdf.CellFormat(bodyW, 4.5, "MUEBLERÍA MSP", "", 1, "L", false, 0, "")

	// Row 2: report title
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.SetFont("PoppinsB", "", 17)
	pdf.CellFormat(bodyW, 9, "Reporte de cliente", "", 1, "L", false, 0, "")

	// Slate horizontal rule
	pdf.SetDrawColor(slateR, slateG, slateB)
	pdf.SetLineWidth(0.6)
	y := pdf.GetY() + 0.5
	pdf.Line(margin, y, pageW-margin, y)
	pdf.SetLineWidth(0.2)
	pdf.SetDrawColor(hairR, hairG, hairB)
	pdf.Ln(3.5)
}

// drawClienteBlock renders the client identity section.
func drawClienteBlock(pdf *fpdf.Fpdf, c outbound.ReporteClienteDatos) {
	// Name
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.SetFont("PoppinsSB", "", 12)
	pdf.CellFormat(bodyW, 6.5, c.Nombre, "", 1, "L", false, 0, "")
	pdf.Ln(0.5)

	// 2-column key/value pairs (ID removed per Cambio 5)
	colW := bodyW / 2
	type kv struct{ k, v string }
	pairs := []kv{
		{"Teléfono", c.Telefono},
		{"Zona", c.Zona},
		{"Cobrador", c.Cobrador},
	}

	rowH := 4.6
	for i := 0; i < len(pairs); i += 2 {
		left := pairs[i]
		// label
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.SetFont("PlexMono", "", 6.5)
		pdf.CellFormat(colW/3, rowH, strings.ToUpper(left.k), "", 0, "L", false, 0, "")
		// value
		pdf.SetTextColor(inkR, inkG, inkB)
		pdf.SetFont("Poppins", "", 8.5)
		if i+1 < len(pairs) {
			right := pairs[i+1]
			pdf.CellFormat(colW-colW/3, rowH, left.v, "", 0, "L", false, 0, "")
			// label right
			pdf.SetTextColor(grayR, grayG, grayB)
			pdf.SetFont("PlexMono", "", 6.5)
			pdf.CellFormat(colW/3, rowH, strings.ToUpper(right.k), "", 0, "L", false, 0, "")
			// value right
			pdf.SetTextColor(inkR, inkG, inkB)
			pdf.SetFont("Poppins", "", 8.5)
			pdf.CellFormat(colW-colW/3, rowH, right.v, "", 1, "L", false, 0, "")
		} else {
			pdf.CellFormat(bodyW-colW/3, rowH, left.v, "", 1, "L", false, 0, "")
		}
	}

	// Dirección — full width. Label width matches the key/value rows above
	// (colW/3 == bodyW/6) so the value left-aligns with ID/Zona, not centered.
	labelW := colW / 3
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.CellFormat(labelW, rowH, "DIRECCIÓN", "", 0, "L", false, 0, "")
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.SetFont("Poppins", "", 8.5)
	pdf.CellFormat(bodyW-labelW, rowH, c.Direccion, "", 1, "L", false, 0, "")

	if nota := strings.TrimSpace(c.Notas); nota != "" {
		drawNota(pdf, nota)
	}

	pdf.Ln(2)
}

// drawNota renders the client's free-form note as a wrapped block with a NOTA
// label and a slate accent rule on its left. The note can be long, so it wraps
// with MultiCell; auto page break is re-enabled around it in case it overflows.
func drawNota(pdf *fpdf.Fpdf, nota string) {
	pdf.Ln(1.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.CellFormat(bodyW, 4, "NOTA", "", 1, "L", false, 0, "")

	// Left accent rule alongside the wrapped text.
	startY := pdf.GetY()
	startPage := pdf.PageNo()
	pdf.SetLeftMargin(margin + 3)
	pdf.SetX(margin + 3)
	pdf.SetAutoPageBreak(true, margin)
	writeNotaRich(pdf, nota)
	pdf.SetAutoPageBreak(false, margin)
	pdf.SetLeftMargin(margin)

	// Accent rule alongside the note (only when it stayed on one page).
	if pdf.PageNo() == startPage {
		pdf.SetDrawColor(slateR, slateG, slateB)
		pdf.SetLineWidth(0.5)
		pdf.Line(margin, startY+0.5, margin, pdf.GetY()-1)
		pdf.SetLineWidth(0.2)
		pdf.SetDrawColor(hairR, hairG, hairB)
	}
}

// writeNotaRich writes the note with inline highlighting: follow-up dates and
// ****markers**** in semibold, single-* account markers as bullets, the rest as
// body text. Uses Write so the text word-wraps while switching fonts per token.
func writeNotaRich(pdf *fpdf.Fpdf, nota string) {
	const lineH = 4.0
	normal := func() { pdf.SetFont("Poppins", "", 7.5); pdf.SetTextColor(inkR, inkG, inkB) }
	strong := func() { pdf.SetFont("PoppinsSB", "", 7.5); pdf.SetTextColor(inkR, inkG, inkB) }

	last := 0
	for _, loc := range notaTokenRe.FindAllStringSubmatchIndex(nota, -1) {
		if loc[0] > last {
			normal()
			pdf.Write(lineH, nota[last:loc[0]])
		}
		switch {
		case loc[2] >= 0: // ****emphasis**** → strip asterisks, bold
			strong()
			pdf.Write(lineH, strings.TrimSpace(strings.ReplaceAll(nota[loc[2]:loc[3]], "*", "")))
		case loc[4] >= 0: // date → bold
			strong()
			pdf.Write(lineH, nota[loc[4]:loc[5]])
		default: // single "*" account marker → bullet
			pdf.SetFont("Poppins", "", 7.5)
			pdf.SetTextColor(grayR, grayG, grayB)
			pdf.Write(lineH, "• ")
		}
		last = loc[1]
	}
	if last < len(nota) {
		normal()
		pdf.Write(lineH, nota[last:])
	}
	pdf.Ln(lineH)
}

// drawResumenBand renders the 6-metric financial summary strip. abonadoIngreso
// is collected money only (excludes condonación/pérdida); noCobrado is the sum
// of forgiven debt + write-offs — so ABONADO here means real cash, coherent with
// the per-venta subtotals and the rest of the app. totalVentas is the client's
// total number of sales (unfiltered), used for the # VENTAS metric.
func drawResumenBand(pdf *fpdf.Fpdf, r outbound.ResumenFicha, abonadoIngreso, noCobrado decimal.Decimal, totalVentas int) {
	bandH := 13.0
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
		{"# VENTAS", strconv.Itoa(totalVentas)},
	}

	for i, m := range metrics {
		x := margin + float64(i)*colW
		// Vertical separator (skip first)
		if i > 0 {
			pdf.Line(x, y+2, x, y+bandH-2)
		}
		// Label
		pdf.SetFont("PlexMono", "", 6)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.SetXY(x, y+2)
		pdf.CellFormat(colW, 4.5, m.label, "", 0, "C", false, 0, "")
		// Value
		pdf.SetFont("PlexMonoMed", "", 9.5)
		pdf.SetTextColor(inkR, inkG, inkB)
		pdf.SetXY(x, y+6.5)
		pdf.CellFormat(colW, 5.5, m.value, "", 0, "C", false, 0, "")
	}

	pdf.SetXY(margin, y+bandH+3)
}

// drawVentas renders per-venta sections with payment tables.
func drawVentas(pdf *fpdf.Fpdf, ventas []outbound.ReporteVenta) {
	for _, v := range ventas {
		drawVenta(pdf, v)
	}
}

// drawVenta renders one venta section: header, line items, credit terms (when
// applicable) and its payment table.
func drawVenta(pdf *fpdf.Fpdf, v outbound.ReporteVenta) {
	// Keep the venta header + meta with the start of its table: if too little
	// room remains, start on a fresh page so the section is never orphaned.
	metaH := 18.0 + float64(len(v.Productos))*3.9
	if len(v.Productos) > 0 {
		metaH += 3
	}
	if v.Credito != nil {
		metaH += 5
	}
	if metaH > 70 {
		metaH = 70
	}
	if pdf.GetY()+metaH+14 > bottomLimit {
		pdf.AddPage()
	}
	pdf.Ln(1.5)

	drawVentaHeader(pdf, v)
	drawArticulos(pdf, v.Productos)
	if v.Credito != nil {
		drawCredito(pdf, v.Credito)
	}
	drawPagosTable(pdf, v)

	pdf.Ln(2.5)
}

// drawArticulos renders the sale's line items: "cantidad × nombre", unit price
// and line total.
func drawArticulos(pdf *fpdf.Fpdf, productos []outbound.ReporteProducto) {
	if len(productos) == 0 {
		return
	}
	const labelW, unitW, impW = 22.0, 30.0, 30.0
	nameW := bodyW - labelW - unitW - impW
	for i, p := range productos {
		pdf.SetFont("PlexMono", "", 6.5)
		pdf.SetTextColor(grayR, grayG, grayB)
		label := ""
		if i == 0 {
			label = "ARTÍCULOS"
		}
		pdf.CellFormat(labelW, 3.9, label, "", 0, "L", false, 0, "")

		pdf.SetFont("Poppins", "", 7.5)
		pdf.SetTextColor(inkR, inkG, inkB)
		nombre := trimDecimal(p.Cantidad) + " × " + p.Nombre
		pdf.CellFormat(nameW, 3.9, fitText(pdf, nombre, nameW), "", 0, "L", false, 0, "")

		pdf.SetFont("PlexMono", "", 6.5)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.CellFormat(unitW, 3.9, "c/u "+formatMXN(p.PrecioUnitario), "", 0, "R", false, 0, "")
		pdf.SetFont("PlexMonoMed", "", 7)
		pdf.SetTextColor(inkR, inkG, inkB)
		pdf.CellFormat(impW, 3.9, formatMXN(p.Importe), "", 1, "R", false, 0, "")
	}
	pdf.Ln(0.5)
}

// drawCredito renders the credit-contract terms as a tidy label/value grid
// (two pairs per row) under a "CRÉDITO" gutter label — the same visual language
// as the client block, instead of a cramped run-on line.
func drawCredito(pdf *fpdf.Fpdf, c *outbound.ReporteCredito) {
	type kv struct{ k, v string }
	var pairs []kv
	if c.Parcialidad.IsPositive() {
		v := formatMXN(c.Parcialidad)
		if c.FormaPago != "" {
			v += " " + strings.ToLower(c.FormaPago)
		}
		pairs = append(pairs, kv{"Parcialidad", v})
	}
	if c.PlazoMeses > 0 {
		pairs = append(pairs, kv{"Plazo", fmt.Sprintf("%d meses", c.PlazoMeses)})
	}
	if c.Enganche.IsPositive() {
		pairs = append(pairs, kv{"Enganche", formatMXN(c.Enganche)})
	}
	if c.PrecioContado.IsPositive() {
		pairs = append(pairs, kv{"Precio contado", formatMXN(c.PrecioContado)})
	}
	hasVend := len(c.Vendedores) > 0
	if len(pairs) == 0 && !hasVend {
		return
	}

	const gutterW, subLabelW, rowH = 22.0, 26.0, 4.0
	colW := (bodyW - gutterW) / 2
	valueW := colW - subLabelW

	for i := 0; i < len(pairs); i += 2 {
		drawGutter(pdf, gutterW, rowH, i == 0)
		drawCampo(pdf, pairs[i].k, pairs[i].v, subLabelW, valueW)
		if i+1 < len(pairs) {
			drawCampo(pdf, pairs[i+1].k, pairs[i+1].v, subLabelW, valueW)
		}
		pdf.Ln(rowH)
	}

	if hasVend {
		drawGutter(pdf, gutterW, rowH, len(pairs) == 0)
		drawCampo(pdf, "Vendedor", strings.Join(c.Vendedores, ", "), subLabelW, bodyW-gutterW-subLabelW)
		pdf.Ln(rowH)
	}
	pdf.Ln(0.5)
}

// drawGutter draws the left-column section label (only on the first row).
func drawGutter(pdf *fpdf.Fpdf, w, rowH float64, first bool) {
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	label := ""
	if first {
		label = "CRÉDITO"
	}
	pdf.CellFormat(w, rowH, label, "", 0, "L", false, 0, "")
}

// drawCampo draws one uppercase label + value pair (no line break).
func drawCampo(pdf *fpdf.Fpdf, label, value string, labelW, valueW float64) {
	pdf.SetFont("PlexMono", "", 6)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.CellFormat(labelW, 4.0, strings.ToUpper(label), "", 0, "L", false, 0, "")
	pdf.SetFont("Poppins", "", 7.5)
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.CellFormat(valueW, 4.0, fitText(pdf, value, valueW), "", 0, "L", false, 0, "")
}

// trimDecimal formats a quantity without trailing zeros (2 not 2.00; 1.5 kept).
func trimDecimal(d decimal.Decimal) string {
	if d.Equal(d.Truncate(0)) {
		return d.Truncate(0).String()
	}
	return d.StringFixed(2)
}

// drawContinuation re-establishes which venta a table belongs to at the top of
// a continuation page.
func drawContinuation(pdf *fpdf.Fpdf, folio string) {
	pdf.SetFont("PoppinsSB", "", 8.5)
	pdf.SetTextColor(slateR, slateG, slateB)
	pdf.CellFormat(bodyW, 5, folio+"  (continúa)", "", 1, "L", false, 0, "")
	pdf.Ln(0.5)
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
	pdf.Ln(2)

	// Left side: folio + date + almacen
	startY := pdf.GetY()
	pdf.SetFont("PoppinsSB", "", 9.5)
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.CellFormat(80, 5.5, v.Folio, "", 0, "L", false, 0, "")

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
	pdf.SetFont("PlexMonoMed", "", 9.5)
	pdf.SetTextColor(inkR, inkG, inkB)
	totalStr := formatMXN(v.Total)
	pdf.CellFormat(bodyW-80-40, 5.5, totalStr, "", 0, "R", false, 0, "")

	// Chip: draw bordered rect + text
	chipW := 36.0
	chipX := pageW - margin - chipW
	chipY := startY
	pdf.SetDrawColor(chipR, chipG, chipB)
	pdf.SetLineWidth(0.4)
	pdf.Rect(chipX, chipY, chipW, 5.5, "D")
	pdf.SetFont("PlexMono", "", 7)
	pdf.SetTextColor(chipR, chipG, chipB)
	pdf.SetXY(chipX, chipY)
	pdf.CellFormat(chipW, 5.5, chipText, "", 1, "C", false, 0, "")

	// Second line: date + almacen
	pdf.SetXY(margin, startY+6)
	pdf.SetFont("Poppins", "", 8)
	pdf.SetTextColor(grayR, grayG, grayB)
	dateAlm := formatFecha(v.Fecha) + "   " + v.Almacen
	pdf.CellFormat(bodyW, 4.5, dateAlm, "", 1, "L", false, 0, "")

	pdf.Ln(1.5)
}

// drawPagosTable renders the payment table for a venta, paginating manually so
// long tables repeat their column header (and the venta folio) on each page.
// Payments are sorted ascending by date and grouped by month with a subtle
// month-header separator row.
func drawPagosTable(pdf *fpdf.Fpdf, v outbound.ReporteVenta) {
	if len(v.Pagos) == 0 {
		pdf.SetFont("Poppins", "", 7.5)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.CellFormat(bodyW, 4.5, "Sin pagos registrados", "", 1, "L", false, 0, "")
		return
	}

	// Sort a copy of the pagos slice ascending by date so month grouping is clean.
	pagos := make([]outbound.ReportePago, len(v.Pagos))
	copy(pagos, v.Pagos)
	sort.Slice(pagos, func(i, j int) bool {
		return pagos[i].Fecha.Before(pagos[j].Fecha)
	})

	colFecha := 26.0
	colConcepto := 78.0
	colCobrador := 60.0
	colImporte := bodyW - colFecha - colConcepto - colCobrador
	rowH := 4.5
	cols := pagoCols{colFecha, colConcepto, colCobrador, colImporte, rowH}

	drawColHdr := makePagosColumnHeader(pdf, cols)
	drawColHdr()

	ingreso, condon, perdida := drawPagoRows(pdf, v.Folio, pagos, cols, drawColHdr)

	lines := buildSubtotales(ingreso, condon, perdida)
	if pdf.GetY()+float64(len(lines))*rowH > bottomLimit {
		pdf.AddPage()
		drawContinuation(pdf, v.Folio)
		drawColHdr()
	}
	labelArea := colFecha + colConcepto + colCobrador
	for i, ln := range lines {
		drawSubtotalRow(pdf, labelArea, colImporte, rowH, ln.label, ln.amount, ln.r, ln.g, ln.b, i == 0)
	}
}

// makePagosColumnHeader returns a closure that draws the payment table column
// header using the given column widths.
func makePagosColumnHeader(pdf *fpdf.Fpdf, cols pagoCols) func() {
	return func() {
		pdf.SetFont("PlexMono", "", 6)
		pdf.SetTextColor(grayR, grayG, grayB)
		pdf.SetFillColor(hairR, hairG, hairB)
		headers := []struct {
			text  string
			width float64
			align string
		}{
			{"FECHA", cols.fecha, "L"},
			{"CONCEPTO", cols.concepto, "L"},
			{"COBRADOR", cols.cobrador, "L"},
			{"IMPORTE", cols.importe, "R"},
		}
		for _, h := range headers {
			pdf.CellFormat(h.width, cols.rowH, h.text, "", 0, h.align, true, 0, "")
		}
		pdf.Ln(cols.rowH)
	}
}

// drawPagoRows iterates sorted pagos, inserting month-group headers on month
// changes and paginating manually. Returns per-category payment totals.
func drawPagoRows(
	pdf *fpdf.Fpdf,
	folio string,
	pagos []outbound.ReportePago,
	cols pagoCols,
	drawColHdr func(),
) (decimal.Decimal, decimal.Decimal, decimal.Decimal) {
	var ingreso, condon, perdida decimal.Decimal
	var lastYM [2]int // [year, month] of the previous row; zero = none yet
	for i, p := range pagos {
		curY, curM := p.Fecha.Year(), p.Fecha.Month()
		if lastYM[0] != curY || lastYM[1] != int(curM) {
			// Anti-orphan: ensure month header + at least one payment fit together.
			if pdf.GetY()+2*cols.rowH > bottomLimit {
				pdf.AddPage()
				drawContinuation(pdf, folio)
				drawColHdr()
			}
			drawPagosMonthHeader(pdf, p.Fecha, cols.rowH)
			lastYM = [2]int{curY, int(curM)}
		}
		if pdf.GetY()+cols.rowH > bottomLimit {
			pdf.AddPage()
			drawContinuation(pdf, folio)
			drawColHdr()
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
	return ingreso, condon, perdida
}

// drawPagosMonthHeader draws a tinted month separator row (e.g. "ABRIL 2024").
func drawPagosMonthHeader(pdf *fpdf.Fpdf, t time.Time, rowH float64) {
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.SetTextColor(slateR, slateG, slateB)
	pdf.SetFillColor(hairR, hairG, hairB)
	pdf.CellFormat(bodyW, rowH, formatMesAnio(t), "", 1, "L", true, 0, "")
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

	pdf.SetFont("PlexMono", "", 7)
	pdf.SetTextColor(inkR, inkG, inkB)
	pdf.CellFormat(c.fecha, c.rowH, formatFecha(p.Fecha), "", 0, "L", fill, 0, "")

	pdf.SetFont("Poppins", "", 7.5)
	pdf.SetTextColor(cr, cg, cb)
	pdf.CellFormat(c.concepto, c.rowH, fitText(pdf, p.Concepto, c.concepto), "", 0, "L", fill, 0, "")

	pdf.SetFont("Poppins", "", 7.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.CellFormat(c.cobrador, c.rowH, fitText(pdf, p.Cobrador, c.cobrador), "", 0, "L", fill, 0, "")

	pdf.SetFont("PlexMonoMed", "", 7.5)
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
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.SetTextColor(r, g, b)
	// Reserve a gap between the right-aligned label and the amount so they never collide.
	const subtotalGap = 4.0
	pdf.CellFormat(labelArea-subtotalGap, rowH, label, "", 0, "R", false, 0, "")
	pdf.CellFormat(subtotalGap, rowH, "", "", 0, "R", false, 0, "")
	pdf.SetFont("PlexMonoMed", "", 8)
	pdf.SetTextColor(r, g, b)
	pdf.CellFormat(colImporte, rowH, formatMXN(total), "", 1, "R", false, 0, "")
}

// drawFooter renders the two-line page footer (called by fpdf on every page).
// Line 1: generation info (generadoPor · date · X de Y ventas).
// Line 2: page number right-aligned.
func drawFooter(pdf *fpdf.Fpdf, gen time.Time, generadoPor string, impresas, total int) {
	pdf.SetY(-16)
	pdf.SetDrawColor(hairR, hairG, hairB)
	pdf.SetLineWidth(0.2)
	pdf.Line(margin, pdf.GetY(), pageW-margin, pdf.GetY())
	pdf.Ln(1.5)

	// Line 1: "Generado por <Nombre> · <fecha> · X de Y ventas"
	infoText := fmt.Sprintf("Generado por %s · %s · %d de %d ventas",
		generadoPor, formatFechaHora(gen), impresas, total)
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.CellFormat(bodyW, 4, infoText, "", 1, "L", false, 0, "")

	// Line 2: page number right-aligned
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.SetTextColor(grayR, grayG, grayB)
	pdf.CellFormat(bodyW, 4, "Página "+strconv.Itoa(pdf.PageNo())+" de {nb}", "", 0, "R", false, 0, "")
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

// formatMesAnio formats a month/year as "ABRIL 2024" for month-group headers.
func formatMesAnio(t time.Time) string {
	return fmt.Sprintf("%s %d", mesesLargo[t.Month()-1], t.Year())
}

// formatFechaHora formats a datetime as "20 jun 2026, 14:30".
func formatFechaHora(t time.Time) string {
	return fmt.Sprintf("%d %s %d, %02d:%02d", t.Day(), meses[t.Month()-1], t.Year(), t.Hour(), t.Minute())
}
