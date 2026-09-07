//nolint:misspell // Spanish labels are user-facing (descripcion, informativo, etc.).
package render

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
	"github.com/abdimuy/msp-api/internal/comprobantes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

//go:embed fonts/*.ttf
var fontsFS embed.FS

const (
	pageW  = 215.9 // Letter width in mm
	pageH  = 279.4 // Letter height in mm
	margin = 15.0
	bodyW  = pageW - 2*margin
)

// PDFRenderer renders comprobante models to PDF bytes.
type PDFRenderer struct{}

// NewPDFRenderer constructs the single renderer shared by the venta and pago
// methods.
func NewPDFRenderer() *PDFRenderer { return &PDFRenderer{} }

// PDFRenderer satisfies the outbound.Renderer port.
var _ outbound.Renderer = (*PDFRenderer)(nil)

// Venta renders the sale receipt. All content comes from c's getters — the
// renderer reads nothing from anywhere else.
func (r *PDFRenderer) Venta(_ context.Context, c domain.ComprobanteVenta) ([]byte, error) {
	pdf, err := newPDF(c.Fecha())
	if err != nil {
		return nil, err
	}

	masthead(pdf, "Comprobante de venta", "Folio "+c.Folio())
	etiquetaLine(pdf, "Fecha", formatFechaCorta(c.Fecha()))
	clienteBlock(pdf, c.ClienteNombre(), c.ClienteDomicilio())
	articulosTable(pdf, c.Articulos())
	totalesVenta(pdf, c.Total(), c.Enganche(), c.Saldo())
	planPago(pdf, c.PlanPago())
	etiquetaLine(pdf, "Vendedor", c.Vendedor())
	etiquetaObligatoria(pdf)

	return output(pdf)
}

// Pago renders the payment receipt. The remaining balance after this payment
// is the datum the document exists for, so it goes where it can be seen.
func (r *PDFRenderer) Pago(_ context.Context, c domain.ComprobantePago) ([]byte, error) {
	pdf, err := newPDF(c.Fecha())
	if err != nil {
		return nil, err
	}

	masthead(pdf, "Comprobante de pago", "Folio "+c.Folio())
	etiquetaLine(pdf, "Fecha", formatFechaCorta(c.Fecha()))
	clienteBlock(pdf, c.ClienteNombre(), "")
	detallePago(pdf, c.Monto(), c.FormaCobro(), c.VentaFolio())
	saldoRestantePago(pdf, c.SaldoRestante())
	etiquetaLine(pdf, "Cobró", c.Cobrador())
	etiquetaObligatoria(pdf)

	return output(pdf)
}

// newPDF creates a Letter portrait document with the fonts loaded and the
// creation date fixed to the comprobante's own timestamp.
func newPDF(creation time.Time) (*fpdf.Fpdf, error) {
	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(false, margin)
	if err := loadFonts(pdf); err != nil {
		return nil, err
	}
	pdf.SetCreationDate(creation)
	// ModDate otherwise defaults to time.Now() at Output, which would make two
	// renders of the same model differ. SetCatalogSort orders internal
	// resources consistently. Together these are what fpdf's own docs require
	// for two PDFs built the same way to be byte-identical.
	pdf.SetCatalogSort(true)
	pdf.SetModificationDate(creation)
	pdf.AddPage()
	return pdf, nil
}

// loadFonts registers the embedded TTF families. Helvetica and friends are
// Latin-1 and would render accented surnames wrong, so every variant is a
// UTF-8 TTF registered by family name.
func loadFonts(pdf *fpdf.Fpdf) error {
	fonts := []struct {
		family string
		file   string
	}{
		{"Poppins", "fonts/Poppins-Regular.ttf"},
		{"PoppinsSB", "fonts/Poppins-SemiBold.ttf"},
		{"PlexMono", "fonts/IBMPlexMono-Regular.ttf"},
	}
	for _, f := range fonts {
		b, err := fontsFS.ReadFile(f.file)
		if err != nil {
			return fmt.Errorf("leer fuente %s: %w", f.file, err)
		}
		// AddUTF8FontFromBytes is known to mutate its input slice when
		// subsetting; pass a copy so each render is independent and
		// deterministic (gofpdf issue #316).
		fontBytes := append([]byte(nil), b...)
		pdf.AddUTF8FontFromBytes(f.family, "", fontBytes)
		if pdf.Error() != nil {
			return fmt.Errorf("registrar fuente %s: %w", f.family, pdf.Error())
		}
	}
	return nil
}

