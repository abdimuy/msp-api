// Package app contains the use cases of the garantias module.
package app

import (
	"context"
	"errors"
	"time"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// AbrirGarantiaCmd contains the input to open a warranty folio.
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
	ClaveIdempotencia string
	DeviceCreatedAt   time.Time
}

// AbrirGarantia opens a new warranty folio.
//
// It is the one command that cannot use ejecutarFolio: there is no folio to
// lock yet. It still runs the whole write inside one transaction — folio
// number, aggregate and pending events land together or not at all (§4.4) —
// and it treats a repeated key as the success it is (§3.3), returning the folio
// that key opened.
func (s *Service) AbrirGarantia(ctx context.Context, cmd AbrirGarantiaCmd) (*domain.Garantia, error) {
	actor, err := s.actorDeApertura(ctx, cmd)
	if err != nil {
		return nil, err
	}

	var creada *domain.Garantia
	err = s.tx.RunInTx(ctx, func(ctx context.Context) error {
		vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
		if err != nil {
			return err
		}
		if vista != nil {
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
			GPSLat:         cmd.GPSLat,
			GPSLon:         cmd.GPSLon,
			AbiertoPor:     actor.Usuario,
			Now:            s.clock.Now(),
			Actor:          actor,
		})
		if err != nil {
			return err
		}
		if err := s.garantias.Crear(ctx, abierta); err != nil {
			if errors.Is(err, domain.ErrClaveIdempotenciaDuplicada) {
				return nil // the racing twin wins; read its folio below
			}
			return err
		}
		creada = abierta
		return nil
	})
	if err != nil {
		return nil, err
	}
	if creada != nil {
		return creada, nil
	}

	vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
	if err != nil {
		return nil, err
	}
	if vista == nil {
		return nil, domain.ErrClaveIdempotenciaDuplicada
	}
	return s.garantias.Obtener(ctx, vista.GarantiaID())
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
