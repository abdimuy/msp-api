package app

import (
	"context"
	"sort"

	"github.com/google/uuid"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// nombreDesconocido stands in for a lista id whose VALOR_DESPLEGADO could not
// be resolved (e.g. the Microsip catalog row was deleted after the mapping
// was saved). The slot is still shown — with its id — so an admin can spot
// and fix the drift instead of losing the assignment silently.
const nombreDesconocido = "(desconocido)"

// ListarVendedores returns one VendedorAsignacion per application usuario,
// LEFT-JOINed against MSP_CFG_VENDEDOR_MICROSIP: usuarios without a mapping
// row get all three slots nil ("sin asignar"). Ordered by Nombre.
func (s *Service) ListarVendedores(ctx context.Context) ([]configdomain.VendedorAsignacion, error) {
	usuarios, err := s.usuarios.ListarUsuarios(ctx)
	if err != nil {
		return nil, err
	}

	mappings, err := s.repo.ListarVendedorMappings(ctx)
	if err != nil {
		return nil, err
	}
	byUsuario := make(map[uuid.UUID]configdomain.VendedorMapping, len(mappings))
	idSet := make(map[int]struct{})
	for _, m := range mappings {
		byUsuario[m.UsuarioID] = m
		for _, id := range []*int{m.ListaID1, m.ListaID2, m.ListaID3} {
			if id != nil {
				idSet[*id] = struct{}{}
			}
		}
	}

	var nombres map[int]string
	if len(idSet) > 0 {
		ids := make([]int, 0, len(idSet))
		for id := range idSet {
			ids = append(ids, id)
		}
		nombres, err = s.catalogo.ResolverNombresLista(ctx, ids)
		if err != nil {
			return nil, err
		}
	}

	sorted := make([]configdomain.AppUsuario, len(usuarios))
	copy(sorted, usuarios)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Nombre < sorted[j].Nombre })

	result := make([]configdomain.VendedorAsignacion, 0, len(sorted))
	for _, u := range sorted {
		m, ok := byUsuario[u.ID]
		var v1, v2, v3 *configdomain.VendedorSlot
		if ok {
			v1 = buildSlot(m.ListaID1, nombres)
			v2 = buildSlot(m.ListaID2, nombres)
			v3 = buildSlot(m.ListaID3, nombres)
		}
		result = append(result, configdomain.NewVendedorAsignacion(u.ID, u.Nombre, u.Email, v1, v2, v3))
	}
	return result, nil
}

// buildSlot resolves a mapped lista id into a VendedorSlot, falling back to
// nombreDesconocido when the id is missing from the resolved name map.
func buildSlot(id *int, nombres map[int]string) *configdomain.VendedorSlot {
	if id == nil {
		return nil
	}
	nombre, ok := nombres[*id]
	if !ok {
		nombre = nombreDesconocido
	}
	return &configdomain.VendedorSlot{ListaID: *id, Nombre: nombre}
}

// ListarIdentidadesMicrosip returns every Microsip credit-vendor identity
// (one row per VALOR_DESPLEGADO across the three attributes), used to
// populate the admin's picker when assigning a VendedorMapping.
func (s *Service) ListarIdentidadesMicrosip(ctx context.Context) ([]configdomain.IdentidadMicrosip, error) {
	return s.catalogo.ListarIdentidadesMicrosip(ctx)
}
