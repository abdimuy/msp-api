// Package ventsearch provides the Meilisearch-backed implementation of the
// ventas search index.
package ventsearch

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// upsertBatchSize is the maximum number of documents sent to Meilisearch in a
// single UpsertDocs call. 10000 is well within the Meilisearch recommended
// range and keeps individual payloads under ~5 MB for typical VentaDoc sizes.
const upsertBatchSize = 10_000

// moneyScale is the number of decimal places for monetary fields stored as
// exact strings in the index (precio_total_str).
const moneyScale int32 = 2

// MeilisearchVentaSearchIndex implements outbound.VentaSearchIndex using the
// platform Meilisearch client. It maps outbound.VentaSearchDoc → VentaDoc and
// bulk-upserts in batches of upsertBatchSize.
type MeilisearchVentaSearchIndex struct {
	client    platformmeili.Client
	indexName string
}

// NewMeilisearchVentaSearchIndex returns a MeilisearchVentaSearchIndex backed
// by the given platform client. indexName is the Meilisearch UID (e.g.
// "ventas").
func NewMeilisearchVentaSearchIndex(client platformmeili.Client, indexName string) *MeilisearchVentaSearchIndex {
	return &MeilisearchVentaSearchIndex{
		client:    client,
		indexName: indexName,
	}
}

// Compile-time assertion: MeilisearchVentaSearchIndex satisfies the port.
var _ outbound.VentaSearchIndex = (*MeilisearchVentaSearchIndex)(nil)

// Reconciliar maps each VentaSearchDoc to a VentaDoc and bulk-upserts them
// into the Meilisearch index in batches. Returns on the first batch error.
func (idx *MeilisearchVentaSearchIndex) Reconciliar(ctx context.Context, docs []outbound.VentaSearchDoc) error {
	for start := 0; start < len(docs); start += upsertBatchSize {
		end := start + upsertBatchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := make([]VentaDoc, 0, end-start)
		for _, d := range docs[start:end] {
			batch = append(batch, mapDoc(d))
		}
		if err := idx.client.UpsertDocs(ctx, idx.indexName, batch); err != nil {
			return fmt.Errorf("ventsearch: upsert batch [%d,%d): %w", start, end, err)
		}
	}
	return nil
}

// IndexarUno upserts a single venta document. Used by the incremental
// outbox reindex handler after every venta mutation.
func (idx *MeilisearchVentaSearchIndex) IndexarUno(ctx context.Context, doc outbound.VentaSearchDoc) error {
	if err := idx.client.UpsertDocs(ctx, idx.indexName, []VentaDoc{mapDoc(doc)}); err != nil {
		return fmt.Errorf("ventsearch: upsert one %s: %w", doc.ID, err)
	}
	return nil
}

// Eliminar removes a single venta document by ID.
func (idx *MeilisearchVentaSearchIndex) Eliminar(ctx context.Context, id uuid.UUID) error {
	if err := idx.client.DeleteDocs(ctx, idx.indexName, []string{id.String()}); err != nil {
		return fmt.Errorf("ventsearch: delete %s: %w", id, err)
	}
	return nil
}

// mapDoc projects an outbound.VentaSearchDoc to the wire-level VentaDoc,
// computing derived fields (ID string, money float+str, date epoch-seconds +
// display string).
func mapDoc(d outbound.VentaSearchDoc) VentaDoc {
	vd := VentaDoc{
		ID:             d.ID.String(),
		NombreCliente:  d.NombreCliente,
		Telefono:       d.Telefono,
		Direccion:      d.Direccion,
		Folio:          d.Folio,
		Vendedor:       d.Vendedor,
		TipoVenta:      d.TipoVenta,
		Situacion:      d.Situacion,
		Sincronizacion: d.Sincronizacion,
		ZonaClienteID:  d.ZonaClienteID,
		VendedorEmail:  d.VendedorEmail,
		ClienteID:      d.ClienteID,
		Estado:         d.Estado,
		PrecioTotal:    d.PrecioTotal.InexactFloat64(), // numeric: sort/filter key only
		PrecioTotalStr: d.PrecioTotal.StringFixed(moneyScale),
	}

	// Store FechaVenta as epoch-seconds for sortable/filterable numeric, and
	// as an RFC3339 UTC string for display. Zero time → 0 / empty.
	if !d.FechaVenta.IsZero() {
		vd.FechaVentaTs = d.FechaVenta.UTC().Unix()
		vd.FechaVenta = d.FechaVenta.UTC().Format(time.RFC3339)
	}

	// Store CreatedAt as epoch-seconds for sortable numeric, and as an
	// RFC3339 UTC string for display. Zero time → 0 / empty (sorts last via
	// omitempty).
	if !d.CreatedAt.IsZero() {
		vd.CreatedAtTs = d.CreatedAt.UTC().Unix()
		vd.CreatedAt = d.CreatedAt.UTC().Format(time.RFC3339)
	}

	return vd
}
