-- ============================================================================
-- Migración 000050: MSP_GA_GARANTIA + MSP_GA_ARTICULO + MSP_GA_EVENTO +
--                    MSP_GA_IMAGEN + GEN_MSP_GA_FOLIO
-- ============================================================================
--
-- Por qué:
--   El módulo de garantías da seguimiento a un artículo desde que se reporta
--   defectuoso hasta su resolución final, sin perder de vista en qué etapa del
--   proceso va ni dónde está físicamente. Cuatro tablas materializan esto:
--   el folio (MSP_GA_GARANTIA), el artículo bajo custodia con ciclo de vida
--   propio (MSP_GA_ARTICULO), la línea de tiempo inmutable (MSP_GA_EVENTO) y
--   la evidencia fotográfica (MSP_GA_IMAGEN). Garantías es un módulo sellado
--   (ADR-0009): estas tablas no llevan llave foránea hacia ninguna tabla
--   fuera del módulo, y CLIENTE_ID/VENTA_ID/ARTICULO_ID se guardan como
--   enteros opacos sin FK hacia Microsip. Los cálculos, IDs y timestamps
--   viven en Go (CLAUDE.md §1).
--
-- MSP_GA_GARANTIA:
--   Un folio por reclamo de garantía. PK = UUID CHAR(36) ASCII generado en
--   Go. FOLIO es el identificador legible en campo (formato GA-000123),
--   asignado con GEN_MSP_GA_FOLIO. ORIGEN distingue piso (sin cliente ni
--   venta) de cliente. ESTADO_CUENTA solo aplica a origen cliente. ESTADO es
--   el estado del folio (abierto → en_proceso → listo_entrega → entregado →
--   cerrado, con cancelado como salida terminal alterna). El domicilio
--   (CALLE..GPS_LON) es un snapshot tomado al abrir el folio, no una copia
--   perezosa: si el cliente se muda después, el folio conserva a dónde se
--   fue a recoger el mueble. VIGENCIA_HASTA la captura el operador; el
--   sistema no calcula ni valida ninguna política de plazos todavía.
--
-- MSP_GA_ARTICULO:
--   La cosa física bajo custodia, con ciclo de vida propio — no un campo de
--   texto del folio. PK = UUID CHAR(36) ASCII. ROL distingue el artículo
--   original del que lo sustituye tras un cambio físico. ETAPA y UBICACION
--   son columnas distintas a propósito: ETAPA es dónde va el artículo en el
--   proceso (registrado, en_revision, dictamen_recibido, etc.) y UBICACION
--   es dónde está físicamente (domicilio_cliente, taller, proveedor, etc.).
--   Un artículo puede estar en etapa dictamen_recibido y ubicación proveedor
--   porque todavía no lo regresan — son dos preguntas distintas que merecen
--   índice propio cada una, no deducirse una de la otra. Un cambio físico
--   crea una segunda fila con ROL='reemplazo': el original sigue su propio
--   camino hasta standby/segunda_mano/desarmado/merma mientras el reemplazo
--   va directo al cliente. Por eso el folio puede quedar cerrado mientras el
--   artículo original sigue en standby — son registros distintos bajo el
--   mismo folio. DICTAMEN solo aplica a la ruta proveedor; DESENLACE es el
--   resultado final cuando el artículo sale por el flujo paralelo.
--
-- MSP_GA_EVENTO:
--   La línea de tiempo del folio, inmutable. PK = UUID CHAR(36) ASCII.
--   ARTICULO_REF es nullable porque los eventos del folio (apertura, cambio
--   de estado de cuenta, cierre) no apuntan a un artículo en particular.
--   MSP_GA_EVENTO no tiene UPDATED_AT: un evento de auditoría no se edita;
--   si algo salió mal en campo se agrega un evento de corrección, nunca se
--   reescribe uno pasado. ETAPA_DESDE/ETAPA_HASTA se guardan explícitamente
--   para que el histórico no dependa de recalcular desde el origen si el
--   catálogo de etapas cambia de nombre. ROL_DECISOR es nullable y solo se
--   llena en eventos de decisión (p. ej. quién autorizó un cambio físico).
--   DEVICE_CREATED_AT es el reloj del teléfono del cobrador (offline-first);
--   CREATED_AT es el reloj del servidor al recibir. CLAVE_IDEMPOTENCIA es el
--   UUID que genera el propio teléfono: si el dispositivo sincroniza dos
--   veces por mala señal, el UNIQUE rechaza el duplicado sin que el cobrador
--   vea un error.
--
-- MSP_GA_IMAGEN:
--   Evidencia fotográfica de un evento — cuelga del evento, no del folio.
--   PK = UUID CHAR(36) ASCII. La llave foránea hacia MSP_GA_EVENTO va SIN
--   ON DELETE CASCADE: las fotos son la evidencia de en qué estado se
--   recogió el mueble y no se borran en cascada bajo ninguna circunstancia.
--   RUTA (no URL) porque el blob vive en filesystem local bajo STORAGE_DIR
--   (ADR-0003), y en UTF8 porque una ruta con acentos no debe reventar.
--
-- Borrado:
--   No hay borrado, ni físico ni lógico, en ninguna de las cuatro tablas.
--   Un folio abierto por error se cancela (ESTADO='cancelado', con su propio
--   evento y motivo) — es un hecho del negocio, no una eliminación.
--
-- Restricciones:
--   Sin DEFAULT en ninguna columna — IDs, timestamps y folios se pasan desde
--   Go (CLAUDE.md §1). Sin triggers, sin procedimientos, sin CHECK de reglas
--   de negocio. Única excepción, contemplada explícitamente por la regla:
--   GEN_MSP_GA_FOLIO, un generador propio del módulo para asignación atómica
--   de folio — es infraestructura, no lógica de negocio, y no se comparte
--   con ningún otro módulo.
-- ============================================================================

