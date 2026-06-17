// Command analytics-export-creditdata dumps the RAW cargo, abono, and venta
// streams needed by the offline analytics harnesses (analysis/creditscorecard,
// analysis/recompra). It intentionally exports raw rows with NO temporal logic —
// all point-in-time reconstruction (as-of-date saldo, behaviour, delinquency
// label, BTYD calibration) lives in the Python harnesses so the methodology is
// in one tested place.
//
// Three CSVs are written to --dir:
//   - cargos.csv: one row per credit cargo (DOCTO_CC) — start date + total.
//   - abonos.csv: one row per payment-ledger movement (DOCTO_CC payments),
//     including concept (abono vs castigo vs condonación) and GPS.
//   - ventas.csv: one row per Microsip sale header (DOCTOS_PV) — client, date,
//     net amount, doc type (V/P); the transactional substrate for the
//     recompra/CLV BTYD harness (TIPO_DOCTO lets it segment sale occasions).
//
// Read-only. Dev/ops tooling (mirrors cmd/seed-cobrador). Uses the pure-Go
// Firebird driver, so it needs no native fbclient (unlike the Python driver).
//
// Usage:
//
//	source .env && go run ./cmd/analytics-export-creditdata --dir analysis/creditscorecard/.data
package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

const cargosSQL = `
SELECT DOCTO_CC_ID, CLIENTE_ID, ZONA_CLIENTE_ID, FECHA_CARGO,
       PRECIO_TOTAL, NUM_PAGOS, CARGO_CANCELADO
FROM MSP_SALDOS_VENTAS`

const abonosSQL = `
SELECT DOCTO_CC_ID, CLIENTE_ID, CONCEPTO_CC_ID, FECHA, IMPORTE,
       LAT, LON, CANCELADO, APLICADO
FROM MSP_PAGOS_VENTAS`

const ventasSQL = `
SELECT CLIENTE_ID, FECHA, IMPORTE_NETO, TIPO_DOCTO
FROM DOCTOS_PV
WHERE TIPO_DOCTO IN ('V', 'P') AND ESTATUS = 'N'`

func main() {
	dir := flag.String("dir", "analysis/creditscorecard/.data", "output directory for the CSVs")
	flag.Parse()

	ctx := context.Background()

	cfg, err := config.Load()
	must(err)

	pool, err := firebird.New(cfg.Firebird)
	must(err)
	must(pool.Start(ctx))
	defer func() { _ = pool.Stop(ctx) }()

	must(os.MkdirAll(*dir, 0o750))

	nCargos := exportCargos(ctx, pool, filepath.Join(*dir, "cargos.csv"))
	_, _ = fmt.Printf("✔ cargos.csv: %d rows\n", nCargos)

	nAbonos := exportAbonos(ctx, pool, filepath.Join(*dir, "abonos.csv"))
	_, _ = fmt.Printf("✔ abonos.csv: %d rows\n", nAbonos)

	nVentas := exportVentas(ctx, pool, filepath.Join(*dir, "ventas.csv"))
	_, _ = fmt.Printf("✔ ventas.csv: %d rows\n", nVentas)
}

func exportCargos(ctx context.Context, pool *firebird.Pool, path string) int {
	rows, err := pool.QueryContext(ctx, cargosSQL)
	must(err)
	defer func() { _ = rows.Close() }()

	w, closeW := newCSV(path)
	defer closeW()
	must(w.Write([]string{
		"DOCTO_CC_ID", "CLIENTE_ID", "ZONA_CLIENTE_ID", "FECHA_CARGO",
		"PRECIO_TOTAL", "NUM_PAGOS", "CARGO_CANCELADO",
	}))

	n := 0
	for rows.Next() {
		var (
			doctoCC, clienteID, numPagos int
			zona                         sql.NullInt64
			fechaCargo                   time.Time
			precioTotal                  string
			cancelado                    string
		)
		must(rows.Scan(&doctoCC, &clienteID, &zona, &fechaCargo, &precioTotal, &numPagos, &cancelado))
		must(w.Write([]string{
			itoa(doctoCC), itoa(clienteID), nullInt(zona), fmtDate(fechaCargo),
			precioTotal, itoa(numPagos), cancelado,
		}))
		n++
	}
	must(rows.Err())
	w.Flush()
	must(w.Error())
	return n
}

func exportAbonos(ctx context.Context, pool *firebird.Pool, path string) int {
	rows, err := pool.QueryContext(ctx, abonosSQL)
	must(err)
	defer func() { _ = rows.Close() }()

	w, closeW := newCSV(path)
	defer closeW()
	must(w.Write([]string{
		"DOCTO_CC_ID", "CLIENTE_ID", "CONCEPTO_CC_ID", "FECHA", "IMPORTE",
		"LAT", "LON", "CANCELADO", "APLICADO",
	}))

	n := 0
	for rows.Next() {
		var (
			doctoCC, clienteID, concepto int
			fecha                        time.Time
			importe                      string
			lat, lon                     sql.NullString
			cancelado, aplicado          string
		)
		must(rows.Scan(&doctoCC, &clienteID, &concepto, &fecha, &importe, &lat, &lon, &cancelado, &aplicado))
		must(w.Write([]string{
			itoa(doctoCC), itoa(clienteID), itoa(concepto), fmtTime(fecha), importe,
			nullStr(lat), nullStr(lon), cancelado, aplicado,
		}))
		n++
	}
	must(rows.Err())
	w.Flush()
	must(w.Error())
	return n
}

func exportVentas(ctx context.Context, pool *firebird.Pool, path string) int {
	rows, err := pool.QueryContext(ctx, ventasSQL)
	must(err)
	defer func() { _ = rows.Close() }()

	w, closeW := newCSV(path)
	defer closeW()
	must(w.Write([]string{"CLIENTE_ID", "FECHA", "IMPORTE_NETO", "TIPO_DOCTO"}))

	n := 0
	for rows.Next() {
		var (
			clienteID   int
			fecha       time.Time
			importeNeto string
			tipoDocto   string
		)
		must(rows.Scan(&clienteID, &fecha, &importeNeto, &tipoDocto))
		must(w.Write([]string{itoa(clienteID), fmtDate(fecha), importeNeto, tipoDocto}))
		n++
	}
	must(rows.Err())
	w.Flush()
	must(w.Error())
	return n
}

func newCSV(path string) (*csv.Writer, func()) {
	f, err := os.Create(path) //nolint:gosec // dev tool, operator-provided path
	must(err)
	w := csv.NewWriter(f)
	return w, func() { _ = f.Close() }
}

func itoa(n int) string { return strconv.Itoa(n) }

func nullInt(v sql.NullInt64) string {
	if !v.Valid {
		return ""
	}
	return strconv.FormatInt(v.Int64, 10)
}

func nullStr(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02")
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func must(err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "✘ %v\n", err)
		os.Exit(1)
	}
}
