# Desempeño de cobradores — pulido 2: plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Una sola regla de lista en 3 columnas para teléfono y escritorio, un tablero cuyo costo no crece con semanas abandonadas (contrato v2.2 por parámetro, v2.1 congelado), cifras defendibles bajo CLAUDE.md §8 y una pantalla de escritorio que nunca muestra un número bajo un rótulo que no le corresponde.

**Architecture:** Toda regla nueva vive en `msp-api/internal/desempeno` (dominio puro → app → `desempenohttp`), con la lista de zonas excluidas en `internal/config` consumida por su contrato. El API sirve **v2.1 sin cambios** por omisión y **v2.2** con `contrato=2.2`; el escritorio nuevo pide sólo v2.2. El teléfono deriva la columna en el aparato con la misma regla, amarrada a Go por el juego de casos compartido (`testdata/estado-cuenta`, mismo `VERSION` en los dos repos).

**Tech Stack:** Go 1.x + Huma v2 + chi + Firebird (firebirdsql) · React/TypeScript + Vitest + MSW + Tauri 2 · Kotlin/Compose + JUnit4 + Gson (pruebas) + Roborazzi.

**Spec:** `docs/superpowers/specs/2026-10-15-desempeno-pulido-2-design.md` (aprobado; decisiones cerradas en §10). Contrato vigente: `docs/superpowers/specs/2026-10-14-desempeno-contratos.md` (v2.1, se actualiza a v2.2 en la Tarea 0 **antes** de cualquier código).

---

## Global Constraints

Copiados del spec y de `CLAUDE.md`; aplican a TODAS las tareas.

- **Código en inglés o español del dominio; texto al usuario en español, corto, sin punto final.** Nunca la palabra "ciclo" en una cadena visible: se dice "semana". Textos de 2–4 palabras, sin frases explicativas.
- **Lógica en el servidor** (D10). Escritorio y teléfono sólo pintan; el teléfono deriva la columna en el aparato con la regla amarrada por casos compartidos.
- **Columnas (D18):** `por_visitar` · `no_paga` · `pagados`; etiquetas "Por visitar", "No paga", "Pagados". `diferido` con fecha > hoy → `no_paga`; `pagado` y `parcial` → `pagados`; todo lo demás (incluida toda cita y la promesa vencida o de hoy) → `por_visitar`. La etiqueta de `parcial` sigue siendo "Abonó parcial".
- **"Sin nada"** no es columna: filtro de cobertura sólo en el escritorio (DA-3). `cobertura_por_columna.sin_nada == cobertura.total − cobertura.con_algo`; `por_visitar + no_paga + pagados == cobertura.total`.
- **Semana vencida (D19):** `DiasSemanaVencida = 14`; vencida ⇔ `ahora − inicio_semana > 14 × 24 h`, con el mismo `ahora` de la respuesta. Sigue `configurado = true`.
- **`ultimo_movimiento`:** actividad más reciente registrada por el usuario en los últimos **92 días** (`RangoMaximo`); `null` = nada en 92 días.
- **`dias_navegables`:** vencida → los últimos 14 días de negocio hasta hoy (hoy incluido); no vencida → días de inicio de semana a hoy.
- **Actividad de semana vencida** sin `desde`: `[hasta − 14 días, hasta]`, sin rótulo.
- **Mayoreo fuera (D20, DA-1):** zona **23301**, por la tabla `MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS`; ficha de un usuario excluido → **409 `desempeno_zona_excluida`** "la zona del usuario está excluida de desempeño". Cuentas de semana vencida en v2.2 → **409 `desempeno_semana_vencida`** (DA-2).
- **`cobro.pct = null` cuando `esperado = 0`** (v2.1 y v2.2) — sólo tras la compuerta §8 (Tarea 3).
- **Horario (D21):** escritorio con pasos de 30 min: inicio 00:00–23:00, fin 00:30–23:30 más 23:59; si inicio ≥ fin, fin = `min(inicio + 1 h, 23:59)`. Servidor: `desde_hora < hasta_hora` del mismo día (sin cambio de validación).
- **Eje:** `desde_hora = min(7, hora(D), hora(primera actividad visible))`, `hasta_hora = max(20, ⌈H⌉, hora(última actividad visible) + 1)`, acotado a 0–24; `por_hora` v2.2 = una entrada por hora de `[desde_hora, hasta_hora)`; v2.1 = 7..19.
- **Totales del día:** opción A del mock (`…/scratchpad/mock-totales/project/Main.dc.html`, artifact `Lrz7JritH7exy1dbbpqghY`): encabezado de la tarjeta "Línea del día" + importe junto a cada carril. Sin tarjetas nuevas.
- **Carga Hoy↔Semana:** toggle inmediato, esqueleto, "Cargando semana…"/"Cargando hoy…" tras **1 s**, "No se pudo cargar" + "Reintentar"; nunca una cifra cuyo `periodo` no coincida con el seleccionado.
- **Cuentas:** primeras **60** filas y más al acercarse al final (`IntersectionObserver`), sin dependencia nueva.
- **CLAUDE.md §1:** `CREATED_AT` y todo valor lo pone Go; sin `DEFAULT`, sin triggers, sin procedimientos. **§2:** cruces de módulo sólo por el paquete de contratos. **§3:** códigos de error en inglés snake_case, mensajes en español minúsculas sin punto.
- **CLAUDE.md §8 (dinero):** nada por debajo de MEDIDO justifica un cambio; dos compuertas con agentes **independientes** (ni el autor ni el mismo agente dos veces). DA-4: M3 sólo se mide.
- **Producción SÓLO LECTURA.** Las mediciones en producción las ejecuta **el dueño** con su propio token o por `.bat` con sólo `SELECT`; el agente prepara el comando y analiza la salida (no maneja credenciales del usuario).
- **Base de desarrollo compartida:** nada persistente; pruebas en `fbtestutil.WithTestTransaction`. Aplicar una migración en dev exige autorización explícita del dueño en ese momento y `make fb-snapshot` antes.
- **Evidencia:** siguientes libres verificados el 2026-10-16: **E-INF-018**, **E-APP-070**, **E-VEN-008**. Este plan reserva `E-INF-018…023` y `E-APP-070`; `E-VEN-008` **no se usa** (nada de ventas). Antes de escribir cada entrada, `grep -rn "E-INF-0NN" docs/` para confirmar que sigue libre (la rama `fix/precios-orden-contado` ya usa E-INF-017 y el borrador `2026-10-15-visitas-compromisos-avisos-design.md:1012` menciona E-INF-018 como futura: si esa rama llega antes, correr todos los números de este plan al siguiente libre y anotarlo). Cada ID se cita en el comentario del código, el nombre de la prueba y el mensaje del commit.
- **Renumeración (2026-10-16, tras la tanda de precios/diagnóstico):** E-APP-068 y E-APP-069 ya están ocupados en `app-android.md` (precios en contado y `LAST_UPLOAD_ERROR_CODE`); este plan usa **E-APP-070** donde decía E-APP-068. E-INF-018…023 siguen reservados para este plan; los planes de visitas corren a E-INF-024+ y E-APP-071+.
- **Commits:** conventional commits (`feat|fix|test|docs|perf|refactor|chore(desempeno|config|pagos|cobranza): …`), **sin líneas de atribución**, nunca `--no-verify`, **sin push, sin tag, sin despliegue**.
- **Pruebas:** TDD; `go clean -testcache` antes de reportar verde; `-timeout 1800s` contra Firebird; nunca pipear a `tail`/`head` la corrida cuyo resultado importa. Gradle: `--no-parallel`, un módulo a la vez, `--rerun-tasks`, contar pruebas en el XML. Roborazzi: borrar `build/outputs/roborazzi`, `recordRoborazziDebug`, revisar **a ojo** el `git diff` de cada PNG (PIL en RGB, no RGBA).
- **Room:** este plan no toca la base local del teléfono; **v34 está tomada** por `feat/diagnostico-envio` — no crear migraciones.
- **Escritorio:** compilar siempre con la feature `devtools` (ya está en `src-tauri/Cargo.toml:24`; no quitarla).

## Review Focus

Condiciones que el spec implica y que ninguna prueba "natural" toca; cada una tiene su prueba en la tarea indicada.

1. **El borde exacto de 14 días** (14 d − 1 min no vencida, 14 d exactos no vencida, 14 d + 1 min vencida) y un usuario vencido que **comparte zona e inicio** con uno no vencido: el vencido sale vacío sin vaciar al otro. → Tarea 5 (borde) y Tarea 12 (zona compartida).
2. **Un escritorio 1.26.0 contra el API nuevo:** sin `contrato` la respuesta no trae ni una clave v2.2 y los golden v2.1 no cambian un byte; y la caché de `singleflight` no mezcla una petición v2.1 con una v2.2 simultánea. → Tarea 11.
3. **Horario que no toca la actividad** (toda antes, toda después, pausa en curso que cruza el fin del horario): horas activas 0, nunca negativas, y pagos/importes idénticos con y sin horario. → Tarea 6.
4. **Respuestas que llegan fuera de orden en el toggle Hoy↔Semana** (la de Hoy llega después de la de Semana): nunca se pinta la cifra de otro periodo. → Tarea 20 (ficha) y Tarea 19 (tablero).
5. **Preferencias guardadas de versiones anteriores:** el chip del teléfono guardado **por ordinal** (`ListaDeClientesViewModel.kt:186-190`, clave `pagos_lista_segmento`) y las columnas de Actividad `…columnas.v1` del escritorio: ninguna restaura un valor equivocado. → Tarea 16 y Tarea 22.

---

## Mapa de archivos

**msp-api** (`/Volumes/M2-1TB/Developer/msp-api`, base `main` @ `e1762f5`)

| Archivo | Responsabilidad | Tarea |
|---|---|---|
| `docs/superpowers/specs/2026-10-14-desempeno-contratos.md` | Contrato v2.2 (ley) | 0 |
| `scripts/medicion/desempeno-pulido2/*.sh`, `*.jq`, `*.sql` | Mediciones solo lectura | 1, 2 |
| `docs/evidencia/infra.md` | E-INF-018…023 | 1, 2, 4, 10, 12, 14 |
| `internal/desempeno/domain/columna.go` (+`_test`) | `ColumnaDe` | 4 |
| `internal/desempeno/domain/semana_vencida.go` (+`_test`), `calendario.go`, `errors.go` | Semana vencida, días navegables, errores nuevos | 5 |
| `internal/desempeno/domain/eje.go` (+`_test`), `jornada.go` | Recorte al horario, eje, `por_hora` de 24 h | 6 |
| `migrations-firebird/000066_*`, `internal/config/**`, `internal/config/config_contracts.go` | Zonas excluidas | 7 |
| `internal/desempeno/infra/desempenohttp/auth.go` | Cancelación ≠ 503 | 8 |
| `testdata/estado-cuenta/*`, `internal/desempeno/domain/casos_compartidos_test.go` | Casos con `hoy` y `columna` | 9 |
| `internal/desempeno/domain/cobro.go` | `pct = null` con `esperado = 0` | 10 |
| `internal/desempeno/app/{contrato,tablero,service}.go`, `ports/outbound/ports.go`, `infra/clients/config_zonas_client.go`, `infra/desempenohttp/{dto_v22,mapper_v22,handlers,routes}.go`, `cmd/api/desempeno_wiring.go` | Tablero v2.2 + exclusión | 11 |
| `internal/desempeno/app/movimientos.go`, `infra/desempenofb/movimientos_repo.go`, `migrations-firebird/000067_*` (sólo si la medición lo pide) | Vencidos fuera del lote + `ultimo_movimiento` | 12 |
| `internal/desempeno/app/{usuario,jornada,cuentas,actividad}.go`, http | Ficha v2.2 | 13 |
| `internal/desempeno/infra/desempenofb/{equivalencia_tablero,medicion_tablero}_test.go` | Arnés y rendimiento | 14 |

**sistema-cobro-web** (base: rama `fix/mapas-y-permiso-fotos` @ `5d727f7` = `feat/desempeno` 1.26.0 + arreglo de la llave de mapas; rama nueva `feat/desempeno-pulido-2`). Todo bajo `src/modules/desempeno/` salvo indicación.

| Archivo | Tarea |
|---|---|
| `infrastructure/http/{dtos,params,HttpDesempenoAdapter}.ts`, `infrastructure/mappers/dtoADominio.ts`, `domain/entities/*`, `domain/values/estadoCuenta.ts`, `application/ports/DesempenoPort.ts`, `application/__tests__/fakeDesempenoPort.ts`, `src/test/msw/{fixtures,handlers}/desempeno.ts`, `components/cuentas/CuentasTab.tsx` | 17 |
| `components/cuentas/TablaCuentas.tsx` | 18 |
| `components/tablero/{TableroScreen,TableroFila,TarjetasTablero,ordenTablero,FilaVencida,FilaEsqueleto}.tsx` | 19 |
| `components/DesempenoScreen.tsx`, `components/ficha/{FichaScreen,FichaEncabezado,FichaEsqueleto,AvisoSemanaVencida}.tsx`, `presentation/hooks/useUsuario.ts`, `components/lib/format.ts` | 20 |
| `components/lib/escala.ts`, `components/jornada/{EjeJornada,JornadaTab,NavegadorDia}.tsx`, `components/linea/LineaActividad.tsx` | 21 |
| `components/comunes/ControlHorario.tsx`, `presentation/actividadPreferencias.ts` | 22 |
| `src/modules/configuracion/**` (pestaña "Desempeño") | 23 |

**msp-app-kt** (base: rama `feat/estado-cuenta-casos-compartidos` @ `083c0d61` = `main` 2.20.0 + runner de casos E-APP-057; ese runner **no** está en `main`). Rama nueva `feat/lista-tres-columnas`.

| Archivo | Tarea |
|---|---|
| `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/{TratoDelEstado,ColumnaDeCobranza}.kt`, `core/common/src/test/kotlin/.../cobranza/domain/casos/{CasoDeEstadoCuenta,EstadoCuentaCasosCompartidosTest}.kt`, `core/common/src/test/resources/estado-cuenta/*` | 15 |
| `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/{EstadoCuentaUi,SegmentoDeCobranza→ColumnaDeLaLista,CarteraEnPantalla,ListaDeClientesViewModel,ListaDeClientesScreen,ScrollPorSegmento}.kt`, `ui/components/PiezasDeLaLista.kt`, `app/src/main/java/com/example/msp_app/di/MenuLateralModule.kt`, pruebas y goldens | 16 |

## Fases, dependencias y paralelismo

```
Fase 0  T0 contrato v2.2 ──► aprobación del dueño
Fase 1  T1 medición prod (M1,M2,M6) ─┐   T2 medición M3/M4 (no bloquea)
                                     └► T3 compuerta ANTES (agente independiente)
Fase 2  API  ┌ T4 ColumnaDe ──► T9 casos compartidos v2 ──────────────► (Fase 3)
  (grupo A,  ├ T5 semana vencida
  paralelo)  ├ T6 horario/eje
             ├ T7 config zonas excluidas
             ├ T8 cancelación ≠ 503
             └ T10 pct null  (sólo tras T3 = SOSTIENE para M2)
             T11 tablero v2.2 (tras T4,T6,T7,T8) ─► T12 vencidos + último movimiento (tras T5,T11,T3/M6)
             ─► T13 ficha v2.2 ─► T14 arnés + rendimiento + compuerta DESPUÉS
Fase 3  Android (tras T9): T15 core ─► T16 lista 3 columnas
Fase 4  Escritorio (tras T0): T17 adaptador v2.2 ─► { T18, T20, T21, T22 } en paralelo ─► T19 (tras T21)
                              T22 y T23 no dependen de T17 (T23 sí de T7)
Fase 5  T24 versiones, notas de despliegue, cierre de compuertas
```

**Paralelo seguro (worktrees separados):**
- msp-api grupo A: T4, T5, T6, T7, T8 (archivos disjuntos; T5 es el único que toca `domain/errors.go` y `calendario.go`; T6 el único que toca `jornada.go`; T7 no toca `internal/desempeno`). T10 en paralelo con todos cuando su compuerta lo permita (sólo `cobro.go`). T9 en paralelo con T5–T8 después de T4.
- Escritorio: T17 sola primero; luego T18, T20, T21 y T22 a la vez; T19 después de T21 (los dos tocan `TableroScreen.tsx`); T23 en cualquier momento tras T7 (módulo `configuracion`, disjunto).
- Android: T15 → T16 en serie, en paralelo con todo el escritorio y con T10–T14.
- **No paralelo:** T11 → T12 → T13 → T14 (todos tocan `app/tablero.go`, `desempenohttp/*`).

Worktrees: `git -C <repo> worktree add ../<repo>-<tarea> -b <rama> <base>`. Ramas por tarea de msp-api: `feat/desempeno-p2-tNN` desde `main`; se integran en `feat/desempeno-pulido-2` en el orden de las fases. En msp-api, copiar `.env` del checkout principal al worktree antes de pruebas de Firebird (`cp /Volumes/M2-1TB/Developer/msp-api/.env <worktree>/`). Antes de crear cada worktree: `git worktree list` (los `.wt-*` y `msp-api-*` existentes son de otros agentes; no tocarlos).

**Conflictos conocidos con ramas ajenas (decirlo al integrar, no resolver por cuenta propia):**
- msp-app-kt `feat/badges-blancos-sin-al-corriente` toca `feature/pagos/.../ui/EstadoCuentaUi.kt` y los goldens `pagos_lista_dark_1_0.png`, `pagos_lista_dark_1_5.png`: la Tarea 16 los cambia también. Orden de fusión: decisión del dueño (ver "Para el dueño").
- msp-app-kt `fix/precios-orden-y-contado` toca `core/common/build.gradle.kts`: la Tarea 15 no lo toca.
- msp-api `fix/precios-orden-contado` agrega E-INF-017 en `docs/evidencia/infra.md`: sólo choque de texto al final del archivo.

---

## Fase 0

### Task 0: Contrato v2.2 en el archivo de contratos

**Files:**
- Modify: `docs/superpowers/specs/2026-10-14-desempeno-contratos.md` (cabecera y §0–§5, §7; agregar §8 "v2.2")

**Interfaces:**
- Consumes: spec §6.
- Produces: el texto que las Tareas 11, 13 y 17 copian **a mano** en sus pruebas de claves (`clavesUsuarioV22`, etc.).

- [ ] **Step 1: Escribir la cabecera de versión**

Agregar como primera línea de versiones:

```markdown
Versión 2.2 (2026-10-16): parámetro `contrato=2.2` en todo `/v2/desempeno/usuarios*`; sin él se sirve v2.1 congelado. Tres columnas (`cobertura_por_columna`, `columna`), semana vencida, `ultimo_movimiento`, `promedio_diario`, `hoy`, `eje`, `horario`, condonaciones con importe, `GET /v2/desempeno/usuarios/{uid}`. Cambios compatibles que también aplican a v2.1: zonas excluidas (una fila menos) y `cobro.pct = null` con `esperado = 0`.
```

- [ ] **Step 2: Agregar la sección §8 "Contrato v2.2" copiando literalmente el spec §6.2** (los cuatro bloques `jsonc`, las reglas de `cobertura_por_columna`, `semana_vencida ⇒ …`, `promedio_diario = null en hoy`), más estas precisiones que el spec deja implícitas y que las pruebas fijan:

```markdown
- `contrato` acepta `2.1` y `2.2`; cualquier otro valor → 422 `desempeno_parametro_invalido`, `location: query.contrato`.
- `GET /v2/desempeno/usuarios/{uid}` exige `contrato=2.2` (sin él → 422 `query.contrato`): no existe en v2.1.
- `ultimo_movimiento = { "id": "pago:…" | "visita:…", "tipo": "pago" | "condonacion" | "visita", "hora": "...Z" } | null`.
- `promedio_diario.minutos_activos` entero redondeado (mitad hacia arriba); `pagos_importe` string de dinero con 2 decimales; `visitas` número JSON con 2 decimales.
- `eje = { "desde_hora": int 0–23, "hasta_hora": int 1–24 }`; en el tablero uno solo para todas las filas; en la jornada y en la fila por usuario el de esa respuesta.
- `horario = { "desde_hora": "HH:MM", "hasta_hora": "HH:MM" } | null` (eco de la consulta, jornada v2.2).
- En v2.2 `cuentas` rechaza `segmento` (422 `query.segmento`); usa `columna`.
- Con horario, en v2.2, `minutos_activos` y `pausas {n, minutos}` cuentan sólo lo que cae dentro del horario; `linea.pausas` y la señal `pausa_larga` conservan la pausa completa.
- Errores nuevos: `desempeno_zona_excluida` (409, todo endpoint por usuario, v2.1 y v2.2), `desempeno_semana_vencida` (409, sólo `cuentas` v2.2).
```

- [ ] **Step 3: En §2 y §5 (v2.1) anotar las dos únicas excepciones compatibles** ("una fila menos por zona excluida", "`pct` null con `esperado` 0") y el retiro de v2.1 (spec §6.3).

- [ ] **Step 4: Verificar que no queda la palabra prohibida ni marcas pendientes**

Run: `grep -nE 'ciclo|TBD|TODO' docs/superpowers/specs/2026-10-14-desempeno-contratos.md`
Expected: sin salida.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-10-14-desempeno-contratos.md
git commit -m "docs(desempeno): contrato v2.2 por parametro con v2.1 congelado"
```

- [ ] **Step 6: Aprobación.** El orquestador presenta el diff al dueño. **Ninguna tarea que dependa del contrato (11, 13, 17) empieza sin su aprobación** (spec §9.1).

---

## Fase 1 — Mediciones (§8, antes de cambiar)

### Task 1: Medición en producción, solo lectura — M1, M2, M6 (y la parte prod de M3)

**Files:**
- Create: `scripts/medicion/desempeno-pulido2/capturar.sh`
- Create: `scripts/medicion/desempeno-pulido2/analizar.jq`
- Modify: `docs/evidencia/infra.md` (entradas E-INF-018 parte "distribución", E-INF-019, E-INF-020, E-INF-021 parte prod)

**Interfaces:**
- Consumes: API de producción v2.1 (`GET /v2/desempeno/usuarios?periodo=semana`, `GET /v2/desempeno/usuarios/{uid}/cuentas`), token del dueño en `MSP_TOKEN` (lo pone el dueño; el agente nunca lo lee ni lo pide).
- Produces: archivos de salida **fuera del repo** en `~/Documents/msp-gestion/mediciones/2026-10-16-desempeno/` (contienen nombres) y cifras agregadas (sin nombres de clientes) en las entradas de evidencia.

- [ ] **Step 1: Escribir `capturar.sh`** (sólo GET; falla si falta el token; nunca imprime el token)

```bash
#!/usr/bin/env bash
# Medición solo lectura para docs/superpowers/plans/2026-10-16-desempeno-pulido-2.md, Tarea 1.
# La corre EL DUEÑO con su token:  MSP_TOKEN=... API=https://apidev.loclx.io ./capturar.sh
# Sólo hace GET a /v2/desempeno (contrato v2.1 vigente). Escribe en $SALIDA (fuera del repo).
set -euo pipefail
: "${MSP_TOKEN:?falta MSP_TOKEN}"
: "${API:?falta API}"
SALIDA="${SALIDA:-$HOME/Documents/msp-gestion/mediciones/2026-10-16-desempeno}"
mkdir -p "$SALIDA"
get() { curl -fsS --max-time 120 -H "Authorization: Bearer $MSP_TOKEN" "$API$1"; }
date -u +%FT%TZ > "$SALIDA/fecha.txt"
get "/v2/desempeno/usuarios?periodo=semana" > "$SALIDA/tablero_semana.json"
get "/v2/desempeno/usuarios?periodo=hoy"    > "$SALIDA/tablero_hoy.json"
jq -r '.usuarios[] | select(.configurado) | .uid' "$SALIDA/tablero_semana.json" | while read -r uid; do
  get "/v2/desempeno/usuarios/$uid/cuentas" > "$SALIDA/cuentas_$uid.json" || echo "$uid" >> "$SALIDA/cuentas_fallidas.txt"
  sleep 1
done
echo "listo: $SALIDA"
```

- [ ] **Step 2: Escribir `analizar.jq`** (todas las cifras que piden M1, M2, M6 y M3-prod)

```jq
# Uso: jq -n -f analizar.jq --slurpfile t tablero_semana.json --slurpfile c <(jq -s . cuentas_*.json) --arg ahora "$(cat fecha.txt)"
def dias(a; b): ((b | fromdateiso8601) - (a | fromdateiso8601)) / 86400;
def columna(c):
  if (c.trato == "pagado" or c.trato == "parcial") then "pagados"
  elif c.trato == "diferido" then "REVISAR_FECHA"   # el servidor de hoy no tiene fecha de promesa: debe salir 0
  else "por_visitar" end;
