// Package ventfb hosts Firebird-backed implementations of the cobranza
// outbound ports. Spanish vocabulary (pago, zona, cargo, concepto) is used by
// project convention; misspell linting is silenced at the package level.
//
//nolint:misspell // Spanish domain vocabulary by project convention.
package ventfb

import (
	"context"
	"database/sql"
	"log/slog"
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
//
// DOCTOS_CC.DESCRIPCION tiene CHARACTER SET NONE (bytes Win1252 crudos de
// Microsip, a diferencia de CLIENTES.NOMBRE que es ISO8859_1). Leerla verbatim
// sobre una conexión FB_CHARSET=UTF8 hace que el driver truene en filas con
// acentos (firebird_error). El CAST a WIN1252 fuerza a Firebird a transcodificar
// a UTF8 válido en el wire. CLIENTES.NOMBRE (ISO8859_1) no necesita el cast.
//
// La última columna expone el UUID original de MSP_PAGOS_RECIBIDOS que la app
// móvil generó al capturar el pago, resuelto vía SUBQUERY ESCALAR
// correlacionada (NO un JOIN) — ver la nota en pagoFromClause sobre por qué.
// MIN(pr.ID) colapsa a un solo valor (o NULL) incluso si algún día existiera
// más de una fila de MSP_PAGOS_RECIBIDOS con el mismo IMPTE_DOCTO_CC_ID,
// haciendo el fan-out estructuralmente imposible en vez de simplemente
// improbable. El índice IDX_MSP_PAGOS_RECIB_IMPTE (migración 000054) vuelve
// esta subquery un lookup indexado por fila, no un scan.
//
// REGLA: el valor SIEMPRE sale de Microsip (DOCTOS_CC / IMPORTES_DOCTOS_CC).
// De MSP_PAGOS_VENTAS solo se toma lo que en Microsip no existe:
//
//	UPDATED_AT  el cursor incremental — Microsip no tiene marca de cambio
//	TX_ID       el watermark (ADR-0007)
//	la EXISTENCIA de la fila cuando Microsip ya la borró (tombstone)
//
// Ese ultimo caso es lo que sostiene cada COALESCE: `i` y `dc` entran por
// LEFT JOIN a proposito, para que un pago borrado fisicamente en Microsip
// siga viajando por el sync y el telefono lo elimine en vez de quedarse con
// un pago fantasma (migraciones 000019/000020). Son 144 filas hoy. Cuando
// Microsip tiene la fila, manda Microsip; cuando ya no la tiene, la cache es
// lo unico que queda.
//
// Por que la regla: la cache SE DESFASA al editar el encabezado. Medido sobre
// los 1,931,042 pagos de produccion: 43 folios y 41 fechas divergentes, y —el
// caso extremo— LAT/LON en NULL en el 100% de las filas mientras DOCTOS_CC
// tiene 965,930 pagos con coordenada. Ese hueco es el que hacia que el cliente
// pintara su centinela de "sin ubicacion" (Long.MAX_VALUE) como si fuera una
// distancia en metros.
//
// FECHA y LAT/LON salen de DOCTOS_CC, no de la caché. Regla: los datos que
// viajan al teléfono son los de Microsip; MSP_PAGOS_VENTAS existe para
// DETECTAR cambios (su UPDATED_AT es el cursor incremental), no para ser la
// verdad. Sus columnas son copias y una copia puede quedar sin poblar sin que
// nadie se entere — que es exactamente lo que pasó: LAT/LON se crearon en la
// migración 000013 como placeholder ("hoy no hay fuente") y quedaron NULL en
// el 100% de las filas, mientras DOCTOS_CC tenía 805,378 pagos con coordenada.
// El cliente terminó pintando su centinela de "sin ubicación" (Long.MAX_VALUE)
// como si fuera una distancia en metros.
//
// El COALESCE contra p.* es por los tombstones: dc entra por LEFT JOIN
// precisamente para que las filas cuyo DOCTO_CC padre fue borrado en Microsip
// sigan viajando por el sync (ver pagoFromClause). Sin él, esas filas
// llegarían con FECHA NULL y reventarían el scan.
//
// FECHA es la EXCEPCION a la regla, y va al reves que las demas: la cache
// primero, Microsip de respaldo. No es descuido — es que la HORA del cobro no
// existe en Microsip. DOCTOS_CC.FECHA es un DATE (solo dia); el instante real
// en que el cobrador capturo el pago vive en MSP_PAGOS_RECIBIDOS y de ahi lo
// hereda MSP_PAGOS_VENTAS.FECHA. Por la regla misma —de la cache solo lo que
// Microsip no tiene— la hora le toca a la cache.
//
// La legacy hacia exactamente esto:
//
//	COALESCE(MSP_PAGOS_RECIBIDOS.FECHA, DOCTOS_CC.FECHA + DOCTOS_CC.HORA)
//
// Medido en produccion: de los pagos de una semana en la zona 34, 165 traen
// hora real y 7 vienen a medianoche (capturas de oficina, que solo tienen
// fecha). Tomar dc.FECHA para todos aplanaria esos 165 a las 00:00 y moveria
// el borde de la ventana del cobrador: un pago cobrado despues de la hora en
// que abre su semana se le saldria del corte. Por eso NO se toca.
//
// El CAST del respaldo no es cosmetico: DOCTOS_CC.FECHA es DATE y la de la
// cache es TIMESTAMP; Firebird rechaza el COALESCE entre ambos con "Datatypes
// are not comparable" (SQLSTATE HY004). Es un error que solo aparece contra
// Firebird real — compila y los tests unitarios pasan igual.
const selectPagoColsP = `
	p.IMPTE_DOCTO_CC_ID,
	COALESCE(dc.DOCTO_CC_ID, p.DOCTO_CC_ID),
	COALESCE(i.DOCTO_CC_ACR_ID, p.DOCTO_CC_ACR_ID),
	COALESCE(dc.CLIENTE_ID, p.CLIENTE_ID),
	p.ZONA_CLIENTE_ID,
	COALESCE(dc.FOLIO, p.FOLIO),
	COALESCE(dc.CONCEPTO_CC_ID, p.CONCEPTO_CC_ID),
	COALESCE(p.FECHA, CAST(dc.FECHA AS TIMESTAMP)),
	COALESCE(i.IMPORTE + i.IMPUESTO, p.IMPORTE + p.IMPUESTO),
	COALESCE(i.IMPUESTO, p.IMPUESTO),
	dc.LAT,
	dc.LON,
	COALESCE(i.CANCELADO, p.CANCELADO),
	COALESCE(i.APLICADO, p.APLICADO),
	p.UPDATED_AT,
	COALESCE(CAST(dc.DESCRIPCION AS VARCHAR(200) CHARACTER SET WIN1252), ''),
	COALESCE(c.NOMBRE, ''),
	dc.COBRADOR_ID,
	fcd.FORMA_COBRO_ID,
	(SELECT MIN(pr.ID) FROM MSP_PAGOS_RECIBIDOS pr WHERE pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID)`

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
//
// NOTA sobre pago_recibido_id: MSP_PAGOS_RECIBIDOS NO se agrega aquí como
// JOIN. Si el predicado pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID alguna vez
// dejara de ser 1:1 (no hay UNIQUE constraint que lo garantice — solo un
// índice, migración 000054), un LEFT JOIN provocaría fan-out: la misma fila
// de MSP_PAGOS_VENTAS se duplicaría una vez por cada MSP_PAGOS_RECIBIDOS que
// matcheé, y el cliente móvil recibiría pagos repetidos. En vez de eso,
// selectPagoColsP resuelve pago_recibido_id con una SUBQUERY ESCALAR
// correlacionada (MIN(pr.ID) ...) que por construcción SQL solo puede
// devolver cero o un valor por fila externa — el fan-out es estructuralmente
// imposible, no simplemente improbable. pagoFromClause se mantiene sin
// cambios (ni un JOIN más) precisamente para que esta garantía cubra tanto el
// sync/by-ids como el digest/ListIDs, que comparten esta misma constante.
const pagoFromClause = `
FROM MSP_PAGOS_VENTAS p
JOIN MSP_SALDOS_VENTAS s        ON s.DOCTO_CC_ID = p.DOCTO_CC_ACR_ID
LEFT JOIN IMPORTES_DOCTOS_CC i  ON i.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID
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
	lat := scanCoordinate(p.latRaw)
	lon := scanCoordinate(p.lonRaw)

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

// pagoEnrichedRowScan extiende pagoRowScan con los campos resueltos via JOIN
// para el endpoint /sync/pagos (cobrador, cliente, forma_cobro, pago_recibido
// UUID).
type pagoEnrichedRowScan struct {
	pagoRowScan
	cobradorRaw       sql.NullString
	nombreClienteRaw  sql.NullString
	cobradorIDRaw     sql.NullInt64
	formaCobroIDRaw   sql.NullInt64
	pagoRecibidoIDRaw sql.NullString
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
		&p.pagoRecibidoIDRaw,
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
		PagoRecibidoID: nullableStringPtr(p.pagoRecibidoIDRaw),
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

// scanCoordinate decodifica DOCTOS_CC.LAT / .LON, que NO son numéricos: son
// VARCHAR(100) con el valor en texto ("18.2708885", "-97.1446266"). Por eso el
// sistema Node traía un conversor de buffer para estas dos columnas.
//
// Devuelve nil —"sin ubicación"— en tres casos:
//
//   - SQL NULL: el pago no trae coordenada.
//   - Valor cero: `0` y `0.0` son el centinela de "sin señal GPS" que graba la
//     app al capturar sin fix. Son 68,526 pagos. Dejarlos pasar como 0.0 es
//     peor que descartarlos: el cliente los toma por una ubicación real y
//     calcula el centroide de la venta contra el punto (0,0), en el Golfo de
//     Guinea, así que la distancia sale igual de absurda pero sin el número
//     gigante que delata el problema.
//   - Texto no parseable: se descarta la coordenada en vez de reventar. Es un
//     campo de presentación (ordenar por cercanía); un valor sucio no puede
//     costarle al cobrador la página entera del sync — y con ella su cobranza.
//     Hoy las 805,378 filas están limpias, pero quien escribe esa columna es
//     Microsip y el legacy, no nosotros.
func scanCoordinate(raw any) *decimal.Decimal {
	if raw == nil {
		return nil
	}
	d, err := firebird.ScanDecimal(raw, 8)
	if err != nil {
		slog.Warn("cobranza: coordenada ilegible en DOCTOS_CC; se omite",
			"error", err)
		return nil
	}
	if d.IsZero() {
		return nil
	}
	return &d
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

// nullableStringPtr returns nil when v is SQL NULL, otherwise a pointer to
// the value. Distinct from nullableString (which collapses NULL to "") — used
// for fields where "absent" and "empty string" are meaningfully different
// (e.g. pago_recibido_id, where nil signals "no MSP_PAGOS_RECIBIDOS row").
func nullableStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}
