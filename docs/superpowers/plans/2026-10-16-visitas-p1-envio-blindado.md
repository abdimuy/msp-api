# Visitas P1 — Envío blindado: plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Que una visita capturada en el teléfono termine **confirmada por el servidor** (`MSP_VISITAS`, probado por su UUID) o **resguardada** en `MSP_FAILED_INTENTS` (probado por `X-Intent-Captured`), y nunca "enviada" sin prueba; y que una divergencia se detecte el mismo día.

**Architecture:** Servidor (msp-api, `internal/visitas`): extractor de resumen para la bandeja, clave de blob con nonce por intento, `imagen_ids` en `GET /v2/visitas/{id}`, y `MaxBodyBytes` explícito solo si la medición lo confirma. Teléfono (msp-app-kt): el worker de visitas pasa a la tabla compartida `classifyUpload` de `:core:upload` precedida de una **prueba de existencia completa**; dos columnas nuevas en `Visit` (`CONFIRMADA_EN`, `RESGUARDADA_EN`, Room v35); reencolado ordenado sin tope; poda que exige confirmación; reconciliación inversa diaria; Auto Backup sin la base; telemetría sin datos personales; kill-switch `config/visitas_envio`. Todo cubierto por simulación determinista con fallas, mutación dirigida, verificador de historial, caos con Toxiproxy en dev y un agente adversario, con las dos compuertas de CLAUDE.md §8.

**Tech Stack:** Go 1.25, Huma v2.37.3, Firebird 5 (`nakagami/firebirdsql v0.9.19`), `pgregory.net/rapid v1.3.0`, testify; Kotlin, Room 2.6.1, WorkManager 2.10.2 + `work-testing`, Robolectric 4.14.1, OkHttp 4.12 `mockwebserver`, Gson; Toxiproxy 2.9.0 (solo dev); PIT vía `pl.droidsonroids.gradle.pitest` (nuevo, DA-14 aprobado).

**Spec:** `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-15-visitas-compromisos-avisos-design.md` (§1–§4, §7, §8, §9, §10 Q1/Q4/Q5/Q6, §11 pasos 0–3, §12, §13). Leer también `docs/COBRANZA-SYNC.md` completo, `docs/module-standards/ENTREGA_GARANTIZADA.md`, `docs/registro-de-evidencia.md` y `docs/investigacion/2026-10-15-sync-offline-practicas.md`.

**Planes hermanos (orden de dependencia):** este (P1) → `2026-10-16-visitas-p2-compromisos.md` → `2026-10-16-visitas-p3-tareas-avisos.md`. P2 no empieza su parte de teléfono hasta que este plan esté fusionado; su parte de servidor puede empezar en cuanto termine la Tarea 1 de este plan (la guardia).

---

## Global Constraints

Cada tarea incluye implícitamente todas estas reglas (copiadas del spec y de `CLAUDE.md`).

- **Garantía nombrada:** "al-menos-una-vez + deduplicación por UUID en el servidor = efectivamente-una-vez". Nadie escribe "exactly once" en código, pruebas ni mensajes (spec §2.1).
- **Nada se suelta sin prueba del servidor**: «la apliqué» (existencia completa) o «la tengo guardada» (`X-Intent-Captured`) (spec §2.2-1).
- **Un solo lugar decide** qué hacer con una respuesta: `classifyUpload` de `:core:upload` (`UploadDecision.kt:65-78`). `:core:upload` **no se modifica** (vedado, §7.1).
- **Sin tope de reintentos** en el camino de visitas (`Result.retry()` sin cap; spec §4.2-4).
- **Sin lógica en la base** (CLAUDE.md §1): IDs y timestamps en Go/Kotlin; ninguna migración en este plan.
- **Código en inglés/español del repo, mensajes al usuario en español**, minúsculas, sin punto final (CLAUDE.md §3). Códigos de error en snake_case inglés.
- **Archivos vedados** (spec §7.1, enmendado 2026-10-16): la guardia de la Tarea 1 los bloquea. En msp-app-kt, `AppDatabase.kt` solo puede cambiar `version`, `entities`, accesores DAO nuevos, sus `import` y la línea de `addMigrations`.
- **Room:** `feat/diagnostico-envio` ya ocupa **v34** (`envio_diagnostico`, spec §4.7). Este plan sube a **v35**. P2 usará v36 y P3 v37. Ningún migración reescribe filas.
- **Kill-switch remoto:** documento nuevo `config/visitas_envio` (`modo: "v3" | "v2_actual"`). **Nunca** tocar `config/api_settings` (`COBRANZA-SYNC.md:442-443`).
- **Gson ignora defaults de Kotlin** (memoria `reference_gson_ignora_defaults_kotlin`): todo campo nuevo de DTO que el servidor pueda omitir se declara **nulable**.
- **Hilt:** nada que sostenga un API service creado con `ApiProvider`/`V2ApiProvider` lleva `@Singleton` (memoria `reference_msp_app_kt_hilt_baseurl_killswitch`). Los workers no usan `@HiltWorker`: siguen el patrón `@JvmOverloads` con costuras por defecto.
- **Gradle Android:** siempre `--no-parallel`, un módulo a la vez (KSP falla en paralelo); `--rerun-tasks` y contar pruebas en el XML antes de creer un verde (memoria `reference_gradle_ksp_paralelo_y_goldens`). Borrar `app/build` si hubo `assembleProdRelease` antes de `prePushCheck`.
- **Go:** `go clean -testcache` antes de reportar; caches en `/Volumes/M2-1TB/.go-cache`; gremlins con `TMPDIR=/Volumes/M2-1TB/.go-cache/tmp`.
- **Pruebas contra Firebird dev:** `fbtestutil.WithTestTransaction` salvo que la prueba deba confirmar (y entonces `t.Cleanup` que borre hijos antes que padres **sin descartar el error**). `make fb-snapshot NAME=antes-<tarea>` antes de cualquier prueba que confirme.
- **Producción es SOLO LECTURA.** Las mediciones en producción son tareas **MEDIR** que habilita el dueño (túnel/SSH); el agente prepara los `.sql`/`.bat`, los corre primero en dev con control positivo, y el dueño los corre o los autoriza.
- **Commits:** conventional commits, en español, **sin línea de atribución**, citando el identificador de evidencia (`Cierra E-VIS-004.`). **Nunca `--no-verify`**, nunca `push`, nunca desplegar.
- **Evidencia:** área nueva `docs/evidencia/visitas.md` (`E-VIS-001`…). Números siguientes libres medidos el 2026-10-16: `app-android.md` tiene encabezados hasta **E-APP-067** (su línea "siguiente libre E-APP-058" está desfasada) → este plan usa **E-APP-071…E-APP-075**; `infra.md` llega a E-INF-016 y `E-INF-017` ya lo cita `2026-10-16-orden-precios-design.md` → los planes de visitas usan **E-INF-024+**; `cobranza.md` tiene encabezados hasta **E-COB-018**, `registro-de-evidencia.md:169` usa E-COB-019 y el spec de desempeño reserva E-COB-020 → los planes de visitas usan **E-COB-031+** por instrucción del dueño. Si al escribir una entrada el número ya existe, se toma el siguiente libre y se corrigen las citas (`registro-de-evidencia.md:257-259`).
- **Renumeración respecto al spec** (§9.2 citaba números ya ocupados): spec E-APP-058→**E-APP-071** (backoff), 059→**072** (reencolado), 060→**073** (Auto Backup), 061→**074** (`SALDO_REST - 0.0`), 062→**075** (`retryOnConnectionFailure`). E-VIS-* conserva la numeración del spec.
- **Renumeración (2026-10-16, segunda):** E-APP-068/069 los ocupó la tanda de precios/diagnóstico y E-APP-070 y E-INF-018…023 el plan de desempeño: este plan usa **E-APP-071…076** y **E-INF-024+** (ya corregido en todo el texto).

## Review Focus

1. **El cuerpo 2xx con un id distinto o vacío** (proxy/túnel que devuelve HTML 200, o JSON de otra visita): la visita no se suelta. Prueba en Tarea 9 (`v3_200_con_id_distinto_no_suelta`, `v3_200_sin_cuerpo_no_suelta`).
2. **Visitas legadas con `GUARDADO_EN_MICROSIP = 1` y `CONFIRMADA_EN` nulo tras migrar a v35**: la poda nueva no las borra hasta que la reconciliación inversa las confirme; si el servidor no las tiene, vuelven a pendiente. Pruebas en Tareas 12 y 13 (`podaNoBorraEntregadaLegadaSinConfirmar`, `inversaDevuelveAPendienteLaLegadaQueElServidorNoTiene`).
3. **El servidor contesta una tanda de by-ids con error o con ids ajenos**: una tanda fallida no decide nada; un id no pedido no se confirma. Prueba en Tarea 13 (`tandaFallidaNoDevuelveNiConfirma`, `idAjenoNoSeConfirma`).
4. **Visita con fotos donde el GET dice 200 pero `imagen_ids` no trae una foto local pendiente** (o el servidor viejo omite `imagen_ids`): no se suelta por existencia. Prueba en Tarea 8 (`existenciaSinLaFotoPendienteEsIncompleta`, `imagenIdsAusenteConFotosPendientesEsIncompleta`).
5. **El kill-switch con valor inválido, documento ausente o Firestore caído**: el modo queda en `v3` (el seguro) y el worker no truena. Prueba en Tarea 10 (`documentoAusenteEsV3`, `valorDesconocidoEsV3`, `errorDeFirestoreNoCambiaElModo`).

---

## Mapa de archivos

**msp-api** (rama `feat/visitas-p1-envio`, desde `main`, en el checkout principal `/Volumes/M2-1TB/Developer/msp-api` — memoria `feedback_trabajar_en_repo_principal`; antes de crear la rama, `git status`: si hay cambios ajenos sin commitear, **preguntar al dueño** antes de cambiar de rama):

| Archivo | Responsabilidad |
|---|---|
| `scripts/guardia-aislamiento.sh` (crear) | Bloquea diffs que tocan archivos de dinero; verifica las excepciones por contenido del hunk. |
| `scripts/guardia-aislamiento.lista` (crear) | Patrones vedados (spec §7.1). |
| `scripts/guardia-aislamiento_test.sh` (crear) | Control positivo y negativo de la guardia en un repo temporal. |
| `lefthook.yml` (modificar) | Hook `guardia-aislamiento` en pre-commit y pre-push. |
| `.github/workflows/ci.yml` (modificar) | Paso de la guardia en el job `quality`. |
| `docs/evidencia/visitas.md` (crear) | Registro E-VIS-001… |
| `docs/evidencia/app-android.md` (modificar) | E-APP-071…072; corregir la línea "siguiente libre". |
| `scripts/medicion/visitas-p1-dev.sql`, `scripts/medicion/visitas-p1-prod.sql`, `scripts/medicion/visitas-p1-prod.bat` (crear) | Q1, Q4, Q6 (solo lectura). |
| `internal/visitas/infra/failedintents/resumen.go` + `_test.go` (crear) | Extractor de resumen de `/v2/visitas` (H10). |
| `cmd/api/failedintent_wiring.go` (modificar, **una línea**) | `.Registrar("/v2/visitas", "visitas", …)`. |
| `internal/visitas/infra/visitashttp/multipart_helpers.go` (modificar) | Nonce por intento en la clave de blob (H11). |
| `internal/visitas/app/registrar_visita_con_imagenes_test.go` (modificar) | Prueba H11 (E-VIS-004). |
| `internal/visitas/app/obtener_visita.go` (modificar) | `ObtenerVisitaConImagenIDs`. |
| `internal/visitas/infra/visitashttp/dto.go`, `handlers.go` (modificar) | `imagen_ids` en `GET /visitas/{id}`. |
| `internal/visitas/infra/visitashttp/routes.go` (modificar) | `MaxBodyBytes` (solo si E-VIS-003 lo confirma). |
| `internal/visitas/infra/visitashttp/limite_cuerpo_e2e_test.go` (crear) | E-VIS-003 (413 a 1 MiB). |
| `internal/visitas/sim/` (crear, solo `_test.go`) | Simulación determinista Go + historial + verificador. |
| `scripts/caos/toxiproxy-visitas.sh` (crear) | Caos en dev. |

**msp-app-kt** (rama `feat/visitas-envio-blindado`; **base `feat/diagnostico-envio`** porque esa rama ya modifica `PendingVisitsWorker.kt`, `AppDatabase.kt` (v34) y `RetrofitClientFactory.kt`; si para entonces ya está fusionada a `main`, la base es `main`. Checkout principal `/Volumes/M2-1TB/Developer/msp-app-kt`, que hoy está en `feat/estado-cuenta-casos-compartidos`: antes de cambiar, `git status` limpio y confirmar con el dueño):

| Archivo | Responsabilidad |
|---|---|
| `scripts/guardia-aislamiento.sh`, `.lista`, `_test.sh` (crear) | Guardia del lado teléfono. |
| `scripts/pre-commit`, `scripts/pre-push` (modificar) | Llaman a la guardia. |
| `core/database/.../entities/VisitEntity.kt` (modificar) | `CONFIRMADA_EN`, `RESGUARDADA_EN`. |
| `core/database/.../migrations/Migration34to35.kt` (crear) | Dos `ALTER TABLE ADD COLUMN`. |
| `core/database/schemas/.../35.json` (generado) | Esquema exportado. |
| `core/database/.../AppDatabase.kt` (modificar, excepción enumerada) | `version = 35`, `MIGRATION_34_35`. |
| `core/database/.../dao/visit/VisitDao.kt` (modificar) | `confirmar`, `resguardar`, `devolverAPendiente`, `pendientesOrdenadas`, `entregadasDesde`, poda nueva. |
| `core/common/.../visitas/envio/ExistenciaCompleta.kt` (crear) | Función pura: ¿el servidor tiene la visita completa? |
| `core/common/.../visitas/envio/DecisionDeEnvio.kt` (crear) | Función pura: salida tras 2xx/error. |
| `core/common/.../visitas/envio/ReglaDePoda.kt` (crear) | Límites de retención (DA-4). |
| `core/common/.../sync/pendingwork/domain/usecases/ReconcileDeliveredVisitsUseCase.kt` (crear) + puertos | Reconciliación inversa. |
| `app/.../data/api/services/visits/V2VisitsApi.kt` (modificar) | `obtenerVisita(id)`; `VisitaDTO.imagen_ids`. |
| `app/.../workers/PendingVisitsWorker.kt` (modificar) | Modo v3 con existencia + `classifyUpload`; modo `v2_actual` conserva lo de hoy. |
| `app/.../features/visit/upload/domain/VisitUploadClassifier.kt` | Se conserva **solo** para el modo `v2_actual` durante P1; se borra en la Tarea 20. |
| `app/.../core/visitas/envio/ModoEnvioVisitas.kt`, `FirestoreModoEnvioVisitas.kt` (crear) | Kill-switch. |
| `app/.../workers/VisitsReconcileWorker.kt` (modificar) | Reencolado ordenado de todas las pendientes. |
| `app/.../core/sync/pendingwork/data/synchronizers/VisitsPendingSynchronizer.kt` (modificar) | Sin tope de 50 ni orden aleatorio. |
| `app/.../workers/VisitsDailyReconcileWorker.kt` (crear) | Reconciliación inversa cada 24 h. |
| `app/.../core/visitas/envio/TelemetriaDeVisitas.kt` (crear) | Lista blanca cerrada hacia `RemoteLogger`. |
| `app/src/main/res/xml/data_extraction_rules.xml`, `backup_rules.xml` (modificar) | Excluir `database`. |
| `app/src/test/.../visitas/envio/simulacion/*` (crear) | Simulación Kotlin + historial + verificador. |

---

## Paralelismo

| Carril | Tareas | Puede correr en paralelo con |
|---|---|---|
| A — msp-api | 1(api), 3, 4, 5, 6, 7, 16 | Carril B (otro repo). Dentro del carril, 4, 5 y 6 tocan archivos distintos: paralelas entre sí **solo** si cada una va en su propio worktree (`.wt-visitas-p1-<n>` creado con permiso del dueño); en el checkout principal, en serie. |
| B — msp-app-kt | 1(app), 8–15, 17 | Carril A. Dentro del carril, **en serie** (Gradle/KSP no tolera dos builds; varias tocan `VisitDao.kt`). |
| C — compuertas y mediciones | 2, 6b, 18, 19, 21 | Dependen de A y B; las tareas MEDIR de producción dependen del dueño. |

Camino crítico: 1 → 2 (mediciones + compuerta antes) → 8 → 9 → 12/13 → 17 (simulación) → 18 (caos) → 19 (adversario) → 21 (compuerta después).

---

### Task 1: Guardia de aislamiento en los dos repos (capa 2)

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento.sh`
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento.lista`
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento_test.sh`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/lefthook.yml` (pre-commit y pre-push)
- Modify: `/Volumes/M2-1TB/Developer/msp-api/.github/workflows/ci.yml` (job `quality`)
- Create: `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/guardia-aislamiento.sh` (mismo script)
- Create: `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/guardia-aislamiento.lista`
- Create: `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/guardia-aislamiento_test.sh`
- Modify: `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/pre-commit`, `scripts/pre-push`

**Interfaces:**
- Produces: `scripts/guardia-aislamiento.sh [--rango <rev-range>] [--staged]` → exit 0 si el diff es limpio, 1 si toca un vedado (imprime `guardia: vedado: <archivo> (patrón <p>)`), 2 si error de uso. Sin bandera para saltarla.

- [ ] **Step 1: Escribir la lista de msp-api**

`/Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento.lista`:
```
# Archivos de dinero. Spec 2026-10-15-visitas-compromisos-avisos-design.md §7.1.
# Quitar una línea es un PR aparte que aprueba el dueño. No hay bandera para saltar esta guardia.
internal/cobranza/**
internal/ventas/**
internal/rutas/**
internal/clientes/**
internal/desempeno/domain/cobro.go
internal/desempeno/domain/formas_cobro.go
internal/platform/firebird/**
internal/platform/idempotency/**
internal/platform/failedintent/**
migrations-firebird/*pagos*
migrations-firebird/*saldos*
migrations-firebird/*sync_epoch*
docs/COBRANZA-SYNC.md
cmd/api/failedintent_wiring.go
```
(`cmd/api/failedintent_wiring.go` se lista para que la excepción por contenido de abajo sea la única puerta.)

- [ ] **Step 2: Escribir la prueba de la guardia (debe fallar: el script no existe)**

`/Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento_test.sh`:
```bash
#!/usr/bin/env bash
# Prueba de la guardia de aislamiento: control positivo (lo vedado se rechaza)
# y negativo (lo permitido pasa). Corre en un repo git temporal.
set -euo pipefail
AQUI="$(cd "$(dirname "$0")" && pwd)"
GUARDIA="$AQUI/guardia-aislamiento.sh"
LISTA="$AQUI/guardia-aislamiento.lista"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
cd "$TMP"
git init -q -b main . && git config user.email t@t && git config user.name t
mkdir -p scripts internal/cobranza/app internal/visitas/app cmd/api migrations-firebird
cp "$GUARDIA" scripts/ && cp "$LISTA" scripts/
printf 'package main\n\nfunc x() {\n\treturn failedintent.NewRegistroExtractores().\n\t\tRegistrar("/v2/ventas", "ventas", a).\n\t\tRegistrar("/v2/cobranza/pagos", "pagos", b)\n}\n' > cmd/api/failedintent_wiring.go
echo 'package app' > internal/cobranza/app/x.go
echo 'package app' > internal/visitas/app/x.go
git add -A && git commit -qm base
falla=0
espera() { # $1 = código esperado, $2 = descripción
  set +e; bash scripts/guardia-aislamiento.sh --rango main...HEAD >/dev/null 2>&1; rc=$?; set -e
  if [[ $rc -ne $1 ]]; then echo "FALLA: $2 (esperado $1, obtuvo $rc)"; falla=1; else echo "ok: $2"; fi
  git checkout -q main && git branch -D -q prueba 2>/dev/null || true
}
git checkout -q -b prueba; echo '// x' >> internal/visitas/app/x.go; git commit -qam v
espera 0 "tocar solo visitas pasa"
git checkout -q -b prueba; echo '// x' >> internal/cobranza/app/x.go; git commit -qam c
espera 1 "tocar cobranza se rechaza (control positivo)"
git checkout -q -b prueba
sed -i.bak 's#Registrar("/v2/cobranza/pagos", "pagos", b)#Registrar("/v2/cobranza/pagos", "pagos", b).\n\t\tRegistrar("/v2/visitas", "visitas", c)#' cmd/api/failedintent_wiring.go && rm cmd/api/failedintent_wiring.go.bak
git commit -qam w
espera 0 "agregar solo la línea Registrar(/v2/visitas) en el wiring pasa"
git checkout -q -b prueba; sed -i.bak 's#"ventas", a#"ventas", z#' cmd/api/failedintent_wiring.go && rm cmd/api/failedintent_wiring.go.bak; git commit -qam w2
espera 1 "cambiar otra línea del wiring se rechaza"
git checkout -q -b prueba
printf "INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT) VALUES ('visitas', 0, 0, 'x', CAST('2026-10-16 00:00:00' AS TIMESTAMP));\n" > migrations-firebird/000067_msp_cfg_sync_epoch_visitas_tareas.up.sql
git add -A && git commit -qm e
espera 0 "migración que solo inserta epoch de visitas pasa"
git checkout -q -b prueba
printf "UPDATE MSP_CFG_SYNC_EPOCH SET EPOCH = 1 WHERE RECURSO = 'pagos';\n" > migrations-firebird/000067_msp_cfg_sync_epoch_x.up.sql
git add -A && git commit -qm e2
espera 1 "migración de epoch con UPDATE o pagos se rechaza"
exit $falla
```

