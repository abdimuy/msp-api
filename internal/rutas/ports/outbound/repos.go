// Package outbound defines the interfaces the rutas module needs from outside.
//
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package outbound

import (
	"context"

	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// RutasRepo is the read port for the rutas module.
type RutasRepo interface {
	ListarRutas(ctx context.Context) ([]rutasdomain.RutaResumen, error)
}
