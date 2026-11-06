# Módulo de garantías — diseño

- **Fecha:** 2026-07-27
- **Estado:** aprobado, listo para plan de implementación
- **Módulo:** `internal/garantias`
- **Migración:** `000050_create_msp_ga_garantias`
- **Nota:** la `000047` se reasignó a `MSP_VISITAS` (rama `feat/visitas`), que ya tiene código escrito. La `000048` la reserva asistencia y la `000049` comprobantes. **Antes de escribir la migración, correr `ls migrations-firebird/` y confirmar que la 50 sigue libre** — con varios módulos en vuelo el número es donde chocan.
- **Prefijo de tablas:** `MSP_GA_*`
- **Actualización 2026-10-26 (decisiones del dueño):** la firma de conformidad y la auditoría GPS entran en v1 (§4.1); la idempotencia de las rutas POST es la del dominio, no el middleware (§6); columnas nuevas en la migración `000071` (§3.3, §3.4); tareas 9 y 10 (§10).

## 1. Objetivo

Dar seguimiento a un artículo desde que se reporta como defectuoso hasta su resolución final, sin perder visibilidad del **estado del proceso** ni de la **ubicación física** del producto en ningún punto.

### Dentro del alcance

- Folio de garantía de punta a punta: origen (piso o cliente), recolección, diagnóstico, ruta de reparación (proveedor externo o taller interno), entrega y cierre.
- Rastreo de etapa y ubicación de cada artículo bajo custodia, incluido el artículo sustituido tras un cambio físico.
- Evidencia fotográfica y coordenadas GPS por evento.
- Captura offline-first desde el teléfono del cobrador.

### Fuera del alcance

- **Cualquier escritura a Microsip** (`DOCTOS_IN`, `DOCTOS_IN_DET`, `ARTICULOS`, `SALDOS_IN`).
- Catálogo de refacciones o rastreo individual de piezas.
- Cruce con el módulo `inventario`.

`merma`, `segunda_mano` y `desarmado` son **estados del registro**, no movimientos contables. Significan "esto fue lo que pasó con el artículo y aquí está la evidencia". Si en el futuro deben generar el movimiento real en Microsip, lo hace un consumidor del evento, fuera de este módulo.

## 2. Decisiones de arquitectura

### 2.1 Módulo sellado

Garantías se construye como **vertical slice sellado**, siguiendo [ADR-0009](../../adr/0009-asistencia-sealed-module.md), con la motivación de que pueda extraerse a bonanza-api de forma mecánica.

- Cero imports de otros módulos, **incluidos sus paquetes de contratos**. Más estricto que la regla general del repo.
- Se permite `internal/platform/**`: `idempotency`, `imageprocessor`, `firebird`, `apperror`, `audit`, `httpdispatch`, `pagination`, `response`, `validator`.
- Tablas propias `MSP_GA_*`, sin llave foránea hacia ninguna tabla fuera del módulo.
- El módulo posee su cableado: `internal/garantias/module.go` expone un `fx.Option` que el composition root incluye en una línea.
- Firebird queda detrás de los repositorios. `firebird.ToWallClock` y `firebird.ScanUTCTime` no aparecen en `domain` ni en `app`.

**Garantías es el primer módulo sellado que se construye.** ADR-0009 está escrito pero `internal/asistencia` no existe todavía.

El andamiaje ya está puesto (commit `f6c53ab` en `main`): `make check-sealed` recorre los módulos de `SEALED_MODULES` y omite los que aún no existen, y `.golangci.yml` tiene la regla `garantias-sealed` que prohíbe estáticamente importar cualquier `internal/` que no sea el propio módulo o `platform`. Ambos verificados contra una fuga real antes de commitear. `make check-sealed` corre en `pre-push`.

### 2.2 La costura con auth

Los códigos de permiso viven en `internal/auth/domain/permission_codes.go`, que es una lista estática. Un módulo sellado no puede importar `auth`.

Garantías declara sus códigos como constantes propias y consulta a través de un puerto `Identity` de dos operaciones. **El registro de esos códigos en el catálogo de auth es una línea en el composition root que no viaja con el módulo** al extraerlo. Es la única costura conocida, y está documentada aquí para que la extracción no la descubra por sorpresa.

### 2.3 Identificadores externos opacos

`CLIENTE_ID`, `VENTA_ID` y `ARTICULO_ID` se guardan como enteros sin llave foránea y sin resolución. El módulo nunca pregunta quién es el cliente 24037: recibe el entero y lo devuelve. Es lo que permite que el esquema corra en un motor que no tiene Microsip al lado.

