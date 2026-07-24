//nolint:misspell // Spanish domain vocabulary by project convention.
package app

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

type fakeNBPReader struct {
	nbp *outbound.NextBestProduct
	err error
}

func (f fakeNBPReader) GetNBP(context.Context, int) (*outbound.NextBestProduct, error) {
	return f.nbp, f.err
}

func newServiceForNBP(reader outbound.NextBestProductReader) *Service {
	return &Service{logger: slog.Default(), nbpReader: reader}
}

func TestResolveNBP_PoblaProductoYPlan(t *testing.T) {
	t.Parallel()
	s := newServiceForNBP(fakeNBPReader{nbp: &outbound.NextBestProduct{
		Nombre: "Comedor de 6 sillas 'Puebla'",
		Precio: decimal.RequireFromString("8000"),
	}})
	nombre, enganche, parcialidad, cadencia := s.resolveNBP(context.Background(), 42)
	assert.Equal(t, "Comedor de 6 sillas 'Puebla'", nombre)
	assert.Equal(t, "$800", enganche)                // 10% de 8000
	assert.Equal(t, "$150 a la semana", parcialidad) // (7200/52=138.4)→150
	assert.Equal(t, "semanal", cadencia)
}

func TestResolveNBP_DegradaLimpio(t *testing.T) {
	t.Parallel()
	cases := map[string]*Service{
		"sin_reader":     newServiceForNBP(nil),
		"nil_sugerencia": newServiceForNBP(fakeNBPReader{nbp: nil}),
		"error_reader":   newServiceForNBP(fakeNBPReader{err: errors.New("boom")}),
		"precio_invalido": newServiceForNBP(fakeNBPReader{nbp: &outbound.NextBestProduct{
			Nombre: "X", Precio: decimal.Zero, // impagable → se descarta TODO el NBP
		}}),
	}
	for name, s := range cases {
		s := s
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			n, e, p, c := s.resolveNBP(context.Background(), 1)
			assert.Empty(t, n)
			assert.Empty(t, e)
			assert.Empty(t, p)
			assert.Empty(t, c)
		})
	}
}

func TestFormatoPesos(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "$50", formatoPesos(decimal.RequireFromString("50")))
	assert.Equal(t, "$150", formatoPesos(decimal.RequireFromString("150")))
	assert.Equal(t, "$1,200", formatoPesos(decimal.RequireFromString("1200")))
	assert.Equal(t, "$8,000", formatoPesos(decimal.RequireFromString("8000")))
	assert.Equal(t, "$12,500", formatoPesos(decimal.RequireFromString("12500")))
}

func TestAdverbioCadencia(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a la semana", adverbioCadencia("semanal"))
	assert.Equal(t, "cada quincena", adverbioCadencia("quincenal"))
	assert.Equal(t, "al mes", adverbioCadencia("mensual"))
	assert.Equal(t, "a la semana", adverbioCadencia("desconocido")) // default seguro
}
