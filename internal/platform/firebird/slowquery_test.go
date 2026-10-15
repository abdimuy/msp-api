package firebird

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/logger"
)

// steppingClock returns start, start+step, start+2*step, ... on every call.
type steppingClock struct {
	mu   sync.Mutex
	next time.Time
	step time.Duration
}

func (c *steppingClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.next
	c.next = c.next.Add(c.step)
	return t
}

// slowConn wires a wrapper conn with a JSON logger built the same way the
// API builds its own (logger.New adds request_id from the context), and a
// clock where every statement "takes" step.
func slowConn(t *testing.T, threshold, step time.Duration) (*cancelProofConn, *fakeConn, *bytes.Buffer) {
	t.Helper()
	conn, fc, _ := newWrapped()
	buf := &bytes.Buffer{}
	conn.slow = slowQueryLog{
		threshold: threshold,
		logger:    logger.New(logger.Options{Level: "info", Format: "json", Output: buf}),
		now:       (&steppingClock{next: time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC), step: step}).now,
	}
	return conn, fc, buf
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		out = append(out, m)
	}
	return out
}

// drain reads every row then closes, the way database/sql does.
func drain(t *testing.T, rows driver.Rows) {
	t.Helper()
	for {
		if err := rows.Next(nil); err != nil {
			require.ErrorIs(t, err, io.EOF)
			break
		}
	}
	require.NoError(t, rows.Close())
}

const consultaConDatos = "SELECT NOMBRE FROM CLIENTES WHERE RFC = 'GOMA800101XXX'"

func TestSlowQuery_QueryAboveThresholdIsLoggedWithLabelAndRequestID(t *testing.T) {
	t.Parallel()
	conn, _, buf := slowConn(t, time.Second, 2*time.Second)
	ctx := logger.WithRequestID(context.Background(), "req-7f3a")

	rows, err := conn.QueryContext(ctx, consultaConDatos, nil)
	require.NoError(t, err)
	// Closed here, not through drain(): the label is the frame that closes the
	// rows, which in production is the repository method's deferred Close.
	require.NoError(t, rows.Close())

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	l := lines[0]
	assert.Equal(t, "firebird.consulta_lenta", l["msg"])
	assert.Equal(t, "WARN", l["level"])
	assert.Equal(t, "req-7f3a", l["request_id"])
	assert.InDelta(t, 2000, l["duracion_ms"], 0.1)
	assert.Equal(t, "query", l["operacion"])
	assert.Contains(t, l["consulta"], "TestSlowQuery_QueryAboveThresholdIsLoggedWithLabelAndRequestID",
		"la etiqueta es la función que lanzó la sentencia, fuera de database/sql y de este paquete")
	assert.Contains(t, l["origen"], "slowquery_test.go:")
	assert.NotEmpty(t, l["sql_hash"])
	assert.NotContains(t, buf.String(), "GOMA800101XXX", "el texto de la sentencia puede traer datos: nunca va al log")
	assert.NotContains(t, buf.String(), "CLIENTES")
}

