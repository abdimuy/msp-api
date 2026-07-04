//nolint:misspell // ventas vocabulary is Spanish per project convention.
package outbound

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// MaxTotalHitsVentas is the pagination cap shared between the Meilisearch
// index settings (PaginationMaxTotalHits) and the cursor-emission guard in
// the app layer. Both sites must use this constant so they stay in sync
// automatically.
const MaxTotalHitsVentas = 50_000

// VentaSearchDoc is the ports-level contract document for the Meilisearch
// ventas index. It is a plain struct with no JSON tags and no meilisearch
// import — infra maps it to the wire shape (ventsearch.VentaDoc).
//
// Fields mirror ventsearch.VentaDoc one-to-one so the mapping in the infra
// adapter is trivial. This package never imports internal/ventas/domain —
// the domain→VentaSearchDoc mapping is performed by the outbox reindex
// handler (Task 2), keeping this port free of domain dependencies.
type VentaSearchDoc struct {
	// ID is the venta's primary key. Serialized to string for the Meilisearch
	// document id.
	ID uuid.UUID

	// NombreCliente is the cliente snapshot name embedded in the venta.
	NombreCliente string
	// Telefono is the cliente snapshot phone number.
	Telefono string
	// Direccion is the combined address string (calle + colonia + poblacion +
	// ciudad) used for full-text search.
	Direccion string
	// Folio is the Microsip folio assigned once the venta is aplicada. Empty
	// when still pendiente.
	Folio string
	// Vendedor is the concatenated names of the venta's vendedores.
	Vendedor string

	// TipoVenta is domain.TipoVenta.String() ("CONTADO" | "CREDITO").
	TipoVenta string
	// Situacion is domain.Situacion.String() ("borrador" | "revisada" |
	// "aprobada" | "cancelada").
	Situacion string
	// Sincronizacion is domain.Sincronizacion.String() ("pendiente" |
	// "aplicada").
	Sincronizacion string
	// ZonaClienteID is the Microsip zona identifier for the venta's cliente.
	ZonaClienteID int
	// VendedorEmail is the email of the vendedor who registered the venta
	// (auth identity, not the display vendedor snapshot).
	VendedorEmail string
	// ClienteID is the Microsip CLIENTE_ID once linked; 0 when unlinked.
	ClienteID int
	// Estado is domain.EstadoRegistro.String() ("active" | "deleted").
	Estado string

	// FechaVenta is the sale date.
	FechaVenta time.Time
	// PrecioTotal is the exact total sale amount.
	PrecioTotal decimal.Decimal
	// CreatedAt is the venta's audit creation timestamp.
	CreatedAt time.Time
}

// VentasSearchQuery carries all parameters for a single Meilisearch ventas
// search. The app layer builds this from ListarVentasInput and passes it to
// VentaSearchIndex.Buscar; the infra adapter translates it to SearchParams.
type VentasSearchQuery struct {
	// Q is the full-text query string. Empty means browse (no text search).
	Q string

	// TipoVenta restricts to an exact tipo_venta value. Nil = no filter.
	TipoVenta *string
	// Situacion restricts to an exact situacion value. Nil = no filter.
	Situacion *string
	// Sincronizacion restricts to an exact sincronizacion value. Nil = no filter.
	Sincronizacion *string
	// ZonaClienteID restricts to a specific zona. Nil = no filter.
	ZonaClienteID *int
	// VendedorEmail restricts to a specific vendedor email. Nil = no filter.
	VendedorEmail *string
	// ClienteID restricts to a specific Microsip cliente. Nil = no filter.
	ClienteID *int
	// Estado restricts to an exact estado value. Nil = no filter.
	Estado *string
	// IncluirCanceladas, when false (default), excludes ventas whose situacion
	// is "cancelada". When true, canceled ventas are included in results.
	IncluirCanceladas bool
	// FechaDesde restricts to ventas with FechaVenta >= this instant. Nil = no
	// lower bound.
	FechaDesde *time.Time
	// FechaHasta restricts to ventas with FechaVenta < this instant. Nil = no
	// upper bound.
	FechaHasta *time.Time
	// PrecioMin restricts to ventas with PrecioTotal >= this amount. Nil = no
	// lower bound.
	PrecioMin *decimal.Decimal
	// PrecioMax restricts to ventas with PrecioTotal < this amount. Nil = no
	// upper bound.
	PrecioMax *decimal.Decimal

	// SortBy is the sort column ("fecha_venta" | "precio_total" |
	// "nombre_cliente"). Empty means default order (fecha_venta:desc when Q
	// is empty, Meilisearch relevance when Q is set).
	SortBy string
	// SortOrder is "asc" or "desc" (default "asc" when SortBy is set).
	SortOrder string
	// Offset is the 0-based result index to start from (for offset pagination).
	Offset int
	// Limit is the maximum number of results to return. Default 50 if zero.
	Limit int
}

// VentasSearchResultado is the result of a VentaSearchIndex.Buscar call. It
// carries ordered IDs only — the read path hydrates each venta by ID from
// Firebird to keep the VentaDTO contract byte-identical.
type VentasSearchResultado struct {
	// IDs is the page of matched venta IDs, in Meilisearch result order.
	IDs []uuid.UUID
	// Total is the estimated total number of hits for this query, clamped to
	// MaxTotalHitsVentas.
	Total int
}

// VentaSearchIndex is the outbound port for the Meilisearch ventas index. The
// implementation lives in infra/ventsearch.
type VentaSearchIndex interface {
	// Buscar executes a ventas search against the Meilisearch index and
	// returns the matched venta IDs in result order together with the
	// estimated total. Returns an apperror with KindServiceUnavailable when
	// Meilisearch is not configured or transiently unavailable — callers must
	// surface this as HTTP 503 or fall back to the Firebird keyset listing.
	Buscar(ctx context.Context, q VentasSearchQuery) (VentasSearchResultado, error)

	// Reconciliar bulk-upserts the full given venta set into the index. Used
	// by the periodic reconcile worker (warm-up + drift recovery).
	Reconciliar(ctx context.Context, docs []VentaSearchDoc) error

	// IndexarUno upserts a single venta document. Used by the incremental
	// outbox reindex handler after every venta mutation.
	IndexarUno(ctx context.Context, doc VentaSearchDoc) error

	// Eliminar removes a single venta document by ID. Cancellation is modeled
	// as a soft-delete upsert (situacion="cancelada"), NOT a call to Eliminar
	// — this method exists for the rare case a venta must be purged outright
	// (e.g. created by mistake and hard-deleted).
	Eliminar(ctx context.Context, id uuid.UUID) error
}
