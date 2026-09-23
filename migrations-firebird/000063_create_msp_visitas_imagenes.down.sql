-- ============================================================================
-- Rollback de la migración 000063: MSP_VISITAS_IMAGENES
-- ============================================================================
--
-- MSP_VISITAS_IMAGENES SÍ es una tabla greenfield creada por el up.sql de esta
-- migración (a diferencia de MSP_VISITAS, que es legacy y cuyo down lleva una
-- advertencia grande). Por lo tanto este rollback puede borrarla sin destruir
-- datos que la migración no haya creado.
--
-- OJO: los blobs en disco bajo STORAGE_DIR/visitas/ NO se tocan aquí. Al
-- borrar las filas quedan huérfanos en el filesystem; hay que limpiarlos a
-- mano si el rollback es definitivo.
--
-- DROP TABLE ya elimina la PK, la FK y el índice que Firebird creó para ella,
-- así que no hay DROP INDEX ni DROP CONSTRAINT: si el up hubiera fallado a la
-- mitad, intentar dropear un objeto que nunca se creó abortaría el rollback.
-- El bloque dropea SOLO SI EXISTE, espejando la idempotencia del up.
-- ============================================================================

SET TERM ^ ;

EXECUTE BLOCK
AS
BEGIN
  IF (EXISTS (
    SELECT 1 FROM RDB$RELATIONS
    WHERE RDB$RELATION_NAME = 'MSP_VISITAS_IMAGENES'
  )) THEN
    EXECUTE STATEMENT 'DROP TABLE MSP_VISITAS_IMAGENES';
END^

SET TERM ; ^

COMMIT;

DELETE FROM MSP_MIGRATIONS WHERE ID = 63;

COMMIT;
