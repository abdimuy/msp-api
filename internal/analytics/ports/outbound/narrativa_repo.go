package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

// NarrativaRow is the persisted cache record (matches MSP_AN_CLIENTE_NARRATIVA).
type NarrativaRow struct {
	ClienteID int
	Texto     string
	Rasgos    []string // validated catalog codes
	InputHash string
	Modelo    string
}

// PendienteRow is one queued client awaiting generation (MSP_AN_NARRATIVA_PENDIENTE).
type PendienteRow struct {
	ClienteID int
	InputHash string
}

// NarrativaRepo persists the narrativa cache and the bounded pending queue.
// Implemented by the Firebird adapter. All timestamps/UUIDs are set in Go by the
// adapter (CLAUDE.md §1).
type NarrativaRepo interface {
	// GetNarrativa returns the cached row for a client, or (nil, nil) if absent.
	GetNarrativa(ctx context.Context, clienteID int) (*NarrativaRow, error)
	// UpsertNarrativa inserts or updates the cached row (one per CLIENTE_ID).
	UpsertNarrativa(ctx context.Context, n domain.Narrativa) error
	// Encolar idempotently enqueues a client for generation (PK CLIENTE_ID).
	Encolar(ctx context.Context, clienteID int, inputHash string) error
	// ListarPendientes returns up to limit queued clients.
	ListarPendientes(ctx context.Context, limit int) ([]PendienteRow, error)
	// BorrarPendiente removes a client from the queue.
	BorrarPendiente(ctx context.Context, clienteID int) error
}
