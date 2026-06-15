//nolint:misspell // Spanish domain vocabulary (clientes, buscar, reconciliar, etc.) per project convention.
package search

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis/analyzer/custom"
	"github.com/blevesearch/bleve/v2/analysis/char/asciifolding"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// Compile-time assertion: BleveIndex satisfies the SearchIndex port.
var _ outbound.SearchIndex = (*BleveIndex)(nil)

const (
	// analyzerName is the name registered in the Bleve index mapping for the
	// custom accent-insensitive, case-insensitive analyzer.
	analyzerName = "msp_clientes"

	// textoField is the name of the indexed text field on each document.
	textoField = "texto"

	// batchSize controls how many documents are flushed per Bleve batch
	// during Reconciliar. Batching is significantly faster than one-by-one
	// calls to Index().
	batchSize = 500

	// defaultFuzziness allows a Levenshtein edit-distance of 1 on each term
	// — tolerates single-character typos without degrading precision too much.
	defaultFuzziness = 1
)

// indexHolder wraps a bleve.Index so that atomic.Pointer can store an interface
// behind a concrete pointer (atomic.Pointer requires a concrete type).
type indexHolder struct {
	idx bleve.Index
}

// BleveIndex is the Bleve-backed in-memory full-text search index for the
// clientes module. It satisfies outbound.SearchIndex.
//
// The live index is held behind an atomic pointer so that Reconciliar can
// perform a full atomic swap — concurrent Buscar calls always see either
// the old or the new index, never a partial build.
//
// The index is memory-only (bleve.NewMemOnly); it is rebuilt from Firebird on
// a background schedule and does not persist between restarts.
type BleveIndex struct {
	// current is an *indexHolder (or nil before the first reconciliation).
	// Use loadCurrent / storeCurrent helpers for all access.
	current atomic.Pointer[indexHolder]

	// reconcileMu serialises concurrent Reconciliar calls so we never build
	// two replacement indexes simultaneously.
	reconcileMu sync.Mutex
}

// loadCurrent returns the active bleve.Index, or nil when not yet ready.
func (b *BleveIndex) loadCurrent() bleve.Index {
	h := b.current.Load()
	if h == nil {
		return nil
	}
	return h.idx
}

// storeCurrent atomically installs a new index and returns the previously
// active one (or nil on first install). The caller must Close the old index.
func (b *BleveIndex) storeCurrent(idx bleve.Index) bleve.Index {
	newHolder := &indexHolder{idx: idx}
	oldHolder := b.current.Swap(newHolder)
	if oldHolder == nil {
		return nil
	}
	return oldHolder.idx
}

// EstaListo reports whether the index has been populated and is safe to query.
// It returns false before the first successful Reconciliar call completes.
func (b *BleveIndex) EstaListo() bool {
	return b.current.Load() != nil
}

// Reconciliar performs a full rebuild of the search index from docs and then
// atomically swaps the new index in place of the old one. Concurrent Buscar
// calls continue serving the old index until the swap completes.
//
// A call with an empty docs slice is valid: it installs an empty index and
// marks the index as ready (EstaListo returns true afterwards).
func (b *BleveIndex) Reconciliar(ctx context.Context, docs []outbound.SearchDoc) error {
	b.reconcileMu.Lock()
	defer b.reconcileMu.Unlock()

	newIdx, err := newMemIndex()
	if err != nil {
		return err
	}

	// Index documents in batches for throughput (~44k docs in ~1-2s).
	batch := newIdx.NewBatch()
	for i, doc := range docs {
		if ctx.Err() != nil {
			_ = newIdx.Close()
			return ctx.Err()
		}

		docID := strconv.Itoa(doc.ClienteID)
		if err := batch.Index(docID, map[string]interface{}{textoField: doc.Texto}); err != nil {
			_ = newIdx.Close()
			return err
		}

		// Flush the batch every batchSize documents and on the last document.
		if (i+1)%batchSize == 0 || i == len(docs)-1 {
			if err := newIdx.Batch(batch); err != nil {
				_ = newIdx.Close()
				return err
			}
			batch.Reset()
		}
	}

	// Atomically replace the live index; close the old one.
	if old := b.storeCurrent(newIdx); old != nil {
		_ = old.Close()
	}
	return nil
}

// Buscar returns up to limit client IDs whose indexed text best matches query,
// ordered by descending Bleve relevance score.
//
// It is safe to call before the first Reconciliar: an empty slice is returned
// with no error, so callers can degrade gracefully (EstaListo guards whether to
// use the FTS path or the SQL fallback).
func (b *BleveIndex) Buscar(ctx context.Context, queryStr string, limit int) ([]int, error) {
	if queryStr == "" || limit <= 0 {
		return []int{}, nil
	}

	idx := b.loadCurrent()
	if idx == nil {
		return []int{}, nil
	}

	// MatchQuery runs the input through the same custom analyzer used at index
	// time: asciifolding → unicode tokenize → to_lower. Each term is matched
	// with Fuzziness 1 to absorb single-character typos. Operator OR means a
	// document needs to contain at least one term (natural multi-term ranking).
	mq := bleve.NewMatchQuery(queryStr)
	mq.SetField(textoField)
	mq.SetFuzziness(defaultFuzziness)
	mq.SetOperator(query.MatchQueryOperatorOr)
	mq.Analyzer = analyzerName

	req := bleve.NewSearchRequestOptions(mq, limit, 0, false)

	// Honour context cancellation before issuing the search.
	if err := ctx.Err(); err != nil {
		return []int{}, err
	}

	res, err := idx.SearchInContext(ctx, req)
	if err != nil {
		return []int{}, err
	}

	ids := make([]int, 0, len(res.Hits))
	for _, hit := range res.Hits {
		id, err := strconv.Atoi(hit.ID)
		if err != nil {
			// Defensive: skip malformed IDs rather than aborting the result set.
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// newMemIndex builds a fresh in-memory Bleve index with the custom
// accent-insensitive, case-insensitive analyzer attached to textoField.
func newMemIndex() (bleve.Index, error) {
	im := mapping.NewIndexMapping()

	// Register the custom analyzer:
	//   char filter  → asciifolding   (García → garcia, Ángeles → angeles)
	//   tokenizer    → unicode        (word-boundary splitting, unicode-aware)
	//   token filter → to_lower       (lowercase everything)
	analyzerDef := map[string]interface{}{
		"type":         custom.Name,
		"char_filters": []string{asciifolding.Name},
		"tokenizer":    unicode.Name,
		"token_filters": []string{
			lowercase.Name,
		},
	}
	if err := im.AddCustomAnalyzer(analyzerName, analyzerDef); err != nil {
		return nil, err
	}

	// Map textoField to a text field with our custom analyzer.
	textField := mapping.NewTextFieldMapping()
	textField.Analyzer = analyzerName

	docMapping := mapping.NewDocumentMapping()
	docMapping.AddFieldMappingsAt(textoField, textField)

	// Disable dynamic field indexing so only textoField is stored/analyzed.
	im.DefaultMapping = docMapping
	im.DefaultMapping.Dynamic = false

	return bleve.NewMemOnly(im)
}
