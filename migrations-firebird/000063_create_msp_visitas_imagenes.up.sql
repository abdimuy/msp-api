-- ============================================================================
-- Migración 000063: MSP_VISITAS_IMAGENES (fotos adjuntas a una visita)
-- ============================================================================
--
-- Por qué:
--   POST /v2/visitas pasa a aceptar fotos en la MISMA request (multipart), de
--   modo que visita + imágenes se persisten en una sola transacción y nunca
--   queda una imagen huérfana ni una visita sin su evidencia. En la calle la
--   segunda request es la que no llega; por eso no hay endpoint separado de
--   adjuntado.
--
--   La tabla espeja MSP_PAGOS_IMAGENES (migración 000016) columna por columna,
--   salvo el nombre de la FK y el tipo de VISITA_ID. Es una tabla MSP_*
--   greenfield, así que sigue CLAUDE.md §1 SIN excepciones: sin DEFAULT, sin
--   triggers, sin generadores, sin CHECK de negocio. ID, CREATED_AT,
--   UPDATED_AT, CREATED_BY y UPDATED_BY se pasan SIEMPRE desde Go.
--
-- Columnas:
--   ID            CHAR(36)      PK, UUID generado en Go (o enviado por el
--                                cliente como `id_<n>` para que un reintento
--                                no duplique la imagen).
--   VISITA_ID     CHAR(36)      FK → MSP_VISITAS(ID), ON DELETE CASCADE.
--   STORAGE_KIND  VARCHAR(20)   'FILESYSTEM' (único valor que escribe Go).
--   STORAGE_KEY   VARCHAR(500)  clave relativa bajo STORAGE_DIR; el convenio
--                                es "visitas/<visita_id>/<imagen_id><ext>".
--   MIME          VARCHAR(50)   tipo declarado, validado contra la whitelist
--                                del dominio (IsAllowedMime).
--   SIZE_BYTES    BIGINT        tamaño del blob.
--   DESCRIPCION   VARCHAR(200)  nota opcional por imagen (UTF8).
--   CREATED_AT / UPDATED_AT     TIMESTAMP, desde Go vía firebird.ToWallClock.
--   CREATED_BY / UPDATED_BY     CHAR(36), usuario que subió la imagen.
--
-- Sobre el CHARACTER SET de la FK:
--   MSP_VISITAS es legacy: en test/prod su columna ID es
--   CHAR(36) CHARACTER SET NONE (creada por el backend Node), mientras que en
--   un entorno recién sembrado la migración 000047 la crea como
--   CHAR(36) CHARACTER SET ASCII. VISITA_ID se declara ASCII y la FK se probó
--   empíricamente contra AMBAS formas del padre en MUEBLERA.FDB: la creación
--   de la constraint y un INSERT real con un ID existente funcionan en los dos
--   casos (un UUID es ASCII puro, así que los bytes coinciden).
--
-- Índice sobre VISITA_ID:
--   No se declara. Firebird crea automáticamente un índice para toda FOREIGN
--   KEY; añadir uno explícito duplicaría la estructura.
--
-- Idempotencia de esta migración:
--   El bloque crea la tabla SOLO SI NO EXISTE, igual que 000047, para que
--   re-aplicarla sobre una base que ya la tiene sea un no-op en lugar de un
--   error que aborta el resto del lote.
-- ============================================================================

SET TERM ^ ;

EXECUTE BLOCK
AS
BEGIN
  IF (NOT EXISTS (
    SELECT 1 FROM RDB$RELATIONS
    WHERE RDB$RELATION_NAME = 'MSP_VISITAS_IMAGENES'
  )) THEN
  BEGIN
    EXECUTE STATEMENT '
      CREATE TABLE MSP_VISITAS_IMAGENES (
        ID            CHAR(36)      CHARACTER SET ASCII  NOT NULL,
        VISITA_ID     CHAR(36)      CHARACTER SET ASCII  NOT NULL,
        STORAGE_KIND  VARCHAR(20)   CHARACTER SET ASCII  NOT NULL,
        STORAGE_KEY   VARCHAR(500)  CHARACTER SET ASCII  NOT NULL,
        MIME          VARCHAR(50)   CHARACTER SET ASCII  NOT NULL,
        SIZE_BYTES    BIGINT                             NOT NULL,
        DESCRIPCION   VARCHAR(200)  CHARACTER SET UTF8,
        CREATED_AT    TIMESTAMP                          NOT NULL,
        UPDATED_AT    TIMESTAMP                          NOT NULL,
        CREATED_BY    CHAR(36)      CHARACTER SET ASCII  NOT NULL,
        UPDATED_BY    CHAR(36)      CHARACTER SET ASCII  NOT NULL,
        CONSTRAINT PK_MSP_VISITAS_IMAGENES PRIMARY KEY (ID),
        CONSTRAINT FK_MSP_VISITAS_IMG_VISITA FOREIGN KEY (VISITA_ID)
          REFERENCES MSP_VISITAS(ID) ON DELETE CASCADE
      )';
  END
END^

SET TERM ; ^

COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (63, '000063_create_msp_visitas_imagenes', CURRENT_TIMESTAMP);

COMMIT;
