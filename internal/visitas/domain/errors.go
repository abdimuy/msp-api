// Package domain holds the visitas module's aggregate and sentinel errors.
// It depends only on the standard library, uuid, golang.org/x/text/unicode/norm,
// and platform/{apperror,audit} — never on app, infra, or other modules.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Sentinel errors for the visitas domain. All are produced via apperror.New*
// constructors so they participate in the typed error model and so the
// err113 linter does not flag them.
//
// Error codes are snake_case English; messages are lowercase Spanish without
// a trailing period, per project conventions.
var (
	// ErrVisitaIDRequerido is returned when the client-provided UUID is the
	// zero value. The ID doubles as the idempotency key end-to-end (mobile
	// app generates it, the repo — Task 2 — rejects a duplicate insert with
	// ErrVisitaYaExiste).
	ErrVisitaIDRequerido = apperror.NewValidation(
		"visita_id_requerido",
		"el id de la visita es obligatorio",
	)

	// ErrVisitaClienteRequerido is returned when ClienteID is non-positive.
	ErrVisitaClienteRequerido = apperror.NewValidation(
		"visita_cliente_requerido",
		"el cliente de la visita es obligatorio",
	)

	// ErrVisitaCobradorRequerido is returned when the cobrador name is blank
	// after trimming.
	ErrVisitaCobradorRequerido = apperror.NewValidation(
		"visita_cobrador_requerido",
		"el nombre del cobrador es obligatorio",
	)

	// ErrVisitaCobradorDemasiadoLargo is returned when the cobrador name
	// exceeds MSP_VISITAS.COBRADOR's column width (VARCHAR(150)).
	ErrVisitaCobradorDemasiadoLargo = apperror.NewValidation(
		"visita_cobrador_demasiado_largo",
		"el nombre del cobrador es demasiado largo",
	)

	// ErrVisitaTipoRequerido is returned when tipoVisita is blank after
	// trimming.
	ErrVisitaTipoRequerido = apperror.NewValidation(
		"visita_tipo_requerido",
		"el tipo de visita es obligatorio",
	)

	// ErrVisitaTipoDemasiadoLargo is returned when tipoVisita exceeds
	// MSP_VISITAS.TIPO_VISITA's column width (VARCHAR(100)).
	ErrVisitaTipoDemasiadoLargo = apperror.NewValidation(
		"visita_tipo_demasiado_largo",
		"el tipo de visita es demasiado largo",
	)

	// ErrVisitaNotaDemasiadoLarga is returned when nota exceeds
	// MSP_VISITAS.NOTA's column width (VARCHAR(10000)).
	ErrVisitaNotaDemasiadoLarga = apperror.NewValidation(
		"visita_nota_demasiado_larga",
		"la nota de la visita es demasiado larga",
	)

	// ErrVisitaFechaRequerida is returned when fecha is the zero time.Time.
	ErrVisitaFechaRequerida = apperror.NewValidation(
		"visita_fecha_requerida",
		"la fecha de la visita es obligatoria",
	)

	// ErrVisitaFechaFutura is returned when fecha is more than
	// fechaFuturaTolerancia ahead of the reference "now". Old dates are
	// ALWAYS accepted — an offline visit may be uploaded days later, and
	// rejecting it would be data loss.
	ErrVisitaFechaFutura = apperror.NewValidation(
		"visita_fecha_futura",
		"la fecha de la visita no puede ser futura",
	)

	// ErrVisitaStringCaracteresInvalidos is returned by validateSafeChars
	// when a string contains characters that would corrupt persistence.
	ErrVisitaStringCaracteresInvalidos = apperror.NewValidation(
		"visita_string_caracteres_invalidos",
		"el texto contiene caracteres no permitidos",
	)

	// ErrVisitaYaExiste is returned by CrearVisita (app/repo layer, Task 2)
	// when a visita with the same client-provided ID already exists in
	// MSP_VISITAS (idempotency collision). Declared here — a sentinel error
	// belongs with the aggregate it protects — but no logic in this package
	// produces it; there is nothing to check against without a repository.
	ErrVisitaYaExiste = apperror.NewConflict(
		"visita_ya_existe",
		"la visita con ese id ya existe",
	)
)
