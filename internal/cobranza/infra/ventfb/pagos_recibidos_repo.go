//nolint:misspell // Microsip table/column identifiers in Spanish are kept verbatim.
package ventfb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertions: the repo satisfies both outbound ports it claims.
var (
	_ outbound.PagosRecibidosRepo = (*PagosRecibidosRepo)(nil)
	_ outbound.PagosImagenesRepo  = (*PagosRecibidosRepo)(nil)
)

// estado is the single-character Firebird column value matching a
// Sincronizacion enum value.
//
//	SincronizacionPendiente → 'P'
//	SincronizacionAplicada  → 'A'
const (
	estadoPendiente = "P"
	estadoAplicada  = "A"
)

func estadoFromSincronizacion(s domain.Sincronizacion) string {
	if s == domain.SincronizacionAplicada {
		return estadoAplicada
	}
	return estadoPendiente
}

func sincronizacionFromEstado(e string) domain.Sincronizacion {
	if e == estadoAplicada {
		return domain.SincronizacionAplicada
	}
	return domain.SincronizacionPendiente
}

// PagosRecibidosRepo implements outbound.PagosRecibidosRepo and
// outbound.PagosImagenesRepo backed by MSP_PAGOS_RECIBIDOS and
// MSP_PAGOS_IMAGENES in Firebird.
//
// The repo is transaction-aware via firebird.GetQuerier: when ctx carries
// an active tx, all SQL routes through it; otherwise the connection pool
// is used directly. Callers (AplicarPago, PagoRetryWorker) are responsible
// for spanning the transaction boundary.
type PagosRecibidosRepo struct {
	pool *firebird.Pool
}

// NewPagosRecibidosRepo builds a PagosRecibidosRepo wired to the given pool.
func NewPagosRecibidosRepo(pool *firebird.Pool) *PagosRecibidosRepo {
	return &PagosRecibidosRepo{pool: pool}
}

// ─── SQL: MSP_PAGOS_RECIBIDOS ───────────────────────────────────────────────

const selectPagoRecibidoCols = `
	ID,
	CARGO_DOCTO_CC_ID,
	CLIENTE_ID,
	COBRADOR_ID,
	COBRADOR,
	IMPORTE,
	FORMA_COBRO_ID,
	CONCEPTO_CC_ID,
	FECHA,
	LAT,
	LON,
	ESTADO,
	INTENTOS,
	ULTIMO_ERROR,
	DOCTO_CC_ID,
	IMPTE_DOCTO_CC_ID,
	FOLIO,
	RECEIVED_AT,
	APLICADO_AT,
	CREATED_BY,
	UPDATED_BY,
	UPDATED_AT`

const insertPagoRecibidoSQL = `
INSERT INTO MSP_PAGOS_RECIBIDOS (
	ID,
	CARGO_DOCTO_CC_ID, CLIENTE_ID, COBRADOR_ID, COBRADOR,
	IMPORTE, FORMA_COBRO_ID, CONCEPTO_CC_ID,
	FECHA, LAT, LON,
	ESTADO, INTENTOS, ULTIMO_ERROR,
	DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, FOLIO,
	RECEIVED_AT, APLICADO_AT,
	CREATED_BY, UPDATED_BY, UPDATED_AT
) VALUES (
	?,
	?, ?, ?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?, ?,
	?, ?,
	?, ?, ?
)`

const updatePagoRecibidoSQL = `
UPDATE MSP_PAGOS_RECIBIDOS SET
	ESTADO            = ?,
	INTENTOS          = ?,
	ULTIMO_ERROR      = ?,
	DOCTO_CC_ID       = ?,
	IMPTE_DOCTO_CC_ID = ?,
	FOLIO             = ?,
	APLICADO_AT       = ?,
	UPDATED_BY        = ?,
	UPDATED_AT        = ?
WHERE ID = ?`

const lockPagoRecibidoSQL = `SELECT 1 FROM MSP_PAGOS_RECIBIDOS WHERE ID = ? WITH LOCK`

const findPagoRecibidoByIDSQL = `
SELECT ` + selectPagoRecibidoCols + `
FROM MSP_PAGOS_RECIBIDOS
WHERE ID = ?`

const listPagosRecibidosPendientesSQL = `
SELECT FIRST ? ` + selectPagoRecibidoCols + `
FROM MSP_PAGOS_RECIBIDOS
WHERE ESTADO = '` + estadoPendiente + `' AND INTENTOS < ?
ORDER BY RECEIVED_AT ASC, ID ASC`

