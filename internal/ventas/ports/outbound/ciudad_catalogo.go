//nolint:misspell // Spanish vocabulary (ciudad, catálogo) per project convention.
package outbound

import "context"

// CiudadCatalogo resolves a captured ciudad string against Microsip's
// CIUDADES catalog.
//
// EstadoID comes from the SAME row as CiudadID and is never chosen
// separately: the catalog spans several states, so resolving them
// independently yields clientes whose ciudad belongs to one state and whose
// estado says another.
type CiudadCatalogo interface {
	// Resolver returns the catalog IDs for nombre, or nil when the catalog
	// has no match. A nil result is NOT an error — the caller decides whether
	// a miss blocks the apply.
	Resolver(ctx context.Context, nombre string) (*CiudadResuelta, error)
}

// CiudadResuelta is a matched CIUDADES row, reduced to the two IDs the
// cliente auto-create needs.
type CiudadResuelta struct {
	CiudadID int
	EstadoID int
}
