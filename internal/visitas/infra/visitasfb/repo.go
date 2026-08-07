// Package visitasfb implements internal/visitas/ports/outbound.VisitasRepo
// against MSP_VISITAS, a preexisting legacy Firebird table (migration
// 000047). See that migration's header comment and the domain.Visita doc
// comment for why the table has no audit columns and why its text columns —
// though CHARACTER SET NONE — must be bound as plain UTF-8 Go strings
// (never firebird.EncodeWin1252; see
// docs/module-standards/ENCODING_HANDLING.md and the internal reference
// reference_firebird_legacy_nombre_plain_string).
package visitasfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/ports/outbound"
)

// Compile-time assertion: Repo satisfies outbound.VisitasRepo.
var _ outbound.VisitasRepo = (*Repo)(nil)

// Repo implements outbound.VisitasRepo backed by MSP_VISITAS in Firebird.
//
// The repo is transaction-aware via firebird.GetQuerier: when ctx carries an
// active tx, all SQL routes through it; otherwise the connection pool is
// used directly.
type Repo struct {
	pool *firebird.Pool
}

// New builds a Repo wired to the given pool.
func New(pool *firebird.Pool) *Repo {
	return &Repo{pool: pool}
}

const selectVisitaCols = `
	ID,
	COBRADOR,
	COBRADOR_ID,
	FECHA,
	FORMA_COBRO_ID,
	LAT,
	LNG,
	NOTA,
	TIPO_VISITA,
	ZONA_CLIENTE_ID,
	CLIENTE_ID,
	IMPTE_DOCTO_CC_ID`

const insertVisitaSQL = `
INSERT INTO MSP_VISITAS (
	ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG,
	NOTA, TIPO_VISITA, ZONA_CLIENTE_ID, CLIENTE_ID, IMPTE_DOCTO_CC_ID
) VALUES (
	?, ?, ?, ?, ?, ?, ?,
	?, ?, ?, ?, ?
)`

const findVisitaByIDSQL = `
SELECT ` + selectVisitaCols + `
FROM MSP_VISITAS
WHERE ID = ?`

// Insert persists a new Visita. Returns domain.ErrVisitaYaExiste if the UUID
// collides with an existing row (idempotency key — the mobile app generates
// the ID offline and may safely retry the request).
func (r *Repo) Insert(ctx context.Context, v *domain.Visita) error {
	return firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		args := []any{
			v.ID().String(),
			v.Cobrador(),
			v.CobradorID(),
			firebird.ToWallClock(v.Fecha()),
			v.FormaCobroID(),
			v.Lat(),
			v.Lng(),
			nullableString(v.Nota()),
			v.TipoVisita(),
			v.ZonaClienteID(),
			v.ClienteID(),
			nullableInt(v.ImpteDoctoCCID()),
		}
		_, err := q.ExecContext(ctx, insertVisitaSQL, args...)
		if err != nil {
			mapped := firebird.MapError(err)
			var ae *apperror.Error
			if errors.As(mapped, &ae) && ae.Code == "firebird_unique_violation" {
				return domain.ErrVisitaYaExiste
			}
			return mapped
		}
		return nil
	})
}

// FindByID loads a single Visita by its UUID. Returns
// domain.ErrVisitaNoEncontrada on miss.
func (r *Repo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Visita, error) {
	var result *domain.Visita
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		row := q.QueryRowContext(ctx, findVisitaByIDSQL, id.String())
		v, serr := scanVisitaRow(row)
		if serr != nil {
			return serr
		}
		result = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// scanVisitaRaw mirrors selectVisitaCols positionally with raw scan targets.
// NOTA is nullable (sql.NullString) and IMPTE_DOCTO_CC_ID is nullable
// (sql.NullInt32); every other column is NOT NULL in MSP_VISITAS and scans
// directly. FECHA is scanned into `any` so firebird.ScanUTCTime can normalize
// the driver's wall-clock value to UTC.
//
// Text columns are NOT wrapped in COALESCE(col, empty-string-literal) on the
// SELECT — that pattern is a documented failure mode against MSP_VISITAS's
// CHARACTER SET NONE columns (see migration 000047 and the module's task
// brief).
type scanVisitaRaw struct {
	id             string
	cobrador       string
	cobradorID     int
	fechaRaw       any
	formaCobroID   int
	lat            float64
	lng            float64
	nota           sql.NullString
	tipoVisita     string
	zonaClienteID  int
	clienteID      int
	impteDoctoCCID sql.NullInt32
}

func (s *scanVisitaRaw) targets() []any {
	return []any{
		&s.id,
		&s.cobrador,
		&s.cobradorID,
		&s.fechaRaw,
		&s.formaCobroID,
		&s.lat,
		&s.lng,
		&s.nota,
		&s.tipoVisita,
		&s.zonaClienteID,
		&s.clienteID,
		&s.impteDoctoCCID,
	}
}

func (s *scanVisitaRaw) hydrate() (*domain.Visita, error) {
	id, err := uuid.Parse(s.id)
	if err != nil {
		return nil, apperror.NewInternal("visita_id_invalido", "id de visita inválido en la base de datos").WithError(err)
	}
	fecha, err := firebird.ScanUTCTime(s.fechaRaw)
	if err != nil {
		return nil, err
	}
	return domain.RehydrateVisita(domain.RehydrateVisitaParams{
		ID:             id,
		Cobrador:       s.cobrador,
		CobradorID:     s.cobradorID,
		Fecha:          fecha,
		FormaCobroID:   s.formaCobroID,
		Lat:            s.lat,
		Lng:            s.lng,
		Nota:           nullStringToString(s.nota),
		TipoVisita:     s.tipoVisita,
		ZonaClienteID:  s.zonaClienteID,
		ClienteID:      s.clienteID,
		ImpteDoctoCCID: nullInt32ToPtr(s.impteDoctoCCID),
	}), nil
}

func scanVisitaRow(row *sql.Row) (*domain.Visita, error) {
	var raw scanVisitaRaw
	if err := row.Scan(raw.targets()...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrVisitaNoEncontrada
		}
		return nil, firebird.MapError(err)
	}
	return raw.hydrate()
}

// ─── Small helpers ──────────────────────────────────────────────────────────

// nullableString returns nil (SQL NULL) when s is empty, otherwise s itself.
// Used to bind Visita.Nota(), which is "" when NOTA has no value.
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nullableInt returns nil (SQL NULL) when p is nil, otherwise the pointed-to
// value. Used to bind Visita.ImpteDoctoCCID().
func nullableInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// nullStringToString unwraps a sql.NullString to "" when NULL.
func nullStringToString(n sql.NullString) string {
	if !n.Valid {
		return ""
	}
	return n.String
}

// nullInt32ToPtr unwraps a sql.NullInt32 to nil when NULL.
func nullInt32ToPtr(n sql.NullInt32) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int32)
	return &v
}
