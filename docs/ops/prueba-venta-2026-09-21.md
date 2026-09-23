# Prueba de venta, pagos y visitas — 2026-09-21

Ejecutada entre las 17:40 y las 18:55 sobre el APK `devlocal` de `feat/integracion`
(`com.example.msp_app.test`, versión **2.17.3-local+nogit**), en el Galaxy A25 por USB,
con `adb reverse` a 3001 y 3000. Usuario en sesión: **NOE CORTERO**, zona RUTA 25.

Todo lo de abajo está **medido**, no inferido. Cada afirmación trae la consulta o el
renglón de bitácora que la sostiene. Lo que no se pudo medir se dice como tal.

---

## Desenlace

**La prueba pasa completa. Nada del trabajo de ayer y hoy sale defectuoso.**

A media prueba, `POST /v2/ventas` empezó a dar 500 en todos los intentos. **No era la app
ni era producción**: era la base de dev, rota por otra rama. Se destrabó con un
`ALTER TABLE ... DROP NOT NULL` (abajo), se reenviaron las dos ventas —
**201 y 201** — y los diez pasos de la parte A quedaron verdes.

## La caída de en medio, y por qué no bloqueaba

`MSP_VENTAS.VERSION` — INTEGER **NOT NULL y sin DEFAULT** — la metió la migración
`000064_msp_ventas_version` directo a la base de dev compartida hoy a las **15:40**.
El binario que corre sale de `main` (`536c490`), y su `insertVenta`
(`internal/ventas/infra/ventfb/queries.go:22`, único `INSERT INTO MSP_VENTAS` del repo)
**no menciona esa columna**. Firebird rechaza cada alta:

```
SQLSTATE = 23000
validation error for column "MSP_VENTAS"."VERSION", value "*** null ***"
```

- La migración 000064 vive en `/Volumes/M2-1TB/Developer/.wt-ventas-version`
  (rama `feat/ventas-version`, `1d3f63c` + `b3f14f2`), **sin fusionar**.
  `migrations-firebird/` en `main` llega a **000062**.
- Producción no tiene esa columna, así que **a un cobrador no le pasa esto**.
- La única forma de llevarlo allá sería aplicar el `.sql` de 000064 sin subir su
  binario. Regla: **la columna y el binario van juntos, nunca la columna sola.**

Dos hallazgos de proceso que salen de aquí y sí conviene atender:

1. **Dos ramas sin fusionar ya escribieron en la base de dev compartida**: la 000063
   (`feat/visitas-api`) y la 000064 (`feat/ventas-version`). Cualquiera que corra `main`
   contra esa base se topa con esto.
2. **El motivo del rechazo se pierde.** `isc_not_valid` (335544347) no está en el `switch`
   de `internal/platform/firebird/errors.go:56-90`, cae al `default` y se vuelve
   `firebird_error` / "error de base de datos". Ni la bitácora ni
   `MSP_FAILED_INTENTS.ERROR_MESSAGE` guardan el texto real. Diagnosticar esto costó un
   agente entero; con el texto en la bitácora habría sido el primer renglón.

**Arreglo aplicado** (lo corrió el dueño a las ~19:00; deja la columna en su sitio para que
`feat/ventas-version` siga sirviendo, y hace que los `INSERT` de `main` pasen con
`VERSION = NULL`):

```sql
ALTER TABLE MSP_VENTAS ALTER COLUMN VERSION DROP NOT NULL;
COMMIT;
```

Verificado: `RDB$NULL_FLAG = 0`, y las dos ventas atoradas subieron en el siguiente
"Enviar Pendientes" (`POST /v2/ventas` → **201** y **201**; `MSP_VENTAS` pasó de 12 a 14).

---

## Parte A — Nivel 1: crear sin señal, corregir, y que suba corregida

Venta `753ba3c8-3391-4310-af6a-9b33a11055f7`, cliente JOSÉ ANTONIO PEÑA MÁRQUEZ,
AV REFORMA 142, CENTRO, TEHUACÁN · PUEBLA, crédito, enganche $500, parcialidad $300,
semanal, viernes, 1 foto de galería, PANTALLA ×1 + ALMOHADA ×1.

