package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// newEvento is unexported and no public mutator sets AuditoriaGPS yet (that is
// Cerrar, tarea 9). This internal test pins the guard so the contract cannot
// let an unknown audit value reach the AUDITORIA_GPS column.
func TestNewEvento_AuditoriaGPSInvalida(t *testing.T) {
	t.Parallel()
	ahora := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	base := EventoParams{
		GarantiaID:        uuid.New(),
		Tipo:              TipoEventoFolioCerrado,
		Usuario:           "juan",
		CreatedAt:         ahora,
		DeviceCreatedAt:   ahora,
		ClaveIdempotencia: uuid.NewString(),
	}
	mala := AuditoriaGPS("quiza")
	p := base
	p.AuditoriaGPS = &mala
	if e, err := newEvento(p); !errors.Is(err, ErrAuditoriaGPSInvalida) || e != nil {
		t.Fatalf("newEvento(auditoría inválida) = %v, %v; want ErrAuditoriaGPSInvalida", e, err)
	}
	buena := AuditoriaGPSNoAplica
	p.AuditoriaGPS = &buena
	e, err := newEvento(p)
	if err != nil {
		t.Fatalf("newEvento(no_aplica): %v", err)
	}
	if e.AuditoriaGPS() == nil || *e.AuditoriaGPS() != AuditoriaGPSNoAplica {
		t.Errorf("AuditoriaGPS() = %v, want no_aplica", e.AuditoriaGPS())
	}
}
