# Comprobantes — Tarea 4: entidad `Envio` y modelos de contenido

> **Rama:** `feat/comprobantes-domain` (la misma, encima de lo ya mergeado)
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)
> **Plan:** tareas `0.3` y `0.4` de la tanda 0
> **Plazo:** entrega el **jueves 13 a más tardar a las 12:00 pm**. Ver el calendario al final.

## Dónde encaja

Los tres value objects que entregaste son las piezas; esto es lo que las usa. `Envio` es la fila de `MSP_CM_ENVIO` viva en memoria: la que el worker de envío reclama, la que el botón de detener intenta ganar, y la que sabe si todavía se puede parar.

Las dos tareas juntas desbloquean `0.5` (puertos y contratos), y `0.5` abre las nueve tareas de la tanda 1. Es la ruta crítica del módulo: hoy no se puede avanzar en nada más de comprobantes hasta que esto exista.

---

## Lo primero, y es lo que más fácil se equivoca

**Para las entidades, `New`/`Hydrate` **sí** es el patrón correcto.**

Acabas de quitar `New`/`Hydrate`/`Value`/`Equals`/`IsZero` de los tres value objects, y con razón: eran enums y les tocaba `Parse`/`IsValid`/`String`. **No apliques esa lección aquí.** `docs/module-standards/AGGREGATE_PATTERNS.md` es explícito para entidades:

> - **`NewX(...)` / `CrearX(p XParams)`** valida cada entrada y devuelve `(*X, error)`.
> - **`HydrateX(p HydrateXParams) *X`** omite la validación. Solo lo usa el repositorio al reconstruir desde filas persistidas.

Son dos capas distintas del estándar y cada una tiene su forma. Si dudas, la regla corta: **enums cerrados → `Parse`/`IsValid`/`String`; entidades y VOs multi-campo → `New`/`Hydrate`.**

---

## Leer antes de escribir (obligatorio, en este orden)

1. **`docs/module-standards/AGGREGATE_PATTERNS.md`** — completo. Es el documento que gobierna esta tarea: dos constructores, campos privados, un getter por campo, sin setters.
2. **`docs/module-standards/02-value-objects-errors.md`, "VO Categoría 3 — Composite VO"** — es la forma que les toca a los dos modelos de contenido de `0.4`.
3. **El spec, §4.3, §4.4, §5.1 y §6.2.** El §4.4 es el que explica por qué `Envio` se comporta como se comporta.
4. **Tu propio `internal/comprobantes/domain/estado_envio.go`** — la entidad se apoya en `CanTransitionTo` que ya escribiste. No dupliques la máquina de estados dentro de la entidad.
5. **`internal/cobranza/domain/pago_recibido.go`** — una entidad real del repositorio con campos privados, getters y constructores. Como referencia de forma, no de contenido.

---

## Entregable 1 — `internal/comprobantes/domain/envio.go`

### Campos

Uno por columna de `MSP_CM_ENVIO` (§5.1), **todos privados**: `id`, `tipo` (`TipoComprobante`), `referencia`, `clienteID`, `telefono`, `estado` (`EstadoEnvio`), `programadoPara`, `documentoRuta`, `canal` (`Canal`), `mensajeExternoID`, `intentos`, `ultimoError`, `detenidoPor`, `enviadoEn`.

Embebe **`audit.Timestamped`** (no `audit.Auditable`): la tabla tiene `CREATED_AT` y `UPDATED_AT` pero no lleva usuario de creación — quien detiene queda en `DETENIDO_POR`, que es un campo propio.

Un getter por cada campo privado. Sin setters.

### Constructores

```go
func CrearEnvio(p CrearEnvioParams) (*Envio, error)   // valida; nace en en_espera
func HydrateEnvio(p HydrateEnvioParams) *Envio        // sin validación, solo el repositorio
```

