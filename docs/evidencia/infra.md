# Evidencia — infraestructura y entorno

Registro de hechos y su prueba sobre el entorno: la máquina de desarrollo, el
build, las herramientas de prueba y el servidor de producción. Son las trampas
que hacen que un verde no signifique nada, o que un comando parezca haber
corrido cuando no corrió.

El diseño y las reglas viven en
[`../registro-de-evidencia.md`](../registro-de-evidencia.md).

## Los cuatro niveles

| Nivel | Qué significa | Qué tiene que enseñar |
|---|---|---|
| **MEDIDO** | Se ejecutó y hay salida | La salida, el comando o la consulta, la fecha, **dónde** (dev / producción / prueba) y un **control positivo** |
| **LEÍDO** | Está escrito en el código | `archivo:línea`. No se ejecutó |
| **INFERIDO** | Se deduce de lo leído | De qué se deduce, y **qué lo volvería MEDIDO** |
| **SUPUESTO** | Nadie lo revisó | Nada. Es una creencia, y se marca como tal |

Aquí la regla dura de dinero no aplica directamente, pero sí su corolario, y es
el que cobra factura: **una ausencia no es un hallazgo** hasta enseñar el
control positivo que demuestre que la búsqueda o la prueba habría encontrado el
caso. Casi todo lo de este archivo existe porque alguien confió en un verde.

## Cómo se cita desde el código

El identificador (`E-INF-003`) se cita desde el comentario que explica por qué
una prueba está configurada de una forma rara, o desde el mensaje del commit:

```kotlin
@GraphicsMode(GraphicsMode.Mode.NATIVE) // sin esto las métricas son basura: E-INF-003
```

Los números son correlativos y **nunca se reutilizan ni se renumeran**. Lo
refutado **no se borra**: cambia de estado a REFUTADA y se queda.

**Siguiente identificador libre: `E-INF-013`.**

---

### E-INF-001 · `timeout` no existe en macOS

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-23, Mac del desarrollador (Darwin 25.2.0), zsh y bash
- **Cómo y salida:**

  ```
  $ which timeout
  timeout not found

  $ timeout 2 echo hola
  zsh: command not found: timeout
  exit=127

  $ bash -c 'timeout 2 echo hola; echo exit=$?'
  bash: timeout: command not found
  exit=127
  ```

- **Consecuencia:** cualquier comando envuelto en `timeout ...` **no se ejecuta**.
  El comando de adentro nunca corre, y lo que se lee después es el efecto de que
  no corriera, no su resultado.
- **CORRECCIÓN a la versión que circulaba:** se venía diciendo que *"sale 0 sin
  ejecutar"*. **Eso no se reprodujo:** el shell devuelve **127**, no 0. La parte
  peligrosa de la afirmación —el comando no corre— sí es cierta; la parte del
  código de salida, no. Si alguien construyó una tubería asumiendo que el 0
  ocultaba el fallo, el supuesto era otro.
- **Alternativa:** `gtimeout` (coreutils de Homebrew) si está instalado, o
  correr el comando en segundo plano y vigilarlo.
- **Caduca si:** se instalan las coreutils de GNU en el `PATH` por defecto

### E-INF-002 · `testDebugUnitTest` no graba ni verifica goldens de Roborazzi

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-22, `msp-app-kt`
- **Procedencia:** nota de memoria del proyecto
  (`reference_roborazzi_verde_no_prueba_nada`). **La salida cruda no está en el
  árbol.**
- **Qué dice:** `captureRoboImage` es un **no-op sin `-P`**. Correr
  `:modulo:testDebugUnitTest` y verlo verde **no compara ni una imagen**. Las
  tareas reales son `recordRoborazziDebug` (graba los PNG) y
  `verifyRoborazziDebug` (compara contra lo grabado).
