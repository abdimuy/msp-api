# Desempeño de cobradores, carril E (Android): casos compartidos de estado por venta en Kotlin. Plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Que el juego de casos JSON de estado por venta que genera msp-api corra también contra `EstadoCuentaDeriver` de Kotlin, con un hash `VERSION` que demuestre que los dos repos prueban exactamente los mismos bytes.

**Architecture:** Dos cambios aditivos en el dominio puro de `:core:common` (campo `rechazado` en `PagoEnVentana` y la función de cobertura `CoberturaDelPeriodo.ventasConAlgo`, que comparte el índice de visitas con el deriver), y tres piezas de prueba: un ayudante que lista y hashea los casos, una prueba del `VERSION` y una prueba parametrizada (JUnit 4 `Parameterized`) que convierte cada JSON en entradas del deriver y compara contra `esperado`. Un script copia los casos desde `msp-api/testdata/estado-cuenta/` y detecta divergencia.

**Tech Stack:** Kotlin, JUnit 4.13.2 (`org.junit.runners.Parameterized`), Gson 2.8.9 (`libs.gson`, solo `testImplementation`), `java.security.MessageDigest`, Gradle (`./gradlew --no-parallel --no-daemon`), zsh/bash.

**Spec:** `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-14-desempeno-cobradores-design.md` (§2 "Estado por venta" y "Cobertura", §7 "Juego de casos compartido", §8 carril E) y `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-14-desempeno-contratos.md` (§1 `EstadoCuenta`, §7 formato de los casos).

**Repo de trabajo:** `/Volumes/M2-1TB/Developer/msp-app-kt`, rama `main` (rutas de abajo relativas a ese repo salvo que digan `msp-api/`).

## Global Constraints

- Gradle siempre `./gradlew --no-parallel --no-daemon …`; nunca dos Gradle a la vez (ni en dos terminales ni en dos agentes).
- Commits conventional en español (`test(cobranza): …`, `feat(cobranza): …`, `chore(scripts): …`), **sin** líneas de atribución; nunca `--no-verify`.
- Directorio de casos en Android: `core/common/src/test/resources/estado-cuenta/`; copia **byte a byte** de `msp-api/testdata/estado-cuenta/` (contratos §7).
- Un archivo por caso, `NNN-nombre.json`, más `VERSION` con el SHA-256 de la concatenación ordenada de los `.json` (contratos §7).
- Códigos de estado del contrato: `"pago" | "abono_parcial" | "visite_vuelvo" | "prometio_proxima" | "se_nego" | "cita_a_una_hora" | "no_estaba" | "sin_tocar"` (contratos §1).
- "El dinero manda: abono de la semana = suma de pagos con forma de cobro ∈ {157, 158, 52569}; condonación (137026) no cuenta como dinero; pagos rechazados (`DOCTO_CC_ID = -1`) no cuentan." (spec §2)
- "Cobertura: venta 'con algo' si en la semana tiene al menos un pago (de cualquier forma, incluido rechazado o condonación) o una visita que le aplique según la regla anterior." (spec §2)
- Dominio de `:core:common` puro: cero `android.*`, cero `Instant.now()`; dinero en `BigDecimal`, nunca `Double` (regla detekt `NoDoubleForMoney`); cobertura Kover del módulo ≥ 90 %.
- Diferencia teléfono/servidor (el teléfono solo ve sus visitas) **no** cambia el deriver: mismas entradas → mismo resultado.
- CLAUDE.md §8 de msp-api: el cambio de la Task 1 toca la derivación de dinero; cada afirmación nueva se registra con identificador `E-APP-NNN` en `msp-api/docs/evidencia/app-android.md` (último usado al escribir este plan: `E-APP-057`; verificar el siguiente libre al ejecutar) y se cita desde el KDoc, el nombre de la prueba y el commit.

## Dependencia dura (bloquea Tasks 3 y 4)

El plan del API (carril C1) genera `msp-api/testdata/estado-cuenta/*.json` y `VERSION`. **Al escribir este plan no existe** (`ls msp-api/testdata` → "No such file or directory", 2026-10-14) **ni existe aún el plan del API.** Tasks 1 y 2 no dependen de eso y pueden empezar ya. Tasks 3 y 4 empiezan cuando `ls /Volumes/M2-1TB/Developer/msp-api/testdata/estado-cuenta/VERSION` exista en la rama `main` de msp-api.

Lo que este plan **exige** del juego del API (si el plan del API decide otra cosa, se ajusta este plan antes de ejecutar, no durante):

1. `VERSION` = 64 caracteres hex **minúsculas**, seguidos opcionalmente de un `\n`. Se calcula sobre los bytes crudos de los `*.json`, ordenados por nombre de archivo en orden de bytes (`LC_ALL=C sort`), concatenados sin separador. Comando de referencia: `cd testdata/estado-cuenta && ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1`.
2. Nombres de archivo `^[0-9]{3}-[a-z0-9-]+\.json$` (sin espacios ni acentos, para que el orden sea el mismo en Go, Kotlin y shell). En el directorio no hay nada más que esos archivos y `VERSION`.
3. Dinero (`parcialidad`, `importe`) como **string** decimal; nunca número JSON.
4. Pago rechazado se representa con `"rechazado": true` en el pago (no con `docto_cc_id`; ver Task 1).
5. `msp-api/.gitattributes` con `testdata/estado-cuenta/** -text`, para que git no toque fines de línea.
6. Cobertura mínima del juego (la Task 4 la verifica): los 8 estados aparecen en algún `esperado`; aparecen `con_algo: true` y `con_algo: false`; hay al menos un pago `rechazado: true` y una condonación (forma 137026).

## Decisión sobre "rechazado" (reportar al dueño)

LEÍDO 2026-10-14: la regla de rechazado **vive en el adaptador, no en el deriver**. `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/data/adapter/RoomPagosAdapter.kt:101-110` (`aHistorial`) descarta las filas con `DOCTO_CC_ID == DOCTO_CC_ID_RECHAZADO_POR_EL_SERVIDOR` (`-1`, `core/database/.../entities/PaymentEntity.kt:67`) **y** las formas fuera de {157, 158, 52569} antes de llamar a `EstadoCuentaDeriver.derivar`. `PagoEnVentana` (`DerivacionModelos.kt:36`) no tiene cómo expresar "rechazado".

Decisión: el caso JSON lleva `"rechazado": bool`; `PagoEnVentana` gana `rechazado: Boolean = false` y el deriver lo excluye del abono (Task 1); la cobertura lo cuenta (Task 2). El adaptador **no se toca**: sigue filtrando, nunca pasa `rechazado = true`, así que el estado que ve el cobrador en el teléfono no cambia (INFERIDO de la lectura de `RoomPagosAdapter.kt:101-110`; lo volvería MEDIDO correr la suite de `:feature:pagos`, que la Task 4 corre dentro de `prePushCheck`). Alternativa descartada: filtrar los rechazados en el arnés de la prueba. Eso haría que Kotlin y Go no recibieran las mismas entradas y escondería la regla en código de prueba.

