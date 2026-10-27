# Registro de evidencia

> **Estado: ADOPTADO (2026-09-23).** El dueño lo aprobó. La regla vive en
> `CLAUDE.md` §8 ("Nivel de evidencia obligatorio en todo cambio que toque
> dinero") y el registro en `docs/evidencia/`, sembrado el mismo día con las
> afirmaciones `E-COB-001…012`, `E-APP-001…009` y `E-INF-001…006`.
>
> **Segunda y tercera tandas.** El 2026-09-23 (tarde) entraron
> `E-COB-013…016` y `E-APP-010…028`. El **2026-09-24** entraron
> `E-APP-029…047` y `E-INF-007…008` —la primera tanda con **mediciones en un
> aparato**— y, sobre todo, **se corrigieron cuatro fichas ya sembradas**
> (`E-APP-010`, `E-APP-020`, `E-APP-022`, `E-APP-024`) y se **enlazó** la
> contradicción entre `E-APP-022` y `E-COB-014`, que convivían sin que ninguna
> lo reconociera. Ninguna se borró: cada corrección deja escrito **qué se creyó
> y por qué parecía razonable**, que es la regla de la §4.4. Es la primera vez
> que el registro se corrige a sí mismo, y es lo que había que probar que
> funciona.
>
> El texto de abajo se conserva **tal cual se escribió como propuesta**: es el
> razonamiento que la sostiene, incluidos los tres errores del 2026-09-23 que la
> motivaron. Las preguntas de la §7 ya están contestadas: se adopta, vive en
> `msp-api/docs/evidencia/` (con un puntero en `msp-app-kt/docs/evidencia.md`),
> y sí se sembró.

## 1. El problema, nombrado con precisión

No es que los agentes se equivoquen. Es que **una afirmación equivocada, una vez
escrita, se vuelve indistinguible de una verificada** — y desde ahí todos la
tratan como hecho.

Tres casos del 2026-09-23, todos el mismo día:

1. **Un comentario del código afirmaba algo falso.** `reconcile.go:411-414` dice
   que el recompute *"actualiza `UPDATED_AT` cuando la fila cambia"*. La
   proc lo actualiza **siempre**. Ese comentario justificó el diseño durante
   meses y nadie lo verificó porque parecía ya verificado.
2. **Un agente escaló una inferencia como si fuera casi certeza** (que la lápida
   huérfana se volteaba a `'N'`, con pérdida de datos). Al medirla resultó
   **falsa**: la proc se estrella antes de escribir.
3. **Yo afirmé que el paso de saldos comparaba antes de escribir.** No compara.
   Lo leí rápido y lo di por hecho; dos agentes independientes me corrigieron.

Y el síntoma que el dueño describe con sus palabras:

> *"me pasa mucho que toco lo de pagos y se supone que está bien y luego salen
> más cosas"*

La causa raíz es la misma en los cuatro casos: **las afirmaciones circulan sin
nivel de evidencia y sin dueño.** No hay forma de preguntarle a una frase
*"¿quién midió esto, cuándo, y con qué?"*.

## 2. Qué ya existe, y por qué no alcanza

El proyecto **ya tiene la forma correcta** en un lugar: `docs/COBRANZA-SYNC.md`.
Sus secciones traen *"Invariante duro"*, *"Por qué (incidente real)"*, *"Pruebas
que lo guardan"* y un catálogo de incidentes. Esa estructura funciona — es lo
que hizo que el arbitraje de hoy encontrara la contradicción.

Lo que le falta, y es justo lo que nos mordió:

- **No dice el nivel de evidencia de cada afirmación.** Una frase medida y una
  supuesta se ven exactamente igual.
- **No dice cuándo ni dónde se midió.** Una cifra de la base de dev y una de
  producción se leen igual, y no valen lo mismo.
- **No hay compuerta.** Nada obliga a verificar antes de cambiar, ni a
  comprobar después que el cambio hizo eso y **sólo** eso.
- **Lo refutado se borra.** Y entonces alguien vuelve a deducirlo en tres meses.

Los otros dos mecanismos cubren cosas distintas y se quedan como están:
`docs/adr/` guarda **decisiones**; `MEMORY.md` guarda **punteros entre
sesiones**. Lo que falta es un lugar para **hechos y su prueba**.

## 3. Lo que encontré investigando

La literatura de 2026 sobre agentes convergió en dos ideas que aplican directo:

**Libro mayor de evidencia con relación explícita.** Cada afirmación entra
acompañada de su paquete de evidencia, y un adjudicador le asigna **una** de
cuatro relaciones —*sostiene, contradice, falta evidencia, evidencia mixta*— y
emite una ruta de revisión. La ruta se activa para las tres últimas y se apaga
sólo para *sostiene*. Lo medido en ese trabajo: la condición con agente
adjudicador llegó a 0.676 de exactitud contra 0.383 de la mejor línea base sin
agente. El planteamiento del problema es literalmente el nuestro: *los agentes
redactan afirmaciones más rápido de lo que se puede comprobar si la evidencia
citada las sostiene*.

**Registros de decisión legibles por máquina, inmutables y numerados.** El patrón
`.agents/decisions/NNNN-titulo.md` con encabezado YAML (`number`, `title`,
`date`, `status`, `deciders`, `area`, `supersedes`, `superseded_by`) y la regla
de que **un cambio de criterio no reescribe el registro anterior: lo sustituye**.
Los agentes lo consultan *just-in-time*, cuando encuentran ambigüedad.

Y una tercera, de la línea de provenance: lo que hay que registrar no es sólo
*qué* se concluyó, sino **la relación de soporte entre la evidencia y la
conclusión** — porque una fuente puede ser relevante sin sostener la afirmación
concreta que la cita.

Fuentes al final.

## 4. El diseño

### 4.1 Los cuatro niveles de evidencia

Toda afirmación lleva uno. Es lo único verdaderamente obligatorio de esta
propuesta; lo demás es andamio.

| Nivel | Qué significa | Qué hay que enseñar |
|---|---|---|
| **MEDIDO** | Se ejecutó y hay salida | La salida pegada, la consulta o comando, la fecha, **dónde** (dev / producción / prueba) y un **control positivo** |
| **LEÍDO** | Está escrito en el código | `archivo:línea`. No se ejecutó |
| **INFERIDO** | Se deduce de lo leído | De qué se deduce, y **qué lo volvería MEDIDO** |
| **SUPUESTO** | Nadie lo revisó | Nada. Es una creencia, y se marca como tal |

**La regla dura, y es la que habría parado los tres errores de hoy:**

> Ningún cambio que toque **dinero** —pagos, saldos, ventas, cobranza— puede
> apoyarse en una afirmación por debajo de **MEDIDO**. Si la afirmación está en
> INFERIDO, primero se mide; si no se puede medir, no se cambia.

Corolario que ya cobró factura: **una ausencia no es un hallazgo** hasta enseñar
un control positivo que demuestre que la búsqueda habría encontrado el caso.

### 4.2 Dónde vive

`docs/evidencia/<area>.md`, un archivo por área: `cobranza.md`, `ventas.md`,
`visitas.md`, `app-android.md`, `infra.md`. Dentro, afirmaciones numeradas que
**nunca se renumeran**.

Identificador: **`E-COB-014`** — área y número correlativo. Es lo que hace que
esto escale: el identificador se cita desde el **comentario del código**, el
**nombre de la prueba** y el **mensaje del commit**, así que se llega a la
evidencia *desde el código*, sin leer el documento entero.

### 4.3 La forma de una afirmación

```markdown
### E-COB-014 · El reconciliador reescribe UPDATED_AT aunque nada cambie

- **Nivel:** MEDIDO
- **Estado:** VIGENTE
- **Medido:** 2026-09-23, base de **dev** (`mueblera-firebird`)
- **Cómo:** comparación fila a fila entre los respaldos del 15-ago y el 2-sep,
  todas las columnas menos `UPDATED_AT` y `TX_ID`.
  **2,173,269 filas comunes, 0 con cambio de valor.**
- **Control positivo:** mutando 3 filas del volcado, el comparador reporta 3.
- **Vive en:** `internal/cobranza/app/reconcile.go:415-444`,
  `migrations-firebird/000023_recompute_changelog.up.sql:147`
- **Guardada por:** — (sin prueba todavía)
- **Contradice:** el comentario de `reconcile.go:411-414`
- **Caduca si:** cambia la definición de `MSP_RECOMPUTE_PAGO`
```

Campos que hacen el trabajo:

- **Contradice** — apunta al comentario, doc o afirmación que quedó desmentida.
  Sin esto, la mentira sigue viva en su sitio original.
- **Guardada por** — la prueba que la fija. Una afirmación sin prueba es una
  afirmación que va a volver a romperse.
- **Caduca si** — la condición que la vuelve sospechosa. Es lo que permite
  detectar conocimiento rancio sin releerlo todo.

### 4.4 Lo refutado NO se borra

Cuando algo se desmiente, se marca **REFUTADA** con la fecha y cómo, y **se
queda**. Hoy refutamos tres cosas; si se borran, alguien las vuelve a deducir en
tres meses y el ciclo se repite.

```markdown
### E-COB-019 · La lápida huérfana se voltea a CARGO_CANCELADO='N'

- **Nivel:** MEDIDO  ·  **Estado:** REFUTADA (2026-09-23)
- **Se creyó cierta** porque el `IF (v_cancelado = 'S')` da UNKNOWN con NULL y
  la ejecución sí cae al upsert normal. La lectura del PSQL era correcta.
- **Por qué es falsa:** ese upsert **no puede completarse** — `CLIENTE_ID` y
  `FECHA_CARGO` también quedan NULL y son `NOT NULL`. Lanza `isc_not_valid`, el
  `WHEN ANY DO` se lo traga, y es un no-op.
- **Corroboración de campo:** `MSP_SALDOS_ERRORS` tiene 3,915 filas, 100% del
  mismo error, sobre 2,798 cargos, todos sin fila en `DOCTOS_CC`.
- **Cuidado:** la protección es **accidental**, depende de dos `NOT NULL`.
  Relajar el esquema o "arreglar" la proc con un `COALESCE` la reactiva.
```

Ese último renglón es el que vale oro: la afirmación es falsa **hoy**, y dice
exactamente qué la volvería cierta.

### 4.5 Las dos compuertas

**Antes del cambio — verificación independiente.**

Un agente que **no sabe la conclusión** vuelve a derivar cada afirmación en la
que se apoya el cambio. Recibe la pregunta, no la respuesta. Devuelve, por cada
afirmación, **una** relación:

| Relación | Qué significa | ¿Bloquea? |
|---|---|---|
| **SOSTIENE** | La evidencia establece la afirmación | No |
| **CONTRADICE** | La evidencia la desmiente | **Sí** |
| **FALTA EVIDENCIA** | El paquete no la establece | **Sí** |
| **PARCIAL** | Sostiene una parte y desmiente otra | **Sí** |

Cualquier cosa que no sea SOSTIENE **detiene el cambio** y vuelve al autor. No
es consejo: es compuerta.

**Después del cambio — verificación de alcance.**

Otro agente independiente comprueba tres cosas, y la segunda es la que ataca el
*"luego salen más cosas"*:

1. **Hace lo que dijo.** La prueba que lo guarda existe y **se pone roja al
   revertir el arreglo**. Sin esa comprobación, un verde no significa nada.
2. **No hace nada más.** Enumerar —no adivinar— **todos** los consumidores de lo
   que se tocó: quién más lee esa columna, esa función, ese endpoint. Cada uno,
   revisado. Éste es el paso que hoy no existe y es exactamente por donde
   aparecen las sorpresas.
3. **Lo que quedó sin verificar**, dicho en voz alta. "No medido" es una
   respuesta válida; inventarlo no.

**Por qué los agentes tienen que ser independientes y no el mismo:** hoy los dos
agentes del reconciliador se **contradijeron**, y esa contradicción fue lo que
produjo la verdad. Un agente que valida su propio trabajo reproduce su propio
punto ciego — es lo que me pasó a mí tres veces en un día.

### 4.6 Cómo se cierra el círculo con el código

El identificador se cita desde donde vive la trampa:

```go
// Escribimos sólo si algo cambió: la proc mueve UPDATED_AT siempre
// y UPDATED_AT es el cursor del sync. Ver E-COB-014.
```

```kotlin
@Test fun `no borra por ausencia si la lista del servidor vino vacia`() // E-APP-007
```

```
fix(cobranza): comparar antes de escribir en el barrido de pagos

Cierra E-COB-014. Verificada antes por [pre-flight] y después por [alcance].
```

Así, quien lea ese comentario dentro de seis meses **puede preguntarle a la
frase quién la midió**. Que es precisamente lo que no se pudo hacer con el
comentario de `reconcile.go:411-414`.

## 5. Qué cuesta, y por qué escala

- **Escribir una afirmación**: unas diez líneas, y el agente que la midió ya
  tiene todo el material. El costo real es cero; hoy esa información se produce
  igual y **se tira** al terminar la sesión.
- **La compuerta previa**: un agente más por cambio que toque dinero. Hoy ya
  lanzamos tres o cinco por investigación; esto es uno, con un encargo estrecho.
- **La compuerta posterior**: otro agente, con la lista de consumidores.
- **Leer**: nadie lee el archivo completo. Se llega por el identificador desde el
  código, o por `grep` del área. Por eso un archivo por área aguanta cientos de
  afirmaciones sin volverse ilegible.
- **Numeración**: correlativa por área y **nunca se reutiliza**. Dos ramas que
  agregan afirmaciones a la vez chocan en el número; se resuelve como un
  conflicto normal, renumerando **la que entra después** y corrigiendo sus citas.

## 6. Lo que deliberadamente NO hace

- **No sustituye a las pruebas.** Una afirmación sin prueba sigue siendo frágil;
  el campo *Guardada por* existe para que eso se vea.
- **No sustituye a `docs/COBRANZA-SYNC.md`.** Ése es el contrato narrativo de un
  área delicada. El registro es la evidencia detrás. Las secciones de ese
  documento pueden ganar identificadores y apuntar al registro.
- **No sustituye a los ADR.** Decisiones allá, hechos aquí.
- **No pide ceremonia para todo.** Un cambio de color en una pantalla no lleva
  compuerta. La regla dura aplica a **dinero**: pagos, saldos, ventas, cobranza,
  y al sync que los mueve.

## 7. Lo que hay que decidir

1. **¿Se adopta?** Si sí, entra una línea en `CLAUDE.md` que lo haga obligatorio
   para cambios de dinero — sin eso es papel.
2. **¿Dónde vive el registro?** Propongo `msp-api/docs/evidencia/`, porque ahí
   está el `CLAUDE.md` que gobierna; pero las afirmaciones del cliente Android
   son del otro repo. La alternativa es un archivo en cada uno.
3. **¿Se siembra con lo de hoy?** Hay material para arrancar con unas veinte
   afirmaciones ya medidas, tres refutadas y varias inferidas pendientes de
   medición. Sembrarlo es lo que lo vuelve útil desde el primer día.

---

## Fuentes

- [Evidence-Ledger Adjudication for Claim-Evidence Traceability](https://arxiv.org/abs/2607.26512)
- [From Agent Traces to Trust: A Survey of Evidence Tracing and Execution Provenance in LLM Agents](https://arxiv.org/html/2606.04990)
- [From Traceability to Justifiability: Accountability Structures in Agentic Software Engineering](https://arxiv.org/pdf/2608.23610)
- [chore(governance): adopt .agents/decisions for AI agent architectural and policy precedents](https://github.com/PAIR-code/deliberate-lab/issues/1243)
- [Reproducibility in the Age of Agentic AI: Context Engineering at the Timescale of a Codebase](https://arxiv.org/pdf/2609.11728)
- [Architecture decision record (ADR) examples and templates](https://github.com/architecture-decision-record/architecture-decision-record)