// ─── PagosRecibidosRepo methods ─────────────────────────────────────────────

// Insert persists a new PagoRecibido. The repo trusts that p has been
// constructed via NewPagoRecibido (so state == pendiente, intentos == 0).
func (r *PagosRecibidosRepo) Insert(ctx context.Context, p *domain.PagoRecibido) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	a := p.Audit()
	args := []any{
		p.ID().String(),
		p.CargoDoctoCCID(), p.ClienteID(), p.CobradorID(), p.Cobrador(),
		p.Importe(), p.FormaCobroID(), p.ConceptoCCID(),
		firebird.ToWallClock(p.FechaHoraPago()), p.Lat(), p.Lon(),
		estadoFromSincronizacion(p.Sincronizacion()), p.Intentos(), p.UltimoError(),
		p.DoctoCCID(), p.ImpteDoctoCCID(), p.Folio(),
		firebird.ToWallClock(p.ReceivedAt()), nullableWallClock(p.AplicadoAt()),
		a.CreatedBy().String(), a.UpdatedBy().String(), firebird.ToWallClock(a.UpdatedAt()),
	}
	_, err := q.ExecContext(ctx, insertPagoRecibidoSQL, args...)
	if err != nil {
		mapped := firebird.MapError(err)
		// Duplicate primary key → idempotency conflict. The platform mapper
		// returns the generic "firebird_unique_violation" Conflict; surface
		// it as the domain-typed ErrPagoYaExiste so callers can fast-path.
		var ae *apperror.Error
		if errors.As(mapped, &ae) && ae.Code == "firebird_unique_violation" {
			return domain.ErrPagoYaExiste
		}
		return mapped
	}
	return nil
}

// Update persists state changes (MarcarAplicada / RegistrarFallo). Returns
// ErrPagoNoEncontrado when no row matches the ID.
func (r *PagosRecibidosRepo) Update(ctx context.Context, p *domain.PagoRecibido) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	a := p.Audit()
	args := []any{
		estadoFromSincronizacion(p.Sincronizacion()),
		p.Intentos(),
		p.UltimoError(),
		p.DoctoCCID(),
		p.ImpteDoctoCCID(),
		p.Folio(),
		nullableWallClock(p.AplicadoAt()),
		a.UpdatedBy().String(),
		firebird.ToWallClock(a.UpdatedAt()),
		p.ID().String(),
	}
	res, err := q.ExecContext(ctx, updatePagoRecibidoSQL, args...)
	if err != nil {
		return firebird.MapError(err)
	}
	return ensureRowAffectedPago(res, domain.ErrPagoNoEncontrado)
}

// LockByID acquires SELECT … WITH LOCK on the row. Must be inside a tx.
func (r *PagosRecibidosRepo) LockByID(ctx context.Context, id uuid.UUID) error {
	if !firebird.HasTx(ctx) {
		return firebird.ErrNoTx
	}
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var one int
	err := q.QueryRowContext(ctx, lockPagoRecibidoSQL, id.String()).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ErrPagoNoEncontrado
	}
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// FindByID loads a PagoRecibido with its imagenes child collection.
func (r *PagosRecibidosRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.PagoRecibido, error) {
	var result *domain.PagoRecibido
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, findPagoRecibidoByIDSQL, id.String())
		p, serr := scanPagoRecibidoRow(row)
		if serr != nil {
			return serr
		}
		imgs, serr := r.ListImagenes(ctx, id)
		if serr != nil {
			return serr
		}
		result = hydrateWithImagenes(p, imgs)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListPendientes drains the outbox: pendientes with intentos < maxIntentos,
// ordered by RECEIVED_AT ascending (oldest first) for fair processing.
// Imagenes are NOT loaded — the retry worker doesn't need them.
func (r *PagosRecibidosRepo) ListPendientes(ctx context.Context, maxIntentos, limit int) ([]*domain.PagoRecibido, error) {
	var out []*domain.PagoRecibido
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, listPagosRecibidosPendientesSQL, limit, maxIntentos)
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			p, scanErr := scanPagoRecibidoRowFromRows(rows)
			if scanErr != nil {
				return scanErr
			}
			out = append(out, p)
		}
		if serr := rows.Err(); serr != nil {
			return firebird.MapError(serr)
		}
		return nil
	})
	return out, err
}

// ─── PagosImagenesRepo methods (child collection) ────────────────────────

