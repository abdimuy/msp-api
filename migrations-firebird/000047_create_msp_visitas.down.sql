-- MSP_VISITAS es una tabla legacy PREEXISTENTE en test/prod (~226k filas,
-- creada por el backend Node), no una tabla que esta migración necesariamente
-- creó ahí. Este down es seguro/idempotente por diseño: solo elimina la tabla
-- si existe (mismo patrón que 000028_create_gen_mst_folio.down.sql para
-- GEN_MST_FOLIO).
--
-- ADVERTENCIA: en test/prod el up.sql de esta migración fue un no-op (la
-- tabla ya existía), así que este down NO debe ejecutarse ahí — destruiría
-- ~226k filas de datos legacy reales que esta migración nunca creó. Usar
-- únicamente en entornos dev/CI recién sembrados donde el up.sql fue el que
-- efectivamente creó la tabla.

SET TERM ^ ;

EXECUTE BLOCK
AS
BEGIN
  IF (EXISTS (
    SELECT 1 FROM RDB$RELATIONS
    WHERE RDB$RELATION_NAME = 'MSP_VISITAS'
  )) THEN
    EXECUTE STATEMENT 'DROP TABLE MSP_VISITAS';
END^

SET TERM ; ^

COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 47;
COMMIT;
