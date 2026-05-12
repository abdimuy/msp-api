package ventoutbox

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fakeRunner is a txRunner whose RunInTx behavior is configured per-test.
type fakeRunner struct {
	err    error
	called int
}

func (f *fakeRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	f.called++
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

func TestEnqueuer_ImplementsPort(t *testing.T) {
	t.Parallel()
	var _ outbound.OutboxEnqueuer = (*Enqueuer)(nil)
}

func TestEnqueuer_RunnerFailure_LogsAndReturnsNil(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	boom := errors.New("simulated postgres tx open failure")
	runner := &fakeRunner{err: boom}
	enq := newEnqueuerWithRunner(runner)

	err := enq.Enqueue(context.Background(), "venta", uuid.New(), "venta.creada", map[string]any{"k": "v"})
	require.NoError(t, err, "best-effort: Enqueue must not surface the runner error")
	assert.Equal(t, 1, runner.called)
	assert.Contains(t, buf.String(), "ventas.outbox_enqueue_failed")
	assert.Contains(t, buf.String(), "venta.creada")
}

func TestEnqueuer_RunnerFailure_WithUnmarshalablePayload_LogsPlaceholder(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	boom := errors.New("simulated postgres tx open failure")
	runner := &fakeRunner{err: boom}
	enq := newEnqueuerWithRunner(runner)

	unmarshalable := make(chan int)
	err := enq.Enqueue(context.Background(), "venta", uuid.New(), "venta.creada", unmarshalable)
	require.NoError(t, err, "best-effort: Enqueue must not surface the runner error")
	assert.Equal(t, 1, runner.called)
	assert.Contains(t, buf.String(), "ventas.outbox_enqueue_failed")
	assert.Contains(t, buf.String(), "<unmarshalable>",
		"the payload field must record a placeholder when json.Marshal fails")
}

func TestEnqueuer_RunnerSuccess_StillReturnsNil_OnInnerEnqueueFailure(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	runner := &fakeRunner{}
	enq := newEnqueuerWithRunner(runner)

	err := enq.Enqueue(context.Background(), "venta", uuid.New(), "venta.creada", "payload")
	require.NoError(t, err)
	assert.Equal(t, 1, runner.called)
	assert.Contains(t, buf.String(), "ventas.outbox_enqueue_failed",
		"inner enqueue should fail without an active tx and be logged")
}
