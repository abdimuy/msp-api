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

// Compile-time assertion: VentasRepo satisfies the outbound port.
var _ outbound.VentasRepo = (*VentasRepo)(nil)

// VentasRepo implements outbound.VentasRepo backed by a JOIN over
// MSP_SALDOS_VENTAS + CLIENTES + ZONAS_CLIENTES + COBRADORES + DIRS_CLIENTES +
// LIBRES_CARGOS_CC + DOCTOS_PV. Performance is gated by the
// IDX_MSP_SALDOS_ZONA_FUP index on the driving table; downstream JOINs are
// pure PK lookups (~51 ms for a 1000-row page on a representative zone).
type VentasRepo struct {
	pool *firebird.Pool
}

// NewVentasRepo builds a VentasRepo wired to the given pool.
func NewVentasRepo(pool *firebird.Pool) *VentasRepo {
	return &VentasRepo{pool: pool}
}

// ─── SQL ─────────────────────────────────────────────────────────────────────

// selectVentaCols is the canonical SELECT list for the enriched venta query.
// Order matches ventaRowScan.scanFrom one-to-one.
//
// Notes:
//   - VENDEDOR_1/2/3 y FREC_PAGO se resuelven desde LISTAS_ATRIBUTOS a su
//     VALOR_DESPLEGADO (texto humano). LIBRES_CARGOS_CC guarda solo los IDs
//     (l.VENDEDOR_1/2/3 son IDs de atributo, l.FORMA_DE_PAGO es el ID que
//     mapea a SEMANAL/QUINCENAL/MENSUAL). El sistema Node legacy hace
//     exactamente estos JOINs — los respetamos para no romper la app.
//   - ESTADO se resuelve desde el catalogo ESTADOS via DIRS_CLIENTES.ESTADO_ID.
const selectVentaCols = `
	s.DOCTO_CC_ID, s.DOCTO_PV_ID, s.CLIENTE_ID, s.ZONA_CLIENTE_ID, s.FOLIO,
	s.FECHA_CARGO, s.PRECIO_TOTAL, s.TOTAL_IMPORTE, s.IMPTE_REST,
	s.SALDO, s.NUM_PAGOS, s.FECHA_ULT_PAGO, s.CARGO_CANCELADO, s.UPDATED_AT,
	dc.FECHA,
	c.NOMBRE, c.LIMITE_CREDITO, c.NOTAS, c.COBRADOR_ID,
	z.NOMBRE,
	cob.NOMBRE,
	d.CALLE, d.POBLACION, e.NOMBRE, d.TELEFONO1,
	l.PARCIALIDAD, l.ENGANCHE, l.TIEMPO_A_CORTO_PLAZOMESES,
	l.MONTO_A_CORTO_PLAZO, l.PRECIO_DE_CONTADO, l.AVAL_O_RESPONSABLE,
	UPPER(lv1.VALOR_DESPLEGADO),
	UPPER(lv2.VALOR_DESPLEGADO),
	UPPER(lv3.VALOR_DESPLEGADO),
	UPPER(lfp.VALOR_DESPLEGADO)`

const ventaFromClause = `
FROM MSP_SALDOS_VENTAS s
JOIN CLIENTES c                ON c.CLIENTE_ID       = s.CLIENTE_ID
LEFT JOIN ZONAS_CLIENTES z     ON z.ZONA_CLIENTE_ID  = s.ZONA_CLIENTE_ID
LEFT JOIN COBRADORES cob       ON cob.COBRADOR_ID    = c.COBRADOR_ID
LEFT JOIN DIRS_CLIENTES d      ON d.CLIENTE_ID       = s.CLIENTE_ID AND d.ES_DIR_PPAL = 'S'
LEFT JOIN ESTADOS e            ON e.ESTADO_ID        = d.ESTADO_ID
LEFT JOIN LIBRES_CARGOS_CC l   ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lv1 ON lv1.LISTA_ATRIB_ID = l.VENDEDOR_1
LEFT JOIN LISTAS_ATRIBUTOS lv2 ON lv2.LISTA_ATRIB_ID = l.VENDEDOR_2
LEFT JOIN LISTAS_ATRIBUTOS lv3 ON lv3.LISTA_ATRIB_ID = l.VENDEDOR_3
LEFT JOIN LISTAS_ATRIBUTOS lfp ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN DOCTOS_PV dc         ON dc.DOCTO_PV_ID     = s.DOCTO_PV_ID`

