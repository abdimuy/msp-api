//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app

// White-box tests for unexported functions: tick(), backoffDelay(), elegible().
// They live in package app (not app_test) so they can access unexported symbols
// directly. Lifecycle tests live in the companion pago_retry_worker_test.go
// (package app_test).

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── local fakes (re-declared in package app for white-box access) ───────────

// internalFixedClock is a local copy of fixedClock for use in package app
// tests. (fixedClock is defined in service_test.go which is package app_test.)
type internalFixedClock struct{ T time.Time }

func (c internalFixedClock) Now() time.Time { return c.T }

// internalFakeRepo is a minimal in-memory PagosRecibidosRepo for white-box
// tick/elegible tests.
type internalFakeRepo struct {
	rows    map[uuid.UUID]*domain.PagoRecibido
	listErr error
}

func newInternalFakeRepo() *internalFakeRepo {
	return &internalFakeRepo{rows: map[uuid.UUID]*domain.PagoRecibido{}}
}

func (r *internalFakeRepo) Insert(_ context.Context, p *domain.PagoRecibido) error {
	r.rows[p.ID()] = p
	return nil
}

func (r *internalFakeRepo) Update(_ context.Context, p *domain.PagoRecibido) error {
	r.rows[p.ID()] = p
	return nil
}

func (r *internalFakeRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.PagoRecibido, error) {
	p, ok := r.rows[id]
	if !ok {
		return nil, domain.ErrPagoNoEncontrado
	}
	return p, nil
}

func (r *internalFakeRepo) LockByID(_ context.Context, id uuid.UUID) error {
	if _, ok := r.rows[id]; !ok {
		return domain.ErrPagoNoEncontrado
	}
	return nil
}

func (r *internalFakeRepo) ListPendientes(_ context.Context, maxIntentos, limit int) ([]*domain.PagoRecibido, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	var out []*domain.PagoRecibido
	for _, p := range r.rows {
		if p.IsPendiente() && p.Intentos() < maxIntentos {
			out = append(out, p)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// internalFakeWriter is a minimal MicrosipPagoWriter for white-box tests.
type internalFakeWriter struct {
	result    outbound.MicrosipPagoResult
	err       error
	callCount int
}

func (f *internalFakeWriter) Aplicar(_ context.Context, _ outbound.MicrosipPagoInput) (outbound.MicrosipPagoResult, error) {
	f.callCount++
	return f.result, f.err
}

// internalFakeTxRunner is a minimal TxRunner for white-box tests.
type internalFakeTxRunner struct{}

func (internalFakeTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// newInternalAplicarSvc wires a Service for white-box tick tests.
func newInternalAplicarSvc(
	repo *internalFakeRepo,
	writer *internalFakeWriter,
	now time.Time,
) *Service {
	return NewService(
		nil, // saldos — not needed
		nil, // pagos  — not needed
		nil, // ventas — not needed
		internalFixedClock{T: now},
		repo,
		nil, // pagosImagenes
		writer,
		nil, // storage
		nil, // imageProc
		internalFakeTxRunner{},
	)
}

// buildWorker constructs a PagoRetryWorker for white-box tests.
func buildWorker(
	t *testing.T,
	repo *internalFakeRepo,
	writer *internalFakeWriter,
	now time.Time,
	cfg PagoRetryWorkerConfig,
) *PagoRetryWorker {
	t.Helper()
	svc := newInternalAplicarSvc(repo, writer, now)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	w := NewPagoRetryWorker(svc, repo, internalFixedClock{T: now}, cfg, logger)
	t.Cleanup(func() { _ = w.Stop(context.Background()) })
	return w
}

// pendingPago inserts a fresh pendiente (Intentos=0) into repo and returns it.
func pendingPago(t *testing.T, repo *internalFakeRepo, now time.Time) *domain.PagoRecibido {
	t.Helper()
	p, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             uuid.New(),
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  now.Add(-30 * time.Minute),
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Insert(context.Background(), p))
	return p
}

// pagoWithIntentos builds a pendiente via Hydrate with a specific Intentos count
// and UpdatedAt. This is the correct way to control UpdatedAt for elegible()
// tests, because audit.Auditable.MarkUpdated uses time.Now() internally.
func pagoWithIntentos(intentos int, updatedAt, now time.Time) *domain.PagoRecibido {
	return domain.HydratePagoRecibido(domain.HydratePagoRecibidoParams{
		ID:             uuid.New(),
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		ConceptoCCID:   87327,
		FechaHoraPago:  now.Add(-30 * time.Minute),
		Sincronizacion: domain.SincronizacionPendiente,
		Intentos:       intentos,
		ReceivedAt:     now,
		CreatedAt:      now,
		UpdatedAt:      updatedAt,
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
}

// ─── tick() tests ─────────────────────────────────────────────────────────────

func TestPagoRetryWorker_Tick_ListPendientesError(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	repo := newInternalFakeRepo()
	repo.listErr = errors.New("list_failed")
	writer := &internalFakeWriter{}

	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: 30 * time.Second,
		BackoffCap:  30 * time.Minute,
		BatchLimit:  100,
	}
	w := buildWorker(t, repo, writer, now, cfg)

	// tick() should log the error and return without calling the writer.
	w.tick(context.Background())

	assert.Equal(t, 0, writer.callCount, "writer must not be called when ListPendientes fails")
}

func TestPagoRetryWorker_Tick_EmptyList(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	repo := newInternalFakeRepo() // empty — no pendientes
	writer := &internalFakeWriter{}

	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: 30 * time.Second,
		BackoffCap:  30 * time.Minute,
		BatchLimit:  100,
	}
	w := buildWorker(t, repo, writer, now, cfg)

	w.tick(context.Background())

	assert.Equal(t, 0, writer.callCount, "writer must not be called when there are no pendientes")
}

func TestPagoRetryWorker_Tick_AllEligible_AllApplied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	repo := newInternalFakeRepo()
	writer := &internalFakeWriter{
		result: outbound.MicrosipPagoResult{
			DoctoCCID:      9001,
			ImpteDoctoCCID: 9002,
			Folio:          "AB-2026-001",
		},
	}

	// Seed 3 pendientes with Intentos=0 — all eligible immediately.
	pendingPago(t, repo, now)
	pendingPago(t, repo, now)
	pendingPago(t, repo, now)

	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: 30 * time.Second,
		BackoffCap:  30 * time.Minute,
		BatchLimit:  100,
	}
	w := buildWorker(t, repo, writer, now, cfg)

	w.tick(context.Background())

	assert.Equal(t, 3, writer.callCount, "writer must be called once per eligible pendiente")
}

func TestPagoRetryWorker_Tick_AllSkippedByBackoff(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	repo := newInternalFakeRepo()
	writer := &internalFakeWriter{}

	// Seed 3 pendientes with Intentos=2 and UpdatedAt=now → backoff not elapsed.
	// BackoffBase=1h, BackoffCap=1h, Intentos=2 → delay = 1h * 2^1 = 2h (capped
	// to 1h). Since now-now=0 < 1h, all are skipped.
	for range 3 {
		p := pagoWithIntentos(2, now, now)
		require.NoError(t, repo.Insert(context.Background(), p))
	}

	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: time.Hour,
		BackoffCap:  time.Hour,
		BatchLimit:  100,
	}
	w := buildWorker(t, repo, writer, now, cfg)

	w.tick(context.Background())

	assert.Equal(t, 0, writer.callCount, "writer must not be called when all rows are in backoff")
}

