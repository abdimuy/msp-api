-- ============================================================================
-- Migración 000062: MSP_FLOTA_ASIGNACION_ACTUAL + MSP_FLOTA_ASIGNACION_CAMBIOS
--                   + MSP_FLOTA_FOTOS
-- ============================================================================
--
-- Por qué:
--   Quién maneja cada camioneta vive en Firestore, colección `users`, campo
--   CAMIONETA_ASIGNADA (entero = ALMACENES.ALMACEN_ID). Se cambia desde la
--   pantalla del escritorio (AsignacionAlmacenes.tsx hace un updateDoc y nada
--   más), desde la app Android, y desde la consola de Firebase a mano.
--
--   **Ninguno de los tres deja rastro.** Medido contra el Firestore de
--   producción: no existe colección de bitácora, el documento del usuario no
--   tiene UPDATED_AT, y el `update_time` que Firestore mantiene solo NO SIRVE
--   porque la app reescribe FECHA_VERSION_APP cada vez que se abre — el
--   update_time coincide al milisegundo con ese campo. Es decir: el reloj que
--   trae Firestore mide "cuándo se abrió la app", no "cuándo se movió a
--   alguien de camioneta".
--
--   Consecuencia operativa: cuando la oficina reporta "el vendedor de esta
--   venta no es el que está asignado", no hay forma de saber si la asignación
--   cambió DESPUÉS de la venta. Estas tres tablas cierran eso.
--
-- Cómo, y qué precisión da:
--   Un worker (flota-snapshot-worker) fotografía la colección completa cada
--   15 minutos y COMPARA VALORES contra la última foto guardada. No se fía de
--   update_time — por lo dicho arriba — ni de ningún gancho en la pantalla,
--   porque un gancho no vería los cambios hechos a mano en la consola de
--   Firebase, que son justo los que hoy no dejan rastro.
--
--   La consecuencia honesta: la foto solo puede ACOTAR el cambio a la ventana
--   entre dos fotos. No sabe el segundo exacto y no sabe quién lo hizo. Por
--   eso la columna se llama DETECTADO_EN y no CAMBIADO_EN, y por eso viaja
--   VENTANA_DESDE al lado: el cambio ocurrió en algún momento del intervalo
--   (VENTANA_DESDE, DETECTADO_EN]. Nombrar la columna CAMBIADO_EN sería
--   afirmar una precisión que el método no tiene.
--
--   Y una segunda pérdida, también por diseño: dos cambios dentro de la misma
--   ventana (A→B→C entre dos fotos) se registran como UNO SOLO, A→C. El paso
--   intermedio por B no existe para este sistema. Subir la frecuencia reduce
--   la probabilidad, nunca la elimina.
--
-- ── MSP_FLOTA_ASIGNACION_ACTUAL ─────────────────────────────────────────────
--   El último estado observado, una fila por usuario del roster. Es el
--   "antes" contra el que se compara la siguiente foto; no es un catálogo ni
--   una fuente de verdad — Firestore lo sigue siendo. Si alguien la vacía, el
--   sistema no inventa cambios: la línea base se decide con MSP_FLOTA_FOTOS
--   (ver abajo), no con que esta tabla esté vacía.
--
--   USUARIO_UID es la PK: el id del documento de Firestore (uid de Firebase).
--   Medido en el proyecto de desarrollo: 62 documentos, el uid más largo de
--   28 caracteres. VARCHAR(128) ASCII deja margen de sobra y mantiene la
--   llave del índice corta; un uid de Firebase es alfanumérico ASCII, no
--   texto de usuario.
--
--   CAMIONETA_ID es NULLABLE a propósito y NULL significa exactamente una
--   cosa: "el roster no le asigna camioneta". Cubre los tres casos que en
--   Firestore se ven distintos y significan lo mismo — campo ausente (26 de
--   los 62 documentos), campo presente con valor null (1 documento), y valor
--   no positivo. Sin FK hacia ALMACENES: el valor viene de un sistema externo
--   que no valida nada, y una FK convertiría un dato sucio en una caída del
--   worker en lugar de en una fila que se puede mirar.
--
-- ── MSP_FLOTA_ASIGNACION_CAMBIOS ────────────────────────────────────────────
--   La bitácora, append-only. Una fila por transición de camioneta detectada.
--
--   NO tiene llave foránea hacia MSP_FLOTA_ASIGNACION_ACTUAL, y es
--   deliberado: cuando un usuario desaparece del roster su fila de ACTUAL se
--   borra, y su historia tiene que sobrevivir a eso. Una FK (con o sin
--   cascade) borraría justo la evidencia de la baja.
--
--   TIPO distingue las cuatro transiciones, que tienen causas distintas y no
--   deben colapsarse en "cambió":
--     'asignacion'   — no tenía camioneta y ahora tiene (incluye al usuario
--                      nuevo que aparece ya con camioneta).
--     'reasignacion' — tenía la A y ahora tiene la B.
--     'retiro'       — tenía camioneta y se le quitó (el campo se borró, se
--                      puso en null o en cero). El usuario sigue existiendo.
--     'baja_usuario' — el documento entero desapareció del roster teniendo
--                      camioneta. Nadie "le quitó la camioneta": se fue.
--   Un usuario que entra o sale del roster SIN camioneta no genera fila aquí
--   —no pasó nada relativo a camionetas— pero sí se refleja en ACTUAL.
--
--   CAMIONETA_ANTERIOR / CAMIONETA_NUEVA nullable por lo mismo que arriba:
--   NULL es "sin camioneta", que es un extremo legítimo de tres de las cuatro
--   transiciones.
--
--   EMAIL y NOMBRE se copian tal como se observaron en esa foto. Es un
--   snapshot, no una referencia: si la persona cambia de nombre después, la
--   fila histórica conserva con qué nombre se le vio ese día.
--
--   El CHECK sobre TIPO es defensa en profundidad y nada más. La regla
--   canónica vive en internal/flota/domain (TipoCambio.Valido), como manda
--   CLAUDE.md §1.
--
-- ── MSP_FLOTA_FOTOS ─────────────────────────────────────────────────────────
--   Una fila por ejecución exitosa del worker. Existe por dos razones, y
--   ninguna es decorativa:
--
--   1. Es de donde sale VENTANA_DESDE. El límite inferior del cambio es
--      cuándo corrió la foto ANTERIOR, no cuándo se vio por última vez a ese
--      usuario en ese estado (que podrían ser semanas y sería un límite
--      inútil).
--
--   2. Es el control positivo. Sin ella, "no hubo cambios este mes" y "el
--      worker lleva un mes muerto" producen exactamente la misma tabla vacía
--      de cambios. Con ella, la ausencia de cambios es un hallazgo y no una
--      suposición: se puede demostrar que la consulta HABRÍA encontrado la
--      fila porque las fotos siguieron corriendo.
--
--   ES_LINEA_BASE marca la primera ejecución: la que solo retrata el estado
--   inicial y NO emite cambios, porque no tiene contra qué comparar. Sin esa
--   marca, el primer día generaría un aluvión de falsos "cambios" —una fila
--   por persona— que además serían mentira: nadie movió nada, simplemente
--   empezamos a mirar.
--
--   Crecimiento: 96 fotos al día (una cada 15 min) ≈ 35 mil filas al año, de
--   ~40 bytes útiles cada una. Alrededor de 1.5 MB anuales. No lleva janitor
--   a propósito: purgarla destruiría el control positivo del punto 2, que es
--   justo lo que la hace valer.
--
-- Sin DEFAULT, sin trigger, sin generador y sin CURRENT_TIMESTAMP en las tres
--   tablas: los UUID salen de uuid.New() y los timestamps de clock.Now()
--   envueltos en firebird.ToWallClock, por la regla dura #1 de CLAUDE.md. El
--   único CURRENT_TIMESTAMP del archivo es el del registro en MSP_MIGRATIONS,
--   que es como se escriben todas las migraciones del repo.
--
-- Sin GRANT a PUBLIC (a diferencia de las migraciones 000051/052/053): esas
--   existen porque triggers colgados de tablas nativas de Microsip leen
--   nuestras tablas con los privilegios del usuario de Microsip. Aquí no hay
--   ningún trigger y Microsip no toca estas tablas — solo las escribe el API.
--
-- ORDEN DE DESPLIEGUE: esta migración va PRIMERO y puede ir sola. Las tablas
--   nacen vacías y nadie las lee todavía; el binario con el worker va después.
--   La primera ejecución del worker tras el despliegue escribe la línea base.
-- ============================================================================

