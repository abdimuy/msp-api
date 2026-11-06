// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// TestRegistrarDictamen_CaminoFeliz applies a no-fault verdict on a supplier
// article and lands it where that branch waits for the client.
func TestRegistrarDictamen_CaminoFeliz(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso,
		conRuta(piso(), domain.RutaReparacionProveedor), domain.EtapaDictamenRecibido)

	res, err := svc.RegistrarDictamen(context.Background(), app.RegistrarDictamenCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Dictamen:          string(domain.DictamenSinFalla),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("RegistrarDictamen: %v", err)
	}

	art := articuloDe(t, res, s.primero())
	if art.Etapa() != domain.EtapaEsperaRespuestaCliente {
		t.Errorf("etapa = %s, want espera_respuesta_cliente", art.Etapa())
	}
	if art.Dictamen() == nil || *art.Dictamen() != domain.DictamenSinFalla {
		t.Errorf("dictamen = %v, want sin_falla", art.Dictamen())
	}
	if tx.calls != 1 || len(grepo.saved) != 1 {
		t.Errorf("tx = %d, saved = %d; want 1 y 1", tx.calls, len(grepo.saved))
	}
	if ev := eventoEscrito(t, grepo); ev.Tipo() != domain.TipoEventoDictamenRegistrado {
		t.Errorf("evento = %s, want dictamen_registrado", ev.Tipo())
	}
}

// TestRegistrarDictamen_SinRutaProveedor pins cross-validation #1: a verdict is
// only meaningful on a supplier route, and the domain refuses anything else.
func TestRegistrarDictamen_SinRutaProveedor(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaDictamenRecibido)

	_, err := svc.RegistrarDictamen(context.Background(), app.RegistrarDictamenCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Dictamen:          string(domain.DictamenAceptada),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrArticuloDictamenSoloProveedor) {
		t.Fatalf("want ErrArticuloDictamenSoloProveedor, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestRegistrarDictamen_DictamenInvalido rejects an unknown verdict while
// parsing the command, before the transaction opens.
func TestRegistrarDictamen_DictamenInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso,
		conRuta(piso(), domain.RutaReparacionProveedor), domain.EtapaDictamenRecibido)

	_, err := svc.RegistrarDictamen(context.Background(), app.RegistrarDictamenCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Dictamen:          "quizas",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrDictamenInvalido) {
		t.Fatalf("want ErrDictamenInvalido, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestRegistrarDictamen_EtapaNoCorresponde only applies from dictamen_recibido:
// the stage machine refuses the jump from anywhere else.
func TestRegistrarDictamen_EtapaNoCorresponde(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso,
		conRuta(piso(), domain.RutaReparacionProveedor), domain.EtapaEnRevision)

	_, err := svc.RegistrarDictamen(context.Background(), app.RegistrarDictamenCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Dictamen:          string(domain.DictamenSinFalla),
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