($t[0].usuarios) as $us
| {
  m1_distribucion_columnas: ([$c[0][] | .cuentas[] | columna(.)] | group_by(.) | map({(.[0]): length}) | add),
  m1_pares_trato_segmento: ([$c[0][] | .cuentas[] | "\(.trato)|\(.segmento)"] | group_by(.) | map({(.[0]): length}) | add),
  m2_esperado_cero_pct_no_nulo: [$us[] | select(.configurado and .cobro.esperado == "0.00" and .cobro.pct != null)
       | {zona_id, zona_nombre, pct: .cobro.pct}],
  m2_control_esperado_positivo: ([$us[] | select(.configurado and .cobro.esperado != "0.00")] | length),
  m6_vencidos: [$us[] | select(.configurado and .inicio_semana != null and dias(.inicio_semana; $ahora) > 14)
       | {uid, zona_id, zona_nombre, dias: (dias(.inicio_semana; $ahora) | floor),
          actividad_propia_ultimos_14d: ([.lineas[] | select(dias(.dia + "T12:00:00Z"; $ahora) <= 14) | .puntos | length] | add // 0)}],
  m3_largo_ventana_dias: ([$us[] | select(.configurado and .inicio_semana != null) | dias(.inicio_semana; $ahora) | floor]
       | group_by(.) | map({(.[0] | tostring): length}) | add),
  m3_cobrado_sobre_esperado: [$us[] | select(.configurado and .cobro.esperado != "0.00")
       | {zona_nombre, dias: (dias(.inicio_semana; $ahora) | floor),
          razon: (((.cobro.cobrado | tonumber) / (.cobro.esperado | tonumber)) * 100 | floor)}]
       | sort_by(-.razon)
}
```

- [ ] **Step 3: Control positivo del análisis en dev (el agente lo corre)**

Levantar el API local (`make dev`, token `dev:<uid>:<email>` según memoria `reference_msp_api_devmode_auth_dev_db_state`) y correr `capturar.sh` + `analizar.jq` contra `http://localhost:3011`. Expected: `m1_pares_trato_segmento` contiene `parcial|pagados` (E-INF-016 vigente) y ningún `diferido|*` (el servidor no tiene fecha de promesa); `m6_vencidos` no vacío con el reloj de dev; `m3_cobrado_sobre_esperado` reproduce un valor ≥ 300 (el 330 % medido en dev). Si cualquiera de los tres no aparece, el análisis está mal: corregir antes de pedir la corrida en producción. Pegar la salida en el reporte de la tarea (sin nombres).

- [ ] **Step 4: Paso humano — corrida en producción.** Entregar al dueño, tal cual:

```text
cd /Volumes/M2-1TB/Developer/msp-api/scripts/medicion/desempeno-pulido2
MSP_TOKEN='<su token de Firebase>' API='https://apidev.loclx.io' ./capturar.sh
```

Esperar la confirmación del dueño ("listo: …").

- [ ] **Step 5: Analizar y registrar**

Run: `cd ~/Documents/msp-gestion/mediciones/2026-10-16-desempeno && jq -n -f /Volumes/M2-1TB/Developer/msp-api/scripts/medicion/desempeno-pulido2/analizar.jq --slurpfile t tablero_semana.json --slurpfile c <(jq -s . cuentas_*.json) --arg ahora "$(cat fecha.txt)" > analisis.json`

Escribir en `docs/evidencia/infra.md` (cifras agregadas, sin nombres de clientes; nombres de cobradores sólo si el dueño lo autoriza):
- **E-INF-018 · Tres columnas: misma lista en teléfono y escritorio** — sección "Distribución en producción (MEDIDO prod 2026-10-16, v2.1)": conteo por columna proyectada y por par `trato|segmento`. Estado inicial: `EN MEDICIÓN`.
- **E-INF-019 · `pct` es null cuando no hay nada esperado** — usuarios con `esperado = 0` y `pct` no nulo, a qué zonas pertenecen (¿sólo 23301?), y el control (cuántos con `esperado > 0`).
- **E-INF-020 · Semana vencida (> 14 d) fuera del lote del tablero** — cuántos usuarios cumplen > 14 días y, de ellos, cuántos tienen actividad propia en los últimos 14 días. **Si alguno la tiene, se detiene la Tarea 12 y se reporta al dueño con el uid y el número de puntos antes de seguir** (spec §7 M6).
- **E-INF-021 · Cobrado sobre ventanas de varios días** — distribución del largo de ventana y de `cobrado/esperado` en producción. Estado: `EN MEDICIÓN` (la completa la Tarea 2).

- [ ] **Step 6: Commit** (scripts y evidencia; las salidas crudas NO van al repo)

```bash
git add scripts/medicion/desempeno-pulido2/capturar.sh scripts/medicion/desempeno-pulido2/analizar.jq docs/evidencia/infra.md
git commit -m "docs(desempeno): mediciones solo lectura de columnas, pct y semanas vencidas en produccion (E-INF-018, E-INF-019, E-INF-020, E-INF-021)"
```

### Task 2: Medición M3 (cobrado en ventanas largas) y M4 (Patricio de la Luz, R/03) — sólo medir

**Files:**
- Create: `scripts/medicion/desempeno-pulido2/m4-desglose.sh`
- Modify: `docs/evidencia/infra.md` (E-INF-021 completa, E-INF-022 nueva)

**Interfaces:**
- Consumes: `GET /v2/rutas/usuarios/{uid}/cobranza` (rutas, `internal/rutas/infra/rutashttp/routes.go:92`) y `GET /v2/desempeno/usuarios?periodo=semana`; `TestMedirTablero` con `MEDIR_UNO_VIEJO=1` (`internal/desempeno/infra/desempenofb/medicion_tablero_test.go:282,299`).
- Produces: **ningún cambio de código**. Dos entradas de evidencia con la medición y su control positivo; la decisión de M3 (DA-4) queda para el dueño con los números en la mano.

- [ ] **Step 1: Reproducir el 330 % en dev con su consulta**

Run: `cd <worktree> && set -a && . ./.env && set +a && go clean -testcache && MEDIR_TABLERO=1 MEDIR_UNO_VIEJO=1 go test -timeout 1800s -count=1 -run TestMedirTablero -v ./internal/desempeno/infra/desempenofb/`
Expected: la salida lista la fila del usuario viejo; anotar `cobrado`, `esperado`, días de ventana y número de ventas. Control positivo: el mismo test sin `MEDIR_UNO_VIEJO` da filas con `cobrado/esperado ≤ 150 %`.

- [ ] **Step 2: Comparar contra Cobranza semanal.** Para el mismo usuario y reloj, llamar a rutas (`CobranzaDeUsuario`) desde una prueba de medición nueva en el mismo archivo, `TestMedirCobradoContraRutas` (se salta sin `MEDIR_TABLERO`), que imprime `Σ AbonoSemana`, `Σ Parcialidad (AplicaPonderado)` y `PctPonderado` de rutas junto a `Cobro` de desempeño:

```go
//nolint:paralleltest // lee la base compartida; corre sola.
func TestMedirCobradoContraRutas(t *testing.T) {
	if os.Getenv("MEDIR_TABLERO") == "" {
		t.Skip("MEDIR_TABLERO no definido")
	}
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	rutasSvc := rutasapp.NewService(rutasfb.NewRutasRepo(pool), rutasfb.NewCobranzaRepo(pool), nil)
	for _, u := range usuariosDeMedicion(t, pool) { // helper existente del archivo; si no existe con ese nombre, usar el que arma los 55 usuarios
		cu, err := rutasSvc.CobranzaDeUsuario(context.Background(), u)
		require.NoError(t, err)
		var abono, esperado decimal.Decimal
		for _, v := range cu.Ventas {
			abono = abono.Add(v.AbonoSemana)
			if v.AplicaPonderado {
				esperado = esperado.Add(v.Parcialidad)
			}
		}
		t.Logf("uid=%s zona=%d dias=%.1f rutas_abono=%s rutas_esperado=%s rutas_pct=%v",
			u.UID, u.ZonaID, medAhora.Sub(*u.InicioSemana).Hours()/24, abono.StringFixed(2), esperado.StringFixed(2), cu.PctPonderado)
	}
}
```

(Antes de escribirla, leer `medicion_tablero_test.go:1-120` para usar los nombres reales del reloj y del armado de usuarios; los campos de `rutas.CobranzaUsuario` se leen en `internal/rutas/rutas_contracts.go`.) Expected: las cifras de rutas coinciden con las de desempeño para el mismo usuario (D-API-1); eso demuestra que un arreglo sólo en desempeño **divergiría** de Cobranza semanal.

- [ ] **Step 3: M4 en producción — paso humano.** `m4-desglose.sh` (sólo GET) descarga para el uid de Patricio de la Luz (R/03) y para un control cuyas cifras sí cuadran (el dueño elige cuál):

```bash
#!/usr/bin/env bash
# M4 (spec §7): la corre el dueño. MSP_TOKEN=... API=... UID_PATRICIO=... UID_CONTROL=... ./m4-desglose.sh
set -euo pipefail
: "${MSP_TOKEN:?}"; : "${API:?}"; : "${UID_PATRICIO:?}"; : "${UID_CONTROL:?}"
SALIDA="${SALIDA:-$HOME/Documents/msp-gestion/mediciones/2026-10-16-desempeno}"
get() { curl -fsS --max-time 120 -H "Authorization: Bearer $MSP_TOKEN" "$API$1"; }
for u in "$UID_PATRICIO" "$UID_CONTROL"; do
  get "/v2/rutas/usuarios/$u/cobranza" > "$SALIDA/m4_rutas_$u.json"
  get "/v2/desempeno/usuarios/$u/cuentas" > "$SALIDA/m4_cuentas_$u.json"
done
get "/v2/desempeno/usuarios?periodo=semana" > "$SALIDA/m4_tablero.json"
```

- [ ] **Step 4: Recomputar a mano** `pct` (promedio de aportes por venta, `internal/rutas/domain/desglose.go:48-65`) y `falta = max(0, esperado − cobrado)` (`domain/cobro.go:30`) para los dos usuarios, en una tabla por venta (folio sin nombre de cliente, parcialidad, abono, aporte, aplica). Confirmar o refutar la hipótesis INFERIDA del spec (un sobrepago en una venta lleva `falta` a 0 sin subir el promedio). El control debe cuadrar; si no cuadra, el procedimiento está mal y no se concluye nada.

- [ ] **Step 5: Registrar** E-INF-021 (completa: dev 330 % reproducido, prod distribución, igualdad con rutas; opciones del spec §7 M3 listadas sin elegir; Estado `MEDIDO`, decisión: dueño) y **E-INF-022 · % cobro y "por cobrar" de Patricio de la Luz (R/03)** (MEDIDO prod con control, o REFUTADA la hipótesis con qué la volvería cierta). **Ningún cambio de código** sale de esta tarea.

- [ ] **Step 6: Commit**

```bash
git add scripts/medicion/desempeno-pulido2/m4-desglose.sh internal/desempeno/infra/desempenofb/medicion_tablero_test.go docs/evidencia/infra.md
git commit -m "test(desempeno): mide cobrado en ventanas largas contra rutas y el caso R/03 (E-INF-021, E-INF-022)"
```

- [ ] **Step 7: Entregar al dueño** las opciones de M3 con sus cifras (spec §7: esperado proporcional, cobrado limitado a 7 días, o sólo rotular) y la conclusión de M4. Si el dueño elige una opción, se escribe **otro** plan con su propia compuerta antes; este plan no la implementa.

### Task 3: Compuerta ANTES (agente independiente) para M1, M2, M6

**Files:** ninguno de código. Resultado en `docs/evidencia/infra.md` (línea "Compuerta antes" de E-INF-018, 019, 020).

**Interfaces:**
- Consumes: el repo y las salidas de la Tarea 1 (ruta del directorio de mediciones). **No** recibe las conclusiones de la Tarea 1.
- Produces: veredicto por afirmación: SOSTIENE / CONTRADICE / FALTA EVIDENCIA / PARCIAL. **Todo lo que no sea SOSTIENE detiene la tarea que depende** (T4/T9/T11 para M1 en lo que toca el teléfono y el escritorio, T10 para M2, T12 para M6).

- [ ] **Step 1: Despachar un agente nuevo** (no el autor de T1, no uno que ya haya hecho una compuerta de este plan) con exactamente estas preguntas:

```text
Repos: /Volumes/M2-1TB/Developer/msp-api (main), /Volumes/M2-1TB/Developer/msp-app-kt (feat/estado-cuenta-casos-compartidos).
Mediciones: ~/Documents/msp-gestion/mediciones/2026-10-16-desempeno/ (no las modifiques).
Responde cada pregunta con el nivel de evidencia (MEDIDO/LEÍDO/INFERIDO/SUPUESTO), archivo:línea o comando con salida, y un veredicto.
1. ¿En qué segmento (v2.1) cae hoy cada uno de los 8 tratos en el servidor y en el teléfono? ¿Dónde difieren?
2. Con la regla "diferido con fecha futura → no_paga; pagado/parcial → pagados; todo lo demás → por_visitar", ¿qué cuentas cambian de lugar respecto de v2.1 en el servidor y en el teléfono? ¿Puede alguna quedar sin columna?
3. ¿Puede hoy el servidor producir trato "diferido" o "cita" con fecha? ¿De dónde saldría la fecha?
4. ¿Qué usuarios de producción tienen esperado = 0 y pct no nulo? ¿Por qué rutas produce un pct con esperado 0? ¿Algún usuario con esperado > 0 cambiaría si pct se vuelve null sólo con esperado = 0?
5. ¿Cuántos usuarios de producción tienen inicio de semana > 14 días? ¿Alguno tiene actividad propia en los últimos 14 días? ¿Con qué consulta lo comprobaste y cuál es tu control positivo?
6. ¿Qué consumidores tiene SegmentoDe en msp-api y SegmentoDeCobranza en msp-app-kt? Enuméralos con grep y di qué patrón usaste.
```

- [ ] **Step 2: Anotar** el veredicto en cada entrada. Si alguna no es SOSTIENE, reportarlo al dueño y **no** arrancar la tarea dependiente.

---

## Fase 2 — API (`msp-api`)

### Task 4: `ColumnaDe` en el dominio (D18, E-INF-018)

**Files:**
- Create: `internal/desempeno/domain/columna.go`
- Test: `internal/desempeno/domain/columna_test.go`

**Interfaces:**
- Consumes: `domain.TratoDe(ResultadoCuenta) Trato` (`presentacion.go:25`), `Fecha.Despues` (`calendario.go:76`).
- Produces:
  ```go
  type Columna string
  const ColumnaPorVisitar Columna = "por_visitar"; ColumnaNoPaga Columna = "no_paga"; ColumnaPagados Columna = "pagados"
  func Columnas() []Columna
  func ParseColumna(s string) (Columna, error)
  func ColumnaDe(r ResultadoCuenta, hoy Fecha) Columna
  ```
  `SegmentoDe` (`presentacion.go:121`) **no se toca** (v2.1 congelado).

- [ ] **Step 1: Escribir la prueba que falla** (`columna_test.go`)

```go
package domain_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func fecha(t *testing.T, s string) *domain.Fecha {
	t.Helper()
	f, err := domain.ParseFecha(s)
	require.NoError(t, err)
	return &f
}

func horaCita(t *testing.T, s string) *domain.HoraDelDia {
	t.Helper()
	h, err := domain.ParseHoraDelDia(s)
	require.NoError(t, err)
	return &h
}

// E-INF-018: la tabla de spec §3.1, estado por estado y fecha por fecha.
func TestColumnaDe_E_INF_018_TablaDelSpec(t *testing.T) {
	t.Parallel()
	hoy := *fecha(t, "2026-10-15")
	fechas := map[string]*domain.Fecha{
		"vencida": fecha(t, "2026-10-14"), "hoy": fecha(t, "2026-10-15"), "futura": fecha(t, "2026-10-16"), "nula": nil,
	}
	for nombreFecha, f := range fechas {
		for _, e := range domain.EstadosCuenta() {
			r := domain.ResultadoCuenta{Estado: e, FechaPromesa: f, FechaCita: f, HoraCita: horaCita(t, "10:00")}
			got := domain.ColumnaDe(r, hoy)
			var want domain.Columna
			switch {
			case e == domain.EstadoPago || e == domain.EstadoAbonoParcial:
				want = domain.ColumnaPagados
			case e == domain.EstadoPrometioProxima && nombreFecha == "futura":
				want = domain.ColumnaNoPaga
			default:
				want = domain.ColumnaPorVisitar
			}
			assert.Equal(t, want, got, "estado %s, fecha %s", e, nombreFecha)
		}
	}
}

// Las filas que cambian respecto de SegmentoDe (v2.1): se pinan una por una
// para que revertir a la regla de segmentos las ponga rojas.
func TestColumnaDe_E_INF_018_LoQueCambiaContraV21(t *testing.T) {
	t.Parallel()
	hoy := *fecha(t, "2026-10-15")
	casos := []struct {
		nombre string
		r      domain.ResultadoCuenta
		want   domain.Columna
	}{
		{"cita a futuro va a por visitar", domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, FechaCita: fecha(t, "2026-10-20"), HoraCita: horaCita(t, "16:30")}, domain.ColumnaPorVisitar},
		{"cita con hora sin fecha va a por visitar", domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, HoraCita: horaCita(t, "16:30")}, domain.ColumnaPorVisitar},
		{"se negó va a por visitar", domain.ResultadoCuenta{Estado: domain.EstadoSeNego}, domain.ColumnaPorVisitar},
		{"promesa a futuro no paga", domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-10-16")}, domain.ColumnaNoPaga},
		{"promesa de hoy va a por visitar", domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-10-15")}, domain.ColumnaPorVisitar},
		{"promesa sin fecha va a por visitar", domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima}, domain.ColumnaPorVisitar},
		{"abono parcial va a pagados", domain.ResultadoCuenta{Estado: domain.EstadoAbonoParcial}, domain.ColumnaPagados},
		{"no estaba va a por visitar", domain.ResultadoCuenta{Estado: domain.EstadoNoEstaba}, domain.ColumnaPorVisitar},
		{"estado desconocido va a por visitar", domain.ResultadoCuenta{Estado: "x"}, domain.ColumnaPorVisitar},
	}
	for _, c := range casos {
		assert.Equal(t, c.want, domain.ColumnaDe(c.r, hoy), c.nombre)
	}
}

// Invariante (spec §3.1): total y disjunta — toda cuenta cae en exactamente
// una de las tres, con estados y fechas al azar (semilla fija, 20 000 casos).
func TestColumnaDe_E_INF_018_TotalYDisjunta(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20261016)) //nolint:gosec // prueba determinista
	estados := append(domain.EstadosCuenta(), "desconocido")
	base := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	alAzar := func() *domain.Fecha {
		if rng.Intn(4) == 0 {
			return nil
		}
		f := domain.DiaDe(base.AddDate(0, 0, rng.Intn(21)-10), time.UTC)
		return &f
	}
	validas := map[domain.Columna]bool{}
	for _, c := range domain.Columnas() {
		validas[c] = true
	}
	for i := 0; i < 20000; i++ {
		hoy := domain.DiaDe(base.AddDate(0, 0, rng.Intn(7)-3), time.UTC)
		r := domain.ResultadoCuenta{Estado: estados[rng.Intn(len(estados))], FechaPromesa: alAzar(), FechaCita: alAzar()}
		if rng.Intn(2) == 0 {
			r.HoraCita = horaCita(t, "09:30")
		}
		got := domain.ColumnaDe(r, hoy)
		require.True(t, validas[got], "columna fuera de las tres: %q", got)
		trato := domain.TratoDe(r)
		require.Equal(t, trato == domain.TratoPagado || trato == domain.TratoParcial, got == domain.ColumnaPagados, "%+v", r)
		require.Equal(t, trato == domain.TratoDiferido && r.FechaPromesa.Despues(hoy), got == domain.ColumnaNoPaga, "%+v", r)
	}
}

func TestParseColumna(t *testing.T) {
	t.Parallel()
	for _, c := range domain.Columnas() {
		got, err := domain.ParseColumna(string(c))
		require.NoError(t, err)
		assert.Equal(t, c, got)
	}
	for _, malo := range []string{"", "sin_nada", "POR_VISITAR", "volver"} {
		_, err := domain.ParseColumna(malo)
		require.Error(t, err, malo)
	}
	assert.Equal(t, []domain.Columna{domain.ColumnaPorVisitar, domain.ColumnaNoPaga, domain.ColumnaPagados}, domain.Columnas())
}
```

- [ ] **Step 2: Correr y ver que falla**

Run: `go test ./internal/desempeno/domain/ -run 'TestColumnaDe|TestParseColumna' -count=1`
Expected: FAIL de compilación `undefined: domain.ColumnaDe`.

- [ ] **Step 3: Implementar** `columna.go`

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// Columna es la columna de la lista de cuentas en contrato v2.2 (D18,
// E-INF-018): la misma en el teléfono (msp-app-kt core/common
// ColumnaDeCobranza.kt) y en el escritorio. Reemplaza a Segmento, que se
// conserva sin cambios sólo para servir v2.1.
type Columna string

// Las tres columnas, en el orden en que se pintan.
const (
	ColumnaPorVisitar Columna = "por_visitar"
	ColumnaNoPaga     Columna = "no_paga"
	ColumnaPagados    Columna = "pagados"
)

// Columnas devuelve las tres en orden de pantalla.
func Columnas() []Columna { return []Columna{ColumnaPorVisitar, ColumnaNoPaga, ColumnaPagados} }

// ParseColumna valida un código del contrato v2.2.
func ParseColumna(s string) (Columna, error) {
	for _, c := range Columnas() {
		if string(c) == s {
			return c, nil
		}
	}
	return "", ErrParametroInvalido.WithField("parametro", "columna")
}

// ColumnaDe es total: toda cuenta cae en exactamente una columna. La pregunta
// de la lista es "¿a qué puerta voy?": todo lo que pide ir —negativas, citas
// de cualquier fecha, promesas vencidas o de hoy— va a por visitar; sólo la
// promesa con fecha posterior a hoy se aparta (no paga). Pagado y abono
// parcial van a pagados (la etiqueta del parcial sigue siendo "Abonó parcial").
// hoy es el día de negocio (America/Mexico_City). Decisión D18 del dueño,
// 2026-10-15; evidencia E-INF-018.
func ColumnaDe(r ResultadoCuenta, hoy Fecha) Columna {
	switch TratoDe(r) {
	case TratoPagado, TratoParcial:
		return ColumnaPagados
	case TratoDiferido:
		if r.FechaPromesa != nil && r.FechaPromesa.Despues(hoy) {
			return ColumnaNoPaga
		}
		return ColumnaPorVisitar
	}
	return ColumnaPorVisitar
}
```

- [ ] **Step 4: Correr y ver verde**

Run: `go clean -testcache && go test ./internal/desempeno/domain/ -count=1`
Expected: PASS (incluidas las pruebas existentes de `presentacion_test.go`, que no cambian).

- [ ] **Step 5: Prueba roja al revertir (control de la regla).** Reemplazar temporalmente el cuerpo de `ColumnaDe` por la regla v2.1 traducida (`SegmentoDe`: `sin_visitar`/`volver` → `por_visitar`, `despues` → `no_paga`, `pagados` → `pagados`, sin segmento → `por_visitar`):

```go
	seg, _ := SegmentoDe(r, hoy)
	switch seg {
	case SegmentoPagados:
		return ColumnaPagados
	case SegmentoDespues:
		return ColumnaNoPaga
	}
	return ColumnaPorVisitar
```

Run: `go test ./internal/desempeno/domain/ -run TestColumnaDe -count=1`
Expected: FAIL en "cita a futuro va a por visitar" y "se negó va a por visitar" (y en la tabla para `cita|futura`, `se_nego|*`). Pegar la salida en el reporte; luego restaurar el cuerpo del Step 3 (el archivo es nuevo: no hay versión en git a la cual volver) y volver a correr verde.

- [ ] **Step 6: Lint**

Run: `golangci-lint run ./internal/desempeno/...`
Expected: sin hallazgos.

- [ ] **Step 7: Commit**

```bash
git add internal/desempeno/domain/columna.go internal/desempeno/domain/columna_test.go
git commit -m "feat(desempeno): columna por cuenta en tres columnas para contrato v2.2 (E-INF-018)"
```

### Task 5: Semana vencida, días navegables y errores nuevos (D19, DA-2, D20)

**Files:**
- Create: `internal/desempeno/domain/semana_vencida.go`
- Test: `internal/desempeno/domain/semana_vencida_test.go`
- Modify: `internal/desempeno/domain/calendario.go` (agregar `MasDias`)
- Modify: `internal/desempeno/domain/errors.go` (agregar dos errores)

**Interfaces:**
- Produces:
  ```go
  const DiasSemanaVencida = 14
  func (u UsuarioCobranza) SemanaVencidaEn(ahora time.Time) bool
  func DiasNavegables(u UsuarioCobranza, ahora time.Time, loc *time.Location) []Fecha // nil si no hay inicio
  func (f Fecha) MasDias(n int) Fecha
  var ErrUsuarioSemanaVencida // 409 desempeno_semana_vencida
  var ErrZonaExcluida         // 409 desempeno_zona_excluida
  ```

- [ ] **Step 1: Prueba que falla**

```go
package domain_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

func usuarioConInicio(inicio time.Time) domain.UsuarioCobranza {
	return domain.UsuarioCobranza{UID: "uid-ivan", Nombre: "Iván Vázquez Aldama", ZonaID: 23301, InicioSemana: &inicio}
}

// E-INF-020: el borde es "más de 14 × 24 h", medido con el mismo ahora.
func TestSemanaVencida_E_INF_020_BordeExacto(t *testing.T) {
	t.Parallel()
	ahora := time.Date(2026, 10, 15, 18, 0, 0, 0, time.UTC)
	catorce := 14 * 24 * time.Hour
	assert.False(t, usuarioConInicio(ahora.Add(-catorce+time.Minute)).SemanaVencidaEn(ahora), "14 d − 1 min")
	assert.False(t, usuarioConInicio(ahora.Add(-catorce)).SemanaVencidaEn(ahora), "14 d exactos no es más de 14")
	assert.True(t, usuarioConInicio(ahora.Add(-catorce-time.Minute)).SemanaVencidaEn(ahora), "14 d + 1 min")
	assert.False(t, domain.UsuarioCobranza{ZonaID: 1}.SemanaVencidaEn(ahora), "sin inicio no es vencida")
	assert.Equal(t, 14, domain.DiasSemanaVencida)
}

func TestDiasNavegables_E_INF_020(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	ahora := time.Date(2026, 10, 16, 2, 0, 0, 0, time.UTC) // 15 oct 20:00 CDMX
	vencido := usuarioConInicio(ahora.AddDate(0, 0, -338))
	dias := domain.DiasNavegables(vencido, ahora, loc)
	require.Len(t, dias, 14, "vencida: los últimos 14 días de negocio, hoy incluido")
	assert.Equal(t, "2026-10-02", dias[0].String())
	assert.Equal(t, "2026-10-15", dias[13].String(), "el último es hoy de negocio, no el día UTC")

	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC) // jueves 9 08:46 CDMX
	normal := usuarioConInicio(inicio)
	dias = domain.DiasNavegables(normal, ahora, loc)
	require.Len(t, dias, 7)
	assert.Equal(t, "2026-10-09", dias[0].String())
	assert.Equal(t, "2026-10-15", dias[6].String())

	assert.Nil(t, domain.DiasNavegables(domain.UsuarioCobranza{ZonaID: 1}, ahora, loc))
}

func TestFecha_MasDias(t *testing.T) {
	t.Parallel()
	f := *fecha(t, "2026-03-01")
	assert.Equal(t, "2026-02-28", f.MasDias(-1).String())
	assert.Equal(t, "2026-03-15", f.MasDias(14).String())
	assert.Equal(t, f, f.MasDias(0))
}

func TestErroresNuevos_SonConflictosConSuCodigo(t *testing.T) {
	t.Parallel()
	for codigo, err := range map[string]error{
		"desempeno_semana_vencida": domain.ErrUsuarioSemanaVencida,
		"desempeno_zona_excluida":  domain.ErrZonaExcluida,
	} {
		var ae *apperror.Error
		require.True(t, errors.As(err, &ae), codigo)
		assert.Equal(t, codigo, ae.Code)
		assert.Equal(t, http.StatusConflict, ae.Kind.HTTPStatus())
	}
	var ae *apperror.Error
	require.True(t, errors.As(domain.ErrZonaExcluida, &ae))
	assert.Equal(t, "la zona del usuario está excluida de desempeño", ae.Message)
}
```

- [ ] **Step 2: Correr y ver que falla**

Run: `go test ./internal/desempeno/domain/ -run 'TestSemanaVencida|TestDiasNavegables|TestFecha_MasDias|TestErroresNuevos' -count=1`
Expected: FAIL `undefined: domain.DiasSemanaVencida`.

- [ ] **Step 3: Implementar**

`calendario.go`, después de `Siguiente`:

```go
// MasDias es el día calendario n días después (n negativo: antes).
func (f Fecha) MasDias(n int) Fecha {
	t := time.Date(f.anio, f.mes, f.dia+n, 0, 0, 0, 0, time.UTC)
	return Fecha{anio: t.Year(), mes: t.Month(), dia: t.Day()}
}
```

`errors.go`, al final del bloque de variables (copiar el estilo de `ErrUsuarioSinZona` del mismo archivo):

```go
	// ErrUsuarioSemanaVencida: Cuentas v2.2 de un usuario con inicio de semana
	// de más de 14 días (DA-2, E-INF-020).
	ErrUsuarioSemanaVencida = apperror.NewConflict(
		"desempeno_semana_vencida",
		"el inicio de semana del usuario tiene más de 14 días",
	)
	// ErrZonaExcluida: la zona del usuario está en
	// MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS (D20, DA-1).
	ErrZonaExcluida = apperror.NewConflict(
		"desempeno_zona_excluida",
		"la zona del usuario está excluida de desempeño",
	)
```

`semana_vencida.go`:

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "time"

// DiasSemanaVencida: un inicio de semana con MÁS de 14 × 24 h respecto de
// ahora es una semana abandonada (D19, E-INF-020). Sus cifras de 300 días no
// se pueden leer y su ventana costaba 44 s y 16.7 MB al tablero (MEDIDO dev).
const DiasSemanaVencida = 14

const duracionSemanaVencida = DiasSemanaVencida * 24 * time.Hour

// SemanaVencidaEn dice si el inicio de semana tiene más de 14 días respecto de
// ahora (el mismo ahora de toda la respuesta). Sin inicio no es vencida: es
// "sin configurar".
func (u UsuarioCobranza) SemanaVencidaEn(ahora time.Time) bool {
	return u.InicioSemana != nil && ahora.Sub(*u.InicioSemana) > duracionSemanaVencida
}

// DiasNavegables son los días que la ficha deja elegir: con semana vencida,
// los últimos 14 días de negocio hasta hoy (hoy incluido); si no, del día del
// inicio de semana a hoy (≤ 15 por definición). nil sin inicio de semana.
func DiasNavegables(u UsuarioCobranza, ahora time.Time, loc *time.Location) []Fecha {
	if u.InicioSemana == nil {
		return nil
	}
	hoy := DiaDe(ahora, loc)
	desde := DiaDe(*u.InicioSemana, loc)
	if u.SemanaVencidaEn(ahora) {
		desde = hoy.MasDias(-(DiasSemanaVencida - 1))
	}
	out := []Fecha{}
	for d := desde; !d.Despues(hoy); d = d.Siguiente() {
		out = append(out, d)
	}
	return out
}
```

- [ ] **Step 4: Verde** — Run: `go clean -testcache && go test ./internal/desempeno/domain/ -count=1` · Expected: PASS.

- [ ] **Step 5: Roja al revertir.** Cambiar `>` por `>=` en `SemanaVencidaEn` → `TestSemanaVencida_E_INF_020_BordeExacto` debe fallar en "14 d exactos"; cambiar `DiasSemanaVencida - 1` por `DiasSemanaVencida` → `TestDiasNavegables` debe fallar con 15 días. Restaurar y verde. Pegar ambas salidas.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/domain/semana_vencida.go internal/desempeno/domain/semana_vencida_test.go internal/desempeno/domain/calendario.go internal/desempeno/domain/errors.go
git commit -m "feat(desempeno): semana vencida de mas de 14 dias y dias navegables (E-INF-020)"
```

### Task 6: Horas activas recortadas al horario, eje y `por_hora` de 24 h (D21)

**Files:**
- Create: `internal/desempeno/domain/eje.go`
- Test: `internal/desempeno/domain/eje_test.go`, `internal/desempeno/domain/jornada_horario_test.go`
- Modify: `internal/desempeno/domain/jornada.go:31-36` (`ParametrosJornada`), `:178-200` (`CalcularJornada`), `:278-300` (`calcularPorHora`), `:302-347` (`resumir`)

**Interfaces:**
- Produces:
  ```go
  // ParametrosJornada gana dos banderas que sólo pone v2.2 (v2.1 las deja en false y no cambia un byte):
  type ParametrosJornada struct { PausaMin int; Horario *Horario; RecortarAlHorario bool; PorHoraDelDia bool }
  type Eje struct { DesdeHora, HastaHora int }
  var EjePorOmision = Eje{DesdeHora: 7, HastaHora: 20}
  func EjeDe(h *Horario, horas []time.Time, loc *time.Location) Eje
  func HorasDeLineas(ls []LineaDelDia) []time.Time
  func (l LineaDelDia) EnEje(e Eje) LineaDelDia // PorHora filtrado a [Desde, Hasta)
  func (h HoraDelDia) Hora() int
  func (h HoraDelDia) HoraArriba() int // ⌈h⌉ en horas: 18:00→18, 18:30→19
  ```

- [ ] **Step 1: Pruebas que fallan** — `jornada_horario_test.go` (usa los helpers de `jornada_test.go`; antes de escribir, leer `jornada_test.go:1-45` y reutilizar sus constructores de actividades y su `loc`; si se llaman distinto de `pagoA`/`visitaA` abajo, usar los reales):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func parametros(t *testing.T, desde, hasta string, recortar bool) domain.ParametrosJornada {
	t.Helper()
	p, err := domain.NuevosParametrosJornada("45", desde, hasta)
	require.NoError(t, err)
	p.RecortarAlHorario = recortar
	return p
}

// Spec §3.4 y §8.1: toda dentro, toda fuera, cruzando inicio, cruzando fin,
// pausa partida por el horario.
func TestHorasActivas_ConHorario(t *testing.T) {
	t.Parallel()
	loc := cdmxDominio(t)
	dia := *fecha(t, "2026-10-14")
	en := func(hhmm string) time.Time { return horaEn(t, dia, hhmm, loc) }
	acts := func(horas ...string) []domain.Actividad {
		out := []domain.Actividad{}
		for i, h := range horas {
			out = append(out, pagoA(t, "pago:"+h, en(h), 100+i))
		}
		return out
	}
	ahora := en("23:00")
	casos := []struct {
		nombre                 string
		acts                   []domain.Actividad
		desde, hasta           string
		minutos, pausas, pausaMin int
	}{
		{"toda dentro", acts("09:00", "10:00", "12:00"), "08:00", "18:00", 180 - 120, 1, 120},
		{"toda fuera, antes", acts("05:00", "06:00"), "08:00", "18:00", 0, 0, 0},
		{"toda fuera, después", acts("19:00", "20:00"), "08:00", "18:00", 0, 0, 0},
		{"cruza el inicio", acts("07:00", "07:30", "09:00"), "08:00", "18:00", 60 - 60, 1, 60},
		{"cruza el fin", acts("17:00", "17:20", "19:00"), "08:00", "18:00", 60 - 40, 1, 40},
		{"pausa partida por el inicio", acts("07:00", "08:30", "09:00"), "08:00", "18:00", 60 - 30, 1, 30},
	}
	for _, c := range casos {
		j := domain.CalcularJornada(c.acts, parametros(t, c.desde, c.hasta, true), dia, loc, ahora)
		assert.Equal(t, c.minutos, j.Resumen.MinutosActivos, c.nombre+": minutos activos")
		assert.Equal(t, c.pausas, j.Resumen.Pausas.N, c.nombre+": pausas que tocan el horario")
		assert.Equal(t, c.pausaMin, j.Resumen.Pausas.Minutos, c.nombre+": minutos de pausa recortados")
		assert.GreaterOrEqual(t, j.Resumen.MinutosActivos, 0, c.nombre)
		sin := domain.CalcularJornada(c.acts, parametros(t, c.desde, c.hasta, false), dia, loc, ahora)
		assert.Equal(t, sin.Resumen.Pagos, j.Resumen.Pagos, c.nombre+": el dinero nunca se recorta")
		assert.Equal(t, sin.Linea.Pausas, j.Linea.Pausas, c.nombre+": la línea dibuja la pausa real completa")
		assert.Equal(t, sin.Senales, j.Senales, c.nombre+": las señales no cambian")
	}
}

// v2.1 congelado: sin la bandera, con horario, nada cambia respecto de hoy.
func TestHorasActivas_SinBanderaEsLaReglaDeV21(t *testing.T) {
	t.Parallel()
	loc := cdmxDominio(t)
	dia := *fecha(t, "2026-10-14")
	en := func(hhmm string) time.Time { return horaEn(t, dia, hhmm, loc) }
	acts := []domain.Actividad{pagoA(t, "pago:a", en("07:00"), 1), pagoA(t, "pago:b", en("19:00"), 2)}
	j := domain.CalcularJornada(acts, parametros(t, "08:00", "18:00", false), dia, loc, en("23:00"))
	assert.Equal(t, 720-720, j.Resumen.MinutosActivos, "una pausa de 12 h entre las dos: 0 activos, como hoy")
	assert.Equal(t, 1, j.Resumen.Pausas.N)
	assert.Equal(t, 720, j.Resumen.Pausas.Minutos)
	require.Len(t, j.Linea.PorHora, 13, "v2.1: 7..19")
	assert.Equal(t, 7, j.Linea.PorHora[0].Hora)
}

// La pausa en curso con horario: cuenta sólo lo que cae dentro y nunca resta activos.
func TestHorasActivas_PausaEnCursoCruzaElFin(t *testing.T) {
	t.Parallel()
	loc := cdmxDominio(t)
	dia := *fecha(t, "2026-10-14")
	en := func(hhmm string) time.Time { return horaEn(t, dia, hhmm, loc) }
	acts := []domain.Actividad{pagoA(t, "pago:a", en("16:00"), 1), pagoA(t, "pago:b", en("17:00"), 2)}
	j := domain.CalcularJornada(acts, parametros(t, "08:00", "18:00", true), dia, loc, en("19:30"))
	assert.Equal(t, 60, j.Resumen.MinutosActivos)
	assert.Equal(t, 1, j.Resumen.Pausas.N)
	assert.Equal(t, 60, j.Resumen.Pausas.Minutos, "17:00–18:00 dentro; 18:00–19:30 fuera")
	assert.True(t, j.Resumen.Pausas.EnCurso)
}

func TestPorHora_DelDiaCompletoYEnEje(t *testing.T) {
	t.Parallel()
	loc := cdmxDominio(t)
	dia := *fecha(t, "2026-10-14")
	en := func(hhmm string) time.Time { return horaEn(t, dia, hhmm, loc) }
	acts := []domain.Actividad{pagoA(t, "pago:a", en("05:10"), 1), pagoA(t, "pago:b", en("21:40"), 2)}
	p := parametros(t, "", "", false)
	p.PorHoraDelDia = true
	j := domain.CalcularJornada(acts, p, dia, loc, en("23:00"))
	require.Len(t, j.Linea.PorHora, 24)
	assert.Equal(t, 1, j.Linea.PorHora[5].Pagos, "la de las 05:10 ya no se pierde")
	e := domain.EjeDe(nil, domain.HorasDeLineas([]domain.LineaDelDia{j.Linea}), loc)
	assert.Equal(t, domain.Eje{DesdeHora: 5, HastaHora: 22}, e)
	l := j.Linea.EnEje(e)
	require.Len(t, l.PorHora, 17, "[5, 22)")
	assert.Equal(t, 5, l.PorHora[0].Hora)
	assert.Equal(t, 21, l.PorHora[16].Hora)
}
```

`eje_test.go`:

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestEjeDe(t *testing.T) {
	t.Parallel()
	loc := cdmxDominio(t)
	dia := *fecha(t, "2026-10-14")
	en := func(hhmm string) time.Time { return horaEn(t, dia, hhmm, loc) }
	horario := func(d, h string) *domain.Horario {
		p, err := domain.NuevosParametrosJornada("", d, h)
		require.NoError(t, err)
		return p.Horario
	}
	casos := []struct {
		nombre string
		h      *domain.Horario
		horas  []time.Time
		want   domain.Eje
	}{
		{"sin nada: 7–20", nil, nil, domain.Eje{DesdeHora: 7, HastaHora: 20}},
		{"actividad dentro de 7–20 no lo cambia", nil, []time.Time{en("09:00"), en("18:59")}, domain.Eje{DesdeHora: 7, HastaHora: 20}},
		{"actividad a las 19:10 abre hasta 20", nil, []time.Time{en("19:10")}, domain.Eje{DesdeHora: 7, HastaHora: 20}},
		{"actividad a las 20:05 abre hasta 21", nil, []time.Time{en("20:05")}, domain.Eje{DesdeHora: 7, HastaHora: 21}},
		{"horario 05:30–22:30", horario("05:30", "22:30"), nil, domain.Eje{DesdeHora: 5, HastaHora: 23}},
		{"horario hasta 23:59 → 24", horario("00:00", "23:59"), nil, domain.Eje{DesdeHora: 0, HastaHora: 24}},
		{"actividad a las 23:30 → 24, nunca 25", nil, []time.Time{en("23:30")}, domain.Eje{DesdeHora: 7, HastaHora: 24}},
	}
	for _, c := range casos {
		assert.Equal(t, c.want, domain.EjeDe(c.h, c.horas, loc), c.nombre)
	}
	assert.Equal(t, domain.Eje{DesdeHora: 7, HastaHora: 20}, domain.EjePorOmision)
}
```

Agregar a `helpers_test.go` del paquete (si no existen con otro nombre): `cdmxDominio(t)`, `horaEn(t, dia Fecha, hhmm, loc)` y `pagoA(t, id, hora, ventaID)` que arma un pago aplicado de efectivo de 100.00 con `RegistradoPor{Origen: OrigenApp, CobradorID: 1}`.

- [ ] **Step 2: Correr y ver que falla** — Run: `go test ./internal/desempeno/domain/ -run 'TestHorasActivas|TestPorHora|TestEjeDe' -count=1` · Expected: FAIL `p.RecortarAlHorario undefined`.

- [ ] **Step 3: Implementar.**

En `jornada.go`, `ParametrosJornada`:

```go
type ParametrosJornada struct {
	PausaMin int
	Horario  *Horario
	// RecortarAlHorario (contrato v2.2, D21): con Horario, las horas activas y
	// el resumen de pausas cuentan sólo lo que cae dentro del horario. Pagos,
	// visitas, importes, la línea y las señales NUNCA se recortan. v2.1 lo
	// deja en false.
	RecortarAlHorario bool
	// PorHoraDelDia (contrato v2.2): por_hora cubre las 24 horas; el servicio
	// lo recorta al eje con LineaDelDia.EnEje. v2.1: 7..19.
	PorHoraDelDia bool
}
```

En `CalcularJornada`, reemplazar `PorHora: calcularPorHora(delDia, loc)` por `PorHora: calcularPorHora(delDia, loc, p.PorHoraDelDia)` y `j.Resumen = resumir(delDia, pausas)` por `j.Resumen = resumir(delDia, pausas, recorteDe(p, dia, loc))`.

```go
// recorte es el [desde, hasta) del horario en el día, o nil sin recorte.
type recorte struct{ desde, hasta time.Time }

func recorteDe(p ParametrosJornada, dia Fecha, loc *time.Location) *recorte {
	if !p.RecortarAlHorario || p.Horario == nil {
		return nil
	}
	return &recorte{desde: p.Horario.Desde.EnDia(dia, loc), hasta: p.Horario.Hasta.EnDia(dia, loc)}
}

// minutosDentro es |[a, b] ∩ recorte| en minutos (b − a si no hay recorte).
func (r *recorte) minutosDentro(a, b time.Time) int {
	if r == nil {
		return minutosEntre(a, b)
	}
	if a.Before(r.desde) {
		a = r.desde
	}
	if b.After(r.hasta) {
		b = r.hasta
	}
	if !b.After(a) {
		return 0
	}
	return minutosEntre(a, b)
}
```

`calcularPorHora(acts, loc, diaCompleto bool)`: si `diaCompleto`, horas 0..23; si no, 7..19 (las constantes actuales). Mismo cuerpo con `primera, ultima` como variables.

`resumir(acts, pausas, rc *recorte)`: el bucle de pausas pasa a

```go
	for _, p := range pausas {
		m := rc.minutosDentro(p.Desde, p.Hasta)
		if rc != nil && m == 0 {
			continue // con horario, una pausa que no toca el horario no cuenta
		}
		if rc == nil {
			m = p.Minutos
		}
		r.Pausas.N++
		r.Pausas.Minutos += m
		if p.EnCurso {
			r.Pausas.EnCurso = true
		} else {
			cerradas += m
		}
	}
	// Las pausas cerradas caen entre primera y última (y, recortadas, dentro de
	// la intersección con el horario): la resta nunca es negativa.
	r.MinutosActivos = rc.minutosDentro(primera, ultima) - cerradas
```

`eje.go`:

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "time"

// Eje es el rango de horas que pinta la línea del día (contrato v2.2, D21):
// [DesdeHora, HastaHora), horas de negocio enteras, 0 ≤ DesdeHora < HastaHora ≤ 24.
type Eje struct {
	DesdeHora int
	HastaHora int
}

// EjePorOmision es el eje fijo de v2.1 (7–20).
var EjePorOmision = Eje{DesdeHora: 7, HastaHora: 20}

// EjeDe: desde = min(7, hora(D), hora(primera)); hasta = max(20, ⌈H⌉,
// hora(última)+1); acotado a 0–24. horas son las actividades visibles.
func EjeDe(h *Horario, horas []time.Time, loc *time.Location) Eje {
	e := EjePorOmision
	if h != nil {
		e.DesdeHora = min(e.DesdeHora, h.Desde.Hora())
		e.HastaHora = max(e.HastaHora, h.Hasta.HoraArriba())
	}
	for _, t := range horas {
		hh := t.In(loc).Hour()
		e.DesdeHora = min(e.DesdeHora, hh)
		e.HastaHora = max(e.HastaHora, hh+1)
	}
	e.DesdeHora = max(0, e.DesdeHora)
	e.HastaHora = min(24, e.HastaHora)
	return e
}

// HorasDeLineas junta la hora de cada punto de las líneas.
func HorasDeLineas(ls []LineaDelDia) []time.Time {
	out := []time.Time{}
	for _, l := range ls {
		for _, p := range l.Puntos {
			out = append(out, p.Hora)
		}
	}
	return out
}

// EnEje devuelve la línea con PorHora reducido a [e.DesdeHora, e.HastaHora).
func (l LineaDelDia) EnEje(e Eje) LineaDelDia {
	ph := make([]Hora, 0, e.HastaHora-e.DesdeHora)
	for _, h := range l.PorHora {
		if h.Hora >= e.DesdeHora && h.Hora < e.HastaHora {
			ph = append(ph, h)
		}
	}
	l.PorHora = ph
	return l
}
```

`calendario.go` no se toca en esta tarea (es de la Tarea 5): los dos métodos de `HoraDelDia` van en `eje.go`:

```go
// Hora es la hora entera (0–23).
func (h HoraDelDia) Hora() int { return h.hora }

// HoraArriba es la hora redondeada hacia arriba: 18:00 → 18, 18:30 → 19, 23:59 → 24.
func (h HoraDelDia) HoraArriba() int {
	if h.minuto > 0 {
		return h.hora + 1
	}
	return h.hora
}
```

- [ ] **Step 4: Verde** — Run: `go clean -testcache && go test ./internal/desempeno/... -count=1` · Expected: PASS, incluidas **todas** las pruebas existentes de jornada, tablero y contrato (v2.1 sin cambios porque las banderas valen false).

- [ ] **Step 5: Propiedad aleatoria.** Agregar a `jornada_horario_test.go`:

```go
// Para cualquier día y horario al azar: 0 ≤ activos recortados ≤ activos sin
// recortar, y pagos/visitas/condonaciones idénticos (semilla fija, 3 000 días).
func TestHorasActivas_PropiedadRecorteNuncaCreaNiQuitaDinero(t *testing.T) {
	t.Parallel()
	loc := cdmxDominio(t)
	dia := *fecha(t, "2026-10-14")
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // prueba determinista
	for i := 0; i < 3000; i++ {
		var acts []domain.Actividad
		for k := 0; k < rng.Intn(12); k++ {
			m := rng.Intn(24 * 60)
			acts = append(acts, pagoA(t, fmt.Sprintf("pago:%d-%d", i, k), horaEn(t, dia, fmt.Sprintf("%02d:%02d", m/60, m%60), loc), k))
		}
		d := rng.Intn(23 * 60)
		h := d + 1 + rng.Intn(24*60-1-d)
		desde, hasta := fmt.Sprintf("%02d:%02d", d/60, d%60), fmt.Sprintf("%02d:%02d", h/60, h%60)
		ahora := horaEn(t, dia, "23:59", loc)
		con := domain.CalcularJornada(acts, parametros(t, desde, hasta, true), dia, loc, ahora)
		sin := domain.CalcularJornada(acts, parametros(t, desde, hasta, false), dia, loc, ahora)
		require.GreaterOrEqual(t, con.Resumen.MinutosActivos, 0)
		require.LessOrEqual(t, con.Resumen.MinutosActivos, sin.Resumen.MinutosActivos+sin.Resumen.Pausas.Minutos)
		require.Equal(t, sin.Resumen.Pagos, con.Resumen.Pagos)
		require.Equal(t, sin.Resumen.Visitas, con.Resumen.Visitas)
		require.Equal(t, sin.Resumen.Condonaciones, con.Resumen.Condonaciones)
	}
}
```

(imports `fmt`, `math/rand`). Run igual que el Step 4 · Expected: PASS.

- [ ] **Step 6: Roja al revertir.** Con `resumir` regresado a `minutosEntre(primera, ultima) - cerradas` sin recorte: `TestHorasActivas_ConHorario` falla en "toda fuera" y "cruza el fin". Con `calcularPorHora` siempre 7..19: `TestPorHora_DelDiaCompletoYEnEje` falla. Pegar salidas; restaurar; verde.

- [ ] **Step 7: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/domain/eje.go internal/desempeno/domain/eje_test.go internal/desempeno/domain/jornada.go internal/desempeno/domain/jornada_horario_test.go internal/desempeno/domain/helpers_test.go
git commit -m "feat(desempeno): horas activas recortadas al horario y eje segun horario y actividad para v2.2"
```

### Task 7: Zonas excluidas de desempeño en Configuración (D20, DA-1)

**Files:**
- Create: `migrations-firebird/000066_create_msp_cfg_desempeno_zonas_excluidas.up.sql` y `.down.sql`
- Create: `internal/config/domain/zona_excluida_desempeno.go` (+`_test.go`)
- Create: `internal/config/infra/configfb/zonas_excluidas_desempeno.go` (+`_test.go`)
- Create: `internal/config/app/zonas_excluidas_desempeno.go` (+`_test.go`)
- Create: `internal/config/infra/confighttp/zonas_excluidas_desempeno.go` (+`_test.go`)
- Create: `internal/config/config_contracts.go`
- Modify: `internal/config/ports/outbound/repos.go` (port nuevo), `internal/config/app/service.go` (dependencia nueva), `internal/config/infra/confighttp/routes.go` (tres operaciones), `cmd/api/config_wiring.go`, `internal/config/app/fakes_test.go`

**Interfaces:**
- Produces:
  ```go
  // internal/config/config_contracts.go
  package config
  type ZonasExcluidasDesempenoReader interface {
      // ZonasExcluidasDesempeno devuelve los ZONA_ID excluidos de desempeño, en orden ascendente.
      ZonasExcluidasDesempeno(ctx context.Context) ([]int, error)
  }
  // satisfecho por *configapp.Service
  ```
  HTTP: `GET /v2/config/desempeno/zonas-excluidas` (`config:leer`) → `{"items":[{"zona_id":23301,"zona_nombre":"MAYOREO","created_at":"...Z"}]}`; `PUT /v2/config/desempeno/zonas-excluidas/{zonaId}` (`config:administrar`, idempotente, 404 `zona_no_existe` si la zona no existe en `ZONAS_CLIENTES`) → 200 con el item; `DELETE` mismo path (`config:administrar`, idempotente) → 204.

- [ ] **Step 1: Migración** (sólo estructura; CLAUDE.md §1: sin DEFAULT, `CREATED_AT`/`CREATED_BY` desde Go)

`000066_create_msp_cfg_desempeno_zonas_excluidas.up.sql`:

```sql
-- 000066_create_msp_cfg_desempeno_zonas_excluidas
--
-- Zonas que no aparecen en "Desempeño de cobradores" (D20, DA-1 del spec
-- docs/superpowers/specs/2026-10-15-desempeno-pulido-2-design.md): hoy sólo
-- MAYOREO (23301), que no cobra por parcialidad. La administra el área de
-- Configuración (internal/config). Sólo estructura (CLAUDE.md §1): CREATED_AT
-- y CREATED_BY (MSP_USUARIOS.ID del administrador) los pone Go.
CREATE TABLE MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS (
  ZONA_ID    INTEGER                    NOT NULL,
  CREATED_AT TIMESTAMP                  NOT NULL,
  CREATED_BY CHAR(36) CHARACTER SET ASCII NOT NULL,
  CONSTRAINT PK_MSP_CFG_DESEMPENO_ZONAS_EXCL PRIMARY KEY (ZONA_ID)
);
COMMIT;

INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT)
VALUES (66, '000066_create_msp_cfg_desempeno_zonas_excluidas', CURRENT_TIMESTAMP);
COMMIT;
```

`.down.sql`:

```sql
DROP TABLE MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS;
DELETE FROM MSP_MIGRATIONS WHERE ID = 66;
COMMIT;
```

Antes de escribirla, `ls migrations-firebird | tail -2` para confirmar que 000066 sigue libre (si otra rama la tomó, usar la siguiente y corregir el número en los dos archivos y en el INSERT).

- [ ] **Step 2: Paso humano — aplicar en dev.** Pedir autorización explícita al dueño; con ella: `make fb-snapshot && make fb-migrate-up && make fb-migrate-status`. Expected: aparece la fila 66. Sin autorización, las pruebas de `configfb` de esta tarea quedan **sin correr** y se dice así en el reporte (no se dan por verdes).

- [ ] **Step 3: Dominio — prueba que falla** (`zona_excluida_desempeno_test.go`)

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	configdomain "github.com/abdimuy/msp-api/internal/config/domain"
)

func TestNuevaZonaExcluidaDesempeno(t *testing.T) {
	t.Parallel()
	por := uuid.New()
	ahora := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	z, err := configdomain.NuevaZonaExcluidaDesempeno(23301, por, ahora)
	require.NoError(t, err)
	assert.Equal(t, 23301, z.ZonaID)
	assert.Equal(t, por, z.CreatedBy)
	assert.Equal(t, ahora, z.CreatedAt)

	for _, malo := range []int{0, -1} {
		_, err := configdomain.NuevaZonaExcluidaDesempeno(malo, por, ahora)
		require.ErrorIs(t, err, configdomain.ErrZonaExcluidaInvalida, "%d", malo)
	}
	_, err = configdomain.NuevaZonaExcluidaDesempeno(23301, uuid.Nil, ahora)
	require.ErrorIs(t, err, configdomain.ErrZonaExcluidaInvalida)
}
```

Implementación (`zona_excluida_desempeno.go`):

```go
package domain

import (
	"time"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// ZonaExcluidaDesempeno es una fila de MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS.
type ZonaExcluidaDesempeno struct {
	ZonaID     int
	ZonaNombre string // resuelto de ZONAS_CLIENTES al listar; vacío al crear
	CreatedAt  time.Time
	CreatedBy  uuid.UUID
}

// ErrZonaExcluidaInvalida: zona ≤ 0 o sin administrador.
var ErrZonaExcluidaInvalida = apperror.NewValidation(
	"zona_excluida_invalida",
	"la zona o el usuario de la exclusión no son válidos",
)

// NuevaZonaExcluidaDesempeno valida y fija CreatedAt desde Go (CLAUDE.md §1).
func NuevaZonaExcluidaDesempeno(zonaID int, por uuid.UUID, ahora time.Time) (ZonaExcluidaDesempeno, error) {
	if zonaID <= 0 || por == uuid.Nil {
		return ZonaExcluidaDesempeno{}, ErrZonaExcluidaInvalida
	}
	return ZonaExcluidaDesempeno{ZonaID: zonaID, CreatedAt: ahora.UTC(), CreatedBy: por}, nil
}
```

Run: `go test ./internal/config/domain/ -count=1` · rojo antes, verde después.

- [ ] **Step 4: Port y repositorio.** En `ports/outbound/repos.go`:

```go
// ZonasExcluidasDesempenoRepo persiste MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS.
type ZonasExcluidasDesempenoRepo interface {
	// ListarZonasExcluidasDesempeno: todas, con ZONAS_CLIENTES.NOMBRE, por ZONA_ID.
	ListarZonasExcluidasDesempeno(ctx context.Context) ([]configdomain.ZonaExcluidaDesempeno, error)
	// AgregarZonaExcluidaDesempeno inserta; si ya existe no hace nada (idempotente).
	AgregarZonaExcluidaDesempeno(ctx context.Context, z configdomain.ZonaExcluidaDesempeno) error
	// QuitarZonaExcluidaDesempeno borra; si no existe no hace nada.
	QuitarZonaExcluidaDesempeno(ctx context.Context, zonaID int) error
}
```

Prueba de integración `configfb/zonas_excluidas_desempeno_test.go` (patrón de `zona_caja_test.go`; todo dentro de `fbtestutil.WithTestTransaction`):

```go
func TestZonasExcluidasDesempeno_AgregarListarQuitar(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := configfb.NewConfigRepo(pool)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		zona := primeraZonaCliente(t, ctx, pool) // SELECT FIRST 1 ZONA_CLIENTE_ID, NOMBRE FROM ZONAS_CLIENTES ORDER BY 1
		por := uuid.New()
		z, err := configdomain.NuevaZonaExcluidaDesempeno(zona.ID, por, time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, repo.AgregarZonaExcluidaDesempeno(ctx, z))
		require.NoError(t, repo.AgregarZonaExcluidaDesempeno(ctx, z), "idempotente")
		lista, err := repo.ListarZonasExcluidasDesempeno(ctx)
		require.NoError(t, err)
		require.Len(t, filtrar(lista, zona.ID), 1)
		got := filtrar(lista, zona.ID)[0]
		assert.Equal(t, zona.Nombre, got.ZonaNombre)
		assert.Equal(t, por, got.CreatedBy)
		assert.True(t, got.CreatedAt.Equal(z.CreatedAt), "CREATED_AT sale tal cual lo puso Go")
		require.NoError(t, repo.QuitarZonaExcluidaDesempeno(ctx, zona.ID))
		require.NoError(t, repo.QuitarZonaExcluidaDesempeno(ctx, zona.ID), "idempotente")
		lista, err = repo.ListarZonasExcluidasDesempeno(ctx)
		require.NoError(t, err)
		assert.Empty(t, filtrar(lista, zona.ID))
	})
}
```

(`primeraZonaCliente` y `filtrar` son helpers locales del archivo; leer `zona_caja_test.go` y `encoding_acentos_test.go` para el patrón real de `WithTestTransaction` y la lectura de `NOMBRE` con `firebird.Win1252`.)

Implementación `configfb/zonas_excluidas_desempeno.go`: `SELECT z.ZONA_ID, zc.NOMBRE, z.CREATED_AT, z.CREATED_BY FROM MSP_CFG_DESEMPENO_ZONAS_EXCLUIDAS z LEFT JOIN ZONAS_CLIENTES zc ON zc.ZONA_CLIENTE_ID = z.ZONA_ID ORDER BY z.ZONA_ID`; `CREATED_AT` con `firebird.ScanUTCTime`; insertar con `firebird.ToWallClock(z.CreatedAt)` y `z.CreatedBy.String()`; "si ya existe no hace nada" con `SELECT COUNT(*)` previo dentro de `firebird.RunInTx` (no MERGE: bug del driver, memoria `reference_firebirdsql_merge_param_bug`). Agregar `var _ outbound.ZonasExcluidasDesempenoRepo = (*ConfigRepo)(nil)`.

Run: `set -a && . ./.env && set +a && go clean -testcache && go test -timeout 1800s -count=1 ./internal/config/infra/configfb/ -run TestZonasExcluidasDesempeno` · Expected: PASS (sólo si el Step 2 se autorizó).

- [ ] **Step 5: Servicio — prueba que falla** (`app/zonas_excluidas_desempeno_test.go`, con fakes en `fakes_test.go`): `ExcluirZonaDesempeno` con zona inexistente → `ErrZonaNoEncontrada` (reutilizar el error que ya usa `AsignarZonaCaja` en `app/zona_caja_errors.go`; leerlo y usar su nombre real); con zona existente → llama al repo con `CreatedAt == reloj` y `CreatedBy == por`; `ZonasExcluidasDesempeno` devuelve `[]int` ascendente aunque el repo devuelva desordenado; `QuitarZonaExcluidaDesempeno` delega. El servicio recibe un `reloj func() time.Time` por `NewService`: como `NewService` ya tiene cinco parámetros posicionales, agregar dos más al final (`zonasExcluidas outbound.ZonasExcluidasDesempenoRepo, ahora func() time.Time`) y actualizar `cmd/api/config_wiring.go:31` (`configapp.NewService(repo, repo, usuarios, repo, repo, repo, time.Now)`) y los constructores de prueba.

```go
// ExcluirZonaDesempeno saca la zona de "Desempeño de cobradores" (D20).
func (s *Service) ExcluirZonaDesempeno(ctx context.Context, zonaID int, por uuid.UUID) (configdomain.ZonaExcluidaDesempeno, error) {
	ok, err := s.zonaCajaExistente.ZonaExiste(ctx, zonaID)
	if err != nil {
		return configdomain.ZonaExcluidaDesempeno{}, err
	}
	if !ok {
		return configdomain.ZonaExcluidaDesempeno{}, ErrZonaNoEncontrada // nombre real: ver app/zona_caja_errors.go
	}
	z, err := configdomain.NuevaZonaExcluidaDesempeno(zonaID, por, s.ahora())
	if err != nil {
		return configdomain.ZonaExcluidaDesempeno{}, err
	}
	return z, s.zonasExcluidas.AgregarZonaExcluidaDesempeno(ctx, z)
}

// ZonasExcluidasDesempeno cumple config.ZonasExcluidasDesempenoReader.
func (s *Service) ZonasExcluidasDesempeno(ctx context.Context) ([]int, error) {
	zs, err := s.zonasExcluidas.ListarZonasExcluidasDesempeno(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(zs))
	for _, z := range zs {
		out = append(out, z.ZonaID)
	}
	sort.Ints(out)
	return out, nil
}
```

- [ ] **Step 6: Contrato del módulo** `internal/config/config_contracts.go` (con la interfaz de arriba y `var _ ZonasExcluidasDesempenoReader = (*configapp.Service)(nil)` **en una prueba** del paquete `app`, no en el contrato, para no importar `app` desde la raíz). Revisar `.golangci.yml` (`importas` y `depguard`, líneas ~300-470) y agregar el alias `config` para `github.com/abdimuy/msp-api/internal/config` si `importas` exige alias para paquetes raíz de módulo (comparar con cómo está `internal/rutas`).

- [ ] **Step 7: HTTP** — `confighttp/zonas_excluidas_desempeno.go` con DTO `ZonaExcluidaDesempenoDTO{ZonaID int "zona_id"; ZonaNombre string "zona_nombre"; CreatedAt string "created_at"}` y tres handlers; permisos con `requirePerm(cu, auth.PermConfigLeer)` / `auth.PermConfigAdministrar` (leer los nombres reales en `confighttp/handlers.go`); el `por` del PUT es `cu.ID`. Registrar en `routes.go` con OperationIDs `listar-config-desempeno-zonas-excluidas`, `excluir-config-desempeno-zona`, `quitar-config-desempeno-zona`. Prueba `zonas_excluidas_desempeno_test.go` (patrón `handlers_test.go`): 401 sin usuario, 403 sin permiso, GET 200 con items, PUT 200 e idempotente, PUT de zona inexistente 404 con `code=` del error, DELETE 204.

- [ ] **Step 8: Verde completo** — Run: `go clean -testcache && go test ./internal/config/... ./cmd/api/... -count=1 && golangci-lint run ./internal/config/... ./cmd/api/...` · Expected: PASS y sin hallazgos.

- [ ] **Step 9: Commit**

```bash
git add migrations-firebird/000066_create_msp_cfg_desempeno_zonas_excluidas.up.sql migrations-firebird/000066_create_msp_cfg_desempeno_zonas_excluidas.down.sql internal/config cmd/api/config_wiring.go .golangci.yml
git commit -m "feat(config): zonas excluidas de desempeno administradas en configuracion"
```

### Task 8: Una cancelación del cliente no es un 503 en el log (D28)

**Files:**
- Modify: `internal/desempeno/infra/desempenohttp/auth.go:44-60`
- Test: `internal/desempeno/infra/desempenohttp/robustez_test.go` (prueba nueva al final)

**Interfaces:** ninguna nueva. El cuerpo de la respuesta al cliente no cambia (sigue siendo el 503 de `firebird_timeout`; el cliente ya se fue).

- [ ] **Step 1: Prueba que falla**

```go
// D28: una petición que el cliente canceló llega como firebird_timeout (la
// plataforma traduce context.Canceled a 503, firebird/errors.go:39-46). En
// desempeño se registra como Info "cancelada_por_cliente", no como incidente.
func TestErrores_CancelacionDelClienteNoEsNoDisponible(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	lg := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	u := &usuariosContados{fakeUsuarios: fakeUsuarios{err: firebird.MapError(context.Canceled)}}
	cu := usuarioCon(auth.PermDesempenoVer)
	h := routerOpts(&cu, servicioCon(t, u), desempenohttp.WithLogger(lg))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/desempeno/usuarios/abc123/cuentas", nil).WithContext(ctx)
	h.ServeHTTP(httptest.NewRecorder(), req)

	log := buf.String()
	assert.Contains(t, log, "desempeno.http.cancelada_por_cliente")
	assert.Contains(t, log, `"level":"INFO"`)
	assert.NotContains(t, log, "desempeno.http.no_disponible")
}
```

(Control positivo existente: `TestErrores_503DeFirebirdRegistraElTimeout` — un `DeadlineExceeded` con contexto vivo sigue siendo Warn `no_disponible`.)

- [ ] **Step 2: Rojo** — Run: `go test ./internal/desempeno/infra/desempenohttp/ -run TestErrores -count=1` · Expected: FAIL (el log trae `no_disponible`).

- [ ] **Step 3: Implementar** en `mapAppError`:

```go
	if status >= http.StatusInternalServerError {
		if errors.Is(ctx.Err(), context.Canceled) {
			// D28: el cliente cerró la petición (cambió de pestaña, de periodo);
			// no es un incidente del servidor.
			h.logger().InfoContext(ctx, "desempeno.http.cancelada_por_cliente", "code", ae.Code, "status", status)
		} else {
			h.logger().WarnContext(ctx, "desempeno.http.no_disponible",
				"code", ae.Code, "status", status, "error", err)
		}
	}
```

- [ ] **Step 4: Verde** — Run: `go clean -testcache && go test ./internal/desempeno/infra/desempenohttp/ -count=1` · PASS.

- [ ] **Step 5: Roja al revertir** (regresar el `if`): falla la prueba nueva; `TestErrores_503DeFirebirdRegistraElTimeout` sigue verde en ambos sentidos. Restaurar.

- [ ] **Step 6: Lo no verificado, en voz alta:** qué escribe el log de acceso de la plataforma para esa petición (spec §10) no se toca ni se mide aquí; anotarlo en el reporte.

- [ ] **Step 7: Commit**

```bash
git add internal/desempeno/infra/desempenohttp/auth.go internal/desempeno/infra/desempenohttp/robustez_test.go
git commit -m "fix(desempeno): la cancelacion del cliente se registra como info y no como 503"
```

### Task 9: Casos compartidos con `hoy` y `columna` (spec §8.2)

**Depende de:** Tarea 4. **Bloquea:** Tarea 15.

**Files:**
- Modify: `testdata/estado-cuenta/001…040-*.json` (agregar `hoy` y `columna`), `testdata/estado-cuenta/VERSION`
- Create: `testdata/estado-cuenta/041-cita-a-futuro.json` … `049-no-estaba-por-visitar.json` (9 casos)
- Create: `scripts/casos-estado-cuenta-columna.jq`
- Modify: `internal/desempeno/domain/casos_compartidos_test.go` (parseo de `hoy` y `columna`; comparación de columna; cobertura mínima)

**Interfaces:**
- Produces: formato v2 del caso — raíz con claves `nombre, ventana, hoy, cuentas, pagos, visitas, esperado`; `hoy` = `"YYYY-MM-DD"`; cada esperado `{venta_id, estado, con_algo, columna}`. VERSION nuevo = `ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256`. La Tarea 15 copia el directorio **byte a byte**.

- [ ] **Step 1: Prueba que falla** — en `casos_compartidos_test.go`: `casoJSON` gana `Hoy domain.Fecha`; `esperadoJSON` gana `Columna domain.Columna`; `parsearCaso` exige `"hoy"` en la raíz (`objeto(texto, archivo, "nombre", "ventana", "hoy", "cuentas", "pagos", "visitas", "esperado")`) y `"columna"` en cada esperado (`objeto(raw, donde, "venta_id", "estado", "con_algo", "columna")`), parsea `hoy` con `domain.ParseFecha` y `columna` con `domain.ParseColumna` (un valor desconocido es error). `casoValido` gana `"hoy": "2026-10-14",` y `"columna": "pagados"`; `malos` gana `{"columna desconocida", `"columna": "pagados"`, `"columna": "volver"`, "columna"}` y `{"hoy ausente", `"hoy": "2026-10-14",`, ``, "hoy"}`. `TestCasosEstadoCuenta_CadaCasoContraElDeriver` agrega:

```go
				assert.Equal(t, e.Columna, domain.ColumnaDe(r, c.Hoy), "%s — venta %d: columna (E-INF-018)", c.Nombre, ventaID)
```

y `TestCasosEstadoCuenta_CoberturaMinimaDelJuego` exige además que aparezcan las tres columnas y, por nombre de archivo, los nueve casos nuevos:

```go
	columnas := map[domain.Columna]bool{}
	// … dentro del bucle de esperados: columnas[e.Columna] = true
	for _, c := range domain.Columnas() {
		assert.True(t, columnas[c], "ningún caso espera la columna %s", c)
	}
	casos := cargarCasos(t)
	for _, n := range []string{
		"041-cita-a-futuro.json", "042-cita-hora-sin-dia-por-visitar.json", "043-promesa-vencida.json",
		"044-promesa-de-hoy.json", "045-promesa-a-futuro-no-paga.json", "046-promesa-sin-fecha.json",
		"047-se-nego-por-visitar.json", "048-parcial-en-pagados.json", "049-no-estaba-por-visitar.json",
	} {
		_, ok := casos[n]
		assert.True(t, ok, "falta el caso obligatorio %s (spec §8.2)", n)
	}
```

- [ ] **Step 2: Rojo** — Run: `go test ./internal/desempeno/domain/ -run 'TestCasosEstadoCuenta|TestParsearCaso' -count=1` · Expected: FAIL `falta la clave "hoy"` en los 40 casos.

- [ ] **Step 3: Agregar `hoy` y `columna` a los 40 casos sin usar `ColumnaDe`** (para no derivar el esperado del código que se prueba). `scripts/casos-estado-cuenta-columna.jq`, segunda implementación de la tabla del spec §3.1, independiente:

```jq
# hoy = día de negocio del fin de la ventana (todos los casos actuales terminan el 14 oct, 22:40Z = 16:40 CDMX).
# columna por la tabla del spec §3.1; prometio_proxima queda "REVISAR" y se llena A MANO.
def columna($estado):
  if ($estado == "pago" or $estado == "abono_parcial") then "pagados"
  elif $estado == "prometio_proxima" then "REVISAR"
  else "por_visitar" end;
{nombre, ventana, hoy: "2026-10-14", cuentas, pagos, visitas,
 esperado: [.esperado[] | {venta_id, estado, con_algo, columna: columna(.estado)}]}
```

Run: `cd testdata/estado-cuenta && for f in 0[0-3]*.json 040-*.json; do jq -f ../../scripts/casos-estado-cuenta-columna.jq "$f" > "$f.tmp" && mv "$f.tmp" "$f"; done && grep -l REVISAR *.json`
Expected: lista **exactamente** `023-reagendar-es-prometio-proxima.json` y `027-fecha-promesa-no-cambia-estado.json` (si sale otro, revisar). Llenar a mano por la tabla: 023 (promesa sin fecha → `regresas` → `por_visitar`); 027 (`fecha_promesa` 2026-10-20 > hoy 2026-10-14 → `no_paga`). Verificar que `jq` conservó la sangría de 2 espacios y el `\n` final de los originales (`git diff --stat` debe mostrar sólo líneas agregadas: `git diff testdata/estado-cuenta | grep '^-' | grep -v '^---' | wc -l` → 0, salvo la línea de cierre de `con_algo` que gana coma; revisar a ojo).

- [ ] **Step 4: Escribir los 9 casos nuevos** a mano. Ventana de todos: `{"inicio": "2026-10-09T13:46:00Z", "fin": "2026-10-15T22:40:00Z"}`, `"hoy": "2026-10-15"`, una cuenta `{"venta_id": 1, "cliente_id": 10, "parcialidad": "150.00"}`. Las visitas usan literales ya presentes en el juego (`grep -h tipo_visita testdata/estado-cuenta/*.json | sort -u`). Ejemplo completo, `045-promesa-a-futuro-no-paga.json`:

```json
{
  "nombre": "una promesa con fecha posterior a hoy es la unica que no paga",
  "ventana": {
    "inicio": "2026-10-09T13:46:00Z",
    "fin": "2026-10-15T22:40:00Z"
  },
  "hoy": "2026-10-15",
  "cuentas": [
    {
      "venta_id": 1,
      "cliente_id": 10,
      "parcialidad": "150.00"
    }
  ],
  "pagos": [],
  "visitas": [
    {
      "cliente_id": 10,
      "venta_id": 1,
      "tipo_visita": "Pidió reagendar visita",
      "fecha_hora": "2026-10-12T16:00:00Z",
      "fecha_cita": null,
      "hora_cita": null,
      "fecha_promesa": "2026-10-16"
    }
  ],
  "esperado": [
    {
      "venta_id": 1,
      "estado": "prometio_proxima",
      "con_algo": true,
      "columna": "no_paga"
    }
  ]
}
```

Los otros ocho cambian sólo `nombre`, la visita o el pago y el esperado:

| Archivo | Visita / pago | Esperado `estado` → `columna` |
|---|---|---|
| `041-cita-a-futuro.json` | "Pidió que regrese otro día", `fecha_cita` "2026-10-20", `hora_cita` "16:30" | `cita_a_una_hora` → `por_visitar` |
| `042-cita-hora-sin-dia-por-visitar.json` | "Pidió que regrese otro día", `fecha_cita` null, `hora_cita` "10:00" (el deriver no la hace cita: caso 026) | `visite_vuelvo` → `por_visitar` |
| `043-promesa-vencida.json` | "Pidió reagendar visita", `fecha_promesa` "2026-10-13" | `prometio_proxima` → `por_visitar` |
| `044-promesa-de-hoy.json` | "Pidió reagendar visita", `fecha_promesa` "2026-10-15" | `prometio_proxima` → `por_visitar` |
| `046-promesa-sin-fecha.json` | "Pidió reagendar visita", `fecha_promesa` null | `prometio_proxima` → `por_visitar` |
| `047-se-nego-por-visitar.json` | "No pagará esta ocasión" | `se_nego` → `por_visitar` |
| `048-parcial-en-pagados.json` | sin visitas; pago `{venta_id 1, importe "100.00", forma_cobro_id 157, fecha_hora "2026-10-12T16:00:00Z", rechazado false}` | `abono_parcial` → `pagados` |
| `049-no-estaba-por-visitar.json` | "No se encontraba", `venta_id` null (alcance cliente) | `no_estaba` → `por_visitar` |

Nota (dato para el reporte): "cita sin fecha" del spec §8.2 no es alcanzable como estado `cita` — el deriver sólo hace cita con `fecha_cita` (`TipoVisitaCatalogo.kt:220-221`, caso 026); el caso 042 documenta adónde cae (`visite_vuelvo`, `por_visitar`).

- [ ] **Step 5: VERSION** — Run: `cd testdata/estado-cuenta && ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1 > VERSION && cat VERSION` · anotar el hash en el reporte (la Tarea 15 lo exige igual).

- [ ] **Step 6: Verde** — Run: `go clean -testcache && go test ./internal/desempeno/domain/ -count=1` · Expected: PASS con 49 subcasos.

- [ ] **Step 7: Roja al revertir** — con `ColumnaDe` regresado a la regla v2.1 del Step 5 de la Tarea 4: fallan 041, 047 y la tabla; restaurar.

- [ ] **Step 8: Commit**

```bash
git add testdata/estado-cuenta scripts/casos-estado-cuenta-columna.jq internal/desempeno/domain/casos_compartidos_test.go
git commit -m "test(desempeno): casos compartidos con hoy y columna esperada, nueve casos de columnas (E-INF-018)"
```

### Task 10: `cobro.pct = null` cuando `esperado = 0` (D20, M2, E-INF-019)

**Gate:** sólo si la Tarea 3 devolvió **SOSTIENE** para las preguntas 4 (M2). Si no, esta tarea no se ejecuta y se reporta.

**Files:**
- Modify: `internal/desempeno/domain/cobro.go:22-32`
- Test: `internal/desempeno/domain/cobro_test.go`

**Interfaces:** `CalcularCobro(cs CobroSemana) Cobro` — misma firma. Afecta v2.1 y v2.2 (spec §6.1).

- [ ] **Step 1: Prueba que falla**

```go
// E-INF-019: un porcentaje sin denominador no es "0 %" (MAYOREO: parcialidad 0 en
// todas sus ventas → rutas da "0.00").
func TestCalcularCobro_E_INF_019_SinEsperadoNoHayPct(t *testing.T) {
	t.Parallel()
	cero := decimal.RequireFromString("0.00")
	c := domain.CalcularCobro(domain.CobroSemana{PctPonderado: &cero, Ventas: []domain.CobroVenta{
		{VentaID: 1, Parcialidad: decimal.Zero, AbonoSemana: decimal.RequireFromString("500"), AplicaPonderado: true},
	}})
	assert.True(t, c.Esperado.IsZero())
	assert.Nil(t, c.Pct)
	assert.Equal(t, "500.00", c.Cobrado.StringFixed(2), "el cobrado no cambia")

	// Control positivo: con esperado > 0, pct sigue siendo el de rutas, tal cual.
	pct := decimal.RequireFromString("65.90")
	c = domain.CalcularCobro(domain.CobroSemana{PctPonderado: &pct, Ventas: []domain.CobroVenta{
		{VentaID: 1, Parcialidad: decimal.RequireFromString("150"), AbonoSemana: decimal.RequireFromString("100"), AplicaPonderado: true},
	}})
	require.NotNil(t, c.Pct)
	assert.Equal(t, "65.90", c.Pct.StringFixed(2))

	// Ninguna venta aplica: esperado 0 → null aunque rutas diga otra cosa.
	c = domain.CalcularCobro(domain.CobroSemana{PctPonderado: &pct, Ventas: []domain.CobroVenta{
		{VentaID: 1, Parcialidad: decimal.RequireFromString("150"), AplicaPonderado: false},
	}})
	assert.Nil(t, c.Pct)
}
```

- [ ] **Step 2: Rojo** — Run: `go test ./internal/desempeno/domain/ -run TestCalcularCobro -count=1` · Expected: FAIL (`Pct` no es nil).

- [ ] **Step 3: Implementar** — al final de `CalcularCobro`, antes del `return`:

```go
	// D20, E-INF-019: sin nada esperado no hay porcentaje (null, no "0.00").
	// Local a desempeño: rutas y Cobranza semanal no cambian.
	if c.Esperado.IsZero() {
		c.Pct = nil
	}
```

- [ ] **Step 4: Verde** — Run: `go clean -testcache && go test ./internal/desempeno/... -count=1` · Expected: PASS. Si alguna prueba existente esperaba `pct` con esperado 0, **no** se ajusta en silencio: se lista en el reporte con su archivo:línea y se ajusta citando E-INF-019.

- [ ] **Step 5: Roja al revertir** (quitar el `if`): falla `TestCalcularCobro_E_INF_019`. Restaurar.

- [ ] **Step 6: Evidencia** — E-INF-019 pasa a "Prueba roja/verde (MEDIDO 2026-10-16, unitaria)" con la salida, y "Consumidores (LEÍDO, `grep -rn "\.Pct\b\|Cobro.Pct" internal/`)": enumerarlos. La comparación por arnés (sólo cambian filas con `esperado = 0`) se hace en la Tarea 14.

- [ ] **Step 7: Commit**

```bash
git add internal/desempeno/domain/cobro.go internal/desempeno/domain/cobro_test.go docs/evidencia/infra.md
git commit -m "fix(desempeno): pct nulo cuando no hay nada esperado en la semana (E-INF-019)"
```

### Task 11: Contrato 2.2 en el tablero: columnas, `hoy`, `eje`, `promedio_diario`, zonas excluidas (D18, D20, D21, D26-1/2/3)

**Depende de:** T0 aprobada, T4, T6, T7, T8 integradas en `feat/desempeno-pulido-2`.

**Files:**
- Create: `internal/desempeno/app/contrato.go`, `internal/desempeno/app/contrato_test.go`
- Create: `internal/desempeno/app/tablero_v22_test.go`
- Modify: `internal/desempeno/app/service.go` (opciones, exclusión en `resolverUsuario`), `internal/desempeno/app/tablero.go` (todo el archivo), `internal/desempeno/app/fakes_test.go` (fake de zonas excluidas)
- Modify: `internal/desempeno/ports/outbound/ports.go` (`ZonasExcluidas`)
- Create: `internal/desempeno/infra/clients/config_zonas_client.go` (+`_test.go`)
- Create: `internal/desempeno/infra/desempenohttp/dto_v22.go`, `internal/desempeno/infra/desempenohttp/mapper_v22.go`, `internal/desempeno/infra/desempenohttp/contrato_v22_test.go`, `internal/desempeno/infra/desempenohttp/testdata/tablero_semana_v22.json`
- Modify: `internal/desempeno/infra/desempenohttp/handlers.go:68-78,134-173` (parámetro `contrato`, clave de `singleflight`), `internal/desempeno/infra/desempenohttp/robustez_test.go` (prueba nueva)
- Modify: `cmd/api/desempeno_wiring.go`, `cmd/api/desempeno_wiring_test.go`

**Interfaces:**
- Consumes: `domain.ColumnaDe`, `domain.Columnas` (T4); `domain.Eje`, `domain.EjeDe`, `domain.HorasDeLineas`, `LineaDelDia.EnEje`, `ParametrosJornada.RecortarAlHorario/PorHoraDelDia` (T6); `domain.ErrZonaExcluida` (T5); `config.ZonasExcluidasDesempenoReader` (T7).
- Produces (las usan T12, T13, T14):
  ```go
  // app/contrato.go
  type Contrato string
  const Contrato21 Contrato = "2.1"; Contrato22 Contrato = "2.2"
  func ParseContrato(s string) (Contrato, error) // "" → 2.1; otro → ErrParametroInvalido{parametro: contrato}
  func (c Contrato) Parametros(p domain.ParametrosJornada) domain.ParametrosJornada // en 2.2 enciende las dos banderas
  // app/service.go
  type Opcion func(*Service)
  func ConZonasExcluidas(z outbound.ZonasExcluidas) Opcion
  func NewService(usuarios, actividades, cuentas, clock, loc, opts ...Opcion) *Service
  // app/tablero.go
  type TableroInput struct { Periodo, PausaMin, DesdeHora, HastaHora, Contrato string }
  type Tablero struct { GeneradoAt time.Time; Hoy domain.Fecha; Periodo Periodo; PausaMin int; Contrato Contrato; Eje domain.Eje; Filas []FilaTablero }
  type CoberturaPorColumna struct { PorVisitar, NoPaga, Pagados, SinNada int }
  type PromedioDiario struct { Dias, MinutosActivos int; PagosImporte, Visitas decimal.Decimal }
  type UltimoMovimiento struct { ID string; Tipo domain.TipoActividad; Hora time.Time } // lo llena T12
  // FilaTablero gana: PorColumna CoberturaPorColumna; PromedioDiario *PromedioDiario; SemanaVencida bool; UltimoMovimiento *UltimoMovimiento
  func (s *Service) calcularFilas(ctx context.Context, us []domain.UsuarioCobranza, ix domain.IndiceUsuarios, ahora time.Time, periodo Periodo, p domain.ParametrosJornada, c Contrato) ([]FilaTablero, error)
  // ports/outbound
  type ZonasExcluidas interface { ZonasExcluidas(ctx context.Context) (map[int]bool, error) }
  ```

- [ ] **Step 1: Prueba de `ParseContrato`** (`contrato_test.go`)

```go
func TestParseContrato(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]desempenoapp.Contrato{"": desempenoapp.Contrato21, "2.1": desempenoapp.Contrato21, "2.2": desempenoapp.Contrato22} {
		got, err := desempenoapp.ParseContrato(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, malo := range []string{"2", "2.3", "v2.2", " 2.2"} {
		_, err := desempenoapp.ParseContrato(malo)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, malo)
	}
}
```

Implementación `contrato.go`:

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import "github.com/abdimuy/msp-api/internal/desempeno/domain"

// Contrato es la versión de respuesta que pide el cliente (spec §6.1): sin el
// parámetro se sirve v2.1 congelado, el que entiende el escritorio 1.26.0.
type Contrato string

// Versiones servidas.
const (
	Contrato21 Contrato = "2.1"
	Contrato22 Contrato = "2.2"
)

// ParseContrato: "" o "2.1" → 2.1; "2.2" → 2.2; cualquier otro, 422.
func ParseContrato(s string) (Contrato, error) {
	switch s {
	case "", string(Contrato21):
		return Contrato21, nil
	case string(Contrato22):
		return Contrato22, nil
	}
	return "", invalido("contrato")
}

// Parametros enciende en 2.2 el recorte de horas activas al horario y el
// por_hora de 24 h (D21). En 2.1 los deja como vienen (apagados).
func (c Contrato) Parametros(p domain.ParametrosJornada) domain.ParametrosJornada {
	if c == Contrato22 {
		p.RecortarAlHorario = true
		p.PorHoraDelDia = true
	}
	return p
}
```

Run: `go test ./internal/desempeno/app/ -run TestParseContrato -count=1` (rojo → verde).

- [ ] **Step 2: Pruebas del tablero v2.2 que fallan** (`tablero_v22_test.go`, sobre `nuevoMundo` de `escenario_test.go`)

```go
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

type fakeZonasExcluidas struct {
	zonas map[int]bool
	err   error
}

func (f fakeZonasExcluidas) ZonasExcluidas(context.Context) (map[int]bool, error) { return f.zonas, f.err }

func servicioV22(t *testing.T, m mundo, opts ...desempenoapp.Opcion) *desempenoapp.Service {
	t.Helper()
	return desempenoapp.NewService(m.usuarios, m.actividades, m.cuentas, relojFijo{m.ahora}, cdmx(t), opts...)
}

// Spec §6.1: sin contrato y con contrato=2.1 el tablero es idéntico.
func TestTablero_SinContratoEsV21(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := servicioV22(t, m)
	for _, periodo := range []string{"hoy", "semana"} {
		a, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: periodo})
		require.NoError(t, err)
		b, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: periodo, Contrato: "2.1"})
		require.NoError(t, err)
		assert.Equal(t, a, b, periodo)
		assert.Equal(t, desempenoapp.Contrato21, a.Contrato)
		for _, f := range a.Filas {
			assert.Nil(t, f.PromedioDiario, "v2.1 no calcula promedio")
			for _, l := range f.Lineas {
				assert.Len(t, l.PorHora, 13, "v2.1: por_hora 7..19")
			}
		}
	}
}

