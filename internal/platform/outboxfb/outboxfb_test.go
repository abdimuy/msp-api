package outboxfb_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/outboxfb"
)

func TestErrTransient_IsAValue(t *testing.T) {
	t.Parallel()
	require.Error(t, outboxfb.ErrTransient)
	wrapped := errors.Join(outboxfb.ErrTransient, errors.New("upstream"))
	assert.ErrorIs(t, wrapped, outboxfb.ErrTransient)
}

type stubHandler struct {
	eventType string
	called    bool
}

func (h *stubHandler) EventType() string { return h.eventType }
func (h *stubHandler) Handle(_ context.Context, _ outboxfb.Event) error {
	h.called = true
	return nil
}

func TestHandlerRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	reg := outboxfb.NewHandlerRegistry()
	h := &stubHandler{eventType: "push_to_microsip"}
	reg.Register(h)

	got := reg.Lookup("push_to_microsip")
	require.NotNil(t, got)
	assert.Same(t, h, got)
}

func TestHandlerRegistry_LookupMiss_ReturnsNil(t *testing.T) {
	t.Parallel()
	reg := outboxfb.NewHandlerRegistry()
	assert.Nil(t, reg.Lookup("nope"))
}

func TestHandlerRegistry_DuplicateRegistration_Panics(t *testing.T) {
	t.Parallel()
	reg := outboxfb.NewHandlerRegistry()
	reg.Register(&stubHandler{eventType: "x"})

	assert.Panics(t, func() {
		reg.Register(&stubHandler{eventType: "x"})
	}, "duplicate registration must panic")
}

func TestEnqueue_NoTxInCtx_Errors(t *testing.T) {
	t.Parallel()
	// No tx in context — must return outbox_no_tx.
	err := outboxfb.Enqueue(context.Background(), nil, outboxfb.Event{
		ID:          uuid.New(),
		Aggregate:   "venta",
		AggregateID: uuid.New(),
		EventType:   "venta.creada",
		Payload:     []byte(`{"a":1}`),
	})
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok, "expected apperror, got: %T %v", err, err)
	assert.Equal(t, "outbox_no_tx", appErr.Code)
}
