package firebird

import (
	"context"
	"database/sql/driver"
	"hash/fnv"
	"log/slog"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// slowQueryMsg is the log message every slow statement is reported under, so
// support can grep one stable key.
const slowQueryMsg = "firebird.consulta_lenta"

// slowQueryLog reports statements whose execution (for queries: execution plus
// fetch, until the rows are closed) takes longer than threshold.
//
// The statement text is never logged: it can carry literals with customer
// data. Instead each line names the Go function that ran the statement (the
// first frame outside database/sql, otelsql and this package) plus a short
// hash of the text, which tells apart two statements issued from the same
// function. The request_id is added by the API's slog handler from the
// context, so the line correlates with the access log of the same request.
type slowQueryLog struct {
	threshold time.Duration
	logger    *slog.Logger // nil → slog.Default() at log time.
	now       func() time.Time
}

// enabled reports whether statements must be timed at all.
func (l *slowQueryLog) enabled() bool { return l.threshold > 0 }

// clock returns the configured clock, falling back to time.Now.
func (l *slowQueryLog) clock() time.Time {
	if l.now == nil {
		return time.Now()
	}
	return l.now()
}

// observe logs one finished statement when it crossed the threshold. rows is
// -1 for exec statements.
func (l *slowQueryLog) observe(ctx context.Context, op, query string, start time.Time, rows int, failed bool) {
	elapsed := l.clock().Sub(start)
	if elapsed <= l.threshold {
		return
	}
	lg := l.logger
	if lg == nil {
		lg = slog.Default()
	}
	label, origin := statementCaller()
	attrs := []any{
		"duracion_ms", elapsed.Milliseconds(),
		"operacion", op,
		"consulta", label,
		"origen", origin,
		"sql_hash", sqlHash(query),
	}
	if rows >= 0 {
		attrs = append(attrs, "filas", rows)
	}
	if d, ok := StatementTimeout(ctx); ok {
		attrs = append(attrs, "tope_s", timeoutSeconds(d))
	}
	if failed {
		attrs = append(attrs, "fallo", true)
	}
	lg.WarnContext(ctx, slowQueryMsg, attrs...)
}

// sqlHash is a short, stable fingerprint of the statement text.
func sqlHash(query string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(query))
	return strconv.FormatUint(uint64(h.Sum32()), 16)
}

// callerSkipPrefixes are the function-name prefixes that never identify the
// code that issued a statement: they are the plumbing between it and us.
var callerSkipPrefixes = []string{
	"runtime.",
	"database/sql.",
	"github.com/XSAM/otelsql",
	"go.opentelemetry.io/",
}

// modulePrefix is trimmed from labels to keep them short.
const modulePrefix = "github.com/abdimuy/msp-api/internal/"

// statementCaller returns (label, origin): it walks the stack to the first frame outside the database
// plumbing and this package's non-test files. Only called for slow
// statements, so its cost never touches the fast path.
func statementCaller() (string, string) {
	pcs := make([]uintptr, 48)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if !isPlumbingFrame(f) {
			return strings.TrimPrefix(f.Function, modulePrefix),
				filepath.Base(filepath.Dir(f.File)) + "/" + filepath.Base(f.File) + ":" + strconv.Itoa(f.Line)
		}
		if !more {
			return "desconocido", ""
		}
	}
}

// isPlumbingFrame reports whether f belongs to database/sql, otelsql, the
// runtime or this package (its test files excepted, so white-box tests see
// themselves as the caller).
func isPlumbingFrame(f runtime.Frame) bool {
	for _, p := range callerSkipPrefixes {
		if strings.HasPrefix(f.Function, p) {
			return true
		}
	}
	dir := filepath.ToSlash(filepath.Dir(f.File))
	return strings.HasSuffix(dir, "internal/platform/firebird") && !strings.HasSuffix(f.File, "_test.go")
}

// timedRows wraps driver.Rows so a query is timed until its rows are closed —
// fetching is usually where a slow dashboard query spends its time.
type timedRows struct {
	base   driver.Rows
	log    *slowQueryLog
	ctx    context.Context //nolint:containedctx // carried only to log with the request's attributes on Close.
	query  string
	start  time.Time
	rows   int
	failed bool
	closed bool
}

// Compile-time proof that timedRows keeps the column metadata firebirdsqlRows
// exposes; dropping one would silently change sql.Rows.ColumnTypes().
var (
	_ driver.Rows                           = (*timedRows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*timedRows)(nil)
	_ driver.RowsColumnTypeLength           = (*timedRows)(nil)
	_ driver.RowsColumnTypeNullable         = (*timedRows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*timedRows)(nil)
	_ driver.RowsColumnTypeScanType         = (*timedRows)(nil)
)

// Columns implements driver.Rows.
func (r *timedRows) Columns() []string { return r.base.Columns() }

// Next implements driver.Rows, counting rows for the log line.
func (r *timedRows) Next(dest []driver.Value) error {
	err := r.base.Next(dest)
	if err == nil {
		r.rows++
	} else if !isEOF(err) {
		r.failed = true
	}
	return err
}

// Close implements driver.Rows and reports the statement when it was slow.
func (r *timedRows) Close() error {
	err := r.base.Close()
	if !r.closed {
		r.closed = true
		r.log.observe(r.ctx, "query", r.query, r.start, r.rows, r.failed || err != nil)
	}
	return err //nolint:wrapcheck // verbatim driver error.
}

// ColumnTypeDatabaseTypeName forwards when the base supports it.
func (r *timedRows) ColumnTypeDatabaseTypeName(index int) string {
	if c, ok := r.base.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return c.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

// ColumnTypeLength forwards when the base supports it.
func (r *timedRows) ColumnTypeLength(index int) (int64, bool) {
	if c, ok := r.base.(driver.RowsColumnTypeLength); ok {
		return c.ColumnTypeLength(index)
	}
	return 0, false
}

// ColumnTypeNullable forwards when the base supports it.
func (r *timedRows) ColumnTypeNullable(index int) (bool, bool) {
	if c, isC := r.base.(driver.RowsColumnTypeNullable); isC {
		return c.ColumnTypeNullable(index)
	}
	return false, false
}

// ColumnTypePrecisionScale forwards when the base supports it.
func (r *timedRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if c, isC := r.base.(driver.RowsColumnTypePrecisionScale); isC {
		return c.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

// ColumnTypeScanType forwards when the base supports it; otherwise it returns
// the same any type database/sql assumes for drivers without the interface.
func (r *timedRows) ColumnTypeScanType(index int) reflect.Type {
	if c, ok := r.base.(driver.RowsColumnTypeScanType); ok {
		return c.ColumnTypeScanType(index)
	}
	return reflect.TypeOf(new(any)).Elem()
}
