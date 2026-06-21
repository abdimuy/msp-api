//nolint:misspell // Spanish domain vocabulary by project convention.
package analyticsfb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Compile-time assertion: Repo must implement NarrativaRepo.
var _ outbound.NarrativaRepo = (*Repo)(nil)

// ─── GetNarrativa ─────────────────────────────────────────────────────────────

// GetNarrativa returns the cached narrativa row for clienteID, or (nil, nil) if
// no row exists.
func (r *Repo) GetNarrativa(ctx context.Context, clienteID int) (*outbound.NarrativaRow, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	var (
		texto     sql.NullString
		rasgosRaw sql.NullString
		inputHash string
		modelo    sql.NullString
	)
	err := q.QueryRowContext(ctx, selectNarrativa, clienteID).
		Scan(&texto, &rasgosRaw, &inputHash, &modelo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil //nolint:nilnil // (nil, nil) means "not found" per NarrativaRepo contract.
	}
	if err != nil {
		return nil, firebird.MapError(err)
	}

	rasgos, err := unmarshalRasgos(rasgosRaw.String)
	if err != nil {
		return nil, fmt.Errorf("analyticsfb: unmarshal rasgos for cliente %d: %w", clienteID, err)
	}

	return &outbound.NarrativaRow{
		ClienteID: clienteID,
		Texto:     texto.String,
		Rasgos:    rasgos,
		InputHash: inputHash,
		Modelo:    modelo.String,
	}, nil
}

// unmarshalRasgos decodes the JSON blob column into a []string.
// An empty or null JSON string returns an empty (non-nil) slice.
func unmarshalRasgos(raw string) ([]string, error) {
	if raw == "" || raw == "null" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if out == nil {
		return []string{}, nil
	}
	return out, nil
}

// ─── UpsertNarrativa ──────────────────────────────────────────────────────────

// UpsertNarrativa inserts or updates the cached narrativa for n.ClienteID.
// Uses UPDATE-then-INSERT because the firebirdsql driver has a -804 bug with MERGE.
func (r *Repo) UpsertNarrativa(ctx context.Context, n domain.Narrativa) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)

	rasgosJSON, err := marshalRasgos(n.Rasgos)
	if err != nil {
		return fmt.Errorf("analyticsfb: marshal rasgos for cliente %d: %w", n.ClienteID, err)
	}

	now := time.Now().UTC()
	generadaEn := firebird.ToWallClock(n.GeneradaEn)
	updatedAt := firebird.ToWallClock(now)
	createdAt := firebird.ToWallClock(now)

	// Attempt UPDATE first.
	res, err := q.ExecContext(ctx, updateNarrativa,
		n.Texto,
		rasgosJSON,
		n.InputHash,
		n.Modelo,
		generadaEn,
		updatedAt,
		n.ClienteID, // WHERE
	)
	if err != nil {
		return fmt.Errorf("analyticsfb: update narrativa cliente %d: %w", n.ClienteID, firebird.MapError(err))
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("analyticsfb: rows affected narrativa: %w", firebird.MapError(err))
	}
	if affected > 0 {
		return nil
	}

	// Row didn't exist — INSERT.
	_, err = q.ExecContext(ctx, insertNarrativa,
		uuid.New().String(),
		n.ClienteID,
		n.Texto,
		rasgosJSON,
		n.InputHash,
		n.Modelo,
		generadaEn,
		createdAt,
		updatedAt,
	)
	if err != nil {
		return fmt.Errorf("analyticsfb: insert narrativa cliente %d: %w", n.ClienteID, firebird.MapError(err))
	}
	return nil
}

// marshalRasgos encodes a []string to a JSON array string.
// A nil or empty slice becomes "[]" (never "null").
func marshalRasgos(rasgos []string) (string, error) {
	if len(rasgos) == 0 {
		return "[]", nil
	}
	b, err := json.Marshal(rasgos)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ─── Encolar ──────────────────────────────────────────────────────────────────

// Encolar idempotently enqueues clienteID in the pending queue.
// Re-enqueuing an already-queued client refreshes its INPUT_HASH and ENCOLADA_EN.
func (r *Repo) Encolar(ctx context.Context, clienteID int, inputHash string) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	now := time.Now().UTC()
	encoladaEn := firebird.ToWallClock(now)

	res, err := q.ExecContext(ctx, updatePendiente,
		inputHash,
		encoladaEn,
		clienteID, // WHERE
	)
	if err != nil {
		return fmt.Errorf("analyticsfb: update pendiente cliente %d: %w", clienteID, firebird.MapError(err))
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("analyticsfb: rows affected pendiente: %w", firebird.MapError(err))
	}
	if affected > 0 {
		return nil
	}

	_, err = q.ExecContext(ctx, insertPendiente,
		clienteID,
		inputHash,
		encoladaEn,
	)
	if err != nil {
		return fmt.Errorf("analyticsfb: insert pendiente cliente %d: %w", clienteID, firebird.MapError(err))
	}
	return nil
}

// ─── ListarPendientes ─────────────────────────────────────────────────────────

// ListarPendientes returns up to limit queued clients ordered by ENCOLADA_EN ASC.
func (r *Repo) ListarPendientes(ctx context.Context, limit int) ([]outbound.PendienteRow, error) {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	// limit is interpolated (not bound) because Firebird's FIRST clause does not accept
	// a positional parameter; limit is a trusted internal int (config BatchSize / constants),
	// so there is no injection vector.
	query := fmt.Sprintf("SELECT FIRST %d CLIENTE_ID, INPUT_HASH FROM MSP_AN_NARRATIVA_PENDIENTE ORDER BY ENCOLADA_EN", limit)
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("analyticsfb: listar pendientes: %w", firebird.MapError(err))
	}
	defer func() { _ = rows.Close() }()

	// Initialize non-nil so an empty queue returns [] (matches the in-memory fake
	// and the NarrativaRepo contract), not a nil slice.
	result := make([]outbound.PendienteRow, 0, limit)
	for rows.Next() {
		var row outbound.PendienteRow
		if err := rows.Scan(&row.ClienteID, &row.InputHash); err != nil {
			return nil, fmt.Errorf("analyticsfb: scan pendiente: %w", firebird.MapError(err))
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("analyticsfb: listar pendientes rows: %w", firebird.MapError(err))
	}
	return result, nil
}

// ─── BorrarPendiente ──────────────────────────────────────────────────────────

// BorrarPendiente removes clienteID from the pending queue.
// A no-op when the client is not queued (no error).
func (r *Repo) BorrarPendiente(ctx context.Context, clienteID int) error {
	q := firebird.GetQuerier(ctx, r.pool.DB)
	_, err := q.ExecContext(ctx, deletePendiente, clienteID)
	if err != nil {
		return fmt.Errorf("analyticsfb: borrar pendiente cliente %d: %w", clienteID, firebird.MapError(err))
	}
	return nil
}
