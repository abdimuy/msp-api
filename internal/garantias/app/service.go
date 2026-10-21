// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

// Deps holds the outbound dependencies of the garantias application service.
type Deps struct {
	GarantiaRepo   outbound.GarantiaRepo
	EventoRepo     outbound.EventoRepo
	FolioGenerator outbound.FolioGenerator
	TxRunner       outbound.TxRunner
	Identity       outbound.Identity
	Clock          outbound.Clock
}

// Service is the application service for garantias commands.
//
// An aggregate is saved once: a command loads it, mutates it, saves it and
// drops it. The repository does not empty eventosPendientes after saving and
// no command reuses an aggregate that was already saved.
type Service struct {
	garantias outbound.GarantiaRepo
	eventos   outbound.EventoRepo
	folios    outbound.FolioGenerator
	tx        outbound.TxRunner
	identity  outbound.Identity
	clock     outbound.Clock
}

// NewService creates a new Service.
func NewService(deps Deps) *Service {
	return &Service{
		garantias: deps.GarantiaRepo,
		eventos:   deps.EventoRepo,
		folios:    deps.FolioGenerator,
		tx:        deps.TxRunner,
		identity:  deps.Identity,
		clock:     deps.Clock,
	}
}

// operacion is what a command does to the folio once it is locked and the
// idempotency key is clear. Every mutating command is this plus the identity
// and permission checks around it, which is why the five steps live in one
// helper instead of being copied per command.
type operacion func(ctx context.Context, g *domain.Garantia, actor domain.ActorParams) error

// huellaEvento is what a command expects to find behind its idempotency key.
// The plain "same folio" rule the first revision used was too loose: a key
// reused on this folio with a different command, or with the same command and
// different content, answered "already done" and dropped the second
// operation (review of the B, 20-oct). These three fields are what separates
// a true replay from a reuse: the event type identifies the command, and for
// the ones that name an article or an arrival stage, so do articuloRef and
// etapaHasta. A command that carries neither leaves them nil.
type huellaEvento struct {
	tipo        domain.TipoEvento
	articuloRef *uuid.UUID
	etapaHasta  *domain.Etapa
}

// ejecutarFolio applies the five steps every mutating command shares:
//
//  1. resolve the user from Identity — never from the command
//  2. check the command's permission
//  3. inside a transaction: lock the folio first, then reject a replayed or
//     foreign idempotency key, mutate it, save it
//  4. a duplicate key surfacing from Guardar is a rollback, not a commit: the
//     write already placed the folio rows but failed on the event UNIQUE, and
//     committing them would persist a change without its event (§4.4)
//  5. after the rollback, re-read who owns the key — the twin won, so the
//     command answers with the current state — and re-read the folio outside
//     the transaction to return it
//
// tipo is the event this command records. The lock must come before the key
// lookup (3 before a): two requests to the same folio then serialize, the
// second one sees the first one's event and answers as a replay. That serial
// behavior is not a performance detail — it is what the phone receives: the
// simultaneous retry must be a success, not a stage_transition_forbidden over
// an article that actually saved.
func (s *Service) ejecutarFolio(
	ctx context.Context,
	garantiaID uuid.UUID,
	permiso domain.Permiso,
	actor domain.ActorParams,
	huella huellaEvento,
	mutar operacion,
) (*domain.Garantia, error) {
	usuario, err := s.usuario(ctx)
	if err != nil {
		return nil, err
	}
	actor.Usuario = usuario
	if err := s.exigirPermiso(ctx, permiso); err != nil {
		return nil, err
	}
	clave, err := normalizarClave(actor.ClaveIdempotencia)
	if err != nil {
		return nil, err
	}
	actor.ClaveIdempotencia = clave

	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		// Lock the folio before looking at the key. With the lock held, two
		// requests to the same folio serialize, so the second one does see the
		// event the first one wrote and the cheap replay path below triggers
		// instead of racing into Guardar.
		g, err := s.garantias.ObtenerParaActualizar(ctx, garantiaID)
		if err != nil {
			return err
		}
		vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
		if err != nil {
			return err
		}
		repite, err := revisarClave(vista, garantiaID, huella)
		if err != nil {
			return err
		}
		if repite {
			// Replay of this exact command: succeed with the current state
			// and write nothing.
			return nil
		}

		if err := mutar(ctx, g, actor); err != nil {
			return err
		}
		if err := s.garantias.Guardar(ctx, g); err != nil {
			if errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
				// Return the error so RunInTx rolls back the half-written
				// folio; the twin's key is resolved again right below.
				return domain.ErrClaveIdempotenciaDuplicada
			}
			return err
		}
		return nil
	})
	if err != nil {
		if !errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
			return nil, err
		}
		return s.resolverDuplicada(ctx, actor.ClaveIdempotencia, garantiaID, huella)
	}
	return s.garantias.Obtener(ctx, garantiaID)
}

