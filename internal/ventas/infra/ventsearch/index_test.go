package ventsearch_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ventsearchmeili "github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platformmeili "github.com/abdimuy/msp-api/internal/platform/meilisearch"
)

// recorder satisfies the narrower fakeClient interface exposed via
// NewMeilisearchVentaSearchIndexForTest. It records upsert/delete calls and
// returns a canned search result for assertions.
type recorder struct {
	upsertBatches [][]ventsearchmeili.VentaDocForTest
	deleteCalls   [][]string
	indexUID      string

	upsertErr error
	deleteErr error

	searchResult platformmeili.SearchResult
	searchErr    error
	searchParams platformmeili.SearchParams
}

func (r *recorder) UpsertDocs(_ context.Context, indexUID string, docs any) error {
	if r.upsertErr != nil {
		return r.upsertErr
	}
	r.indexUID = indexUID
	r.upsertBatches = append(r.upsertBatches, ventsearchmeili.ExtractBatch(docs))
	return nil
}

func (r *recorder) DeleteDocs(_ context.Context, indexUID string, ids []string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.indexUID = indexUID
	r.deleteCalls = append(r.deleteCalls, ids)
	return nil
}

func (r *recorder) Search(
	_ context.Context,
	indexUID string,
	params platformmeili.SearchParams,
) (platformmeili.SearchResult, error) {
	r.indexUID = indexUID
	r.searchParams = params
	if r.searchErr != nil {
		return platformmeili.SearchResult{}, r.searchErr
	}
	return r.searchResult, nil
}

// ── Reconciliar ───────────────────────────────────────────────────────────

func TestReconciliar_MapsFieldsAndSendsToCorrectIndex(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "my-ventas")

	id := uuid.New()
	doc := outbound.VentaSearchDoc{
		ID:            id,
		NombreCliente: "JUAN PEREZ",
		PrecioTotal:   decimal.RequireFromString("1500.50"),
	}

	err := idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{doc})
	require.NoError(t, err)
	require.Len(t, rec.upsertBatches, 1)
	require.Len(t, rec.upsertBatches[0], 1)

	got := rec.upsertBatches[0][0]
	assert.Equal(t, id.String(), got.ID)
	assert.Equal(t, "JUAN PEREZ", got.NombreCliente)
	assert.Equal(t, "1500.50", got.PrecioTotalStr)
	assert.Equal(t, "my-ventas", rec.indexUID)
}

func TestReconciliar_EmptyInput_NoUpsertCall(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.Reconciliar(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, rec.upsertBatches)
}

func TestReconciliar_PropagatesUpsertError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("boom")
	rec := &recorder{upsertErr: wantErr}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{{ID: uuid.New()}})
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// ── IndexarUno ────────────────────────────────────────────────────────────

func TestIndexarUno_UpsertsSingleDoc(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	id := uuid.New()
	doc := outbound.VentaSearchDoc{ID: id, NombreCliente: "ANA GOMEZ"}

	err := idx.IndexarUno(context.Background(), doc)
	require.NoError(t, err)
	require.Len(t, rec.upsertBatches, 1)
	require.Len(t, rec.upsertBatches[0], 1)
	assert.Equal(t, id.String(), rec.upsertBatches[0][0].ID)
	assert.Equal(t, "ANA GOMEZ", rec.upsertBatches[0][0].NombreCliente)
	assert.Equal(t, "ventas", rec.indexUID)
}

func TestIndexarUno_PropagatesUpsertError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("upsert failed")
	rec := &recorder{upsertErr: wantErr}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()})
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// ── Eliminar ──────────────────────────────────────────────────────────────

func TestEliminar_DeletesByID(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	id := uuid.New()
	err := idx.Eliminar(context.Background(), id)
	require.NoError(t, err)
	require.Len(t, rec.deleteCalls, 1)
	assert.Equal(t, []string{id.String()}, rec.deleteCalls[0])
	assert.Equal(t, "ventas", rec.indexUID)
}

func TestEliminar_PropagatesDeleteError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("delete failed")
	rec := &recorder{deleteErr: wantErr}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.Eliminar(context.Background(), uuid.New())
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// ── Buscar ────────────────────────────────────────────────────────────────

func rawHit(t *testing.T, id string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(map[string]string{"id": id})
	require.NoError(t, err)
	return b
}

func TestBuscar_MapsHitsToOrderedIDs(t *testing.T) {
	t.Parallel()
	id1, id2 := uuid.New(), uuid.New()
	rec := &recorder{
		searchResult: platformmeili.SearchResult{
			Hits:               []json.RawMessage{rawHit(t, id1.String()), rawHit(t, id2.String())},
			EstimatedTotalHits: 2,
		},
	}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	result, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.NoError(t, err)
	assert.Equal(t, []uuid.UUID{id1, id2}, result.IDs)
	assert.Equal(t, 2, result.Total)
	assert.Equal(t, "ventas", rec.indexUID)
}

func TestBuscar_ClampsTotalToMaxTotalHitsVentas(t *testing.T) {
	t.Parallel()
	rec := &recorder{
		searchResult: platformmeili.SearchResult{
			Hits:               nil,
			EstimatedTotalHits: outbound.MaxTotalHitsVentas + 1000,
		},
	}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	result, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.NoError(t, err)
	assert.Equal(t, outbound.MaxTotalHitsVentas, result.Total)
}

func TestBuscar_EmptyHits_ReturnsEmptyIDs(t *testing.T) {
	t.Parallel()
	rec := &recorder{
		searchResult: platformmeili.SearchResult{Hits: nil, EstimatedTotalHits: 0},
	}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	result, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.NoError(t, err)
	assert.Empty(t, result.IDs)
	assert.Equal(t, 0, result.Total)
}

func TestBuscar_NotConfigured_ReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	rec := &recorder{searchErr: platformmeili.ErrMeilisearchNotConfigured}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	_, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperror.KindServiceUnavailable, appErr.Kind)
}

func TestBuscar_Transient_ReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	rec := &recorder{searchErr: platformmeili.ErrMeilisearchTransient}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	_, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperror.KindServiceUnavailable, appErr.Kind)
}

func TestBuscar_OtherError_ReturnsInternal(t *testing.T) {
	t.Parallel()
	rec := &recorder{searchErr: errors.New("boom")}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	_, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
}

func TestBuscar_MalformedHit_ReturnsInternal(t *testing.T) {
	t.Parallel()
	rec := &recorder{
		searchResult: platformmeili.SearchResult{
			Hits: []json.RawMessage{json.RawMessage(`{"id": "not-a-uuid"}`)},
		},
	}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	_, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{})
	require.Error(t, err)
	var appErr *apperror.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
}

func TestBuscar_PassesQueryOffsetLimit(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	_, err := idx.Buscar(context.Background(), outbound.VentasSearchQuery{
		Q:      "juan",
		Offset: 20,
		Limit:  10,
	})
	require.NoError(t, err)
	assert.Equal(t, "juan", rec.searchParams.Query)
	assert.Equal(t, int64(20), rec.searchParams.Offset)
	assert.Equal(t, int64(10), rec.searchParams.Limit)
}
