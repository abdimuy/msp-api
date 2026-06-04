//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertions: SaldosRepo satisfies both ports.
var (
	_ outbound.SaldosRepo             = (*SaldosRepo)(nil)
	_ outbound.SaldosTombstoneCleaner = (*SaldosRepo)(nil)
)

// SaldosRepo implements outbound.SaldosRepo backed by the MSP_SALDOS_VENTAS
// materialized cache in Firebird. All reads are expected to hit PK or
// covering indexes for sub-10ms latency.
type SaldosRepo struct {
	pool *firebird.Pool
}

// NewSaldosRepo builds a SaldosRepo wired to the given pool.
func NewSaldosRepo(pool *firebird.Pool) *SaldosRepo {
	return &SaldosRepo{pool: pool}
}

// ─── SQL ─────────────────────────────────────────────────────────────────────

const selectSaldoCols = `
	DOCTO_CC_ID,
	DOCTO_PV_ID,
	CLIENTE_ID,
	ZONA_CLIENTE_ID,
	FOLIO,
	FECHA_CARGO,
	PRECIO_TOTAL,
	TOTAL_IMPORTE,
	IMPTE_REST,
	SALDO,
	NUM_PAGOS,
	FECHA_ULT_PAGO,
	CARGO_CANCELADO,
	UPDATED_AT`

const selectSaldoPorVenta = `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE DOCTO_PV_ID = ?`

const selectSaldoPorCargo = `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE DOCTO_CC_ID = ?`

const selectSaldosAbiertasPorCliente = `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE CLIENTE_ID = ? AND SALDO > 0
ORDER BY FECHA_CARGO DESC`

// ─── SaldosRepo methods ───────────────────────────────────────────────────────

// PorVenta returns the saldo for the given PV document ID.
// Returns ErrSaldoNoEncontrado when no cache row exists.
func (r *SaldosRepo) PorVenta(ctx context.Context, doctoPVID int) (*domain.Saldo, error) {
	var result *domain.Saldo
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, selectSaldoPorVenta, doctoPVID)
		scanned, serr := scanSaldo(row)
		if errors.Is(serr, sql.ErrNoRows) {
			return domain.ErrSaldoNoEncontrado
		}
		if serr != nil {
			return firebird.MapError(serr)
		}
		result = scanned
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PorCargo returns the saldo for the given cargo (DOCTOS_CC) ID.
// Returns ErrSaldoNoEncontrado when no cache row exists.
func (r *SaldosRepo) PorCargo(ctx context.Context, doctoCCID int) (*domain.Saldo, error) {
	var result *domain.Saldo
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, selectSaldoPorCargo, doctoCCID)
		scanned, serr := scanSaldo(row)
		if errors.Is(serr, sql.ErrNoRows) {
			return domain.ErrSaldoNoEncontrado
		}
		if serr != nil {
			return firebird.MapError(serr)
		}
		result = scanned
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// EnRutaPorZona returns ventas abiertas (saldo > 0) for the given zona, plus
// ventas saldadas whose FECHA_ULT_PAGO >= desde. Pass desde=time.Time{} (zero
// value) to suppress the UNION branch and return only open balances. desde is
// truncated to DATE precision by the underlying column type, so any HH:MM:SS
// component is ignored.
func (r *SaldosRepo) EnRutaPorZona(ctx context.Context, zonaID int, desde time.Time) ([]domain.Saldo, error) {
	var result []domain.Saldo
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		var (
			rows *sql.Rows
			qerr error
		)

		if desde.IsZero() {
			// Single branch: only abiertas. Uses IDX_MSP_SALDOS_ZONA_SALDO.
			query := `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ? AND SALDO > 0
ORDER BY FECHA_CARGO DESC`
			rows, qerr = q.QueryContext(ctx, query, zonaID)
		} else {
			// UNION: abiertas + recientemente pagadas. Each branch uses its own
			// covering index: IDX_..._ZONA_SALDO and IDX_..._ZONA_FUP.
			query := `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ? AND SALDO > 0
UNION
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ? AND SALDO <= 0
  AND FECHA_ULT_PAGO >= ?
ORDER BY FECHA_CARGO DESC`
			rows, qerr = q.QueryContext(ctx, query, zonaID, zonaID, desde)
		}
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanSaldoRows(rows)
		return serr
	})
	return result, err
}

// AbiertasPorCliente returns all open saldos (saldo > 0) for the given cliente.
func (r *SaldosRepo) AbiertasPorCliente(ctx context.Context, clienteID int) ([]domain.Saldo, error) {
	var result []domain.Saldo
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, selectSaldosAbiertasPorCliente, clienteID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanSaldoRows(rows)
		return serr
	})
	return result, err
}

