# Comprobantes por WhatsApp — diseño

- **Fecha:** 2026-07-29
- **Estado:** aprobado, listo para plan de implementación
- **Módulo:** `internal/comprobantes`
- **Migración:** `000049_create_msp_cm_comprobantes`
- **Prefijo de tablas:** `MSP_CM_*`

## 1. Objetivo y alcance

Mandarle al cliente un comprobante en PDF por WhatsApp cuando su venta se registra en Microsip, y cuando su pago se aplica en Microsip.

### Dentro del alcance

- Detección de los dos hechos, cubriendo **ambos orígenes** de captura: nuestra API y la interfaz de Microsip.
- Generación del PDF del comprobante.
- Una ventana de espera con botón para detener el envío, operable desde la aplicación de escritorio.
- El canal de envío, con implementación local para pruebas y la API de WhatsApp Business para producción.
- Registro auditable de qué se envió, a quién, cuándo y por qué canal.

### Fuera del alcance

- **Validar o aprobar pagos.** Los pagos siguen entrando a Microsip exactamente como hoy. Ver §4.1.
- CFDI o cualquier comprobante fiscal.
- La pantalla de la aplicación de escritorio (proyecto aparte; este spec define los endpoints que consume).

### Por qué un módulo nuevo

Atiende dos módulos —`ventas` y `cobranza`— así que vivir dentro de uno de ellos dejaría la mitad del código en el lugar equivocado. No es un módulo sellado: consume contratos de `ventas`, `cobranza` y `clientes` a través de puertos, que es la regla general del repositorio (`CLAUDE.md` §2).

### Nota sobre el número de migración

`000047` está reservada por el spec de garantías y `000048` por el de asistencia. Ninguna tiene código escrito y asistencia está en pausa, pero se respetan las reservas para que no haya dos documentos reclamando el mismo número — el error exacto que ya ocurrió una vez. Comprobantes toma `000049`.

## 2. Los dos disparadores

Son distintos, y la razón importa.

### 2.1 Ventas — el evento de dominio que ya existe

`venta.aplicada` (`internal/ventas/domain/events.go`) se emite cuando la venta se materializa en Microsip y ya se encola en `MSP_OUTBOX_EVENTS`. Hoy tiene un consumidor: `ventoutbox/search_handler.go`, que reindexa en Meilisearch.

Se agrega un segundo consumidor. **Cero trabajo de detección**, y el patrón ya está establecido.

Es el momento semánticamente correcto: en ventas hay un proceso previo en nuestras tablas, y el comprobante no debe salir hasta que la venta exista en Microsip.

### 2.2 Pagos — el changelog, no un evento nuestro

Los pagos entran por dos caminos: nuestra API (que los inserta ya aplicados — `cobranza/infra/microsip/pago_writer.go` pone `APLICADO='S'`) y la captura directa en la interfaz de Microsip por parte de oficina.

Un evento de dominio nuestro solo vería el primero. **`MSP_PAGOS_CHANGELOG` los ve todos**, porque cuelga de los triggers de Firebird ([ADR-0007](../../adr/0007-cobranza-push-watermark.md)): no le importa quién escribió la fila.

Y hay un segundo beneficio que no es obvio: **usar un solo mecanismo evita el doble envío.** Si se disparara desde nuestro outbox *y* desde el changelog, un pago capturado en la app saldría dos veces.

> **Hecho verificado (2026-07-29, base de desarrollo):** `IMPORTES_DOCTOS_CC` tiene 2,173,264 renglones con `TIPO_IMPTE='R'` y **cero** en `APLICADO='N'`. `MSP_RECOMPUTE_PAGO` lee y cachea `IDC.APLICADO` sin filtrarlo, así que la transición a aplicado es observable por el changelog. Nuestra API inserta ya aplicado, de modo que para ese origen "insertado" y "aplicado" coinciden.

### 2.3 Un pago, un comprobante

Un documento de pago puede acreditar varios cargos, y entonces `IMPORTES_DOCTOS_CC` tiene varios renglones para el mismo pago.

> **Medido (2025):** 343,266 documentos de pago contra 344,118 renglones de importe. Solo 0.25% acredita más de un cargo.

Es raro, pero cuando pasa un cliente recibiría tres WhatsApps del mismo pago. **Se agrupa por `DOCTO_CC_ID`, no por `IMPTE_DOCTO_CC_ID`.**

## 3. Durabilidad

El listener de ADR-0007 publica a un bus en memoria para alimentar el SSE. Si la API se reinicia entre el evento y el envío, ese envío se pierde.

Se copia la arquitectura que ADR-0007 ya validó, con la misma división de responsabilidades:

