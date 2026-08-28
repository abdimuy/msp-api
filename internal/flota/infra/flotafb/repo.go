// Package flotafb implements the flota module's Repo against the three
// Firebird tables created by migration 000062: MSP_FLOTA_ASIGNACION_ACTUAL,
// MSP_FLOTA_ASIGNACION_CAMBIOS and MSP_FLOTA_FOTOS.
//
// Every timestamp is written through firebird.ToWallClock and read back
// through firebird.ScanUTCTime, per docs/module-standards/DATETIME_HANDLING.md
// — the columns hold naked wall-clock in America/Mexico_City like the rest of
// the schema, and the domain only ever sees UTC.
package flotafb

import (
	"context"
	"database/sql"
	"errors"
	"time"

	flotadomain "github.com/abdimuy/msp-api/internal/flota/domain"
	"github.com/abdimuy/msp-api/internal/flota/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: Repo satisfies the module's outbound port.
var _ outbound.Repo = (*Repo)(nil)

// Repo is the Firebird-backed implementation of outbound.Repo.
//
// Transaction-aware via firebird.GetQuerier: when ctx carries an active tx
// every statement routes through it. The service always calls inside one, so
// a whole snapshot commits or rolls back together.
type Repo struct {
	pool *firebird.Pool
}

// New builds a Repo wired to the given pool.
func New(pool *firebird.Pool) *Repo {
	return &Repo{pool: pool}
}

const ultimaFotoSQL = `
SELECT FIRST 1 EJECUTADO_EN
FROM MSP_FLOTA_FOTOS
ORDER BY EJECUTADO_EN DESC`

const listarAsignacionesSQL = `
SELECT USUARIO_UID, EMAIL, NOMBRE, CAMIONETA_ID, CREATED_AT, UPDATED_AT
FROM MSP_FLOTA_ASIGNACION_ACTUAL`

const insertarAsignacionSQL = `
INSERT INTO MSP_FLOTA_ASIGNACION_ACTUAL (
	USUARIO_UID, EMAIL, NOMBRE, CAMIONETA_ID, CREATED_AT, UPDATED_AT
) VALUES (?, ?, ?, ?, ?, ?)`

const actualizarAsignacionSQL = `
UPDATE MSP_FLOTA_ASIGNACION_ACTUAL
SET EMAIL = ?, NOMBRE = ?, CAMIONETA_ID = ?, UPDATED_AT = ?
WHERE USUARIO_UID = ?`

const eliminarAsignacionSQL = `
DELETE FROM MSP_FLOTA_ASIGNACION_ACTUAL
WHERE USUARIO_UID = ?`

const insertarCambioSQL = `
INSERT INTO MSP_FLOTA_ASIGNACION_CAMBIOS (
	ID, USUARIO_UID, EMAIL, NOMBRE, CAMIONETA_ANTERIOR, CAMIONETA_NUEVA,
	TIPO, VENTANA_DESDE, DETECTADO_EN, CREATED_AT
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const registrarFotoSQL = `
INSERT INTO MSP_FLOTA_FOTOS (
	ID, EJECUTADO_EN, USUARIOS_OBSERVADOS, CAMBIOS_DETECTADOS,
	ES_LINEA_BASE, CREATED_AT
) VALUES (?, ?, ?, ?, ?, ?)`

// UltimaFoto returns when the most recent snapshot ran. An empty ledger
// yields (zero, false, nil) — "we have never looked" — which is what makes
// the next pass a baseline.
func (r *Repo) UltimaFoto(ctx context.Context) (time.Time, bool, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var raw any
	err := q.QueryRowContext(ctx, ultimaFotoSQL).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, firebird.MapError(err)
	}
	t, err := firebird.ScanUTCTime(raw)
	if err != nil {
		return time.Time{}, false, err
	}
	return t, true, nil
}

// ListarAsignaciones returns the whole mirror. It is a full scan by design:
// the table holds one row per person in the roster (62 in the dev project),
// and the comparison needs all of them.
func (r *Repo) ListarAsignaciones(ctx context.Context) ([]*flotadomain.Asignacion, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	rows, err := q.QueryContext(ctx, listarAsignacionesSQL)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []*flotadomain.Asignacion
	for rows.Next() {
		a, err := scanAsignacion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// InsertarAsignaciones adds mirror rows for people seen for the first time.
func (r *Repo) InsertarAsignaciones(ctx context.Context, altas []*flotadomain.Asignacion) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for _, a := range altas {
		_, err := q.ExecContext(ctx, insertarAsignacionSQL,
			a.UsuarioUID(),
			nullableString(a.Email()),
			nullableString(a.Nombre()),
			camionetaArg(a.Camioneta()),
			firebird.ToWallClock(a.Audit().CreatedAt()),
			firebird.ToWallClock(a.Audit().UpdatedAt()),
		)
		if err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

// ActualizarAsignaciones overwrites mirror rows whose observed state changed.
func (r *Repo) ActualizarAsignaciones(ctx context.Context, cambiadas []*flotadomain.Asignacion) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for _, a := range cambiadas {
		_, err := q.ExecContext(ctx, actualizarAsignacionSQL,
			nullableString(a.Email()),
			nullableString(a.Nombre()),
			camionetaArg(a.Camioneta()),
			firebird.ToWallClock(a.Audit().UpdatedAt()),
			a.UsuarioUID(),
		)
		if err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

// EliminarAsignaciones removes mirror rows for people who left the roster.
// Their history rows survive — MSP_FLOTA_ASIGNACION_CAMBIOS carries no
// foreign key precisely so this delete cannot reach them.
func (r *Repo) EliminarAsignaciones(ctx context.Context, uids []string) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for _, uid := range uids {
		if _, err := q.ExecContext(ctx, eliminarAsignacionSQL, uid); err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

// InsertarCambios appends history rows.
func (r *Repo) InsertarCambios(ctx context.Context, cambios []*flotadomain.Cambio) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	for _, c := range cambios {
		_, err := q.ExecContext(ctx, insertarCambioSQL,
			c.ID().String(),
			c.UsuarioUID(),
			nullableString(c.Email()),
			nullableString(c.Nombre()),
			camionetaArg(c.Anterior()),
			camionetaArg(c.Nueva()),
			string(c.Tipo()),
			firebird.ToWallClock(c.VentanaDesde()),
			firebird.ToWallClock(c.DetectadoEn()),
			firebird.ToWallClock(c.CreatedAt()),
		)
		if err != nil {
			return firebird.MapError(err)
		}
	}
	return nil
}

// RegistrarFoto appends the ledger row for one completed pass.
func (r *Repo) RegistrarFoto(ctx context.Context, foto *flotadomain.Foto) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(ctx, registrarFotoSQL,
		foto.ID().String(),
		firebird.ToWallClock(foto.EjecutadoEn()),
		foto.UsuariosObservados(),
		foto.CambiosDetectados(),
		boolAsSmallint(foto.EsLineaBase()),
		firebird.ToWallClock(foto.CreatedAt()),
	)
	if err != nil {
		return firebird.MapError(err)
	}
	return nil
}

// rowScanner is the shared surface of *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanAsignacion rebuilds one mirror row. Timestamps go through ScanUTCTime
// so the entity only ever holds UTC.
func scanAsignacion(row rowScanner) (*flotadomain.Asignacion, error) {
	var (
		uid                  string
		email, nombre        sql.NullString
		camioneta            sql.NullInt64
		createdRaw, updRaw   any
		createdAt, updatedAt time.Time
	)
	if err := row.Scan(&uid, &email, &nombre, &camioneta, &createdRaw, &updRaw); err != nil {
		return nil, firebird.MapError(err)
	}
	createdAt, err := firebird.ScanUTCTime(createdRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err = firebird.ScanUTCTime(updRaw)
	if err != nil {
		return nil, err
	}
	return flotadomain.HidratarAsignacion(flotadomain.HidratarAsignacionParams{
		UsuarioUID: uid,
		Email:      email.String,
		Nombre:     nombre.String,
		Camioneta:  camionetaDesdeColumna(camioneta),
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}), nil
}

// camionetaArg binds a Camioneta to the nullable INTEGER column: NULL is the
// single storage shape of "no camioneta".
func camionetaArg(c flotadomain.Camioneta) any {
	if !c.Asignada() {
		return nil
	}
	return c.ID()
}

// camionetaDesdeColumna is the inverse of camionetaArg.
func camionetaDesdeColumna(v sql.NullInt64) flotadomain.Camioneta {
	if !v.Valid {
		return flotadomain.SinCamioneta()
	}
	id := int(v.Int64)
	return flotadomain.CamionetaDesdeRoster(&id)
}

// nullableString binds "" as NULL. The roster genuinely omits these fields on
// some documents, and storing an empty string would claim we observed a blank
// name rather than none at all.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// boolAsSmallint maps the flag to the SMALLINT column guarded by
// CK_MSP_FLOTA_FOTOS_LINEA_BASE.
func boolAsSmallint(b bool) int {
	if b {
		return 1
	}
	return 0
}
