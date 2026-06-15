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

// ─── ListarDirectorio ─────────────────────────────────────────────────────────

// buildDirectorioQuery constructs the SQL query and argument list for ListarDirectorio.
// args[0] is always pageSize (the FIRST ? in the FIRST ? clause).
func buildDirectorioQuery(p outbound.ListParams, f outbound.FiltroDirectorio) (string, []any) {
	pageSize := clampPageSize(p.PageSize)
	cursorNombre, cursorID, err := decodeCursorDir(p.Cursor)
	if err != nil {
		cursorNombre, cursorID = "", 0
	}

	args := make([]any, 0, 8)
	args = append(args, pageSize)
	conditions := make([]string, 0, 4)

	if cursorNombre != "" {
		conditions = append(conditions,
			"(c.NOMBRE > ? OR (c.NOMBRE = ? AND c.CLIENTE_ID > ?))",
		)
		args = append(args, firebird.Win1252(cursorNombre), firebird.Win1252(cursorNombre), cursorID)
	}

	if len(f.ClienteIDs) > 0 {
		pageSize = len(f.ClienteIDs)
		args[0] = pageSize
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

	var query string
	if f.ConSaldo {
		// Wrap the inner (un-FIRST'd) query in a derived table so the outer
		// FIRST ? applies only to rows that already have SALDO_TOTAL > 0.
		// Without this, FIRST pageSize is applied before the saldo filter,
		// under-sizing pages and breaking the "has next page" detection.
		inner := queryListarDirectorioInner + andClause + " ORDER BY c.NOMBRE, c.CLIENTE_ID"
		query = "SELECT FIRST ? * FROM (" + inner + ") d WHERE d.SALDO_TOTAL > 0 ORDER BY d.NOMBRE, d.CLIENTE_ID"
	} else {
		query = queryListarDirectorioBase + andClause + " ORDER BY c.NOMBRE, c.CLIENTE_ID"
	}
	return query, args
}

// ListarDirectorio returns a cursor-paginated list of clients enriched with
// their total outstanding saldo. Applies optional filters from FiltroDirectorio.
func (r *ClientesRepo) ListarDirectorio(
	ctx context.Context,
	p outbound.ListParams,
	f outbound.FiltroDirectorio,
) (outbound.Page[outbound.DirectorioItem], error) {
	// Log warning for malformed cursor before building query.
	if _, _, err := decodeCursorDir(p.Cursor); err != nil {
		slog.WarnContext(ctx, "clientesfb: invalid directory cursor, starting from first page",
			"cursor", p.Cursor, "err", err)
	}

	query, args := buildDirectorioQuery(p, f)
	pageSize, _ := args[0].(int)

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
		return outbound.Page[outbound.DirectorioItem]{}, err
	}

	// Build next cursor from last item.
	nextCursor := ""
	if len(items) == pageSize {
		last := items[len(items)-1]
		nextCursor = encodeCursorDir(last.Cliente.Nombre(), last.Cliente.ClienteID())
	}
	return outbound.Page[outbound.DirectorioItem]{
		Items:      items,
		NextCursor: nextCursor,
	}, nil
}

// ─── ObtenerResumenFicha ──────────────────────────────────────────────────────

func (r *ClientesRepo) fetchFichaTotales(ctx context.Context, q firebird.Querier, clienteID int) (decimal.Decimal, decimal.Decimal, int, int, error) {
	var totRow resumenFichaTotalesRaw
	if serr := totRow.scanFrom(q.QueryRowContext(ctx, queryResumenFichaTotales, clienteID)); serr != nil {
		return decimal.Zero, decimal.Zero, 0, 0, firebird.MapError(serr)
	}
	return totRow.assemble()
}

func (r *ClientesRepo) fetchAbonosPorMes(ctx context.Context, q firebird.Querier, clienteID int) ([]outbound.PuntoMensual, error) {
	rows, err := q.QueryContext(ctx, queryAbonosPorMes, clienteID)
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

func (r *ClientesRepo) fetchCompradoVsAbonado(ctx context.Context, q firebird.Querier, clienteID int) ([]outbound.PuntoCompradoAbonado, error) {
	rows, err := q.QueryContext(ctx, queryCompradoVsAbonado, clienteID, clienteID)
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
func (r *ClientesRepo) ObtenerResumenFicha(ctx context.Context, clienteID int) (outbound.ResumenFicha, error) {
	var resumen outbound.ResumenFicha
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)

		totalComprado, totalAbonado, numVentas, numPagos, err := r.fetchFichaTotales(ctx, q, clienteID)
		if err != nil {
			return err
		}
		resumen.TotalComprado = totalComprado
		resumen.TotalAbonado = totalAbonado
		resumen.NumVentas = numVentas
		resumen.NumPagos = numPagos

		// Derived fields.
		resumen.SaldoTotal = totalComprado.Sub(totalAbonado)
		if resumen.SaldoTotal.IsNegative() {
			resumen.SaldoTotal = decimal.Zero
		}
		if numVentas > 0 && !totalComprado.IsZero() {
			resumen.TicketPromedio = totalComprado.Div(decimal.NewFromInt(int64(numVentas)))
		}
		if !totalComprado.IsZero() {
			resumen.PctLiquidado = totalAbonado.Div(totalComprado).Mul(decimal.NewFromInt(100))
		}

		pts, err := r.fetchAbonosPorMes(ctx, q, clienteID)
		if err != nil {
			return err
		}
		resumen.AbonosPorMes = pts

		cva, err := r.fetchCompradoVsAbonado(ctx, q, clienteID)
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

// ─── BuscarClienteIDsBasico ───────────────────────────────────────────────────

// BuscarClienteIDsBasico is the SQL LIKE fallback for client search.
// Returns up to limit matching client IDs ordered by name.
func (r *ClientesRepo) BuscarClienteIDsBasico(ctx context.Context, query string, limit int) ([]int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	likeArg := firebird.Win1252("%" + query + "%")
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, queryBuscarBasico, limit, likeArg)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int
	for rows.Next() {
		var id int
		if serr := rows.Scan(&id); serr != nil {
			return nil, firebird.MapError(serr)
		}
		ids = append(ids, id)
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return ids, nil
}

// ─── LeerDocumentosBusqueda ───────────────────────────────────────────────────

// LeerDocumentosBusqueda returns all active clients as SearchDocs for index
// warm-up and periodic refresh. Expects ~44k rows.
func (r *ClientesRepo) LeerDocumentosBusqueda(ctx context.Context) ([]outbound.SearchDoc, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, queryLeerDocumentos)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	// Pre-allocate for ~44k rows to avoid repeated slice growth.
	docs := make([]outbound.SearchDoc, 0, 45000)
	for rows.Next() {
		var raw searchDocRaw
		if serr := raw.scanFrom(rows); serr != nil {
			return nil, firebird.MapError(serr)
		}
		docs = append(docs, raw.assembleSearchDoc())
	}
	if rerr := rows.Err(); rerr != nil {
		return nil, firebird.MapError(rerr)
	}
	return docs, nil
}
