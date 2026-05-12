//nolint:misspell // ventas vocabulary is Spanish (productos, vendedores) per project convention.
package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// ListParams is the cursor-pagination input accepted by VentaRepo.List.
// Cursor is opaque to the caller (server encodes/decodes it); PageSize is
// the desired page size, with the repo applying its own minimum/maximum if
// necessary.
type ListParams struct {
	Cursor   string
	PageSize int
}

// Page is the generic cursor-paginated result returned by List methods.
// NextCursor is the empty string when there are no more pages.
type Page[T any] struct {
	Items      []T
	NextCursor string
}

// ListVentasFilters is the structured filter set accepted by VentaRepo.List.
// All fields are optional: a zero value disables the filter. Time bounds are
// inclusive of Desde and exclusive of Hasta to match standard ranged query
// semantics.
type ListVentasFilters struct {
	// Desde restricts to ventas with FechaVenta >= Desde.
	Desde *time.Time
	// Hasta restricts to ventas with FechaVenta < Hasta.
	Hasta *time.Time
	// VendedorUsuarioID restricts to ventas whose vendedores include the
	// given usuario id.
	VendedorUsuarioID *uuid.UUID
	// TipoVenta restricts to a specific tipo de venta. Empty string disables.
	TipoVenta string
	// IncluirCanceladas controls whether soft-cancelled ventas are returned.
	// Defaults to false (cancelled ventas excluded).
	IncluirCanceladas bool
}

// VentaRepo persists and retrieves Venta aggregates as a single unit. The
// repository implementation is responsible for keeping the multi-table
// write transactional — child rows (combos, productos, vendedores,
// imágenes) live and die with their parent.
type VentaRepo interface {
	// Save inserts a new venta with all children in a single transaction.
	// FK-ordered: header → combos → productos → vendedores → imágenes.
	Save(ctx context.Context, v *domain.Venta) error

	// Update writes back the header columns of v. Used for cancellation
	// and other header-only mutations. Child collections are NOT
	// re-synced here — image add/remove use dedicated methods.
	Update(ctx context.Context, v *domain.Venta) error

	// FindByID loads a venta with its full children collection populated.
	// Returns ErrVentaNotFound on miss.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Venta, error)

	// List returns a cursor-paginated page of ventas matching the filters.
	List(ctx context.Context, p ListParams, f ListVentasFilters) (Page[*domain.Venta], error)

	// InsertImagen persists a single new imagen child for the given venta.
	// Used by AdjuntarImagen which adds one image at a time.
	InsertImagen(ctx context.Context, ventaID uuid.UUID, img *domain.Imagen) error

	// DeleteImagen removes a single imagen child by primary key. Returns
	// ErrImagenNotFound if the row is already absent.
	DeleteImagen(ctx context.Context, ventaID, imagenID uuid.UUID) error
}
