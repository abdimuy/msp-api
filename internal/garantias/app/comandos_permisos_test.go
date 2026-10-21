// Package app_test contains tests for application commands.
package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// comandosConPermiso is every mutating command with the permission it must ask
// for, AbrirGarantia included: the table pins what each command asks, so a folio
// already stored is irrelevant here and the plain seed of the shared cases is
// enough for all of them.
func comandosConPermiso() []comandoCompartido {
	return append(comandosCompartidos(), comandoCompartido{
		nombre:  "AbrirGarantia",
		permiso: domain.PermisoCrear,
		tipo:    domain.TipoEventoFolioAbierto,
		invocar: func(svc *app.Service, _ *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
			return svc.AbrirGarantia(context.Background(), app.AbrirGarantiaCmd{
				Origen:            domain.OrigenFolioPiso,
				Description:       "mueble roto",
				ClaveIdempotencia: clave,
				DeviceCreatedAt:   now,
			})
		},
	})
}

// permisosDeSpec is the five codes of spec §7, the ones the composition root
// registers from garantias.Permisos().
func permisosDeSpec() []struct {
	nombre  string
	permiso domain.Permiso
} {
	return []struct {
		nombre  string
		permiso domain.Permiso
	}{
		{nombre: "leer", permiso: permLeer},
		{nombre: "crear", permiso: permCrear},
		{nombre: "actualizar", permiso: permActualizar},
		{nombre: "autorizar", permiso: permAutorizar},
		{nombre: "cerrar", permiso: permCerrar},
	}
}

// TestComandos_TablaDePermisos walks every command against every permission
// (command × permission, 14 × 5) and asserts both directions of the rule:
//
//   - denying the command's own permission stops it, before the transaction;
//   - denying any other code does not — a command asking for the wrong code is
//     caught either way (not asking for autorizar makes AutorizarCambioFisico
//     pass while autorizar is denied; asking for actualizar makes it fail when
//     actualizar is denied).
//
// That second half is what makes the table worth having: it pins the command's
// declared permission, not just that some permission is checked.
func TestComandos_TablaDePermisos(t *testing.T) {
	t.Parallel()
	for _, c := range comandosConPermiso() {
		for _, p := range permisosDeSpec() {
			t.Run(c.nombre+"/"+p.nombre, func(t *testing.T) {
				t.Parallel()
				svc, grepo, _, _, tx, id, clk, _ := setupService()
				denegado := p.permiso
				id.denegar = &denegado
				s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

				_, err := c.invocar(svc, s.g, s.primero(), clave(), clk.Now())
				denegadoErr := errors.Is(err, domain.ErrPermisoDenegado)

				if p.permiso == c.permiso {
					if !denegadoErr {
						t.Errorf("al negarle %s debe rechazar con ErrPermisoDenegado, got %v", p.nombre, err)
					}
					if !sinEscrituras(tx, grepo) {
						t.Errorf("sin permiso no escribe: tx=%d saved=%d created=%d",
							tx.calls, len(grepo.saved), len(grepo.created))
					}
					return
				}
				if denegadoErr {
					t.Errorf("pide %s y no %s: quedó bloqueado al negarle %s", c.permiso, p.nombre, p.nombre)
				}
			})
		}
	}
}