### 2.4 Sin lógica en la base

Aplicación estricta de la regla dura #1 de `CLAUDE.md`. Sin `DEFAULT`, sin triggers, sin procedimientos, sin `CHECK` de reglas de negocio. IDs con `uuid.New()`, timestamps con `time.Now()` envueltos en `firebird.ToWallClock` al escribir.

Única excepción, ya contemplada por la regla: **un generador propio del módulo, `GEN_MSP_GA_FOLIO`**, para asignación atómica de folio. No se comparte con `ventas`, y sobrevive la migración porque en PostgreSQL es una `SEQUENCE`.

## 3. Modelo de datos

Cuatro tablas. La decisión estructural es que **el artículo es una entidad con ciclo de vida propio**, no un campo de texto del folio.

### 3.1 `MSP_GA_GARANTIA` — el folio

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK, UUID generado en Go |
| `FOLIO` | `VARCHAR(12)` ASCII | UNIQUE. Formato `GA-000123`, legible en campo |
| `ORIGEN` | `VARCHAR(10)` ASCII | `piso` \| `cliente` |
| `CLIENTE_ID` | `INTEGER` | NULL si `piso`. Opaco, sin FK |
| `VENTA_ID` | `INTEGER` | NULL si `piso`. Opaco, sin FK |
| `ESTADO_CUENTA` | `VARCHAR(20)` ASCII | `liquidada` \| `saldo_pendiente`. NULL si `piso` |
| `ESTADO` | `VARCHAR(24)` ASCII | Estado del folio (§4.1) |
| `DESCRIPCION` | `BLOB SUB_TYPE TEXT` UTF8 | La falla reportada |
| `VIGENCIA_HASTA` | `DATE` | Nullable. **Capturada por el operador**, no calculada (§9.2) |
| `CALLE` | `VARCHAR(300)` UTF8 | Snapshot del domicilio al abrir |
| `NUMERO_EXTERIOR` | `VARCHAR(20)` UTF8 | |
| `COLONIA` | `VARCHAR(100)` UTF8 | |
| `LOCALIDAD` | `VARCHAR(100)` UTF8 | |
| `CIUDAD` | `VARCHAR(100)` UTF8 | |
| `CODIGO_POSTAL` | `VARCHAR(10)` UTF8 | |
| `GPS_LAT`, `GPS_LON` | `DOUBLE PRECISION` | Del domicilio |
| `ABIERTO_POR` | `VARCHAR(64)` UTF8 | |
| `CERRADO_EN` | `TIMESTAMP` | |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | NOT NULL, ambos desde Go |

El domicilio es un **snapshot al abrir**, no una copia perezosa: si el cliente se muda, el folio conserva a dónde se fue a recoger.

### 3.2 `MSP_GA_ARTICULO` — la cosa física bajo custodia

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `GARANTIA_ID` | `CHAR(36)` ASCII | FK → `MSP_GA_GARANTIA` |
| `ROL` | `VARCHAR(12)` ASCII | `original` \| `reemplazo` |
| `ARTICULO_ID` | `INTEGER` | El de Microsip. Opaco, sin FK, nullable |
| `CLAVE` | `VARCHAR(30)` UTF8 | SKU legible |
| `DESCRIPCION` | `VARCHAR(300)` UTF8 | |
| `RUTA` | `VARCHAR(12)` ASCII | `proveedor` \| `taller`. NULL hasta el diagnóstico |
| `ETAPA` | `VARCHAR(28)` ASCII | Dónde va en el proceso (§4.2) |
| `UBICACION` | `VARCHAR(28)` ASCII | Dónde está físicamente (§4.3) |
| `DICTAMEN` | `VARCHAR(16)` ASCII | `aceptada` \| `rechazada` \| `sin_falla`. Solo ruta proveedor |
| `DESENLACE` | `VARCHAR(16)` ASCII | `reparado` \| `reemplazado` \| `devuelto` \| `segunda_mano` \| `desarmado` \| `merma` |
| `CERRADO_EN` | `TIMESTAMP` | |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

**`ETAPA` y `UBICACION` son ortogonales y por eso son columnas distintas.** Un artículo puede estar en etapa `dictamen_recibido` y ubicación `proveedor` porque todavía no lo regresan. El requisito es saber dónde está cada cosa; eso merece una columna propia y un índice propio, no deducirse de la etapa.

