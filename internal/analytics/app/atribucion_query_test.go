//nolint:misspell // Spanish vocabulary per project convention.
package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/analytics/app"
	"github.com/abdimuy/msp-api/internal/analytics/domain"
)

func TestAtribucion_BasicCounts(t *testing.T) {
	t.Parallel()

	cohorteFecha := testNow.AddDate(-3, 0, 0)

	// treatment — convertido (purchase after cohort)
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, EnControl: false,
		FechaUltimaCompra: testNow.AddDate(0, -1, 0), // after cohort
		Frecuencia:        3, Monetary: decimal.NewFromInt(10_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})
	// treatment — no convertido (no purchase)
	c2 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 2, EnControl: false,
		FechaUltimaCompra: time.Time{}, // zero
		Frecuencia:        0, Monetary: decimal.NewFromInt(5_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})
	// treatment — no convertido (purchase BEFORE cohort)
	c3 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 3, EnControl: false,
		FechaUltimaCompra: testNow.AddDate(-4, 0, 0), // before cohort
		Frecuencia:        2, Monetary: decimal.NewFromInt(8_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})
	// control — convertido
	c4 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 4, EnControl: true,
		FechaUltimaCompra: testNow.AddDate(0, -2, 0), // after cohort
		Frecuencia:        3, Monetary: decimal.NewFromInt(12_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})
	// control — no convertido
	c5 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 5, EnControl: true,
		FechaUltimaCompra: time.Time{},
		Frecuencia:        0, Monetary: decimal.NewFromInt(3_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c1, c2, c3, c4, c5}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	res, err := svc.Atribucion(context.Background(), app.AtribucionParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// treatment: 3 total, 1 converted
	if res.TreatmentTotal != 3 {
		t.Errorf("TreatmentTotal: got %d, want 3", res.TreatmentTotal)
	}
	if res.TreatmentConvertidos != 1 {
		t.Errorf("TreatmentConvertidos: got %d, want 1", res.TreatmentConvertidos)
	}
	// control: 2 total, 1 converted
	if res.ControlTotal != 2 {
		t.Errorf("ControlTotal: got %d, want 2", res.ControlTotal)
	}
	if res.ControlConvertidos != 1 {
		t.Errorf("ControlConvertidos: got %d, want 1", res.ControlConvertidos)
	}

	// TasaTreatment = 1/3
	wantTasaTreatment := decimal.NewFromInt(1).Div(decimal.NewFromInt(3))
	if !res.TasaTreatment.Equal(wantTasaTreatment) {
		t.Errorf("TasaTreatment: got %s, want %s", res.TasaTreatment, wantTasaTreatment)
	}

	// TasaControl = 1/2
	wantTasaControl := decimal.NewFromInt(1).Div(decimal.NewFromInt(2))
	if !res.TasaControl.Equal(wantTasaControl) {
		t.Errorf("TasaControl: got %s, want %s", res.TasaControl, wantTasaControl)
	}

	// Uplift = TasaTreatment - TasaControl (negative here)
	wantUplift := wantTasaTreatment.Sub(wantTasaControl)
	if !res.Uplift.Equal(wantUplift) {
		t.Errorf("Uplift: got %s, want %s", res.Uplift, wantUplift)
	}
}

func TestAtribucion_EqualFechaAndCohorteFecha_NotConvertido(t *testing.T) {
	t.Parallel()

	// A candidate whose FechaUltimaCompra EQUALS CohorteFecha must NOT be
	// counted as converted — conversion requires strictly After.
	cohorteFecha := testNow.AddDate(-1, 0, 0)
	c1 := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 1, EnControl: false,
		FechaUltimaCompra: cohorteFecha, // equal, not after
		Frecuencia:        2, Monetary: decimal.NewFromInt(8_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{c1}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	res, err := svc.Atribucion(context.Background(), app.AtribucionParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.TreatmentTotal != 1 {
		t.Errorf("TreatmentTotal: got %d, want 1", res.TreatmentTotal)
	}
	if res.TreatmentConvertidos != 0 {
		t.Errorf("TreatmentConvertidos: got %d, want 0 — equal fecha must not count as convertido",
			res.TreatmentConvertidos)
	}
}

func TestAtribucion_EmptyGroups_NoZeroDivide(t *testing.T) {
	t.Parallel()

	repo := newFakeWinbackRepo()
	repo.candidates = nil // empty set
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	res, err := svc.Atribucion(context.Background(), app.AtribucionParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.TasaTreatment.IsZero() {
		t.Errorf("TasaTreatment should be 0 for empty treatment group, got %s", res.TasaTreatment)
	}
	if !res.TasaControl.IsZero() {
		t.Errorf("TasaControl should be 0 for empty control group, got %s", res.TasaControl)
	}
	if !res.Uplift.IsZero() {
		t.Errorf("Uplift should be 0, got %s", res.Uplift)
	}
}

func TestAtribucion_ControlIncludedAlways(t *testing.T) {
	t.Parallel()

	// Even when IncluirControl would normally filter control out for ListarWinback,
	// Atribucion must always include control candidates.
	cohorteFecha := testNow.AddDate(-1, 0, 0)
	ctrl := mustCandidato(domain.CrearWinbackCandidatoParams{
		ClienteID: 10, EnControl: true,
		FechaUltimaCompra: testNow.AddDate(0, -1, 0),
		Frecuencia:        3, Monetary: decimal.NewFromInt(10_000),
		CohorteFecha: cohorteFecha, Now: testNow,
	})

	repo := newFakeWinbackRepo()
	repo.candidates = []*domain.WinbackCandidato{ctrl}
	svc := app.NewService(repo, nil, fixedClock{testNow}, nil)

	res, err := svc.Atribucion(context.Background(), app.AtribucionParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ControlTotal != 1 {
		t.Errorf("ControlTotal: got %d, want 1 — control must always be included", res.ControlTotal)
	}
}
