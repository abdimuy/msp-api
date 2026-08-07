# Comprobantes por WhatsApp — plan de implementación

- **Fecha:** 2026-07-29
- **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)
- **Rama base:** `feat/comprobantes`
- **Módulo:** `internal/comprobantes` · tablas `MSP_CM_*` · migración `000049`
- **25 tareas · 60–65 h estimadas · 10 delegables**

## Reglas del corte

Dos reglas gobiernan la división, y son las mismas que se usaron en garantías y asistencia:

1. **Los contratos se fijan antes que nada.** Mientras `ports/outbound` no exista, cualquier división es falsa: dos personas programando contra un puerto que no está definido inventan dos puertos distintos.
2. **Dos tareas nunca escriben el mismo archivo.** Lo que mata la paralelización no es la lógica compartida, son los conflictos de merge.

Tamaños en tiempo del maintainer con IA: **S** 1–2 h · **M** 2–3 h · **L** 3–5 h. Multiplicar por ~4 para estimar a un dev junior.

---

## Tanda 0 — el molde (bloquea todo)

| # | Tarea | Depende de | Tamaño | Delegable |
|---|---|---|---|---|
| 0.1 | Migración `000049` | — | S | **Sí** |
| 0.2 | `domain`: value objects, catálogos, errores | — | M | **Sí** |
| 0.3 | `domain`: entidad `Envio` + máquina de estados | 0.2 | M | No |
| 0.4 | `domain`: modelos de contenido del comprobante | 0.2 | S | Sí |
| 0.5 | `ports/outbound` + `comprobantes_contracts.go` | 0.3, 0.4 | S | No |

**0.1 — Migración `000049`**
`migrations-firebird/000049_create_msp_cm_comprobantes.{up,down}.sql`
Las tres tablas de §5 del spec: `MSP_CM_ENVIO`, `MSP_CM_CURSOR`, `MSP_CM_CONFIG`, más los índices de §5.4. El `UNIQUE(TIPO, REFERENCIA)` es la restricción que garantiza un comprobante por hecho — no es opcional.

**0.2 — Value objects**
`domain/tipo_comprobante.go` · `domain/estado_envio.go` · `domain/canal.go` · `domain/errors.go` · `domain/doc.go` + pruebas.
Molde a copiar: `internal/ventas/domain/tipo_venta.go` (enums) y `internal/ventas/domain/estado_registro.go` (estado). Ver `docs/module-standards/02-value-objects-errors.md`.

**0.3 — Entidad `Envio`**
`domain/envio.go` + pruebas.
La máquina de §4.3: `en_espera → enviando → enviado`, con `detenido`, `fallido` y `sin_telefono`. Las transiciones válidas en un solo lugar. **No delegable:** de aquí depende que el botón de detener no mienta.

**0.4 — Modelos de contenido**
`domain/comprobante_venta.go` · `domain/comprobante_pago.go` + pruebas.
Las estructuras de §6.2 — lo que el renderizador recibe. Sin lógica de presentación.

**0.5 — Puertos y contratos**
`ports/outbound/*.go` · `comprobantes_contracts.go`.
Los once puertos de §7 del spec.

---

## Tanda 1 — app y repositorios (paralelas tras 0.5)

| # | Tarea | Depende de | Tamaño | Delegable |
|---|---|---|---|---|
| 1.1 | `app`: encolar desde `venta.aplicada` | 0.5 | M | No |
| 1.2 | `app`: worker del cursor de pagos | 0.5 | L | No |
| 1.3 | `app`: worker de envío | 0.5 | L | No |
| 1.4 | `app`: detener y reenviar | 0.5 | S | No |
| 1.5 | `app`: consultas de lista y detalle | 0.5 | M | **Sí** |
| 1.6 | `app`: configuración | 0.5 | S | **Sí** |
| 1.7 | `comprobantesfb`: escritura | 0.5 | L | No |
| 1.8 | `comprobantesfb`: lecturas | 0.5 | M | **Sí** |
| 1.9 | Lectores de datos externos | 0.5 | L | No |

**1.1 — Encolar desde `venta.aplicada`**
`app/encolar_venta.go` · `infra/comprobantesoutbox/venta_handler.go`.
Segundo consumidor del evento que ya existe en el outbox, junto al de Meilisearch (`ventas/infra/ventoutbox/search_handler.go` es la referencia). Renderiza el PDF y crea el envío en `en_espera` con `PROGRAMADO_PARA`.

**1.2 — Worker del cursor de pagos**
`app/cursor_pagos.go`.
Recorre `MSP_PAGOS_CHANGELOG` desde `MSP_CM_CURSOR.SEQ_ID`, **agrupa por `DOCTO_CC_ID`** (§2.3 — el 0.25% que acredita varios cargos), y encola. El `POST_EVENT` solo lo despierta antes; el cursor es la garantía.
**No delegable:** un cursor que se salta un tramo pierde comprobantes en silencio. No falla en rojo.

**1.3 — Worker de envío**
`app/enviar.go`.
Toma los que están en `en_espera` con `PROGRAMADO_PARA <= now`, hace el claim atómico, manda por el `Sender`, registra `CANAL` y `MENSAJE_EXTERNO_ID`. Maneja `sin_telefono` como terminal sin consumir intentos.
**No delegable:** el claim atómico es lo que sostiene la garantía del botón.

