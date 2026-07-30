# Comprobantes — Tarea 1: migración `000049` + modelos de contenido

> **Rama:** `feat/comprobantes-base` (ya creada, sacada de `feat/comprobantes`)
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)

## Dónde encaja

El módulo de comprobantes manda al cliente un PDF por WhatsApp cuando su venta se registra en Microsip y cuando su pago se aplica. Esta tarea son **dos entregables independientes** que no dependen de nada más del módulo:

1. La migración con las tres tablas.
2. Los modelos de contenido: las estructuras con los datos que van impresos en cada comprobante.

Se pueden hacer en cualquier orden.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras. En especial la **#1 (nada de lógica en la base)** y la **#3 (código en inglés, mensajes de usuario en español)**.
2. `migrations-firebird/000046_create_msp_rx_conversacion.up.sql` — el modelo de encabezado y cierre.
3. `migrations-firebird/000046_create_msp_rx_conversacion.down.sql` — el modelo de `down`. Fíjate en lo **corto** que es.
4. `docs/superpowers/specs/2026-07-29-comprobantes-whatsapp-design.md` §5 y §6 — de dónde salen las tablas y qué lleva cada comprobante.

---

# Entregable 1 — Migración `000049`

## Archivos

```
migrations-firebird/000049_create_msp_cm_comprobantes.up.sql
migrations-firebird/000049_create_msp_cm_comprobantes.down.sql
```

**Antes de escribir, corre `ls migrations-firebird/` y confirma que la 49 sigue libre.** Hay varios módulos en vuelo y el número de migración es donde chocan; ya pasó una vez.

## Restricciones (CLAUDE.md regla #1)

- **Ningún `DEFAULT` en ninguna columna.** Ni `CURRENT_TIMESTAMP`, ni UUID por defecto. Todo viene desde Go.
- Sin triggers, sin procedimientos, sin generadores, sin `CHECK` de reglas de negocio.
- Permitido: `PRIMARY KEY`, `UNIQUE`, `NOT NULL`, índices, tipos de columna.
- UUIDs son `CHAR(36) CHARACTER SET ASCII`. Texto de usuario es `CHARACTER SET UTF8`.

## DDL — usar exactamente esto

```sql
CREATE TABLE MSP_CM_ENVIO (
  ID                  CHAR(36)      CHARACTER SET ASCII   NOT NULL,
  TIPO                VARCHAR(12)   CHARACTER SET ASCII   NOT NULL,
  REFERENCIA          VARCHAR(40)   CHARACTER SET ASCII   NOT NULL,
  CLIENTE_ID          INTEGER                             NOT NULL,
  TELEFONO            VARCHAR(20)   CHARACTER SET ASCII,
  ESTADO              VARCHAR(12)   CHARACTER SET ASCII   NOT NULL,
  PROGRAMADO_PARA     TIMESTAMP                           NOT NULL,
  DOCUMENTO_RUTA      VARCHAR(500)  CHARACTER SET UTF8,
  CANAL               VARCHAR(20)   CHARACTER SET ASCII,
  MENSAJE_EXTERNO_ID  VARCHAR(64)   CHARACTER SET ASCII,
  INTENTOS            SMALLINT                            NOT NULL,
  ULTIMO_ERROR        VARCHAR(500)  CHARACTER SET UTF8,
  DETENIDO_POR        VARCHAR(64)   CHARACTER SET UTF8,
  ENVIADO_EN          TIMESTAMP,
  CREATED_AT          TIMESTAMP                           NOT NULL,
  UPDATED_AT          TIMESTAMP                           NOT NULL,

  CONSTRAINT PK_MSP_CM_ENVIO     PRIMARY KEY (ID),
  CONSTRAINT UQ_MSP_CM_ENVIO_REF UNIQUE (TIPO, REFERENCIA)
);

CREATE TABLE MSP_CM_CURSOR (
  CLAVE       VARCHAR(32)  CHARACTER SET ASCII   NOT NULL,
  SEQ_ID      BIGINT                             NOT NULL,
  UPDATED_AT  TIMESTAMP                          NOT NULL,

  CONSTRAINT PK_MSP_CM_CURSOR PRIMARY KEY (CLAVE)
);

CREATE TABLE MSP_CM_CONFIG (
  ID                 CHAR(36)  CHARACTER SET ASCII   NOT NULL,
  VENTANA_VENTA_MIN  SMALLINT                        NOT NULL,
  VENTANA_PAGO_MIN   SMALLINT                        NOT NULL,
  HABILITADO_VENTA   SMALLINT                        NOT NULL,
  HABILITADO_PAGO    SMALLINT                        NOT NULL,
  MAX_INTENTOS       SMALLINT                        NOT NULL,
  CREATED_AT         TIMESTAMP                       NOT NULL,
  UPDATED_AT         TIMESTAMP                       NOT NULL,

  CONSTRAINT PK_MSP_CM_CONFIG PRIMARY KEY (ID)
);
```

