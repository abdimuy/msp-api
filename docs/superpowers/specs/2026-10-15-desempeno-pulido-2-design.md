# Desempeño de cobradores — pulido 2 (diseño)

Fecha: 2026-10-15 · Estado: borrador para revisión del dueño · Sin código de producto
Repos: `msp-api` (servidor), `sistema-cobro-web` (escritorio; copia de trabajo
`/Volumes/M2-1TB/Developer/sistema-cobro-web-desempeno`, rama `feat/desempeno`, 1.26.0 = `f3d11f4`),
`msp-app-kt` (app Android: lista de cobranza en 3 columnas y casos compartidos)

Complementa, y en lo que contradiga sustituye, a
`2026-10-14-desempeno-cobradores-design.md` (diseño, decisiones D1–D17) y
`2026-10-14-desempeno-contratos.md` (contrato v2.1). Este documento define el
**contrato v2.2**; el archivo de contratos se actualiza **antes** de escribir código
(regla del propio archivo de contratos: "cambiar un nombre exige cambiar este archivo primero").

Convención de evidencia (CLAUDE.md §8, `docs/registro-de-evidencia.md`): cada referencia
`archivo:línea` de este documento es **LEÍDO 2026-10-15** sobre `main` de msp-api (`e1762f5`),
`feat/desempeno` del escritorio (`f3d11f4`) y `feat/estado-cuenta-casos-compartidos` de
msp-app-kt (`083c0d61`). Las cifras que vienen de mediciones previas se marcan con su nivel y
origen; ninguna se volvió a ejecutar para escribir este documento.

---

## 1. Contexto y objetivo

Estado en producción: API `e1762f5` (incluye E-INF-016: en el escritorio el abono parcial cae
en "Pagados", divergencia deliberada con el teléfono) y escritorio 1.26.0 publicado.

Al usar la pantalla en producción el dueño encontró cinco familias de problemas:

1. **Dos vocabularios de lista.** El teléfono reparte las cuentas en 4 segmentos (Sin visitar,
   Volver, Después, Pagados) y el escritorio en los mismos 4 pero con la regla del parcial
   cambiada (E-INF-016). Nadie lee igual la misma cuenta en los dos aparatos.
2. **Semanas viejas.** Usuarios cuyo inicio de semana no se reinició hace meses hacen que el
   servidor lea ventanas enormes. MEDIDO (dev, reportado al dueño el 2026-10-15, no re-ejecutado
   aquí): 55 usuarios con ventana de 338 días → tablero de 44 s y 16.7 MB. Hoy cuesta lo mismo que
   Semana porque cobertura y cobro siempre se calculan sobre la semana del usuario
   (`internal/desempeno/app/tablero.go:194-210` arma una lectura `[inicio de semana, ahora]` por
   cada `(zona, inicio)`; `:287-294` recorre día por día en Semana).
3. **Cifras que no significan lo que parecen**: mayoreo con `pct` "0.00" sin nada esperado,
   cobrado de muchos días contra una sola parcialidad (330 % medido en dev), una ficha con
   "% cobro semana 66.6 %" y "por cobrar $0".
4. **Pantalla que miente mientras carga**: al cambiar Hoy↔Semana se ven números viejos bajo el
   rótulo nuevo; los esqueletos no tienen la forma de la UI; el mapa de la Jornada dice "Mapa no
   disponible" en producción.
5. **Detalles**: horario limitado a 07:00–09:00 / 17:00–19:00, eje fijo 7–20 h, totales del día
   sueltos, "hace 0 d", columna Nota oculta, 503 falsos en el log.

**Objetivo:** una sola regla de lista para teléfono y escritorio, un tablero cuyo costo no crezca
con semanas abandonadas, cifras que se puedan defender (§8 donde toque dinero) y una pantalla
que nunca muestre un número bajo un rótulo que no le corresponde. Toda la lógica nueva vive en el
servidor; escritorio y teléfono solo pintan (D10).

---

## 2. Decisiones del dueño (2026-10-15)

Numeración continua con el diseño anterior (D1–D17).