`CrearEnvio` genera el `id` con `uuid.New()` y los timestamps con `time.Now()` recibido como parámetro — **nunca los toma la base** (regla dura #1 de `CLAUDE.md`). Nace siempre en `EstadoEnvioEnEspera` con `intentos = 0`.

Validaciones mínimas: `tipo` y `canal` válidos, `referencia` no vacía, `clienteID` positivo, `telefono` no vacío salvo que el envío nazca en `sin_telefono` (ver abajo).

### Los métodos de transición

Cada uno comprueba la transición con `estado.CanTransitionTo(...)` y devuelve un centinela si no procede. **Ninguno cambia el estado sin pasar por esa comprobación.**

| Método | Transición | Qué más hace |
|---|---|---|
| `Reclamar(now)` | `en_espera → enviando` | — |
| `Detener(por, now)` | `en_espera → detenido` | fija `detenidoPor` |
| `MarcarEnviado(mensajeExternoID, now)` | `enviando → enviado` | fija `mensajeExternoID` y `enviadoEn` |
| `MarcarFallido(motivo, now)` | `enviando → fallido` | fija `ultimoError` |
| `Reenviar(now)` | `fallido → en_espera` | **incrementa `intentos`** |

Todos actualizan `UpdatedAt`.

**`sin_telefono` no es una transición: es un estado de nacimiento.** Un envío se crea en `sin_telefono` cuando el cliente no tiene teléfono utilizable — no se "cae" ahí desde otro estado. Por eso no aparece en el mapa de transiciones que ya escribiste, y por eso `CrearEnvio` necesita un camino que lo permita (un parámetro o un constructor hermano; elige tú y documenta el porqué).

### Lo que el dominio **no** hace: la carrera

El §4.4 explica que detener y reclamar compiten sobre la misma fila, y que **quien afecta un renglón gana**. Esa atomicidad la da el `UPDATE ... WHERE ID = ? AND ESTADO = 'en_espera'` del repositorio, no la entidad.

La entidad es el guardián en memoria: rechaza la transición imposible y deja el estado consistente. **No intentes resolver la carrera aquí** — ni mutex, ni versión, ni reintento. Si el `UPDATE` afecta cero renglones, el que llamó perdió, y eso lo maneja la capa de aplicación en otra tarea.

### Sin eventos de dominio

`Envio` **no** emite eventos. Comprobantes *consume* el evento `venta.aplicada` que ya existe y el changelog de pagos (§2.1 y §2.3), pero no produce eventos propios. No agregues una lista de eventos pendientes.

### Errores centinela

Los que necesites, en `errors.go`, con el patrón que ya usaste — código en inglés snake_case, mensaje en español minúsculas sin punto final. Como mínimo uno para la transición inválida. Nómbralos siguiendo la fórmula `Err{Entidad}{Detalle}`.

---

## Entregable 2 — Los modelos de contenido

`internal/comprobantes/domain/comprobante_venta.go` y `comprobante_pago.go`.

Son **VO de Categoría 3** (composite): struct inmutable, se pasa por valor, `New{VO}Params`, `New` valida, `Hydrate` no, sin setters. No son entidades: no tienen identidad ni ciclo de vida — son lo que el renderizador recibe.

**Contenido, del §6.2, literal:**

- **`ComprobanteVenta`**: folio de Microsip y fecha · nombre y domicilio del cliente · artículos con cantidad y precio · total, enganche y saldo · el plan de pago en palabras claras (cuánto, cada cuándo, cuántas) · vendedor.
- **`ComprobantePago`**: folio y fecha · nombre del cliente · monto y forma de cobro · a qué venta se aplicó · **saldo restante después de este pago** · quién cobró.

**Sin lógica de presentación.** Nada de formatear moneda, fechas ni armar frases: eso es del renderizador, que es otra tarea. Aquí solo viven los datos y su validación.

**El dinero es `decimal.Decimal`**, nunca `float64`. Es la convención del repositorio (`internal/ventas/domain/combo.go`) y está permitido en `domain`.

**El saldo restante es un dato de entrada, no calculado.** El dominio no consulta la base ni sabe de saldos: lo recibe ya resuelto. Si lo calculas aquí, el modelo deja de ser un modelo.

---

## Pruebas

En `package domain_test`, caja negra, tabla-driven, con el mismo rigor que las que ya entregaste.

Para `Envio`:
- `CrearEnvio` con entradas válidas y **cada validación rechazada con su centinela**, verificado con `errors.Is`.
- **Cada método de transición desde cada estado**: el válido que funciona y los inválidos que devuelven el centinela sin mutar el estado. Esa última parte importa: comprueba que tras un rechazo el `Estado()` siga siendo el de antes.
- Que `Reenviar` incremente `intentos`, y que dos reenvíos den 2.
- Que `HydrateEnvio` reconstruya sin validar, con un caso de basura a propósito.

Para los modelos de contenido: constructor válido, cada validación con su centinela, y `Hydrate` sin validar.

**Cobertura ≥ 99%** en `internal/comprobantes/domain` (`TESTING_REQUIREMENTS.md`). Hoy está en 100%; no la bajes.

---

## Archivos que puedes tocar

```
internal/comprobantes/domain/envio.go
internal/comprobantes/domain/comprobante_venta.go
internal/comprobantes/domain/comprobante_pago.go
internal/comprobantes/domain/errors.go          (solo agregar centinelas)
+ un _test.go por cada archivo nuevo
docs/superpowers/plans/comprobantes-task-4-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** No toques los tres value objects ya mergeados, ni `doc.go`, ni `.golangci.yml`.

---

## Verification

Con la caché de tests limpia. Los seis devuelven 0:

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/comprobantes/domain/
go tool cover -func=cov.out | tail -1        # ≥ 99.0%
```

`go clean -testcache` no es opcional: un `ok` cacheado no prueba que tu cambio corrió.

---

## Reporte

`docs/superpowers/plans/comprobantes-task-4-report.md`, con la salida **literal** de esos comandos y una descripción de lo que entregaste **de verdad**. El de la tarea 2 quedó bien en la segunda vuelta; que este salga bien a la primera.

---

## Calendario y puntos de control

| Cuándo | Qué |
|---|---|
| **Hoy, la hora que te queda** | **Solo leer.** Los cinco documentos de la lista de arriba. Una hora no alcanza para escribir la entidad y sí para llegar el lunes sabiendo qué vas a escribir. |
| Lunes 10 | `0.4`, los dos modelos de contenido. Son los más sencillos y te dejan el resto de la semana para la entidad. |
| Martes 11, fin de jornada | **Punto de control: `envio.go` escrito, antes de sus pruebas.** Mándamelo. Es donde salen las preguntas, y contestarlas con el archivo en la mano cuesta minutos. |
| Miércoles 12 | Las pruebas de `Envio`. |
| **Jueves 13, 12:00 pm** | **Entrega, a más tardar.** Es una hora antes de tu salida a propósito: si algo truena a última hora, queda margen para avisar. |

Si te atoras más de dos horas en una sola cosa, avisa. Y si algo del spec no alcanza para decidir —como pasó con `fallido` y `/reenviar`—, pregunta antes de escribirlo: adivinar sale más caro que preguntar.
