//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// MotivoInicioSemanaFuturo es el tercer motivo de "sin configurar" (contrato
// v2.1 §2): FECHA_CARGA_INICIAL quedó después de ahora (capturada mal o con la
// zona horaria equivocada en Firestore). No hay ventana [inicio, ahora] que
// calcular, así que la fila sale sin configurar en vez de tumbar el tablero.
const MotivoInicioSemanaFuturo = "inicio_semana_futuro"

// ErrUsuarioInicioSemanaFuturo comparte código con ErrUsuarioSinInicioSemana
// (409 desempeno_usuario_sin_inicio_semana, contrato v2.1) pero dice qué pasa:
// errors.Is la empata con las dos, porque apperror compara por código.
var ErrUsuarioInicioSemanaFuturo = apperror.NewConflict(
	"desempeno_usuario_sin_inicio_semana",
	"el inicio de semana del usuario es posterior a la hora actual, corríjalo en usuarios",
)

// InicioSemanaFuturo dice si FECHA_CARGA_INICIAL es posterior a ahora.
func (u UsuarioCobranza) InicioSemanaFuturo(ahora time.Time) bool {
	return u.InicioSemana != nil && u.InicioSemana.After(ahora)
}

// ConfiguracionEn es Configuracion evaluada en ahora: además de zona e inicio
// de semana exige que el inicio no sea futuro.
func (u UsuarioCobranza) ConfiguracionEn(ahora time.Time) (bool, string) {
	if ok, motivo := u.Configuracion(); !ok {
		return false, motivo
	}
	if u.InicioSemanaFuturo(ahora) {
		return false, MotivoInicioSemanaFuturo
	}
	return true, ""
}

// ExigirConfiguradoEn traduce ConfiguracionEn a los errores 409 del contrato.
func (u UsuarioCobranza) ExigirConfiguradoEn(ahora time.Time) error {
	if err := u.ExigirConfigurado(); err != nil {
		return err
	}
	if u.InicioSemanaFuturo(ahora) {
		return ErrUsuarioInicioSemanaFuturo
	}
	return nil
}