// ResumenZonas returns an aggregated view of open saldos grouped by zona.
// Rows with NULL ZONA_CLIENTE_ID are skipped (unzoned clients cannot be on a route).
func (r *SaldosRepo) ResumenZonas(ctx context.Context) ([]domain.ResumenZona, error) {
	var result []domain.ResumenZona
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, `
SELECT ZONA_CLIENTE_ID, COUNT(*), SUM(SALDO)
FROM MSP_SALDOS_VENTAS
WHERE SALDO > 0
  AND ZONA_CLIENTE_ID IS NOT NULL
GROUP BY ZONA_CLIENTE_ID
ORDER BY ZONA_CLIENTE_ID`)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				zonaID      int
				totalVentas int
				saldoRaw    any
			)
			if serr := rows.Scan(&zonaID, &totalVentas, &saldoRaw); serr != nil {
				return firebird.MapError(serr)
			}
			saldo, serr := firebird.ScanDecimal(saldoRaw, 2)
			if serr != nil {
				return serr
			}
			result = append(result, domain.HydrateResumenZona(zonaID, totalVentas, saldo))
		}
		if serr := rows.Err(); serr != nil {
			return firebird.MapError(serr)
		}
		return nil
	})
	return result, err
}

// SyncPorZona returns a page of saldos for incremental sync. Tombstones are
// included so the client can propagate cancellations. See port doc.
func (r *SaldosRepo) SyncPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int,
) (outbound.SyncPage[domain.Saldo], error) {
	pageQuery := func(ctx context.Context, q firebird.Querier, upper time.Time, watermark int64) (*sql.Rows, error) {
		return querySyncPage(ctx, q, syncPageSpec{
			columns:    selectSaldoCols,
			table:      "MSP_SALDOS_VENTAS",
			pkColumn:   "DOCTO_CC_ID",
			zonaID:     zonaID,
			cursor:     cursor,
			upperBound: upper,
			watermark:  watermark,
			afterID:    afterID,
			limit:      limit,
		})
	}
	return runSyncPage[domain.Saldo](ctx, r.pool, cursor, limit, pageQuery, scanSaldoRows)
}

// ByIDs returns the Saldo rows for the given primary keys (DOCTO_CC_IDs),
// constrained to ZONA_CLIENTE_ID = zonaID. Rows whose PK is in ids but whose
// zona does not match are silently excluded (authorization filter, not 404).
// No watermark filtering — the caller (by-ids HTTP endpoint) obtained these
// PKs from the SSE listener which only publishes committed rows.
//
// Duplicate IDs in the input are deduplicated before querying.
func (r *SaldosRepo) ByIDs(ctx context.Context, zonaID int, ids []int) ([]domain.Saldo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// Dedup input IDs.
	seen := make(map[int]struct{}, len(ids))
	unique := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}

	// Build positional placeholders for IN clause.
	placeholders := make([]string, len(unique))
	args := make([]any, 0, len(unique)+1)
	args = append(args, zonaID)
	for i, id := range unique {
		placeholders[i] = "?"
		args = append(args, id)
	}

	query := `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND DOCTO_CC_ID IN (` + strings.Join(placeholders, ",") + `)`

	var result []domain.Saldo
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanSaldoRows(rows)
		return serr
	})
	return result, err
}

// DeleteTombstonesOlderThan deletes tombstones whose UPDATED_AT < cutoff and
// returns how many rows were removed. Implements
// outbound.SaldosTombstoneCleaner.
func (r *SaldosRepo) DeleteTombstonesOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	var n int64
	err := firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		res, eerr := q.ExecContext(ctx, `
DELETE FROM MSP_SALDOS_VENTAS
WHERE CARGO_CANCELADO = 'S' AND UPDATED_AT < ?`,
			firebird.ToWallClock(cutoff),
		)
		if eerr != nil {
			return firebird.MapError(eerr)
		}
		rows, rerr := res.RowsAffected()
		if rerr != nil {
			return firebird.MapError(rerr)
		}
		n = rows
		return nil
	})
	return int(n), err
}

// ─── scan helpers ─────────────────────────────────────────────────────────────

// saldoRowScan mirrors the SELECT list 1:1 in scan-friendly types. Splitting
// the raw scan from the type conversions keeps each step short enough that
// cyclomatic complexity stays under the linter thresholds without nolints.
type saldoRowScan struct {
	doctoCCID       int
	doctoPVIDRaw    sql.NullInt64
	clienteID       int
	zonaRaw         sql.NullInt64
	folio           sql.NullString
	fechaCargoRaw   any
	precioTotalRaw  any
	totalImporteRaw any
	impteRestRaw    any
	saldoRaw        any
	numPagos        int
	fechaUltRaw     any
	cargoCancelado  string
	updatedAtRaw    any
}