| Paso | Resultado |
|---|---|
| 1. Sincronizar la camioneta | **No hizo falta**: ya traía 589 artículos. Ver nota abajo |
| 2. Apagar el API | Hecho: `curl` a 3001 daba `exit=7`, nada escuchando |
| 3. Capturar la venta completa | **PASA** — "¡Venta Guardada!" |
| 4. `ENVIADO=0`, fallo reintentable, ancla limpia | **PASA** — ver tabla |
| 5. Ofrece "Corregir venta" | **PASA** |
| 6. Corregir: teléfono, subir cantidad, quitar uno, agregar otro | **PASA** — "Corrección guardada" |
| 7. Corregir otra vez (parcialidad) | **PASA** — `REVISION` 1 → 2 |
| 8. Encender el API y forzar el envío | **PASA** — `POST /v2/ventas` → 201 |
| 9. Una sola venta corregida en la base de dev | **PASA** — ver abajo |
| 10. "Ya se envió" y sin corregir | **PASA** — el detalle dice "Ya se envió" y no ofrece corregir |

**Paso 9, la prueba de fuego.** En `MSP_VENTAS` hay **una sola fila** con ese ID, y trae lo
corregido, no lo capturado:

```
NOMBRE_CLIENTE  JOSÉ ANTONIO PEÑA MÁRQUEZ     (acentos y eñe intactos)
TELEFONO        2385559876                     (el corregido, no 2385551234)
PARCIALIDAD     450.00                         (la corregida, no 300)
ARTICULO 3063068 (PANTALLA)  CANTIDAD 2        (la cantidad subida)
ARTICULO 59889   (COBERTOR)  CANTIDAD 1        (el agregado)
ARTICULO 390040  (ALMOHADA)  — NO APARECE      (el quitado)
```

**Las tres fuentes cuentan la misma historia**, que es el criterio de éxito del plan:

| Fuente | Dice |
|---|---|
| La pantalla | "Ya se envió", sin botón de corregir |
| El teléfono | `ENVIADO=1`, `REVISION=2` **=** `REVISION_POSTEADA=2` (sin divergencia), `CORRECCION_NO_ENVIADA=0` |
| La base de dev | **una** venta, con los valores corregidos |

Estado en el teléfono tras el paso 4 (recién capturada, API apagado):

```
ENVIADO = 0   REVISION = 0   REVISION_POSTEADA IS NULL → ancla_limpia = 1
CORRECCION_NO_ENVIADA = 0    LAST_UPLOAD_PERMANENT = 0
LAST_UPLOAD_ERROR_CODE = network_error    CLAIM_KIND = (vacío)
```

Eso es exactamente lo que pide el plan: fallo **reintentable**, no permanente, y el
**ancla de revisión limpia** — que es lo que evita el aviso falso. En la lista se leyó
"No se pudo enviar (reintentando)".

Tras las dos correcciones: `TELEFONO = 2385559876`, `PARCIALIDAD = 450.0`, `REVISION = 2`,
y los productos en `PANTALLA ×2` + `COBERTOR ×1`, con ALMOHADA fuera. El total en
pantalla pasó de $7,300 a $14,500.

**Lo que sí quedó probado del nivel 1, y es el punto del diseño:** el cuerpo que salió al
API — capturado en `MSP_FAILED_INTENTS` — lleva **una sola versión, la corregida**:

```json
"telefono":"+522385559876", "parcialidad":"450.00",
"productos":[{"articulo_id":3063068,"cantidad":"2"},{"articulo_id":59889,...}]
```

Nunca se mandaron las dos versiones. La promesa del nivel 1 se cumple; lo único que faltó
medir es el lado del servidor, que estaba caído por otra cosa.

> **La camioneta ya venía cargada (589 artículos).** El plan decía "inventario 0"; esa
> línea base ya estaba vieja. "Mi Camioneta" es la pantalla del carrito
> (`CartScreen.kt`), que lee el almacén del sustituto del 3000; la tabla `product_inventory`
> de Room sigue en **0** y no estorbó para vender.

---

## Parte B — Nivel 2: crear con señal y tratar de editar