// revisarClave decides what an already-registered idempotency key means for
// this command, inside the transaction and after the folio is locked. Absent:
// the command proceeds. Present on this same folio and describing the same
// operation: a replay, which succeeds writing nothing. Present on this same
// folio but describing something else: the key is a repeat, its content is
// not, and answering "already done" would drop the operation being asked for
// now. Any other owner is a key reused against the wrong folio.
func revisarClave(vista *domain.Evento, garantiaID uuid.UUID, huella huellaEvento) (bool, error) {
	switch {
	case vista == nil:
		return false, nil
	case vista.GarantiaID() != garantiaID:
		return false, domain.ErrClaveIdempotenciaDeOtroFolio
	case coincideHuella(vista, huella):
		return true, nil
	default:
		return false, domain.ErrClaveIdempotenciaMismatch
	}
}

// coincideHuella reports whether a stored event describes the same operation
// as the huella. articuloRef and etapaHasta are compared only where the
// command declared one: a nil expected value means the command does not carry
// that field, not that it expects nil, so those commands judge on the type
// alone.
func coincideHuella(vista *domain.Evento, huella huellaEvento) bool {
	if vista.Tipo() != huella.tipo {
		return false
	}
	if huella.articuloRef != nil {
		if vista.ArticuloRef() == nil || *vista.ArticuloRef() != *huella.articuloRef {
			return false
		}
	}
	if huella.etapaHasta != nil {
		if vista.EtapaHasta() == nil || *vista.EtapaHasta() != *huella.etapaHasta {
			return false
		}
	}
	return true
}

// huellaApertura is the huella of AbrirGarantia: the only command that
// creates the folio, so it has no article or stage to name.
func huellaApertura() huellaEvento {
	return huellaEvento{tipo: domain.TipoEventoFolioAbierto}
}

// resolverDuplicada runs after the rollback caused by a duplicate key: the
// twin's event owns it now. An absent key means there was no twin and the
// write is simply lost; an event on another folio means the key was reused
// against the wrong folio; an event on this folio describing another
// operation is the content mismatch the locked path already rejects, and it
// must keep failing here too. Otherwise the folio behind the event is re-read
// and returned.
//
// garantiaID is uuid.Nil for AbrirGarantia, which has no folio to name: the
// key is the only thing that can identify the twin, so the folio check is
// skipped and the huella decides.
func (s *Service) resolverDuplicada(
	ctx context.Context,
	clave string,
	garantiaID uuid.UUID,
	huella huellaEvento,
) (*domain.Garantia, error) {
	vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, clave)
	if err != nil {
		return nil, err
	}
	switch {
	case vista == nil:
		return nil, domain.ErrClaveIdempotenciaDuplicada
	case garantiaID != uuid.Nil && vista.GarantiaID() != garantiaID:
		return nil, domain.ErrClaveIdempotenciaDeOtroFolio
	case !coincideHuella(vista, huella):
		return nil, domain.ErrClaveIdempotenciaMismatch
	}
	return s.garantias.Obtener(ctx, vista.GarantiaID())
}

// normalizarClave canonicalizes the idempotency key before any lookup. The
// domain stores the canonical form (parsed.String(), ronda 3 of #24): a
// search done with the raw spelling would miss a replay sent in uppercase or
// with braces and let it race into the UNIQUE index again.
func normalizarClave(clave string) (string, error) {
	clave = strings.TrimSpace(clave)
	if clave == "" {
		return "", domain.ErrEventoClaveIdempotenciaObligatoria
	}
	parsed, err := uuid.Parse(clave)
	if err != nil {
		return "", domain.ErrEventoClaveIdempotenciaInvalida
	}
	return parsed.String(), nil
}

// usuario resolves the caller from Identity. The value never comes from the
// command: if it did, a phone could sign as somebody else.
func (s *Service) usuario(ctx context.Context) (string, error) {
	u, err := s.identity.UsuarioActual(ctx)
	if err != nil {
		return "", err
	}
	return u.Nombre, nil
}

// exigirPermiso enforces the permission in app, not in http: the command knows
// which action is being asked for, the handler only knows the route.
func (s *Service) exigirPermiso(ctx context.Context, p domain.Permiso) error {
	ok, err := s.identity.TienePermiso(ctx, p)
	if err != nil {
		return err
	}
	if !ok {
		return domain.ErrPermisoDenegado
	}
	return nil
}