## Índices

```sql
CREATE INDEX IDX_MSP_CM_ENVIO_PENDIENTES ON MSP_CM_ENVIO (ESTADO, PROGRAMADO_PARA);
CREATE INDEX IDX_MSP_CM_ENVIO_CLIENTE    ON MSP_CM_ENVIO (CLIENTE_ID);
CREATE INDEX IDX_MSP_CM_ENVIO_CREATED    ON MSP_CM_ENVIO (CREATED_AT);
```

## Lo que hay que escribir tú

El DDL de arriba es literal y no se toca. Lo que sí produces:

1. **El comentario de encabezado del `up`**, en español, explicando *por qué* existen estas tablas. Copia la forma de `000046`. Menciona en particular que:
   - `UQ_MSP_CM_ENVIO_REF` es lo que garantiza **un solo comprobante por hecho**: si el worker reprocesa un tramo tras un reinicio, el segundo intento choca con la restricción y se descarta. La idempotencia la impone la base, no la memoria del worker.
   - `IDX_MSP_CM_ENVIO_PENDIENTES` es el índice del worker: "dame los que están en espera y ya les tocó".
   - `MSP_CM_CURSOR` tiene una sola fila y guarda dónde quedó el recorrido del changelog de pagos.
2. **El orden de creación** y los `COMMIT;` donde `000046` los pone.
3. **El cierre:** `INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (49, '000049_create_msp_cm_comprobantes', CURRENT_TIMESTAMP);` seguido de `COMMIT;`. Copia el formato exacto de `000046`.
4. **El `.down.sql`**: solo `DROP TABLE` en orden inverso, un `COMMIT;` por bloque, y al final `DELETE FROM MSP_MIGRATIONS WHERE ID = 49;` con su `COMMIT;`.

> ⚠️ **En el `down` NO pongas `DROP INDEX` ni `ALTER TABLE ... DROP CONSTRAINT`.** En Firebird, `DROP TABLE` ya elimina los índices y restricciones de esa tabla. Ponerlos es ruido y, peor, si el `up` falló a la mitad el `drop` de un objeto inexistente aborta el rollback y deja la base peor que antes. Mira `000046_..._down.sql`: son cuatro líneas por tabla.

---

# Entregable 2 — Modelos de contenido

## Por qué

Son las estructuras que recibe el generador de PDF: los datos ya reunidos y listos para imprimir. No llevan lógica de presentación ni formato — eso es del renderizador.

## Archivos

```
internal/comprobantes/domain/comprobante_venta.go
internal/comprobantes/domain/comprobante_pago.go
internal/comprobantes/domain/doc.go
internal/comprobantes/domain/comprobante_venta_test.go
internal/comprobantes/domain/comprobante_pago_test.go
```

## `ComprobanteVenta`

Campos, según §6.2 del spec:

| Campo | Tipo | Nota |
|---|---|---|
| `Folio` | `string` | El folio de Microsip |
| `Fecha` | `time.Time` | UTC |
| `ClienteNombre` | `string` | |
| `ClienteDomicilio` | `string` | Ya armado en una línea |
| `Articulos` | `[]ArticuloComprobante` | |
| `Total` | `decimal.Decimal` | |
| `Enganche` | `decimal.Decimal` | |
| `Saldo` | `decimal.Decimal` | |
| `PlanPago` | `string` | En palabras: cuánto, cada cuándo, cuántas |
| `Vendedor` | `string` | |