// E-INF-018 en el tablero: cobertura por columna, total y consistente.
func TestTablero_V22_CoberturaPorColumna(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	tb, err := servicioV22(t, m).Tablero(context.Background(), desempenoapp.TableroInput{Contrato: "2.2"})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-juan")
	assert.Equal(t, desempenoapp.CoberturaPorColumna{PorVisitar: 2, NoPaga: 0, Pagados: 6, SinNada: 1}, f.PorColumna)
	assert.Equal(t, f.CoberturaTotal, f.PorColumna.PorVisitar+f.PorColumna.NoPaga+f.PorColumna.Pagados)
	assert.Equal(t, f.CoberturaTotal-f.CoberturaConAlgo, f.PorColumna.SinNada)
	assert.Equal(t, "2026-10-14", tb.Hoy.String(), "hoy de negocio del mismo ahora")
}

// D26-3: promedio diario de la semana = totales / días transcurridos (inicio a hoy, inclusive).
func TestTablero_V22_PromedioDiarioEnSemana(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := servicioV22(t, m)
	v21, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana"})
	require.NoError(t, err)
	v22, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana", Contrato: "2.2"})
	require.NoError(t, err)
	f21, f22 := filaDe(t, v21, "uid-juan"), filaDe(t, v22, "uid-juan")
	require.NotNil(t, f22.PromedioDiario)
	p := f22.PromedioDiario
	assert.Equal(t, 6, p.Dias, "jueves 9 a martes 14")
	seis := decimal.NewFromInt(6)
	assert.Equal(t, int(decimal.NewFromInt(int64(f22.MinutosActivos)).Div(seis).Round(0).IntPart()), p.MinutosActivos)
	assert.Equal(t, f21.Pagos.Importe.Div(seis).Round(2).StringFixed(2), p.PagosImporte.StringFixed(2))
	assert.Equal(t, decimal.NewFromInt(int64(f21.Visitas)).Div(seis).Round(2).StringFixed(2), p.Visitas.StringFixed(2))
	assert.Equal(t, f21.Pagos, f22.Pagos, "las cifras visibles siguen siendo totales")

	hoy, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "hoy", Contrato: "2.2"})
	require.NoError(t, err)
	assert.Nil(t, filaDe(t, hoy, "uid-juan").PromedioDiario, "en hoy es null")
}

