package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/domain"
)

var errForwarderPermanente = errors.New("400: número inválido")

// ── constructor helper ──────────────────────────────────────────────────

func newServiceFor(repo *buzonRepoFake, fwd *forwarderFake, clock *fixedClock, cfg canalapp.ReenvioConfig) *canalapp.Service {
	return canalapp.NewService(repo, fwd, clock, nil, cfg, nil)
}

// ── RecibirMensaje ───────────────────────────────────────────────────────

func TestRecibirMensaje_MensajeNuevo(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	m, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-1", clock.Now()))
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.Equal(t, domain.EstadoReenvioPendiente, m.Estado())
	assert.Equal(t, 1, repo.countByWamid("wamid-1"))
	assert.Len(t, m.PendingEvents(), 1, "a freshly inserted message keeps its recibido event")
	assert.Equal(t, domain.EventTypeMensajeEntranteRecibido, m.PendingEvents()[0].EventType())
}

func TestRecibirMensaje_MensajeDuplicadoPorWamid_NoOpYSinEvento(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	ctx := context.Background()
	first, err := svc.RecibirMensaje(ctx, mensajeParams("wamid-dup", clock.Now()))
	require.NoError(t, err)

	// Meta redelivers the same webhook — same wamid, arriving "later".
	clock.advance(time.Minute)
	second, err := svc.RecibirMensaje(ctx, mensajeParams("wamid-dup", clock.Now()))
	require.NoError(t, err, "a redelivered webhook must not surface as an error")

	assert.NotEqual(t, first.ID(), second.ID(), "each call builds its own in-memory entity")
	assert.Empty(t, second.PendingEvents(), "a duplicate delivery emits no domain event")
	assert.Equal(t, 1, repo.countByWamid("wamid-dup"), "the mailbox holds exactly one row for this wamid")
	assert.Equal(t, 2, repo.guardarLlamadas, "Guardar is still called each time — idempotency lives in its return value")
}

func TestRecibirMensaje_ValidacionInvalida_NoLlamaAlRepo(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("", clock.Now()))
	require.Error(t, err)
	assert.Equal(t, 0, repo.guardarLlamadas, "an invalid message must never reach the repo")
}

func TestRecibirMensaje_GuardarFalla_PropagaError(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	errRepo := errors.New("sqlite: disk full")
	repo.fallarCon("Guardar", errRepo)
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-x", clock.Now()))
	require.Error(t, err)
	assert.ErrorIs(t, err, errRepo)
}

// ── DrenarCola: forward outcomes ─────────────────────────────────────────

// smallRetry keeps the backoff numbers in the millisecond range so retry
// tests run fast without becoming a no-op: real time still passes between
// attempts.
func smallRetry(maxAttempts int) canalapp.ReenvioConfig {
	cfg := canalapp.ReenvioConfig{}
	cfg.Retry.MaxAttempts = maxAttempts
	cfg.Retry.Backoff = time.Millisecond
	cfg.Retry.MaxBackoff = 5 * time.Millisecond
	cfg.Retry.Jitter = 0
	cfg.Circuit.FailureThreshold = 100 // effectively disabled for these tests
	cfg.Circuit.SuccessThreshold = 1
	cfg.Circuit.Delay = time.Millisecond
	return cfg
}

func recibirUno(t *testing.T, svc *canalapp.Service, clock *fixedClock, wamid string) *domain.MensajeEntrante {
	t.Helper()
	m, err := svc.RecibirMensaje(context.Background(), mensajeParams(wamid, clock.Now()))
	require.NoError(t, err)
	return m
}

func TestDrenarCola_ReenvioOK_MarcaReenviado(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake(nil) // succeeds on first attempt
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(3))

	m := recibirUno(t, svc, clock, "wamid-ok")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Reenviados)
	assert.Equal(t, 0, result.Fallidos)
	assert.Equal(t, 1, fwd.llamadasTotales())

	estado, ok := repo.estado(m.ID())
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioReenviado, estado)
}

