// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// AbrirGarantia reads the key before creating anything, so both of its reads can
// fail. A read error must never be mistaken for "the key is free": that mistake
// would open a second folio for one request.

// TestAbrirGarantia_ErrorAlLeerLaClave propaga el fallo de la consulta de
// idempotencia y no abre ningun folio.
func TestAbrirGarantia_ErrorAlLeerLaClave(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, fgen, _, _, clk, _ := setupService()
	erepo.getErr = errors.New("lectura de la clave fallo")

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "lectura de la clave fallo" {
		t.Fatalf("want el error del repositorio tal cual, got %v", err)
	}
	if len(grepo.created) != 0 || fgen.calls != 0 {
		t.Errorf("no debe crear nada: created=%d folio=%d", len(grepo.created), fgen.calls)
	}
}

// TestAbrirGarantia_FolioInvalido: el contador devolvio un numero que no es un
// folio valido, y sin folio no hay garantia que abrir.
func TestAbrirGarantia_FolioInvalido(t *testing.T) {
	t.Parallel()
	svc, grepo, _, fgen, _, _, clk, _ := setupService()
	fgen.nums = []int{0}

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrFolioInvalido) {
		t.Fatalf("want ErrFolioInvalido, got %v", err)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0", len(grepo.created))
	}
}

// TestAbrirGarantia_FallaElCrear propaga el error de Crear y deja la transaccion
// revertida.
func TestAbrirGarantia_FallaElCrear(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	grepo.createErr = errors.New("Crear fallo")

	_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
		Origen:            domain.OrigenFolioPiso,
		Description:       "mueble roto",
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "Crear fallo" {
		t.Fatalf("want el error del repositorio tal cual, got %v", err)
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
	}
	if len(grepo.created) != 0 {
		t.Errorf("created = %d, want 0 tras el rollback", len(grepo.created))
	}
}
