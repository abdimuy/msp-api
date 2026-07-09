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
// NewMeilisearchVentaSearchIndexForTest. It records upsert/delete/ensure
// calls and returns a canned search result for assertions.
type recorder struct {
	upsertBatches [][]ventsearchmeili.VentaDocForTest
	deleteCalls   [][]string
	ensureCalls   []platformmeili.IndexConfig
	// calls logs the ORDER of method invocations ("ensure", "upsert",
	// "delete") across the whole recorder, so tests can assert that
	// EnsureIndex runs BEFORE UpsertDocs.
	calls    []string
	indexUID string

	upsertErr error
	deleteErr error
	ensureErr error

	searchResult platformmeili.SearchResult
	searchErr    error
	searchParams platformmeili.SearchParams
}

func (r *recorder) EnsureIndex(_ context.Context, cfg platformmeili.IndexConfig) error {
	r.calls = append(r.calls, "ensure")
	r.ensureCalls = append(r.ensureCalls, cfg)
	if r.ensureErr != nil {
		return r.ensureErr
	}
	return nil
}

func (r *recorder) UpsertDocs(_ context.Context, indexUID string, docs any) error {
	r.calls = append(r.calls, "upsert")
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
	assert.Empty(t, rec.ensureCalls, "empty input should not even ensure the index config")
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

// ── EnsureIndex-before-upsert (settings race fix) ──────────────────────────
//
// See .superpowers/sdd/fix-settings-race-brief.md: a UpsertDocs call that
// auto-creates the index leaves it with default (empty) filterable/sortable
// settings, breaking every filtered/sorted search. Reconciliar must ensure
// the index's full config BEFORE every upsert, unconditionally, every tick.

func TestReconciliar_EnsuresIndexConfigBeforeUpsert(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "my-ventas")

	err := idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{{ID: uuid.New()}})
	require.NoError(t, err)

	require.Len(t, rec.ensureCalls, 1)
	assert.Equal(t, "my-ventas", rec.ensureCalls[0].UID)
	assert.Equal(t, ventsearchmeili.DefaultIndexConfig("my-ventas"), rec.ensureCalls[0])
	assert.Equal(t, []string{"ensure", "upsert"}, rec.calls, "EnsureIndex must run before UpsertDocs")
}

func TestReconciliar_PropagatesEnsureIndexError_DoesNotUpsert(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("meili down")
	rec := &recorder{ensureErr: wantErr}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{{ID: uuid.New()}})
	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, rec.upsertBatches, "must not upsert when the index could not be configured")
}

func TestReconciliar_CallsEnsureIndexOnEveryTick(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	doc := outbound.VentaSearchDoc{ID: uuid.New()}
	require.NoError(t, idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{doc}))
	require.NoError(t, idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{doc}))

	assert.Len(t, rec.ensureCalls, 2,
		"Reconciliar must re-ensure the index config unconditionally on every tick (self-healing)")
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

// ── IndexarUno: ensure-index-once guard (settings race fix) ───────────────

func TestIndexarUno_EnsuresIndexConfigBeforeFirstUpsert(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()})
	require.NoError(t, err)
	require.Len(t, rec.ensureCalls, 1)
	assert.Equal(t, []string{"ensure", "upsert"}, rec.calls, "EnsureIndex must run before the first UpsertDocs")
}

func TestIndexarUno_SkipsEnsureIndexAfterFirstSuccess(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	require.NoError(t, idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()}))
	require.NoError(t, idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()}))
	require.NoError(t, idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()}))

	assert.Len(t, rec.ensureCalls, 1,
		"once configured, subsequent IndexarUno calls must not re-apply settings on every event")
	assert.Len(t, rec.upsertBatches, 3)
}

func TestIndexarUno_PropagatesEnsureIndexError_DoesNotUpsert(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("meili down")
	rec := &recorder{ensureErr: wantErr}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()})
	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, rec.upsertBatches)
}

func TestIndexarUno_RetriesEnsureIndexAfterFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("meili down")
	rec := &recorder{ensureErr: wantErr}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	err := idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()})
	require.Error(t, err)

	// A transient failure must not permanently wedge the guard: the NEXT
	// call retries EnsureIndex instead of assuming the index is configured.
	rec.ensureErr = nil
	err = idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()})
	require.NoError(t, err)

	assert.Len(t, rec.ensureCalls, 2)
	assert.Len(t, rec.upsertBatches, 1)
}

func TestIndexarUno_SkipsEnsureIndexWhenReconciliarAlreadyConfigured(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := ventsearchmeili.NewMeilisearchVentaSearchIndexForTest(rec, "ventas")

	require.NoError(t, idx.Reconciliar(context.Background(), []outbound.VentaSearchDoc{{ID: uuid.New()}}))
	require.Len(t, rec.ensureCalls, 1, "sanity: Reconciliar configured the index")

	require.NoError(t, idx.IndexarUno(context.Background(), outbound.VentaSearchDoc{ID: uuid.New()}))

	assert.Len(t, rec.ensureCalls, 1,
		"IndexarUno must reuse the config already applied by Reconciliar (shared guard)")
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
