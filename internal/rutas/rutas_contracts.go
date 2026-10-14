// Package rutas is the cross-module surface of the rutas bounded context.
// Other modules import only this package — never internal/rutas/domain,
// internal/rutas/app or internal/rutas/infra (depguard).
//
// The contract exports the per-user weekly cobranza that rutas already
// computes (window = FECHA_CARGA_INICIAL of the Firestore user), so consumers
// such as desempeno reuse it instead of re-implementing aporte/atrasos.
//
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutas

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// UsuarioCobrador is one Firestore `users` document with COBRADOR_ID > 0.
// ZonaID 0 = no zona; InicioSemana nil = no FECHA_CARGA_INICIAL.
type UsuarioCobrador struct {
	UID          string
	Nombre       string
	Email        string
	CobradorID   int
	ZonaID       int
	ZonaNombre   string
	InicioSemana *time.Time
}

// VentaCobro is the per-venta weekly breakdown rutas computes
// (VentaCobranza enriched by enrichVentas).
type VentaCobro struct {
	VentaID         int
	ClienteID       int
	Parcialidad     decimal.Decimal
	AbonoSemana     decimal.Decimal
	Saldo           decimal.Decimal
	Vencidas        decimal.Decimal
	Aporte          decimal.Decimal
	AplicaPonderado bool
}

// CobranzaUsuario is the weekly cobranza of one user over THEIR window.
type CobranzaUsuario struct {
	Usuario      UsuarioCobrador
	Ventas       []VentaCobro
	PctPonderado *decimal.Decimal
}

// CobranzaUsuariosReader is satisfied by rutas app.Service.
type CobranzaUsuariosReader interface {
	// UsuariosCobradores lists every Firestore user with COBRADOR_ID > 0,
	// configured or not. A Firestore failure is ErrCalendarioNoDisponible.
	UsuariosCobradores(ctx context.Context) ([]UsuarioCobrador, error)
	// CobranzaDeUsuario computes the weekly breakdown for an already-resolved
	// user (no second Firestore read). A user without zona or window returns
	// an empty breakdown, not an error.
	CobranzaDeUsuario(ctx context.Context, u UsuarioCobrador) (CobranzaUsuario, error)
}

// ErrCalendarioNoDisponible wraps any failure reading Firestore `users`.
var ErrCalendarioNoDisponible = apperror.NewServiceUnavailable(
	"rutas_calendario_unavailable",
	"no se pudo leer el calendario de cobradores",
)
