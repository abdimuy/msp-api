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
// permission it must ask for, the event type it must record, and how to build
// and run it.
//
// The state a command needs is not part of this table: every shared case
// resolves before the mutation (identity, permission or idempotency key), so a
// plain folio is enough. Each command's own state lives in its own test file.
//
// preparar exists because the idempotency fingerprint now hashes the whole
// command. A test that wants to replay a command must store the exact huella
// that command carries, and the only honest way to get it is to build the same
// command and hand it to huellaDe. Building it twice is fine as long as both
// copies are identical; the mismatch case builds a second one on purpose.
type comandoCompartido struct {
	nombre   string
	permiso  domain.Permiso
	tipo     domain.TipoEvento
	preparar func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any
	invocar  func(svc *app.Service, cmd any) (*domain.Garantia, error)
}

// ejecutar prepares and runs the command in one step, for the cases that do not
// need the intermediate command value.
func (c comandoCompartido) ejecutar(
	svc *app.Service,
	g *domain.Garantia,
	artID uuid.UUID,
	clave string,
	now time.Time,
) (*domain.Garantia, error) {
	return c.invocar(svc, c.preparar(g, artID, clave, now))
}

// huellaDe builds the command and asks app for its fingerprint. Tests use it to
// store the event a real first execution would have left behind.
func (c comandoCompartido) huellaDe(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) string {
	return app.HuellaDeParaPruebas(c.preparar(g, artID, clave, now))
}

