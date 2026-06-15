//nolint:misspell // Spanish names and domain vocabulary in test fixtures per project convention.
package search_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/infra/search"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// newIndex creates a fresh BleveIndex for use in tests.
func newIndex(t *testing.T) *search.BleveIndex {
	t.Helper()
	return &search.BleveIndex{}
}

// reconcile is a test helper that reconciles docs and fails the test on error.
func reconcile(t *testing.T, idx *search.BleveIndex, docs []outbound.SearchDoc) {
	t.Helper()
	require.NoError(t, idx.Reconciliar(context.Background(), docs))
}

// buscar is a test helper that searches and fails the test on error.
func buscar(t *testing.T, idx *search.BleveIndex, q string, limit int) []int {
	t.Helper()
	ids, err := idx.Buscar(context.Background(), q, limit)
	require.NoError(t, err)
	return ids
}

// --- State tests -------------------------------------------------------------

func TestEstaListo_FalseBeforeReconciliar(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	assert.False(t, idx.EstaListo(), "index must not be ready before first reconciliation")
}

func TestEstaListo_TrueAfterReconciliar(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{{ClienteID: 1, Texto: "María Hernández"}})
	assert.True(t, idx.EstaListo())
}

func TestEstaListo_TrueAfterEmptyReconciliar(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{})
	assert.True(t, idx.EstaListo(), "an empty reconciliation still marks the index ready")
}

// --- Safety before first Reconciliar -----------------------------------------

func TestBuscar_BeforeReconciliar_ReturnsEmptyNoError(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	ids, err := idx.Buscar(context.Background(), "garcia", 10)
	require.NoError(t, err)
	assert.Empty(t, ids, "Buscar before reconciliation must return empty slice, no error")
}

// --- Accent / case folding ---------------------------------------------------

func TestBuscar_AccentFolding_LowercaseQuery(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 101, Texto: "García López Calle Juárez"},
	})

	// "garcia" should match "García" via asciifolding + to_lower
	ids := buscar(t, idx, "garcia", 10)
	assert.Contains(t, ids, 101, "accent-folded lowercase query must match accented indexed name")
}

func TestBuscar_AccentFolding_UppercaseQuery(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 102, Texto: "García López"},
	})

	ids := buscar(t, idx, "GARCIA", 10)
	assert.Contains(t, ids, 102, "uppercase query must match via case folding")
}

func TestBuscar_AccentFolding_MultiWordQuery(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 103, Texto: "García López"},
		{ClienteID: 104, Texto: "Ramírez Torres"},
	})

	ids := buscar(t, idx, "garcia lopez", 10)
	assert.Contains(t, ids, 103)
	assert.NotContains(t, ids, 104)
}

func TestBuscar_AccentedVowelsInQuery(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 105, Texto: "María de los Ángeles Hernández"},
	})

	// Query without accents should still find the document.
	ids := buscar(t, idx, "angeles hernandez", 10)
	assert.Contains(t, ids, 105)
}

func TestBuscar_FullName_MixedAccents(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 106, Texto: "José Guadalupe Pérez Morales"},
	})

	ids := buscar(t, idx, "jose guadalupe perez", 10)
	assert.Contains(t, ids, 106)
}

// --- Multi-term matching across name and address ----------------------------

func TestBuscar_MultiTermNameAndColonia(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		// Texto holds name + address concatenated by the app layer.
		{ClienteID: 201, Texto: "García López Colonia Centro Guadalajara"},
		{ClienteID: 202, Texto: "Ramírez Torres Colonia Tlaquepaque Guadalajara"},
	})

	// Searching by partial name + colonia should return only client 201.
	ids := buscar(t, idx, "garcia centro", 10)
	assert.Contains(t, ids, 201)
	// Client 202 may or may not appear (OR operator) — but 201 should rank first.
	if len(ids) > 0 {
		assert.Equal(t, 201, ids[0], "most relevant document must rank first")
	}
}

// --- Limit capping -----------------------------------------------------------

func TestBuscar_LimitCapsResults(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	docs := make([]outbound.SearchDoc, 10)
	for i := range docs {
		docs[i] = outbound.SearchDoc{ClienteID: 300 + i, Texto: "Hernández Morales"}
	}
	reconcile(t, idx, docs)

	ids := buscar(t, idx, "hernandez", 3)
	assert.LessOrEqual(t, len(ids), 3, "Buscar must respect the limit parameter")
}

// --- Ranking -----------------------------------------------------------------

func TestBuscar_MoreRelevantDocRankedFirst(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		// Cliente 401 has the query term appearing twice — higher term frequency.
		{ClienteID: 401, Texto: "Pérez Bernardo Pérez Colonia Centro"}, //nolint:dupword
		// Cliente 402 has it once.
		{ClienteID: 402, Texto: "Pérez Colonia Norte"},
	})

	ids := buscar(t, idx, "perez", 10)
	require.GreaterOrEqual(t, len(ids), 2)
	assert.Equal(t, 401, ids[0], "document with higher term frequency must rank first")
}

// --- Atomic swap (Reconciliar rebuilds the index completely) ----------------

func TestReconciliar_AtomicSwap_OldDocsGone(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)

	// First build: clienteID 501.
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 501, Texto: "Flores Martínez Colonia Zapopan"},
	})
	ids := buscar(t, idx, "flores", 10)
	assert.Contains(t, ids, 501)

	// Second build: only clienteID 502 — 501 must be gone.
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 502, Texto: "Gutiérrez Ruiz Colonia Polanco"},
	})

	idsAfter := buscar(t, idx, "flores", 10)
	assert.NotContains(t, idsAfter, 501, "after atomic swap, old documents must not appear")

	idsNew := buscar(t, idx, "gutierrez", 10)
	assert.Contains(t, idsNew, 502, "new documents must be findable after swap")
}

func TestReconciliar_AtomicSwap_EmptyReplacesAll(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 601, Texto: "Luna Castillo Tonalá"},
	})

	// Swap with empty docs.
	reconcile(t, idx, []outbound.SearchDoc{})

	ids := buscar(t, idx, "luna", 10)
	assert.Empty(t, ids, "empty reconciliation must remove all previously indexed documents")
	assert.True(t, idx.EstaListo(), "index must still be ready after empty reconciliation")
}

// --- Edge cases --------------------------------------------------------------

func TestBuscar_EmptyQuery_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 701, Texto: "Morales Vega"},
	})
	ids := buscar(t, idx, "", 10)
	assert.Empty(t, ids)
}

func TestBuscar_ZeroLimit_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 702, Texto: "Morales Vega"},
	})
	ids := buscar(t, idx, "morales", 0)
	assert.Empty(t, ids)
}

func TestBuscar_CancelledContext(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 703, Texto: "Mendoza Ríos"},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Cancelled context must return an error (context.Canceled), not panic.
	ids, err := idx.Buscar(ctx, "mendoza", 10)
	require.Error(t, err, "cancelled context must return an error")
	assert.Empty(t, ids)
}

func TestBuscar_TypoTolerance(t *testing.T) {
	t.Parallel()
	idx := newIndex(t)
	reconcile(t, idx, []outbound.SearchDoc{
		{ClienteID: 801, Texto: "Castañeda Villanueva"},
	})

	// "castaneda" (missing tilde) should still match via asciifolding.
	ids := buscar(t, idx, "castaneda", 10)
	assert.Contains(t, ids, 801)
}