**Un cambio físico crea una segunda fila con `ROL='reemplazo'`.** El original sigue su camino hasta `segunda_mano`, `desarmado` o `merma`; el reemplazo va al cliente. Dos filas, dos etapas, un solo folio. Esto es lo que permite que el folio esté `cerrado` mientras el artículo original sigue en `standby` — el requisito explícito del documento de proceso, imposible de representar con una sola columna de estado.

### 3.3 `MSP_GA_EVENTO` — la línea de tiempo, inmutable

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `GARANTIA_ID` | `CHAR(36)` ASCII | FK → `MSP_GA_GARANTIA` |
| `ARTICULO_REF` | `CHAR(36)` ASCII | Nullable: los eventos del folio no apuntan a un artículo |
| `TIPO` | `VARCHAR(28)` ASCII | |
| `DESCRIPCION` | `BLOB SUB_TYPE TEXT` UTF8 | |
| `ETAPA_DESDE` | `VARCHAR(28)` ASCII | |
| `ETAPA_HASTA` | `VARCHAR(28)` ASCII | |
| `UBICACION_DESDE` | `VARCHAR(28)` ASCII | Nullable. Sólo en eventos que mueven el mueble (migración `000070`) |
| `UBICACION_HASTA` | `VARCHAR(28)` ASCII | Ídem |
| `USUARIO` | `VARCHAR(64)` UTF8 | |
| `ROL_DECISOR` | `VARCHAR(16)` ASCII | `carpinteria` \| `oficina` \| `tecnica`. Nullable; solo en eventos de decisión |
| `GPS_LAT`, `GPS_LON` | `DOUBLE PRECISION` | Posición del agente al guardar |
| `DEVICE_CREATED_AT` | `TIMESTAMP` | Reloj del teléfono (offline-first) |
| `CREATED_AT` | `TIMESTAMP` | Reloj del servidor al recibir |
| `CLAVE_IDEMPOTENCIA` | `CHAR(36)` ASCII | UNIQUE |
| `HUELLA` | `CHAR(64)` ASCII | Nullable. SHA-256 en hex del contenido de la petición, sin la clave (§6). Migración `000071` |
| `DISTANCIA_ENTREGA_M` | `DOUBLE PRECISION` | Nullable. Sólo en `folio_cerrado`: metros entre el GPS de la entrega y el de la recolección más lejana (§4.1). Migración `000071` |
| `AUDITORIA_GPS` | `VARCHAR(20)` ASCII | Nullable. Sólo en `folio_cerrado`: `sin_desvio` \| `con_desvio` \| `no_aplica`. Migración `000071` |

**Sin `UPDATED_AT`, sin `UPDATED_BY`, sin columna de borrado lógico.** Un evento de auditoría no se edita; si algo salió mal se agrega un evento de corrección. En un expediente que puede acabar en una reclamación del cliente, la línea de tiempo tiene que ser evidencia y no una opinión editable.

**`ROL_DECISOR` existe porque quien decide no es fijo.** La decisión de si una reparación es rápida puede tomarla carpintería, oficina o el área técnica, según la situación; y la autorización de un cambio físico la toma quien tenga el permiso, que puede ser cualquiera de las tres. El permiso (`garantias:actualizar`, `garantias:autorizar`) controla **si puede**; `ROL_DECISOR` registra **desde qué rol lo hizo**. Son cosas distintas y por eso no se colapsan: sin el rol, dentro de seis meses no hay forma de saber si un cambio caro lo autorizó oficina o lo decidió el carpintero.

`ETAPA_DESDE` / `ETAPA_HASTA` se guardan explícitamente. Sin ellas, reconstruir en qué estado estaba un artículo en una fecha dada obliga a recalcular desde el origen, y basta con que una etapa cambie de nombre para que el histórico mienta.

`CLAVE_IDEMPOTENCIA` es lo que hace viable el offline-first: el teléfono genera el UUID, y si sincroniza dos veces por mala señal el `UNIQUE` rechaza el duplicado mientras el servidor responde éxito.

### 3.4 `MSP_GA_IMAGEN` — evidencia

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `EVENTO_ID` | `CHAR(36)` ASCII | FK → `MSP_GA_EVENTO`, **sin** `ON DELETE CASCADE` |
| `TIPO` | `VARCHAR(12)` ASCII | `evidencia` \| `firma`. Go la escribe **siempre**; una fila anterior a la `000071` tiene `NULL` y se **lee** como `evidencia` |
| `RUTA` | `VARCHAR(500)` UTF8 | Ruta relativa dentro de `STORAGE_DIR` |
| `DESCRIPCION` | `VARCHAR(500)` UTF8 | |
| `SUBIDA_POR` | `VARCHAR(64)` UTF8 | |
| `HUELLA` | `CHAR(64)` ASCII | Nullable. Huella de la subida (§6). Migración `000071` |
| `CREATED_AT` | `TIMESTAMP` | |

