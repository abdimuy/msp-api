// Package microsiphttp hosts the HTTP transport for the microsip catalog
// module: Huma I/O DTOs, handlers, and the chi-mounted router.
package microsiphttp

import (
	"github.com/abdimuy/msp-api/internal/microsip/domain"
)

// AlmacenDTO is the public shape of an almacen. Field names are
// snake_case to match the convention used by the ventas v2 surface.
type AlmacenDTO struct {
	AlmacenID   int    `json:"almacen_id"  doc:"Identificador interno del almacén en Microsip"`
	Almacen     string `json:"almacen"     doc:"Nombre del almacén"`
	Existencias int64  `json:"existencias" doc:"Total de unidades en el almacén (entradas - salidas)"`
}

// ArticuloAlmacenDTO is the public shape of an articulo entry inside an
// almacen. Precios is the legacy concatenated format
// "<LISTA>:<PRECIO>,<LISTA>:<PRECIO>" — kept verbatim so the frontend
// adapter does not need to be touched.
type ArticuloAlmacenDTO struct {
	ArticuloID      int    `json:"articulo_id"`
	Articulo        string `json:"articulo"`
	Existencias     int64  `json:"existencias"      doc:"Unidades en stock para este artículo en el almacén"`
	LineaArticuloID int    `json:"linea_articulo_id"`
	LineaArticulo   string `json:"linea_articulo"`
	Precios         string `json:"precios"          doc:"Cadena <lista>:<precio> separada por comas, p. ej. \"MUEBLERIAS:1234.56,CONTADO:1000.00\""`
}

// ZonaClienteDTO is the public shape of a zona. Zona is the raw
// Microsip name concatenated with the top cobrador (cf. domain layer
// docs) so this DTO ships exactly what the legacy API did.
type ZonaClienteDTO struct {
	ZonaClienteID int    `json:"zona_cliente_id"`
	ZonaCliente   string `json:"zona_cliente"`
}

// listResponse[T] is the envelope every list endpoint returns. Keeping the
// items under "items" matches the convention used by every other v2
// list endpoint (ventas, cobranza, etc.).
type listResponse[T any] struct {
	Items []T `json:"items"`
}

// ─── Input wrappers ──────────────────────────────────────────────────────

// ListarAlmacenesInput is empty — the endpoint takes no parameters.
type ListarAlmacenesInput struct{}

// ListarAlmacenesOutput is the response envelope.
type ListarAlmacenesOutput struct {
	Body listResponse[AlmacenDTO]
}

// ObtenerAlmacenInput is the single-almacen path-param input.
type ObtenerAlmacenInput struct {
	ID int `path:"id" doc:"Identificador del almacén en Microsip" minimum:"1"`
}

// ObtenerAlmacenOutput is the single-almacen response.
type ObtenerAlmacenOutput struct {
	Body AlmacenDTO
}

// ListarArticulosDelAlmacenInput captures the path param plus the optional
// substring filter applied via CONTAINING.
type ListarArticulosDelAlmacenInput struct {
	ID     int    `path:"id"      doc:"Identificador del almacén en Microsip" minimum:"1"`
	Buscar string `query:"buscar" doc:"Subcadena (case-insensitive) buscada en el nombre del artículo. Vacío = sin filtro."`
}

// ListarArticulosDelAlmacenOutput is the article-list envelope.
type ListarArticulosDelAlmacenOutput struct {
	Body listResponse[ArticuloAlmacenDTO]
}

// ListarZonasClienteInput is empty — the endpoint takes no parameters.
type ListarZonasClienteInput struct{}

// ListarZonasClienteOutput is the zonas envelope.
type ListarZonasClienteOutput struct {
	Body listResponse[ZonaClienteDTO]
}

// ─── Mappers ─────────────────────────────────────────────────────────────

func toAlmacenDTO(a domain.Almacen) AlmacenDTO {
	return AlmacenDTO{
		AlmacenID:   a.ID,
		Almacen:     a.Nombre,
		Existencias: a.Existencias,
	}
}

func toArticuloAlmacenDTO(a domain.ArticuloAlmacen) ArticuloAlmacenDTO {
	return ArticuloAlmacenDTO{
		ArticuloID:      a.ArticuloID,
		Articulo:        a.Articulo,
		Existencias:     a.Existencias,
		LineaArticuloID: a.LineaArticuloID,
		LineaArticulo:   a.LineaArticulo,
		Precios:         a.Precios,
	}
}

func toZonaClienteDTO(z domain.ZonaCliente) ZonaClienteDTO {
	return ZonaClienteDTO{
		ZonaClienteID: z.ID,
		ZonaCliente:   z.Nombre,
	}
}
