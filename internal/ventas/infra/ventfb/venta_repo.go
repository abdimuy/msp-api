//nolint:misspell // Spanish vocabulary (productos, etc.) by convention.
package ventfb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// errVendedorDuplicado is the apperror surfaced when an INSERT into
// MSP_VENTAS_VENDEDORES hits the UQ_MSP_VENTAS_VENDEDORES_VENTA_USR unique
// constraint, i.e. the same (venta_id, vendedor_usuario_id) pair is inserted
// twice.
var errVendedorDuplicado = apperror.NewConflict(
	"venta_vendedor_duplicado",
	"el vendedor ya está asignado a esta venta",
)

// VentaRepo is the Firebird-backed implementation of outbound.VentaRepo.
//
// Every method routes its queries through firebird.GetQuerier so it
// transparently joins an ambient transaction installed in the context (used
// by application services and the test harness) and otherwise falls back to
// the shared pool.
type VentaRepo struct {
	pool *firebird.Pool
}

// NewVentaRepo builds a VentaRepo wired to the given pool.
func NewVentaRepo(pool *firebird.Pool) *VentaRepo {
	return &VentaRepo{pool: pool}
}

// Compile-time check: VentaRepo satisfies the outbound port.
var _ outbound.VentaRepo = (*VentaRepo)(nil)

// Save inserts a new venta and every child row (combos → productos →
// vendedores → imágenes) honoring the FK order. All statements run on the
// same querier, so callers that already opened a transaction get atomicity
// for free; callers that did not pay the cost of best-effort cleanup if a
// later row fails (Firebird treats statement-level errors as recoverable
// inside a tx, so production paths should always wrap Save in a TxManager).
//
// Unique violations on UQ_MSP_VENTAS_VENDEDORES_VENTA_USR are mapped to the
// domain-meaningful errVendedorDuplicado apperror.
func (r *VentaRepo) Save(ctx context.Context, v *domain.Venta) error {
	return r.pool.ExecRetry(ctx, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		if err := r.insertHeader(ctx, q, v); err != nil {
			return err
		}
		if err := r.insertCombos(ctx, q, v); err != nil {
			return err
		}
		if err := r.insertProductos(ctx, q, v); err != nil {
			return err
		}
		if err := r.insertVendedores(ctx, q, v); err != nil {
			return err
		}
		return r.insertImagenes(ctx, q, v)
	})
}

func (r *VentaRepo) insertHeader(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
	args, err := headerInsertArgs(v)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, insertVenta, args...); err != nil {
		return firebird.MapError(err)
	}
	return nil
}

