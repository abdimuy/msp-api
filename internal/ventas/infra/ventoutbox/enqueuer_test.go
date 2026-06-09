package ventoutbox

import (
	"context"
	"encoding/json"
	"errors"
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

func TestEnqueuer_RunnerFailure_PropagatesError(t *testing.T) {
	t.Parallel()
	boom := errors.New("simulated firebird tx open failure")
	runner := &fakeRunner{err: boom}
	enq := newEnqueuerWithRunner(runner, nil)

	err := enq.Enqueue(
		context.Background(),
		"venta",
		uuid.New(),
		"venta.creada",
		map[string]any{"k": "v"},
	)
	require.Error(t, err, "atomicity: enqueue errors must propagate so the outer tx can roll back")
	require.ErrorIs(t, err, boom)
	assert.Equal(t, 1, runner.called)
}

func TestEnqueuer_PayloadMarshalFailure_PropagatesError(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	enq := newEnqueuerWithRunner(runner, nil)

	unmarshalable := make(chan int)
	err := enq.Enqueue(
		context.Background(),
		"venta",
		uuid.New(),
		"venta.creada",
		unmarshalable,
	)
	require.Error(t, err)
	assert.Equal(t, 0, runner.called, "runner must not be invoked when the payload can't marshal")
}

func TestMarshalPayload_PassesRawMessageThrough(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"already":"encoded"}`)
	got, err := marshalPayload(raw)
	require.NoError(t, err)
	assert.Equal(t, []byte(raw), []byte(got))
}

func TestMarshalPayload_NilYieldsJSONNull(t *testing.T) {
	t.Parallel()
	got, err := marshalPayload(nil)
	require.NoError(t, err)
	assert.Equal(t, `null`, string(got))
}

func TestMarshalPayload_StructEncodes(t *testing.T) {
	t.Parallel()
	got, err := marshalPayload(map[string]any{"hello": "world"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"hello":"world"}`, string(got))
}

func TestMarshalPayload_ChannelFailsToMarshal(t *testing.T) {
	t.Parallel()
	_, err := marshalPayload(make(chan int))
	require.Error(t, err)
}
