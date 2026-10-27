package garfb_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/infra/garfb"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Contrato de las tareas 9 y 10: Obtener y ListarPorEventos existen para que
// el puerto compile, pero los implementa la tarea 10. Mientras tanto deben
// fallar como error interno, nunca responder "sin imágenes". La tarea 10
// BORRA este archivo al implementarlos.
func TestImagenRepo_MetodosDelContratoPendientes(t *testing.T) {
	t.Parallel()
	repo := garfb.NewImagenRepo(nil)

	img, err := repo.Obtener(context.Background(), uuid.New())
	var ae *apperror.Error
	if img != nil || !errors.As(err, &ae) || ae.Kind != apperror.KindInternal {
		t.Errorf("Obtener = %v, %v; want error interno", img, err)
	}

	imgs, err := repo.ListarPorEventos(context.Background(), []uuid.UUID{uuid.New()})
	if imgs != nil || !errors.As(err, &ae) || ae.Kind != apperror.KindInternal {
		t.Errorf("ListarPorEventos = %v, %v; want error interno", imgs, err)
	}
}
