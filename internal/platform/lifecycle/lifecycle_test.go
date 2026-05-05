package lifecycle_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/platform/lifecycle"
)

type fakeHooks struct {
	startCalled bool
	stopCalled  bool
	startErr    error
	stopErr     error
}

func (f *fakeHooks) Start(_ context.Context) error {
	f.startCalled = true
	return f.startErr
}

func (f *fakeHooks) Stop(_ context.Context) error {
	f.stopCalled = true
	return f.stopErr
}

func TestAppend_StartAndStopAreInvoked(t *testing.T) {
	t.Parallel()
	hooks := &fakeHooks{}

	app := fx.New(
		fx.Invoke(func(lc fx.Lifecycle) {
			lifecycle.Append(lc, "test", hooks)
		}),
		fx.NopLogger,
	)

	ctx := context.Background()
	require.NoError(t, app.Start(ctx))
	require.NoError(t, app.Stop(ctx))

	assert.True(t, hooks.startCalled)
	assert.True(t, hooks.stopCalled)
}

func TestAppend_StartErrorAborts(t *testing.T) {
	t.Parallel()
	hooks := &fakeHooks{startErr: errors.New("boom")}

	app := fx.New(
		fx.Invoke(func(lc fx.Lifecycle) {
			lifecycle.Append(lc, "test", hooks)
		}),
		fx.NopLogger,
	)
	require.Error(t, app.Start(context.Background()))
}

func TestAppend_StopErrorPropagates(t *testing.T) {
	t.Parallel()
	hooks := &fakeHooks{stopErr: errors.New("boom-stop")}

	app := fx.New(
		fx.Invoke(func(lc fx.Lifecycle) {
			lifecycle.Append(lc, "test", hooks)
		}),
		fx.NopLogger,
	)
	require.NoError(t, app.Start(context.Background()))
	require.Error(t, app.Stop(context.Background()))
}
