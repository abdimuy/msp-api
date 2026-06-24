//nolint:misspell // ventas vocabulary is Spanish per project convention.
package outbound

import (
	"context"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
)

// MicrosipJuegoInput carries the recipe and catalog metadata needed to
// match-or-create a juego (kit) in Microsip's ARTICULOS family.
type MicrosipJuegoInput struct {
	// Receta holds the sorted, deduplicated set of {articuloID, unidades} pairs
	// that define this juego. Built from domain.Receta.
	Receta domain.Receta

	// NombrePropuesto is the display name written to ARTICULOS.NOMBRE when no
	// match is found and the juego must be created. Must be non-empty.
	NombrePropuesto string

	// LineaArticuloID is the catalog line/category (LINEA_ARTICULO_ID) assigned
	// to a new ARTICULOS row. Required when creating; ignored when matching.
	LineaArticuloID int
}

// MicrosipJuegoResult is returned by MicrosipJuegoResolver.Resolve after the
// match-or-create logic completes within the caller's transaction.
type MicrosipJuegoResult struct {
	// ArticuloID is the ARTICULOS.ARTICULO_ID of the juego that matches the
	// recipe — either found or newly inserted.
	ArticuloID int

	// Creado is true when a new ARTICULOS row was inserted, false when an
	// existing juego was matched.
	Creado bool
}

// MicrosipJuegoResolver looks up an existing juego whose recipe exactly matches
// the given Receta (same component count, same {COMPONENTE_ID, UNIDADES} pairs)
// or creates a new one if none is found.
//
// Match is purely numeric — no NOMBRE comparison. Two juegos with the same
// components and quantities in any order are considered equal because Receta
// is canonically sorted by articuloID.
//
// The implementation must execute all reads and writes within the caller's
// ambient transaction via firebird.GetQuerier(ctx, pool).
type MicrosipJuegoResolver interface {
	Resolve(ctx context.Context, in MicrosipJuegoInput) (MicrosipJuegoResult, error)
}
