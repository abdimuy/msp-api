//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package outbound

import (
	"context"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// PagosRecibidosRepo is the writable side of the cobranza module — it backs
// the outbox MSP_PAGOS_RECIBIDOS and its child MSP_PAGOS_IMAGENES.
//
// Separate from [PagosRepo] (which reads from the cache MSP_PAGOS_VENTAS): the
// two tables hold distinct projections — MSP_PAGOS_RECIBIDOS is what the app
// captures and we own end-to-end; MSP_PAGOS_VENTAS is the Microsip-driven
// materialized view. Different domain types (PagoRecibido vs Pago) and
// different repos.
type PagosRecibidosRepo interface {
	// Insert persists a new PagoRecibido as ESTADO='P' in the outbox. Returns
	// [domain.ErrPagoYaExiste] if the UUID collides with an existing row
	// (idempotency key — the client may safely retry the request).
	Insert(ctx context.Context, p *domain.PagoRecibido) error

	// Update persists a state change (MarcarAplicada or RegistrarFallo) to an
	// existing row. The repo only updates mutable columns — ID/FECHA/CREATED_*
	// are never overwritten.
	Update(ctx context.Context, p *domain.PagoRecibido) error

	// FindByID loads a single PagoRecibido by its UUID, including all child
	// imágenes. Returns [domain.ErrPagoNoEncontrado] on miss.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.PagoRecibido, error)

	// LockByID acquires a pessimistic SELECT … WITH LOCK on the row inside
	// the current transaction. Must be called before reading the pago in an
	// AplicarPago / RetryWorker flow to serialize concurrent attempts. The
	// caller MUST be inside a transaction.
	LockByID(ctx context.Context, id uuid.UUID) error

	// ListPendientes returns up to `limit` PagoRecibido rows with
	// ESTADO='P' AND INTENTOS<maxIntentos, ordered by RECEIVED_AT ascending
	// (oldest first). The retry worker uses this to drain the outbox.
	// Imágenes are NOT loaded — call FindByID if needed.
	ListPendientes(ctx context.Context, maxIntentos, limit int) ([]*domain.PagoRecibido, error)
}

// PagosImagenesRepo manages the MSP_PAGOS_IMAGENES child collection. Split
// from [PagosRecibidosRepo] to keep each interface under the 8-method
// interfacebloat limit; concrete implementations may satisfy both via a
// single struct.
type PagosImagenesRepo interface {
	// InsertImagen persists a new comprobante row in MSP_PAGOS_IMAGENES.
	InsertImagen(ctx context.Context, pagoID uuid.UUID, img *domain.Imagen) error

	// DeleteImagen removes a comprobante row. Returns
	// [domain.ErrImagenNoEncontrada] when the row does not exist.
	DeleteImagen(ctx context.Context, imagenID uuid.UUID) error

	// FindImagenByID loads a single comprobante row by its UUID, without
	// loading its parent pago. Returns [domain.ErrImagenNoEncontrada] on miss.
	FindImagenByID(ctx context.Context, imagenID uuid.UUID) (*domain.Imagen, error)

	// ListImagenes returns every comprobante attached to pagoID, ordered by
	// CREATED_AT ascending.
	ListImagenes(ctx context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error)
}