**1.4 — Detener y reenviar**
`app/detener.go` · `app/reenviar.go`.
`detener` devuelve `detenido` o `ya_enviado`, nunca un error.

**1.5 — Consultas**
`app/queries_lista.go` · `app/queries_detalle.go`.
Filtros por estado, tipo, fecha y cliente, con paginado.

**1.6 — Configuración**
`app/config.go`. Leer y actualizar ventanas e interruptores.

**1.7 — Escritura en Firebird**
`infra/comprobantesfb/{queries,envio_writer,cursor_repo,config_repo,rowmappers}.go`.
Aquí vive el `UPDATE ... WHERE ESTADO = 'en_espera'` de §4.4.
**No delegable:** un `UPDATE` mal escrito hace que el botón de detener mienta, y eso pasa todas las pruebas superficiales.

**1.8 — Lecturas en Firebird**
`infra/comprobantesfb/envio_reader.go`.
Lista con filtros y paginado, detalle. Solo lectura: un error muestra un número mal en pantalla, no corrompe nada.

**1.9 — Lectores de datos externos**
`infra/clients/{venta_reader,pago_reader,saldo_reader,cliente_reader}.go`.
**No delegable, y no por dificultad:** probablemente haya que **extender los contratos de `cobranza` y `ventas`** para exponer lo que el comprobante necesita. Eso toca módulos ajenos, y es exactamente cómo se rompen las fronteras del repositorio sin que nadie lo note.

---

## Tanda 2 — infraestructura

| # | Tarea | Depende de | Tamaño | Delegable |
|---|---|---|---|---|
| 2.1 | `infra/storage`: `FilesystemProvider` | ninguna | S | **Sí** |
| 2.2 | `infra/render`: PDF de venta | 0.4 | M | **Sí** |
| 2.3 | `infra/render`: PDF de pago | 0.4 | M | **Sí** |
| 2.4 | `infra/sender`: `LocalSender` | 0.5 | S | **Sí** |
| 2.5 | `infra/sender`: `WhatsAppBusinessSender` | cuenta de Meta | M | No |
| 2.6 | `comprobanteshttp`: lista, detalle, documento | 1.5, 1.8 | M | **Sí** |
| 2.7 | `comprobanteshttp`: detener, reenviar, config, SSE | 1.4, 1.6 | M | No |

**2.1** — Referencias: `ventas/infra/storage/filesystem.go` y la de `cobranza`. **Es la misma tarea que el practicante tiene en garantías** — si su entrega salió bien, esta se adapta en media hora.

**2.2 y 2.3** — Patrón: `clientes/infra/clientespdf/render.go` con `fpdf`. Ambos incluyen la etiqueta obligatoria de §6.3: **comprobante informativo, no es un CFDI.** El de pago lleva el **saldo restante**, que es el dato que el cliente busca.

**2.4** — Escribe el PDF a disco y registra destino y cuerpo. Con esto se prueba todo de punta a punta sin depender de Meta.

**2.5** — **Bloqueada por trámite, no por código.** El `Sender` recibe nombre de plantilla y variables, no texto libre (§9.1).

**2.7** — No delegable: expone el claim atómico por HTTP y el SSE. La respuesta `ya_enviado` tiene que ser un resultado, no un error.

---

## Tanda 3 — cierre

| # | Tarea | Depende de | Tamaño |
|---|---|---|---|
| 3.1 | `module.go` + permisos + arranque de workers | todas | M |
| 3.2 | Integración Firebird con `WithTestTransaction` | 1.7, 1.8 | M |
| 3.3 | Las cinco pruebas que sostienen el módulo | 3.2 | L |
| 3.4 | e2e de punta a punta con `LocalSender` | 3.1 | M |

**3.3 — Las cinco de §11 del spec**, y ninguna es opcional:

1. **La carrera del detener** — dos operaciones concurrentes sobre el mismo renglón, con concurrencia real contra Firebird y no con un doble de prueba.
2. **Idempotencia del cursor** — reprocesar un tramo no genera un segundo envío.
3. **Agrupación por documento de pago** — tres cargos producen un envío, no tres.
4. **El saldo restante es el correcto** — aplicar un pago y verificar que el PDF ya lo refleja. `MSP_SALDOS_VENTAS` es un caché de procedimientos de Firebird; *en principio* se actualiza en la misma transacción. No se supone.
5. **Cliente sin teléfono** — termina en `sin_telefono`, no en `fallido`.

Toda la tanda 3 es del maintainer: es donde se descubre lo que las anteriores supusieron mal.

---

## Dependencias externas — arrancar hoy, en paralelo al código

**Aprovisionar la cuenta de WhatsApp Business y dar de alta la plantilla *utility* con encabezado de documento.** No lo acelera nadie y la aprobación de Meta tarda semanas. Si se arranca al final, el módulo queda listo esperando un trámite.

**Cotizar 30,000 mensajes de plantilla *utility* al mes en México.** Es la única pregunta que podría cancelar la función completa. Conviene tener el número antes de gastar sesenta horas.

**Confirmar el volumen contra producción.** La base de desarrollo trae en 2026 un tercio del volumen de 2025 (§10 del spec). Si la caída es real, es una noticia más importante que esta función.

---

## Qué puede arrancar hoy

`0.1` y `0.2` no dependen de nada. `2.1` y `2.4` tampoco, si el brief lleva el puerto escrito verbatim — el mismo recurso que se usó con el storage de garantías.

Todo lo demás espera a `0.5`.
