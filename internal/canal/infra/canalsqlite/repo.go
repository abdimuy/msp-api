// Package canalsqlite implements the canal module's durable mailbox
// (outbound.BuzonRepo) against a local SQLite file via modernc.org/sqlite —
// a pure-Go driver, required because CI cross-compiles
// GOOS=windows GOARCH=amd64 CGO_ENABLED=0 and cgo drivers (mattn/go-sqlite3)
// break that build.
//
// This is the module's storage-precedent deviation documented in the plan's
// "desviaciones conscientes" table: every other module's repo lives in
// infra/{module}fb/ against Firebird, but canal's VPS (docs/adr/0010) has no
// Firebird — the mailbox exists precisely because the store's Firebird is
// on-premise and only reachable through a rotating tunnel. See also
// internal/reactivacion/infra/reactivacionllm and
// internal/ventas/infra/storage for the repo's other non-Firebird infra
// precedents.
//
// canal is a sealed module (ADR-0009): this package imports only the
// standard library, uuid, modernc.org/sqlite (third-party, not gated by the
// seal), internal/canal/domain, internal/canal/ports/outbound, and
// internal/platform/apperror.
package canalsqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver.

	"github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/canal/ports/outbound"
)

// schemaSQL is the mailbox's DDL, embedded at build time. It lives beside
// this code rather than in migrations-firebird/ — that directory's lefthook
// glob triggers make test-firebird-all against a schema Firebird never
// sees. Every statement is idempotent (CREATE ... IF NOT EXISTS), so
// running it against a database that already has the schema — the VPS
// restarting with the file still on disk — is a no-op.
//
//go:embed schema.sql
var schemaSQL string

// Compile-time assertion: Repo satisfies the module's outbound port.
var _ outbound.BuzonRepo = (*Repo)(nil)

// Repo is the SQLite-backed implementation of outbound.BuzonRepo.
//
// A single *sql.DB, deliberately capped at one open connection (see Open):
// SQLite allows only one writer at a time regardless, an in-memory database
// used by tests is per-connection (a second pooled connection would see an
// empty database), and the mailbox's write volume never justifies the
// added complexity of a WAL-mode multi-reader setup.
type Repo struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path — which may be
// ":memory:" for tests — and returns a Repo with its schema ensured.
func Open(path string) (*Repo, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, mapError(err)
	}
	repo, err := New(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

// New wraps an already-open *sql.DB, ensuring the mailbox schema exists.
// Safe to call against an existing database (see schemaSQL). Caps the pool
// at one connection — see Repo's doc comment for why.
func New(db *sql.DB) (*Repo, error) {
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), schemaSQL); err != nil {
		return nil, mapError(err)
	}
	return &Repo{db: db}, nil
}

// Close releases the underlying database connection.
func (r *Repo) Close() error {
	return r.db.Close()
}

// Guardar persists m idempotently, keyed by Wamid. See
// outbound.BuzonRepo.Guardar.
func (r *Repo) Guardar(ctx context.Context, m *domain.MensajeEntrante) (bool, error) {
	res, err := r.db.ExecContext(ctx, insertMensajeSQL,
		m.ID().String(),
		m.Wamid(),
		m.Remitente(),
		m.PhoneNumberID(),
		m.Tipo(),
		m.Contenido(),
		formatUTC(m.TimestampMeta()),
		formatUTC(m.RecibidoEn()),
		m.Estado().String(),
		m.MotivoFallo(),
		formatUTC(m.CreatedAt()),
		formatUTC(m.UpdatedAt()),
	)
	if err != nil {
		return false, mapError(err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, mapError(err)
	}
	return n > 0, nil
}

// ListarPendientes returns up to limite entrantes in
// domain.EstadoReenvioPendiente, ordered by RecibidoEn ascending. See
// outbound.BuzonRepo.ListarPendientes.
func (r *Repo) ListarPendientes(ctx context.Context, limite int) ([]*domain.MensajeEntrante, error) {
	rows, err := r.db.QueryContext(ctx, listarPendientesSQL,
		domain.EstadoReenvioPendiente.String(), limite)
	if err != nil {
		return nil, mapError(err)
	}
	defer func() { _ = rows.Close() }()

	var out []*domain.MensajeEntrante
	for rows.Next() {
		m, err := scanMensaje(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return out, nil
}

// MarcarReenviado moves the entrante identified by id to
// domain.EstadoReenvioReenviado, persisting now as updated_at. See
// outbound.BuzonRepo.MarcarReenviado.
func (r *Repo) MarcarReenviado(ctx context.Context, id uuid.UUID, now time.Time) error {
	res, err := r.db.ExecContext(ctx, marcarReenviadoSQL,
		domain.EstadoReenvioReenviado.String(), formatUTC(now), id.String())
	if err != nil {
		return mapError(err)
	}
	return ensureRowAffected(res)
}

// MarcarFallido moves the entrante identified by id to
// domain.EstadoReenvioFallido, recording motivo and persisting now as
// updated_at. See outbound.BuzonRepo.MarcarFallido.
func (r *Repo) MarcarFallido(ctx context.Context, id uuid.UUID, motivo string, now time.Time) error {
	res, err := r.db.ExecContext(ctx, marcarFallidoSQL,
		domain.EstadoReenvioFallido.String(), motivo, formatUTC(now), id.String())
	if err != nil {
		return mapError(err)
	}
	return ensureRowAffected(res)
}

// ContarPendientes counts entrantes in domain.EstadoReenvioPendiente. See
// outbound.BuzonRepo.ContarPendientes.
func (r *Repo) ContarPendientes(ctx context.Context) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, contarPendientesSQL,
		domain.EstadoReenvioPendiente.String()).Scan(&n)
	if err != nil {
		return 0, mapError(err)
	}
	return n, nil
}

// ensureRowAffected maps a zero-row UPDATE result to
// domain.ErrMensajeEntranteNoEncontrado — the id named a row this
// repository never stored.
func ensureRowAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return mapError(err)
	}
	if n == 0 {
		return domain.ErrMensajeEntranteNoEncontrado
	}
	return nil
}
