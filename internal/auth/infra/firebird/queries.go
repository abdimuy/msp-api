// Package firebird contains the auth module's Firebird repository
// implementations of the outbound ports. Every SQL statement lives in this
// file as a named const so it is easy to grep, audit, and verify against the
// migrations-firebird/000001_create_auth_tables.up.sql schema.
//
// SQL identifiers carry the Spanish column names from the Microsip schema,
// so the misspell linter is suppressed on the const blocks that reference
// columns whose names overlap with English-language misspellings.
package firebird

// ─── Usuario ───────────────────────────────────────────────────────────────

const insertUsuario = `
INSERT INTO MSP_USUARIOS
    (ID, FIREBASE_UID, EMAIL, NOMBRE, TELEFONO, ALMACEN_ID, ACTIVO, ESTATUS,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

const updateUsuario = `
UPDATE MSP_USUARIOS
SET FIREBASE_UID = ?,
    EMAIL = ?,
    NOMBRE = ?,
    TELEFONO = ?,
    ALMACEN_ID = ?,
    ACTIVO = ?,
    ESTATUS = ?,
    UPDATED_AT = ?,
    UPDATED_BY = ?
WHERE ID = ?`

const usuarioColumns = `ID, FIREBASE_UID, EMAIL, NOMBRE, TELEFONO, ALMACEN_ID, ACTIVO, ESTATUS,
       CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const selectUsuarioByID = `
SELECT ` + usuarioColumns + `
FROM MSP_USUARIOS WHERE ID = ?`

const selectUsuarioByFirebaseUID = `
SELECT ` + usuarioColumns + `
FROM MSP_USUARIOS WHERE FIREBASE_UID = ?`

const selectUsuarioByEmail = `
SELECT ` + usuarioColumns + `
FROM MSP_USUARIOS WHERE EMAIL = ?`

const selectUsuariosFirstPage = `
SELECT FIRST ? ` + usuarioColumns + `
FROM MSP_USUARIOS
ORDER BY CREATED_AT, ID`

const selectUsuariosAfterCursor = `
SELECT FIRST ? ` + usuarioColumns + `
FROM MSP_USUARIOS
WHERE (CREATED_AT > ?) OR (CREATED_AT = ? AND ID > ?)
ORDER BY CREATED_AT, ID`

// ─── Usuario ↔ Rol ─────────────────────────────────────────────────────────

const insertUsuarioRol = `
INSERT INTO MSP_USUARIOS_ROLES
    (USUARIO_ID, ROL_ID, CREATED_AT, CREATED_BY)
VALUES (?, ?, ?, ?)`

const deleteUsuarioRol = `
DELETE FROM MSP_USUARIOS_ROLES WHERE USUARIO_ID = ? AND ROL_ID = ?`

//nolint:misspell // DESCRIPCION is a Spanish SQL column name, not "description".
const selectRolesForUsuario = `
SELECT r.ID, r.NOMBRE, r.DESCRIPCION, r.INMUTABLE, r.ACTIVO,
       r.CREATED_AT, r.UPDATED_AT, r.CREATED_BY, r.UPDATED_BY
FROM MSP_USUARIOS_ROLES ur
INNER JOIN MSP_ROLES r ON r.ID = ur.ROL_ID
WHERE ur.USUARIO_ID = ?
ORDER BY r.NOMBRE`

const selectPermisosForUsuario = `
SELECT DISTINCT rp.PERMISO_CODIGO
FROM MSP_USUARIOS_ROLES ur
INNER JOIN MSP_ROLES r ON r.ID = ur.ROL_ID AND r.ACTIVO = TRUE
INNER JOIN MSP_ROLES_PERMISOS rp ON rp.ROL_ID = ur.ROL_ID
WHERE ur.USUARIO_ID = ?
ORDER BY rp.PERMISO_CODIGO`

// ─── Rol ───────────────────────────────────────────────────────────────────

//nolint:misspell // DESCRIPCION is a Spanish SQL column name.
const insertRol = `
INSERT INTO MSP_ROLES
    (ID, NOMBRE, DESCRIPCION, INMUTABLE, ACTIVO,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

//nolint:misspell // DESCRIPCION is a Spanish SQL column name.
const updateRol = `
UPDATE MSP_ROLES
SET NOMBRE = ?,
    DESCRIPCION = ?,
    INMUTABLE = ?,
    ACTIVO = ?,
    UPDATED_AT = ?,
    UPDATED_BY = ?
WHERE ID = ?`

//nolint:misspell // DESCRIPCION is a Spanish SQL column name.
const rolColumns = `ID, NOMBRE, DESCRIPCION, INMUTABLE, ACTIVO,
       CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const selectRolByID = `
SELECT ` + rolColumns + `
FROM MSP_ROLES WHERE ID = ?`

const selectRolByNombre = `
SELECT ` + rolColumns + `
FROM MSP_ROLES WHERE NOMBRE = ?`

const selectRolesFirstPage = `
SELECT FIRST ? ` + rolColumns + `
FROM MSP_ROLES
ORDER BY CREATED_AT, ID`

const selectRolesAfterCursor = `
SELECT FIRST ? ` + rolColumns + `
FROM MSP_ROLES
WHERE (CREATED_AT > ?) OR (CREATED_AT = ? AND ID > ?)
ORDER BY CREATED_AT, ID`

// ─── Rol ↔ Permiso ─────────────────────────────────────────────────────────

const insertRolPermiso = `
INSERT INTO MSP_ROLES_PERMISOS
    (ROL_ID, PERMISO_CODIGO, CREATED_AT, CREATED_BY)
VALUES (?, ?, ?, ?)`

const deleteRolPermiso = `
DELETE FROM MSP_ROLES_PERMISOS WHERE ROL_ID = ? AND PERMISO_CODIGO = ?`

const deleteAllRolPermisos = `
DELETE FROM MSP_ROLES_PERMISOS WHERE ROL_ID = ?`

const selectPermisosForRol = `
SELECT PERMISO_CODIGO
FROM MSP_ROLES_PERMISOS
WHERE ROL_ID = ?
ORDER BY PERMISO_CODIGO`

// ─── Permiso (catálogo) ────────────────────────────────────────────────────

//nolint:misspell // DESCRIPCION is a Spanish SQL column name.
const upsertPermiso = `
UPDATE OR INSERT INTO MSP_PERMISOS (CODIGO, DESCRIPCION, CATEGORIA)
VALUES (?, ?, ?) MATCHING (CODIGO)`

//nolint:misspell // DESCRIPCION is a Spanish SQL column name.
const selectPermisoByCodigo = `
SELECT CODIGO, DESCRIPCION, CATEGORIA
FROM MSP_PERMISOS WHERE CODIGO = ?`

//nolint:misspell // DESCRIPCION is a Spanish SQL column name.
const selectAllPermisos = `
SELECT CODIGO, DESCRIPCION, CATEGORIA
FROM MSP_PERMISOS
ORDER BY CODIGO`

const selectAllPermisoCodigos = `
SELECT CODIGO FROM MSP_PERMISOS ORDER BY CODIGO`