- **La trampa de encima:** `verify` corre contra lo que `record` acabó de
  escribir, así que **encadenar los dos también da verde siempre**. Un golden
  regenerado y "verificado" no dice si la pantalla quedó bien; dice que el
  archivo coincide consigo mismo.
- **La revisión de verdad es el `git diff` de los PNG** — abrirlos y mirarlos.
  Cuando un agente reporte "goldens verdes", la pregunta correcta es **qué vio en
  las imágenes**, no si la tarea pasó.
- **Corroboración LEÍDO (2026-09-23):** `msp-app-kt/INTEGRACION.md:216-217` y
  `:233-235` listan `verifyRoborazziDebug` como tarea **aparte** de
  `testDebugUnitTest` dentro de la compuerta pre-push, lo que confirma que son
  dos cosas distintas.
- **Caduca si:** se cablea la verificación de goldens dentro de
  `testDebugUnitTest`

### E-INF-003 · Sin `@GraphicsMode(NATIVE)` las métricas de fuente de Robolectric son basura

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:**
  `msp-app-kt/core/designsystem/src/test/kotlin/.../component/MoneyNoTruncationTest.kt:68-74`,
  literal: *"verificado en debug que sin Robolectric Native Graphics las métricas
  de fuente son basura (un `TextLayoutResult` de ~14×35px para un string que mide
  cientos de px reales) — este test necesita medidas de layout REALES"*.
- **Consecuencia:** una aserción sobre ancho, alto o truncación de texto puede
  dar **verde contra código roto** si el test no activa RNG. El modo legacy basta
  para semántica (`MspStatusChipTest` y compañía) y no para medidas.
- **PRECISIÓN sobre la cifra:** la versión que circulaba hablaba de *"8 dp
  reportados"*. **Esa cifra no aparece en este archivo** y pertenece a otra
  trampa distinta, la de `boundsInRoot` (E-INF-004). Lo que este código documenta
  es `~14×35 px`. Se registran separadas a propósito: son dos mecanismos, y
  confundirlos lleva a poner `@GraphicsMode` donde el problema era el recorte, o
  al revés.
- **Nivel, con honestidad:** esto es un **comentario de código** que reporta una
  medición ajena. Es LEÍDO, no MEDIDO. **Qué lo volvería MEDIDO:** quitar
  `@GraphicsMode(NATIVE)` de ese test y enseñar el `TextLayoutResult` que sale.
- **Caduca si:** Robolectric cambia el comportamiento del modo legacy

### E-INF-004 · `boundsInRoot` viene recortado por los padres, así que un nodo fuera del viewport "cabe"

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-22, `msp-app-kt`
- **Procedencia:** nota de memoria del proyecto
  (`reference_roborazzi_verde_no_prueba_nada`). **La salida cruda no está en el
  árbol.**
- **El mecanismo:** un nodo **completamente** fuera del viewport devuelve
  `Rect.Zero`, y una aserción del tipo
  `left >= pantalla.left && right <= pantalla.right` **la cumple un rectángulo
  vacío sin esfuerzo**. Perverso: *el chip que menos se veía era el que más fácil
  pasaba*. Uno parcialmente fuera reportaba **8 px de sus 214** y también
  "cabía". Y `assertTextContains` lee la semántica, que trae el texto completo
  aunque no se pinte un solo píxel.
- **La forma honesta de medirlo:** comparar `boundsInRoot` (recortado) **contra
  `size`** (sin recortar). Si no coinciden, el nodo está cortado.
- **Corolario del degradado de orilla:** uno que va de `Transparent` a
  `colors.surface` **sobre** `colors.surface` pinta **cero píxeles** donde no cae
  un glifo debajo. Se diagnostica leyendo la luminancia del PNG, no mirándolo.
- **Y al revés:** un fallo de toque bajo Robolectric no es defecto de producción
  hasta reproducirlo en el aparato. Un `clip(RoundedCornerShape)` con esquinas
  desiguales mata el hit-test de los descendientes **sin gráficos nativos** —
  condición que Robolectric cumple y un teléfono no.
