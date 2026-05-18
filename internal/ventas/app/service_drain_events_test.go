//nolint:misspell // ventas vocabulary is Spanish per project convention.
package app_test

// Tests for Service.drainEvents best-effort contract: when the outbox
// enqueuer errors, the operation still succeeds and a structured log entry
// captures enough fields for an operator to chase down the lost event.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLogs swaps slog's default for a JSON handler writing to a bytes
// buffer for the duration of the test. The returned func returns the
// accumulated log output as a single string.
func captureLogs(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return func() string { return buf.String() }
}

// TestService_DrainEvents_FailingEnqueuer_LogsAndContinues verifies the
// best-effort contract: an outbox failure is logged with enough structured
// fields (aggregate id, event type) to find the lost event later, and the
// business operation that triggered it still returns success.
func TestService_DrainEvents_FailingEnqueuer_LogsAndContinues(t *testing.T) {
	t.Parallel()
	logs := captureLogs(t)

	h := newHarness(t)
	h.outbox.err = errors.New("outbox writer unavailable")

	venta, err := h.svc.CrearVenta(t.Context(), validContadoInput(), uuid.New())
	require.NoError(t, err, "outbox failure must NOT abort the create — best-effort contract")
	require.NotNil(t, venta)
	assert.Equal(t, 1, h.ventas.SaveCalls)

	logOutput := logs()
	assert.Contains(t, logOutput, "ventas.outbox_enqueue_failed",
		"failure must be observable in structured logs")
	// At least one log line must reference both the aggregate_id (the venta
	// uuid) and the event_type so an operator can chase the lost event.
	requireLogFieldsForVenta(t, logOutput, venta.ID())
}

// requireLogFieldsForVenta scans newline-delimited JSON log entries and
// asserts at least one carries the expected aggregate_id and a non-empty
// event_type. A single line with all required fields is enough — extras
// from cleanups or interleaving subtests are ignored.
func requireLogFieldsForVenta(t *testing.T, logOutput string, ventaID uuid.UUID) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(logOutput), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry["msg"] != "ventas.outbox_enqueue_failed" {
			continue
		}
		if id, _ := entry["aggregate_id"].(string); id != ventaID.String() {
			continue
		}
		if et, _ := entry["event_type"].(string); et != "" {
			return
		}
	}
	t.Fatalf("no outbox failure log line found carrying aggregate_id=%s with non-empty event_type; logs:\n%s",
		ventaID, logOutput)
}

// silenceUnusedContext keeps the imports tidy if a future test needs ctx.
var _ = context.Background
