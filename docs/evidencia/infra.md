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

**Siguiente identificador libre: `E-INF-017`.**

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

---

### E-INF-013 · Ninguna sentencia sin `WithStatementTimeout` corre con el tope corto

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-10-14 ~23:14 hora de México (2026-10-15 05:14 UTC), **dev**
  (`mueblera-firebird`, imagen `jacobalberty/firebird:v4.0`), rama
  `perf/desempeno-robustez`. **No medido en producción (Firebird 5.0).**
- **Contexto.** El driver ignora la cancelación del contexto a propósito (la
  cancelación envenenaba el pool), así que un 503 a los 30 s dejaba la
  sentencia corriendo en Firebird (se midió un 503 de 78 s en producción).
  `firebird.WithStatementTimeout(ctx, d)` lo arregla con `SET STATEMENT
  TIMEOUT d` **por sesión**: `nakagami/firebirdsql v0.9.19` manda el tope por
  sentencia del protocolo 16 fijo en 0 (`wireprotocol.go:1163-1166`,
  `1193-1196`, LEÍDO). Ser por sesión es justo el riesgo: que una sentencia de
  **otro módulo** (el sync de teléfonos, una escritura de cobranza) herede los
  25 s del tablero. Esta entrada es la evidencia de que no pasa.
- **Afirmación:** una sentencia emitida sin la opción corre con el tope del
  proceso (`FB_STATEMENT_TIMEOUT`), y lo que el mecanismo hace en la sesión no
  altera ni cursores abiertos ni la transacción en curso.
- **Mediciones** (`internal/platform/firebird/statement_timeout_e_inf_013_integration_test.go`
  y `statement_timeout_integration_test.go`; `go test -count=1 -run
  'TestE_INF_013|TestWithStatementTimeout_Firebird' -v ./internal/platform/firebird/`
  con `.env` de dev):
  1. **Un `ExecContext` sin la opción restaura, visto desde otra conexión.**
     Conexión fija `c1` (attachment 44474); tras una lectura con tope de 2 s,
     otra conexión lee `MON$ATTACHMENTS.MON$STATEMENT_TIMEOUT` de ese
     attachment: **2000 ms** (control positivo: la consulta sí ve el tope
     corto). Tras `c1.ExecContext(EXECUTE BLOCK AS BEGIN END)` sin opción:
     **600000 ms** = el tope del proceso.
  2. **Restaurar no alarga un cursor abierto con 2 s.** En una transacción, un
     cursor abierto con tope de 2 s; en la misma transacción, un `ExecContext`
     sin opción (manda `SET ... 600`) con el cursor abierto. El cursor **murió
     a los 4.48 s** con *"operation was cancelled / Attachment level timeout
     expired"*: el tope que vale es el de cuando se **ejecutó**. Los 2.48 s de
     más son, INFERIDO, el lote de filas que el driver ya tenía en memoria
     (la prueba duerme 5 ms por fila) antes del siguiente `op_fetch`; lo
     volvería MEDIDO registrar en qué fila llega el error.
  3. **Un SET posterior no acota un cursor ya abierto.** Cursor abierto con el
     tope del proceso; en la misma transacción una lectura con tope de 2 s
     (sesión en 2 s); el primer cursor siguió leyendo **5.0 s sin error**. El
     bucle de lectura es el mismo que en la medición 2 sí vio el aborto
     (control positivo).
  4. **El SET dentro de una transacción de escritura no la cambia.**
     `CURRENT_TRANSACTION` antes / tras `SET 2` / tras `SET 600` =
     **13848027 / 13848027 / 13848027**, y una variable `USER_TRANSACTION`
     puesta antes del primer SET sigue ahí (no hubo commit). Control positivo:
     una transacción nueva da otro número y la variable vacía.
  5. **El tope vencido a mitad del fetch se mapea a `firebird_timeout`**
     (`firebird.MapError`, misma ruta que el aborto en ejecución), así que el
     cliente recibe 503 y no un 500 con texto del driver.
