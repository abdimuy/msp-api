# Garantías — Tarea 2: migración `000050` + value objects del dominio

> **Rama:** `feat/garantias-base` (ya creada, sacada de `feat/garantias`)
> **Spec:** [`2026-07-27-garantias-design.md`](../specs/2026-07-27-garantias-design.md)

## Dónde encaja

El módulo de garantías da seguimiento a un artículo desde que se reporta defectuoso hasta su resolución, sin perder de vista **en qué etapa del proceso va** ni **dónde está físicamente**.

Ya hiciste el almacenamiento de evidencia (tarea 1). Esta es la continuación: **las tablas y los tipos base del dominio**, que son lo que desbloquea todo lo demás del módulo.

Dos entregables independientes; se pueden hacer en cualquier orden.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras. En especial la **#1 (nada de lógica en la base)** y la **#3 (código en inglés, mensajes de usuario en español)**.
2. `migrations-firebird/000046_create_msp_rx_conversacion.up.sql` y su `.down.sql` — el modelo de encabezado, formato y cierre. Fíjate en lo corto que es el `down`.
3. `migrations-firebird/000028_create_gen_mst_folio.up.sql` — cómo se declara un generador de folio en este proyecto.
4. Para los enums: `internal/ventas/domain/tipo_venta.go` y su `_test.go`. Para el State VO: la plantilla del estándar `02-value-objects-errors.md` y el ejemplo real `internal/garantias/domain/estado_folio.go`. **No** `internal/ventas/domain/estado_registro.go`: es un Enum VO de categoría 1 y no tiene mapa de transiciones — la cita estaba mal en el estándar y se corrigió el 2026-08-07.
5. `docs/superpowers/specs/2026-07-27-garantias-design.md` §3 y §4 — de dónde salen las tablas y qué significa cada estado.

---

# Entregable 1 — Migración `000050`

## Archivos

```
migrations-firebird/000050_create_msp_ga_garantias.up.sql
migrations-firebird/000050_create_msp_ga_garantias.down.sql
```

**Antes de escribir, corre `ls migrations-firebird/` y confirma que la 50 sigue libre.** Hay cuatro módulos en vuelo y el número de migración es donde chocan. La `000047` la tomó visitas, la `000048` la reserva asistencia y la `000049` comprobantes.

## Restricciones (CLAUDE.md regla #1)

- **Ningún `DEFAULT` en ninguna columna.** Todo viene desde Go.
- Sin triggers, sin procedimientos, sin `CHECK` de reglas de negocio.
- Permitido: `PRIMARY KEY`, `UNIQUE`, `NOT NULL`, `FOREIGN KEY`, índices, tipos.
- **Única excepción:** el generador `GEN_MSP_GA_FOLIO`, para asignación atómica de folio. La regla lo contempla explícitamente como infraestructura.
- UUIDs son `CHAR(36) CHARACTER SET ASCII`. Texto de usuario, `CHARACTER SET UTF8`.

## DDL — usar exactamente esto

```sql
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
  ID            CHAR(36)      CHARACTER SET ASCII            NOT NULL,
  GARANTIA_ID   CHAR(36)      CHARACTER SET ASCII            NOT NULL,
  ARTICULO_REF  CHAR(36)      CHARACTER SET ASCII,
  TIPO          VARCHAR(28)   CHARACTER SET ASCII            NOT NULL,
  DESCRIPCION   BLOB SUB_TYPE TEXT CHARACTER SET UTF8,
  ETAPA_DESDE   VARCHAR(28)   CHARACTER SET ASCII,
  ETAPA_HASTA   VARCHAR(28)   CHARACTER SET ASCII,
  USUARIO       VARCHAR(64)   CHARACTER SET UTF8             NOT NULL,
  ROL_DECISOR   VARCHAR(16)   CHARACTER SET ASCII,
  GPS_LAT       DOUBLE PRECISION,
  GPS_LON       DOUBLE PRECISION,
  CREATED_AT    TIMESTAMP                                    NOT NULL,

  CONSTRAINT PK_MSP_GA_EVENTO     PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_GA_EVENTO_GAR FOREIGN KEY (GARANTIA_ID) REFERENCES MSP_GA_GARANTIA (ID)
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
```

> ⚠️ **La llave foránea de `MSP_GA_IMAGEN` va SIN `ON DELETE CASCADE`.** Las fotos son la evidencia de en qué estado se recogió el mueble; no se borran en cascada nunca.

## Índices

