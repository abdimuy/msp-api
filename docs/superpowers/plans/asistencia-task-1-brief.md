# Asistencia — Tarea 1: migración `000048` + cifrado de plantillas

> **Rama:** `feat/asistencia-base` (ya creada, sacada de `feat/asistencia`)
> **Spec del módulo:** [`docs/superpowers/specs/2026-07-28-asistencia-design.md`](../specs/2026-07-28-asistencia-design.md)

## Dónde encaja

El módulo de asistencia registra la jornada laboral: a qué hora llega cada trabajador, a qué hora sale, cuánto trabajó y qué días faltó. Marca con huella en una PC con lector.

Esta tarea son **dos entregables independientes** que no dependen de nada más del módulo:

1. La migración con las diez tablas.
2. El cifrado de las plantillas biométricas.

Ninguno de los dos necesita que exista código Go del módulo. Se pueden hacer en cualquier orden.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras. En particular la **regla #1 (nada de lógica en la base de datos)** y la **#3 (código en inglés, mensajes de usuario en español)**.
2. `migrations-firebird/000046_create_msp_rx_conversacion.up.sql` — el modelo de encabezado, formato y cierre.
3. `migrations-firebird/000046_create_msp_rx_conversacion.down.sql` — el modelo de `down`. Fíjate en lo **corto** que es.
4. `docs/superpowers/specs/2026-07-28-asistencia-design.md` §3 — de dónde salen las tablas y por qué.
5. `internal/platform/apperror/` — cómo se construyen los errores en este proyecto.

---

# Entregable 1 — Migración `000048`

## Archivos

```
migrations-firebird/000048_create_msp_as_asistencia.up.sql
migrations-firebird/000048_create_msp_as_asistencia.down.sql
```

**El número 48 no es negociable.** La `000047` es del módulo de garantías. Antes de escribir, corre `ls migrations-firebird/` y confirma que la 48 está libre.

## Restricciones (CLAUDE.md regla #1)

- **Ningún `DEFAULT` en ninguna columna.** Ni `CURRENT_TIMESTAMP`, ni UUID por defecto. Todos los valores vienen desde Go.
- Sin triggers, sin procedimientos almacenados, sin generadores, sin `CHECK` de reglas de negocio.
- Permitido: `PRIMARY KEY`, `UNIQUE`, `NOT NULL`, `FOREIGN KEY`, índices, tipos de columna.
- UUIDs son `CHAR(36) CHARACTER SET ASCII`. Texto de usuario es `CHARACTER SET UTF8`. Timestamps son `TIMESTAMP` sin valor por defecto.

## DDL — usar exactamente esto