CREATE TABLE MSP_FLOTA_ASIGNACION_ACTUAL (
  USUARIO_UID   VARCHAR(128) CHARACTER SET ASCII NOT NULL,
  EMAIL         VARCHAR(320) CHARACTER SET UTF8,
  NOMBRE        VARCHAR(200) CHARACTER SET UTF8,
  CAMIONETA_ID  INTEGER,
  CREATED_AT    TIMESTAMP NOT NULL,
  UPDATED_AT    TIMESTAMP NOT NULL,
  CONSTRAINT PK_MSP_FLOTA_ASIGNACION_ACTUAL PRIMARY KEY (USUARIO_UID)
);

COMMIT;

CREATE TABLE MSP_FLOTA_ASIGNACION_CAMBIOS (
  ID                  CHAR(36) CHARACTER SET ASCII NOT NULL,
  USUARIO_UID         VARCHAR(128) CHARACTER SET ASCII NOT NULL,
  EMAIL               VARCHAR(320) CHARACTER SET UTF8,
  NOMBRE              VARCHAR(200) CHARACTER SET UTF8,
  CAMIONETA_ANTERIOR  INTEGER,
  CAMIONETA_NUEVA     INTEGER,
  TIPO                VARCHAR(20) CHARACTER SET ASCII NOT NULL,
  VENTANA_DESDE       TIMESTAMP NOT NULL,
  DETECTADO_EN        TIMESTAMP NOT NULL,
  CREATED_AT          TIMESTAMP NOT NULL,
  CONSTRAINT PK_MSP_FLOTA_ASIGNACION_CAMBIOS PRIMARY KEY (ID),
  CONSTRAINT CK_MSP_FLOTA_CAMBIOS_TIPO CHECK (
    TIPO IN ('asignacion', 'reasignacion', 'retiro', 'baja_usuario')
  )
);