const insertPagoImagenSQL = `
INSERT INTO MSP_PAGOS_IMAGENES (
	ID, PAGO_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
	CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const deletePagoImagenSQL = `DELETE FROM MSP_PAGOS_IMAGENES WHERE ID = ?`

const selectPagoImagenCols = `
	ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
	CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const findPagoImagenByIDSQL = `
SELECT ` + selectPagoImagenCols + `
FROM MSP_PAGOS_IMAGENES
WHERE ID = ?`

const listPagoImagenesSQL = `
SELECT ` + selectPagoImagenCols + `
FROM MSP_PAGOS_IMAGENES
WHERE PAGO_ID = ?
ORDER BY CREATED_AT ASC, ID ASC`

// InsertImagen persists a row in MSP_PAGOS_IMAGENES.
func (r *PagosRecibidosRepo) InsertImagen(ctx context.Context, pagoID uuid.UUID, img *domain.Imagen) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	a := img.Audit()
	storage := img.Storage()
	_, err := q.ExecContext(
		ctx, insertPagoImagenSQL,
		img.ID().String(),
		pagoID.String(),
		storage.Kind().String(),
		storage.Key(),
		img.Mime(),
		img.SizeBytes(),
		img.Descripcion(),
		firebird.ToWallClock(a.CreatedAt()),
		firebird.ToWallClock(a.UpdatedAt()),
		a.CreatedBy().String(),
		a.UpdatedBy().String(),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// DeleteImagen removes a single row from MSP_PAGOS_IMAGENES.
func (r *PagosRecibidosRepo) DeleteImagen(ctx context.Context, imagenID uuid.UUID) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	res, err := q.ExecContext(ctx, deletePagoImagenSQL, imagenID.String())
	if err != nil {
		return firebird.MapError(err)
	}
	return ensureRowAffectedPago(res, domain.ErrImagenNoEncontrada)
}

// FindImagenByID loads a single comprobante row.
func (r *PagosRecibidosRepo) FindImagenByID(ctx context.Context, imagenID uuid.UUID) (*domain.Imagen, error) {
	var result *domain.Imagen
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, findPagoImagenByIDSQL, imagenID.String())
		var serr error
		result, serr = scanPagoImagenRow(row)
		return serr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ListImagenes returns every comprobante attached to pagoID.
func (r *PagosRecibidosRepo) ListImagenes(ctx context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error) {
	var out []*domain.Imagen
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, qerr := q.QueryContext(ctx, listPagoImagenesSQL, pagoID.String())
		if qerr != nil {
			return firebird.MapError(qerr)
		}
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			img, scanErr := scanPagoImagenRowFromRows(rows)
			if scanErr != nil {
				return scanErr
			}
			out = append(out, img)
		}
		if serr := rows.Err(); serr != nil {
			return firebird.MapError(serr)
		}
		return nil
	})
	return out, err
}

// ─── Scanning helpers ───────────────────────────────────────────────────────

// scanPagoRecibidoRaw mirrors selectPagoRecibidoCols positionally with raw
// scan targets that the driver fills before we re-interpret them.
type scanPagoRecibidoRaw struct {
	id             string
	cargoDoctoCCID sql.NullInt64
	clienteID      sql.NullInt64
	cobradorID     sql.NullInt64
	cobrador       sql.NullString
	importeRaw     any
	formaCobroID   sql.NullInt64
	conceptoCCID   sql.NullInt64
	fechaRaw       any
	lat            sql.NullString
	lon            sql.NullString
	estado         string
	intentos       int
	ultimoError    sql.NullString
	doctoCCID      sql.NullInt64
	impteDoctoCCID sql.NullInt64
	folio          sql.NullString
	receivedAtRaw  any
	aplicadoAtRaw  any
	createdBy      sql.NullString
	updatedBy      sql.NullString
	updatedAtRaw   any
}

func (s *scanPagoRecibidoRaw) targets() []any {
	return []any{
		&s.id,
		&s.cargoDoctoCCID, &s.clienteID, &s.cobradorID, &s.cobrador,
		&s.importeRaw, &s.formaCobroID, &s.conceptoCCID,
		&s.fechaRaw, &s.lat, &s.lon,
		&s.estado, &s.intentos, &s.ultimoError,
		&s.doctoCCID, &s.impteDoctoCCID, &s.folio,
		&s.receivedAtRaw, &s.aplicadoAtRaw,
		&s.createdBy, &s.updatedBy, &s.updatedAtRaw,
	}
}