- **Defensas en el código** (`driverwrap.go`, `cancelProofConn`):
  `ResetSession` restaura el tope al sacar la conexión del pool (si falla:
  `ErrBadConn`, que `database/sql` reintenta con otra; nunca falla la sentencia
  de otro dentro de su transacción). `syncStatementTimeout` vuelve a restaurar
  antes de cada sentencia (segunda defensa). Un SET fallido deja la sesión en
  estado **desconocido** y la siguiente sentencia manda su SET sí o sí; si
  falla también, la conexión queda condenada (`IsValid`/`ResetSession`).
- **Trampas encontradas al medir:**
  - Las tablas `MON$` son una foto que vive hasta que **termina** la
    transacción, y la transacción implícita del driver sólo hace commit
    retaining: una lectura simple del pool devolvió una foto vieja (vio la
    sentencia abortada "todavía corriendo" y el tope sin restaurar). Toda
    lectura de `MON$` va en una transacción propia.
  - El driver se registra **una vez por proceso** con el primer `firebird.New`;
    en el binario de pruebas de `platform/firebird` lo gana a veces una prueba
    con `StatementTimeout` 0. Comparar contra el `cfg` propio dio un falso rojo
    (esperaba 600000, el proceso tenía 0). Las pruebas comparan contra
    `RegisteredDefaultStatementTimeout()`.
- **Guardada por:** `TestE_INF_013_*` (integración) y
  `TestStatementTimeout_E_INF_013_*` (unitarias). Mutaciones rojas: `ExecContext`
  que sólo revisa `broken` sin restaurar; `ResetSession` que no restaura;
  override fallido sin marcar desconocido; estado desconocido que no fuerza el
  SET; desconocido + SET fallido que no condena.
- **Caduca si:** el driver empieza a mandar el tope por sentencia del protocolo
  (entonces se usa ése y no hay estado de sesión), se cambia de driver, o
  producción mide distinto en Firebird 5.0 — lo que la volvería MEDIDA allá es
  correr estas mismas pruebas contra una copia con 5.0.

### E-INF-014 · El tablero de desempeño leído por lote da EXACTAMENTE lo mismo que zona por zona, con 6 sentencias en vez de 220

- **Nivel:** MEDIDO · **Estado:** VIGENTE
- **Medido:** 2026-10-14/15, base de **desarrollo** (`mueblera-firebird`, datos
  reales hasta feb-2026), reloj fijo 18-feb-2026 20:00 CDMX. Rama
  `perf/desempeno-lote`. **Ojo:** a mitad de la medición otro carril creó en la
  misma base `IDX_MSP_VISITAS_ZONA_FECHA`, `IDX_MSP_PAGOS_REC_IMPTE_ID` e
  `IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID`; las cifras de antes y después de abajo se
  tomaron una tras otra con esos índices ya presentes. Producción no se tocó.
- **Antes / después** (`TestMedirTablero`, 55 usuarios sintéticos en 44 zonas,
  semana de 6 días; binario de `873a81c` contra esta rama, 00:28–00:31):

  | | sentencias | tiempo por petición |
  |---|---|---|
  | `873a81c` (por zona, 4 filas en paralelo) | 220 (44 × 5) | 7.9 – 9.0 s |
  | por lote, 3 lecturas en paralelo | 6 | 1.6 – 1.9 s |
  | por lote, con UN usuario de semana de 30 días | 6 | 1.8 – 2.7 s |

  Por sentencia (44 zonas): aplicados 0.8 s, visitas 0.23 s, pendientes 0.01 s,
  universo 1.1 s, cobro de rutas ≈ 1.3 s. Las tres lecturas en serie suman
  3.1 s; a la vez (tres conexiones) 1.6 s — Firebird de dev **no** serializa.
- **Equivalencia** (`TestEquivalenciaDesempeno`): 48 zonas reales, 173
  usuarios (uid real de app, COBRADOR_ID compartido, inicios de hace 6 días,
  hace 2 días, HOY 10:00 —la consulta aparte de "hoy"—, hace 30 días, y tres
  sin configurar), 791 casos (tablero hoy y semana, jornada de hoy y de una
  fecha pasada, cuentas con y sin filtro, actividad paginada completa en los
  cinco órdenes y seis filtros): **2,927,700 líneas de JSON, 0 diferencias**
  contra el código anterior con el desempate de abajo. Rutas
  (`TestCobranzaRepo_VentasPorZonas_EquivaleZonaPorZona`): 192 consultas (48
  zonas × 4 inicios) × 2 cortes, 45,860 filas idénticas campo por campo, 23,172
  con abono en la ventana y 1,436 de ellas **saldadas** (la rama que sale de
  los pagos); `CobranzaDeUsuarios` = `CobranzaDeUsuario` =
  `DesglosePorUsuario` para 98 usuarios, 22,438 ventas.