Y `ArticuloComprobante`: `Descripcion string`, `Cantidad decimal.Decimal`, `PrecioUnitario decimal.Decimal`, `Importe decimal.Decimal`.

## `ComprobantePago`

| Campo | Tipo | Nota |
|---|---|---|
| `Folio` | `string` | |
| `Fecha` | `time.Time` | UTC |
| `ClienteNombre` | `string` | |
| `Monto` | `decimal.Decimal` | |
| `FormaCobro` | `string` | |
| `VentaFolio` | `string` | A qué venta se aplicó |
| `SaldoRestante` | `decimal.Decimal` | **Después de este pago** |
| `Cobrador` | `string` | |

> `SaldoRestante` es el dato que el cliente de verdad busca. Un comprobante que solo diga "recibimos $500" deja la pregunta abierta y genera la llamada que el comprobante debía evitar.

## Cómo construirlos

Cada uno con un constructor `NewComprobanteVenta(...)` / `NewComprobantePago(...)` que **valide lo mínimo indispensable** y devuelva error con `internal/platform/apperror`:

- Folio no vacío.
- Nombre del cliente no vacío.
- Montos no negativos (`Total`, `Enganche`, `Saldo`, `Monto`, `SaldoRestante`, `Importe`).
- `ComprobanteVenta` con al menos un artículo.

Errores centinela declarados **a nivel de paquete**, código en inglés snake_case, mensaje en español minúsculas sin punto final. Ejemplo:

```go
var ErrComprobanteFolioRequerido = apperror.NewValidation(
	"receipt_folio_required",
	"el folio del comprobante es obligatorio",
)
```

Campos privados con getters, siguiendo `docs/module-standards/AGGREGATE_PATTERNS.md`.

## Pruebas

Paquete `domain_test`. **El piso de cobertura de `domain` es 99%.**

Por cada constructor: caso válido; y un caso por cada validación que falla, verificando el error centinela con `errors.Is`. Más los getters.

## Restricciones

- `domain` no importa nada fuera de la biblioteca estándar, `uuid`, `decimal` y `internal/platform/apperror`.
- No agregues dependencias al `go.mod`.
- No uses `--no-verify` al commitear.

## Archivos que puedes tocar

Solo estos siete. Cualquier cambio fuera de la lista se rechaza sin revisar:

```
migrations-firebird/000049_create_msp_cm_comprobantes.up.sql
migrations-firebird/000049_create_msp_cm_comprobantes.down.sql
internal/comprobantes/domain/comprobante_venta.go
internal/comprobantes/domain/comprobante_pago.go
internal/comprobantes/domain/doc.go
internal/comprobantes/domain/comprobante_venta_test.go
internal/comprobantes/domain/comprobante_pago_test.go
```

## Verificación

Corre esto y pega la salida completa en el reporte:

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
go test -race -coverprofile=coverage-comprobantes-domain.out ./internal/comprobantes/domain/
go tool cover -func=coverage-comprobantes-domain.out | tail -1
```

Criterios, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` sin errores.
- Pruebas en verde con `-race`.
- Cobertura de `domain` **≥ 99.0%**.
- Revisión manual del SQL: **cero `DEFAULT`, cero triggers**; el `down` sin `DROP INDEX` ni `DROP CONSTRAINT`.

**No corras las migraciones contra la base.** Aquí solo se verifica que el SQL esté bien formado y siga las convenciones.

Si algún comando falla, la tarea no está terminada. No la entregues para que alguien la revise: entrégala cuando pase.

## Si te atoras

Más de **dos horas** trabado en una sola cosa: avisa. No sigas.

## Reporte

`docs/superpowers/plans/comprobantes-task-1-report.md`, con: archivos creados, salida literal de los seis comandos, qué copiaste de `000046` y en qué se diferencia tu migración, y confirmación de que no agregaste dependencias al `go.mod`.

## Rama y commit

Estás en `feat/comprobantes-base`. Dos commits, uno por entregable:

```
feat(comprobantes): migración 000049 con las tablas del módulo
feat(comprobantes): modelos de contenido de los comprobantes
```

Al terminar: `git push`. Sin `--no-verify` y sin pie de atribución a ninguna herramienta de IA.