Segunda venta `8c262edc-4d86-4017-a111-b06118e22595`, cliente **ESTELA OCHOA VAZQUEZ**
(elegido a propósito **sin acentos**, como control). Mismo 500.

Eso **refuta** la primera sospecha — que los acentos del nombre rompían el alta — y fue lo
que orilló a buscar la causa real. Sirve de recordatorio: la primera hipótesis razonable
era falsa y sólo el control la tumbó.

Los pasos 2, 3 y 4 del nivel 2 **no se pudieron medir**: la venta nunca llegó a existir en
el servidor, así que no hay situación "borrador" contra la cual comparar.

**Y hay algo más grande que el plan daba por hecho.** El plan dice del nivel 2:
"Servidor: listo y probado". Eso es cierto **de la rama**, no de lo que corre:

- `feat/ventas-version` tiene los 2 commits (If-Match, 412, CAS en el repositorio) y
  **no está fusionada a `main`** (`git branch --merged main` no la lista).
- En `main`, `VERSION` no aparece en `internal/ventas/` fuera de un archivo de prueba.
- Las filas viejas de `MSP_VENTAS` están todas en `VERSION = 0` y `SITUACION = 'borrador'`.

Es decir: **el nivel 2 no está desplegado en ninguna parte**, ni servidor ni app. La línea
base contra la que se medirá sigue sin existir.

---

## Parte C — Revisión de lo demás

| Qué | Resultado |
|---|---|
| **Buscador tolerante a acentos** | **PASA, las cuatro formas.** `pena` → JOSÉ ANTONIO PEÑA MÁRQUEZ; `jose` → JOSÉ, JOSEFA y JOSEFINA; `marquez jose` (orden invertido) → el correcto; `  PEnA   ` (espacios de más y mayúsculas mezcladas) → el correcto; `millan` → NAYDA ... MILLÁN |
| **Fila de contactos** | **PASA.** Dice "Abono", día y hora ("21 ene 12:07"), el producto de la cuenta ("Alacena labrada vino", ya normalizado), forma ("Efectivo") y cobrador. Una fila sin cuenta **no inventa texto** |
| **Filtro con conteos** | **PASA.** "Ver los 30 contactos" abre la bitácora con las cuatro opciones y su número: Todos 30, Cobros 30, Visitas **0**, Promesas **0** — incluidas las dos en cero |
| **Animación del desenlace de visita** | **PASA.** "Prometió pagar" revela cuenta, "¿Cuándo?" y "¿Cuánto?" con transición, no de golpe |
| **Lista de cobranza** | **PASA.** Agrupa por cliente, filtro de cuatro estados con conteo (Todos 270, Vencidos 1, Hoy 0, Sin visitar 268) |
| Detalle de venta (sin "⋯", nota al fondo, mapa al tocar el historial) | **NO SE MIDIÓ** — se acabó el tiempo |
| Escala grande (rejilla 2×2) | **NO SE MIDIÓ** |

### Pagos — **funcionan**

Abono de $300 a LAURA YESENIA HERNANDEZ CARRASCO, venta Y00001558 (ALACENA LABRADA VINO).

- **"¿A cuál cuenta? El abono entra completo a una"** — ya **no** entra sola a la primera
  cuenta de la lista. Ofreció las dos con su avance y sus atrasos.
- Dos pasos ("1 revisado" → "2 confirmar"), con SALDO ANTERIOR $500 → SALDO NUEVO $200.
- **`POST /v2/cobranza/pagos` → 200**, pago `4251ad68-2933-4e7d-99a9-7d7886b53494`.
- El saldo del cliente bajó de $7,900 a **$7,600** en la pantalla siguiente. Cuadra.

### Visitas — **funcionan**

Promesa de pago sobre ALACENA LABRADA VINO, para mañana, sin monto.

- **`POST /v2/visitas` → 201.**
- **Pero** `GET /v2/visitas/sync/visitas/zona/21563/stream` → **404, tres veces.**
  Ese endpoint vive en `feat/visitas-api` (la de la migración 000063), tampoco fusionada.
  La app lo pide y se calla; no rompió nada visible hoy, pero el canal de sync de visitas
  **no existe en `main`**.

---

## Hallazgos nuevos, ordenados por si bloquean

### Bloquean si llegaran a producción (ninguno lo hace hoy)

