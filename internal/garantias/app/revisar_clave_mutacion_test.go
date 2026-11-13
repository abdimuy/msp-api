package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

func TestRevisarClave_MutacionesPendientes(t *testing.T) {
	t.Parallel()
	garantiaID := uuid.New()
	otroFolio := uuid.New()
	huella := strings.Repeat("a", 64)
	otraHuella := strings.Repeat("b", 64)
	ahora := time.Now().UTC()

	evento := func(folio uuid.UUID, fingerprint string) *domain.Evento {
		return domain.HydrateEvento(domain.HydrateEventoParams{
			ID:              uuid.New(),
			GarantiaID:      folio,
			Tipo:            domain.TipoEventoFolioEntregado,
			Usuario:         "Agente",
			CreatedAt:       ahora,
			DeviceCreatedAt: ahora,
			Huella:          fingerprint,
		})
	}

	casos := []struct {
		nombre       string
		existente    *domain.Evento
		folio        uuid.UUID
		huella       string
		repetido     bool
		errorDeseado error
	}{
		{
			nombre: "clave no registrada",
			folio:  garantiaID,
			huella: huella,
		},
		{
			nombre:    "mismo folio y contenido",
			existente: evento(garantiaID, huella),
			folio:     garantiaID,
			huella:    huella,
			repetido:  true,
		},
		{
			nombre:       "clave perteneciente a otro folio",
			existente:    evento(otroFolio, huella),
			folio:        garantiaID,
			huella:       huella,
			errorDeseado: domain.ErrClaveIdempotenciaDeOtroFolio,
		},
		{
			nombre:       "mismo folio con contenido diferente",
			existente:    evento(garantiaID, huella),
			folio:        garantiaID,
			huella:       otraHuella,
			errorDeseado: domain.ErrClaveIdempotenciaMismatch,
		},
		{
			nombre:       "huella vacia en solicitud",
			existente:    evento(garantiaID, huella),
			folio:        garantiaID,
			huella:       "",
			errorDeseado: domain.ErrClaveIdempotenciaMismatch,
		},
		{
			nombre:       "huella vacia almacenada",
			existente:    evento(garantiaID, ""),
			folio:        garantiaID,
			huella:       huella,
			errorDeseado: domain.ErrClaveIdempotenciaMismatch,
		},
		{
			nombre:    "folio no indicado y misma huella",
			existente: evento(otroFolio, huella),
			folio:     uuid.Nil,
			huella:    huella,
			repetido:  true,
		},
		{
			nombre:       "folio no indicado y diferente huella",
			existente:    evento(otroFolio, huella),
			folio:        uuid.Nil,
			huella:       otraHuella,
			errorDeseado: domain.ErrClaveIdempotenciaMismatch,
		},
	}

	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			t.Parallel()
			repetido, err := revisarClave(
				caso.existente,
				caso.folio,
				caso.huella,
			)

			if !errors.Is(err, caso.errorDeseado) {
				t.Fatalf(
					"error = %v, esperado = %v",
					err,
					caso.errorDeseado,
				)
			}

			if repetido != caso.repetido {
				t.Fatalf(
					"repetido = %t, esperado = %t",
					repetido,
					caso.repetido,
				)
			}
		})
	}
}
