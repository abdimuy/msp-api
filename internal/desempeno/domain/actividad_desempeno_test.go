package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestEtiquetaFormaDeActividad(t *testing.T) {
	t.Parallel()
	pago := func(tipo domain.TipoActividad, forma int) domain.Actividad {
		return domain.Actividad{Tipo: tipo, Pago: &domain.DatosPago{FormaCobroID: forma}}
	}
	assert.Equal(t, "condonacion", domain.EtiquetaFormaDeActividad(pago(domain.TipoCondonacion, 157)),
		"condonación por concepto 27969 con forma 157")
	assert.Equal(t, "efectivo", domain.EtiquetaFormaDeActividad(pago(domain.TipoPago, 157)))
	assert.Equal(t, "otra", domain.EtiquetaFormaDeActividad(pago(domain.TipoPago, 67)))
	assert.Empty(t, domain.EtiquetaFormaDeActividad(domain.Actividad{Tipo: domain.TipoVisita}))
}
