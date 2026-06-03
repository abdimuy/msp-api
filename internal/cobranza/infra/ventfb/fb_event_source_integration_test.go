//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package ventfb_test

// Integration tests for fbEventSource bridge-goroutine lifecycle.
//
// These tests exercise the real *firebirdsql.FbEvent / *Subscription layer to
// verify that Subscribe + Unsubscribe leave no goroutines behind.  They are
// gated behind requireFBEventReachable so they skip cleanly when the Firebird
// auxiliary event port is not accessible (e.g. Docker networking on Mac).
//
// BUG REGRESSION COVERAGE
// ========================
// TestFbEventSource_BridgeGoroutineExitsOnUnsubscribe catches Bug 1 from the
// code review: the pre-fix Subscribe implementation blocked the bridge goroutine
// forever on `for ev := range rawCh` because the driver's doClose(nil) path
// (triggered by Unsubscribe) never closes rawCh.  After the fix the bridge
// selects on a `done` channel that is closed by the unsubscribe func, so the
// goroutine exits promptly.
//
// TestFbEventSource_BridgeExitsOnDriverDisconnect catches Bug 2: the pre-fix
// implementation had no way to detect a TCP drop because rawCh was never
// closed by the driver on network failure.  After the fix the bridge selects on
// errCh (registered via sub.NotifyClose) so it exits when the driver signals
// the connection error.

import (
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// buildEventSourceDSN constructs the DSN from env vars, matching the same
// logic used by requireFBEventReachable and newFBEvent.
func buildEventSourceDSN(t *testing.T) string {
	t.Helper()
	host := envOrDefault("FB_HOST", "localhost")
	port := envOrDefault("FB_PORT", "3050")
	database := os.Getenv("FB_DATABASE")
	user := envOrDefault("FB_USER", "SYSDBA")
	password := os.Getenv("FB_PASSWORD")
	charset := envOrDefault("FB_CHARSET", "UTF8")
	cfg := config.Firebird{
		Host:         host,
		Port:         portFromEnv(t, port),
		Database:     database,
		User:         user,
		Password:     password,
		Charset:      charset,
		WireCrypt:    true,
		WireCompress: false,
	}
	return cfg.DSN()
}

// TestFbEventSource_BridgeGoroutineExitsOnUnsubscribe verifies that the bridge
// goroutine started by Subscribe exits promptly after Unsubscribe is called.
//
// This test would FAIL against the pre-fix code: the old bridge ran
// `for ev := range rawCh` and rawCh was never closed by the driver on a clean
// Unsubscribe — so the goroutine leaked for the process lifetime.  After the
// fix, Unsubscribe closes a `done` channel that unblocks the bridge's select.
//
//nolint:paralleltest
func TestFbEventSource_BridgeGoroutineExitsOnUnsubscribe(t *testing.T) {
	requireFBEnv(t)
	requireFBEventReachable(t)

	src, err := ventfb.NewFbEventSource(buildEventSourceDSN(t))
	require.NoError(t, err, "NewFbEventSource")
	defer func() { _ = src.Close() }()

	baseline := runtime.NumGoroutine()

	_, unsubscribe, err := src.Subscribe([]string{"_goroutine_leak_probe_"})
	require.NoError(t, err, "Subscribe")

	// Goroutine count must have gone up by at least 1 (the bridge goroutine).
	afterSubscribe := runtime.NumGoroutine()
	assert.GreaterOrEqual(t, afterSubscribe, baseline+1,
		"expected at least one new goroutine after Subscribe")

	require.NoError(t, unsubscribe(), "unsubscribe")

	// Allow the bridge goroutine a short window to exit after Unsubscribe.
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseline+1
	}, 2*time.Second, 10*time.Millisecond,
		"bridge goroutine did not exit after Unsubscribe (goroutine leak); "+
			"goroutines after unsubscribe: %d, baseline: %d",
		runtime.NumGoroutine(), baseline)
}

// TestFbEventSource_BridgeExitsOnDriverDisconnect verifies that the bridge
// goroutine exits when the underlying FbEventSource is closed while a
// subscription is active — simulating the driver-initiated disconnect path
// (Bug 2 regression).
//
// Calling src.Close() tears down the FbEvent connection and calls
// unsubscribeNoNotify on all active subscriptions, which triggers doClose with
// a non-nil error via the eventManager's error channel, causing errCh (registered
// via sub.NotifyClose) to fire.  The bridge goroutine must exit via that path.
//
//nolint:paralleltest
func TestFbEventSource_BridgeExitsOnDriverDisconnect(t *testing.T) {
	requireFBEnv(t)
	requireFBEventReachable(t)

	src, err := ventfb.NewFbEventSource(buildEventSourceDSN(t))
	require.NoError(t, err, "NewFbEventSource")

	baseline := runtime.NumGoroutine()

	_, unsubscribe, err := src.Subscribe([]string{"_disconnect_probe_"})
	require.NoError(t, err, "Subscribe")

	afterSubscribe := runtime.NumGoroutine()
	assert.GreaterOrEqual(t, afterSubscribe, baseline+1,
		"expected at least one new goroutine after Subscribe")

	// Close the source — simulates a TCP drop / driver-side disconnect.
	// The bridge must exit via errCh rather than waiting on rawCh forever.
	require.NoError(t, src.Close(), "src.Close")

	// Allow the bridge goroutine a short window to exit.
	assert.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= baseline+1
	}, 2*time.Second, 10*time.Millisecond,
		"bridge goroutine did not exit after src.Close (goroutine leak on TCP drop); "+
			"goroutines after close: %d, baseline: %d",
		runtime.NumGoroutine(), baseline)

	// Unsubscribe after Close is a safe no-op (Unsubscribe checks IsClose).
	_ = unsubscribe()
}
