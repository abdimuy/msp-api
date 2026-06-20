// Package clientesfb implements the Firebird-backed ClientesRepo for the
// clientes hub. All reads target native Microsip tables (CLIENTES, DOCTOS_PV,
// DOCTOS_CC, IMPORTES_DOCTOS_CC, DOCTOS_PV_DET, ARTICULOS, LIBRES_CARGOS_CC,
// DOCTOS_ENTRE_SIS). This module owns no MSP_* tables and never writes.
//
//nolint:misspell // Spanish domain vocabulary (clientes, directorio, ficha, etc.) by project convention.
package clientesfb

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: ClientesRepo satisfies the outbound port.
var _ outbound.ClientesRepo = (*ClientesRepo)(nil)

// ClientesRepo is the native Firebird implementation of outbound.ClientesRepo.
// It reads Microsip tables directly; no MSP_* caches are used.
type ClientesRepo struct {
	pool *firebird.Pool
}

// NewClientesRepo builds a ClientesRepo wired to the given pool.
func NewClientesRepo(pool *firebird.Pool) *ClientesRepo {
	return &ClientesRepo{pool: pool}
}

// ─── ObtenerCliente ───────────────────────────────────────────────────────────

// ObtenerCliente returns the identity projection for a single client.
// Returns domain.ErrClienteNotFound when no row exists for clienteID.
func (r *ClientesRepo) ObtenerCliente(ctx context.Context, clienteID int) (*domain.Cliente, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	row := q.QueryRowContext(ctx, queryObtenerCliente, clienteID)
	var raw clienteRowRaw
	if err := raw.scanFrom(row); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrClienteNotFound
		}
		return nil, firebird.MapError(err)
	}
	c, err := raw.assemble()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ─── ListarDirectorioCompleto ─────────────────────────────────────────────────

// buildDirectorioCompletoQuery constructs the unbounded directory query and its
// argument list from the native filters. No FIRST / cursor — every matching row
// is returned, ordered by NOMBRE then CLIENTE_ID. The grouped saldo derived table
// supplies SALDO_TOTAL; ConSaldo wraps the result so only saldo>0 rows survive.
func buildDirectorioCompletoQuery(f outbound.FiltroDirectorio) (string, []any) {
	args := make([]any, 0, 4)
	conditions := make([]string, 0, 3)

	if len(f.ClienteIDs) > 0 {
		placeholders := buildInPlaceholders(len(f.ClienteIDs))
		conditions = append(conditions, "c.CLIENTE_ID IN "+placeholders)
		for _, id := range f.ClienteIDs {
			args = append(args, id)
		}
	} else {
		if f.ZonaClienteID != nil {
			conditions = append(conditions, "c.ZONA_CLIENTE_ID = ?")
			args = append(args, *f.ZonaClienteID)
		}
		if f.CobradorID != nil {
			conditions = append(conditions, "c.COBRADOR_ID = ?")
			args = append(args, *f.CobradorID)
		}
	}

	andClause := ""
	if len(conditions) > 0 {
		andClause = " AND " + strings.Join(conditions, " AND ")
	}

	if f.ConSaldo {
		// Wrap in a derived table so the ConSaldo predicate applies to the
		// already-computed SALDO_TOTAL. The inner query keeps the grouped join.
		// Column list must match selectDirectorioColsGrouped (selectClienteCols + SALDO_TOTAL),
		// including the two GPS columns added to selectClienteCols.
		inner := queryListarDirectorioCompletoBase + andClause
		query := `SELECT ` +
			`d.CLIENTE_ID, d.NOMBRE, d.LIMITE_CREDITO, d.NOTAS, d.ESTATUS, ` +
			`d.ZONA_CLIENTE_ID, d.ZONA_NOMBRE, d.COBRADOR_ID, d.COBRADOR_NOMBRE, ` +
			`d.NOMBRE_CALLE, d.COLONIA, d.POBLACION, d.ESTADO_NOMBRE, d.TELEFONO1, ` +
			`d.U_LATITUD, d.U_LONGITUD, ` +
			`d.SALDO_TOTAL ` +
			`FROM (` + inner + `) d WHERE d.SALDO_TOTAL > 0 ORDER BY d.NOMBRE, d.CLIENTE_ID`
		return query, args
	}

	query := queryListarDirectorioCompletoBase + andClause + " ORDER BY c.NOMBRE, c.CLIENTE_ID"
	return query, args
}

