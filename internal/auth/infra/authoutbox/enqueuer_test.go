package authoutbox

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
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
	// Capture slog output so we can assert the failure was logged.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	boom := errors.New("simulated postgres tx open failure")
	runner := &fakeRunner{err: boom}
	enq := newEnqueuerWithRunner(runner)

	err := enq.Enqueue(context.Background(), "usuario", uuid.New(), "user.updated", map[string]any{"k": "v"})
	require.NoError(t, err, "best-effort: Enqueue must not surface the runner error")
	assert.Equal(t, 1, runner.called)
	assert.Contains(t, buf.String(), "auth.outbox_enqueue_failed")
	assert.Contains(t, buf.String(), "user.updated")
}

func TestEnqueuer_RunnerFailure_WithUnmarshalablePayload_LogsPlaceholder(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	// Force two failures at once: the runner fails (so we hit the logging
	// branch) AND the payload cannot be JSON-marshaled (a channel). The
	// log line must include the "<unmarshalable>" placeholder so we never
	// silently drop the failure record.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	boom := errors.New("simulated postgres tx open failure")
	runner := &fakeRunner{err: boom}
	enq := newEnqueuerWithRunner(runner)

	unmarshalable := make(chan int)
	err := enq.Enqueue(context.Background(), "rol", uuid.New(), "role.created", unmarshalable)
	require.NoError(t, err, "best-effort: Enqueue must not surface the runner error")
	assert.Equal(t, 1, runner.called)
	assert.Contains(t, buf.String(), "auth.outbox_enqueue_failed")
	assert.Contains(t, buf.String(), "<unmarshalable>",
		"the payload field must record a placeholder when json.Marshal fails")
}

func TestEnqueuer_RunnerSuccess_StillReturnsNil_OnInnerEnqueueFailure(t *testing.T) { //nolint:paralleltest // mutates slog.Default
	// When the runner succeeds but the inner platformoutbox.Enqueue surfaces
	// "no active tx" (because the fake runner does not plant a real pgx.Tx in
	// ctx), the Enqueuer still swallows the error as best-effort.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	runner := &fakeRunner{}
	enq := newEnqueuerWithRunner(runner)

	err := enq.Enqueue(context.Background(), "usuario", uuid.New(), "user.updated", "payload")
	require.NoError(t, err)
	assert.Equal(t, 1, runner.called)
	assert.Contains(t, buf.String(), "auth.outbox_enqueue_failed",
		"inner enqueue should fail without an active tx and be logged")
}