```sql
CREATE INDEX IDX_MSP_GA_GARANTIA_CLIENTE ON MSP_GA_GARANTIA (CLIENTE_ID);
CREATE INDEX IDX_MSP_GA_GARANTIA_ESTADO  ON MSP_GA_GARANTIA (ESTADO, CREATED_AT);
CREATE INDEX IDX_MSP_GA_GARANTIA_CREATED ON MSP_GA_GARANTIA (CREATED_AT);
CREATE INDEX IDX_MSP_GA_ARTICULO_GAR     ON MSP_GA_ARTICULO (GARANTIA_ID);
CREATE INDEX IDX_MSP_GA_ARTICULO_ETAPA   ON MSP_GA_ARTICULO (ETAPA);
CREATE INDEX IDX_MSP_GA_ARTICULO_UBIC    ON MSP_GA_ARTICULO (UBICACION);
CREATE INDEX IDX_MSP_GA_EVENTO_GAR       ON MSP_GA_EVENTO (GARANTIA_ID, CREATED_AT);
CREATE INDEX IDX_MSP_GA_IMAGEN_EVT       ON MSP_GA_IMAGEN (EVENTO_ID);
```

## Lo que hay que escribir tú

El DDL es literal y no se toca. Lo que produces:

1. **El comentario de encabezado del `up`**, en español, explicando *por qué* existen estas tablas. Copia la forma de `000046`. Menciona en particular:
   - **`ETAPA` y `UBICACION` son columnas distintas a propósito.** Etapa es dónde va en el proceso; ubicación es dónde está la cosa. Un artículo puede estar en etapa `dictamen_recibido` y ubicación `proveedor` porque todavía no lo regresan.
   - **Un cambio físico crea una segunda fila con `ROL='reemplazo'`.** Por eso el folio puede estar cerrado mientras el artículo original sigue en `standby`: son registros distintos.
   - **`MSP_GA_EVENTO` no tiene `UPDATED_AT`.** Un evento de auditoría no se edita; si algo salió mal se agrega un evento de corrección.
2. **El orden de creación** respetando las llaves foráneas, y los `COMMIT;` donde `000046` los pone.
3. **El cierre:** `INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (50, '000050_create_msp_ga_garantias', CURRENT_TIMESTAMP);` seguido de `COMMIT;`.
4. **El `.down.sql`**: `DROP TABLE` en orden inverso (imagen, evento, artículo, garantía), `DROP GENERATOR GEN_MSP_GA_FOLIO;`, un `COMMIT;` por bloque, y al final `DELETE FROM MSP_MIGRATIONS WHERE ID = 50;`.

> ⚠️ **En el `down` NO pongas `DROP INDEX` ni `ALTER TABLE ... DROP CONSTRAINT`.** `DROP TABLE` ya los elimina. Si el `up` falló a la mitad, el `drop` de un objeto inexistente aborta el rollback y deja la base peor que antes.

---

# Entregable 2 — Value objects del dominio

## Archivos

```
internal/garantias/domain/doc.go
internal/garantias/domain/errors.go
internal/garantias/domain/origen_folio.go
internal/garantias/domain/estado_cuenta.go
internal/garantias/domain/estado_folio.go
internal/garantias/domain/ruta_reparacion.go
internal/garantias/domain/dictamen.go
internal/garantias/domain/rol_articulo.go
internal/garantias/domain/rol_decisor.go
+ un _test.go por cada uno
```

Mismo patrón que `internal/ventas/domain/tipo_venta.go` (para enums) y `internal/garantias/domain/estado_folio.go` (para el State VO): `type {Tipo} string` con constantes tipadas, `Parse{Tipo}` que valida y devuelve centinela, `IsValid`, `String`, y los ayudantes `Es*` para cada valor. Para `EstadoFolio` se añade además el mapa de transiciones, `CanTransitionTo` e `IsTerminal`. No se usan `Hydrate`, `Value`, `Equals` ni `IsZero` — la hidratación desde la base se hace con un cast directo y la comparación es `==`.

Comentarios de doc **en inglés**. Mensajes de error **en español**.

### Los siete tipos

| Archivo | Tipo | Valores válidos | Ayudantes |
|---|---|---|---|
| `origen_folio.go` | `OrigenFolio` | `piso` · `cliente` | `EsPiso()`, `EsCliente()` |
| `estado_cuenta.go` | `EstadoCuenta` | `liquidada` · `saldo_pendiente` | `EsLiquidada()` |
| `estado_folio.go` | `EstadoFolio` | `abierto` · `en_proceso` · `listo_entrega` · `entregado` · `cerrado` · `cancelado` | `EsTerminal()` (cerrado y cancelado), `EsCancelado()` |
| `ruta_reparacion.go` | `RutaReparacion` | `proveedor` · `taller` | `EsProveedor()`, `EsTaller()` |
| `dictamen.go` | `Dictamen` | `aceptada` · `rechazada` · `sin_falla` | `EsAceptada()`, `EsRechazada()`, `EsSinFalla()` |
| `rol_articulo.go` | `RolArticulo` | `original` · `reemplazo` | `EsOriginal()`, `EsReemplazo()` |
| `rol_decisor.go` | `RolDecisor` | `carpinteria` · `oficina` · `tecnica` | — |