// ListarDirectorioCompleto returns ALL clients matching the native filters, each
// with identity + SaldoTotal, with NO pagination. Saldo is computed with a single
// grouped aggregation (see queryListarDirectorioCompletoBase for the measured
// performance characteristics — sub-second when a zone/cobrador filter bounds the
// set, ~tens of seconds for the unfiltered whole padrón).
func (r *ClientesRepo) ListarDirectorioCompleto(
	ctx context.Context,
	f outbound.FiltroDirectorio,
) ([]outbound.DirectorioItem, error) {
	query, args := buildDirectorioCompletoQuery(f)

	var items []outbound.DirectorioItem
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw directorioRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			item, aerr := raw.assemble()
			if aerr != nil {
				return aerr
			}
			items = append(items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ─── ObtenerResumenFicha ──────────────────────────────────────────────────────

// fetchFichaTotales returns TotalComprado, TotalAbonado, NumVentas, NumPagos.
//
// Two separate queries are used so that each KPI is gated by the correct date:
//   - TotalComprado / NumVentas → cargo.FECHA (sale/charge date)
//   - TotalAbonado  / NumPagos  → abono.FECHA (payment date)
//
// This makes the header KPIs definitionally consistent with the chart queries
// (queryAbonosPorMesBase, buildCompradoVsAbonadoQuery) which already filter
// abonado activity by abono.FECHA. Without this split, a date range would count
// "payments on sales created in range" (wrong) instead of "payments made in
// range" (correct), causing a visible mismatch between the KPI totals and the
// sum of the chart series.
func (r *ClientesRepo) fetchFichaTotales(ctx context.Context, q firebird.Querier, clienteID int, rango outbound.RangoFechas) (decimal.Decimal, decimal.Decimal, int, int, error) {
	// ── Query 1: TotalComprado + NumVentas, filtered by cargo.FECHA ──────────
	compradoQry := queryResumenFichaComprado
	compradoArgs := []any{clienteID}
	if rango.Desde != nil {
		compradoQry += "\n  AND cargo.FECHA >= ?"
		compradoArgs = append(compradoArgs, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		compradoQry += "\n  AND cargo.FECHA <= ?"
		compradoArgs = append(compradoArgs, firebird.ToWallClock(*rango.Hasta))
	}
	var compRow resumenFichaCompradoRaw
	if serr := compRow.scanFrom(q.QueryRowContext(ctx, compradoQry, compradoArgs...)); serr != nil {
		return decimal.Zero, decimal.Zero, 0, 0, firebird.MapError(serr)
	}
	totalComprado, numVentas, err := compRow.assemble()
	if err != nil {
		return decimal.Zero, decimal.Zero, 0, 0, err
	}

	// ── Query 2: TotalAbonado + NumPagos, filtered by abono.FECHA ────────────
	abonadoQry := queryResumenFichaAbonado
	abonadoArgs := []any{clienteID}
	if rango.Desde != nil {
		abonadoQry += "\n  AND abono.FECHA >= ?"
		abonadoArgs = append(abonadoArgs, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		abonadoQry += "\n  AND abono.FECHA <= ?"
		abonadoArgs = append(abonadoArgs, firebird.ToWallClock(*rango.Hasta))
	}
	var aboRow resumenFichaAbonadoRaw
	if serr := aboRow.scanFrom(q.QueryRowContext(ctx, abonadoQry, abonadoArgs...)); serr != nil {
		return decimal.Zero, decimal.Zero, 0, 0, firebird.MapError(serr)
	}
	totalAbonado, numPagos, err := aboRow.assemble()
	if err != nil {
		return decimal.Zero, decimal.Zero, 0, 0, err
	}

	return totalComprado, totalAbonado, numVentas, numPagos, nil
}

// fetchFichaSaldo returns the ficha's total saldo from the MSP_SALDOS_VENTAS cache
// (user-approved exception), the same source the directory uses, so the ficha saldo
// equals the directory saldo exactly.
func (r *ClientesRepo) fetchFichaSaldo(ctx context.Context, q firebird.Querier, clienteID int) (decimal.Decimal, error) {
	var saldoRaw any
	if serr := q.QueryRowContext(ctx, queryResumenFichaSaldo, clienteID).Scan(&saldoRaw); serr != nil {
		return decimal.Zero, firebird.MapError(serr)
	}
	return firebird.ScanDecimal(saldoRaw, 2)
}

func (r *ClientesRepo) fetchAbonosPorMes(ctx context.Context, q firebird.Querier, clienteID int, rango outbound.RangoFechas) ([]outbound.PuntoMensual, error) {
	qry := queryAbonosPorMesBase
	args := []any{clienteID}
	if rango.Desde != nil {
		qry += "\n  AND abono.FECHA >= ?"
		args = append(args, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		qry += "\n  AND abono.FECHA <= ?"
		args = append(args, firebird.ToWallClock(*rango.Hasta))
	}
	qry += queryAbonosPorMesGroupOrder
	rows, err := q.QueryContext(ctx, qry, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var pts []outbound.PuntoMensual
	for rows.Next() {
		var raw abonoMesRowRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		pt, aerr := raw.assemble()
		if aerr != nil {
			return nil, aerr
		}
		pts = append(pts, pt)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return pts, nil
}

func (r *ClientesRepo) fetchCompradoVsAbonado(ctx context.Context, q firebird.Querier, clienteID int, rango outbound.RangoFechas) ([]outbound.PuntoCompradoAbonado, error) {
	compradoExtra, abonadoExtra := "", ""
	if rango.Desde != nil {
		compradoExtra += "\n    AND cargo.FECHA >= ?"
		abonadoExtra += "\n    AND abono.FECHA >= ?"
	}
	if rango.Hasta != nil {
		compradoExtra += "\n    AND cargo.FECHA <= ?"
		abonadoExtra += "\n    AND abono.FECHA <= ?"
	}
	qry := buildCompradoVsAbonadoQuery(compradoExtra, abonadoExtra)
	// Build args: clienteID (comprado branch) + optional dates, then clienteID
	// (abonado branch) + optional dates.
	args := []any{clienteID}
	if rango.Desde != nil {
		args = append(args, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		args = append(args, firebird.ToWallClock(*rango.Hasta))
	}
	args = append(args, clienteID)
	if rango.Desde != nil {
		args = append(args, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		args = append(args, firebird.ToWallClock(*rango.Hasta))
	}
	rows, err := q.QueryContext(ctx, qry, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var pts []outbound.PuntoCompradoAbonado
	for rows.Next() {
		var raw compradoVsAbonadoRowRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		pt, aerr := raw.assemble()
		if aerr != nil {
			return nil, aerr
		}
		pts = append(pts, pt)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return pts, nil
}

// ObtenerResumenFicha returns the pre-aggregated financial summary for a client.
// Uses a read-only snapshot transaction so all sub-queries see consistent data.
// rango optionally filters activity aggregations (TotalComprado, TotalAbonado,
// series) by date; SaldoTotal is always the live outstanding balance.
func (r *ClientesRepo) ObtenerResumenFicha(ctx context.Context, clienteID int, rango outbound.RangoFechas) (outbound.ResumenFicha, error) {
	var resumen outbound.ResumenFicha
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		totalComprado, totalAbonado, numVentas, numPagos, err := r.fetchFichaTotales(ctx, q, clienteID, rango)
		if err != nil {
			return err
		}
		resumen.TotalComprado = totalComprado
		resumen.TotalAbonado = totalAbonado
		resumen.NumVentas = numVentas
		resumen.NumPagos = numPagos

		// SaldoTotal sourced from the MSP_SALDOS_VENTAS cache (user-approved
		// exception) so the ficha saldo equals the directory saldo exactly,
		// rather than re-deriving TotalComprado − TotalAbonado natively.
		// SaldoTotal is NOT range-bounded — it is the live outstanding balance.
		saldoTotal, err := r.fetchFichaSaldo(ctx, q, clienteID)
		if err != nil {
			return err
		}
		resumen.SaldoTotal = saldoTotal
		if resumen.SaldoTotal.IsNegative() {
			resumen.SaldoTotal = decimal.Zero
		}

		// Derived fields.
		if numVentas > 0 && !totalComprado.IsZero() {
			resumen.TicketPromedio = totalComprado.Div(decimal.NewFromInt(int64(numVentas)))
		}
		if !totalComprado.IsZero() {
			resumen.PctLiquidado = totalAbonado.Div(totalComprado).Mul(decimal.NewFromInt(100))
		}

		pts, err := r.fetchAbonosPorMes(ctx, q, clienteID, rango)
		if err != nil {
			return err
		}
		resumen.AbonosPorMes = pts

		cva, err := r.fetchCompradoVsAbonado(ctx, q, clienteID, rango)
		if err != nil {
			return err
		}
		resumen.CompradoVsAbonado = cva
		return nil
	})
	if err != nil {
		return outbound.ResumenFicha{}, err
	}
	return resumen, nil
}

// ─── ListarVentas ─────────────────────────────────────────────────────────────

// ListarVentas returns a cursor-paginated list of sale headers for a client,
// ordered by sale date descending.
func (r *ClientesRepo) ListarVentas(
	ctx context.Context,
	clienteID int,
	p outbound.ListParams,
) (outbound.Page[*domain.VentaCliente], error) {
	pageSize := clampPageSize(p.PageSize)

	cursorFechaStr, cursorID, err := decodeCursorVentas(p.Cursor)
	if err != nil {
		slog.WarnContext(ctx, "clientesfb: invalid ventas cursor, starting from first page",
			"cursor", p.Cursor, "err", err)
		cursorFechaStr, cursorID = "", 0
	}

	args := []any{pageSize, clienteID}
	var extra string
	if cursorFechaStr != "" {
		// Parse stored RFC3339 string back to time.Time so we can bind
		// firebird.ToWallClock (a time.Time), not a raw string.
		// FECHA is a DATE column in DOCTOS_PV; the driver expects a time.Time
		// or its ToWallClock wrapper — not a string literal (see B3 research §5.1).
		cursorFecha, parseErr := time.Parse(time.RFC3339, cursorFechaStr)
		if parseErr != nil {
			slog.WarnContext(ctx, "clientesfb: ventas cursor fecha unparseable, starting from first page",
				"fechaStr", cursorFechaStr, "err", parseErr)
		} else {
			// Keyset descending: FECHA < cursor OR (FECHA = cursor AND DOCTO_PV_ID < cursorID)
			extra = " AND (pv.FECHA < ? OR (pv.FECHA = ? AND pv.DOCTO_PV_ID < ?))"
			fb := firebird.ToWallClock(cursorFecha)
			args = append(args, fb, fb, cursorID)
		}
	}
	query := queryListarVentasBase + extra + " ORDER BY pv.FECHA DESC, pv.DOCTO_PV_ID DESC"

	var items []*domain.VentaCliente
	err = firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, query, args...)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var raw ventaClienteRowRaw
			if serr := raw.scanFrom(rows); serr != nil {
				return firebird.MapError(serr)
			}
			v, aerr := raw.assemble()
			if aerr != nil {
				return aerr
			}
			items = append(items, v)
		}
		return rows.Err()
	})
	if err != nil {
		return outbound.Page[*domain.VentaCliente]{}, err
	}

	nextCursor := ""
	if len(items) == pageSize {
		last := items[len(items)-1]
		nextCursor = encodeCursorVentas(
			last.Fecha().Format(time.RFC3339),
			last.DoctoPVID(),
		)
	}
	return outbound.Page[*domain.VentaCliente]{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// ─── ObtenerVentaDetalle ──────────────────────────────────────────────────────

func (r *ClientesRepo) fetchProductos(ctx context.Context, q firebird.Querier, doctoPVID int) ([]*domain.ProductoVenta, error) {
	rows, err := q.QueryContext(ctx, queryProductos, doctoPVID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var productos []*domain.ProductoVenta
	for rows.Next() {
		var raw productoRowRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		prod, aerr := raw.assemble()
		if aerr != nil {
			return nil, aerr
		}
		productos = append(productos, prod)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return productos, nil
}

func (r *ClientesRepo) fetchContrato(ctx context.Context, q firebird.Querier, venta *domain.VentaCliente, doctoPVID int) (*outbound.ContratoCredito, error) {
	if !venta.Tipo().EsCredito() {
		return nil, nil //nolint:nilnil // nil = no contract data available
	}
	var cRaw contratoRowRaw
	serr := cRaw.scanFrom(q.QueryRowContext(ctx, queryContrato, doctoPVID))
	if errors.Is(serr, sql.ErrNoRows) {
		// Pre-2018 data: crédito sale with no LIBRES_CARGOS_CC row — acceptable.
		return nil, nil //nolint:nilnil // nil = no contract data available
	}
	if serr != nil {
		return nil, firebird.MapError(serr)
	}
	return cRaw.assemble()
}

func (r *ClientesRepo) fetchPagos(ctx context.Context, q firebird.Querier, doctoPVID int) ([]*domain.Pago, error) {
	rows, err := q.QueryContext(ctx, queryPagos, doctoPVID)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var pagos []*domain.Pago
	for rows.Next() {
		var raw pagoRowRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		pago, aerr := raw.assemble()
		if aerr != nil {
			return nil, aerr
		}
		pagos = append(pagos, pago)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return pagos, nil
}

// ObtenerVentaDetalle returns the full detail bundle for a single sale.
// Returns domain.ErrVentaNotFound when no row exists for doctoPVID.
// Wraps all sub-queries in a read transaction for a consistent snapshot.
func (r *ClientesRepo) ObtenerVentaDetalle(ctx context.Context, doctoPVID int) (outbound.VentaDetalle, error) {
	var result outbound.VentaDetalle
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		// 1. Header.
		var hdr ventaClienteRowRaw
		if serr := hdr.scanFrom(q.QueryRowContext(ctx, queryVentaHeader, doctoPVID)); serr != nil {
			if errors.Is(serr, sql.ErrNoRows) {
				return domain.ErrVentaNotFound
			}
			return firebird.MapError(serr)
		}
		venta, serr := hdr.assemble()
		if serr != nil {
			return serr
		}
		result.Venta = venta

		// 2. Productos.
		productos, err := r.fetchProductos(ctx, q, doctoPVID)
		if err != nil {
			return err
		}
		result.Productos = productos

		// 3. Contrato (only for crédito sales).
		contrato, err := r.fetchContrato(ctx, q, venta, doctoPVID)
		if err != nil {
			return err
		}
		result.Contrato = contrato

		// 4. Pagos.
		pagos, err := r.fetchPagos(ctx, q, doctoPVID)
		if err != nil {
			return err
		}
		result.Pagos = pagos
		return nil
	})
	if err != nil {
		return outbound.VentaDetalle{}, err
	}
	return result, nil
}

// fetchRitmoPagos returns individual payment rows for ObtenerRitmoPagoData.
// Each row is one IMPORTES_DOCTOS_CC abono; the optional date range filters by
// the payment date (abono.FECHA).
//
//nolint:dupl // structurally mirrors fetchRitmoVentas; differs in query, raw type, and return type — abstraction not worth it
func (r *ClientesRepo) fetchRitmoPagos(ctx context.Context, q firebird.Querier, clienteID int, rango outbound.RangoFechas) ([]domain.PagoCrudo, error) {
	qry := queryRitmoPagosBase
	args := []any{clienteID}
	if rango.Desde != nil {
		qry += "\n  AND abono.FECHA >= ?"
		args = append(args, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		qry += "\n  AND abono.FECHA <= ?"
		args = append(args, firebird.ToWallClock(*rango.Hasta))
	}
	qry += "\nORDER BY abono.FECHA"
	rows, err := q.QueryContext(ctx, qry, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var pagos []domain.PagoCrudo
	for rows.Next() {
		var raw pagoCrudoRowRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		p, aerr := raw.assemble()
		if aerr != nil {
			return nil, aerr
		}
		pagos = append(pagos, p)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return pagos, nil
}

// fetchRitmoVentas returns sale header rows for ObtenerRitmoPagoData.
// The optional date range filters by the sale date (pv.FECHA).
//
//nolint:dupl // structurally mirrors fetchRitmoPagos; differs in query, raw type, and return type — abstraction not worth it
func (r *ClientesRepo) fetchRitmoVentas(ctx context.Context, q firebird.Querier, clienteID int, rango outbound.RangoFechas) ([]domain.VentaCruda, error) {
	qry := queryRitmoVentasBase
	args := []any{clienteID}
	if rango.Desde != nil {
		qry += "\n  AND pv.FECHA >= ?"
		args = append(args, firebird.ToWallClock(*rango.Desde))
	}
	if rango.Hasta != nil {
		qry += "\n  AND pv.FECHA <= ?"
		args = append(args, firebird.ToWallClock(*rango.Hasta))
	}
	qry += "\nORDER BY pv.FECHA"
	rows, err := q.QueryContext(ctx, qry, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var ventas []domain.VentaCruda
	for rows.Next() {
		var raw ventaCrudaRowRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		v, aerr := raw.assemble()
		if aerr != nil {
			return nil, aerr
		}
		ventas = append(ventas, v)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return ventas, nil
}

// ─── ObtenerPagoDetalle ───────────────────────────────────────────────────────

// ObtenerPagoDetalle returns the rich detail for a single payment document.
// Returns domain.ErrPagoNotFound when no IMPORTES_DOCTOS_CC row with TIPO_IMPTE='R'
// exists for the given doctoCCID (the JOIN makes it a no-row result when the pago
// has no applied importes — the common not-found path).
func (r *ClientesRepo) ObtenerPagoDetalle(ctx context.Context, doctoCCID int) (outbound.PagoDetalle, error) {
	var result outbound.PagoDetalle
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		var raw pagoDetalleRowRaw
		if serr := raw.scanFrom(q.QueryRowContext(ctx, queryPagoDetalle, doctoCCID)); serr != nil {
			if errors.Is(serr, sql.ErrNoRows) {
				return domain.ErrPagoNotFound
			}
			return firebird.MapError(serr)
		}
		assembled, aerr := raw.assemble()
		if aerr != nil {
			return aerr
		}
		result = assembled
		return nil
	})
	if err != nil {
		return outbound.PagoDetalle{}, err
	}
	return result, nil
}

// ObtenerRitmoPagoData fetches the raw payment and sale data required to build
// the weekly payment-rhythm series for a client. Three sequential reads inside a
// read-only snapshot transaction: individual pagos, sale headers, and the live
// saldo from the MSP_SALDOS_VENTAS cache.
// Returns a zero-valued RitmoPagoData (not an error) when the client has no records.
func (r *ClientesRepo) ObtenerRitmoPagoData(ctx context.Context, clienteID int, rango outbound.RangoFechas) (outbound.RitmoPagoData, error) {
	var result outbound.RitmoPagoData
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		pagos, err := r.fetchRitmoPagos(ctx, q, clienteID, rango)
		if err != nil {
			return err
		}
		result.Pagos = pagos

		ventas, err := r.fetchRitmoVentas(ctx, q, clienteID, rango)
		if err != nil {
			return err
		}
		result.Ventas = ventas

		// Saldo actual is not range-bounded — always the live outstanding balance.
		// This makes the saldo series exact for the default unbounded window and only
		// approximate when a Desde/Hasta range is provided (see reconstruirSaldo).
		saldo, err := r.fetchFichaSaldo(ctx, q, clienteID)
		if err != nil {
			return err
		}
		result.SaldoActual = saldo
		return nil
	})
	if err != nil {
		return outbound.RitmoPagoData{}, err
	}
	return result, nil
}
