// Package domain holds the config module's read-models and value objects.
// Everything here is pure (stdlib + uuid + apperror only) so it can be shared
// by ports and app without an import cycle, per the pattern established in
// internal/rutas/domain.
package domain

import (
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ErrVendedorListaIDInvalido is returned when a non-nil vendedor lista id is
// zero or negative. Callers must pass nil to mean "sin mapeo" for a slot —
// never the Microsip sentinel -1.
var ErrVendedorListaIDInvalido = apperror.NewValidation(
	"vendedor_lista_id_invalido",
	"el id de lista de vendedor es inválido",
)

// VendedorMapping is the value object persisted in MSP_CFG_VENDEDOR_MICROSIP:
// the three LISTAS_ATRIBUTOS.LISTA_ATRIB_ID values (one per credit-vendor
// attribute, 19985/19986/19987) mapped to an application usuario. A nil slot
// means "sin mapeo" — the DB stores SQL NULL, never the -1 sentinel; -1 is
// resolved only in the ventas read path (aplicar_config_repo.go).
type VendedorMapping struct {
	UsuarioID uuid.UUID
	ListaID1  *int
	ListaID2  *int
	ListaID3  *int
}

// NewVendedorMapping builds a VendedorMapping, validating that every non-nil
// slot is a positive id. Pass nil for a slot the admin left blank.
func NewVendedorMapping(usuarioID uuid.UUID, listaID1, listaID2, listaID3 *int) (VendedorMapping, error) {
	for _, id := range []*int{listaID1, listaID2, listaID3} {
		if id != nil && *id <= 0 {
			return VendedorMapping{}, ErrVendedorListaIDInvalido
		}
	}
	return VendedorMapping{
		UsuarioID: usuarioID,
		ListaID1:  listaID1,
		ListaID2:  listaID2,
		ListaID3:  listaID3,
	}, nil
}
