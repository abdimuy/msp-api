package clientessearch_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clientessearchmeili "github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// ── ordinal helpers ───────────────────────────────────────────────────────────

func TestSegmentoOrdinal_KnownValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		seg  string
		want int
	}{
		{"LEAL_POR_LIQUIDAR", 0},
		{"DORMIDO_VALIOSO", 1},
		{"ACTIVO", 2},
		{"NUEVO", 3},
		{"FRIO", 4},
		{"PERDIDO", 5},
		{"", 6},
		{"UNKNOWN", 6},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.seg, func(t *testing.T) {
			t.Parallel()
			got := clientessearchmeili.SegmentoOrdinalForTest(tc.seg)
			assert.Equal(t, tc.want, got, "segmento %q should map to ordinal %d", tc.seg, tc.want)
		})
	}
}

func TestEstadoPagoOrdinal_KnownValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ep   string
		want int
	}{
		{"AL_CORRIENTE", 0},
		{"LIQUIDADO", 1},
		{"SIN_CREDITO", 2},
		{"ATRASADO", 3},
		{"MOROSO", 4},
		{"", 5},
		{"UNKNOWN", 5},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.ep, func(t *testing.T) {
			t.Parallel()
			got := clientessearchmeili.EstadoPagoOrdinalForTest(tc.ep)
			assert.Equal(t, tc.want, got, "estado_pago %q should map to ordinal %d", tc.ep, tc.want)
		})
	}
}

// ── Reconciliar tests ─────────────────────────────────────────────────────────

// recorder satisfies the narrower upsertOnlyClient interface exposed via
// NewMeilisearchDirectoryIndexForTest. It records batches for assertions.
type recorder struct {
	batches  [][]clientessearchmeili.ClienteDocForTest
	indexUID string
	err      error
}

func (r *recorder) UpsertDocs(_ context.Context, indexUID string, docs any) error {
	if r.err != nil {
		return r.err
	}
	r.indexUID = indexUID
	r.batches = append(r.batches, clientessearchmeili.ExtractBatch(docs))
	return nil
}

func TestMeilisearchDirectoryIndex_Reconciliar_MapsFieldsCorrectly(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := clientessearchmeili.NewMeilisearchDirectoryIndexForTest(rec, "clientes")

	doc := outbound.DirectorioDoc{
		ClienteID:          42,
		Nombre:             "JUAN PEREZ",
		ZonaID:             5,
		CobradorID:         11,
		Estatus:            "A",
		Telefono:           "3311112222",
		Direccion:          "INSURGENTES CENTRO GUADALAJARA",
		DireccionCalle:     "INSURGENTES",
		DireccionColonia:   "CENTRO",
		DireccionPoblacion: "GUADALAJARA",
		DireccionCorta:     "INSURGENTES, CENTRO, GUADALAJARA",
		Saldo:              decimal.NewFromFloat(1500.50),
		ConSaldo:           true,
		Score:              72,
		Segmento:           "ACTIVO",
		EstadoPago:         "AL_CORRIENTE",
		RecenciaDias:       30,
		Frecuencia:         5,
		Monetary:           decimal.NewFromFloat(20000.00),
		NextBestProduct:    "SALA",
		TienePulso:         true,
	}

	err := idx.Reconciliar(context.Background(), []outbound.DirectorioDoc{doc})
	require.NoError(t, err)
	require.Len(t, rec.batches, 1)
	require.Len(t, rec.batches[0], 1)

	got := rec.batches[0][0]
	assert.Equal(t, "42", got.ID)
	assert.Equal(t, 42, got.ClienteID)
	assert.Equal(t, "JUAN PEREZ", got.Nombre)
	assert.Equal(t, 5, got.ZonaID)
	assert.Equal(t, 11, got.CobradorID)
	assert.True(t, got.ConSaldo)
	assert.Equal(t, "ACTIVO", got.Segmento)
	assert.Equal(t, 2, got.SegmentoOrden, "ACTIVO should map to ordinal 2")
	assert.Equal(t, "AL_CORRIENTE", got.EstadoPago)
	assert.Equal(t, 0, got.EstadoPagoOrden, "AL_CORRIENTE should map to ordinal 0")
	assert.InEpsilon(t, 1500.50, got.Saldo, 0.001) // numeric sort key
	assert.Equal(t, "1500.50", got.SaldoStr)       // exact display value
	assert.Equal(t, "20000.00", got.Monetary)      // exact display value
	assert.Equal(t, 72, got.Score)
	assert.Equal(t, 30, got.RecenciaDias)
	assert.Equal(t, 5, got.Frecuencia)
	assert.True(t, got.TienePulso)
	assert.Equal(t, "SALA", got.NextBestProduct)
	assert.Equal(t, "INSURGENTES, CENTRO, GUADALAJARA", got.DireccionCorta)
	assert.Equal(t, "INSURGENTES CENTRO GUADALAJARA", got.Direccion)
	assert.Equal(t, "A", got.Estatus)
	assert.Equal(t, "3311112222", got.Telefono)
}

