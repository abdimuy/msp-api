// export_test.go exposes internal helpers for white-box testing without
// polluting the production API.
package ventsearch

import (
	"context"

	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"

	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// MapDocForTest exposes mapDoc for unit tests.
func MapDocForTest(d outbound.VentaSearchDoc) VentaDoc { return mapDoc(d) }

// BuildFilterForTest exposes buildFilter for white-box unit tests.
func BuildFilterForTest(q outbound.VentasSearchQuery) string { return buildFilter(q) }

// BuildSortForTest exposes buildSort for white-box unit tests.
func BuildSortForTest(q outbound.VentasSearchQuery) []string { return buildSort(q) }

// VentaDocForTest is an alias for VentaDoc used in test assertions so tests
// in the _test package can reference the concrete type without importing the
// internal struct directly (the _test package is in package ventsearch_test).
type VentaDocForTest = VentaDoc

// ExtractBatch casts the docs value passed to UpsertDocs back to []VentaDoc
// so test fakes can inspect the mapped documents. The cast is safe because
// Reconciliar/IndexarUno always pass []VentaDoc to UpsertDocs.
func ExtractBatch(docs any) []VentaDoc {
	if docs == nil {
		return nil
	}
	if batch, ok := docs.([]VentaDoc); ok {
		return batch
	}
	return nil
}

// fakeClient is the interface satisfied by the test fakes in
// index_test.go/search_query_test.go. It is narrower than
// platformmeili.Client and only carries the methods actually exercised in
// unit tests. Used exclusively from export_test.go.
type fakeClient interface {
	UpsertDocs(ctx context.Context, indexUID string, docs any) error
	DeleteDocs(ctx context.Context, indexUID string, ids []string) error
	Search(ctx context.Context, indexUID string, params platformmeili.SearchParams) (platformmeili.SearchResult, error)
}

// NewMeilisearchVentaSearchIndexForTest constructs a MeilisearchVentaSearchIndex
// backed by any value that exposes UpsertDocs/DeleteDocs/Search. The fake need
// not implement the full platformmeili.Client interface.
func NewMeilisearchVentaSearchIndexForTest(client fakeClient, indexName string) *MeilisearchVentaSearchIndex {
	return &MeilisearchVentaSearchIndex{
		client:    &fakeClientAdapter{inner: client},
		indexName: indexName,
	}
}

// fakeClientAdapter wraps a fakeClient to satisfy platformmeili.Client. The
// methods other than UpsertDocs/DeleteDocs/Search panic — they must not be
// called in tests that use this adapter.
type fakeClientAdapter struct {
	inner fakeClient
}

func (a *fakeClientAdapter) EnsureIndex(_ context.Context, _ platformmeili.IndexConfig) error {
	panic("fakeClientAdapter.EnsureIndex called — not supported in this test fake")
}

func (a *fakeClientAdapter) UpsertDocs(ctx context.Context, indexUID string, docs any) error {
	return a.inner.UpsertDocs(ctx, indexUID, docs)
}

func (a *fakeClientAdapter) DeleteDocs(ctx context.Context, indexUID string, ids []string) error {
	return a.inner.DeleteDocs(ctx, indexUID, ids)
}

func (a *fakeClientAdapter) Search(
	ctx context.Context,
	indexUID string,
	params platformmeili.SearchParams,
) (platformmeili.SearchResult, error) {
	return a.inner.Search(ctx, indexUID, params)
}

func (a *fakeClientAdapter) Close() {}

// Compile-time assertion: fakeClientAdapter satisfies the full platform interface.
var _ platformmeili.Client = (*fakeClientAdapter)(nil)