- [ ] **Step 3: Correr la prueba y ver el rojo**

Run: `bash /Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento_test.sh`
Expected: falla con `cp: ... guardia-aislamiento.sh: No such file or directory`.

- [ ] **Step 4: Escribir la guardia**

`/Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento.sh`:
```bash
#!/usr/bin/env bash
# Guardia de aislamiento del dinero (spec visitas §7.1, capa 2).
# Sale 1 si el diff toca un archivo de scripts/guardia-aislamiento.lista,
# salvo las excepciones enumeradas, que se verifican por CONTENIDO del hunk.
# No hay bandera para saltarla: quitar un patrón es un PR que aprueba el dueño.
set -euo pipefail
RAIZ="$(git rev-parse --show-toplevel)"
LISTA="$RAIZ/scripts/guardia-aislamiento.lista"
modo="rango"; rango="origin/main...HEAD"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --rango) rango="$2"; shift 2 ;;
    --staged) modo="staged"; shift ;;
    *) echo "uso: $0 [--rango <a...b>] [--staged]" >&2; exit 2 ;;
  esac
done
if [[ "$modo" == "staged" ]]; then
  archivos="$(git diff --cached --name-only)"; difc() { git diff --cached -U0 -- "$1"; }
else
  if ! git rev-parse --verify -q "${rango%%...*}" >/dev/null; then rango="main...HEAD"; fi
  archivos="$(git diff --name-only "$rango")"; difc() { git diff -U0 "$rango" -- "$1"; }
fi
glob_a_regex() { # ** = cualquier cosa (incluye /), * = cualquier cosa sin /, [..] se conserva
  local g="$1"
  g="$(printf '%s' "$g" | sed -e 's/[.^$+(){}|]/\\&/g' -e 's/\*\*/§/g' -e 's/\*/[^\/]*/g' -e 's/§/.*/g')"
  printf '^%s$' "$g"
}
# Líneas agregadas/quitadas del hunk, sin las cabeceras.
cambios() { difc "$1" | grep -E '^[+-]' | grep -vE '^(\+\+\+|---) ' || true; }
excepcion_ok() {
  local f="$1" c
  c="$(cambios "$f")"
  case "$f" in
    cmd/api/failedintent_wiring.go)
      # Solo se permite AGREGAR Registrar("/v2/visitas", …) (y el punto que encadena la línea previa).
      [[ -z "$(printf '%s\n' "$c" | grep -E '^-' | grep -vE '^-[[:space:]]*Registrar\("/v2/cobranza/pagos", "pagos", [^)]*\)\)?$' || true)" ]] || return 1
      [[ -z "$(printf '%s\n' "$c" | grep -E '^\+' | grep -vE '^\+[[:space:]]*Registrar\("/v2/(visitas|cobranza/pagos)", "(visitas|pagos)", [^)]*\)\)?\.?$' || true)" ]] || return 1
      printf '%s\n' "$c" | grep -qE '^\+[[:space:]]*Registrar\("/v2/visitas"' ;;
    migrations-firebird/*sync_epoch*)
      [[ "$f" == *.up.sql || "$f" == *.down.sql ]] || return 1
      printf '%s\n' "$c" | grep -qiE "\bUPDATE\b|'pagos'|'ventas'" && return 1
      [[ -z "$(printf '%s\n' "$c" | grep -E '^-' || true)" ]] || return 1
      return 0 ;;
    core/database/src/main/kotlin/com/example/msp_app/core/database/AppDatabase.kt)
      # version, entities, accesores DAO nuevos, imports y addMigrations. Nada más.
      [[ -z "$(printf '%s\n' "$c" | grep -vE '^[+-][[:space:]]*(version[[:space:]]*=[[:space:]]*[0-9]+,?|[A-Za-z]+Entity::class,?|abstract fun [a-zA-Z]+\(\): [A-Za-z]+Dao|import com\.example\.msp_app\.core\.database\.[a-zA-Z.]+|\.addMigrations\(|MIGRATION_[0-9]+_[0-9]+[,)]?|)$' || true)" ]] ;;
    *) return 1 ;;
  esac
}
rc=0
while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  while IFS= read -r p; do
    [[ -z "$p" || "$p" == \#* ]] && continue
    for patron in $p; do
      if [[ "$f" =~ $(glob_a_regex "$patron") ]]; then
        if excepcion_ok "$f"; then echo "guardia: excepción verificada: $f"; else
          echo "guardia: vedado: $f (patrón $patron)"; rc=1; fi
      fi
    done
  done < "$LISTA"
done <<< "$archivos"
exit $rc
```
`chmod +x` ambos scripts.

- [ ] **Step 5: Correr la prueba y ver verde**

Run: `bash /Volumes/M2-1TB/Developer/msp-api/scripts/guardia-aislamiento_test.sh`
Expected: seis líneas `ok:` y exit 0. Si una sale `FALLA`, corregir el script, no la prueba.

- [ ] **Step 6: Engancharla en lefthook y CI**

En `lefthook.yml`, bajo `pre-commit: commands:` agregar:
```yaml
    guardia-aislamiento:
      run: bash scripts/guardia-aislamiento.sh --staged
```
y bajo `pre-push: commands:`:
```yaml
    guardia-aislamiento:
      run: bash scripts/guardia-aislamiento.sh --rango origin/main...HEAD
```
En `.github/workflows/ci.yml`, job `quality`, después del checkout (que debe tener `fetch-depth: 0`; si no lo tiene, agregarlo) agregar:
```yaml
      - name: Guardia de aislamiento del dinero
        run: |
          git fetch origin main --depth=1000
          bash scripts/guardia-aislamiento.sh --rango origin/main...HEAD
      - name: Prueba de la guardia
        run: bash scripts/guardia-aislamiento_test.sh
```

- [ ] **Step 7: Repetir para msp-app-kt**

Copiar el mismo `guardia-aislamiento.sh` (idéntico byte a byte) a `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/`. Lista `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/guardia-aislamiento.lista`:
```
# Spec visitas §7.1 (enmendado 2026-10-16). Quitar una línea es un PR que aprueba el dueño.
app/src/main/java/com/example/msp_app/workers/PendingPaymentsWorker.kt
core/upload/**
app/src/main/java/com/example/msp_app/core/sync/cobranza/**
app/src/main/java/com/example/msp_app/data/pagos/**
app/src/main/java/com/example/msp_app/data/api/services/payment/**
app/src/main/java/com/example/msp_app/data/api/services/cobranza/**
core/database/src/main/kotlin/com/example/msp_app/core/database/dao/payment/**
core/database/src/main/kotlin/com/example/msp_app/core/database/dao/sale/SaleDao.kt
core/database/src/main/kotlin/com/example/msp_app/core/database/entities/PaymentEntity.kt
core/database/src/main/kotlin/com/example/msp_app/core/database/migrations/Migration[0-9]*to3[0-3].kt
core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/EstadoCuentaDeriver.kt
core/common/src/main/kotlin/com/example/msp_app/core/common/cobranza/domain/VentanaCobro.kt
feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/data/**
core/database/src/main/kotlin/com/example/msp_app/core/database/AppDatabase.kt
```
Prueba `/Volumes/M2-1TB/Developer/msp-app-kt/scripts/guardia-aislamiento_test.sh`: mismo esqueleto que el de msp-api, con estos cuatro casos: tocar `app/src/main/java/com/example/msp_app/workers/PendingVisitsWorker.kt` → 0; tocar `.../workers/PendingPaymentsWorker.kt` → 1 (control positivo, spec §7.1 "Prueba del chequeo"); en `AppDatabase.kt` cambiar `version = 34,` por `version = 35,` y agregar `MIGRATION_34_35` dentro de `.addMigrations(` → 0; en `AppDatabase.kt` cambiar `.fallbackToDestructiveMigrationFrom(1..19)` → 1. El archivo semilla de `AppDatabase.kt` del repo temporal:
```kotlin
package x
import com.example.msp_app.core.database.migrations.MIGRATION_33_34
@Database(
    entities = [
        VisitEntity::class,
    ],
    version = 34,
)
abstract class AppDatabase : RoomDatabase() {
    fun b() = builder.addMigrations(
        MIGRATION_33_34,
    ).fallbackToDestructiveMigrationFrom(1..19)
}
```
En `scripts/pre-commit` agregar al inicio (después del `set -e` que ya tenga): `bash scripts/guardia-aislamiento.sh --staged`; en `scripts/pre-push`: `bash scripts/guardia-aislamiento.sh --rango origin/main...HEAD`. Si existe `.github/workflows/` en msp-app-kt, agregar el mismo paso que en msp-api; si no existe, no crearlo (dejarlo dicho en el reporte).

Run: `bash /Volumes/M2-1TB/Developer/msp-app-kt/scripts/guardia-aislamiento_test.sh`
Expected: cuatro `ok:` y exit 0.

- [ ] **Step 8: Commit (cada repo)**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && git add scripts/guardia-aislamiento.sh scripts/guardia-aislamiento.lista scripts/guardia-aislamiento_test.sh lefthook.yml .github/workflows/ci.yml && git commit -m "ci(guardia): bloquea diffs que tocan archivos de dinero durante el trabajo de visitas"
cd /Volumes/M2-1TB/Developer/msp-app-kt && git add scripts/guardia-aislamiento.sh scripts/guardia-aislamiento.lista scripts/guardia-aislamiento_test.sh scripts/pre-commit scripts/pre-push && git commit -m "ci(guardia): bloquea diffs que tocan pagos, saldos y core:upload"
```

---

### Task 2: Registro de evidencia y mediciones previas de P1 (dev + MEDIR producción) y compuerta "antes"

Esta tarea tiene tres partes y **detiene el plan** si la compuerta no devuelve SOSTIENE. Las partes de producción dependen del dueño.

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/docs/evidencia/visitas.md`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/docs/evidencia/app-android.md` (línea 50 "siguiente libre" → `E-APP-076` al terminar este plan; entradas 068–072)
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/medicion/visitas-p1-dev.sql`, `visitas-p1-prod.sql`, `visitas-p1-prod.bat`
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/limite_cuerpo_e2e_test.go` (E-VIS-003)
- Test (Android, rama de la Tarea 8): `app/src/test/java/com/example/msp_app/workers/MedicionesEnvioVisitasTest.kt` (E-VIS-001, E-APP-071, E-APP-072, E-APP-074, E-APP-075)

**Interfaces:**
- Produces: entradas E-VIS-001, -003, -005, -008, -009, -011 (Q6, adelantada a P1 porque P1 confía en que solo este API escribe), E-APP-071…072 con nivel y estado. La Tarea 4 (H11) produce E-VIS-004.

- [ ] **Step 1: Crear `docs/evidencia/visitas.md`**

Copiar el encabezado de `docs/evidencia/infra.md` (líneas 1–37: niveles, cómo se cita) cambiando el título a `# Evidencia — visitas, compromisos y avisos` y la línea final a `**Siguiente identificador libre: \`E-VIS-018\`.**`. Luego una entrada por cada E-VIS-001…017 de spec §9.2, con este formato exacto (ejemplo para la 001; las demás igual, con la afirmación y el "cómo se mide" de la tabla §9.2 y el `archivo:línea` del spec §3.2):

```markdown
### E-VIS-001 · El teléfono marca enviada una visita sin prueba del servidor (H1)

- **Nivel:** LEÍDO · **Estado:** VIGENTE
- **Leído:** 2026-10-15, `VisitUploadClassifier.kt:86,92,95`; `PendingVisitsWorker.kt:116-147`; `DEFAULT_MAX_ATTEMPTS = 10` (`:297`)
- **Qué lo vuelve MEDIDO:** prueba de worker con 500 sin cabecera ×10 ⇒ hoy marca; control positivo: con 2xx marca
- **Lo usa:** P1 (decisión de quitar `VisitUploadClassifier` del modo v3)
```
E-VIS-017 (hueco de commit tardío de pagos, DA-17) además lleva la línea `- **Cruce:** se registra también como E-COB-031 en cobranza.md cuando se mida; no se arregla en este trabajo`.

- [ ] **Step 2: Prueba E-VIS-003 (413 desde 1 MiB) en dev — escribir**

`/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/limite_cuerpo_e2e_test.go`:
```go
package visitashttp_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// E-VIS-003: Huma pone MaxBodyBytes = 1 MiB por omisión (huma.go:1378-1382) y
// responde 413 cuando el cuerpo lo alcanza (huma.go:2127-2128). Esta prueba
// MIDE el tope efectivo de POST /visitas con el router real.
func TestE2E_EVIS003_TopeDelCuerpoDePostVisitas(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	h := mountWithUser(visitaUser(), f.svc)

	cuerpoDe := func(n int) []byte {
		// JSON válido cuya NOTA rellena hasta n bytes; el contenido no importa,
		// el tope se evalúa antes de decodificar.
		base := `{"id":"8f1c2b8e-1d3a-4e4b-9c55-0a1b2c3d4e5f","nota":"`
		cola := `"}`
		return []byte(base + strings.Repeat("a", n-len(base)-len(cola)) + cola)
	}
	post := func(b []byte) int {
		req := httptest.NewRequest(http.MethodPost, "/visitas", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "8f1c2b8e-1d3a-4e4b-9c55-0a1b2c3d4e5f")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.NotEqual(t, http.StatusRequestEntityTooLarge, post(cuerpoDe(1_048_575)),
		"control positivo: 1 byte bajo el tope no es 413")
	assert.Equal(t, http.StatusRequestEntityTooLarge, post(cuerpoDe(1_048_576)),
		"E-VIS-003: a 1 MiB exacto Huma responde 413")
}
```
(`newMultipartFixture`, `mountWithUser`, `visitaUser` existen en `handlers_multipart_test.go:129`, `handlers_test.go:135`, `handlers_test.go:98`; si el paquete de esos archivos es `visitashttp` y no `visitashttp_test`, usar el mismo paquete que ellos.)

- [ ] **Step 3: Correr E-VIS-003 y registrar la salida**

Run: `cd /Volumes/M2-1TB/Developer/msp-api && go clean -testcache && go test ./internal/visitas/infra/visitashttp/ -run TestE2E_EVIS003 -v -count=1`
Expected: si PASA, E-VIS-003 queda **MEDIDO** (pegar la salida en la entrada con fecha y "dev, router en proceso"); si la segunda aserción falla, E-VIS-003 queda **REFUTADA** y la Tarea 7 no se ejecuta. Nota: esta prueba mide el router; el 413 en producción se confirma con Q4 (tamaño máximo real por visita).

- [ ] **Step 4: Mediciones Android en JVM (E-VIS-001, E-APP-071, 069, 071, 072) — escribir**

Se escriben en la rama Android de la Tarea 8 **antes** de tocar el worker (miden lo de hoy). `app/src/test/java/com/example/msp_app/workers/MedicionesEnvioVisitasTest.kt`:
```kotlin
package com.example.msp_app.workers

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.work.BackoffPolicy
import androidx.work.Configuration
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.testing.SynchronousExecutor
import androidx.work.testing.WorkManagerTestInitHelper
import com.example.msp_app.workmanager.enqueuePendingVisitsWorker
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test

/**
 * Mediciones de spec visitas §9.2 sobre el código de HOY (antes de P1).
 * Cada prueba nombra su identificador; su salida se pega en docs/evidencia.
 */
class MedicionesEnvioVisitasTest : com.example.msp_app.core.testing.RoomTestBase() {

    private val context: Context get() = ApplicationProvider.getApplicationContext()

    @Before fun initWm() {
        WorkManagerTestInitHelper.initializeTestWorkManager(
            context, Configuration.Builder().setExecutor(SynchronousExecutor()).build()
        )
    }

    /** E-APP-071 (spec E-APP-058, H1b): sin setBackoffCriteria rige el default de WorkManager. */
    @Test fun `E-APP-071 el worker de visitas usa backoff exponencial de 30 s por omision`() {
        enqueuePendingVisitsWorker(context, "visita-medicion")
        val info: WorkInfo = WorkManager.getInstance(context)
            .getWorkInfosForUniqueWork("sync_pending_visit_visita-medicion").get().single()
        // WorkInfo no expone el backoff; se lee del WorkSpec persistido (API interna
        // de work-runtime, aceptable en una prueba de medición).
        val ws = androidx.work.impl.WorkManagerImpl.getInstance(context)
            .workDatabase.workSpecDao().getWorkSpec(info.id.toString())!!
        assertEquals(BackoffPolicy.EXPONENTIAL, ws.backoffPolicy)
        assertEquals(30_000L, ws.backoffDelayDuration)
        // Acumulado hasta el intento 10 = 30 s × (2^0+…+2^8) = 15,330 s ≈ 4.26 h (spec H1b).
        assertEquals(15_330L, (0..8).sumOf { 30L shl it })
    }
}
```
Agregar en el mismo archivo, siguiendo el estilo de `PendingVisitsWorkerV2Test` (`buildAndRunWorker`, `seed`, `pendingVisit`, `fakeV2Api`, `httpError` de ese archivo: **mover esos helpers a** `app/src/test/java/com/example/msp_app/workers/VisitasWorkerFixtures.kt` como funciones `internal` para compartirlos, sin cambiar su cuerpo):
- `E-VIS-001 un 500 sin cabecera en el intento 10 marca la visita enviada`: `seed(pendingVisit())`, api que lanza `httpError(500)`, `buildAndRunWorker(maxAttempts = 10, runAttemptCount = 9)` → `assertEquals(1, guardadoFlag("visita-001"))`. Control positivo en la misma prueba: con `runAttemptCount = 0` → `assertEquals(0, guardadoFlag(...))`.
- `E-APP-072 el reencolado de sesion toma 50 sin orden`: sembrar 60 pendientes con `FECHA` descendente, ejecutar `VisitsPendingSynchronizer` (constructor en `app/.../core/sync/pendingwork/data/synchronizers/VisitsPendingSynchronizer.kt:23`) con un encolador falso que guarda ids, y afirmar `assertEquals(50, encolados.size)`; registrar si el orden coincide o no con `FECHA ASC` (la salida, no una aserción sobre el orden).
- `E-APP-074 SALDO_REST menos cero no cambia el saldo`: con Room en memoria, insertar ventas con `SALDO_REST` = `0.0`, `-150.5`, `1234.56`, `-0.0` y una fila con `NULL` (si la columna lo permite; si es `NOT NULL`, registrarlo), llamar `db.saleDao().updateTotal(id, 0.0, estado)` y comparar bit a bit con `java.lang.Double.doubleToRawLongBits` (para `-0.0` registrar el resultado real: `-0.0 - 0.0 = -0.0` en IEEE 754). Solo **lee** `SaleDao`; no lo modifica (vedado).
- `E-APP-075 el cliente v2 reintenta el POST al cortarse la conexion`: `MockWebServer` con `enqueue(MockResponse().setSocketPolicy(SocketPolicy.DISCONNECT_AFTER_REQUEST))` seguido de `MockResponse().setResponseCode(201)`; construir el `OkHttpClient` con `RetrofitClientFactory(...).v2()` del módulo `:core:network` (ver `RetrofitClientFactory.kt:64`) apuntando al servidor; hacer un POST y registrar `server.requestCount` (2 = OkHttp reintentó por su cuenta). No se cambia `RetrofitClientFactory` en este plan.

- [ ] **Step 5: Correrlas y registrar**

Run: `cd /Volumes/M2-1TB/Developer/msp-app-kt && ./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*MedicionesEnvioVisitasTest*' --rerun-tasks`
Expected: verdes (miden lo de hoy). Abrir `app/build/test-results/testDevlocalDebugUnitTest/TEST-*MedicionesEnvioVisitasTest.xml`, contar 5 `testcase` y pegar los valores en E-VIS-001 y E-APP-071…072 (fecha, "Robolectric en Mac del desarrollador", control positivo). E-APP-075 y la parte de orden de E-APP-072 son observaciones: quedan MEDIDO con el valor visto.

- [ ] **Step 6: Escribir las consultas de producción (Q1, Q4, Q6) y su versión dev**

