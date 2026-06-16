// Package clientessearch provides the Meilisearch-backed implementation of the
// clientes directory index.
//
//nolint:misspell // Spanish domain vocabulary (directorio, clientes, etc.) by project convention.
package clientessearch

import (
	"context"
	"fmt"
	"strconv"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// upsertBatchSize is the maximum number of documents sent to Meilisearch in a
// single UpsertDocs call. 10000 is well within the Meilisearch recommended
// range and keeps individual payloads under ~5 MB for typical ClienteDoc sizes.
const upsertBatchSize = 10_000

// MeilisearchDirectoryIndex implements outbound.DirectoryIndex using the
// platform Meilisearch client. It maps outbound.DirectorioDoc → ClienteDoc and
// bulk-upserts in batches of upsertBatchSize.
//
// Deletion is out of scope: this reconcile is additive only. Clients that leave
// ESTATUS A+B are near-zero in practice; a future sweep job can handle them.
type MeilisearchDirectoryIndex struct {
	client    platformmeili.Client
	indexName string
}

// NewMeilisearchDirectoryIndex returns a MeilisearchDirectoryIndex backed by
// the given platform client. indexName is the Meilisearch UID (e.g. "clientes").
func NewMeilisearchDirectoryIndex(client platformmeili.Client, indexName string) *MeilisearchDirectoryIndex {
	return &MeilisearchDirectoryIndex{
		client:    client,
		indexName: indexName,
	}
}

// Compile-time assertion: MeilisearchDirectoryIndex satisfies the port.
var _ outbound.DirectoryIndex = (*MeilisearchDirectoryIndex)(nil)

// Reconciliar maps each DirectorioDoc to a ClienteDoc and bulk-upserts them
// into the Meilisearch index in batches. Returns on the first batch error.
func (idx *MeilisearchDirectoryIndex) Reconciliar(ctx context.Context, docs []outbound.DirectorioDoc) error {
	for start := 0; start < len(docs); start += upsertBatchSize {
		end := start + upsertBatchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := make([]ClienteDoc, 0, end-start)
		for _, d := range docs[start:end] {
			batch = append(batch, mapDoc(d))
		}
		if err := idx.client.UpsertDocs(ctx, idx.indexName, batch); err != nil {
			return fmt.Errorf("clientessearch: upsert batch [%d,%d): %w", start, end, err)
		}
	}
	return nil
}

// mapDoc projects an outbound.DirectorioDoc to the wire-level ClienteDoc,
// computing derived fields (ID string, ordinals, con_saldo, direccion_corta).
func mapDoc(d outbound.DirectorioDoc) ClienteDoc {
	return ClienteDoc{
		ID:                 strconv.Itoa(d.ClienteID),
		ClienteID:          d.ClienteID,
		Nombre:             d.Nombre,
		Direccion:          d.Direccion,
		DireccionCalle:     d.DireccionCalle,
		DireccionColonia:   d.DireccionColonia,
		DireccionPoblacion: d.DireccionPoblacion,
		DireccionCorta:     d.DireccionCorta,
		ZonaID:             d.ZonaID,
		ZonaNombre:         d.ZonaNombre,
		CobradorID:         d.CobradorID,
		ConSaldo:           d.ConSaldo,
		Segmento:           d.Segmento,
		EstadoPago:         d.EstadoPago,
		Score:              d.Score,
		RecenciaDias:       d.RecenciaDias,
		Estatus:            d.Estatus,
		SegmentoOrden:      segmentoOrdinal(d.Segmento),
		EstadoPagoOrden:    estadoPagoOrdinal(d.EstadoPago),
		Saldo:              d.Saldo.InexactFloat64(),
		Telefono:           d.Telefono,
		Frecuencia:         d.Frecuencia,
		Monetary:           d.Monetary.InexactFloat64(),
		NextBestProduct:    d.NextBestProduct,
		TienePulso:         d.TienePulso,
	}
}

// ── Ordinal helpers ────────────────────────────────────────────────────────────
//
// These mirror the ordering in internal/clientes/app/buscar_clientes.go
// (segmentoOrdinal / estadoPagoOrdinal). They are duplicated here so the
// index can store a sortable integer alongside the string label; the app
// layer's sort still uses its own copy to avoid a cross-layer import.
// If the ordering ever changes, update BOTH sites.

// Canonical segmento string values (analytics wire vocabulary).
const (
	segLealPorLiquidar = "LEAL_POR_LIQUIDAR"
	segDormidoValioso  = "DORMIDO_VALIOSO"
	segActivo          = "ACTIVO"
	segNuevo           = "NUEVO"
	segFrio            = "FRIO"
	segPerdido         = "PERDIDO"
)

// Canonical estado_pago string values.
const (
	epAlCorriente = "AL_CORRIENTE"
	epLiquidado   = "LIQUIDADO"
	epSinCredito  = "SIN_CREDITO"
	epAtrasado    = "ATRASADO"
	epMoroso      = "MOROSO"
)

// segmentoOrdinal maps a segmento label to a sort ordinal.
// Order: LEAL_POR_LIQUIDAR(0) < DORMIDO_VALIOSO(1) < ACTIVO(2) < NUEVO(3) < FRIO(4) < PERDIDO(5).
// Unknown segmentos → 6 (sort last).
func segmentoOrdinal(s string) int {
	switch s {
	case segLealPorLiquidar:
		return 0
	case segDormidoValioso:
		return 1
	case segActivo:
		return 2
	case segNuevo:
		return 3
	case segFrio:
		return 4
	case segPerdido:
		return 5
	default:
		return 6
	}
}

// estadoPagoOrdinal maps a payment-state label to a sort ordinal ordered by
// solvency (healthiest first). Lower ordinal = healthier.
// Order: AL_CORRIENTE(0) < LIQUIDADO(1) < SIN_CREDITO(2) < ATRASADO(3) < MOROSO(4).
// Unknown states → 5 (sort last).
func estadoPagoOrdinal(s string) int {
	switch s {
	case epAlCorriente:
		return 0
	case epLiquidado:
		return 1
	case epSinCredito:
		return 2
	case epAtrasado:
		return 3
	case epMoroso:
		return 4
	default:
		return 5
	}
}
