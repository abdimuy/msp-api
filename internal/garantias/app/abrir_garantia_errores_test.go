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

// AbrirGarantia runs on the same transaction rules as resolverDuplicada: it
// cannot pre-read its folio (there is none yet), so the duplicate key it may hit
// is the one its own Crear raised.

// TestAbrirGarantia_ClaveInvalida rechaza una clave que no es UUID antes de
// tocar la base.
func TestAbrirGarantia_ClaveInvalida(t *testing.T) {
	t.Parallel()
	svc, grepo, _, fgen, tx, _, clk, _ := setupService()

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: "no-es-uuid",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrEventoClaveIdempotenciaInvalida) {
		t.Fatalf("want ErrEventoClaveIdempotenciaInvalida, got %v", err)
	}
	if tx.calls != 0 || fgen.calls != 0 || len(grepo.created) != 0 {
		t.Errorf("no debe tocar la base: tx=%d folio=%d created=%d", tx.calls, fgen.calls, len(grepo.created))
	}
}

// TestAbrirGarantia_ErrorDeGenerador: si el contador de folios falla, no se abre
// nada.
func TestAbrirGarantia_ErrorDeGenerador(t *testing.T) {
	t.Parallel()
	svc, grepo, _, fgen, _, _, clk, _ := setupService()
	fgen.err = errors.New("contador de folios caido")

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "contador de folios caido" {
		t.Fatalf("want el error del generador tal cual, got %v", err)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0", len(grepo.created))
	}
}

// TestAbrirGarantia_ErrorDelDominio: una apertura invalida — un folio de piso no
// lleva domicilio — no crea nada.
func TestAbrirGarantia_ErrorDelDominio(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		Calle:             "Reforma",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrDomicilioNoPermitido) {
		t.Fatalf("want ErrDomicilioNoPermitido, got %v", err)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0", len(grepo.created))
	}
}

// TestAbrirGarantia_GanaElGemelo cubre el duplicado de §3.3: el gemelo gano el
// UNIQUE, la escritura a medias se revierte y esta llamada responde con el folio
// del gemelo. Nunca con dos folios.
func TestAbrirGarantia_GanaElGemelo(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, tx, _, clk, _ := setupService()
	k := clave()
	gemelo := sembrar(t, grepo, domain.EstadoFolioAbierto, piso(), domain.EtapaRegistrado)
	grepo.createErr = domain.ErrClaveIdempotenciaDuplicada
	grepo.onCreate = func() {
		erepo.addEvento(hydrateEvento(gemelo.g.ID(), k, domain.TipoEventoFolioAbierto, clk.Now()))
	}

	res, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la carrera debe resolverse como exito: %v", err)
	}
	if res == nil || res.ID() != gemelo.g.ID() {
		t.Fatalf("debe volver el folio del gemelo, got %v", res)
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0: el folio a medias se revierte", len(grepo.created))
	}
}

// TestAbrirGarantia_ClaveDeOtroComando: la clave existe pero detras hay otro
// comando. AbrirGarantia no tiene folio propio contra el que comparar, asi que
// decide por contenido (review del 20-oct): el evento no es folio_abierto, de
// modo que reutilizar la clave es un mismatch.
func TestAbrirGarantia_ClaveDeOtroComando(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	k := clave()
	erepo.addEvento(hydrateEvento(uuid.New(), k, domain.TipoEventoNota, clk.Now()))

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaMismatch) {
		t.Fatalf("want ErrClaveIdempotenciaMismatch, got %v", err)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0", len(grepo.created))
	}
}

// TestAbrirGarantia_RepetidaPorClave: la repeticion devuelve el folio que abrio
// esa clave y no crea otro.
func TestAbrirGarantia_RepetidaPorClave(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, fgen, _, _, clk, _ := setupService()
	k := clave()
	original := sembrar(t, grepo, domain.EstadoFolioAbierto, piso(), domain.EtapaRegistrado)
	erepo.addEvento(hydrateEvento(original.g.ID(), k, domain.TipoEventoFolioAbierto, clk.Now()))

	res, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: k,
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la repeticion debe tener exito: %v", err)
	}
	if res == nil || res.ID() != original.g.ID() {
		t.Fatalf("debe volver el folio original, got %v", res)
	}
	if fgen.calls != 0 {
		t.Errorf("folio calls = %d, want 0: no se pide folio nuevo", fgen.calls)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0", len(grepo.created))
	}
}

// TestAbrirGarantia_ClaveDeOtroComandoSinDueno: el duplicado sin gemelo no es un
// exito, es el error tal cual.
func TestAbrirGarantia_DuplicadaSinDueno(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()
	grepo.createErr = domain.ErrClaveIdempotenciaDuplicada

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
		t.Fatalf("want ErrClaveIdempotenciaDuplicada, got %v", err)
	}
}
