package firebird

import (
	"errors"
	"io"

	"github.com/nakagami/firebirdsql"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// Firebird GDS codes we explicitly recognize. Numbers come from the driver's
// errmsgs.go and Firebird's documented isc_* error catalog.
const (
	gdsUniquePrimary = 335544665 // violation of PRIMARY or UNIQUE KEY constraint
	gdsUniqueDup     = 335544349 // attempt to store duplicate value in unique index
	gdsForeignKey    = 335544466 // violation of FOREIGN KEY constraint
	gdsCheck         = 335544558 // operation violates CHECK constraint
	gdsDeadlock      = 335544336 // deadlock
	gdsLockNoWait    = 335544345 // lock conflict on no wait transaction
	gdsLockTimeout   = 335544510 // lock time-out on wait transaction
	gdsIOError       = 335544344 // I/O error during "@1" operation
	gdsConnLostPipe  = 335544648 // connection lost to pipe server
	gdsConnLostDB    = 335544741 // connection lost to database
)

// MapError translates a Firebird driver error into a domain apperror.Error.
// Non-Firebird errors (context cancellation, sql.ErrNoRows, etc.) pass through.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	var fbErr *firebirdsql.FbError
	if !errors.As(err, &fbErr) {
		// Connection-shaped non-FbError: net errors, io.EOF, etc.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return apperror.NewInternal("firebird_connection_lost",
				"conexión con base de datos perdida").
				WithSource("firebird").WithError(err)
		}
		return err
	}
	for _, code := range fbErr.GDSCodes {
		switch code {
		case gdsUniquePrimary, gdsUniqueDup:
			return apperror.NewConflict("firebird_unique_violation",
				"registro duplicado").
				WithSource("firebird").WithError(err)
		case gdsForeignKey:
			return apperror.NewConflict("firebird_fk_violation",
				"referencia inválida").
				WithSource("firebird").WithError(err)
		case gdsCheck:
			return apperror.NewValidation("firebird_check_failed",
				"datos no cumplen restricción del modelo").
				WithSource("firebird").WithError(err)
		case gdsDeadlock, gdsLockNoWait, gdsLockTimeout:
			return apperror.NewConflict("firebird_lock_conflict",
				"operación bloqueada, intente de nuevo").
				WithSource("firebird").WithError(err)
		case gdsIOError:
			return apperror.NewInternal("firebird_io_error",
				"error de entrada/salida de base de datos").
				WithSource("firebird").WithError(err)
		case gdsConnLostPipe, gdsConnLostDB:
			return apperror.NewInternal("firebird_connection_lost",
				"conexión con base de datos perdida").
				WithSource("firebird").WithError(err)
		}
	}
	return apperror.NewInternal("firebird_error",
		"error de base de datos").
		WithSource("firebird").WithError(err)
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