Consecuencia que el plan del API tiene que copiar: su port del deriver recibe `rechazado` y lo excluye del abono, igual que aquí.

## Review Focus

- **Dinero escrito como número JSON** (`"importe": 150.0`): debe fallar el caso con mensaje claro, no convertirse en silencio a `BigDecimal("150.0")` ni pasar por `Double`. Lo prueba la Task 4, `dinero como numero JSON se rechaza`.
- **Clave mal escrita en un caso** (`"fecha_cit"` en vez de `"fecha_cita"`): debe fallar nombrando la clave, no tratar el campo como `null` y dar verde por casualidad. Lo prueba la Task 4, `clave desconocida se rechaza`.
- **`esperado` que no cubre cada cuenta una vez** (le falta una venta o la repite): debe fallar el caso, no comparar solo la intersección. Lo prueba la Task 4, `esperado incompleto se rechaza`.
- **Directorio vacío o recursos fuera del classpath**: debe fallar, no dar un verde con cero casos. Lo prueba la Task 3, `el juego no esta vacio` (más el `requireNotNull` de `directorio()`).
- **Archivo intruso o mal nombrado** (`README.md`, `1-caso.json`) que cambia el orden o el hash entre Go, Kotlin y shell: debe fallar nombrándolo. Lo prueba la Task 3, `archivo fuera de formato se rechaza`.

---

### Task 1: `rechazado` en `PagoEnVentana`; el deriver no lo cuenta como dinero

**Files:**
- Modify: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/DerivacionModelos.kt:30-41` (`PagoEnVentana`)
- Modify: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt:134-141` (`abonosPorVenta`)
- Test: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriverTest.kt`

**Interfaces:**
- Consumes: nada nuevo.
- Produces: `data class PagoEnVentana(val ventaId: Int, val importe: BigDecimal, val formaCobroId: Int, val fechaHora: Instant, val rechazado: Boolean = false)`. El default `false` mantiene compilando a su único llamador de producción (`feature/pagos/.../application/DerivarEstadoDelPeriodo.kt:105`).

- [ ] **Step 1: Escribir las pruebas que fallan**

Agregar a `EstadoCuentaDeriverTest` (usa los ayudantes `pago(...)`, `cuenta(...)` y `estadoDe(...)` que ya existen en el archivo, con parcialidad default `"300"`). Sustituir `NNN` por el identificador de evidencia libre (ver Global Constraints):

```kotlin
    // ─────────────────────────────────────────────────────────────────────
    // PAGO RECHAZADO (DOCTO_CC_ID = -1): no es dinero — E-APP-NNN
    // ─────────────────────────────────────────────────────────────────────

    @Test
    fun `E-APP-NNN pago rechazado solo no es dinero y deja sin tocar`() {
        assertEquals(
            EstadoCuenta.SIN_TOCAR,
            estadoDe(pagos = listOf(pago(importe = "300").copy(rechazado = true)))
        )
    }

    @Test
    fun `E-APP-NNN pago rechazado no se suma al aceptado`() {
        assertEquals(
            EstadoCuenta.ABONO_PARCIAL,
            estadoDe(
                pagos = listOf(
                    pago(importe = "300").copy(rechazado = true),
                    pago(importe = "100")
                )
            )
        )
    }

    @Test
    fun `E-APP-NNN pago rechazado no tapa la visita`() {
        assertEquals(
            EstadoCuenta.SE_NEGO,
            estadoDe(
                pagos = listOf(pago(importe = "300").copy(rechazado = true)),
                visitas = listOf(visita(TipoVisitaCatalogo.NO_VA_A_DAR_PAGO))
            )
        )
    }
```

- [ ] **Step 2: Correr y verificar que fallan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.EstadoCuentaDeriverTest'`
Expected: FAIL de compilación, `Unresolved reference: rechazado`.

- [ ] **Step 3: Implementación mínima**

En `DerivacionModelos.kt`, reemplazar `PagoEnVentana` por:

```kotlin
/**
 * Un pago capturado. [importe] es [BigDecimal] por la misma razón que
 * [CuentaDelPeriodo.parcialidad].
 *
 * [formaCobroId] se filtra contra [VentanaCobro.FORMAS_COBRO_COBRANZA]: la
 * condonación no es cobranza.
 *
 * [rechazado] es `DOCTO_CC_ID = -1` (`DOCTO_CC_ID_RECHAZADO_POR_EL_SERVIDOR`):
 * el servidor no tiene ese pago, así que no es dinero y no entra al abono,
 * aunque sí cuenta como cobertura ([CoberturaDelPeriodo]). Hoy el adaptador del
 * teléfono (`RoomPagosAdapter.aHistorial`) ya los descarta antes de llegar aquí
 * y nunca pasa `true`. El campo existe para que el juego de casos compartido con
 * el servidor (E-APP-NNN) pueda expresar la regla con las mismas entradas en
 * Go y en Kotlin.
 */
data class PagoEnVentana(
    val ventaId: Int,
    val importe: BigDecimal,
    val formaCobroId: Int,
    val fechaHora: Instant,
    val rechazado: Boolean = false
)
```

En `EstadoCuentaDeriver.kt`, `abonosPorVenta`:

```kotlin
    private fun abonosPorVenta(
        pagos: List<PagoEnVentana>,
        ventana: VentanaCobro
    ): Map<Int, BigDecimal> = pagos
        .filter { it.formaCobroId in VentanaCobro.FORMAS_COBRO_COBRANZA }
        // Rechazado (DOCTO_CC_ID = -1) no es dinero que entró: E-APP-NNN.
        .filterNot { it.rechazado }
        .filter { ventana.contiene(it.fechaHora) }
        .groupingBy { it.ventaId }
        .fold(BigDecimal.ZERO) { acumulado, pago -> acumulado + pago.importe }
```

Y en el KDoc de `abonosPorVenta`, debajo del párrafo "Un pago cancelado no debe llegar hasta acá…", agregar: `Un pago rechazado (DOCTO_CC_ID = -1) sí puede llegar —con [PagoEnVentana.rechazado]— y se descarta aquí.`

- [ ] **Step 4: Correr y verificar que pasan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.EstadoCuentaDeriverTest'`
Expected: PASS, las 50 pruebas previas más las 3 nuevas.

- [ ] **Step 5: Control positivo (la prueba guarda el arreglo)**

