package canalsqlite

import (
	"context"
	"errors"
	"io"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// mapError translates a modernc.org/sqlite driver error into an
// apperror.Error, mirroring internal/platform/firebird.MapError's shape for
// the sibling Firebird driver. Non-SQLite errors (context cancellation,
// io.EOF on a lost connection, sql.ErrNoRows) pass through unchanged —
// sql.ErrNoRows in particular is not remapped here because none of this
// repo's methods use QueryRowContext against a possibly-absent row; the
// "not found" case here is expressed instead by RowsAffected() == 0 on an
// UPDATE, see ensureRowAffected.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return apperror.NewServiceUnavailable("canal_mailbox_timeout",
				"el buzón no respondió a tiempo").
				WithSource("canalsqlite").WithError(err)
		}
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return apperror.NewInternal("canal_mailbox_connection_lost",
				"conexión con el buzón perdida").
				WithSource("canalsqlite").WithError(err)
		}
		return err
	}

	code := sqliteErr.Code()
	baseCode := code & 0xff
	switch {
	case code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
		return apperror.NewConflict("canal_mailbox_duplicate",
			"registro duplicado en el buzón").
			WithSource("canalsqlite").WithError(err)
	case baseCode == sqlite3.SQLITE_CONSTRAINT:
		return apperror.NewValidation("canal_mailbox_constraint_violation",
			"los datos no cumplen una restricción del buzón").
			WithSource("canalsqlite").WithError(err)
	case baseCode == sqlite3.SQLITE_BUSY || baseCode == sqlite3.SQLITE_LOCKED:
		return apperror.NewConflict("canal_mailbox_locked",
			"el buzón está ocupado, intente de nuevo").
			WithSource("canalsqlite").WithError(err)
	case baseCode == sqlite3.SQLITE_IOERR:
		return apperror.NewInternal("canal_mailbox_io_error",
			"error de entrada/salida del buzón").
			WithSource("canalsqlite").WithError(err)
	case baseCode == sqlite3.SQLITE_CORRUPT:
		return apperror.NewInternal("canal_mailbox_corrupt",
			"el archivo del buzón está corrupto").
			WithSource("canalsqlite").WithError(err)
	default:
		return apperror.NewInternal("canal_mailbox_error",
			"error del buzón").
			WithSource("canalsqlite").WithError(err)
	}
}