func TestDrenarCola_FalloPermanente_MarcaFallidoConMotivoLegibleSinReintentar(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake(errForwarderPermanente)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(5))

	m := recibirUno(t, svc, clock, "wamid-permanente")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 0, result.Reenviados)
	assert.Equal(t, 1, result.Fallidos)
	assert.Equal(t, 1, fwd.llamadasTotales(), "a permanent error must not burn any retry")

	estado, ok := repo.estado(m.ID())
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estado)

	motivo := repo.motivoFallo(m.ID())
	assert.Contains(t, motivo, "número inválido", "the persisted reason must still be legible to a human")
}

func TestDrenarCola_FalloTransitorioAgotaReintentos_MarcaFallidoConMotivoLegible(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	transitorio := &domain.TransientError{Cause: errors.New("connection reset by peer")}
	fwd := newForwarderFake(transitorio, transitorio, transitorio) // always transient
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(3))

	m := recibirUno(t, svc, clock, "wamid-transitorio-agotado")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 0, result.Reenviados)
	assert.Equal(t, 1, result.Fallidos)
	assert.Equal(t, 3, fwd.llamadasTotales(), "must retry exactly MaxAttempts times before giving up")

	estado, ok := repo.estado(m.ID())
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estado)

	motivo := repo.motivoFallo(m.ID())
	assert.Contains(t, motivo, "connection reset by peer")
}

func TestDrenarCola_ReintentaConBackoffYLuegoTieneExito(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	transitorio := &domain.TransientError{Cause: errors.New("timeout")}
	// Fails twice, then succeeds on the third attempt.
	fwd := newForwarderFake(transitorio, transitorio, nil)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(5))

	m := recibirUno(t, svc, clock, "wamid-recupera")

	start := time.Now()
	result, err := svc.DrenarCola(context.Background(), 10)
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Reenviados)
	assert.Equal(t, 0, result.Fallidos)
	assert.Equal(t, 3, fwd.llamadasTotales())
	assert.GreaterOrEqual(t, elapsed, time.Millisecond, "real backoff delay must have elapsed between attempts")

	estado, ok := repo.estado(m.ID())
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioReenviado, estado)
}

func TestDrenarCola_FalloPermanenteDespuesDeUnTransitorio_DejaDeReintentar(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	transitorio := &domain.TransientError{Cause: errors.New("timeout")}
	// First attempt is transient (worth retrying), second is permanent
	// (must stop immediately, even though attempts remain).
	fwd := newForwarderFake(transitorio, errForwarderPermanente)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(10))

	m := recibirUno(t, svc, clock, "wamid-mezcla")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Fallidos)
	assert.Equal(t, 2, fwd.llamadasTotales(), "stops at the permanent error instead of burning all 10 attempts")

	motivo := repo.motivoFallo(m.ID())
	assert.Contains(t, motivo, "número inválido", "the recorded reason is the permanent one, not the earlier transient one")
}

func TestDrenarCola_SinPendientes_NoLlamaAlForwarder(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, canalapp.DrenarResultado{}, result)
	assert.Equal(t, 0, fwd.llamadasTotales())
}

func TestDrenarCola_ListarPendientesFalla_PropagaError(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	errRepo := errors.New("sqlite: locked")
	repo.fallarCon("ListarPendientes", errRepo)
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	_, err := svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
	assert.ErrorIs(t, err, errRepo)
}

func TestDrenarCola_MotivoDemasiadoLargo_SeTruncaYSiguePersistiendo(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	// The forwarder's own message alone exceeds domain's 500-codepoint bound
	// on MarcarFallido's motivo — proves legibleMotivo actually truncates
	// instead of letting the domain call reject the transition.
	largo := errors.New(strings.Repeat("x", 900))
	fwd := newForwarderFake(largo)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(1))

	m := recibirUno(t, svc, clock, "wamid-motivo-largo")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Fallidos)

	estado, ok := repo.estado(m.ID())
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estado, "MarcarFallido must succeed despite the oversized error message")
	assert.NotEmpty(t, repo.motivoFallo(m.ID()))
}

