package render

import (
	"context"
	"testing"
	"time"

	"github.com/go-pdf/fpdf"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

func TestThousands(t *testing.T) {
	t.Parallel()
	cases := []struct{ in, want string }{
		{"0.00", "0.00"},
		{"1", "1"},
		{"12", "12"},
		{"123", "123"},
		{"1234.50", "1,234.50"},
		{"1234567.00", "1,234,567.00"},
		{"-9876543.10", "-9,876,543.10"},
		{"1000000", "1,000,000"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.want, thousands(tc.in), "thousands(%q)", tc.in)
	}
}

func TestMoney(t *testing.T) {
	t.Parallel()
	require.Equal(t, "$1,234.50", money(decimal.NewFromFloat(1234.5)))
	require.Equal(t, "$0.00", money(decimal.NewFromFloat(0)))
	require.Equal(t, "$999,999.99", money(decimal.NewFromFloat(999999.99)))
}

// TestFormatFechaCorta locks the short date format and, crucially, that the
// printed day is the business (CDMX) calendar day, not the raw UTC day: a
// capture at 22:30 CDMX is the next day in UTC, and the doc must still read
// the previous (correct) date. All times are built in time.UTC.
func TestFormatFechaCorta(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		t    time.Time
		want string
	}{
		{"same day in CDMX", time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), "01/09/2026"},
		{"late CDMX evening is next UTC day", time.Date(2026, 9, 2, 4, 30, 0, 0, time.UTC), "01/09/2026"},
		{"early CDMX morning same UTC day", time.Date(2026, 9, 3, 6, 0, 0, 0, time.UTC), "03/09/2026"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, formatFechaCorta(tc.t))
		})
	}
}

// TestOutput_PropagatesPDFError verifies the brief's "el error de fpdf se
// propaga": a pdf whose internal error state is set yields an error and no
// bytes, never a half document.
func TestOutput_PropagatesPDFError(t *testing.T) {
	t.Parallel()

	// An unknown page size makes fpdf.New set its internal error state.
	pdf := fpdf.New("P", "mm", "no-such-size", "")
	require.Error(t, pdf.Error(), "precondition: fpdf error state must be set")

	got, err := output(pdf)
	require.Error(t, err)
	require.Nil(t, got)
}

// TestRenderer_ImplementsPort locks the port bound without touching it.
func TestRenderer_ImplementsPort(t *testing.T) {
	t.Parallel()
	var _ interface {
		Venta(context.Context, domain.ComprobanteVenta) ([]byte, error)
		Pago(context.Context, domain.ComprobantePago) ([]byte, error)
	} = NewPDFRenderer()
	assert.NotNil(t, NewPDFRenderer())
}

func TestNewPDF_SetCreationDate(t *testing.T) {
	t.Parallel()
	ts := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	pdf, err := newPDF(ts)
	require.NoError(t, err)
	require.NoError(t, pdf.Error())
}