```sql
CREATE TABLE MSP_AS_SEDE (
  ID          CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  NOMBRE      VARCHAR(120)  CHARACTER SET UTF8    NOT NULL,
  CREATED_AT  TIMESTAMP                           NOT NULL,
  UPDATED_AT  TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_AS_SEDE PRIMARY KEY (ID)
);

CREATE TABLE MSP_AS_EMPLEADO (
  ID                  CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  NUMERO              VARCHAR(10)   CHARACTER SET ASCII   NOT NULL,
  NOMBRE              VARCHAR(200)  CHARACTER SET UTF8    NOT NULL,
  SEDE_ID             CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  REFERENCIA_EXTERNA  VARCHAR(64)   CHARACTER SET UTF8,
  FECHA_BAJA          DATE,
  CREATED_AT          TIMESTAMP                           NOT NULL,
  UPDATED_AT          TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_AS_EMPLEADO      PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AS_EMPLEADO_NUM  UNIQUE (NUMERO),
  CONSTRAINT FK_MSP_AS_EMPLEADO_SEDE FOREIGN KEY (SEDE_ID) REFERENCES MSP_AS_SEDE (ID)
);

CREATE TABLE MSP_AS_EQUIPO (
  ID                 CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  CLAVE              VARCHAR(40)   CHARACTER SET ASCII   NOT NULL,
  NOMBRE             VARCHAR(120)  CHARACTER SET UTF8    NOT NULL,
  SEDE_ID            CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  SECRETO_HASH       CHAR(64)      CHARACTER SET ASCII   NOT NULL,
  ULTIMO_CONTACTO    TIMESTAMP,
  DESFASE_RELOJ_SEG  INTEGER,
  CREATED_AT         TIMESTAMP                           NOT NULL,
  UPDATED_AT         TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_AS_EQUIPO       PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AS_EQUIPO_CLAVE UNIQUE (CLAVE),
  CONSTRAINT FK_MSP_AS_EQUIPO_SEDE  FOREIGN KEY (SEDE_ID) REFERENCES MSP_AS_SEDE (ID)
);

CREATE TABLE MSP_AS_PLANTILLA (
  ID           CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  EMPLEADO_ID  CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  DEDO         VARCHAR(20)  CHARACTER SET ASCII   NOT NULL,
  DATOS        BLOB SUB_TYPE BINARY               NOT NULL,
  ALGORITMO    VARCHAR(32)  CHARACTER SET ASCII   NOT NULL,
  CALIDAD      SMALLINT,
  ENROLADA_EN  TIMESTAMP                          NOT NULL,
  ENROLADA_POR VARCHAR(64)  CHARACTER SET UTF8    NOT NULL,
  CREATED_AT   TIMESTAMP                          NOT NULL,
  UPDATED_AT   TIMESTAMP                          NOT NULL,

  CONSTRAINT PK_MSP_AS_PLANTILLA      PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AS_PLANTILLA_DEDO UNIQUE (EMPLEADO_ID, DEDO),
  CONSTRAINT FK_MSP_AS_PLANTILLA_EMP  FOREIGN KEY (EMPLEADO_ID) REFERENCES MSP_AS_EMPLEADO (ID)
);

CREATE TABLE MSP_AS_MARCAJE (
  ID                  CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  EMPLEADO_ID         CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  EQUIPO_ID           CHAR(36)     CHARACTER SET ASCII,
  SEDE_ID             CHAR(36)     CHARACTER SET ASCII,
  METODO              VARCHAR(12)  CHARACTER SET ASCII   NOT NULL,
  INSTANTE            TIMESTAMP                          NOT NULL,
  INSTANTE_EQUIPO     TIMESTAMP,
  INSTANTE_SERVIDOR   TIMESTAMP                          NOT NULL,
  CLAVE_IDEMPOTENCIA  CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  ORIGEN              VARCHAR(12)  CHARACTER SET ASCII   NOT NULL,
  SUPRIMIDO_POR       VARCHAR(16)  CHARACTER SET ASCII,
  CREATED_AT          TIMESTAMP                          NOT NULL,

  CONSTRAINT PK_MSP_AS_MARCAJE       PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AS_MARCAJE_IDEM  UNIQUE (CLAVE_IDEMPOTENCIA),
  CONSTRAINT FK_MSP_AS_MARCAJE_EMP   FOREIGN KEY (EMPLEADO_ID) REFERENCES MSP_AS_EMPLEADO (ID)
);

CREATE TABLE MSP_AS_HORARIO (
  ID             CHAR(36)  CHARACTER SET ASCII   NOT NULL,
  EMPLEADO_ID    CHAR(36)  CHARACTER SET ASCII   NOT NULL,
  DIA_SEMANA     SMALLINT                        NOT NULL,
  ENTRADA        TIME,
  COMIDA_INICIO  TIME,
  COMIDA_FIN     TIME,
  SALIDA         TIME,
  VIGENTE_DESDE  DATE                            NOT NULL,
  VIGENTE_HASTA  DATE,
  CREATED_AT     TIMESTAMP                       NOT NULL,
  UPDATED_AT     TIMESTAMP                       NOT NULL,

  CONSTRAINT PK_MSP_AS_HORARIO     PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_AS_HORARIO_EMP FOREIGN KEY (EMPLEADO_ID) REFERENCES MSP_AS_EMPLEADO (ID)
);

CREATE TABLE MSP_AS_JORNADA (
  ID                  CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  EMPLEADO_ID         CHAR(36)     CHARACTER SET ASCII   NOT NULL,
  FECHA               DATE                               NOT NULL,
  ENTRADA             TIMESTAMP,
  SALIDA              TIMESTAMP,
  MINUTOS_PRESENCIA   INTEGER                            NOT NULL,
  MINUTOS_COMIDA      INTEGER                            NOT NULL,
  MINUTOS_NETOS       INTEGER                            NOT NULL,
  ESTADO              VARCHAR(20)  CHARACTER SET ASCII   NOT NULL,
  RETARDO_MINUTOS     INTEGER                            NOT NULL,
  MARCAJES_GENERADOS  SMALLINT                           NOT NULL,
  DESFASE_SOSPECHOSO  SMALLINT                           NOT NULL,
  REVISADA_EN         TIMESTAMP,
  REVISADA_POR        VARCHAR(64)  CHARACTER SET UTF8,
  CALCULADA_EN        TIMESTAMP                          NOT NULL,
  CREATED_AT          TIMESTAMP                          NOT NULL,
  UPDATED_AT          TIMESTAMP                          NOT NULL,

  CONSTRAINT PK_MSP_AS_JORNADA     PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_AS_JORNADA_EMP UNIQUE (EMPLEADO_ID, FECHA),
  CONSTRAINT FK_MSP_AS_JORNADA_EMP FOREIGN KEY (EMPLEADO_ID) REFERENCES MSP_AS_EMPLEADO (ID)
);

CREATE TABLE MSP_AS_CORRECCION (
  ID              CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  MARCAJE_ID      CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  EMPLEADO_ID     CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  TIPO            VARCHAR(16)   CHARACTER SET ASCII   NOT NULL,
  VALOR_ANTERIOR  VARCHAR(40)   CHARACTER SET ASCII,
  VALOR_NUEVO     VARCHAR(40)   CHARACTER SET ASCII,
  MOTIVO          VARCHAR(300)  CHARACTER SET UTF8    NOT NULL,
  USUARIO         VARCHAR(64)   CHARACTER SET UTF8    NOT NULL,
  CREATED_AT      TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_AS_CORRECCION     PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_AS_CORRECCION_MAR FOREIGN KEY (MARCAJE_ID)  REFERENCES MSP_AS_MARCAJE (ID),
  CONSTRAINT FK_MSP_AS_CORRECCION_EMP FOREIGN KEY (EMPLEADO_ID) REFERENCES MSP_AS_EMPLEADO (ID)
);

CREATE TABLE MSP_AS_CONFIG (
  ID                         CHAR(36)  CHARACTER SET ASCII   NOT NULL,
  HORA_CORTE_JORNADA         TIME                            NOT NULL,
  VENTANA_REBOTE_MIN         SMALLINT                        NOT NULL,
  VENTANA_CLASIFICACION_MIN  SMALLINT                        NOT NULL,
  TOLERANCIA_RETARDO_MIN     SMALLINT                        NOT NULL,
  COMIDA_MAXIMA_MIN          SMALLINT                        NOT NULL,
  JORNADA_ABIERTA_MAX_H      SMALLINT                        NOT NULL,
  DESFASE_MAXIMO_SEG         INTEGER                         NOT NULL,
  CREATED_AT                 TIMESTAMP                       NOT NULL,
  UPDATED_AT                 TIMESTAMP                       NOT NULL,

  CONSTRAINT PK_MSP_AS_CONFIG PRIMARY KEY (ID)
);

CREATE TABLE MSP_AS_FESTIVO (
  FECHA       DATE                                NOT NULL,
  NOMBRE      VARCHAR(120)  CHARACTER SET UTF8    NOT NULL,
  CREATED_AT  TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_AS_FESTIVO PRIMARY KEY (FECHA)
);
```

