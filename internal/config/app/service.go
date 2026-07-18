// Package app implements the config module's Service: administration of the
// vendedor→Microsip mapping consumed by ventas when applying a crédito sale.
package app

import (
	"github.com/abdimuy/msp-api/internal/config/ports/outbound"
)

// Service is the config module's command+query surface.
type Service struct {
	repo              outbound.ConfigRepo
	catalogo          outbound.CatalogoReader
	usuarios          outbound.UsuariosReader
	zonaCajaCatalogo  outbound.ZonaCajaCatalogoLister
	zonaCajaExistente outbound.ZonaCajaCatalogoExistence
}

// NewService builds a Service wired against the given dependencies.
// zonaCajaCatalogo and zonaCajaExistente are split into their own port
// interfaces (see outbound package doc) so no single interface exceeds the
// project's interfacebloat limit; in production all three catalog params are
// typically satisfied by the same *configfb.ConfigRepo value.
func NewService(
	repo outbound.ConfigRepo,
	catalogo outbound.CatalogoReader,
	usuarios outbound.UsuariosReader,
	zonaCajaCatalogo outbound.ZonaCajaCatalogoLister,
	zonaCajaExistente outbound.ZonaCajaCatalogoExistence,
) *Service {
	return &Service{
		repo:              repo,
		catalogo:          catalogo,
		usuarios:          usuarios,
		zonaCajaCatalogo:  zonaCajaCatalogo,
		zonaCajaExistente: zonaCajaExistente,
	}
}
