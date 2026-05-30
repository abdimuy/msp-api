//nolint:misspell // Spanish vocabulary (saldo, zona, cargo, cobranza, etc.) by convention.
package ventfb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: SaldosRepo satisfies the outbound port.
var _ outbound.SaldosRepo = (*SaldosRepo)(nil)

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
	q := firebird.GetQuerier(ctx, r.pool.DB)
	row := q.QueryRowContext(ctx, selectSaldoPorVenta, doctoPVID)
	s, err := scanSaldo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSaldoNoEncontrado
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return s, nil
}

// PorCargo returns the saldo for the given cargo (DOCTOS_CC) ID.
// Returns ErrSaldoNoEncontrado when no cache row exists.
func (r *SaldosRepo) PorCargo(ctx context.Context, doctoCCID int) (*domain.Saldo, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	row := q.QueryRowContext(ctx, selectSaldoPorCargo, doctoCCID)
	s, err := scanSaldo(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrSaldoNoEncontrado
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return s, nil
}

// EnRutaPorZona returns ventas abiertas (saldo > 0) and recently paid
// (saldo <= 0, fecha_ult_pago within ventanaDias days) for the given zona.
// When ventanaDias == 0, only abiertas are returned (no UNION branch).
func (r *SaldosRepo) EnRutaPorZona(ctx context.Context, zonaID, ventanaDias int) ([]domain.Saldo, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	var (
		rows *sql.Rows
		err  error
	)

	if ventanaDias == 0 {
		// Simplified: only open (positive balance) rows for the zona.
		query := `
SELECT ` + selectSaldoCols + `
FROM MSP_SALDOS_VENTAS
WHERE ZONA_CLIENTE_ID = ? AND SALDO > 0
ORDER BY FECHA_CARGO DESC`
		rows, err = q.QueryContext(ctx, query, zonaID)
	} else {
		// UNION: open rows + recently-paid rows within the window.
		desde := time.Now().UTC().AddDate(0, 0, -ventanaDias)
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
		rows, err = q.QueryContext(ctx, query, zonaID, zonaID, desde)
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	return scanSaldoRows(rows)
}

// AbiertasPorCliente returns all open saldos (saldo > 0) for the given cliente.
func (r *SaldosRepo) AbiertasPorCliente(ctx context.Context, clienteID int) ([]domain.Saldo, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, selectSaldosAbiertasPorCliente, clienteID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanSaldoRows(rows)
}

// ResumenZonas returns an aggregated view of open saldos grouped by zona.
// Rows with NULL ZONA_CLIENTE_ID are skipped (unzoned clients cannot be on a route).
func (r *SaldosRepo) ResumenZonas(ctx context.Context) ([]domain.ResumenZona, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, `
SELECT ZONA_CLIENTE_ID, COUNT(*), SUM(SALDO)
FROM MSP_SALDOS_VENTAS
WHERE SALDO > 0
  AND ZONA_CLIENTE_ID IS NOT NULL
GROUP BY ZONA_CLIENTE_ID
ORDER BY ZONA_CLIENTE_ID`)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var result []domain.ResumenZona
	for rows.Next() {
		var (
			zonaID      int
			totalVentas int
			saldoRaw    any
		)
		if err := rows.Scan(&zonaID, &totalVentas, &saldoRaw); err != nil {
			return nil, firebird.MapError(err)
		}
		saldo, err := firebird.ScanDecimal(saldoRaw, 2)
		if err != nil {
			return nil, err
		}
		result = append(result, domain.HydrateResumenZona(zonaID, totalVentas, saldo))
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

// ─── scan helpers ─────────────────────────────────────────────────────────────

// scanSaldo scans one MSP_SALDOS_VENTAS row into a domain.Saldo.
func scanSaldo(row *sql.Row) (*domain.Saldo, error) {
	var (
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
	)
	if err := row.Scan(
		&doctoCCID,
		&doctoPVIDRaw,
		&clienteID,
		&zonaRaw,
		&folio,
		&fechaCargoRaw,
		&precioTotalRaw,
		&totalImporteRaw,
		&impteRestRaw,
		&saldoRaw,
		&numPagos,
		&fechaUltRaw,
		&cargoCancelado,
		&updatedAtRaw,
	); err != nil {
		return nil, err
	}
	return hydrateSaldo(
		doctoCCID, doctoPVIDRaw, clienteID, zonaRaw, folio,
		fechaCargoRaw, precioTotalRaw, totalImporteRaw, impteRestRaw,
		saldoRaw, numPagos, fechaUltRaw, cargoCancelado, updatedAtRaw,
	)
}

// scanSaldoRow scans one row from a *sql.Rows iterator into a domain.Saldo.
func scanSaldoRow(rows *sql.Rows) (*domain.Saldo, error) {
	var (
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
	)
	if err := rows.Scan(
		&doctoCCID,
		&doctoPVIDRaw,
		&clienteID,
		&zonaRaw,
		&folio,
		&fechaCargoRaw,
		&precioTotalRaw,
		&totalImporteRaw,
		&impteRestRaw,
		&saldoRaw,
		&numPagos,
		&fechaUltRaw,
		&cargoCancelado,
		&updatedAtRaw,
	); err != nil {
		return nil, err
	}
	return hydrateSaldo(
		doctoCCID, doctoPVIDRaw, clienteID, zonaRaw, folio,
		fechaCargoRaw, precioTotalRaw, totalImporteRaw, impteRestRaw,
		saldoRaw, numPagos, fechaUltRaw, cargoCancelado, updatedAtRaw,
	)
}

// scanSaldoRows iterates a *sql.Rows, scanning each into a domain.Saldo slice.
func scanSaldoRows(rows *sql.Rows) ([]domain.Saldo, error) {
	var result []domain.Saldo
	for rows.Next() {
		s, err := scanSaldoRow(rows)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		result = append(result, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

// hydrateSaldo converts raw scanned values into a domain.Saldo.
//
//nolint:cyclop,funlen // scan conversions are mechanical; one per column; extracted for reuse.
func hydrateSaldo(
	doctoCCID int,
	doctoPVIDRaw sql.NullInt64,
	clienteID int,
	zonaRaw sql.NullInt64,
	folio sql.NullString,
	fechaCargoRaw any,
	precioTotalRaw any,
	totalImporteRaw any,
	impteRestRaw any,
	saldoRaw any,
	numPagos int,
	fechaUltRaw any,
	cargoCancelado string,
	updatedAtRaw any,
) (*domain.Saldo, error) {
	var doctoPVID *int
	if doctoPVIDRaw.Valid {
		v := int(doctoPVIDRaw.Int64)
		doctoPVID = &v
	}

	var zonaID *int
	if zonaRaw.Valid {
		v := int(zonaRaw.Int64)
		zonaID = &v
	}

	folioStr := ""
	if folio.Valid {
		folioStr = folio.String
	}

	fechaCargo, err := firebird.ScanUTCTime(fechaCargoRaw)
	if err != nil {
		return nil, err
	}

	precioTotal, err := firebird.ScanDecimal(precioTotalRaw, 2)
	if err != nil {
		return nil, err
	}

	totalImporte, err := firebird.ScanDecimal(totalImporteRaw, 2)
	if err != nil {
		return nil, err
	}

	impteRest, err := firebird.ScanDecimal(impteRestRaw, 2)
	if err != nil {
		return nil, err
	}

	saldo, err := firebird.ScanDecimal(saldoRaw, 2)
	if err != nil {
		return nil, err
	}

	var fechaUltPago *time.Time
	if fechaUltRaw != nil {
		t, scanErr := firebird.ScanUTCTime(fechaUltRaw)
		if scanErr != nil {
			return nil, scanErr
		}
		fechaUltPago = &t
	}

	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}

	s := domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:      doctoCCID,
		DoctoPVID:      doctoPVID,
		ClienteID:      clienteID,
		ZonaClienteID:  zonaID,
		Folio:          folioStr,
		FechaCargo:     fechaCargo,
		PrecioTotal:    precioTotal,
		TotalImporte:   totalImporte,
		ImpteRest:      impteRest,
		Saldo:          saldo,
		NumPagos:       numPagos,
		FechaUltPago:   fechaUltPago,
		CargoCancelado: cargoCancelado == "S",
		UpdatedAt:      updatedAt,
	})
	return &s, nil
}