El `ID` de la imagen puede venir de fuera: en la subida de evidencia es el `Idempotency-Key`, y en la firma lo deriva el dominio de la clave de `Entregar`. Así un reintento cae sobre la misma llave primaria en vez de crear una segunda fila.

La evidencia cuelga del **evento**, no del folio. `RUTA` (no `URL`) porque es filesystem local por [ADR-0003](../../adr/0003-storage-deferred.md), y en UTF8 porque un path con acentos no debe reventar.

### 3.5 Borrado

**No hay borrado, ni físico ni lógico.** Ninguna tabla lleva bandera de estado técnico. Un folio abierto por error se **cancela** (`ESTADO='cancelado'`, con su evento y su motivo), que es un hecho del negocio. Las fotos nunca se borran en cascada: son la evidencia de en qué estado se recogió el mueble.

### 3.6 Índices

```
MSP_GA_GARANTIA:  UNIQUE(FOLIO), (CLIENTE_ID), (ESTADO, CREATED_AT), (CREATED_AT)
MSP_GA_ARTICULO:  (GARANTIA_ID), (ETAPA), (UBICACION)
MSP_GA_EVENTO:    (GARANTIA_ID, DEVICE_CREATED_AT), UNIQUE(CLAVE_IDEMPOTENCIA)
MSP_GA_IMAGEN:    (EVENTO_ID)
```

Indexado sobre lo que consulta el tablero operativo: etapa y ubicación.

## 4. Estados y transiciones

Dos máquinas separadas. Esa separación es lo que hace que el modelo aguante el requisito de flujos concurrentes.

### 4.1 Folio — seis estados

```
abierto → en_proceso → listo_entrega → entregado → cerrado
   └──────────────── cancelado (terminal) ─────────────┘
```

`entregado` y `cerrado` son distintos a propósito: entre ambos ocurre la auditoría que pide el proceso — comparar el GPS de la entrega contra el de la recolección para detectar desvíos — más la firma de conformidad. Si fueran un solo estado, esa validación no tendría dónde vivir.

**Decidido el 2026-10-26: las dos entran en v1.**

**Firma de conformidad, en `Entregar`.**
- `Entregar` exige la firma del cliente. Sin firma → **422 `warranty_signature_required`** (`ErrFirmaRequerida`), sin tocar el folio ni encolar eventos.
- La firma es una imagen de `TIPO='firma'` colgada del evento `folio_entregado` que la misma llamada crea. La crea el agregado (`ImagenesPendientesForRepo`) y `GarantiaRepo.Guardar` la inserta en la **misma transacción** que el evento, después de él. No hay entrega sin firma ni firma sin entrega.
- Llega en la misma petición que `Entregar` (multipart, §6). El blob se guarda antes de la transacción con una ruta única por intento; si al final la fila de la imagen no quedó registrada con **esa** ruta (error o reintento), el blob se borra.
- La subida genérica de evidencia **nunca** crea firmas: siempre escribe `TIPO='evidencia'`.

**Auditoría GPS, en `Cerrar`.**
- `Cerrar` calcula la distancia haversine (radio 6,371,000 m) entre el GPS del evento `folio_entregado` (obligatorio desde R9) y el GPS de cada evento de recolección del folio (`etapa_avanzada` con `ETAPA_HASTA='recolectado'`), y se queda con la **mayor**.
- Umbral: **300 m**, constante del dominio (`UmbralDesvioMetros`), documentada como ajustable: cambiarla es editar una constante y correr las pruebas.
- Distancia **mayor** que el umbral → desvío. `Cerrar` exige entonces `justificacion_desvio` no vacía; sin ella → **422 `warranty_deviation_justification_required`** (`ErrJustificacionDesvioRequerida`). **No bloquea**: con justificación, el folio se cierra.
- Si el folio no tiene recolección con GPS (origen piso, o una recolección capturada sin coordenadas), la auditoría **no aplica**: el folio se cierra y el evento lo dice.
- El evento `folio_cerrado` guarda `DISTANCIA_ENTREGA_M` (nula cuando no aplica), `AUDITORIA_GPS` (`sin_desvio` \| `con_desvio` \| `no_aplica`) y, si la hubo, la justificación en `DESCRIPCION`.
- El agregado no carga la línea de tiempo (decisión 7 de la tarea 4): `app` lee los eventos del folio dentro de la transacción, ya con el candado, y le pasa al dominio los puntos. El cálculo y la regla viven en el dominio.

