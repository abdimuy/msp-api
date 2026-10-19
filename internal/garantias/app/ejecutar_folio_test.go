// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// These tests cover the guards ejecutarFolio puts around every mutation. Each
// one is written against RegistrarDiagnostico because that is the shape all
// thirteen commands share: a key, a folio and, on the supplier route, a decider
// role. The command itself is covered in its own file.

// TestEjecutarFolio_ClaveObligatoria refuses an empty idempotency key. The key
// is the whole contract of §3.3 — without it a retry would mutate twice — so a
// missing one stops the command before the transaction opens.
func TestEjecutarFolio_ClaveObligatoria(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:      s.g.ID(),
		ArticuloID:      s.primero(),
		Ruta:            string(domain.RutaReparacionProveedor),
		RolDecisor:      rolDecisorPrueba(),
		DeviceCreatedAt: clk.Now(),
	})
	if !errors.Is(err, domain.ErrEventoClaveIdempotenciaObligatoria) {
		t.Fatalf("want ErrEventoClaveIdempotenciaObligatoria, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestEjecutarFolio_ClaveInvalida refuses a key that is not a UUID, and says so
// as a validation problem rather than a conflict.
func TestEjecutarFolio_ClaveInvalida(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: "no-es-uuid",
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrEventoClaveIdempotenciaInvalida) {
		t.Fatalf("want ErrEventoClaveIdempotenciaInvalida, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestEjecutarFolio_ClaveNoCanonica replays when the key arrives spelled
// differently — braces and uppercase, which uuid.Parse accepts. The domain
// stores the canonical form, so a lookup with the raw spelling would miss its
// own event and race into the UNIQUE index.
func TestEjecutarFolio_ClaveNoCanonica(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, tx, _, clk, _ := setupService()
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
	k := clave()
	// The event is registered under the canonical spelling the domain writes,
	// while the phone sends the same UUID with braces and capitals.
	erepo.addEvento(hydrateEvento(s.g.ID(), k, domain.TipoEventoDiagnosticoRegistrado, clk.Now()))

	res, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: "{" + strings.ToUpper(k) + "}",
		DeviceCreatedAt:   clk.Now(),
	})
	if err != nil {
		t.Fatalf("la repeticion por clave debe tener exito: %v", err)
	}
	if res == nil || res.ID() != s.g.ID() {
		t.Fatalf("debe volver el folio, got %v", res)
	}
	if !sinFilas(grepo) {
		t.Errorf("la repeticion no escribe: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
	if tx.calls != 1 {
		t.Errorf("tx = %d, want 1", tx.calls)
	}
}

// TestEjecutarFolio_ErrorDeIdentidad y de permiso son errores de infraestructura,
// no denegaciones: se propagan tal cual y nada se escribe.
func TestEjecutarFolio_ErrorDeIdentidad(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, id, clk, _ := setupService()
	id.errUser = errors.New("firebase caido")
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "firebase caido" {
		t.Fatalf("want el error de Identity tal cual, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

func TestEjecutarFolio_ErrorDePermiso(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, id, clk, _ := setupService()
	id.errPerm = errors.New("servicio de permisos caido")
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "servicio de permisos caido" {
		t.Fatalf("want el error de Identity tal cual, got %v", err)
	}
	if !sinEscrituras(tx, grepo) {
		t.Errorf("no debe abrir transaccion: tx=%d", tx.calls)
	}
}

// TestEjecutarFolio_FolioInexistente: el folio se bloquea antes que la clave, así
// que un id desconocido no llega ni a mirar la clave.
func TestEjecutarFolio_FolioInexistente(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, _, _, clk, _ := setupService()

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        uuid.New(),
		ArticuloID:        uuid.New(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if !errors.Is(err, domain.ErrGarantiaNoEncontrada) {
		t.Fatalf("want ErrGarantiaNoEncontrada, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestEjecutarFolio_ErrorAlLeerLaClave propaga un fallo de la consulta de
// idempotencia sin mutar nada. Sin esto, un error de lectura se confundiría con
// "clave ausente" y la operación se ejecutaría dos veces.
func TestEjecutarFolio_ErrorAlLeerLaClave(t *testing.T) {
	t.Parallel()
	svc, grepo, erepo, _, _, _, clk, _ := setupService()
	erepo.getErr = errors.New("lectura de la clave fallo")
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "lectura de la clave fallo" {
		t.Fatalf("want el error del repositorio tal cual, got %v", err)
	}
	if !sinFilas(grepo) {
		t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
	}
}

// TestEjecutarFolio_FallaElGuardado propaga el error del repositorio y la
// transacción se revierte: nada queda guardado.
func TestEjecutarFolio_FallaElGuardado(t *testing.T) {
	t.Parallel()
	svc, grepo, _, _, tx, _, clk, _ := setupService()
	grepo.saveErr = errors.New("Guardar fallo")
	s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

	_, err := svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
		GarantiaID:        s.g.ID(),
		ArticuloID:        s.primero(),
		Ruta:              string(domain.RutaReparacionProveedor),
		RolDecisor:        rolDecisorPrueba(),
		ClaveIdempotencia: clave(),
		DeviceCreatedAt:   clk.Now(),
	})
	if err == nil || err.Error() != "Guardar fallo" {
		t.Fatalf("want el error del repositorio tal cual, got %v", err)
	}
	if tx.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", tx.rollbacks)
	}
	if len(grepo.saved) != 0 {
		t.Errorf("saved = %d, want 0 tras el rollback", len(grepo.saved))
	}
}
