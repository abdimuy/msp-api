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

// ─── Lecturas por lote (tablero) ─────────────────────────────────────────────
//
// El tablero necesita la semana de TODAS las zonas a la vez. Leerla zona por
// zona costaba cinco sentencias por zona (medido: 221 por petición con 44
// zonas) y varias de ellas recorrían todas las zonas cada vez. Estas
// interfaces son opcionales: el servicio las usa si el repositorio las
// implementa y, si no, cae a las lecturas por zona con el mismo resultado.

// ConsultaActividades es una ventana [Desde, Hasta] (ambos incluidos) de una zona.
type ConsultaActividades struct {
	ZonaID int
	Desde  time.Time
	Hasta  time.Time
}

// ActividadesLoteRepo responde varias consultas con un número fijo de
// sentencias. out[i] es exactamente lo que ActividadesZona devolvería para
// consultas[i] (mismas actividades, mismo orden) y es un slice propio.
type ActividadesLoteRepo interface {
	ActividadesZonas(ctx context.Context, consultas []ConsultaActividades) ([][]domain.Actividad, error)
}

// ConsultaUniverso es el universo de una zona para un inicio de semana.
type ConsultaUniverso struct {
	ZonaID       int
	InicioSemana time.Time
}

// CuentasLoteRepo responde varias consultas de universo: out[i] es exactamente UniversoSemana(consultas[i]).
type CuentasLoteRepo interface {
	UniversoZonas(ctx context.Context, consultas []ConsultaUniverso) ([][]domain.CuentaUniverso, error)
}

// CobrosLote pide el cobro de varios usuarios: out[i] es exactamente CobroSemana(us[i]), con una sola lectura
// de rutas para todos.
type CobrosLote interface {
	CobrosSemana(ctx context.Context, us []domain.UsuarioCobranza) ([]domain.CobroSemana, error)
}

// Clock abstrae time.Now() para las pruebas.
type Clock interface {
	Now() time.Time
}

// ProductionClock devuelve la hora actual en UTC.
type ProductionClock struct{}

// Now devuelve time.Now().UTC().
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