Comentar temporalmente la línea `.filterNot { it.rechazado }`, correr el comando del Step 4 y confirmar que **fallan** las pruebas `pago rechazado solo…` (da `PAGO`) y `pago rechazado no se suma…` (da `PAGO`). Restaurar la línea, volver a correr y confirmar PASS. Anotar las dos salidas (fecha, "dev local") para la evidencia de la Task 4.

- [ ] **Step 6: Commit**

```bash
git add core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/DerivacionModelos.kt \
        core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt \
        core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriverTest.kt
git commit -m "feat(cobranza): pago rechazado expresable en el dominio y fuera del abono (E-APP-NNN)"
```

---

### Task 2: función de cobertura `CoberturaDelPeriodo.ventasConAlgo`

**Files:**
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/IndiceVisitas.kt` (el índice sale del deriver, sin cambiar su cuerpo)
- Modify: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt:201-248` (quitar la clase anidada `IndiceVisitas`; las llamadas `IndiceVisitas.de(...)` y `indice.masRecienteFor(...)` quedan iguales)
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/CoberturaDelPeriodo.kt`
- Test: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/CoberturaDelPeriodoTest.kt`

**Interfaces:**
- Consumes: `PagoEnVentana.rechazado` (Task 1).
- Produces: `object CoberturaDelPeriodo { fun ventasConAlgo(cuentas: List<CuentaDelPeriodo>, pagos: List<PagoEnVentana>, visitas: List<VisitaEnVentana>, ventana: VentanaCobro): Set<Int> }`. Devuelve los `ventaId` de `cuentas` que tienen algo. Además, `internal class IndiceVisitas` con `fun masRecienteFor(cuenta: CuentaDelPeriodo): VisitaEnVentana?`, `val huerfanas: Int` y `companion fun de(visitasDelPeriodo: List<VisitaEnVentana>): IndiceVisitas`.

- [ ] **Step 1: Escribir las pruebas que fallan**

`CoberturaDelPeriodoTest.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain

import java.math.BigDecimal
import java.time.Instant
import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * Cobertura: una venta tiene "algo" en la semana si recibió al menos un pago de
 * cualquier forma (rechazado y condonación incluidos) o una visita que le
 * aplique con la MISMA regla de alcance que [EstadoCuentaDeriver]. Spec de
 * desempeño §2 "Cobertura".
 */
class CoberturaDelPeriodoTest {

    private val ventana = VentanaCobro(
        inicio = Instant.parse("2026-10-09T13:46:00Z"),
        fin = Instant.parse("2026-10-14T22:40:00Z")
    )
    private val dentro = Instant.parse("2026-10-11T16:00:00Z")
    private val antes = Instant.parse("2026-10-09T13:45:59Z")

    private val ventaUno = 1
    private val ventaDos = 2
    private val clienteRosario = 10

    private val cuentas = listOf(
        CuentaDelPeriodo(ventaUno, clienteRosario, BigDecimal("150.00")),
        CuentaDelPeriodo(ventaDos, clienteRosario, BigDecimal("200.00"))
    )

    private fun pago(ventaId: Int, formaCobroId: Int = 157, fechaHora: Instant = dentro, rechazado: Boolean = false) =
        PagoEnVentana(ventaId, BigDecimal("150.00"), formaCobroId, fechaHora, rechazado)

    private fun visita(tipo: String, ventaId: Int?, fechaHora: Instant = dentro, fechaCita: LocalDate? = null) =
        VisitaEnVentana(clienteRosario, ventaId, tipo, fechaHora, fechaCita = fechaCita)

    private fun conAlgo(pagos: List<PagoEnVentana> = emptyList(), visitas: List<VisitaEnVentana> = emptyList()) =
        CoberturaDelPeriodo.ventasConAlgo(cuentas, pagos, visitas, ventana)

    @Test
    fun `sin pagos ni visitas no hay cobertura`() {
        assertEquals(emptySet<Int>(), conAlgo())
    }

    @Test
    fun `pago marca solo su venta`() {
        assertEquals(setOf(ventaUno), conAlgo(pagos = listOf(pago(ventaUno))))
    }

    @Test
    fun `E-APP-NNN pago rechazado cuenta como algo`() {
        assertEquals(setOf(ventaUno), conAlgo(pagos = listOf(pago(ventaUno, rechazado = true))))
    }

    @Test
    fun `condonacion cuenta como algo`() {
        assertEquals(setOf(ventaDos), conAlgo(pagos = listOf(pago(ventaDos, formaCobroId = 137026))))
    }

    @Test
    fun `pago fuera de la ventana no cuenta`() {
        assertEquals(emptySet<Int>(), conAlgo(pagos = listOf(pago(ventaUno, fechaHora = antes))))
    }

    @Test
    fun `no estaba de alcance cliente marca todas las ventas del cliente`() {
        assertEquals(
            setOf(ventaUno, ventaDos),
            conAlgo(visitas = listOf(visita(TipoVisitaCatalogo.NO_SE_ENCONTRABA, ventaId = null)))
        )
    }

    @Test
    fun `cita con dia marca todas las ventas aunque el literal sea de venta`() {
        assertEquals(
            setOf(ventaUno, ventaDos),
            conAlgo(
                visitas = listOf(
                    visita(TipoVisitaCatalogo.PIDE_TIEMPO, ventaId = ventaUno, fechaCita = LocalDate.parse("2026-10-15"))
                )
            )
        )
    }

    @Test
    fun `visita de alcance venta marca solo su venta`() {
        assertEquals(
            setOf(ventaDos),
            conAlgo(visitas = listOf(visita(TipoVisitaCatalogo.NO_VA_A_DAR_PAGO, ventaId = ventaDos)))
        )
    }

    @Test
    fun `visita de alcance venta sin venta ligada no marca nada`() {
        assertEquals(
            emptySet<Int>(),
            conAlgo(visitas = listOf(visita(TipoVisitaCatalogo.NO_VA_A_DAR_PAGO, ventaId = null)))
        )
    }

    @Test
    fun `visita fuera de la ventana no cuenta`() {
        assertEquals(
            emptySet<Int>(),
            conAlgo(visitas = listOf(visita(TipoVisitaCatalogo.NO_SE_ENCONTRABA, ventaId = null, fechaHora = antes)))
        )
    }

    @Test
    fun `pago de una venta que no esta en cuentas no agrega nada`() {
        assertEquals(emptySet<Int>(), conAlgo(pagos = listOf(pago(ventaId = 99))))
    }
}
```

- [ ] **Step 2: Correr y verificar que fallan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.CoberturaDelPeriodoTest'`
Expected: FAIL de compilación, `Unresolved reference: CoberturaDelPeriodo`.

- [ ] **Step 3: Extraer `IndiceVisitas`**