- **Controles positivos:** quitar del universo las saldadas por fecha → el
  arnés falla (`VentasUniverso` 174 → 169); quitar el desempate → falla en 3
  casos; contar condonaciones en el abono de rutas → la equivalencia de rutas
  falla en la primera zona. Cada prueba unitaria nueva se puso roja con su
  mutación (truncado a 100 µs, hora de pared, tolerancia de grupo, regla
  saldo/abono, deduplicado).
- **Empates (hallazgo):** ninguna de las tres consultas de actividades tiene
  ORDER BY y la derivación resuelve empates por orden de entrada
  (`deriver.go` `masReciente`, visitas "la primera de la entrada"), así que
  quién "determina" una venta en un empate exacto dependía del plan. En la
  ventana de 30 días de dev: 1 grupo de 2 pagos de la misma venta a la misma
  hora, 52 grupos (104 visitas) del mismo cliente a la misma hora, 0
  pendientes. Desempate fijado: (hora, id), gana el id **menor en orden
  lexicográfico** del ID completo (`visita:<uuid>`, `pago:<n>`), el mismo
  criterio que ya usan `domain.OrdenarActividades` y el cursor de la
  actividad; no se hace numérico porque los ids de visitas y pendientes son
  UUID. **CORRECCIÓN (compuerta 2):** la versión anterior de esta entrada
  decía "una venta"; son **tres**. Contra la salida del código anterior **sin**
  desempate cambian `DeterminadoPor`, `UltimaActividadID` y
  `ultima_actividad.id` de las ventas 13914738, 14394018 y 14719574 (zona
  2990693) en `cuentas/equiv-a` (las 3), `equiv-b` (las 3) y `equiv-c` (1,
  por su inicio de hoy). Ningún caso de tablero, jornada ni actividad cambia;
  estado, etiqueta, segmento y hora de última actividad son iguales. En los
  tres casos son dos visitas **duplicadas**: mismo cliente, misma venta, mismo
  segundo, mismo cobrador (2991009), "No se encontraba", sin nota ni fotos
  (consultado en `MSP_VISITAS` el 2026-10-15); solo cambia cuál de los dos IDs
  se muestra.
- **Trampa medida:** un OR por zona en el WHERE (`(zona = ? AND fecha >= ?) OR
  …`) para no ensanchar la ventana hace que Firebird deje de usar el rango de
  fechas del índice: con un usuario de 30 días, aplicados 0.8 → 15.9 s y cobro
  de rutas 29 s. Con una rama `UNION ALL` por grupo de zonas (inicios a menos
  de 24 h entre sí) vuelve a 0.9 s.
- **Límite de 256 contextos (compuerta 2, MEDIDO en dev FB 4.0.2):** cada
  rama `UNION ALL` repite los contextos de su consulta; con 16 o más grupos de
  inicio los aplicados fallaban con `Too many Contexts of
  Relation/Procedure/Views. Maximum allowed is 256` y el tablero entero daba
  500. Arreglo: sentencias de a lo más `ramasPorSentencia` ramas (mitad del
  límite / contextos de la rama). Prueba con 48 grupos (un inicio por zona a
  25 h del anterior más un segundo inicio escalonado: 0, 5 h, 23:59, 24 h,
  24 h + 150 µs, 30 h, 47 h, 49 h, 3 d, 5 d 7 h): roja antes con ese error,
  verde después, 58,417 actividades, 23,538 cuentas y 23,409 filas de rutas
  idénticas a la lectura consulta por consulta. Tablero en el peor caso (55
  usuarios, cada uno con su inicio a 25 h del anterior): 22 sentencias,
  **6.0–10.3 s** (base compartida con otros carriles, de ahí el rango). Correr
  las sentencias de actividades a la vez se midió peor: 10.9–11.2 s.