CREATE GENERATOR GEN_MSP_GA_FOLIO;

CREATE TABLE MSP_GA_GARANTIA (
  ID               CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  FOLIO            VARCHAR(12)   CHARACTER SET ASCII   NOT NULL,
  ORIGEN           VARCHAR(10)   CHARACTER SET ASCII   NOT NULL,
  CLIENTE_ID       INTEGER,
  VENTA_ID         INTEGER,
  ESTADO_CUENTA    VARCHAR(20)   CHARACTER SET ASCII,
  ESTADO           VARCHAR(24)   CHARACTER SET ASCII   NOT NULL,
  DESCRIPCION      BLOB SUB_TYPE TEXT CHARACTER SET UTF8 NOT NULL,
  VIGENCIA_HASTA   DATE,
  CALLE            VARCHAR(300)  CHARACTER SET UTF8,
  NUMERO_EXTERIOR  VARCHAR(20)   CHARACTER SET UTF8,
  COLONIA          VARCHAR(100)  CHARACTER SET UTF8,
  LOCALIDAD        VARCHAR(100)  CHARACTER SET UTF8,
  CIUDAD           VARCHAR(100)  CHARACTER SET UTF8,
  CODIGO_POSTAL    VARCHAR(10)   CHARACTER SET UTF8,
  GPS_LAT          DOUBLE PRECISION,
  GPS_LON          DOUBLE PRECISION,
  ABIERTO_POR      VARCHAR(64)   CHARACTER SET UTF8    NOT NULL,
  CERRADO_EN       TIMESTAMP,
  CREATED_AT       TIMESTAMP                           NOT NULL,
  UPDATED_AT       TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_GA_GARANTIA       PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_GA_GARANTIA_FOLIO UNIQUE (FOLIO)
);

CREATE TABLE MSP_GA_ARTICULO (
  ID           CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  GARANTIA_ID  CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  ROL          VARCHAR(12)   CHARACTER SET ASCII   NOT NULL,
  ARTICULO_ID  INTEGER,
  CLAVE        VARCHAR(30)   CHARACTER SET UTF8,
  DESCRIPCION  VARCHAR(300)  CHARACTER SET UTF8    NOT NULL,
  RUTA         VARCHAR(12)   CHARACTER SET ASCII,
  ETAPA        VARCHAR(28)   CHARACTER SET ASCII   NOT NULL,
  UBICACION    VARCHAR(28)   CHARACTER SET ASCII   NOT NULL,
  DICTAMEN     VARCHAR(16)   CHARACTER SET ASCII,
  DESENLACE    VARCHAR(16)   CHARACTER SET ASCII,
  CERRADO_EN   TIMESTAMP,
  CREATED_AT   TIMESTAMP                           NOT NULL,
  UPDATED_AT   TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_GA_ARTICULO     PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_GA_ARTICULO_GAR FOREIGN KEY (GARANTIA_ID) REFERENCES MSP_GA_GARANTIA (ID)
);

