// Package ventfb hosts Firebird-backed implementations of the cobranza
// outbound ports. Spanish vocabulary (pago, zona, cargo, concepto) is used by
// project convention; misspell linting is silenced at the package level.
//
//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertions: PagosRepo satisfies both ports.
var (
	_ outbound.PagosRepo             = (*PagosRepo)(nil)
	_ outbound.PagosTombstoneCleaner = (*PagosRepo)(nil)
)

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
	var result []domain.Pago
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE DOCTO_CC_ACR_ID = ?
  AND CANCELADO = 'N'
ORDER BY FECHA`, doctoCCID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanPagoRows(rows)
		return serr
	})
	return result, err
}

// PorCliente returns every pago hecho por el cliente, ordered by FECHA
// descending.
func (r *PagosRepo) PorCliente(ctx context.Context, clienteID int) ([]domain.Pago, error) {
	var result []domain.Pago
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE CLIENTE_ID = ?
  AND CANCELADO = 'N'
ORDER BY FECHA DESC`, clienteID)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanPagoRows(rows)
		return serr
	})
	return result, err
}

// EnRutaPorZona returns pagos hechos en la zona con FECHA >= desde, ordered by
// FECHA descending. Pass desde=time.Time{} (zero value) to return all pagos
// for the zone.
func (r *PagosRepo) EnRutaPorZona(ctx context.Context, zonaID int, desde time.Time) ([]domain.Pago, error) {
	var result []domain.Pago
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		var (
			rows *sql.Rows
			qerr error
		)
		if desde.IsZero() {
			rows, qerr = q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE ZONA_CLIENTE_ID = ?
  AND CANCELADO = 'N'
ORDER BY FECHA DESC`, zonaID)
		} else {
			rows, qerr = q.QueryContext(ctx, `
SELECT `+selectPagoCols+`
FROM MSP_PAGOS_VENTAS
WHERE ZONA_CLIENTE_ID = ? AND FECHA >= ?
  AND CANCELADO = 'N'
ORDER BY FECHA DESC`, zonaID, firebird.ToWallClock(desde))
		}
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanPagoRows(rows)
		return serr
	})
	return result, err
}

// SyncPorZona returns a page of pagos for incremental sync. See port doc.
//
// Filtro de saldo dinámico (ver queryPagoSyncPage):
//   - cursor zero + desde zero: solo pagos de cargos con saldo activo.
//   - cursor zero + desde set:  + pagos cuyo p.FECHA >= desde (incluye el
//     pago final que saldó una venta).
//   - cursor set:               sin filtro de saldo; los pagos de ventas
//     recién saldadas viajan al cliente.
//
// El filtro de concepto IN (87327, 27969) — cobranza en ruta y abono
// mostrador — se mantiene en todos los modos para excluir conceptos
// internos del cache (155, 11, ...) que confundirían al cobrador.
//
// Nota: tombstones (CANCELADO='S') NO se filtran en el sync — se incluyen
// intencionalmente para que el cliente móvil reciba la señal de borrado y
// elimine la fila de su caché local.
func (r *PagosRepo) SyncPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde time.Time,
) (outbound.SyncPage[domain.Pago], error) {
	pageQuery := func(ctx context.Context, q firebird.Querier, upper time.Time, watermark int64) (*sql.Rows, error) {
		return queryPagoSyncPage(ctx, q, pagoSyncSpec{
			zonaID:     zonaID,
			cursor:     cursor,
			upperBound: upper,
			watermark:  watermark,
			afterID:    afterID,
			limit:      limit,
			desde:      desde,
		})
	}
	return runSyncPage[domain.Pago](ctx, r.pool, cursor, limit, pageQuery, scanEnrichedPagoRows)
}

// selectPagoColsP es la lista enriquecida que la app movil consume via
// /sync/pagos. Incluye campos resueltos desde DOCTOS_CC (descripcion como
// COBRADOR), CLIENTES (NOMBRE_CLIENTE) y FORMAS_COBRO_DOCTOS (FORMA_COBRO_ID)
// que el sistema Node legacy entregaba. La suma IMPORTE + IMPUESTO se hace
// en SELECT para que el cliente reciba el importe con IVA, alineado con la
// parcialidad que el cobrador realmente cobra.
//
// Las columnas s.* del JOIN con MSP_SALDOS_VENTAS no se exponen; solo se
// usan para filtrar `s.SALDO > 0` (ver queryPagoSyncPage).
const selectPagoColsP = `
	p.IMPTE_DOCTO_CC_ID,
	p.DOCTO_CC_ID,
	p.DOCTO_CC_ACR_ID,
	p.CLIENTE_ID,
	p.ZONA_CLIENTE_ID,
	p.FOLIO,
	p.CONCEPTO_CC_ID,
	p.FECHA,
	(p.IMPORTE + p.IMPUESTO),
	p.IMPUESTO,
	p.LAT,
	p.LON,
	p.CANCELADO,
	p.APLICADO,
	p.UPDATED_AT,
	COALESCE(dc.DESCRIPCION, ''),
	COALESCE(c.NOMBRE, ''),
	dc.COBRADOR_ID,
	fcd.FORMA_COBRO_ID`

// queryPagoSyncPage es la variante del helper generico con JOIN contra
// MSP_SALDOS_VENTAS para filtrar solo pagos de ventas activas. Misma
// semantica de cursor (>= con tie-break por pk) que el helper estandar.
// pagoFromClause arma el FROM completo: pagos cache + saldos cache para
// filtrar (solo activos), DOCTOS_CC del header del abono para DESCRIPCION
// y COBRADOR_ID, CLIENTES para NOMBRE_CLIENTE, FORMAS_COBRO_DOCTOS para
// FORMA_COBRO_ID. El sistema Node legacy hace estos JOINs.
//
// IMPORTANTE: DOCTOS_CC y CLIENTES van por LEFT JOIN para que tombstones
// (CANCELADO='S' en MSP_PAGOS_VENTAS) — y especialmente las filas cuyo
// DOCTO_CC padre fue borrado en Microsip — sigan apareciendo en el sync y en
// /by-ids. Con INNER JOIN un DELETE FROM DOCTOS_CC dejaba huérfana la fila
// del cache: el trigger marca CANCELADO='S' pero el JOIN excluye la row y
// el cliente móvil nunca recibe la señal de borrado. Los campos enriquecidos
// quedan NULL/” para tombstones, lo cual es correcto — el cliente sólo
// necesita el flag `cancelado` para borrar localmente.
//
// MSP_SALDOS_VENTAS sigue como INNER JOIN: el saldo padre nunca desaparece
// (cancelar el pago refunde el saldo, no lo borra), así que un fallo del
// JOIN ahí sí indica un cargo huérfano y debe excluirse.
//
// Filtro de concepto: el Node solo entrega pagos con CONCEPTO_CC_ID IN
// (87327, 27969) — cobranza en ruta y abono mostrador. El cache pre-incluye
// otros conceptos (155, 11, 27968...) que no son cobranza activa y
// confundirian al cobrador. Lo filtramos a nivel del query del sync.
const pagoFromClause = `
FROM MSP_PAGOS_VENTAS p
JOIN MSP_SALDOS_VENTAS s        ON s.DOCTO_CC_ID = p.DOCTO_CC_ACR_ID
LEFT JOIN DOCTOS_CC dc          ON dc.DOCTO_CC_ID = p.DOCTO_CC_ID
LEFT JOIN CLIENTES c            ON c.CLIENTE_ID = p.CLIENTE_ID
LEFT JOIN FORMAS_COBRO_DOCTOS fcd
       ON fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC' AND fcd.DOCTO_ID = p.DOCTO_CC_ID`

// pagoConceptoFilter excluye conceptos internos del cache que el cobrador
// no debe ver (155, 11, 27968…). Se mantiene en todos los modos.
const pagoConceptoFilter = `p.CONCEPTO_CC_ID IN (87327, 27969)`

// pagoSyncSpec parametriza el query de sync de pagos. desde acota el filtro
// de saldo en TODAS las páginas (no solo la primera): el cliente debe
// mandar el mismo `desde` en cada request para que las páginas 2+ no se
// cuelen pagos viejos de ventas saldadas históricas.
//
// watermark excluye filas escritas por transacciones aún en vuelo.
// Solo se devuelven filas con p.TX_ID < watermark (strict less-than; ver
// comment en syncPageSpec sobre el off-by-one crítico).
type pagoSyncSpec struct {
	zonaID     int
	cursor     time.Time
	upperBound time.Time
	// watermark: MinActiveTransactionID; solo filas con p.TX_ID < watermark
	// se incluyen en la respuesta.  SentinelNoActiveTx (math.MaxInt64) cuando
	// no hay transacciones activas — todas las filas committed pasan el filtro.
	watermark int64
	afterID   int
	limit     int
	desde     time.Time
}

func queryPagoSyncPage(ctx context.Context, q firebird.Querier, spec pagoSyncSpec) (*sql.Rows, error) {
	upper := firebird.ToWallClock(spec.upperBound)
	// Filtro de saldo dinámico según `desde` (independiente del cursor):
	//   - desde zero: solo pagos de cargos con SALDO > 0.
	//   - desde set:  + pagos con p.FECHA >= desde (incluye el pago final
	//                 que saldó una venta dentro de la ventana del cobrador).
	saldoFilter := `s.SALDO > 0`
	var statusArgs []any
	if !spec.desde.IsZero() {
		saldoFilter = `(s.SALDO > 0 OR p.FECHA >= ?)`
		statusArgs = []any{firebird.ToWallClock(spec.desde)}
	}
	if spec.cursor.IsZero() {
		// Positional order: limit, zonaID, upper, watermark, afterID, [desde?]
		args := append([]any{spec.limit, spec.zonaID, upper, spec.watermark, spec.afterID}, statusArgs...)
		query := `
SELECT FIRST ? ` + selectPagoColsP + pagoFromClause + `
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.UPDATED_AT <= ?
  AND p.TX_ID < ?
  AND p.IMPTE_DOCTO_CC_ID > ?
  AND ` + saldoFilter + `
  AND ` + pagoConceptoFilter + `
ORDER BY p.UPDATED_AT, p.IMPTE_DOCTO_CC_ID`
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		return rows, nil
	}
	cur := firebird.ToWallClock(spec.cursor)
	// Positional order: limit, zonaID, cur, upper, watermark, cur(x2), afterID, [desde?]
	args := append([]any{spec.limit, spec.zonaID, cur, upper, spec.watermark, cur, cur, spec.afterID}, statusArgs...)
	query := `
SELECT FIRST ? ` + selectPagoColsP + pagoFromClause + `
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.UPDATED_AT >= ?
  AND p.UPDATED_AT <= ?
  AND p.TX_ID < ?
  AND (p.UPDATED_AT > ? OR (p.UPDATED_AT = ? AND p.IMPTE_DOCTO_CC_ID > ?))
  AND ` + saldoFilter + `
  AND ` + pagoConceptoFilter + `
ORDER BY p.UPDATED_AT, p.IMPTE_DOCTO_CC_ID`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return rows, nil
}

// ByIDs returns the enriched Pago rows for the given primary keys, constrained
// to ZONA_CLIENTE_ID = zonaID. Uses selectPagoColsP + pagoFromClause for
// drift-free parity with SyncPorZona. No watermark filtering — the caller
// (by-ids HTTP endpoint) already obtained these PKs from the SSE listener
// which only publishes committed rows.
//
// Duplicate IDs in the input are deduplicated before querying. Rows whose
// PK is in ids but whose zona does not match are silently excluded.
//
//nolint:dupl // structurally mirrors VentasRepo.ByIDs; differs in column list + scanner + return type — abstraction not worth it
func (r *PagosRepo) ByIDs(ctx context.Context, zonaID int, ids []int) ([]domain.Pago, error) {
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
SELECT ` + selectPagoColsP + pagoFromClause + `
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.IMPTE_DOCTO_CC_ID IN (` + strings.Join(placeholders, ",") + `)`

	var result []domain.Pago
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		var serr error
		result, serr = scanEnrichedPagoRows(rows)
		return serr
	})
	return result, err
}

// DeleteTombstonesOlderThan deletes tombstones whose UPDATED_AT < cutoff and
// returns how many rows were removed. Mirrors
// SaldosRepo.DeleteTombstonesOlderThan: implements the cleanup half of the
// tombstone protocol introduced by migration 000019. The reconciler calls
// this on its weekly pass to keep the cache bounded.
func (r *PagosRepo) DeleteTombstonesOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	var n int64
	err := firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		res, eerr := q.ExecContext(
			ctx, `
DELETE FROM MSP_PAGOS_VENTAS
WHERE CANCELADO = 'S' AND UPDATED_AT < ?`,
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

// pagoEnrichedRowScan extiende pagoRowScan con los 4 campos resueltos via
// JOIN para el endpoint /sync/pagos (cobrador, cliente, forma_cobro).
type pagoEnrichedRowScan struct {
	pagoRowScan
	cobradorRaw      sql.NullString
	nombreClienteRaw sql.NullString
	cobradorIDRaw    sql.NullInt64
	formaCobroIDRaw  sql.NullInt64
}

func (p *pagoEnrichedRowScan) scanFrom(rows *sql.Rows) error {
	return rows.Scan(
		&p.impteID, &p.doctoCCID, &p.acrID, &p.clienteID,
		&p.zonaRaw, &p.folioRaw, &p.conceptoCCID,
		&p.fechaRaw, &p.importeRaw, &p.impuestoRaw,
		&p.latRaw, &p.lonRaw,
		&p.cancelado, &p.aplicado, &p.updatedAtRaw,
		&p.cobradorRaw, &p.nombreClienteRaw,
		&p.cobradorIDRaw, &p.formaCobroIDRaw,
	)
}

func (p *pagoEnrichedRowScan) hydrate() (domain.Pago, error) {
	base, err := p.pagoRowScan.hydrate()
	if err != nil {
		return domain.Pago{}, err
	}
	return domain.HydratePago(domain.HydratePagoParams{
		ImpteDoctoCCID: base.ImpteDoctoCCID(),
		DoctoCCID:      base.DoctoCCID(),
		DoctoCCAcrID:   base.DoctoCCAcrID(),
		ClienteID:      base.ClienteID(),
		ZonaClienteID:  base.ZonaClienteID(),
		Folio:          base.Folio(),
		ConceptoCCID:   base.ConceptoCCID(),
		Fecha:          base.Fecha(),
		Importe:        base.Importe(),
		Impuesto:       base.Impuesto(),
		Lat:            base.Lat(),
		Lon:            base.Lon(),
		Cancelado:      base.Cancelado(),
		Aplicado:       base.Aplicado(),
		UpdatedAt:      base.UpdatedAt(),
		Cobrador:       nullableString(p.cobradorRaw),
		CobradorID:     nullableInt(p.cobradorIDRaw),
		NombreCliente:  nullableString(p.nombreClienteRaw),
		FormaCobroID:   nullableInt(p.formaCobroIDRaw),
	}), nil
}

func scanEnrichedPagoRows(rows *sql.Rows) ([]domain.Pago, error) {
	var result []domain.Pago
	for rows.Next() {
		var rs pagoEnrichedRowScan
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
