package app

import (
	"context"

	"github.com/google/uuid"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Microsip LISTAS_ATRIBUTOS.ATRIBUTO_ID values for the three credit-vendor
// free-list fields on LIBRES_CARGOS_CC (CARGOS_CC object). Mirrors the
// constants documented in configfb — duplicated here because app must not
// import infra (depguard app-no-infra).
const (
	atributoVendedor1 = 19985
	atributoVendedor2 = 19986
	atributoVendedor3 = 19987
)

// AsignarVendedor validates and upserts the vendedor→Microsip mapping for
// usuarioID. Each non-nil slot must belong to its matching attribute
// (l1→19985, l2→19986, l3→19987) — passing e.g. a VENDEDOR_2 id as slot 1
// returns a validation error. A usuarioID absent from MSP_USUARIOS surfaces
// as a NotFound (translated from the upsert's FK violation).
func (s *Service) AsignarVendedor(ctx context.Context, usuarioID uuid.UUID, listaID1, listaID2, listaID3 *int) error {
	mapping, err := configdomain.NewVendedorMapping(usuarioID, listaID1, listaID2, listaID3)
	if err != nil {
		return err
	}

	checks := []struct {
		id       *int
		atributo int
	}{
		{listaID1, atributoVendedor1},
		{listaID2, atributoVendedor2},
		{listaID3, atributoVendedor3},
	}
	for _, c := range checks {
		if c.id == nil {
			continue
		}
		pertenece, perr := s.catalogo.ListaIDPerteneceAtributo(ctx, *c.id, c.atributo)
		if perr != nil {
			return perr
		}
		if !pertenece {
			return errVendedorListaIDNoPertenece
		}
	}

	if err := s.repo.UpsertVendedorMapping(ctx, mapping); err != nil {
		if appErr, ok := apperror.As(err); ok && appErr.Code == "firebird_fk_violation" {
			return errUsuarioNoExiste
		}
		return err
	}
	return nil
}

// EliminarVendedor removes the vendedor→Microsip mapping for usuarioID, if
// any. Deleting a mapping that does not exist is a no-op (idempotent).
func (s *Service) EliminarVendedor(ctx context.Context, usuarioID uuid.UUID) error {
	return s.repo.DeleteVendedorMapping(ctx, usuarioID)
}
