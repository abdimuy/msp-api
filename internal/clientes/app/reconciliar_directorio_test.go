//nolint:misspell // Spanish vocabulary (directorio, pulso, clientes, etc.) per project convention.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/clientes/app"
	"github.com/abdimuy/msp-api/internal/clientes/domain"
	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// newService is a helper for tests in this file that need a Service wired with
// the provided fakes for repo, analytics, and dirIndex (search and clock get
// zero-value stubs since ReconciliarDirectorio does not use them).
func newReconcileService(
	repo outbound.ClientesRepo,
	anl outbound.AnalyticsClient,
	dirIdx outbound.DirectoryIndex,
) *app.Service {
	return app.NewService(repo, anl, dirIdx, fixedClock{T: fixedTime})
}

func TestReconciliarDirectorio_HappyPath(t *testing.T) {
	t.Parallel()

	c1 := newCliente(1, "JUAN PEREZ")
	c2 := newCliente(2, "MARIA LOPEZ")

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c1, SaldoTotal: decimal.NewFromFloat(1000)},
			{Cliente: c2, SaldoTotal: decimal.Zero},
		},
	}

	pulso1 := analytics.ClientePulsoContract{
		ClienteID:    1,
		Score:        80,
		Segmento:     "ACTIVO",
		EstadoPago:   "AL_CORRIENTE",
		RecenciaDias: 15,
		Frecuencia:   4,
		Monetary:     decimal.NewFromFloat(12000),
	}

	anl := &fakeAnalyticsClient{
		pulsosMap: map[int]analytics.ClientePulsoContract{
			1: pulso1,
			// client 2 has no pulse
		},
	}

	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, n, "should return count of docs sent to index")

	// Verify the index received both docs.
	require.Len(t, dirIdx.lastDocs, 2)

	// Verify doc for client 1 (with pulse).
	var doc1 outbound.DirectorioDoc
	for _, d := range dirIdx.lastDocs {
		if d.ClienteID == 1 {
			doc1 = d
		}
	}
	assert.Equal(t, 1, doc1.ClienteID)
	assert.Equal(t, "JUAN PEREZ", doc1.Nombre)
	assert.True(t, doc1.ConSaldo, "saldo 1000 > 0 → ConSaldo true")
	assert.True(t, doc1.TienePulso)
	assert.Equal(t, 80, doc1.Score)
	assert.Equal(t, "ACTIVO", doc1.Segmento)
	assert.Equal(t, "AL_CORRIENTE", doc1.EstadoPago)
	assert.Equal(t, 15, doc1.RecenciaDias)
	assert.Equal(t, 4, doc1.Frecuencia)
	assert.True(t, doc1.Monetary.Equal(decimal.NewFromFloat(12000)))

	// Verify doc for client 2 (no pulse).
	var doc2 outbound.DirectorioDoc
	for _, d := range dirIdx.lastDocs {
		if d.ClienteID == 2 {
			doc2 = d
		}
	}
	assert.Equal(t, 2, doc2.ClienteID)
	assert.Equal(t, "MARIA LOPEZ", doc2.Nombre)
	assert.False(t, doc2.ConSaldo, "saldo 0 → ConSaldo false")
	assert.False(t, doc2.TienePulso)
	assert.Equal(t, 0, doc2.Score, "no pulse → zero Score")
}

func TestReconciliarDirectorio_EmptyRepo(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{dirCompleto: nil}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.Nil(t, dirIdx.lastDocs, "should not call Reconciliar when repo is empty")
}

func TestReconciliarDirectorio_RepoError(t *testing.T) {
	t.Parallel()

	repoErr := errors.New("firebird down")
	repo := &fakeClientesRepo{dirCompletoErr: repoErr}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciliar_directorio_list_failed")
}

func TestReconciliarDirectorio_AnalyticsError(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{
				Cliente:    newCliente(1, "TEST"),
				SaldoTotal: decimal.Zero,
			},
		},
	}
	anl := &fakeAnalyticsClient{pulsosErr: errors.New("analytics timeout")}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciliar_directorio_pulsos_failed")
}

