// Package app implements the config module's Service: administration of the
// vendedor→Microsip mapping consumed by ventas when applying a crédito sale.
package app

import (
	"github.com/abdimuy/msp-api/internal/config/ports/outbound"
)

// Service is the config module's command+query surface.
type Service struct {
	repo     outbound.ConfigRepo
	catalogo outbound.CatalogoReader
	usuarios outbound.UsuariosReader
}

// NewService builds a Service wired against the given dependencies.
func NewService(
	repo outbound.ConfigRepo,
	catalogo outbound.CatalogoReader,
	usuarios outbound.UsuariosReader,
) *Service {
	return &Service{repo: repo, catalogo: catalogo, usuarios: usuarios}
}