COMMIT;

-- El listado siempre es cronológico descendente ("qué se movió últimamente"),
-- de ahí el índice DESCENDING sobre DETECTADO_EN.
CREATE DESCENDING INDEX IDX_MSP_FLOTA_CAMBIOS_DETECTADO
  ON MSP_FLOTA_ASIGNACION_CAMBIOS (DETECTADO_EN);

COMMIT;

-- La pregunta que motiva el módulo es por persona ("¿a Eliseo cuándo lo
-- movieron?"), así que la columna de filtro va primero y la de orden después.
CREATE INDEX IDX_MSP_FLOTA_CAMBIOS_USUARIO
  ON MSP_FLOTA_ASIGNACION_CAMBIOS (USUARIO_UID, DETECTADO_EN);

COMMIT;

CREATE TABLE MSP_FLOTA_FOTOS (
  ID                  CHAR(36) CHARACTER SET ASCII NOT NULL,
  EJECUTADO_EN        TIMESTAMP NOT NULL,
  USUARIOS_OBSERVADOS INTEGER NOT NULL,
  CAMBIOS_DETECTADOS  INTEGER NOT NULL,
  ES_LINEA_BASE       SMALLINT NOT NULL,
  CREATED_AT          TIMESTAMP NOT NULL,
  CONSTRAINT PK_MSP_FLOTA_FOTOS PRIMARY KEY (ID),
  CONSTRAINT CK_MSP_FLOTA_FOTOS_LINEA_BASE CHECK (ES_LINEA_BASE IN (0, 1))
);

COMMIT;

-- Cada foto arranca leyendo la anterior (SELECT FIRST 1 ... ORDER BY
-- EJECUTADO_EN DESC) para fijar VENTANA_DESDE. El índice descendente hace de
-- esa lectura un solo salto al final del índice en lugar de un sort.
CREATE DESCENDING INDEX IDX_MSP_FLOTA_FOTOS_EJECUTADO
  ON MSP_FLOTA_FOTOS (EJECUTADO_EN);

COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (62, '000062_create_msp_flota_asignaciones', CURRENT_TIMESTAMP);

COMMIT;