### 4.2 Artículo — diecinueve etapas

Catálogo completo, agrupado por fase:

| Fase | Etapas |
|---|---|
| Tronco común | `registrado` · `pendiente_recoleccion` · `recolectado` · `en_revision` |
| Ruta proveedor | `orden_generada` · `enviado_proveedor` · `dictamen_recibido` · `reparado_proveedor` · `espera_respuesta_cliente` |
| Ruta taller | `en_taller` · `reparado_taller` |
| Convergencia | `cambio_autorizado` · `listo_entrega` · `entregado` · `reingresado_inventario` |
| Flujo paralelo | `standby` · `segunda_mano` · `desarmado` · `merma` |

**Tronco común**

```
registrado → pendiente_recoleccion → recolectado → en_revision
             (solo origen cliente)                      │
                                    el diagnóstico fija RUTA y bifurca:
                                    RUTA=proveedor → orden_generada
                                    RUTA=taller    → en_taller
```

**Ruta proveedor**

```
orden_generada → enviado_proveedor → dictamen_recibido
                                          │
        ┌─────────────────────────────────┼──────────────────────────┐
     aceptada                         rechazada                  sin_falla
        │                                 │                          │
  reparado_proveedor              listo_entrega            espera_respuesta_cliente
        │                     (DESENLACE=devuelto,                   │
  listo_entrega                se lo queda el cliente)   ┌───────────┼───────────┐
                                                      acepta    no acepta   no se pudo
                                                         │           │       devolver
                                                  listo_entrega      │          │
                                                          cambio_autorizado  standby
```

La rama `no se pudo devolver` cubre el caso excepcional en que el cliente nunca responde o se niega a recibir el producto funcional. El camino normal es devolvérselo; esta salida existe para que el artículo no quede atorado en `espera_respuesta_cliente` indefinidamente.

**Ruta taller**

```
en_taller → ¿reparación rápida?
              ├── sí → reparado_taller → listo_entrega
              └── no → cambio_autorizado
```

**Cambio físico** — alcanzable desde ambas rutas. El artículo original sale del camino del cliente y pasa a `standby`; se crea la fila `ROL='reemplazo'`, que entra directo a `listo_entrega`.

**Cierre**

```
listo_entrega → entregado                  (terminal, origen cliente)
en_revision   → reingresado_inventario     (terminal, origen piso)
```

**Flujo paralelo** — corre independiente del folio:

```
standby → segunda_mano | desarmado | merma   (los tres terminales)
```

`desarmado` es terminal: se registra que el artículo se desarmó, con nota y evidencia de lo aprovechado. Las piezas no se rastrean individualmente.

### 4.3 Ubicaciones

```
domicilio_cliente · en_transito · almacen_revision · taller ·
proveedor · almacen_segunda_mano · entregado · baja
```

### 4.4 Invariante: no hay cambio de etapa sin evento

El agregado produce el cambio de estado y el evento correspondiente **en la misma operación**, y `GarantiaRepo.Guardar` los persiste en la misma transacción. No existe un camino que mueva un artículo sin dejar rastro. `ETAPA_DESDE` / `ETAPA_HASTA` los llena el agregado, no quien llama.

### 4.5 Dónde viven las transiciones

En un solo archivo, `internal/garantias/domain/transiciones.go`, como una tabla de etapa → etapas permitidas. No repartidas en condicionales por los comandos.

Es deliberado: el catálogo de etapas proviene del documento de proceso, que describe el flujo ideal (§9). Cuando el levantamiento en taller obligue a cambiarlo, el cambio debe ser **editar una tabla en Go y correr los tests** — sin migración, sin datos que tocar, sin `CHECK` en la base que actualizar. Ésta es la razón concreta por la que la regla dura #1 se aplica aquí sin excepciones.

## 5. Puertos outbound

