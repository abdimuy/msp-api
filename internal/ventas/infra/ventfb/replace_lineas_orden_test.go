//nolint:misspell // Spanish vocabulary (productos, combos) by convention.
package ventfb_test

// replace_lineas_orden_test.go pins the STATEMENT ORDER of
// VentaRepo.ReplaceLineas without a database.
//
// Why it exists: the order is load-bearing —
// FK_MSP_VENTAS_PRODUCTOS_COMBO forces delete-productos → delete-combos →
// insert-combos → insert-productos — but the only tests that could detect a
// reordering were the Firebird e2e ones, and CI deliberately runs without
// FB_DATABASE (CLAUDE.md §7). Measured: swapping either pair leaves
// `go test -short ./internal/ventas/...` completely green while the real
// write dies with a foreign-key violation in production.
//
// The repo talks to a *sql.DB, so the seam is the driver: a
// driver.Connector backed by a connection that records every ExecContext.
// Same technique already used by internal/ventas/app/service_runintx_test.go.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/ventfb"
)

// ─── recording driver ───────────────────────────────────────────────────────

// recordedExec is one ExecContext the repo issued.
type recordedExec struct {
	query string
	args  []driver.NamedValue
}

// execLog is the shared, mutex-guarded tape every connection writes to.
type execLog struct {
	mu    sync.Mutex
	execs []recordedExec
}

func (l *execLog) add(e recordedExec) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.execs = append(l.execs, e)
}

func (l *execLog) snapshot() []recordedExec {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]recordedExec(nil), l.execs...)
}

// recordingConnector hands out connections that all write to the same tape.
type recordingConnector struct{ log *execLog }

func (c recordingConnector) Connect(_ context.Context) (driver.Conn, error) {
	return &recordingConn{log: c.log}, nil
}
func (c recordingConnector) Driver() driver.Driver { return recordingDriver{} }

// recordingDriver is the unused fallback — sql.OpenDB takes the Connector path.
type recordingDriver struct{}

func (recordingDriver) Open(string) (driver.Conn, error) { panic("unreachable") }

// recordingConn records ExecContext and answers "1 row affected" so
// ensureRowAffected does not turn the header UPDATE into ErrVentaNotFound.
type recordingConn struct{ log *execLog }

func (c *recordingConn) ExecContext(
	_ context.Context, query string, args []driver.NamedValue,
) (driver.Result, error) {
	c.log.add(recordedExec{query: query, args: append([]driver.NamedValue(nil), args...)})
	return oneRowResult{}, nil
}

// CheckNamedValue accepts every argument verbatim. The repo binds
// decimal.Decimal, time.Time and untyped nils; converting them here would
// only add a way for this fake to fail for reasons unrelated to the order.
func (c *recordingConn) CheckNamedValue(_ *driver.NamedValue) error { return nil }

func (c *recordingConn) Prepare(string) (driver.Stmt, error) {
	return nil, driver.ErrSkip
}
func (c *recordingConn) Close() error              { return nil }
func (c *recordingConn) Begin() (driver.Tx, error) { return nil, driver.ErrSkip }

// oneRowResult reports a single affected row.
type oneRowResult struct{}

func (oneRowResult) LastInsertId() (int64, error) { return 0, nil }
func (oneRowResult) RowsAffected() (int64, error) { return 1, nil }

// newRecordingRepo builds a VentaRepo whose pool is backed by the recording
// driver, plus the tape it writes to.
func newRecordingRepo(t *testing.T) (*ventfb.VentaRepo, *execLog) {
	t.Helper()
	log := &execLog{}
	db := sql.OpenDB(recordingConnector{log: log})
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return ventfb.NewVentaRepo(&firebird.Pool{DB: db}), log
}