- **Lo que NO se midió:** producción (índices distintos: dev tiene
  `IDX_MSP_PAGOS_RECIBIDOS_DOCTO_CC`, producción `IDX_PAGOS_REC_DOCTO_CC` e
  `IDX_MSP_PAGOS_DOCTO_CC_ID`; no se fijó ningún `PLAN`); la base sin los
  índices del otro carril (se crearon durante la medición); el tablero con
  usuarios reales de Firestore.
- **Guardada por:** `TestEquivalenciaDesempeno` (con `EQUIV_GRABAR` /
  `EQUIV_COMPARAR`, grabación fuera del repo), las dos pruebas de equivalencia
  de rutas, `lote_internal_test.go`, `cobranza_lote_internal_test.go` y
  `TestTablero_PorLoteUnaLecturaDeCadaTipoYMismoResultado`.
- **Caduca si:** cambian las consultas por zona de desempeño o
  `queryVentasPorZona` de rutas sin volver a correr el arnés.

### E-INF-015 · Con la migración 000065 las subconsultas `MIN(ID)` sobre `MSP_PAGOS_RECIBIDOS` navegan el índice compuesto y no la PK, con resultados idénticos

- **Nivel:** MEDIDO (dev) · INFERIDO (producción) · **Estado:** VIGENTE
- **Nota de numeración:** `E-INF-013` aparece reclamado a la vez en las ramas
  `perf/desempeno-lote` y `perf/desempeno-robustez`, y `E-INF-014` sería el
  siguiente de una de ellas; este registro toma `E-INF-015` para no chocar. Si
  al fusionar queda un hueco, el hueco se queda (los números no se reutilizan).
- **Medido:** 2026-10-15, base de dev (`mueblera-firebird`, Firebird **4.0.2**,
  `MON$PAGE_BUFFERS` = 2048 × 8 KB = 16 MB, `DefaultDbCachePages` sin fijar),
  después del respaldo `data/firebird-snapshots/antes_idx_desempeno_20261014.fbk`.
- **El defecto (antes de 000065):** `queryPagosAplicados`
  (`internal/desempeno/infra/desempenofb/actividades_repo.go`) y
  `pagoRecibidoDelAbono` (`internal/clientes/infra/clientesfb/queries.go`) se
  resolvían con `PLAN (PR2 ORDER PK_MSP_PAGOS_RECIBIDOS INDEX
  (IDX_MSP_PAGOS_RECIB_IMPTE))` y `PLAN (PR3 ORDER PK_MSP_PAGOS_RECIBIDOS INDEX
  (IDX_MSP_PAGOS_RECIBIDOS_DOCTO_CC))`: recorrer la PK en orden hasta el primer
  ID que pase el filtro, y entera cuando no hay ninguno.
- **Recalcular sólo las estadísticas NO lo arregla** (medido): con
  `IDX_MSP_PAGOS_RECIB_IMPTE` en 0.05 (antes 0.333) y el índice de
  `DOCTO_CC_ID` en 2.9e-6, el plan de `PR2`/`PR3` siguió en `ORDER
  PK_MSP_PAGOS_RECIBIDOS`. Sí arregla pendientes (abajo).
- **Después de 000065:** `PLAN (PR2 ORDER IDX_MSP_PAGOS_REC_IMPTE_ID)` y
  `PLAN (PR3 ORDER IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID)` — navegación acotada por
  el prefijo de igualdad, incluido `IMPTE_DOCTO_CC_ID IS NULL` como segundo
  segmento de `(DOCTO_CC_ID, IMPTE_DOCTO_CC_ID, ID)`. Visitas pasa a
  `V INDEX (IDX_MSP_VISITAS_ZONA_FECHA)`; pendientes, con estadísticas al día, a
  `PR INDEX (IDX_MSP_PAGOS_RECIBIDOS, IDX_MSP_PAGOS_REC_ESTADO)`.