## Índices

```sql
CREATE INDEX IDX_MSP_AS_EMPLEADO_SEDE    ON MSP_AS_EMPLEADO (SEDE_ID);
CREATE INDEX IDX_MSP_AS_EQUIPO_SEDE      ON MSP_AS_EQUIPO (SEDE_ID);
CREATE INDEX IDX_MSP_AS_PLANTILLA_EMP    ON MSP_AS_PLANTILLA (EMPLEADO_ID);
CREATE INDEX IDX_MSP_AS_MARCAJE_EMP      ON MSP_AS_MARCAJE (EMPLEADO_ID, INSTANTE);
CREATE INDEX IDX_MSP_AS_MARCAJE_EQUIPO   ON MSP_AS_MARCAJE (EQUIPO_ID, INSTANTE);
CREATE INDEX IDX_MSP_AS_HORARIO_EMP      ON MSP_AS_HORARIO (EMPLEADO_ID, DIA_SEMANA, VIGENTE_DESDE);
CREATE INDEX IDX_MSP_AS_JORNADA_FECHA    ON MSP_AS_JORNADA (FECHA, ESTADO);
CREATE INDEX IDX_MSP_AS_CORRECCION_EMP   ON MSP_AS_CORRECCION (EMPLEADO_ID, CREATED_AT);
CREATE INDEX IDX_MSP_AS_CORRECCION_MAR   ON MSP_AS_CORRECCION (MARCAJE_ID);
```

