-- Revert 000026: drop ESTATUS column and restore NOT NULL on FIREBASE_UID.
--
-- ADVERTENCIA: el paso 2 (SET NOT NULL en FIREBASE_UID) fallará si existen
-- filas con FIREBASE_UID = NULL (p. ej. vendedores creados con ESTATUS =
-- 'VENDEDOR_ONLY'). Esos datos no pueden recuperarse. Este down sólo es
-- seguro en entornos de desarrollo donde no se hayan insertado esas filas.

ALTER TABLE MSP_USUARIOS DROP ESTATUS;
COMMIT;

-- Fallará si hay filas con FIREBASE_UID IS NULL. Esperado — ver nota arriba.
ALTER TABLE MSP_USUARIOS ALTER COLUMN FIREBASE_UID SET NOT NULL;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 26;
COMMIT;