// D21: un solo eje para todas las filas, y por_hora recortado a él.
func TestTablero_V22_EjeComunYPorHoraEnEje(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	m.actividades.porZona[zonaRuta21] = append(m.actividades.porZona[zonaRuta21],
		pagoDe("pago:madrugada", m.juan, local(t, 14, "05:40"), 2, 11, "100.00", domain.FormaCobroEfectivo))
	tb, err := servicioV22(t, m).Tablero(context.Background(), desempenoapp.TableroInput{Contrato: "2.2", DesdeHora: "08:00", HastaHora: "21:30"})
	require.NoError(t, err)
	assert.Equal(t, domain.Eje{DesdeHora: 5, HastaHora: 22}, tb.Eje)
	for _, f := range tb.Filas {
		for _, l := range f.Lineas {
			require.Len(t, l.PorHora, 17, f.Usuario.UID)
			assert.Equal(t, 5, l.PorHora[0].Hora)
		}
	}
}

// D20: la zona excluida no aparece, en v2.1 ni en v2.2; la ficha responde 409.
func TestTablero_ZonaExcluidaNoSeLista(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := servicioV22(t, m, desempenoapp.ConZonasExcluidas(fakeZonasExcluidas{zonas: map[int]bool{zonaRuta21: true}}))
	for _, c := range []string{"", "2.2"} {
		tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Contrato: c})
		require.NoError(t, err)
		require.Len(t, tb.Filas, 1, "sólo Lupe, que no tiene zona")
		assert.Equal(t, "uid-lupe", tb.Filas[0].Usuario.UID)
	}
	_, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrZonaExcluida)
	_, err = svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{})
	require.ErrorIs(t, err, domain.ErrZonaExcluida)
	_, err = svc.Actividad(context.Background(), "uid-juan", desempenoapp.FiltroActividadInput{})
	require.ErrorIs(t, err, domain.ErrZonaExcluida)
}

// Sin la lista de zonas el tablero no inventa: falla completo.
func TestTablero_FallaSiNoSeLeenLasZonasExcluidas(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	boom := errors.New("firebird caído")
	svc := servicioV22(t, m, desempenoapp.ConZonasExcluidas(fakeZonasExcluidas{err: boom}))
	_, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)
}
```

Run: `go test ./internal/desempeno/app/ -count=1` · Expected: FAIL de compilación (`desempenoapp.Opcion`, `TableroInput.Contrato`, `PorColumna`…).

- [ ] **Step 3: Implementar el servicio.**

`ports/outbound/ports.go`:

```go
// ZonasExcluidas: zonas fuera de desempeño (D20), leídas de Configuración por
// su contrato (internal/config). Un error tumba la consulta: nunca se lista a
// medias.
type ZonasExcluidas interface {
	ZonasExcluidas(ctx context.Context) (map[int]bool, error)
}
```

`service.go`: campo `zonas outbound.ZonasExcluidas` (por omisión `sinZonasExcluidas{}` que devuelve un mapa vacío), `type Opcion func(*Service)`, `func ConZonasExcluidas(z outbound.ZonasExcluidas) Opcion { return func(s *Service) { s.zonas = z } }`, `NewService(..., loc *time.Location, opts ...Opcion)`. `resolverUsuario` lee las zonas excluidas después de encontrar al usuario y devuelve `domain.ErrZonaExcluida` si `ex[u.ZonaID]`.

`tablero.go`:
1. `TableroInput` gana `Contrato string`; `Tablero` gana `Hoy`, `Contrato`, `Eje`; `FilaTablero` gana `PorColumna`, `PromedioDiario`, `SemanaVencida`, `UltimoMovimiento` (los dos últimos los usa T12; aquí quedan en cero).
2. `Tablero(ctx, in)`: `c, err := ParseContrato(in.Contrato)`; `p = c.Parametros(p)`; lee usuarios; arma el índice de registradores con **todos** ellos (la atribución de una actividad no depende de que su autor se liste); lee `s.zonas.ZonasExcluidas(ctx)` y sólo entonces quita de la lista de filas (`visibles`) a los de zona excluida. Luego `filas, err := s.calcularFilas(ctx, visibles, ix, ahora, periodo, p, c)`; si `c == Contrato22`, `t.Eje = domain.EjeDe(p.Horario, horasDeFilas(filas), s.loc)` y cada `fila.Lineas[i] = fila.Lineas[i].EnEje(t.Eje)`; si no, `t.Eje = domain.EjePorOmision`. Mismo orden por zona y nombre.
3. `calcularFilas` = el cuerpo actual de `semanasDelTablero` + el bucle de `filaTablero`, sin cambiar su lógica de lote.
4. `filaTablero` mueve `propiasSemana := registradasPor(sem.actividades, u)` **dentro** de la rama Semana (D26-1: en Hoy no se usa); en Semana cuenta `dias` del bucle y, en 2.2, `fila.PromedioDiario = promedioDe(fila, dias)`.
5. `llenarCobertura` llena además `fila.PorColumna` con `domain.ColumnaDe(r, hoy)` (siempre; v2.1 no lo serializa) y `PorColumna.SinNada` igual que `PorSegmento.SinNada`.

```go
func promedioDe(f FilaTablero, dias int) *PromedioDiario {
	if dias <= 0 {
		return nil
	}
	d := decimal.NewFromInt(int64(dias))
	return &PromedioDiario{
		Dias:           dias,
		MinutosActivos: int(decimal.NewFromInt(int64(f.MinutosActivos)).Div(d).Round(0).IntPart()),
		PagosImporte:   f.Pagos.Importe.Div(d).Round(2),
		Visitas:        decimal.NewFromInt(int64(f.Visitas)).Div(d).Round(2),
	}
}

func horasDeFilas(fs []FilaTablero) []time.Time {
	var ls []domain.LineaDelDia
	for _, f := range fs {
		ls = append(ls, f.Lineas...)
	}
	return domain.HorasDeLineas(ls)
}
```

- [ ] **Step 4: Verde del servicio** — Run: `go clean -testcache && go test ./internal/desempeno/app/ -count=1` · Expected: PASS, incluidas **sin tocarlas** `TestTablero_*`, `TestCuentas_*` y `TestCuentasYTablero_E_INF_016_*`.

- [ ] **Step 5: Cliente de Configuración** (`infra/clients/config_zonas_client.go`)

```go
// configZonasClient adapta config.ZonasExcluidasDesempenoReader (lo cumple el
// app.Service de config) al puerto outbound.ZonasExcluidas.
type configZonasClient struct{ r config.ZonasExcluidasDesempenoReader }

// NewConfigZonasClient envuelve el lector de Configuración.
func NewConfigZonasClient(r config.ZonasExcluidasDesempenoReader) outbound.ZonasExcluidas {
	return &configZonasClient{r: r}
}

func (c *configZonasClient) ZonasExcluidas(ctx context.Context) (map[int]bool, error) {
	ids, err := c.r.ZonasExcluidasDesempeno(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}
```

Prueba (`config_zonas_client_test.go`): un lector fake con `[23301]` da `{23301: true}`; un error se propaga tal cual (`require.ErrorIs`). Wiring: `provideDesempenoService(pool, rutasSvc, configSvc *configapp.Service)` agrega `desempenoapp.ConZonasExcluidas(desempenoclients.NewConfigZonasClient(configSvc))`; `desempeno_wiring_test.go` gana una aserción de que la opción está puesta (exponer `dependenciasDesempeno.zonas` y exigir que no sea nil).

- [ ] **Step 6: HTTP — pruebas de contrato que fallan** (`contrato_v22_test.go`). Claves copiadas **a mano** del contrato v2.2 (Tarea 0), no de los DTO:

```go
var (
	clavesTableroV22 = []string{"generado_at", "hoy", "periodo", "pausa_min", "eje", "usuarios"}
	clavesUsuarioV22 = []string{
		"uid", "nombre", "email", "zona_id", "zona_nombre", "configurado", "motivo_no_configurado", "inicio_semana",
		"ventas_universo", "estado_ahora", "estado_desde", "ultima", "primera_actividad", "ultima_actividad",
		"minutos_activos", "pausas", "pagos", "pagos_por_forma", "condonaciones", "no_aplicados", "visitas",
		"cobertura", "cobertura_por_columna", "cobro", "lineas",
		"semana_vencida", "ultimo_movimiento", "promedio_diario",
	}
	clavesColumnas = []string{"por_visitar", "no_paga", "pagados", "sin_nada"}
	clavesEje      = []string{"desde_hora", "hasta_hora"}
	clavesPromedio = []string{"dias", "minutos_activos", "pagos_importe", "visitas"}
)

func TestContratoV22_Tablero(t *testing.T) {
	t.Parallel()
	m := get(t, "/desempeno/usuarios?periodo=semana&contrato=2.2")
	claves(t, m, clavesTableroV22, "tablero v2.2")
	claves(t, m["eje"], clavesEje, "eje")
	u := claves(t, primero(t, m["usuarios"], "usuarios"), clavesUsuarioV22, "usuario v2.2")
	claves(t, u["cobertura_por_columna"], clavesColumnas, "cobertura_por_columna")
	if u["promedio_diario"] != nil {
		p := claves(t, u["promedio_diario"], clavesPromedio, "promedio_diario")
		_, esNumero := p["visitas"].(float64)
		assert.True(t, esNumero, "visitas del promedio es número JSON")
		_, esTexto := p["pagos_importe"].(string)
		assert.True(t, esTexto, "pagos_importe es dinero en string")
	}
	fixture(t, "tablero_semana_v22", getCrudo(t, "/desempeno/usuarios?periodo=semana&contrato=2.2"))
}

// v2.1 congelado: la misma petición sin contrato no trae ni una clave nueva.
func TestContratoV22_SinParametroNoTraeClavesNuevas(t *testing.T) {
	t.Parallel()
	m := get(t, "/desempeno/usuarios?periodo=semana")
	claves(t, m, clavesTablero, "tablero v2.1")
	claves(t, primero(t, m["usuarios"], "usuarios"), clavesUsuario, "usuario v2.1")
}

func TestContratoV22_ContratoDesconocidoEs422(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	rec := httptest.NewRecorder()
	router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/desempeno/usuarios?contrato=3", nil))
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	assert.Contains(t, rec.Body.String(), "query.contrato")
}
```

En `robustez_test.go`, copiar `TestTablero_ParametrosDistintosNoSeMezclan` (`:136`) como `TestTablero_ContratosDistintosNoSeMezclan` con `paths` = `"/desempeno/usuarios?periodo=semana"` y `"/desempeno/usuarios?periodo=semana&contrato=2.2"`; asserción: `u.llamadas.Load() == 2` y que la respuesta 2.2 contiene `"eje"` y la 2.1 no.

Run: `go test ./internal/desempeno/infra/desempenohttp/ -count=1` · Expected: FAIL.

- [ ] **Step 7: Implementar HTTP.** `dto_v22.go`:

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

// DTOs del contrato v2.2 (docs/superpowers/specs/2026-10-14-desempeno-contratos.md §8).
// Tipos aparte de los de v2.1 a propósito: v2.1 queda congelado byte a byte
// para el escritorio 1.26.0 (spec §6.1). Regla §0: nunca omitempty.

// EjeDTO — horas [desde_hora, hasta_hora) de la línea.
type EjeDTO struct {
	DesdeHora int `json:"desde_hora"`
	HastaHora int `json:"hasta_hora"`
}

// CoberturaPorColumnaDTO — las tres columnas más sin_nada.
type CoberturaPorColumnaDTO struct {
	PorVisitar int `json:"por_visitar"`
	NoPaga     int `json:"no_paga"`
	Pagados    int `json:"pagados"`
	SinNada    int `json:"sin_nada"`
}

// MovimientoDTO — ultimo_movimiento.
type MovimientoDTO struct {
	ID   string `json:"id"`
	Tipo string `json:"tipo"`
	Hora string `json:"hora"`
}

// PromedioDiarioDTO — totales de la semana entre días transcurridos.
type PromedioDiarioDTO struct {
	Dias           int     `json:"dias"`
	MinutosActivos int     `json:"minutos_activos"`
	PagosImporte   string  `json:"pagos_importe"`
	Visitas        float64 `json:"visitas"`
}

// UsuarioTableroV22DTO — una fila v2.2: la de v2.1 sin cobertura_por_segmento y con los campos nuevos.
type UsuarioTableroV22DTO struct {
	UID                 string                 `json:"uid"`
	Nombre              string                 `json:"nombre"`
	Email               string                 `json:"email"`
	ZonaID              *int                   `json:"zona_id"`
	ZonaNombre          *string                `json:"zona_nombre"`
	Configurado         bool                   `json:"configurado"`
	MotivoNoConfigurado *string                `json:"motivo_no_configurado"`
	InicioSemana        *string                `json:"inicio_semana"`
	VentasUniverso      int                    `json:"ventas_universo"`
	EstadoAhora         string                 `json:"estado_ahora"`
	EstadoDesde         *string                `json:"estado_desde"`
	Ultima              *UltimaDTO             `json:"ultima"`
	PrimeraActividad    *string                `json:"primera_actividad"`
	UltimaActividad     *string                `json:"ultima_actividad"`
	MinutosActivos      int                    `json:"minutos_activos"`
	Pausas              PausasResumenDTO       `json:"pausas"`
	Pagos               ConteoImporteDTO       `json:"pagos"`
	PagosPorForma       PagosPorFormaDTO       `json:"pagos_por_forma"`
	Condonaciones       ConteoImporteDTO       `json:"condonaciones"`
	NoAplicados         ConteoImporteDTO       `json:"no_aplicados"`
	Visitas             int                    `json:"visitas"`
	Cobertura           CoberturaDTO           `json:"cobertura"`
	CoberturaPorColumna CoberturaPorColumnaDTO `json:"cobertura_por_columna"`
	Cobro               CobroDTO               `json:"cobro"`
	Lineas              []LineaDelDiaDTO       `json:"lineas"`
	SemanaVencida       bool                   `json:"semana_vencida"`
	UltimoMovimiento    *MovimientoDTO         `json:"ultimo_movimiento"`
	PromedioDiario      *PromedioDiarioDTO     `json:"promedio_diario"`
}

// TableroV22DTO — GET /desempeno/usuarios?contrato=2.2.
type TableroV22DTO struct {
	GeneradoAt string                 `json:"generado_at"`
	Hoy        string                 `json:"hoy"`
	Periodo    string                 `json:"periodo"`
	PausaMin   int                    `json:"pausa_min"`
	Eje        EjeDTO                 `json:"eje"`
	Usuarios   []UsuarioTableroV22DTO `json:"usuarios"`
}
```

`mapper_v22.go`: `aTableroV22DTO(t)` y `aFilaV22DTO(f)` que parten de `aFilaDTO(f)` (copiando campo por campo, sin duplicar la lógica de nulos) y agregan `CoberturaPorColumna`, `SemanaVencida`, `UltimoMovimiento` (nil → nil), `PromedioDiario` (`Visitas: p.Visitas.InexactFloat64()`, `PagosImporte: dinero(p.PagosImporte)`). `TestDTOs_SinOmitempty` debe incluir los tipos nuevos (agregarlos a su lista si la prueba enumera tipos a mano).

`handlers.go`: `TableroInput` gana `Contrato string \`query:"contrato" doc:"2.1 (por omisión) | 2.2"\``; `claveTablero` agrega `in.Contrato` a la clave (con el comentario: "la versión cambia el cálculo: v2.2 saca a las semanas vencidas del lote"); `TableroOutput` pasa a `struct{ Body any }` y el handler elige `aTableroDTO(t)` o `aTableroV22DTO(t)` según `t.Contrato`. Si Huma rechaza `Body any` al registrar, usar `Body json.RawMessage` con `json.Marshal` del DTO elegido y dejarlo dicho en el comentario.

- [ ] **Step 8: Golden v2.2** — Run: `DESEMPENO_ACTUALIZAR_FIXTURES=1 go test ./internal/desempeno/infra/desempenohttp/ -run TestContratoV22_Tablero -count=1` y **revisar a mano** `testdata/tablero_semana_v22.json` contra el contrato v2.2 (cada clave, cada null). Luego sin la variable: verde.

- [ ] **Step 9: v2.1 sin un byte de diferencia**

Run: `go clean -testcache && go test ./internal/desempeno/... ./cmd/api/... -count=1 && git diff --exit-code -- internal/desempeno/infra/desempenohttp/testdata/tablero_hoy.json internal/desempeno/infra/desempenohttp/testdata/tablero_semana.json internal/desempeno/infra/desempenohttp/testdata/jornada.json internal/desempeno/infra/desempenohttp/testdata/cuentas.json`
Expected: PASS y `git diff` sin salida (exit 0). Si T10 ya está integrada y algún golden v2.1 tenía `pct` con `esperado` "0.00", ese único cambio se acepta citando E-INF-019 y se dice en el reporte.

- [ ] **Step 10: Roja al revertir** — (a) quitar `in.Contrato` de `claveTablero` → `TestTablero_ContratosDistintosNoSeMezclan` rojo; (b) quitar el filtro de zonas excluidas → `TestTablero_ZonaExcluidaNoSeLista` rojo; (c) en `llenarCobertura` contar `PorColumna` con `SegmentoDe` → `TestTablero_V22_CoberturaPorColumna` sigue verde en este escenario (no hay citas ni negativas): **agregar** al escenario de esa prueba una visita "No pagará esta ocasión" de Juan a la venta 8 y esperar `PorVisitar: 2` (la 8 pasa de `despues` a `por_visitar`); con la reversión, rojo. Pegar las tres salidas; restaurar.

- [ ] **Step 11: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/... ./cmd/api/...
git add internal/desempeno cmd/api
git commit -m "feat(desempeno): tablero en contrato 2.2 con tres columnas, eje, promedio diario y zonas excluidas (E-INF-018)"
```

### Task 12: Semanas vencidas fuera del lote y `ultimo_movimiento` (D19, M6, E-INF-020, E-INF-023)

**Gate:** Tarea 3 = SOSTIENE para M6 **y** ningún usuario vencido con actividad propia en 14 días (o el dueño ya decidió qué hacer con ellos).

**Files:**
- Create: `internal/desempeno/app/movimientos.go`, `internal/desempeno/app/tablero_vencidas_test.go`
- Modify: `internal/desempeno/app/tablero.go` (`calcularFilas`, `filaTablero`), `internal/desempeno/app/fakes_test.go` (`fakeActividades` implementa `MovimientosRepo`)
- Modify: `internal/desempeno/ports/outbound/ports.go` (`MovimientosRepo`)
- Create: `internal/desempeno/infra/desempenofb/movimientos_repo.go`, `internal/desempeno/infra/desempenofb/movimientos_repo_test.go`
- Modify: `internal/desempeno/infra/desempenofb/medicion_tablero_test.go` (`MEDIR_TODOS_VIEJOS`, `MEDIR_CONTRATO`, `TestMedirUltimoMovimiento`)
- Modify: `internal/desempeno/infra/desempenohttp/mapper_v22.go` (ya mapea; sólo prueba)
- Modify: `cmd/api/desempeno_wiring_test.go`
- Create (sólo si el Step 6 lo exige): `migrations-firebird/000067_idx_movimientos_por_cobrador.up.sql`/`.down.sql`

**Interfaces:**
- Consumes: `UsuarioCobranza.SemanaVencidaEn` (T5); `FilaTablero.SemanaVencida/UltimoMovimiento`, `calcularFilas` (T11).
- Produces:
  ```go
  // ports/outbound
  type ConsultaMovimientos struct { CobradorIDs []int; UIDs []string; Desde, Hasta time.Time }
  type MovimientosRepo interface {
      // MovimientosRecientes: pagos capturados por la app (MSP_PAGOS_RECIBIDOS, por COBRADOR_ID o CREATED_BY→FIREBASE_UID)
      // y visitas (MSP_VISITAS.COBRADOR_ID) con hora en [Desde, Hasta], de más reciente a más antigua. Cada Actividad
      // trae ID, Tipo, Hora y RegistradoPor{UsuarioUID, CobradorID, Origen: app}; nada más.
      MovimientosRecientes(ctx context.Context, c ConsultaMovimientos) ([]domain.Actividad, error)
  }
  ```
  `*desempenofb.ActividadesRepo` lo implementa; el servicio lo detecta por aserción de tipo (como los lotes) y **falla** con `domain.ErrLoteIncompleto` si hace falta y no está.

- [ ] **Step 1: Pruebas que fallan** (`tablero_vencidas_test.go`)

```go
// E-INF-020: en v2.2 una semana vencida no entra al lote; su fila sale vacía,
// configurada y marcada, con último movimiento.
func TestTablero_V22_SemanaVencidaFueraDelLote(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	viejo := m.ahora.AddDate(0, 0, -338)
	ivan := domain.UsuarioCobranza{UID: "uid-ivan", Nombre: "Iván Vázquez Aldama", CobradorID: 11800,
		ZonaID: zonaRuta34, ZonaNombre: "MAYOREO", InicioSemana: &viejo}
	m.usuarios.usuarios = append(m.usuarios.usuarios, ivan)
	m.actividades.movimientos = []domain.Actividad{
		visitaDe("visita:vieja", ivan, m.ahora.AddDate(0, 0, -40), 0, 99, domain.TipoVisitaNoSeEncontraba),
	}
	lote := &fakeActividadesLote{fakeActividades: m.actividades}
	svc := desempenoapp.NewService(m.usuarios, lote, m.cuentas, relojFijo{m.ahora}, cdmx(t))

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana", Contrato: "2.2"})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-ivan")
	assert.True(t, f.Configurado)
	assert.True(t, f.SemanaVencida)
	assert.Empty(t, f.Lineas)
	assert.Nil(t, f.Cobro.Pct)
	assert.Nil(t, f.PromedioDiario)
	assert.Equal(t, 0, f.CoberturaTotal)
	require.NotNil(t, f.UltimoMovimiento)
	assert.Equal(t, "visita:vieja", f.UltimoMovimiento.ID)
	for _, c := range lote.lotes[0] {
		assert.NotEqual(t, zonaRuta34, c.ZonaID, "la ventana del vencido no se lee")
	}

	// v2.1: el mismo usuario se sigue calculando completo (spec §6.1, costo aceptado).
	tb21, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana"})
	require.NoError(t, err)
	assert.False(t, filaDe(t, tb21, "uid-ivan").SemanaVencida)
}