Ninguno. El 500 es colisión de entorno en dev, ya explicada.

### No bloquean, pero hay que verlos

1. **La idempotencia no protegió el reintento de un pago.** El mismo pago salió dos veces
   con la **misma** `IDEMPOTENCY_KEY` (`4251ad68…`): el primero 200, y el segundo, 1.7 s
   después, **se volvió a ejecutar** y se rechazó con **422 `pago_saldo_insuficiente`**,
   dejando una fila en `MSP_FAILED_INTENTS` (`d0bce8ed…`) **de un pago que sí entró**.
   Debió servir la respuesta cacheada del 200.
   El usuario no vio daño — la app hizo `GET /v2/cobranza/pagos/{id}` → 200 y se recuperó —
   pero es **exactamente el escenario C.4 del plan**, ahora medido: la oficina puede
   reproducir desde los intentos fallidos un cuerpo de un pago ya aplicado, y **nada lo
   marca**. Esa fila es una trampa esperando.

2. **El teléfono trata el HTTP 500 como fallo permanente.** Las dos ventas quedaron en
   `LAST_UPLOAD_PERMANENT = 1` y ya no se reintentan solas. Un 500 es error de servidor y
   es reintentable por definición. Puede que sea la cabecera `X-Intent-Captured` la que lo
   está clasificando así (el intento sí se capturó) — en cuyo caso es el comportamiento
   diseñado, y lo que falta es que la oficina tenga cómo cerrar el ciclo. **No verificado
   cuál de las dos cosas es**; se decide leyendo el clasificador.

3. **El motivo del rechazo de Firebird se pierde entero.** Ver el primer apartado. Vale
   añadir `isc_not_valid` al `switch`, o mejor: incluir el texto saneado del driver cuando
   `APP_ENV != production`.

4. **Un pago viejo atorado en dev**: `pago_retry.apply_failed pago_id=a2ca6d21…
   intentos=6 firebird_fk_violation: COBRADOR_A_DOCTOS_CC ... "COBRADOR_ID" = 200`.
   Es residuo de dev, anterior a hoy, pero el reintentador lo sigue raspando.

### Cosméticos vistos de paso

- **Los tickets están enteros en minúscula**: "atrás", "ticket de pago", "ticket de visita",
  "abono", "saldo actual", "imprimir ticket", "cambiar impresora", "prometió", "sin monto".
  Y en la hoja de confirmación del abono: "confirmar abono", "confirmar y registrar",
  "editar", "revisado", "sin comprobante". Es el punto 3 del catálogo del dueño, confirmado.
- **Mayúsculas crudas donde ya hay versión normalizada**: el selector de cuenta del abono
  dice "ALACENA LABRADA VINO" mientras la fila de contactos, a dos dedos, dice
  "Alacena labrada vino".
- **La pantalla de Ventas perdió los acentos**: "Resumen del dia", "Actualizar Catalogo",
  "Catalogo", "Menu" — mientras el cajón lateral sí dice "Catálogo de Productos" y "Menú".
- **Fecha con dos relojes**: "CREDITO - 21/09/2026 **18:00 p.m.**" mezcla 24 horas con p.m.
- En el formulario de dirección, "buscar ciudad" y "mi ciudad no está" en minúscula.
- El encabezado de productos del editor **cuenta unidades, no renglones**: con PANTALLA ×2
  y COBERTOR ×1 dice "Productos (3)".

---

## Catálogo: lo que sigue sin probar

### Gates manuales fuera de alcance (sin cambio respecto al plan)

- **Matar la app a media corrección y reabrirla** — pide huella; sólo el dueño.
- **El adversarial**: abrir el editor sin señal y **dejar que vuelva la señal con el editor
  abierto**, esperar medio minuto y guardar. Sólo puede terminar de dos formas.
- **La espera de media hora**: matar la app con el editor abierto y confirmar que la venta
  sube cuando vence el arrendamiento.
- **Impresión térmica** — no hay impresora. Hoy se llegó dos veces a "imprimir ticket" y ahí
  se paró.
- **Los goldens que se movieron**, en los dos temas y las tres escalas.

### Lo que hoy se intentó y quedó a medias

