// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestAgregarArticulo_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	antes := g.ArticulosCount()

	res, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa de centro desvencijada",
		ClaveIdempotencia: "k-agregar-1",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("AgregarArticulo: %v", err)
	}
	if res.ArticulosCount() != antes+1 {
		t.Errorf("articulos = %d, want %d", res.ArticulosCount(), antes+1)
	}
	if len(repo.saved) != 1 {
		t.Errorf("saved = %d, want 1", len(repo.saved))
	}
	if tx.calls != 1 {
		t.Errorf("tx.calls = %d, want 1", tx.calls)
	}
}

func TestAgregarArticulo_NoAutenticado(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.errUser = domain.ErrUsuarioNoAutenticado

	_, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa",
		ClaveIdempotencia: "k-x",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrUsuarioNoAutenticado) {
		t.Fatalf("want ErrUsuarioNoAutenticado, got %v", err)
	}
	if tx.calls != 0 || len(repo.saved) != 0 || len(repo.created) != 0 {
		t.Errorf("no debe escribir: tx=%d saved=%d created=%d", tx.calls, len(repo.saved), len(repo.created))
	}
}

func TestAgregarArticulo_SinPermiso(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.denegar = &permCrear

	_, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa",
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

func TestAgregarArticulo_ClaveRepetida_NoEscribe(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	erepo.addEvento(hydrateEvento(g.ID(), "k-rep", clk.Now()))

	res, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa",
		ClaveIdempotencia: "k-rep",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("una repeticion no es error: %v", err)
	}
	if res == nil || res.ID() != g.ID() {
		t.Fatalf("debe devolver el folio existente")
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0 (repeticion no escribe)", len(repo.saved))
	}
}

func TestAgregarArticulo_CarreraEnGuardar(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)

	// The pre-check finds nothing, then the twin commits the same key while we
	// are saving: Guardar rejects on the unique index and the command must
	// still come back as a success carrying the folio (spec §3.4).
	repo.onSave = func() {
		erepo.addEvento(hydrateEvento(g.ID(), "k-carrera", clk.Now()))
	}
	repo.saveErr = domain.ErrClaveIdempotenciaDuplicada

	res, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa",
		ClaveIdempotencia: "k-carrera",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la carrera se trata como repeticion: %v", err)
	}
	if res == nil || res.ID() != g.ID() {
		t.Fatalf("debe devolver el folio existente")
	}
}

func TestAgregarArticulo_ClaveDeOtroFolio(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, tx, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	otro := seedFolio(t, repo, clk.Now(), 1)
	erepo.addEvento(hydrateEvento(otro.ID(), "k-ajeno", clk.Now()))

	_, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "mesa",
		ClaveIdempotencia: "k-ajeno",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
		t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0", len(repo.saved))
	}
	if tx.calls != 1 {
		t.Errorf("la transaccion si corre: tx.calls = %d", tx.calls)
	}
}

func TestAgregarArticulo_ErrorDeDominio_NoGuarda(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 0)

	_, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
		GarantiaID:        g.ID(),
		Clave:             "B",
		Description:       "",
		ClaveIdempotencia: "k-dom",
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil {
		t.Fatal("descripcion vacia debe fallar en el dominio")
	}
	if len(repo.saved) != 0 {
		t.Errorf("un error de dominio no debe Guardar: saved = %d", len(repo.saved))
	}
}