// output serializes the finished document. The fpdf error state is checked
// here and only here — on any failure the caller gets an error and no half
// bytes.
func output(pdf *fpdf.Fpdf) ([]byte, error) {
	if err := pdf.Error(); err != nil {
		return nil, fmt.Errorf("generar pdf: %w", err)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("generar pdf: %w", err)
	}
	return buf.Bytes(), nil
}

// money formats a decimal amount with thousands separator and two decimals,
// e.g. 1234.5 -> "$1,234.50". decimal is never printed raw: "1234.5" on a
// receipt reads wrong.
func money(v decimal.Decimal) string {
	return "$" + thousands(v.Round(2).StringFixed(2))
}

// thousands inserts the thousands separator into an integer-with-decimals
// string. It is intentionally decimal-agnostic: it splits on the decimal point
// and only decorates the integer part, leaving any trailing decimals intact.
func thousands(s string) string {
	frac := ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		frac = s[i:]
		s = s[:i]
	}
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(r))
	}
	p := string(out) + frac
	if neg {
		p = "-" + p
	}
	return p
}

// formatFechaCorta formats a UTC timestamp as the business-day short date
// "DD/MM/YYYY", first resolving the instant to the business zone. Reading the
// raw UTC calendar day would print tomorrow's date for a capture made late in
// the CDMX evening, which is exactly the day-shift bug DATETIME_HANDLING.md
// exists to prevent.
func formatFechaCorta(t time.Time) string {
	return t.In(firebird.BusinessTZ()).Format("02/01/2006")
}

// label writes a small uppercase header line in the technical mono face.
func label(pdf *fpdf.Fpdf, text string) {
	pdf.SetFont("PlexMono", "", 7)
	pdf.SetTextColor(120, 120, 120)
	pdf.CellFormat(bodyW, 4, strings.ToUpper(text), "", 1, "L", false, 0, "")
}

// etiquetaLine draws a single labelled value: full-width label, right-aligned
// value on the same row.
func etiquetaLine(pdf *fpdf.Fpdf, name, v string) {
	if v == "" {
		return
	}
	pdf.Ln(1)
	label(pdf, name)
	pdf.SetFont("Poppins", "", 9.5)
	pdf.SetTextColor(31, 41, 55)
	pdf.CellFormat(bodyW, 5.5, v, "", 1, "L", false, 0, "")
}

// sectionTitle draws a bold section header with a hairline under it.
func sectionTitle(pdf *fpdf.Fpdf, text string) {
	pdf.Ln(2)
	pdf.SetFont("PoppinsSB", "", 10)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(bodyW, 6, text, "", 1, "L", false, 0, "")
	pdf.SetDrawColor(209, 213, 219)
	pdf.SetLineWidth(0.3)
	y := pdf.GetY() + 0.5
	pdf.Line(margin, y, pageW-margin, y)
	pdf.Ln(2.5)
}

// masthead draws the title and folio row at the top.
func masthead(pdf *fpdf.Fpdf, title, folio string) {
	pdf.SetFont("PoppinsSB", "", 16)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(bodyW, 8, title, "", 0, "L", false, 0, "")
	pdf.SetFont("PlexMono", "", 11)
	pdf.SetTextColor(79, 70, 229)
	pdf.CellFormat(0, 8, folio, "", 1, "R", false, 0, "")
	pdf.Ln(3)
}

// clienteBlock draws the client name and optional address.
func clienteBlock(pdf *fpdf.Fpdf, nombre, domicilio string) {
	label(pdf, "Cliente")
	pdf.SetFont("Poppins", "", 9.5)
	pdf.SetTextColor(31, 41, 55)
	pdf.CellFormat(bodyW, 5.5, nombre, "", 1, "L", false, 0, "")
	if domicilio != "" {
		pdf.SetFont("Poppins", "", 8.5)
		pdf.SetTextColor(107, 114, 128)
		pdf.MultiCell(bodyW, 4.5, domicilio, "", "L", false)
	}
	pdf.Ln(2)
}

