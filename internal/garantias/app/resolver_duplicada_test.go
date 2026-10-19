// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// These tests cover resolverDuplicada: the path taken when Guardar fails on the
// event UNIQUE index. By then the folio rows were already written and the
// transaction is rolled back, so the winner owns the key and this command has to
// answer with what the winner left — never with a second write.

// TestResolverDuplicada_GanaElGemelo walks the race of §3.4. The twin registers
// its event between the pre-check and Guardar, which makes Guardar stage the
// write and then fail exactly like the UNIQUE index would: the command must roll
// back, resolve the key again and answer with the twin's folio.
func TestResolverDuplicada_GanaElGemelo(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
	k := clave()
	grepo.saveErr = domain.ErrClaveIdempotenciaDuplicada
	grepo.onSave = func() {
		// The twin's event, registered while nothing owns the key yet. Its folio
		// is the one this command must answer with.
		erepo.addEvento(hydrateEvento(s.g.ID(), k, domain.TipoEventoDiagnosticoRegistrado, clk.Now()))
	}

	res, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la carrera debe resolverse como exito: %v", err)
	}
	if res == nil || res.ID() != s.g.ID() {
		t.Fatalf("debe volver el folio del gemelo, got %v", res)
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1: la escritura a medias se revierte", tx.rollbacks)
	}
	if len(grepo.saved) != 0 || len(grepo.created) != 0 {
		t.Errorf("nada debe quedar guardado: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestResolverDuplicada_ClaveDeOtroFolio: el gemelo que gano la carrera no es
// nuestro — la clave pertenece a otro folio o a otro comando — y eso es un
// conflicto, no un exito.
func TestResolverDuplicada_ClaveDeOtroFolio(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
	k := clave()
	grepo.saveErr = domain.ErrClaveIdempotenciaDuplicada
	grepo.onSave = func() {
		erepo.addEvento(hydrateEvento(uuid.New(), k, domain.TipoEventoDiagnosticoRegistrado, clk.Now()))
	}

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
		t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
	}
}

// TestResolverDuplicada_SinDueno: si al releer la clave ya no hay nadie detrás,
// la escritura se perdió sin que otro la reclamara y el error se propaga tal
// cual. Sin esta rama, un fallo de UNIQUE sin gemelo se reportaría como exito.
func TestResolverDuplicada_SinDueno(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
	grepo.saveErr = domain.ErrClaveIdempotenciaDuplicada

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
		t.Fatalf("want ErrClaveIdempotenciaDuplicada, got %v", err)
	}
}

// TestResolverDuplicada_ErrorAlReleer propaga un fallo de la relectura: sin esta
// rama, un error de la base se confundiría con "no hay dueño".
func TestResolverDuplicada_ErrorAlReleer(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
	grepo.saveErr = domain.ErrClaveIdempotenciaDuplicada
	grepo.onSave = func() { erepo.getErr = errors.New("relectura de la clave fallo") }

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "relectura de la clave fallo" {
		t.Fatalf("want el error del repositorio tal cual, got %v", err)
	}
}
