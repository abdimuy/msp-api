// export_test.go exposes internal helpers for white-box testing without
// polluting the production API.
package clientessearch

import (
	"context"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// SegmentoOrdinalForTest exposes segmentoOrdinal for unit tests.
func SegmentoOrdinalForTest(s string) int { return segmentoOrdinal(s) }

// ClienteDocToDirectorioDocForTest exposes the read-path mapping (Meilisearch
// hit → port doc) so tests can assert the exact money round-trip.
func ClienteDocToDirectorioDocForTest(cd ClienteDoc) outbound.DirectorioDoc {
	return clienteDocToDirectorioDoc(cd)
}

// EstadoPagoOrdinalForTest exposes estadoPagoOrdinal for unit tests.
func EstadoPagoOrdinalForTest(s string) int { return estadoPagoOrdinal(s) }

// ClienteDocForTest is an alias for ClienteDoc used in test assertions so tests
// in the _test package can reference the concrete type without importing the
// internal struct directly (the _test package is in package clientessearch_test).
type ClienteDocForTest = ClienteDoc

// ExtractBatch casts the docs value passed to UpsertDocs back to []ClienteDoc
// so test fakes can inspect the mapped documents. The cast is safe because
// Reconciliar always passes []ClienteDoc to UpsertDocs.
func ExtractBatch(docs any) []ClienteDoc {
	if docs == nil {
		return nil
	}
	if batch, ok := docs.([]ClienteDoc); ok {
		return batch
	}
	return nil
}

// upsertOnlyClient is an interface satisfied by the test fakes below. It is
// narrower than platformmeili.Client — only the method that Reconciliar actually
// calls. Used exclusively from export_test.go.
type upsertOnlyClient interface {
	UpsertDocs(ctx context.Context, indexUID string, docs any) error
}

// NewMeilisearchDirectoryIndexForTest constructs a MeilisearchDirectoryIndex
// backed by any value that exposes UpsertDocs. The fake need not implement the
// full platformmeili.Client interface.
func NewMeilisearchDirectoryIndexForTest(client upsertOnlyClient, indexName string) *MeilisearchDirectoryIndex {
	return &MeilisearchDirectoryIndex{
		client:    &upsertOnlyAdapter{inner: client},
		indexName: indexName,
	}
}

// upsertOnlyAdapter wraps an upsertOnlyClient to satisfy platformmeili.Client.
// The methods other than UpsertDocs panic — they must not be called in tests
// that use this adapter.
type upsertOnlyAdapter struct {
	inner upsertOnlyClient
}

func (a *upsertOnlyAdapter) EnsureIndex(_ context.Context, _ platformmeili.IndexConfig) error {
	panic("upsertOnlyAdapter.EnsureIndex called — not supported in this test fake")
}

func (a *upsertOnlyAdapter) UpsertDocs(ctx context.Context, indexUID string, docs any) error {
	return a.inner.UpsertDocs(ctx, indexUID, docs)
}

func (a *upsertOnlyAdapter) DeleteDocs(_ context.Context, _ string, _ []string) error {
	panic("upsertOnlyAdapter.DeleteDocs called — not supported in this test fake")
}

func (a *upsertOnlyAdapter) Search(
	_ context.Context,
	_ string,
	_ platformmeili.SearchParams,
) (platformmeili.SearchResult, error) {
	panic("upsertOnlyAdapter.Search called — not supported in this test fake")
}

func (a *upsertOnlyAdapter) Close() {}

// Compile-time assertion: upsertOnlyAdapter satisfies the full platform interface.
var _ platformmeili.Client = (*upsertOnlyAdapter)(nil)

// BuildFilterForTest exposes buildFilter for white-box unit tests.
func BuildFilterForTest(q outbound.DirectorioQuery) string { return buildFilter(q) }

// BuildSortForTest exposes buildSort for white-box unit tests.
func BuildSortForTest(sortBy, sortOrder, query string) []string {
	return buildSort(sortBy, sortOrder, query)
}