// articulosTable lists each article with quantity, unit price and line total.
func articulosTable(pdf *fpdf.Fpdf, articulos []domain.ArticuloComprobante) {
	sectionTitle(pdf, "Artículos")

	descW := bodyW * 0.52
	qtyW := bodyW * 0.12
	pxW := bodyW * 0.18
	impW := bodyW * 0.18

	pdf.SetFillColor(243, 244, 246)
	pdf.SetFont("PlexMono", "", 6.5)
	pdf.SetTextColor(107, 114, 128)
	pdf.CellFormat(descW, 5, "ARTÍCULO", "B", 0, "L", true, 0, "")
	pdf.CellFormat(qtyW, 5, "CANT.", "B", 0, "R", true, 0, "")
	pdf.CellFormat(pxW, 5, "PRECIO", "B", 0, "R", true, 0, "")
	pdf.CellFormat(impW, 5, "IMPORTE", "B", 1, "R", true, 0, "")

	pdf.SetFont("Poppins", "", 9)
	pdf.SetTextColor(31, 41, 55)
	for _, a := range articulos {
		pdf.SetX(margin)
		pdf.CellFormat(descW, 6, a.Descripcion(), "", 0, "L", false, 0, "")
		pdf.SetFont("PlexMono", "", 8.5)
		pdf.CellFormat(qtyW, 6, a.Cantidad().String(), "", 0, "R", false, 0, "")
		pdf.CellFormat(pxW, 6, money(a.PrecioUnitario()), "", 0, "R", false, 0, "")
		pdf.CellFormat(impW, 6, money(a.Importe()), "", 1, "R", false, 0, "")
		pdf.SetFont("Poppins", "", 9)
	}
	pdf.Ln(1)
}

// totalLine draws a label with a right-aligned figure.
func totalLine(pdf *fpdf.Fpdf, text, v string, big bool) {
	size := 10.0
	if big {
		size = 12
	}
	pdf.SetFont("PoppinsSB", "", size)
	pdf.SetTextColor(17, 24, 39)
	pdf.CellFormat(bodyW*0.5, 6.5, text, "", 0, "L", false, 0, "")
	pdf.CellFormat(bodyW*0.5, 6.5, v, "", 1, "R", false, 0, "")
}

func totalesVenta(pdf *fpdf.Fpdf, total, enganche, saldo decimal.Decimal) {
	sectionTitle(pdf, "Resumen")
	totalLine(pdf, "Total", money(total), true)
	totalLine(pdf, "Enganche", money(enganche), false)
	totalLine(pdf, "Saldo", money(saldo), false)
	pdf.Ln(1)
}

func planPago(pdf *fpdf.Fpdf, plan string) {
	if plan == "" {
		return
	}
	sectionTitle(pdf, "Plan de pago")
	pdf.SetFont("Poppins", "", 9.5)
	pdf.SetTextColor(31, 41, 55)
	pdf.MultiCell(bodyW, 5.5, plan, "", "L", false)
	pdf.Ln(2)
}

func detallePago(pdf *fpdf.Fpdf, monto decimal.Decimal, forma, ventaFolio string) {
	sectionTitle(pdf, "Detalle del pago")
	totalLine(pdf, "Monto", money(monto), true)
	totalLine(pdf, "Forma de cobro", forma, false)
	totalLine(pdf, "Aplicado a la venta", ventaFolio, false)
	pdf.Ln(1)
}

// saldoRestantePago highlights what the payment document exists for.
func saldoRestantePago(pdf *fpdf.Fpdf, saldo decimal.Decimal) {
	sectionTitle(pdf, "Saldo restante")
	pdf.SetFont("PoppinsSB", "", 14)
	pdf.SetTextColor(190, 18, 60)
	pdf.CellFormat(0, 8, money(saldo), "", 1, "L", false, 0, "")
	pdf.Ln(2)
}

// etiquetaObligatoria draws the §6.3 disclaimer inside a bordered box so it
// reads as part of the document, not decoration.
func etiquetaObligatoria(pdf *fpdf.Fpdf) {
	pdf.Ln(4)
	pdf.SetDrawColor(217, 119, 6)
	pdf.SetFillColor(255, 251, 235)
	pdf.SetTextColor(146, 64, 14)
	pdf.SetFont("PlexMono", "", 7.5)
	y := pdf.GetY()
	pdf.Rect(margin, y, bodyW, 10, "DF")
	pdf.SetXY(margin+3, y+1.5)
	pdf.CellFormat(bodyW-6, 7, "Comprobante informativo, no es un CFDI", "", 1, "L", false, 0, "")
	pdf.SetY(y + 10)
}