func TestReconciliarDirectorio_IndexError(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: newCliente(1, "TEST"), SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{err: errors.New("meilisearch down")}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconciliar_directorio_index_failed")
}

func TestReconciliarDirectorio_DireccionBuilt(t *testing.T) {
	t.Parallel()

	c := domain.HydrateCliente(domain.HydrateClienteParams{
		ClienteID: 99,
		Nombre:    "DIRECCION TEST",
		Direccion: domain.HydrateDireccion(domain.HydrateDireccionParams{
			Calle:     "AV. HIDALGO",
			Colonia:   "CENTRO",
			Poblacion: "GUADALAJARA",
		}),
	})

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	// Direccion (full-text): space-joined
	assert.Equal(t, "AV. HIDALGO CENTRO GUADALAJARA", doc.Direccion)
	// DireccionCorta: comma-joined (from Direccion.Corta())
	assert.Equal(t, "AV. HIDALGO, CENTRO, GUADALAJARA", doc.DireccionCorta)
	assert.Equal(t, "AV. HIDALGO", doc.DireccionCalle)
	assert.Equal(t, "CENTRO", doc.DireccionColonia)
	assert.Equal(t, "GUADALAJARA", doc.DireccionPoblacion)
}

func TestReconciliarDirectorio_UsesListarDirectorioCompleto(t *testing.T) {
	t.Parallel()

	repo := &fakeClientesRepo{dirCompleto: nil}
	anl := &fakeAnalyticsClient{}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, _ = svc.ReconciliarDirectorio(context.Background())
	assert.True(t, repo.listarComplCalled, "ReconciliarDirectorio must call ListarDirectorioCompleto")
}

// ── B2: cobranza signals in reconcile ────────────────────────────────────────

func TestReconciliarDirectorio_CobranzaSignalsMappedFromPulso(t *testing.T) {
	t.Parallel()

	c1 := newCliente(50, "CLIENTE CON COBRANZA")

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c1, SaldoTotal: decimal.NewFromFloat(5000)},
		},
	}

	fechaProxPago := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	pulso := analytics.ClientePulsoContract{
		ClienteID:       50,
		Score:           65,
		Segmento:        "ACTIVO",
		EstadoPago:      "ATRASADO",
		RecenciaDias:    20,
		Frecuencia:      3,
		Monetary:        decimal.NewFromFloat(8000),
		TierRiesgo:      "EN_RIESGO",
		PctPagosATiempo: decimal.RequireFromString("60.00"),
		FechaProxPago:   fechaProxPago,
	}

	anl := &fakeAnalyticsClient{
		pulsosMap: map[int]analytics.ClientePulsoContract{50: pulso},
	}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Equal(t, "EN_RIESGO", doc.TierRiesgo)
	assert.Equal(t, "60.00", doc.PctPagosATiempo.StringFixed(2))
	assert.Equal(t, fechaProxPago.Unix(), doc.FechaProxPago.Unix())
}

func TestReconciliarDirectorio_CobranzaSignalsZeroWhenNoPulso(t *testing.T) {
	t.Parallel()

	c := newCliente(51, "SIN PULSO COBRANZA")
	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{} // empty pulsosMap → no pulse for cliente 51
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.False(t, doc.TienePulso)
	assert.Empty(t, doc.TierRiesgo, "no pulso → empty tier")
	assert.True(t, doc.PctPagosATiempo.IsZero(), "no pulso → zero pct")
	assert.True(t, doc.FechaProxPago.IsZero(), "no pulso → zero time")
}

// ── R3: credit-risk signals in reconcile ─────────────────────────────────────

