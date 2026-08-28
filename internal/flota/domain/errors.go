package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors of the flota domain. Codes in English snake_case, messages
// in Spanish lowercase without a trailing period (CLAUDE.md §3).
var (
	// ErrUsuarioUIDRequerido is returned when an observation carries no
	// Firestore document id. Without it there is nothing to key history on.
	ErrUsuarioUIDRequerido = apperror.NewValidation(
		"flota_usuario_uid_requerido",
		"el identificador del usuario es obligatorio",
	)

	// ErrCamionetaInvalida is returned by NuevaCamioneta for a non-positive
	// almacén id. Note that the roster path does NOT use it: a zero, negative
	// or absent value there is a legitimate "no camioneta", not a defect.
	ErrCamionetaInvalida = apperror.NewValidation(
		"flota_camioneta_invalida",
		"el identificador de la camioneta debe ser mayor que cero",
	)

	// ErrObservacionDuplicada is returned when one snapshot carries the same
	// usuario twice. Firestore document ids are unique, so this can only mean
	// the adapter is broken; letting it through would insert a duplicate row
	// and, worse, hide one of the two states.
	ErrObservacionDuplicada = apperror.NewValidation(
		"flota_observacion_duplicada",
		"la foto trae al mismo usuario más de una vez",
	)

	// ErrFotoVacia is the safety rail against an empty read. A roster that
	// suddenly reports zero people while we already have assignments on record
	// is far more likely to be a Firestore outage, a revoked credential or a
	// silent downgrade to the free plan than sixty simultaneous resignations.
	// Recording it would delete every current row and emit a flood of
	// 'baja_usuario'. The tick aborts instead and writes nothing.
	ErrFotoVacia = apperror.NewConflict(
		"flota_foto_vacia",
		"la foto del roster llegó vacía habiendo asignaciones registradas",
	)

	// ErrVentanaInvalida guards the detection window: the moment of detection
	// must come after the previous snapshot. An inverted window would produce
	// history rows nobody can interpret.
	ErrVentanaInvalida = apperror.NewValidation(
		"flota_ventana_invalida",
		"la ventana de detección debe terminar después de comenzar",
	)
)
