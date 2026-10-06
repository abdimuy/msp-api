// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestIniciarProceso_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-proceso-1",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("IniciarProceso: %v", err)
	}
	if res.Estado() != domain.EstadoFolioEnProceso {
		t.Errorf("estado = %q, want en_proceso", res.Estado())
	}
	if len(repo.saved) != 1 || tx.calls != 1 {
		t.Errorf("saved=%d tx=%d, want 1/1", len(repo.saved), tx.calls)
	}
}

func TestIniciarProceso_NoAutenticado(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.errUser = domain.ErrUsuarioNoAutenticado

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-x",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrUsuarioNoAutenticado) {
		t.Fatalf("want ErrUsuarioNoAutenticado, got %v", err)
	}
	if tx.calls != 0 || len(repo.saved) != 0 {
		t.Errorf("no debe escribir: tx=%d saved=%d", tx.calls, len(repo.saved))
	}
}

func TestIniciarProceso_SinPermiso(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.denegar = &permActualizar

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-x",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrPermisoDenegado) {
		t.Fatalf("want ErrPermisoDenegado, got %v", err)
	}
	if tx.calls != 0 || len(repo.saved) != 0 {
		t.Errorf("no debe escribir: tx=%d saved=%d", tx.calls, len(repo.saved))
	}
}

func TestIniciarProceso_ClaveRepetida_NoEscribe(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	erepo.addEvento(hydrateEvento(g.ID(), "k-rep-pr", clk.Now()))

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-rep-pr",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("una repeticion no es error: %v", err)
	}
	if res == nil || res.ID() != g.ID() {
		t.Fatal("debe devolver el folio existente")
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0", len(repo.saved))
	}
}

func TestIniciarProceso_CarreraEnGuardar(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	repo.onSave = func() {
		erepo.addEvento(hydrateEvento(g.ID(), "k-carrera-pr", clk.Now()))
	}
	repo.saveErr = domain.ErrClaveIdempotenciaDuplicada

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-carrera-pr",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la carrera se trata como repeticion: %v", err)
	}
	if res == nil || res.ID() != g.ID() {
		t.Fatal("debe devolver el folio existente")
	}
}

func TestIniciarProceso_ClaveDeOtroFolio(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	otro := seedFolio(t, repo, clk.Now(), 1)
	erepo.addEvento(hydrateEvento(otro.ID(), "k-ajeno-pr", clk.Now()))

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-ajeno-pr",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
		t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0", len(repo.saved))
	}
}

func TestIniciarProceso_SinArticulos_NoGuarda(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 0)

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: "k-sin-art",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrGarantiaSinArticulos) {
		t.Fatalf("want ErrGarantiaSinArticulos, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("un error de dominio no debe Guardar: saved = %d", len(repo.saved))
	}
}

func TestAbrirGarantia_ClaveRepetida_ReusaElFolio(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	erepo.addEvento(hydrateEvento(g.ID(), "k-open", clk.Now()))

	res, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "silla",
		ClaveIdempotencia: "k-open",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("una repeticion no es error: %v", err)
	}
	if res == nil || res.ID() != g.ID() {
		t.Fatal("debe devolver el folio que abrio esa clave")
	}
	if len(repo.created) != 0 {
		t.Errorf("created = %d, want 0", len(repo.created))
	}
}