func TestMeilisearchDirectoryIndex_Reconciliar_NoPulse(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := clientessearchmeili.NewMeilisearchDirectoryIndexForTest(rec, "clientes")

	doc := outbound.DirectorioDoc{
		ClienteID:  7,
		Nombre:     "SIN PULSO",
		Estatus:    "B",
		TienePulso: false,
		// all pulse fields left at zero values
	}

	err := idx.Reconciliar(context.Background(), []outbound.DirectorioDoc{doc})
	require.NoError(t, err)
	require.Len(t, rec.batches, 1)
	got := rec.batches[0][0]
	assert.Equal(t, "7", got.ID)
	assert.False(t, got.TienePulso)
	assert.False(t, got.ConSaldo)
	// Unknown/empty segmento and estado_pago → sort-last ordinals.
	assert.Equal(t, 6, got.SegmentoOrden, "empty segmento should map to sort-last ordinal 6")
	assert.Equal(t, 5, got.EstadoPagoOrden, "empty estado_pago should map to sort-last ordinal 5")
	// Zero pulse fields.
	assert.Equal(t, 0, got.Score)
	assert.Equal(t, "0.00", got.Monetary)
}

func TestMeilisearchDirectoryIndex_MoneyRoundTripExact(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := clientessearchmeili.NewMeilisearchDirectoryIndexForTest(rec, "clientes")

	// Values chosen so the float64 binary representation is NOT exact — proving
	// the string round-trip (not the numeric saldo) is what preserves precision.
	in := outbound.DirectorioDoc{
		ClienteID: 1,
		Saldo:     decimal.RequireFromString("12345.67"),
		Monetary:  decimal.RequireFromString("999999.99"),
	}
	require.NoError(t, idx.Reconciliar(context.Background(), []outbound.DirectorioDoc{in}))

	stored := rec.batches[0][0]
	assert.Equal(t, "12345.67", stored.SaldoStr)
	assert.Equal(t, "999999.99", stored.Monetary)

	out := clientessearchmeili.ClienteDocToDirectorioDocForTest(stored)
	assert.Equal(t, "12345.67", out.Saldo.StringFixed(2))
	assert.Equal(t, "999999.99", out.Monetary.StringFixed(2))
}

func TestMeilisearchDirectoryIndex_Reconciliar_EmptyInput(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := clientessearchmeili.NewMeilisearchDirectoryIndexForTest(rec, "clientes")

	err := idx.Reconciliar(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, rec.batches, "no UpsertDocs call expected for empty/nil input")
}

func TestMeilisearchDirectoryIndex_Reconciliar_SendsToCorrectIndex(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	idx := clientessearchmeili.NewMeilisearchDirectoryIndexForTest(rec, "my-clientes")

	_ = idx.Reconciliar(context.Background(), []outbound.DirectorioDoc{
		{ClienteID: 1, Nombre: "TEST"},
	})
	assert.Equal(t, "my-clientes", rec.indexUID)
}