// clasificar reduces a statement to the token this test asserts on.
func clasificar(q string) string {
	flat := strings.Join(strings.Fields(q), " ")
	switch {
	case strings.HasPrefix(flat, "DELETE FROM MSP_VENTAS_PRODUCTOS"):
		return "delete-productos"
	case strings.HasPrefix(flat, "DELETE FROM MSP_VENTAS_COMBOS"):
		return "delete-combos"
	case strings.HasPrefix(flat, "INSERT INTO MSP_VENTAS_COMBOS"):
		return "insert-combo"
	case strings.HasPrefix(flat, "INSERT INTO MSP_VENTAS_PRODUCTOS"):
		return "insert-producto"
	case strings.HasPrefix(flat, "UPDATE MSP_VENTAS"):
		return "touch-header"
	}
	return "OTRO: " + flat
}

// ─── the test ───────────────────────────────────────────────────────────────

// TestReplaceLineas_OrdenDeSentencias pins the exact statement sequence,
// including the case that mixes a combo, its child and a stand-alone
// producto: the stand-alone carries a NULL COMBO_ID and rides along in the
// same insert-productos pass, after the combos are back on disk.
//
// Any reordering of the four line-item statements fails here — with no
// Firebird, so CI catches it.
func TestReplaceLineas_OrdenDeSentencias(t *testing.T) {
	t.Parallel()
	repo, log := newRecordingRepo(t)

	userID := uuid.New()
	comboID := uuid.New()
	venta := buildVentaConCombo(t, userID, comboID, 101, 19, decimal.NewFromInt(1))

	nuevoComboID := uuid.New()
	comboPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("3500.00"),
		decimal.RequireFromString("3200.00"),
		decimal.RequireFromString("2900.00"),
	)
	require.NoError(t, err)
	lineaPrecios, err := domain.NewMontoSnapshot(
		decimal.RequireFromString("1200.00"),
		decimal.RequireFromString("1100.00"),
		decimal.RequireFromString("1000.00"),
	)
	require.NoError(t, err)
	origen, destino := 19, 11058
	require.NoError(t, venta.ReemplazarLineas(domain.ReemplazarLineasParams{
		Combos: []domain.CrearVentaComboInput{{
			ID: nuevoComboID, Nombre: "Combo Nuevo", Precios: comboPrecios,
			Cantidad: decimal.NewFromInt(1), AlmacenOrigen: origen, AlmacenDestino: destino,
		}},
		Productos: []domain.CrearVentaProductoInput{
			{
				ID: uuid.New(), ArticuloID: 101, Articulo: "Hijo del combo",
				Cantidad: decimal.NewFromInt(3), Precios: lineaPrecios, ComboID: &nuevoComboID,
			},
			{
				ID: uuid.New(), ArticuloID: 202, Articulo: "Producto suelto",
				Cantidad: decimal.NewFromInt(1), Precios: lineaPrecios,
				AlmacenOrigen: &origen, AlmacenDestino: &destino,
			},
		},
		By: userID, Now: time.Now().UTC(),
	}))

	require.NoError(t, repo.ReplaceLineas(t.Context(), venta))

	execs := log.snapshot()
	got := make([]string, 0, len(execs))
	for _, e := range execs {
		got = append(got, clasificar(e.query))
	}
	assert.Equal(t, []string{
		"delete-productos", // must precede delete-combos: the rows on disk still
		"delete-combos",    // reference the combos about to be deleted (FK).
		"insert-combo",     // must precede insert-producto: the new productos
		"insert-producto",  // reference a combo that has to exist first.
		"insert-producto",
		"touch-header",
	}, got, "el orden de ReplaceLineas es la restricción de la FK, no un detalle")

	// The stand-alone producto rides in the same pass with a NULL COMBO_ID —
	// it is never split out, and it is never inserted before the combos.
	var comboIDs []any
	for _, e := range execs {
		if clasificar(e.query) != "insert-producto" {
			continue
		}
		// COMBO_ID is the 9th bound parameter of insertProducto.
		comboIDs = append(comboIDs, e.args[8].Value)
	}
	require.Len(t, comboIDs, 2)
	assert.Equal(t, nuevoComboID.String(), comboIDs[0], "el hijo apunta al combo nuevo")
	assert.Nil(t, comboIDs[1], "el producto suelto va con COMBO_ID nulo")
}
