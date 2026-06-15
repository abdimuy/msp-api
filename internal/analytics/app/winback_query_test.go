//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestListarWinback_Ordering(t *testing.T) {
	t.Parallel()

	// Build candidates with known scores:
	//  - c1: recencia=30 (ACTIVO), monetary=50000, phone, saldo=0 → SIN_CREDITO (mult=0.85)
	//    recenciaComp=0.15 (≤90), value=1.0, contact=1
	//    base=0.45*0.15 + 0.30*1.0 + 0.10*1.0 = 0.0675+0.30+0.10 = 0.4675
	//    score=round(100*0.4675*0.85)=round(39.7375)=40
	//
	//  - c2: recencia=30 (ACTIVO), monetary=5000, no phone, saldo=0 → SIN_CREDITO (mult=0.85)
	//    recenciaComp=0.15, value=0.1
	//    base=0.45*0.15 + 0.30*0.1 = 0.0675+0.03 = 0.0975
	//    score=round(100*0.0975*0.85)=round(8.2875)=8
	//
	//  - c3: recencia=400 (LEAL_POR_LIQUIDAR), monetary=25000, phone, porLiq=50%, saldo=0 → SIN_CREDITO (mult=0.85)
	//    recenciaComp=1.0 (180≤400≤540), value=0.5, contact=1, porLiq=0.5
	//    base=0.45*1.0 + 0.30*0.5 + 0.10*1.0 + 0.15*0.5 = 0.45+0.15+0.10+0.075 = 0.775
	//    score=round(100*0.775*0.85)=round(65.875)=66
	//
	// Expected order: c3(66) > c1(40) > c2(8)
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, Nombre: "Alpha", Zona: "Z1", Telefono: "555-0001",
		FechaUltimaCompra: testNow.AddDate(0, 0, -30),
		Frecuencia:        5, Monetary: decimal.NewFromInt(50_000),
		PorLiquidarPct: decimal.Zero, CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, Nombre: "Beta", Zona: "Z1", Telefono: "",
		FechaUltimaCompra: testNow.AddDate(0, 0, -30),
		Frecuencia:        5, Monetary: decimal.NewFromInt(5_000),
		PorLiquidarPct: decimal.Zero, CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	c3 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 3, Nombre: "Gamma", Zona: "Z1", Telefono: "555-0003",
		FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia:        5, Monetary: decimal.NewFromInt(25_000),
		PorLiquidarPct: decimal.NewFromFloat(50.0), CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c3, c2, c1} // intentionally out of order

	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: true,
		IncluirActivos: true, // include ACTIVO/NUEVO so c1 and c2 appear
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Expected order (Score desc, Monetary desc on tie, ClienteID asc on tie):
	//   [0] c3 clienteID=3 score=66 (LEAL_POR_LIQUIDAR)
	//   [1] c1 clienteID=1 score=40 (ACTIVO)
	//   [2] c2 clienteID=2 score=8  (ACTIVO)
	wantOrder := []struct {
		clienteID int
		score     int
	}{
		{clienteID: 3, score: 66},
		{clienteID: 1, score: 40},
		{clienteID: 2, score: 8},
	}
	for i, want := range wantOrder {
		if items[i].Candidato.ClienteID() != want.clienteID {
			t.Errorf("items[%d]: got clienteID=%d score=%d, want clienteID=%d score=%d",
				i, items[i].Candidato.ClienteID(), items[i].Score.Int(), want.clienteID, want.score)
		}
		if items[i].Score.Int() != want.score {
			t.Errorf("items[%d]: got score=%d, want score=%d (clienteID=%d)",
				i, items[i].Score.Int(), want.score, items[i].Candidato.ClienteID())
		}
	}

	// All items must have a populated EstadoPago.
	for i, item := range items {
		assert.NotEmpty(t, item.EstadoPago.String(),
			"items[%d]: EstadoPago must be populated", i)
		assert.True(t, item.EstadoPago.IsValid(),
			"items[%d]: EstadoPago must be a valid value, got %q", i, item.EstadoPago)
	}
}

