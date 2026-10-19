// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestRegistrarDiagnostico_CaminoFeliz fixes the route and advances the article
// in one call: en_revision becomes orden_generada with the supplier route
// recorded, and the decision carries the operator's decider role.
func TestRegistrarDiagnostico_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	res, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("RegistrarDiagnostico: %v", err)
	}

	art := articuloDe(t, res, s.primero())
	if art.Etapa() != domain.EtapaOrdenGenerada {
		t.Errorf("etapa = %s, want orden_generada", art.Etapa())
	}
	if art.Ruta() == nil || *art.Ruta() != domain.RutaReparacionProveedor {
		t.Errorf("ruta = %v, want proveedor", art.Ruta())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}

	ev := eventoEscrito(t, grepo)
	if ev.Tipo() != domain.TipoEventoDiagnosticoRegistrado {
		t.Errorf("evento = %s, want diagnostico_registrado", ev.Tipo())
	}
	if ev.RolDecisor() == nil || *ev.RolDecisor() != domain.RolDecisorOficina {
		t.Errorf("rol del evento = %v, want oficina", ev.RolDecisor())
	}
	if ev.EtapaDesde() == nil || *ev.EtapaDesde() != domain.EtapaEnRevision {
		t.Errorf("etapa desde = %v, want en_revision", ev.EtapaDesde())
	}
	if ev.EtapaHasta() == nil || *ev.EtapaHasta() != domain.EtapaOrdenGenerada {
		t.Errorf("etapa hasta = %v, want orden_generada", ev.EtapaHasta())
	}
}

// TestRegistrarDiagnostico_RolObligatorio pins that the missing decider role is
// the domain's invariant, not a pre-check: the folio is locked, the domain
// refuses, and the transaction rolls back without rows.
func TestRegistrarDiagnostico_RolObligatorio(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrRolDecisorObligatorio) {
		t.Fatalf("want ErrRolDecisorObligatorio, got %v", err)
	}
	if tx.calls != 1 {
		t.Errorf("tx = %d, want 1: la regla vive en el dominio", tx.calls)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestRegistrarDiagnostico_RolInvalido pins the other half: an unknown role is
// rejected while parsing the command, before the transaction opens.
func TestRegistrarDiagnostico_RolInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        "gerente",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrRolDecisorInvalido) {
		t.Fatalf("want ErrRolDecisorInvalido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestRegistrarDiagnostico_RutaInvalida rejects an unknown route by parsing it
// first, without touching the store.
func TestRegistrarDiagnostico_RutaInvalida(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              "magia",
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrRutaReparacionInvalida) {
		t.Fatalf("want ErrRutaReparacionInvalida, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestRegistrarDiagnostico_EtapaNoCorresponde lets the stage machine speak: from
// registrado the article cannot jump to orden_generada, and the command returns
// the domain's error untouched.
func TestRegistrarDiagnostico_EtapaNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaRegistrado)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrTransicionEtapaNoPermitida) {
		t.Fatalf("want ErrTransicionEtapaNoPermitida, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}
