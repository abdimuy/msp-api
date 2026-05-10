-- ============================================================================
-- Rollback de la migración 000001: AUTH
-- ============================================================================
-- ⚠️  DESTRUCTIVO. Borra usuarios, roles, permisos y todas sus asignaciones.
--    Si hay tablas dependientes (ventas, etc.) que referencian usuarios,
--    los FKs van a impedir estos DROPs. Bajar primero las migraciones
--    posteriores.
-- ============================================================================

DROP INDEX IDX_MSP_ROLES_PERMISOS_PERMISO;
DROP TABLE MSP_ROLES_PERMISOS;

DROP INDEX IDX_MSP_USUARIOS_ROLES_ROL;
DROP TABLE MSP_USUARIOS_ROLES;

DROP INDEX IDX_MSP_PERMISOS_CATEGORIA;
DROP TABLE MSP_PERMISOS;

DROP INDEX IDX_MSP_ROLES_ACTIVO;
DROP TABLE MSP_ROLES;

DROP INDEX IDX_MSP_USUARIOS_ACTIVO;
DROP INDEX IDX_MSP_USUARIOS_ALMACEN;
DROP TABLE MSP_USUARIOS;

DROP TABLE MSP_MIGRATIONS;

COMMIT;