## Lo que hay que escribir tú

**El DDL de arriba es literal y no se toca.** Lo que sí hay que producir:

1. **El bloque de comentario de encabezado del `up`**, en español, explicando *por qué* existen estas tablas. Copia la forma de `000046_create_msp_rx_conversacion.up.sql` — explica qué es cada tabla y menciona las dos separaciones que gobiernan el diseño: que `MSP_AS_PLANTILLA` está aparte porque tiene el dato biométrico y se borra en distinto momento que las checadas, y que `MSP_AS_MARCAJE` es el hecho inmutable mientras `MSP_AS_JORNADA` es un cálculo que se puede rehacer.
2. **El orden de creación**, respetando las llaves foráneas: sede antes que empleado y equipo, empleado antes que plantilla, marcaje y horario, marcaje antes que corrección.
3. **Los `COMMIT;`** donde el archivo 000046 los pone.
4. **El cierre:** `INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (48, '000048_create_msp_as_asistencia', CURRENT_TIMESTAMP);` seguido de `COMMIT;`. Copia el formato exacto de 000046.
5. **El `.down.sql`**, que es solo `DROP TABLE` en el orden inverso (hijas antes que padres), un `COMMIT;` por bloque, y al final `DELETE FROM MSP_MIGRATIONS WHERE ID = 48;` con su `COMMIT;`.

> ⚠️ **En el `down` NO pongas `DROP INDEX` ni `ALTER TABLE ... DROP CONSTRAINT`.** En Firebird, `DROP TABLE` ya elimina los índices y restricciones de esa tabla. Ponerlos es ruido y, peor, si el `up` falló a la mitad el `drop` de un objeto inexistente aborta el rollback y deja la base peor que antes. Mira `000046_create_msp_rx_conversacion.down.sql`: son cuatro líneas por tabla y nada más.

---

# Entregable 2 — Cifrado de plantillas

## Por qué

Las plantillas de huella se guardan en la base de datos. Si alguien obtiene un respaldo de la base, no debe poder reconstruir las huellas. Se cifran en Go antes de escribirlas y se descifran al leerlas.

## Archivos

```
internal/asistencia/ports/outbound/cifrador.go
internal/asistencia/infra/cifrado/aesgcm.go
internal/asistencia/infra/cifrado/doc.go
internal/asistencia/infra/cifrado/aesgcm_test.go
```

## El puerto — copiar exactamente

`internal/asistencia/ports/outbound/cifrador.go`:

```go
package outbound

import "context"

// Cifrador protects biometric templates at rest. Templates are encrypted in Go
// before they reach Firebird and decrypted after they are read back, so a
// database backup alone never exposes usable fingerprint data.
//
// Implementations must produce a different ciphertext every time for the same
// plaintext (fresh nonce per call) and must fail loudly on tampered input
// rather than returning garbage.
type Cifrador interface {
	// Cifrar encrypts plaintext. The returned slice is self-contained: it
	// carries whatever nonce or header the implementation needs to decrypt it.
	Cifrar(ctx context.Context, claro []byte) ([]byte, error)

	// Descifrar reverses Cifrar. It returns an error if the input was
	// truncated, corrupted, or encrypted with a different key.
	Descifrar(ctx context.Context, cifrado []byte) ([]byte, error)
}
```

## La implementación

`AESGCMCifrador`, en el paquete `cifrado`. Requisitos:

- **Solo biblioteca estándar:** `crypto/aes`, `crypto/cipher`, `crypto/rand`, `io`. **No agregues dependencias al `go.mod`.**
- `NewAESGCMCifrador(llave []byte) (*AESGCMCifrador, error)` — la llave debe medir exactamente **32 bytes** (AES-256). Cualquier otro tamaño devuelve error.
- `Cifrar` genera un **nonce nuevo con `crypto/rand` en cada llamada** y lo antepone al texto cifrado: el resultado es `nonce || ciphertext`.
- `Descifrar` separa el nonce, descifra y verifica. Si la entrada mide menos que el nonce, o el sello no cuadra, devuelve error — **nunca datos parciales**.
- Errores con `internal/platform/apperror`. **Código en inglés y snake_case; mensaje en español, en minúsculas y sin punto final.** Ejemplo: `apperror.NewValidation("cipher_key_invalid_size", "la llave de cifrado debe medir 32 bytes")`.
- Los errores centinela se declaran **una sola vez a nivel de paquete**, no dentro de las funciones.

## Pruebas

Paquete `cifrado_test`. Mínimo:

- **Ida y vuelta:** cifrar y descifrar devuelve exactamente los mismos bytes.
- **Nonce distinto por llamada:** cifrar dos veces el mismo texto produce **dos resultados diferentes**. Esta prueba es la que detecta el error más común de esta implementación.
- Descifrar con una **llave distinta** falla.
- Descifrar una entrada **truncada** falla.
- Descifrar una entrada a la que se le **cambió un byte** falla.
- Llave de 16, 31 o 33 bytes en el constructor: falla.
- Texto claro **vacío**: funciona, ida y vuelta.
- Texto claro grande (por ejemplo 64 KB, tamaño realista de una plantilla): funciona.

## Restricciones

- **Prohibido importar cualquier `internal/` que no sea `internal/asistencia/...` o `internal/platform/...`.** Ni `ventas`, ni `cobranza`, ni ningún otro módulo. Asistencia es un módulo sellado; importar de otro módulo es motivo de rechazo directo.
- No agregues dependencias nuevas al `go.mod`.
- No uses `--no-verify` al commitear, bajo ninguna circunstancia.

## Archivos que puedes tocar

Solo estos seis. Cualquier cambio fuera de la lista se rechaza sin revisar:

```
migrations-firebird/000048_create_msp_as_asistencia.up.sql
migrations-firebird/000048_create_msp_as_asistencia.down.sql
internal/asistencia/ports/outbound/cifrador.go
internal/asistencia/infra/cifrado/aesgcm.go
internal/asistencia/infra/cifrado/doc.go
internal/asistencia/infra/cifrado/aesgcm_test.go
```

## Verificación

Corre esto y pega la salida completa en el reporte. Todo tiene que pasar:

```sh
gofmt -l internal/asistencia
go vet ./internal/asistencia/...
go build ./...
golangci-lint run ./internal/asistencia/...
go test -race -coverprofile=coverage-asistencia-cifrado.out ./internal/asistencia/infra/cifrado/
go tool cover -func=coverage-asistencia-cifrado.out | tail -1
```

Criterios de aceptación, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` terminan sin errores.
- Las pruebas pasan con `-race`.
- La cobertura de `infra/cifrado` es **≥ 85.0%**.
- Revisión manual del SQL: **cero `DEFAULT`, cero triggers, cero procedimientos**; el `down` sin `DROP INDEX` ni `DROP CONSTRAINT`.

**No corras las migraciones contra la base de datos.** Eso se hace después, aparte. Aquí solo se verifica que el SQL esté bien formado y siga las convenciones.

Si algún comando falla, la tarea no está terminada. No la entregues para que alguien la revise: entrégala cuando pase.

## Si te atoras

Si llevas **más de dos horas** trabadο en una sola cosa, avisa. No sigas.

## Reporte

Escríbelo en `docs/superpowers/plans/asistencia-task-1-report.md`:

- Archivos creados.
- Salida literal de los seis comandos.
- Qué copiaste de `000046` y en qué se diferencia tu migración.
- Confirmación explícita de que ningún archivo importa otro módulo, y de que no agregaste dependencias al `go.mod`.

## Commit

Estás en `feat/asistencia-base`. Dos commits, uno por entregable:

```
feat(asistencia): migración 000048 con las diez tablas del módulo
feat(asistencia): cifrado AES-GCM de plantillas biométricas
```

Al terminar: `git push`. Sin `--no-verify`, y sin pie de atribución a ninguna herramienta de IA en el mensaje.
