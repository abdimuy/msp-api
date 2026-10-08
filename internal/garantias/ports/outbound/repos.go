package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// ListarGarantiasFiltros narrows the bandeja. Every field is optional; nil
// means "no filter". Etapa and Ubicacion match a folio when AT LEAST ONE of
// its articles is in that stage or place.
type ListarGarantiasFiltros struct {
	Estado    *domain.EstadoFolio
	Origen    *domain.OrigenFolio
	ClienteID *int
	Etapa     *domain.Etapa
	Ubicacion *domain.Ubicacion
	Desde     *time.Time // CREATED_AT >= Desde
	Hasta     *time.Time // CREATED_AT <  Hasta
}

// Paginacion is the keyset page request. Cursor is opaque and empty on the
// first page.
type Paginacion struct {
	Cursor string
	Limite int
}

// Pagina is one page of results. SiguienteCursor is empty on the last page.
type Pagina[T any] struct {
	Items           []T
	SiguienteCursor string
}

// GarantiaRepo persists the folio aggregate: header, articles and the
// pending timeline events, always together.
//
// There are TWO write methods on purpose (brief decision 2). The aggregate
// does not know whether it is new, and a single Guardar that guessed with a
// SELECT first would cost a query on every write plus a race on the opening:
// two concurrent openings would both see "no row" and both insert.
//
// Guardar does NOT empty the aggregate's eventosPendientes after writing them
// (brief decision 9): a command loads a folio, mutates it, saves it and drops
// it. Nobody reuses a saved aggregate, and a repository that cleared the queue
// would be the one place where that assumption could be violated silently.
type GarantiaRepo interface {
	// Crear inserts a folio that does not exist yet: the header, every
	// article and every pending event. Must run inside a transaction.
	Crear(ctx context.Context, g *domain.Garantia) error
	// Guardar persists a folio loaded with ObtenerParaActualizar: updates
	// the header, updates each existing article, inserts the new ones and
	// inserts every pending event. Must run inside a transaction.
	// Returns domain.ErrGarantiaNoEncontrada when the header row is gone.
	Guardar(ctx context.Context, g *domain.Garantia) error
	// ObtenerParaActualizar loads the folio with its articles and locks the
	// header row (SELECT ... WITH LOCK) until the transaction ends. Must run
	// inside a transaction.
	ObtenerParaActualizar(ctx context.Context, id uuid.UUID) (*domain.Garantia, error)
	// Obtener loads the folio with its articles, without locking.
	Obtener(ctx context.Context, id uuid.UUID) (*domain.Garantia, error)
	// ObtenerPorFolio is Obtener by the human-readable folio.
	ObtenerPorFolio(ctx context.Context, folio domain.Folio) (*domain.Garantia, error)
}

// BandejaRepo is the read side of the folio list. It is split from
// GarantiaRepo because the commands never list, and because it ships in a
// later task than the write side.
type BandejaRepo interface {
	// Listar returns the bandeja, newest first (CREATED_AT DESC, ID DESC),
	// each folio with its articles.
	Listar(ctx context.Context, f ListarGarantiasFiltros, p Paginacion) (Pagina[*domain.Garantia], error)
}

// EventoRepo reads the timeline. It is read-only by design (spec §5):
// events are written only through GarantiaRepo, together with the change
// they record. That is what keeps the §4.4 invariant — no stage change
// without its event — a property of the schema, not of whichever command
// writes next.
type EventoRepo interface {
	// ListarPorGarantia returns the folio's timeline ordered by
	// DEVICE_CREATED_AT, then CREATED_AT, then ID.
	ListarPorGarantia(ctx context.Context, garantiaID uuid.UUID) ([]*domain.Evento, error)
	// ObtenerPorClaveIdempotencia returns the event carrying that key, or
	// (nil, nil) when none does. A repeated key is a repetition, not an
	// error (brief decision 6).
	ObtenerPorClaveIdempotencia(ctx context.Context, clave string) (*domain.Evento, error)
}

// ImagenRepo persists evidence metadata. The blob itself goes through
// StorageProvider.
type ImagenRepo interface {
	Registrar(ctx context.Context, img *domain.Imagen) error
	ListarPorEvento(ctx context.Context, eventoID uuid.UUID) ([]*domain.Imagen, error)
}

// FolioGenerator hands out the next folio number from GEN_MSP_GA_FOLIO.
// Numbers are never reused, so a rolled-back transaction leaves a gap.
// Reusing a number would make two folios share a human-readable identity in
// a module whose whole point is a paper trail.
type FolioGenerator interface {
	Siguiente(ctx context.Context) (int, error)
}
