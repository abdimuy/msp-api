// Package ventfb hosts Firebird-backed implementations of the cobranza
// outbound ports. Spanish vocabulary (pago, zona, cargo, concepto) is used by
// project convention; misspell linting is silenced at the package level.
//
//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import (
	"context"
	"database/sql"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: PagosRepo satisfies the outbound port.
var _ outbound.PagosRepo = (*PagosRepo)(nil)

// PagosRepo implements outbound.PagosRepo backed by the MSP_PAGOS_VENTAS
// materialized cache in Firebird. Reads hit covering indexes for sub-30ms
// latency.
type PagosRepo struct {
	pool *firebird.Pool
}

// NewPagosRepo builds a PagosRepo wired to the given pool.
func NewPagosRepo(pool *firebird.Pool) *PagosRepo {
	return &PagosRepo{pool: pool}
}

// ─── SQL ─────────────────────────────────────────────────────────────────────

const selectPagoCols = `
	IMPTE_DOCTO_CC_ID,
	DOCTO_CC_ID,
	DOCTO_CC_ACR_ID,
	CLIENTE_ID,
	ZONA_CLIENTE_ID,
	FOLIO,
	CONCEPTO_CC_ID,
	FECHA,
	IMPORTE,
	IMPUESTO,
	LAT,
	LON,
	CANCELADO,
	APLICADO,
	UPDATED_AT`

// PorVenta returns every pago acreditado al cargo doctoCCID, ordered by FECHA
// ascending.
func (r *PagosRepo) PorVenta(ctx context.Context, doctoCCID int) ([]domain.Pago, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE DOCTO_CC_ACR_ID = ?
ORDER BY FECHA`, doctoCCID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanPagoRows(rows)
}

// PorCliente returns every pago hecho por el cliente, ordered by FECHA
// descending.
func (r *PagosRepo) PorCliente(ctx context.Context, clienteID int) ([]domain.Pago, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE CLIENTE_ID = ?
ORDER BY FECHA DESC`, clienteID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanPagoRows(rows)
}

// EnRutaPorZona returns pagos hechos en la zona con FECHA >= desde, ordered by
// FECHA descending. Pass desde=time.Time{} (zero value) to return all pagos
// for the zone.
func (r *PagosRepo) EnRutaPorZona(ctx context.Context, zonaID int, desde time.Time) ([]domain.Pago, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	var (
		rows *sql.Rows
		err  error
	)
	if desde.IsZero() {
		rows, err = q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
ORDER BY FECHA DESC`, zonaID)
	} else {
		rows, err = q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE ZONA_CLIENTE_ID = ? AND FECHA >= ?
ORDER BY FECHA DESC`, zonaID, firebird.ToWallClock(desde))
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanPagoRows(rows)
}

// SyncPorZona returns a page of pagos for incremental sync. See port doc.
func (r *PagosRepo) SyncPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int,
) (outbound.SyncPage[domain.Pago], error) {
	pageQuery := func(ctx context.Context, q firebird.Querier, upper time.Time) (*sql.Rows, error) {
		return querySyncPage(ctx, q, syncPageSpec{
			columns:    selectPagoCols,
			table:      "MSP_PAGOS_VENTAS",
			pkColumn:   "IMPTE_DOCTO_CC_ID",
			zonaID:     zonaID,
			cursor:     cursor,
			upperBound: upper,
			afterID:    afterID,
			limit:      limit,
		})
	}
	return runSyncPage[domain.Pago](ctx, r.pool, cursor, limit, pageQuery, scanPagoRows)
}

// ─── scan helpers ─────────────────────────────────────────────────────────────

// pagoRowScan mirrors the SELECT list 1:1 in scan-friendly types. Keeping the
// raw scan separate from the type conversions keeps each step short enough
// that cyclomatic complexity stays under linter thresholds without nolints.
type pagoRowScan struct {
	impteID      int
	doctoCCID    int
	acrID        int
	clienteID    int
	zonaRaw      sql.NullInt64
	folioRaw     sql.NullString
	conceptoCCID int
	fechaRaw     any
	importeRaw   any
	impuestoRaw  any
	latRaw       any
	lonRaw       any
	cancelado    string
	aplicado     string
	updatedAtRaw any
}

func (p *pagoRowScan) scanFrom(rows *sql.Rows) error {
	return rows.Scan(
		&p.impteID, &p.doctoCCID, &p.acrID, &p.clienteID,
		&p.zonaRaw, &p.folioRaw, &p.conceptoCCID,
		&p.fechaRaw, &p.importeRaw, &p.impuestoRaw,
		&p.latRaw, &p.lonRaw,
		&p.cancelado, &p.aplicado, &p.updatedAtRaw,
	)
}

// hydrate converts the raw scan values into a domain.Pago. The function is a
// linear sequence of Scan* calls; no branching beyond NULL checks on the
// nullable columns.
func (p *pagoRowScan) hydrate() (domain.Pago, error) {
	fecha, err := firebird.ScanUTCTime(p.fechaRaw)
	if err != nil {
		return domain.Pago{}, err
	}
	importe, err := firebird.ScanDecimal(p.importeRaw, 2)
	if err != nil {
		return domain.Pago{}, err
	}
	impuesto, err := firebird.ScanDecimal(p.impuestoRaw, 2)
	if err != nil {
		return domain.Pago{}, err
	}
	updatedAt, err := firebird.ScanUTCTime(p.updatedAtRaw)
	if err != nil {
		return domain.Pago{}, err
	}
	lat, err := scanNullableDecimal(p.latRaw, 8)
	if err != nil {
		return domain.Pago{}, err
	}
	lon, err := scanNullableDecimal(p.lonRaw, 8)
	if err != nil {
		return domain.Pago{}, err
	}

	return domain.HydratePago(domain.HydratePagoParams{
		ImpteDoctoCCID: p.impteID,
		DoctoCCID:      p.doctoCCID,
		DoctoCCAcrID:   p.acrID,
		ClienteID:      p.clienteID,
		ZonaClienteID:  nullableInt(p.zonaRaw),
		Folio:          nullableString(p.folioRaw),
		ConceptoCCID:   p.conceptoCCID,
		Fecha:          fecha,
		Importe:        importe,
		Impuesto:       impuesto,
		Lat:            lat,
		Lon:            lon,
		Cancelado:      p.cancelado == "S",
		Aplicado:       p.aplicado == "S",
		UpdatedAt:      updatedAt,
	}), nil
}

func scanPagoRows(rows *sql.Rows) ([]domain.Pago, error) {
	var result []domain.Pago
	for rows.Next() {
		var rs pagoRowScan
		if err := rs.scanFrom(rows); err != nil {
			return nil, firebird.MapError(err)
		}
		p, err := rs.hydrate()
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}

// scanNullableDecimal returns nil when raw is the SQL NULL marker; otherwise
// it delegates to firebird.ScanDecimal. A nil *decimal.Decimal IS the value
// here — it encodes "this column was NULL" — so returning (nil, nil) is the
// correct signature, but err113's nilnil rule disagrees. Wrap the contract in
// a helper named for what it does so the call sites read plainly.
func scanNullableDecimal(raw any, scale int) (*decimal.Decimal, error) {
	if raw == nil {
		return nil, nil //nolint:nilnil // nil = SQL NULL; see helper doc.
	}
	d, err := firebird.ScanDecimal(raw, scale)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func nullableInt(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	i := int(v.Int64)
	return &i
}

func nullableString(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return v.String
}
