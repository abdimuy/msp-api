package outbox_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/outbox"
)

func TestErrTransient_IsAValue(t *testing.T) {
	t.Parallel()
	require.Error(t, outbox.ErrTransient)
	wrapped := errors.Join(outbox.ErrTransient, errors.New("upstream"))
	assert.ErrorIs(t, wrapped, outbox.ErrTransient)
}

type stubHandler struct {
	eventType string
	called    bool
}

func (h *stubHandler) EventType() string                              { return h.eventType }
func (h *stubHandler) Handle(_ context.Context, _ outbox.Event) error { h.called = true; return nil }

func TestHandlerRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()
	reg := outbox.NewHandlerRegistry()
	h := &stubHandler{eventType: "push_to_microsip"}
	reg.Register(h)

	got := reg.Lookup("push_to_microsip")
	require.NotNil(t, got)
	assert.Same(t, h, got)
}

func TestHandlerRegistry_LookupMiss_ReturnsNil(t *testing.T) {
	t.Parallel()
	reg := outbox.NewHandlerRegistry()
	assert.Nil(t, reg.Lookup("nope"))
}

func TestHandlerRegistry_DuplicateRegistration_Panics(t *testing.T) {
	t.Parallel()
	reg := outbox.NewHandlerRegistry()
	reg.Register(&stubHandler{eventType: "x"})

	assert.Panics(t, func() {
		reg.Register(&stubHandler{eventType: "x"})
	}, "duplicate registration must panic")
}

func TestEnqueue_FailsWithoutActiveTx(t *testing.T) {
	t.Parallel()
	// No tx in context → must error.
	err := outbox.Enqueue(context.Background(), "cliente", uuid.Nil, "push", map[string]any{"a": 1})
	require.Error(t, err)
}