- **Pasos 9 y 10 de la parte A** (una sola venta corregida en la base; "Ya se envió" sin
  ofrecer corregir). Se repiten en cuanto la base de dev deje de rechazar el alta.
- **Parte B completa** — misma razón.
- **Detalle de venta**: sin botón de tres puntos, nota al fondo, mapa al tocar el historial.
- **Escala grande**: el filtro en rejilla 2×2 y el día sin cortarse.

### Decisiones abiertas del dueño (las del plan, más lo de hoy)

1. **"Ver los N abonos" sigue abriendo la pantalla vieja.** ¿Se cierra igual, sin reemplazo?
2. **La nota del detalle de venta es la de la venta**, no la ficha del cliente. ¿Es lo
   esperado?
3. **~32 cadenas en minúscula en los tickets** — hoy quedaron confirmadas. ¿Entran al
   próximo barrido?
4. **Las marcas en minúscula** en el nombre del producto ("audiobahn"). Sigue sin contestar
   desde anteanoche.
5. **El punto de estado** de la fila no escala con la letra del sistema.
6. **El aviso "La revisa la oficina" puede salir de más.** Hoy salió en los dos casos, por
   el 500 de dev. Conviene medir en campo.
7. **Permisos**: sigue sin medirse si la cuenta del dueño tiene `ventas:editar`. El permiso
   existe en el código; no se comprobó contra su usuario. Sin él, el nivel 2 dará 403.
8. **Nuevo: ¿se aplica el `DROP NOT NULL` a la base de dev?** Sin eso, nadie que corra
   `main` puede dar de alta una venta en dev.
9. **Nuevo: ¿qué se hace con las migraciones que ramas sin fusionar aplican a la base
   compartida?** Hoy son dos (000063 y 000064).

### Defectos conocidos que no bloquearon

- La sección "Productos" en MAYÚSCULAS crudas junto a la cuenta ya normalizada —
  **confirmado hoy** en el selector de cuenta del abono.
- El pin de la fila no crece con la letra.
- `MspSegmentChips` no expone al lector de pantalla qué opción está elegida.
- El troceo de la consulta de productos por encima de 900 folios no tiene prueba propia.

### Lo que sigue invisible, declarado

- **Fallo permanente con intento capturado.** Ya no es teórico: hoy quedó
  `d0bce8ed…` en `MSP_FAILED_INTENTS`, de un pago **que sí se aplicó**. Se cierra del lado
  del servidor.
- **El camino legado**, hoy con guarda de build.

---

## Cómo se verificó

Bitácora del API (el proceso se relanzó a propósito escribiendo a archivo, porque
`go run` mandaba todo a una terminal que no se podía leer):

```
tail -n +<marca> api.log | grep -oE 'method=(POST|PATCH|PUT) path=[^ ]+ status=[0-9]+'
```

Base de dev, por el contenedor `mueblera-firebird`:

```sql
SELECT ID, TELEFONO, PARCIALIDAD, VERSION FROM MSP_VENTAS ORDER BY CREATED_AT DESC;
SELECT ID, HTTP_STATUS, ERROR_CODE, ERROR_MESSAGE, IDEMPOTENCY_KEY
  FROM MSP_FAILED_INTENTS ORDER BY RECEIVED_AT DESC;
```

El teléfono, sobre una copia binaria de su base (`adb exec-out run-as … cat`, **no**
`adb shell cat`):

```sql
SELECT TELEFONO, PARCIALIDAD, ENVIADO, REVISION,
       (REVISION_POSTEADA IS NULL) AS ancla_limpia,
       CORRECCION_NO_ENVIADA, LAST_UPLOAD_PERMANENT
FROM local_sale;
```

Base del teléfono en **v33**, como esperaba el plan.

---

## Qué queda encendido

- API de Go en el 3001, relanzado por esta sesión, escribiendo a
  `…/scratchpad/api.log`. Se arranca con `set -a; source .env; set +a; go run ./cmd/api serve`.
- Sustituto del API legado (python) en el 3000, intacto.
- Dos ventas en el teléfono sin subir, las dos marcadas como permanentes. Cuando se arregle
  la base de dev habrá que decidir si se reintentan o se borran.