// Review Focus 1: un vencido que comparte zona con uno vigente no lo vacía.
func TestTablero_V22_VencidoEnZonaCompartidaNoTocaAlVigente(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := desempenoapp.NewService(m.usuarios, m.actividades, m.cuentas, relojFijo{m.ahora}, cdmx(t))
	antes, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana", Contrato: "2.2"})
	require.NoError(t, err)

	viejo := m.ahora.AddDate(0, 0, -30)
	m.usuarios.usuarios = append(m.usuarios.usuarios, domain.UsuarioCobranza{
		UID: "uid-beto", Nombre: "Alberto Ruiz Cano", CobradorID: 11900, ZonaID: zonaRuta21, ZonaNombre: "RUTA 21", InicioSemana: &viejo})
	despues, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana", Contrato: "2.2"})
	require.NoError(t, err)
	assert.Equal(t, filaDe(t, antes, "uid-juan"), filaDe(t, despues, "uid-juan"))
	assert.True(t, filaDe(t, despues, "uid-beto").SemanaVencida)
}

// ultimo_movimiento de un vigente sale de su ventana sin otra lectura; nulo a 92 d + 1 min.
func TestTablero_V22_UltimoMovimiento(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := desempenoapp.NewService(m.usuarios, m.actividades, m.cuentas, relojFijo{m.ahora}, cdmx(t))
	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Contrato: "2.2"})
	require.NoError(t, err)
	juan := filaDe(t, tb, "uid-juan")
	require.NotNil(t, juan.UltimoMovimiento)
	assert.Equal(t, "pago:p-80", juan.UltimoMovimiento.ID, "la última propia de la ventana")
	assert.Zero(t, m.actividades.llamadasMovimientos, "con actividad en la ventana no se consulta aparte")

	viejo := m.ahora.AddDate(0, 0, -200)
	m.usuarios.usuarios = append(m.usuarios.usuarios, domain.UsuarioCobranza{
		UID: "uid-ana", Nombre: "Ana Lilia Ortega", CobradorID: 12000, ZonaID: zonaRuta34, ZonaNombre: "RUTA 34", InicioSemana: &viejo})
	m.actividades.movimientos = []domain.Actividad{
		visitaDe("visita:fuera", domain.UsuarioCobranza{CobradorID: 12000}, m.ahora.Add(-92*24*time.Hour-time.Minute), 0, 98, domain.TipoVisitaNoSeEncontraba),
	}
	tb, err = svc.Tablero(context.Background(), desempenoapp.TableroInput{Contrato: "2.2"})
	require.NoError(t, err)
	assert.Nil(t, filaDe(t, tb, "uid-ana").UltimoMovimiento, "92 d + 1 min: nada")
	require.Equal(t, 1, m.actividades.llamadasMovimientos, "una sola consulta por lote")
	assert.Equal(t, m.ahora.Add(-92*24*time.Hour), m.actividades.ultimaConsultaMovimientos.Desde)
}
```

En `fakes_test.go`, `fakeActividades` gana `movimientos []domain.Actividad`, `llamadasMovimientos int`, `ultimaConsultaMovimientos outbound.ConsultaMovimientos` y

```go
func (f *fakeActividades) MovimientosRecientes(_ context.Context, c outbound.ConsultaMovimientos) ([]domain.Actividad, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llamadasMovimientos++
	f.ultimaConsultaMovimientos = c
	var out []domain.Actividad
	for _, a := range f.movimientos {
		if !a.Hora.Before(c.Desde) && !a.Hora.After(c.Hasta) {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hora.After(out[j].Hora) })
	return out, nil
}
```

Run: `go test ./internal/desempeno/app/ -run 'TestTablero_V22_' -count=1` · Expected: FAIL.

- [ ] **Step 2: Implementar** — en `calcularFilas` (sólo con `c == Contrato22`): saltar de las claves del lote a los usuarios con `u.SemanaVencidaEn(ahora)`; en `filaTablero`, si 2.2 y vencida: `fila := filaVacia(u, ""); fila.Configurado = true; fila.SemanaVencida = true; return fila`. Después de armar las filas, `s.llenarUltimoMovimiento(ctx, filas, ix, ahora, semanas)` en `movimientos.go`:

```go
// llenarUltimoMovimiento (D19): la actividad más reciente que registró cada
// usuario configurado en los últimos 92 días. Si ya hay una propia en la
// ventana leída, sale de ahí; para el resto (vencidos y quien no hizo nada en
// su semana) UNA lectura por lote, con la misma atribución del tablero
// (IndiceUsuarios.Completar + RegistradaPor).
func (s *Service) llenarUltimoMovimiento(ctx context.Context, filas []FilaTablero, ix domain.IndiceUsuarios,
	ahora time.Time, propias func(i int) []domain.Actividad) error {
	var faltan []int
	for i := range filas {
		if !filas[i].Configurado {
			continue
		}
		if ps := propias(i); len(ps) > 0 {
			ultima := ps[len(ps)-1] // ordenadas por hora
			filas[i].UltimoMovimiento = &UltimoMovimiento{ID: ultima.ID, Tipo: ultima.Tipo, Hora: ultima.Hora}
			continue
		}
		faltan = append(faltan, i)
	}
	if len(faltan) == 0 {
		return nil
	}
	repo, ok := s.actividades.(outbound.MovimientosRepo)
	if !ok {
		return domain.ErrLoteIncompleto
	}
	c := outbound.ConsultaMovimientos{Desde: ahora.Add(-RangoMaximo), Hasta: ahora}
	for _, i := range faltan {
		u := filas[i].Usuario
		c.UIDs = append(c.UIDs, u.UID)
		if u.CobradorID > 0 {
			c.CobradorIDs = append(c.CobradorIDs, u.CobradorID)
		}
	}
	acts, err := repo.MovimientosRecientes(ctx, c)
	if err != nil {
		return err
	}
	for _, i := range faltan {
		for _, a := range acts { // de más reciente a más antigua
			if domain.RegistradaPor(ix.Completar(a), filas[i].Usuario) {
				filas[i].UltimoMovimiento = &UltimoMovimiento{ID: a.ID, Tipo: a.Tipo, Hora: a.Hora}
				break
			}
		}
	}
	return nil
}
```

`propias(i)`: en Semana, las `propiasSemana` ya ordenadas de esa fila; en Hoy, las de hoy si hay; si no, `registradasPor(sem.actividades, u)` ordenadas (sólo para esa fila). Guardarlas en un slice paralelo a `filas` dentro de `calcularFilas`, no en `FilaTablero`. **Sólo en 2.2** (v2.1 no gasta ni una lectura más).

- [ ] **Step 3: Verde** — Run: `go clean -testcache && go test ./internal/desempeno/... -count=1` · PASS.

- [ ] **Step 4: Repositorio** — prueba de integración primero (`movimientos_repo_test.go`, patrón de `actividades_seed_test.go`, todo en `WithTestTransaction`, datos con nombres mexicanos realistas): sembrar una captura en `MSP_PAGOS_RECIBIDOS` con `CREATED_BY` de un usuario sembrado y una visita en `MSP_VISITAS` con `COBRADOR_ID`, una dentro y una fuera del rango; esperar sólo las de dentro, en orden descendente, con `ID` = `domain.IDPagoAplicado(impte)` si `IMPTE_DOCTO_CC_ID` no es nulo y `domain.IDPagoPendiente(id)` si lo es, `ID` = `domain.IDVisita(id)` para la visita, y `RegistradoPor.UsuarioUID` = `FIREBASE_UID` del creador. Leer antes `actividades_seed_test.go` y `actividades_repo.go:440-560` para las columnas NOT NULL de cada tabla y los tipos (`MSP_PAGOS_RECIBIDOS.ID` es `NONE`; `COBRADOR` UTF8).

Implementación (`movimientos_repo.go`): dos sentencias sin tablas derivadas (firebirdsql v0.9.19 no enlaza `?` dentro de ellas, `actividades_repo.go:39-41`), con la lista `IN` armada con `?` por elemento en tandas de 500:

```sql
SELECT pr.ID, pr.IMPTE_DOCTO_CC_ID, pr.FORMA_COBRO_ID, pr.CONCEPTO_CC_ID, pr.FECHA, pr.COBRADOR_ID, u.FIREBASE_UID
FROM MSP_PAGOS_RECIBIDOS pr
LEFT JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE pr.FECHA >= ? AND pr.FECHA <= ?
  AND (pr.COBRADOR_ID IN (?, …) OR u.FIREBASE_UID IN (?, …))
```

```sql
SELECT v.ID, v.FECHA, v.COBRADOR_ID
FROM MSP_VISITAS v
WHERE v.FECHA >= ? AND v.FECHA <= ? AND v.COBRADOR_ID IN (?, …)
```

Fechas con `firebird.ToWallClock`, lectura con `firebird.ScanUTCTime`; el orden descendente se hace en Go (`sort.SliceStable` por hora y luego por ID) para no depender del plan. `Tipo` del pago con `domain.ClasificarTipoPago(forma, concepto)`. Run: `set -a && . ./.env && set +a && go clean -testcache && go test -timeout 1800s -count=1 ./internal/desempeno/infra/desempenofb/ -run TestMovimientosRecientes` · PASS.

- [ ] **Step 5: Medir el costo y el plan antes de darla por buena (spec §3.2, E-INF-023).** Agregar a `medicion_tablero_test.go` `TestMedirUltimoMovimiento` (se salta sin `MEDIR_TABLERO`): con el reloj de dev, pide `MovimientosRecientes` para los 55 cobradores de la medición y para 5, frío y tibio; imprime tiempo, filas leídas y el `PLAN` de las dos sentencias (obtenerlo con `isql` `SET PLANONLY ON;` sobre el texto exacto, o con la herramienta que ya use `E-INF-015`; leer cómo se obtuvo allí). Run: `MEDIR_TABLERO=1 go test -timeout 1800s -count=1 -run TestMedirUltimoMovimiento -v ./internal/desempeno/infra/desempenofb/`.

- [ ] **Step 6: Decidir el índice con el número.** Si alguna de las dos sentencias pasa de **200 ms tibio** en dev o su plan recorre `MSP_VISITAS`/`MSP_PAGOS_RECIBIDOS` entero (`NATURAL`), escribir `000067_idx_movimientos_por_cobrador` con `CREATE INDEX IDX_MSP_VISITAS_COB_FECHA ON MSP_VISITAS (COBRADOR_ID, FECHA);` y `CREATE INDEX IDX_MSP_PAGOS_REC_COB_FECHA ON MSP_PAGOS_RECIBIDOS (COBRADOR_ID, FECHA);` + `SET STATISTICS` de ambos + el `INSERT INTO MSP_MIGRATIONS (67, …)`, con la cabecera explicativa al estilo de 000065 (memoria: en producción `CREATE INDEX` se aplica **sólo con el API detenido** y con tope). Aplicarla en dev sólo con autorización del dueño (`make fb-snapshot` antes) y repetir el Step 5. Si no hace falta, no se escribe y E-INF-023 dice por qué con las cifras.

- [ ] **Step 7: Rendimiento del tablero con vencidos (spec §8.4).** En `medicion_tablero_test.go` agregar `MEDIR_TODOS_VIEJOS=1` (los 55 usuarios con inicio `ahora − 338 d`) y `MEDIR_CONTRATO` (pasa a `TableroInput.Contrato`). Correr las cuatro combinaciones {v2.1, v2.2} × {hoy, semana}, frío y tibio, con y sin `MEDIR_TODOS_VIEJOS`, y registrar tiempo, sentencias y tamaño de la respuesta serializada. Criterio de aceptación: v2.2 con los 55 vencidos cuesta lo mismo (±20 %) que v2.2 sin vencidos; v2.1 con vencidos reproduce el orden de magnitud medido (44 s / 16.7 MB). Registrar todo en **E-INF-023** con fecha, dev, reloj y control positivo (la corrida v2.1).

- [ ] **Step 8: Roja al revertir** — quitar el salto de vencidos en `calcularFilas` → `TestTablero_V22_SemanaVencidaFueraDelLote` rojo (la zona 34 aparece en el lote); quitar el atajo de la ventana en `llenarUltimoMovimiento` → `llamadasMovimientos` deja de ser 0 → rojo. Restaurar.

- [ ] **Step 9: Wiring** — `desempeno_wiring_test.go`: exigir `d.actividades.(desempenooutbound.MovimientosRepo)` ok (como los lotes; un decorador que no lo reexponga degradaría a error).

- [ ] **Step 10: Evidencia y commit** — E-INF-020 gana "Prueba roja/verde" y "Consumidores" (`grep -rn "SemanaVencida\|SemanaVencidaEn" internal/`); E-INF-023 nueva.

```bash
golangci-lint run ./internal/desempeno/... ./cmd/api/...
git add internal/desempeno cmd/api docs/evidencia/infra.md migrations-firebird
git commit -m "perf(desempeno): semanas vencidas fuera del lote del tablero y ultimo movimiento en 92 dias (E-INF-020, E-INF-023)"
```

### Task 13: Ficha en contrato 2.2 — fila por usuario, jornada, cuentas, actividad

**Depende de:** T12.

**Files:**
- Create: `internal/desempeno/app/usuario.go`, `internal/desempeno/app/usuario_test.go`, `internal/desempeno/app/ficha_v22_test.go`
- Modify: `internal/desempeno/app/jornada.go`, `internal/desempeno/app/cuentas.go`, `internal/desempeno/app/actividad.go:36-53,166-189`
- Modify: `internal/desempeno/infra/desempenohttp/{dto_v22,mapper_v22,handlers,routes}.go`, `contrato_v22_test.go`; Create `testdata/usuario_v22.json`, `testdata/jornada_v22.json`, `testdata/cuentas_v22.json`

**Interfaces:**
- Consumes: `calcularFilas`, `Contrato`, `ParseContrato` (T11); `DiasNavegables`, `ErrUsuarioSemanaVencida` (T5); `ParseColumna`, `ColumnaDe` (T4); `EjeDe`, `EnEje` (T6).
- Produces:
  ```go
  type VistaUsuario struct { GeneradoAt time.Time; Hoy domain.Fecha; Periodo Periodo; PausaMin int; Eje domain.Eje; Fila FilaTablero; InicioSemanaDia *domain.Fecha; DiasNavegables []domain.Fecha }
  func (s *Service) Usuario(ctx context.Context, uid string, in TableroInput) (VistaUsuario, error) // exige Contrato22
  // JornadaInput, CuentasInput, FiltroActividadInput ganan Contrato string; CuentasInput gana Columna string
  // VistaJornada gana Hoy domain.Fecha; Eje domain.Eje; Horario *domain.Horario; Contrato Contrato
  // CuentaVista gana Columna domain.Columna; VistaCuentas gana Columnas ConteosColumnas; Contrato Contrato
  type ConteosColumnas struct { PorVisitar, NoPaga, Pagados, SinNada, Total int }
  ```
  HTTP: `GET /desempeno/usuarios/{uid}` (OperationID `desempeno-usuario`), sólo con `contrato=2.2`.

- [ ] **Step 1: Pruebas que fallan** (`usuario_test.go`, `ficha_v22_test.go`)

```go
func TestUsuario_V22_UnaFilaSoloDeSuZona(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	lote := &fakeActividadesLote{fakeActividades: m.actividades}
	svc := desempenoapp.NewService(m.usuarios, lote, m.cuentas, relojFijo{m.ahora}, cdmx(t))
	v, err := svc.Usuario(context.Background(), "uid-juan", desempenoapp.TableroInput{Contrato: "2.2"})
	require.NoError(t, err)
	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Contrato: "2.2"})
	require.NoError(t, err)
	esperada := filaDe(t, tb, "uid-juan")
	esperada.Lineas = v.Fila.Lineas // el eje de la fila sola puede ser más angosto que el del tablero
	assert.Equal(t, esperada, v.Fila, "misma fila que en el tablero")
	assert.Equal(t, "2026-10-09", v.InicioSemanaDia.String())
	require.Len(t, v.DiasNavegables, 6)
	assert.Equal(t, "2026-10-14", v.Hoy.String())
	for _, l := range lote.lotes {
		for _, c := range l {
			assert.Equal(t, zonaRuta21, c.ZonaID, "sólo la zona del usuario")
		}
	}
	_, err = svc.Usuario(context.Background(), "uid-juan", desempenoapp.TableroInput{})
	require.ErrorIs(t, err, domain.ErrParametroInvalido, "sin contrato=2.2 no existe")
	_, err = svc.Usuario(context.Background(), "uid-nadie", desempenoapp.TableroInput{Contrato: "2.2"})
	require.ErrorIs(t, err, domain.ErrUsuarioNoEncontrado)
}

func TestJornada_V22_HoyEjeHorarioYCondonaciones(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	v, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{Contrato: "2.2", DesdeHora: "08:00", HastaHora: "18:00"})
	require.NoError(t, err)
	assert.Equal(t, "2026-10-14", v.Hoy.String())
	require.NotNil(t, v.Horario)
	assert.Equal(t, domain.Eje{DesdeHora: 7, HastaHora: 20}, v.Eje)
	assert.Len(t, v.Jornada.Linea.PorHora, 13)
	assert.Equal(t, "50.00", v.Jornada.Resumen.Condonaciones.Importe.StringFixed(2))

	v21, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{DesdeHora: "08:00", HastaHora: "18:00"})
	require.NoError(t, err)
	assert.Equal(t, v21.Jornada.Resumen.Pagos, v.Jornada.Resumen.Pagos)
}

func TestCuentas_V22_ColumnaYConteos(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	v, err := svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{Contrato: "2.2"})
	require.NoError(t, err)
	assert.Equal(t, desempenoapp.ConteosColumnas{PorVisitar: 2, NoPaga: 0, Pagados: 6, SinNada: 1, Total: 8}, v.Columnas)
	for _, c := range v.Cuentas {
		assert.Equal(t, domain.ColumnaDe(c.Resultado, domain.DiaDe(m.ahora, cdmx(t))), c.Columna)
	}
	f, err := svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{Contrato: "2.2", Columna: "pagados"})
	require.NoError(t, err)
	assert.Len(t, f.Cuentas, 6)
	_, err = svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{Contrato: "2.2", Segmento: "volver"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido, "segmento no existe en 2.2")
	_, err = svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{Contrato: "2.2", Columna: "volver"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
	sn, err := svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{Contrato: "2.2", Columna: "sin_nada"})
	require.NoError(t, err)
	assert.Len(t, sn.Cuentas, 1)
}

// DA-2: cuentas de semana vencida → 409 en 2.2; v2.1 sin cambio.
func TestCuentas_V22_SemanaVencidaEs409(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	viejo := m.ahora.AddDate(0, 0, -20)
	for i := range m.usuarios.usuarios {
		if m.usuarios.usuarios[i].UID == "uid-juan" {
			m.usuarios.usuarios[i].InicioSemana = &viejo
		}
	}
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	_, err := svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{Contrato: "2.2"})
	require.ErrorIs(t, err, domain.ErrUsuarioSemanaVencida)
	_, err = svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{})
	require.NoError(t, err, "v2.1 congelado")
}

// §3.2: actividad por omisión de semana vencida = últimos 14 días; explícito conserva 92 d y su 422.
func TestActividad_V22_RangoPorOmisionDeSemanaVencida(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	viejo := m.ahora.AddDate(0, 0, -338)
	for i := range m.usuarios.usuarios {
		if m.usuarios.usuarios[i].UID == "uid-juan" {
			m.usuarios.usuarios[i].InicioSemana = &viejo
		}
	}
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	_, err := svc.Actividad(context.Background(), "uid-juan", desempenoapp.FiltroActividadInput{Contrato: "2.2"})
	require.NoError(t, err)
	llamada := m.actividades.llamadas[len(m.actividades.llamadas)-1]
	assert.Equal(t, m.ahora.Add(-14*24*time.Hour), llamada.desde)
	assert.Equal(t, m.ahora, llamada.hasta)
	_, err = svc.Actividad(context.Background(), "uid-juan", desempenoapp.FiltroActividadInput{
		Contrato: "2.2", Desde: m.ahora.AddDate(0, 0, -93).Format(time.RFC3339)})
	require.ErrorIs(t, err, domain.ErrParametroInvalido, "explícito > 92 d sigue siendo 422")
}
```

Run: `go test ./internal/desempeno/app/ -count=1` · FAIL.

- [ ] **Step 2: Implementar** — `usuario.go`:

```go
// Usuario es una sola fila del tablero calculada sólo para la zona del usuario
// (spec §3.5): la ficha deja de pedir el tablero de toda la flota cada minuto.
func (s *Service) Usuario(ctx context.Context, uid string, in TableroInput) (VistaUsuario, error) {
	c, err := ParseContrato(in.Contrato)
	if err != nil {
		return VistaUsuario{}, err
	}
	if c != Contrato22 {
		return VistaUsuario{}, invalido("contrato")
	}
	periodo, err := parsePeriodo(in.Periodo)
	if err != nil {
		return VistaUsuario{}, err
	}
	p, err := domain.NuevosParametrosJornada(in.PausaMin, in.DesdeHora, in.HastaHora)
	if err != nil {
		return VistaUsuario{}, err
	}
	p = c.Parametros(p)
	cx, err := s.resolverUsuario(ctx, uid) // ya devuelve ErrZonaExcluida
	if err != nil {
		return VistaUsuario{}, err
	}
	filas, err := s.calcularFilas(ctx, []domain.UsuarioCobranza{cx.usuario}, cx.indice, cx.ahora, periodo, p, c)
	if err != nil {
		return VistaUsuario{}, err
	}
	f := filas[0]
	eje := domain.EjeDe(p.Horario, domain.HorasDeLineas(f.Lineas), s.loc)
	for i := range f.Lineas {
		f.Lineas[i] = f.Lineas[i].EnEje(eje)
	}
	v := VistaUsuario{
		GeneradoAt: cx.ahora, Hoy: domain.DiaDe(cx.ahora, s.loc), Periodo: periodo, PausaMin: p.PausaMin,
		Eje: eje, Fila: f, DiasNavegables: domain.DiasNavegables(cx.usuario, cx.ahora, s.loc),
	}
	if cx.usuario.InicioSemana != nil {
		d := domain.DiaDe(*cx.usuario.InicioSemana, s.loc)
		v.InicioSemanaDia = &d
	}
	if v.DiasNavegables == nil {
		v.DiasNavegables = []domain.Fecha{}
	}
	return v, nil
}
```

`calcularFilas` necesita el índice con **todos** los usuarios (ya lo trae `resolverUsuario`) y debe llenar `ultimo_movimiento` también aquí (lo hace por dentro desde T12).

`jornada.go`: `ParseContrato(in.Contrato)`, `p = c.Parametros(p)`; en 2.2, `eje := EjeDe(p.Horario, horas de j.Linea.Puntos)` y `j.Linea = j.Linea.EnEje(eje)`; la vista lleva `Hoy`, `Eje`, `Horario: p.Horario`, `Contrato`. En 2.1 todo igual que hoy.

`cuentas.go`: `parseFiltroCuentas(in, c)`: en 2.2, `in.Segmento != ""` → `invalido("segmento")`; `in.Columna` ∈ {"", columnas, `sin_nada`} si no `invalido("columna")`; en 2.1 se ignora `Columna`. En `Cuentas`: tras `resolverUsuario`, si 2.2 y `c.usuario.SemanaVencidaEn(c.ahora)` → `domain.ErrUsuarioSemanaVencida` (antes de leer nada). Cada `CuentaVista.Columna = domain.ColumnaDe(r, hoy)` y `contarColumna(&vista.Columnas, col)`; `vista.Columnas.SinNada`/`Total` como los de segmento. El filtro en 2.2 compara `Columna`.

`actividad.go`: `FiltroActividadInput.Contrato`; `parseRango(desdeS, hastaS, c, contrato)`: en el `default` (sin `desde`), si 2.2 y `c.usuario.SemanaVencidaEn(c.ahora)`, `desde = hasta.Add(-domain.DiasSemanaVencida * 24 * time.Hour)`; el resto sin cambio.

- [ ] **Step 3: Verde del servicio** — Run: `go clean -testcache && go test ./internal/desempeno/app/ -count=1` · PASS (las de v2.1 intactas).

- [ ] **Step 4: HTTP.** DTOs nuevos en `dto_v22.go`:

```go
// UsuarioV22DTO — GET /desempeno/usuarios/{uid}?contrato=2.2.
type UsuarioV22DTO struct {
	GeneradoAt      string               `json:"generado_at"`
	Hoy             string               `json:"hoy"`
	Periodo         string               `json:"periodo"`
	PausaMin        int                  `json:"pausa_min"`
	Eje             EjeDTO               `json:"eje"`
	Usuario         UsuarioTableroV22DTO `json:"usuario"`
	InicioSemanaDia *string              `json:"inicio_semana_dia"`
	DiasNavegables  []string             `json:"dias_navegables"`
}

// HorarioDTO — eco del horario de la consulta.
type HorarioDTO struct {
	DesdeHora string `json:"desde_hora"`
	HastaHora string `json:"hasta_hora"`
}

// ResumenJornadaV22DTO — condonaciones con importe.
type ResumenJornadaV22DTO struct {
	PrimeraActividad *string          `json:"primera_actividad"`
	UltimaActividad  *string          `json:"ultima_actividad"`
	MinutosActivos   int              `json:"minutos_activos"`
	Pausas           PausasResumenDTO `json:"pausas"`
	Pagos            ConteoImporteDTO `json:"pagos"`
	Visitas          int              `json:"visitas"`
	Condonaciones    ConteoImporteDTO `json:"condonaciones"`
	Clientes         int              `json:"clientes"`
}

// JornadaV22DTO — jornada con hoy, eje y horario.
type JornadaV22DTO struct {
	UID         string               `json:"uid"`
	Fecha       string               `json:"fecha"`
	Hoy         string               `json:"hoy"`
	PausaMin    int                  `json:"pausa_min"`
	GeneradoAt  string               `json:"generado_at"`
	Eje         EjeDTO               `json:"eje"`
	Horario     *HorarioDTO          `json:"horario"`
	Actividades []ActividadDTO       `json:"actividades"`
	Linea       LineaDelDiaDTO       `json:"linea"`
	Senales     []SenalDTO           `json:"senales"`
	Resumen     ResumenJornadaV22DTO `json:"resumen"`
}

