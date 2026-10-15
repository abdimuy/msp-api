//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Errores del módulo. Los códigos son los del contrato §0
// (docs/superpowers/specs/2026-10-14-desempeno-contratos.md) byte a byte.
var (
	// ErrUsuarioNoEncontrado: el uid no existe en Firestore `users` (o no es cobrador).
	ErrUsuarioNoEncontrado = apperror.NewNotFound(
		"desempeno_usuario_not_found",
		"el usuario no existe o no es cobrador",
	)
	// ErrUsuarioSinZona: el usuario no tiene ZONA_CLIENTE_ID en Firestore.
	ErrUsuarioSinZona = apperror.NewConflict(
		"desempeno_usuario_sin_zona",
		"el usuario no tiene zona asignada",
	)
	// ErrUsuarioSinInicioSemana: el usuario no tiene FECHA_CARGA_INICIAL.
	ErrUsuarioSinInicioSemana = apperror.NewConflict(
		"desempeno_usuario_sin_inicio_semana",
		"el usuario no tiene inicio de semana",
	)
	// ErrParametroInvalido: cualquier parámetro de consulta fuera de contrato.
	// El nombre del parámetro viaja en WithField("parametro", ...).
	ErrParametroInvalido = apperror.NewValidation(
		"desempeno_parametro_invalido",
		"parámetro inválido",
	)
	// ErrFirestoreNoDisponible: no se pudo leer `users`. Nunca se inventan
	// zonas ni semanas (spec §6).
	ErrFirestoreNoDisponible = apperror.NewServiceUnavailable(
		"desempeno_firestore_unavailable",
		"no se pudo consultar a los usuarios, intente de nuevo",
	)
	// ErrLoteIncompleto: un repositorio por lote devolvió otro número de
	// respuestas que de consultas. Defecto del adaptador, nunca de datos: la
	// consulta falla antes que repartir cifras a la zona equivocada.
	ErrLoteIncompleto = apperror.NewInternal(
		"desempeno_lote_incompleto",
		"no se pudo armar el tablero, intente de nuevo",
	)
)