- **Caduca si:** cambia la semántica de `boundsInRoot` en Compose

### E-INF-005 · `run.bat` trunca `api.log` en cada arranque

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-23
- **Dónde:** `docs/ops/pendientes-2026-09-11.md:33-34` (*"El log los tiene, pero
  `run.bat` trunca `api.log` en cada arranque"*) y
  `docs/ops/despliegue-pagos-perdidos-2026-09-02.md:105`, que por eso incluye el
  paso *"respaldar el log ANTES de reiniciar: run.bat lo TRUNCA en cada
  arranque"*, con el `copy /Y` de la línea 108.
- **El mecanismo:** el arranque es
  `msp-api.exe serve > C:\msp-api\api.log 2>&1` — la redirección `>` trunca.
  (La forma se ve en `docs/deploy-test-server-runbook.md:134` para el servidor de
  prueba.)
- **Consecuencia:** reiniciar el servicio para investigar un incidente **destruye
  la evidencia del incidente**. El respaldo del log va antes del reinicio,
  siempre.
- **Límite de esta lectura:** `run.bat` **no está en el repositorio** — vive en
  el servidor Windows. Lo verificado es lo que dicen los runbooks, no el archivo.
  **Qué lo volvería MEDIDO:** un `type C:\msp-api\run.bat` por SSH.
- **Caduca si:** se cambia `>` por `>>` o se mete el arranque en `nssm`

### E-INF-006 · KSP truena con módulos en paralelo

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-22, `msp-app-kt`
- **Procedencia:** nota de memoria del proyecto
  (`reference_gradle_ksp_paralelo_y_goldens`). **La salida cruda no está en el
  árbol.**
- **Qué dice:** KSP falla con `Wrong plugin option format: null` cuando dos
  módulos corren a la vez (`kspDebugUnitTestKotlin`). **Siempre `--no-parallel`,
  un módulo a la vez.** Reintentar basta para que pase.
- **Corroboración parcial LEÍDO (2026-09-23):** `msp-app-kt/gradle.properties:13`
  tiene `org.gradle.parallel=true` **comentado**. *Honestamente:* esa línea viene
  comentada desde la plantilla de Android Studio y **no trae ninguna nota que la
  ligue a KSP**, así que corrobora el estado (el paralelo está apagado) y **no**
  la causa.
- **Dos trampas hermanas de la misma nota:**
  - **El worktree compartido pega.** Con varios agentes encima, el árbol se rompe
    a ratos por ediciones en vuelo de otro agente. **Un rojo puede no ser tuyo:**
    verificar antes de investigar.
  - **Un verde cacheado no prueba nada.** Usar `--rerun-tasks` y **contar las
    pruebas reportadas en el XML** antes de confiar en un resultado.
- **Caduca si:** sube la versión de KSP o del plugin de Kotlin

### E-INF-007 · `config/db_debug.blockDangerousQueries` está en `true`, no en `false`

- **Nivel:** LEÍDO (el código) + MEDIDO por sus efectos (la corrida del
  2026-09-24) · **Estado:** VIGENTE (es una **corrección a una nota de entorno**)
- **Leído / medido:** 2026-09-24, `msp-app-kt`, rama `feat/notas-visibles`
  (`d88dfb8c`)
- **Qué se venía diciendo:** que el depurador remoto del Room tenía
  `blockDangerousQueries` en **`false`** —es decir, que aceptaría escrituras—.
  **No es así.**
- **Dónde está escrito:**
  - `app/.../core/debug/models/DebugConfig.kt:11` —
    `val blockDangerousQueries: Boolean = true` (valor por omisión del modelo);
  - `DebugConfig.kt:20` — al leer el documento de Firestore,
    `blockDangerousQueries = map["blockDangerousQueries"] as? Boolean ?: true`.
    **El `?: true` es lo que manda:** si la clave falta en `config/db_debug`, o
    viene con otro tipo, **el bloqueo se queda encendido**. Para apagarlo hay que
    escribir `false` explícitamente y del tipo correcto.
  - `app/.../core/debug/DebugCommandExecutor.kt:91` —
    `if (config.blockDangerousQueries && isDangerousQuery(query))` es quien lo
    aplica, antes de `executeRawQuery` (`:99`).
- **Corroboración de campo:** la lectura del 2026-09-24 (E-APP-029 y siguientes,
  `app-android.md`) se hizo **sólo con `SELECT`**, que es lo único que ese
  camino admite con el bloqueo puesto. Ninguna consulta de escritura se intentó,
  así que **no hay control positivo de que el bloqueo rechace** — lo que hay es
  que la ruta permitida funcionó. **Qué lo volvería MEDIDO del todo:** mandar un
  `UPDATE` de prueba y enseñar el rechazo.
- **Por qué importa, en las dos direcciones:** creerlo en `false` invita a
  planear una corrección de datos por el depurador que **va a rebotar**; y
  creerlo en `false` cuando está en `true` también hace pensar que el aparato
  está más expuesto de lo que está. El estado real es el conservador.
- **Caduca si:** alguien escribe `blockDangerousQueries: false` en
  `config/db_debug`, o cambia el `?: true` de `DebugConfig.kt:20`

### E-INF-008 · El truncamiento del depurador remoto es SILENCIOSO: se calcula y se tira

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-09-24, `msp-app-kt`, rama `feat/notas-visibles` (`d88dfb8c`)
- **El corte existe y está bien hecho:**
  `app/.../core/debug/DebugCommandExecutor.kt:145` —
  `while (cursor.moveToNext() && rowCount < maxRows)`, con
  `maxRows = config.maxQueryRows` (`:99`), cuyo valor por omisión es **1000**
  (`DebugConfig.kt:10` y `:19`).
- **Y la señal del corte se calcula:** `DebugCommandExecutor.kt:163-164` —
  `val totalRows = if (cursor.moveToLast()) cursor.position + 1 else 0` y
  `val truncated = totalRows > maxRows`; las dos viajan en el
  `QueryResult` (`:170-171`), que las declara en `:211-212`.
- **Y ahí se pierde. La cita exacta es `DebugCommandExecutor.kt:56-66`**, donde
  el `QueryResult` se convierte en el `DebugResult` que va a Firestore:

  ```kotlin
  DebugResult(
      commandId = command.id,
      status = if (result.success) CommandStatus.SUCCESS else CommandStatus.ERROR,
      columnNames = result.columnNames,
      data = result.data,
      rowCount = result.data?.size,
      ...
  )
  ```

  **`result.truncated` y `result.totalRows` no se pasan.** No es que `toMap()`
  los omita: **`DebugResult` no tiene esos campos** (`DebugResult.kt:9-19`), así
  que `toMap()` (`:21-34`) no tiene nada que copiar. La pérdida ocurre **una
  capa antes** de donde se venía señalando.
- **Y `rowCount` remata el engaño:** es `result.data?.size`, o sea **el tamaño
  de lo que sobrevivió al corte**. Un resultado cortado en 1000 reporta
  `rowCount = 1000` y **se lee igual que un resultado completo de 1000 filas**.
- **Consecuencia operativa, y es la que cuesta dinero:** una consulta de censo
  —"cuántos pagos hay", "cuántas condonaciones tiene este cargo"— puede volver
  cortada y **nunca anunciarlo**. Una ausencia leída sobre un resultado cortado
  es una ausencia falsa, que es exactamente lo que el corolario de
  `registro-de-evidencia.md` §4.1 prohíbe dar por hallazgo.
- **La defensa que funciona, y hay que usarla siempre:** un **canario** —mandar
  `COUNT(*)` en la misma tanda y cruzarlo contra el `rowCount` del resultado—.
  Es lo que hizo la lectura del 2026-09-24 (`COUNT(*) = 24` contra
  `rowCount = 24`, **E-APP-029** en `app-android.md`), y sin él esa medición no
  valdría nada.
- **Caduca si:** `DebugResult` gana `truncated`/`totalRows` y
  `DebugCommandExecutor.kt:56-66` los pasa, o desaparece `maxQueryRows`

### E-INF-009 · Una prueba que lee la constante que debería fijar NO puede fallar

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, `msp-app-kt`, módulo nuevo `:core:geo`
- **Qué pasó.** La prueba que debía fijar el radio de agrupación de 30 m estaba
  escrita así:

  ```kotlin
  medicion(desplazado(PUERTA, AgrupadorDeLugares.RADIO_DE_AGRUPACION_M - 1))  // "29 m"
  medicion(desplazado(PUERTA, AgrupadorDeLugares.RADIO_DE_AGRUPACION_M + 1))  // "31 m"
  ```

  Se mutó la constante de `30.0` a `60.0` para comprobar el rojo. **La suite
  pasó**: 36 casos, 0 fallos, `BUILD SUCCESSFUL`. La prueba medía 59 y 61 metros
  contra un radio de 60 y seguía cumpliéndose.
- **El mecanismo:** una aserción escrita **en términos del valor que verifica**
  es una tautología. Se mueve junto con el código y por construcción nunca lo
  contradice. Es la versión sutil del golden regenerado que se compara consigo
  mismo (E-INF-002): el archivo coincide con el archivo, la prueba coincide con
  la constante.
- **El arreglo, y el control:** distancias **literales** (29.0 y 31.0). Con la
  constante todavía en `60.0`, la suite dio `36 tests completed, 1 failed` y el
  XML nombró `el radio corta en treinta metros, ni mas ni menos`. Restaurada a
  `30.0`: 36 casos, 0 fallos, y `diff -r` contra el respaldo **vacío** — sin
  restos de la mutación.
- **Corroboración por contraste (control positivo del método):** las otras dos
  reglas del módulo sí estaban escritas con literales y sí se cayeron al
  mutarlas. Quitar `!it.esCompartido && !it.esSoloDeTransferencias` de
  `LugaresDelCliente` puso **3 rojas**; bajar `MINIMO_PARA_EL_CIRCULO` de 5 a 2
  puso **2 rojas**. O sea: el problema no era la suite, era esa prueba.
- **Consecuencia, y por qué se registra:** el repo ya exige *"cada arreglo con
  una prueba que se ponga ROJA al revertirlo, compruébelo de verdad"*
  (`msp-app-kt/CLAUDE.md` §2). Este caso añade **cuál** es la forma de fallar
  que no se ve: la prueba existe, tiene buen nombre, cubre la línea, entra en la
  cobertura — y no puede fallar. **Una prueba que importa una constante del
  código bajo prueba hay que mirarla dos veces.**
- **Nota de honestidad sobre esta misma corrida:** en el primer intento de la
  mutación el build salió `FAILED` sin que ningún XML registrara fallos. No se
  reprodujo en las dos corridas siguientes (las dos `BUILD SUCCESSFUL` con la
  constante mutada), así que **la causa quedó sin probar** y lo que vale es el
  resultado reproducible: con la prueba tautológica, mutar la constante no la
  rompe.
- **Caduca si:** se agrega un linter o una regla de detekt que prohíba importar
  constantes de producción en las aserciones que las fijan

### E-INF-010 · Una prueba que depende de archivos escritos por otra prueba de la misma corrida valida la corrida anterior

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, `msp-app-kt`, módulo nuevo `:feature:ubicacion`
- **Qué se escribió.** Para fijar un defecto que los goldens acababan de
  destapar (ver **E-INF-011**), se escribió una prueba que comparaba el **SHA-1
  de dos PNG**:

  ```kotlin
  assertNotEquals(
      "la hoja no distingue un punto compartido de un lugar de puras transferencias",
      File(carpeta, "ubicacion_compartido_light.png").sha1(),
      File(carpeta, "ubicacion_transferencias_light.png").sha1()
  )
  ```

  Los PNG los produce **otra clase de prueba del mismo módulo**
  (`LaHojaDiceLoQueSabeTest`, vía `recordRoborazziDebug`).
- **Los dos comportamientos, los dos medidos en la misma sesión:**
  - Con los goldens **ya en disco** de una corrida previa: **verde**. Pero lo que
    validaba eran **las imágenes viejas**, no las que esa corrida acababa de
    producir.
  - Tras `rm src/test/screenshots/*.png` y regrabar en la misma invocación de
    Gradle: **`28 tests completed, 2 failed`**, con
    `java.lang.AssertionError: no hay goldens grabados` y
    `java.lang.AssertionError: falta ubicacion_compartido_light.png`. La prueba
    del hash corrió **antes** de que la de captura escribiera los archivos.
- **El mecanismo:** **JUnit no garantiza el orden entre clases de prueba**, así
  que una prueba que lee un artefacto producido por otra de la misma corrida
  tiene dos finales, y **los dos son malos**: si gana la carrera, falla por
  archivos ausentes; si la pierde —o si quedaron restos de antes— pasa validando
  el estado **anterior**. El verde no dice nada del código que se acaba de
  cambiar.
- **Por qué cuenta como falso verde y no como test flaky:** un flaky falla a
  veces sin motivo. Éste **pasa a veces sin mirar lo que dice mirar**, que es
  peor, porque nadie investiga un verde.
- **La forma correcta, y es la que quedó:** montar la pieza y **leer lo que
  pinta** (`createComposeRule` + `onNodeWithText`), en vez de comparar
  subproductos en disco. Determinista, sin archivos, sin orden. Verificada por
  mutación: revertir el arreglo del defecto pone **3 rojas** que lo nombran.
- **Alcance:** aplica a cualquier prueba que lea un artefacto de `build/` o de
  `src/test/screenshots/` que produzca otra prueba. **No** aplica a los goldens
  de Roborazzi en sí, cuyo `verify` es una tarea de Gradle aparte con dependencia
  declarada — ése tiene su propia trampa distinta, **E-INF-002**.
- **Guardada por:** — (se retiró la prueba frágil; la lección vive aquí y en el
  KDoc de `LaHojaDiceLasDosCuentasTest`)
- **Caduca si:** el módulo declara un orden explícito entre esas pruebas y una
  dependencia de tarea real entre producir y comparar

### E-INF-011 · El productor probado no prueba al consumidor: quince pruebas verdes sobre una función a medio cablear

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, `msp-app-kt`, `:feature:ubicacion`
- **La regla de negocio.** Decisión cerrada del dueño: un punto de cobro
  compartido por muchos clientes **no se nombra** ("la tienda", "la casa del
  cobrador" son conclusiones que la app no puede sostener). En su lugar la hoja
  enseña **las dos cuentas**: *"Aquí cobran 30 cobradores a 381 clientes"*.
- **Lo que había:** quince pruebas verdes, entre ellas una que afirmaba
  exactamente esa frase sobre `LugarEnElMapa.detalle` — el **productor** del
  texto.
- **Lo que NO había:** una sola prueba de que la **hoja** lo pintara. Y no lo
  pintaba: `HojaDeLosLugares` sólo mostraba `puerta.detalle`, y en un punto
  compartido no hay puerta (ese es el punto), así que caía en el aviso genérico
  *"Sin punto propio medido"* y **los dos números no aparecían en ninguna
  parte** salvo el globo del marcador, detrás de un toque que el cobrador no
  tiene por qué adivinar.
- **Consecuencia:** la decisión aprobada estaba **construida a medias**, con
  **quince pruebas de dominio y nueve goldens en verde** sobre ella.
- **Qué lo cazó, y no fue un aserto:** dos goldens salieron **idénticos byte a
  byte** (`4ab8daed…` para `ubicacion_compartido_light.png` y
  `ubicacion_transferencias_light.png`). Dos estados que deben decir cosas
  distintas produciendo el mismo píxel. **Mirar cada imagen por separado no lo
  habría delatado**: las dos se veían perfectamente razonables.
- **La lección, que es la que hay que recordar:** **una prueba que verifica que
  el dato EXISTE no dice nada sobre si LLEGA A LOS OJOS DE ALGUIEN.** Probar el
  productor —la función que arma la cadena, el mapper, el use case— deja
  descubierto todo el tramo entre él y el pixel. Cuando una regla de negocio se
  expresa como *"el usuario ve X"*, la prueba tiene que **montar la pantalla y
  leer X**, no comprobar que alguien sabe calcular X.
- **Por qué es el peor de los cuatro falsos verdes de esta semana:** las otras
  tres variantes (**E-INF-002** golden comparado consigo mismo, **E-INF-009**
  prueba que lee la constante que fija, **E-INF-010** prueba que valida la
  corrida anterior) **se ven raras al leerlas**. Ésta se ve impecable: nombre
  correcto, aserto sobre el string exacto de la regla, cubre la línea, entra en
  la cobertura. Lo único que le falta es el consumidor, y eso no se nota leyendo
  la prueba — se nota comparando dos imágenes.

**Nota hermana, misma enfermedad desde otro ángulo: la prueba que repite la
suposición del código.**

En la misma pasada, el texto salía como *"Aquí cobra 30 cobradores a 381
clientes"* — sin concordancia. Estaba **verde**, porque la prueba afirmaba
`assertEquals("Aquí cobra 30 cobradores a 381 clientes", …)`: quien escribió el
código y quien escribió el aserto cometieron **el mismo error, copiado**. El
aserto no fijaba la propiedad (*el verbo concuerda con el número*), sino que
repetía la salida observada.

Se encontró **mirando el PNG**. El arreglo trae una prueba que fija la propiedad
en los dos extremos (singular con un cobrador, plural con varios) en vez de
clavar una cadena.

**Nota 2 — la misma enfermedad con el consumidor siendo una COMPUERTA.**

El 2026-09-24 se reportó *"el árbol está verde"* tras verificar los módulos
propios (`:core:geo`, `:feature:ubicacion`) y las pruebas de navegación. El hook
de pre-commit corre otra cosa: `ktlintCheck` **en la raíz** y
`testDevlocalDebugUnitTest` **en la raíz** — 1,723 pruebas. Ahí había **cinco
rojas**, una de ellas propia (un control tocable de 75 px contra el mínimo de
100 del repo). El commit de otro agente quedó bloqueado, con 98 archivos
esperando.

El error no fue no correr las pruebas: fue **correr las que yo elegí en vez de
las que cobran**. Verificar al productor —mis módulos— y no al consumidor —la
compuerta real—. Desde entonces la comprobación es leer `.git/hooks/pre-commit`
y ejecutar sus pasos tal cual.

**Nota 3 — una regla escrita que ninguna prueba cobra es una regla que no
existe.**

El KDoc de `MSP_SOFT_EDGE_FADE` documentaba haber **abandonado** el criterio
blando (el saldo arriba del *punto opaco*, el 44 % de la banda) por uno duro (el
saldo arriba de donde la banda **empieza**), y explicaba por qué: con el blando,
un golden mostraba el `$3,550` gris y desenfocado.

**Las tres afirmaciones de `ElDineroNoSeMeteBajoLaBarraTest` seguían midiendo
contra el punto opaco.** Medido:

```
saldo.bottom = 568.0     banda.top = 568.0     puntoOpaco = 645.44
```

**77 dp de holgura falsa.** Con `FONDO_NORMAL` subido a **120 dp** —muy por
encima del presupuesto `M + fade ≤ 162`— las tres seguían **verdes**. La regla
dura estaba escrita en la documentación y **no la cobraba nadie**.

Es la variante más cara de todas las de esta familia, porque el KDoc **da
confianza**: quien lo lee cree que la propiedad está garantizada, y lo que está
garantizado es otra cosa más floja. Guardada ahora por
`ElDineroNoSeMeteBajoLaBarraTest::el SALDO TOTAL queda arriba de donde la banda
EMPIEZA`, que se pone roja con **un solo dp** de más.

- **Guardada por:**
  `msp-app-kt/feature/ubicacion/src/test/.../ui/LaHojaDiceLasDosCuentasTest.kt`
  (monta la hoja con `createComposeRule` y lee el texto; revertir el arreglo
  pone **3 rojas**) y
  `.../domain/RotulosTest.kt::el verbo concuerda con el numero de cobradores`.
- **Caduca si:** — (es una lección de método; no caduca con una versión)

### E-INF-012 · Un contador de píxeles sobre goldens da una respuesta limpia y falsa en tema oscuro

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-09-24, `msp-app-kt`, goldens de `:feature:pagos`
- **Qué se quería medir:** si al bajar `FONDO_NORMAL` de 118 a 82 dp la calle del
  fondo del detalle de cliente quedaba cortada. Se escribió un contador sobre el
  PNG: recorrer las filas de arriba y quedarse con las que tienen **píxeles
  oscuros** (`< 120` de luminancia), o sea "tinta".
- **Lo que devolvió:**

  ```
  light_1_0   antes= 17.5dp   ahora=  4.0dp   -> CORTADA
  dark_1_0    antes= 70.0dp   ahora= 70.0dp   -> ok        <-- FALSO
  light_2_0   antes= 16.5dp   ahora= 16.5dp   -> ok
  ```

  La conclusión que se leía sola: *"sólo afecta al tema claro"*. **Es falsa.** Al
  abrir `pagos_cliente_dark_1_0.png` la calle estaba **igual de partida** que en
  claro.
- **El mecanismo:** en tema oscuro **el fondo es oscuro y la tinta es clara**. El
  criterio `< 120` no contaba glifos: contaba **el fondo**, así que toda fila
  daba positivo y el "bloque de texto" medía 70 dp — el alto de la franja
  entera— idéntico antes y después. Una métrica que mide lo contrario de lo que
  cree devuelve un número **estable**, y la estabilidad se lee como ausencia de
  cambio.
- **Lo que lo cazó:** abrir el PNG. Nada más.
- **El arreglo, para quien vuelva a escribir uno:** el criterio de "tinta"
  **depende del tema**. `< 120` en claro, `> 140` en oscuro. Con eso, las cuatro
  escalas dieron lo mismo que la versión commiteada (17.5 / 17.5 / 16.5 / 16.5) y
  el número volvió a significar algo.
- **Por qué se registra, y no como anécdota:** contar píxeles sobre un golden es
  la forma **correcta** de convertir "se ve cortado" en un número, y se va a
  volver a hacer. El riesgo no es que falle ruidosamente: es que devuelva una
  cifra redonda y plausible. **Un contador de píxeles se valida contra un caso
  donde se sabe la respuesta antes de creerle** — aquí habría bastado correrlo
  sobre el golden commiteado en oscuro y notar que 70 dp no es el alto de una
  línea de texto.
- **Parentesco:** es la familia de `E-INF-011` desde el otro lado. Allá una regla
  cierta no tenía quien la cobrara; aquí **la medición sí existe y miente**.
- **Guardada por:** — (es una lección de método sobre análisis de imágenes; no
  hay prueba automática que la fije)
- **Caduca si:** los goldens dejan de tener variante oscura, o se adopta una
  utilidad compartida de análisis que resuelva el umbral por tema