| Puerto | Operaciones |
|---|---|
| `GarantiaRepo` | `Guardar(agregado)` · `Obtener(id)` · `ObtenerPorFolio` · `Listar(filtros)` |
| `EventoRepo` | `ListarPorGarantia` — **solo lectura** |
| `ImagenRepo` | `Registrar` · `Obtener(id)` · `ListarPorEvento` · `ListarPorEventos(ids)` — una sola consulta para todo el detalle |
| `StorageProvider` | `Guardar(bytes)` · `Abrir` · `Eliminar` |
| `FolioGenerator` | `Siguiente()` |
| `Identity` | `UsuarioActual()` · `TienePermiso()` |
| `Clock` | `Now()` |
| `IDGenerator` | `Nuevo()` |

`EventoRepo` es de solo lectura por diseño: los eventos no se escriben por su cuenta, sino como eventos de dominio pendientes que el agregado acumula y `GarantiaRepo.Guardar` persiste atómicamente. Así la invariante de §4.4 no depende de la disciplina de quien escribe el comando.

`StorageProvider` tiene una única implementación, `FilesystemProvider` sobre `STORAGE_DIR`, siguiendo el patrón ya establecido en `ventas` y `cobranza` y la decisión de ADR-0003. No hay selector ni backend alterno.

## 6. Endpoints

```
POST   /v2/garantias                                 abrir folio (piso o cliente)              crear
GET    /v2/garantias                                 bandeja: filtros + paginado               leer
GET    /v2/garantias/catalogos                       etapas, ubicaciones, transiciones válidas leer
GET    /v2/garantias/{id}                            folio + artículos + timeline + imágenes   leer
POST   /v2/garantias/{id}/iniciar-proceso            abierto → en_proceso                      actualizar
POST   /v2/garantias/{id}/listo-entrega              en_proceso → listo_entrega                actualizar
POST   /v2/garantias/{id}/entregar                   GPS + firma (multipart)                   cerrar
POST   /v2/garantias/{id}/cerrar                     auditoría GPS, cierre                     cerrar
POST   /v2/garantias/{id}/cancelar                                                             cerrar
POST   /v2/garantias/{id}/articulos                  agregar artículo al folio                 crear
POST   .../articulos/{aid}/avanzar                   transición simple                         actualizar
POST   .../articulos/{aid}/diagnostico               fija RUTA                                 actualizar
POST   .../articulos/{aid}/dictamen                  solo ruta proveedor                       actualizar
POST   .../articulos/{aid}/mover                     ubicación manual (R3)                     actualizar
POST   .../articulos/{aid}/cambio-fisico             crea reemplazo, original → standby        autorizar
POST   .../articulos/{aid}/no-devuelto               espera_respuesta_cliente → standby (R8)   autorizar
POST   .../articulos/{aid}/desenlace                 standby → segunda_mano|desarmado|merma    autorizar
POST   /v2/garantias/{id}/eventos/{eid}/imagenes     evidencia (multipart)                     actualizar
GET    /v2/garantias/{id}/eventos/{eid}/imagenes/{iid}   el binario de la imagen               leer
```

Diecinueve operaciones: quince `POST` y cuatro `GET`. `iniciar-proceso`, `listo-entrega`, `mover` y `no-devuelto` se agregaron con los comandos de la tarea 8; la descarga de la imagen, con la tarea 10.

`avanzar` cubre las transiciones que no cargan datos adicionales; los cinco endpoints específicos existen porque sí los cargan.

**`GET /v2/garantias/catalogos` proyecta la tabla de `transiciones.go`.** Con eso el frontend no reimplementa la máquina de estados: pregunta qué acciones son válidas para la etapa actual. Cuando el catálogo cambie tras el levantamiento de campo, el frontend se entera solo, sin despliegues coordinados.

**Idempotencia (decidido el 2026-10-26): la del dominio, no el middleware.** Este párrafo decía antes que se apoyaba en `internal/platform/idempotency`; ya no.

- Toda ruta `POST` exige el encabezado **`Idempotency-Key`**, UUID. Sin él, o con un valor que no es UUID → 422, antes del handler (`garhttp.EncabezadoIdempotencia`).
- La clave viaja al comando y se guarda en el evento (`CLAVE_IDEMPOTENCIA`, `UNIQUE`), junto con la **huella**: SHA-256 del contenido **completo** del comando — todos sus campos, incluidos los de la ruta, menos la clave misma —, prefijado con el nombre del comando para que dos comandos con la misma forma no compartan huella.
- **Misma clave y misma huella → 200** con el estado actual del folio, sin escribir nada.
- **Misma clave y huella distinta → 409 `warranty_idempotency_key_mismatch`.** Incluye una fila sin huella: nunca se toma como repetición.
- Misma clave en otro folio → 409 `warranty_idempotency_key_other_folio`, como hoy.
- La subida de evidencia usa la misma regla, con la clave como `ID` de la imagen y la huella en `MSP_GA_IMAGEN.HUELLA`; la huella incluye los bytes de la imagen.
- Las rutas de garantías **no** pasan por el middleware `platform/idempotency`. La clave y la huella viven en el evento que registra el cambio, en la misma transacción, y el reintento se resuelve con el candado del folio ya tomado; el módulo, sellado, no depende de una tabla de claves aparte.
- Todas las rutas `POST` responden **200** con el folio, también la primera vez (abrir incluido). El teléfono trata igual el primer envío y el reintento.

