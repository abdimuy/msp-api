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
	// ClienteID restricts to ventas linked to the given Microsip cliente.
	ClienteID *int
	// TipoVenta restricts to a specific tipo de venta. Empty string disables.
	TipoVenta string
	// Situacion restricts to a specific situación (borrador, revisada,
	// aprobada, cancelada). Empty string disables.
	Situacion string
	// Sincronizacion restricts to a specific sincronización (pendiente,
	// aplicada). Empty string disables.
	Sincronizacion string
	// IncluirCanceladas controls whether soft-cancelled ventas are returned.
	// Defaults to false (cancelled ventas excluded).
	IncluirCanceladas bool
}

// ClienteExistenceChecker is a single-method port consulted by the ventas
// service to validate that an optional cliente_id on a venta points to a
// real row in Microsip's CLIENTES table.
type ClienteExistenceChecker interface {
	// Exists reports whether a row with the given CLIENTE_ID exists in
	// Microsip's CLIENTES. Returns (false, nil) when not found; (false, err)
	// for transport / query failures.
	Exists(ctx context.Context, clienteID int) (bool, error)
}

// VendedorUsuarioExistenceChecker is consulted by the ventas service to
// validate that every vendedor on a CrearVenta request has a corresponding
// row in MSP_USUARIOS before any INSERT is attempted. Without this check,
// an unknown vendedor usuario_id only fails at the DB FK layer, surfacing
// as a generic 409 "firebird_fk_violation" with no field-level hint.
type VendedorUsuarioExistenceChecker interface {
	// MissingIDs returns the subset of ids that do NOT have a matching row
	// in MSP_USUARIOS. Returns an empty slice (never nil) when every id is
	// present. Implementations may return ids in any order. Passing an
	// empty slice short-circuits to ([], nil) without hitting the database.
	MissingIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}

// VentaRepo persists and retrieves Venta aggregates as a single unit. The
// repository implementation is responsible for keeping the multi-table
// write transactional — child rows (combos, productos, vendedores,
// imágenes) live and die with their parent.
//
//nolint:interfacebloat // one method per aggregate-level mutation; cohesive.
type VentaRepo interface {
	// Save inserts a new venta with all children in a single transaction.
	// FK-ordered: header → combos → productos → vendedores → imágenes.
	Save(ctx context.Context, v *domain.Venta) error

	// Update writes back the header columns of v. Used for cancellation
	// and other header-only mutations. Child collections are NOT
	// re-synced here — image add/remove use dedicated methods.
	Update(ctx context.Context, v *domain.Venta) error

	// UpdateHeader writes back the mutable header columns of v (excluding
	// cancellation, which goes through Update). Used by ActualizarHeader.
	UpdateHeader(ctx context.Context, v *domain.Venta) error

	// UpdateCliente writes back the cliente snapshot + cliente_id link.
	UpdateCliente(ctx context.Context, v *domain.Venta) error

	// ReplaceProductos atomically deletes the existing productos rows for
	// v and inserts the current productos slice.
	ReplaceProductos(ctx context.Context, v *domain.Venta) error

	// ReplaceCombos atomically deletes the existing combos rows for v and
	// inserts the current combos slice.
	ReplaceCombos(ctx context.Context, v *domain.Venta) error

	// ReplaceVendedores atomically deletes the existing vendedores rows
	// for v and inserts the current vendedores slice.
	ReplaceVendedores(ctx context.Context, v *domain.Venta) error

	// FindByID loads a venta with its full children collection populated.
	// Returns ErrVentaNotFound on miss.
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Venta, error)

	// LockByID takes a pessimistic row lock on the venta header (SELECT ...
	// WITH LOCK) to serialize concurrent mutations on the same venta. Must be
	// called inside a transaction. Returns ErrVentaNotFound on miss. Used by
	// AplicarVenta as an anti-double-submit guard.
	LockByID(ctx context.Context, id uuid.UUID) error

	// List returns a cursor-paginated page of ventas matching the filters.
	List(ctx context.Context, p ListParams, f ListVentasFilters) (Page[*domain.Venta], error)

	// InsertImagen persists a single new imagen child for the given venta.
	// Used by AdjuntarImagen which adds one image at a time.
	InsertImagen(ctx context.Context, ventaID uuid.UUID, img *domain.Imagen) error

	// DeleteImagen removes a single imagen child by primary key. Returns
	// ErrImagenNotFound if the row is already absent.
	DeleteImagen(ctx context.Context, ventaID, imagenID uuid.UUID) error
}
