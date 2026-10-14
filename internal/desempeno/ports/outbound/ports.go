// Package outbound declara lo que el módulo desempeno necesita de afuera. Las
// implementaciones viven en internal/desempeno/infra/* y se arman en cmd/api.
//
//nolint:misspell // vocabulario español por convención del proyecto.
package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// UsuariosCobranza lee los usuarios de Firestore y la cobranza semanal que ya
// calcula rutas (vía su contrato; implementación en infra/clients).
type UsuariosCobranza interface {
	// ListarUsuarios: todos los usuarios con COBRADOR_ID > 0, configurados o
	// no. Firestore caído → domain.ErrFirestoreNoDisponible.
	ListarUsuarios(ctx context.Context) ([]domain.UsuarioCobranza, error)
	// CobroSemana: desglose de rutas sobre la semana del usuario.
	CobroSemana(ctx context.Context, u domain.UsuarioCobranza) (domain.CobroSemana, error)
}

// ActividadesRepo lee pagos, condonaciones y visitas de una zona. Consultas
// propias: no usa ni modifica las del sync (COBRANZA-SYNC.md).
type ActividadesRepo interface {
	// ActividadesZona devuelve las actividades con hora en [desde, hasta]
	// (ambos incluidos) de la zona, en cualquier orden. RegistradoPor viene a
	// medio llenar (uid por CREATED_BY, COBRADOR_ID y nombre crudo); el
	// servicio lo completa con domain.IndiceUsuarios.
	ActividadesZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]domain.Actividad, error)
}

// CuentasRepo lee el universo de ventas de la semana.
type CuentasRepo interface {
	UniversoSemana(ctx context.Context, zonaID int, inicioSemana time.Time) ([]domain.CuentaUniverso, error)
}

// Clock abstrae time.Now() para las pruebas.
type Clock interface {
	Now() time.Time
}

// ProductionClock devuelve la hora actual en UTC.
type ProductionClock struct{}

// Now devuelve time.Now().UTC().
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
