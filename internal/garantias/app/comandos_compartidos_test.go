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

// comandoCompartido is one mutating command as the shared cases see it: the
// permission it must ask for, the event type it must record, and how to run it.
//
// The state a command needs is not part of this table: every shared case
// resolves before the mutation (identity, permission or idempotency key), so a
// plain folio is enough. Each command's own state lives in its own test file.
type comandoCompartido struct {
	nombre  string
	permiso domain.Permiso
	tipo    domain.TipoEvento
	invocar func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error)
}

// comandosCompartidos lists every mutating command except AbrirGarantia, which
// builds the folio itself and already carries its own identity and permission
// tests in abrir_garantia_test.go.
//
// The tipo of each entry is the event the command's own test must find in the
// store: it is the value ejecutarFolio compares against when a resend arrives,
// so a command that passes a different one can never be replayed (mutant C6).
func comandosCompartidos() []comandoCompartido {
	return []comandoCompartido{
		{
			nombre:  "AgregarArticulo",
			permiso: domain.PermisoCrear,
			tipo:    domain.TipoEventoArticuloAgregado,
			invocar: func(svc *app.Service, g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
					GarantiaID:        g.ID(),
					Clave:             "B",
					Description:       "articulo B",
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "AvanzarArticulo",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoEtapaAvanzada,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					EtapaDestino:      domain.EtapaEnRevision,
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "IniciarProceso",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoFolioEnProceso,
			invocar: func(svc *app.Service, g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.IniciarProceso(context.Background(), app.IniciarProcesoCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "RegistrarDiagnostico",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoDiagnosticoRegistrado,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.RegistrarDiagnostico(context.Background(), app.RegistrarDiagnosticoCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Ruta:              string(domain.RutaReparacionProveedor),
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "RegistrarDictamen",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoDictamenRegistrado,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.RegistrarDictamen(context.Background(), app.RegistrarDictamenCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Dictamen:          string(domain.DictamenSinFalla),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "AutorizarCambioFisico",
			permiso: domain.PermisoAutorizar,
			tipo:    domain.TipoEventoCambioAutorizado,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.AutorizarCambioFisico(context.Background(), app.AutorizarCambioFisicoCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "MarcarNoDevuelto",
			permiso: domain.PermisoAutorizar,
			tipo:    domain.TipoEventoDevolucionFallida,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.MarcarNoDevuelto(context.Background(), app.MarcarNoDevueltoCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "RegistrarDesenlace",
			permiso: domain.PermisoAutorizar,
			tipo:    domain.TipoEventoDesenlaceRegistrado,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.RegistrarDesenlace(context.Background(), app.RegistrarDesenlaceCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Desenlace:         string(domain.DesenlaceSegundaMano),
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "MoverArticulo",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoArticuloMovido,
			invocar: func(svc *app.Service, g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.MoverArticulo(context.Background(), app.MoverArticuloCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Ubicacion:         string(domain.UbicacionTaller),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "MarcarListoEntrega",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoFolioListoEntrega,
			invocar: func(svc *app.Service, g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.MarcarListoEntrega(context.Background(), app.MarcarListoEntregaCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "Entregar",
			permiso: domain.PermisoCerrar,
			tipo:    domain.TipoEventoFolioEntregado,
			invocar: func(svc *app.Service, g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				lat, lon := gpsPrueba()
				return svc.Entregar(context.Background(), app.EntregarCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
					GPSLat:            lat,
					GPSLon:            lon,
				})
			},
		},
		{
			nombre:  "Cerrar",
			permiso: domain.PermisoCerrar,
			tipo:    domain.TipoEventoFolioCerrado,
			invocar: func(svc *app.Service, g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.Cerrar(context.Background(), app.CerrarCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
		{
			nombre:  "Cancelar",
			permiso: domain.PermisoCerrar,
			tipo:    domain.TipoEventoFolioCancelado,
			invocar: func(svc *app.Service, g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) (*domain.Garantia, error) {
				return svc.Cancelar(context.Background(), app.CancelarCmd{
					GarantiaID:        g.ID(),
					Motivo:            "cliente no aparece",
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				})
			},
		},
	}
}

// TestComandos_SinUsuario is the 401 of every mutating command: the caller is
// resolved from Identity and never from the command, and a phone that cannot
// authenticate writes nothing.
func TestComandos_SinUsuario(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, _, _, tx, id, clk, _ := setupService()
			id.errUser = domain.ErrUsuarioNoAutenticado
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

			_, err := c.invocar(svc, s.g, s.primero(), clave(), clk.Now())
			if !errors.Is(err, domain.ErrUsuarioNoAutenticado) {
				t.Fatalf("want ErrUsuarioNoAutenticado, got %v", err)
			}
			if !sinEscrituras(tx, grepo) {
				t.Errorf("no debe escribir: tx=%d saved=%d created=%d", tx.calls, len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_SinPermiso is the 403 of every mutating command: denying its own
// permission must stop it before the transaction, so the folio is not even
// locked for a request that is going to be refused.
func TestComandos_SinPermiso(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, _, _, tx, id, clk, _ := setupService()
			denegado := c.permiso
			id.denegar = &denegado
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

			_, err := c.invocar(svc, s.g, s.primero(), clave(), clk.Now())
			if !errors.Is(err, domain.ErrPermisoDenegado) {
				t.Fatalf("want ErrPermisoDenegado, got %v", err)
			}
			if !sinEscrituras(tx, grepo) {
				t.Errorf("no debe escribir: tx=%d saved=%d created=%d", tx.calls, len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_ClaveDeOtroFolio pins the foreign-key rule: a key already
// recorded against a different folio is refused as such, and nothing is staged
// — the rejection happens inside the transaction, after the lock, before any
// mutation.
func TestComandos_ClaveDeOtroFolio(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, _, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			erepo.addEvento(hydrateEvento(uuid.New(), k, c.tipo, clk.Now()))

			_, err := c.invocar(svc, s.g, s.primero(), k, clk.Now())
			if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
				t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
			}
			if !sinFilas(grepo) {
				t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_ClaveDeOtroTipo is the other face of the same rule: the key may
// belong to this folio but to a different command. Replaying Cerrar with the key
// Entregar already spent must not pass for an Entregar replay.
func TestComandos_ClaveDeOtroTipo(t *testing.T) {
	t.Parallel()
	otroTipo := domain.TipoEventoNota
	for _, c := range comandosCompartidos() {
		if c.tipo == otroTipo {
			continue
		}
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, _, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			erepo.addEvento(hydrateEvento(s.g.ID(), k, otroTipo, clk.Now()))

			_, err := c.invocar(svc, s.g, s.primero(), k, clk.Now())
			if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
				t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
			}
			if !sinFilas(grepo) {
				t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_RepetidaPorClave is the replay of each command under its own
// key: it succeeds, returns the folio and writes nothing at all.
//
// The registered event carries the tipo this table declares, so a command that
// hands a different one to ejecutarFolio fails here instead of replaying. That
// is what IniciarProceso did before this entrega (mutant C6): the domain wrote
// folio_en_proceso while the command asked the store for etapa_avanzada.
func TestComandos_RepetidaPorClave(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, tx, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			erepo.addEvento(hydrateEvento(s.g.ID(), k, c.tipo, clk.Now()))

			res, err := c.invocar(svc, s.g, s.primero(), k, clk.Now())
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
				t.Errorf("tx = %d, want 1: se abre, se mira la clave y se cierra sin mutar", tx.calls)
			}
		})
	}
}
