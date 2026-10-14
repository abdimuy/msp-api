package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestEtiquetaFormaCobro(t *testing.T) {
	t.Parallel()
	casos := map[int]string{
		157:    "efectivo",
		52569:  "transferencia",
		158:    "cheque",
		137026: "condonacion",
		0:      "otra",
		67:     "otra", // "Efectivo" del catálogo FORMAS_COBRO, que miente (E-COB-017)
	}
	for id, esperado := range casos {
		assert.Equal(t, esperado, domain.EtiquetaFormaCobro(id), "forma %d", id)
	}
}

func TestEsFormaDeDinero(t *testing.T) {
	t.Parallel()
	for _, id := range []int{157, 158, 52569} {
		assert.True(t, domain.EsFormaDeDinero(id), "forma %d es dinero", id)
	}
	for _, id := range []int{137026, 0, 67, 68, 71, 999} {
		assert.False(t, domain.EsFormaDeDinero(id), "forma %d no es dinero", id)
	}
}

func TestCatalogoFormasCobro_CodigosDelFiltro(t *testing.T) {
	t.Parallel()
	got := domain.CatalogoFormasCobro()
	codigos := make([]string, 0, len(got))
	for _, f := range got {
		codigos = append(codigos, f.Codigo)
		assert.NotEmpty(t, f.Etiqueta)
	}
	assert.Equal(t, []string{"efectivo", "transferencia", "cheque", "condonacion", "otra"}, codigos)
}
