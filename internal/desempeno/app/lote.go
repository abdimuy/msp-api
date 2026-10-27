//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
)

// actividadesLote resuelve varias ventanas de actividades: en una sola lectura
// si el repositorio sabe leer por lote (outbound.ActividadesLoteRepo), si no
// una lectura por ventana. Completa quién registró cada actividad.
func (s *Service) actividadesLote(
	ctx context.Context, ix domain.IndiceUsuarios, consultas []outbound.ConsultaActividades,
) ([][]domain.Actividad, error) {
	if len(consultas) == 0 {
		return nil, nil
	}
	out, err := s.leerActividades(ctx, consultas)
	if err != nil {
		return nil, err
	}
	for _, acts := range out {
		for i := range acts {
			acts[i] = ix.Completar(acts[i])
		}
	}
	return out, nil
}

// leerActividades lee por lote si el repositorio sabe, si no por ventana.
func (s *Service) leerActividades(ctx context.Context, consultas []outbound.ConsultaActividades) ([][]domain.Actividad, error) {
	if l, ok := s.actividades.(outbound.ActividadesLoteRepo); ok {
		out, err := l.ActividadesZonas(ctx, consultas)
		if err != nil {
			return nil, err
		}
		if len(out) != len(consultas) {
			return nil, domain.ErrLoteIncompleto
		}
		return out, nil
	}
	out := make([][]domain.Actividad, len(consultas))
	for i, c := range consultas {
		acts, err := s.actividades.ActividadesZona(ctx, c.ZonaID, c.Desde, c.Hasta)
		if err != nil {
			return nil, err
		}
		out[i] = acts
	}
	return out, nil
}

// universoLote: igual que actividadesLote, para el universo de la semana.
func (s *Service) universoLote(ctx context.Context, consultas []outbound.ConsultaUniverso) ([][]domain.CuentaUniverso, error) {
	if len(consultas) == 0 {
		return nil, nil
	}
	if l, ok := s.cuentas.(outbound.CuentasLoteRepo); ok {
		out, err := l.UniversoZonas(ctx, consultas)
		if err != nil {
			return nil, err
		}
		if len(out) != len(consultas) {
			return nil, domain.ErrLoteIncompleto
		}
		return out, nil
	}
	out := make([][]domain.CuentaUniverso, len(consultas))
	for i, c := range consultas {
		u, err := s.cuentas.UniversoSemana(ctx, c.ZonaID, c.InicioSemana)
		if err != nil {
			return nil, err
		}
		out[i] = u
	}
	return out, nil
}

// cobrosLote: el cobro de rutas de varios usuarios, en una lectura si el
// adaptador lo permite (outbound.CobrosLote).
func (s *Service) cobrosLote(ctx context.Context, us []domain.UsuarioCobranza) ([]domain.CobroSemana, error) {
	if len(us) == 0 {
		return nil, nil
	}
	if l, ok := s.usuarios.(outbound.CobrosLote); ok {
		out, err := l.CobrosSemana(ctx, us)
		if err != nil {
			return nil, err
		}
		if len(out) != len(us) {
			return nil, domain.ErrLoteIncompleto
		}
		return out, nil
	}
	out := make([]domain.CobroSemana, len(us))
	for i, u := range us {
		c, err := s.usuarios.CobroSemana(ctx, u)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}
