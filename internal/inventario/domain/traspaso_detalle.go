//nolint:misspell // domain vocabulary is Spanish (traspaso, artículo, etc.) per project convention.
package domain

import "github.com/google/uuid"

// TraspasoDetalle is a child entity of the Traspaso aggregate. It represents
// one article line in the movement. Its lifecycle is fully managed through
// the Traspaso aggregate root — external callers never construct it directly.
type TraspasoDetalle struct {
	id         uuid.UUID
	articuloID int
	cantidad   Cantidad
}

// newDetalle validates and constructs a TraspasoDetalle. Package-private:
// external callers must go through CrearTraspaso.
//
// Returns ErrArticuloIDInvalido when articuloID ≤ 0. Propagates Cantidad's
// error (ErrCantidadInvalida / ErrCantidadEscalaInvalida) when cantidad is
// invalid.
func newDetalle(id uuid.UUID, articuloID int, cantidad Cantidad) (*TraspasoDetalle, error) {
	if articuloID <= 0 {
		return nil, ErrArticuloIDInvalido
	}
	if cantidad.IsZero() {
		return nil, ErrCantidadInvalida
	}
	return &TraspasoDetalle{
		id:         id,
		articuloID: articuloID,
		cantidad:   cantidad,
	}, nil
}

// HydrateDetalleParams carries the persisted shape of a TraspasoDetalle for
// repository reconstruction.
type HydrateDetalleParams struct {
	ID         uuid.UUID
	ArticuloID int
	Cantidad   Cantidad
}

// HydrateDetalle rebuilds a TraspasoDetalle from persistence without
// validation. Intended for repository use only.
func HydrateDetalle(p HydrateDetalleParams) *TraspasoDetalle {
	return &TraspasoDetalle{
		id:         p.ID,
		articuloID: p.ArticuloID,
		cantidad:   p.Cantidad,
	}
}

// ID returns the detalle's primary key.
func (d *TraspasoDetalle) ID() uuid.UUID { return d.id }

// ArticuloID returns the Microsip article identifier.
func (d *TraspasoDetalle) ArticuloID() int { return d.articuloID }

// Cantidad returns the quantity of articles in this line.
func (d *TraspasoDetalle) Cantidad() Cantidad { return d.cantidad }