- **Cifras, base quieta (sin otras sentencias activas), A/B intercalado; el
  "antes" es el mismo texto con el plan viejo forzado por `PLAN` en la
  subconsulta (los índices viejos siguen existiendo):**

  | Consulta (zona, ventana) | Filas | Antes: s / fetches / reads | Después: s / fetches / reads |
  |---|---|---|---|
  | aplicados 21552, 13–19 feb 2026 | 206 | 1.50–1.57 / 916,599 / 496,896 | 0.039–0.044 / 21,590 / 1,347 |
  | aplicados 21552, 92 días | 2,820 | 24.4 / 13,918,297 / 8,356,971 | 0.36–0.40 / 232,439 / 9,515 |
  | pendientes 21563, desde 20 nov 2025 | 0 | 0.53–0.55 / 952,220 / 4,841 | 0.010 / 4,189 / 372 |
  | visitas 21557, 92 días | 3,919 | 0.34–0.37 / 170,083 | 0.22–0.24 / 116,433 |
  | visitas 21552, 7 días | 205 | 0.027–0.028 / 13,117 | 0.018–0.020 / 8,746 |
  | detalle de pago (lote), 7 días | 210 | 0.52 / 313,361 / 174,392 | 0.011 / 6,640 / 490 |
  | detalle de pago (lote), 92 días | 2,909 | 8.39 / 4,804,536 / 2,962,466 | 0.093 / 62,015 / 4,935 |

  Lecturas de registro (`MON$RECORD_IDX_READS`) de `MSP_PAGOS_RECIBIDOS` en
  pendientes: 84,873 → 2. De `MSP_VISITAS` en visitas 92 días: 55,881 → 3,919.
- **Trampa de método encontrada al medir:** con otro agente corriendo 4–9
  consultas pesadas en paralelo contra la misma base, **los fetches no son
  estables** (aplicados 92 días, mismo plan nuevo: 232 mil en quieto, 1.5 y 2.2
  millones con carga; el plan viejo llegó a 86.9 millones y 719 s). Sólo valen
  las cifras de la tabla, tomadas con `MON$STATEMENTS` vacío. Y en el script
  de producción la zona NO puede ir como subconsulta dentro de cada consulta:
  esa subconsulta sola cuesta 5.35 millones de fetches en dev.
- **Control positivo de equivalencia:** salida `SET LIST` completa de cada
  consulta (con `ORDER BY` añadido sólo para comparar) **idéntica byte a byte**
  antes de 000065, después, y con el plan viejo forzado, en las siete
  consultas; `cmp` sí distingue dos salidas distintas. Las salidas no son
  triviales: aplicados 7 días trae 199 de 206 filas con captura ligada; 92
  días, 2,729 de 2,820; detalle 92 días, 2,730 de 2,909. Como en dev casi todas
  las capturas ligan por `DOCTO_CC_ID` (sólo 19 claves no nulas de
  `IMPTE_DOCTO_CC_ID`), cada rama se probó además sobre la tabla entera contra
  una agregación por recorrido natural (`PLAN (... NATURAL)`): `PR2`, 19 de 19
  claves iguales; `PR3`, 348,012 `DOCTO_CC_ID` con captura iguales y 19 sin
  captura de ese tipo (las que sólo tienen capturas con importe). Pendientes
  da 0 filas en dev; su control es la misma condición sin la zona: 2 filas
  iguales con el plan viejo forzado y con el nuevo.
- **Qué lo volvería MEDIDO en producción:** correr
  `scripts/medicion/desempeno-indices-produccion.sql` antes y después de
  aplicar 000065 (Firebird **5.0**, con otro optimizador y otra distribución de
  `IMPTE_DOCTO_CC_ID`) y ver en `P4a`/`P4b`/`P5` las líneas `PLAN (PR2 ORDER
  IDX_MSP_PAGOS_REC_IMPTE_ID)` y `PLAN (PR3 ORDER IDX_MSP_PAGOS_REC_DOCTO_IMPTE_ID)`.
- **Caduca si:** cambia la forma de las subconsultas (`MIN` → otra cosa), se
  borran o renombran los índices de 000065, o producción muestra otro plan


### E-INF-016 · En el escritorio el abono parcial cuenta en "pagados", no en "volver" (divergencia deliberada con el teléfono)

- **Nivel:** MEDIDO (dev) · **Estado:** VIGENTE
- **Decisión del dueño, 2026-10-15:** en la pantalla "Desempeño de cobradores"
  del escritorio, una cuenta con trato `parcial` (`EstadoAbonoParcial`) cae en
  el segmento `pagados`. Su trato, etiqueta ("Abonó parcial") y detalle no
  cambian. El teléfono **no** cambia: `SegmentoDeCobranza.kt` de msp-app-kt
  —la fuente que porta `internal/desempeno/domain/presentacion.go`— sigue
  mandando el parcial a "volver". Desde hoy `SegmentoDe` y
  `SegmentoDeCobranza.contiene` ya no son idénticos en ese único caso.
