-- ============================================================================
-- Migración 000047: MSP_VISITAS (tabla legacy preexistente del backend Node)
-- ============================================================================
--
-- Por qué:
--   El módulo internal/visitas migra a Go la captura de "visitas" de cobranza
--   (registro de que un cobrador tocó la puerta de un cliente, con o sin pago)
--   que hoy vive en el backend Node legacy. MSP_VISITAS YA EXISTE en test/prod
--   con ~226k filas creadas por ese backend — esta NO es una tabla greenfield.
--
--   Sus columnas de texto son CHARACTER SET NONE pero contienen bytes UTF-8
--   válidos (verificado empíricamente contra la data real). El writer Go de
--   este módulo debe enlazar (bind) UTF-8 plano — NO usar firebird.Win1252 /
--   firebird.EncodeWin1252 contra esta tabla (ver
--   docs/module-standards/ENCODING_HANDLING.md; mismo criterio que
--   ARTICULOS/CLIENTES.NOMBRE, ver referencia interna
--   reference_firebird_legacy_nombre_plain_string).
--
--   MSP_VISITAS NO tiene columnas CREATED_AT/UPDATED_AT. Esto es una
--   DESVIACIÓN INTENCIONAL y ACOTADA a esta tabla legacy respecto a la
--   convención greenfield de MSP_* (CLAUDE.md §1 exige ID/timestamps pasados
--   desde Go, pero no exige que TODA tabla tenga columnas de auditoría — aquí
--   simplemente no existen porque el backend Node nunca las modeló). La
--   entidad de dominio Go (internal/visitas/domain.Visita) igual embebe
--   audit.Auditable por consistencia de familia (Tipo A CRUD), pero esos
--   timestamps son SOLO EN MEMORIA: el repositorio (Task 2 de este plan) no
--   los persiste ni los lee de ninguna columna.
--
--   Esta migración crea la tabla SOLO SI NO EXISTE (chequeo contra
--   RDB$RELATIONS). En test/prod, donde la tabla legacy ya existe, el bloque
--   es un no-op y la tabla legacy queda intacta. En un entorno nuevo/vacío
--   (dev limpio, CI), crea la tabla con EXACTAMENTE las 11 columnas de
--   negocio que el backend Node definió, para que los INSERTs del writer Go
--   sean idénticos en ambos tipos de entorno.
--
-- Columnas (igual en ambos entornos):
--   ID                 CHAR(36)          PK, UUID generado en Go (uuid.New()),
--                                         idempotency key end-to-end.
--   COBRADOR           VARCHAR(150)      nombre libre del cobrador, requerido.
--   COBRADOR_ID        INTEGER           id Microsip del cobrador, opcional.
--   FECHA              TIMESTAMP         instante de negocio de la visita
--                                         (capturado por el cliente/app),
--                                         requerido.
--   FORMA_COBRO_ID     INTEGER           forma de cobro Microsip, opcional.
--   LAT / LNG          DOUBLE PRECISION  coordenadas de captura, opcionales.
--   NOTA               VARCHAR(10000)    nota libre del cobrador, opcional.
--   TIPO_VISITA        VARCHAR(100)      tipo de visita, requerido.
--   ZONA_CLIENTE_ID    INTEGER           zona del cliente al momento, opcional.
--   CLIENTE_ID         INTEGER           cliente Microsip visitado, requerido.
--   IMPTE_DOCTO_CC_ID  INTEGER           importe/pago Microsip asociado a la
--                                         visita, opcional (NULL si la visita
--                                         no derivó en cobro aplicado).
--
-- Restricciones (CLAUDE.md §1):
--   Sin DEFAULT, sin CREATED_AT/UPDATED_AT, sin triggers, sin procedimientos,
--   sin generadores, sin CHECK de negocio. IDs y timestamps se pasan desde Go.
-- ============================================================================

SET TERM ^ ;

EXECUTE BLOCK
AS
BEGIN
  IF (NOT EXISTS (
    SELECT 1 FROM RDB$RELATIONS
    WHERE RDB$RELATION_NAME = 'MSP_VISITAS'
  )) THEN
  BEGIN
    EXECUTE STATEMENT '
      CREATE TABLE MSP_VISITAS (
        ID                 CHAR(36)          CHARACTER SET ASCII  NOT NULL,
        COBRADOR           VARCHAR(150)      CHARACTER SET UTF8   NOT NULL,
        COBRADOR_ID        INTEGER,
        FECHA              TIMESTAMP                              NOT NULL,
        FORMA_COBRO_ID     INTEGER,
        LAT                DOUBLE PRECISION,
        LNG                DOUBLE PRECISION,
        NOTA               VARCHAR(10000)    CHARACTER SET UTF8,
        TIPO_VISITA        VARCHAR(100)      CHARACTER SET UTF8   NOT NULL,
        ZONA_CLIENTE_ID    INTEGER,
        CLIENTE_ID         INTEGER                                NOT NULL,
        IMPTE_DOCTO_CC_ID  INTEGER,
        CONSTRAINT PK_MSP_VISITAS PRIMARY KEY (ID)
      )';
  END
END^

SET TERM ; ^

COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (47, '000047_create_msp_visitas', CURRENT_TIMESTAMP);
COMMIT;
