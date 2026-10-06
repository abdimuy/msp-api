// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// The five permission codes of spec §7, as pointers so a fake can deny exactly
// one without spelling out the other four.
var (
	permCrear      = domain.PermisoCrear
	permActualizar = domain.PermisoActualizar
)

func setupService() (*app.Service, *fakeGarantiaRepo, *fakeEventoRepo, *fakeFolioGen, *fakeTxRunner, *fakeIdentity, *fakeClock) {
	grepo := newFakeGarantiaRepo()
	erepo := newFakeEventoRepo()
	fgen := &fakeFolioGen{nums: []int{1001}}
	tx := &fakeTxRunner{}
	id := &fakeIdentity{usuario: outbound.Usuario{ID: "u1", Nombre: "Juan"}}
	clk := &fakeClock{now: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	svc := app.NewService(app.Deps{
		GarantiaRepo:   grepo,
		EventoRepo:     erepo,
		FolioGenerator: fgen,
		TxRunner:       tx,
		Identity:       id,
		Clock:          clk,
	})
	return svc, grepo, erepo, fgen, tx, id, clk
}

func TestAbrirGarantia_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, fgen, tx, _, clk := setupService()
	cmd := app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: "k1",
		DeviceCreatedAt:   clk.Now(),
	}
	g, err := svc.AbrirGarantia(context.Background(), cmd)
	if err != nil {
		t.Fatalf("AbrirGarantia: %v", err)
	}
	if g == nil {
		t.Fatal("garantia nil")
	}
	if fgen.calls != 1 {
		t.Errorf("folio calls = %d", fgen.calls)
	}
	if tx.calls < 1 {
		t.Errorf("tx.calls = %d", tx.calls)
	}
	if len(grepo.created) != 1 {
		t.Errorf("created = %d", len(grepo.created))
	}
	if g.AbiertoPor() != "Juan" {
		t.Errorf("AbiertoPor = %q, want el nombre que trae Identity", g.AbiertoPor())
	}
}

func TestAbrirGarantia_NoAutenticado(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, id, clk := setupService()
	id.errUser = domain.ErrUsuarioNoAutenticado
	cmd := app.AbrirGarantiaCmd{
		Origen:          domain.OrigenFolioPiso,
		Description:     "mueble",
		DeviceCreatedAt: clk.Now(),
	}
	_, err := svc.AbrirGarantia(context.Background(), cmd)
	if !errors.Is(err, domain.ErrUsuarioNoAutenticado) {
		t.Fatalf("want ErrUsuarioNoAutenticado, got %v", err)
	}
	if tx.calls != 0 || len(grepo.created) != 0 {
		t.Errorf("no debe escribir: tx=%d created=%d", tx.calls, len(grepo.created))
	}
}

func TestAbrirGarantia_SinPermisoCrear(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, id, clk := setupService()
	id.denegar = &permCrear
	cmd := app.AbrirGarantiaCmd{
		Origen:          domain.OrigenFolioPiso,
		Description:     "mueble",
		DeviceCreatedAt: clk.Now(),
	}
	_, err := svc.AbrirGarantia(context.Background(), cmd)
	if !errors.Is(err, domain.ErrPermisoDenegado) {
		t.Fatalf("want ErrPermisoDenegado, got %v", err)
	}
	if tx.calls != 0 || len(grepo.created) != 0 {
		t.Errorf("no debe escribir: tx=%d created=%d", tx.calls, len(grepo.created))
	}
}