// ConteosColumnasDTO — conteos de cuentas v2.2.
type ConteosColumnasDTO struct {
	PorVisitar int `json:"por_visitar"`
	NoPaga     int `json:"no_paga"`
	Pagados    int `json:"pagados"`
	SinNada    int `json:"sin_nada"`
	Total      int `json:"total"`
}

// CuentaV22DTO — CuentaDTO con columna (nunca null) en lugar de segmento.
type CuentaV22DTO struct {
	VentaID         int                 `json:"venta_id"`
	DoctoPVID       *int                `json:"docto_pv_id"`
	Folio           *string             `json:"folio"`
	ClienteID       int                 `json:"cliente_id"`
	ClienteNombre   string              `json:"cliente_nombre"`
	Producto        *string             `json:"producto"`
	Estado          string              `json:"estado"`
	Trato           string              `json:"trato"`
	Etiqueta        string              `json:"etiqueta"`
	Detalle         string              `json:"detalle"`
	Columna         string              `json:"columna"`
	ConAlgo         bool                `json:"con_algo"`
	DeterminadoPor  *string             `json:"determinado_por"`
	UltimaActividad *ActividadCuentaDTO `json:"ultima_actividad"`
	Saldo           string              `json:"saldo"`
	Parcialidad     string              `json:"parcialidad"`
	AbonoSemana     string              `json:"abono_semana"`
	NumAtrasos      int                 `json:"num_atrasos"`
	Nueva           bool                `json:"nueva"`
}

// CuentasV22DTO — GET /desempeno/usuarios/{uid}/cuentas?contrato=2.2.
type CuentasV22DTO struct {
	InicioSemana string             `json:"inicio_semana"`
	Conteos      ConteosColumnasDTO `json:"conteos"`
	Cuentas      []CuentaV22DTO     `json:"cuentas"`
}
```

Mappers en `mapper_v22.go` partiendo de los de v2.1. Inputs ganan `Contrato string \`query:"contrato"\`` (y `Columna string \`query:"columna"\`` en Cuentas); `JornadaOutput`/`CuentasOutput` pasan a `Body any` igual que el tablero; `UsuarioInput{UID path; Periodo, PausaMin, DesdeHora, HastaHora, Contrato query}`. Ruta nueva en `routes.go`:

```go
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-usuario", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}",
		Summary: "Fila del tablero de un usuario (contrato 2.2)", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Usuario)
```

Pruebas en `contrato_v22_test.go` (claves copiadas a mano del contrato): `clavesUsuarioFicha = {"generado_at","hoy","periodo","pausa_min","eje","usuario","inicio_semana_dia","dias_navegables"}`, `clavesJornadaV22 = {"uid","fecha","hoy","pausa_min","generado_at","eje","horario","actividades","linea","senales","resumen"}`, `clavesCuentasV22`, `clavesCuentaV22` (la de v2.1 con `columna` en lugar de `segmento`), `clavesConteosV22 = {"por_visitar","no_paga","pagados","sin_nada","total"}`; `resumen.condonaciones` es objeto `{n, importe}`; golden `usuario_v22`, `jornada_v22`, `cuentas_v22` (generar con `DESEMPENO_ACTUALIZAR_FIXTURES=1` y **revisar a mano**); 409 `code=desempeno_zona_excluida` en los cuatro endpoints por usuario con un servicio con zona excluida; 409 `code=desempeno_semana_vencida` en cuentas 2.2; 422 `query.contrato` en `/desempeno/usuarios/{uid}` sin contrato; los golden v2.1 sin un byte de diferencia (mismo `git diff --exit-code` de la Tarea 11, Step 9).

- [ ] **Step 5: Verde completo** — Run: `go clean -testcache && go test ./internal/desempeno/... ./cmd/api/... -count=1 && golangci-lint run ./internal/desempeno/... ./cmd/api/...` · PASS.

- [ ] **Step 6: Roja al revertir** — quitar la exclusión de `resolverUsuario` → las cuatro pruebas 409 rojas; quitar el 409 de vencida → `TestCuentas_V22_SemanaVencidaEs409` roja; quitar el recorte a 14 d → `TestActividad_V22_RangoPorOmisionDeSemanaVencida` roja. Restaurar.

- [ ] **Step 7: Commit**

```bash
git add internal/desempeno
git commit -m "feat(desempeno): ficha en contrato 2.2 con fila por usuario, jornada con eje, cuentas por columna y actividad de semana vencida (E-INF-018, E-INF-020)"
```

### Task 14: Arnés de equivalencia, rendimiento final y compuerta DESPUÉS del API

**Depende de:** T10–T13 integradas.

**Files:**
- Modify: `internal/desempeno/infra/desempenofb/equivalencia_tablero_test.go` (proyección v2.1, normalización de `pct`, prueba de columnas)
- Modify: `docs/evidencia/infra.md` (E-INF-018, 019, 020, 023: arnés y compuerta después)

**Interfaces:**
- Consumes: `TestEquivalenciaDesempeno` (`:291`), `grabarTodo` (`:214`), `compararGrabaciones`.
- Produces: evidencia MEDIDA de que v2.1 no cambió salvo lo permitido y de que v2.2 sólo difiere por la tabla de §3.1.

- [ ] **Step 1: Grabar la referencia con el código anterior** (spec §8.3-1). En un worktree de `e1762f5`:

Run: `git -C /Volumes/M2-1TB/Developer/msp-api worktree add ../msp-api-ref-e1762f5 e1762f5 && cd ../msp-api-ref-e1762f5 && cp ../msp-api/.env . && set -a && . ./.env && set +a && EQUIV_GRABAR=$HOME/Documents/msp-gestion/mediciones/2026-10-16-desempeno/equiv-e1762f5.json go test -timeout 3600s -count=1 -run TestEquivalenciaDesempeno ./internal/desempeno/infra/desempenofb/`
Expected: PASS y el archivo escrito (fuera del repo: trae nombres de dev).

- [ ] **Step 2: Proyección v2.1 en el arnés.** En `grabacion.poner`, antes de serializar, quitar recursivamente las claves que sólo existen desde este plan (los structs de app ganaron campos que v2.1 no serializa):

```go
// camposV22 existen en los structs de app desde el contrato 2.2 y no se
// serializan en v2.1: se quitan para comparar v2.1 contra la grabación de e1762f5.
var camposV22 = map[string]bool{
	"Hoy": true, "Contrato": true, "Eje": true, "PorColumna": true, "PromedioDiario": true,
	"SemanaVencida": true, "UltimoMovimiento": true, "Columna": true, "Columnas": true, "Horario": true,
}

func sinCamposV22(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k := range x {
			if camposV22[k] {
				delete(x, k)
				continue
			}
			x[k] = sinCamposV22(x[k])
		}
	case []any:
		for i := range x {
			x[i] = sinCamposV22(x[i])
		}
	}
	return v
}
```

(Leer `poner` primero: si serializa con `json.MarshalIndent`, hacer `Marshal` → `Unmarshal` a `any` → `sinCamposV22` → `MarshalIndent` con la misma sangría.) Y con `EQUIV_PCT_ESPERADO_CERO=1`, en **las dos** grabaciones, poner `Pct: null` en todo objeto con `"Esperado": "0"` (normalización de E-INF-019).

- [ ] **Step 3: Comparar v2.1** — Run: `EQUIV_COMPARAR=…/equiv-e1762f5.json EQUIV_PCT_ESPERADO_CERO=1 go test -timeout 3600s -count=1 -run TestEquivalenciaDesempeno -v ./internal/desempeno/infra/desempenofb/`
Expected: PASS, "0 diferentes". **Control positivo:** sin `EQUIV_PCT_ESPERADO_CERO`, FAIL exactamente en los casos con `Esperado "0"` y `Pct` no nulo (su número debe coincidir con el de dev de E-INF-019); y con `camposV22` vacío, FAIL en todos los casos. Pegar las tres salidas resumidas.

- [ ] **Step 4: v2.2 contra v2.1 por cuenta** (spec §8.3-3). Prueba nueva en el mismo archivo, `TestEquivalenciaColumnasV22` (se salta sin `EQUIV_COLUMNAS=1`), que para cada usuario sintético del arnés pide `Cuentas` en 2.1 y en 2.2 con el **código actual** y exige, por venta: mismos `Estado`, `Trato`, `Etiqueta`, `Detalle`, `Saldo`, `Parcialidad`, `AbonoVentana`, `NumAtrasos`, `UltimaActividad`; y `Columna` igual a esta tabla escrita **en la prueba** (independiente de `ColumnaDe`):

```go
func columnaDeLaTabla(trato domain.Trato, fechaPromesa *domain.Fecha, hoy domain.Fecha) domain.Columna {
	switch trato {
	case domain.TratoPagado, domain.TratoParcial:
		return domain.ColumnaPagados
	case domain.TratoDiferido:
		if fechaPromesa != nil && fechaPromesa.Despues(hoy) {
			return domain.ColumnaNoPaga
		}
	}
	return domain.ColumnaPorVisitar
}
```

Además, en el tablero de los dos periodos, para cada fila **no vencida**: todos los campos comunes iguales entre 2.1 y 2.2 salvo `Lineas[*].PorHora` (eje) y `Eje`; `PorColumna` cumple las dos sumas del contrato. Imprime la matriz de transición `segmento v2.1 → columna v2.2` (conteos). Run con `EQUIV_COLUMNAS=1`. Expected: PASS; la matriz sólo tiene las transiciones de la tabla del spec §3.1 (`sin_visitar→por_visitar`, `volver→por_visitar`, `despues→por_visitar` (se negó), `pagados→pagados`, y `null→por_visitar` si hubiera). **Control positivo:** cambiar en `columnaDeLaTabla` `TratoParcial` a `ColumnaPorVisitar` → FAIL en las cuentas con abono parcial.

- [ ] **Step 5: Rendimiento final** — repetir el Step 7 de la Tarea 12 sobre la rama integrada y actualizar E-INF-023.

- [ ] **Step 6: Compuerta DESPUÉS** — despachar un agente **nuevo** (ni el autor de ninguna tarea del API, ni el de la compuerta antes) con:

```text
Rama: feat/desempeno-pulido-2 de /Volumes/M2-1TB/Developer/msp-api. Evidencia: docs/evidencia/infra.md E-INF-018…023.
Para cada cambio que toca dinero —ColumnaDe (domain/columna.go), pct nulo con esperado 0 (domain/cobro.go),
semana vencida fuera del lote (app/tablero.go, app/movimientos.go)—:
1. Revierte SÓLO el archivo de la regla a e1762f5 (o borra la regla nueva) y corre `go test ./internal/desempeno/...`:
   ¿qué pruebas se ponen rojas? Pega la salida. Si ninguna, es un hallazgo.
2. Enumera con grep (di el patrón) todos los consumidores de ColumnaDe, Columna, CalcularCobro, Cobro.Pct,
   SemanaVencidaEn, SemanaVencida, UltimoMovimiento en todo el repo. ¿Alguno no está cubierto por una prueba?
3. ¿Hay algún camino por el que una respuesta SIN contrato=2.2 cambie respecto de e1762f5, además de zonas excluidas y pct?
4. Di en voz alta lo que no verificaste.
```

Registrar el resultado en cada entrada (línea "Compuerta después"). Todo hallazgo se corrige en una tarea nueva antes de seguir; no se reescribe la evidencia para que cuadre.

- [ ] **Step 7: Commit**

```bash
git add internal/desempeno/infra/desempenofb/equivalencia_tablero_test.go docs/evidencia/infra.md
git commit -m "test(desempeno): arnes de equivalencia v2.1 contra e1762f5 y columnas v2.2 contra la tabla del spec (E-INF-018, E-INF-019, E-INF-020)"
```

---

## Fase 3 — Android (`msp-app-kt`)

Base: `feat/estado-cuenta-casos-compartidos` @ `083c0d61` (= `main` 2.20.0 + el runner de casos compartidos, que **no** está en `main`). Rama nueva `feat/lista-tres-columnas` en un worktree propio: `git -C /Volumes/M2-1TB/Developer/msp-app-kt worktree add ../msp-app-kt-columnas -b feat/lista-tres-columnas feat/estado-cuenta-casos-compartidos` (el checkout principal lo usa el dueño; no trabajar ahí). Sin cambios de Room (v34 es de `feat/diagnostico-envio`). Gradle: `--no-parallel`, un módulo a la vez, `--rerun-tasks`.

### Task 15: Trato y columna en `:core:common`, amarrados por los casos compartidos (D18, E-APP-070)

**Depende de:** T9 (el directorio `testdata/estado-cuenta` nuevo y su VERSION).

**Files:**
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/TratoDelEstado.kt`
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/ColumnaDeCobranza.kt`
- Test: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/ColumnaDeCobranzaTest.kt`
- Modify: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/CasoDeEstadoCuenta.kt:28,37-45,49-72,129-136` (`hoy`, `columna`)
- Modify: `core/common/src/test/kotlin/com/example/msp_app/core/common/cobranza/domain/casos/EstadoCuentaCasosCompartidosTest.kt:25-47,59-84`
- Modify: `core/common/src/test/resources/estado-cuenta/*` (copia byte a byte con `scripts/casos-estado-cuenta.sh copiar`)

**Interfaces:**
- Consumes: `EstadoCuenta` (`EstadoCuenta.kt:34-58`), `ResultadoEstadoCuenta` (`DerivacionModelos.kt:105-114`).
- Produces (los usa T16):
  ```kotlin
  enum class TratoDelEstado { PAGADO, PARCIAL, REGRESAS, DIFERIDO, ESCALAR, CITA, NADIE, SIN_TRABAJAR }
  object TratoDeEstado { fun de(estado: EstadoCuenta, fechaPromesa: LocalDate?, horaCita: LocalTime?): TratoDelEstado }
  enum class ColumnaDeCobranza(val codigo: String) { POR_VISITAR("por_visitar"), NO_PAGA("no_paga"), PAGADOS("pagados") }
  fun columnaDe(estado: EstadoCuenta, fechaPromesa: LocalDate?, horaCita: LocalTime?, hoy: LocalDate): ColumnaDeCobranza
  ```

- [ ] **Step 1: Copiar los casos de msp-api**

Run: `cd ../msp-app-kt-columnas && MSP_API_DIR=/Volumes/M2-1TB/Developer/msp-api-<worktree con T9 integrada> scripts/casos-estado-cuenta.sh copiar && cat core/common/src/test/resources/estado-cuenta/VERSION`
Expected: termina con `verificar` OK y el VERSION es **el mismo hash** que reportó la Tarea 9.

- [ ] **Step 2: Correr el runner y ver que falla por el formato nuevo**

Run: `./gradlew --no-parallel :core:common:testDebugUnitTest --rerun-tasks --tests '*casos*'`
Expected: FAIL `clave desconocida [hoy]` (el parser es estricto, `CasoDeEstadoCuenta.kt:138-143`).

- [ ] **Step 3: Prueba de la regla que falla** (`ColumnaDeCobranzaTest.kt`) — misma tabla que Go (`columna_test.go` de la Tarea 4), fila por fila:

```kotlin
package com.example.msp_app.core.common.cobranza.domain

import java.time.LocalDate
import java.time.LocalTime
import kotlin.random.Random
import org.junit.Assert.assertEquals
import org.junit.Test

class ColumnaDeCobranzaTest {
    private val hoy = LocalDate.parse("2026-10-15")
    private val fechas = mapOf(
        "vencida" to LocalDate.parse("2026-10-14"),
        "hoy" to hoy,
        "futura" to LocalDate.parse("2026-10-16"),
        "nula" to null
    )

    @Test
    fun `E-APP-070 la tabla del spec estado por estado y fecha por fecha`() {
        for ((nombre, f) in fechas) {
            for (e in EstadoCuenta.entries) {
                val obtenido = columnaDe(e, fechaPromesa = f, horaCita = LocalTime.of(10, 0), hoy = hoy)
                val esperado = when {
                    e == EstadoCuenta.PAGO || e == EstadoCuenta.ABONO_PARCIAL -> ColumnaDeCobranza.PAGADOS
                    e == EstadoCuenta.PROMETIO_PROXIMA && nombre == "futura" -> ColumnaDeCobranza.NO_PAGA
                    else -> ColumnaDeCobranza.POR_VISITAR
                }
                assertEquals("$e, fecha $nombre", esperado, obtenido)
            }
        }
    }

    @Test
    fun `E-APP-070 lo que cambia contra los cuatro segmentos`() {
        assertEquals(ColumnaDeCobranza.PAGADOS, columnaDe(EstadoCuenta.ABONO_PARCIAL, null, null, hoy))
        assertEquals(ColumnaDeCobranza.POR_VISITAR, columnaDe(EstadoCuenta.SE_NEGO, null, null, hoy))
        assertEquals(ColumnaDeCobranza.POR_VISITAR, columnaDe(EstadoCuenta.CITA_A_UNA_HORA, null, LocalTime.of(16, 30), hoy))
        assertEquals(ColumnaDeCobranza.POR_VISITAR, columnaDe(EstadoCuenta.CITA_A_UNA_HORA, null, null, hoy))
        assertEquals(ColumnaDeCobranza.NO_PAGA, columnaDe(EstadoCuenta.PROMETIO_PROXIMA, LocalDate.parse("2026-10-20"), null, hoy))
        assertEquals(ColumnaDeCobranza.POR_VISITAR, columnaDe(EstadoCuenta.PROMETIO_PROXIMA, null, null, hoy))
    }

    @Test
    fun `E-APP-070 total y disjunta con entradas al azar`() {
        val rng = Random(20261016)
        repeat(20_000) {
            val e = EstadoCuenta.entries[rng.nextInt(EstadoCuenta.entries.size)]
            val f = if (rng.nextInt(4) == 0) null else hoy.plusDays((rng.nextInt(21) - 10).toLong())
            val h = if (rng.nextBoolean()) LocalTime.of(9, 30) else null
            val c = columnaDe(e, f, h, hoy)
            val trato = TratoDeEstado.de(e, f, h)
            assertEquals(trato == TratoDelEstado.PAGADO || trato == TratoDelEstado.PARCIAL, c == ColumnaDeCobranza.PAGADOS)
            assertEquals(trato == TratoDelEstado.DIFERIDO && f!!.isAfter(hoy), c == ColumnaDeCobranza.NO_PAGA)
        }
    }

    @Test
    fun `los codigos son los del contrato v2_2`() {
        assertEquals(listOf("por_visitar", "no_paga", "pagados"), ColumnaDeCobranza.entries.map { it.codigo })
    }
}
```

- [ ] **Step 4: Implementar.** `TratoDelEstado.kt` — el enum se **mueve** aquí con el mismo nombre y orden que `EstadoCuentaUi.kt:24-51` (copiar sus KDoc), más la derivación de `EstadoCuentaUi.tratoDe` (`:122-135`) sobre primitivos:

```kotlin
package com.example.msp_app.core.common.cobranza.domain

import java.time.LocalDate
import java.time.LocalTime

/** Cómo se trata un estado en la lista (antes en feature/pagos EstadoCuentaUi.kt; movido para que los casos compartidos lo prueben). */
enum class TratoDelEstado { PAGADO, PARCIAL, REGRESAS, DIFERIDO, ESCALAR, CITA, NADIE, SIN_TRABAJAR }

object TratoDeEstado {
    /** Una promesa sin fecha y una cita sin hora se tratan como REGRESAS: nada sostiene sacarlas de la ruta. */
    fun de(estado: EstadoCuenta, fechaPromesa: LocalDate?, horaCita: LocalTime?): TratoDelEstado = when (estado) {
        EstadoCuenta.PAGO -> TratoDelEstado.PAGADO
        EstadoCuenta.ABONO_PARCIAL -> TratoDelEstado.PARCIAL
        EstadoCuenta.VISITE_VUELVO -> TratoDelEstado.REGRESAS
        EstadoCuenta.PROMETIO_PROXIMA -> if (fechaPromesa == null) TratoDelEstado.REGRESAS else TratoDelEstado.DIFERIDO
        EstadoCuenta.SE_NEGO -> TratoDelEstado.ESCALAR
        EstadoCuenta.CITA_A_UNA_HORA -> if (horaCita == null) TratoDelEstado.REGRESAS else TratoDelEstado.CITA
        EstadoCuenta.NO_ESTABA -> TratoDelEstado.NADIE
        EstadoCuenta.SIN_TOCAR -> TratoDelEstado.SIN_TRABAJAR
    }
}
```

`ColumnaDeCobranza.kt`:

```kotlin
package com.example.msp_app.core.common.cobranza.domain

import java.time.LocalDate
import java.time.LocalTime

/**
 * Columna de la lista de cobranza (D18 del dueño, 2026-10-15; E-APP-070): la
 * misma regla que msp-api `internal/desempeno/domain/columna.go` (E-INF-018),
 * amarrada por el juego de casos compartido. La pregunta es "¿a qué puerta
 * voy?": sólo la promesa con fecha posterior a hoy se aparta; pagado y abono
 * parcial van a PAGADOS; todo lo demás, a POR_VISITAR.
 */
enum class ColumnaDeCobranza(val codigo: String) {
    POR_VISITAR("por_visitar"),
    NO_PAGA("no_paga"),
    PAGADOS("pagados")
}

fun columnaDe(estado: EstadoCuenta, fechaPromesa: LocalDate?, horaCita: LocalTime?, hoy: LocalDate): ColumnaDeCobranza =
    when (TratoDeEstado.de(estado, fechaPromesa, horaCita)) {
        TratoDelEstado.PAGADO, TratoDelEstado.PARCIAL -> ColumnaDeCobranza.PAGADOS
        TratoDelEstado.DIFERIDO ->
            if (fechaPromesa != null && fechaPromesa.isAfter(hoy)) ColumnaDeCobranza.NO_PAGA else ColumnaDeCobranza.POR_VISITAR
        else -> ColumnaDeCobranza.POR_VISITAR
    }
```

- [ ] **Step 5: Runner con `hoy` y `columna`.** `CasoDeEstadoCuenta`: raíz con `"hoy"` (`soloClaves(archivo, "nombre", "ventana", "hoy", "cuentas", "pagos", "visitas", "esperado")`, campo `val hoy: LocalDate`); `Esperado` gana `val columna: ColumnaDeCobranza` (`soloClaves(…, "venta_id", "estado", "con_algo", "columna")`, `ColumnaDeCobranza.entries.firstOrNull { it.codigo == s } ?: error("$archivo: columna desconocida \"$s\"")`). `EstadoCuentaCasosCompartidosTest` arma `obtenido` con `columnaDe(r.estado, r.fechaPromesa, r.horaCita, caso.hoy)` de cada `ResultadoEstadoCuenta`; `JuegoDeCasosCoberturaTest` exige además las tres columnas y los nueve archivos 041–049 (misma lista que Go, Tarea 9 Step 1). `CasoDeEstadoCuentaParseoTest` gana los casos "hoy ausente" y "columna desconocida" con el texto exacto del error.

- [ ] **Step 6: Verde**

Run: `./gradlew --no-parallel :core:common:testDebugUnitTest --rerun-tasks && ./gradlew --no-parallel :core:common:koverVerify`
Expected: BUILD SUCCESSFUL; contar en `core/common/build/test-results/testDebugUnitTest/*.xml` que `EstadoCuentaCasosCompartidosTest` corrió **49** casos (`grep -c '<testcase' …EstadoCuentaCasosCompartidosTest.xml`). Kover ≥ 90 %.

- [ ] **Step 7: Roja al revertir** — en `columnaDe` mandar `PARCIAL` a `POR_VISITAR` (la regla vieja del teléfono): rojos `048-parcial-en-pagados`, la tabla y el aleatorio. Restaurar. Pegar salida.

- [ ] **Step 8: Commit**

```bash
git add core/common
git commit -m "feat(cobranza): trato y columna de la lista en core common con casos compartidos de columna (E-APP-070)"
```

### Task 16: La lista del cobrador en 3 pastillas (D18, E-APP-070)

**Depende de:** T15.

**Files:**
- Modify: `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/SegmentoDeCobranza.kt` (todo: 3 valores que delegan en `columnaDe`)
- Modify: `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/EstadoCuentaUi.kt:24-51,122-135` (`typealias` + delegación)
- Modify: `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/ListaDeClientesViewModel.kt:40,186-190,392`
- Modify: `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/CarteraEnPantalla.kt:85-93`
- Modify: `app/src/main/java/com/example/msp_app/data/menu/ContadoresDelMenuAdapter.kt:9,25`, `app/src/main/java/com/example/msp_app/di/MenuLateralModule.kt:99-102`
- Modify (pruebas): `feature/pagos/src/test/kotlin/com/example/msp_app/feature/pagos/ui/{SegmentoDeCobranzaTest,SegmentosVsPestanasLegadasTest,CarteraEnPantallaTest,ListaDeClientesViewModelTest,ElNombreSeArrastraTest,ElBuscadorNoPierdeElFocoTest,OrdenDeLaListaTest,ScrollDeLaListaPorOrdenYSegmentoTest,ElToggleDeTemaEnLaListaTest,ElControlSegmentadoNoSeRompeALetraGrandeTest,ListaSeVeYSeTocaTest,ListaDeClientesHeaderTest,ElBuscadorSeLeeGrandeTest}.kt`, `feature/pagos/src/test/kotlin/com/example/msp_app/feature/pagos/screenshot/{BusquedaCompactaScreenshotTest,ListaMatrixScreenshotTest}.kt`, `app/src/test/java/com/example/msp_app/navigation/CadaPantallaDeCobranzaRespetaLaBarraDeEstadoTest.kt`, `app/src/test/java/com/example/msp_app/data/visitas/LaPromesaLlegaAlSemaforoTest.kt`
- Modify (goldens): `feature/pagos/src/test/screenshots/pagos_lista_{light,dark}_{1_0,1_5,2_0}.png`, `pagos_lista_vacia_{light,dark}.png`, `pagos_barra_compacta_{light,dark}_{1_0,2_0}.png`, `pagos_busqueda_compacta_{light,dark}.png`

**Interfaces:**
- Consumes: `columnaDe`, `ColumnaDeCobranza`, `TratoDelEstado`, `TratoDeEstado` (T15).
- Produces: `enum class SegmentoDeCobranza(val etiqueta: String, val columna: ColumnaDeCobranza) { POR_VISITAR("Por visitar", …), NO_PAGA("No paga", …), PAGADOS("Pagados", …) }` con `fun contiene(estado: EstadoDelPeriodo, hoy: LocalDate): Boolean`. Se conserva el nombre del tipo a propósito (17 archivos de prueba y la pantalla lo nombran; renombrarlo agranda el diff y los choques con otras ramas sin cambiar nada que se vea).

- [ ] **Step 1: Reescribir `SegmentoDeCobranzaTest.kt` primero** (rojo): las 8 × 4 combinaciones de la Tarea 15 vistas a través de `SegmentoDeCobranza.entries.single { it.contiene(estado, hoy) }` (exactamente una contiene cada cuenta), incluida la fila que hoy está en `:114` (`PARCIAL to VOLVER_A_VISITAR`) que pasa a `PAGADOS`; y `entries.map { it.etiqueta } == listOf("Por visitar", "No paga", "Pagados")`. Borrar la prueba de `:274` ("una cita con hora pero SIN dia no cae en ningun chip"): con D18 ya no existe cuenta sin chip; reemplazarla por "una cita con hora y sin día cae en Por visitar".

Run: `./gradlew --no-parallel :feature:pagos:testDebugUnitTest --rerun-tasks --tests '*SegmentoDeCobranzaTest*'` · Expected: FAIL de compilación (`POR_VISITAR` no existe).

- [ ] **Step 2: Implementar `SegmentoDeCobranza.kt`**

```kotlin
/**
 * Las tres pastillas de la lista (D18 del dueño, 2026-10-15; E-APP-070): las
 * mismas del escritorio. La regla vive en :core:common [columnaDe] y la prueban
 * los casos compartidos con msp-api; aquí sólo se pinta.
 */
enum class SegmentoDeCobranza(val etiqueta: String, val columna: ColumnaDeCobranza) {
    POR_VISITAR("Por visitar", ColumnaDeCobranza.POR_VISITAR),
    NO_PAGA("No paga", ColumnaDeCobranza.NO_PAGA),
    PAGADOS("Pagados", ColumnaDeCobranza.PAGADOS);

    fun contiene(estado: EstadoDelPeriodo, hoy: LocalDate): Boolean =
        columnaDe(estado.estado, estado.fechaPromesa, estado.horaCita, hoy) == columna
}
```

`EstadoCuentaUi.kt`: borrar el `enum class TratoDelEstado` (`:24-51`), dejar `typealias TratoDelEstado = com.example.msp_app.core.common.cobranza.domain.TratoDelEstado` (así ningún import de `feature.pagos.ui.TratoDelEstado` cambia) y `fun tratoDe(estado: EstadoDelPeriodo): TratoDelEstado = TratoDeEstado.de(estado.estado, estado.fechaPromesa, estado.horaCita)`. Cambio mínimo en ese archivo: lo toca también `feat/badges-blancos-sin-al-corriente`.

- [ ] **Step 3: El chip guardado no restaura un valor equivocado (Review Focus 5).** Prueba nueva en `ListaDeClientesViewModelTest.kt`:

```kotlin
@Test
fun `un chip guardado por ordinal en la version anterior no restaura otra columna`() {
    // 2.20.0 guardaba value.ordinal en "pagos_lista_segmento": 3 era PAGADOS y hoy no existe ese índice.
    val handle = SavedStateHandle(mapOf("pagos_lista_segmento" to 3))
    val vm = viewModelCon(handle) // usar el constructor de prueba que ya tiene el archivo
    assertEquals(SegmentoDeCobranza.POR_VISITAR, vm.uiState.value.segmento)
}

@Test
fun `el chip elegido se guarda por nombre y se restaura`() {
    val handle = SavedStateHandle()
    viewModelCon(handle).elegirSegmento(SegmentoDeCobranza.NO_PAGA)
    assertEquals("NO_PAGA", handle.get<String>("pagos_lista_columna"))
    assertEquals(SegmentoDeCobranza.NO_PAGA, viewModelCon(handle).uiState.value.segmento)
}
```

Implementación en el VM (`:186-190`, `:392`): `CLAVE_SEGMENTO = "pagos_lista_columna"`; guardar `value.name`; leer `SegmentoDeCobranza.entries.firstOrNull { it.name == savedStateHandle.get<String>(CLAVE_SEGMENTO) } ?: SegmentoDeCobranza.POR_VISITAR`; default de `UiState.segmento` (`:40`) = `POR_VISITAR`. `ScrollPorSegmento` ya descarta nombres desconocidos (`ScrollPorSegmento.kt:20-23`): agregar a `ScrollDeLaListaPorOrdenYSegmentoTest` un caso que restaura `["VOLVER_A_VISITAR", 4, 0]` sin tronar.

- [ ] **Step 4: Contador del menú lateral.** `CarteraEnPantalla.clientesPorVolver` → `clientesPorVisitar` (cuenta `POR_VISITAR`); `ContadoresDelMenuAdapter.kt:25` pasa a `"$it por visitar"`; `MenuLateralModule.kt:99-102` llama al nuevo nombre. **Decisión del dueño pendiente** (ver "Para el dueño"): si prefiere conservar "por volver", sólo cambia el literal; la cuenta es la de "Por visitar" en cualquier caso, porque "Volver" ya no existe. Actualizar la prueba del contador que exista (`grep -rn "por volver" app/src/test feature/pagos/src/test`).

- [ ] **Step 5: Consumidores restantes.** Reemplazar en las pruebas listadas en **Files** `SIN_VISITAR`/`VOLVER_A_VISITAR` → `POR_VISITAR`, `YA_NO_ESTA_SEMANA` → `NO_PAGA` **sólo donde la cuenta del fixture sigue cayendo ahí con la regla nueva**: los fixtures `ListaFixtures.kt` usan PAGO, SIN_TOCAR, SE_NEGO y PROMETIO_PROXIMA; un SE_NEGO que antes estaba en "Después" ahora está en "Por visitar" — esos conteos cambian y la prueba se ajusta citando E-APP-070 en el nombre o el mensaje. Los tags de chip (`CHIP_DE_SEGMENTO_TAG + name.lowercase()`) pasan a `…por_visitar`, `…no_paga`, `…pagados`. `SegmentosVsPestanasLegadasTest`: leerla completa antes; si compara contra las pestañas viejas de 4, reescribir su tabla a 3 columnas y dejar en su KDoc qué cambió y por qué (D18). `LaPromesaLlegaAlSemaforoTest` (`app`, `:174-224`): ajustar el segmento esperado de la promesa a futuro (`NO_PAGA`).

Run, módulo por módulo: `./gradlew --no-parallel :feature:pagos:testDebugUnitTest --rerun-tasks` y luego `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --rerun-tasks`
Expected: BUILD SUCCESSFUL; contar pruebas en los XML (no menos que antes del cambio salvo la borrada del Step 1).

- [ ] **Step 6: Goldens de Roborazzi, revisados a ojo**

