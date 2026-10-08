// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// AbrirGarantiaCmd contains the input to open a warranty folio.
//
// The two GPS pairs are different data (spec §3.1 and §3.3): GPSLat/GPSLon is
// where the agent was when saving, and it belongs to the folio_abierto event
// on any origin, piso included. DomicilioGPSLat/DomicilioGPSLon is where the
// client lives, it belongs to the folio's address, and the domain rejects it
// on piso folios along with the rest of the address.
type AbrirGarantiaCmd struct {
	Origen            domain.OrigenFolio
	ClienteID         *int
	VentaID           *int
	EstadoCuenta      *domain.EstadoCuenta
	Description       string
	VigenciaHasta     *time.Time
	Calle             string
	NumeroExterior    string
	Colonia           string
	Localidad         string
	Ciudad            string
	CodigoPostal      string
	GPSLat            *float64
	GPSLon            *float64
	DomicilioGPSLat   *float64
	DomicilioGPSLon   *float64
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
}

// AbrirGarantia opens a new warranty folio.
//
// It is the one command that cannot use ejecutarFolio: there is no folio to
// lock yet. It still runs the whole write inside one transaction — folio
// number, aggregate and pending events land together or not at all (§4.4) —
// and it treats a repeated key as the success it is (§3.3), returning the folio
// that key opened. A key counts as a replay only when the event it belongs to
// is folio_abierto; anything else means the key is being reused against the
// command, which is ErrClaveIdempotenciaDeOtroFolio.
func (s *Service) AbrirGarantia(ctx context.Context, cmd AbrirGarantiaCmd) (*domain.Garantia, error) {
	actor, err := s.actorDeApertura(ctx, cmd)
	if err != nil {
		return nil, err
	}
	// Normalized after identity and permission, so an unauthenticated or
	// unauthorized request fails with its own sentinel, before touching the
	// key. Once here, every lookup below uses the canonical spelling.
	clave, err := normalizarClave(cmd.ClaveIdempotencia)
	if err != nil {
		return nil, err
	}
	actor.ClaveIdempotencia = clave

	var creada *domain.Garantia
	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
		if err != nil {
			return err
		}
		if vista != nil {
			if vista.Tipo() != domain.TipoEventoFolioAbierto {
				return domain.ErrClaveIdempotenciaDeOtroFolio
			}
			return nil // replay: its folio is re-read below, not rebuilt
		}

		num, err := s.folios.Siguiente(ctx)
		if err != nil {
			return err
		}
		folio, err := domain.NewFolio(num)
		if err != nil {
			return err
		}
		abierta, err := domain.AbrirGarantia(domain.AbrirGarantiaParams{
			Folio:          folio,
			Origen:         cmd.Origen,
			ClienteID:      cmd.ClienteID,
			VentaID:        cmd.VentaID,
			EstadoCuenta:   cmd.EstadoCuenta,
			Description:    cmd.Description,
			VigenciaHasta:  cmd.VigenciaHasta,
			Calle:          cmd.Calle,
			NumeroExterior: cmd.NumeroExterior,
			Colonia:        cmd.Colonia,
			Localidad:      cmd.Localidad,
			Ciudad:         cmd.Ciudad,
			CodigoPostal:   cmd.CodigoPostal,
			GPSLat:         cmd.DomicilioGPSLat,
			GPSLon:         cmd.DomicilioGPSLon,
			AbiertoPor:     actor.Usuario,
			Now:            s.clock.Now(),
			Actor:          actor,
		})
		if err != nil {
			return err
		}
		if err := s.garantias.Crear(ctx, abierta); err != nil {
			if errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
				// Return the error so RunInTx rolls back the half-written
				// header (the racing twin wins; its folio is read below).
				return domain.ErrClaveIdempotenciaDuplicada
			}
			return err
		}
		creada = abierta
		return nil
	})
	if err != nil && !errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
		return nil, err
	}
	if creada == nil {
		// The write rolled back; whoever owns the key now wins. For AbrirGarantia
		// the key is a replay only when the event behind it is folio_abierto.
		return s.resolverDuplicada(ctx, actor.ClaveIdempotencia, func(v *domain.Evento) bool {
			return v.Tipo() == domain.TipoEventoFolioAbierto
		})
	}
	// Answer with the re-read folio, like every other command: an aggregate
	// still in memory and one read back from the store can differ in timestamp
	// precision, and the replay of this same command already answers with the
	// second shape.
	return s.garantias.Obtener(ctx, creada.ID())
}

// actorDeApertura resolves the user from Identity and checks the permiso. The
// user's name never comes from the command.
func (s *Service) actorDeApertura(ctx context.Context, cmd AbrirGarantiaCmd) (domain.ActorParams, error) {
	actor := actorDe(cmd.ClaveIdempotencia, cmd.DeviceCreatedAt, cmd.GPSLat, cmd.GPSLon)
	usuario, err := s.usuario(ctx)
	if err != nil {
		return domain.ActorParams{}, err
	}
	if err := s.exigirPermiso(ctx, domain.PermisoCrear); err != nil {
		return domain.ActorParams{}, err
	}
	actor.Usuario = usuario
	return actor, nil
}
