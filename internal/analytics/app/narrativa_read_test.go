// Package app_test — narrativa_read_test.go exercises aplicarNarrativa and the
// end-to-end read-path through ObtenerPulsoCliente.
//
//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics"
	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/narrativamem"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
)

// testPulsoComp builds a minimal PulsoComputado with distinct field values so
// NarrativaInputHash returns a deterministic, non-trivial hash.
func testPulsoComp() analytics.PulsoComputado {
	return analytics.PulsoComputado{
		BandaCredito:    "MEDIO",
		BandaRecompra:   "ALTO",
		BandaCLV:        "BAJO",
		CreditoResumen:  "pagador puntual",
		RecompraResumen: "alta propensión",
		CLVResumen:      "CLV $10,000",
	}
}

// testNarrativaClienteID is the canonical clienteID used in narrativa tests.
const testNarrativaClienteID = 777

// TestAplicarNarrativa_NoRepo verifies that a Service without WithNarrativa
// leaves comp.Narrativa and comp.RasgosIA empty without panicking.
func TestAplicarNarrativa_NoRepo(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)
	// No WithNarrativa call — narrativaRepo is nil.

	comp := testPulsoComp()
	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Empty(t, comp.Narrativa, "narrativa must be empty when no repo")
	assert.Nil(t, comp.RasgosIA, "RasgosIA must be nil when no repo")
}

// TestAplicarNarrativa_FreshHit verifies that a matching cached row populates
// comp.Narrativa and comp.RasgosIA with resolved Spanish labels.
func TestAplicarNarrativa_FreshHit(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	hash := app.NarrativaInputHash(comp, "")

	nRepo := narrativamem.New()
	require.NoError(t, nRepo.UpsertNarrativa(context.Background(), domain.Narrativa{
		ClienteID: testNarrativaClienteID,
		Texto:     "lectura del analista",
		Rasgos:    []string{"loyal_but_stagnant", "churn_risk"},
		InputHash: hash,
		Modelo:    "test-model",
	}))

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, false)

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Equal(t, "lectura del analista", comp.Narrativa)
	assert.Equal(t, []string{"Leal pero estancado", "Riesgo de fuga"}, comp.RasgosIA)
}

// TestAplicarNarrativa_UnknownCodeDropped verifies that unknown trait codes are
// silently dropped while known codes are resolved.
func TestAplicarNarrativa_UnknownCodeDropped(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	hash := app.NarrativaInputHash(comp, "")

	nRepo := narrativamem.New()
	require.NoError(t, nRepo.UpsertNarrativa(context.Background(), domain.Narrativa{
		ClienteID: testNarrativaClienteID,
		Texto:     "texto con código desconocido",
		Rasgos:    []string{"loyal_but_stagnant", "gone_from_catalog"},
		InputHash: hash,
		Modelo:    "test-model",
	}))

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, false)

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Equal(t, []string{"Leal pero estancado"}, comp.RasgosIA,
		"unknown code must be dropped; known code must be resolved")
}

// TestAplicarNarrativa_NegativeCacheHit verifies that a matching row with
// empty Texto and no Rasgos leaves comp empty and does NOT enqueue.
func TestAplicarNarrativa_NegativeCacheHit(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	hash := app.NarrativaInputHash(comp, "")

	nRepo := narrativamem.New()
	// Negative-cache row: hash matches but Texto is empty and Rasgos is empty.
	require.NoError(t, nRepo.UpsertNarrativa(context.Background(), domain.Narrativa{
		ClienteID: testNarrativaClienteID,
		Texto:     "",
		Rasgos:    []string{},
		InputHash: hash,
		Modelo:    "test-model",
	}))

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, true) // enabled=true to confirm no enqueue happens on negative hit

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Empty(t, comp.Narrativa, "comp must be empty on negative-cache hit")
	assert.Nil(t, comp.RasgosIA, "RasgosIA must be nil on negative-cache hit")
	assert.Equal(t, 0, nRepo.PendientesCount(), "negative-cache hit must NOT enqueue")
}