Run: `rm -rf feature/pagos/build/outputs/roborazzi && ./gradlew --no-parallel :feature:pagos:recordRoborazziDebug --rerun-tasks && git status --short feature/pagos/src/test/screenshots`
Expected: cambian exactamente los PNG listados en **Files** (y ninguno de `pagos_tarjeta_nueva_*`, `pagos_hoja_orden_*`, `pagos_renglon_orden_distinto_*`). Para cada PNG cambiado: abrir el anterior (`git show HEAD:<ruta> > /tmp/…/antes.png`) y el nuevo, mirarlos, y describir en el reporte qué se ve (tres pastillas "Por visitar", "No paga", "Pagados", con conteos, sin texto cortado a 2.0×). Diferencia por píxeles con PIL **en RGB**: `ImageChops.difference(a.convert("RGB"), b.convert("RGB")).getbbox()` debe caer sólo en la franja del control segmentado. Luego `./gradlew --no-parallel :feature:pagos:verifyRoborazziDebug` (sabiendo que verifica contra lo recién grabado: no prueba nada por sí solo).

- [ ] **Step 7: Prueba en aparato o emulador** (memoria `reference_app_dev_login_emulador`): con datos de dev, la lista muestra tres pastillas; una cuenta con abono parcial aparece en "Pagados"; deslizar entre pastillas funciona en los dos sentidos y no pasa de la tercera. Captura en el reporte.

- [ ] **Step 8: Gate completo del repo** — Run: `./gradlew prePushCheck` (si truena por `GooglePlayServicesMissingManifest` tras un `prodRelease`, ver memoria `reference_prepushcheck_residuo_prodrelease`: limpiar ese residuo y repetir). Expected: BUILD SUCCESSFUL.

- [ ] **Step 9: Evidencia (texto) y commit.** Redactar **E-APP-070 · Tres columnas en el teléfono, la misma regla que el escritorio** (MEDIDO 2026-10-16 unitario; casos compartidos con VERSION `<hash>` verdes en Go y Kotlin; prueba roja al revertir; consumidores enumerados con `grep -rn "SegmentoDeCobranza\|TratoDelEstado\|clientesPorVolver" --include=*.kt .`; goldens revisados y lo que se vio; lo no verificado: comportamiento en la flota). El texto lo commitea la Tarea 24 en `msp-api/docs/evidencia/app-android.md` (ese archivo tiene cambios sin commitear de otra sesión en el checkout principal de msp-api: no tocarlo desde aquí).

```bash
git add feature/pagos app core/common
git commit -m "feat(pagos): la lista del cobrador en tres columnas por visitar, no paga y pagados (E-APP-070)"
```

---

## Fase 4 — Escritorio (`sistema-cobro-web`)

Base: `fix/mapas-y-permiso-fotos` @ `5d727f7` (worktree `/Volumes/M2-1TB/Developer/sistema-cobro-web-arreglos`), que ya trae D22 (`87dc139`: todos los mapas por `MapaConCarga` → `cargarGoogleMaps(obtenerLlaveGoogleMaps())`; prueba de un solo `<script data-msp-google-maps>` en `src/components/maps/__tests__/cargadorGoogleMaps.test.ts:16-35`). **No se rehace**: la Tarea 21 sólo agrega la prueba de dos mapas montados. Rama nueva: `git -C /Volumes/M2-1TB/Developer/sistema-cobro-web worktree add ../sistema-cobro-web-pulido2 -b feat/desempeno-pulido-2 fix/mapas-y-permiso-fotos`; cada tarea en su propio worktree desde esa rama (`…-pulido2-tNN`).

Comandos (de `package.json`): `npx vitest run <ruta>`; `npx tsc --noEmit`; `npm run lint`. No hay React Query: los hooks usan `useCargaRemota` (`presentation/hooks/lib/useCargaRemota.ts:30`) + `useCadaMinuto` (60 s). Las pruebas montan con `renderConDesempeno` (`src/modules/desempeno/test/renderConDesempeno.tsx`) sobre `FakeDesempenoPort` (`application/__tests__/fakeDesempenoPort.ts:540`; `Respuesta<T> = T | (() => T | Promise<T>)`, `:532`) y MSW con `onUnhandledRequest: "error"` (`src/test/setup.ts:50`). `vitest.config.ts` fija `TZ=UTC`.

### Task 17: Adaptador v2.2 — DTOs, mapeador estricto, dominio y `contrato=2.2` en toda petición

**Depende de:** T0 aprobada (el contrato v2.2 es la fuente; no hace falta el API corriendo: MSW).

**Files:**
- Modify: `src/modules/desempeno/infrastructure/http/dtos.ts` (DTOs v2.2; los de v2.1 se borran)
- Modify: `src/modules/desempeno/infrastructure/http/params.ts` (`contrato`, `columna`)
- Modify: `src/modules/desempeno/infrastructure/http/HttpDesempenoAdapter.ts` (+ `obtenerUsuario`)
- Modify: `src/modules/desempeno/infrastructure/mappers/dtoADominio.ts` (`leerUsuario`, `dtoToTablero`, `dtoToJornada`, `dtoToCuentas`, + `dtoToFichaUsuario`)
- Modify: `src/modules/desempeno/domain/entities/{Tablero,Jornada,Cuentas,Linea,index}.ts`, `domain/values/estadoCuenta.ts`
- Modify: `src/modules/desempeno/application/ports/DesempenoPort.ts`, `application/usecases/consultas.ts`, `application/__tests__/fakeDesempenoPort.ts`
- Modify: `src/test/msw/fixtures/desempeno.ts`, `src/test/msw/handlers/desempeno.ts`
- Modify (mecánico, para que compile): `components/cuentas/CuentasTab.tsx:21-26,48,92-115`, `components/tablero/ordenTablero.ts:44-45`, `components/ficha/FichaEncabezado.tsx:98-104`, `components/jornada/EjeJornada.tsx:18-23`, `components/jornada/JornadaTab.tsx:148-150`
- Test: `infrastructure/mappers/__tests__/dtoADominio.test.ts`, `infrastructure/http/HttpDesempenoAdapter.test.ts`, `components/cuentas/__tests__/CuentasTab.test.tsx`

**Interfaces:**
- Produces (las usan T18–T21):
  ```ts
  // domain/values/estadoCuenta.ts
  export const COLUMNAS = ["por_visitar", "no_paga", "pagados"] as const;
  export type Columna = (typeof COLUMNAS)[number];
  export const esColumna = (x: unknown): x is Columna => …;
  export const ETIQUETA_COLUMNA: Record<Columna, string> = { por_visitar: "Por visitar", no_paga: "No paga", pagados: "Pagados" };
  // domain/entities/Tablero.ts
  export type Eje = { readonly desdeHora: number; readonly hastaHora: number };
  export type CoberturaPorColumna = { readonly porVisitar: number; readonly noPaga: number; readonly pagados: number; readonly sinNada: number };
  export type UltimoMovimiento = { readonly id: string; readonly tipo: TipoActividad; readonly hora: string };
  export type PromedioDiario = { readonly dias: number; readonly minutosActivos: number; readonly pagosImporte: string; readonly visitas: number };
  // UsuarioTablero: quita coberturaPorSegmento; agrega semanaVencida, ultimoMovimiento, coberturaPorColumna, promedioDiario
  // Tablero: agrega hoy: string, eje: Eje
  export type FichaUsuario = { readonly generadoAt: string; readonly hoy: string; readonly periodo: Periodo; readonly pausaMin: number; readonly eje: Eje; readonly usuario: UsuarioTablero; readonly inicioSemanaDia: string | null; readonly diasNavegables: readonly string[] };
  // domain/entities/Jornada.ts: Jornada agrega hoy, eje, horario: { desdeHora: string; hastaHora: string } | null; ResumenJornada.condonaciones: Conteo
  // domain/entities/Cuentas.ts: Cuenta.columna: Columna (nunca null); ConteosCuentas = { porVisitar, noPaga, pagados, sinNada, total }; ColumnaFiltro = Columna | "sin_nada"
  // DesempenoPort: ConsultaCuentas.columna: ColumnaFiltro | null (sustituye segmento);
  //   obtenerUsuario(uid: string, consulta: ConsultaTablero, signal?: AbortSignal): Promise<FichaUsuario>
  ```

- [ ] **Step 1: Pruebas del mapeador que fallan** (`dtoADominio.test.ts`, agregar):

```ts
import { describe, expect, it } from "vitest";
import { dtoToCuentas, dtoToFichaUsuario, dtoToJornada, dtoToTablero } from "../dtoADominio";
import { cuentasDTO, fichaUsuarioDTO, jornadaDTO, tableroDTO } from "@/test/msw/fixtures/desempeno";

describe("contrato v2.2", () => {
  it("lee columnas, semana vencida, último movimiento, promedio, hoy y eje", () => {
    const t = dtoToTablero(tableroDTO);
    expect(t.hoy).toBe("2026-10-15");
    expect(t.eje).toEqual({ desdeHora: 6, hastaHora: 21 });
    const u = t.usuarios[0];
    expect(u.coberturaPorColumna).toEqual({ porVisitar: 66, noPaga: 3, pagados: 140, sinNada: 46 });
    expect(u.semanaVencida).toBe(false);
    expect(u.promedioDiario).toEqual({ dias: 6, minutosActivos: 310, pagosImporte: "1433.33", visitas: 2.17 });
  });

  it("rechaza una respuesta v2.1 (cobertura_por_segmento sin columnas)", () => {
    const { cobertura_por_columna: _fuera, ...sinColumnas } = tableroDTO.usuarios[0];
    const v21 = { ...tableroDTO, usuarios: [{ ...sinColumnas, cobertura_por_segmento: { sin_visitar: 1, volver: 0, despues: 0, pagados: 0, sin_nada: 1 } }] };
    expect(() => dtoToTablero(v21 as never)).toThrow(/cobertura_por_columna/);
  });

  it("rechaza una columna desconocida en una cuenta", () => {
    const malo = { ...cuentasDTO, cuentas: [{ ...cuentasDTO.cuentas[0], columna: "volver" }] };
    expect(() => dtoToCuentas(malo as never)).toThrow(/columna/);
  });

  it("condonaciones de la jornada traen importe", () => {
    expect(dtoToJornada(jornadaDTO).resumen.condonaciones).toEqual({ n: 1, importe: "50.00" });
  });

  it("ficha por usuario con días navegables", () => {
    const f = dtoToFichaUsuario(fichaUsuarioDTO);
    expect(f.diasNavegables.at(-1)).toBe(f.hoy);
    expect(f.inicioSemanaDia).toBe("2026-10-09");
  });
});
```

Y en `HttpDesempenoAdapter.test.ts`: cada uno de los cinco métodos por usuario y el tablero mandan `contrato=2.2` (capturar `request.url` en el handler de MSW y `expect(new URL(url).searchParams.get("contrato")).toBe("2.2")`); `obtenerCuentas` manda `columna` y nunca `segmento`; `obtenerUsuario` pega a `/v2/desempeno/usuarios/<uid>`.

Run: `npx vitest run src/modules/desempeno/infrastructure` · Expected: FAIL.

- [ ] **Step 2: Fixtures v2.2** en `src/test/msw/fixtures/desempeno.ts`: `tableroDTO`, `jornadaDTO`, `cuentasDTO` pasan a v2.2 **copiando los ejemplos del contrato v2.2** (Tarea 0) campo por campo (`hoy`, `eje`, `semana_vencida`, `ultimo_movimiento`, `cobertura_por_columna`, `promedio_diario`, `columna`, `conteos {por_visitar, no_paga, pagados, sin_nada, total}`, `resumen.condonaciones {n, importe}`, `horario`); nuevo `fichaUsuarioDTO` (`usuario` = `tableroDTO.usuarios[0]`, `inicio_semana_dia: "2026-10-09"`, `dias_navegables` del 09 al 15). Nombres de personas reales mexicanos (memoria `feedback_realistic_test_data`). Handler nuevo `*/v2/desempeno/usuarios/:uid` en `handlers/desempeno.ts` (registrar **antes** de los de `/:uid/jornada` sólo si MSW lo requiere; con paths exactos no choca).

- [ ] **Step 3: Implementar dominio, DTOs, mapeador y adaptador.** En el mapeador, siguiendo los ayudantes existentes (`obj`, `numero`, `dinero`, `codigo`, `booleano`, `textoONulo`):

```ts
function leerEje(v: unknown, campo: string): Eje {
  const o = obj(v, campo);
  return { desdeHora: numero(o.desde_hora, `${campo}.desde_hora`), hastaHora: numero(o.hasta_hora, `${campo}.hasta_hora`) };
}

function leerColumnas(v: unknown, campo: string): CoberturaPorColumna {
  const o = obj(v, campo);
  return {
    porVisitar: numero(o.por_visitar, `${campo}.por_visitar`),
    noPaga: numero(o.no_paga, `${campo}.no_paga`),
    pagados: numero(o.pagados, `${campo}.pagados`),
    sinNada: numero(o.sin_nada, `${campo}.sin_nada`),
  };
}

function leerPromedio(v: unknown, campo: string): PromedioDiario | null {
  if (v === null) return null;
  const o = obj(v, campo);
  return {
    dias: numero(o.dias, `${campo}.dias`),
    minutosActivos: numero(o.minutos_activos, `${campo}.minutos_activos`),
    pagosImporte: dinero(o.pagos_importe, `${campo}.pagos_importe`),
    visitas: numero(o.visitas, `${campo}.visitas`),
  };
}

function leerMovimiento(v: unknown, campo: string): UltimoMovimiento | null {
  if (v === null) return null;
  const o = obj(v, campo);
  return { id: texto(o.id, `${campo}.id`), tipo: codigo(o.tipo, esTipoActividad, `${campo}.tipo`), hora: texto(o.hora, `${campo}.hora`) };
}
```

`leerUsuario` usa `leerColumnas(d.cobertura_por_columna, "cobertura_por_columna")` (si falta, `obj` lanza con el nombre del campo: así se rechaza un JSON v2.1), `booleano(d.semana_vencida, …)`, `leerMovimiento`, `leerPromedio`; deja de leer `cobertura_por_segmento`. `params.ts`: `paramsDeTablero`/`paramsDeJornada`/`paramsDeActividad`/`paramsDeCuentas` agregan `contrato: "2.2"` (una constante `CONTRATO = "2.2"` en `params.ts`, un solo lugar); `paramsDeCuentas` manda `columna` en vez de `segmento`. `HttpDesempenoAdapter.obtenerUsuario` = `GET ${USUARIOS}/${encodeURIComponent(uid)}` con `{ periodo, ...paramsDeJornada(c) }`. `consultas.ts` gana `obtenerUsuario(port, uid, consulta, signal)`. `FakeDesempenoPort` gana `usuarioCalls`, `usuarioResponse: Respuesta<FichaUsuario> = makeFichaUsuario()` y el método. Cambios mecánicos para compilar: `ordenTablero.ts:44-45` lee `coberturaPorColumna.sinNada`; `FichaEncabezado.tsx` "Faltan" lee `coberturaPorColumna.sinNada`; `EjeJornada.tsx:18-23` y `JornadaTab.tsx:148-150` leen `resumen.condonaciones.n`.

- [ ] **Step 4: Cuentas en 3 columnas** (`CuentasTab.tsx`). Prueba primero (`components/cuentas/__tests__/CuentasTab.test.tsx`):

```tsx
it("pinta Por visitar, No paga, Pagados y el filtro Sin nada aparte", async () => {
  const { port } = renderConDesempeno(<CuentasTab uid="uid-juan" />);
  for (const etiqueta of ["Por visitar", "No paga", "Pagados", "Sin nada"]) {
    expect(await screen.findByRole("button", { name: new RegExp(etiqueta) })).toBeInTheDocument();
  }
  expect(screen.queryByRole("button", { name: /Volver|Después|Sin visitar/ })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: /No paga/ }));
  expect(port.cuentasCalls.at(-1)?.consulta.columna).toBe("no_paga");
  await userEvent.click(screen.getByRole("button", { name: /Sin nada/ }));
  expect(port.cuentasCalls.at(-1)?.consulta.columna).toBe("sin_nada");
});
```

Implementación: `PILDORAS` = `COLUMNAS.map((c) => [c, ETIQUETA_COLUMNA[c], claveConteo(c)])` con `claveConteo` `por_visitar→porVisitar`, `no_paga→noPaga`, `pagados→pagados`; estado inicial `"por_visitar"` (`:48`); la pastilla "Sin nada" (`:92-115`) sigue aparte, con su `InfoRapida`, y manda `columna: "sin_nada"`.

- [ ] **Step 5: Verde y tipos** — Run: `npx tsc --noEmit && npx vitest run src/modules/desempeno && npm run lint` · Expected: sin errores. Si alguna prueba existente esperaba "Volver"/"Después"/`coberturaPorSegmento`, actualizarla citando D18 en la descripción del `it`.

- [ ] **Step 6: Roja al revertir** — devolver `leerUsuario` a leer `cobertura_por_segmento`: la prueba "rechaza una respuesta v2.1" y la de columnas fallan. Restaurar.

- [ ] **Step 7: Commit**

```bash
git add src
git commit -m "feat(desempeno): adaptador del contrato 2.2 con tres columnas y cuentas por columna"
```

### Task 18: Cuentas sin pintar cientos de filas (D26)

**Depende de:** T17. **Paralela con** T20, T21, T22, T23.

**Files:**
- Modify: `src/modules/desempeno/components/cuentas/TablaCuentas.tsx:54`
- Test: `src/modules/desempeno/components/cuentas/__tests__/TablaCuentas.test.tsx`

**Interfaces:** `TablaCuentas` mantiene sus props; agrega una constante exportada `FILAS_INICIALES = 60` y `FILAS_POR_TANDA = 60`.

- [ ] **Step 1: Prueba que falla** — el stub de `IntersectionObserver` de `src/test/setup.ts:34-40` no dispara nada; la prueba lo reemplaza con uno controlable:

```tsx
import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FILAS_INICIALES, TablaCuentas } from "../TablaCuentas";
import { makeCuenta } from "../../../application/__tests__/fakeDesempenoPort";

let disparar: () => void = () => {};
beforeEach(() => {
  vi.stubGlobal("IntersectionObserver", class {
    constructor(cb: IntersectionObserverCallback) {
      disparar = () => cb([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
    }
    observe() {}
    disconnect() {}
    unobserve() {}
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("TablaCuentas", () => {
  it("pinta 60 filas y 60 más al acercarse al final", () => {
    const cuentas = Array.from({ length: 150 }, (_, i) => makeCuenta({ ventaId: i + 1 }));
    render(<TablaCuentas cuentas={cuentas} onAbrir={() => {}} />);
    expect(screen.getAllByTestId("fila-cuenta")).toHaveLength(FILAS_INICIALES);
    act(() => disparar());
    expect(screen.getAllByTestId("fila-cuenta")).toHaveLength(120);
    act(() => disparar());
    expect(screen.getAllByTestId("fila-cuenta")).toHaveLength(150);
  });

  it("al cambiar la lista vuelve a 60", () => {
    const muchas = Array.from({ length: 150 }, (_, i) => makeCuenta({ ventaId: i + 1 }));
    const { rerender } = render(<TablaCuentas cuentas={muchas} onAbrir={() => {}} />);
    act(() => disparar());
    rerender(<TablaCuentas cuentas={muchas.slice(0, 100)} onAbrir={() => {}} />);
    expect(screen.getAllByTestId("fila-cuenta")).toHaveLength(60);
  });
});
```

(Leer las props reales de `TablaCuentas` y el nombre del constructor de cuentas del fake antes; si `makeCuenta` no se exporta, exportarlo. Agregar `data-testid="fila-cuenta"` al `<tr>`.)

- [ ] **Step 2: Rojo** — Run: `npx vitest run src/modules/desempeno/components/cuentas` · FAIL (150 filas).

- [ ] **Step 3: Implementar** (patrón de `src/modules/clientes/components/ClientesTable.tsx:175,194`, sin dependencia nueva):

```tsx
export const FILAS_INICIALES = 60;
export const FILAS_POR_TANDA = 60;

// dentro del componente
const [visibles, setVisibles] = useState(FILAS_INICIALES);
useEffect(() => setVisibles(FILAS_INICIALES), [cuentas]);
const centinela = useRef<HTMLTableRowElement | null>(null);
useEffect(() => {
  const nodo = centinela.current;
  if (!nodo || visibles >= cuentas.length) return;
  const obs = new IntersectionObserver((entradas) => {
    if (entradas.some((e) => e.isIntersecting)) setVisibles((v) => Math.min(v + FILAS_POR_TANDA, cuentas.length));
  }, { rootMargin: "400px" });
  obs.observe(nodo);
  return () => obs.disconnect();
}, [visibles, cuentas.length]);
// …cuentas.slice(0, visibles).map(…) y, si visibles < cuentas.length, <tr ref={centinela} aria-hidden="true" />
```

- [ ] **Step 4: Verde** — Run: `npx tsc --noEmit && npx vitest run src/modules/desempeno && npm run lint` · PASS.

- [ ] **Step 5: Commit**

```bash
git add src/modules/desempeno/components/cuentas
git commit -m "perf(desempeno): cuentas pinta 60 filas y carga mas al llegar al final"
```

### Task 19: Tablero — semanas vencidas al final, orden por promedio, "Hoy sin actividad", esqueleto fiel y carga honesta

**Depende de:** T17 y **T21** (las dos tocan `TableroScreen.tsx`; ésta va después).

**Files:**
- Create: `src/modules/desempeno/components/tablero/FilaVencida.tsx`, `src/modules/desempeno/components/tablero/FilaEsqueleto.tsx`
- Modify: `src/modules/desempeno/components/tablero/{TableroScreen,TableroFila,TarjetasTablero,ordenTablero}.tsx|ts`, `src/modules/desempeno/components/comunes/Estados.tsx:46-60` (sólo si se retira `FilasCargando`)
- Test: `src/modules/desempeno/components/tablero/__tests__/{TableroVencidas,TableroCarga,ordenTablero}.test.tsx|ts`

**Interfaces:**
- Consumes: `UsuarioTablero.semanaVencida`, `ultimoMovimiento`, `promedioDiario`, `Tablero.periodo` (T17).
- Produces: `textoAhora(u, periodo)`; `valor(u, clave, periodo)` en `ordenTablero.ts`.

- [ ] **Step 1: Pruebas que fallan**

```tsx
describe("semanas vencidas (D19)", () => {
  it("no se pintan hasta pedirlas y muestran su último movimiento", async () => {
    const port = new FakeDesempenoPort();
    port.tableroResponse = makeTablero({
      usuarios: [
        makeUsuario({ uid: "u1", nombre: "Juan Carlos Méndez" }),
        makeUsuario({ uid: "u2", nombre: "Iván Vázquez Aldama", semanaVencida: true, inicioSemana: "2025-11-11T14:00:00Z",
          ultimoMovimiento: { id: "visita:x", tipo: "visita", hora: "2026-09-05T16:00:00Z" } }),
        makeUsuario({ uid: "u3", nombre: "Ana Lilia Ortega", semanaVencida: true, ultimoMovimiento: null }),
      ],
    });
    renderConDesempeno(<TableroScreen onAbrirUsuario={() => {}} puedeConfigurar />, { port });
    expect(await screen.findByText("Juan Carlos Méndez")).toBeInTheDocument();
    expect(screen.queryByText("Iván Vázquez Aldama")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Ver 2 sin actividad reciente" }));
    expect(screen.getByText("Iván Vázquez Aldama")).toBeInTheDocument();
    expect(screen.getByText(/Último movimiento: 5 sept/)).toBeInTheDocument();
    expect(screen.getByText("Sin movimiento en 92 días")).toBeInTheDocument();
  });
});

describe("carga honesta del tablero (D23)", () => {
  it("al cambiar a Semana nunca pinta las cifras de Hoy", async () => {
    const port = new FakeDesempenoPort();
    let soltarSemana: (t: Tablero) => void = () => {};
    port.tableroResponse = () => makeTablero({ periodo: "hoy", usuarios: [makeUsuario({ pagos: { n: 7, importe: "700.00" } })] });
    renderConDesempeno(<TableroScreen onAbrirUsuario={() => {}} puedeConfigurar />, { port });
    expect(await screen.findByText("$700")).toBeInTheDocument();
    port.tableroResponse = () => new Promise<Tablero>((r) => { soltarSemana = r; });
    await userEvent.click(screen.getByRole("radio", { name: "Semana" }));
    expect(screen.queryByText("$700")).not.toBeInTheDocument();
    expect(screen.getAllByTestId("fila-esqueleto").length).toBeGreaterThan(0);
    act(() => soltarSemana(makeTablero({ periodo: "semana", usuarios: [makeUsuario({ pagos: { n: 30, importe: "3000.00" } })] })));
    expect(await screen.findByText("$3,000")).toBeInTheDocument();
  });

  it("una respuesta que no es del periodo elegido no se pinta (Review Focus 4)", async () => {
    const port = new FakeDesempenoPort();
    port.tableroResponse = makeTablero({ periodo: "hoy" }); // llega 'hoy' estando elegido 'semana'
    renderConDesempeno(<TableroScreen onAbrirUsuario={() => {}} puedeConfigurar />, { port, preferencias: { periodo: "semana" } });
    expect(await screen.findAllByTestId("fila-esqueleto")).not.toHaveLength(0);
  });
});

it("en Semana el no empezado dice Hoy sin actividad; en Hoy, No ha empezado", () => {
  const u = makeUsuario({ estadoAhora: "no_ha_empezado" });
  expect(textoAhora(u, "semana")).toBe("Hoy sin actividad");
  expect(textoAhora(u, "hoy")).toBe("No ha empezado");
});
```

`ordenTablero.test.ts`: en Semana, `ordenarUsuarios` por `"pagos"` ordena por `promedioDiario.pagosImporte` (un usuario con 6 días y $6,000 total —$1,000/día— queda detrás de uno con 2 días y $3,000 —$1,500/día—); en Hoy, por `pagos.importe`; `"horas"` y `"visitas"` igual con `minutosActivos` y `visitas` del promedio; promedio `null` va al final.

(Antes de escribirlas: leer los constructores reales de `fakeDesempenoPort.ts` (`makeTablero`, `makeUsuario` o como se llamen), el formato real de `dinero()` (`components/lib/format.ts:91-94`, sin centavos) y de fecha corta; ajustar los textos esperados a esos formateadores, sin inventar otros.)

- [ ] **Step 2: Rojo** — Run: `npx vitest run src/modules/desempeno/components/tablero` · FAIL.

- [ ] **Step 3: Implementar.**
- `TableroScreen.tsx`: `const vigentes = usuarios.filter((u) => !u.semanaVencida)`, `const vencidas = usuarios.filter((u) => u.semanaVencida)`; tabla con `vigentes`; al final, si `vencidas.length > 0`, botón `Ver ${vencidas.length} sin actividad reciente` que despliega `<FilaVencida>` por cada una. Las cifras sólo se pintan si `tablero && tablero.periodo === periodo` (de `usePreferencias()`); si no, `FilaEsqueleto` × 8, y tras 1 s sin datos el texto `periodo === "semana" ? "Cargando semana…" : "Cargando hoy…"`; con `error` y sin datos del periodo, `ErrorConReintento` con título "No se pudo cargar" y botón "Reintentar" (componente existente en `comunes/Estados.tsx`).
- `FilaVencida.tsx`: nombre, zona, `textoInicioSemana(inicio, hoy)` (`format.ts:133`) y `u.ultimoMovimiento ? \`Último movimiento: ${fechaCorta(u.ultimoMovimiento.hora)}\` : "Sin movimiento en 92 días"`.
- `FilaEsqueleto.tsx`: `<tr data-testid="fila-esqueleto">` con las mismas celdas y anchos que `TableroFila` (nombre + zona en dos líneas, barra de línea con la altura de `LineaActividad`, una celda por cifra con el ancho de su columna) usando `Skeleton`. Sustituye a `FilasCargando` (`TableroScreen.tsx:160`); si `FilasCargando` queda sin uso, borrarlo de `Estados.tsx`.
- `TableroFila.tsx:29-33` y `TarjetasTablero.tsx:42-49`: `textoAhora(u, periodo)` — en Semana `no_ha_empezado` → "Hoy sin actividad".
- `ordenTablero.ts`: `valor(u, clave, periodo)`; claves `horas`, `pagos`, `visitas` usan `u.promedioDiario` en Semana; el popover del encabezado agrega "ordena por promedio diario" en Semana. `faltan` usa `coberturaPorColumna.sinNada`.
- Prueba de altura del esqueleto: renderizar una `TableroFila` y una `FilaEsqueleto` y comparar `getComputedStyle(...).height` de la fila (jsdom no hace layout: la prueba compara las **clases de alto** (`h-…`) de ambas filas; la comparación real se hace a ojo en el Step 5).

- [ ] **Step 4: Verde** — Run: `npx tsc --noEmit && npx vitest run src/modules/desempeno && npm run lint` · PASS.

- [ ] **Step 5: A ojo** — `npm run dev` contra el API local con v2.2 (rama integrada de msp-api) o, sin API, con el `FakeDesempenoPort` del modo demo si existe; captura de: tablero cargando (esqueleto), tablero cargado (nada salta de alto), botón de vencidas desplegado. Adjuntar al reporte.

- [ ] **Step 6: Commit**

```bash
git add src/modules/desempeno/components
git commit -m "feat(desempeno): tablero con semanas vencidas al final, orden por promedio en semana y carga sin cifras de otro periodo"
```

### Task 20: Ficha — fila por usuario, `hoy` del servidor, toggle honesto, esqueleto, aviso de semana vencida, "· hoy"

**Depende de:** T17. **Paralela con** T18, T21, T22, T23.

**Files:**
- Create: `src/modules/desempeno/presentation/hooks/useUsuario.ts`
- Create: `src/modules/desempeno/components/ficha/FichaEsqueleto.tsx`, `src/modules/desempeno/components/ficha/AvisoSemanaVencida.tsx`
- Modify: `src/modules/desempeno/components/DesempenoScreen.tsx:37-90` (deja `useTablero` y `hoyDeNegocio`)
- Modify: `src/modules/desempeno/components/ficha/{FichaScreen,FichaEncabezado}.tsx`
- Modify: `src/modules/desempeno/components/lib/format.ts:133-138` (`textoInicioSemana`)
- Test: `src/modules/desempeno/components/__tests__/DesempenoScreen.test.tsx`, `src/modules/desempeno/components/lib/__tests__/format.test.ts`, `src/modules/desempeno/presentation/hooks/__tests__/useUsuario.test.tsx`

**Interfaces:**
- Consumes: `obtenerUsuario`, `FichaUsuario` (T17).
- Produces: `useUsuario(uid: string): { ficha: FichaUsuario | null; isLoading: boolean; error: DomainError | null; refresh: () => void }` (refresco cada minuto con `useCadaMinuto`). `FichaScreen` recibe `ficha: FichaUsuario` y pasa `ficha.hoy`, `ficha.diasNavegables`, `ficha.inicioSemanaDia` a `JornadaTab` (la Tarea 21 los consume en `NavegadorDia`).

- [ ] **Step 1: Pruebas que fallan** (`DesempenoScreen.test.tsx`, agregar):