func TestDrenarCola_CircuitoAbierto_SaltaSinMarcarFallido(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	transitorio := &domain.TransientError{Cause: errors.New("connection refused")}
	fwd := newForwarderFake(transitorio)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))

	cfg := canalapp.ReenvioConfig{}
	cfg.Retry.MaxAttempts = 1
	cfg.Retry.Backoff = time.Millisecond
	cfg.Retry.MaxBackoff = time.Millisecond
	cfg.Circuit.FailureThreshold = 1 // trips after the very first failure
	cfg.Circuit.SuccessThreshold = 1
	cfg.Circuit.Delay = time.Hour // stays open for the rest of the test
	svc := newServiceFor(repo, fwd, clock, cfg)

	m1 := recibirUno(t, svc, clock, "wamid-trip")
	m2 := recibirUno(t, svc, clock, "wamid-skip")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	// One of the two trips the breaker (Fallidos or Saltados depending on
	// map iteration order); the other is always skipped once it is open.
	assert.Equal(t, 1, result.Saltados)
	assert.Equal(t, 1, result.Fallidos)

	// Whichever message was skipped stays pendiente untouched.
	var pendienteVisto, fallidoVisto bool
	e1, ok1 := repo.estado(m1.ID())
	e2, ok2 := repo.estado(m2.ID())
	require.True(t, ok1)
	require.True(t, ok2)
	for _, e := range []domain.EstadoReenvio{e1, e2} {
		if e == domain.EstadoReenvioPendiente {
			pendienteVisto = true
		}
		if e == domain.EstadoReenvioFallido {
			fallidoVisto = true
		}
	}
	assert.True(t, pendienteVisto, "the skipped message must stay pendiente, not fallido")
	assert.True(t, fallidoVisto)
}

// ── TxRunner ─────────────────────────────────────────────────────────────

func TestRecibirMensaje_UsaElTxRunnerInyectado(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	tx := &txFake{}
	svc := canalapp.NewService(repo, fwd, clock, tx, canalapp.ReenvioConfig{}, nil)

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-tx", clock.Now()))
	require.NoError(t, err)
	assert.Equal(t, 1, tx.veces(), "Guardar must run inside the injected transaction")
}

func TestRecibirMensaje_TxRunnerFalla_PropagaErrorSinGuardar(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	errTx := errors.New("no se pudo iniciar la transacción")
	tx := &txFake{fallarCon: errTx}
	svc := canalapp.NewService(repo, fwd, clock, tx, canalapp.ReenvioConfig{}, nil)

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-tx-falla", clock.Now()))
	require.ErrorIs(t, err, errTx)
	assert.Equal(t, 0, repo.guardarLlamadas, "a transaction that never begins must never reach Guardar")
}

func TestDrenarCola_UsaElTxRunnerInyectadoAlPersistirElReenvio(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake(nil)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	tx := &txFake{}
	svc := canalapp.NewService(repo, fwd, clock, tx, smallRetry(3), nil)

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-tx-reenvio", clock.Now()))
	require.NoError(t, err)
	tx.mu.Lock()
	tx.ejecutada = 0 // only count the DrenarCola-triggered transaction below.
	tx.mu.Unlock()

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Reenviados)
	assert.Equal(t, 1, tx.veces(), "persisting MarcarReenviado must run inside the injected transaction")
}

// ── persistence failures ────────────────────────────────────────────────

func TestDrenarCola_PersistirReenviadoFalla_PropagaError(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	errRepo := errors.New("sqlite: busy")
	fwd := newForwarderFake(nil)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(3))

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-persist-ok", clock.Now()))
	require.NoError(t, err)
	repo.fallarCon("MarcarReenviado", errRepo)

	_, err = svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
	assert.ErrorIs(t, err, errRepo)
}

func TestDrenarCola_PersistirFallidoFalla_PropagaError(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	errRepo := errors.New("sqlite: busy")
	fwd := newForwarderFake(errForwarderPermanente)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(3))

	_, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-persist-fail", clock.Now()))
	require.NoError(t, err)
	repo.fallarCon("MarcarFallido", errRepo)

	_, err = svc.DrenarCola(context.Background(), 10)
	require.Error(t, err)
	assert.ErrorIs(t, err, errRepo)
}
