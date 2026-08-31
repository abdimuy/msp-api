package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	"github.com/abdimuy/msp-api/internal/canal/domain"
)

var errForwarderPermanente = errors.New("400: número inválido")

// ── constructor helper ──────────────────────────────────────────────────

func newServiceFor(repo *buzonRepoFake, fwd *forwarderFake, clock *fixedClock, cfg canalapp.ReenvioConfig) *canalapp.Service {
	return canalapp.NewService(repo, fwd, newSenderFake(), clock, nil, cfg, nil)
}

// ── RecibirMensaje ───────────────────────────────────────────────────────

func TestRecibirMensaje_MensajeNuevo(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	inserted, err := svc.RecibirMensaje(context.Background(), mensajeParams("wamid-1", clock.Now()))
	require.NoError(t, err)
	assert.True(t, inserted)

	id, ok := repo.idPorWamid("wamid-1")
	require.True(t, ok)
	estado, ok := repo.estado(id)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioPendiente, estado)
	assert.Equal(t, 1, repo.countByWamid("wamid-1"))
}

func TestRecibirMensaje_MensajeDuplicadoPorWamid_NoOpYSinEvento(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, canalapp.ReenvioConfig{})

	ctx := context.Background()
	firstInserted, err := svc.RecibirMensaje(ctx, mensajeParams("wamid-dup", clock.Now()))
	require.NoError(t, err)
	assert.True(t, firstInserted, "the first delivery of a wamid must be reported as inserted")

	// Meta redelivers the same webhook — same wamid, arriving "later".
	clock.advance(time.Minute)
	secondInserted, err := svc.RecibirMensaje(ctx, mensajeParams("wamid-dup", clock.Now()))
	require.NoError(t, err, "a redelivered webhook must not surface as an error")
	assert.False(t, secondInserted, "a redelivered wamid must be reported as not-inserted")

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

// recibirUno inserts one message via RecibirMensaje and returns its
// persisted id, looked up through the repo by wamid — RecibirMensaje
// itself no longer returns the entity (see its doc comment for why).
func recibirUno(t *testing.T, svc *canalapp.Service, repo *buzonRepoFake, clock *fixedClock, wamid string) uuid.UUID {
	t.Helper()
	inserted, err := svc.RecibirMensaje(context.Background(), mensajeParams(wamid, clock.Now()))
	require.NoError(t, err)
	require.True(t, inserted)
	id, ok := repo.idPorWamid(wamid)
	require.True(t, ok)
	return id
}

func TestDrenarCola_ReenvioOK_MarcaReenviado(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake(nil) // succeeds on first attempt
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(3))

	id := recibirUno(t, svc, repo, clock, "wamid-ok")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Reenviados)
	assert.Equal(t, 0, result.Fallidos)
	assert.Equal(t, 1, fwd.llamadasTotales())

	estado, ok := repo.estado(id)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioReenviado, estado)
}

func TestDrenarCola_FalloPermanente_MarcaFallidoConMotivoLegibleSinReintentar(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake(errForwarderPermanente)
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(5))

	id := recibirUno(t, svc, repo, clock, "wamid-permanente")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 0, result.Reenviados)
	assert.Equal(t, 1, result.Fallidos)
	assert.Equal(t, 1, fwd.llamadasTotales(), "a permanent error must not burn any retry")

	estado, ok := repo.estado(id)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estado)

	motivo := repo.motivoFallo(id)
	assert.Contains(t, motivo, "número inválido", "the persisted reason must still be legible to a human")
}

func TestDrenarCola_FalloTransitorioAgotaReintentos_MarcaFallidoConMotivoLegible(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	transitorio := &domain.TransientError{Cause: errors.New("connection reset by peer")}
	fwd := newForwarderFake(transitorio, transitorio, transitorio) // always transient
	clock := newFixedClock(time.Date(2026, 8, 31, 10, 0, 0, 0, time.UTC))
	svc := newServiceFor(repo, fwd, clock, smallRetry(3))

	id := recibirUno(t, svc, repo, clock, "wamid-transitorio-agotado")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 0, result.Reenviados)
	assert.Equal(t, 1, result.Fallidos)
	assert.Equal(t, 3, fwd.llamadasTotales(), "must retry exactly MaxAttempts times before giving up")

	estado, ok := repo.estado(id)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estado)

	motivo := repo.motivoFallo(id)
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

	id := recibirUno(t, svc, repo, clock, "wamid-recupera")

	start := time.Now()
	result, err := svc.DrenarCola(context.Background(), 10)
	elapsed := time.Since(start)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Reenviados)
	assert.Equal(t, 0, result.Fallidos)
	assert.Equal(t, 3, fwd.llamadasTotales())
	assert.GreaterOrEqual(t, elapsed, time.Millisecond, "real backoff delay must have elapsed between attempts")

	estado, ok := repo.estado(id)
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

	id := recibirUno(t, svc, repo, clock, "wamid-mezcla")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Fallidos)
	assert.Equal(t, 2, fwd.llamadasTotales(), "stops at the permanent error instead of burning all 10 attempts")

	motivo := repo.motivoFallo(id)
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

	id := recibirUno(t, svc, repo, clock, "wamid-motivo-largo")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Fallidos)

	estado, ok := repo.estado(id)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estado, "MarcarFallido must succeed despite the oversized error message")
	assert.NotEmpty(t, repo.motivoFallo(id))
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

	// idTrip is received strictly before idSkip (clock.advance in between),
	// and the fake's ListarPendientes is RecibidoEn-ascending (see its own
	// doc comment) — so idTrip is deterministically the one DrenarCola
	// reaches first, trips the breaker, and idSkip is deterministically the
	// one that finds it already open.
	idTrip := recibirUno(t, svc, repo, clock, "wamid-trip")
	clock.advance(time.Second)
	idSkip := recibirUno(t, svc, repo, clock, "wamid-skip")

	result, err := svc.DrenarCola(context.Background(), 10)
	require.NoError(t, err)

	assert.Equal(t, 1, result.Fallidos, "the earliest pendiente trips the breaker on its own failure")
	assert.Equal(t, 1, result.Saltados, "the later pendiente is skipped once the breaker is open")

	estadoTrip, ok := repo.estado(idTrip)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioFallido, estadoTrip)

	estadoSkip, ok := repo.estado(idSkip)
	require.True(t, ok)
	assert.Equal(t, domain.EstadoReenvioPendiente, estadoSkip, "the skipped message must stay pendiente, not fallido")
}

// ── TxRunner ─────────────────────────────────────────────────────────────

func TestRecibirMensaje_UsaElTxRunnerInyectado(t *testing.T) {
	t.Parallel()
	repo := newBuzonRepoFake()
	fwd := newForwarderFake()
	clock := newFixedClock(time.Now().UTC())
	tx := &txFake{}
	svc := canalapp.NewService(repo, fwd, newSenderFake(), clock, tx, canalapp.ReenvioConfig{}, nil)

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
	svc := canalapp.NewService(repo, fwd, newSenderFake(), clock, tx, canalapp.ReenvioConfig{}, nil)

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
	svc := canalapp.NewService(repo, fwd, newSenderFake(), clock, tx, smallRetry(3), nil)

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
