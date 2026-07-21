//nolint:misspell // Spanish domain vocabulary by project convention.
package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
)

// ListarMensajesParams controls which MSP_RX_MENSAJES rows
// MensajeRepo.Listar returns.
type ListarMensajesParams struct {
	// Estado restricts results to one estado. Empty string = no filter.
	Estado domain.EstadoMensaje
	// Segmento restricts results to one segmento. Empty string = no filter.
	Segmento domain.Segmento
	// Limit caps the number of rows returned. Zero = no explicit cap.
	Limit int
}

// MensajeRepo persists and retrieves the MSP_RX_MENSAJES outbound queue.
type MensajeRepo interface {
	// Insertar bulk-inserts newly queued mensajes. Every row is a fresh INSERT
	// (the PK is a unique UUID) — never an upsert.
	Insertar(ctx context.Context, mensajes []*domain.Mensaje) error

	// ListarPendientes returns up to limit rows with ESTADO='encolado', ordered
	// by ENCOLADO_EN ascending (oldest first).
	ListarPendientes(ctx context.Context, limit int) ([]*domain.Mensaje, error)

	// Actualizar persists the current state of m (ESTADO, SENDER_KIND,
	// ENVIADO_EN, ERROR, UPDATED_AT) by ID.
	Actualizar(ctx context.Context, m *domain.Mensaje) error

	// Listar returns mensajes matching p, ordered by ENCOLADO_EN.
	Listar(ctx context.Context, p ListarMensajesParams) ([]*domain.Mensaje, error)

	// ContarEnviadosHoy counts rows with ESTADO='enviado' AND ENVIADO_EN >=
	// desde — the governor's daily-cap counter.
	ContarEnviadosHoy(ctx context.Context, desde time.Time) (int, error)

	// ClientesConMensaje returns the set of clienteIDs that already have at
	// least one MSP_RX_MENSAJES row, so EncolarCohorte does not duplicate.
	ClientesConMensaje(ctx context.Context) (map[int]bool, error)
}