`/Volumes/M2-1TB/Developer/msp-api/scripts/medicion/visitas-p1-prod.sql` (copiar Q1, Q4 y Q6 **literalmente** de spec §10; encabezado y cierre así):
```sql
-- Mediciones P1 visitas (spec §10). SOLO LECTURA. Leer los errores ANTES que los números.
SET TRANSACTION READ ONLY ISOLATION LEVEL READ COMMITTED;
SET LIST ON;
-- == Q1a rechazos por código ==
SELECT HTTP_STATUS, ERROR_CODE, STATUS, COUNT(*) AS N
FROM MSP_FAILED_INTENTS
WHERE PATH STARTING WITH '/v2/visitas' AND METHOD = 'POST'
GROUP BY HTTP_STATUS, ERROR_CODE, STATUS;
-- == Q1b filas y llaves ==
SELECT COUNT(*) AS FILAS, COUNT(DISTINCT IDEMPOTENCY_KEY) AS LLAVES
FROM MSP_FAILED_INTENTS WHERE PATH STARTING WITH '/v2/visitas' AND METHOD = 'POST';
-- == Q1c rechazadas que NO estan en MSP_VISITAS ==
SELECT COUNT(DISTINCT f.IDEMPOTENCY_KEY) AS PERDIDAS_EN_BANDEJA
FROM MSP_FAILED_INTENTS f
WHERE f.PATH STARTING WITH '/v2/visitas' AND f.METHOD = 'POST'
  AND f.IDEMPOTENCY_KEY IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM MSP_VISITAS v WHERE v.ID = f.IDEMPOTENCY_KEY);
-- == Q4a fotos ==
SELECT COUNT(*) AS FOTOS, MAX(SIZE_BYTES) AS MAX_FOTO FROM MSP_VISITAS_IMAGENES;
-- == Q4b maximo por visita ==
SELECT MAX(T) AS MAX_POR_VISITA FROM (SELECT VISITA_ID, SUM(SIZE_BYTES) AS T FROM MSP_VISITAS_IMAGENES GROUP BY VISITA_ID);
-- == Q6a ids que no son v3 ni v4 desde el cutover ==
SELECT COUNT(*) AS N, MIN(FECHA) AS DESDE, MAX(FECHA) AS HASTA FROM MSP_VISITAS
WHERE FECHA >= TIMESTAMP '2026-08-13 00:00:00' AND SUBSTRING(ID FROM 15 FOR 1) NOT IN ('3', '4');
-- == Q6b procesos conectados ==
SELECT MON$REMOTE_PROCESS, COUNT(*) AS N FROM MON$ATTACHMENTS GROUP BY MON$REMOTE_PROCESS;
COMMIT;
-- == cierre: debe dar 0 ==
SELECT COUNT(*) AS ATORADAS FROM MON$STATEMENTS WHERE MON$STATE = 2;
```
Q4c (`SELECT STORAGE_KEY FROM MSP_VISITAS_IMAGENES`) va en un archivo aparte `visitas-p1-prod-claves.sql` porque su salida se compara contra `dir /b /s <STORAGE_DIR>\visitas` (PowerShell: `Get-ChildItem -Recurse -File $env:STORAGE_DIR\visitas | % { $_.FullName }`), con una clave inventada `visitas/00000000-0000-0000-0000-000000000000/x.jpg` agregada a la lista como control positivo (debe salir "sin archivo").

`visitas-p1-prod.bat` (patrón de memoria `reference_isql_produccion_trampas`: solo `.bat`, sin `CAST ... AS VARCHAR` concatenado):
```bat
@echo off
REM Mediciones P1 visitas. SOLO LECTURA. Lo corre el dueño o con su autorización.
"C:\Program Files\Firebird\Firebird_5_0\isql.exe" -user %ISC_USER% -password %ISC_PASSWORD% -ch UTF8 -i visitas-p1-prod.sql -o visitas-p1-prod.out localhost:MUEBLERA
type visitas-p1-prod.out
```
`visitas-p1-dev.sql`: mismas consultas para el contenedor dev (`docker exec mueblera-firebird /usr/local/firebird/bin/isql -u SYSDBA -p masterkey -ch UTF8 -i /tmp/visitas-p1-dev.sql /firebird/data/MUEBLERA.FDB`), precedidas de los **controles positivos**: (a) provocar un 422 de visita contra el API dev (`curl -s -X POST http://127.0.0.1:3001/v2/visitas -H "Authorization: Bearer dev:<uid>:<email>" -H 'Content-Type: application/json' -H 'Idempotency-Key: 11111111-1111-4111-8111-111111111111' -d '{"id":"11111111-1111-4111-8111-111111111111","tipo_visita":"no-existe"}'`, token de memoria `reference_msp_api_devmode_auth_dev_db_state`) y verificar que aparece en Q1a/Q1b/Q1c; (b) para Q6a, insertar dentro de una transacción que se revierte un id con versión `1` y ver que se cuenta.

- [ ] **Step 7: Correr en dev con control positivo y registrar**

Run: el `docker exec` de arriba. Expected: los controles positivos aparecen; ningún error `SQLSTATE` en la salida (leer los errores antes que los números). Registrar como "dev" en E-VIS-005/-008/-011.

- [ ] **Step 8: MEDIR en producción (DEPENDE DEL DUEÑO)**

El agente **no** se conecta a producción por su cuenta. Entregar al dueño `visitas-p1-prod.sql`, `visitas-p1-prod-claves.sql` y `visitas-p1-prod.bat` con la instrucción: copiar por `scp` al servidor (memoria `reference_windows_server_ssh`), correr el `.bat`, devolver `visitas-p1-prod.out`. Con la salida: actualizar E-VIS-005 (Q1), E-VIS-008 (Q4), E-VIS-011 (Q6) a MEDIDO "producción, fecha". Si `ATORADAS` ≠ 0, avisar al dueño de inmediato (congela el watermark de pagos, `WM:47-60`).

- [ ] **Step 9: MEDIR Q5/Q5b en el teléfono del dueño (DEPENDE DEL DUEÑO)**

Requiere autorización explícita del dueño y `config/db_debug.allowedDevices` con su correo (`COBRANZA-SYNC.md:416-428`; `blockDangerousQueries` = `true` según E-INF-007). Comandos SELECT del spec §10 Q5. Con los ids de `SELECT ID FROM Visit WHERE GUARDADO_EN_MICROSIP = 1`, generar `visitas-p1-prod-q5b.sql` en tandas de ≤ 120 ids (`SELECT ID FROM MSP_VISITAS WHERE ID IN (...)`) más un id que sí existe como control positivo. Registrar E-VIS-009 y E-VIS-010.

- [ ] **Step 10: Compuerta "antes" de P1 (agente independiente)**

Despachar un agente nuevo (no el autor, no un agente que ya trabajó en este plan) con **solo las preguntas**, sin las conclusiones:
```
Pregunta para verificar, sin conclusión previa. Repositorios: /Volumes/M2-1TB/Developer/msp-api y /Volumes/M2-1TB/Developer/msp-app-kt (rama feat/diagnostico-envio).
1. ¿El worker de visitas puede marcar GUARDADO_EN_MICROSIP=1 sin que el servidor tenga la visita ni la haya resguardado? ¿En qué códigos y en qué intento?
2. ¿Cuál es el retraso acumulado de WorkManager hasta el intento 10 del worker de visitas?
3. ¿Qué tope de cuerpo aplica hoy a POST /v2/visitas y qué responde al alcanzarlo?
4. ¿Puede la limpieza de blobs de un intento fallido borrar el archivo al que apunta una fila confirmada por otro intento?
5. ¿Cuántas visitas rechazadas hay en la bandeja de producción que no están en MSP_VISITAS? (usar docs/evidencia/visitas.md E-VIS-005 y re-derivar desde la salida pegada)
6. ¿El reencolado de visitas pendientes tiene tope y orden?
7. ¿La base de Room entra en Auto Backup?
8. ¿OkHttp reintenta el POST de visitas por su cuenta?
9. ¿Escribe alguien más que este API en MSP_VISITAS?
Para cada una: SOSTIENE / CONTRADICE / FALTA EVIDENCIA / PARCIAL contra la entrada E-VIS/E-APP correspondiente, con archivo:línea o salida.
```
Expected: todas SOSTIENE. **Cualquier otra cosa detiene el plan** hasta medir lo que falte. La respuesta se pega en `docs/evidencia/visitas.md` bajo `## Compuerta antes P1 (fecha)`.

- [ ] **Step 11: Commit**

```bash
cd /Volumes/M2-1TB/Developer/msp-api && git add docs/evidencia/visitas.md docs/evidencia/app-android.md scripts/medicion/visitas-p1-* internal/visitas/infra/visitashttp/limite_cuerpo_e2e_test.go && git commit -m "docs(evidencia): mediciones previas del envío de visitas y compuerta antes de P1 (E-VIS-001, E-VIS-003, E-APP-071..072)"
cd /Volumes/M2-1TB/Developer/msp-app-kt && git add app/src/test/java/com/example/msp_app/workers/MedicionesEnvioVisitasTest.kt app/src/test/java/com/example/msp_app/workers/VisitasWorkerFixtures.kt app/src/test/java/com/example/msp_app/workers/PendingVisitsWorkerV2Test.kt && git commit -m "test(visitas): mide el envío de hoy antes de cambiarlo (E-VIS-001, E-APP-071..072)"
```

---

### Task 3: Extractor de resumen de `/v2/visitas` para la bandeja (H10)

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/failedintents/resumen.go`
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/failedintents/resumen_test.go`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/cmd/api/failedintent_wiring.go:146-150` (solo agregar la línea `Registrar`; quitar el comentario de :143-145 **no** está permitido por la guardia: dejarlo y agregar debajo, en el mismo bloque, una línea `// Desde P1 visitas sí tiene extractor (spec visitas §4.3-2).` — **ojo**: la guardia rechaza líneas agregadas que no sean `Registrar`; por eso **no** se agrega comentario y se deja el viejo tal cual; la contradicción del comentario se anota en el reporte para un PR aparte que apruebe el dueño)

**Interfaces:**
- Consumes: `failedintent.ResumenExtractor` (`internal/platform/failedintent/resumen.go:79-92`): `Extraer(path string, body []byte, contentType string) *Resumen`; `Resumen{Modulo, Titulo string; Monto *decimal.Decimal; Referencia string; ClienteID *int}`.
- Produces: `failedintents.NewResumenExtractor() *ResumenExtractor` en el paquete `github.com/abdimuy/msp-api/internal/visitas/infra/failedintents`.

- [ ] **Step 1: Prueba que falla**

`resumen_test.go`:
```go
package failedintents_test

import (
	"bytes"
	"mime/multipart"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/infra/failedintents"
)

func TestResumenVisita_JSON(t *testing.T) {
	t.Parallel()
	body := []byte(`{"id":"8f1c2b8e-1d3a-4e4b-9c55-0a1b2c3d4e5f","cliente_id":4521,"cobrador":"Rosa Méndez",` +
		`"tipo_visita":"No se encontraba","impte_docto_cc_id":13914738,"fecha":"2026-10-16T17:30:00Z",` +
		`"nota":"dato personal que NO debe salir"}`)
	r := failedintents.NewResumenExtractor().Extraer("/v2/visitas", body, "application/json")
	require.NotNil(t, r)
	assert.Equal(t, "No se encontraba · Rosa Méndez", r.Titulo)
	assert.Equal(t, "venta 13914738 · 2026-10-16T17:30:00Z", r.Referencia)
	require.NotNil(t, r.ClienteID)
	assert.Equal(t, 4521, *r.ClienteID)
	assert.Nil(t, r.Monto, "una visita no mueve dinero")
	assert.NotContains(t, r.Titulo+r.Referencia, "dato personal", "nunca la nota completa (spec §4.3-2)")
}

func TestResumenVisita_Multipart(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.WriteField("datos",
		`{"cliente_id":77,"cobrador":"Juan Pérez","tipo_visita":"Pidió que regrese otro día","fecha":"2026-10-16T10:00:00Z"}`))
	require.NoError(t, mw.Close())
	r := failedintents.NewResumenExtractor().Extraer("/v2/visitas", buf.Bytes(), mw.FormDataContentType())
	require.NotNil(t, r)
	assert.Equal(t, "Pidió que regrese otro día · Juan Pérez", r.Titulo)
	assert.Equal(t, "2026-10-16T10:00:00Z", r.Referencia, "sin venta, la referencia es solo la fecha")
}

func TestResumenVisita_CuerpoIrreconocibleDaNil(t *testing.T) {
	t.Parallel()
	ex := failedintents.NewResumenExtractor()
	assert.Nil(t, ex.Extraer("/v2/visitas", []byte("no es json"), "application/json"))
	assert.Nil(t, ex.Extraer("/v2/visitas", []byte(`{}`), "application/json"))
	assert.Nil(t, ex.Extraer("/v2/visitas", nil, "multipart/form-data; boundary=x"))
}
```

- [ ] **Step 2: Correr y ver el rojo**

Run: `cd /Volumes/M2-1TB/Developer/msp-api && go test ./internal/visitas/infra/failedintents/ -count=1`
Expected: FAIL, `package .../visitas/infra/failedintents is not in std` / no compila.

- [ ] **Step 3: Implementación**

`resumen.go`:
```go
// Package failedintents resume una visita rechazada para la bandeja de
// intentos fallidos (spec visitas §4.3-2, H10): cliente, venta, tipo y fecha.
// Nunca la nota: puede traer datos personales y no ayuda a corregir.
package failedintents

import (
	"bytes"
	"encoding/json"
	"mime"
	"mime/multipart"
	"strconv"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// ResumenExtractor implementa failedintent.ResumenExtractor para /v2/visitas.
type ResumenExtractor struct{}

// NewResumenExtractor construye el extractor. No tiene estado.
func NewResumenExtractor() *ResumenExtractor { return &ResumenExtractor{} }

var _ failedintent.ResumenExtractor = (*ResumenExtractor)(nil)

type cuerpoVisita struct {
	ClienteID      int    `json:"cliente_id"`
	Cobrador       string `json:"cobrador"`
	TipoVisita     string `json:"tipo_visita"`
	ImpteDoctoCCID *int   `json:"impte_docto_cc_id"`
	Fecha          string `json:"fecha"`
}

// maxDatosBytes acota cuánto se lee del campo datos de un multipart.
const maxDatosBytes = 64 * 1024

// Extraer es puro y nunca entra en pánico: ante cualquier cosa rara, nil.
func (ResumenExtractor) Extraer(_ string, body []byte, contentType string) *failedintent.Resumen {
	raw := body
	if mt, params, err := mime.ParseMediaType(contentType); err == nil && strings.HasPrefix(mt, "multipart/") {
		raw = datosDeMultipart(body, params["boundary"])
	}
	if len(raw) == 0 {
		return nil
	}
	var c cuerpoVisita
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil
	}
	if c.ClienteID == 0 && c.Cobrador == "" && c.TipoVisita == "" && c.Fecha == "" {
		return nil
	}
	r := &failedintent.Resumen{Titulo: unir(" · ", c.TipoVisita, c.Cobrador)}
	venta := ""
	if c.ImpteDoctoCCID != nil && *c.ImpteDoctoCCID > 0 {
		venta = "venta " + strconv.Itoa(*c.ImpteDoctoCCID)
	}
	r.Referencia = unir(" · ", venta, c.Fecha)
	if c.ClienteID > 0 {
		id := c.ClienteID
		r.ClienteID = &id
	}
	return r
}

func datosDeMultipart(body []byte, boundary string) []byte {
	if boundary == "" || len(body) == 0 {
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		p, err := mr.NextPart()
		if err != nil {
			return nil
		}
		if p.FormName() != "datos" {
			continue
		}
		buf := make([]byte, maxDatosBytes)
		n, _ := p.Read(buf)
		for n < maxDatosBytes {
			m, e := p.Read(buf[n:])
			n += m
			if e != nil {
				break
			}
		}
		return buf[:n]
	}
}

func unir(sep string, partes ...string) string {
	out := make([]string, 0, len(partes))
	for _, p := range partes {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}
```

- [ ] **Step 4: Verde**

Run: `go test ./internal/visitas/infra/failedintents/ -count=1 -race`
Expected: PASS (3 pruebas).

- [ ] **Step 5: Registrar en el wiring (una línea)**

En `cmd/api/failedintent_wiring.go:146-150`, el retorno queda:
```go
	return failedintent.NewRegistroExtractores().
		Registrar("/v2/ventas", "ventas", ventasfailedintents.NewResumenExtractor()).
		Registrar("/v2/cobranza/pagos", "pagos", cobranzafailedintents.NewResumenExtractor()).
		Registrar("/v2/visitas", "visitas", visitasfailedintents.NewResumenExtractor())
```
y el import `visitasfailedintents "github.com/abdimuy/msp-api/internal/visitas/infra/failedintents"`. El import es una línea agregada que la guardia **rechazaría**: ampliar la excepción de la Tarea 1 con la regex `^\+[[:space:]]*visitasfailedintents "github.com/abdimuy/msp-api/internal/visitas/infra/failedintents"$` y su caso en `guardia-aislamiento_test.sh` (agregar import → 0), en el mismo commit, y decirlo en el reporte (el spec §7.1 omitió el import; es un hueco del spec).

- [ ] **Step 6: Prueba de wiring**

Agregar a `cmd/api/failedintent_wiring_test.go` (crear si no existe, `package main`):
```go
func TestResumenExtractor_VisitasRegistrado(t *testing.T) {
	t.Parallel()
	r := provideFailedIntentResumenExtractor().Extraer("/v2/visitas",
		[]byte(`{"cliente_id":1,"tipo_visita":"No se encontraba","fecha":"2026-10-16T10:00:00Z"}`), "application/json")
	require.NotNil(t, r)
	assert.Equal(t, "visitas", r.Modulo, "el registro pone el módulo (resumen.go:31-53)")
}
```
Run: `go test ./cmd/api/ -run 'TestResumenExtractor_VisitasRegistrado|TestFailedIntentRutasRaiz' -count=1`
Expected: PASS. Revertir la línea `Registrar("/v2/visitas"…)` y correr: la prueba nueva debe ponerse **roja**; restaurar.

- [ ] **Step 7: Commit**

```bash
git add internal/visitas/infra/failedintents cmd/api/failedintent_wiring.go cmd/api/failedintent_wiring_test.go scripts/guardia-aislamiento.sh scripts/guardia-aislamiento_test.sh
git commit -m "feat(visitas): resumen legible de la visita rechazada en la bandeja (H10)"
```

---

### Task 4: Limpieza de blobs sin pisar a otro intento (H11, E-VIS-004)

**Files:**
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/app/registrar_visita_con_imagenes_test.go` (prueba E-VIS-004)
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/multipart_helpers.go:332-346` (nonce)
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/handlers_multipart_test.go` (prueba de clave por intento)
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/ports/outbound/storage.go:36` y `app/registrar_visita_con_imagenes.go:21` (solo el comentario del formato de clave)

**Interfaces:**
- Produces: formato de clave `visitas/<visita_id>/<imagen_id>-<nonce><ext>`, `nonce` = 16 hex de `crypto/rand`, uno por petición; `newStorageKeyVisita(visitaID, imagenID uuid.UUID, nonce, filename, mimeType string) string`.

- [ ] **Step 1: Medición E-VIS-004 — escribir la prueba (debe ponerse ROJA hoy)**

Agregar a `registrar_visita_con_imagenes_test.go`:
```go
// E-VIS-004 (H11): un segundo intento que falla dentro de su transacción no
// puede borrar el archivo al que apunta la fila que confirmó el primero.
func TestRegistrarVisitaConImagenes_EVIS004_FalloDelSegundoIntentoNoBorraElBlobDelPrimero(t *testing.T) {
	t.Parallel()
	f := newImagenesFixture(t)
	id, imgID := uuid.New(), uuid.New()

	// Intento 1: confirma la visita y su foto.
	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "FOTO-1", withKey("visitas/"+id.String()+"/"+imgID.String()+"-aaaa.jpg"))}, uuid.New())
	require.NoError(t, err)
	claveConfirmada := f.imgRepo.porVisita[id][0].Storage().Key()

	// Intento 2, concurrente: su lectura previa no vio la foto (carrera), sube
	// su copia y su transacción falla al confirmar.
	f.imgRepo.ocultarEnLecturaPrevia = true
	f.tx.failAfterFn = errors.New("commit fallido inyectado")
	_, err = f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, imgID, "FOTO-1", withKey("visitas/"+id.String()+"/"+imgID.String()+"-bbbb.jpg"))}, uuid.New())
	require.Error(t, err)

	_, sigue := f.store.blobs[claveConfirmada]
	assert.True(t, sigue, "E-VIS-004: la fila confirmada apunta a %s y su archivo debe seguir", claveConfirmada)
}
```
Para que compile: en el `fakeImagenesRepo` del mismo archivo agregar el campo `ocultarEnLecturaPrevia bool` y, en `FindImagenIDs`, `if r.ocultarEnLecturaPrevia { return nil, nil }`; agregar el helper `withKey(k string) func(*app.ImagenUploadInput) { return func(u *app.ImagenUploadInput) { u.StorageKey = k } }` (el builder `upload` ya acepta `opts ...func(*app.ImagenUploadInput)`, `:192`).

**Medición del estado de hoy:** correr primero la prueba con **la misma clave** en los dos intentos (cambiar `-bbbb` por `-aaaa`, que es lo que hace hoy el HTTP: clave determinista):

Run: `go test ./internal/visitas/app/ -run EVIS004 -count=1 -v`
Expected con claves iguales: **FAIL** (`E-VIS-004: la fila confirmada apunta a ...`) → E-VIS-004 queda **MEDIDO** (dev, prueba con fallas inyectadas). Pegar la salida en la entrada. Si pasa con claves iguales, E-VIS-004 queda REFUTADA y el resto de esta tarea no se hace.

- [ ] **Step 2: Con claves distintas (lo que producirá el nonce) la prueba pasa sin tocar el servicio**

Restaurar `-bbbb` en el segundo intento.
Run: `go test ./internal/visitas/app/ -run EVIS004 -count=1`
Expected: PASS — `cleanupBlobs` (`:245-250`) borra solo las claves que **este** intento escribió. El arreglo vive en la capa HTTP.

- [ ] **Step 3: Prueba HTTP que fija la clave por intento (roja hoy)**