Crear `IndiceVisitas.kt` con el **mismo cuerpo y KDoc** que la clase anidada actual (`EstadoCuentaDeriver.kt:201-248`). Lo único que cambia es la cabecera: `private class IndiceVisitas(` pasa a `internal class IndiceVisitas(`. Al KDoc se le agrega: `Lo comparten [EstadoCuentaDeriver] y [CoberturaDelPeriodo]: una sola regla de "qué visita le aplica a qué venta".` Borrar la clase anidada del deriver. Correr el comando del Step 4 de la Task 1 y confirmar PASS (la refactorización no cambia nada).

- [ ] **Step 4: Implementación mínima de la cobertura**

`CoberturaDelPeriodo.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain

/**
 * Cobertura de la semana: ¿la venta tiene "algo"? Spec de desempeño de
 * cobradores §2: al menos un pago de CUALQUIER forma —rechazado (E-APP-NNN) y
 * condonación incluidos— o una visita que le aplique.
 *
 * "Que le aplique" es la misma regla de [EstadoCuentaDeriver], vía el mismo
 * [IndiceVisitas]: las visitas de alcance venta con esa venta y las de alcance
 * cliente de su cliente. Una visita de alcance venta sin venta ligada no le
 * aplica a ninguna.
 *
 * Es distinto del estado: un pago rechazado da cobertura y no da dinero, así que
 * una venta puede estar en [EstadoCuenta.SIN_TOCAR] y "con algo" a la vez. Dominio
 * puro, como el deriver.
 */
object CoberturaDelPeriodo {

    fun ventasConAlgo(
        cuentas: List<CuentaDelPeriodo>,
        pagos: List<PagoEnVentana>,
        visitas: List<VisitaEnVentana>,
        ventana: VentanaCobro
    ): Set<Int> {
        val conPago = pagos
            .filter { ventana.contiene(it.fechaHora) }
            .mapTo(HashSet()) { it.ventaId }
        val indice = IndiceVisitas.de(visitas.filter { ventana.contiene(it.fechaHora) })
        return cuentas
            .filter { it.ventaId in conPago || indice.masRecienteFor(it) != null }
            .mapTo(LinkedHashSet()) { it.ventaId }
    }
}
```

- [ ] **Step 5: Correr y verificar que pasan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.*'`
Expected: PASS (deriver, catálogo, ventana y cobertura).

- [ ] **Step 6: Lint, detekt y cobertura del módulo**

Run: `./gradlew --no-parallel --no-daemon :core:common:ktlintCheck :core:common:detekt :core:common:koverVerify`
Expected: BUILD SUCCESSFUL (Kover ≥ 90 %).

- [ ] **Step 7: Commit**

```bash
git add core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/IndiceVisitas.kt \
        core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt \
        core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/CoberturaDelPeriodo.kt \
        core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/CoberturaDelPeriodoTest.kt
git commit -m "feat(cobranza): cobertura de la semana por venta con la misma regla de visitas del deriver"
```

---

### Task 3: copiar los casos desde msp-api y verificar `VERSION`

**Bloqueada por:** la dependencia dura (existe `msp-api/testdata/estado-cuenta/VERSION`).

**Files:**
- Create: `scripts/casos-estado-cuenta.sh` (modos `copiar` y `verificar`)
- Create: `.gitattributes` (o modificar si ya existe al ejecutar)
- Create: `core/common/src/test/resources/estado-cuenta/*.json` y `VERSION` (los genera el script; no se editan a mano)
- Modify: `core/common/build.gradle.kts` (bloque `dependencies`)
- Create: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/JuegoDeCasos.kt`
- Test: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/JuegoDeCasosVersionTest.kt`

**Interfaces:**
- Consumes: el directorio del API con el formato de la "Dependencia dura".
- Produces (código de prueba, paquete `com.example.msp_app.core.common.cobranza.domain.casos`): `internal object JuegoDeCasos { const val DIRECTORIO = "estado-cuenta"; fun directorio(): File; fun archivosDeCaso(dir: File = directorio()): List<File>; fun sha256(archivos: List<File>): String; fun versionDeclarada(dir: File = directorio()): String }`.

- [ ] **Step 1: Script de copia y verificación**

`scripts/casos-estado-cuenta.sh` (hacerlo ejecutable con `chmod +x`):

```bash
#!/usr/bin/env bash
# Juego de casos compartido de estado por venta (desempeño de cobradores).
# Fuente: msp-api/testdata/estado-cuenta. Copia: core/common/src/test/resources/estado-cuenta.
#   copiar    — reemplaza la copia por la fuente, byte a byte, y verifica.
#   verificar — falla (exit 1) si la copia difiere de la fuente o si VERSION no es el hash.
# MSP_API_DIR permite apuntar a otro checkout de msp-api (default: carpeta hermana).
set -euo pipefail

raiz="$(cd "$(dirname "$0")/.." && pwd)"
fuente="${MSP_API_DIR:-$raiz/../msp-api}/testdata/estado-cuenta"
copia="$raiz/core/common/src/test/resources/estado-cuenta"

hash_de() {
  (cd "$1" && ls -- *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1)
}

verificar() {
  [[ -f "$fuente/VERSION" ]] || { echo "no existe $fuente/VERSION" >&2; exit 1; }
  diff -r "$fuente" "$copia" >&2 || { echo "DIVERGENCIA: la copia no es idéntica a la fuente" >&2; exit 1; }
  local declarado calculado
  declarado="$(tr -d '\n' < "$copia/VERSION")"
  calculado="$(hash_de "$copia")"
  [[ "$declarado" == "$calculado" ]] || { echo "VERSION=$declarado pero el hash es $calculado" >&2; exit 1; }
  echo "ok: juego idéntico, VERSION $declarado"
}

case "${1:-}" in
  copiar)
    [[ -f "$fuente/VERSION" ]] || { echo "no existe $fuente/VERSION" >&2; exit 1; }
    rm -rf "$copia"
    mkdir -p "$copia"
    cp -p "$fuente"/*.json "$fuente/VERSION" "$copia/"
    verificar
    ;;
  verificar) verificar ;;
  *) echo "uso: $0 copiar|verificar" >&2; exit 2 ;;
esac
```

`diff -r` también detecta archivos que sobran o faltan en cualquiera de los dos lados. Si la fuente tiene algo distinto de `*.json` y `VERSION`, `verificar` lo marca como divergencia, que es lo que se quiere (formato de la "Dependencia dura", punto 2).

- [ ] **Step 2: `.gitattributes` y la copia**

`.gitattributes` en la raíz de msp-app-kt:

```
core/common/src/test/resources/estado-cuenta/** -text
```

Run: `./scripts/casos-estado-cuenta.sh copiar`
Expected: `ok: juego idéntico, VERSION <64 hex>`, el mismo valor que `cat /Volumes/M2-1TB/Developer/msp-api/testdata/estado-cuenta/VERSION`.