func TestReconciliarDirectorio_CreditoSignalsMappedFromPulso(t *testing.T) {
	t.Parallel()

	c := newCliente(60, "CLIENTE CON CREDITO")

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.NewFromFloat(3000)},
		},
	}

	pulso := analytics.ClientePulsoContract{
		ClienteID:    60,
		Score:        70,
		BandaCredito: "MEDIO",
		ScoreCredito: 55,
	}

	anl := &fakeAnalyticsClient{
		pulsosMap: map[int]analytics.ClientePulsoContract{60: pulso},
	}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Equal(t, "MEDIO", doc.BandaCredito)
	assert.Equal(t, 55, doc.ScoreCredito)
}

func TestReconciliarDirectorio_CreditoSignalsEmptyWhenNoPulso(t *testing.T) {
	t.Parallel()

	c := newCliente(61, "CLIENTE CONTADO SIN PULSO")
	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{} // no pulse → no credit signals
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Empty(t, doc.BandaCredito, "no pulso → empty banda_credito")
	assert.Equal(t, 0, doc.ScoreCredito, "no pulso → 0 score_credito")
}

// ── Fase A: repurchase propensity signals in reconcile ────────────────────────

func TestReconciliarDirectorio_RecompraSignalsMappedFromPulso(t *testing.T) {
	t.Parallel()

	c := newCliente(70, "CLIENTE CON RECOMPRA")

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.NewFromFloat(2000)},
		},
	}

	pulso := analytics.ClientePulsoContract{
		ClienteID:     70,
		Score:         65,
		BandaRecompra: "ALTA",
		ScoreRecompra: 82,
	}

	anl := &fakeAnalyticsClient{
		pulsosMap: map[int]analytics.ClientePulsoContract{70: pulso},
	}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Equal(t, "ALTA", doc.BandaRecompra)
	assert.Equal(t, 82, doc.ScoreRecompra)
}

func TestReconciliarDirectorio_RecompraSignalsEmptyWhenNoPulso(t *testing.T) {
	t.Parallel()

	c := newCliente(71, "CLIENTE SIN PULSO RECOMPRA")
	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{} // no pulse → no recompra signals
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Empty(t, doc.BandaRecompra, "no pulso → empty banda_recompra")
	assert.Equal(t, 0, doc.ScoreRecompra, "no pulso → 0 score_recompra")
}

// ── Fase B: CLV signals in reconcile ─────────────────────────────────────────

func TestReconciliarDirectorio_CLVSignalsMappedFromPulso(t *testing.T) {
	t.Parallel()

	c := newCliente(80, "CLIENTE CON CLV")

	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.NewFromFloat(5000)},
		},
	}

	pulso := analytics.ClientePulsoContract{
		ClienteID: 80,
		Score:     70,
		BandaCLV:  "ALTO",
		MontoCLV:  decimal.NewFromFloat(125000.50),
	}

	anl := &fakeAnalyticsClient{
		pulsosMap: map[int]analytics.ClientePulsoContract{80: pulso},
	}
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	n, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Equal(t, "ALTO", doc.BandaCLV)
	assert.Equal(t, "125000.50", doc.CLVStr)
	assert.InDelta(t, 125000.50, doc.CLV, 0.01)
}

func TestReconciliarDirectorio_CLVStr_EmptyWhenBandaCLVEmpty(t *testing.T) {
	t.Parallel()

	c := newCliente(81, "CLIENTE SIN CLV")
	repo := &fakeClientesRepo{
		dirCompleto: []outbound.DirectorioItem{
			{Cliente: c, SaldoTotal: decimal.Zero},
		},
	}
	anl := &fakeAnalyticsClient{} // no pulse → no CLV signals
	dirIdx := &fakeDirectoryIndex{}
	svc := newReconcileService(repo, anl, dirIdx)

	_, err := svc.ReconciliarDirectorio(context.Background())
	require.NoError(t, err)
	require.Len(t, dirIdx.lastDocs, 1)

	doc := dirIdx.lastDocs[0]
	assert.Empty(t, doc.BandaCLV, "no pulso → empty banda_clv")
	assert.Empty(t, doc.CLVStr, "no pulso → empty clv_str (not \"0.00\")")
	assert.InDelta(t, float64(0), doc.CLV, 0.001, "no pulso → 0.0 clv")
}