// SyncPorZona returns a page of enriched ventas for incremental sync.
//
// Note on watermark: the watermark parameter (TX_ID < watermark) is NOT
// applied to the ventas query. MSP_SALDOS_VENTAS does have a TX_ID column
// (added by mig 22), but the ventas sync is out of scope for the cobranza
// push-channel sprint (commit 7). The watermark is accepted from runSyncPage
// and discarded here. This is intentional and correct — ventas correctness
// relies on the UPDATED_AT cursor, and the clock-skew margin (1 s) is
// sufficient for the ventas use-case. If ventas ever needs watermark
// filtering, add it to queryVentaSyncPage and ventaSyncSpec.
func (r *VentasRepo) SyncPorZona(
	ctx context.Context, zonaID int, cursor time.Time, afterID, limit int, desde time.Time,
) (outbound.SyncPage[domain.Venta], error) {
	pageQuery := func(ctx context.Context, q firebird.Querier, upper time.Time, _ int64) (*sql.Rows, error) {
		return queryVentaSyncPage(ctx, q, ventaSyncSpec{
			zonaID:     zonaID,
			cursor:     cursor,
			upperBound: upper,
			afterID:    afterID,
			limit:      limit,
			desde:      desde,
		})
	}
	return runSyncPage[domain.Venta](ctx, r.pool, cursor, limit, pageQuery, scanVentaRows)
}