// comandosCompartidos lists every mutating command except AbrirGarantia, which
// builds the folio itself and already carries its own identity and permission
// tests in abrir_garantia_test.go.
func comandosCompartidos() []comandoCompartido {
	return []comandoCompartido{
		{
			nombre:  "AgregarArticulo",
			permiso: domain.PermisoCrear,
			tipo:    domain.TipoEventoArticuloAgregado,
			preparar: func(g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) any {
				return app.AgregarArticuloCmd{
					GarantiaID:        g.ID(),
					Clave:             "B",
					Description:       "articulo B",
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.AgregarArticulo(context.Background(), cmd.(app.AgregarArticuloCmd))
			},
		},
		{
			nombre:  "AvanzarArticulo",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoEtapaAvanzada,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.AvanzarArticuloCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					EtapaDestino:      domain.EtapaEnRevision,
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.AvanzarArticulo(context.Background(), cmd.(app.AvanzarArticuloCmd))
			},
		},
		{
			nombre:  "IniciarProceso",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoFolioEnProceso,
			preparar: func(g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) any {
				return app.IniciarProcesoCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.IniciarProceso(context.Background(), cmd.(app.IniciarProcesoCmd))
			},
		},
		{
			nombre:  "RegistrarDiagnostico",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoDiagnosticoRegistrado,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.RegistrarDiagnosticoCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Ruta:              string(domain.RutaReparacionProveedor),
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.RegistrarDiagnostico(context.Background(), cmd.(app.RegistrarDiagnosticoCmd))
			},
		},
		{
			nombre:  "RegistrarDictamen",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoDictamenRegistrado,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.RegistrarDictamenCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Dictamen:          string(domain.DictamenSinFalla),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.RegistrarDictamen(context.Background(), cmd.(app.RegistrarDictamenCmd))
			},
		},
		{
			nombre:  "AutorizarCambioFisico",
			permiso: domain.PermisoAutorizar,
			tipo:    domain.TipoEventoCambioAutorizado,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.AutorizarCambioFisicoCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.AutorizarCambioFisico(context.Background(), cmd.(app.AutorizarCambioFisicoCmd))
			},
		},
		{
			nombre:  "MarcarNoDevuelto",
			permiso: domain.PermisoAutorizar,
			tipo:    domain.TipoEventoDevolucionFallida,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.MarcarNoDevueltoCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.MarcarNoDevuelto(context.Background(), cmd.(app.MarcarNoDevueltoCmd))
			},
		},
		{
			nombre:  "RegistrarDesenlace",
			permiso: domain.PermisoAutorizar,
			tipo:    domain.TipoEventoDesenlaceRegistrado,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.RegistrarDesenlaceCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Desenlace:         string(domain.DesenlaceSegundaMano),
					RolDecisor:        rolDecisorPrueba(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.RegistrarDesenlace(context.Background(), cmd.(app.RegistrarDesenlaceCmd))
			},
		},
		{
			nombre:  "MoverArticulo",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoArticuloMovido,
			preparar: func(g *domain.Garantia, artID uuid.UUID, clave string, now time.Time) any {
				return app.MoverArticuloCmd{
					GarantiaID:        g.ID(),
					ArticuloID:        artID,
					Ubicacion:         string(domain.UbicacionTaller),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.MoverArticulo(context.Background(), cmd.(app.MoverArticuloCmd))
			},
		},
		{
			nombre:  "MarcarListoEntrega",
			permiso: domain.PermisoActualizar,
			tipo:    domain.TipoEventoFolioListoEntrega,
			preparar: func(g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) any {
				return app.MarcarListoEntregaCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.MarcarListoEntrega(context.Background(), cmd.(app.MarcarListoEntregaCmd))
			},
		},
		{
			nombre:  "Entregar",
			permiso: domain.PermisoCerrar,
			tipo:    domain.TipoEventoFolioEntregado,
			preparar: func(g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) any {
				lat, lon := gpsPrueba()
				return app.EntregarCmd{
					GarantiaID:        g.ID(),
					Firma:             firmaPNGPrueba(),
					FirmaContentType:  "image/png",
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
					GPSLat:            lat,
					GPSLon:            lon,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.Entregar(context.Background(), cmd.(app.EntregarCmd))
			},
		},
		{
			nombre:  "Cerrar",
			permiso: domain.PermisoCerrar,
			tipo:    domain.TipoEventoFolioCerrado,
			preparar: func(g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) any {
				return app.CerrarCmd{
					GarantiaID:        g.ID(),
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.Cerrar(context.Background(), cmd.(app.CerrarCmd))
			},
		},
		{
			nombre:  "Cancelar",
			permiso: domain.PermisoCerrar,
			tipo:    domain.TipoEventoFolioCancelado,
			preparar: func(g *domain.Garantia, _ uuid.UUID, clave string, now time.Time) any {
				return app.CancelarCmd{
					GarantiaID:        g.ID(),
					Motivo:            "cliente no aparece",
					ClaveIdempotencia: clave,
					DeviceCreatedAt:   now,
				}
			},
			invocar: func(svc *app.Service, cmd any) (*domain.Garantia, error) {
				return svc.Cancelar(context.Background(), cmd.(app.CancelarCmd))
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

			_, err := c.ejecutar(svc, s.g, s.primero(), clave(), clk.Now())
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

			_, err := c.ejecutar(svc, s.g, s.primero(), clave(), clk.Now())
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

			_, err := c.ejecutar(svc, s.g, s.primero(), k, clk.Now())
			if !errors.Is(err, domain.ErrClaveIdempotenciaDeOtroFolio) {
				t.Fatalf("want ErrClaveIdempotenciaDeOtroFolio, got %v", err)
			}
			if !sinFilas(grepo) {
				t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_ClaveDeOtroTipo is the other face of the rule: the key may
// belong to this same folio but to a different operation. The folio is right
// and what the key describes is not, so the answer is the mismatch conflict —
// not "other folio".
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

			_, err := c.ejecutar(svc, s.g, s.primero(), k, clk.Now())
			if !errors.Is(err, domain.ErrClaveIdempotenciaMismatch) {
				t.Fatalf("want ErrClaveIdempotenciaMismatch, got %v", err)
			}
			if !sinFilas(grepo) {
				t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_ClaveSinHuella is the fail-closed rule for rows written before
// the full fingerprint existed: a key whose event has no huella can never be
// answered "already done", because there is no content to compare it with. It
// is a mismatch, not a replay.
func TestComandos_ClaveSinHuella(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, _, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			erepo.addEvento(hydrateEvento(s.g.ID(), k, c.tipo, clk.Now()))

			_, err := c.ejecutar(svc, s.g, s.primero(), k, clk.Now())
			if !errors.Is(err, domain.ErrClaveIdempotenciaMismatch) {
				t.Fatalf("want ErrClaveIdempotenciaMismatch, got %v", err)
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
// The stored event carries the exact fingerprint the command computes, so a
// command that hashed something different than the request it later compares
// (or that forgot to set ActorParams.Huella) can no longer replay. That is the
// defect this test was rebuilt for: the fingerprint is the whole request now,
// not just its event type.
func TestComandos_RepetidaPorClave(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, tx, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			h := c.huellaDe(s.g, s.primero(), k, clk.Now())
			erepo.addEvento(hydrateEventoConHuella(s.g.ID(), k, c.tipo, h, clk.Now()))

			res, err := c.ejecutar(svc, s.g, s.primero(), k, clk.Now())
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

// TestComandos_ClaveRepetidaConOtroContenido pins the content half of the rule:
// the same key on the right folio is only a replay when the request is the
// same. A second request with the same key and different content is a conflict,
// so the operation it asks for is not silently dropped.
func TestComandos_ClaveRepetidaConOtroContenido(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, _, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			original := c.preparar(s.g, s.primero(), k, clk.Now())
			// Same key and folio, one minute apart: the body differs, so the
			// fingerprint does too.
			otra := c.preparar(s.g, s.primero(), k, clk.Now().Add(time.Minute))
			erepo.addEvento(hydrateEventoConHuella(
				s.g.ID(), k, c.tipo, app.HuellaDeParaPruebas(otra), clk.Now()))

			_, err := c.invocar(svc, original)
			if !errors.Is(err, domain.ErrClaveIdempotenciaMismatch) {
				t.Fatalf("want ErrClaveIdempotenciaMismatch, got %v", err)
			}
			if !sinFilas(grepo) {
				t.Errorf("no debe escribir: saved=%d created=%d", len(grepo.saved), len(grepo.created))
			}
		})
	}
}

// TestComandos_CandadoAntesDeLaClave pins the order inside the transaction: the
// folio is locked (ObtenerParaActualizar) before the idempotency key is looked
// up (ObtenerPorClaveIdempotencia). The other way around, two simultaneous
// retries would both read "clave libre" and the second would race into Guardar
// instead of answering as the replay it is.
func TestComandos_CandadoAntesDeLaClave(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, _, _, _, _, clk, ord := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

			if _, err := c.ejecutar(svc, s.g, s.primero(), clave(), clk.Now()); err != nil {
				// El resultado no importa: ambos pasos corren antes de que el
				// comando pueda rechazar nada por su cuenta.
				t.Logf("%s rechazó: %v", c.nombre, err)
			}
			candado := primerPaso(ord, "obtener_para_actualizar")
			busqueda := primerPaso(ord, "buscar_clave")
			if candado < 0 || busqueda < 0 {
				t.Fatalf("faltan pasos: %v", ord.pasos())
			}
			if candado > busqueda {
				t.Errorf("la clave se buscó antes del candado: %v", ord.pasos())
			}
		})
	}
}

// TestComandos_UsuarioSaleDeIdentity proves the actor name is the one Identity
// returns ("Juan"), never something the command brought: the pending events are
// the only witness, and they are checked on the raw aggregate Guardar received.
func TestComandos_UsuarioSaleDeIdentity(t *testing.T) {
	t.Parallel()
	t.Run("AvanzarArticulo", func(t *testing.T) {
		t.Parallel()
		svc, grepo, _, _, _, _, clk, _ := setupService()
		s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaRegistrado)

		if _, err := svc.AvanzarArticulo(context.Background(), app.AvanzarArticuloCmd{
			GarantiaID:        s.g.ID(),
			ArticuloID:        s.primero(),
			EtapaDestino:      domain.EtapaEnRevision,
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		}); err != nil {
			t.Fatalf("AvanzarArticulo: %v", err)
		}
		usuarioEventos(t, grepo.lastSaved, "Juan")
	})
	t.Run("AgregarArticulo", func(t *testing.T) {
		t.Parallel()
		svc, grepo, _, _, _, _, clk, _ := setupService()
		s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)

		if _, err := svc.AgregarArticulo(context.Background(), app.AgregarArticuloCmd{
			GarantiaID:        s.g.ID(),
			Clave:             "B",
			Description:       "articulo B",
			ClaveIdempotencia: clave(),
			DeviceCreatedAt:   clk.Now(),
		}); err != nil {
			t.Fatalf("AgregarArticulo: %v", err)
		}
		usuarioEventos(t, grepo.lastSaved, "Juan")
	})
}
