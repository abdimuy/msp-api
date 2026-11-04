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

	var resultado *domain.Imagen

	err = s.tx.RunInTx(ctx, func(txCtx context.Context) error {
		eventos, err := s.eventos.ListarPorGarantia(txCtx, cmd.GarantiaID)
		if err != nil {
			return err
		}

		eventoExiste := false
		for _, evento := range eventos {
			if evento != nil && evento.ID() == cmd.EventoID {
				eventoExiste = true
				break
			}
		}

		if !eventoExiste {
			return domain.ErrImagenEventoAjeno
		}

		existente, err := s.imagenes.Obtener(txCtx, imagenID)
		switch {
		case err == nil:
			if !imagenCoincideReintento(existente, cmd.EventoID, huella) {
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
	if err != nil {
		if !errors.Is(err, domain.ErrImagenDuplicada) {
			return nil, err
		}

		// Otra petición concurrente pudo ganar con la misma clave.
		existente, lookupErr := s.imagenes.Obtener(ctx, imagenID)
		if lookupErr != nil {
			return nil, lookupErr
		}

		if !imagenCoincideReintento(existente, cmd.EventoID, huella) {
			return nil, domain.ErrClaveIdempotenciaMismatch
		}

		return existente, nil
	}

	return resultado, nil
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