```tsx
it("la ficha pide sólo su usuario, nunca el tablero de la flota", async () => {
  const { port } = renderConDesempeno(<DesempenoScreen />, { ruta: "/?usuario=uid-juan" });
  await screen.findByText(/Juan Carlos Méndez/);
  expect(port.tableroCalls).toHaveLength(0);
  expect(port.usuarioCalls.at(-1)?.uid).toBe("uid-juan");
});

it("toggle a Semana: el toggle cambia al instante, cifras a esqueleto, 'Cargando semana…' tras 1 s, nunca la cifra de Hoy", async () => {
  vi.useFakeTimers({ shouldAdvanceTime: true });
  const port = new FakeDesempenoPort();
  port.usuarioResponse = makeFichaUsuario({ periodo: "hoy", usuario: makeUsuario({ pagos: { n: 3, importe: "450.00" } }) });
  renderConDesempeno(<DesempenoScreen />, { port, ruta: "/?usuario=uid-juan" });
  expect(await screen.findByText("$450")).toBeInTheDocument();
  let soltar: (f: FichaUsuario) => void = () => {};
  port.usuarioResponse = () => new Promise<FichaUsuario>((r) => { soltar = r; });
  await userEvent.click(screen.getByRole("radio", { name: "Semana" }));
  expect(screen.getByRole("radio", { name: "Semana" })).toBeChecked();
  expect(screen.queryByText("$450")).not.toBeInTheDocument();
  expect(screen.getByTestId("ficha-esqueleto")).toBeInTheDocument();
  expect(screen.getByRole("tablist")).toBeInTheDocument(); // el marco no se desmonta
  expect(screen.queryByText("Cargando semana…")).not.toBeInTheDocument();
  act(() => { vi.advanceTimersByTime(1000); });
  expect(screen.getByText("Cargando semana…")).toBeInTheDocument();
  act(() => soltar(makeFichaUsuario({ periodo: "semana", usuario: makeUsuario({ pagos: { n: 20, importe: "3000.00" } }) })));
  expect(await screen.findByText("$3,000")).toBeInTheDocument();
  vi.useRealTimers();
});

it("si falla la carga dice No se pudo cargar con Reintentar", async () => {
  const port = new FakeDesempenoPort();
  port.throwOnNext.obtenerUsuario = new DomainError("desempeno_no_disponible", "la base de datos no respondió a tiempo");
  renderConDesempeno(<DesempenoScreen />, { port, ruta: "/?usuario=uid-juan" });
  expect(await screen.findByText("No se pudo cargar")).toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Reintentar" }));
  expect(port.usuarioCalls.length).toBeGreaterThanOrEqual(2);
});

it("semana vencida: aviso, tarjetas en — y sin cifras", async () => {
  const port = new FakeDesempenoPort();
  port.usuarioResponse = makeFichaUsuario({
    usuario: makeUsuario({ semanaVencida: true, inicioSemana: "2025-11-11T14:00:00Z",
      ultimoMovimiento: { id: "visita:x", tipo: "visita", hora: "2026-09-05T16:00:00Z" } }),
  });
  renderConDesempeno(<DesempenoScreen />, { port, ruta: "/?usuario=uid-juan" });
  expect(await screen.findByText(/Semana vencida · inicio .* · último movimiento 5 sept/)).toBeInTheDocument();
  expect(screen.getAllByText("—").length).toBeGreaterThanOrEqual(7);
});

it("el día de la URL se acota con el hoy del servidor, no con el reloj de la Mac", async () => {
  vi.setSystemTime(new Date("2026-10-16T05:30:00Z")); // ya es 16 en UTC, todavía 15 en CDMX
  const port = new FakeDesempenoPort();
  port.usuarioResponse = makeFichaUsuario({ hoy: "2026-10-15" });
  renderConDesempeno(<DesempenoScreen />, { port, ruta: "/?usuario=uid-juan&dia=2026-10-16" });
  await screen.findByText(/Juan Carlos Méndez/);
  expect(port.jornadaCalls.at(-1)?.fecha).toBe("2026-10-15");
  vi.useRealTimers();
});
```

`format.test.ts`: `textoInicioSemana("2026-10-15T14:00:00Z", "2026-10-15")` termina en `"· hoy"` (no `"hace 0 d"`); `textoInicioSemana(<inicio de hace 3 días>, hoy)` sigue en `"· hace 3 d"`; inicio futuro del mismo día (`"2026-10-15T23:00:00Z"`, hoy `"2026-10-15"`) también `"· hoy"`.

- [ ] **Step 2: Rojo** — Run: `npx vitest run src/modules/desempeno/components src/modules/desempeno/presentation` · FAIL.

- [ ] **Step 3: Implementar.**
- `useUsuario.ts` (mismo patrón que `useTablero.ts`): `cargar = useCallback((signal) => obtenerUsuario(port, uid, consultaTablero, signal), [port, uid, consultaTablero])`; `useCargaRemota(cargar)`; `useCadaMinuto(refresh, true)`.
- `DesempenoScreen.FichaConectada`: quitar `useTablero`, el estado `ultimo` (`:44-46`) y `hoyDeNegocio()` (`:49`); `const { ficha, error, refresh } = useUsuario(uid)`; `const { periodo } = usePreferencias()`; `const vigente = ficha && ficha.periodo === periodo ? ficha : null`. El marco (encabezado con nombre si ya se conoce, pestañas, contenido de Actividad) **no se desmonta**: mientras `!vigente`, las cifras y la línea se reemplazan por `<FichaEsqueleto />` (nombre, las 7 tarjetas de `FichaEncabezado.tsx:89-108` y la tarjeta "Línea del día", mismas alturas); un `useEffect` con `setTimeout(1000)` muestra `"Cargando semana…"`/`"Cargando hoy…"`; con `error` y sin `vigente`, `"No se pudo cargar"` + `"Reintentar"` (`refresh`). `hoy = ficha?.hoy`; el `dia` de la URL se acota con `ficha.hoy` (si no hay ficha todavía, no se pide jornada). El 409 `desempeno_zona_excluida` se muestra con `EstadoVacio` y el mensaje del servidor.
- `FichaEncabezado.tsx`: `ESTADO.no_ha_empezado` depende del periodo (`"Hoy sin actividad"` en Semana); con `u.semanaVencida`, las 7 tarjetas muestran `"—"` y arriba `<AvisoSemanaVencida u={u} hoy={hoy} />` con el texto `Semana vencida · inicio ${fechaCorta(inicio)} · último movimiento ${u.ultimoMovimiento ? fechaCorta(u.ultimoMovimiento.hora) : "sin movimiento en 92 días"}`. La tarjeta "% cobro semana" y su "por cobrar" **no se tocan** (M4 sin cerrar, spec §4.6).
- `format.ts:136-137`: antes de la línea actual `if (dia > hoy) return base;`, agregar `if (dia === hoy) return \`${base} · hoy\`;` (spec D28: "si el inicio es del mismo día de negocio que hoy, el texto termina en '· hoy'"); `dia > hoy` (otro día futuro) sigue devolviendo `base`; el resto sigue en `· hace N d`. `hoy` es el del servidor (`ficha.hoy`/`tablero.hoy`).

- [ ] **Step 4: Verde** — Run: `npx tsc --noEmit && npx vitest run src/modules/desempeno && npm run lint` · PASS.

- [ ] **Step 5: Roja al revertir** — reponer el `ultimo` de `DesempenoScreen.tsx:44-46`: la prueba del toggle falla ("$450" visible). Restaurar.

- [ ] **Step 6: A ojo** — capturas: ficha cargando (esqueleto con la forma real), ficha cargada (nada salta), toggle a Semana lento (con throttling de red en el inspector: la app se compila con `devtools`), ficha de una semana vencida.

- [ ] **Step 7: Commit**

```bash
git add src/modules/desempeno
git commit -m "feat(desempeno): ficha con fila propia, hoy del servidor, carga honesta entre hoy y semana y aviso de semana vencida"
```

### Task 21: Jornada — eje del servidor, totales del día (opción A), días navegables

**Depende de:** T17. **Paralela con** T18, T20, T22, T23. **Antes de** T19.

**Files:**
- Modify: `src/modules/desempeno/components/lib/escala.ts` (todo)
- Modify: `src/modules/desempeno/components/jornada/{EjeJornada,JornadaTab,NavegadorDia}.tsx`, `src/modules/desempeno/components/linea/LineaActividad.tsx:5,63`, `src/modules/desempeno/components/tablero/TableroScreen.tsx:7,139` (sólo el eje)
- Test: `src/modules/desempeno/components/lib/__tests__/escala.test.ts`, `src/modules/desempeno/components/jornada/__tests__/{EjeJornada,NavegadorDia,JornadaTab}.test.tsx`, `src/components/maps/__tests__/dosMapas.test.tsx`

**Interfaces:**
- Consumes: `Eje`, `Jornada.eje/hoy/horario`, `resumen.condonaciones: Conteo`, `FichaUsuario.diasNavegables/inicioSemanaDia` (T17, T20).
- Produces (`escala.ts`):
  ```ts
  export type Escala = { posicion(iso: string): number; posicionMinutos(min: number): number; ancho(desde: string, hasta: string): number; posicionHorario(hhmm: string): number; marcas: readonly number[] };
  export function escalaDe(eje: Eje): Escala; // marcas: cada 3 h desde eje.desdeHora, más la última si cae dentro
  export const EJE_POR_OMISION: Eje = { desdeHora: 7, hastaHora: 20 };
  export function minutosDelDia(iso: string): number; // sin cambio
  ```
  `NavegadorDia` props: `{ dia: string; hoy: string; inicioSemanaDia: string | null; diasNavegables: readonly string[]; onCambiar(dia: string): void }`.

- [ ] **Step 1: Pruebas que fallan**

`escala.test.ts`:

```ts
it("con eje 5–22 una actividad a las 05:30 ya no se pega al borde", () => {
  const e = escalaDe({ desdeHora: 5, hastaHora: 22 });
  expect(e.posicionMinutos(5 * 60 + 30)).toBeCloseTo((30 / (17 * 60)) * 100, 5);
  expect(e.posicionMinutos(4 * 60)).toBe(0);
  expect(e.posicionMinutos(23 * 60)).toBe(100);
});
it("el eje por omisión reproduce el 7–20 de antes", () => {
  const e = escalaDe(EJE_POR_OMISION);
  expect(e.posicionMinutos(7 * 60)).toBe(0);
  expect(e.posicionMinutos(20 * 60)).toBe(100);
  expect(e.marcas).toEqual([7, 10, 13, 16, 19]);
});
```

`EjeJornada.test.tsx` (opción A del mock):

```tsx
it("encabezado de la tarjeta con horario, activas, pausas, clientes y cobrado; importe por carril", () => {
  render(<EjeJornada jornada={makeJornada({
    horario: { desdeHora: "08:00", hastaHora: "18:00" },
    resumen: { ...makeJornada().resumen, minutosActivos: 346, pausas: { n: 2, minutos: 70, enCurso: false }, clientes: 38,
      pagos: { n: 41, importe: "8200.00" }, visitas: 13, condonaciones: { n: 0, importe: "0.00" } },
  })} vista="bloques" />);
  const tarjeta = screen.getByRole("region", { name: /Línea del día/ });
  expect(within(tarjeta).getByText("$8,200")).toBeInTheDocument();
  expect(within(tarjeta).getByText(/08:00–18:00 · 5 h 46 min · 2 pausas · 38 clientes/)).toBeInTheDocument();
  expect(within(tarjeta).getByText(/Pagos/).closest("[data-carril]")).toHaveTextContent("41 · $8,200");
  expect(within(tarjeta).getByText(/Condonaciones/).closest("[data-carril]")).toHaveTextContent("0 · $0");
  expect(within(tarjeta).getByText(/Visitas/).closest("[data-carril]")).toHaveTextContent("13");
  expect(within(tarjeta).getByText(/Visitas/).closest("[data-carril]")).not.toHaveTextContent("$");
});
it("las etiquetas de hora salen del eje del servidor", () => {
  render(<EjeJornada jornada={makeJornada({ eje: { desdeHora: 5, hastaHora: 22 } })} vista="bloques" />);
  expect(screen.getByText("05")).toBeInTheDocument();
  expect(screen.getByText("21")).toBeInTheDocument();
});
```

`JornadaTab.test.tsx`: los textos sueltos de `JornadaTab.tsx:91-95` y `:148-150` ya no están (`queryByText(/activas · pausas de/)` es null; `queryByText(/pagos · .* visitas · .* condonaciones/)` es null). `NavegadorDia.test.tsx`: con `diasNavegables` de 14 días y `inicioSemanaDia` de hace 338 días hay 14 chips de día + 1 chip "inicio" (no 339); al tocar "inicio" llama `onCambiar(inicioSemanaDia)`. `dosMapas.test.tsx`: montar dos `MapaConCarga` a la vez (con `cargarGoogleMaps` real y `window.google` ausente) y esperar `document.querySelectorAll("script[data-msp-google-maps]").length === 1`.

(Leer antes `EjeJornada.tsx` completo, `mock-totales/project/Main.dc.html` del scratchpad (`/private/tmp/claude-501/-Volumes-M2-1TB-Developer-msp-api/e3d3046c-26d0-4968-9472-42a92c6af97a/scratchpad/mock-totales/project/Main.dc.html`, opción A) y los formateadores `duracionCorta`, `plural`, `dinero` de `format.ts`; ajustar los textos esperados a esos formateadores y al mock, nunca inventar uno nuevo.)

- [ ] **Step 2: Rojo** — Run: `npx vitest run src/modules/desempeno/components src/components/maps` · FAIL.

- [ ] **Step 3: Implementar.**
- `escala.ts`: `escalaDe(eje)` cierra sobre `INICIO = eje.desdeHora * 60`, `FIN = eje.hastaHora * 60`; `marcas` = `desdeHora, desdeHora+3, …` < `hastaHora`. Quitar `HORA_INICIO_EJE`, `HORA_FIN_EJE`, `MARCAS_EJE` y las funciones sueltas; sus tres consumidores (`EjeJornada`, `LineaActividad.tsx:5,63`, `TableroScreen.tsx:7,139`) reciben el eje: la jornada usa `jornada.eje`; el tablero usa `tablero.eje` (uno solo para alinear todas las filas, spec §3.4).
- `EjeJornada.tsx`: `<section aria-label="Línea del día">` con encabezado (izquierda: "Línea del día" + fecha; derecha, en mono: `horario ? \`${desde}–${hasta}\` : null`, `duracionCorta(minutosActivos)`, `plural(pausas.n, "pausa", "pausas")`, `plural(clientes, "cliente", "clientes")` unidos por " · ", y `dinero(resumen.pagos.importe)` grande en serif); cada carril (`:49-84`) con `data-carril={tipo}` y `{etiqueta} {n}` + ` · ${dinero(importe)}` para pagos y condonaciones; `HORAS` (`:24-27`) desde `jornada.eje`.
- `JornadaTab.tsx`: borrar `:91-95` y `:148-150`; pasar a `NavegadorDia` `hoy={ficha.hoy}`, `diasNavegables`, `inicioSemanaDia` (props nuevas de `JornadaTab` que llena `FichaScreen`).
- `NavegadorDia.tsx:18`: `const semana = diasNavegables;` + chip "inicio" que lleva a `inicioSemanaDia` (si existe y no está ya en la lista).

- [ ] **Step 4: Verde** — Run: `npx tsc --noEmit && npx vitest run src && npm run lint` · PASS.

- [ ] **Step 5: A ojo contra el mock** — captura de la tarjeta "Línea del día" junto a la opción A del mock; describir diferencias en el reporte. Una jornada con actividad a las 05:40 y a las 21:10 se ve completa (sin puntos pegados al borde).

- [ ] **Step 6: Commit**

```bash
git add src
git commit -m "feat(desempeno): linea del dia con eje del servidor, totales en su encabezado e importe por carril"
```

### Task 22: Horario libre en pasos de 30 min y columna Nota para preferencias viejas (D21, D28)

**Depende de:** nada del contrato (puede empezar con T17). **Paralela.**

**Files:**
- Modify: `src/modules/desempeno/components/comunes/ControlHorario.tsx:5-6,28-54`
- Modify: `src/modules/desempeno/presentation/actividadPreferencias.ts:27-38` (+ migración v1 → v2)
- Test: `src/modules/desempeno/components/comunes/__tests__/ControlHorario.test.tsx`, `src/modules/desempeno/presentation/context/__tests__/preferencias.test.tsx`

**Interfaces:**
- Produces: `export const INICIOS_HORARIO: readonly string[]` (00:00…23:00 cada 30 min, 47 valores), `export const FINES_HORARIO: readonly string[]` (00:30…23:30 cada 30 min + "23:59", 48 valores), `export function finTrasInicio(inicio: string, fin: string): string`; `CLAVE_COLUMNAS = "desempeno.actividad.columnas.v2"`, `export function leerColumnasGuardadas(almacen: Storage): ClaveColumna[] | null`.

- [ ] **Step 1: Pruebas que fallan**

```tsx
describe("ControlHorario (D21)", () => {
  it("ofrece 00:00–23:00 de inicio y 00:30–23:30 más 23:59 de fin", () => {
    expect(INICIOS_HORARIO[0]).toBe("00:00");
    expect(INICIOS_HORARIO.at(-1)).toBe("23:00");
    expect(INICIOS_HORARIO).toHaveLength(47);
    expect(FINES_HORARIO[0]).toBe("00:30");
    expect(FINES_HORARIO.at(-2)).toBe("23:30");
    expect(FINES_HORARIO.at(-1)).toBe("23:59");
    expect(FINES_HORARIO).toHaveLength(48);
  });
  it("si el inicio queda en o después del fin, el fin se recorre a inicio + 1 h, máximo 23:59", () => {
    expect(finTrasInicio("18:00", "17:00")).toBe("19:00");
    expect(finTrasInicio("18:00", "18:00")).toBe("19:00");
    expect(finTrasInicio("23:00", "18:00")).toBe("23:59");
    expect(finTrasInicio("22:30", "18:00")).toBe("23:30");
    expect(finTrasInicio("08:00", "18:00")).toBe("18:00");
  });
  it("las preferencias guardadas siguen siendo opciones válidas", () => {
    for (const h of ["07:00", "07:30", "08:00", "08:30", "09:00"]) expect(INICIOS_HORARIO).toContain(h);
    for (const h of ["17:00", "17:30", "18:00", "18:30", "19:00"]) expect(FINES_HORARIO).toContain(h);
  });
  it("elegir un inicio posterior al fin recorre el fin en pantalla", async () => {
    const cambiar = vi.fn();
    render(<ControlHorario horario={{ desde: "08:00", hasta: "18:00" }} onCambiar={cambiar} />);
    await userEvent.selectOptions(screen.getByLabelText("Inicio de horario"), "20:00");
    expect(cambiar).toHaveBeenLastCalledWith({ desde: "20:00", hasta: "21:00" });
  });
});

describe("columnas de Actividad (D28)", () => {
  it("una lista v1 guardada sin nota gana nota una sola vez y se guarda en v2", () => {
    localStorage.setItem("desempeno.actividad.columnas.v1", JSON.stringify(["tipo", "hora", "cliente", "monto"]));
    expect(leerColumnasGuardadas(localStorage)).toEqual(["tipo", "hora", "cliente", "monto", "nota"]);
    expect(JSON.parse(localStorage.getItem("desempeno.actividad.columnas.v2")!)).toEqual(["tipo", "hora", "cliente", "monto", "nota"]);
  });
  it("si después la oculta, se respeta", () => {
    localStorage.setItem("desempeno.actividad.columnas.v1", JSON.stringify(["tipo", "hora"]));
    localStorage.setItem("desempeno.actividad.columnas.v2", JSON.stringify(["tipo", "hora"]));
    expect(leerColumnasGuardadas(localStorage)).toEqual(["tipo", "hora"]);
  });
  it("sin nada guardado usa las de por defecto", () => {
    localStorage.clear();
    expect(leerColumnasGuardadas(localStorage)).toBeNull();
  });
});
```

(Leer `ControlHorario.tsx` y `actividadPreferencias.ts` completos y el uso de `almacenLocal` (`presentation/hooks/lib/almacenLocal.ts`) antes; todo acceso a `localStorage` va en `try/catch` como el existente. Ajustar el nombre real de la prop `onCambiar` si es otro.)

- [ ] **Step 2: Rojo / Step 3: Implementar**

```ts
const cada30 = (desde: number, hasta: number) =>
  Array.from({ length: (hasta - desde) / 30 + 1 }, (_, i) => {
    const m = desde + i * 30;
    return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
  });
export const INICIOS_HORARIO = cada30(0, 23 * 60);
export const FINES_HORARIO = [...cada30(30, 23 * 60 + 30), "23:59"];
const aMin = (h: string) => Number(h.slice(0, 2)) * 60 + Number(h.slice(3, 5));
export function finTrasInicio(inicio: string, fin: string): string {
  if (aMin(inicio) < aMin(fin)) return fin;
  const m = Math.min(aMin(inicio) + 60, 23 * 60 + 59);
  return `${String(Math.floor(m / 60)).padStart(2, "0")}:${String(m % 60).padStart(2, "0")}`;
}
```

`leerColumnasGuardadas`: si existe v2 válida, devolverla; si no y existe v1 válida, agregar `"nota"` si falta, guardar en v2 y devolver; si no, `null`. `CLAVE_COLUMNAS` pasa a v2 y la escritura sólo usa v2. **Review Focus 5**: v1 no se borra (el escritorio 1.26.0 en otra Mac no se ve afectado: cada Mac tiene su almacenamiento).

- [ ] **Step 4: Verde** — Run: `npx tsc --noEmit && npx vitest run src/modules/desempeno && npm run lint` · PASS.

- [ ] **Step 5: Commit**

```bash
git add src/modules/desempeno
git commit -m "feat(desempeno): horario de todo el dia en pasos de 30 minutos y columna nota para preferencias guardadas"
```

### Task 23: Configuración — pestaña "Desempeño" con zonas excluidas (D20, DA-1)

**Depende de:** T7 (endpoints `/v2/config/desempeno/zonas-excluidas`). **Paralela** con todo el escritorio (módulo `configuracion`, disjunto).

**Files:**
- Create: `src/modules/configuracion/domain/entities/ZonaExcluidaDesempeno.ts`
- Create: `src/modules/configuracion/infrastructure/mappers/dtoToZonaExcluidaDesempeno.ts` (+`__tests__`)
- Create: `src/modules/configuracion/application/usecases/{listarZonasExcluidasDesempeno,excluirZonaDesempeno,quitarZonaExcluidaDesempeno}.ts`
- Create: `src/modules/configuracion/presentation/hooks/useZonasExcluidasDesempeno.ts` (+`.test.tsx`)
- Create: `src/modules/configuracion/components/desempeno/ZonasExcluidasScreen.tsx` (+`.test.tsx`)
- Modify: `src/modules/configuracion/application/ports/ConfiguracionPort.ts`, `infrastructure/http/{HttpConfiguracionAdapter,dtos}.ts`, `application/__tests__/fakeConfiguracionPort.ts`, `components/ConfiguracionShell.tsx:27-44`

**Interfaces:**
- Consumes: `GET/PUT/DELETE /v2/config/desempeno/zonas-excluidas[/{zonaId}]` (T7); el catálogo de zonas ya disponible en `listarOpcionesZonasCajas` (`opciones.zonas`).
- Produces: `ZonaExcluidaDesempeno = { zonaId: number; zonaNombre: string; createdAt: string }`; pestaña `"desempeno"` con el título "Desempeño".

- [ ] **Step 1: Pruebas que fallan** (`ZonasExcluidasScreen.test.tsx`, patrón de `ZonasCajasScreen.test.tsx`): lista "MAYOREO" con su fecha; "Excluir zona" abre un combobox con las zonas del catálogo que no están excluidas, y al confirmar llama `excluirZonaDesempeno(23301)` y la lista se refresca **tras** la respuesta del servidor (no antes); "Quitar" pide confirmación y llama `quitar…(23301)`; un error del servidor se muestra con su mensaje (mapeador `errorMapper.ts` del módulo, que ya entiende `code=`). Textos: "Zonas fuera de desempeño", "Excluir zona", "Quitar" — cortos, sin explicación.

- [ ] **Step 2: Implementar** siguiendo exactamente las capas de `zonasCajas` (entidad → DTO → mapeador → caso de uso → puerto → adaptador → hook → pantalla). La pestaña se agrega en `ConfiguracionShell.tsx` junto a "Zonas y cajas", con el mismo gating de módulo que el resto de Configuración (el backend hace cumplir `config:administrar`).

- [ ] **Step 3: Verde** — Run: `npx tsc --noEmit && npx vitest run src/modules/configuracion && npm run lint` · PASS.

- [ ] **Step 4: Commit**

```bash
git add src/modules/configuracion
git commit -m "feat(configuracion): zonas excluidas de desempeno"
```

---

## Fase 5 — Cierre

### Task 24: Versiones, evidencia del teléfono, estado de E-INF-016 y notas de despliegue (sin publicar)

**Depende de:** T14, T16, T17–T23.

**Files:**
- Modify (escritorio): `package.json:4`, `src-tauri/tauri.conf.json:4`, `src-tauri/Cargo.toml:3` → `1.27.0` (verificar que `Cargo.toml:24` sigue con `devtools`)
- Modify (Android): `app/build.gradle.kts:81-82` → `versionCode = 68`, `versionName = "2.21.0"` (si otra rama ya tomó 68/2.21.0 al integrar, usar el siguiente y decirlo)
- Modify (msp-api): `docs/evidencia/app-android.md` (E-APP-070 con el texto de la Tarea 16), `docs/evidencia/infra.md` (E-INF-016 y E-INF-018)
- Create (fuera de git, memoria `feedback_docs_gestion_fuera_de_git`): `~/Documents/msp-gestion/despliegue-desempeno-pulido-2.md`

- [ ] **Step 1: Versiones** — cambiar los tres archivos del escritorio y el de Android; correr `npx tsc --noEmit && npx vitest run && npm run lint` en el escritorio y `./gradlew prePushCheck` en Android. Commits: `chore(release): escritorio 1.27.0` y `chore(release): 2.21.0 (68)`. **Sin tag, sin push, sin publicar.**

- [ ] **Step 2: E-INF-016.** No se borra: su estado pasa a `VIGENTE SÓLO EN v2.1 — caduca al retiro de v2.1 (spec §6.3)`, con puntero a **E-INF-018** (la regla de v2.2, igual en teléfono y escritorio, cumple su "Caduca si: el teléfono adopta la misma regla"). El comentario de `SegmentoDe` (`presentacion.go:116-120`) se actualiza para decirlo (spec §3.1: "v2.1 se congela").

- [ ] **Step 3: E-APP-070** — commit en msp-api del texto redactado en la Tarea 16 (en el worktree de integración de msp-api, no en el checkout principal, que tiene cambios sin commitear de otra sesión en ese archivo).

```bash
git add docs/evidencia/app-android.md docs/evidencia/infra.md internal/desempeno/domain/presentacion.go
git commit -m "docs(desempeno): tres columnas en telefono y escritorio, E-INF-016 queda solo para v2.1 (E-INF-018, E-APP-070)"
```

- [ ] **Step 4: Notas de despliegue** (fuera de git) con el orden **no negociable** del spec §9 y lo aprendido en producción:
  1. Contrato v2.2 aprobado (T0). 2. Casos compartidos con el mismo VERSION en los dos repos (T9/T15). 3. Mediciones y compuertas antes (T1–T3). 4. API: respaldo del binario y de la base; migración 000066 (y 000067 si existe) **con el API detenido** y el script con tope de `C:\msp-api\migrar-con-tope.ps1` (memoria del 15-oct: `CREATE INDEX` espera a toda conexión y bloquea INSERTs); binario nuevo; comprobar que el escritorio 1.26.0 sigue cargando (v2.1); **el dueño agrega la zona 23301** en Configuración → Desempeño (o con `PUT /v2/config/desempeno/zonas-excluidas/23301` con su token): sin eso, mayoreo sigue apareciendo. Compuerta después (T14). 5. Escritorio 1.27.0 y app 2.21.0 el mismo día; la app puede salir antes que el escritorio, nunca antes del paso 2. 6. Retiro de v2.1 sólo cuando el log de acceso muestre 7 días seguidos sin peticiones a `/v2/desempeno/usuarios*` sin `contrato=2.2` (MEDIDO, spec §6.3) — **otro plan**.
  Plan de reversa: binario anterior + `000066 .down.sql` (la tabla nueva no la lee nadie más); el escritorio 1.26.0 sigue funcionando contra el API nuevo y contra el viejo; la app 2.20.0 no depende del API para la lista.

- [ ] **Step 5: Revisión final de toda la rama** por un agente nuevo en cada repo (superpowers:requesting-code-review), con este plan y el spec como referencia; cualquier hallazgo se corrige en una tarea nueva.

---

## Self-review (2026-10-16)

**1. Cobertura del spec.**

| Spec | Tarea |
|---|---|
| D18 / §3.1 columnas, invariante, consumidores | T4, T9, T11, T13, T14, T15, T16, T17 |
| §3.1 "v2.1 se congela" | T11 Step 9, T13 Step 4, T14 Step 3, T24 Step 2 |
| D19 / §3.2 vencida, `ultimo_movimiento`, días navegables, actividad 14 d, cuentas 409 (DA-2) | T5, T12, T13, T19, T20, T21 |
| D20 / §3.3 exclusión configurable (DA-1), 409, `pct` null | T7, T10, T11, T13, T23 |
| D21 / §3.4 horas activas con horario, eje, `por_hora` | T6, T11, T13, T21, T22 |
| §3.5 condonaciones con importe, `horario`, endpoint por usuario | T13, T17, T20, T21 |
| §3.6-1 Hoy sin `registradasPor` de la semana | T11 (paso 3.4) |
| §3.6-2 mismo `hoy` | T11, T13, T20 |
| §3.6-3 `promedio_diario` y orden en Semana | T11, T19 |
| §3.6-4 cobrado ventanas largas (M3) — sólo medir | T1, T2 (sin código, DA-4) |
| §3.6-5 cancelación ≠ 503 | T8 |
| D22 mapa | ya en `fix/mapas-y-permiso-fotos`; T21 agrega la prueba de dos mapas |
| D23 / D24 carga honesta y esqueletos | T19, T20 |
| D25 totales opción A | T21 |
| D26 escritorio: Cuentas 60 filas, ficha sin flota, mismo hoy, orden promedio, "Hoy sin actividad" | T18, T20, T19 |
| D27 / M4 Patricio | T2 (sin código) |
| D28 "· hoy", columna Nota, 503 | T20, T22, T8 |
| §6 versionado, contrato v2.2, errores | T0, T11, T13 |
| §7 M1–M6 y compuertas | T1, T2, T3, T10 (gate), T12 (gate), T14 |
| §8.1–8.5 pruebas | cada tarea; T14 arnés y rendimiento |
| §9 orden de despliegue | T24 Step 4 |

**Huecos del spec que este plan cierra con una decisión explícita (para que el dueño la vea, sección siguiente):** la UI de Configuración para las zonas excluidas no estaba diseñada en el spec §4 (T23 la hace mínima); el alta de la zona 23301 no está en ningún paso del spec (T24 Step 4: la hace el dueño tras desplegar, no una migración, porque `CREATED_BY` debe ser un usuario real); el contador del menú lateral del teléfono ("N por volver") no lo menciona el spec (T16 Step 4); el chip guardado por ordinal en el teléfono (T16 Step 3); la caché `singleflight` del tablero debía incluir la versión (T11); "cita sin fecha" no es alcanzable como estado `cita` (T9 Step 4); `/usuarios/{uid}` sólo existe en 2.2 (T0).

**2. Marcadores.** Revisado: sin "TBD", "TODO", "implement later", "similar a la tarea N". Donde una prueba depende de un nombre real que no se pudo fijar desde aquí (constructores de fakes del escritorio, helpers de `jornada_test.go`, nombre del error de zona inexistente en `config`), el paso dice qué archivo leer y con qué sustituir.

**3. Tipos.** `Columna`/`ColumnaDe`/`ParseColumna`/`Columnas` (T4) se usan igual en T9, T11, T13, T14; `ParametrosJornada.RecortarAlHorario`/`PorHoraDelDia` (T6) los enciende `Contrato.Parametros` (T11); `FilaTablero.PorColumna`, `PromedioDiario`, `SemanaVencida`, `UltimoMovimiento` (T11) los llena T12 y los serializa `aFilaV22DTO`; `calcularFilas` (T11) lo reusa `Usuario` (T13); `outbound.ConsultaMovimientos`/`MovimientosRepo` (T12) los implementa `ActividadesRepo`; en el escritorio `COLUMNAS`/`ETIQUETA_COLUMNA`/`FichaUsuario`/`obtenerUsuario` (T17) los usan T18–T21; `escalaDe`/`EJE_POR_OMISION` (T21) los usa T19; en Android `ColumnaDeCobranza`/`columnaDe`/`TratoDeEstado` (T15) los usa T16.

**4. Review Focus.** Las cinco líneas tienen prueba en su tarea: borde de 14 d (T5) y zona compartida (T12 `TestTablero_V22_VencidoEnZonaCompartidaNoTocaAlVigente`); v2.1 intacto y `singleflight` por versión (T11 Steps 6 y 9); horario sin actividad dentro y pausa en curso (T6); respuestas fuera de orden (T19 "una respuesta que no es del periodo elegido", T20 toggle); preferencias viejas (T16 Step 3, T22).

## Para el dueño (decisiones que el plan no puede tomar)

1. **Contador del menú lateral del teléfono:** con tres columnas ya no existe "Volver". El plan cuenta "Por visitar" y cambia el texto a "N por visitar". ¿Lo quiere así o prefiere conservar "por volver"?
2. **Orden de fusión en Android** con `feat/badges-blancos-sin-al-corriente` (toca `EstadoCuentaUi.kt` y los goldens `pagos_lista_dark_*`): ¿cuál entra primero? El que entre segundo regraba esos goldens.
3. **Base de Android:** el plan parte de `feat/estado-cuenta-casos-compartidos`, que tiene el runner de casos y no está en `main`. ¿Se fusiona esa rama a `main` antes, o la rama nueva la arrastra?
4. **M3 (DA-4):** con las cifras de la Tarea 2 en la mano, elegir entre esperado proporcional, cobrado de 7 días o sólo rotular. Ese cambio va en un plan aparte.
5. **Alta de MAYOREO (23301):** se hace a mano después del despliegue, desde Configuración → Desempeño. Hasta entonces mayoreo sigue en el tablero.
6. **Números de versión:** escritorio 1.27.0 y app 2.21.0 (68), si ninguna otra rama los toma antes.