> `RolDecisor` existe porque **quien decide no es fijo**: la reparación rápida la puede decidir carpintería, oficina o el área técnica según la situación. El permiso controla *si puede*; este tipo registra *desde qué rol lo hizo*.

### `errors.go`

Centinelas a nivel de paquete, con `internal/platform/apperror`. Código en inglés snake_case, mensaje en español minúsculas sin punto final:

| Variable | Código | Mensaje |
|---|---|---|
| `ErrOrigenFolioInvalido` | `warranty_origin_invalid` | `origen de folio inválido` |
| `ErrEstadoCuentaInvalido` | `warranty_account_state_invalid` | `estado de cuenta inválido` |
| `ErrEstadoFolioInvalido` | `warranty_folio_state_invalid` | `estado de folio inválido` |
| `ErrRutaReparacionInvalida` | `warranty_repair_route_invalid` | `ruta de reparación inválida` |
| `ErrDictamenInvalido` | `warranty_verdict_invalid` | `dictamen inválido` |
| `ErrRolArticuloInvalido` | `warranty_item_role_invalid` | `rol de artículo inválido` |
| `ErrRolDecisorInvalido` | `warranty_decider_role_invalid` | `rol de quien decide inválido` |

> **Fuera de alcance de esta tarea:** `EtapaArticulo` (19 valores), `Ubicacion` (8) y `Desenlace` (6). Son los catálogos grandes y van con la tabla de transiciones en otra tarea. No los escribas aquí.

## Pruebas

Paquete `domain_test`. **El piso de cobertura de `domain` es 99%**, así que ahí está la mayor parte del trabajo.

Por cada tipo: cada valor válido con su ayudante en verdadero y los demás en falso; valor inválido devolviendo el centinela correcto verificado con `errors.Is`; cadena vacía inválida; `TestX_WireValues` para fijar los literales; y `TestX_IsValid` para verificar el método. Para `EstadoFolio` se añaden además pruebas de `CanTransitionTo` e `IsTerminal`. No se usan `IsZero`, `Equals` ni `Hydrate` — esos métodos ya no existen.

## Restricciones

- `domain` no importa nada fuera de la biblioteca estándar y `internal/platform/apperror`.
- **Prohibido importar cualquier `internal/` que no sea `internal/garantias/...` o `internal/platform/...`.** Garantías es un módulo sellado (ADR-0009); importar otro módulo es rechazo directo, y `make check-sealed` lo detecta.
- No agregues dependencias al `go.mod`.
- No uses `--no-verify` al commitear.

## Archivos que puedes tocar

Solo los listados arriba en los dos entregables. Cualquier cambio fuera se rechaza sin revisar.

## Verificación

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
make check-sealed MODULE=garantias
go test -race -coverprofile=coverage-garantias-domain.out ./internal/garantias/domain/
go tool cover -func=coverage-garantias-domain.out | tail -1
```

Criterios, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` sin errores.
- `make check-sealed MODULE=garantias` dice `✔ garantias is sealed`.
- Pruebas en verde con `-race`.
- Cobertura de `domain` **≥ 99.0%**.
- Revisión manual del SQL: cero `DEFAULT`, cero triggers; el `down` sin `DROP INDEX` ni `DROP CONSTRAINT`; la FK de imágenes sin `ON DELETE CASCADE`.

**No corras las migraciones contra la base.**


## Si te atoras

Más de **dos horas** trabado en una sola cosa: avisa. No sigas.

## Reporte

`docs/superpowers/plans/garantias-task-2-report.md`, con: archivos creados, salida literal de los siete comandos, qué copiaste de `000046` y de los moldes correctos (`tipo_venta.go` y `estado_folio.go`) y en qué se diferencia lo tuyo, y confirmación de que ningún archivo importa otro módulo.

## Rama y commit

Estás en `feat/garantias-base`. Dos commits:

```
feat(garantias): migración 000050 con las tablas del módulo
feat(garantias): value objects y errores del dominio
```

**No hagas `git push`.** Por ahora no hay acceso de escritura al remoto: tus commits se quedan en tu rama local. Al terminar, avisa y coordinamos cómo se integra tu trabajo.

Sin `--no-verify` bajo ninguna circunstancia, y sin pie de atribución a ninguna herramienta de IA en el mensaje del commit.

