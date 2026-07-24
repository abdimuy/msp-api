// Package microsip is the cross-module surface of the microsip catalog
// bounded context. Other modules import only this package — never
// internal/microsip/domain, internal/microsip/app, or
// internal/microsip/infra. The depguard linter enforces the rule.
//
// The contract exports:
//   - Catalogo: a read-only query interface over the in-stock article
//     catalog, consumed by the reactivación module's next-best-product
//     reader (and any future consumer that needs to suggest a real product).
//   - ArticuloCatalogo: a flat DTO whose legacy concatenated Precios string
//     has already been parsed into a per-list price map.
//
//nolint:misspell // Spanish vocabulary (Articulo, Categoria) per project convention.
package microsip

import (
	"context"

	"github.com/shopspring/decimal"
)

// ArticuloCatalogo is the projected, cross-module view of a single in-stock
// article. It is a flat struct of primitive values so consumers never need
// to import the microsip domain types. Unlike the raw domain row, its Precios
// field is already parsed from the legacy "<LISTA>:<PRECIO>,..." string into a
// name→price map (see parsePrecios).
type ArticuloCatalogo struct {
	// ArticuloID is the Microsip ARTICULO_ID.
	ArticuloID int
	// LineaArticuloID is the Microsip LINEA_ARTICULO_ID (the product line /
	// category the article belongs to).
	LineaArticuloID int
	// Nombre is the article's display name (ARTICULOS.NOMBRE).
	Nombre string
	// Categoria is the product line's display name (LINEAS_ARTICULOS.NOMBRE).
	Categoria string
	// Existencias is the summed stock across the queried almacén (> 0).
	Existencias int64
	// Precios maps each configured price-list NAME (e.g. "MUEBLERIAS",
	// "CONTADO") to its price. Lists absent from the article's row are simply
	// missing from the map — callers must tolerate absences.
	Precios map[string]decimal.Decimal
}

// Catalogo is the read-only query surface of the microsip catalog. Consumers
// depend on this interface (not on internal/microsip/app.Service) so the
// dependency direction stays: consumer → microsip (contract) → microsip/app.
type Catalogo interface {
	// ListarEnStock returns the articles with positive existencias in the
	// given almacén, optionally filtered by a name substring (empty matches
	// all). Prices come pre-parsed into ArticuloCatalogo.Precios.
	ListarEnStock(ctx context.Context, almacenID int, buscar string) ([]ArticuloCatalogo, error)
}
