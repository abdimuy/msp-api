//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionsender_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionsender"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

func TestFakeSender_Enviar_Success(t *testing.T) {
	t.Parallel()
	s := reactivacionsender.NewFakeSender(nil)
	err := s.Enviar(context.Background(), outbound.Destino{ClienteID: 101, Telefono: "238 111 2222"}, "cuerpo de prueba")
	require.NoError(t, err)
}

func TestFakeSender_Kind(t *testing.T) {
	t.Parallel()
	s := reactivacionsender.NewFakeSender(nil)
	assert.Equal(t, domain.SenderSimulado, s.Kind())
}

func TestWhatsmeowSender_Enviar_NoConfigurado(t *testing.T) {
	t.Parallel()
	s := reactivacionsender.NewWhatsmeowSender()
	err := s.Enviar(context.Background(), outbound.Destino{ClienteID: 101, Telefono: "238 111 2222"}, "cuerpo")
	require.Error(t, err)

	var ae *apperror.Error
	require.ErrorAs(t, err, &ae)
	assert.Equal(t, "whatsmeow_no_configurado", ae.Code)
}

func TestWhatsmeowSender_Kind(t *testing.T) {
	t.Parallel()
	s := reactivacionsender.NewWhatsmeowSender()
	assert.Equal(t, domain.SenderReal, s.Kind())
}
