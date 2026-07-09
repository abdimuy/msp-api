// Package ventsearch provides the Meilisearch-backed implementation of the
// ventas search index.
package ventsearch

import (
	"context"
	"fmt"
	"sync"
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

	// mu guards configured. See ensureIndexOnce.
	mu         sync.Mutex
	configured bool
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
//
// Before upserting, it unconditionally (re-)applies the index's full
// configuration (primaryKey + searchable/filterable/sortable/ranking/
// pagination, from DefaultIndexConfig) via EnsureIndex. This guards against
// the race where a previous UpsertDocs (from this or another process) was
// the call that auto-created the index: Meilisearch gives an
// auto-created index default settings (searchableAttributes=["*"],
// filterableAttributes=[], sortableAttributes=[]), which makes any filtered
// or sorted search fail. Re-applying identical settings on an
// already-configured index is cheap — Meilisearch diffs the incoming
// settings against the stored ones and only recomputes the structures for
// attributes that actually changed — so doing this on every reconcile tick
// is safe and doubles as self-healing if the index is ever deleted or reset
// at runtime, without requiring an API restart. See
// .superpowers/sdd/fix-settings-race-brief.md for the incident this closes.
func (idx *MeilisearchVentaSearchIndex) Reconciliar(ctx context.Context, docs []outbound.VentaSearchDoc) error {
	if len(docs) == 0 {
		return nil
	}
	if err := idx.ensureIndexConfigured(ctx); err != nil {
		return fmt.Errorf("ventsearch: ensure index config: %w", err)
	}
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
//
// Before the first upsert, it ensures the index carries its full
// configuration exactly once per process (see ensureIndexOnce) — this
// closes the window where an outbox event fires before the reconcile
// worker's warm-up tick (which also calls EnsureIndex, unconditionally —
// see Reconciliar) has had a chance to run, and would otherwise be the call
// that auto-creates a "bare" index with default (empty) filterable/sortable
// settings.
func (idx *MeilisearchVentaSearchIndex) IndexarUno(ctx context.Context, doc outbound.VentaSearchDoc) error {
	if err := idx.ensureIndexOnce(ctx); err != nil {
		return fmt.Errorf("ventsearch: ensure index config: %w", err)
	}
	if err := idx.client.UpsertDocs(ctx, idx.indexName, []VentaDoc{mapDoc(doc)}); err != nil {
		return fmt.Errorf("ventsearch: upsert one %s: %w", doc.ID, err)
	}
	return nil
}

// ensureIndexConfigured unconditionally (re-)applies the index's full
// DefaultIndexConfig via EnsureIndex, then marks the index as configured so
// ensureIndexOnce can short-circuit. Called by Reconciliar on every tick —
// see that method's doc comment for why re-applying identical settings
// every time is safe and desirable (self-healing).
func (idx *MeilisearchVentaSearchIndex) ensureIndexConfigured(ctx context.Context) error {
	if err := idx.client.EnsureIndex(ctx, DefaultIndexConfig(idx.indexName)); err != nil {
		return err
	}
	idx.mu.Lock()
	idx.configured = true
	idx.mu.Unlock()
	return nil
}

// ensureIndexOnce calls EnsureIndex only if this instance has not yet
// observed a successful configuration (from either Reconciliar or a prior
// IndexarUno call). It exists to avoid re-applying settings on every single
// incremental upsert (which can fire many times per minute), while still
// guaranteeing the very first write from this process — whichever path
// reaches it first — configures the index before creating it "bare".
//
// If EnsureIndex fails, configured stays false so the NEXT call (from
// either path) retries; a transient Meilisearch outage does not permanently
// wedge the index in an unconfigured state.
func (idx *MeilisearchVentaSearchIndex) ensureIndexOnce(ctx context.Context) error {
	idx.mu.Lock()
	already := idx.configured
	idx.mu.Unlock()
	if already {
		return nil
	}
	return idx.ensureIndexConfigured(ctx)
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
