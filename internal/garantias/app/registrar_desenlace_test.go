// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestRegistrarDesenlace_CaminoFeliz closes a standby article as second-hand
// stock, with its stage, its location and the outcome recorded together.
func TestRegistrarDesenlace_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaStandby)

	res, err := svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Desenlace:         string(domain.DesenlaceSegundaMano),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("RegistrarDesenlace: %v", err)
	}

	art := articuloDe(t, res, s.primero())
	if art.Etapa() != domain.EtapaSegundaMano {
		t.Errorf("etapa = %s, want segunda_mano", art.Etapa())
	}
	if art.Ubicacion() != domain.UbicacionAlmacenSegundaMano {
		t.Errorf("ubicacion = %s, want almacen_segunda_mano", art.Ubicacion())
	}
	if art.Desenlace() == nil || *art.Desenlace() != domain.DesenlaceSegundaMano {
		t.Errorf("desenlace = %v, want segunda_mano", art.Desenlace())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoDesenlaceRegistrado {
		t.Errorf("evento = %s, want desenlace_registrado", ev.Tipo())
	}
}

// TestRegistrarDesenlace_RolObligatorio keeps the outcome frozen when nobody
// says in which capacity they are deciding.
func TestRegistrarDesenlace_RolObligatorio(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaStandby)

	_, err := svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Desenlace:         string(domain.DesenlaceSegundaMano),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrRolDecisorObligatorio) {
		t.Fatalf("want ErrRolDecisorObligatorio, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestRegistrarDesenlace_RolInvalido is rejected while parsing the command.
func TestRegistrarDesenlace_RolInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaStandby)

	_, err := svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Desenlace:         string(domain.DesenlaceSegundaMano),
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

// TestRegistrarDesenlace_DesenlaceInvalido rejects an unknown outcome while
// parsing the command.
func TestRegistrarDesenlace_DesenlaceInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaStandby)

	_, err := svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Desenlace:         "regalado",
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrDesenlaceInvalido) {
		t.Fatalf("want ErrDesenlaceInvalido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestRegistrarDesenlace_SoloParalelo pins cross-validation: the three outcomes
// of the main flow cannot be recorded here, and the domain names the reason.
func TestRegistrarDesenlace_SoloParalelo(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaStandby)

	_, err := svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Desenlace:         string(domain.DesenlaceReparado),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrArticuloDesenlaceSoloParalelo) {
		t.Fatalf("want ErrArticuloDesenlaceSoloParalelo, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestRegistrarDesenlace_EtapaNoCorresponde only closes a standby article.
func TestRegistrarDesenlace_EtapaNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Desenlace:         string(domain.DesenlaceSegundaMano),
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
