//nolint:misspell // Spanish vocabulary (Articulo, Precios, Categoria) per project convention.
package microsip

import (
	"strings"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/microsip/domain"
)

// ArticuloCatalogoFromDomain projects a domain.ArticuloAlmacen onto the
// cross-module ArticuloCatalogo DTO, parsing the legacy concatenated Precios
// string into a name→price map. It is called only from the ServiceAdapter in
// this package.
func ArticuloCatalogoFromDomain(a domain.ArticuloAlmacen) ArticuloCatalogo {
	return ArticuloCatalogo{
		ArticuloID:      a.ArticuloID,
		LineaArticuloID: a.LineaArticuloID,
		Nombre:          a.Articulo,
		Categoria:       a.LineaArticulo,
		Existencias:     a.Existencias,
		Precios:         parsePrecios(a.Precios),
	}
}

// parsePrecios parses the legacy Firebird LIST-aggregate string
// "<NOMBRE_LISTA>:<PRECIO>,<NOMBRE_LISTA>:<PRECIO>" into a name→price map.
//
// The order of entries is non-deterministic (LIST does not guarantee it), so
// parsing is BY NAME. Malformed or unparseable entries — a missing ':', an
// empty name, a non-numeric price — are tolerated and skipped rather than
// failing the whole article: a partial price map is more useful than none, and
// consumers already tolerate absent lists. An empty input yields an empty
// (non-nil) map.
func parsePrecios(raw string) map[string]decimal.Decimal {
	out := make(map[string]decimal.Decimal)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	for _, entry := range strings.Split(raw, ",") {
		nombre, precioStr, ok := strings.Cut(entry, ":")
		if !ok {
			continue
		}
		nombre = strings.TrimSpace(nombre)
		if nombre == "" {
			continue
		}
		precio, err := decimal.NewFromString(strings.TrimSpace(precioStr))
		if err != nil {
			continue
		}
		out[nombre] = precio
	}
	return out
}
