package firebird

import (
	"context"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/nakagami/firebirdsql"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Firebird GDS codes we explicitly recognize. Numbers come from the driver's
// errmsgs.go and Firebird's documented isc_* error catalog.
const (
	gdsUniquePrimary  = 335544665 // violation of PRIMARY or UNIQUE KEY constraint
	gdsUniqueDup      = 335544349 // attempt to store duplicate value in unique index
	gdsForeignKey     = 335544466 // violation of FOREIGN KEY constraint
	gdsCheck          = 335544558 // operation violates CHECK constraint
	gdsDeadlock       = 335544336 // deadlock
	gdsLockNoWait     = 335544345 // lock conflict on no wait transaction
	gdsLockTimeout    = 335544510 // lock time-out on wait transaction
	gdsIOError        = 335544344 // I/O error during "@1" operation
	gdsConnLostPipe   = 335544648 // connection lost to pipe server
	gdsConnLostDB     = 335544741 // connection lost to database
	gdsAttStmtTimeout = 335545128 // attachment level timeout expired
	gdsReqStmtTimeout = 335545129 // statement level timeout expired
)

// MapError translates a Firebird driver error into a domain apperror.Error.
// Non-Firebird errors (context cancellation, sql.ErrNoRows, etc.) pass through.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	var fbErr *firebirdsql.FbError
	if !errors.As(err, &fbErr) {
		// A deadline or cancellation on the caller's side means the query never
		// produced an answer: surface it as 503 so the client retries instead of
		// reading a raw context error it cannot classify.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return apperror.NewServiceUnavailable("firebird_timeout",
				"la base de datos no respondió a tiempo").
				WithSource("firebird").WithError(conTextoSaneado(err))
		}
		// Connection-shaped non-FbError: net errors, io.EOF, etc.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return apperror.NewInternal("firebird_connection_lost",
				"conexión con base de datos perdida").
				WithSource("firebird").WithError(conTextoSaneado(err))
		}
		// Se sanea aunque no se mapee: este error también acaba escrito en
		// columnas nuestras que son UTF8, y ahí un byte suelto da -303.
		return conTextoSaneado(err)
	}
	for _, code := range fbErr.GDSCodes {
		switch code {
		case gdsUniquePrimary, gdsUniqueDup:
			return apperror.NewConflict("firebird_unique_violation",
				"registro duplicado").
				WithSource("firebird").WithError(conTextoSaneado(err))
		case gdsForeignKey:
			return apperror.NewConflict("firebird_fk_violation",
				"referencia inválida").
				WithSource("firebird").WithError(conTextoSaneado(err))
		case gdsCheck:
			return apperror.NewValidation("firebird_check_failed",
				"datos no cumplen restricción del modelo").
				WithSource("firebird").WithError(conTextoSaneado(err))
		case gdsDeadlock, gdsLockNoWait, gdsLockTimeout:
			return apperror.NewConflict("firebird_lock_conflict",
				"operación bloqueada, intente de nuevo").
				WithSource("firebird").WithError(conTextoSaneado(err))
		case gdsAttStmtTimeout, gdsReqStmtTimeout:
			// The server cut the statement at its configured ceiling. Not a
			// lock conflict: retrying the same slow query would only burn the
			// same budget again.
			return apperror.NewServiceUnavailable("firebird_timeout",
				"la base de datos no respondió a tiempo").
				WithSource("firebird").WithError(conTextoSaneado(err))
		case gdsIOError:
			return apperror.NewInternal("firebird_io_error",
				"error de entrada/salida de base de datos").
				WithSource("firebird").WithError(conTextoSaneado(err))
		case gdsConnLostPipe, gdsConnLostDB:
			return apperror.NewInternal("firebird_connection_lost",
				"conexión con base de datos perdida").
				WithSource("firebird").WithError(conTextoSaneado(err))
		}
	}
	return apperror.NewInternal("firebird_error",
		"error de base de datos").
		WithSource("firebird").WithError(conTextoSaneado(err))
}

// saneadoError envuelve un error del driver para que su TEXTO sea UTF-8 válido,
// conservando la cadena para errors.Is / errors.As.
//
// Existe porque el mensaje que devuelve Firebird cuando Microsip lanza una
// EXCEPTION llega en Windows-1252: la "é" de "crédito" viaja como 0xE9. Ese
// texto termina, vía apperror.Error(), escrito en columnas nuestras que son
// CHARACTER SET UTF8 —MSP_PAGOS_RECIBIDOS.ULTIMO_ERROR,
// MSP_OUTBOX_EVENTS.LAST_ERROR— y ahí Firebird lo rechaza con
// "-303 Malformed string", deshaciendo la transacción ENTERA.
//
// Se saneo AQUÍ, y no en cada sitio que guarda un motivo, porque éste es el
// punto donde los bytes ajenos entran a nuestro mundo: es la frontera que
// ENCODING_HANDLING.md llama el adapter de Microsip. Sanearlo en cada
// consumidor sería repetir la misma guarda en sitios que se irán olvidando.
//
// Unwrap conserva el error original, así que errors.As(err, &fbErr) y las
// comprobaciones de GDS siguen funcionando igual.
type saneadoError struct{ err error }

func (e saneadoError) Error() string { return SanearParaUTF8(e.err.Error()) }
func (e saneadoError) Unwrap() error { return e.err }

// conTextoSaneado envuelve err sólo cuando hace falta. Un error cuyo texto ya
// es UTF-8 válido se devuelve tal cual: envolver por envolver añadiría una capa
// a cada error de base de datos del sistema sin arreglar nada.
func conTextoSaneado(err error) error {
	if err == nil {
		return nil
	}
	if utf8.ValidString(err.Error()) {
		return err
	}
	return saneadoError{err: err}
}

// IsTransient reports whether err is a Firebird error whose retry has a real
// chance of succeeding: lock conflicts, deadlocks, and connection drops. Non-
// transient errors (FK, CHECK, unique violations) return false. Used by
// ExecRetry to decide whether to spin.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	if appErr, ok := apperror.As(err); ok {
		switch appErr.Code {
		case "firebird_lock_conflict",
			"firebird_io_error",
			"firebird_connection_lost":
			return true
		}
	}
	// Fallback for raw driver errors (in case caller hasn't run MapError yet).
	var fbErr *firebirdsql.FbError
	if errors.As(err, &fbErr) {
		for _, code := range fbErr.GDSCodes {
			switch code {
			case gdsDeadlock, gdsLockNoWait, gdsLockTimeout,
				gdsIOError, gdsConnLostPipe, gdsConnLostDB:
				return true
			}
		}
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	return false
}
