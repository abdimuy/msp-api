package narrativamem_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/narrativamem"
)

func TestRepo_GetNarrativa_Miss(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	row, err := r.GetNarrativa(context.Background(), 99)
	require.NoError(t, err)
	assert.Nil(t, row, "GetNarrativa on empty repo must return nil")
}

func TestRepo_UpsertThenGet_RoundTrip(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	n := domain.Narrativa{
		ClienteID:  1,
		Texto:      "Buen pagador con alto valor.",
		Rasgos:     []string{"steady_reliable", "dormant_valuable"},
		InputHash:  "abc123",
		Modelo:     "test-model",
		GeneradaEn: time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC),
	}

	err := r.UpsertNarrativa(context.Background(), n)
	require.NoError(t, err)

	row, err := r.GetNarrativa(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, row)

	assert.Equal(t, 1, row.ClienteID)
	assert.Equal(t, "Buen pagador con alto valor.", row.Texto)
	assert.Equal(t, []string{"steady_reliable", "dormant_valuable"}, row.Rasgos)
	assert.Equal(t, "abc123", row.InputHash)
	assert.Equal(t, "test-model", row.Modelo)
}

func TestRepo_Upsert_Twice_OneRow(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	first := domain.Narrativa{
		ClienteID: 2,
		Texto:     "Primera versión.",
		InputHash: "hash-v1",
		Modelo:    "m1",
	}
	second := domain.Narrativa{
		ClienteID: 2,
		Texto:     "Segunda versión.",
		InputHash: "hash-v2",
		Modelo:    "m2",
	}

	require.NoError(t, r.UpsertNarrativa(context.Background(), first))
	require.NoError(t, r.UpsertNarrativa(context.Background(), second))

	assert.Equal(t, 1, r.NarrativaCount(), "two upserts for same clienteID must produce one row")

	row, err := r.GetNarrativa(context.Background(), 2)
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.Equal(t, "Segunda versión.", row.Texto, "second upsert must overwrite first")
	assert.Equal(t, "hash-v2", row.InputHash)
}

func TestRepo_Encolar_Idempotent(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	ctx := context.Background()

	require.NoError(t, r.Encolar(ctx, 10, "h1"))
	require.NoError(t, r.Encolar(ctx, 10, "h2")) // second call — same clienteID

	assert.Equal(t, 1, r.PendientesCount(), "two Encolar for same clienteID must produce one pending row")

	rows, err := r.ListarPendientes(ctx, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 10, rows[0].ClienteID)
	assert.Equal(t, "h2", rows[0].InputHash, "second Encolar must overwrite inputHash")
}

func TestRepo_ListarPendientes_Limit(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	ctx := context.Background()

	// Enqueue three clients with IDs 30, 10, 20.
	require.NoError(t, r.Encolar(ctx, 30, "h30"))
	require.NoError(t, r.Encolar(ctx, 10, "h10"))
	require.NoError(t, r.Encolar(ctx, 20, "h20"))

	// With limit=2 we should get the two lowest clienteIDs: 10, 20.
	rows, err := r.ListarPendientes(ctx, 2)
	require.NoError(t, err)
	require.Len(t, rows, 2, "limit=2 must cap results")
	assert.Equal(t, 10, rows[0].ClienteID, "deterministic order: lowest clienteID first")
	assert.Equal(t, 20, rows[1].ClienteID)
}

func TestRepo_ListarPendientes_DeterministicOrder(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	ctx := context.Background()

	// Insert out of order.
	for _, id := range []int{50, 10, 30, 20, 40} {
		require.NoError(t, r.Encolar(ctx, id, "h"))
	}

	rows, err := r.ListarPendientes(ctx, 0) // limit=0 means no cap
	require.NoError(t, err)
	require.Len(t, rows, 5)

	for i := range len(rows) - 1 {
		assert.Less(t, rows[i].ClienteID, rows[i+1].ClienteID,
			"rows must be in ascending clienteID order")
	}
}

func TestRepo_BorrarPendiente_Removes(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	ctx := context.Background()

	require.NoError(t, r.Encolar(ctx, 5, "h5"))
	require.NoError(t, r.Encolar(ctx, 6, "h6"))
	assert.Equal(t, 2, r.PendientesCount())

	require.NoError(t, r.BorrarPendiente(ctx, 5))
	assert.Equal(t, 1, r.PendientesCount(), "BorrarPendiente must remove the row")

	rows, err := r.ListarPendientes(ctx, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, 6, rows[0].ClienteID, "only clienteID=6 must remain")
}

func TestRepo_BorrarPendiente_Noop(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	// BorrarPendiente on a non-existent row must succeed silently.
	err := r.BorrarPendiente(context.Background(), 999)
	assert.NoError(t, err)
}

func TestRepo_GetNarrativa_ReturnsCopy(t *testing.T) {
	t.Parallel()

	r := narrativamem.New()
	ctx := context.Background()

	n := domain.Narrativa{ClienteID: 7, Texto: "Original.", Rasgos: []string{"churn_risk"}, InputHash: "x", Modelo: "m"}
	require.NoError(t, r.UpsertNarrativa(ctx, n))

	row, err := r.GetNarrativa(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, row)

	// Mutate the returned row — must not affect the stored value.
	row.Texto = "Mutated."
	row.Rasgos[0] = "loyal_but_stagnant"

	row2, err := r.GetNarrativa(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, "Original.", row2.Texto, "internal state must not be mutated by caller")
}
