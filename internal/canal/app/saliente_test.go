package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	canaldomain "github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/platform/whatsapp"
)

// newSalienteService builds a Service wired against sender for
// EnviarSaliente/ContarPendientes tests — the mailbox/forwarder fakes are
// never exercised here, so bare (unscripted) ones are enough.
func newSalienteService(sender *senderFake) *canalapp.Service {
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	return canalapp.NewService(repo, fwd, sender, clock, nil, canalapp.ReenvioConfig{}, nil)
}

// ── validation ───────────────────────────────────────────────────────────

func TestEnviarSaliente_MissingDestinatario_ErrorSinTocarElSender(t *testing.T) {
	t.Parallel()
	sender := newSenderFake()
	svc := newSalienteService(sender)

	_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Tipo:  canalapp.TipoSalienteTexto,
		Texto: "hola",
	})

	require.ErrorIs(t, err, canaldomain.ErrSalienteDestinatarioRequerido)
	assert.Equal(t, 0, sender.textCallCount(), "validation must fail before the sender is ever called")
}

func TestEnviarSaliente_TipoInvalido_Error(t *testing.T) {
	t.Parallel()
	svc := newSalienteService(newSenderFake())

	_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         "fax",
	})

	require.ErrorIs(t, err, canaldomain.ErrSalienteTipoInvalido)
}

func TestEnviarSaliente_TipoTextoSinTexto_Error(t *testing.T) {
	t.Parallel()
	svc := newSalienteService(newSenderFake())

	_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         canalapp.TipoSalienteTexto,
	})

	require.ErrorIs(t, err, canaldomain.ErrSalienteTextoRequerido)
}

func TestEnviarSaliente_TipoPlantillaSinPlantilla_Error(t *testing.T) {
	t.Parallel()
	svc := newSalienteService(newSenderFake())

	_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         canalapp.TipoSalientePlantilla,
	})

	require.ErrorIs(t, err, canaldomain.ErrSalientePlantillaRequerida)
}

// ── success paths ────────────────────────────────────────────────────────

func TestEnviarSaliente_Texto_LlamaAlSenderYDevuelveWamid(t *testing.T) {
	t.Parallel()
	sender := newSenderFake()
	sender.scriptText(senderOutcome{wamid: "wamid.texto-1"})
	svc := newSalienteService(sender)

	wamid, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         canalapp.TipoSalienteTexto,
		Texto:        "hola cliente",
	})

	require.NoError(t, err)
	assert.Equal(t, "wamid.texto-1", wamid)
	require.Equal(t, 1, sender.textCallCount())
	call := sender.lastTextCall()
	assert.Equal(t, "5215500000000", call.to)
	assert.Equal(t, "hola cliente", call.body)
}

func TestEnviarSaliente_Plantilla_LlamaAlSenderConLosCamposCorrectos(t *testing.T) {
	t.Parallel()
	sender := newSenderFake()
	sender.scriptTemplate(senderOutcome{wamid: "wamid.tmpl-1"})
	svc := newSalienteService(sender)

	wamid, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         canalapp.TipoSalientePlantilla,
		Plantilla: &canalapp.SalientePlantilla{
			Nombre:     "recordatorio_pago",
			Idioma:     "es_MX",
			Parametros: []string{"Juan", "500"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "wamid.tmpl-1", wamid)
	require.Equal(t, 1, sender.templateCallCount())
	call := sender.lastTemplateCall()
	assert.Equal(t, "5215500000000", call.to)
	assert.Equal(t, "recordatorio_pago", call.body)
}

// ── error classification ────────────────────────────────────────────────

func TestEnviarSaliente_ClasificaErroresDelSender(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		waErr   error
		wantErr error
	}{
		{"ventana_cerrada", whatsapp.ErrWindowClosed, canaldomain.ErrSalienteVentanaCerrada},
		{"numero_invalido", whatsapp.ErrInvalidNumber, canaldomain.ErrSalienteNumeroInvalido},
		{"limite_tasa", whatsapp.ErrRateLimited, canaldomain.ErrSalienteLimiteTasa},
		{"deshabilitado", whatsapp.ErrWhatsAppDisabled, canaldomain.ErrSalienteCanalDeshabilitado},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sender := newSenderFake()
			sender.scriptText(senderOutcome{err: tc.waErr})
			svc := newSalienteService(sender)

			_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
				Destinatario: "5215500000000",
				Tipo:         canalapp.TipoSalienteTexto,
				Texto:        "hola",
			})

			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func TestEnviarSaliente_ErrorTransitorioGenerico_SeClasificaComoEnvioFallido(t *testing.T) {
	t.Parallel()
	// A generic transport failure (e.g. WhatsApp 5xx), wrapped the same way
	// internal/platform/whatsapp's own realClient wraps one — not one of
	// the four specifically-recognized sentinels.
	transitorio := &whatsapp.TransientError{Cause: errors.New("whatsapp: status 503 servidor no disponible")}
	sender := newSenderFake()
	sender.scriptText(senderOutcome{err: transitorio})
	svc := newSalienteService(sender)

	_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         canalapp.TipoSalienteTexto,
		Texto:        "hola",
	})

	require.ErrorIs(t, err, canaldomain.ErrSalienteEnvioFallido)
}

func TestEnviarSaliente_ErrorNoClasificado_SeDevuelveSinEnvolver(t *testing.T) {
	t.Parallel()
	// A permanent, unclassified error (not transient, not one of the four
	// specific sentinels) must come back exactly as the sender returned it
	// — canalhttp's mapAppError already falls through to a 500 for any
	// non-apperror error, so wrapping it here would change nothing except
	// hide the original cause.
	original := errors.New("whatsapp: status 400 solicitud inválida")
	sender := newSenderFake()
	sender.scriptText(senderOutcome{err: original})
	svc := newSalienteService(sender)

	_, err := svc.EnviarSaliente(context.Background(), canalapp.EnviarSalienteParams{
		Destinatario: "5215500000000",
		Tipo:         canalapp.TipoSalienteTexto,
		Texto:        "hola",
	})

	require.ErrorIs(t, err, original)
}

// ── ContarPendientes ─────────────────────────────────────────────────────

func TestContarPendientes_DelegaAlRepo(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := canalapp.NewService(repo, fwd, newSenderFake(), clock, nil, canalapp.ReenvioConfig{}, nil)

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-pendiente", clock.Now()))
	require.NoError(t, err)

	got, err := svc.ContarPendientes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	want, err := repo.ContarPendientes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestContarPendientes_PropagaErrorDelRepo(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	repo.fallarCon("ContarPendientes", errNoEncontrado)
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := canalapp.NewService(repo, fwd, newSenderFake(), clock, nil, canalapp.ReenvioConfig{}, nil)

	_, err := svc.ContarPendientes(context.Background())
	require.ErrorIs(t, err, errNoEncontrado)
}
