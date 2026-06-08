//nolint:misspell // domain vocabulary is Spanish (almacén, etc.) per project convention.
package domain

// Almacen is a read-model value object representing a row from Microsip's
// ALMACENES table. It is a data carrier used for projections only — it has no
// lifecycle or invariants of its own.
type Almacen struct {
	ID     int
	Nombre string
}

// NewAlmacen constructs an Almacen read-model projection.
func NewAlmacen(id int, nombre string) Almacen {
	return Almacen{ID: id, Nombre: nombre}
}