// actorDe builds the ActorParams of a command. Usuario is filled in by
// ejecutarFolio from Identity; the rest (key, device time, GPS) comes from the
// command because it describes what the device reported, not who is calling.
func actorDe(clave string, deviceCreatedAt time.Time, lat, lon *float64) domain.ActorParams {
	return domain.ActorParams{
		ClaveIdempotencia: clave,
		DeviceCreatedAt:   deviceCreatedAt,
		GPSLat:            lat,
		GPSLon:            lon,
	}
}

// actorDeConRol is actorDe plus the decider role a decision command carries.
// The role travels in the command because it says in which capacity the
// operator is deciding, not who they are.
func actorDeConRol(clave string, deviceCreatedAt time.Time, lat, lon *float64, rol *domain.RolDecisor) domain.ActorParams {
	actor := actorDe(clave, deviceCreatedAt, lat, lon)
	actor.RolDecisor = rol
	return actor
}

// decisionCmd is the envelope of a decision command after its own input has
// been parsed. It keeps the shared plumbing of those commands — role, key,
// device stamp, GPS — in one type so each command reads as what it is: a parse,
// a permission, an event type, and the aggregate method it calls.
type decisionCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	RolDecisor        string
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// nuevoDecisionCmd fills the shared envelope of a decision command from its
// fields. Both RegistrarDiagnostico and RegistrarDesenlace build it the same
// way; the constructor keeps that one line in one place.
func nuevoDecisionCmd(
	garantiaID, articuloID uuid.UUID,
	rolDecisor, clave string,
	deviceCreatedAt time.Time,
	lat, lon *float64,
) decisionCmd {
	return decisionCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		RolDecisor:        rolDecisor,
		ClaveIdempotencia: clave,
		DeviceCreatedAt:   deviceCreatedAt,
		GPSLat:            lat,
		GPSLon:            lon,
	}
}

// ejecutarDecision parses the decider role, then runs the aggregate method
// inside the folio transaction. The role is parsed here, before the transaction
// opens: an unknown role fails with ErrRolDecisorInvalido, and an empty one
// travels as nil so the domain raises ErrRolDecisorObligatorio.
//
// Every decision command names the article it decides on and none names an
// arrival stage, so the huella is built here — type plus articuloRef — instead of
// each command repeating the same literal.
func (s *Service) ejecutarDecision(
	ctx context.Context,
	cmd decisionCmd,
	permiso domain.Permiso,
	tipo domain.TipoEvento,
	ejecutar func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error,
) (*domain.Garantia, error) {
	rol, err := rolDecisorPtr(cmd.RolDecisor)
	if err != nil {
		return nil, err
	}
	return s.ejecutarFolio(ctx, cmd.GarantiaID, permiso,
		actorDeConRol(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon, rol),
		huellaEvento{tipo: tipo, articuloRef: &cmd.ArticuloID},
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return ejecutar(g, actor, s.clock.Now())
		})
}

// articuloCmd is the envelope of an article-scoped command that is not a
// decision: it names the article, the key, the device stamp and the GPS, but
// carries no decider role (the domain decides who may run it from the stage and
// the route).
type articuloCmd struct {
	GarantiaID        uuid.UUID
	ArticuloID        uuid.UUID
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
	GPSLat            *float64
	GPSLon            *float64
}

// ejecutarArticulo runs an article-scoped command that is not a decision. The
// huella is built here — type plus articuloRef — so each command reads as what
// it is: a parse, a permission, an event type and the aggregate method.
func (s *Service) ejecutarArticulo(
	ctx context.Context,
	cmd articuloCmd,
	permiso domain.Permiso,
	tipo domain.TipoEvento,
	ejecutar func(g *domain.Garantia, actor domain.ActorParams, ahora time.Time) error,
) (*domain.Garantia, error) {
	return s.ejecutarFolio(ctx, cmd.GarantiaID, permiso,
		actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon),
		huellaEvento{tipo: tipo, articuloRef: &cmd.ArticuloID},
		func(_ context.Context, g *domain.Garantia, actor domain.ActorParams) error {
			return ejecutar(g, actor, s.clock.Now())
		})
}

// rolDecisorPtr parses the decider role of a command. An empty value means the
// caller did not send one: it yields nil so the domain raises
// ErrRolDecisorObligatorio, while a non-empty but unknown value fails here with
// the domain's ErrRolDecisorInvalido.
func rolDecisorPtr(rol string) (*domain.RolDecisor, error) {
	rol = strings.TrimSpace(rol)
	if rol == "" {
		//nolint:nilnil // an empty role is "the caller sent none", not an error: the domain decides
		return nil, nil
	}
	parsed, err := domain.ParseRolDecisor(rol)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