| # | Fecha | Decisión | Por qué |
|---|---|---|---|
| D18 | 2026-10-15 | **Lista en 3 columnas, igual en teléfono y escritorio**: "Por visitar" · "No paga" · "Pagados". Sustituye los 4 segmentos y elimina la divergencia E-INF-016. | Un cobrador y la oficina deben ver la misma cuenta en el mismo lugar. La pregunta de la lista es "¿a qué puerta voy?": todo lo que pide ir (incluidas negativas, citas y promesas vencidas) va junto; solo la promesa con fecha futura se aparta. |
| D19 | 2026-10-15 | **Semana vencida**: inicio de semana con más de 14 días. El servidor la marca y **no lee su ventana**; el escritorio la oculta del tablero tras "Ver N sin actividad reciente". Ficha con aviso, días limitados y "último movimiento". | Quita el costo medido (44 s / 16.7 MB) y evita cifras de 338 días que nadie puede leer. "Último movimiento" separa al abandonado del que no ha empezado hoy. |
| D20 | 2026-10-15 | **Mayoreo fuera de Desempeño** (zona 23301). Mecanismo: decisión abierta DA-1. Además `cobro.pct = null` cuando `esperado = 0`. | Mayoreo no cobra por parcialidad (parcialidad 0 en sus 169 cuentas, pagos de conceptos 155/11 — dato reportado por el dueño, no re-medido aquí); su fila solo produce ruido. Un porcentaje sin denominador no es "0 %". |
| D21 | 2026-10-15 | **Horario libre** 00:00–23:59, fin siempre después de inicio, sin cruzar medianoche; eje de la línea ancho según horario y actividad; **horas activas recortadas al horario** cuando lo hay. | Hay cobradores fuera de 7–19 h; hoy lo que cae fuera se pega al borde del eje. El horario solo generaba señales; el dueño quiere que también acote las horas activas. |
| D22 | 2026-10-15 | **Mapa de la Jornada** con el mismo cargador y llave que Ventas. | En 1.24–1.26 el mapa nunca carga en producción: lee una variable vacía. |
| D23 | 2026-10-15 | **Carga Hoy↔Semana honesta**: toggle inmediato, esqueleto, nunca número viejo bajo rótulo nuevo, "Cargando semana…", "Reintentar". | Hoy se pueden ver cifras de Hoy rotuladas Semana. |
| D24 | 2026-10-15 | **Esqueletos fieles** en tablero y ficha. | Que nada salte al llegar los datos. |
| D25 | 2026-10-15 | **Totales del día en la tarjeta "Línea del día"**, opción A del mock (https://claude.ai/artifact/Lrz7JritH7exy1dbbpqghY). Sin tarjetas que repitan las de arriba. | El dueño rechazó repetir tarjetas; quiere los totales del día donde se lee el día. |
| D26 | 2026-10-15 | **Correcciones de las auditorías** de semanas viejas (lista en §3.6 y §4.6), incluida la del cobrado sobre ventanas largas por la ruta §8. | Cada una es un defecto visto en uso real o en auditoría. |
| D27 | 2026-10-15 | **Rareza de Patricio de la Luz (R/03)**: medir antes de tocar. | "% cobro semana 66.6 %" junto a "por cobrar $0" es inconsistente a la vista; la causa no está medida. |
| D28 | 2026-10-15 | **Pendientes menores**: "hace 0 d", columna Nota oculta, cancelaciones logueadas como 503. | Ruido visible o en el log. |

---

## 3. Diseño — servidor (`msp-api`, `internal/desempeno`)

Vertical slice intacto (CLAUDE.md §2): todo cambio vive en `internal/desempeno`, salvo la lista de
zonas excluidas, que vive en `internal/config` y se consume por su contrato (§3.3). No se toca el
sync de cobranza ni `internal/rutas`.

### 3.1 Las 3 columnas (D18)

**Regla** (dominio, reemplaza a `SegmentoDe`, `internal/desempeno/domain/presentacion.go:121-143`):
`ColumnaDe(r ResultadoCuenta, hoy Fecha) Columna`, **total** (nunca "sin columna"):

| Trato (código) | Columna | Hoy (v2.1) |
|---|---|---|
| `sin_trabajar` | `por_visitar` | sin_visitar |
| `regresas` | `por_visitar` | volver |
| `nadie` (no estaba) | `por_visitar` | volver |
| `escalar` (se negó / conflicto) | `por_visitar` | despues |
| `cita` — cualquier fecha (vencida, hoy, futura) o sin fecha | `por_visitar` | volver / despues / sin segmento |
| `diferido` (promesa) con fecha ≤ hoy | `por_visitar` | volver |
| `diferido` con fecha > hoy | `no_paga` | despues |
| `parcial` (abonó parcial) | `pagados` | pagados (E-INF-016) |
| `pagado` | `pagados` | pagados |

- Etiquetas de UI: "Por visitar", "No paga", "Pagados". La etiqueta de la cuenta con trato
  `parcial` sigue siendo "Abonó parcial"; solo cambia la columna.
- **Cita sin fecha** cae en `por_visitar`: con D18 toda cita pide ir, así que la fecha ya no decide.
  Esto elimina el único caso en que hoy una cuenta no cae en ningún segmento
  (`presentacion.go:145-148` devuelve `false` con fecha nula).
- **Promesa sin fecha**: no existe como `diferido`; `tratoDe` del teléfono la manda a `regresas`
  (LEÍDO, comentario en `SegmentoDeCobranza.kt:95-96`). En Go es INFERIDO que el port hace lo mismo;
  lo vuelve MEDIDO un caso compartido nuevo (§7.2) que debe pasar en Go y en Kotlin.
- **Invariante**: las tres columnas son disjuntas y cubren todos los tratos. Prueba de dominio
  que recorre los 8 estados × fechas (vencida, hoy, futura, nula) y exige exactamente una columna.
- **"Sin nada"** no es columna: es el filtro de cobertura de D5 (ventas sin pago ni visita en la
  semana) y se conserva en el escritorio como filtro aparte (ver DA-3).
- Consumidores de la regla, enumerados (LEÍDO con `grep -rn "SegmentoDe("` sobre `internal/`):
  `app/cuentas.go:147` (conteos y filtro de Cuentas) y `app/tablero.go:310` (`llenarCobertura` →
  `cobertura_por_segmento`). Ningún consumidor fuera de `internal/desempeno`.
- **v2.1 se congela**: `SegmentoDe` se conserva sin cambios solo para servir contrato v2.1 a los
  escritorios 1.26.0 durante la transición (§5). Se borra cuando se retire v2.1.

### 3.2 Semana vencida (D19)

- Constante de dominio `DiasSemanaVencida = 14`. Regla: `ahora − inicio_semana > 14 × 24 h`.
  Se evalúa con el mismo `ahora` que el resto de la respuesta.
- Un usuario con semana vencida sigue **configurado** (`configurado = true`) y lleva
  `semana_vencida = true`. No es un motivo de "sin configurar": tiene zona e inicio válidos, y su
  ficha debe poder abrir la Jornada de cualquier día.
- **Tablero**: los usuarios con semana vencida **no entran** al lote de `semanasDelTablero`
  (`app/tablero.go:194-210`): ni universo, ni actividades de la ventana, ni cobro de rutas. Su fila
  sale con métricas en cero/`null` (misma forma que `filaVacia`, `app/tablero.go:145-154`),
  `lineas = []`, `cobro.pct = null`, cobertura `0/0`, más `ultimo_movimiento`.
- **`ultimo_movimiento`** (todas las filas configuradas, vencidas o no): la actividad más reciente
  **registrada por ese usuario** (misma atribución que el tablero: pago por `CREATED_BY`/`COBRADOR_ID`
  de `MSP_PAGOS_RECIBIDOS`, visita por `MSP_VISITAS.COBRADOR_ID`) dentro de los últimos 92 días
  (`RangoMaximo`, `app/actividad.go:22`). `null` = nada en 92 días. Para filas no vencidas con
  actividad propia en la ventana ya leída, se toma de ahí sin otra consulta; para las demás, una
  consulta por lote nueva. Su plan de ejecución y costo se miden antes de darla por buena
  (§7.4); si no hay índice que la sirva, migración que agrega índice (permitido por CLAUDE.md §1).
- **Jornada** de un usuario con semana vencida: igual que hoy (un día no depende de la ventana).
- **Días navegables**: el servidor entrega `dias_navegables` (los últimos 14 días de negocio hasta
  hoy, en orden) e `inicio_semana_dia`. El escritorio pinta chips solo de esos días más un chip
  "inicio"; ya no genera 339 chips (`NavegadorDia.tsx:18`, `diasEntre(inicioSemanaDia, hoy)`).
  Para usuarios no vencidos, `dias_navegables` = días desde el inicio de semana hasta hoy
  (≤ 15 por definición).
- **Actividad**: sin `desde` explícito, el rango por omisión de un usuario con semana vencida es
  `[hasta − 14 días, hasta]`, sin rótulo (decisión del dueño: no quiere rótulo de recorte). Para
  usuarios no vencidos el rango por omisión sigue siendo `[inicio de semana, ahora]`, que por
  definición mide ≤ 14 días, así que el recorte silencioso a 92 días de `app/actividad.go:159-185`
  deja de dispararse en el rango por omisión. Un rango explícito conserva el máximo de 92 días y
  su 422.
- **Cuentas** de un usuario con semana vencida: ver DA-2.

### 3.3 Mayoreo fuera y `pct` sin denominador (D20)

- **Exclusión** (mecanismo según DA-1; el diseño asume la recomendación, lista configurable):
  tabla `MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS (ZONA_ID INTEGER PK, CREATED_AT TIMESTAMP NOT NULL,
  CREATED_BY CHAR(36)...)` en `internal/config`, con lectura y alta/baja en Configuración.
  Estructural solamente: `CREATED_AT` lo pone Go (CLAUDE.md §1). `internal/desempeno` la lee por un
  puerto de salida implementado en `infra/clients` contra el contrato de `config`.
- Efecto: el tablero **no lista** a usuarios cuya zona está excluida (en v2.1 y v2.2: quitar una fila
  no rompe al escritorio 1.26.0). Los endpoints de ficha de ese usuario responden 409
  `desempeno_zona_excluida` ("la zona del usuario está excluida de desempeño").
- **`pct = null` cuando `esperado = 0`**: en `CalcularCobro` (`domain/cobro.go:22-31`). Hoy `pct` se
  copia tal cual de rutas (`cobro.go:23`, `Pct: cs.PctPonderado`); rutas lo calcula como promedio
  de aportes sobre las ventas que aplican (`internal/rutas/domain/desglose.go:48-65`), así que con
  ventas que aplican y parcialidad 0 da "0.00" aunque `esperado` sea 0. El cambio es local a
  desempeño; **no** se toca rutas ni Cobranza semanal. Aplica a v2.1 y v2.2 (`pct` ya es nullable
  en v2.1). Toca una cifra de dinero mostrada → ruta §8 (§6, M2).

### 3.4 Horario y horas activas (D21)

- **Validación**: sin cambio. El servidor ya acepta cualquier `desde_hora < hasta_hora` dentro del
  día (`domain/jornada.go:40-68`; `ParseHoraDelDia`, `domain/calendario.go:86-95`, acepta 00:00–23:59).
  No hay medianoche que cruzar porque ambos son horas del mismo día.
- **Horas activas** (`ResumenJornada.MinutosActivos`, hoy `jornada.go:324`:
  `minutosEntre(primera, ultima) − pausas cerradas`):
  - **Sin horario**: igual que hoy.
  - **Con horario** `[D, H]` del día: `minutos_activos = |[primera, última] ∩ [D, H]| − Σ |pausa
    cerrada ∩ [D, H]|`. Tramos y pausas se recortan al horario; lo que queda fuera no suma ni resta.
  - El hueco **antes** de la primera actividad y **después** de la última no es pausa (ya están
    fuera de `[primera, última]`). Lo marcan las señales `inicio_tarde` / `fin_temprano`
    (`domain/senales.go:90-117`), que no cambian.
  - `resumen.pausas {n, minutos}` con horario cuenta las pausas que intersecan el horario, con sus
    minutos recortados. La lista de pausas de la línea y la señal `pausa_larga` conservan la pausa
    real completa (lo que se dibuja es lo que pasó).
  - **Pagos, visitas, condonaciones, importes y cobertura no se recortan nunca**: un pago fuera de
    horario sigue siendo dinero cobrado.
  - El tablero suma las horas activas de cada día con la misma regla (Hoy y Semana).
- **Eje**: el servidor entrega `eje {desde_hora, hasta_hora}` (enteros, horas de negocio):
  `desde_hora = min(7, hora(D), hora(primera actividad visible))`,
  `hasta_hora = max(20, ⌈H⌉, hora(última actividad visible) + 1)`, acotado a 0–24. En el tablero
  es uno solo para todas las filas (para que alineen); en la jornada es el del día.
  `por_hora` (hoy fijo 7..19, `domain/jornada.go:19-21` y `:279-288`) cubre en v2.2 una entrada por
  hora de `[desde_hora, hasta_hora)`. v2.1 conserva 7..19.

### 3.5 Totales del día (D25) y endpoint por usuario (D26)

- **Jornada v2.2**: `resumen.condonaciones` pasa a `{n, importe}` (misma forma que `pagos`). El dato
  ya existe en dominio (`ResumenJornada.Condonaciones` es `ConteoImporte`, `domain/jornada.go:149`;
  se suma en `:334`) y hoy el DTO solo expone el conteo (`infra/desempenohttp/dto.go:206-215`,
  `mapper.go:189`). Se agrega `horario {desde_hora, hasta_hora} | null` (eco de la consulta) y `eje`.
- **Nuevo** `GET /v2/desempeno/usuarios/{uid}?periodo&pausa_min&desde_hora&hasta_hora&contrato=2.2`:
  una sola fila del tablero calculada **solo para la zona de ese usuario**. La ficha deja de pedir el
  tablero de toda la flota cada minuto (`DesempenoScreen.tsx:39` usa `useTablero()`, que refresca
  cada minuto en `useTablero.ts:28`).

### 3.6 Correcciones de auditoría del lado del servidor (D26)

1. **Hoy no calcula la semana cuando no hace falta.** Cobertura y cobro en Hoy siguen siendo de la
   semana (contrato §2, D8), así que la lectura de la ventana se queda; lo que deja de hacerse en Hoy
   es filtrar `registradasPor(sem.actividades, u)` (se calcula antes de ramificar por periodo,
   `app/tablero.go:275`, y en Hoy no se usa). El costo grande lo quita D19 (ventana ≤ 14 días).
   Se mide con `TestMedirTablero` (§7.4).
2. **Mismo "hoy" para ficha y tablero.** El servidor agrega `hoy` (`YYYY-MM-DD` de negocio, calculado
   con su `ahora`) a tablero, fila por usuario y jornada. El escritorio deja de usar
   `hoyDeNegocio()` del reloj local (`DesempenoScreen.tsx:49`).
3. **Orden en Semana justo.** El servidor agrega a cada fila de Semana `promedio_diario {dias,
   minutos_activos, pagos_importe, visitas}`: totales del usuario entre los días de su ventana
   transcurridos (`dias` = días de negocio de inicio a hoy, inclusive). En Hoy es `null`. El
   escritorio ordena por promedio en Semana (§4.6).
4. **Cobrado sobre ventanas largas** (330 % medido en dev; `domain/cobro.go:22-31` suma `AbonoSemana`
   de toda la ventana contra **una** parcialidad por venta): **toca dinero**. No se cambia nada
   hasta cumplir la ruta §8 (§6, M3). D19 quita las ventanas > 14 días pero no las de 8–14.
5. **Cancelación del cliente ≠ 503.** `internal/platform/firebird/errors.go:39-46` traduce
   `context.Canceled` a 503 `firebird_timeout`, y `desempenohttp/auth.go:44-60` lo registra como
   `WarnContext "desempeno.http.no_disponible"`. Cambio acotado a desempeño (no a la plataforma, que
   comparten todos los módulos): en `mapAppError`, si `ctx.Err() == context.Canceled`, se registra en
   nivel Info como `desempeno.http.cancelada_por_cliente` y no como no disponible. Lo que escriba el
   log de acceso de la plataforma para esa petición no se verificó (queda en §10).

---

## 4. Diseño — escritorio (`sistema-cobro-web`, `src/modules/desempeno`)

El escritorio sigue sin calcular: pinta lo que trae el contrato v2.2 y manda `contrato=2.2`.

### 4.1 Tres columnas (D18)
- Cuentas: pastillas "Por visitar", "No paga", "Pagados" y el filtro "Sin nada" (DA-3). Sustituyen a
  `CuentasTab.tsx:22-24` y `SEGMENTOS` (`domain/values/estadoCuenta.ts:29`).
- Tablero y ficha: `cobertura_por_columna` en lugar de `cobertura_por_segmento`.

### 4.2 Semana vencida (D19)
- Tablero: las filas con `semana_vencida` no se pintan; al final, botón "Ver N sin actividad reciente"
  que las despliega (N = filas con la bandera, contadas de lo recibido). Cada una muestra nombre, zona,
  inicio de semana y "Último movimiento: …" (o "Sin movimiento en 92 días").
- Ficha: aviso arriba ("Semana vencida · inicio {fecha} · último movimiento {fecha}"), tarjetas en "—",
  chips de `dias_navegables` + chip "inicio".

### 4.3 Horario y eje (D21)
- `ControlHorario.tsx:5-6` pasa de 5 opciones fijas por lado a selects con pasos de 30 min: inicio
  00:00–23:00, fin 00:30–23:30 más 23:59. Si el inicio elegido es ≥ fin, el fin se recorre a
  `min(inicio + 1 h, 23:59)`. Las preferencias guardadas (07:00–09:00 / 17:00–19:00 y el
  predeterminado 08:00–18:00, `presentation/preferencias.ts:23`) caen dentro de las opciones nuevas y
  siguen válidas sin migración.
- `components/lib/escala.ts:5-7` deja las constantes 7–20 y recibe el `eje` del servidor;
  `posicionMinutos` (`:19-22`) deja de pegar al borde lo que ahora cabe. `EjeJornada.tsx:24-27` y
  `MARCAS_EJE` se derivan del eje recibido.

### 4.4 Mapa (D22)
- `MapaJornada.tsx:31` y `MapaJornadaGoogle.tsx:16-17` (`useLoadScript` con
  `VITE_GOOGLE_MAPS_API_KEY`), `clientes/components/ficha/FichaUbicacion.tsx:22`,
  `clientes/components/ficha/FichaMapEmbed.tsx:16` y `clientes/components/detalle/PagoModal.tsx:38`
  leen `VITE_GOOGLE_MAPS_API_KEY`, que en `.env.production` está vacía (verificado: la línea termina en
  `=`). Ventas y Ventas Locales usan `cargarGoogleMaps(LLAVE_GOOGLE_MAPS)`
  (`src/components/maps/cargadorGoogleMaps.ts:16`, vía `MapaConCarga.tsx:25`).
- Cambio: todos pasan por `cargadorGoogleMaps.ts` y `LLAVE_GOOGLE_MAPS`; se retira `useLoadScript`
  de `MapaJornadaGoogle.tsx`. El cargador ya evita la doble carga (`cargaEnCurso` y `google.maps`
  presente). Prueba: con dos componentes de mapa montados, un solo `<script data-msp-google-maps>` en
  el documento.

### 4.5 Carga Hoy↔Semana y esqueletos (D23, D24)
- Causa leída: la ficha conserva el último tablero mientras llega el nuevo y lo rotula con su propio
  periodo (`DesempenoScreen.tsx:40-46`), mientras el toggle ya marca el nuevo.
- Regla: al tocar el toggle, el toggle cambia al instante; cifras, tarjetas y línea pasan a esqueleto;
  si la respuesta tarda más de 1 s aparece "Cargando semana…" (o "Cargando hoy…"); si falla,
  "No se pudo cargar" con "Reintentar". El marco de la ficha (encabezado, pestañas, lo tecleado en
  Actividad) no se desmonta. Nunca se pinta una cifra cuyo `periodo` no coincida con el seleccionado.
- Esqueletos con la forma real: fila de tablero (nombre + zona, barra de línea, celdas de cifras con su
  ancho) en lugar del genérico `FilasCargando` (`comunes/Estados.tsx:46`); ficha con nombre, las
  7 tarjetas (`FichaEncabezado.tsx:90-107`), pestañas y tarjeta "Línea del día". Mismas alturas que
  el contenido real.

### 4.6 Totales del día y correcciones (D25, D26, D28)
- **Totales (opción A)**: la tarjeta de `EjeJornada.tsx` gana encabezado: a la izquierda "Línea del
  día" + fecha; a la derecha, en mono, `horario · horas activas · pausas · clientes` y el cobrado del
  día (`resumen.pagos.importe`) grande en serif. Cada carril lleva su importe junto al conteo
  (`EjeJornada.tsx:55`): "Pagos 41 · $8,200", "Condonaciones 0 · $0" (importe de
  `resumen.condonaciones.importe`, v2.2); "Visitas" queda con conteo. Se quitan los textos sueltos
  `JornadaTab.tsx:91-95` y `:148-150`. Sin tarjetas nuevas.
- **Ficha sin tablero de flota**: la ficha usa el endpoint por usuario (§3.5), refresco cada minuto.
- **Cuentas sin pintar cientos de filas**: `TablaCuentas.tsx:54` hace `cuentas.map` de todo. Se
  pintan las primeras 60 y más al acercarse al final (centinela con `IntersectionObserver`), sin
  dependencia nueva (el proyecto no tiene librería de virtualización).
- **Mismo "hoy"**: todo cálculo de "es hoy" usa `hoy` del servidor (§3.6-2).
- **Orden en Semana**: en `ordenTablero.ts:28-49` las claves `horas`, `pagos` y `visitas` usan
  `promedio_diario` cuando el periodo es Semana; el encabezado lo dice en su popover ("ordena por
  promedio diario"). Las cifras visibles siguen siendo totales.
- **"No ha empezado" en Semana**: en vista Semana el texto es "Hoy sin actividad"
  (`TableroFila.tsx:29-33`, `FichaEncabezado.tsx:11`, `TarjetasTablero.tsx:45`). En Hoy no cambia.
- **"hace 0 d"** (`components/lib/format.ts:137`): si el inicio es del mismo día de negocio que `hoy`,
  el texto termina en "· hoy".
- **Columna Nota**: `COLUMNAS_POR_DEFECTO` la incluye, pero quien guardó columnas antes no la ve
  (`presentation/actividadPreferencias.ts:24-38`, clave `desempeno.actividad.columnas.v1`). Al leer una
  lista guardada en v1 se agrega "nota" una sola vez y se guarda en la clave `…columnas.v2`; si
  después la oculta, se respeta.
- **% cobro y "por cobrar"** juntos en una tarjeta (`FichaEncabezado.tsx:105-107`): no se cambia hasta
  cerrar M4 (§6).

---

## 5. Diseño — Android (`msp-app-kt`) para las 3 columnas (D18)

- Hoy: `SegmentoDeCobranza` (`feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/SegmentoDeCobranza.kt:53-73`),
  regla en `contiene` (`:82-106`) y `caePorFecha` (`:113-120`); trato en `EstadoCuentaUi.kt:24`
  (`TratoDelEstado`, en `:feature:pagos`, junto a imports de Compose).
- Cambio: la regla pura `columnaDe(estado, fechas, hoy)` y `TratoDelEstado` se mueven a
  `:core:common` (dominio de cobranza, sin Compose) para que el **mismo** corredor de casos
  compartidos (`core/common/src/test/kotlin/.../cobranza/domain/casos/EstadoCuentaCasosCompartidosTest.kt`)
  pruebe estado y columna. `feature/pagos` solo pinta las tres pastillas.
- Consumidores a ajustar (LEÍDO con búsqueda de `SegmentoDeCobranza` en `main`): `CarteraEnPantalla.kt`,
  `ListaDeClientesScreen.kt`, `components/PiezasDeLaLista.kt`, `ListaDeClientesViewModel.kt`,
  `ScrollPorSegmento.kt` y `domain/OrdenDeLaLista.kt` (todos bajo `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/`),
  más 17 archivos de prueba que lo nombran (incluidos `SegmentoDeCobranzaTest.kt`,
  `SegmentosVsPestanasLegadasTest.kt` y dos de capturas Roborazzi).
- Base de datos local: sin cambio (la columna se deriva; no se guarda).
- Versión nueva de la app (posterior a la vigente); ver orden de despliegue (§9).

---

## 6. Cambios de contrato y compatibilidad

### 6.1 Versionado
- Parámetro de consulta `contrato` en todo `/v2/desempeno/usuarios*`: `2.1` (por omisión) o `2.2`.
  Sin el parámetro el servidor responde **v2.1 congelado**: misma forma y mismas reglas que hoy,
  salvo dos cambios compatibles que aplican a ambas versiones: exclusión de zonas (una fila menos) y
  `pct = null` con `esperado = 0` (`pct` ya es nullable).
- Por qué así: el mapeador del escritorio 1.26.0 es estricto — `codigo()` lanza error ante un valor
  desconocido (`infrastructure/mappers/dtoADominio.ts:92-95`) y lo aplica a `motivo_no_configurado`
  (`:239-242`) y a `segmento` (`:372`); `numero()` lanza si falta `cobertura_por_segmento.volver` o
  `.despues` (`:268-272`). Cualquier código nuevo en un campo existente tumbaría su tablero entero.
  Que 1.26.0 ignore **campos extra** es INFERIDO (el mapeador lee claves por nombre); v2.1 no agrega
  campos, así que no depende de esa inferencia.
- Costo aceptado: mientras un escritorio pida v2.1, sus usuarios con semana vencida se siguen
  calculando completos (no hay forma de expresarlos en v2.1).

### 6.2 Contrato v2.2 (delta sobre v2.1)

`GET /v2/desempeno/usuarios?…&contrato=2.2`
```jsonc
{ "generado_at": "...Z", "hoy": "2026-10-15", "periodo": "semana", "pausa_min": 45,
  "eje": { "desde_hora": 6, "hasta_hora": 21 },
  "usuarios": [ {
    // ...todos los campos de v2.1, salvo cobertura_por_segmento...
    "semana_vencida": false,
    "ultimo_movimiento": { "id": "pago:...", "tipo": "pago", "hora": "...Z" } | null,
    "cobertura_por_columna": { "por_visitar": 66, "no_paga": 3, "pagados": 140, "sin_nada": 46 },
    "promedio_diario": { "dias": 6, "minutos_activos": 310, "pagos_importe": "1433.33", "visitas": 2.17 } | null,
    "cobro": { "cobrado": "...", "esperado": "0.00", "falta": "0.00", "pct": null }
  } ] }
```
- `cobertura_por_columna.sin_nada == cobertura.total − cobertura.con_algo`;
  `por_visitar + no_paga + pagados == cobertura.total` (las columnas son totales).
- `semana_vencida = true` ⇒ métricas en cero/`null`, `lineas = []`, `promedio_diario = null`.
- `promedio_diario = null` en `periodo=hoy`. `visitas` del promedio es decimal con 2 cifras
  (número JSON); `pagos_importe` es string de dinero.

`GET /v2/desempeno/usuarios/{uid}?…&contrato=2.2` (nuevo)
```jsonc
{ "generado_at": "...Z", "hoy": "2026-10-15", "periodo": "hoy", "pausa_min": 45, "eje": {...},
  "usuario": UsuarioTablero /* misma forma que un elemento de usuarios[] v2.2 */,
  "inicio_semana_dia": "2026-10-09" | null,
  "dias_navegables": ["2026-10-09", "...", "2026-10-15"] }
```

`GET /v2/desempeno/usuarios/{uid}/jornada?…&contrato=2.2`: agrega `hoy`, `eje`, `horario`;
`resumen.condonaciones` pasa de entero a `{ "n": 0, "importe": "0.00" }`; `linea.por_hora` cubre el eje.

`GET /v2/desempeno/usuarios/{uid}/cuentas?…&contrato=2.2`: parámetro `columna=por_visitar|no_paga|pagados|sin_nada`
(sustituye a `segmento`); `conteos {por_visitar, no_paga, pagados, sin_nada, total}`; en cada cuenta
`columna` (nunca `null`) en lugar de `segmento`.

`GET /v2/desempeno/usuarios/{uid}/actividad?…&contrato=2.2`: misma forma; rango por omisión según §3.2.

Errores nuevos: `desempeno_zona_excluida` (409). Si DA-2 se aprueba: `desempeno_semana_vencida` (409).

### 6.3 Retiro de v2.1
Se retira cuando el log de acceso muestre **cero** peticiones a `/v2/desempeno/usuarios*` sin
`contrato=2.2` durante 7 días seguidos (MEDIDO sobre el log de producción, no supuesto). Entonces se
borran `SegmentoDe`, sus DTOs y la rama v2.1, y el archivo de contratos queda solo con v2.2.

---

## 7. Qué toca dinero y su ruta §8

"Dinero" según CLAUDE.md §8: pagos, saldos, ventas, cobranza y el sync. Cada punto: entrada nueva en
`docs/evidencia/` con el siguiente número libre al escribirla (en `infra.md` hoy sería `E-INF-017`),
citada desde el comentario del código, el nombre de la prueba y el commit; **compuerta antes** y
**compuerta después** por agentes independientes del autor.

| # | Cambio | Por qué es dinero | Antes de cambiar (MEDIDO) | Después |
|---|---|---|---|---|
| M1 | 3 columnas (D18) | Decide qué cuentas ve el cobrador como pendientes; una cuenta mal puesta es una puerta que no se toca | Casos compartidos con `columna` pasando en Go y Kotlin con el mismo `VERSION`; arnés de equivalencia (§8.3) mostrando que las únicas diferencias son las de la tabla de §3.1; distribución por columna en producción (solo lectura) | Prueba roja al revertir; consumidores enumerados (§3.1, §5); E-INF-016 deja de estar VIGENTE (su "Caduca si: el teléfono adopta la misma regla" se cumple) con puntero a la entrada nueva, sin borrarse |
| M2 | `pct = null` con `esperado = 0` (D20) | Cifra de cobro mostrada | Producción, solo lectura: cuántos usuarios tienen `esperado = 0` y `pct` no nulo, y que son los de mayoreo u otros identificables; control positivo: un usuario con `esperado > 0` sigue igual | Prueba roja al revertir; arnés: solo cambian filas con `esperado = 0` |
| M3 | Cobrado sobre ventanas de varios días (D26) | Cobrado y % de cobro | Producción, solo lectura: distribución de largo de ventana y de `cobrado/esperado` por usuario; reproducir el 330 % de dev con su consulta; comparar contra Cobranza semanal (`/v2/rutas/...`), que usa el mismo `AbonoSemana` — un arreglo solo en desempeño **divergiría** de lo que oficina ve en Cobranza semanal (D-API-1, `cobro.go:6-12`) | La opción (esperado proporcional a semanas de la ventana, cobrado limitado a 7 días, o solo rotular) la elige el dueño **con la medición en la mano**; ver DA-4 |
| M4 | Rareza Patricio de la Luz (D27) | % de cobro y falta | Medir, no adivinar: sus ventas de la semana con parcialidad, `AbonoSemana`, aporte y `AplicaPonderado`; recomputar `pct` y `falta` a mano; control positivo: el mismo procedimiento sobre un usuario cuyas dos cifras sí cuadran. Hipótesis INFERIDA a confirmar o refutar: `pct` es promedio de aportes por venta (`desglose.go:48-65`) y `falta` es `max(0, esperado − cobrado)` agregado (`cobro.go:30`), dos medidas distintas; un sobrepago en una venta puede llevar `falta` a 0 sin subir el promedio | Ningún cambio de código hasta que la entrada esté en MEDIDO y la compuerta antes dé SOSTIENE |
| M5 | Horas activas con horario (D21) | No es dinero (tiempo); importes no se recortan | — (ruta normal de pruebas) | Prueba que asegura que pagos/importes del día no cambian con o sin horario |
| M6 | Semana vencida (D19) | Oculta cifras de cobro de esos usuarios | Producción, solo lectura: cuántos usuarios cumplen > 14 días y que ninguno tenga actividad propia en los últimos 14 días (si alguno la tiene, se reporta al dueño antes de ocultarlo) | Arnés: filas no vencidas idénticas a v2.1 salvo lo de M1/M2 |

Toda medición en producción es **solo lectura** (memoria del proyecto: producción SOLO LECTURA; consultas
por `.bat`).

---

## 8. Estrategia de pruebas

### 8.1 Pruebas rojas al revertir
Cada cambio de regla trae una prueba que se pone roja al regresar el archivo de la regla al commit
anterior (como se hizo en E-INF-016). Mínimo: `ColumnaDe` (8 estados × 4 fechas), invariante de
partición, `DiasSemanaVencida` en 14 d exactos (14 d − 1 min no vencida; 14 d + 1 min vencida),
`pct` con `esperado = 0`, horas activas con horario (actividad toda dentro, toda fuera, cruzando
inicio, cruzando fin, pausa partida por el horario), `por_hora` cubriendo el eje, `ultimo_movimiento`
nulo a 92 d + 1 min, cancelación registrada como Info.

### 8.2 Casos compartidos Kotlin↔Go
- Hoy: 40 casos + `VERSION` en `msp-api/testdata/estado-cuenta/` y copia en
  `msp-app-kt/core/common/src/test/resources/estado-cuenta/` (rama `feat/estado-cuenta-casos-compartidos`,
  E-APP-057). Ninguno trae columna ni segmento (LEÍDO: 0 archivos contienen `segmento`).
- Cambio de formato: cada caso gana `"hoy": "YYYY-MM-DD"` y cada esperado gana `"columna"`. Casos
  nuevos obligatorios: cita a futuro, cita sin fecha, promesa vencida, promesa de hoy, promesa a
  futuro, promesa sin fecha, se negó, parcial, no estaba. Nuevo `VERSION`; el mismo hash en los dos
  repos demuestra que el juego es idéntico. Corre en Go (`internal/desempeno/domain/casos_compartidos_test.go`)
  y en Kotlin (corredor de `:core:common`, §5).

### 8.3 Arnés de equivalencia
`TestEquivalenciaDesempeno` (`internal/desempeno/infra/desempenofb/equivalencia_tablero_test.go:291`):
1. Grabar referencia con `e1762f5` (fuera del repo, contiene nombres de dev).
2. Con el cambio, **v2.1** debe salir idéntica salvo filas de zonas excluidas y `pct` con `esperado = 0`
   (comparación estructural completa, como en E-INF-016).
3. **v2.2** contra v2.1: por cada cuenta, la columna nueva debe corresponder a la tabla de §3.1 a partir
   de su trato; cualquier otra diferencia es defecto. Control positivo: el arnés falla con el cambio
   (detecta las diferencias esperadas).

### 8.4 Rendimiento
`TestMedirTablero` (`internal/desempeno/infra/desempenofb/medicion_tablero_test.go:299`) con el
escenario de 55 usuarios y semana de 338 días: antes (v2.1) y después (v2.2), Hoy y Semana, frío y
tibio, con tiempo, sentencias y tamaño de respuesta. Se reporta la cifra medida en la entrada de
evidencia; el criterio de aceptación es que v2.2 con usuarios vencidos cueste lo mismo que el
escenario sin ellos (`MEDIR_UNO_VIEJO` ya existe para el caso de un solo viejo). Incluye la consulta de
`ultimo_movimiento` y su `PLAN` en Firebird.

### 8.5 Escritorio y Android
- Escritorio: mapeador v2.2 con MSW (incluido un JSON v2.1 que ya no debe aceptarse en el adaptador
  nuevo); toggle Hoy↔Semana con respuesta lenta (esqueleto, "Cargando semana…", nunca cifra de otro
  periodo) y fallida ("Reintentar"); esqueletos con la misma altura que el contenido; un solo script de
  Maps; columna Nota para preferencias v1; "· hoy".
- Android: casos compartidos en `:core:common`; pruebas de la lista con 3 pastillas; capturas
  Roborazzi regeneradas **y revisadas a ojo** (un verde de Roborazzi no prueba nada si no compara).

---

## 9. Orden de despliegue

1. **Contrato**: actualizar `2026-10-14-desempeno-contratos.md` a v2.2 (este documento §6) y aprobarlo.
2. **Casos compartidos** con `hoy` y `columna`, mismo `VERSION` en msp-api y msp-app-kt.
3. **Mediciones §7** en producción (solo lectura) y compuertas "antes" para M1, M2, M6. M3 y M4 siguen
   su propio paso y no bloquean lo demás.
4. **API** (v2.1 por omisión + v2.2 por parámetro + exclusión de zonas + migración de
   `MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS` y, si hace falta, índice para `ultimo_movimiento`). El escritorio
   1.26.0 sigue funcionando (v2.1). Compuerta "después".
5. **Escritorio** (versión posterior a 1.26.0) pidiendo `contrato=2.2`, **y app Android** con las 3
   columnas, publicados el mismo día para que teléfono y escritorio no difieran. La app no depende del
   API para esto (deriva en el aparato), así que puede publicarse antes que el escritorio, nunca antes del
   paso 2.
6. **Retiro de v2.1** según §6.3.

---

## 10. Decisiones (antes abiertas)

Resueltas por el dueño el 2026-10-15:

- **DA-1 · Cómo se excluye mayoreo → (b) lista configurable de zonas excluidas en Configuración**
  (`MSP_CFG_*`, `internal/config`), como asume §3.3.
- **DA-2 · Cuentas de un usuario con semana vencida → sí:** el endpoint responde 409
  `desempeno_semana_vencida` y la pestaña muestra el aviso.
- **DA-3 · "Sin nada" → se conserva solo en el escritorio**, como filtro de cobertura (D5) fuera de las 3
  columnas.
- **DA-4 · Cobrado sobre ventanas de varios días (M3) → medir primero.** Nada cambia hasta tener la
  medición con control positivo y las dos compuertas de §8 de CLAUDE.md; la opción se decide con esos
  números.

Aprobados también por el dueño (2026-10-15), propuestos por el autor del documento: consulta de
`ultimo_movimiento` acotada a 92 días (su costo se mide), Cuentas con 60 filas iniciales, "Cargando
semana…" tras 1 s, Actividad de semana vencida con los últimos 14 días por omisión.

No quedan decisiones abiertas en este documento.

Quedó sin verificar al escribir este documento: el registro de acceso de la plataforma para peticiones
canceladas (§3.6-5); que 1.26.0 ignore campos extra (no se necesita, §6.1); la equivalencia Go↔Kotlin de
la promesa sin fecha (§3.1, la cubre un caso nuevo); todas las cifras de producción de §7.

---

## 11. Fuera de alcance

- **Documento 2 "visitas, compromisos y avisos"** (promesas y citas al servidor, avisos): va aparte.
- Cambios en `internal/rutas` o en Cobranza semanal (M3 solo mide y compara).
- Cambios en el sync de cobranza (`docs/COBRANZA-SYNC.md`): nada aquí toca sync, digest ni by-ids.
- GPS continuo, sanciones, nómina (siguen fuera, diseño anterior §1).

---

## 12. Auto-revisión (2026-10-15)

- **Sin pendientes sin dueño**: lo no decidido está en §10 con recomendación o propuesta; lo no medido
  está en §7 con su medición y en el párrafo final de §10.
- **Contradicciones revisadas**: (1) D19 oculta cifras y M6 exige medir que nadie con actividad reciente
  quede oculto — compatibles: la regla es por fecha de inicio, la medición la valida. (2) "Teléfono y
  escritorio iguales" vs. "Sin nada" en escritorio — resuelto como filtro de cobertura, no columna, y
  abierto en DA-3. (3) v2.1 congelado vs. `pct = null` y exclusión — son los únicos cambios en v2.1 y
  ambos son compatibles con el mapeador de 1.26.0 (`pct` nullable; una fila menos). (4) Horas activas
  recortadas vs. dinero — los importes nunca se recortan (§3.4).
- **Ambigüedades cerradas**: umbral exacto (> 14 × 24 h); ventana de "último movimiento" (92 días);
  días navegables (14); pasos del horario (30 min, más 23:59); regla de corrimiento del fin
  (`min(inicio + 1 h, 23:59)`); cita sin fecha (Por visitar); retiro de v2.1 (7 días sin peticiones v2.1,
  medido); tiempo para "Cargando semana…" (1 s); filas iniciales de Cuentas (60).
- **Vocabulario**: "semana", nunca la otra palabra; textos de UI cortos.
- **Citas**: cada `archivo:línea` se leyó el 2026-10-15 en los commits de la cabecera. Dos citas del
  encargo se corrigieron al leer: `senales.go:90-117` (no 91-118) y `format.ts:137` (no 136);
  `ResumenJornadaDTO` está en `dto.go:206-215`.