func TestListarWinback_SegmentoFilter(t *testing.T) {
	t.Parallel()

	// c1: ACTIVO (recent, frecuencia>1)
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, Zona: "Z1", FechaUltimaCompra: testNow.AddDate(0, 0, -10),
		Frecuencia: 3, Monetary: decimal.NewFromInt(10_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	// c2: PERDIDO (recencia > 730)
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, Zona: "Z1", FechaUltimaCompra: testNow.AddDate(0, 0, -800),
		Frecuencia: 2, Monetary: decimal.NewFromInt(1_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c1, c2}

	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	// Explicit segmento filter overrides the default exclusion of ACTIVO.
	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		Segmento:       "ACTIVO",
		IncluirControl: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item after segmento filter, got %d", len(items))
	}
	if items[0].Candidato.ClienteID() != 1 {
		t.Errorf("expected clienteID=1 (ACTIVO), got %d", items[0].Candidato.ClienteID())
	}
	if items[0].Segmento != domain.SegmentoActivo {
		t.Errorf("expected ACTIVO segmento, got %q", items[0].Segmento)
	}
}

func TestListarWinback_InvalidSegmento(t *testing.T) {
	t.Parallel()
	repo := newFakeWinbackRepo()
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	_, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		Segmento: "NO_EXISTE",
	})
	if err == nil {
		t.Fatal("expected error for invalid segmento, got nil")
	}
}

func TestListarWinback_ZonaPassthrough(t *testing.T) {
	t.Parallel()

	// Use lapsed candidates so they are not excluded by the default IncluirActivos=false.
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, Zona: "NORTE", FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 3, Monetary: decimal.NewFromInt(5_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, Zona: "SUR", FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 3, Monetary: decimal.NewFromInt(5_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c1, c2}

	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		Zona: "NORTE", IncluirControl: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item for zona=NORTE, got %d", len(items))
	}
	if items[0].Candidato.Zona() != "NORTE" {
		t.Errorf("expected zona=NORTE, got %q", items[0].Candidato.Zona())
	}
}

func TestListarWinback_IncluirControlMapping(t *testing.T) {
	t.Parallel()

	// c1: treatment group, lapsed so it is not excluded by default
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 3, Monetary: decimal.NewFromInt(5_000),
		EnControl: false, CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	// c2: control group, also lapsed
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 3, Monetary: decimal.NewFromInt(5_000),
		EnControl: true, CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c1, c2}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	// IncluirControl=false → should exclude c2
	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (treatment only), got %d", len(items))
	}
	if items[0].Candidato.ClienteID() != 1 {
		t.Errorf("expected clienteID=1 (treatment), got %d", items[0].Candidato.ClienteID())
	}

	// IncluirControl=true → both
	items, err = svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items (treatment+control), got %d", len(items))
	}
}

func TestListarWinback_LimitAfterSorting(t *testing.T) {
	t.Parallel()

	// Build 5 candidates with distinct scores (different monetary, same recency=50d).
	// recenciaDias=50 → ACTIVO segment → excluded by default.
	// Use IncluirActivos=true to include them.
	// All have saldo=0 (not set) → SIN_CREDITO (mult=0.85).
	// recenciaComp=0.15 (≤90), contact=1 (phone set), porLiq=0.
	// value varies by monetary: i+1 * 10000 / 50000.
	// base = 0.45*0.15 + 0.30*value + 0.10*1.0 = 0.0675 + 0.30*value + 0.10
	// score = round(100 * base * 0.85)
	// clienteID=5: monetary=50000, value=1.0 → base=0.4675 → score=round(39.7375)=40
	// clienteID=4: monetary=40000, value=0.8 → base=0.4075 → score=round(34.6375)=35
	candidates := make([]*domain.WinbackCandidato, 5)
	for i := range candidates {
		candidates[i] = mustCandidato(domain.CrearWinbackCandidatoParams{
			ClienteID: i + 1, Telefono: "555-0001",
			FechaUltimaCompra: testNow.AddDate(0, 0, -50),
			Frecuencia:        5,
			Monetary:          decimal.NewFromInt(int64((i + 1) * 10_000)),
			CohorteFecha:      testNow.AddDate(-1, 0, 0), Now: testNow,
		})
	}

	repo := newFakeWinbackRepo()
	repo.candidates = candidates
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		Limit: 2, IncluirControl: true, IncluirActivos: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items after limit, got %d", len(items))
	}
	// The top 2 should be the highest-scoring (highest monetary = clienteID 5 and 4).
	if items[0].Candidato.ClienteID() != 5 {
		t.Errorf("first item should be clienteID=5 (highest monetary/score), got %d", items[0].Candidato.ClienteID())
	}
	if items[1].Candidato.ClienteID() != 4 {
		t.Errorf("second item should be clienteID=4, got %d", items[1].Candidato.ClienteID())
	}
}