En `handlers_multipart_test.go`:
```go
// H11/E-VIS-004: dos peticiones con la misma visita y la misma foto escriben
// claves DISTINTAS, para que la limpieza de una no pise a la otra.
func TestCrearVisita_MultipartClaveDeBlobLlevaNoncePorIntento(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	id := "8f1c2b8e-1d3a-4e4b-9c55-0a1b2c3d4e5f"
	datos := `{"id":"` + id + `","cliente_id":1,"cobrador_id":2,"cobrador":"Rosa Méndez","forma_cobro_id":0,` +
		`"lat":19.4,"lng":-99.1,"tipo_visita":"No se encontraba","zona_cliente_id":3,"fecha":"2026-10-16T10:00:00Z"}`
	img := jpeg("0e0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d", "")
	body, ct := buildMultipart(t, datos, []testImagen{img})
	require.Equal(t, http.StatusCreated, postMultipart(t, f, body, ct, id).Code)
	primera := f.store.keys()
	require.Len(t, primera, 1)
	assert.Regexp(t, `^visitas/`+id+`/0e0b1c2d-3e4f-4a5b-8c6d-7e8f9a0b1c2d-[0-9a-f]{16}\.jpg$`, primera[0])

	// Segundo intento con la foto oculta en la lectura previa: escribe su propia clave.
	f.imgRepo.ocultarEnLecturaPrevia = true
	body2, ct2 := buildMultipart(t, datos, []testImagen{img})
	require.Equal(t, http.StatusCreated, postMultipart(t, f, body2, ct2, id).Code)
	assert.Len(t, f.store.keys(), 2, "cada intento tiene su clave; el huérfano lo barre el conserje (RCI:241-244)")
}
```
(agregar `ocultarEnLecturaPrevia` también a `httpFakeImagenesRepo`, `handlers_multipart_test.go:38-74`, con el mismo efecto en `FindImagenIDs`).

Run: `go test ./internal/visitas/infra/visitashttp/ -run NoncePorIntento -count=1`
Expected: FAIL (la clave no lleva sufijo).

- [ ] **Step 4: Implementar el nonce**

En `multipart_helpers.go`, reemplazar `newStorageKeyVisita` (`:332-346`):
```go
// newStorageKeyVisita arma la clave de una foto:
// "visitas/<visita_id>/<imagen_id>-<nonce><ext>".
// El nonce es por PETICIÓN (spec visitas §4.3-3, H11, E-VIS-004): dos
// intentos concurrentes de la misma foto escriben claves distintas, y la
// limpieza de un intento fallido (cleanupBlobs) solo borra las suyas. La fila
// confirmada siempre apunta a la clave de su propio intento; los huérfanos los
// barre el conserje.
func newStorageKeyVisita(visitaID, imagenID uuid.UUID, nonce, filename, mimeType string) string {
	ext := canonicalExtVisita(mimeType)
	if ext == "" {
		ext = path.Ext(filename)
	}
	return "visitas/" + visitaID.String() + "/" + imagenID.String() + "-" + nonce + ext
}

// nuevoNonceDeIntento devuelve 16 hex aleatorios. Si crypto/rand falla, la
// petición falla: nunca se reutiliza una clave determinista.
func nuevoNonceDeIntento() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", apperror.NewInternal("visita_nonce_no_disponible", "no se pudo preparar el guardado de la foto")
	}
	return hex.EncodeToString(b[:]), nil
}
```
(conservar el cuerpo actual de la elección de extensión si difiere; lo único nuevo es `"-" + nonce`). En `parseImagenesFromForm` (`:208`), calcular `nonce, err := nuevoNonceDeIntento()` **una vez** antes del bucle (devolver el error si falla) y pasarlo a `newStorageKeyVisita(visitaID, imagenID, nonce, fh.Filename, mimeType)` en `:250`. Imports: `crypto/rand`, `encoding/hex`, `apperror`. Verificar que `isSafeStorageKey` (`domain/imagen_storage.go:113`) acepta `-`: agregar a `domain/imagen_storage_test.go` el caso `"visitas/<uuid>/<uuid>-0123456789abcdef.jpg"` como válido.

Actualizar los dos comentarios del formato (`ports/outbound/storage.go:36`, `app/registrar_visita_con_imagenes.go:21`).

- [ ] **Step 5: Verde y rojo al revertir**

Run: `go test ./internal/visitas/... -count=1 -race`
Expected: PASS todo (incluye `TestE2E_VisitasMultipart_*` si `FB_DATABASE` está puesto: correr también `make test-firebird-all` filtrado: `FB_DATABASE=... go test ./internal/visitas/... -p 1 -count=1 -timeout 600s`).
Revertir solo `"-" + nonce +` → `go test ./internal/visitas/infra/visitashttp/ -run NoncePorIntento` debe salir **FAIL**; restaurar y verificar con `git diff` que no quedó resto.

- [ ] **Step 6: Commit**

```bash
git add internal/visitas
git commit -m "fix(visitas): la clave de cada foto lleva nonce por intento para que una limpieza no borre la de otro (H11). Cierra E-VIS-004."
```

---

### Task 5: `imagen_ids` en `GET /v2/visitas/{id}` (prueba de existencia completa del lado servidor)

**Files:**
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/app/obtener_visita.go`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/app/obtener_visita_test.go`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/dto.go` (nuevo `VisitaDetalleDTO`)
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/handlers.go:171-190`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/obtener_visita_e2e_test.go`

**Interfaces:**
- Consumes: `outbound.VisitasImagenesRepo.FindImagenIDs(ctx, visitaID) ([]uuid.UUID, error)`.
- Produces: `func (s *Service) ObtenerVisitaConImagenIDs(ctx context.Context, id uuid.UUID) (*domain.Visita, []uuid.UUID, error)` (ids ordenados como texto; slice vacío no nulo si no hay fotos; si `s.imagenes == nil`, `errVisitasImagenesNoConfigurado`). JSON de `GET /v2/visitas/{id}`: los campos de hoy **más** `"imagen_ids": ["…"]` (siempre presente, `[]` si no hay). P2 agrega aquí `updated_at` y los cinco campos de compromiso.

- [ ] **Step 1: Prueba de servicio que falla**

En `obtener_visita_test.go`:
```go
func TestObtenerVisitaConImagenIDs_DevuelveLasFotosOrdenadas(t *testing.T) {
	t.Parallel()
	f := newImagenesFixture(t)
	id := uuid.New()
	a, b := uuid.MustParse("00000000-0000-4000-8000-00000000000b"), uuid.MustParse("00000000-0000-4000-8000-00000000000a")
	_, err := f.svc.RegistrarVisitaConImagenes(context.Background(), imagenesInput(id),
		[]app.ImagenUploadInput{upload(id, a, "A"), upload(id, b, "B")}, uuid.New())
	require.NoError(t, err)

	v, ids, err := f.svc.ObtenerVisitaConImagenIDs(context.Background(), id)
	require.NoError(t, err)
	assert.Equal(t, id, v.ID())
	assert.Equal(t, []uuid.UUID{b, a}, ids, "ordenados por texto para que la comparación del teléfono sea estable")
}