Wiring con Huma + chi según `docs/module-standards/HUMA_WIRING.md`. Fechas RFC3339 UTC en el contrato, por `DATETIME_HANDLING.md`. UTF-8 en todas las columnas `MSP_GA_*`, por `ENCODING_HANDLING.md`.

## 7. Permisos

| Código | Cubre |
|---|---|
| `garantias:leer` | Bandeja, consulta, catálogos y descarga de imágenes |
| `garantias:crear` | Abrir folio, agregar artículos |
| `garantias:actualizar` | Iniciar proceso, avanzar, diagnóstico, dictamen, mover, listo para entrega, evidencia |
| `garantias:autorizar` | Cambio físico, no devuelto y desenlace del standby |
| `garantias:cerrar` | Entregar, cerrar, cancelar |

`garantias:autorizar` está separado porque el cambio físico lo autoriza oficina, no campo. Un cobrador no debe poder autorizarse un reemplazo desde el teléfono: es una decisión con costo, y si el permiso no la separa, la política queda a la buena voluntad.

## 8. Verificación

Compuertas estándar de `docs/module-standards/TESTING_REQUIREMENTS.md`, sin descuento:

| Paquete | Piso de cobertura |
|---|---|
| `internal/garantias/domain` | ≥ 99% |
| `internal/garantias/app` | ≥ 90% |
| `internal/garantias/infra/garfb` | ≥ 80% (requiere `FB_DATABASE`) |
| `internal/garantias/infra/storage` | ≥ 85% |
| `internal/garantias/infra/garhttp` | ≥ 70% |
| Mutación (`domain` + `app`) | kill-rate ≥ 80% |

Adicionalmente, específico de este módulo:

**La tabla de transiciones se prueba exhaustivamente.** Es finita: se recorre entera y se verifica que toda transición no listada sea rechazada. Es el corazón del módulo y no admite muestreo.

**Un e2e por rama:** origen piso; origen cliente por ruta proveedor; origen cliente por ruta taller. El de taller incluye cambio físico, con la fila de reemplazo entregada y el artículo original terminando en `standby` **con el folio ya cerrado** — la verificación directa de que el requisito de flujos concurrentes quedó satisfecho.

**Barrido de seguridad** table-driven sobre **todas** las operaciones que registra la API (diecinueve, §6), verificando el permiso exigido en cada una. La tabla se compara contra las operaciones del OpenAPI: una ruta nueva sin su fila hace fallar la prueba.

**`make check-sealed`** corre en cada entrega vía `pre-push`, no al final. Es lo que impide que el módulo se cablee a `clientes` o a `ventas` y que se descubra semanas después.

Los tests de integración se envuelven en `fbtestutil.WithTestTransaction` para que las escrituras se reviertan y la base compartida de desarrollo no acumule estado.

## 9. Supuestos a validar en campo

### 9.1 Confirmado

- **Dictamen `rechazada`:** el artículo se lo queda el cliente. Va a `listo_entrega` con `DESENLACE='devuelto'` y se entrega sin reparar.
- **Dictamen `sin_falla` sin respuesta del cliente:** el camino normal es devolverlo. La salida a `standby` cubre el caso excepcional en que no se pudo entregar.
- **Quién decide la reparación rápida:** varía — carpintería, oficina o técnica, según la situación. Por eso el rol se registra como dato (`ROL_DECISOR`) en vez de codificarse como permiso único.
- **Autorización del cambio físico:** siempre se autoriza, pero únicamente por quien tenga el permiso. `garantias:autorizar` queda como permiso separado.

### 9.2 Pendiente de confirmar