```
trigger Firebird → MSP_PAGOS_CHANGELOG (SEQ_ID)
                        ↓
       worker con cursor sobre SEQ_ID   ←   POST_EVENT solo lo despierta antes
                        ↓
        crea MSP_CM_ENVIO en estado 'en_espera' (y renderiza el PDF)
                        ↓
       worker de envío: reclama, manda, registra
```

**`POST_EVENT` da latencia baja. El cursor sobre `SEQ_ID` da la garantía de no perder nada.** Si la API estuvo caída dos horas, al arrancar el cursor sigue donde estaba y procesa lo pendiente.

El outbox se usa solo para el paso `venta.aplicada` → encolar, que es instantáneo. `MSP_CM_ENVIO` **es** la cola de envío: tiene hora programada y estado gobernado por una persona, cosas que el outbox no modela.

## 4. La ventana y la garantía del botón

### 4.1 Se bloquea el comprobante, no el pago

Se evaluó mover la validación de pagos a nuestra aplicación de escritorio, para controlar desde nuestro sistema cuándo sale el comprobante. **Se rechazó para v1**, por tres razones:

1. **Cambia el camino del dinero.** Poner una compuerta significa que los pagos esperan a que alguien actúe. Si nadie actúa, el pago no existe en Microsip: el saldo del cliente está mal y el efectivo del cobrador no cuadra.
2. **Depende de una conducta que no ocurre.** La investigación del checador lo documentó: *"administración solo revisa los registros; no se va a poner a corregirlos a mano. Cualquier diseño que dependa de que alguien haga clic en una lista de pendientes se degrada a 'nadie lo cierra nunca'."* A 940 pagos diarios (§10) nadie revisa esa lista.
3. **No elimina el otro camino.** Oficina va a seguir capturando pagos en Microsip, así que el changelog seguiría siendo necesario.

Lo que sí se construye: el pago entra como siempre, y **el comprobante espera una ventana con botón para detenerlo.** El modo de falla es el correcto — si nadie mira, se manda igual.

Mover la validación a nuestro sistema es la arquitectura más profesional a largo plazo y es la dirección correcta, pero es un proyecto propio con sus permisos y su capacitación. Si algún día se hace, esta lista ya es la mitad del camino.

### 4.2 La garantía no es el tiempo real

El tiempo real hace que el botón desaparezca rápido. Lo que hace que **nunca mienta** es una transición atómica.

Es la misma distinción de ADR-0007: el push da latencia, la base da correctitud.

### 4.3 Estados

```
en_espera → enviando → enviado
     ↓          ↓
  detenido   fallido
```

Más `sin_telefono`, terminal (§5.1).

**`enviando` no es un hueco, es un estado visible.** Entre el momento en que se decide mandar y el momento en que WhatsApp acepta el mensaje, cancelar es imposible: ya salió de nuestras manos. La pantalla lo muestra como "enviando…" con el botón **deshabilitado**, no como un botón que se esfuma sin explicación.

### 4.4 El claim atómico

Las dos operaciones que compiten hacen lo mismo:

```sql
-- el worker de envío, cuando le toca
UPDATE MSP_CM_ENVIO SET ESTADO = 'enviando' WHERE ID = ? AND ESTADO = 'en_espera'

-- el botón de detener
UPDATE MSP_CM_ENVIO SET ESTADO = 'detenido' WHERE ID = ? AND ESTADO = 'en_espera'
```

Quien afecta un renglón, gana. **Quien afecta cero renglones, perdió**, y eso es una respuesta, no un error. Firebird serializa las dos actualizaciones sobre la misma fila; no hay empate posible.

`POST /detener` responde `detenido` o `ya_enviado`. Nunca un error. La pantalla dice "ya se envió" con esas palabras — lo que no puede pasar es que diga "detenido" cuando el mensaje ya salió.

### 4.5 Cuenta regresiva del servidor

Cada envío guarda `PROGRAMADO_PARA`. La pantalla muestra *"se envía en 4:32"* junto al botón.

Que la cuenta salga de una fecha guardada y no de un temporizador del navegador importa: si alguien cierra la pantalla y la reabre, o se le cae el internet, al reconectar ve el tiempo correcto. Un temporizador del cliente se reiniciaría y mostraría una ventana que ya no existe.

### 4.6 Si el SSE se cae, nada se rompe

La pantalla se pone stale, el operador ve una cuenta desactualizada, aprieta detener y el servidor le dice "ya se envió". Incómodo, correcto. **La corrección no depende de que el canal en vivo funcione.**

### 4.7 La ventana

