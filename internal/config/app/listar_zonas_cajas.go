package app

import (
	"context"
	"sort"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

// ListarZonasCajas returns one ZonaCajaAsignacion per client zone
// (ZONAS_CLIENTES), LEFT-JOINed against MSP_CFG_ZONA_CAJA: a zone with no
// config row still appears (every slot nil); a config row with a
// SinMapeoZonaCaja (-1) slot also surfaces nil for that slot. The four
// catalogs (cajas/cajeros/vendedores/cobradores) are each read exactly once
// and resolved via an in-memory id→nombre map, never per zone. Ordered by
// ZonaNombre.
func (s *Service) ListarZonasCajas(ctx context.Context) ([]configdomain.ZonaCajaAsignacion, error) {
	zonas, err := s.zonaCajaCatalogo.ListarZonas(ctx)
	if err != nil {
		return nil, err
	}

	configs, err := s.repo.ListarZonaCajaConfigs(ctx)
	if err != nil {
		return nil, err
	}
	byZona := make(map[int]configdomain.ZonaCajaConfig, len(configs))
	for _, c := range configs {
		byZona[c.ZonaClienteID] = c
	}

	cajaNombres, err := s.catalogoRefMap(ctx, s.zonaCajaCatalogo.ListarCajas)
	if err != nil {
		return nil, err
	}
	cajeroNombres, err := s.catalogoRefMap(ctx, s.zonaCajaCatalogo.ListarCajeros)
	if err != nil {
		return nil, err
	}
	vendedorNombres, err := s.catalogoRefMap(ctx, s.zonaCajaCatalogo.ListarVendedoresCatalogo)
	if err != nil {
		return nil, err
	}
	cobradorNombres, err := s.catalogoRefMap(ctx, s.zonaCajaCatalogo.ListarCobradores)
	if err != nil {
		return nil, err
	}

	sorted := make([]configdomain.CatalogoRef, len(zonas))
	copy(sorted, zonas)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Nombre < sorted[j].Nombre })

	result := make([]configdomain.ZonaCajaAsignacion, 0, len(sorted))
	for _, z := range sorted {
		asig := configdomain.ZonaCajaAsignacion{ZonaClienteID: z.ID, ZonaNombre: z.Nombre}
		if cfg, ok := byZona[z.ID]; ok {
			asig.Caja = buildCatalogoRef(cfg.CajaID, cajaNombres)
			asig.Cajero = buildCatalogoRef(cfg.CajeroID, cajeroNombres)
			asig.Vendedor = buildCatalogoRef(cfg.VendedorID, vendedorNombres)
			asig.Cobrador = buildCatalogoRef(cfg.CobradorID, cobradorNombres)
		}
		result = append(result, asig)
	}
	return result, nil
}

// catalogoRefMap reads a catalog once via list and folds it into an
// id→nombre map.
func (s *Service) catalogoRefMap(
	ctx context.Context,
	list func(context.Context) ([]configdomain.CatalogoRef, error),
) (map[int]string, error) {
	refs, err := list(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[int]string, len(refs))
	for _, r := range refs {
		m[r.ID] = r.Nombre
	}
	return m, nil
}

// buildCatalogoRef resolves a stored slot id into a *CatalogoRef: nil for the
// SinMapeoZonaCaja sentinel, otherwise the id with its resolved name
// (falling back to nombreDesconocido when the catalog row is missing).
func buildCatalogoRef(id int, nombres map[int]string) *configdomain.CatalogoRef {
	if id == configdomain.SinMapeoZonaCaja {
		return nil
	}
	nombre, ok := nombres[id]
	if !ok {
		nombre = nombreDesconocido
	}
	return &configdomain.CatalogoRef{ID: id, Nombre: nombre}
}

// ListarOpcionesZonasCajas returns every catalog needed to populate the
// zonas/cajas administration screen's dropdowns.
func (s *Service) ListarOpcionesZonasCajas(ctx context.Context) (configdomain.OpcionesZonasCajas, error) {
	zonas, err := s.zonaCajaCatalogo.ListarZonas(ctx)
	if err != nil {
		return configdomain.OpcionesZonasCajas{}, err
	}
	cajas, err := s.zonaCajaCatalogo.ListarCajas(ctx)
	if err != nil {
		return configdomain.OpcionesZonasCajas{}, err
	}
	cajeros, err := s.zonaCajaCatalogo.ListarCajeros(ctx)
	if err != nil {
		return configdomain.OpcionesZonasCajas{}, err
	}
	vendedores, err := s.zonaCajaCatalogo.ListarVendedoresCatalogo(ctx)
	if err != nil {
		return configdomain.OpcionesZonasCajas{}, err
	}
	cobradores, err := s.zonaCajaCatalogo.ListarCobradores(ctx)
	if err != nil {
		return configdomain.OpcionesZonasCajas{}, err
	}
	return configdomain.OpcionesZonasCajas{
		Zonas:      zonas,
		Cajas:      cajas,
		Cajeros:    cajeros,
		Vendedores: vendedores,
		Cobradores: cobradores,
	}, nil
}
