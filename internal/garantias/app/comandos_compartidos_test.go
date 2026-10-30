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
// permission it must ask for, the event type it must record, the idempotency
// huella it declares, and how to run it.
//
// The state a command needs is not part of this table: every shared case
// resolves before the mutation (identity, permission or idempotency key), so a
// plain folio is enough. Each command's own state lives in its own test file.
type comandoCompartido struct {
	nombre  string
	permiso domain.Permiso
	tipo    domain.TipoEvento
	// huella builds the event the command expects to meet behind its key on a
	// retry. It mirrors the huella the command hands ejecutarFolio: tipo alone
	// for folio commands, plus articuloRef — and etapaHasta for AvanzarArticulo
	// — for the ones that name them. The shared test seeds exactly this so a
	// command that declares a different huella than the one it writes fails
	// instead of replaying (mutant C6).
	huella  func(garantiaID uuid.UUID, clave string, artID uuid.UUID, now time.Time) *domain.Evento
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
			huella:  huellaDeFolio(domain.TipoEventoArticuloAgregado),
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
			huella:  huellaDeEtapa(domain.TipoEventoEtapaAvanzada, domain.EtapaEnRevision),
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
			huella:  huellaDeFolio(domain.TipoEventoFolioEnProceso),
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
			huella:  huellaDeArticulo(domain.TipoEventoDiagnosticoRegistrado),
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
			huella:  huellaDeArticulo(domain.TipoEventoDictamenRegistrado),
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
			huella:  huellaDeArticulo(domain.TipoEventoCambioAutorizado),
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
			huella:  huellaDeArticulo(domain.TipoEventoDevolucionFallida),
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
			huella:  huellaDeArticulo(domain.TipoEventoDesenlaceRegistrado),
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
			huella:  huellaDeArticulo(domain.TipoEventoArticuloMovido),
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
			huella:  huellaDeFolio(domain.TipoEventoFolioListoEntrega),
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
			huella:  huellaDeFolio(domain.TipoEventoFolioEntregado),
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
			huella:  huellaDeFolio(domain.TipoEventoFolioCerrado),
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
			huella:  huellaDeFolio(domain.TipoEventoFolioCancelado),
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

// TestComandos_ClaveDeOtroTipo is the other face of the rule, now under the
// content semantics of the 20-oct review: the key may belong to this same folio
// but to a different operation. The folio is right and what the key describes
// is not, so the answer is the mismatch conflict — not "other folio".
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
// The stored event is the one the command's own huella describes — its tipo,
// the articuloRef when it names an article, and the arrival stage for
// AvanzarArticulo — so a command that hands ejecutarFolio a different huella
// than the one it writes can never replay. That is what IniciarProceso did
// before this entrega (mutant C6): the domain wrote folio_en_proceso while the
// command asked the store for etapa_avanzada.
func TestComandos_RepetidaPorClave(t *testing.T) {
	t.Parallel()
	for _, c := range comandosCompartidos() {
		t.Run(c.nombre, func(t *testing.T) {
			t.Parallel()
			svc, grepo, erepo, _, tx, _, clk, _ := setupService()
			s := sembrar(t, grepo, domain.EstadoFolioEnProceso, piso(), domain.EtapaEnRevision)
			k := clave()
			erepo.addEvento(c.huella(s.g.ID(), k, s.primero(), clk.Now()))

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

			if _, err := c.invocar(svc, s.g, s.primero(), clave(), clk.Now()); err != nil {
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