Control positivo del script: `printf ' ' >> core/common/src/test/resources/estado-cuenta/VERSION && ./scripts/casos-estado-cuenta.sh verificar; echo "exit=$?"` debe imprimir `DIVERGENCIA…` y `exit=1`. Luego `./scripts/casos-estado-cuenta.sh copiar` para restaurar.

- [ ] **Step 3: Gson para pruebas**

En `core/common/build.gradle.kts`, dentro de `dependencies { … }`, después de `testImplementation(project(":core:testing"))`:

```kotlin
    // El juego de casos compartido con msp-api (estado por venta) es JSON.
    // Solo pruebas: el dominio de :core:common no parsea JSON.
    testImplementation(libs.gson)
```

- [ ] **Step 4: Escribir las pruebas que fallan**

`JuegoDeCasosVersionTest.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain.casos

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder

/**
 * El VERSION del juego compartido es el SHA-256 de los .json ordenados por
 * nombre y concatenados (contratos de desempeño §7). El mismo VERSION en
 * msp-api y aquí demuestra que los dos repos prueban los mismos bytes; cambiar
 * un caso sin actualizar VERSION pone esto en rojo.
 */
class JuegoDeCasosVersionTest {

    @get:Rule
    val tmp = TemporaryFolder()

    @Test
    fun `VERSION coincide con el SHA-256 de los casos`() {
        val archivos = JuegoDeCasos.archivosDeCaso()
        assertEquals(JuegoDeCasos.versionDeclarada(), JuegoDeCasos.sha256(archivos))
    }

    @Test
    fun `el juego no esta vacio`() {
        assertTrue("el directorio de casos no trae ningún .json", JuegoDeCasos.archivosDeCaso().isNotEmpty())
    }

    @Test
    fun `VERSION tiene 64 hex minusculas`() {
        assertTrue(Regex("^[0-9a-f]{64}$").matches(JuegoDeCasos.versionDeclarada()))
    }

    @Test
    fun `sha256 concatena en orden de nombre - vector conocido`() {
        // Escritos en desorden a propósito: el orden lo pone archivosDeCaso.
        File(tmp.root, "002-b.json").writeText("b")
        File(tmp.root, "001-a.json").writeText("a")
        File(tmp.root, "VERSION").writeText("x")
        val archivos = JuegoDeCasos.archivosDeCaso(tmp.root)
        assertEquals(listOf("001-a.json", "002-b.json"), archivos.map { it.name })
        // printf ab | shasum -a 256
        assertEquals(
            "fb8e20fc2e4c3f248c60c39bd652f3c1347298bb977b8b4d5903b85055620603",
            JuegoDeCasos.sha256(archivos)
        )
    }

    @Test
    fun `archivo fuera de formato se rechaza`() {
        File(tmp.root, "001-a.json").writeText("a")
        File(tmp.root, "README.md").writeText("x")
        try {
            JuegoDeCasos.archivosDeCaso(tmp.root)
            fail("debió rechazar README.md")
        } catch (e: IllegalArgumentException) {
            assertTrue(e.message.orEmpty().contains("README.md"))
        }
    }
}
```

- [ ] **Step 5: Correr y verificar que fallan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.casos.*'`
Expected: FAIL de compilación, `Unresolved reference: JuegoDeCasos`.

- [ ] **Step 6: Implementación mínima del ayudante**

`JuegoDeCasos.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain.casos

import java.io.File
import java.security.MessageDigest

/**
 * Acceso al juego de casos compartido con msp-api
 * (`src/test/resources/estado-cuenta/`, copiado con
 * `scripts/casos-estado-cuenta.sh copiar`; nunca se edita a mano).
 */
internal object JuegoDeCasos {

    const val DIRECTORIO = "estado-cuenta"
    private const val VERSION = "VERSION"
    private val NOMBRE_CASO = Regex("^[0-9]{3}-[a-z0-9-]+\\.json$")

    fun directorio(): File {
        val url = requireNotNull(javaClass.classLoader?.getResource(DIRECTORIO)) {
            "no está src/test/resources/$DIRECTORIO en el classpath de prueba; corre scripts/casos-estado-cuenta.sh copiar"
        }
        require(url.protocol == "file") { "los casos deben leerse de disco, llegaron como ${url.protocol}" }
        return File(url.toURI())
    }

    /** Los .json del juego en orden de nombre (= `LC_ALL=C sort`, nombres solo ASCII). */
    fun archivosDeCaso(dir: File = directorio()): List<File> {
        val visibles = dir.listFiles().orEmpty().filterNot { it.name.startsWith(".") }
        val fueraDeFormato = visibles.filterNot { it.name == VERSION || NOMBRE_CASO.matches(it.name) }
        require(fueraDeFormato.isEmpty()) {
            "archivos fuera de formato en $DIRECTORIO: ${fueraDeFormato.map { it.name }.sorted()}"
        }
        return visibles.filter { NOMBRE_CASO.matches(it.name) }.sortedBy { it.name }
    }

    fun sha256(archivos: List<File>): String {
        val digest = MessageDigest.getInstance("SHA-256")
        archivos.forEach { digest.update(it.readBytes()) }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    fun versionDeclarada(dir: File = directorio()): String =
        File(dir, VERSION).readText(Charsets.US_ASCII).trim()
}
```

- [ ] **Step 7: Correr y verificar que pasan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.casos.*'`
Expected: PASS (5 pruebas).

- [ ] **Step 8: Control positivo del VERSION en Kotlin**

Agregar un espacio al final de un `.json` de la copia (`printf ' ' >> core/common/src/test/resources/estado-cuenta/001-*.json`), correr el comando del Step 7 y confirmar que **falla** `VERSION coincide con el SHA-256 de los casos`. Restaurar con `./scripts/casos-estado-cuenta.sh copiar` y confirmar PASS.

- [ ] **Step 9: Commit**

```bash
git add .gitattributes scripts/casos-estado-cuenta.sh core/common/build.gradle.kts \
        core/common/src/test/resources/estado-cuenta \
        core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/JuegoDeCasos.kt \
        core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/JuegoDeCasosVersionTest.kt
git commit -m "test(cobranza): juego de casos de estado por venta copiado de msp-api con VERSION verificado"
```

---

### Task 4: la prueba parametrizada corre cada caso contra el deriver y la cobertura

**Bloqueada por:** Tasks 1, 2 y 3.

**Files:**
- Create: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/CasoDeEstadoCuenta.kt` (parseo JSON → entradas del dominio)
- Test: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/EstadoCuentaCasosCompartidosTest.kt`
- Test: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/CasoDeEstadoCuentaParseoTest.kt`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/docs/evidencia/app-android.md` (entrada `E-APP-NNN`)

