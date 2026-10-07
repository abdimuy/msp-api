// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// errBD simulates a Firebird failure coming out of any port. The command must
// pass it through untouched instead of dressing it as a domain error.
var errBD = errors.New("firebird caido")

func TestErroresDePuertoSePropagan(t *testing.T) {
	t.Parallel()

	t.Run("evento_repo_falla_al_ver_la_clave", func(t *testing.T) {
		t.Parallel()
		svc, repo, erepo, _, _, _, clk := setupService()
		g := seedFolio(t, repo, clk.Now(), 1)
		erepo.getErr = errBD

		_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
			GarantiaID:        g.ID(),
			ArticuloID:        firstArticuloID(t, g),
			EtapaDestino:      domain.EtapaPendienteRecoleccion,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, errBD) {
			t.Fatalf("want errBD, got %v", err)
		}
		if len(repo.saved) != 0 {
			t.Errorf("saved = %d, want 0", len(repo.saved))
		}
	})

	t.Run("folio_inexistente", func(t *testing.T) {
		t.Parallel()
		svc, _, _, _, _, _, clk := setupService()

		_, err := svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
			GarantiaID:        uuidAleatorio(),
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, domain.ErrGarantiaNoEncontrada) {
			t.Fatalf("want ErrGarantiaNoEncontrada, got %v", err)
		}
	})

	// Control of TestAgregarArticulo_CarreraEnGuardar: same call site, only the
	// error differs, so only ErrClaveIdempotenciaDuplicada may be swallowed.
	t.Run("guardar_falla_de_verdad", func(t *testing.T) {
		t.Parallel()
		svc, repo, _, _, _, _, clk := setupService()
		g := seedFolio(t, repo, clk.Now(), 1)
		repo.saveErr = errBD

		_, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
			GarantiaID:        g.ID(),
			Clave:             "B",
			Description:       "mesa",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, errBD) {
			t.Fatalf("want errBD, got %v", err)
		}
	})

	t.Run("tiene_permiso_falla", func(t *testing.T) {
		t.Parallel()
		svc, repo, _, _, tx, id, clk := setupService()
		g := seedFolio(t, repo, clk.Now(), 1)
		id.errPerm = errBD

		_, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
			GarantiaID:        g.ID(),
			ArticuloID:        firstArticuloID(t, g),
			EtapaDestino:      domain.EtapaPendienteRecoleccion,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, errBD) {
			t.Fatalf("want errBD, got %v", err)
		}
		if tx.calls != 0 || len(repo.saved) != 0 {
			t.Errorf("no debe escribir: tx=%d saved=%d", tx.calls, len(repo.saved))
		}
	})

	t.Run("generador_de_folio_falla", func(t *testing.T) {
		t.Parallel()
		svc, repo, _, fgen, _, _, clk := setupService()
		fgen.err = errBD

		_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
			Origen:            domain.OrigenFolioPiso,
			Description:       "silla",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, errBD) {
			t.Fatalf("want errBD, got %v", err)
		}
		if len(repo.created) != 0 {
			t.Errorf("created = %d, want 0", len(repo.created))
		}
	})

	t.Run("crear_falla_de_verdad", func(t *testing.T) {
		t.Parallel()
		svc, repo, _, _, _, _, clk := setupService()
		repo.createErr = errBD

		_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
			Origen:            domain.OrigenFolioPiso,
			Description:       "silla",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, errBD) {
			t.Fatalf("want errBD, got %v", err)
		}
	})

	t.Run("piso_rechaza_datos_de_cliente", func(t *testing.T) {
		t.Parallel()
		svc, repo, _, _, _, _, clk := setupService()
		cliente := 77

		_, err := svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
			Origen:            domain.OrigenFolioPiso,
			ClienteID:         &cliente,
			Description:       "silla",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		})
		if !errors.Is(err, domain.ErrClienteIDNoPermitido) {
			t.Fatalf("want ErrClienteIDNoPermitido, got %v", err)
		}
		if len(repo.created) != 0 {
			t.Errorf("created = %d, want 0", len(repo.created))
		}
	})
}