// TestListarWinback_ExcludesActivosByDefault verifies that ACTIVO and NUEVO
// segments are omitted from results when IncluirActivos=false (the default).
func TestListarWinback_ExcludesActivosByDefault(t *testing.T) {
	t.Parallel()

	// cActivo: recencia=10d → ACTIVO (frecuencia>1)
	cActivo := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, Zona: "Z1", FechaUltimaCompra: testNow.AddDate(0, 0, -10),
		Frecuencia: 3, Monetary: decimal.NewFromInt(20_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	// cNuevo: recencia=10d, frecuencia=1 → NUEVO
	cNuevo := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, Zona: "Z1", FechaUltimaCompra: testNow.AddDate(0, 0, -10),
		Frecuencia: 1, Monetary: decimal.NewFromInt(10_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	// cFrio: recencia=400d, low monetary → FRIO (should be included)
	cFrio := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 3, Zona: "Z1", FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 2, Monetary: decimal.NewFromInt(5_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{cActivo, cNuevo, cFrio}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	// Default: IncluirActivos=false → only FRIO should appear.
	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: true,
		// IncluirActivos: false (default)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item (FRIO only), got %d", len(items))
	}
	if items[0].Candidato.ClienteID() != 3 {
		t.Errorf("expected clienteID=3 (FRIO), got %d", items[0].Candidato.ClienteID())
	}
	if items[0].Segmento != domain.SegmentoFrio {
		t.Errorf("expected segment FRIO, got %q", items[0].Segmento)
	}

	// With IncluirActivos=true → all 3 appear.
	allItems, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: true,
		IncluirActivos: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(allItems) != 3 {
		t.Fatalf("expected 3 items with IncluirActivos=true, got %d", len(allItems))
	}
}

func TestListarWinback_EstadoPago_Populated(t *testing.T) {
	t.Parallel()

	// cSinCredito: saldo=0, FechaUltimoPago zero → SIN_CREDITO.
	// recencia=400 (lapsed) so it is not excluded by the default IncluirActivos=false.
	cSinCredito := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 10, FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 2, Monetary: decimal.NewFromInt(10_000),
		Saldo:        decimal.Zero,
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
		// FechaUltimoPago zero + saldo == 0 → SIN_CREDITO
	})
	// cMoroso: saldo>0, FechaUltimoPago zero → MOROSO
	cMoroso := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 11, FechaUltimaCompra: testNow.AddDate(0, 0, -400),
		Frecuencia: 4, Monetary: decimal.NewFromInt(30_000),
		Saldo:        decimal.NewFromInt(8_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
		// FechaUltimoPago zero + saldo > 0 → MOROSO
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{cSinCredito, cMoroso}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	items, err := svc.ListarWinback(context.Background(), app.ListarWinbackParams{
		IncluirControl: true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	byID := make(map[int]app.WinbackListItem)
	for _, it := range items {
		byID[it.Candidato.ClienteID()] = it
	}

	if byID[10].EstadoPago != domain.EstadoPagoSinCredito {
		t.Errorf("clienteID=10: expected SIN_CREDITO, got %q", byID[10].EstadoPago)
	}
	if byID[11].EstadoPago != domain.EstadoPagoMoroso {
		t.Errorf("clienteID=11: expected MOROSO, got %q", byID[11].EstadoPago)
	}
}
