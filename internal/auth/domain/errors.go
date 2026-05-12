// Package domain holds the auth module's entities, value objects, and
// sentinel errors. It depends only on the standard library, uuid, decimal,
// and the platform/{domain,apperror} packages — never on app, infra, or
// other modules.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the auth domain. All are produced via apperror.New*
// constructors so they participate in the typed error model (Kind →
// HTTPStatus) and so the err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per the project conventions.
var (
	// ErrUsuarioNotFound is returned when a usuario lookup misses.
	ErrUsuarioNotFound = apperror.NewNotFound(
		"usuario_not_found",
		"usuario no encontrado",
	)
	// ErrUsuarioYaExiste is returned when attempting to create a usuario
	// whose email or firebase uid collides with an existing one.
	ErrUsuarioYaExiste = apperror.NewConflict(
		"usuario_ya_existe",
		"el usuario ya existe",
	)
	// ErrUsuarioInactivo is returned when an operation requires an active
	// usuario but the target is deactivated.
	ErrUsuarioInactivo = apperror.NewForbidden(
		"usuario_inactivo",
		"el usuario está inactivo",
	)

	// ErrRolNotFound is returned when a rol lookup misses.
	ErrRolNotFound = apperror.NewNotFound(
		"rol_not_found",
		"rol no encontrado",
	)
	// ErrRolInmutable is returned when an operation tries to mutate or
	// deactivate a rol marked as inmutable (system catalog).
	ErrRolInmutable = apperror.NewForbidden(
		"rol_inmutable",
		"no se puede modificar un rol inmutable",
	)
	// ErrRolYaExiste is returned when creating a rol whose name collides
	// with an existing one.
	ErrRolYaExiste = apperror.NewConflict(
		"rol_ya_existe",
		"ya existe un rol con ese nombre",
	)
	// ErrRolNombreRequerido is returned when a rol name is empty.
	ErrRolNombreRequerido = apperror.NewValidation(
		"rol_nombre_required",
		"el nombre del rol es obligatorio",
	)
	// ErrRolNombreDemasiadoLargo is returned when a rol name exceeds 50
	// characters (the column width in Firebird).
	ErrRolNombreDemasiadoLargo = apperror.NewValidation(
		"rol_nombre_too_long",
		"el nombre del rol excede 50 caracteres",
	)
	// ErrRolDescripcionDemasiadoLarga is returned when a rol description
	// exceeds 255 characters.
	ErrRolDescripcionDemasiadoLarga = apperror.NewValidation(
		"rol_description_too_long",
		"la descripción del rol excede 255 caracteres",
	)

	// ErrPermisoNotFound is returned when a permiso lookup misses.
	ErrPermisoNotFound = apperror.NewNotFound(
		"permiso_not_found",
		"permiso no encontrado",
	)
	// ErrPermisoCodigoRequerido is returned when a permiso code is empty.
	ErrPermisoCodigoRequerido = apperror.NewValidation(
		"permiso_codigo_required",
		"el código del permiso es obligatorio",
	)
	// ErrPermisoCodigoDemasiadoLargo is returned when a permiso code exceeds
	// 60 characters.
	ErrPermisoCodigoDemasiadoLargo = apperror.NewValidation(
		"permiso_codigo_too_long",
		"el código del permiso excede 60 caracteres",
	)
	// ErrPermisoDescripcionRequerida is returned when a permiso description
	// is empty.
	ErrPermisoDescripcionRequerida = apperror.NewValidation(
		"permiso_description_required",
		"la descripción del permiso es obligatoria",
	)
	// ErrPermisoDescripcionDemasiadoLarga is returned when a permiso
	// description exceeds 255 characters.
	ErrPermisoDescripcionDemasiadoLarga = apperror.NewValidation(
		"permiso_description_too_long",
		"la descripción del permiso excede 255 caracteres",
	)
	// ErrPermisoCategoriaRequerida is returned when a permiso category is
	// empty.
	ErrPermisoCategoriaRequerida = apperror.NewValidation(
		"permiso_categoria_required",
		"la categoría del permiso es obligatoria",
	)
	// ErrPermisoCategoriaDemasiadoLarga is returned when a permiso category
	// exceeds 30 characters.
	ErrPermisoCategoriaDemasiadoLarga = apperror.NewValidation(
		"permiso_categoria_too_long",
		"la categoría del permiso excede 30 caracteres",
	)

	// ErrEmailRequerido is returned when an email input is empty.
	ErrEmailRequerido = apperror.NewValidation(
		"email_required",
		"el email es obligatorio",
	)
	// ErrEmailInvalido is returned when an email input fails validation.
	ErrEmailInvalido = apperror.NewValidation(
		"email_invalid",
		"el email no es válido",
	)
	// ErrEmailDemasiadoLargo is returned when an email exceeds 255 characters.
	ErrEmailDemasiadoLargo = apperror.NewValidation(
		"email_too_long",
		"el email excede 255 caracteres",
	)

	// ErrFirebaseUIDRequerido is returned when a firebase uid is empty.
	ErrFirebaseUIDRequerido = apperror.NewValidation(
		"firebase_uid_required",
		"el firebase uid es obligatorio",
	)
	// ErrFirebaseUIDInvalido is returned when a firebase uid contains
	// disallowed characters.
	ErrFirebaseUIDInvalido = apperror.NewValidation(
		"firebase_uid_invalid",
		"el firebase uid no es válido",
	)
	// ErrFirebaseUIDDemasiadoLargo is returned when a firebase uid exceeds
	// 128 characters.
	ErrFirebaseUIDDemasiadoLargo = apperror.NewValidation(
		"firebase_uid_too_long",
		"el firebase uid excede 128 caracteres",
	)

	// ErrNombreRequerido is returned when a nombre is empty.
	ErrNombreRequerido = apperror.NewValidation(
		"nombre_required",
		"el nombre es obligatorio",
	)
	// ErrNombreDemasiadoLargo is returned when a nombre exceeds 200
	// characters (the column width in Firebird).
	ErrNombreDemasiadoLargo = apperror.NewValidation(
		"nombre_too_long",
		"el nombre excede 200 caracteres",
	)
	// ErrNombreInvalido is returned when a nombre contains disallowed
	// characters.
	ErrNombreInvalido = apperror.NewValidation(
		"nombre_invalid",
		"el nombre contiene caracteres no permitidos",
	)
)
