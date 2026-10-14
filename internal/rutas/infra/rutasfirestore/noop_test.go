package rutasfirestore_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/rutas/infra/rutasfirestore"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

// El noop se usa cuando Firestore no está configurado o no arrancó
// (cmd/api/rutas_wiring.go). Debe declararse no configurado para que los
// consumidores que no pueden fingir una flota vacía (desempeño) respondan 503.
func TestNoopCalendarioClient_NoEstaConfigurado(t *testing.T) {
	t.Parallel()
	var c outbound.CalendarioCobradorClient = rutasfirestore.NoopCalendarioClient{}
	conf, ok := c.(outbound.CalendarioConfigurable)
	if assert.True(t, ok, "el noop implementa CalendarioConfigurable") {
		assert.False(t, conf.Configurado())
	}
}