Configurable y distinta por tipo. Valor inicial: **5 minutos** para ambos.

Como `PROGRAMADO_PARA` se guarda por envío, cambiar el valor no afecta a los que ya están en cola: cada uno respeta la ventana con la que nació.

## 5. Modelo de datos

### 5.1 `MSP_CM_ENVIO` — la cola y el registro

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `TIPO` | `VARCHAR(12)` ASCII | `venta` \| `pago` |
| `REFERENCIA` | `VARCHAR(40)` ASCII | UUID de la venta, o `DOCTO_CC_ID` del pago |
| `CLIENTE_ID` | `INTEGER` | |
| `TELEFONO` | `VARCHAR(20)` ASCII | Snapshot al encolar |
| `ESTADO` | `VARCHAR(12)` ASCII | Ver §4.3 |
| `PROGRAMADO_PARA` | `TIMESTAMP` | |
| `DOCUMENTO_RUTA` | `VARCHAR(500)` UTF8 | Ruta relativa dentro de `STORAGE_DIR` |
| `CANAL` | `VARCHAR(20)` ASCII | `local` \| `whatsapp_business` |
| `MENSAJE_EXTERNO_ID` | `VARCHAR(64)` ASCII | El id que devuelve WhatsApp |
| `INTENTOS` | `SMALLINT` | |
| `ULTIMO_ERROR` | `VARCHAR(500)` UTF8 | |
| `DETENIDO_POR` | `VARCHAR(64)` UTF8 | |
| `ENVIADO_EN` | `TIMESTAMP` | |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

`UNIQUE (TIPO, REFERENCIA)`

**El `UNIQUE` es lo que garantiza un solo comprobante por hecho.** Si el cursor reprocesa un tramo del changelog tras un reinicio, el segundo intento choca con la restricción y se descarta. La idempotencia la impone la base, no la memoria del worker.

**`sin_telefono` es un estado terminal, no una falla.**

> **Medido (2026-07-29):** 10,866 de 15,792 clientes activos (`ESTATUS IN ('A','V')`) tienen `DIRS_CLIENTES.TELEFONO1` de 10 dígitos o más — **68.8%**. Uno de cada tres no tiene a dónde recibir nada.

Tratarlo como falla llenaría la bitácora de ruido y esconderían las fallas reales. Y vuelve consultable la pregunta "¿a cuántos no les pudimos avisar?", que el negocio va a querer.

`CANAL` guarda cuál implementación respondió, para que un envío de prueba con el sender local nunca se cuente como entregado de verdad.

### 5.2 `MSP_CM_CURSOR` — dónde quedó el recorrido

| Columna | Tipo | Notas |
|---|---|---|
| `CLAVE` | `VARCHAR(32)` ASCII | PK. `pagos_changelog` |
| `SEQ_ID` | `BIGINT` | |
| `UPDATED_AT` | `TIMESTAMP` | |

Una fila. Es lo que hace que un reinicio de la API no pierda ni duplique nada.

### 5.3 `MSP_CM_CONFIG` — una sola fila

| Columna | Tipo | Notas |
|---|---|---|
| `ID` | `CHAR(36)` ASCII | PK |
| `VENTANA_VENTA_MIN` | `SMALLINT` | Inicial: 5 |
| `VENTANA_PAGO_MIN` | `SMALLINT` | Inicial: 5 |
| `HABILITADO_VENTA` | `SMALLINT` | 0/1 |
| `HABILITADO_PAGO` | `SMALLINT` | 0/1 |
| `MAX_INTENTOS` | `SMALLINT` | |
| `CREATED_AT`, `UPDATED_AT` | `TIMESTAMP` | |

Los interruptores no son adorno: permiten arrancar solo con ventas, medir, y encender pagos después. Y apagar todo en un minuto si algo sale mal en producción, sin desplegar.

### 5.4 Índices

```
MSP_CM_ENVIO  UNIQUE(TIPO, REFERENCIA), (ESTADO, PROGRAMADO_PARA), (CLIENTE_ID), (CREATED_AT)
```

`(ESTADO, PROGRAMADO_PARA)` es el índice del worker: "dame los que están en espera y ya les tocó".

## 6. El comprobante

### 6.1 El PDF se genera al encolar, no al enviar

**Si el operador no puede ver el documento, ¿sobre qué decide cuando aprieta detener?**

La lista muestra un enlace de vista previa al PDF real, el mismo archivo que va a salir. Sin eso, el botón es un acto de fe.

Beneficio secundario: si la generación falla, falla temprano y visible, no en el momento del envío cuando ya nadie está mirando.

### 6.2 Contenido