func TestSlowQuery_BelowThresholdIsSilent(t *testing.T) {
	t.Parallel()
	conn, _, buf := slowConn(t, time.Second, 500*time.Millisecond)
	rows, err := conn.QueryContext(context.Background(), "SELECT 1 FROM RDB$DATABASE", nil)
	require.NoError(t, err)
	drain(t, rows)
	_, err = conn.ExecContext(context.Background(), "UPDATE X SET Y = 1", nil)
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestSlowQuery_ExecAndPreparedPathsAreTimed(t *testing.T) {
	t.Parallel()
	conn, _, buf := slowConn(t, time.Second, 3*time.Second)

	_, err := conn.ExecContext(context.Background(), "UPDATE X SET Y = 1", nil)
	require.NoError(t, err)

	stmt, err := conn.PrepareContext(context.Background(), "SELECT 1 FROM RDB$DATABASE")
	require.NoError(t, err)
	rows, err := stmt.(driver.StmtQueryContext).QueryContext(context.Background(), nil)
	require.NoError(t, err)
	drain(t, rows)
	_, err = stmt.(driver.StmtExecContext).ExecContext(context.Background(), nil)
	require.NoError(t, err)

	lines := logLines(t, buf)
	require.Len(t, lines, 3)
	assert.Equal(t, "exec", lines[0]["operacion"])
	assert.Equal(t, "query", lines[1]["operacion"])
	assert.Equal(t, "exec", lines[2]["operacion"])
}

func TestSlowQuery_LogsTheStatementTimeoutOverride(t *testing.T) {
	t.Parallel()
	conn, _, buf := slowConn(t, time.Second, 2*time.Second)
	ctx := WithStatementTimeout(context.Background(), 25*time.Second)
	rows, err := conn.QueryContext(ctx, "SELECT 1 FROM RDB$DATABASE", nil)
	require.NoError(t, err)
	drain(t, rows)
	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	assert.InDelta(t, 25, lines[0]["tope_s"], 0.1)
}

func TestSlowQuery_DisabledThresholdDoesNotWrapRows(t *testing.T) {
	t.Parallel()
	conn, _, _ := newWrapped()
	rows, err := conn.QueryContext(context.Background(), "SELECT 1", nil)
	require.NoError(t, err)
	assert.IsType(t, &fakeRows{}, rows, "sin umbral no se paga ningún envoltorio")
}

func TestSlowQuery_SameTextSameHashDifferentTextDifferentHash(t *testing.T) {
	t.Parallel()
	first := sqlHash("SELECT 1")
	assert.Equal(t, first, sqlHash("SELECT 1"))
	assert.NotEqual(t, sqlHash("SELECT 1"), sqlHash("SELECT 2"))
}

// richRows implements every column-type interface firebirdsqlRows does.
type richRows struct{ fakeRows }

func (*richRows) ColumnTypeDatabaseTypeName(int) string             { return "VARCHAR" }
func (*richRows) ColumnTypeLength(int) (int64, bool)                { return 40, true }
func (*richRows) ColumnTypeNullable(int) (bool, bool)               { return true, true }
func (*richRows) ColumnTypePrecisionScale(int) (int64, int64, bool) { return 18, 2, true }
func (*richRows) ColumnTypeScanType(int) reflect.Type               { return reflect.TypeOf("") }

func TestSlowQuery_TimedRowsForwardsColumnTypes(t *testing.T) {
	t.Parallel()
	r := &timedRows{base: &richRows{}, log: &slowQueryLog{now: time.Now}, ctx: context.Background()}
	assert.Equal(t, "VARCHAR", r.ColumnTypeDatabaseTypeName(0))
	n, ok := r.ColumnTypeLength(0)
	assert.True(t, ok)
	assert.Equal(t, int64(40), n)
	null, ok := r.ColumnTypeNullable(0)
	assert.True(t, ok)
	assert.True(t, null)
	p, s, ok := r.ColumnTypePrecisionScale(0)
	assert.True(t, ok)
	assert.Equal(t, [2]int64{18, 2}, [2]int64{p, s})
	assert.Equal(t, reflect.TypeOf(""), r.ColumnTypeScanType(0))

	// A base without the interfaces degrades to database/sql's own defaults.
	bare := &timedRows{base: &fakeRows{}, log: &slowQueryLog{now: time.Now}, ctx: context.Background()}
	assert.Empty(t, bare.ColumnTypeDatabaseTypeName(0))
	_, ok = bare.ColumnTypeLength(0)
	assert.False(t, ok)
	_, ok = bare.ColumnTypeNullable(0)
	assert.False(t, ok)
	_, _, ok = bare.ColumnTypePrecisionScale(0)
	assert.False(t, ok)
	assert.Equal(t, reflect.TypeOf(new(any)).Elem(), bare.ColumnTypeScanType(0))
}

func TestCancelProofDriver_OpenPassesSlowQueryLogToConn(t *testing.T) {
	t.Parallel()
	fc := &fakeConn{stmt: &fakeStmt{}}
	l := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := &cancelProofDriver{base: &fakeDriver{conn: fc}, slowQueryThreshold: time.Second, logger: l}
	c, err := d.Open("dsn")
	require.NoError(t, err)
	conn, ok := c.(*cancelProofConn)
	require.True(t, ok)
	assert.Equal(t, time.Second, conn.slow.threshold)
	assert.Same(t, l, conn.slow.logger)
}