func (s *scanPagoRecibidoRaw) hydrate() (*domain.PagoRecibido, error) {
	id, err := uuid.Parse(s.id)
	if err != nil {
		return nil, apperror.NewInternal("pago_id_invalido", "id de pago inválido en la base de datos").WithError(err)
	}
	importe := scanImporteOrZero(s.importeRaw)
	fecha, err := firebird.ScanUTCTime(s.fechaRaw)
	if err != nil {
		return nil, err
	}
	receivedAt, err := firebird.ScanUTCTime(s.receivedAtRaw)
	if err != nil {
		return nil, err
	}
	aplicadoAt, err := scanOptionalUTCTime(s.aplicadoAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(s.updatedAtRaw)
	if err != nil {
		return nil, err
	}
	createdBy := parseOptionalUUID(s.createdBy)
	updatedBy := parseOptionalUUID(s.updatedBy)
	return domain.HydratePagoRecibido(domain.HydratePagoRecibidoParams{
		ID:             id,
		CargoDoctoCCID: nullInt64ToInt(s.cargoDoctoCCID),
		ClienteID:      nullInt64ToInt(s.clienteID),
		CobradorID:     nullInt64ToInt(s.cobradorID),
		Cobrador:       nullStringToString(s.cobrador),
		Importe:        importe,
		FormaCobroID:   nullInt64ToInt(s.formaCobroID),
		ConceptoCCID:   nullInt64ToInt(s.conceptoCCID),
		FechaHoraPago:  fecha,
		Lat:            nullStringToPtr(s.lat),
		Lon:            nullStringToPtr(s.lon),
		Sincronizacion: sincronizacionFromEstado(s.estado),
		Intentos:       s.intentos,
		UltimoError:    nullStringToPtr(s.ultimoError),
		DoctoCCID:      nullInt64ToPtr(s.doctoCCID),
		ImpteDoctoCCID: nullInt64ToPtr(s.impteDoctoCCID),
		Folio:          nullStringToPtr(s.folio),
		ReceivedAt:     receivedAt,
		AplicadoAt:     aplicadoAt,
		CreatedAt:      receivedAt, // CREATED_AT not in MSP_PAGOS_RECIBIDOS; reuse RECEIVED_AT for audit.
		UpdatedAt:      updatedAt,
		CreatedBy:      createdBy,
		UpdatedBy:      updatedBy,
	}), nil
}

func scanPagoRecibidoRow(row *sql.Row) (*domain.PagoRecibido, error) {
	var raw scanPagoRecibidoRaw
	if err := row.Scan(raw.targets()...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrPagoNoEncontrado
		}
		return nil, firebird.MapError(err)
	}
	return raw.hydrate()
}

func scanPagoRecibidoRowFromRows(rows *sql.Rows) (*domain.PagoRecibido, error) {
	var raw scanPagoRecibidoRaw
	if err := rows.Scan(raw.targets()...); err != nil {
		return nil, firebird.MapError(err)
	}
	return raw.hydrate()
}

// scanPagoImagenRaw mirrors selectPagoImagenCols positionally.
type scanPagoImagenRaw struct {
	id           string
	storageKind  string
	storageKey   string
	mime         string
	sizeBytes    int64
	descripcion  sql.NullString
	createdAtRaw any
	updatedAtRaw any
	createdBy    string
	updatedBy    string
}

func (s *scanPagoImagenRaw) targets() []any {
	return []any{
		&s.id, &s.storageKind, &s.storageKey, &s.mime, &s.sizeBytes, &s.descripcion,
		&s.createdAtRaw, &s.updatedAtRaw, &s.createdBy, &s.updatedBy,
	}
}

func (s *scanPagoImagenRaw) hydrate() (*domain.Imagen, error) {
	id, err := uuid.Parse(s.id)
	if err != nil {
		return nil, apperror.NewInternal("pago_imagen_id_invalido", "id de imagen inválido en la base de datos").WithError(err)
	}
	createdAt, err := firebird.ScanUTCTime(s.createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(s.updatedAtRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := uuid.Parse(s.createdBy)
	if err != nil {
		return nil, apperror.NewInternal("pago_imagen_created_by_invalido", "created_by inválido").WithError(err)
	}
	updatedBy, err := uuid.Parse(s.updatedBy)
	if err != nil {
		return nil, apperror.NewInternal("pago_imagen_updated_by_invalido", "updated_by inválido").WithError(err)
	}
	kind, err := domain.ParseStorageKind(s.storageKind)
	if err != nil {
		return nil, err
	}
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID:          id,
		Storage:     domain.HydrateImagenStorage(kind, s.storageKey),
		Mime:        s.mime,
		SizeBytes:   s.sizeBytes,
		Descripcion: nullStringToPtr(s.descripcion),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	}), nil
}

