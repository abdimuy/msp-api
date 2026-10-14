//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// UsuarioCobranza es un usuario de Firestore `users` con COBRADOR_ID > 0,
// visto por desempeño (lo provee el contrato de rutas). Una fila del tablero
// por cada uno (D1).
type UsuarioCobranza struct {
	UID          string
	Nombre       string
	Email        string
	CobradorID   int
	ZonaID       int // 0 = sin zona
	ZonaNombre   string
	InicioSemana *time.Time // nil = sin FECHA_CARGA_INICIAL
}

// Motivos de "sin configurar" (contrato §2 `motivo_no_configurado`).
const (
	MotivoSinZona         = "sin_zona"
	MotivoSinInicioSemana = "sin_inicio_semana"
)

// Configuracion dice configurado si tiene zona e inicio de semana; si no, el
// motivo (la zona se reporta primero).
func (u UsuarioCobranza) Configuracion() (bool, string) {
	switch {
	case u.ZonaID <= 0:
		return false, MotivoSinZona
	case u.InicioSemana == nil:
		return false, MotivoSinInicioSemana
	default:
		return true, ""
	}
}

// ExigirConfigurado traduce Configuracion a los errores 409 del contrato.
func (u UsuarioCobranza) ExigirConfigurado() error {
	ok, motivo := u.Configuracion()
	if ok {
		return nil
	}
	if motivo == MotivoSinZona {
		return ErrUsuarioSinZona
	}
	return ErrUsuarioSinInicioSemana
}

// CobroVenta es lo que rutas ya calcula por venta (se consume, no se
// reimplementa: spec §3.1).
type CobroVenta struct {
	VentaID         int
	Parcialidad     decimal.Decimal
	AbonoSemana     decimal.Decimal
	Vencidas        decimal.Decimal
	AplicaPonderado bool
}

// CobroSemana es el desglose de rutas para la semana del usuario.
type CobroSemana struct {
	Ventas       []CobroVenta
	PctPonderado *decimal.Decimal
}
