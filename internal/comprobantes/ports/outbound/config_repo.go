package outbound

import (
	"context"
	"time"
)

// Config is the single row of MSP_CM_CONFIG.
type Config struct {
	VentanaCancelacion time.Duration
	VentasActivas      bool
	PagosActivos       bool
	MaxIntentos        int
}

// ConfigRepo reads and updates the module's windows and switches.
type ConfigRepo interface {
	Leer(ctx context.Context) (Config, error)
	Actualizar(ctx context.Context, c Config, now time.Time) error
}