func TestPagoRetryWorker_Tick_Mixed(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	repo := newInternalFakeRepo()
	writer := &internalFakeWriter{
		result: outbound.MicrosipPagoResult{
			DoctoCCID:      9001,
			ImpteDoctoCCID: 9002,
			Folio:          "AB-2026-001",
		},
	}

	// Eligible: Intentos=0, always eligible.
	pendingPago(t, repo, now)

	// Skipped: Intentos=2 with UpdatedAt=now-1ms, base=30s → delay=60s > 1ms.
	skipped := pagoWithIntentos(2, now.Add(-time.Millisecond), now)
	require.NoError(t, repo.Insert(context.Background(), skipped))

	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: 30 * time.Second,
		BackoffCap:  30 * time.Minute,
		BatchLimit:  100,
	}
	w := buildWorker(t, repo, writer, now, cfg)

	w.tick(context.Background())

	assert.Equal(t, 1, writer.callCount, "only the Intentos=0 row must be applied")
}

// ─── backoffDelay() tests ────────────────────────────────────────────────────

func TestBackoffDelay_Boundaries(t *testing.T) {
	t.Parallel()

	base := 30 * time.Second
	cap30m := 30 * time.Minute

	tests := []struct {
		name     string
		intentos int
		base     time.Duration
		cap      time.Duration
		want     time.Duration
	}{
		{
			name:     "intentos_0_returns_0",
			intentos: 0,
			base:     base,
			cap:      cap30m,
			want:     0,
		},
		{
			name:     "intentos_1_returns_base",
			intentos: 1,
			base:     base,
			cap:      cap30m,
			want:     30 * time.Second, // 2^0 * 30s = 30s
		},
		{
			name:     "intentos_2_doubles",
			intentos: 2,
			base:     base,
			cap:      cap30m,
			want:     60 * time.Second, // 2^1 * 30s = 60s
		},
		{
			name:     "intentos_3_quadruples",
			intentos: 3,
			base:     base,
			cap:      cap30m,
			want:     120 * time.Second, // 2^2 * 30s = 120s
		},
		{
			name:     "intentos_10_capped_at_30min",
			intentos: 10,
			base:     base,
			cap:      cap30m,
			want:     cap30m, // 2^9 * 30s = 256 * 30s = ~2.1h → cap
		},
		{
			name:     "intentos_100_capped",
			intentos: 100,
			base:     base,
			cap:      cap30m,
			want:     cap30m,
		},
		{
			name:     "intentos_63_overflow_capped",
			intentos: 63,
			base:     time.Second,
			cap:      10 * time.Second,
			want:     10 * time.Second, // exp clamped at 30 → 2^30 * 1s >> cap
		},
		{
			name:     "intentos_negative_returns_0",
			intentos: -5,
			base:     base,
			cap:      cap30m,
			want:     0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := backoffDelay(tc.intentos, tc.base, tc.cap)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBackoffDelay_PowOverflowSafetyNet(t *testing.T) {
	t.Parallel()

	// intentos=1000 with base=1ns and cap=10s — exp is clamped to 30.
	// 2^30 * 1ns = ~1.07s, which is < cap. Either way the result must be >= 0.
	got := backoffDelay(1000, time.Nanosecond, 10*time.Second)
	assert.GreaterOrEqual(t, int64(got), int64(0), "backoffDelay must never return negative duration")
	assert.LessOrEqual(t, got, 10*time.Second, "backoffDelay must not exceed cap")
}

// ─── elegible() tests ────────────────────────────────────────────────────────

// makeWorkerForElegible builds a minimal worker to exercise elegible().
func makeWorkerForElegible(t *testing.T, cfg PagoRetryWorkerConfig, now time.Time) *PagoRetryWorker {
	t.Helper()
	repo := newInternalFakeRepo()
	writer := &internalFakeWriter{}
	return buildWorker(t, repo, writer, now, cfg)
}

func TestPagoRetryWorker_Elegible_IntentosZero(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: 30 * time.Second,
		BackoffCap:  30 * time.Minute,
		BatchLimit:  100,
	}
	w := makeWorkerForElegible(t, cfg, now)

	// A pago with Intentos=0 must always be eligible regardless of UpdatedAt.
	p := pagoWithIntentos(0, now, now)
	assert.True(t, w.elegible(p, now), "Intentos=0 must always be eligible")
}

func TestPagoRetryWorker_Elegible_NeedsBackoff(t *testing.T) {
	t.Parallel()

	// T is the reference point (UpdatedAt of the pago after RegistrarFallo).
	T := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	cfg := PagoRetryWorkerConfig{
		Interval:    time.Minute,
		MaxIntentos: 10,
		BackoffBase: 30 * time.Second,
		BackoffCap:  30 * time.Minute,
		BatchLimit:  100,
	}
	// The worker clock doesn't matter for elegible() — we pass now directly.
	w := makeWorkerForElegible(t, cfg, T)

	// Intentos=1 → delay = 30s * 2^0 = 30s.
	// Use HydratePagoRecibido with UpdatedAt=T so we control the backoff window.
	p := pagoWithIntentos(1, T, T)

	// T+29s → not yet eligible.
	assert.False(t, w.elegible(p, T.Add(29*time.Second)),
		"must not be eligible before backoff window elapses")

	// T+30s → exactly at threshold → eligible.
	assert.True(t, w.elegible(p, T.Add(30*time.Second)),
		"must be eligible exactly when backoff window elapses")

	// T+31s → past threshold → still eligible.
	assert.True(t, w.elegible(p, T.Add(31*time.Second)),
		"must be eligible after backoff window has passed")
}

// ─── helper: satisfy outbound.PagosRecibidosRepo for internalFakeRepo ────────

// Ensure internalFakeRepo satisfies outbound.PagosRecibidosRepo at compile
// time. This prevents silent drift if the interface changes.
var _ outbound.PagosRecibidosRepo = (*internalFakeRepo)(nil)

// Ensure internalFakeWriter satisfies outbound.MicrosipPagoWriter.
var _ outbound.MicrosipPagoWriter = (*internalFakeWriter)(nil)

// Ensure internalFakeTxRunner satisfies TxRunner.
var _ TxRunner = internalFakeTxRunner{}

// Ensure internalFixedClock satisfies outbound.Clock.
var _ outbound.Clock = internalFixedClock{}
