//nolint:misspell // Spanish column names (DESCRIPCION) match the Firebird schema exactly.
package firebird

import (
	"database/sql"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// rowScanner is the minimal surface satisfied by both *sql.Row and *sql.Rows.
// Repository helpers accept this so the same mapper works for single-row
// reads and paginated iteration.
type rowScanner interface {
	Scan(dest ...any) error
}

// parseUUIDColumn converts a CHAR(36) UUID column to a uuid.UUID, wrapping
// driver-side surprises in an apperror so the caller does not return raw
// driver errors.
func parseUUIDColumn(column, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, apperror.NewInternal(
			"firebird_uuid_invalid",
			"uuid inválido en columna de base de datos",
		).
			WithSource("firebird").
			WithError(err).
			WithField("column", column).
			WithField("raw_value", raw)
	}
	return id, nil
}

// usuarioFromRow rebuilds a Usuario entity from one scanned row in the
// MSP_USUARIOS layout. Order of columns must match usuarioColumns.
func usuarioFromRow(s rowScanner) (*domain.Usuario, error) {
	var (
		idRaw, fuid, email         string
		nombre                     firebird.Win1252 // CHARACTER SET ISO8859_1 — Win1252 boundary
		telefono                   sql.NullString
		almacenID                  sql.NullInt32
		activo                     bool
		createdAtRaw, updatedAtRaw any
		createdByRaw, updatedByRaw string
	)
	if err := s.Scan(
		&idRaw, &fuid, &email, &nombre, &telefono, &almacenID, &activo,
		&createdAtRaw, &updatedAtRaw, &createdByRaw, &updatedByRaw,
	); err != nil {
		return nil, err
	}

	createdAt, err := firebird.ScanUTCTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}

	id, err := parseUUIDColumn("ID", idRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", createdByRaw)
	if err != nil {
		return nil, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", updatedByRaw)
	if err != nil {
		return nil, err
	}

	var telOpt *platform.Telefono
	if telefono.Valid {
		t := platform.HydrateTelefono(telefono.String)
		telOpt = &t
	}
	var almacenOpt *int
	if almacenID.Valid {
		n := int(almacenID.Int32)
		almacenOpt = &n
	}

	return domain.HydrateUsuario(domain.HydrateUsuarioParams{
		ID:          id,
		FirebaseUID: domain.HydrateFirebaseUID(fuid),
		Email:       domain.HydrateEmail(email),
		Nombre:      domain.HydrateNombre(string(nombre)),
		Telefono:    telOpt,
		AlmacenID:   almacenOpt,
		Activo:      activo,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	}), nil
}

// rolFromRow rebuilds a Rol entity from one scanned row. Column order must
// match rolColumns.
func rolFromRow(s rowScanner) (*domain.Rol, error) {
	var (
		idRaw                      string
		nombre                     firebird.Win1252 // CHARACTER SET ISO8859_1 — Win1252 boundary
		descRaw                    sql.NullString   // nullable; decoded below
		inmutable, activo          bool
		createdAtRaw, updatedAtRaw any
		createdByRaw, updatedByRaw string
	)
	if err := s.Scan(
		&idRaw, &nombre, &descRaw, &inmutable, &activo,
		&createdAtRaw, &updatedAtRaw, &createdByRaw, &updatedByRaw,
	); err != nil {
		return nil, err
	}

	createdAt, err := firebird.ScanUTCTime(createdAtRaw)
	if err != nil {
		return nil, err
	}
	updatedAt, err := firebird.ScanUTCTime(updatedAtRaw)
	if err != nil {
		return nil, err
	}

	id, err := parseUUIDColumn("ID", idRaw)
	if err != nil {
		return nil, err
	}
	createdBy, err := parseUUIDColumn("CREATED_BY", createdByRaw)
	if err != nil {
		return nil, err
	}
	updatedBy, err := parseUUIDColumn("UPDATED_BY", updatedByRaw)
	if err != nil {
		return nil, err
	}

	// DESCRIPCION is nullable (CHARACTER SET ISO8859_1). Decode the string
	// through Win1252 only when the value is present.
	var descOpt *string
	if descRaw.Valid {
		var w firebird.Win1252
		if scanErr := w.Scan(descRaw.String); scanErr != nil {
			return nil, scanErr
		}
		d := string(w)
		descOpt = &d
	}

	return domain.HydrateRol(domain.HydrateRolParams{
		ID:          id,
		Nombre:      string(nombre),
		Description: descOpt,
		Inmutable:   inmutable,
		Activo:      activo,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
		CreatedBy:   createdBy,
		UpdatedBy:   updatedBy,
	}), nil
}

// permisoFromRow rebuilds a Permiso value from one scanned row out of
// MSP_PERMISOS. Column order matches selectPermisoByCodigo /
// selectAllPermisos.
func permisoFromRow(s rowScanner) (*domain.Permiso, error) {
	var (
		codigo      string
		description firebird.Win1252 // CHARACTER SET ISO8859_1 — Win1252 boundary
		categoria   firebird.Win1252 // CHARACTER SET ISO8859_1 — Win1252 boundary
	)
	if err := s.Scan(&codigo, &description, &categoria); err != nil {
		return nil, err
	}
	p := domain.HydratePermiso(domain.Permission(codigo), string(description), string(categoria))
	return &p, nil
}
