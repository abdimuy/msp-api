// Package app es la capa de casos de uso del módulo desempeno (solo lectura).
// Un archivo por consulta: tablero.go, jornada.go, actividad.go, cuentas.go,
// exportar.go (CQRS_PATTERN.md).
//
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
)

// Service reúne las dependencias del módulo.
type Service struct {
	usuarios    outbound.UsuariosCobranza
	actividades outbound.ActividadesRepo
	cuentas     outbound.CuentasRepo
	clock       outbound.Clock
	loc         *time.Location
}

// NewService arma el servicio. loc es la zona de negocio
// (firebird.BusinessTZ() en producción, America/Mexico_City).
func NewService(
	usuarios outbound.UsuariosCobranza,
	actividades outbound.ActividadesRepo,
	cuentas outbound.CuentasRepo,
	clock outbound.Clock,
	loc *time.Location,
) *Service {
	return &Service{usuarios: usuarios, actividades: actividades, cuentas: cuentas, clock: clock, loc: loc}
}

// contexto es lo que toda consulta por usuario necesita: el usuario, el índice
// para resolver registradores y "ahora".
type contexto struct {
	usuario domain.UsuarioCobranza
	indice  domain.IndiceUsuarios
	ahora   time.Time
}

// resolverUsuario lista los usuarios (una sola lectura de Firestore), busca uid
// y devuelve el índice para completar registradores.
func (s *Service) resolverUsuario(ctx context.Context, uid string) (contexto, error) {
	us, err := s.usuarios.ListarUsuarios(ctx)
	if err != nil {
		return contexto{}, err
	}
	for _, u := range us {
		if u.UID == uid {
			return contexto{usuario: u, indice: domain.NuevoIndiceUsuarios(us), ahora: s.clock.Now()}, nil
		}
	}
	return contexto{}, domain.ErrUsuarioNoEncontrado
}

// actividadesCompletas trae las actividades de la zona y completa quién las
// registró.
func (s *Service) actividadesCompletas(
	ctx context.Context, ix domain.IndiceUsuarios, zonaID int, desde, hasta time.Time,
) ([]domain.Actividad, error) {
	acts, err := s.actividades.ActividadesZona(ctx, zonaID, desde, hasta)
	if err != nil {
		return nil, err
	}
	for i := range acts {
		acts[i] = ix.Completar(acts[i])
	}
	return acts, nil
}

// registradasPor filtra las actividades que registró u.
func registradasPor(acts []domain.Actividad, u domain.UsuarioCobranza) []domain.Actividad {
	out := make([]domain.Actividad, 0, len(acts))
	for _, a := range acts {
		if domain.RegistradaPor(a, u) {
			out = append(out, a)
		}
	}
	return out
}
