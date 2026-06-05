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
//
// All text columns are CHARACTER SET UTF8 (migration 000005); the driver
// delivers UTF-8 Go strings directly.
func usuarioFromRow(s rowScanner) (*domain.Usuario, error) {
	var (
		idRaw, email               string
		fuid                       sql.NullString
		nombre                     string
		telefono                   sql.NullString
		almacenID                  sql.NullInt32
		activo                     bool
		estatus                    string
		createdAtRaw, updatedAtRaw any
		createdByRaw, updatedByRaw string
	)
	if err := s.Scan(
		&idRaw, &fuid, &email, &nombre, &telefono, &almacenID, &activo, &estatus,
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

	var firebaseUIDOpt domain.FirebaseUID
	if fuid.Valid {
		firebaseUIDOpt = domain.HydrateFirebaseUID(fuid.String)
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
		FirebaseUID: firebaseUIDOpt,
		Email:       domain.HydrateEmail(email),
		Nombre:      domain.HydrateNombre(nombre),
		Telefono:    telOpt,
		AlmacenID:   almacenOpt,
		Activo:      activo,
		Estatus:     domain.Estatus(estatus),
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
		nombre                     string
		descRaw                    sql.NullString
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

	var descOpt *string
	if descRaw.Valid {
		d := descRaw.String
		descOpt = &d
	}

	return domain.HydrateRol(domain.HydrateRolParams{
		ID:          id,
		Nombre:      nombre,
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
		description string
		categoria   string
	)
	if err := s.Scan(&codigo, &description, &categoria); err != nil {
		return nil, err
	}
	p := domain.HydratePermiso(domain.Permission(codigo), description, categoria)
	return &p, nil
}