// ByIDs returns the enriched Venta rows for the given primary keys, constrained
// to ZONA_CLIENTE_ID = zonaID. Uses selectVentaCols + ventaFromClause for
// shape parity with SyncPorZona. No watermark filtering — the caller (by-ids
// HTTP endpoint) obtained these PKs from the SSE listener which only publishes
// committed rows.
//
// Duplicate IDs in the input are deduplicated before querying. Rows whose
// PK is in ids but whose zona does not match are silently excluded.
//
//nolint:dupl // structurally mirrors PagosRepo.ByIDs; differs in column list + scanner + return type — abstraction not worth it
func (r *VentasRepo) ByIDs(ctx context.Context, zonaID int, ids []int) ([]domain.Venta, error) {
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
SELECT ` + selectVentaCols + ventaFromClause + `
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.DOCTO_CC_ID IN (` + strings.Join(placeholders, ",") + `)`

	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	return scanVentaRows(rows)
}

// ventaSyncSpec parametrizes the enriched venta sync page query. Same cursor
// semantics as syncPageSpec but the SELECT/FROM are fixed to the enriched
// projection.
type ventaSyncSpec struct {
	zonaID     int
	cursor     time.Time
	upperBound time.Time
	afterID    int
	limit      int
	// desde acota el filtro de saldados en el sync inicial (cursor zero).
	// Ignorado cuando cursor != zero. Ver queryVentaSyncPage.
	desde time.Time
}

// queryVentaSyncPage builds and executes the enriched venta page query. Same
// cursor-and-tie-break semantics as querySyncPage in page_helpers.go but with
// the JOIN baked in.
//
// Filtro de saldo dinámico según `desde` (independiente del cursor — el
// cliente debe mandar el mismo `desde` en TODAS las páginas, no solo en la
// primera, para que la paginación no se cuele saldados que no caben en la
// ventana del cobrador):
//
//   - desde zero (legacy admin/full sync sin ventana): solo activos
//     (SALDO > 0) + tombstones (CARGO_CANCELADO = 'S'). Saldados
//     silenciosos no se mandan.
//
//   - desde set (sync del cobrador con FECHA_CARGA_INICIAL): tres ramas
//     deben cumplirse, cada una alineada con la semántica del cliente:
//
//     1. Activos (SALDO > 0): siempre viajan — el cobrador los necesita
//     para cobrar.
//
//     2. Tombstones (CARGO_CANCELADO='S') cuya cancelación real en
//     Microsip cae dentro de la ventana (FECHA_HORA_CANCELACION >=
//     desde). Sin este sub-filtro, cualquier backfill del cache que
//     toque UPDATED_AT (típico en migraciones que llaman
//     MSP_RECOMPUTE_SALDO_VENTA en bucle) "resucita" tombstones de
//     cancelaciones de 2018-2025 que ya nadie tiene en local. El
//     cliente las recibiría solo para borrarlas en no-op silencioso
//     — bandwidth desperdiciado más ruido en logs de sync. NULL en
//     FECHA_HORA_CANCELACION se trata como "fecha desconocida" y se
//     propaga (defensivo: mejor mandar un delete de más que perder
//     una cancelación legítima).
//
//     3. Saldadas con pago de cobranza activa (CONCEPTO_CC_ID IN 87327
//     cobranza en ruta, 27969 abono mostrador) en ventana. NO basta
//     FECHA_ULT_PAGO porque esa columna avanza con pagos
//     administrativos (anticipos, condonaciones, ajustes) que
//     /sync/pagos ya filtra fuera — incluirlas aquí desperdiciaba
//     bandwidth porque el cliente terminaba borrándolas en
//     mergeVentas. El filtro replica exacto el de /sync/pagos
//     (pagos_repo.pagoConceptoFilter) vía EXISTS sobre
//     MSP_PAGOS_VENTAS (índice IDX_MSP_PAGOS_CARGO).
func queryVentaSyncPage(ctx context.Context, q firebird.Querier, spec ventaSyncSpec) (*sql.Rows, error) {
	upper := firebird.ToWallClock(spec.upperBound)
	statusFilter := `(s.SALDO > 0 OR s.CARGO_CANCELADO = 'S')`
	var statusArgs []any
	if !spec.desde.IsZero() {
		desde := firebird.ToWallClock(spec.desde)
		statusFilter = `(s.SALDO > 0
		OR (s.CARGO_CANCELADO = 'S' AND EXISTS (
			SELECT 1 FROM DOCTOS_CC dc
			WHERE dc.DOCTO_CC_ID = s.DOCTO_CC_ID
			  AND (dc.FECHA_HORA_CANCELACION IS NULL
			       OR dc.FECHA_HORA_CANCELACION >= ?)
		))
		OR EXISTS (
			SELECT 1 FROM MSP_PAGOS_VENTAS p
			WHERE p.DOCTO_CC_ACR_ID = s.DOCTO_CC_ID
			  AND p.FECHA          >= ?
			  AND p.CONCEPTO_CC_ID IN (87327, 27969)
		))`
		statusArgs = []any{desde, desde}
	}
	if spec.cursor.IsZero() {
		args := append([]any{spec.limit, spec.zonaID, upper, spec.afterID}, statusArgs...)
		query := `
SELECT FIRST ? ` + selectVentaCols + ventaFromClause + `
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.UPDATED_AT <= ?
  AND s.DOCTO_CC_ID > ?
  AND ` + statusFilter + `
ORDER BY s.UPDATED_AT, s.DOCTO_CC_ID`
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		return rows, nil
	}
	cur := firebird.ToWallClock(spec.cursor)
	// UPDATED_AT >= cursor (no estricto) habilita el tie-break por
	// DOCTO_CC_ID. Ver page_helpers.queryVentaSyncPage para el detalle del
	// caso que motiva esta forma.
	args := append([]any{spec.limit, spec.zonaID, cur, upper, cur, cur, spec.afterID}, statusArgs...)
	query := `
SELECT FIRST ? ` + selectVentaCols + ventaFromClause + `
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.UPDATED_AT >= ?
  AND s.UPDATED_AT <= ?
  AND (s.UPDATED_AT > ? OR (s.UPDATED_AT = ? AND s.DOCTO_CC_ID > ?))
  AND ` + statusFilter + `
ORDER BY s.UPDATED_AT, s.DOCTO_CC_ID`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return rows, nil
}

// ─── scan helpers ─────────────────────────────────────────────────────────────

// ventaRowScan mirrors selectVentaCols 1:1. Splitting the raw scan from the
// type conversions keeps cyclomatic complexity bounded.
type ventaRowScan struct {
	// MSP_SALDOS_VENTAS.
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

	// DOCTOS_PV.
	fechaVentaRaw any

	// CLIENTES.
	clienteNombreRaw sql.NullString
	limiteCreditoRaw any
	clienteNotasRaw  sql.NullString
	cobradorIDRaw    sql.NullInt64

	// ZONAS_CLIENTES.
	zonaNombreRaw sql.NullString

	// COBRADORES.
	nombreCobradorRaw sql.NullString

	// DIRS_CLIENTES.
	calleRaw    sql.NullString
	ciudadRaw   sql.NullString
	estadoRaw   sql.NullString
	telefonoRaw sql.NullString

	// LIBRES_CARGOS_CC.
	parcialidadRaw           sql.NullInt64
	engancheRaw              any
	tiempoCortoPlazoMesesRaw sql.NullInt64
	montoCortoPlazoRaw       any
	precioDeContadoRaw       any
	avalOResponsableRaw      sql.NullString
	vendedor1Raw             sql.NullString
	vendedor2Raw             sql.NullString
	vendedor3Raw             sql.NullString
	frecPagoRaw              sql.NullString
}

func (s *ventaRowScan) scanFrom(r scannable) error {
	return r.Scan(
		&s.doctoCCID, &s.doctoPVIDRaw, &s.clienteID, &s.zonaRaw, &s.folio,
		&s.fechaCargoRaw, &s.precioTotalRaw, &s.totalImporteRaw, &s.impteRestRaw,
		&s.saldoRaw, &s.numPagos, &s.fechaUltRaw, &s.cargoCancelado, &s.updatedAtRaw,
		&s.fechaVentaRaw,
		&s.clienteNombreRaw, &s.limiteCreditoRaw, &s.clienteNotasRaw, &s.cobradorIDRaw,
		&s.zonaNombreRaw,
		&s.nombreCobradorRaw,
		&s.calleRaw, &s.ciudadRaw, &s.estadoRaw, &s.telefonoRaw,
		&s.parcialidadRaw, &s.engancheRaw, &s.tiempoCortoPlazoMesesRaw,
		&s.montoCortoPlazoRaw, &s.precioDeContadoRaw, &s.avalOResponsableRaw,
		&s.vendedor1Raw, &s.vendedor2Raw, &s.vendedor3Raw,
		&s.frecPagoRaw,
	)
}

// hydrate converts the raw scan values into a domain.Venta.
func (s *ventaRowScan) hydrate() (domain.Venta, error) {
	amounts, err := s.scanSaldoAmounts()
	if err != nil {
		return domain.Venta{}, err
	}
	timestamps, err := s.scanTimestamps()
	if err != nil {
		return domain.Venta{}, err
	}
	contract, err := s.scanContrato()
	if err != nil {
		return domain.Venta{}, err
	}
	return domain.HydrateVenta(domain.HydrateVentaParams{
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

		FechaVenta: timestamps.fechaVenta,

		ClienteNombre:  nullableString(s.clienteNombreRaw),
		LimiteCredito:  contract.limiteCredito,
		ClienteNotas:   nullableString(s.clienteNotasRaw),
		CobradorID:     nullableInt(s.cobradorIDRaw),
		NombreCobrador: nullableString(s.nombreCobradorRaw),

		ZonaNombre: nullableString(s.zonaNombreRaw),

		Calle:    nullableString(s.calleRaw),
		Ciudad:   nullableString(s.ciudadRaw),
		Estado:   nullableString(s.estadoRaw),
		Telefono: nullableString(s.telefonoRaw),

		Parcialidad:           nullableInt(s.parcialidadRaw),
		Enganche:              contract.enganche,
		TiempoCortoPlazoMeses: nullableInt(s.tiempoCortoPlazoMesesRaw),
		MontoCortoPlazo:       contract.montoCortoPlazo,
		PrecioDeContado:       contract.precioDeContado,
		AvalOResponsable:      nullableString(s.avalOResponsableRaw),
		Vendedor1:             nullableString(s.vendedor1Raw),
		Vendedor2:             nullableString(s.vendedor2Raw),
		Vendedor3:             nullableString(s.vendedor3Raw),
		FrecPago:              nullableString(s.frecPagoRaw),
	}), nil
}

// ventaSaldoAmounts groups the four MSP_SALDOS_VENTAS decimal columns.
type ventaSaldoAmounts struct {
	precioTotal  decimal.Decimal
	totalImporte decimal.Decimal
	impteRest    decimal.Decimal
	saldo        decimal.Decimal
}

func (s *ventaRowScan) scanSaldoAmounts() (ventaSaldoAmounts, error) {
	precio, err := firebird.ScanDecimal(s.precioTotalRaw, 2)
	if err != nil {
		return ventaSaldoAmounts{}, err
	}
	total, err := firebird.ScanDecimal(s.totalImporteRaw, 2)
	if err != nil {
		return ventaSaldoAmounts{}, err
	}
	rest, err := firebird.ScanDecimal(s.impteRestRaw, 2)
	if err != nil {
		return ventaSaldoAmounts{}, err
	}
	saldo, err := firebird.ScanDecimal(s.saldoRaw, 2)
	if err != nil {
		return ventaSaldoAmounts{}, err
	}
	return ventaSaldoAmounts{precio, total, rest, saldo}, nil
}

// ventaTimestamps groups all the timestamp columns; fechaUltPago and
// fechaVenta are optional.
type ventaTimestamps struct {
	fechaCargo   time.Time
	fechaUltPago *time.Time
	updatedAt    time.Time
	fechaVenta   *time.Time
}

func (s *ventaRowScan) scanTimestamps() (ventaTimestamps, error) {
	fechaCargo, err := firebird.ScanUTCTime(s.fechaCargoRaw)
	if err != nil {
		return ventaTimestamps{}, err
	}
	updatedAt, err := firebird.ScanUTCTime(s.updatedAtRaw)
	if err != nil {
		return ventaTimestamps{}, err
	}
	ts := ventaTimestamps{fechaCargo: fechaCargo, updatedAt: updatedAt}
	if s.fechaUltRaw != nil {
		t, err := firebird.ScanUTCTime(s.fechaUltRaw)
		if err != nil {
			return ventaTimestamps{}, err
		}
		ts.fechaUltPago = &t
	}
	if s.fechaVentaRaw != nil {
		t, err := firebird.ScanUTCTime(s.fechaVentaRaw)
		if err != nil {
			return ventaTimestamps{}, err
		}
		ts.fechaVenta = &t
	}
	return ts, nil
}

// ventaContractAmounts groups the LIBRES_CARGOS_CC nullable decimals plus
// CLIENTES.LIMITE_CREDITO. nil pointer when the source column was SQL NULL.
type ventaContractAmounts struct {
	limiteCredito   *decimal.Decimal
	enganche        *decimal.Decimal
	montoCortoPlazo *decimal.Decimal
	precioDeContado *decimal.Decimal
}

func (s *ventaRowScan) scanContrato() (ventaContractAmounts, error) {
	limite, err := scanNullDecimal2Ptr(s.limiteCreditoRaw)
	if err != nil {
		return ventaContractAmounts{}, err
	}
	enganche, err := scanNullDecimal2Ptr(s.engancheRaw)
	if err != nil {
		return ventaContractAmounts{}, err
	}
	monto, err := scanNullDecimal2Ptr(s.montoCortoPlazoRaw)
	if err != nil {
		return ventaContractAmounts{}, err
	}
	contado, err := scanNullDecimal2Ptr(s.precioDeContadoRaw)
	if err != nil {
		return ventaContractAmounts{}, err
	}
	return ventaContractAmounts{limite, enganche, monto, contado}, nil
}

// scanNullDecimal2Ptr maps a possibly-nil NUMERIC(_,2) driver value to
// *decimal.Decimal. Returns nil when src is nil.
func scanNullDecimal2Ptr(src any) (*decimal.Decimal, error) {
	if src == nil {
		return nil, nil //nolint:nilnil // nil = column was SQL NULL.
	}
	d, err := firebird.ScanDecimal(src, 2)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// scanVentaRows iterates a *sql.Rows, scanning each into a domain.Venta slice.
func scanVentaRows(rows *sql.Rows) ([]domain.Venta, error) {
	var result []domain.Venta
	for rows.Next() {
		var rs ventaRowScan
		if err := rs.scanFrom(rows); err != nil {
			return nil, firebird.MapError(err)
		}
		v, err := rs.hydrate()
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return result, nil
}
