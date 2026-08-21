package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/comprobantes/domain"
)

// FiltroEnvios narrows a listing. Zero values mean "no filter".
type FiltroEnvios struct {
	Estado    *domain.EstadoEnvio
	Tipo      *domain.TipoComprobante
	ClienteID *int
	Desde     *time.Time
	Hasta     *time.Time
	Limite    int
	Offset    int
}

// EnvioRepo persists the delivery queue and its log.
//
// The two conditional methods are the heart of the module. Both return a
// bool, not an error, when they lose: losing the race is a NORMAL outcome,
// not a failure. Spec §4.4 — whoever affects the row wins, and the atomicity
// comes from the conditional UPDATE, never from a read-then-write in Go.
type EnvioRepo interface {
	// Guardar inserts or updates a delivery unconditionally. Used for the
	// transitions that do not race.
	Guardar(ctx context.Context, e *domain.Envio) error

	// Obtener reads one delivery by id.
	Obtener(ctx context.Context, id uuid.UUID) (*domain.Envio, error)

	// Listar returns the deliveries matching the filter.
	Listar(ctx context.Context, f FiltroEnvios) ([]*domain.Envio, error)

	// ReclamarLote claims up to limite deliveries that are en_espera with
	// PROGRAMADO_PARA <= now, moving each to enviando in the same statement
	// that selects it. A delivery already claimed by another worker is simply
	// not returned.
	ReclamarLote(ctx context.Context, limite int, now time.Time) ([]*domain.Envio, error)

	// DetenerSiEnEspera moves a delivery to detenido only if it is still
	// en_espera. Returns false when the row was already claimed — that is the
	// "ya_enviado" answer the button shows, and it is a result, not an error.
	DetenerSiEnEspera(ctx context.Context, id uuid.UUID, por string, now time.Time) (bool, error)
}