**Interfaces:**
- Consumes: `JuegoDeCasos` (Task 3), `PagoEnVentana(…, rechazado)` (Task 1), `CoberturaDelPeriodo.ventasConAlgo` (Task 2), `EstadoCuentaDeriver.derivar`.
- Produces (código de prueba): `internal data class Esperado(val ventaId: Int, val estado: EstadoCuenta, val conAlgo: Boolean)`; `internal data class CasoDeEstadoCuenta(val archivo: String, val nombre: String, val ventana: VentanaCobro, val cuentas: List<CuentaDelPeriodo>, val pagos: List<PagoEnVentana>, val visitas: List<VisitaEnVentana>, val esperado: List<Esperado>)` con `companion fun desdeJson(archivo: String, texto: String): CasoDeEstadoCuenta`; `internal object CodigoEstado { fun aEnum(codigo: String): EstadoCuenta; fun aCodigo(estado: EstadoCuenta): String }`.

- [ ] **Step 1: Escribir las pruebas de parseo que fallan**

`CasoDeEstadoCuentaParseoTest.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain.casos

import com.example.msp_app.core.common.cobranza.domain.EstadoCuenta
import java.math.BigDecimal
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test

class CasoDeEstadoCuentaParseoTest {

    private val valido = """
        { "nombre": "cliente con dos ventas, no estaba marca ambas",
          "ventana": { "inicio": "2026-10-09T13:46:00Z", "fin": "2026-10-14T22:40:00Z" },
          "cuentas": [ { "venta_id": 1, "cliente_id": 10, "parcialidad": "150.00" },
                       { "venta_id": 2, "cliente_id": 10, "parcialidad": "200.00" } ],
          "pagos":   [ { "venta_id": 1, "importe": "150.00", "forma_cobro_id": 157,
                         "fecha_hora": "2026-10-10T15:00:00Z", "rechazado": false } ],
          "visitas": [ { "cliente_id": 10, "venta_id": null, "tipo_visita": "No se encontraba",
                         "fecha_hora": "2026-10-11T16:00:00Z", "fecha_cita": null, "hora_cita": null,
                         "fecha_promesa": null } ],
          "esperado": [ { "venta_id": 1, "estado": "pago", "con_algo": true },
                        { "venta_id": 2, "estado": "no_estaba", "con_algo": true } ] }
    """.trimIndent()

    private fun rechaza(json: String, debeMencionar: String) {
        try {
            CasoDeEstadoCuenta.desdeJson("999-prueba.json", json)
            fail("debió rechazar: $debeMencionar")
        } catch (e: IllegalArgumentException) {
            assertTrue("mensaje: ${e.message}", e.message.orEmpty().contains(debeMencionar))
        }
    }

    @Test
    fun `el ejemplo del contrato se parsea`() {
        val caso = CasoDeEstadoCuenta.desdeJson("001-ejemplo.json", valido)
        assertEquals(2, caso.cuentas.size)
        assertEquals(BigDecimal("150.00"), caso.pagos.single().importe)
        assertEquals(null, caso.visitas.single().ventaId)
        assertEquals(EstadoCuenta.NO_ESTABA, caso.esperado[1].estado)
    }

    @Test
    fun `los ocho codigos del contrato van y vuelven`() {
        val codigos = listOf(
            "pago", "abono_parcial", "visite_vuelvo", "prometio_proxima",
            "se_nego", "cita_a_una_hora", "no_estaba", "sin_tocar"
        )
        assertEquals(EstadoCuenta.entries.toSet(), codigos.map(CodigoEstado::aEnum).toSet())
        codigos.forEach { assertEquals(it, CodigoEstado.aCodigo(CodigoEstado.aEnum(it))) }
    }

    @Test
    fun `codigo de estado desconocido se rechaza`() {
        rechaza(valido.replace("\"no_estaba\"", "\"NO_ESTABA\""), "NO_ESTABA")
    }

    @Test
    fun `dinero como numero JSON se rechaza`() {
        rechaza(valido.replace("\"importe\": \"150.00\"", "\"importe\": 150.0"), "importe")
    }

    @Test
    fun `clave desconocida se rechaza`() {
        rechaza(valido.replace("\"fecha_cita\": null", "\"fecha_cit\": null"), "fecha_cit")
    }

    @Test
    fun `clave obligatoria ausente se rechaza`() {
        rechaza(valido.replace(", \"rechazado\": false", ""), "rechazado")
    }

    @Test
    fun `esperado incompleto se rechaza`() {
        val sinVentaDos = valido.replace(
            Regex(""",\s*\{ "venta_id": 2, "estado": "no_estaba", "con_algo": true \}"""),
            ""
        )
        // Control: el reemplazo sí ocurrió (si no, la prueba pasaría por otra razón).
        assertTrue(sinVentaDos != valido)
        rechaza(sinVentaDos, "esperado")
    }
}
```

- [ ] **Step 2: Correr y verificar que fallan**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.casos.CasoDeEstadoCuentaParseoTest'`
Expected: FAIL de compilación, `Unresolved reference: CasoDeEstadoCuenta`.

- [ ] **Step 3: Implementación del parseo**

