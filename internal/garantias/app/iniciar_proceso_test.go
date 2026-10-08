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
	svc, repo, _, _, tx, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: clave(),
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
	usuarioEventos(t, repo.lastSaved, "Juan")
}

func TestIniciarProceso_NoAutenticado(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.errUser = domain.ErrUsuarioNoAutenticado

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: clave(),
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
	svc, repo, _, _, tx, id, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.denegar = &permActualizar

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: clave(),
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
	svc, repo, erepo, _, _, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	erepo.addEvento(hydrateEvento(g.ID(), k, domain.TipoEventoEtapaAvanzada, clk.Now()))

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: k,
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
	svc, repo, erepo, _, tx, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	repo.onSave = func() {
		erepo.addEvento(hydrateEvento(g.ID(), k, domain.TipoEventoEtapaAvanzada, clk.Now()))
	}
	repo.saveErr = domain.ErrClaveIdempotenciaDuplicada

	res, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la carrera se trata como repeticion: %v", err)
	}
	if res == nil || res.ID() != g.ID() {
		t.Fatal("debe devolver el folio existente")
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1 (la escritura a medias no se confirma)", tx.rollbacks)
	}
	if len(repo.saved) != 0 {
		t.Errorf("saved = %d, want 0 (el folio no se persistio)", len(repo.saved))
	}
	estado, ok := repo.obtained[g.ID()]
	if !ok || estado.Estado() != domain.EstadoFolioAbierto {
		t.Errorf("el repo no debe haber cambiado: estado = %v", estado.Estado())
	}
}

func TestIniciarProceso_ClaveDeOtroFolio(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	otro := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	erepo.addEvento(hydrateEvento(otro.ID(), k, domain.TipoEventoEtapaAvanzada, clk.Now()))

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: k,
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
	svc, repo, _, _, _, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 0)

	_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
		GarantiaID:        g.ID(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrGarantiaSinArticulos) {
		t.Fatalf("want ErrGarantiaSinArticulos, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("un error de dominio no debe Guardar: saved = %d", len(repo.saved))
	}
}

// TestAbrirGarantia_ClaveRepetida_ReusaElFolio pins the review round: in
// AbrirGarantia a key is a replay only when the event it owns is folio_abierto.
func TestAbrirGarantia_ClaveRepetida_ReusaElFolio(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	erepo.addEvento(hydrateEvento(g.ID(), k, domain.TipoEventoFolioAbierto, clk.Now()))

	res, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "silla",
		ClaveIdempotencia: k,
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

// TestAbrirGarantia_ClaveDeOtroEvento pins the other half: the same key but
// owned by a different command's event is not this command's replay.
func TestAbrirGarantia_ClaveDeOtroEvento(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk, _ := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	erepo.addEvento(hydrateEvento(g.ID(), k, domain.TipoEventoArticuloAgregado, clk.Now()))

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "silla",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
		t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Errorf("created = %d, want 0", len(repo.created))
	}
}

// TestAbrirGarantia_CarreraEnCrear pins review round 1: when Crear fails on
// the UNIQUE the header write must roll back and the twin's folio must win.
func TestAbrirGarantia_CarreraEnCrear(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, tx, _, clk, _ := setupService()
	twin := seedFolio(t, repo, clk.Now(), 0)
	k := clave()
	repo.onCreate = func() {
		erepo.addEvento(hydrateEvento(twin.ID(), k, domain.TipoEventoFolioAbierto, clk.Now()))
	}
	repo.createErr = domain.ErrClaveIdempotenciaDuplicada

	res, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "silla",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la carrera se trata como repeticion: %v", err)
	}
	if res == nil || res.ID() != twin.ID() {
		t.Fatal("debe devolver el folio que abrio el gemelo")
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1 (la cabecera a medias no se confirma)", tx.rollbacks)
	}
	if len(repo.created) != 0 {
		t.Errorf("created = %d, want 0 (la cabecera no se persistio)", len(repo.created))
	}
}

// TestAbrirGarantia_CarreraConClaveDeOtroEvento covers the type check on the
// resolverDuplicada path (review round 2, menor a): the twin that won the
// UNIQUE with a key that belongs to another command's event is not this
// command's replay, even though it owns the key now.
func TestAbrirGarantia_CarreraConClaveDeOtroEvento(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, tx, _, clk, _ := setupService()
	twin := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	repo.onCreate = func() {
		erepo.addEvento(hydrateEvento(twin.ID(), k, domain.TipoEventoArticuloAgregado, clk.Now()))
	}
	repo.createErr = domain.ErrClaveIdempotenciaDuplicada

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "silla",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
		t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1 (la cabecera a medias no se confirma)", tx.rollbacks)
	}
	if len(repo.created) != 0 {
		t.Errorf("created = %d, want 0 (la cabecera no se persistio)", len(repo.created))
	}
}