// TestAplicarNarrativa_StaleEnabled verifies that a stale row (different hash)
// leaves comp empty and enqueues the client with the CURRENT hash when LLM is
// enabled.
func TestAplicarNarrativa_StaleEnabled(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	currentHash := app.NarrativaInputHash(comp, "")

	nRepo := narrativamem.New()
	// Seed a stale row (different hash).
	require.NoError(t, nRepo.UpsertNarrativa(context.Background(), domain.Narrativa{
		ClienteID: testNarrativaClienteID,
		Texto:     "lectura antigua",
		Rasgos:    []string{"steady_reliable"},
		InputHash: "stale_hash_000000000000000000000000000000000000000000000000000000",
		Modelo:    "test-model",
	}))

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, true)

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Empty(t, comp.Narrativa, "comp must be empty on stale hit")
	assert.Nil(t, comp.RasgosIA, "RasgosIA must be nil on stale hit")

	pendientes, err := nRepo.ListarPendientes(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, pendientes, 1, "stale+enabled must enqueue exactly one client")
	assert.Equal(t, testNarrativaClienteID, pendientes[0].ClienteID)
	assert.Equal(t, currentHash, pendientes[0].InputHash, "queued hash must be the CURRENT hash")
}

// TestAplicarNarrativa_MissEnabled verifies that an empty repo with LLM
// enabled enqueues the client with the current hash.
func TestAplicarNarrativa_MissEnabled(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	currentHash := app.NarrativaInputHash(comp, "")

	nRepo := narrativamem.New() // empty repo — miss

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, true)

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Empty(t, comp.Narrativa, "comp must be empty on miss")
	assert.Nil(t, comp.RasgosIA, "RasgosIA must be nil on miss")

	pendientes, err := nRepo.ListarPendientes(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, pendientes, 1, "miss+enabled must enqueue exactly one client")
	assert.Equal(t, testNarrativaClienteID, pendientes[0].ClienteID)
	assert.Equal(t, currentHash, pendientes[0].InputHash)
}

// TestAplicarNarrativa_MissDisabled verifies that an empty repo with LLM
// disabled does NOT enqueue and comp stays empty.
func TestAplicarNarrativa_MissDisabled(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	nRepo := narrativamem.New() // empty repo — miss

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, false) // disabled

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Empty(t, comp.Narrativa, "comp must be empty on miss+disabled")
	assert.Nil(t, comp.RasgosIA, "RasgosIA must be nil on miss+disabled")
	assert.Equal(t, 0, nRepo.PendientesCount(), "miss+disabled must NOT enqueue")
}

// TestObtenerPulsoCliente_NarrativaEndToEnd is an end-to-end test that drives
// ObtenerPulsoCliente through a miss (first call, enqueue) then a fresh hit
// (second call, serve) scenario.
func TestObtenerPulsoCliente_NarrativaEndToEnd(t *testing.T) {
	t.Parallel()

	c := makeNarrativaCandidato(testNarrativaClienteID)

	wbRepo := newFakeWinbackRepo()
	wbRepo.candidates = []*domain.WinbackCandidato{c}

	nRepo := narrativamem.New()

	svc := app.NewService(wbRepo, nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, true)

	// First call: repo is empty → miss → should enqueue.
	pulse1, err := svc.ObtenerPulsoCliente(context.Background(), testNarrativaClienteID)
	require.NoError(t, err)

	assert.Empty(t, pulse1.Narrativa, "first call: narrativa must be empty on miss")
	assert.Nil(t, pulse1.RasgosIA, "first call: RasgosIA must be nil on miss")

	pendientes, err := nRepo.ListarPendientes(context.Background(), 0)
	require.NoError(t, err)
	require.Len(t, pendientes, 1, "first call: client must be enqueued")

	queuedHash := pendientes[0].InputHash
	assert.NotEmpty(t, queuedHash, "queued hash must be non-empty")

	// Seed a narrativa row with the hash that was enqueued.
	require.NoError(t, nRepo.UpsertNarrativa(context.Background(), domain.Narrativa{
		ClienteID: testNarrativaClienteID,
		Texto:     "lectura X",
		Rasgos:    []string{"cash_reliable"},
		InputHash: queuedHash,
		Modelo:    "test-model",
	}))

	// Second call: fresh hit → serve cached narrativa.
	pulse2, err := svc.ObtenerPulsoCliente(context.Background(), testNarrativaClienteID)
	require.NoError(t, err)

	assert.Equal(t, "lectura X", pulse2.Narrativa, "second call: narrativa must be served from cache")
	assert.Equal(t, []string{"Contado confiable"}, pulse2.RasgosIA, "second call: RasgosIA must resolve cash_reliable")
}