func TestObtenerVisitaConImagenIDs_SinFotosEsVacioNoNil(t *testing.T) {
	t.Parallel()
	f := newImagenesFixture(t)
	id := uuid.New()
	_, err := f.svc.RegistrarVisita(context.Background(), imagenesInput(id), uuid.New())
	require.NoError(t, err)
	_, ids, err := f.svc.ObtenerVisitaConImagenIDs(context.Background(), id)
	require.NoError(t, err)
	assert.NotNil(t, ids)
	assert.Empty(t, ids)
}
```
Run: `go test ./internal/visitas/app/ -run ObtenerVisitaConImagenIDs -count=1` → FAIL (método inexistente).

- [ ] **Step 2: Implementar**

En `obtener_visita.go`:
```go
// ObtenerVisitaConImagenIDs es la prueba de EXISTENCIA COMPLETA del teléfono
// (spec visitas §4.2-3, §4.3-4): la visita y los ids de sus fotos. Un 200
// sin una foto que el teléfono tiene pendiente NO cuenta como entregada.
func (s *Service) ObtenerVisitaConImagenIDs(ctx context.Context, id uuid.UUID) (*domain.Visita, []uuid.UUID, error) {
	if s.imagenes == nil {
		return nil, nil, errVisitasImagenesNoConfigurado
	}
	v, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	ids, err := s.imagenes.FindImagenIDs(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	out := make([]uuid.UUID, 0, len(ids))
	out = append(out, ids...)
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return v, out, nil
}
```

- [ ] **Step 3: DTO + handler + prueba HTTP**

En `dto.go`:
```go
// VisitaDetalleDTO es la respuesta de GET /visitas/{id}: VisitaDTO + imagen_ids.
// Aditivo: un teléfono viejo ignora el campo (Gson, V2VisitsApi.kt:147-161).
type VisitaDetalleDTO struct {
	VisitaDTO
	ImagenIDs []string `json:"imagen_ids"`
}
```
`ObtenerVisitaOutput{Body VisitaDetalleDTO}`. En `ObtenerVisita` (`handlers.go:171-190`) cambiar la llamada a `visita, ids, err := h.svc.ObtenerVisitaConImagenIDs(ctx, visitaID)` y armar `VisitaDetalleDTO{VisitaDTO: toVisitaDTO(visita), ImagenIDs: idsComoTexto(ids)}` con `idsComoTexto` que devuelve `[]string{}` (nunca nil, para que salga `[]` y no `null`). En `obtener_visita_e2e_test.go` agregar la aserción de que el JSON trae `"imagen_ids":[]` para una visita sin fotos y los ids para una con fotos. Run: `go test ./internal/visitas/... -count=1 -race` → PASS.

- [ ] **Step 4: Rojo al revertir**

Quitar `ImagenIDs` del DTO → las pruebas nuevas FAIL; restaurar.

- [ ] **Step 5: Commit**

```bash
git add internal/visitas
git commit -m "feat(visitas): GET /v2/visitas/{id} devuelve imagen_ids para la prueba de existencia completa"
```

---

### Task 6: `MaxBodyBytes` explícito en `POST /visitas` (condicionado a E-VIS-003 + Q4)

**Ejecutar solo si** E-VIS-003 quedó MEDIDO en la Tarea 2 **y** la compuerta antes sostuvo. Si E-VIS-003 quedó REFUTADA, marcar la tarea como "no aplica" con la cita.

**Files:**
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/routes.go:194-207` y la firma de `MountRouter`/`MountOption`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/cmd/api/server.go:360-374`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitashttp/limite_cuerpo_e2e_test.go`

**Interfaces:**
- Produces: `visitashttp.WithMaxBodyBytes(n int64) MountOption`; el servidor lo pasa con `int64(cfg.HTTP.MaxBodySizeMB) * 1024 * 1024`.

- [ ] **Step 1: Prueba que falla**

En `limite_cuerpo_e2e_test.go` agregar:
```go
func TestE2E_PostVisitasAceptaHastaElTopeGlobal(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	h := mountWithUserOpts(visitaUser(), f.svc, visitashttp.WithMaxBodyBytes(10*1024*1024))
	// 1.1 MiB: hoy 413 (E-VIS-003); con el tope global de 10 MB ya no.
	code := postCrudo(h, cuerpoDeTamano(1_153_434))
	assert.NotEqual(t, http.StatusRequestEntityTooLarge, code)
	assert.Equal(t, http.StatusRequestEntityTooLarge, postCrudo(h, cuerpoDeTamano(10*1024*1024)),
		"control positivo: el tope nuevo sigue existiendo")
}
```
(mover `cuerpoDe`/`post` de la Tarea 2 a helpers de archivo `cuerpoDeTamano(n int) []byte` y `postCrudo(h http.Handler, b []byte) int`; `mountWithUserOpts` = `mountWithUser` que reenvía `opts ...visitashttp.MountOption` a `MountRouter`).
Run → FAIL (no existe `WithMaxBodyBytes`).

- [ ] **Step 2: Implementar**

En `routes.go`, agregar al struct de opciones `maxBodyBytes int64` y:
```go
// WithMaxBodyBytes fija el tope de cuerpo de POST /visitas. Sin esta opción
// Huma pone 1 MiB y responde 413 (E-VIS-003); el servidor pasa el tope global
// HTTP_MAX_BODY_SIZE_MB para que una visita con fotos no se rechace.
func WithMaxBodyBytes(n int64) MountOption { return func(o *mountOptions) { o.maxBodyBytes = n } }
```
(usar el nombre real del struct de opciones de `routes.go`). En `registerOperations`, en la operación `crear-visita`, `MaxBodyBytes: maxBody` (pasar `maxBody` como parámetro nuevo de `registerOperations`; si es 0, no se asigna y Huma queda como hoy). En `cmd/api/server.go:373`: `visitashttp.MountRouter(r, visitasSvc, visitashttp.WithSSE(eventBus, cfg.Visitas, logger), visitashttp.WithMaxBodyBytes(int64(cfg.HTTP.MaxBodySizeMB)*1024*1024))`.

- [ ] **Step 3: Verde, rojo al revertir, commit**

Run: `go test ./internal/visitas/... ./cmd/api/ -count=1 -race` → PASS. Quitar `MaxBodyBytes: maxBody` → la prueba FAIL; restaurar.
```bash
git add internal/visitas cmd/api/server.go
git commit -m "fix(visitas): POST /v2/visitas acepta hasta el tope global de cuerpo (H7). Cierra E-VIS-003."
```

---

### Task 7: Suite de servidor P1 completa + lint

**Files:** ninguno nuevo.

- [ ] **Step 1:** `cd /Volumes/M2-1TB/Developer/msp-api && go clean -testcache && golangci-lint run ./... && go test -race -short ./... -timeout 300s`
Expected: 0 issues, PASS.
- [ ] **Step 2:** `make fb-snapshot NAME=antes-visitas-p1 && make test-firebird-all` (con `FB_DATABASE` de dev; memoria `reference_suite_integracion_timeout_override`: si se corta, `-timeout 1800s`).
Expected: PASS. Pegar el resumen en el reporte de la tarea.
- [ ] **Step 3:** `make coverage-visitas` → PASS (pisos de `scripts/cover-floors-visitas.txt`). Si un piso baja, agregar prueba, no bajar el piso.

---

### Task 8: Room v35 — `CONFIRMADA_EN` y `RESGUARDADA_EN` en `Visit`

**Base:** rama `feat/visitas-envio-blindado` creada desde `feat/diagnostico-envio` (o `main` si ya se fusionó): `git -C /Volumes/M2-1TB/Developer/msp-app-kt switch -c feat/visitas-envio-blindado feat/diagnostico-envio` (previo `git status` limpio).

**Files:**
- Modify: `core/database/src/main/kotlin/com/example/msp_app/core/database/entities/VisitEntity.kt` (después de `CITA_HORA`, línea ~98)
- Create: `core/database/src/main/kotlin/com/example/msp_app/core/database/migrations/Migration34to35.kt`
- Modify: `core/database/src/main/kotlin/com/example/msp_app/core/database/AppDatabase.kt` (`version = 35`, `MIGRATION_34_35` en `addMigrations`; nada más)
- Generate: `core/database/schemas/com.example.msp_app.core.database.AppDatabase/35.json`
- Create: `core/database/src/test/kotlin/com/example/msp_app/core/database/migration/Migration34to35Test.kt`
- Modify: `core/database/src/main/kotlin/com/example/msp_app/core/database/dao/visit/VisitDao.kt`
- Create: `core/database/src/test/kotlin/com/example/msp_app/core/database/dao/visit/VisitDaoEntregaTest.kt`

**Interfaces:**
- Produces (`VisitEntity`): `val CONFIRMADA_EN: String? = null`, `val RESGUARDADA_EN: String? = null` (instantes en `AppTime.toWireFormat`).
- Produces (`VisitDao`), los **únicos** escritores de esas columnas:
  - `suspend fun confirmar(ids: List<String>, ahora: String): Int` — `UPDATE Visit SET GUARDADO_EN_MICROSIP = 1, CONFIRMADA_EN = :ahora, RESGUARDADA_EN = NULL WHERE ID IN (:ids) AND ID NOT IN (SELECT VISITA_ID FROM visita_imagenes WHERE SUBIDA_EN IS NULL)` (conserva la retención por foto de `markSyncedByIds`, `VisitDao.kt:229-236`).
  - `suspend fun confirmarTrasSubida(id: String, ahora: String): Int` — igual sin la retención por foto (el worker acaba de subir las fotos que viajaron).
  - `suspend fun resguardar(id: String, ahora: String): Int` — `UPDATE Visit SET GUARDADO_EN_MICROSIP = 1, RESGUARDADA_EN = :ahora WHERE ID = :id AND CONFIRMADA_EN IS NULL`.
  - `suspend fun devolverAPendiente(ids: List<String>): Int` — `UPDATE Visit SET GUARDADO_EN_MICROSIP = 0, CONFIRMADA_EN = NULL, RESGUARDADA_EN = NULL WHERE ID IN (:ids)`.
  - `suspend fun pendientesOrdenadas(): List<String>` — `SELECT ID FROM Visit WHERE GUARDADO_EN_MICROSIP = 0 ORDER BY FECHA ASC, ID ASC`.
  - `suspend fun entregadasDesde(desde: String): List<String>` — `SELECT ID FROM Visit WHERE GUARDADO_EN_MICROSIP = 1 AND RESGUARDADA_EN IS NULL AND (CONFIRMADA_EN IS NULL OR CONFIRMADA_EN >= :desde) ORDER BY FECHA ASC, ID ASC`.
  - `suspend fun findVisitById(id: String): VisitEntity?` (ya existe, `:76`).

- [ ] **Step 1: Prueba de migración que falla**

`Migration34to35Test.kt` (patrón de `Migration32to33Test`: `RobolectricTestBase`, `MigrationTestHelper`):
```kotlin
class Migration34to35Test : RobolectricTestBase() {
    @get:Rule val helper = MigrationTestHelper(
        InstrumentationRegistry.getInstrumentation(), AppDatabase::class.java
    )

    @Test fun `v35 agrega CONFIRMADA_EN y RESGUARDADA_EN nulas sin reescribir filas`() {
        helper.createDatabase(DB, 34).use { db ->
            db.execSQL(
                "INSERT INTO Visit (ID, CLIENTE_ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG, " +
                    "TIPO_VISITA, ZONA_CLIENTE_ID, IMPTE_DOCTO_CC_ID, GUARDADO_EN_MICROSIP) " +
                    "VALUES ('v-1', 1, 'Rosa', 2, '2026-10-16T10:00:00Z', 0, 0.0, 0.0, 'No se encontraba', 3, 0, 1)"
            )
        }
        helper.runMigrationsAndValidate(DB, 35, true, MIGRATION_34_35).use { db ->
            db.query("SELECT GUARDADO_EN_MICROSIP, CONFIRMADA_EN, RESGUARDADA_EN FROM Visit WHERE ID = 'v-1'").use { c ->
                assertTrue(c.moveToFirst())
                assertEquals(1, c.getInt(0))
                assertTrue("la fila vieja queda sin confirmar (spec §4.2-7)", c.isNull(1))
                assertTrue(c.isNull(2))
            }
        }
    }

    private companion object { const val DB = "migracion-34-35" }
}
```
Run: `./gradlew --no-parallel :core:database:testDebugUnitTest --tests '*Migration34to35Test*'` → FAIL (no compila).

- [ ] **Step 2: Entidad, migración, base**

`VisitEntity.kt`, tras `CITA_HORA`:
```kotlin
    /** Instante (wire format) en que el servidor CONFIRMÓ esta visita por su UUID
     *  (2xx con eco del id, GET de existencia completa o by-ids). Nulo = no probada.
     *  Spec visitas §4.2-1/-7. Lo escriben solo VisitDao.confirmar/confirmarTrasSubida. */
    @ColumnInfo(name = "CONFIRMADA_EN") val CONFIRMADA_EN: String? = null,
    /** Instante en que el servidor la RESGUARDÓ en la bandeja (X-Intent-Captured).
     *  Está en la oficina, no en MSP_VISITAS. Lo escribe solo VisitDao.resguardar. */
    @ColumnInfo(name = "RESGUARDADA_EN") val RESGUARDADA_EN: String? = null,
```
`Migration34to35.kt`:
```kotlin
package com.example.msp_app.core.database.migrations

import androidx.room.migration.Migration
import androidx.sqlite.db.SupportSQLiteDatabase

/** Spec visitas §4.2-7: dos columnas nulas; ninguna fila se reescribe. */
val MIGRATION_34_35 = object : Migration(34, 35) {
    override fun migrate(db: SupportSQLiteDatabase) {
        db.execSQL("ALTER TABLE `Visit` ADD COLUMN `CONFIRMADA_EN` TEXT")
        db.execSQL("ALTER TABLE `Visit` ADD COLUMN `RESGUARDADA_EN` TEXT")
    }
}
```
`AppDatabase.kt`: `version = 35,`, import `com.example.msp_app.core.database.migrations.MIGRATION_34_35`, y `MIGRATION_34_35,` dentro de `.addMigrations(`. Nada más (la guardia lo verifica).

- [ ] **Step 3: Exportar el esquema y verde**

Run: `./gradlew --no-parallel :core:database:kspDebugKotlin && ./gradlew --no-parallel :core:database:testDebugUnitTest --tests '*Migration34to35Test*' --rerun-tasks`
Expected: aparece `schemas/.../35.json`; PASS. Correr también `SchemaIntegrityTest` y `MigracionDesde27y28HastaV34Test` (de la rama de diagnóstico): si exigen "hasta la última versión", extenderlos a 35 en este mismo paso.

- [ ] **Step 4: Prueba de DAO que falla**

`VisitDaoEntregaTest.kt` (`RoomTestBase`):
```kotlin
class VisitDaoEntregaTest : RoomTestBase() {
    private val dao get() = db.visitDao()

    @Test fun `confirmar estampa CONFIRMADA_EN y limpia RESGUARDADA_EN`() = runTest {
        dao.insertVisit(visita("a"))
        assertEquals(1, dao.confirmar(listOf("a"), "2026-10-16T18:00:00Z"))
        val v = dao.findVisitById("a")!!
        assertEquals(1, v.GUARDADO_EN_MICROSIP); assertEquals("2026-10-16T18:00:00Z", v.CONFIRMADA_EN); assertNull(v.RESGUARDADA_EN)
    }

    @Test fun `confirmar retiene la visita con foto sin subir`() = runTest {
        dao.insertVisit(visita("a"))
        db.visitImageDao().insertAll(listOf(imagen(visitaId = "a", subidaEn = null)))
        assertEquals(0, dao.confirmar(listOf("a"), "2026-10-16T18:00:00Z"))
        assertNull(dao.findVisitById("a")!!.CONFIRMADA_EN)
    }

    @Test fun `resguardar no pisa una confirmada`() = runTest {
        dao.insertVisit(visita("a")); dao.confirmar(listOf("a"), "t1")
        assertEquals(0, dao.resguardar("a", "t2"))
        assertEquals("t1", dao.findVisitById("a")!!.CONFIRMADA_EN)
    }

    @Test fun `devolverAPendiente limpia las dos marcas`() = runTest {
        dao.insertVisit(visita("a")); dao.resguardar("a", "t1")
        dao.devolverAPendiente(listOf("a"))
        val v = dao.findVisitById("a")!!
        assertEquals(0, v.GUARDADO_EN_MICROSIP); assertNull(v.CONFIRMADA_EN); assertNull(v.RESGUARDADA_EN)
    }

    @Test fun `pendientesOrdenadas devuelve todas por FECHA e ID`() = runTest {
        (1..60).forEach { i -> dao.insertVisit(visita(id = "v%02d".format(61 - i), fecha = "2026-10-%02dT10:00:00Z".format(1 + i % 28))) }
        val ids = dao.pendientesOrdenadas()
        assertEquals(60, ids.size)
        val fechas = ids.map { dao.findVisitById(it)!!.FECHA }
        assertEquals(fechas.sorted(), fechas)
    }
}
```
con los constructores de prueba `visita(id, fecha = "2026-10-16T10:00:00Z", guardado = 0)` y `imagen(visitaId, subidaEn)` en el mismo archivo (todos los campos obligatorios de `VisitEntity.kt:40-99` y `VisitImageEntity.kt`).
Run → FAIL (métodos inexistentes).

- [ ] **Step 5: Implementar los métodos del DAO** con las consultas exactas de **Interfaces**. Run → PASS.

- [ ] **Step 6: Prueba de arquitectura: solo dos escritores no nulos (I3)**

`app/src/test/java/com/example/msp_app/core/visitas/envio/EscritoresDeEntregaTest.kt` usando `com.example.msp_app.navigation.EscanerDeFuentes` (ya existe; ver `EnvioDiagnosticoAislamientoTest.kt` de la rama de diagnóstico):
```kotlin
class EscritoresDeEntregaTest {
    private val escritura = Regex("""(CONFIRMADA_EN|RESGUARDADA_EN)\s*=\s*:""")

    @Test fun `I3 solo VisitDao escribe CONFIRMADA_EN y RESGUARDADA_EN`() {
        val usos = EscanerDeFuentes.archivosQueContienen(escritura)
        assertEquals(
            setOf("core/database/src/main/kotlin/com/example/msp_app/core/database/dao/visit/VisitDao.kt"),
            usos.map { it.rutaRelativa }.toSet()
        )
    }

    @Test fun `control positivo el escaner ve los usos del DAO`() {
        assertTrue(EscanerDeFuentes.archivosQueContienen(escritura).isNotEmpty())
    }
}
```
(adaptar `archivosQueContienen`/`rutaRelativa` a la API real de `EscanerDeFuentes`; si no expone una búsqueda por regex, usar la misma forma que `EnvioDiagnosticoAislamientoTest`). Además, en `VisitDaoEntregaTest`, `assertEquals(3, Regex("""CONFIRMADA_EN = :ahora|RESGUARDADA_EN = :ahora""").findAll(fuenteDe(VisitDao)).count())` no hace falta: la prueba de arquitectura cubre archivos; las funciones son las tres de **Interfaces**.

Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*EscritoresDeEntregaTest*' --rerun-tasks` → PASS.

- [ ] **Step 7: Commit**

```bash
git add core/database app/src/test/java/com/example/msp_app/core/visitas/envio/EscritoresDeEntregaTest.kt
git commit -m "feat(database): Room v35, Visit con CONFIRMADA_EN y RESGUARDADA_EN y sus únicos escritores"
```

---

### Task 9: Existencia completa y decisión de envío como funciones puras + cliente `GET /v2/visitas/{id}`

**Files:**
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/visitas/envio/ExistenciaCompleta.kt`
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/visitas/envio/DecisionDeEnvio.kt`
- Create: `core/common/src/test/kotlin/com/example/msp_app/core/common/visitas/envio/ExistenciaCompletaTest.kt`
- Create: `core/common/src/test/kotlin/com/example/msp_app/core/common/visitas/envio/DecisionDeEnvioTest.kt`
- Modify: `app/src/main/java/com/example/msp_app/data/api/services/visits/V2VisitsApi.kt` (endpoint y DTO)

**Interfaces:**
- Consumes: `classifyUpload(code, reachedMspApi, captureConfirmed): UploadDecision` (`core/upload/.../UploadDecision.kt:65-78`) — se **llama**, no se copia. `:core:common` necesita `implementation(project(":core:upload"))` en `core/common/build.gradle.kts` si no la tiene (verificar; si agregarla crea un ciclo, poner `DecisionDeEnvio.kt` en `app/src/main/java/com/example/msp_app/core/visitas/envio/` y su prueba en `app/src/test/...`).
- Produces:
```kotlin
enum class Existencia { COMPLETA, INCOMPLETA, NO_EXISTE, INDETERMINADA }
data class VisitaEnServidor(val id: String?, val imagenIds: List<String>?)
fun existenciaCompleta(enviada: String, respuesta: VisitaEnServidor?, fotosPendientes: Set<String>, codigoGet: Int?): Existencia
enum class SalidaDeEnvio { CONFIRMADA, RESGUARDADA, REINTENTA }
fun salidaTras2xx(enviada: String, idEnCuerpo: String?): SalidaDeEnvio
fun salidaTrasError(existencia: Existencia, codigo: Int, captureConfirmed: Boolean): SalidaDeEnvio
```
- Produces (`V2VisitsApi`): `@GET("v2/visitas/{id}") suspend fun obtenerVisita(@Path("id") id: String): VisitaDetalleDTO` con `data class VisitaDetalleDTO(val id: String?, ..., val imagen_ids: List<String>?)` (todos nulables).

- [ ] **Step 1: Pruebas que fallan**

`ExistenciaCompletaTest.kt`:
```kotlin
class ExistenciaCompletaTest {
    private val id = "8f1c2b8e-1d3a-4e4b-9c55-0a1b2c3d4e5f"

    @Test fun `200 con el mismo id y sin fotos pendientes es completa`() =
        assertEquals(Existencia.COMPLETA, existenciaCompleta(id, VisitaEnServidor(id, emptyList()), emptySet(), 200))
    @Test fun `404 no existe`() =
        assertEquals(Existencia.NO_EXISTE, existenciaCompleta(id, null, emptySet(), 404))
    @Test fun `500 o red es indeterminada`() {
        assertEquals(Existencia.INDETERMINADA, existenciaCompleta(id, null, emptySet(), 500))
        assertEquals(Existencia.INDETERMINADA, existenciaCompleta(id, null, emptySet(), null))
    }
    @Test fun `200 con otro id es indeterminada`() =
        assertEquals(Existencia.INDETERMINADA, existenciaCompleta(id, VisitaEnServidor("otro", emptyList()), emptySet(), 200))
    @Test fun existenciaSinLaFotoPendienteEsIncompleta() =
        assertEquals(Existencia.INCOMPLETA, existenciaCompleta(id, VisitaEnServidor(id, listOf("f1")), setOf("f1", "f2"), 200))
    @Test fun imagenIdsAusenteConFotosPendientesEsIncompleta() =
        assertEquals(Existencia.INCOMPLETA, existenciaCompleta(id, VisitaEnServidor(id, null), setOf("f1"), 200))
    @Test fun `imagen_ids ausente sin fotos pendientes es completa`() =
        assertEquals(Existencia.COMPLETA, existenciaCompleta(id, VisitaEnServidor(id, null), emptySet(), 200))
}
```
`DecisionDeEnvioTest.kt` — las de `ENTREGA_GARANTIZADA.md` §"Pruebas que no pueden faltar" lado teléfono más las de spec §4.2-5:
```kotlin
class DecisionDeEnvioTest {
    private val id = "8f1c2b8e-1d3a-4e4b-9c55-0a1b2c3d4e5f"
    @Test fun `2xx con el mismo id confirma`() = assertEquals(SalidaDeEnvio.CONFIRMADA, salidaTras2xx(id, id))
    @Test fun v3_200_con_id_distinto_no_suelta() = assertEquals(SalidaDeEnvio.REINTENTA, salidaTras2xx(id, "otro"))
    @Test fun v3_200_sin_cuerpo_no_suelta() = assertEquals(SalidaDeEnvio.REINTENTA, salidaTras2xx(id, null))
    @Test fun `existencia completa confirma ante cualquier error`() =
        listOf(400, 409, 422, 500, 503).forEach { assertEquals(SalidaDeEnvio.CONFIRMADA, salidaTrasError(Existencia.COMPLETA, it, false)) }
    @Test fun `409 nunca suelta sin existencia completa`() = Existencia.entries.filter { it != Existencia.COMPLETA }.forEach { e ->
        listOf(true, false).forEach { cap -> assertEquals(SalidaDeEnvio.REINTENTA, salidaTrasError(e, 409, cap)) }
    }
    @Test fun `existencia sin compromiso o sin foto no suelta por existencia`() =
        assertEquals(SalidaDeEnvio.REINTENTA, salidaTrasError(Existencia.INCOMPLETA, 500, false))
    @Test fun `4xx con custodia confirmada se resguarda`() =
        assertEquals(SalidaDeEnvio.RESGUARDADA, salidaTrasError(Existencia.NO_EXISTE, 422, true))
    @Test fun `4xx sin custodia reintenta`() =
        assertEquals(SalidaDeEnvio.REINTENTA, salidaTrasError(Existencia.NO_EXISTE, 422, false))
    @Test fun `401 siempre reintenta`() = listOf(true, false).forEach {
        assertEquals(SalidaDeEnvio.REINTENTA, salidaTrasError(Existencia.NO_EXISTE, 401, it))
    }
    @Test fun `404 sin llegar al api reintenta`() =
        assertEquals(SalidaDeEnvio.REINTENTA, salidaTrasError(Existencia.INDETERMINADA, 404, false))
    @Test fun `500 sin custodia reintenta`() =
        assertEquals(SalidaDeEnvio.REINTENTA, salidaTrasError(Existencia.NO_EXISTE, 500, false))
}
```
Run: `./gradlew --no-parallel :core:common:testDebugUnitTest --tests '*visitas.envio*'` → FAIL.

- [ ] **Step 2: Implementación**

`ExistenciaCompleta.kt`:
```kotlin
package com.example.msp_app.core.common.visitas.envio

/** Spec visitas §4.2-3: "existencia COMPLETA", no existencia a secas. */
enum class Existencia { COMPLETA, INCOMPLETA, NO_EXISTE, INDETERMINADA }

/** Lo que devolvió GET /v2/visitas/{id}. `imagenIds` nulo = el servidor no lo dijo. */
data class VisitaEnServidor(val id: String?, val imagenIds: List<String>?)

fun existenciaCompleta(
    enviada: String,
    respuesta: VisitaEnServidor?,
    fotosPendientes: Set<String>,
    codigoGet: Int?,
): Existencia = when {
    codigoGet == 404 -> Existencia.NO_EXISTE
    codigoGet != 200 || respuesta == null -> Existencia.INDETERMINADA
    respuesta.id != enviada -> Existencia.INDETERMINADA
    fotosPendientes.isEmpty() -> Existencia.COMPLETA
    respuesta.imagenIds == null -> Existencia.INCOMPLETA
    respuesta.imagenIds.containsAll(fotosPendientes) -> Existencia.COMPLETA
    else -> Existencia.INCOMPLETA
}
```
`DecisionDeEnvio.kt`:
```kotlin
package com.example.msp_app.core.common.visitas.envio

import com.example.msp_app.core.upload.UploadDecision
import com.example.msp_app.core.upload.classifyUpload

/** Las tres salidas posibles del worker (spec §4.5, I1). No existe una cuarta. */
enum class SalidaDeEnvio { CONFIRMADA, RESGUARDADA, REINTENTA }

/** "HTTP 200 no es éxito": sin eco del mismo id no se suelta (spec §4.2-2). */
fun salidaTras2xx(enviada: String, idEnCuerpo: String?): SalidaDeEnvio =
    if (idEnCuerpo == enviada) SalidaDeEnvio.CONFIRMADA else SalidaDeEnvio.REINTENTA

/**
 * Error HTTP: primero la existencia completa; si no, decide la tabla compartida
 * [classifyUpload] (un solo lugar decide, spec §2.2-2). RELEASE por custodia = RESGUARDADA.
 */
fun salidaTrasError(existencia: Existencia, codigo: Int, captureConfirmed: Boolean): SalidaDeEnvio {
    if (existencia == Existencia.COMPLETA) return SalidaDeEnvio.CONFIRMADA
    return when (classifyUpload(codigo, reachedMspApi = false, captureConfirmed = captureConfirmed)) {
        UploadDecision.RELEASE -> if (codigo in 200..299) SalidaDeEnvio.REINTENTA else SalidaDeEnvio.RESGUARDADA
        UploadDecision.RETRY -> SalidaDeEnvio.REINTENTA
    }
}
```
(Un 2xx no llega a `salidaTrasError`: lo atiende `salidaTras2xx`; la rama `200..299` solo blinda el caso imposible.)

- [ ] **Step 3: Verde**

Run: `./gradlew --no-parallel :core:common:testDebugUnitTest --tests '*visitas.envio*' --rerun-tasks` → PASS; contar 18 `testcase` en el XML.

- [ ] **Step 4: Cliente GET**

En `V2VisitsApi.kt` agregar:
```kotlin
    /** Prueba de existencia completa (spec visitas §4.2-3). 404 = no existe. */
    @GET("v2/visitas/{id}")
    suspend fun obtenerVisita(@Path("id") id: String): VisitaDetalleDTO
```
y el DTO (todo nulable: Gson no aplica defaults):
```kotlin
data class VisitaDetalleDTO(
    val id: String?, val cliente_id: Int?, val cobrador_id: Int?, val cobrador: String?,
    val forma_cobro_id: Int?, val lat: Double?, val lng: Double?, val nota: String?,
    val tipo_visita: String?, val zona_cliente_id: Int?, val impte_docto_cc_id: Int?,
    val fecha: String?, val created_at: String?, val imagen_ids: List<String>?,
)
```
Actualizar los `fakeV2Api` de las pruebas (`VisitasWorkerFixtures.kt`) para implementar `obtenerVisita` (por omisión lanza `AssertionError("no se esperaba GET")`). Prueba MockWebServer en `app/src/test/.../data/api/services/visits/V2VisitsApiObtenerVisitaTest.kt`: responde `{"id":"x","imagen_ids":["a"]}` → `dto.imagen_ids == listOf("a")`; responde `{"id":"x"}` → `imagen_ids == null` (servidor viejo); afirmar `request.path == "/v2/visitas/x"`.
Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*V2VisitsApiObtenerVisitaTest*' --rerun-tasks` → PASS.

- [ ] **Step 5: Commit**

```bash
git add core/common app/src/main/java/com/example/msp_app/data/api/services/visits/V2VisitsApi.kt app/src/test
git commit -m "feat(visitas): existencia completa y decisión de envío puras sobre classifyUpload, y GET de la visita"
```

---

### Task 10: Kill-switch `config/visitas_envio`

**Files:**
- Create: `app/src/main/java/com/example/msp_app/core/visitas/envio/ModoEnvioVisitas.kt`
- Create: `app/src/main/java/com/example/msp_app/core/visitas/envio/FirestoreModoEnvioVisitas.kt`
- Create: `app/src/test/java/com/example/msp_app/core/visitas/envio/ModoEnvioVisitasTest.kt`
- Modify: `app/src/main/java/com/example/msp_app/MspApplication.kt` (una línea en `onCreate`: `FirestoreModoEnvioVisitas.instalar()`; **conflicto conocido** con `feat/diagnostico-envio`, que también toca `MspApplication.onCreate`: como esta rama sale de ella, no hay conflicto)

**Interfaces:**
- Produces:
```kotlin
enum class ModoEnvioVisitas { V3, V2_ACTUAL }
fun modoDesde(datos: Map<String, Any?>?): ModoEnvioVisitas   // pura
object FirestoreModoEnvioVisitas { fun instalar(); val actual: ModoEnvioVisitas /* @Volatile, V3 por omisión */ }
```

- [ ] **Step 1: Pruebas que fallan**

```kotlin
class ModoEnvioVisitasTest {
    @Test fun documentoAusenteEsV3() = assertEquals(ModoEnvioVisitas.V3, modoDesde(null))
    @Test fun valorDesconocidoEsV3() = assertEquals(ModoEnvioVisitas.V3, modoDesde(mapOf("modo" to "v9")))
    @Test fun `tipo equivocado es V3`() = assertEquals(ModoEnvioVisitas.V3, modoDesde(mapOf("modo" to 3L)))
    @Test fun `v2_actual explicito vuelve al envio de hoy`() =
        assertEquals(ModoEnvioVisitas.V2_ACTUAL, modoDesde(mapOf("modo" to "v2_actual")))
    @Test fun errorDeFirestoreNoCambiaElModo() {
        FirestoreModoEnvioVisitas.aplicarParaPrueba(mapOf("modo" to "v2_actual"))
        FirestoreModoEnvioVisitas.alFallarParaPrueba(RuntimeException("sin red"))
        assertEquals(ModoEnvioVisitas.V2_ACTUAL, FirestoreModoEnvioVisitas.actual)
        FirestoreModoEnvioVisitas.aplicarParaPrueba(null)
    }
}
```

- [ ] **Step 2: Implementación**

`ModoEnvioVisitas.kt`:
```kotlin
package com.example.msp_app.core.visitas.envio

/**
 * Kill-switch del envío de visitas (spec visitas §11 paso 2). Documento
 * `config/visitas_envio`, campo `modo`. NUNCA `config/api_settings`.
 * V3 = envío blindado (por omisión: el viejo es el que pierde visitas).
 * V2_ACTUAL = el envío de hoy, conservado en el APK solo durante P1.
 */
enum class ModoEnvioVisitas { V3, V2_ACTUAL }

fun modoDesde(datos: Map<String, Any?>?): ModoEnvioVisitas =
    if ((datos?.get("modo") as? String) == "v2_actual") ModoEnvioVisitas.V2_ACTUAL else ModoEnvioVisitas.V3
```
`FirestoreModoEnvioVisitas.kt` (patrón de `RemoteDbDebugger.kt:100-133` y `FirestoreMinVersionConfigSource`):
```kotlin
package com.example.msp_app.core.visitas.envio

import android.util.Log
import androidx.annotation.VisibleForTesting
import com.example.msp_app.core.utils.Constants
import com.google.firebase.firestore.FirebaseFirestore

object FirestoreModoEnvioVisitas {
    private const val TAG = "ModoEnvioVisitas"
    private const val DOCUMENTO = "visitas_envio"
    @Volatile var actual: ModoEnvioVisitas = ModoEnvioVisitas.V3; private set
    @Volatile private var instalado = false

    fun instalar() {
        if (instalado) return
        instalado = true
        try {
            FirebaseFirestore.getInstance().collection(Constants.COLLECTION_CONFIG).document(DOCUMENTO)
                .addSnapshotListener { snap, err ->
                    if (err != null) { alFallar(err); return@addSnapshotListener }
                    aplicar(snap?.data)
                }
        } catch (e: Exception) { alFallar(e) }
    }

    private fun aplicar(datos: Map<String, Any?>?) {
        val nuevo = modoDesde(datos)
        if (nuevo != actual) Log.w(TAG, "modo de envío de visitas: $actual -> $nuevo")
        actual = nuevo
    }

    /** Un error de Firestore NO cambia el modo vigente. */
    private fun alFallar(e: Throwable) { Log.w(TAG, "config/visitas_envio ilegible; se conserva $actual", e) }

    @VisibleForTesting fun aplicarParaPrueba(datos: Map<String, Any?>?) = aplicar(datos)
    @VisibleForTesting fun alFallarParaPrueba(e: Throwable) = alFallar(e)
}
```

- [ ] **Step 3: Verde y commit**

Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*ModoEnvioVisitasTest*' --rerun-tasks` → PASS.
```bash
git add app/src/main/java/com/example/msp_app/core/visitas/envio app/src/test/java/com/example/msp_app/core/visitas/envio app/src/main/java/com/example/msp_app/MspApplication.kt
git commit -m "feat(visitas): kill-switch remoto config/visitas_envio, v3 por omisión"
```
**Dependiente del dueño (antes del canario, Tarea 22):** crear en Firestore de producción el documento `config/visitas_envio` con `modo: "v3"` (o dejarlo ausente, que equivale). Para revertir: `modo: "v2_actual"`.

---

### Task 11: Worker de visitas v3 (cierra H1, H1b)

**Files:**
- Modify: `app/src/main/java/com/example/msp_app/workers/PendingVisitsWorker.kt` (sobre la versión de `feat/diagnostico-envio`, que ya anota `EnvioDiagnostico.sink` en cada salida: **conservar esas llamadas** en el camino v3)
- Create: `app/src/test/java/com/example/msp_app/workers/PendingVisitsWorkerV3Test.kt`
- Modify: `app/src/test/java/com/example/msp_app/workers/PendingVisitsWorkerV2Test.kt` (renombrar la clase a `PendingVisitsWorkerModoV2ActualTest` y fijar `modo = { ModoEnvioVisitas.V2_ACTUAL }` en su builder: sus pruebas que fijan el defecto quedan como pruebas del modo de reversa, con un KDoc que lo dice)

**Interfaces:**
- Consumes: Tareas 8, 9, 10 (`VisitDao.confirmarTrasSubida`, `resguardar`, `findVisitById`; `existenciaCompleta`, `salidaTras2xx`, `salidaTrasError`; `FirestoreModoEnvioVisitas.actual`); `HEADER_INTENT_CAPTURED` de `:core:upload`.
- Produces: constructor del worker con dos costuras nuevas al final: `@VisibleForTesting internal val modo: () -> ModoEnvioVisitas = { FirestoreModoEnvioVisitas.actual }` y `@VisibleForTesting internal val visitDao: VisitDao = AppDatabase.getInstance(appContext).visitDao()`. En modo V3: `maxAttempts` se ignora (sin tope). Logs: `RECONCILED_VIA_GET`, `CONFIRMADA`, `RESGUARDADA`, `REINTENTA <motivo>`.

- [ ] **Step 1: Pruebas que fallan**

`PendingVisitsWorkerV3Test.kt` (helpers de `VisitasWorkerFixtures.kt`; `buildAndRunWorker` gana los parámetros `modo` y `getVisita: suspend (String) -> VisitaDetalleDTO`):
```kotlin
// En VisitasWorkerFixtures.kt:
internal fun httpErrorConCabecera(code: Int, nombre: String, valor: String): HttpException {
    val crudo = okhttp3.Response.Builder()
        .code(code).message("x").protocol(okhttp3.Protocol.HTTP_1_1)
        .request(okhttp3.Request.Builder().url("http://x/v2/visitas").build())
        .header(nombre, valor)
        .build()
    return HttpException(Response.error<VisitaDTO>("{}".toResponseBody("application/json".toMediaType()), crudo))
}

class PendingVisitsWorkerV3Test : RoomTestBase() {
    private fun conCabecera(code: Int) =
        httpErrorConCabecera(code, HEADER_INTENT_CAPTURED, "c0ffee00-0000-4000-8000-000000000000")

    @Test fun `2xx con el mismo id confirma dentro de NonCancellable`() = runTest {
        seed(pendingVisit())
        val r = buildAndRunWorker(api = fakeV2Api { _, b -> visitaDto(id = b.id) }, modo = V3)
        assertEquals(Result.success(), r)
        assertNotNull(db.visitDao().findVisitById("visita-001")!!.CONFIRMADA_EN)
    }

    @Test fun v3_200_con_id_distinto_no_suelta() = runTest {
        seed(pendingVisit())
        assertEquals(Result.retry(), buildAndRunWorker(api = fakeV2Api { _, _ -> visitaDto(id = "otra") }, modo = V3,
            getVisita = { throw httpError(404) }))
        assertEquals(0, guardadoFlag("visita-001"))
    }

    @Test fun `5xx sostenido 12 h no suelta`() = runTest {
        seed(pendingVisit())
        // 12 h con el backoff por omisión ≈ intento 12; se prueba hasta el 30 sin tope.
        (0..30).forEach { intento ->
            val r = buildAndRunWorker(api = fakeV2Api { _, _ -> throw httpError(503) }, modo = V3,
                runAttemptCount = intento, getVisita = { throw httpError(503) })
            assertEquals("intento $intento", Result.retry(), r)
        }
        assertEquals(0, guardadoFlag("visita-001"))
    }

    @Test fun `500 seguido de GET 200 completo suelta una vez con RECONCILED_VIA_GET`() = runTest {
        seed(pendingVisit())
        var gets = 0
        val r = buildAndRunWorker(api = fakeV2Api { _, _ -> throw httpError(500) }, modo = V3,
            getVisita = { gets++; visitaDetalle(id = it, imagenIds = emptyList()) })
        assertEquals(Result.success(), r); assertEquals(1, gets)
        assertNotNull(db.visitDao().findVisitById("visita-001")!!.CONFIRMADA_EN)
    }

    @Test fun `409 nunca suelta sin existencia completa`() = runTest {
        seed(pendingVisit())
        assertEquals(Result.retry(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw conCabecera(409) }, modo = V3,
            getVisita = { throw httpError(404) }))
        assertEquals(0, guardadoFlag("visita-001"))
    }

    @Test fun `422 con X-Intent-Captured se resguarda y no se confirma`() = runTest {
        seed(pendingVisit())
        assertEquals(Result.success(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw conCabecera(422) }, modo = V3,
            getVisita = { throw httpError(404) }))
        val v = db.visitDao().findVisitById("visita-001")!!
        assertEquals(1, v.GUARDADO_EN_MICROSIP); assertNull(v.CONFIRMADA_EN); assertNotNull(v.RESGUARDADA_EN)
    }

    @Test fun `422 sin cabecera reintenta (el pool trabado pierde la captura)`() = runTest {
        seed(pendingVisit())
        assertEquals(Result.retry(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw httpError(422) }, modo = V3,
            getVisita = { throw httpError(404) }))
        assertEquals(0, guardadoFlag("visita-001"))
    }

    @Test fun `existencia sin la foto pendiente no suelta`() = runTest {
        seed(pendingVisit()); sembrarImagen(visitaId = "visita-001", imagenId = "f1")
        assertEquals(Result.retry(), buildAndRunWorker(api = fakeV2Api(multipart = { _, _, _ -> throw httpError(500) }), modo = V3,
            getVisita = { visitaDetalle(id = it, imagenIds = emptyList()) }))
        assertEquals(0, guardadoFlag("visita-001"))
    }

    @Test fun `IOException nunca suelta`() = runTest {
        seed(pendingVisit())
        assertEquals(Result.retry(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw java.io.IOException("x") }, modo = V3))
        assertEquals(0, guardadoFlag("visita-001"))
    }

    @Test fun `ya confirmada no se vuelve a mandar`() = runTest {
        seed(pendingVisit()); db.visitDao().confirmar(listOf("visita-001"), "t")
        assertEquals(Result.success(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw AssertionError("no debe mandar") }, modo = V3))
    }

    @Test fun `ya resguardada no se vuelve a mandar`() = runTest {
        seed(pendingVisit()); db.visitDao().resguardar("visita-001", "t")
        assertEquals(Result.success(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw AssertionError("no debe mandar") }, modo = V3))
    }

    @Test fun `GET de existencia que lanza cualquier cosa es indeterminado`() = runTest {
        seed(pendingVisit())
        assertEquals(Result.retry(), buildAndRunWorker(api = fakeV2Api { _, _ -> throw httpError(500) }, modo = V3,
            getVisita = { throw IllegalStateException("json roto") }))
    }
}
```
(`visitaDto(id)` y `visitaDetalle(id, imagenIds)` también van en `VisitasWorkerFixtures.kt`: construyen `VisitaDTO`/`VisitaDetalleDTO` con el id dado y el resto de campos de `pendingVisit()`. `V3` es `ModoEnvioVisitas.V3` importado.)

Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*PendingVisitsWorkerV3Test*'` → FAIL (no compila).

- [ ] **Step 2: Implementar el camino v3**

En `PendingVisitsWorker.kt`:
1. `doWork`: tras cargar la visita, `if (modo() == ModoEnvioVisitas.V2_ACTUAL || !useV2) return if (useV2) uploadV2(visit) else uploadLegacy(visit)` (camino de hoy, intacto). Si no, `return uploadV3(visit)`.
2. Nuevo:
```kotlin
    private suspend fun uploadV3(visit: VisitEntity): Result {
        if (visit.CONFIRMADA_EN != null || visit.RESGUARDADA_EN != null) {
            Log.i(TAG, "Visita ${visit.ID} ya entregada o resguardada; no se vuelve a mandar")
            return Result.success()
        }
        val comprobantes = comprobantesDe(visit) ?: return Result.retry().also {
            Log.w(TAG, "Visita ${visit.ID}: comprobantes ilegibles; REINTENTA")
        }
        return try {
            val respuesta = enviar(visit, comprobantes)
            when (salidaTras2xx(visit.ID, respuesta.id)) {
                SalidaDeEnvio.CONFIRMADA -> confirmar(visit, comprobantes, "CONFIRMADA")
                else -> verificarYDecidir(visit, codigo = 200, captureConfirmed = false)
            }
        } catch (e: HttpException) {
            val captured = !e.response()?.headers()?.get(HEADER_INTENT_CAPTURED).isNullOrBlank()
            verificarYDecidir(visit, e.code(), captured)
        } catch (e: CancellationException) {
            throw e
        } catch (e: IOException) {
            Log.w(TAG, "Visita ${visit.ID}: red; REINTENTA", e); Result.retry()
        } catch (e: Exception) {
            Log.e(TAG, "Visita ${visit.ID}: inesperado; REINTENTA", e); Result.retry()
        }
    }

    private suspend fun verificarYDecidir(visit: VisitEntity, codigo: Int, captureConfirmed: Boolean): Result {
        val pendientes = try { imagenes.getPendientesDe(visit.ID).map { it.ID }.toSet() } catch (e: Exception) {
            return Result.retry().also { Log.w(TAG, "Visita ${visit.ID}: fotos ilegibles; REINTENTA", e) }
        }
        val existencia = consultarExistencia(visit.ID, pendientes)
        return when (salidaTrasError(existencia, codigo, captureConfirmed)) {
            SalidaDeEnvio.CONFIRMADA -> withContext(NonCancellable) {
                visitDao.confirmar(listOf(visit.ID), AppTime.toWireFormat(clock.now()))
                Log.i(TAG, "RECONCILED_VIA_GET: visita ${visit.ID} (HTTP original $codigo)"); Result.success()
            }
            SalidaDeEnvio.RESGUARDADA -> withContext(NonCancellable) {
                visitDao.resguardar(visit.ID, AppTime.toWireFormat(clock.now()))
                Log.w(TAG, "RESGUARDADA: visita ${visit.ID} (HTTP $codigo, X-Intent-Captured)"); Result.success()
            }
            SalidaDeEnvio.REINTENTA -> Result.retry().also {
                Log.w(TAG, "REINTENTA: visita ${visit.ID} HTTP $codigo existencia=$existencia custodia=$captureConfirmed")
            }
        }
    }

    private suspend fun consultarExistencia(id: String, pendientes: Set<String>): Existencia = try {
        val dto = v2Api.obtenerVisita(id)
        existenciaCompleta(id, VisitaEnServidor(dto.id, dto.imagen_ids), pendientes, 200)
    } catch (e: HttpException) {
        existenciaCompleta(id, null, pendientes, e.code())
    } catch (e: CancellationException) { throw e } catch (_: Exception) {
        Existencia.INDETERMINADA
    }

    private suspend fun confirmar(visit: VisitEntity, comprobantes: PartesDeComprobantesDeVisita, log: String): Result =
        withContext(NonCancellable) {
            markUploaded(comprobantes)
            visitDao.confirmarTrasSubida(visit.ID, AppTime.toWireFormat(clock.now()))
            Log.i(TAG, "$log: visita ${visit.ID}"); Result.success()
        }
```
3. Mantener las anotaciones de diagnóstico de la rama base (`diagnostico.anotar…`) en cada `return` del camino v3, igual que en `uploadV2`, dentro de su `try/catch` que las traga.

- [ ] **Step 3: Verde**

Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*PendingVisitsWorker*' --rerun-tasks`
Expected: PASS de V3 (12) y del modo de reversa. Contar en el XML.

- [ ] **Step 4: Rojo al revertir (cada fila)**

Para cada uno de: (a) quitar la comparación de id en `salidaTras2xx`, (b) quitar `if (existencia == COMPLETA)`, (c) cambiar `RESGUARDADA` por `CONFIRMADA` en `salidaTrasError`, (d) quitar el `return` de ya confirmada: revertir, correr `PendingVisitsWorkerV3Test` + `DecisionDeEnvioTest`, **ver rojo**, restaurar, `git diff --stat` vacío respecto al commit previo del paso. Anotar los cuatro rojos en el reporte.

- [ ] **Step 5: Commit**

```bash
git add app/src/main/java/com/example/msp_app/workers/PendingVisitsWorker.kt app/src/test/java/com/example/msp_app/workers
git commit -m "fix(visitas): el worker v3 solo suelta con existencia completa o custodia, sin tope de reintentos (H1, H1b). Cierra E-VIS-001."
```

---

### Task 12: Reencolado ordenado sin tope + poda segura (H2, H3)

**Files:**
- Modify: `app/src/main/java/com/example/msp_app/workers/VisitsReconcileWorker.kt`
- Modify: `app/src/main/java/com/example/msp_app/core/sync/pendingwork/data/synchronizers/VisitsPendingSynchronizer.kt:23` (quitar `take(MAX_ITEMS_PER_SYNC)` y usar `pendientesOrdenadas()`; `SyncAllPendingWorkUseCase` **no se toca**)
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/visitas/envio/ReglaDePoda.kt`
- Modify: `core/database/.../dao/visit/VisitDao.kt:294-305` (poda nueva)
- Modify: `app/src/main/java/com/example/msp_app/data/local/datasource/visit/VisitsLocalDataSource.kt:194-211`
- Test: `app/src/test/java/com/example/msp_app/workers/VisitsReconcileWorkerReencoladoTest.kt`, `core/common/src/test/.../ReglaDePodaTest.kt`, `core/database/src/test/.../VisitDaoPodaTest.kt`

**Interfaces:**
- Produces: `object ReglaDePoda { const val CONFIRMADAS_DIAS = 30; const val RESGUARDADAS_DIAS = 90; fun limiteConfirmadas(ahora: Instant): String; fun limiteResguardadas(ahora: Instant): String }` (DA-4).
- Produces (`VisitDao`): `deleteUploadedVisits(confirmadasAntesDe: String, resguardadasAntesDe: String, comprobantesDesde: String)` con:
```sql
DELETE FROM Visit
WHERE GUARDADO_EN_MICROSIP = 1
  AND ( (CONFIRMADA_EN IS NOT NULL AND CONFIRMADA_EN < :confirmadasAntesDe)
     OR (RESGUARDADA_EN IS NOT NULL AND CONFIRMADA_EN IS NULL AND RESGUARDADA_EN < :resguardadasAntesDe) )
  AND ID NOT IN (SELECT VISITA_ID FROM visita_imagenes WHERE SUBIDA_EN IS NULL AND CREADA_EN >= :comprobantesDesde)
```
  y `suspend fun resguardadasPorPodar(antesDe: String): Int` (`SELECT COUNT(*) …` de la segunda rama) para reportar antes de podar. La regla vieja de "conservar 90 días tras el compromiso" (`MAX(PROMESA_FECHA, CITA_FECHA)`) **se conserva** como tercera condición `AND MAX(COALESCE(PROMESA_FECHA,''), COALESCE(CITA_FECHA,'')) < :conservarCompromisosDesde` (P2 la reemplaza con DA-4 + bajada).

- [ ] **Step 1: Pruebas que fallan**

`VisitDaoPodaTest.kt`:
```kotlin
@Test fun podaNoBorraEntregadaLegadaSinConfirmar() = runTest {
    dao.insertVisit(visita("legada", guardado = 1)) // CONFIRMADA_EN nulo: anterior a v35
    dao.deleteUploadedVisits("2099-01-01T00:00:00Z", "2099-01-01T00:00:00Z", "2099-01-01T00:00:00Z", "2099-01-01")
    assertNotNull(dao.findVisitById("legada"))
}
@Test fun `poda borra la confirmada hace mas de 30 dias`() = runTest {
    dao.insertVisit(visita("vieja")); dao.confirmar(listOf("vieja"), "2026-09-01T00:00:00Z")
    dao.deleteUploadedVisits("2026-09-16T00:00:00Z", "2026-07-18T00:00:00Z", "2026-10-01T00:00:00Z", "2026-07-18")
    assertNull(dao.findVisitById("vieja"))
}
@Test fun `poda no borra la confirmada reciente ni la pendiente`() = runTest {
    dao.insertVisit(visita("reciente")); dao.confirmar(listOf("reciente"), "2026-10-15T00:00:00Z")
    dao.insertVisit(visita("pendiente"))
    dao.deleteUploadedVisits("2026-09-16T00:00:00Z", "2026-07-18T00:00:00Z", "2026-10-01T00:00:00Z", "2026-07-18")
    assertNotNull(dao.findVisitById("reciente")); assertNotNull(dao.findVisitById("pendiente"))
}
@Test fun `resguardada se poda solo tras 90 dias`() = runTest {
    dao.insertVisit(visita("r")); dao.resguardar("r", "2026-08-01T00:00:00Z")
    dao.deleteUploadedVisits("2026-09-16T00:00:00Z", "2026-07-18T00:00:00Z", "2026-10-01T00:00:00Z", "2026-07-18")
    assertNotNull(dao.findVisitById("r"))
    assertEquals(1, dao.resguardadasPorPodar("2026-10-31T00:00:00Z"))
}
```
(el cuarto argumento es `conservarCompromisosDesde`.)
`VisitsReconcileWorkerReencoladoTest.kt`:
```kotlin
@Test fun `el reconciliador reencola las 60 pendientes en orden FECHA ID con KEEP`() = runTest {
    (1..60).forEach { seed(pendingVisit(id = "v%02d".format(it), fecha = "2026-10-%02dT10:00:00Z".format(29 - (it % 28)))) }
    val encoladas = mutableListOf<String>()
    buildReconcileWorker(trigger = {}, encolar = { encoladas += it }).doWork()
    assertEquals(db.visitDao().pendientesOrdenadas(), encoladas)
    assertEquals(60, encoladas.size)
}
@Test fun `si la lectura de pendientes falla el reconciliador sigue y devuelve success`() = runTest { /* DAO que revienta → success, trigger llamado */ }
```
Run → FAIL.

- [ ] **Step 2: Implementar**

`VisitsReconcileWorker`: costura nueva `@VisibleForTesting internal val encolar: (String) -> Unit = { enqueuePendingVisitsWorker(appContext, it) }` y `@VisibleForTesting internal val pendientes: suspend () -> List<String> = { AppDatabase.getInstance(appContext).visitDao().pendientesOrdenadas() }`. En `doWork`, **antes** del `trigger()`:
```kotlin
        try { pendientes().forEach(encolar) } catch (e: CancellationException) { throw e } catch (e: Exception) {
            Log.w(TAG, "reencolado de visitas pendientes falló; lo reintenta el siguiente periodo", e)
        }
```
(`enqueuePendingVisitsWorker` ya usa `KEEP` y nombre único por id: sin amplificación.) Disparo al abrir la app: localizar con `grep -rn "enqueueVisitsReconcileNowWorker" app/src/main` dónde se llama hoy (arranque/sesión); si no se llama al abrir la app, agregar la llamada en el mismo lugar donde `MspApplication`/`MainActivity` encola el reconciliador de cobranza al arrancar (`grep -rn "enqueueCobranzaReconcile" app/src/main`). La red recuperada ya la cubre la restricción `CONNECTED` de cada trabajo (dicho en el KDoc).

`VisitsPendingSynchronizer`: reemplazar la lectura + `take(MAX_ITEMS_PER_SYNC)` por `visitDao.pendientesOrdenadas()` completo (el KDoc cita E-APP-072).

`VisitsLocalDataSource.kt:194-200`: calcular los límites con `ReglaDePoda` y, antes de podar, `val r = visitDao.resguardadasPorPodar(limiteResguardadas)`; si `r > 0`, reportarlo por `TelemetriaDeVisitas` (Tarea 14; hasta entonces `Log.w`) **y no podar ese día** (spec §4.2-8: "se reporta antes de podar"); la poda de resguardadas ocurre en la corrida siguiente, ya reportada.

- [ ] **Step 3: Verde, rojo al revertir, commit**

Run: `./gradlew --no-parallel :core:database:testDebugUnitTest --tests '*VisitDaoPodaTest*' --rerun-tasks && ./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*VisitsReconcileWorker*' --rerun-tasks` → PASS.
Rojos: quitar `CONFIRMADA_EN IS NOT NULL AND` → `podaNoBorraEntregadaLegadaSinConfirmar` FAIL; quitar el reencolado → prueba de 60 FAIL; volver a `take(50)` → la prueba de E-APP-072 de la Tarea 2, ajustada ahora a esperar 60, FAIL.
```bash
git add app core
git commit -m "fix(visitas): reencola todas las pendientes en orden y la poda exige confirmación del servidor (H2, H3). Cierra E-APP-072."
```

---

### Task 13: Reconciliación inversa diaria (capa 5)

**Files:**
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/sync/pendingwork/domain/ports/DeliveredVisitsStore.kt`
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/sync/pendingwork/domain/usecases/ReconcileDeliveredVisitsUseCase.kt`
- Create: `core/common/src/test/kotlin/com/example/msp_app/core/common/sync/pendingwork/ReconcileDeliveredVisitsUseCaseTest.kt`
- Create: `app/src/main/java/com/example/msp_app/core/sync/pendingwork/data/visits/RoomDeliveredVisitsStore.kt`
- Create: `app/src/main/java/com/example/msp_app/workers/VisitsDailyReconcileWorker.kt`
- Modify: `app/src/main/java/com/example/msp_app/workmanager/WorkManagerUtils.kt` (encolar periódico 24 h `visits_daily_reconcile`, `KEEP`, red)
- Modify: `app/src/main/java/com/example/msp_app/core/sync/pendingwork/data/visits/RoomPendingVisitsStore.kt` (`markSynced` → `visitDao.confirmar(ids, ahora)` en lugar de `markSyncedByIds`, para que lo confirmado por el reconciliador de pendientes también estampe `CONFIRMADA_EN`)

**Interfaces:**
- Consumes: `VisitCustodyRegistry.findExisting(ids): List<String>` y `MAX_IDS_PER_REQUEST = 100` (existentes); `SyncErrorReporter`.
- Produces:
```kotlin
interface DeliveredVisitsStore {
    suspend fun entregadasDesde(desde: String): List<String>
    suspend fun confirmar(ids: List<String>, ahora: String): Int
    suspend fun devolverAPendiente(ids: List<String>): Int
}
data class ConciliacionInversa(val revisadas: Int, val faltantes: Int, val confirmadas: Int, val tandasFallidas: Int)
class ReconcileDeliveredVisitsUseCase(store, registry, errorReporter, clock: () -> Instant) { suspend fun execute(dias: Int = 30): ConciliacionInversa }
```
  Métrica (Tarea 14): `visitas.reconcilia.devuelta_a_pendiente` = `faltantes` (alerta si > 0), `visitas.conciliacion {revisadas, faltantes}`.

- [ ] **Step 1: Pruebas que fallan** (fakes de `ReconcileVisitsFakes.kt` como modelo)

```kotlin
class ReconcileDeliveredVisitsUseCaseTest {
    @Test fun inversaDevuelveAPendienteLaLegadaQueElServidorNoTiene() = runTest {
        val store = FakeDelivered(entregadas = listOf("a", "b")); val reg = FakeRegistry(existentes = setOf("a"))
        val r = uc(store, reg).execute()
        assertEquals(listOf("b"), store.devueltas); assertEquals(listOf("a"), store.confirmadas)
        assertEquals(ConciliacionInversa(revisadas = 2, faltantes = 1, confirmadas = 1, tandasFallidas = 0), r)
    }
    @Test fun tandaFallidaNoDevuelveNiConfirma() = runTest {
        val store = FakeDelivered(entregadas = (1..150).map { "v$it" }); val reg = FakeRegistry(fallarTanda = 1)
        val r = uc(store, reg).execute()
        assertTrue(store.devueltas.none { it in (101..150).map { n -> "v$n" } })
        assertEquals(1, r.tandasFallidas)
    }
    @Test fun idAjenoNoSeConfirma() = runTest {
        val store = FakeDelivered(entregadas = listOf("a")); val reg = FakeRegistry(existentes = setOf("a", "intruso"))
        uc(store, reg).execute()
        assertEquals(listOf("a"), store.confirmadas)
    }
    @Test fun `tandas de a lo mas 100`() = runTest {
        val reg = FakeRegistry(existentes = emptySet())
        uc(FakeDelivered(entregadas = (1..250).map { "v$it" }), reg).execute()
        assertEquals(listOf(100, 100, 50), reg.tamanos)
    }
    @Test fun `cancelacion se propaga`() = runTest { /* registry lanza CancellationException → execute la relanza */ }
}
```

- [ ] **Step 2: Implementar** (mismo estilo de `ReconcileVisitsUseCase`: `attempt{}` que relanza `CancellationException`; una tanda fallida cuenta y **no decide**; ids no pedidos se descartan y se reporta `visitas_reconcilia_respuesta_ajena`):
```kotlin
class ReconcileDeliveredVisitsUseCase(
    private val store: DeliveredVisitsStore,
    private val registry: VisitCustodyRegistry,
    private val errorReporter: SyncErrorReporter,
    private val clock: () -> Instant,
) {
    suspend fun execute(dias: Int = 30): ConciliacionInversa {
        val ahora = clock()
        val desde = AppTime.toWireFormat(ahora.minus(Duration.ofDays(dias.toLong())))
        val ids = store.entregadasDesde(desde).distinct()
        var faltantes = 0; var confirmadas = 0; var fallidas = 0
        ids.chunked(VisitCustodyRegistry.MAX_IDS_PER_REQUEST).forEach { tanda ->
            val existentes = try { registry.findExisting(tanda).toSet() } catch (e: CancellationException) { throw e }
                catch (e: Exception) { fallidas++; errorReporter.report("visitas_reconcilia_consulta_fallida", e); return@forEach }
            if ((existentes - tanda.toSet()).isNotEmpty()) errorReporter.report("visitas_reconcilia_respuesta_ajena", null)
            val si = tanda.filter { it in existentes }; val no = tanda.filter { it !in existentes }
            if (si.isNotEmpty()) confirmadas += store.confirmar(si, AppTime.toWireFormat(ahora))
            if (no.isNotEmpty()) faltantes += store.devolverAPendiente(no)
        }
        return ConciliacionInversa(ids.size, faltantes, confirmadas, fallidas)
    }
}
```
(adaptar la firma de `errorReporter.report` a la real de `SyncErrorReporter.kt`; `AppTime` vive en `:core:common`, verificar el paquete.) Tras devolver a pendiente, el worker diario encola cada una con `enqueuePendingVisitsWorker`. `VisitsDailyReconcileWorker`: patrón de `VisitsReconcileWorker` (`@JvmOverloads`, costuras), siempre `Result.success()`, y emite la telemetría de la Tarea 14. Encolado: periódico 24 h y, al abrir la app, `OneTimeWork` si la última corrida (SharedPreferences `visitas_conciliacion_ultima`) tiene > 24 h.

- [ ] **Step 3: Verde, rojos, commit**

Run: `./gradlew --no-parallel :core:common:testDebugUnitTest --tests '*ReconcileDeliveredVisits*' --rerun-tasks` y `:app:testDevlocalDebugUnitTest --tests '*VisitsDailyReconcile*'` → PASS. Rojo: quitar el `return@forEach` del `catch` → `tandaFallidaNoDevuelveNiConfirma` FAIL.
```bash
git add core/common app
git commit -m "feat(visitas): reconciliación inversa diaria; lo que el servidor no tiene vuelve a pendiente (capa 5)"
```

---

### Task 14: Telemetría de visitas sin datos personales (H14) + pantalla de pendientes

**Files:**
- Create: `app/src/main/java/com/example/msp_app/core/visitas/envio/TelemetriaDeVisitas.kt`
- Create: `app/src/test/java/com/example/msp_app/core/visitas/envio/TelemetriaDeVisitasTest.kt`
- Create: `app/src/main/java/com/example/msp_app/core/visitas/envio/RotuloDeEnvio.kt` (+ test)
- Modify: `app/src/main/java/com/example/msp_app/features/home/screens/Home.kt` (donde hoy se listan/cuentan pendientes, `Home.kt:166,202`, y donde la rama de diagnóstico pinta el motivo)
- Modify: `app/src/main/java/com/example/msp_app/workers/VisitsDailyReconcileWorker.kt`, `VisitsReconcileWorker.kt` (emitir)
- Modify: `core/database/.../VisitDao.kt` (`suspend fun conteosDeEnvio(desde24h: String): ConteosDeEnvio` — `SELECT SUM(GUARDADO_EN_MICROSIP = 0) AS pendientes, MIN(CASE WHEN GUARDADO_EN_MICROSIP = 0 THEN FECHA END) AS masVieja, SUM(RESGUARDADA_EN IS NOT NULL AND CONFIRMADA_EN IS NULL) AS resguardadas, SUM(CONFIRMADA_EN >= :desde24h) AS confirmadas24h FROM Visit`)

**Interfaces:**
- Hallazgo (LEÍDO, informe de exploración 2026-10-16): el puerto `core/telemetry` termina en `StubTelemetrySink` (no sale del teléfono); **solo `RemoteLogger` llega a Firestore**. La telemetría de visitas va por `Logger.get().logEvent(module = "VISITAS_ENVIO", eventType = EventType.SYNC_EVENT /*o el más cercano existente*/, eventName, data)`.
- Produces: `object TelemetriaDeVisitas { val CLAVES: Set<String>; fun estado(c: ConteosDeEnvio, ahora: Instant, version: String): Map<String, Any>; fun conciliacion(r: ConciliacionInversa): Map<String, Any>; fun emitir(evento: String, datos: Map<String, Any>) }`. `CLAVES = setOf("visitas.pendientes", "visitas.pendiente_mas_vieja_min", "visitas.resguardadas", "visitas.confirmadas_24h", "app.version", "visitas.reconcilia.devuelta_a_pendiente", "visitas.conciliacion.revisadas", "visitas.conciliacion.faltantes", "visitas.conciliacion.tandas_fallidas", "visitas.resguardadas_por_podar")`. `emitir` descarta cualquier clave fuera de `CLAVES` y traga toda excepción.
- Produces: `fun rotuloDeEnvio(v: VisitEntity): String` = `"enviada"` si `CONFIRMADA_EN != null`; `"en revisión de oficina"` si `RESGUARDADA_EN != null`; `"pendiente de envío"` si `GUARDADO_EN_MICROSIP == 0`; `"enviada (sin confirmar)"` en otro caso (legadas v34).

- [ ] **Step 1: Pruebas que fallan**

```kotlin
class TelemetriaDeVisitasTest {
    @Test fun `ningun evento emite datos personales`() {
        val d = TelemetriaDeVisitas.estado(ConteosDeEnvio(3, "2026-10-16T10:00:00Z", 1, 7), Instant.parse("2026-10-16T12:00:00Z"), "2.21.0")
        assertTrue(TelemetriaDeVisitas.CLAVES.containsAll(d.keys))
        assertEquals(120L, d["visitas.pendiente_mas_vieja_min"])
    }
    @Test fun `emitir descarta claves fuera de la lista blanca`() {
        val capturado = mutableListOf<Map<String, Any>>()
        TelemetriaDeVisitas.sinkParaPrueba = { _, m -> capturado += m }
        TelemetriaDeVisitas.emitir("visitas_estado", mapOf("visitas.pendientes" to 1, "cliente" to "Rosa"))
        assertEquals(setOf("visitas.pendientes"), capturado.single().keys)
    }
    @Test fun `si la telemetria revienta el envio sigue`() {
        TelemetriaDeVisitas.sinkParaPrueba = { _, _ -> error("boom") }
        TelemetriaDeVisitas.emitir("visitas_estado", mapOf("visitas.pendientes" to 1)) // no lanza
    }
}
class RotuloDeEnvioTest { /* cuatro casos de Interfaces */ }
```

- [ ] **Step 2: Implementar** según Interfaces; en `Home.kt`, donde se pinta cada pendiente (o el contador), usar `rotuloDeEnvio`; las resguardadas aparecen en la lista con "en revisión de oficina" (spec §4.4). Emitir `visitas_estado` en cada corrida de `VisitsReconcileWorker` y `visitas_conciliacion` en `VisitsDailyReconcileWorker`.

- [ ] **Step 3: Verde y commit**

Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*TelemetriaDeVisitas*' --tests '*RotuloDeEnvio*' --rerun-tasks` → PASS.
```bash
git add app core
git commit -m "feat(visitas): telemetría de envío con lista blanca cerrada y rótulo 'en revisión de oficina' (H14)"
```

---

### Task 15: Auto Backup sin la base (H15, E-APP-073)

**Files:**
- Modify: `app/src/main/res/xml/data_extraction_rules.xml`
- Modify: `app/src/main/res/xml/backup_rules.xml`
- Create: `app/src/test/java/com/example/msp_app/ReglasDeRespaldoTest.kt`

- [ ] **Step 1: Medición E-APP-073 en emulador (antes del cambio)**

Requiere emulador API 34 con cuenta Google de prueba (si el agente no puede arrancarlo, la medición es **del dueño** y se marca así). Comandos:
```bash
./gradlew --no-parallel :app:installDevlocalDebug
adb shell bmgr enable true && adb shell bmgr transport com.android.localtransport/.LocalTransport
# abrir la app, iniciar sesión, capturar una visita sin red (queda pendiente)
adb shell bmgr backupnow com.example.msp_app.test
adb uninstall com.example.msp_app.test && ./gradlew --no-parallel :app:installDevlocalDebug
adb shell bmgr restore com.example.msp_app.test
adb shell run-as com.example.msp_app.test ls -la databases/
```
Registrar en E-APP-073 si `msp_db` volvió con la visita (control positivo: el mismo flujo sí restaura `shared_prefs`).

- [ ] **Step 2: Prueba que falla**

```kotlin
class ReglasDeRespaldoTest {
    private fun xml(nombre: String) = File("src/main/res/xml/$nombre").readText()
    @Test fun `la base de Room no entra en respaldo en la nube ni en transferencia`() {
        val r = xml("data_extraction_rules.xml")
        assertTrue(Regex("""<cloud-backup[^>]*>[\s\S]*<exclude domain="database"\s*path="\."\s*/>[\s\S]*</cloud-backup>""").containsMatchIn(r))
        assertTrue(Regex("""<device-transfer>[\s\S]*<exclude domain="database"\s*path="\."\s*/>[\s\S]*</device-transfer>""").containsMatchIn(r))
    }
    @Test fun `API menor a 31 tambien la excluye`() =
        assertTrue(xml("backup_rules.xml").contains("""<exclude domain="database" path="."/>"""))
}
```
Run → FAIL.

- [ ] **Step 3: Cambiar los XML**

`data_extraction_rules.xml`:
```xml
<?xml version="1.0" encoding="utf-8"?>
<!-- Spec visitas §4.2-9 (H15, E-APP-073): la base de Room nunca se respalda ni se
     transfiere. Restaurarla resucitaría pendientes o la base de otro cobrador. -->
<data-extraction-rules>
    <cloud-backup>
        <exclude domain="database" path="."/>
    </cloud-backup>
    <device-transfer>
        <exclude domain="database" path="."/>
    </device-transfer>
</data-extraction-rules>
```
`backup_rules.xml`:
```xml
<?xml version="1.0" encoding="utf-8"?>
<full-backup-content>
    <exclude domain="database" path="."/>
</full-backup-content>
```

- [ ] **Step 4: Verde, repetir el Step 1 (debe NO restaurar `msp_db`), commit**

```bash
git add app/src/main/res/xml app/src/test/java/com/example/msp_app/ReglasDeRespaldoTest.kt
git commit -m "fix(app): Auto Backup excluye la base de Room (H15). Cierra E-APP-073."
```

---

### Task 16: Simulación determinista Go con fallas, historial y verificador (§8.1, §8.6)

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/sim/telefono_test.go` (modelo del teléfono)
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/sim/red_test.go` (red con RNG y fallas)
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/sim/historial_test.go` (historial JSONL + verificador I1–I3)
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/sim/sim_test.go` (rapid `StateMachineActions`, semillas fijas)
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/sim/sim_firebird_test.go` (versión lenta)
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/sim/testdata/semillas/` (semillas congeladas)

**Interfaces:**
- Consumes: `app.NewService(repo, clock, app.WithImagenes(...))`, `app.RegistrarVisitaConImagenes`, `app.ObtenerVisitaConImagenIDs`, `app.VisitasExistentesPorIDs`; el middleware real de captura no se usa: la bandeja se modela con un `fakeStore` que puede fallar a la vez que la petición (`Store.Save` fallando).
- Produces (solo pruebas): `type Telefono struct{ salida map[string]*VisitaLocal }` con `VisitaLocal{ID string; Guardado bool; ConfirmadaEn, ResguardadaEn *time.Time; Fotos map[string]bool /*subida*/}`; `func (t *Telefono) Intento(ctx, s *Servidor, r *Red, id string)` que replica la decisión Kotlin (Tarea 9) — **la tabla de decisión se escribe una vez aquí y se fija con los mismos casos de `DecisionDeEnvioTest`** (tabla `casosDecision` en Go con los 11 casos); `type Evento struct{Paso int; Tipo string /*captura|envio|respuesta|marca|reconcilia*/; VisitaID string; Detalle string}`; `func Verificar(h []Evento, servidor, bandeja map[string]bool, tel *Telefono) error` que comprueba I1, I2, I3.

- [ ] **Step 1: Escribir el verificador y su prueba con historiales hechos a mano (control positivo)**

`historial_test.go`:
```go
package sim

import (
	"errors"
	"fmt"
	"testing"
)

type Evento struct {
	Paso     int
	Tipo     string // captura | envio | respuesta | marca_confirmada | marca_resguardada | devuelta
	VisitaID string
	Detalle  string
}

// Verificar comprueba I1–I3 (spec §2.3) sobre un historial y el estado final.
// I3 se comprueba en CADA marca: una marca_confirmada exige que, en ese paso,
// la visita ya estuviera en el servidor; una marca_resguardada, en la bandeja.
func Verificar(h []Evento, enServidorEn map[string]int, enBandejaEn map[string]int, final map[string]bool /*pendiente*/) error {
	capturas := map[string]int{}
	for _, e := range h {
		switch e.Tipo {
		case "captura":
			capturas[e.VisitaID]++
			if capturas[e.VisitaID] > 1 {
				return fmt.Errorf("I2: %s capturada dos veces", e.VisitaID)
			}
		case "marca_confirmada":
			if p, ok := enServidorEn[e.VisitaID]; !ok || p > e.Paso {
				return fmt.Errorf("I3: %s confirmada en el paso %d sin estar en el servidor", e.VisitaID, e.Paso)
			}
		case "marca_resguardada":
			if p, ok := enBandejaEn[e.VisitaID]; !ok || p > e.Paso {
				return fmt.Errorf("I3: %s resguardada en el paso %d sin fila en la bandeja", e.VisitaID, e.Paso)
			}
		}
	}
	for id := range capturas {
		_, srv := enServidorEn[id]
		_, bnd := enBandejaEn[id]
		if !srv && !bnd && !final[id] {
			return fmt.Errorf("I1: %s no está en el servidor, ni en la bandeja, ni pendiente", id)
		}
	}
	return nil
}

var errEsperado = errors.New("esperado")

func TestVerificador_ControlPositivo(t *testing.T) {
	t.Parallel()
	malo := []Evento{{1, "captura", "a", ""}, {2, "marca_confirmada", "a", ""}}
	if err := Verificar(malo, map[string]int{}, map[string]int{}, map[string]bool{}); err == nil {
		t.Fatal("el verificador debe cazar una confirmación sin servidor (control positivo)")
	}
	perdida := []Evento{{1, "captura", "a", ""}}
	if err := Verificar(perdida, map[string]int{}, map[string]int{}, map[string]bool{"a": false}); err == nil {
		t.Fatal("el verificador debe cazar una visita perdida (I1)")
	}
	bueno := []Evento{{1, "captura", "a", ""}, {3, "marca_confirmada", "a", ""}}
	if err := Verificar(bueno, map[string]int{"a": 2}, map[string]int{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
}
```
Run: `go test ./internal/visitas/sim/ -run Verificador -count=1` → PASS (es la herramienta; su control positivo es la propia prueba).

- [ ] **Step 2: Red y servidor con fallas**

`red_test.go`: `type Falla int` con `PerderPeticion, PerderRespuestaTrasCommit, Duplicar, Timeout, Cuerpo200Truncado, CapturaFalla, Commit Fallido, Status413, Status422CampoDesconocido`; `type Red struct{ rng *rand.Rand; prob map[Falla]float64 }` y `func (r *Red) Enviar(ctx, srv *Servidor, req Peticion) (Respuesta, error)` que, según el RNG sembrado: no llama al servidor (`PerderPeticion` → `error io`), llama y descarta la respuesta (`PerderRespuestaTrasCommit` → `error io`), llama dos veces (`Duplicar`), o devuelve `Respuesta{Status: 200, ID: ""}` (`Cuerpo200Truncado`). `Servidor` envuelve el `*app.Service` real sobre los fakes de `app` (copiar `fakeVisitasRepo`/`fakeImagenesRepo`/`fakeStorage`/`fakeTxRunner` de `app/registrar_visita_con_imagenes_test.go:43-160` a `sim/fakes_test.go`, porque los de `app` son de paquete de prueba) más una `bandeja map[string]int` (paso en que se guardó) y un modo `capturaFalla` en el que un 4xx no deja fila **ni** cabecera. Validación: para provocar 422 se manda `TipoVisita` inválido en las capturas marcadas "rechazable" (fracción configurable).

- [ ] **Step 3: Teléfono modelo**

`telefono_test.go`: `Intento` implementa exactamente la Tarea 11 (si ya confirmada/resguardada → nada; enviar; 2xx con eco → confirmar; si no → GET de existencia completa → `salidaTrasError` con la tabla `classifyUpload` reproducida en Go **y fijada por** `TestTablaDecision_IgualQueKotlin` con los 11 casos de `DecisionDeEnvioTest` en una tabla Go). Registra eventos en el historial con el número de paso.

- [ ] **Step 4: Máquina de estados con rapid**

`sim_test.go`:
```go
func TestSim_EnvioDeVisitas(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		m := nuevoMundo(rapid.Int64().Draw(rt, "semilla"))
		rt.Repeat(map[string]func(*rapid.T){
			"capturar":   func(rt *rapid.T) { m.capturar(rapid.Bool().Draw(rt, "rechazable"), rapid.IntRange(0, 2).Draw(rt, "fotos")) },
			"intentar":   func(rt *rapid.T) { m.intentarUna(rapid.IntRange(0, 1000).Draw(rt, "cual")) },
			"reconciliar": func(rt *rapid.T) { m.reconciliacionInversa() },
			"matarProceso": func(rt *rapid.T) { m.matarEntreRespuestaYMarca() },
			"":           func(rt *rapid.T) { if err := m.verificar(); err != nil { rt.Fatal(err) } }, // tras CADA acción
		})
		m.repararRedYDrenar()
		if err := m.verificarFinal(); err != nil { rt.Fatal(err) }
	})
}
```
`verificarFinal` exige además: tras drenar con red perfecta, ninguna visita pendiente salvo las de `capturaFalla` permanente. Cada semilla que falle se guarda en `testdata/semillas/<nombre>.txt` y `TestSim_SemillasCongeladas` las re-ejecuta siempre.
Run: `go test ./internal/visitas/sim/ -count=1 -race -rapid.checks=2000` → PASS.

- [ ] **Step 5: Control positivo de la simulación (rojo con el defecto de hoy)**

Agregar una bandera de prueba `defectoH1 bool` al teléfono modelo que reproduce `VisitUploadClassifier` (5xx en el intento 10 → marca; 4xx → marca). `TestSim_ControlPositivo_DefectoH1EsCazado` corre 200 semillas con `defectoH1 = true` y `capturaFalla` > 0 y **exige** que `Verificar` devuelva error en al menos una. Sin este rojo, la simulación no prueba nada.

- [ ] **Step 6: Versión lenta contra Firebird dev**

`sim_firebird_test.go`: mismo mundo con `visitasfb.New(pool)` real dentro de `fbtestutil.WithTestTransaction`, 20 semillas fijas, `requireFBEnv`. Run: `FB_DATABASE=... go test ./internal/visitas/sim/ -run Firebird -p 1 -count=1`.

- [ ] **Step 7: Historial en disco**

Con `SIM_HISTORIAL_DIR` definido, cada corrida escribe `<semilla>.jsonl` (un `Evento` por línea) y `go run ./internal/visitas/sim/verificador` — **no**: el verificador es `TestVerificarHistorialesEnDisco`, que lee todos los `.jsonl` de `SIM_HISTORIAL_DIR` (y de la corrida Kotlin, Tarea 17) y aplica `Verificar`. Mismo formato JSON en los dos lados: `{"paso":1,"tipo":"captura","visita_id":"…","detalle":"…"}`.

- [ ] **Step 8: Commit**

```bash
git add internal/visitas/sim
git commit -m "test(visitas): simulación determinista del envío con fallas, historial y verificador de I1-I3"
```

---

### Task 17: Simulación Kotlin del worker real + mutación dirigida (PIT y gremlins)

**Files:**
- Create: `app/src/test/java/com/example/msp_app/visitas/envio/simulacion/SimulacionEnvioVisitasTest.kt`
- Create: `app/src/test/java/com/example/msp_app/visitas/envio/simulacion/MundoSimulado.kt`
- Modify: `core/common/build.gradle.kts` (plugin `pl.droidsonroids.pitest` con `targetClasses` acotado)
- Modify: `gradle/libs.versions.toml` (versión del plugin)

**Interfaces:**
- Consumes: el worker real (`PendingVisitsWorker`, modo V3) vía `TestListenableWorkerBuilder`, Room en memoria (`RoomTestBase`), un `V2VisitsApi` falso con fallas sembradas (mismo catálogo que la Tarea 16), `ReconcileDeliveredVisitsUseCase` real.
- Produces: historial JSONL en `app/build/sim-historial/<semilla>.jsonl` con el formato de la Tarea 16.

- [ ] **Step 1: Mundo simulado**

`MundoSimulado`: servidor en memoria (`Map<String, VisitaServidor>`, bandeja `Map<String, Int>`), reloj virtual, `Random(semilla)`; el API falso aplica las fallas de red (perder petición = `IOException` sin tocar el servidor; perder respuesta tras commit = inserta y lanza `IOException`; duplicar; 200 con id vacío; 4xx con/sin `X-Intent-Captured` según `capturaFalla`). `obtenerVisita` responde desde el mapa (404 si no está).

- [ ] **Step 2: Prueba**

```kotlin
class SimulacionEnvioVisitasTest : RoomTestBase() {
    @Test fun `500 semillas I1 I2 I3 tras cada paso`() = runTest {
        (1L..500L).forEach { semilla ->
            val m = MundoSimulado(semilla, db, context)
            repeat(200) { paso ->
                m.accionAleatoria(paso)      // capturar | intentar(worker real) | reconciliar | matar entre 2xx y marca
                m.verificarInvariantes(paso) // I1-I3 sobre el estado y el historial; falla con la semilla en el mensaje
            }
            m.repararRedYDrenar(); m.verificarFinal()
            m.escribirHistorial(File("build/sim-historial/$semilla.jsonl"))
            m.limpiar()
        }
    }
    @Test fun `control positivo el modo v2_actual pierde visitas en la simulacion`() = runTest {
        val perdidas = (1L..200L).count { s -> MundoSimulado(s, db, context, modo = ModoEnvioVisitas.V2_ACTUAL, capturaFalla = 0.3).correrYContarViolaciones() > 0 }
        assertTrue("la simulación debe cazar el defecto de hoy (H1)", perdidas > 0)
    }
}
```
"Matar entre 2xx y marca" se simula cancelando la corrutina del worker justo después de que el API falso responde (`Job.cancel()` desde el falso): con `NonCancellable`, la marca ocurre igual; sin él, la visita queda pendiente y el reintento la confirma por existencia (I1 se cumple en ambos casos, I3 nunca se viola).
Run: `./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*SimulacionEnvioVisitasTest*' --rerun-tasks` → PASS; luego `SIM_HISTORIAL_DIR=/Volumes/M2-1TB/Developer/msp-app-kt/app/build/sim-historial go test ./internal/visitas/sim/ -run TestVerificarHistorialesEnDisco -count=1` desde msp-api → PASS (verificador fuera de línea sobre el historial Kotlin).

- [ ] **Step 3: PIT acotado**

En `gradle/libs.versions.toml`: `pitest-android = "0.2.19"` y `[plugins] pitest-android = { id = "pl.droidsonroids.pitest", version.ref = "pitest-android" }`. En `core/common/build.gradle.kts`:
```kotlin
plugins { alias(libs.plugins.pitest.android) }
pitest {
    targetClasses.set(listOf(
        "com.example.msp_app.core.common.visitas.envio.*",
        "com.example.msp_app.core.common.sync.pendingwork.domain.usecases.ReconcileDeliveredVisitsUseCase*",
    ))
    junit5PluginVersion.set("1.2.1") // solo si el módulo usa JUnit 5; con JUnit 4 quitar esta línea
    threads.set(2); outputFormats.set(listOf("HTML", "XML")); timestampedReports.set(false)
}
```
Run: `./gradlew --no-parallel :core:common:pitestDebug`. Expected: reporte en `core/common/build/reports/pitest/`. **Todo mutante vivo** se mata con una prueba nueva o se documenta como equivalente en `docs/evidencia/app-android.md` (entrada nueva E-APP-076 "mutantes equivalentes del envío de visitas"). Si el plugin no funciona con el AGP del repo (`Mutation coverage 0` o error de configuración), **no se salta**: se registra el error como E-INF-0xx (siguiente libre) y se pregunta al dueño; mientras tanto la mutación se hace a mano con el procedimiento de "rojo al revertir" ya aplicado en la Tarea 11.

- [ ] **Step 4: Gremlins del lado Go**

Run: `cd /Volumes/M2-1TB/Developer/msp-api && make test-mutation-visitas-domain && make test-mutation-visitas-app` (Makefile:379-453, `TMPDIR` externo). Revisar mutantes vivos en `app/registrar_visita_con_imagenes.go` (limpieza de blobs) y `app/obtener_visita.go`; matarlos o documentarlos en `docs/evidencia/visitas.md`.

- [ ] **Step 5: Commit**

```bash
cd /Volumes/M2-1TB/Developer/msp-app-kt && git add app/src/test/java/com/example/msp_app/visitas core/common/build.gradle.kts gradle/libs.versions.toml && git commit -m "test(visitas): simulación del worker real con fallas sembradas y mutación dirigida"
```

---

### Task 18: Caos con Toxiproxy en dev (§8.4) — solo dev

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/caos/toxiproxy-visitas.sh`
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/caos/estado-estable-visitas.sql`

**Interfaces:**
- Consumes: API dev (`msp-api-dev`), Firebird dev (`mueblera-firebird:3050`), emulador con APK `devlocal` apuntando a `127.0.0.1:3001` por `adb reverse` (COBRANZA-SYNC.md §9; **nunca** `devserver`, que es producción).

- [ ] **Step 1: Script**

```bash
#!/usr/bin/env bash
# Caos de visitas en DEV (spec §8.4). Producción nunca.
set -euo pipefail
docker run -d --rm --name toxiproxy -p 8474:8474 -p 23001:23001 -p 23050:23050 ghcr.io/shopify/toxiproxy:2.9.0
api() { curl -sf -X "$1" "http://127.0.0.1:8474$2" -H 'Content-Type: application/json' ${3:+-d "$3"}; }
api POST /proxies '{"name":"telefono_api","listen":"0.0.0.0:23001","upstream":"host.docker.internal:3001"}'
api POST /proxies '{"name":"api_firebird","listen":"0.0.0.0:23050","upstream":"host.docker.internal:3050"}'
# El API dev debe apuntar a 127.0.0.1:23050 (FB_HOST/FB_PORT en .env dev) y recrearse: docker compose up -d --force-recreate msp-api-dev
adb reverse tcp:3001 tcp:23001
toxico() { api POST "/proxies/$1/toxics" "$2"; }
fase() { echo "== $1 ($2 s)"; sleep "$2"; for p in telefono_api api_firebird; do api GET "/proxies/$p/toxics" | jq -r '.[].name' | while read -r n; do api DELETE "/proxies/$p/toxics/$n"; done; done; }
toxico telefono_api '{"name":"lat","type":"latency","attributes":{"latency":3000,"jitter":2000}}'; fase "latencia teléfono-API" 300
toxico telefono_api '{"name":"rst","type":"reset_peer","attributes":{"timeout":500},"toxicity":0.3}'; fase "reset teléfono-API" 300
toxico telefono_api '{"name":"sl","type":"slicer","attributes":{"average_size":64,"delay":50}}'; fase "multipart en rebanadas" 300
toxico api_firebird '{"name":"to","type":"timeout","attributes":{"timeout":2000},"toxicity":0.2}'; fase "Firebird timeout (commit hecho, respuesta perdida)" 300
toxico api_firebird '{"name":"lim","type":"limit_data","attributes":{"bytes":2048}}'; fase "Firebird corta datos" 120
api POST /proxies/telefono_api '{"enabled":false}'; sleep 600; api POST /proxies/telefono_api '{"enabled":true}'
echo "fallas retiradas; esperar 30 min y correr estado-estable-visitas.sql + depurador remoto"
```
Durante las fases, el agente captura visitas en el emulador (o con un script `adb shell input` que repite el flujo de captura) — mínimo 50 visitas, 10 con foto.

- [ ] **Step 2: Hipótesis de estado estable**

Tras 30 min sin fallas: (a) en el emulador, `SELECT COUNT(*) FROM Visit WHERE GUARDADO_EN_MICROSIP = 0` = 0 (o solo las rechazadas sin custodia, explicadas); (b) `estado-estable-visitas.sql` en dev: cada id capturado (lista sacada del emulador con `SELECT ID, CONFIRMADA_EN, RESGUARDADA_EN FROM Visit`) está en `MSP_VISITAS` si tiene `CONFIRMADA_EN`, o en `MSP_FAILED_INTENTS.IDEMPOTENCY_KEY` si tiene `RESGUARDADA_EN`; (c) el pool del API no quedó envenenado (`SELECT COUNT(*) FROM MON$STATEMENTS WHERE MON$STATE = 2` = 0; si no, `docker restart msp-api-dev` y anotarlo como hallazgo). Registrar el resultado como E-VIS-018 (siguiente libre) "caos dev P1".

- [ ] **Step 3: Restaurar el entorno**

`docker stop toxiproxy`, `.env` dev de vuelta a Firebird directo, `docker compose up -d --force-recreate msp-api-dev`, `adb reverse tcp:3001 tcp:3001`. Verificar con `git diff` que `.env` no quedó cambiado en el repo.

- [ ] **Step 4: Commit**

```bash
git add scripts/caos docs/evidencia/visitas.md
git commit -m "test(visitas): caos en dev con Toxiproxy y estado estable del envío"
```

---

### Task 19: Agente adversario (§8.7)

- [ ] **Step 1:** Despachar un agente independiente (no autor, no revisor previo) con el spec, este plan y las dos ramas, y **un solo encargo**: "pierde, duplica o corrompe una visita (o su foto) entre la captura en el teléfono y `MSP_VISITAS`/`MSP_FAILED_INTENTS`. Para cada escenario: pasos exactos, qué invariante viola, y una prueba que lo demuestre."
- [ ] **Step 2:** Cada escenario se convierte en una semilla congelada (Tarea 16 `testdata/semillas/` o caso fijo en `SimulacionEnvioVisitasTest`) o en una prueba unitaria. Si alguno **pasa** (la prueba nueva queda roja contra el código), se detiene el plan, se arregla con su prueba roja al revertir, y se repite el adversario.
- [ ] **Step 3:** Commit de las pruebas nuevas: `test(visitas): escenarios del agente adversario como semillas fijas`.

---

### Task 20: Retiro de `VisitUploadClassifier` (después del paso 3 de despliegue)

**Ejecutar solo** cuando el dueño confirme que la flota corre P1 en modo v3 con métricas estables 7 días (spec §11 paso 3). Antes de eso el modo `v2_actual` es la reversa.

**Files:**
- Delete: `app/src/main/java/com/example/msp_app/features/visit/upload/domain/VisitUploadClassifier.kt`
- Delete: `app/src/test/java/com/example/msp_app/workers/PendingVisitsWorkerModoV2ActualTest.kt` (las tres pruebas que fijan el defecto H1)
- Modify: `PendingVisitsWorker.kt` (quitar `uploadV2` y el modo), `ModoEnvioVisitas.kt` (el valor `v2_actual` pasa a significar "no enviar visitas y reportar": decisión del dueño en ese momento; si no la toma, se deja solo V3 y el documento se ignora)

- [ ] **Step 1:** Borrar, compilar, correr `./gradlew --no-parallel prePushCheck` (con `rm -rf app/build` antes si hubo release). Expected: verde; `grep -rn VisitUploadClassifier` sin resultados (control positivo: el mismo grep antes del borrado da ≥ 1).
- [ ] **Step 2:** Commit `refactor(visitas): retira el clasificador que soltaba visitas sin prueba (H1)`.

---

### Task 21: Compuerta "después" de P1 (agente independiente) y suite completa

- [ ] **Step 1: Suite completa en los dos repos**

msp-api: `go clean -testcache && golangci-lint run ./... && go test -race -short ./... && make test-firebird-all && make coverage-visitas`.
msp-app-kt: `rm -rf app/build && ./gradlew --no-parallel prePushCheck --rerun-tasks` (contar clases en los XML), `bash scripts/guardia-aislamiento.sh --rango main...HEAD` (debe pasar), `bash scripts/guardia-aislamiento_test.sh`.

- [ ] **Step 2: Compuerta "después"** — agente independiente distinto del de la Tarea 2 con este encargo:
```
Verificación de alcance de P1 (envío de visitas). No confíes en reportes previos.
1. Para cada arreglo (H1, H2, H3, H7 si aplica, H10, H11, H15, existencia completa, eco del id, NonCancellable): revierte SOLO esa línea, corre la prueba que lo guarda, confirma ROJO, restaura, confirma con git diff que no quedó resto. Lista prueba y salida.
2. ENUMERA (con grep, pegando el comando y su salida, y un control positivo del grep) todos los consumidores de: Visit (tabla y entidad), getPendingVisits/pendientesOrdenadas, markSyncedByIds/confirmar, deleteUploadedVisits, GET /v2/visitas/{id}, la clave de blob de visitas, MSP_VISITAS_IMAGENES.STORAGE_KEY, el extractor de resumen. Para cada uno: ¿sigue correcto?
3. Di en voz alta lo que quedó SIN verificar (por ejemplo: conducta en fabricantes concretos, Auto Backup en un teléfono real, tiempos reales de la flota).
4. ¿La guardia de aislamiento pasa en las dos ramas? ¿Algún archivo vedado cambió?
```
Expected: sin hallazgos bloqueantes; lo no verificado queda escrito en `docs/evidencia/visitas.md` `## Compuerta después P1 (fecha)`.

- [ ] **Step 3: Commit** `docs(evidencia): compuerta después de P1 del envío de visitas`.

---

### Task 22: Preparación del canario (DEPENDE DEL DUEÑO; el agente no despliega)

- [ ] **Step 1:** Redactar en `docs/evidencia/visitas.md` `## Runbook canario P1` los pasos de spec §11 pasos 1–3 con los comandos concretos: snapshot (`make fb-snapshot NAME=antes-visitas-p1`), respaldo del binario en el servidor, `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build`, reversa (binario anterior probado en dev el mismo día), APK canario solo en el teléfono del dueño (DA-5), documento `config/visitas_envio` (Tarea 10), y la reconciliación inversa **a diario** sobre ese teléfono antes de abrir a la flota (DA-5).
- [ ] **Step 2:** Criterios de avance (spec §11): 48 h sin aumento de 5xx en `/v2/visitas`, bandeja legible, cero visitas pendientes > 30 min con red, reconciliación inversa sin faltantes. Las alertas de spec §12 que aplican a P1: 1 (`devuelta_a_pendiente` > 0), 3 (pendiente > 2 h con red, DA-18), 5 (teléfono sin reporte > 24 h).
- [ ] **Step 3:** Commit `docs(visitas): runbook del canario de P1`.

---

## Self-review (2026-10-16)

- **Cobertura del spec P1:** §4.2-1 (Tarea 11 "ya confirmada"), -2 (Tareas 9, 11), -3 (9, 11), -4 (11, sin tope), -5 (11, 20), -6 (12), -7 (8), -8 (12), -9 (15), -10 (14); §4.3-1 (6), -2 (3), -3 (4), -4 (5), -5 (solo se mide: Q1 en Tarea 2); §4.4 (14); §4.6-1 (13; la conciliación directa es P2); §7.1 (1); §7.2 E-APP-074 (2); §8.1 (16, 17), §8.2 (rojos en 3, 4, 5, 6, 11, 12, 13), §8.4 (18), §8.5 (17), §8.6 (16, 17), §8.7 (19); §9 (2, 21); §10 Q1/Q4/Q5/Q6 (2); §11 pasos 0–3 (2, 22); §12 alertas 1/3/5 (14, 22).
- **Huecos del spec encontrados:** (1) la guardia de §7.1 no permite el `import` que acompaña a `.Registrar("/v2/visitas")` — se amplía la excepción en la Tarea 3 y se reporta; (2) §4.2-5 dice "se borra `VisitUploadClassifier`" y §11 paso 2 dice "el código viejo sigue en el APK durante P1" — se resuelve conservándolo solo para el modo `v2_actual` y retirándolo en la Tarea 20; (3) el comentario de `failedintent_wiring.go:143-145` ("visitas NO tiene extractor") queda falso y la guardia no deja corregirlo — PR aparte del dueño; (4) §4.2-10 supone telemetría propia, pero solo `RemoteLogger` llega a Firestore (`StubTelemetrySink`); (5) la línea "siguiente libre" de `app-android.md` está desfasada (058 vs 067 real); (6) el cliente Android no tenía `GET /v2/visitas/{id}` (`V2VisitsApi.kt:43-50` lo descartaba a propósito).
- **Placeholders:** ninguno de los prohibidos; los lugares donde el ejecutor debe localizar un sitio de llamada (Tarea 12, disparo al abrir la app) llevan el comando `grep` exacto y el criterio.
- **Tipos:** `Existencia`, `VisitaEnServidor`, `SalidaDeEnvio`, `salidaTras2xx`, `salidaTrasError`, `existenciaCompleta`, `ModoEnvioVisitas`, `VisitDao.confirmar/confirmarTrasSubida/resguardar/devolverAPendiente/pendientesOrdenadas/entregadasDesde/conteosDeEnvio/resguardadasPorPodar`, `ReconcileDeliveredVisitsUseCase`, `ConciliacionInversa`, `TelemetriaDeVisitas.CLAVES`, `ObtenerVisitaConImagenIDs`, `VisitaDetalleDTO` (Go y Kotlin), `WithMaxBodyBytes` se usan con el mismo nombre en todas las tareas.
