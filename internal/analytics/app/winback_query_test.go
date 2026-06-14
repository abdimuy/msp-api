//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestListarWinback_Ordering(t *testing.T) {
	t.Parallel()

	// Build candidates with known scores:
	//  - c1: recent, high value, with phone → very high score (ACTIVO)
	//  - c2: recent, lower value, no phone  → lower score (ACTIVO)
	//  - c3: lapsed 400d, high monetary, phone, saldo → LEAL_POR_LIQUIDAR
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
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	// Expected order (Score desc, Monetary desc on tie, ClienteID asc on tie):
	//   [0] c1 clienteID=1 score=85 (ACTIVO, monetary=50000, phone)
	//   [1] c3 clienteID=3 score=68 (LEAL_POR_LIQUIDAR, monetary=25000, phone, porLiq=50%)
	//   [2] c2 clienteID=2 score=34 (ACTIVO, monetary=5000, no phone)
	wantOrder := []struct {
		clienteID int
		score     int
	}{
		{clienteID: 1, score: 85},
		{clienteID: 3, score: 68},
		{clienteID: 2, score: 34},
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

	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, Zona: "NORTE", FechaUltimaCompra: testNow.AddDate(0, 0, -10),
		Frecuencia: 3, Monetary: decimal.NewFromInt(5_000),
		CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, Zona: "SUR", FechaUltimaCompra: testNow.AddDate(0, 0, -10),
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

	// c1: treatment group
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, FechaUltimaCompra: testNow.AddDate(0, 0, -10),
		Frecuencia: 3, Monetary: decimal.NewFromInt(5_000),
		EnControl: false, CohorteFecha: testNow.AddDate(-1, 0, 0), Now: testNow,
	})
	// c2: control group
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, FechaUltimaCompra: testNow.AddDate(0, 0, -10),
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

	// Build 5 candidates with distinct scores (different monetary, same recency).
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
		Limit: 2, IncluirControl: true,
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