func (r *VentaRepo) insertCombos(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
	for _, c := range v.CombosForRepo() {
		a := c.Audit()
		// NOMBRE_COMBO is CHARACTER SET ISO8859_1 — encode UTF-8 → Win1252.
		nombreEnc, err := firebird.EncodeWin1252(c.Nombre())
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, insertCombo,
			c.ID().String(), v.ID().String(), nombreEnc,
			c.Precios().Anual(), c.Precios().CortoPlazo(), c.Precios().Contado(),
			c.Cantidad(), c.AlmacenOrigen(), c.AlmacenDestino(),
			firebird.ToWallClock(a.CreatedAt()), firebird.ToWallClock(a.UpdatedAt()),
			a.CreatedBy().String(), a.UpdatedBy().String(),
		); err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

func (r *VentaRepo) insertProductos(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
	for _, p := range v.ProductosForRepo() {
		var comboID any
		if p.ComboID() != nil {
			comboID = p.ComboID().String()
		}
		a := p.Audit()
		// ARTICULO is CHARACTER SET ISO8859_1 — encode UTF-8 → Win1252.
		articuloEnc, err := firebird.EncodeWin1252(p.Articulo())
		if err != nil {
			return err
		}
		if _, err := q.ExecContext(ctx, insertProducto,
			p.ID().String(), v.ID().String(),
			p.ArticuloID(), articuloEnc, p.Cantidad(),
			p.Precios().Anual(), p.Precios().CortoPlazo(), p.Precios().Contado(),
			comboID, nullableIntArg(p.AlmacenOrigen()), nullableIntArg(p.AlmacenDestino()),
			firebird.ToWallClock(a.CreatedAt()), firebird.ToWallClock(a.UpdatedAt()),
			a.CreatedBy().String(), a.UpdatedBy().String(),
		); err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

func (r *VentaRepo) insertVendedores(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
	for _, vd := range v.VendedoresForRepo() {
		a := vd.Audit()
		// VENDEDOR_NOMBRE is CHARACTER SET ISO8859_1 — encode UTF-8 → Win1252.
		nombreEnc, encErr := firebird.EncodeWin1252(vd.Snapshot().Nombre())
		if encErr != nil {
			return encErr
		}
		_, err := q.ExecContext(ctx, insertVendedor,
			vd.ID().String(), v.ID().String(),
			vd.Snapshot().UsuarioID().String(),
			vd.Snapshot().Email(), nombreEnc,
			firebird.ToWallClock(a.CreatedAt()), firebird.ToWallClock(a.UpdatedAt()),
			a.CreatedBy().String(), a.UpdatedBy().String(),
		)
		if err != nil {
			mapped := firebird.MapError(err)
			if isUniqueViolation(mapped) {
				return errVendedorDuplicado.WithError(err)
			}
			return mapped
		}
	}
	return nil
}

func (r *VentaRepo) insertImagenes(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
	for _, img := range v.ImagenesForRepo() {
		if err := execInsertImagen(ctx, q, v.ID(), img); err != nil {
			return err
		}
	}
	return nil
}

// headerInsertArgs builds the positional argument slice for insertVenta. The
// slice mirrors the column order in queries.go's insertVenta statement.
//
// ISO8859_1 text columns are encoded through Win1252 at this boundary.
//
//nolint:funlen // wide column set; keep all args in one place for readability.
func headerInsertArgs(v *domain.Venta) ([]any, error) {
	plazo, enganche, parcialidad, frec := planFields(v.PlanCredito())
	semana, mes := diaCobranzaFields(v.DiaCobranza())
	canceledAt, canceledBy, cancelReason, err := cancelacionFieldsEnc(v.Cancelacion())
	if err != nil {
		return nil, err
	}
	approvedAt, approvedBy := aprobacionFields(v.Aprobacion())
	a := v.Audit()

	// ISO8859_1 columns — encode UTF-8 → Win1252.
	nombreEnc, err := firebird.EncodeWin1252(v.Cliente().Nombre().Value())
	if err != nil {
		return nil, err
	}
	avalEnc, err := encodeNullableAval(v)
	if err != nil {
		return nil, err
	}
	calleEnc, err := firebird.EncodeWin1252(v.Direccion().Calle())
	if err != nil {
		return nil, err
	}
	numExtEnc, err := firebird.EncodeWin1252Ptr(v.Direccion().NumeroExterior())
	if err != nil {
		return nil, err
	}
	coloniaEnc, err := firebird.EncodeWin1252(v.Direccion().Colonia())
	if err != nil {
		return nil, err
	}
	poblacionEnc, err := firebird.EncodeWin1252(v.Direccion().Poblacion())
	if err != nil {
		return nil, err
	}
	ciudadEnc, err := firebird.EncodeWin1252(v.Direccion().Ciudad())
	if err != nil {
		return nil, err
	}
	notaEnc, err := firebird.EncodeWin1252Ptr(v.Nota())
	if err != nil {
		return nil, err
	}

	return []any{
		v.ID().String(), nombreEnc,
		nullableTelefonoArg(v), avalEnc,
		calleEnc,
		numExtEnc,
		coloniaEnc, poblacionEnc, ciudadEnc,
		nullableIntArg(v.Direccion().ZonaClienteID()),
		v.GPS().Latitud(), v.GPS().Longitud(),
		firebird.ToWallClock(v.FechaVenta()), v.TipoVenta().String(),
		v.Montos().Anual(), v.Montos().CortoPlazo(), v.Montos().Contado(),
		plazo, enganche, parcialidad, frec,
		semana, mes,
		notaEnc,
		firebird.ToWallClock(a.CreatedAt()), firebird.ToWallClock(a.UpdatedAt()),
		a.CreatedBy().String(), a.UpdatedBy().String(),
		canceledAt, canceledBy, cancelReason,
		nullableIntArg(v.ClienteID()), v.Status().String(),
		approvedAt, approvedBy,
	}, nil
}

// aprobacionFields decomposes an optional Aprobacion into the two nullable
// columns on MSP_VENTAS. The timestamp is wall-clock-shifted to BusinessTZ
// so Firebird stores it consistently with Microsip's convention.
//
//nolint:nonamedreturns // multi-arity tuples are clearer when named.
func aprobacionFields(a *domain.Aprobacion) (at, by any) {
	if a == nil {
		return nil, nil
	}
	return firebird.ToWallClock(a.At()), a.By().String()
}

func nullableTelefonoArg(v *domain.Venta) any {
	if v.Cliente().Telefono() == nil {
		return nil
	}
	return v.Cliente().Telefono().Value()
}

// encodeNullableAval encodes AVAL_O_RESPONSABLE (CHARACTER SET ISO8859_1) for
// SQL writes. Returns (nil, nil) when the venta has no aval — the SQL NULL
// representation for a nullable column.
func encodeNullableAval(v *domain.Venta) (driver.Value, error) {
	if v.Cliente().Aval() == nil {
		return nil, nil //nolint:nilnil // SQL NULL for nullable column; (nil, nil) is the standard pattern.
	}
	return firebird.EncodeWin1252(v.Cliente().Aval().Value())
}

func nullableIntArg(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// planFields decomposes a *domain.PlanCredito into the four NULL-or-value
// columns it spans on MSP_VENTAS (PLAZO_MESES, ENGANCHE, PARCIALIDAD,
// FREC_PAGO).
//
//nolint:nonamedreturns // multi-arity tuples are clearer when named.
func planFields(p *domain.PlanCredito) (plazo, enganche, parcialidad, frec any) {
	if p == nil {
		return nil, nil, nil, nil
	}
	return p.PlazoMeses(), p.Enganche(), p.Parcialidad(), p.FrecPago().String()
}

// diaCobranzaFields decomposes a *domain.DiaCobranza into the two NULL-or-
// value columns it spans on MSP_VENTAS (DIA_COBRANZA_SEMANA,
// DIA_COBRANZA_MES).
//
//nolint:nonamedreturns // multi-arity tuples are clearer when named.
func diaCobranzaFields(d *domain.DiaCobranza) (semana, mes any) {
	if d == nil {
		return nil, nil
	}
	if d.IsSemana() {
		semana = d.Semana().String()
	}
	if d.IsMes() {
		mes = *d.Mes()
	}
	return semana, mes
}

// cancelacionFields decomposes the optional Cancelacion into the three
// nullable columns on MSP_VENTAS. The timestamp is wall-clock-shifted to
// BusinessTZ so Firebird stores it consistently with Microsip's convention.
//
// cancelacionFieldsEnc is the Win1252-encoding variant used when building SQL
// args for CANCEL_REASON (CHARACTER SET ISO8859_1).
//
//nolint:nonamedreturns // multi-arity tuples are clearer when named.
func cancelacionFieldsEnc(c *domain.Cancelacion) (at, by, reason any, err error) {
	if c == nil {
		return nil, nil, nil, nil
	}
	reasonEnc, encErr := firebird.EncodeWin1252(c.Reason())
	if encErr != nil {
		return nil, nil, nil, encErr
	}
	return firebird.ToWallClock(c.At()), c.By().String(), reasonEnc, nil
}

// Update writes back the cancellation triplet plus STATUS and the audit
// fields. Used for the Cancelar path.
func (r *VentaRepo) Update(ctx context.Context, v *domain.Venta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	canceledAt, canceledBy, cancelReason, err := cancelacionFieldsEnc(v.Cancelacion())
	if err != nil {
		return err
	}
	a := v.Audit()
	res, err := q.ExecContext(ctx, updateVentaHeader,
		canceledAt, canceledBy, cancelReason,
		v.Status().String(),
		firebird.ToWallClock(a.UpdatedAt()), a.UpdatedBy().String(),
		v.ID().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return ensureRowAffected(res, domain.ErrVentaNotFound)
}

// ensureRowAffected maps RowsAffected==0 to notFound and propagates driver
// errors via MapError.
func ensureRowAffected(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}

// UpdateHeader rewrites the editable header fields of v. Used by
// ActualizarHeader.
//
//nolint:funlen // wide column set; splitting buys nothing.
func (r *VentaRepo) UpdateHeader(ctx context.Context, v *domain.Venta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	plazo, enganche, parcialidad, frec := planFields(v.PlanCredito())
	semana, mes := diaCobranzaFields(v.DiaCobranza())
	a := v.Audit()

	// ISO8859_1 columns — encode UTF-8 → Win1252.
	calleEnc, err := firebird.EncodeWin1252(v.Direccion().Calle())
	if err != nil {
		return err
	}
	numExtEnc, err := firebird.EncodeWin1252Ptr(v.Direccion().NumeroExterior())
	if err != nil {
		return err
	}
	coloniaEnc, err := firebird.EncodeWin1252(v.Direccion().Colonia())
	if err != nil {
		return err
	}
	poblacionEnc, err := firebird.EncodeWin1252(v.Direccion().Poblacion())
	if err != nil {
		return err
	}
	ciudadEnc, err := firebird.EncodeWin1252(v.Direccion().Ciudad())
	if err != nil {
		return err
	}
	notaEnc, err := firebird.EncodeWin1252Ptr(v.Nota())
	if err != nil {
		return err
	}

	res, err := q.ExecContext(ctx, updateVentaHeaderFull,
		calleEnc,
		numExtEnc,
		coloniaEnc, poblacionEnc, ciudadEnc,
		nullableIntArg(v.Direccion().ZonaClienteID()),
		v.GPS().Latitud(), v.GPS().Longitud(),
		firebird.ToWallClock(v.FechaVenta()),
		v.Montos().Anual(), v.Montos().CortoPlazo(), v.Montos().Contado(),
		plazo, enganche, parcialidad, frec,
		semana, mes,
		notaEnc,
		firebird.ToWallClock(a.UpdatedAt()), a.UpdatedBy().String(),
		v.ID().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return ensureRowAffected(res, domain.ErrVentaNotFound)
}

// UpdateCliente rewrites the cliente snapshot + cliente_id link.
func (r *VentaRepo) UpdateCliente(ctx context.Context, v *domain.Venta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	a := v.Audit()

	// NOMBRE_CLIENTE and AVAL_O_RESPONSABLE are CHARACTER SET ISO8859_1.
	nombreEnc, err := firebird.EncodeWin1252(v.Cliente().Nombre().Value())
	if err != nil {
		return err
	}
	avalEnc, err := encodeNullableAval(v)
	if err != nil {
		return err
	}

	res, err := q.ExecContext(ctx, updateVentaCliente,
		nullableIntArg(v.ClienteID()),
		nombreEnc,
		nullableTelefonoArg(v), avalEnc,
		firebird.ToWallClock(a.UpdatedAt()), a.UpdatedBy().String(),
		v.ID().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return ensureRowAffected(res, domain.ErrVentaNotFound)
}

// ReplaceProductos deletes existing producto rows for v and re-inserts the
// current slice. Intended to run inside a TxManager-managed transaction.
func (r *VentaRepo) ReplaceProductos(ctx context.Context, v *domain.Venta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteProductosByVenta, v.ID().String()); err != nil {
		return firebird.MapError(err)
	}
	if err := r.insertProductos(ctx, q, v); err != nil {
		return err
	}
	return r.touchHeader(ctx, q, v)
}

// ReplaceCombos deletes existing combo rows for v and re-inserts the
// current slice.
func (r *VentaRepo) ReplaceCombos(ctx context.Context, v *domain.Venta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteCombosByVenta, v.ID().String()); err != nil {
		return firebird.MapError(err)
	}
	if err := r.insertCombos(ctx, q, v); err != nil {
		return err
	}
	return r.touchHeader(ctx, q, v)
}

// ReplaceVendedores deletes existing vendedor rows for v and re-inserts the
// current slice.
func (r *VentaRepo) ReplaceVendedores(ctx context.Context, v *domain.Venta) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	if _, err := q.ExecContext(ctx, deleteVendedoresByVenta, v.ID().String()); err != nil {
		return firebird.MapError(err)
	}
	if err := r.insertVendedores(ctx, q, v); err != nil {
		return err
	}
	return r.touchHeader(ctx, q, v)
}

// touchHeader writes the venta's updated_at/by so the audit trail reflects
// the child-collection replacement.
func (r *VentaRepo) touchHeader(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
	a := v.Audit()
	res, err := q.ExecContext(ctx,
		`UPDATE MSP_VENTAS SET UPDATED_AT = ?, UPDATED_BY = ? WHERE ID = ?`,
		firebird.ToWallClock(a.UpdatedAt()), a.UpdatedBy().String(), v.ID().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return ensureRowAffected(res, domain.ErrVentaNotFound)
}

// FindByID loads a venta with its full children collection populated. It
// performs five queries on the same querier — header + four batched
// WHERE VENTA_ID = ? reads — so the result is consistent within the active
// transaction.
func (r *VentaRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Venta, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	raw, err := loadHeaderRaw(ctx, q, id)
	if err != nil {
		return nil, err
	}
	combos, err := loadCombos(ctx, q, id)
	if err != nil {
		return nil, err
	}
	productos, err := loadProductos(ctx, q, id)
	if err != nil {
		return nil, err
	}
	vendedores, err := loadVendedores(ctx, q, id)
	if err != nil {
		return nil, err
	}
	imagenes, err := loadImagenes(ctx, q, id)
	if err != nil {
		return nil, err
	}
	return assembleVenta(raw, combos, productos, vendedores, imagenes)
}

func loadHeaderRaw(ctx context.Context, q firebird.Querier, id uuid.UUID) (*ventaRowRaw, error) {
	row := q.QueryRowContext(ctx, selectVentaByID, id.String())
	raw, err := scanVentaRowRaw(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, domain.ErrVentaNotFound
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}
	return raw, nil
}

func loadCombos(ctx context.Context, q firebird.Querier, ventaID uuid.UUID) ([]*domain.Combo, error) {
	rows, err := q.QueryContext(ctx, selectCombosByVenta, ventaID.String())
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*domain.Combo, 0)
	for rows.Next() {
		c, scanErr := scanCombo(rows)
		if scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

func loadProductos(ctx context.Context, q firebird.Querier, ventaID uuid.UUID) ([]*domain.Producto, error) {
	rows, err := q.QueryContext(ctx, selectProductosByVenta, ventaID.String())
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*domain.Producto, 0)
	for rows.Next() {
		p, scanErr := scanProducto(rows)
		if scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

func loadVendedores(ctx context.Context, q firebird.Querier, ventaID uuid.UUID) ([]*domain.Vendedor, error) {
	rows, err := q.QueryContext(ctx, selectVendedoresByVenta, ventaID.String())
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*domain.Vendedor, 0)
	for rows.Next() {
		v, scanErr := scanVendedor(rows)
		if scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

func loadImagenes(ctx context.Context, q firebird.Querier, ventaID uuid.UUID) ([]*domain.Imagen, error) {
	rows, err := q.QueryContext(ctx, selectImagenesByVenta, ventaID.String())
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	out := make([]*domain.Imagen, 0)
	for rows.Next() {
		img, scanErr := scanImagen(rows)
		if scanErr != nil {
			return nil, firebird.MapError(scanErr)
		}
		out = append(out, img)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// List returns a cursor-paginated page of ventas matching f. Children are
// loaded for every venta on the page (4 batched queries shared across the
// page) so the response carries a fully hydrated aggregate; pageSize is
// bounded by clampPageSize so this stays cheap.
func (r *VentaRepo) List(
	ctx context.Context,
	p outbound.ListParams,
	f outbound.ListVentasFilters,
) (outbound.Page[*domain.Venta], error) {
	size := clampPageSize(p.PageSize)
	curT, curID, err := decodeCursor(p.Cursor)
	if err != nil {
		return outbound.Page[*domain.Venta]{}, err
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	// The driver writes time parameters as wall-clock fields without TZ
	// conversion. Re-stamp our UTC cursor and filter values in BusinessTZ
	// so the predicate compares like-with-like against the rows Firebird
	// stored (also wall-clock in BusinessTZ).
	curTWall := firebird.ToWallClock(curT)
	query, args := buildListQuery(size+1, p.Cursor != "", curTWall, curID, f)
	rows, qErr := q.QueryContext(ctx, query, args...)
	if qErr != nil {
		return outbound.Page[*domain.Venta]{}, firebird.MapError(qErr)
	}
	headers, scanErr := scanListRows(rows)
	if scanErr != nil {
		return outbound.Page[*domain.Venta]{}, scanErr
	}
	return r.hydrateListPage(ctx, q, headers, size)
}

// scanListRows iterates rows and produces the slice of raw venta rows.
func scanListRows(rows *sql.Rows) ([]*ventaRowRaw, error) {
	defer func() { _ = rows.Close() }()
	out := make([]*ventaRowRaw, 0)
	for rows.Next() {
		raw, err := scanVentaRowRaw(rows)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		out = append(out, raw)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// hydrateListPage takes the scanned header rows, trims the size+1 sentinel
// row, batches the child loads, and builds the outbound.Page.
func (r *VentaRepo) hydrateListPage(
	ctx context.Context,
	q firebird.Querier,
	headers []*ventaRowRaw,
	size int,
) (outbound.Page[*domain.Venta], error) {
	var nextCursor string
	if len(headers) > size {
		cursor, err := nextCursorFromLast(headers[size-1])
		if err != nil {
			return outbound.Page[*domain.Venta]{}, err
		}
		nextCursor = cursor
		headers = headers[:size]
	}
	if len(headers) == 0 {
		return outbound.Page[*domain.Venta]{Items: []*domain.Venta{}, NextCursor: nextCursor}, nil
	}
	items, err := r.assembleListItems(ctx, q, headers)
	if err != nil {
		return outbound.Page[*domain.Venta]{}, err
	}
	return outbound.Page[*domain.Venta]{Items: items, NextCursor: nextCursor}, nil
}

// nextCursorFromLast builds the opaque cursor pointing at the last item on
// the current page. The cursor's payload is the (FECHA_VENTA, ID) tuple of
// that row so subsequent List calls can resume the DESC scan after it.
func nextCursorFromLast(last *ventaRowRaw) (string, error) {
	id, err := uuid.Parse(last.idRaw)
	if err != nil {
		return "", apperror.NewInternal(
			"firebird_uuid_invalid",
			"uuid inválido en columna de base de datos",
		).WithSource("firebird").WithError(err).WithField("column", "ID")
	}
	fv, err := firebird.ScanUTCTime(last.fechaVentaRaw)
	if err != nil {
		return "", err
	}
	return encodeCursor(fv, id), nil
}

// assembleListItems loads the four child collections for every venta on the
// page and combines them with the scanned headers into hydrated aggregates.
func (r *VentaRepo) assembleListItems(
	ctx context.Context,
	q firebird.Querier,
	headers []*ventaRowRaw,
) ([]*domain.Venta, error) {
	items := make([]*domain.Venta, 0, len(headers))
	for _, raw := range headers {
		id, err := uuid.Parse(raw.idRaw)
		if err != nil {
			return nil, apperror.NewInternal(
				"firebird_uuid_invalid",
				"uuid inválido en columna de base de datos",
			).WithSource("firebird").WithError(err).WithField("column", "ID")
		}
		combos, err := loadCombos(ctx, q, id)
		if err != nil {
			return nil, err
		}
		productos, err := loadProductos(ctx, q, id)
		if err != nil {
			return nil, err
		}
		vendedores, err := loadVendedores(ctx, q, id)
		if err != nil {
			return nil, err
		}
		imagenes, err := loadImagenes(ctx, q, id)
		if err != nil {
			return nil, err
		}
		v, err := assembleVenta(raw, combos, productos, vendedores, imagenes)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, nil
}

// buildListQuery composes the dynamic SELECT for List from the configured
// filter set. Conditions are appended in a fixed order (filters before the
// cursor predicate) and bound to the returned args slice in the same order.
//
//nolint:nonamedreturns // multi-result tuple is clearer when named.
func buildListQuery(
	limit int,
	hasCursor bool,
	curT any,
	curID uuid.UUID,
	f outbound.ListVentasFilters,
) (sqlText string, args []any) {
	args = []any{limit}
	conds := []string{}
	args, conds = appendVendedorFilter(args, conds, f)
	args, conds = appendTipoVentaFilter(args, conds, f)
	args, conds = appendClienteIDFilter(args, conds, f)
	args, conds = appendDesdeFilter(args, conds, f)
	args, conds = appendHastaFilter(args, conds, f)
	conds = appendCanceladasFilter(conds, f)
	if hasCursor {
		conds = append(conds, cursorPredicateDesc)
		args = append(args, curT, curT, curID.String())
	}
	sqlText = selectVentasBase
	if len(conds) > 0 {
		sqlText += "WHERE " + strings.Join(conds, " AND ") + " "
	}
	sqlText += orderClause
	return sqlText, args
}

//nolint:nonamedreturns // multi-result tuple is clearer when named.
func appendVendedorFilter(
	args []any, conds []string, f outbound.ListVentasFilters,
) (nextArgs []any, nextConds []string) {
	if f.VendedorUsuarioID == nil {
		return args, conds
	}
	conds = append(conds,
		"EXISTS (SELECT 1 FROM MSP_VENTAS_VENDEDORES vd "+
			"WHERE vd.VENTA_ID = v.ID AND vd.VENDEDOR_USUARIO_ID = ?)")
	args = append(args, f.VendedorUsuarioID.String())
	return args, conds
}

//nolint:nonamedreturns // multi-result tuple is clearer when named.
func appendTipoVentaFilter(
	args []any, conds []string, f outbound.ListVentasFilters,
) (nextArgs []any, nextConds []string) {
	if f.TipoVenta == "" {
		return args, conds
	}
	conds = append(conds, "v.TIPO_VENTA = ?")
	args = append(args, f.TipoVenta)
	return args, conds
}

//nolint:nonamedreturns // multi-result tuple is clearer when named.
func appendClienteIDFilter(
	args []any, conds []string, f outbound.ListVentasFilters,
) (nextArgs []any, nextConds []string) {
	if f.ClienteID == nil {
		return args, conds
	}
	conds = append(conds, "v.CLIENTE_ID = ?")
	args = append(args, *f.ClienteID)
	return args, conds
}

//nolint:nonamedreturns // multi-result tuple is clearer when named.
func appendDesdeFilter(
	args []any, conds []string, f outbound.ListVentasFilters,
) (nextArgs []any, nextConds []string) {
	if f.Desde == nil {
		return args, conds
	}
	conds = append(conds, "v.FECHA_VENTA >= ?")
	args = append(args, firebird.ToWallClock(*f.Desde))
	return args, conds
}

//nolint:nonamedreturns // multi-result tuple is clearer when named.
func appendHastaFilter(
	args []any, conds []string, f outbound.ListVentasFilters,
) (nextArgs []any, nextConds []string) {
	if f.Hasta == nil {
		return args, conds
	}
	conds = append(conds, "v.FECHA_VENTA < ?")
	args = append(args, firebird.ToWallClock(*f.Hasta))
	return args, conds
}

func appendCanceladasFilter(
	conds []string, f outbound.ListVentasFilters,
) []string {
	if f.IncluirCanceladas {
		return conds
	}
	return append(conds, "v.CANCELED_AT IS NULL")
}

// InsertImagen persists a single new imagen child for the given venta. Used
// by AdjuntarImagen which adds images one at a time.
func (r *VentaRepo) InsertImagen(ctx context.Context, ventaID uuid.UUID, img *domain.Imagen) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	return execInsertImagen(ctx, q, ventaID, img)
}

// execInsertImagen runs the imagen INSERT against the supplied querier.
func execInsertImagen(
	ctx context.Context,
	q firebird.Querier,
	ventaID uuid.UUID,
	img *domain.Imagen,
) error {
	a := img.Audit()
	// DESCRIPCION is nullable and CHARACTER SET ISO8859_1 — encode UTF-8 → Win1252.
	descEnc, err := firebird.EncodeWin1252Ptr(img.Descripcion())
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, insertImagen,
		img.ID().String(), ventaID.String(),
		img.Storage().Kind().String(), img.Storage().Key(),
		img.Mime(), img.SizeBytes(),
		descEnc,
		firebird.ToWallClock(a.CreatedAt()), firebird.ToWallClock(a.UpdatedAt()),
		a.CreatedBy().String(), a.UpdatedBy().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// DeleteImagen removes a single imagen child by primary key. Returns
// ErrImagenNotFound when no row matches.
func (r *VentaRepo) DeleteImagen(ctx context.Context, ventaID, imagenID uuid.UUID) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	res, err := q.ExecContext(ctx, deleteImagen, imagenID.String(), ventaID.String())
	if err != nil {
		return firebird.MapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}
	if n == 0 {
		return domain.ErrImagenNotFound
	}
	return nil
}

// isUniqueViolation reports whether err — already passed through MapError —
// is the Firebird unique-violation apperror.
func isUniqueViolation(err error) bool {
	appErr, ok := apperror.As(err)
	if !ok {
		return false
	}
	return appErr.Code == "firebird_unique_violation"
}