CREATE TABLE MSP_GA_EVENTO (
  ID                  CHAR(36)      CHARACTER SET ASCII            NOT NULL,
  GARANTIA_ID         CHAR(36)      CHARACTER SET ASCII            NOT NULL,
  ARTICULO_REF        CHAR(36)      CHARACTER SET ASCII,
  TIPO                VARCHAR(28)   CHARACTER SET ASCII            NOT NULL,
  DESCRIPCION         BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  ETAPA_DESDE         VARCHAR(28)   CHARACTER SET ASCII,
  ETAPA_HASTA         VARCHAR(28)   CHARACTER SET ASCII,
  USUARIO             VARCHAR(64)   CHARACTER SET UTF8             NOT NULL,
  ROL_DECISOR         VARCHAR(16)   CHARACTER SET ASCII,
  GPS_LAT             DOUBLE PRECISION,
  GPS_LON             DOUBLE PRECISION,
  CREATED_AT          TIMESTAMP                                    NOT NULL,
  DEVICE_CREATED_AT   TIMESTAMP                                    NOT NULL,
  CLAVE_IDEMPOTENCIA  CHAR(36)      CHARACTER SET ASCII            NOT NULL,

  CONSTRAINT PK_MSP_GA_EVENTO     PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_GA_EVENTO_GAR FOREIGN KEY (GARANTIA_ID) REFERENCES MSP_GA_GARANTIA (ID),
  CONSTRAINT UQ_MSP_GA_EVENTO_CLAVE UNIQUE (CLAVE_IDEMPOTENCIA)
);

CREATE TABLE MSP_GA_IMAGEN (
  ID           CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  EVENTO_ID    CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  RUTA         VARCHAR(500)  CHARACTER SET UTF8    NOT NULL,
  DESCRIPCION  VARCHAR(500)  CHARACTER SET UTF8,
  SUBIDA_POR   VARCHAR(64)   CHARACTER SET UTF8    NOT NULL,
  CREATED_AT   TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_GA_IMAGEN     PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_GA_IMAGEN_EVT FOREIGN KEY (EVENTO_ID) REFERENCES MSP_GA_EVENTO (ID)
);

-- ─── Índices ────────────────────────────────────────────────────────────────
-- Indexado sobre lo que consulta el tablero operativo: etapa y ubicación del
-- artículo, y estado/fecha del folio.

CREATE INDEX IDX_MSP_GA_GARANTIA_CLIENTE ON MSP_GA_GARANTIA (CLIENTE_ID);
CREATE INDEX IDX_MSP_GA_GARANTIA_ESTADO  ON MSP_GA_GARANTIA (ESTADO, CREATED_AT);
CREATE INDEX IDX_MSP_GA_GARANTIA_CREATED ON MSP_GA_GARANTIA (CREATED_AT);
CREATE INDEX IDX_MSP_GA_ARTICULO_GAR     ON MSP_GA_ARTICULO (GARANTIA_ID);
CREATE INDEX IDX_MSP_GA_ARTICULO_ETAPA   ON MSP_GA_ARTICULO (ETAPA);
CREATE INDEX IDX_MSP_GA_ARTICULO_UBIC    ON MSP_GA_ARTICULO (UBICACION);
CREATE INDEX IDX_MSP_GA_EVENTO_GAR ON MSP_GA_EVENTO (GARANTIA_ID, DEVICE_CREATED_AT);
CREATE INDEX IDX_MSP_GA_IMAGEN_EVT       ON MSP_GA_IMAGEN (EVENTO_ID);

-- ─── Registro ────────────────────────────────────────────────────────────────
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (50, '000050_create_msp_ga_garantias', CURRENT_TIMESTAMP);

COMMIT;