- **Caso que la originó:** Noé Cortero, R/25, semana del 2026-10-15.
  Y00002299 (parcialidad 150) pagó 200 → pagó; Y00002364 (parcialidad 250)
  pagó 200 y Y00001500 (parcialidad 150) pagó 100 → abono parcial, mostrados
  en "volver". Con la regla nueva: pagados = 3, volver = 0.
- **Única fuente de la regla (LEÍDO 2026-10-15, enumerado con `grep -rn` de
  `SegmentoDe(`, `SegmentoVolver`, `SegmentoPagados` y `TratoParcial` sobre
  todo el `.go` del repo):** `domain.SegmentoDe` (`presentacion.go`). Sus dos
  consumidores, `app/cuentas.go` (`Cuentas` → `contarSegmento`, conteos y
  filtro `segmento`) y `app/tablero.go` (`llenarCobertura` →
  `cobertura_por_segmento`), cuentan el segmento que devuelve, sin copia propia
  de la regla. `infra/desempenohttp` sólo copia los conteos al DTO;
  `infra/desempenofb` no conoce segmentos. Fuera de `internal/desempeno` no hay
  ningún consumidor.
- **Prueba roja/verde (MEDIDO 2026-10-15, unitaria):**
  `TestCuentasYTablero_E_INF_016_AbonoParcialCuentaComoPagados` (caso de Noé:
  conteos de cuentas, filtro `volver` vacío, `cobertura_por_segmento` del
  tablero) y la fila "abono parcial" de
  `TestPresentacion_TratoEtiquetaDetalleSegmento`. Con `presentacion.go`
  regresado a `564bd74`: rojas (`Volver:2, Pagados:1` en vez de
  `Volver:0, Pagados:3`); con el cambio, verdes. También se pusieron rojas
  `TestTablero_Hoy_CifrasDeJuan`, `TestCuentas_EstadoPorVentaYConteos` y
  `TestCuentas_FiltrosYOrden`, que afirmaban parcial → volver y se
  actualizaron.
- **Arnés de equivalencia (MEDIDO 2026-10-15, base de dev, reloj 18-feb-2026
  20:00 CDMX):** `TestEquivalenciaDesempeno` grabado con `564bd74` (791 casos,
  48 zonas, 173 usuarios) y comparado con el cambio: 280 casos distintos
  (tablero hoy y semana, 139 `cuentas/`, 139 `cuentas-sin-nada/`); 0 casos de
  jornada ni de actividad. Comparación estructural completa (no sólo la primera
  línea que muestra el arnés): las únicas rutas JSON que cambian son
  `Cuentas[].Segmento` (3,624 veces, **todas** `volver → pagados` y **todas**
  de cuentas con trato `parcial`), `Conteos.Volver/Pagados` y
  `Filas[].PorSegmento.Volver/Pagados`; en los 280 grupos de conteo, `Volver`
  baja exactamente lo que sube `Pagados`, y en los 159 casos `cuentas/` sin
  error la baja de `Volver` es igual al número de parciales. Tablero semana,
  suma de la flota: volver 5,811 → 2,187, pagados 13,347 → 16,971. Control
  positivo: el mismo arnés sí detecta la diferencia (falla con 280 casos), y
  ninguna cuenta parcial quedó fuera de `pagados`.
- **La referencia regrabada** quedó fuera del repo (contiene nombres de
  clientes de dev), como manda el arnés; quien vuelva a correr la equivalencia
  debe grabar con el código de este cambio en adelante.
- **Lo que NO se midió:** producción; el caso de Noé con sus datos reales (dev
  llega a feb-2026; el caso se reprodujo con sus montos en la prueba unitaria);
  el escritorio pintando el segmento nuevo; si algún otro cliente del API (el
  teléfono no consume estos endpoints, INFERIDO de que es pantalla de
  escritorio) lee `segmento`.
- **Caduca si:** el dueño revierte la decisión, o el teléfono adopta la misma
  regla (entonces deja de ser divergencia y el comentario de `SegmentoDe` debe
  decirlo).