`CasoDeEstadoCuenta.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain.casos

import com.example.msp_app.core.common.cobranza.domain.CuentaDelPeriodo
import com.example.msp_app.core.common.cobranza.domain.EstadoCuenta
import com.example.msp_app.core.common.cobranza.domain.PagoEnVentana
import com.example.msp_app.core.common.cobranza.domain.VentanaCobro
import com.example.msp_app.core.common.cobranza.domain.VisitaEnVentana
import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import java.math.BigDecimal
import java.time.Instant
import java.time.LocalDate
import java.time.LocalTime
import java.util.Locale

/** Códigos snake_case del contrato (§1 `EstadoCuenta`) ↔ enum del teléfono. */
internal object CodigoEstado {
    private val porCodigo: Map<String, EstadoCuenta> =
        EstadoCuenta.entries.associateBy { it.name.lowercase(Locale.ROOT) }

    fun aEnum(codigo: String): EstadoCuenta =
        requireNotNull(porCodigo[codigo]) { "estado desconocido en el caso: \"$codigo\"" }

    fun aCodigo(estado: EstadoCuenta): String = estado.name.lowercase(Locale.ROOT)
}

internal data class Esperado(val ventaId: Int, val estado: EstadoCuenta, val conAlgo: Boolean)

/**
 * Un caso del juego compartido con msp-api (contratos de desempeño §7). El
 * parseo es ESTRICTO a propósito: una clave desconocida, una obligatoria
 * ausente o dinero como número JSON fallan con el nombre del problema, porque un
 * campo mal escrito que se leyera como `null` daría verde por casualidad.
 */
internal data class CasoDeEstadoCuenta(
    val archivo: String,
    val nombre: String,
    val ventana: VentanaCobro,
    val cuentas: List<CuentaDelPeriodo>,
    val pagos: List<PagoEnVentana>,
    val visitas: List<VisitaEnVentana>,
    val esperado: List<Esperado>
) {
    override fun toString(): String = "$archivo — $nombre"

    companion object {
        fun desdeJson(archivo: String, texto: String): CasoDeEstadoCuenta {
            val raiz = JsonParser.parseString(texto).asJsonObject
            raiz.soloClaves(archivo, "nombre", "ventana", "cuentas", "pagos", "visitas", "esperado")
            val v = raiz.obj(archivo, "ventana").also { it.soloClaves(archivo, "inicio", "fin") }
            val cuentas = raiz.arr(archivo, "cuentas").map {
                it.soloClaves(archivo, "venta_id", "cliente_id", "parcialidad")
                CuentaDelPeriodo(it.int(archivo, "venta_id"), it.int(archivo, "cliente_id"), it.dinero(archivo, "parcialidad"))
            }
            val caso = CasoDeEstadoCuenta(
                archivo = archivo,
                nombre = raiz.str(archivo, "nombre"),
                ventana = VentanaCobro(Instant.parse(v.str(archivo, "inicio")), Instant.parse(v.str(archivo, "fin"))),
                cuentas = cuentas,
                pagos = raiz.arr(archivo, "pagos").map {
                    it.soloClaves(archivo, "venta_id", "importe", "forma_cobro_id", "fecha_hora", "rechazado")
                    PagoEnVentana(
                        ventaId = it.int(archivo, "venta_id"),
                        importe = it.dinero(archivo, "importe"),
                        formaCobroId = it.int(archivo, "forma_cobro_id"),
                        fechaHora = Instant.parse(it.str(archivo, "fecha_hora")),
                        rechazado = it.bool(archivo, "rechazado")
                    )
                },
                visitas = raiz.arr(archivo, "visitas").map {
                    it.soloClaves(
                        archivo, "cliente_id", "venta_id", "tipo_visita", "fecha_hora",
                        "fecha_cita", "hora_cita", "fecha_promesa"
                    )
                    VisitaEnVentana(
                        clienteId = it.int(archivo, "cliente_id"),
                        ventaId = it.intONull(archivo, "venta_id"),
                        tipoVisita = it.str(archivo, "tipo_visita"),
                        fechaHora = Instant.parse(it.str(archivo, "fecha_hora")),
                        fechaPromesa = it.strONull(archivo, "fecha_promesa")?.let(LocalDate::parse),
                        fechaCita = it.strONull(archivo, "fecha_cita")?.let(LocalDate::parse),
                        horaCita = it.strONull(archivo, "hora_cita")?.let(LocalTime::parse)
                    )
                },
                esperado = raiz.arr(archivo, "esperado").map {
                    it.soloClaves(archivo, "venta_id", "estado", "con_algo")
                    Esperado(it.int(archivo, "venta_id"), CodigoEstado.aEnum(it.str(archivo, "estado")), it.bool(archivo, "con_algo"))
                }
            )
            val idsCuentas = cuentas.map { it.ventaId }
            val idsEsperado = caso.esperado.map { it.ventaId }
            require(idsCuentas.size == idsCuentas.toSet().size) { "$archivo: venta_id repetido en cuentas" }
            require(idsEsperado.sorted() == idsCuentas.sorted()) {
                "$archivo: esperado debe traer exactamente una entrada por cuenta; cuentas=$idsCuentas esperado=$idsEsperado"
            }
            return caso
        }

        private fun JsonObject.soloClaves(archivo: String, vararg permitidas: String) {
            val sobran = keySet() - permitidas.toSet()
            val faltan = permitidas.toSet() - keySet()
            require(sobran.isEmpty()) { "$archivo: clave desconocida $sobran" }
            require(faltan.isEmpty()) { "$archivo: falta la clave $faltan" }
        }

        private fun JsonObject.campo(archivo: String, k: String): JsonElement =
            requireNotNull(get(k)) { "$archivo: falta la clave $k" }

        private fun JsonObject.obj(archivo: String, k: String): JsonObject = campo(archivo, k).asJsonObject
        private fun JsonObject.arr(archivo: String, k: String): List<JsonObject> =
            campo(archivo, k).asJsonArray.map { it.asJsonObject }
        private fun JsonObject.str(archivo: String, k: String): String {
            val e = campo(archivo, k)
            require(e.isJsonPrimitive && e.asJsonPrimitive.isString) { "$archivo: $k debe ser string" }
            return e.asString
        }
        private fun JsonObject.strONull(archivo: String, k: String): String? =
            if (campo(archivo, k).isJsonNull) null else str(archivo, k)
        private fun JsonObject.int(archivo: String, k: String): Int {
            val e = campo(archivo, k)
            require(e.isJsonPrimitive && e.asJsonPrimitive.isNumber) { "$archivo: $k debe ser número" }
            return e.asInt
        }
        private fun JsonObject.intONull(archivo: String, k: String): Int? =
            if (campo(archivo, k).isJsonNull) null else int(archivo, k)
        private fun JsonObject.bool(archivo: String, k: String): Boolean {
            val e = campo(archivo, k)
            require(e.isJsonPrimitive && e.asJsonPrimitive.isBoolean) { "$archivo: $k debe ser booleano" }
            return e.asBoolean
        }
        /** Dinero SOLO desde string (`"150.00"`): nunca pasa por `Double`. */
        private fun JsonObject.dinero(archivo: String, k: String): BigDecimal {
            val e = campo(archivo, k)
            require(e.isJsonPrimitive && e.asJsonPrimitive.isString) {
                "$archivo: $k es dinero y debe venir como string decimal, no como número JSON"
            }
            return BigDecimal(e.asString)
        }
    }
}
```

Si detekt marca `LongMethod`/`TooManyFunctions` en código de prueba, partir `desdeJson` en funciones privadas `cuentaDe`, `pagoDe`, `visitaDe` y `esperadoDe` sin cambiar el comportamiento. **No** suprimir la regla.

- [ ] **Step 4: Correr y verificar que pasa el parseo**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.casos.CasoDeEstadoCuentaParseoTest'`
Expected: PASS (7 pruebas).

- [ ] **Step 5: Escribir la prueba parametrizada y la de cobertura del juego**

