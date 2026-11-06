package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

const evidenciaMaxBytes int64 = 10 * 1024 * 1024

// AdjuntarImagenCmd contiene los datos para adjuntar evidencia a un evento.
type AdjuntarImagenCmd struct {
	GarantiaID        uuid.UUID
	EventoID          uuid.UUID
	Description       string
	Contenido         []byte
	ClaveIdempotencia string
}

// AdjuntarImagen guarda una evidencia de forma idempotente.
func (s *Service) AdjuntarImagen(
	ctx context.Context,
	cmd AdjuntarImagenCmd,
) (*domain.Imagen, error) {
	usuario, err := s.usuario(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.exigirPermiso(ctx, domain.PermisoActualizar); err != nil {
		return nil, err
	}

	clave, err := normalizarClave(cmd.ClaveIdempotencia)
	if err != nil {
		return nil, err
	}
	cmd.ClaveIdempotencia = clave

	imagenID, err := uuid.Parse(clave)
	if err != nil {
		return nil, domain.ErrEventoClaveIdempotenciaInvalida
	}

	huella := huellaDe(cmd)

	ruta, err := s.guardarBlob(
		ctx,
		"garantias/"+cmd.GarantiaID.String(),
		cmd.Contenido,
		evidenciaMaxBytes,
	)
	if err != nil {
		return nil, err
	}

	defer s.descartarBlobSiNoQuedo(
		context.WithoutCancel(ctx),
		imagenID,
		ruta,
	)

	return s.registrarImagenAdjunta(
		ctx,
		cmd,
		imagenID,
		huella,
		ruta,
		usuario,
	)
}

func (s *Service) registrarImagenAdjunta(
	ctx context.Context,
	cmd AdjuntarImagenCmd,
	imagenID uuid.UUID,
	huella string,
	ruta string,
	usuario string,
) (*domain.Imagen, error) {
	var resultado *domain.Imagen

	err := s.tx.RunInTx(ctx, func(txCtx context.Context) error {
		if err := s.validarEventoAdjunto(
			txCtx,
			cmd.GarantiaID,
			cmd.EventoID,
		); err != nil {
			return err
		}

		existente, err := s.imagenes.Obtener(txCtx, imagenID)
		switch {
		case err == nil:
			if !imagenCoincideReintento(
				existente,
				cmd.EventoID,
				huella,
			) {
				return domain.ErrClaveIdempotenciaMismatch
			}

			resultado = existente
			return nil

		case errors.Is(err, domain.ErrImagenNoEncontrada):
			// La clave todavía no tiene imagen.

		default:
			return err
		}

		nueva, err := domain.NewImagen(domain.NewImagenParams{
			ID:          &imagenID,
			EventoID:    cmd.EventoID,
			Tipo:        domain.TipoImagenEvidencia,
			Ruta:        ruta,
			Description: cmd.Description,
			SubidaPor:   usuario,
			Huella:      huella,
			CreatedAt:   s.clock.Now(),
		})
		if err != nil {
			return err
		}

		if err := s.imagenes.Registrar(txCtx, nueva); err != nil {
			return err
		}

		resultado = nueva
		return nil
	})
	if err == nil {
		return resultado, nil
	}

	if !errors.Is(err, domain.ErrImagenDuplicada) {
		return nil, err
	}

	return s.resolverImagenDuplicada(
		ctx,
		imagenID,
		cmd.EventoID,
		huella,
	)
}

func (s *Service) validarEventoAdjunto(
	ctx context.Context,
	garantiaID uuid.UUID,
	eventoID uuid.UUID,
) error {
	eventos, err := s.eventos.ListarPorGarantia(ctx, garantiaID)
	if err != nil {
		return err
	}

	for _, evento := range eventos {
		if evento != nil && evento.ID() == eventoID {
			return nil
		}
	}

	return domain.ErrImagenEventoAjeno
}

func (s *Service) resolverImagenDuplicada(
	ctx context.Context,
	imagenID uuid.UUID,
	eventoID uuid.UUID,
	huella string,
) (*domain.Imagen, error) {
	existente, err := s.imagenes.Obtener(ctx, imagenID)
	if err != nil {
		return nil, err
	}

	if !imagenCoincideReintento(existente, eventoID, huella) {
		return nil, domain.ErrClaveIdempotenciaMismatch
	}

	return existente, nil
}

func imagenCoincideReintento(
	imagen *domain.Imagen,
	eventoID uuid.UUID,
	huella string,
) bool {
	return imagen != nil &&
		imagen.Tipo() == domain.TipoImagenEvidencia &&
		imagen.EventoID() == eventoID &&
		huella != "" &&
		imagen.Huella() == huella
}