**Comprobante de venta:** folio de Microsip y fecha · nombre y domicilio del cliente · artículos con cantidad y precio · total, enganche y saldo · el plan de pago en palabras claras (cuánto, cada cuándo, cuántas) · vendedor.

**Comprobante de pago:** folio y fecha · nombre del cliente · monto y forma de cobro · a qué venta se aplicó · **saldo restante después de este pago** · quién cobró.

Ese saldo restante es lo que el cliente de verdad quiere saber. Un comprobante que solo diga "recibimos $500" deja la pregunta abierta y genera la llamada que el comprobante debía evitar.

### 6.3 La etiqueta obligatoria

El documento dice, visible, que es un **comprobante informativo y no un CFDI**.

Un papel con folio, monto y sello que llega por WhatsApp se parece lo suficiente a un comprobante fiscal para que alguien lo trate como tal. Aclararlo cuesta una línea y evita un malentendido con consecuencias reales.

## 7. Puertos outbound

| Puerto | Da | Fuente |
|---|---|---|
| `VentaReader` | folio, artículos, totales, plan de pago | contratos de `ventas` |
| `PagoReader` | monto, forma de cobro, folio, cargo acreditado | `MSP_PAGOS_VENTAS` |
| `SaldoReader` | saldo restante | `MSP_SALDOS_VENTAS` |
| `ClienteReader` | nombre, teléfono, domicilio | contratos de `clientes` |
| `Renderer` | el PDF | `fpdf`, patrón de `clientespdf` |
| `Sender` | el envío | `LocalSender` / `WhatsAppBusinessSender` |
| `Storage` | guardar y leer el PDF | `FilesystemProvider`, [ADR-0003](../../adr/0003-storage-deferred.md) |
| `EnvioRepo` | la cola y el registro | `MSP_CM_ENVIO` |
| `CursorRepo` | el avance del changelog | `MSP_CM_CURSOR` |
| `ConfigRepo` | ventanas e interruptores | `MSP_CM_CONFIG` |
| `Clock`, `IDGenerator` | | |

## 8. Endpoints y permisos

```
GET    /v2/comprobantes                      lista con filtros: estado, tipo, fecha, cliente
GET    /v2/comprobantes/{id}                 detalle: estado, destino, intentos, error
GET    /v2/comprobantes/{id}/documento       el PDF — la vista previa de la lista
POST   /v2/comprobantes/{id}/detener         el claim atómico
POST   /v2/comprobantes/{id}/reenviar        para los que quedaron en fallido
GET    /v2/comprobantes/pendientes/stream    SSE de cambios de estado
GET    /v2/comprobantes/config
PUT    /v2/comprobantes/config               ventanas e interruptores
```

| Permiso | Cubre |
|---|---|
| `comprobantes:leer` | Lista, detalle, documento, stream |
| `comprobantes:detener` | Detener y reenviar |
| `comprobantes:administrar` | Ventanas e interruptores |

`administrar` va aparte porque apagar el interruptor de pagos detiene 940 mensajes diarios. Eso no debería estar al alcance de quien solo revisa la lista.

Wiring con Huma + chi (`HUMA_WIRING.md`). Fechas RFC3339 UTC (`DATETIME_HANDLING.md`). UTF-8 en las columnas `MSP_CM_*` (`ENCODING_HANDLING.md`).

## 9. El canal

Un puerto con dos implementaciones, siguiendo el patrón de `reactivacion`:

- **`LocalSender`** — escribe el PDF a disco y registra destino y cuerpo. Es con lo que se prueba todo mientras llega la cuenta de negocios, y sirve para siempre como modo de pruebas.
- **`WhatsAppBusinessSender`** — la API oficial de Meta.

Cambiar de una a otra es una línea en el composition root.

> **Estado actual:** `reactivacion/infra/reactivacionsender/whatsmeow_stub.go` es un stub cuyo `Enviar` siempre devuelve *"el canal de whatsapp aún no está configurado"*, y la interfaz `MessageSender` solo acepta texto (`cuerpo string`), sin adjuntos. **No hay canal de WhatsApp funcionando en el repositorio.** Comprobantes define su propio puerto `Sender` con soporte de documento; no reusa el de reactivación.

### 9.1 La restricción de la API de negocios

La API de WhatsApp Business **no permite texto libre por iniciativa del negocio.** Fuera de la ventana de 24 horas que se abre cuando el cliente escribe primero, solo se pueden enviar **plantillas aprobadas por Meta**.

Sí cubre este caso: una plantilla de categoría *utility* con encabezado de documento manda el PDF, y es el uso estándar para comprobantes. Implica tres cosas:

- El texto es una **plantilla con variables**, no una cadena que armemos libremente. El diseño del `Sender` debe reflejarlo: recibe el nombre de la plantilla y sus variables, no un cuerpo.
- Hay que **dar de alta la plantilla y esperar aprobación** antes del primer envío.
- Cada mensaje **tiene costo** (§10).

Conviene confirmarlo con quien aprovisione la cuenta, porque Meta mueve estas políticas.

## 10. Volumen y costo

> **Medido en la base de desarrollo, año 2025 completo:**
>
> | | Al año | Al día |
> |---|---|---|
> | Ventas | 16,595 | **45** |
> | Pagos (documentos) | 343,266 | **940** |

Cerca de **985 mensajes diarios, unos 30,000 al mes.**

No hay tarifa verificada para plantillas *utility* en México en este documento — Meta la ha movido varias veces y no se cotizó. **Es una pregunta concreta y cotizable: "treinta mil mensajes de plantilla *utility* al mes en México".** Debe responderse antes de comprometer la función, no después.

**Reparo sobre los datos:** en 2026 la base de desarrollo trae 15 ventas y 298 pagos diarios, un tercio de 2025. No se determinó si es caída real del negocio o un respaldo incompleto. Se planea con 2025 porque es el año completo y el escenario caro; conviene confirmar contra producción.

### 10.1 Dos consecuencias de diseño

**La lista "por enviar" no es una cola de revisión.** A 985 diarios nadie la revisa. El envío automático tras la ventana **es el camino normal**, no un respaldo. La lista sirve para detener uno cuando alguien nota un error, y para ver los que fallaron.

**Un cliente que paga semanal recibiría 52 mensajes al año.** Puede ser un servicio o una molestia. Vale la pena un piloto por zona o un umbral de monto antes de encenderlo para todos — decisión del negocio, no del diseño, y los interruptores por tipo ya lo permiten.

## 11. Verificación

Compuertas estándar de `TESTING_REQUIREMENTS.md`: `domain` ≥ 99%, `app` ≥ 90%, `infra/comprobantesfb` ≥ 80%, `infra/storage` ≥ 85%, `infra/comprobanteshttp` ≥ 70%, mutación ≥ 80%.

Y cinco pruebas específicas, que son las que sostienen el módulo:

**La carrera del detener.** Dos operaciones concurrentes sobre el mismo renglón: una gana, la otra recibe cero filas afectadas. Con concurrencia real contra Firebird, no con un doble de prueba. Es la garantía de que el botón no miente.

**Idempotencia del cursor.** Reprocesar un tramo del changelog no genera un segundo envío: choca con `UNIQUE(TIPO, REFERENCIA)`.

**Agrupación por documento de pago.** Un pago que acredita tres cargos produce **un** envío, no tres.

**El saldo restante es el correcto.** Integración contra Firebird: aplicar un pago y verificar que el saldo del PDF ya refleja ese pago. `MSP_SALDOS_VENTAS` es un caché que recalculan procedimientos de Firebird; en principio se actualiza en la misma transacción, pero si no, el comprobante diría un saldo viejo — el error que un cliente sí nota. No se supone: se prueba.

**Cliente sin teléfono.** Termina en `sin_telefono`, no en `fallido`, y no consume intentos.

Integración envuelta en `fbtestutil.WithTestTransaction`.

## 12. Fuera de alcance de v1

**Se construye después:** estado de entrega y lectura del mensaje (los webhooks de Meta) · reintento con respaldo exponencial más allá de `MAX_INTENTOS` · plantilla por zona o por tipo de cliente · umbral de monto · comprobante por correo como alternativa para el 31% sin teléfono · panel de métricas de entrega.

**No se construye:** CFDI ni comprobante fiscal · validación o aprobación de pagos (§4.1) · conversación de dos vías por WhatsApp · envío desde la app del cobrador.

## 13. Referencias

- [ADR-0001](../../adr/0001-outbox-strategy.md) — outbox
- [ADR-0003](../../adr/0003-storage-deferred.md) — almacenamiento en filesystem local
- [ADR-0007](../../adr/0007-cobranza-push-watermark.md) — changelog, watermark y push; de aquí sale toda la arquitectura de durabilidad de §3
- `internal/ventas/domain/events.go` — `venta.aplicada`
- `internal/cobranza/infra/microsip/pago_writer.go` — inserta con `APLICADO='S'`
- `internal/clientes/infra/clientespdf/render.go` — patrón de generación de PDF
- `CLAUDE.md` §1 (sin lógica en la base), §2 (vertical slices), §3 (idioma), §5 (restricciones de stack)
