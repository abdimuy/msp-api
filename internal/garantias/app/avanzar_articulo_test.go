// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestAvanzarArticulo_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	artID := firstArticuloID(t, g)

	res, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        artID,
		EtapaDestino:      domain.EtapaPendienteRecoleccion,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("AvanzarArticulo: %v", err)
	}
	for a := range res.Articulos() {
		if a.ID() == artID && a.Etapa() != domain.EtapaPendienteRecoleccion {
			t.Errorf("etapa = %q, want pendiente_recoleccion", a.Etapa())
		}
	}
	if len(repo.saved) != 1 || tx.calls != 1 {
		t.Errorf("saved=%d tx=%d, want 1/1", len(repo.saved), tx.calls)
	}
	usuarioEventos(t, repo.lastSaved, "Juan")
}

func TestAvanzarArticulo_NoAutenticado(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.errUser = domain.ErrUsuarioNoAutenticado

	_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        firstArticuloID(t, g),
		EtapaDestino:      domain.EtapaPendienteRecoleccion,
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

func TestAvanzarArticulo_SinPermiso(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, id, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	id.denegar = &permActualizar

	_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        firstArticuloID(t, g),
		EtapaDestino:      domain.EtapaPendienteRecoleccion,
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

func TestAvanzarArticulo_ClaveRepetida_NoEscribe(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	erepo.addEvento(hydrateEvento(g.ID(), k, domain.TipoEventoArticuloAgregado, clk.Now()))

	res, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        firstArticuloID(t, g),
		EtapaDestino:      domain.EtapaPendienteRecoleccion,
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

func TestAvanzarArticulo_CarreraEnGuardar(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, tx, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	repo.onSave = func() {
		erepo.addEvento(hydrateEvento(g.ID(), k, domain.TipoEventoArticuloAgregado, clk.Now()))
	}
	repo.saveErr = domain.ErrClaveIdempotenciaDuplicada

	res, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        firstArticuloID(t, g),
		EtapaDestino:      domain.EtapaPendienteRecoleccion,
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
	if !ok {
		t.Fatal("el folio original se perdio")
	}
	for a := range estado.Articulos() {
		if a.Etapa() != domain.EtapaRegistrado {
			t.Errorf("etapa = %q, want la original (registrado)", a.Etapa())
		}
	}
}

func TestAvanzarArticulo_ClaveDeOtroFolio(t *testing.T) {
	t.Parallel()
	svc, repo, erepo, _, _, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)
	otro := seedFolio(t, repo, clk.Now(), 1)
	k := clave()
	erepo.addEvento(hydrateEvento(otro.ID(), k, domain.TipoEventoArticuloAgregado, clk.Now()))

	_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        firstArticuloID(t, g),
		EtapaDestino:      domain.EtapaPendienteRecoleccion,
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

func TestAvanzarArticulo_TransicionInvalida_NoGuarda(t *testing.T) {
	t.Parallel()
	svc, repo, _, _, tx, _, clk := setupService()
	g := seedFolio(t, repo, clk.Now(), 1)

	// registrado -> listo_entrega no es una arista valida de la maquina
	_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
		GarantiaID:        g.ID(),
		ArticuloID:        firstArticuloID(t, g),
		EtapaDestino:      domain.EtapaListoEntrega,
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
		t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
	}
	if len(repo.saved) != 0 {
		t.Errorf("un error de dominio no debe Guardar: saved = %d", len(repo.saved))
	}
	if tx.calls != 1 {
		t.Errorf("la transaccion si corre: tx.calls = %d", tx.calls)
	}
}
