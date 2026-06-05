-- ============================================================================
-- Migración 000026: FIREBASE_UID nullable + columna ESTATUS en MSP_USUARIOS
-- ============================================================================
--
-- Por qué FIREBASE_UID pasa a nullable:
--   Los vendedores que existen sólo en Firestore (catálogo de camionetas de
--   venta) nunca se autentican vía Firebase; no tienen cuenta de correo en el
--   proyecto Firebase. Se crearán filas en MSP_USUARIOS con FIREBASE_UID = NULL
--   y ESTATUS = 'VENDEDOR_ONLY' para que las tablas de ventas puedan
--   referenciarlos por USUARIO_ID sin depender de un uid de Firebase.
--   La restricción UQ_MSP_USUARIOS_FIREBASE_UID sigue vigente: en SQL92 un
--   índice único permite múltiples NULLs, por lo que no hay conflicto.
--
-- Por qué se agrega ESTATUS:
--   Un marcador explícito evita depender de `FIREBASE_UID IS NULL` como flag
--   implícito. Los valores iniciales son:
--     'FIREBASE_USER'  — usuario que se autentica normalmente vía Firebase.
--     'VENDEDOR_ONLY'  — vendedor sin cuenta Firebase; acceso sólo de catálogo.
--   Esto permite promover un VENDEDOR_ONLY a FIREBASE_USER en el futuro si el
--   vendedor llega a crear cuenta, sin alterar el esquema.
-- ============================================================================

ALTER TABLE MSP_USUARIOS ALTER COLUMN FIREBASE_UID DROP NOT NULL;
COMMIT;

ALTER TABLE MSP_USUARIOS ADD ESTATUS VARCHAR(20) CHARACTER SET ASCII;
COMMIT;

UPDATE MSP_USUARIOS SET ESTATUS = 'FIREBASE_USER';
COMMIT;

ALTER TABLE MSP_USUARIOS ALTER COLUMN ESTATUS SET NOT NULL;
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (26, '000026_usuarios_estatus_firebase_uid_nullable', CURRENT_TIMESTAMP);
COMMIT;
