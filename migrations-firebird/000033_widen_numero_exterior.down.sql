-- Revert 000033: restaurar MSP_VENTAS.NUMERO_EXTERIOR a VARCHAR(20).
--
-- ADVERTENCIA: fallará si existen filas con NUMERO_EXTERIOR de más de 20
-- caracteres (capturadas tras esta migración). Esos valores no caben en
-- VARCHAR(20) y Firebird rechazará el cambio. Sólo es seguro en desarrollo
-- sin datos largos. Recordá bajar también `maxNumeroExteriorLength` a 20.

ALTER TABLE MSP_VENTAS ALTER COLUMN NUMERO_EXTERIOR TYPE VARCHAR(20) CHARACTER SET UTF8;
COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 33;
COMMIT;