// TestObtenerPulsosClientes_NeverEnqueues asserts that the LIST path never
// serves or enqueues narrativa, even when WithNarrativa is configured.
func TestObtenerPulsosClientes_NeverEnqueues(t *testing.T) {
	t.Parallel()

	c := makeNarrativaCandidato(testNarrativaClienteID)

	wbRepo := newFakeWinbackRepo()
	wbRepo.candidates = []*domain.WinbackCandidato{c}

	nRepo := narrativamem.New()

	svc := app.NewService(wbRepo, nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, true)

	result, err := svc.ObtenerPulsosClientes(context.Background(), []int{testNarrativaClienteID})
	require.NoError(t, err)

	require.Contains(t, result, testNarrativaClienteID, "client must appear in LIST result")
	pulse := result[testNarrativaClienteID]

	assert.Empty(t, pulse.Narrativa, "LIST: narrativa must be empty (never served)")
	assert.Nil(t, pulse.RasgosIA, "LIST: RasgosIA must be nil (never resolved)")
	assert.Equal(t, 0, nRepo.PendientesCount(), "LIST: must NOT enqueue")
}

// makeNarrativaCandidato builds a WinbackCandidato suitable for narrativa
// end-to-end tests (scoring aplica, all required fields set).
func makeNarrativaCandidato(clienteID int) *domain.WinbackCandidato {
	return mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID:         clienteID,
		Nombre:            "Cliente Narrativa",
		Zona:              "Z1",
		Telefono:          "555-0777",
		FechaUltimaCompra: testNow.AddDate(0, -6, 0),
		Frecuencia:        4,
		Monetary:          decimal.NewFromInt(20_000),
		Saldo:             decimal.Zero,
		PorLiquidarPct:    decimal.Zero,
		CohorteFecha:      testNow.AddDate(-2, 0, 0),
		Now:               testNow,
	})
}

// TestAplicarNarrativa_ContextoOperativoServed verifies that a cached narrativa
// row with ContextoOperativo="contexto X" populates comp.ContextoOperativo.
func TestAplicarNarrativa_ContextoOperativoServed(t *testing.T) {
	t.Parallel()

	comp := testPulsoComp()
	hash := app.NarrativaInputHash(comp, "")

	nRepo := narrativamem.New()
	require.NoError(t, nRepo.UpsertNarrativa(context.Background(), domain.Narrativa{
		ClienteID:         testNarrativaClienteID,
		Texto:             "lectura del analista",
		Rasgos:            []string{"loyal_but_stagnant"},
		InputHash:         hash,
		Modelo:            "test-model",
		ContextoOperativo: "contexto X",
	}))

	svc := app.NewService(newFakeWinbackRepo(), nil, fixedClock{testNow}, nil).
		WithNarrativa(nRepo, false)

	app.ExportAplicarNarrativa(context.Background(), svc, testNarrativaClienteID, &comp)

	assert.Equal(t, "contexto X", comp.ContextoOperativo,
		"ContextoOperativo from the cached row must be served into comp")
}

// Compile-time assertion: ExportAplicarNarrativa and ExportNarrativaInputHash
// are accessed via the exported wrappers in export_test.go.
var _ outbound.NarrativaRepo = (*narrativamem.Repo)(nil)