1. **La redistribución del catálogo del documento.** Los diecinueve valores de estado se repartieron en tres ejes — estado de folio (§4.1), etapa de artículo (§4.2) y ubicación física (§4.3) — fusionando los que parecen el mismo momento visto desde áreas distintas y añadiendo los que el proceso implica pero no nombra (`standby`, `registrado`). Los estados de origen son reales, pero se espera que algunos se fusionen por redundancia al contrastarlos con el taller.
2. **Vigencia de la garantía — fuera de alcance por ahora, por decisión.** El plazo varía y no hay regla escrita. `VIGENCIA_HASTA` es un campo **que captura el operador**: el sistema acepta la fecha que se le indique y no valida ninguna política. No hay cálculo, no hay tabla de plazos por tipo de producto, y abrir un folio fuera de vigencia no se bloquea. Cuando exista la regla, se añade la validación en el comando de apertura sin tocar el esquema.

Ninguno bloquea la construcción: el primero se resuelve editando `transiciones.go` y el segundo, agregando una validación en el comando de apertura. Esa es precisamente la razón del diseño de §4.5.

## 10. Descomposición del trabajo

Dos reglas gobiernan el corte: los contratos se fijan antes que nada, y **dos tareas nunca escriben el mismo archivo**.

**Tanda 0 — el molde (secuencial, bloquea todo)**
1. ~~Generalizar `make check-sealed` + regla `garantias-sealed`~~ — **hecho** en `main` (`f6c53ab`). Basta con sacar la rama de `main` al día.
2. Migración `000050` — las cuatro tablas y el generador de folio. Confirmar antes que el número siga libre.
3. `domain/` — value objects, catálogos, entidades, `transiciones.go`, errores centinela.
4. `ports/outbound/` + `garantias_contracts.go`.

**Tanda 1 — paralela, sin contacto entre tareas**
- Comandos de apertura y recolección
- Comandos de diagnóstico y ruta
- Comandos de cierre y entrega
- Consultas (bandeja, folio con timeline, catálogos)
- Repositorio Firebird (`infra/garfb`)

**Tanda 2 — paralela**
- `infra/storage` (`FilesystemProvider`)
- `infra/auth` (adaptador de `Identity`)
- `infra/garhttp` (DTOs, handlers, rutas)

**Tanda 3 — cierre, secuencial**
- `module.go` con el `fx.Option` y registro de permisos en el composition root
- Integración Firebird con `WithTestTransaction`
- Los tres e2e

**Tareas 9 y 10 — cierre de v1, en paralelo (26 al 30 de octubre)**

Arrancan de `feat/garantias-integracion`, que junta el dominio y los comandos (PR #30), las consultas y el HTTP de lectura (PR #29) y un commit de **contrato** que ninguna de las dos vuelve a tocar: tipos de imagen y de auditoría, campos nuevos de `Imagen` y `Evento`, centinelas, `ImagenRepo.Obtener` y `ListarPorEventos`, `Deps.Comandos` en `garhttp`, el encabezado de idempotencia y el archivo vacío `routes_comandos.go`. El PR #26 se cierra: #30 es la única fuente de comandos. Las correcciones pendientes de #29 y #30 se terminan en paralelo y se fusionan hacia adelante a la rama de integración.

- **Tarea 9 (Kevin)** — [`garantias-task-9-brief.md`](../plans/garantias-task-9-brief.md): reglas de dominio de la firma y de la auditoría GPS, la huella completa en `app`, las catorce rutas `POST` de comandos y los tres e2e por HTTP contra `garfb`.
- **Tarea 10 (Ruben)** — [`garantias-task-10-brief.md`](../plans/garantias-task-10-brief.md): migración `000071` y su persistencia, subida y descarga de imágenes, catálogos, detalle con imágenes, `module.go` y el cableado en `cmd/api`, los cinco permisos en `auth`, el barrido de seguridad final y los targets de cobertura y mutación.

## 11. Referencias

- `CLAUDE.md` §1 (sin lógica en la base), §2 (vertical slices), §3 (idioma), §5 (restricciones de stack)
- [ADR-0003](../../adr/0003-storage-deferred.md) — almacenamiento en filesystem local
- [ADR-0009](../../adr/0009-asistencia-sealed-module.md) — patrón de módulo sellado y extraíble
- `docs/module-standards/MODULE_TEMPLATE.md`, `AGGREGATE_PATTERNS.md`, `CQRS_PATTERN.md`, `HUMA_WIRING.md`, `DATETIME_HANDLING.md`, `ENCODING_HANDLING.md`, `TESTING_REQUIREMENTS.md`