`EstadoCuentaCasosCompartidosTest.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain.casos

import com.example.msp_app.core.common.cobranza.domain.CoberturaDelPeriodo
import com.example.msp_app.core.common.cobranza.domain.EstadoCuenta
import com.example.msp_app.core.common.cobranza.domain.EstadoCuentaDeriver
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.junit.runners.Parameterized

/**
 * Cada caso del juego compartido con msp-api (`testdata/estado-cuenta/`) corre
 * contra el MISMO deriver que usa la pantalla del cobrador. El servidor corre
 * los mismos archivos contra su port en Go: los dos verdes con el mismo VERSION
 * son la prueba de que teléfono y escritorio derivan igual con las mismas
 * entradas (E-APP-NNN).
 */
@RunWith(Parameterized::class)
class EstadoCuentaCasosCompartidosTest(private val caso: CasoDeEstadoCuenta) {

    @Test
    fun `estado y cobertura coinciden con esperado`() {
        val derivacion = EstadoCuentaDeriver.derivar(caso.cuentas, caso.pagos, caso.visitas, caso.ventana)
        val conAlgo = CoberturaDelPeriodo.ventasConAlgo(caso.cuentas, caso.pagos, caso.visitas, caso.ventana)
        val obtenido = caso.cuentas.map {
            Esperado(it.ventaId, derivacion.porVenta.getValue(it.ventaId).estado, it.ventaId in conAlgo)
        }.sortedBy { it.ventaId }
        assertEquals(caso.toString(), caso.esperado.sortedBy { it.ventaId }, obtenido)
    }

    companion object {
        @JvmStatic
        @Parameterized.Parameters(name = "{0}")
        fun casos(): List<CasoDeEstadoCuenta> =
            JuegoDeCasos.archivosDeCaso().map { CasoDeEstadoCuenta.desdeJson(it.name, it.readText(Charsets.UTF_8)) }
    }
}

/** El juego no puede ser verde por vacío o por no ejercitar las ramas (dependencia dura, punto 6). */
class JuegoDeCasosCoberturaTest {

    private val casos = EstadoCuentaCasosCompartidosTest.casos()

    @Test
    fun `los ocho estados aparecen en algun esperado`() {
        val vistos = casos.flatMap { c -> c.esperado.map { it.estado } }.toSet()
        assertEquals(EstadoCuenta.entries.toSet(), vistos)
    }

    @Test
    fun `aparecen ventas con algo y sin nada`() {
        val valores = casos.flatMap { c -> c.esperado.map { it.conAlgo } }.toSet()
        assertEquals(setOf(true, false), valores)
    }

    @Test
    fun `hay al menos un pago rechazado y una condonacion`() {
        val pagos = casos.flatMap { it.pagos }
        assertTrue("falta un caso con rechazado: true", pagos.any { it.rechazado })
        assertTrue("falta un caso con condonación (137026)", pagos.any { it.formaCobroId == 137026 })
    }
}
```

- [ ] **Step 6: Correr y verificar**

Run: `./gradlew --no-parallel --no-daemon :core:common:testDebugUnitTest --tests 'com.example.msp_app.core.common.cobranza.domain.casos.*'`
Expected: PASS, con un renglón por archivo en el reporte (`build/reports/tests/testDebugUnitTest/index.html`, nombre `[NNN-nombre.json — <nombre>]`). Si un caso falla, **no** se edita el `.json` aquí: el juego es del API. Se anota el caso y la diferencia, se decide con el dueño si el error está en Go, en Kotlin o en el caso, y el arreglo del caso entra por msp-api y se vuelve a copiar con el script.

- [ ] **Step 7: Control positivo de la prueba parametrizada**

Dos mutaciones temporales, una a la vez. Restaurar después de cada una:
1. En `EstadoCuentaDeriver.estadoPorDinero` cambiar `abonoVentana >= parcialidad` por `abonoVentana > parcialidad`. Correr el Step 6: debe fallar al menos un caso (el de borde exacto, si el juego lo trae). Si **ninguno** falla, el juego no tiene borde exacto: reportarlo como hueco al plan del API, sin agregar el caso aquí.
2. Quitar `.filterNot { it.rechazado }` (Task 1). Correr el Step 6: debe fallar el caso del pago rechazado.

Guardar las dos salidas rojas y la verde final (fecha, "dev local") para la evidencia.

- [ ] **Step 8: Verificación cruzada con msp-api**

Run: `./scripts/casos-estado-cuenta.sh verificar`
Expected: `ok: juego idéntico, VERSION <hash>`. Ese hash tiene que ser el mismo que el de la prueba Go del API (`go test ./internal/desempeno/domain/...` en msp-api, si ya existe; si no existe todavía, decirlo así en la evidencia).

- [ ] **Step 9: Compuerta completa**

Run: `./gradlew --no-parallel --no-daemon prePushCheck`
Expected: BUILD SUCCESSFUL. Incluye las pruebas de `:feature:pagos`, que confirman que el adaptador sigue igual con el campo nuevo. Si truena por las 5 pruebas de navegación `GooglePlayServicesMissingManifest` (residuo de `prodRelease`), correr `rm -rf app/build` y repetir. No es un fallo de este cambio.

- [ ] **Step 10: Evidencia**

Agregar en `/Volumes/M2-1TB/Developer/msp-api/docs/evidencia/app-android.md` la entrada `E-APP-NNN` con el formato del documento: afirmación ("el deriver de Kotlin excluye `rechazado` del abono, la cobertura lo cuenta, y el juego compartido `VERSION <hash>` pasa completo en `:core:common`"), nivel **MEDIDO**, fecha, dónde (dev local), controles positivos de la Task 1 Step 5, la Task 3 Step 8 y la Task 4 Step 7 con sus salidas, y lo que quedó sin verificar (por ejemplo, que el teléfono en producción no cambia de comportamiento: INFERIDO de `RoomPagosAdapter.kt:101-110`). El commit va en msp-api: `docs(evidencia): juego de casos de estado por venta corre en Kotlin (E-APP-NNN)`.

- [ ] **Step 11: Commit (msp-app-kt)**

```bash
git add core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/CasoDeEstadoCuenta.kt \
        core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/CasoDeEstadoCuentaParseoTest.kt \
        core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/EstadoCuentaCasosCompartidosTest.kt
git commit -m "test(cobranza): los casos compartidos de estado por venta corren contra el deriver de Kotlin (E-APP-NNN)"
```

---

## Cómo se mantiene sincronizado después

- Cuando el API cambia un caso: en msp-app-kt, `./scripts/casos-estado-cuenta.sh copiar`, correr `:core:common:testDebugUnitTest`, y hacer commit con el `VERSION` nuevo (`test(cobranza): juego de casos actualizado a VERSION <8 primeros hex>`).
- Para detectar divergencia, en cualquier momento: `./scripts/casos-estado-cuenta.sh verificar` (sale con código distinto de cero si la copia difiere). Para comparar sin checkout local de los dos repos, basta comparar el contenido de `VERSION` en los dos repos.
- Fuera de alcance: una prueba que compare contra msp-api en CI. `prePushCheck` no puede asumir que msp-api está en la carpeta hermana.