// scannable is the common surface of *sql.Row and *sql.Rows. Allows
// saldoRowScan.scanFrom to back both the single-row and the iterator path.
type scannable interface {
	Scan(dest ...any) error
}

func (s *saldoRowScan) scanFrom(r scannable) error {
	return r.Scan(
		&s.doctoCCID, &s.doctoPVIDRaw, &s.clienteID, &s.zonaRaw, &s.folio,
		&s.fechaCargoRaw, &s.precioTotalRaw, &s.totalImporteRaw, &s.impteRestRaw,
		&s.saldoRaw, &s.numPagos, &s.fechaUltRaw, &s.cargoCancelado, &s.updatedAtRaw,
	)
}

// hydrate converts the raw scan values into a domain.Saldo.
func (s *saldoRowScan) hydrate() (domain.Saldo, error) {
	amounts, err := s.scanDecimals()
	if err != nil {
		return domain.Saldo{}, err
	}
	timestamps, err := s.scanTimestamps()
	if err != nil {
		return domain.Saldo{}, err
	}
	return domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:      s.doctoCCID,
		DoctoPVID:      nullableInt(s.doctoPVIDRaw),
		ClienteID:      s.clienteID,
		ZonaClienteID:  nullableInt(s.zonaRaw),
		Folio:          nullableString(s.folio),
		FechaCargo:     timestamps.fechaCargo,
		PrecioTotal:    amounts.precioTotal,
		TotalImporte:   amounts.totalImporte,
		ImpteRest:      amounts.impteRest,
		Saldo:          amounts.saldo,
		NumPagos:       s.numPagos,
		FechaUltPago:   timestamps.fechaUltPago,
		CargoCancelado: s.cargoCancelado == "S",
		UpdatedAt:      timestamps.updatedAt,
	}), nil
}

// saldoAmounts holds the four parsed decimal columns of a saldo row.
type saldoAmounts struct {
	precioTotal  decimal.Decimal
	totalImporte decimal.Decimal
	impteRest    decimal.Decimal
	saldo        decimal.Decimal
}

func (s *saldoRowScan) scanDecimals() (saldoAmounts, error) {
	precio, err := firebird.ScanDecimal(s.precioTotalRaw, 2)
	if err != nil {
		return saldoAmounts{}, err
	}
	total, err := firebird.ScanDecimal(s.totalImporteRaw, 2)
	if err != nil {
		return saldoAmounts{}, err
	}
	rest, err := firebird.ScanDecimal(s.impteRestRaw, 2)
	if err != nil {
		return saldoAmounts{}, err
	}
	saldo, err := firebird.ScanDecimal(s.saldoRaw, 2)
	if err != nil {
		return saldoAmounts{}, err
	}
	return saldoAmounts{precio, total, rest, saldo}, nil
}

// saldoTimestamps holds the three parsed timestamp columns. fechaUltPago is
// optional (nil when the column was SQL NULL).
type saldoTimestamps struct {
	fechaCargo   time.Time
	fechaUltPago *time.Time
	updatedAt    time.Time
}

func (s *saldoRowScan) scanTimestamps() (saldoTimestamps, error) {
	fechaCargo, err := firebird.ScanUTCTime(s.fechaCargoRaw)
	if err != nil {
		return saldoTimestamps{}, err
	}
	updatedAt, err := firebird.ScanUTCTime(s.updatedAtRaw)
	if err != nil {
		return saldoTimestamps{}, err
	}
	ts := saldoTimestamps{fechaCargo: fechaCargo, updatedAt: updatedAt}
	if s.fechaUltRaw != nil {
		t, err := firebird.ScanUTCTime(s.fechaUltRaw)
		if err != nil {
			return saldoTimestamps{}, err
		}
		ts.fechaUltPago = &t
	}
	return ts, nil
}

// scanSaldo scans one MSP_SALDOS_VENTAS row into a domain.Saldo.
func scanSaldo(row *sql.Row) (*domain.Saldo, error) {
	var rs saldoRowScan
	if err := rs.scanFrom(row); err != nil {
		return nil, err
	}
	s, err := rs.hydrate()
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// scanSaldoRows iterates a *sql.Rows, scanning each into a domain.Saldo slice.
func scanSaldoRows(rows *sql.Rows) ([]domain.Saldo, error) {
	var result []domain.Saldo
	for rows.Next() {
		var rs saldoRowScan
		if err := rs.scanFrom(rows); err != nil {
			return nil, firebird.MapError(err)
		}
		s, err := rs.hydrate()
		if err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}