func scanPagoImagenRow(row *sql.Row) (*domain.Imagen, error) {
	var raw scanPagoImagenRaw
	if err := row.Scan(raw.targets()...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrImagenNoEncontrada
		}
		return nil, firebird.MapError(err)
	}
	return raw.hydrate()
}

func scanPagoImagenRowFromRows(rows *sql.Rows) (*domain.Imagen, error) {
	var raw scanPagoImagenRaw
	if err := rows.Scan(raw.targets()...); err != nil {
		return nil, firebird.MapError(err)
	}
	return raw.hydrate()
}

// hydrateWithImagenes returns a new PagoRecibido with imagenes attached. The
// only way to attach imagenes to a hydrated aggregate without exposing
// internals is via HydratePagoRecibidoParams.Imagenes — so we re-hydrate by
// reading the projection-shaped fields back out and feeding them in.
func hydrateWithImagenes(p *domain.PagoRecibido, imgs []*domain.Imagen) *domain.PagoRecibido {
	a := p.Audit()
	return domain.HydratePagoRecibido(domain.HydratePagoRecibidoParams{
		ID:             p.ID(),
		CargoDoctoCCID: p.CargoDoctoCCID(),
		ClienteID:      p.ClienteID(),
		CobradorID:     p.CobradorID(),
		Cobrador:       p.Cobrador(),
		Importe:        p.Importe(),
		FormaCobroID:   p.FormaCobroID(),
		ConceptoCCID:   p.ConceptoCCID(),
		FechaHoraPago:  p.FechaHoraPago(),
		Lat:            p.Lat(),
		Lon:            p.Lon(),
		Sincronizacion: p.Sincronizacion(),
		Intentos:       p.Intentos(),
		UltimoError:    p.UltimoError(),
		DoctoCCID:      p.DoctoCCID(),
		ImpteDoctoCCID: p.ImpteDoctoCCID(),
		Folio:          p.Folio(),
		ReceivedAt:     p.ReceivedAt(),
		AplicadoAt:     p.AplicadoAt(),
		CreatedAt:      a.CreatedAt(),
		UpdatedAt:      a.UpdatedAt(),
		CreatedBy:      a.CreatedBy(),
		UpdatedBy:      a.UpdatedBy(),
		Imagenes:       imgs,
	})
}

// ─── Small helpers ──────────────────────────────────────────────────────────

func nullInt64ToInt(n sql.NullInt64) int {
	if !n.Valid {
		return 0
	}
	return int(n.Int64)
}

func nullInt64ToPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

func nullStringToString(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

func nullStringToPtr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func nullableWallClock(t *time.Time) any {
	if t == nil {
		return nil
	}
	w := firebird.ToWallClock(*t)
	return w
}

// isZeroRaw reports whether the driver returned a nil/zero value for a
// nullable field. Firebird's driver returns nil for SQL NULL.
func isZeroRaw(raw any) bool {
	return raw == nil
}

// scanImporteOrZero decodes a NUMERIC(14,2) column tolerating SQL NULL as 0.
func scanImporteOrZero(raw any) decimal.Decimal {
	if isZeroRaw(raw) {
		return decimal.Zero
	}
	d, err := firebird.ScanDecimal(raw, 2)
	if err != nil {
		return decimal.Zero
	}
	return d
}

// scanOptionalUTCTime decodes a nullable TIMESTAMP column. nil raw → nil time.
func scanOptionalUTCTime(raw any) (*time.Time, error) {
	if isZeroRaw(raw) {
		return nil, nil //nolint:nilnil // optional pointer pattern: NULL maps to nil.
	}
	t, err := firebird.ScanUTCTime(raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// parseOptionalUUID decodes a CHAR(36) audit column. Invalid/missing → uuid.Nil
// rather than failing — backfilled rows may have NULL or empty createdBy/
// updatedBy and the aggregate tolerates it (defensive against ancient data).
func parseOptionalUUID(n sql.NullString) uuid.UUID {
	if !n.Valid || n.String == "" {
		return uuid.Nil
	}
	parsed, err := uuid.Parse(n.String)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

// ensureRowAffectedPago is a local mirror of the same helper that already
// exists in venta_repo.go (ventas module) — duplicated here to avoid
// importing internals across modules.
func ensureRowAffectedPago(res sql.Result, notFound error) error {
	n, err := res.RowsAffected()
	if err != nil {
		return firebird.MapError(err)
	}
	if n == 0 {
		return notFound
	}
	return nil
}
