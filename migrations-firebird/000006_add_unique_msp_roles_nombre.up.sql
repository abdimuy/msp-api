-- ============================================================================
-- Migración 000006: agregar UQ_MSP_ROLES_NOMBRE faltante
-- ============================================================================
-- La migración 000001 declara la constraint:
--   CONSTRAINT UQ_MSP_ROLES_NOMBRE UNIQUE (NOMBRE)
-- pero nunca quedó creada en el schema vivo de la DB dev. Probablemente la
-- migración fue editada después de aplicarse o la creación se silenció por
-- algún detalle del parser de Firebird con CHARACTER SET ISO8859_1.
--
-- Síntoma: TestRolRepo_Save_DuplicateNombre y TestRolRepo_Update_DuplicateNombre
-- pasaban inserts duplicados sin error (verificado con SELECT COUNT(*) =2
-- después del segundo INSERT). El repo.Save mapea el unique violation a
-- domain.ErrRolYaExiste vía mapUniqueViolation, pero nunca llegaba a
-- dispararse porque la constraint no existía.
--
-- Fix: ALTER TABLE para agregar la constraint faltante. Antes de crear el
-- constraint hay que asegurar que no haya duplicados en la tabla actual; en
-- la DB dev MSP_ROLES tiene pocos roles bootstrap, así que cualquier
-- colisión sale a la luz aquí (fallaría el ALTER) y debe resolverse a mano.
-- ============================================================================

ALTER TABLE MSP_ROLES
  ADD CONSTRAINT UQ_MSP_ROLES_NOMBRE UNIQUE (NOMBRE);

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (6, '000006_add_unique_msp_roles_nombre', CURRENT_TIMESTAMP);

COMMIT;
