# Visitas P2 — Promesas y citas como hechos de la visita: plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Que la promesa y la cita viajen con la visita al servidor (contrato de visitas 3), que su estado (vigente, cumplida, incumplida, reemplazada) **se derive y nunca se guarde** con una sola regla en Go y Kotlin amarrada por casos compartidos, que el teléfono baje la agenda de su zona por un canal con cursor par igual que pagos, y que el escritorio pinte lo que el servidor deriva.

**Architecture:** Cinco columnas nullable + `UPDATED_AT` + `CREATED_BY` en `MSP_VISITAS` (migración `000066`), filas `visitas`/`tareas` en `MSP_CFG_SYNC_EPOCH` (`000067`), relleno de `UPDATED_AT` en tandas fuera de horario. Dominio Go nuevo en `internal/visitas/domain` (fecha civil, bloques promesa/cita, comparación de reenvío, `DerivarCompromisos`); canal `/v2/visitas/sync/zona/{zona_id}` + digest/ids/by-ids con **un solo predicado** y **marca segura por tiempo**; `GET /v2/visitas/capacidades`, `GET /v2/visitas/agenda`, `POST /v2/visitas/agenda/conciliacion`; contrato `internal/visitas/visitas_contracts.go` que alimenta `ColumnaDe` del documento 1. En el teléfono: Room v36 (`visita_zona`, `visitas_sync_state`, dos columnas de relleno en `Visit`), negociación de capacidad, relleno, sync por zona que **nunca** escribe `Visit`, `Compromisos.kt` con los mismos casos, conciliación directa, y la UI de §5.7. En el escritorio: columna de compromiso en Cuentas.

**Tech Stack:** Go 1.25, Huma v2.37.3, Firebird 5, `pgregory.net/rapid`, `testing/synctest`; Kotlin, Room 2.6.1, WorkManager, Robolectric, MockWebServer, Gson; React 18 + Vite + Vitest + MSW (escritorio).

**Spec:** `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-15-visitas-compromisos-avisos-design.md` (§5 completo, §4.6-2, §7, §8, §9, §10 Q2/Q3/Q5/Q6/Q7/Q8, §11 pasos 4–8, §12, §13). Leer **completo** `docs/COBRANZA-SYNC.md` antes de las Tareas 9–11 y 15–16.

**Depende de:** `2026-10-16-visitas-p1-envio-blindado.md`. Las tareas de servidor 1–13 solo requieren que la Tarea 1 de P1 (guardia) esté fusionada. Las tareas de teléfono 14–20 requieren **P1 fusionado** (Room v35, worker v3, `ExistenciaCompleta`). La Tarea 20 (UI §5.7) requiere además que el trabajo de teléfono del documento 1 y `feat/badges-blancos-sin-al-corriente` estén fusionados (tocan `EstadoCuentaUi.kt` y `SegmentoDeCobranza.kt`).

**Coordinación con el documento 1** (`docs/superpowers/plans/2026-10-16-desempeno-pulido-2.md`, en redacción): la interfaz de la Tarea 8 es el acuerdo; el plan del documento 1 debe consumirla tal cual. Si el documento 1 ya fusionó una forma distinta de `ColumnaDe`, la Tarea 13 adapta **el lado de visitas**, nunca la regla de dinero del documento 1 (spec §5.6).

---

## Global Constraints

- Todas las de P1 (garantía nombrada, guardia, sin `--no-verify`, sin push, sin despliegue, sin atribución en commits, producción solo lectura, Gradle `--no-parallel`, `go clean -testcache`, Gson nulable, sin `@Singleton` sobre servicios con baseURL).
- **CLAUDE.md §1 estricto** para `MSP_VISITAS` y `MSP_CFG_SYNC_EPOCH`: solo `ALTER TABLE ADD` nullable, índices, `INSERT` de filas semilla con `UPDATED_AT` literal. **Prohibido** `DEFAULT`, trigger, `CHECK` de negocio, procedimiento. **El estado del compromiso no tiene columna** (spec §5.1, I9).
- Migraciones: siguiente libre medido el 2026-10-16 = **000066**. Si el documento 1 toma 000066 primero, este plan usa el siguiente libre al escribirla y renumera `000067`.
- **`UPDATED_AT` se estampa en Go dentro de la transacción**, después de una lectura trivial que fuerza su inicio (spec §5.4.4-2). Nunca se reutiliza un `now` tomado antes de abrir la transacción (`RCI:100`).
- **Cursor par** `(UPDATED_AT, ID)`: donde se escribe uno se escribe el otro; todo camino que deje `CURSOR` nulo deja `AFTER_ID` nulo; página vacía devuelve el **cursor recibido** y `after_id = null` = "sin posición nueva" (`COBRANZA-SYNC.md` §2).
- **Epoch `visitas`/`tareas`**: las seis reglas de `COBRANZA-SYNC.md:96-102`; se sube siempre que cambie el `WHERE` o la proyección, **incluido cambiar V** (DA-3 = 92 días). Nunca se suben `pagos` ni `ventas`.
- **Tres canales idénticos** (I4) y **un solo fragmento de predicado** usado por los tres y por la agenda; ningún enriquecimiento con `INNER JOIN`.
- **Ninguna ruta borra** filas de `MSP_VISITAS` (prueba de forma, spec §5.4.7).
- **Teléfono: lo bajado va a `visita_zona`, NUNCA a `Visit`** (I8, spec §5.4.8). Resync y cambio de zona no tocan `Visit`.
- **Códigos y mensajes** (spec §5.2.2): `visita_promesa_fecha_invalida`, `visita_promesa_incompleta`, `visita_promesa_monto_invalido`, `visita_promesa_venta_invalida`, `visita_cita_incompleta`, `visita_cita_invalida` (422), `visita_compromiso_distinto` (422, DA-1). Mensajes en español, minúsculas, sin punto.
- **No se rechaza** una fecha civil pasada o lejana; se cuenta en `visitas.compromiso.fecha_atipica`.
- **Fechas civiles** sin zona; la zona del negocio (`America/Mexico_City`, `firebird.BusinessTZ()`) se inyecta a la derivación; ninguna función de dominio lee el reloj.
- **Evidencia:** E-VIS-002, -006, -007, -010…-016 deben estar en MEDIDO antes de las tareas que las usan; nuevas entradas de P2 en `docs/evidencia/visitas.md` desde el siguiente libre (P1 deja `E-VIS-019` como siguiente si usó la 018 en el caos); hechos de teléfono en `app-android.md` desde el siguiente libre tras P1 (**E-APP-077** si P1 usó la 076); DA-8 (definición de "rechazado") se registra en `cobranza.md` como **E-COB-031** (siguiente libre por instrucción del dueño; el archivo llega a E-COB-018 y E-COB-019/020 ya están citados en otros documentos).
- **Room:** v35 (P1) → **v36** (este plan). La guardia permite en `AppDatabase.kt` solo `version`, `entities`, accesores DAO, imports y `addMigrations`.
- **Escritorio:** lógica en el servidor; el escritorio solo pinta (memoria `feedback_logica_siempre_en_servidor`).

## Review Focus

1. **Dos visitas aplicables a la misma venta en el mismo segundo**, una con compromiso y otra sin él: gana la de menor id lexicográfico, idéntico en Go y Kotlin. Caso compartido obligatorio en la Tarea 5 (`021-empate-mismo-segundo.json`) y prueba de paridad de orden de ids (E-VIS-016) en la Tarea 2.
2. **Promesa capturada a las 23:30 hora local** (ya es el día siguiente en UTC): el "hoy" y el día del compromiso se calculan en la zona del negocio, no en UTC. Caso `022-visita-2330-local.json` en la Tarea 5.
3. **Un reenvío del APK viejo sin los campos de compromiso** sobre una fila que ya tiene promesa: la promesa se queda (I10). Prueba `TestReenvio_BloqueAusenteNoBorra` en la Tarea 7 y caso en la simulación de la Tarea 19.
4. **Página de sync que termina exactamente en un grupo empatado de 2,000 filas con el mismo `UPDATED_AT`**: la siguiente página sigue por `ID` y no repite ni salta. `TestE2E_SyncVisitas_EmpateDe2000` en la Tarea 10; del lado teléfono `segundaCorridaRetomaDelAfterIdPersistidoConTodoElLoteEmpatado` (Tarea 16).
5. **El teléfono cambia de zona con visitas propias pendientes**: `visita_zona` de la zona vieja se borra, cursor y `AFTER_ID` nulos en la misma transacción, `Visit` intacta. `cambioDeZonaNoTocaVisitYDejaCursorYAfterIdNulos` en la Tarea 16.

---

## Mapa de archivos

**msp-api** (rama `feat/visitas-p2-compromisos` desde `main` con P1 fusionado; checkout principal):

| Archivo | Responsabilidad |
|---|---|
| `internal/visitas/domain/calendario.go` (+test) | `FechaCivil`, `HoraCivil`. |
| `internal/visitas/domain/compromiso.go` (+test) | Bloques `Promesa`, `Cita`, validación, igualdad. |
| `internal/visitas/domain/visita.go` (mod) | Visita con bloques, `UpdatedAt`, `CreatedBy`. |
| `internal/visitas/domain/reenvio.go` (+test) | Tabla 5.2.3 como función pura. |
| `internal/visitas/domain/alcance.go` (+test) | Alcance por tipo (copia fiel de desempeño). |
| `internal/visitas/domain/derivar_compromisos.go` (+test) | `DerivarCompromisos` y `ColumnaSugeridaDe`. |
| `internal/visitas/domain/casos_compromisos_test.go` | Cargador y hash de `testdata/compromisos/`. |
| `testdata/compromisos/*.json`, `VERSION` | Casos compartidos Go↔Kotlin. |
| `migrations-firebird/000066_msp_visitas_compromisos.{up,down}.sql` | Columnas + índice. |
| `migrations-firebird/000067_msp_cfg_sync_epoch_visitas_tareas.{up,down}.sql` | Filas de epoch. |
| `scripts/visitas-relleno-updated-at.sh` | Genera el relleno en tandas. |
| `internal/visitas/infra/visitasfb/repo.go` (mod) | Insert/scan con columnas nuevas; estampa en transacción; relleno CAS. |
| `internal/visitas/infra/visitasfb/predicado.go` (+test) | El único predicado de bajada. |
| `internal/visitas/infra/visitasfb/marca_segura.go` (+test) | Marca por tiempo. |
| `internal/visitas/infra/visitasfb/sync_repo.go` (+tests e2e) | Sync, digest, ids, by-ids de zona. |
| `internal/visitas/infra/visitasfb/epoch_repo.go` | Lee `MSP_CFG_SYNC_EPOCH` para `visitas`/`tareas`. |
| `internal/visitas/infra/visitasfb/agenda_repo.go` | Cuentas y pagos de la zona, **solo lectura**. |
| `internal/visitas/app/registrar_visita*.go` (mod) | Flujo de reenvío. |
| `internal/visitas/app/sync.go`, `agenda.go`, `capacidades.go` | Casos de uso. |
| `internal/visitas/infra/visitashttp/*` (mod/crear) | DTOs y rutas nuevas. |
| `internal/visitas/visitas_contracts.go`, `visitas_contracts_mapper.go` | Interfaz con el documento 1. |
| `internal/platform/config/config.go` (mod) | `VISITAS_COMPROMISOS_ENABLED`, `VISITAS_BAJADA_VENTANA_DIAS`. |
| `internal/visitas/sim/` (mod) | Simulación con compromisos y bajada. |

**msp-app-kt** (rama `feat/visitas-compromisos` desde `main` con P1 fusionado):

| Archivo | Responsabilidad |
|---|---|
| `core/database/.../entities/VisitEntity.kt` (mod) | `COMPROMISO_CONFIRMADO_EN`, `COMPROMISO_RESGUARDADO_EN`. |
| `core/database/.../entities/VisitaZonaEntity.kt`, `VisitasSyncStateEntity.kt` (crear) | Tablas de lectura y de cursor. |
| `core/database/.../dao/visit/VisitaZonaDao.kt` (crear) | Escritura de página + cursor en una transacción. |
| `core/database/.../migrations/Migration35to36.kt` (crear) | |
| `core/common/.../visitas/domain/Compromisos.kt` (crear) | La misma regla que Go. |
| `core/common/src/test/resources/compromisos/` | Copia byte a byte de los casos. |
| `scripts/casos-compromisos.sh` (crear) | `copiar` / `verificar`. |
| `app/.../core/visitas/capacidades/*` (crear) | Negociación de contrato. |
| `app/.../data/api/services/visits/V2VisitsApi.kt`, `VisitasZonaSyncApi.kt` | Cuerpo v3, detalle, canal de bajada. |
| `app/.../core/sync/visitas/VisitasZonaSyncManager.kt` (+test) | Sync con cursor par y epoch. |
| `app/.../core/sync/visitas/VisitasZonaReconciler.kt` (+test) | Inventario y rescate. |
| `app/.../core/visitas/agenda/AgendaLocal.kt` (+test) | Unión `Visit ∪ visita_zona` → derivación. |
| `app/.../workers/VisitsDailyReconcileWorker.kt` (mod) | Conciliación directa. |
| `feature/pagos/.../ui/*` (mod, Tarea 20) | §5.7 B, C, E, F. |

**sistema-cobro-web** (rama `feat/agenda-compromisos` desde `feat/desempeno`, worktree existente `/Volumes/M2-1TB/Developer/sistema-cobro-web-desempeno`; ese worktree es del dueño: confirmar antes de usarlo):

| Archivo | Responsabilidad |
|---|---|
| `src/modules/desempeno/infrastructure/http/dtos.ts`, `HttpDesempenoAdapter.ts` | `obtenerAgenda`. |
| `src/modules/desempeno/domain/entities/Agenda.ts` | Entidad. |
| `src/modules/desempeno/presentation/hooks/useAgenda.ts` | Hook. |
| `src/modules/desempeno/components/cuentas/TablaCuentas.tsx`, `TarjetaCuenta.tsx` | Columna "Compromiso". |

## Paralelismo

| Carril | Tareas | Notas |
|---|---|---|
| A — servidor dominio | 3, 4, 5, 6 | En serie (mismo paquete `domain`). |
| B — servidor infra | 7, 9, 10, 11, 12, 13 | Tras A. 9→10→11 en serie. 12 y 13 en paralelo entre sí solo con worktrees separados. |
| C — teléfono | 14–20 | Tras P1 fusionado y Tarea 12 de este plan (contrato HTTP estable). En serie. |
| D — escritorio | 21 | Tras Tarea 12. Paralelo con C (otro repo). |
| E — mediciones/compuertas | 1, 2, 22, 23, 24 | 1 y 2 antes de todo lo demás. |

Camino crítico: 1 → 2 (compuerta antes) → 3 → 5 → 7 → 9 → 10 → 12 → 15 → 16 → 18 → 22 (simulación) → 23 (adversario) → 24 (compuerta después).

---

### Task 1: Mediciones en dev que deciden el diseño (E-VIS-002, -013 previa, -014, -015, -016, Q-hora de `MON$TIMESTAMP`)

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/internal/visitas/infra/visitasfb/mediciones_p2_e2e_test.go`
- Modify: `/Volumes/M2-1TB/Developer/msp-api/docs/evidencia/visitas.md`

**Interfaces:**
- Produces: decisión **DATE vs `CHAR(10)`** para `PROMESA_FECHA` y `CITA_FECHA` (E-VIS-014); decisión de cómo leer `MON$TIMESTAMP` (nueva E-VIS-019, siguiente libre: tipo `TIMESTAMP WITH TIME ZONE` en Firebird 4+); orden de ids en dev (E-VIS-016 dev); `PLAN` del predicado en dev (E-VIS-015 dev); reenvío con datos distintos devuelve 201 y descarta (E-VIS-002).

- [ ] **Step 1: Escribir las mediciones**

```go
package visitasfb_test

// Mediciones de P2 (spec visitas §9.2). Todas en dev, todas con control positivo,
// todas dentro de WithTestTransaction salvo la de dos transacciones (Tarea 10).

// E-VIS-014: DATE ida y vuelta sin corrimiento con TZ del proceso en
// America/Mexico_City y en UTC, en bordes de cambio de día.
func TestMedicion_EVIS014_DateIdaYVuelta(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	for _, tz := range []string{"America/Mexico_City", "UTC"} {
		t.Run(tz, func(t *testing.T) {
			t.Setenv("TZ", tz) // NO paralelo: TZ es del proceso
			time.Local = mustLoad(t, tz)
			fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
				q := firebird.GetQuerier(ctx, pool.DB)
				_, err := q.ExecContext(ctx, `CREATE GLOBAL TEMPORARY TABLE TMP_MED_DATE (D DATE) ON COMMIT DELETE ROWS`)
				_ = err // puede existir de una corrida previa; se verifica abajo con el INSERT
				for _, s := range []string{"2026-03-08", "2026-04-05", "2026-10-25", "2026-11-01", "2026-12-31", "2027-01-01"} {
					_, err := q.ExecContext(ctx, `INSERT INTO TMP_MED_DATE (D) VALUES (CAST(? AS DATE))`, s)
					require.NoError(t, err)
					var leido any
					require.NoError(t, q.QueryRowContext(ctx, `SELECT FIRST 1 D FROM TMP_MED_DATE ORDER BY D DESC`).Scan(&leido))
					t.Logf("E-VIS-014 tz=%s escrito=%s leido=%#v", tz, s, leido)
					// Lo que se compara es lo que el repo HARÍA: formatear el valor leído como YYYY-MM-DD.
					assert.Equal(t, s, formatearComoRepo(leido), "corrimiento de día con TZ=%s", tz)
					_, _ = q.ExecContext(ctx, `DELETE FROM TMP_MED_DATE`)
				}
			})
		})
	}
}
```
(`formatearComoRepo(v any) string`: si es `time.Time`, `v.(time.Time).Format("2006-01-02")` **sin** convertir de zona; si es `string`/`[]byte`, los primeros 10 caracteres. `CREATE GLOBAL TEMPORARY TABLE` es DDL de prueba en dev; si la política del repo no lo permite en pruebas, usar `SELECT CAST(? AS DATE) FROM RDB$DATABASE`, que mide lo mismo sin tabla — preferir esta forma.)

Agregar en el mismo archivo:
- `TestMedicion_EVIS019_MonTimestamp`: abrir una transacción RW con `pool.BeginTx`, ejecutar `SELECT CURRENT_TRANSACTION FROM RDB$DATABASE`, anotar `antes := time.Now().UTC()`; desde **otra** conexión leer `SELECT MON$TIMESTAMP, CAST(MON$TIMESTAMP AS TIMESTAMP), RDB$GET_CONTEXT('SYSTEM','SESSION_TIMEZONE') FROM MON$TRANSACTIONS WHERE MON$TRANSACTION_ID = ?` con el id de la primera; `t.Logf` los tres valores y su tipo Go; afirmar que **alguna** de las dos lecturas, pasada por `firebird.ScanUTCTime`, queda a menos de 2 s de `antes`. El resultado decide qué expresión usa `marca_segura.go` (Tarea 9). Control positivo: la misma consulta con un `MON$TRANSACTION_ID` inexistente devuelve `sql.ErrNoRows`.
- `TestMedicion_EVIS016_OrdenDeIDsDev`: `SELECT ID FROM MSP_VISITAS WHERE ZONA_CLIENTE_ID = ? ORDER BY ID ROWS 500` con la zona de E-INF-014 (2990693) y comparar con `slices.Sorted` en Go (orden de bytes). Registrar coincide/no coincide. La parte de producción es la Tarea 2.
- `TestMedicion_EVIS015_PlanDelPredicadoDev`: `SET PLANONLY` no está disponible por el driver; usar `isql` con el script `scripts/medicion/visitas-p2-plan.sql`:
```sql
SET PLANONLY ON;
SELECT v.ID FROM MSP_VISITAS v
WHERE v.ZONA_CLIENTE_ID = 2990693 AND v.UPDATED_AT IS NOT NULL AND v.FECHA >= DATEADD(-92 DAY TO CURRENT_DATE)
ORDER BY v.UPDATED_AT, v.ID;
```
  y registrar el `PLAN` (antes de la migración no hay `UPDATED_AT`: correrlo **después** de aplicar 000066 en dev, Tarea 6; aquí solo se crea el script).
- `TestMedicion_EVIS002_ReenvioDistintoDescarta` (en `internal/visitas/app/`, con fakes): registrar una visita, reenviar el mismo id con `Nota` distinta → `201` (sin error) y `FindByID` devuelve la nota vieja. PASS hoy = E-VIS-002 MEDIDO.

- [ ] **Step 2: Correr y registrar**

Run: `cd /Volumes/M2-1TB/Developer/msp-api && go clean -testcache && FB_DATABASE=... go test ./internal/visitas/infra/visitasfb/ -run Medicion -v -count=1 -p 1 && go test ./internal/visitas/app/ -run EVIS002 -v -count=1`
Registrar cada salida en su entrada. **Regla de decisión E-VIS-014:** si **cualquier** subcaso difiere, las columnas de fecha son `CHAR(10) CHARACTER SET ASCII` (`YYYY-MM-DD`), y así se escriben la Tarea 6 y el repo (Tarea 7). Si todos coinciden, `DATE`. La decisión se anota en `visitas.md` y en el encabezado de la migración.

- [ ] **Step 3: Commit**

```bash
git add internal/visitas docs/evidencia/visitas.md scripts/medicion/visitas-p2-plan.sql
git commit -m "test(visitas): mediciones que deciden DATE, la marca segura y el orden de ids (E-VIS-002, E-VIS-014, E-VIS-016, E-VIS-019)"
```

---

### Task 2: Mediciones de producción de P2 y DA-8 (MEDIR, DEPENDE DEL DUEÑO) + compuerta "antes" de P2

**Files:**
- Create: `/Volumes/M2-1TB/Developer/msp-api/scripts/medicion/visitas-p2-prod.sql`, `visitas-p2-prod.bat`, `visitas-p2-dev.sql`

- [ ] **Step 1: Escribir las consultas**

`visitas-p2-prod.sql`: encabezado `SET TRANSACTION READ ONLY ISOLATION LEVEL READ COMMITTED;` y cierre `COMMIT; SELECT COUNT(*) AS ATORADAS FROM MON$STATEMENTS WHERE MON$STATE = 2;` como en P1; cuerpo con **Q2, Q3, Q7, Q8 copiadas literalmente del spec §10**, más:
```sql
-- == E-VIS-016 orden de ID en CHARACTER SET NONE ==
SELECT FIRST 500 ID FROM MSP_VISITAS WHERE ZONA_CLIENTE_ID = 2990693 ORDER BY ID;
-- == E-VIS-012 reloj del servidor de base (se compara con el reloj de Go en el mismo servidor) ==
SELECT CURRENT_TIMESTAMP, LOCALTIMESTAMP FROM RDB$DATABASE;
-- == E-VIS-019 MON$TIMESTAMP en producción ==
SELECT FIRST 3 MON$TRANSACTION_ID, MON$TIMESTAMP, MON$READ_ONLY FROM MON$TRANSACTIONS WHERE MON$STATE = 1;
-- == DA-8 / E-COB-031: pendientes con INTENTOS > 0 (rechazado según el servidor) ==
SELECT COUNT(*) AS RECHAZADOS_SERVIDOR FROM MSP_PAGOS_RECIBIDOS WHERE ESTADO = 'P' AND INTENTOS > 0;
```
E-VIS-012: el dueño corre en el mismo servidor, en la misma consola, `powershell -c "[DateTime]::UtcNow.ToString('o')"` justo antes y después del `.bat`; el desfase es `CURRENT_TIMESTAMP` (convertido de CDMX) contra el promedio.
DA-8 (definición única de "rechazado"): el teléfono usa `DOCTO_CC_ID = -1`; medir en el teléfono del dueño con el depurador remoto `SELECT COUNT(*) FROM Payment WHERE DOCTO_CC_ID = -1` y, con esos ids, en producción `SELECT ID, ESTADO, INTENTOS FROM MSP_PAGOS_RECIBIDOS WHERE ID IN (...)` (tandas ≤ 120). Registrar cuántos difieren como **E-COB-031** en `docs/evidencia/cobranza.md` (y corregir su línea "siguiente libre" a `E-COB-032`). La definición que usan los casos compartidos es un booleano `rechazado` en la entrada; cada lado lo mapea de su fuente: servidor `INTENTOS > 0` sobre pendientes (`actividades_repo.go:521-524`), teléfono `DOCTO_CC_ID = -1`. Si la medición muestra diferencias, **se detiene** P2 hasta que el dueño fije cuál manda (spec DA-8).
Q5 (promesas/citas solo en teléfono, E-VIS-010): con el depurador remoto, `SELECT COUNT(*) FROM Visit WHERE PROMESA_FECHA IS NOT NULL OR CITA_FECHA IS NOT NULL` y `SELECT COUNT(*) FROM Visit WHERE (PROMESA_FECHA IS NOT NULL OR CITA_FECHA IS NOT NULL) AND GUARDADO_EN_MICROSIP = 1` (las que se rellenarán).

- [ ] **Step 2: Correr en dev con control positivo**, luego **entregar al dueño** (`scp` + `.bat`). Registrar E-VIS-006 (Q2), -007 (Q3), -010 (Q5), -012, -015 prod (`PLAN` en producción **después** de aplicar 000066 allá, paso 4 de §11: se deja anotado como pendiente hasta entonces), -016 prod, -019 prod, Q7 (DA-9) y Q8 (volumen por zona; contrastar con 3,919 de E-INF-014).
- [ ] **Step 3: E-VIS-011 (Q6) ya se midió en P1**; reconfirmar que no apareció otro escritor (misma consulta Q6).
- [ ] **Step 4: Compuerta "antes" de P2** — agente independiente con solo preguntas:
```
1. ¿Un reenvío de POST /v2/visitas con el mismo id y datos distintos guarda los datos nuevos? (E-VIS-002)
2. ¿Cuántos duplicados lógicos hay en MSP_VISITAS desde 2026-08-13 y qué versión de UUID tienen por mes? (E-VIS-006, -007)
3. ¿Cuántas visitas con promesa o cita existen solo en el teléfono del dueño? (E-VIS-010)
4. ¿Escribe alguien más en MSP_VISITAS? (E-VIS-011)
5. ¿Cuál es el desfase entre el reloj de Go y el de Firebird en el servidor de producción? (E-VIS-012)
6. ¿Un DATE escrito y leído por el driver cambia de día según la TZ del proceso? (E-VIS-014)
7. ¿Cómo se lee MON$TIMESTAMP con este driver y a qué instante corresponde? (E-VIS-019)
8. ¿El orden de MSP_VISITAS.ID por la base coincide con el orden de bytes, en dev y en producción? (E-VIS-016)
9. ¿Coinciden la definición de "pago rechazado" del teléfono y la del servidor? (E-COB-031)
Para cada una: SOSTIENE / CONTRADICE / FALTA EVIDENCIA / PARCIAL, re-derivando desde la salida pegada.
```
E-VIS-013 (marca segura) **no** entra aquí: se mide en la Tarea 10 con la implementación, y su compuerta es la "después". Todo lo que no sea SOSTIENE detiene P2.
- [ ] **Step 5: Commit** `docs(evidencia): mediciones de producción y compuerta antes de P2 (E-VIS-006, -007, -010, -012, -016, -019, E-COB-031)`.

---

### Task 3: Fecha y hora civiles + bloques de compromiso (dominio)

**Files:**
- Create: `internal/visitas/domain/calendario.go`, `calendario_test.go`
- Create: `internal/visitas/domain/compromiso.go`, `compromiso_test.go`
- Modify: `internal/visitas/domain/errors.go`

**Interfaces:**
- Produces:
```go
type FechaCivil struct{ anio int; mes time.Month; dia int }
func ParseFechaCivil(s string) (FechaCivil, error)          // "YYYY-MM-DD" estricto
func FechaCivilDe(t time.Time, loc *time.Location) FechaCivil
func (f FechaCivil) String() string
func (f FechaCivil) Compare(o FechaCivil) int
func (f FechaCivil) DiasHasta(o FechaCivil) int             // o − f en días civiles
func (f FechaCivil) InicioEn(loc *time.Location) time.Time   // 00:00 local, devuelto en UTC
type HoraCivil struct{ hora, minuto int }
func ParseHoraCivil(s string) (HoraCivil, error)            // "HH:MM" 00:00–23:59
func (h HoraCivil) String() string
func (h HoraCivil) EnDia(f FechaCivil, loc *time.Location) time.Time // instante UTC
type Promesa struct{ ventaID *int; fecha FechaCivil; montoCentavos *int64 }
type Cita struct{ fecha FechaCivil; hora *HoraCivil }
func NuevaPromesa(ventaID *int, fecha *string, montoCentavos *int64) (*Promesa, error) // nil,nil si los tres son nil
func NuevaCita(fecha, hora *string) (*Cita, error)                                     // nil,nil si los dos son nil
func (p *Promesa) Igual(o *Promesa) bool; func (c *Cita) Igual(o *Cita) bool         // nil-safe
// getters: VentaID() *int, Fecha() FechaCivil, MontoCentavos() *int64, Hora() *HoraCivil
```
- Errores nuevos en `errors.go` (`apperror.NewValidation`): `ErrVisitaPromesaFechaInvalida` (`visita_promesa_fecha_invalida`, "la fecha de la promesa no es válida"), `ErrVisitaPromesaIncompleta` (`visita_promesa_incompleta`, "la promesa necesita fecha"), `ErrVisitaPromesaMontoInvalido` (`visita_promesa_monto_invalido`, "el monto prometido debe ser mayor a cero"), `ErrVisitaPromesaVentaInvalida` (`visita_promesa_venta_invalida`, "la venta de la promesa no es válida"), `ErrVisitaCitaIncompleta` (`visita_cita_incompleta`, "la hora de la cita necesita fecha"), `ErrVisitaCitaInvalida` (`visita_cita_invalida`, "la fecha u hora de la cita no es válida"); y `apperror.NewValidation("visita_compromiso_distinto", "esta visita ya tiene otro compromiso registrado")` como `ErrVisitaCompromisoDistinto`.

- [ ] **Step 1: Pruebas que fallan**

`calendario_test.go`:
```go
func TestFechaCivil_ParseEstricto(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"2026-10-20", "2024-02-29"} {
		f, err := domain.ParseFechaCivil(ok); require.NoError(t, err); assert.Equal(t, ok, f.String())
	}
	for _, malo := range []string{"", "2026-10-2", "2026-13-01", "2025-02-29", "20-10-2026", "2026-10-20T00:00:00Z", " 2026-10-20"} {
		_, err := domain.ParseFechaCivil(malo); assert.Error(t, err, malo)
	}
}
func TestFechaCivilDe_UsaLaZonaDelNegocio(t *testing.T) {
	t.Parallel()
	cdmx, _ := time.LoadLocation("America/Mexico_City")
	// 2026-10-17T05:30Z = 2026-10-16 23:30 en CDMX (Review Focus 2).
	assert.Equal(t, "2026-10-16", domain.FechaCivilDe(time.Date(2026, 10, 17, 5, 30, 0, 0, time.UTC), cdmx).String())
}
func TestHoraCivil(t *testing.T) {
	t.Parallel()
	h, err := domain.ParseHoraCivil("16:30"); require.NoError(t, err); assert.Equal(t, "16:30", h.String())
	for _, m := range []string{"24:00", "9:30", "16:60", "1630", ""} { _, err := domain.ParseHoraCivil(m); assert.Error(t, err, m) }
}
func TestDiasHasta(t *testing.T) {
	t.Parallel()
	a, _ := domain.ParseFechaCivil("2026-10-01"); b, _ := domain.ParseFechaCivil("2026-10-15")
	assert.Equal(t, 14, a.DiasHasta(b)); assert.Equal(t, -14, b.DiasHasta(a))
}
```
`compromiso_test.go` — una fila por regla de spec §5.2.2:
```go
func TestNuevaPromesa_Reglas(t *testing.T) {
	t.Parallel()
	s := func(v string) *string { return &v }; i := func(v int) *int { return &v }; c := func(v int64) *int64 { return &v }
	casos := []struct{ nombre string; venta *int; fecha *string; monto *int64; err error }{
		{"sin nada es sin promesa", nil, nil, nil, nil},
		{"fecha sola", nil, s("2026-10-20"), nil, nil},
		{"completa", i(13914738), s("2026-10-20"), c(15000), nil},
		{"fecha mal formada", nil, s("20/10/2026"), nil, domain.ErrVisitaPromesaFechaInvalida},
		{"monto sin fecha", nil, nil, c(15000), domain.ErrVisitaPromesaIncompleta},
		{"venta sin fecha", i(1), nil, nil, domain.ErrVisitaPromesaIncompleta},
		{"monto cero", nil, s("2026-10-20"), c(0), domain.ErrVisitaPromesaMontoInvalido},
		{"monto negativo", nil, s("2026-10-20"), c(-1), domain.ErrVisitaPromesaMontoInvalido},
		{"venta cero", i(0), s("2026-10-20"), nil, domain.ErrVisitaPromesaVentaInvalida},
		{"fecha del pasado se acepta", nil, s("2019-01-01"), nil, nil},
		{"fecha muy lejana se acepta", nil, s("2099-12-31"), nil, nil},
	}
	for _, tc := range casos {
		_, err := domain.NuevaPromesa(tc.venta, tc.fecha, tc.monto)
		if tc.err == nil { assert.NoError(t, err, tc.nombre) } else { assert.ErrorIs(t, err, tc.err, tc.nombre) }
	}
}
func TestNuevaCita_Reglas(t *testing.T) { /* sin nada; fecha sola; fecha+hora; hora sin fecha → ErrVisitaCitaIncompleta; fecha mala o hora "25:00" → ErrVisitaCitaInvalida */ }
func TestIgualdadDeBloques(t *testing.T) { /* nil==nil; nil!=x; misma fecha distinta hora → distinto; monto nil vs 0 imposible (0 inválido) */ }
```
Run: `go test ./internal/visitas/domain/ -run 'FechaCivil|HoraCivil|DiasHasta|NuevaPromesa|NuevaCita|Igualdad' -count=1` → FAIL.

- [ ] **Step 2: Implementar** (`FechaCivil` como copia deliberada de `internal/desempeno/domain/calendario.go:Fecha` — depguard impide importarla —, con `ParseFechaCivil` usando `time.Parse("2006-01-02", s)` **y** verificando `t.Format(...) == s` para rechazar formas no canónicas; `FechaCivilDe(t, loc)` = `t.In(loc)` y sus campos; `InicioEn` = `time.Date(a, m, d, 0, 0, 0, 0, loc).UTC()`; `DiasHasta` sobre `time.Date(..., time.UTC)` dividiendo por 24 h, sin DST). `NuevaPromesa`: si los tres nil → `nil, nil`; si `fecha == nil` → `ErrVisitaPromesaIncompleta`; parse → `ErrVisitaPromesaFechaInvalida`; `monto != nil && *monto <= 0` → `ErrVisitaPromesaMontoInvalido`; `venta != nil && *venta <= 0` → `ErrVisitaPromesaVentaInvalida`. `NuevaCita`: ambos nil → nil; `fecha == nil` → `ErrVisitaCitaIncompleta`; parse de fecha u hora → `ErrVisitaCitaInvalida`.
- [ ] **Step 3: Verde** → PASS. **Step 4: Commit** `feat(visitas): fecha y hora civiles y bloques de promesa y cita con sus reglas`.

---

### Task 4: Visita con compromiso, `UpdatedAt` y `CreatedBy`; comparación de reenvío (tabla 5.2.3)

**Files:**
- Modify: `internal/visitas/domain/visita.go` (struct `:46-67`, `CrearVisitaParams` `:74-89`, `NewVisita` `:97`, `RehydrateVisitaParams` `:154-167`, getters)
- Create: `internal/visitas/domain/reenvio.go`, `reenvio_test.go`

**Interfaces:**
- Produces (`CrearVisitaParams` gana): `PromesaVentaID *int; PromesaFecha *string; PromesaMontoCentavos *int64; CitaFecha *string; CitaHora *string`. `NewVisita` valida los bloques **después** de las reglas de hoy, en ese orden.
- Produces (`RehydrateVisitaParams` gana): `Promesa *Promesa; Cita *Cita; UpdatedAt *time.Time; CreatedBy *uuid.UUID`.
- Produces getters: `Promesa() *Promesa`, `Cita() *Cita`, `UpdatedAt() *time.Time`, `CreatedBy() *uuid.UUID`.
- Produces:
```go
type DecisionReenvio struct {
	RellenarPromesa bool
	RellenarCita    bool
	Distinto        bool     // algún bloque presente en ambos y distinto → 422 visita_compromiso_distinto
	BaseDistinta    []string // nombres de campos base distintos (fase A: solo métrica)
}
func CompararReenvio(existente, entrante *Visita) DecisionReenvio
```
  "Base" = `cliente_id`, `cobrador_id`, `fecha` (al segundo, UTC), `tipo_visita`, `impte_docto_cc_id` (nil ≡ 0), `zona_cliente_id`, `forma_cobro_id`, `nota` (NFC), `lat`/`lng` (tolerancia 1e-7).

- [ ] **Step 1: Pruebas que fallan** — una por fila de la tabla 5.2.3 más las de base:
```go
func TestReenvio_Tabla523(t *testing.T) {
	t.Parallel()
	base := visitaDePrueba(t, nil, nil)
	prom := visitaDePrueba(t, promesa(t, "2026-10-20"), nil)
	prom2 := visitaDePrueba(t, promesa(t, "2026-10-21"), nil)
	cita := visitaDePrueba(t, nil, citaDe(t, "2026-10-18", "16:30"))
	casos := []struct{ nombre string; existente, entrante *domain.Visita; esperado domain.DecisionReenvio }{
		{"bloque nulo, llega promesa: rellena", base, prom, domain.DecisionReenvio{RellenarPromesa: true}},
		{"bloque igual: nada", prom, prom, domain.DecisionReenvio{}},
		{"bloque presente, llega ausente: nada (I10)", prom, base, domain.DecisionReenvio{}},
		{"bloque distinto: 422", prom, prom2, domain.DecisionReenvio{Distinto: true}},
		{"promesa y cita independientes", prom, visitaDePrueba(t, promesa(t, "2026-10-20"), citaDe(t, "2026-10-18", "")), domain.DecisionReenvio{RellenarCita: true}},
		{"cita distinta por hora: 422", cita, visitaDePrueba(t, nil, citaDe(t, "2026-10-18", "17:00")), domain.DecisionReenvio{Distinto: true}},
	}
	for _, tc := range casos { assert.Equal(t, tc.esperado, domain.CompararReenvio(tc.existente, tc.entrante), tc.nombre) }
}
func TestReenvio_BloqueAusenteNoBorra(t *testing.T) { /* = fila 4, nombre explícito del Review Focus 3 */ }
func TestReenvio_BaseDistintaSoloSeReporta(t *testing.T) {
	t.Parallel()
	a := visitaConNota(t, "Café"); b := visitaConNota(t, "Café") // misma nota en NFD
	assert.Empty(t, domain.CompararReenvio(a, b).BaseDistinta, "NFC: no es distinta")
	c := visitaConNota(t, "otra")
	assert.Equal(t, []string{"nota"}, domain.CompararReenvio(a, c).BaseDistinta)
	assert.False(t, domain.CompararReenvio(a, c).Distinto, "fase A: base distinta no bloquea (DA-12)")
}
func TestReenvio_LatLngTolerancia(t *testing.T) { /* 19.4 vs 19.40000005 → igual; 19.4 vs 19.4000002 → ["lat"] */ }
func TestReenvio_ImpteNilEsCero(t *testing.T) { /* nil vs 0 → igual */ }
```
(helpers `visitaDePrueba(t, p *Promesa, c *Cita)` usando `RehydrateVisita` con campos fijos.)

- [ ] **Step 2: Implementar** (`CompararReenvio` compara cada bloque: `existente == nil && entrante != nil` → rellena; ambos y `!Igual` → `Distinto`; resto nada). Las reglas de `NewVisita` se extienden: tras las de hoy, `NuevaPromesa(p.PromesaVentaID, p.PromesaFecha, p.PromesaMontoCentavos)` y `NuevaCita(p.CitaFecha, p.CitaHora)`; errores tal cual. `RehydrateVisita` asigna sin validar (filas legadas).
- [ ] **Step 3: Verde + `go test ./internal/visitas/... -count=1 -race`** (las pruebas viejas deben seguir verdes: los campos nuevos son opcionales). **Step 4: Commit** `feat(visitas): la visita lleva su compromiso y el reenvío se decide con la tabla 5.2.3 (H6)`.

---

### Task 5: `DerivarCompromisos` + casos compartidos `testdata/compromisos/` (I5)

**Files:**
- Create: `internal/visitas/domain/alcance.go`, `alcance_test.go`
- Create: `internal/visitas/domain/derivar_compromisos.go`, `derivar_compromisos_test.go`
- Create: `internal/visitas/domain/casos_compromisos_test.go`
- Create: `testdata/compromisos/001-…json` … (mínimo 30) y `testdata/compromisos/VERSION`
- Create: `cmd/api/alcance_visitas_paridad_test.go`

**Interfaces:**
- Produces:
```go
type Alcance int // AlcanceVenta | AlcanceCliente
func AlcanceDe(tipo string, tieneCita bool) Alcance // copia fiel de desempenodomain.AlcanceDeTipoVisita

type EstadoCompromiso string
const (
	EstadoCumplida          EstadoCompromiso = "cumplida"
	EstadoCitaCumplida      EstadoCompromiso = "cita_cumplida"
	EstadoVigenteFutura     EstadoCompromiso = "vigente_futura"
	EstadoVigenteHoy        EstadoCompromiso = "vigente_hoy"
	EstadoIncumplida        EstadoCompromiso = "incumplida"
	EstadoCerradaSinCumplir EstadoCompromiso = "cerrada_sin_cumplir"
)
type TipoCompromiso string // "promesa" | "cita"
const DiasDeGraciaIncumplida = 14

type CuentaCompromiso struct{ VentaID, ClienteID int }
type VisitaCompromiso struct {
	ID string; ClienteID int; VentaID *int; TipoVisita string; Instante time.Time // UTC
	Promesa *Promesa; Cita *Cita
}
type PagoCompromiso struct {
	VentaID int; Instante *time.Time // nil = pago de oficina sin hora (DA-7)
	Fecha FechaCivil                 // fecha civil del pago (siempre)
	Rechazado bool
}
type CompromisoVigente struct {
	VentaID int; VisitaID string; Tipo TipoCompromiso; Estado EstadoCompromiso
	Dia FechaCivil; Hora *HoraCivil; MontoCentavos *int64
}
func DerivarCompromisos(cuentas []CuentaCompromiso, visitas []VisitaCompromiso, pagos []PagoCompromiso,
	hoy FechaCivil, loc *time.Location) map[int]CompromisoVigente

type ColumnaSugerida string // "por_visitar" | "no_paga" | "" (sin opinión: decide el documento 1)
func ColumnaSugeridaDe(c CompromisoVigente) ColumnaSugerida
```

**La regla (spec §5.3, con las precisiones que el spec dejaba abiertas — se reportan como huecos):**
1. Visita **aplicable** a V (DA-6: toda visita capturada cuenta): `AlcanceDe(tipo, cita != nil) == AlcanceCliente && ClienteID == V.ClienteID`, o `AlcanceVenta && VentaID != nil && *VentaID == V.VentaID`.
2. **M** = visita aplicable más reciente (mayor `Instante`; empate → menor `ID` en orden de bytes).
3. **Compromiso de una visita para V**: si trae cita → tipo `cita` (con `MontoCentavos` de la promesa si también trae; **hueco del spec**: una visita con promesa y cita a la vez se trata como cita, igual que `EstadoDeTipoVisita` fuerza `cita_a_una_hora` cuando hay cita); si no, si trae promesa y (`Promesa.VentaID == nil` o `== V`) → tipo `promesa`; promesa sin fecha no existe (DA-16, `NuevaPromesa` ya la rechaza).
4. Si **M** tiene compromiso para V → es el vigente. Día = fecha de la cita o de la promesa.
   - promesa: `cumplida` si hay **pago que cierra**; si no, por día.
   - cita: `cita_cumplida` si hay pago que cierra; si no, por día.
   - por día: `día > hoy` → `vigente_futura`; `== hoy` → `vigente_hoy`; `hoy − día ≤ 14` → `incumplida`; `> 14` → `cerrada_sin_cumplir`.
5. Si **M** no tiene compromiso: sea **P** la visita aplicable más reciente **con** cita para V anterior a M. Si existe y alguna visita aplicable posterior a P tiene `Instante ≥ inicio de la cita` (día + hora civil; sin hora, inicio del día, en `loc`) → `cita_cumplida` de P. En cualquier otro caso → **sin entrada** (reemplazada; no se muestra). **Hueco del spec**: la tabla 5.3.2 pone `cita_cumplida` por visita posterior, pero la definición de "vigente" hace que esa visita posterior reemplace a la cita; esta es la lectura que conserva las dos frases.
6. **Pago que cierra** (para el compromiso de la visita X): pago a V no rechazado con `Instante` estrictamente posterior a `X.Instante`; si el pago no tiene hora (DA-7), cierra si `pago.Fecha >= FechaCivilDe(X.Instante, loc)`. Incluye condonación (no se filtra por forma de cobro).
7. `ColumnaSugeridaDe`: `vigente_hoy` o `incumplida` (cualquier tipo) → `por_visitar`; cita `vigente_futura` → `por_visitar`; promesa `vigente_futura` → `no_paga`; resto → `""`.

- [ ] **Step 1: Formato de caso y primeros casos (rojo)**

`testdata/compromisos/001-promesa-futura.json`:
```json
{
  "nombre": "promesa con fecha futura: vigente_futura, columna no_paga",
  "hoy": "2026-10-16",
  "zona": "America/Mexico_City",
  "cuentas": [{"venta_id": 100, "cliente_id": 10}],
  "visitas": [
    {"id": "00000000-0000-3000-8000-000000000001", "cliente_id": 10, "venta_id": 100,
     "tipo_visita": "Pidió tiempo para pagar", "instante": "2026-10-15T17:00:00Z",
     "promesa": {"venta_id": 100, "fecha": "2026-10-20", "monto_centavos": 15000}, "cita": null}
  ],
  "pagos": [],
  "esperado": [
    {"venta_id": 100, "visita_id": "00000000-0000-3000-8000-000000000001", "tipo": "promesa",
     "estado": "vigente_futura", "dia": "2026-10-20", "hora": null, "monto_centavos": 15000, "columna": "no_paga"}
  ]
}
```
Un `pago`: `{"venta_id": 100, "instante": "2026-10-16T18:00:00Z" | null, "fecha": "2026-10-16", "rechazado": false, "local_pendiente": false}`. Ventas sin compromiso **no** aparecen en `esperado` (y el cargador exige que el resultado tampoco las tenga). `tipo_visita` usa literales reales de `internal/visitas/domain/tipo_visita.go:76-114`.

Lista **mínima obligatoria** de casos (spec §5.3.4 + §5.7 + Review Focus), cada uno su archivo `NNN-kebab.json`:
001 promesa futura · 002 promesa hoy · 003 promesa ayer (incumplida) · 004 promesa hoy−14 (incumplida) · 005 promesa hoy−15 (cerrada_sin_cumplir) · 006 promesa hoy+1 · 007 pago un segundo antes del compromiso (no cierra) · 008 pago un segundo después (cumplida) · 009 condonación cierra · 010 pago rechazado no cierra · 011 captura local pendiente cierra · 012 captura local rechazada no cierra · 013 promesa reemplazada por "No se encontraba" (alcance cliente) · 014 promesa reemplazada por visita de otro cobrador a la misma venta · 015 cita sin hora futura (columna por_visitar) · 016 cita con hora hoy · 017 cita con visita posterior ≥ inicio sin pago (cita_cumplida) · 018 cita con visita posterior **antes** del inicio (sin entrada) · 019 cita cumplida por pago · 020 promesa sin fecha no es compromiso (la visita trae solo monto: se modela como visita sin promesa) · 021 empate mismo segundo (menor id gana) · 022 visita 23:30 local · 023 pago de oficina sin hora del mismo día (cierra, DA-7) · 024 pago de oficina sin hora del día anterior (no cierra) · 025 promesa a otra venta del mismo cliente (no aplica a esta) · 026 promesa con `venta_id` nulo y alcance cliente (aplica a todas las ventas del cliente) · 027 visita y cita a la vez (tipo cita) · 028 **cita de la semana pasada para esta semana sigue visible** (§5.7-A) · 029 cita futura en venta sin abonos (§5.7-C, el estado existe aunque no haya pagos) · 030 cita sin hora de hoy, vigente_hoy (§5.7-F).

`casos_compromisos_test.go`: mismo mecanismo que `internal/desempeno/domain/casos_compartidos_test.go` (regex de nombre `^[0-9]{3}-[a-z0-9-]+\.json$`, `listarCasos` rechaza extraños, `hashDeCasos` SHA-256 de los `.json` en orden de bytes concatenados, `TestCasosCompromisos_VersionCorrespondeAlHash` con mínimo 30 casos y el comando de regeneración en el mensaje: `cd testdata/compromisos && ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1 > VERSION`; `parsearCaso` estricto con `DisallowUnknownFields`; `TestCasosCompromisos_CadaCasoContraLaDerivacion`; `TestCasosCompromisos_CoberturaMinima` que exige al menos un caso por cada `EstadoCompromiso` y por cada `ColumnaSugerida`).

Run: `go test ./internal/visitas/domain/ -run CasosCompromisos -count=1` → FAIL (derivación inexistente).

- [ ] **Step 2: Alcance y su paridad con desempeño**

`alcance.go`: tabla literal→alcance copiada de `internal/desempeno/domain/tipo_visita.go` (`estadoPorTipoVisita` + `EstadoCuenta.Alcance()`), con la misma regla "tener cita fuerza el estado de cita". `cmd/api/alcance_visitas_paridad_test.go`:
```go
func TestAlcanceVisitas_IgualQueDesempeno(t *testing.T) {
	t.Parallel()
	for _, lit := range desempenodomain.CatalogoTiposVisita() {
		for _, conCita := range []bool{false, true} {
			assert.Equal(t, desempenodomain.AlcanceDeTipoVisita(lit.Literal, conCita) == desempenodomain.AlcanceCliente,
				visitasdomain.AlcanceDe(lit.Literal, conCita) == visitasdomain.AlcanceCliente, "%s cita=%v", lit.Literal, conCita)
		}
	}
}
```
(si depguard impide a `cmd/api` importar dominios en pruebas, mover la prueba a un paquete `internal/contratos_test` permitido por `.golangci.yml`, o — último recurso — cubrir cada literal con un caso compartido de alcance; decirlo en el reporte.)

- [ ] **Step 3: Implementar `DerivarCompromisos`** siguiendo los 7 puntos de la regla (índice por cliente y por venta como `desempeno/domain/deriver.go:226-263`, pero **sin ventana**: spec §5.3.2 "visibilidad"). Pruebas unitarias extra en `derivar_compromisos_test.go` con `rapid`: (a) **total**: para historiales generados, ninguna venta fuera de `cuentas` aparece; (b) **idempotencia**: derivar dos veces da lo mismo; (c) **orden de entrada irrelevante**: barajar visitas y pagos no cambia el resultado (fija el desempate); (d) **I9**: no hay estado guardado (la función es pura: prueba de forma que `derivar_compromisos.go` no importa `time.Now` — búsqueda de `time.Now(` en el archivo da 0, con control positivo sobre un archivo que sí lo tiene).

- [ ] **Step 4: Escribir los 30 casos, `VERSION`, verde**

Run: `go test ./internal/visitas/domain/ -count=1 -race` → PASS. Revertir el punto 6 (cambiar `After` por `!Before`) → 007/008 FAIL; restaurar.

- [ ] **Step 5: Commit** `feat(visitas): derivación de compromisos sin estado guardado y casos compartidos (I5, I9)`.

---

### Task 6: Migraciones 000066 y 000067 + relleno de `UPDATED_AT` en tandas

**Files:**
- Create: `migrations-firebird/000066_msp_visitas_compromisos.up.sql`, `.down.sql`
- Create: `migrations-firebird/000067_msp_cfg_sync_epoch_visitas_tareas.up.sql`, `.down.sql`
- Create: `scripts/visitas-relleno-updated-at.sh`
- Create: `internal/visitas/infra/visitasfb/migracion_000066_test.go`

- [ ] **Step 1: Prueba de forma de la migración (rojo)**

```go
// Prueba de forma: CLAUDE.md §1 en las migraciones de visitas (sin DEFAULT,
// trigger, CHECK de negocio ni procedimiento), y que el estado no tenga columna.
func TestMigracion000066_SinLogicaEnLaBase(t *testing.T) {
	t.Parallel()
	for _, f := range []string{"000066_msp_visitas_compromisos.up.sql", "000067_msp_cfg_sync_epoch_visitas_tareas.up.sql"} {
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "migrations-firebird", f))
		require.NoError(t, err, f)
		sql := strings.ToUpper(sinComentarios(string(b)))
		for _, prohibido := range []string{" DEFAULT ", "CREATE TRIGGER", "CREATE PROCEDURE", " CHECK ", "ESTADO"} {
			assert.NotContains(t, sql, prohibido, "%s: %s", f, prohibido)
		}
	}
	assert.Contains(t, strings.ToUpper(sinComentarios("x DEFAULT y")), " DEFAULT ", "control positivo del buscador")
}
```

- [ ] **Step 2: Escribir 000066** (variante según E-VIS-014; abajo la variante `DATE` — si E-VIS-014 obligó a `CHAR(10)`, sustituir `DATE` por `CHAR(10) CHARACTER SET ASCII` en las dos líneas y decirlo en el encabezado):
```sql
-- 000066_msp_visitas_compromisos
-- Spec visitas §5.1. Promesa y cita como HECHOS de la visita. El estado del
-- compromiso NO tiene columna: se deriva en Go/Kotlin (I9).
-- Todo es ADD de columna nullable: no reescribe filas (precedente 000061).
-- UPDATED_AT lo pone Go dentro de la transacción (§5.4.4); queda NULL en las
-- filas viejas hasta el relleno en tandas (scripts/visitas-relleno-updated-at.sh).
-- Decisión DATE vs CHAR(10): E-VIS-014 (docs/evidencia/visitas.md).
-- Sin DEFAULT, sin trigger, sin CHECK, sin procedimiento (CLAUDE.md §1).
ALTER TABLE MSP_VISITAS ADD PROMESA_VENTA_ID INTEGER;
ALTER TABLE MSP_VISITAS ADD PROMESA_FECHA DATE;
ALTER TABLE MSP_VISITAS ADD PROMESA_MONTO_CENTAVOS BIGINT;
ALTER TABLE MSP_VISITAS ADD CITA_FECHA DATE;
ALTER TABLE MSP_VISITAS ADD CITA_HORA CHAR(5) CHARACTER SET ASCII;
ALTER TABLE MSP_VISITAS ADD UPDATED_AT TIMESTAMP;
ALTER TABLE MSP_VISITAS ADD CREATED_BY CHAR(36) CHARACTER SET ASCII;
COMMIT;
CREATE INDEX IDX_MSP_VISITAS_ZONA_UPD ON MSP_VISITAS (ZONA_CLIENTE_ID, UPDATED_AT, ID);
COMMIT;
SET STATISTICS INDEX IDX_MSP_VISITAS_ZONA_UPD;
COMMIT;
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (66, '000066_msp_visitas_compromisos', CURRENT_TIMESTAMP);
COMMIT;
```
(El `CURRENT_TIMESTAMP` del registro de migración es el patrón existente de `MSP_MIGRATIONS`, infraestructura, no negocio.) `.down.sql`: `DROP INDEX IDX_MSP_VISITAS_ZONA_UPD; COMMIT;` + `ALTER TABLE MSP_VISITAS DROP <col>;` para las siete + `DELETE FROM MSP_MIGRATIONS WHERE ID = 66; COMMIT;`. **Nota de despliegue** en el encabezado del down: en producción no se baja (spec §11 paso 4: "no se borran columnas").
Índices `IDX_MSP_VISITAS_PROM_VENTA` / `IDX_MSP_VISITAS_IMPTE`: **no** se crean salvo que el `PLAN` medido en la Tarea 11 (agenda) los pida; si los pide, migración aparte con su medición.

- [ ] **Step 3: Escribir 000067** (pasa la guardia: solo `INSERT` de `'visitas'`/`'tareas'`):
```sql
-- 000067_msp_cfg_sync_epoch_visitas_tareas
-- Spec visitas §5.4.5 / §6.2: generaciones propias de los canales de bajada.
-- Mismo patrón que 000055 (UPDATED_AT literal, MOTIVO explicativo). Nunca se
-- tocan las filas de pagos ni de ventas.
INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT)
VALUES ('visitas', 0, 0, 'semilla inicial de la migracion 000067', CAST('2026-10-16 00:00:00' AS TIMESTAMP));
INSERT INTO MSP_CFG_SYNC_EPOCH (RECURSO, ZONA_CLIENTE_ID, EPOCH, MOTIVO, UPDATED_AT)
VALUES ('tareas', 0, 0, 'semilla inicial de la migracion 000067', CAST('2026-10-16 00:00:00' AS TIMESTAMP));
COMMIT;
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (67, '000067_msp_cfg_sync_epoch_visitas_tareas', CURRENT_TIMESTAMP);
COMMIT;
```
`.down.sql`: `DELETE FROM MSP_CFG_SYNC_EPOCH WHERE RECURSO IN ('visitas', 'tareas'); DELETE FROM MSP_MIGRATIONS WHERE ID = 67; COMMIT;` (la guardia rechaza `'pagos'`/`'ventas'`/`UPDATE`; este down no los tiene).

- [ ] **Step 4: Relleno en tandas**

`scripts/visitas-relleno-updated-at.sh <desde YYYY-MM-DD> <hasta YYYY-MM-DD> > relleno.sql` genera, **un día por tanda** (≤ 10,000 filas por día según E-INF-014: ~43 visitas/día/zona × zonas; si Q8 muestra un día > 10,000, partir por mitades de día), con V = 92 días (DA-3):
```sql
SET TRANSACTION READ WRITE ISOLATION LEVEL READ COMMITTED;
UPDATE MSP_VISITAS SET UPDATED_AT = FECHA
 WHERE UPDATED_AT IS NULL AND FECHA >= TIMESTAMP '2026-07-16 00:00:00' AND FECHA < TIMESTAMP '2026-07-17 00:00:00';
COMMIT;
SELECT COUNT(*) AS ATORADAS FROM MON$STATEMENTS WHERE MON$STATE = 2;
```
Es relleno de dato (como `000064`, `VERSION = 0`), no lógica. Solo filas con `FECHA >= hoy − V` (spec §5.1.2). **Se corre fuera de horario y lo autoriza el dueño** en producción (spec §11 paso 4). En dev se corre ahora, tras `make fb-snapshot NAME=antes-000066`.

- [ ] **Step 5: Aplicar en dev y medir**

Run: `make fb-snapshot NAME=antes-000066 && make fb-migrate-up && bash scripts/visitas-relleno-updated-at.sh $(date -v-92d +%F) $(date +%F) > /tmp/relleno.sql && docker cp /tmp/relleno.sql mueblera-firebird:/tmp/ && docker exec mueblera-firebird /usr/local/firebird/bin/isql -u SYSDBA -p masterkey -ch UTF8 -i /tmp/relleno.sql /firebird/data/MUEBLERA.FDB | grep -E 'ATORADAS|SQLSTATE'`
Expected: `ATORADAS 0` en cada tanda y ningún `SQLSTATE`. Correr `scripts/medicion/visitas-p2-plan.sql` → registrar el `PLAN` en E-VIS-015 (debe usar `IDX_MSP_VISITAS_ZONA_UPD` o `IDX_MSP_VISITAS_ZONA_FECHA`; si es `NATURAL`, detener y reportar).
Run: `go test ./internal/visitas/infra/visitasfb/ -run Migracion000066 -count=1` → PASS.

- [ ] **Step 6: Commit** `feat(visitas): columnas de compromiso y cursor en MSP_VISITAS, y generaciones de bajada (migraciones 000066 y 000067)`.

---

### Task 7: Repositorio y servicio — escritura con estampa en transacción y reenvío completo (cierra H6)

**Files:**
- Modify: `internal/visitas/infra/visitasfb/repo.go` (`selectVisitaCols` `:42-54`, `insertVisitaSQL` `:56-63`, `Insert` `:73-101`, `scanVisitaRaw` `:183-196`)
- Modify: `internal/visitas/ports/outbound/repo.go` (método nuevo)
- Modify: `internal/visitas/app/registrar_visita.go`, `registrar_visita_con_imagenes.go` (`insertOrLoadVisita` `:285-296`)
- Modify: `cmd/api/visitas_wiring.go` (`visitasfb.New(pool, clock)`)
- Test: `internal/visitas/app/reenvio_test.go`, `internal/visitas/infra/visitasfb/repo_compromisos_e2e_test.go`

**Interfaces:**
- Consumes: `domain.CompararReenvio`, `domain.ErrVisitaCompromisoDistinto`.
- Produces (puerto):
```go
type VisitasRepo interface {
	Insert(ctx context.Context, v *domain.Visita) error                  // estampa UPDATED_AT y CREATED_BY dentro de su transacción
	FindByID(ctx context.Context, id uuid.UUID) (*domain.Visita, error)  // con bloques, UpdatedAt, CreatedBy
	FindExistingIDs(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
	RellenarCompromiso(ctx context.Context, id uuid.UUID, p *domain.Promesa, c *domain.Cita) (int64, error) // CAS; filas afectadas
}
```
- `visitasfb.New(pool *firebird.Pool, clock outbound.Clock) *Repo`.
- **Estampa:** dentro de `RunInTx`, primera sentencia `SELECT CURRENT_TRANSACTION FROM RDB$DATABASE`, luego `ahora := r.clock.Now()`, luego el `INSERT`/`UPDATE` con `UPDATED_AT = firebird.ToWallClock(ahora)`. Fechas civiles: si `DATE`, se bindean como `time.Date(a, m, d, 0, 0, 0, 0, firebird.BusinessTZ())` (patrón `microsipseed.fechaCalendario`, deliberadamente **sin** `ToWallClock`) y se leen con el método que E-VIS-014 validó; si `CHAR(10)`, como texto.
- Métricas (log estructurado `slog` con `evento=`): `visitas.reenvio.relleno`, `visitas.reenvio.igual`, `visitas.reenvio.compromiso_distinto`, `visitas.reenvio.base_distinta campo=<x>`, `visitas.compromiso.fecha_atipica` (fecha civil < hoy−365 o > hoy+365, solo cuenta).

- [ ] **Step 1: Pruebas de servicio con fakes (rojo)** — `reenvio_test.go`, una por fila de 5.2.3 contra `RegistrarVisita` y `RegistrarVisitaConImagenes`:
```go
func TestRegistrarVisita_ReenvioRellenaElBloqueNulo(t *testing.T) {
	t.Parallel()
	f := newImagenesFixture(t); id := uuid.New()
	_, err := f.svc.RegistrarVisita(ctx, inputSinCompromiso(id), uuid.New()); require.NoError(t, err)
	v, err := f.svc.RegistrarVisita(ctx, inputConPromesa(id, "2026-10-20"), uuid.New())
	require.NoError(t, err)
	require.NotNil(t, v.Promesa()); assert.Equal(t, "2026-10-20", v.Promesa().Fecha().String())
	assert.Equal(t, 1, f.repo.rellenos, "un solo UPDATE compare-and-set")
}
func TestRegistrarVisita_ReenvioConCompromisoDistintoEs422(t *testing.T) {
	// existente con 2026-10-20, entrante con 2026-10-21 → errors.Is(err, domain.ErrVisitaCompromisoDistinto); la fila no cambia
}
func TestRegistrarVisita_ReenvioSinBloqueNoBorra(t *testing.T) { /* I10 */ }
func TestRegistrarVisita_ReenvioIgualNoEscribe(t *testing.T) { /* rellenos == 0 */ }
func TestRegistrarVisita_RellenoQueAfecta0FilasReleeUnaVez(t *testing.T) {
	// fake: RellenarCompromiso devuelve 0 la primera vez y, al releer, la fila ya trae el mismo bloque → éxito;
	// si al releer trae otro bloque → ErrVisitaCompromisoDistinto. Exactamente 2 lecturas.
}
func TestRegistrarVisita_SSESeAnunciaEnTodoExito(t *testing.T) { /* relleno, igual y nuevo publican; distinto no */ }
```
(extender `fakeVisitasRepo` con `RellenarCompromiso` y contador `rellenos`.)

- [ ] **Step 2: Implementar en el servicio**: en el camino `ErrVisitaYaExiste` de `RegistrarVisita` (`:44-54`) y en `insertOrLoadVisita`, tras `FindByID`:
```go
	d := domain.CompararReenvio(existing, v)
	for _, campo := range d.BaseDistinta { s.log.Info("visitas.reenvio.base_distinta", "campo", campo, "visita_id", v.ID()) }
	switch {
	case d.Distinto:
		s.log.Info("visitas.reenvio.compromiso_distinto", "visita_id", v.ID())
		return nil, domain.ErrVisitaCompromisoDistinto
	case d.RellenarPromesa || d.RellenarCita:
		return s.rellenar(ctx, existing, v, d) // CAS + relectura única
	default:
		s.log.Info("visitas.reenvio.igual", "visita_id", v.ID())
		return existing, nil
	}
```
`rellenar` pasa solo los bloques a rellenar; si `RellenarCompromiso` devuelve 0, relee **una** vez y reevalúa con `CompararReenvio` (igual → devuelve la releída; distinto → 422). `Service` gana `log *slog.Logger` vía opción `WithLogger` (default `slog.Default()`).

El 422 debe salir con `X-Intent-Captured`: el middleware de captura ya guarda todo 4xx de `/v2/visitas` (`FIW:244-255`); prueba e2e en el Step 4.

- [ ] **Step 3: Repo** — `insertVisitaSQL` con las 7 columnas nuevas; `Insert` con la estampa; `RellenarCompromiso`:
```go
const rellenarPromesaSQL = `UPDATE MSP_VISITAS SET PROMESA_VENTA_ID = ?, PROMESA_FECHA = ?, PROMESA_MONTO_CENTAVOS = ?, UPDATED_AT = ?
WHERE ID = ? AND PROMESA_FECHA IS NULL`
const rellenarCitaSQL = `UPDATE MSP_VISITAS SET CITA_FECHA = ?, CITA_HORA = ?, UPDATED_AT = ?
WHERE ID = ? AND CITA_FECHA IS NULL`
```
ambos en la **misma** `RunInTx` con la lectura trivial primero y un único `ahora`; devuelve la suma de filas afectadas (si se pidieron los dos y uno afectó 0, el servicio relee). `scanVisitaRaw` lee las columnas nuevas (nulas en filas viejas) y arma `Promesa`/`Cita` con `domain.NuevaPromesa/NuevaCita`; si una fila de la base trae un valor que el dominio rechaza, **no** falla la lectura: se registra `visitas.lectura.compromiso_invalido` y el bloque queda nulo (una fila rara no tumba el sync). **Ojo** (hallazgo del informe de exploración): `COBRADOR_ID`, `FORMA_COBRO_ID`, `LAT`, `LNG`, `ZONA_CLIENTE_ID` se escanean como no nulos pero la tabla los permite nulos; el canal de bajada leerá filas legadas: cambiar esos scans a `sql.NullInt32`/`sql.NullFloat64` con 0 por omisión, con una prueba e2e que inserta (en `WithTestTransaction`) una fila con esos campos en NULL y la lee.

- [ ] **Step 4: E2E contra Firebird dev**

`repo_compromisos_e2e_test.go` (en `WithTestTransaction`): insertar con promesa y cita → `FindByID` devuelve lo mismo (incluye bordes de E-VIS-014: `2026-10-25`, `2026-11-01`) y `UpdatedAt` no nulo; relleno CAS afecta 1 y luego 0; `CreatedBy` = usuario. En `visitashttp` agregar `TestE2E_Reenvio_CompromisoDistintoSeCapturaEnLaBandeja` (patrón de `TestE2E_VisitasMultipart_UnaVisitaRechazadaConFotoSeCaptura`): 422 con código `visita_compromiso_distinto` y cabecera `X-Intent-Captured` presente.
Run: `FB_DATABASE=... go test ./internal/visitas/... -p 1 -count=1 -race -timeout 600s` → PASS.

- [ ] **Step 5: Rojo al revertir**: quitar `AND PROMESA_FECHA IS NULL` → la prueba e2e "afecta 0 la segunda vez" FAIL; quitar el `case d.Distinto` → `ReenvioConCompromisoDistintoEs422` FAIL. Restaurar.
- [ ] **Step 6: Commit** `fix(visitas): el reenvío rellena el compromiso faltante, rechaza el distinto y nunca borra (H6, I10). Cierra E-VIS-002.`

---

### Task 8: Contrato HTTP de escritura y lectura v3 + capacidades + interfaz con el documento 1

**Files:**
- Modify: `internal/visitas/infra/visitashttp/dto.go` (`CrearVisitaBody` `:73-86`, `VisitaDetalleDTO`)
- Modify: `internal/visitas/infra/visitashttp/handlers.go`, `handlers_visitas_existentes_por_ids.go`, `routes.go`
- Create: `internal/visitas/infra/visitashttp/handlers_capacidades.go` (+test)
- Modify: `internal/platform/config/config.go` (struct `Visitas` `:301-313`)
- Create: `internal/visitas/visitas_contracts.go`, `internal/visitas/visitas_contracts_mapper.go` (+test)

**Interfaces:**
- `CrearVisitaBody` gana: `PromesaVentaID *int json:"promesa_venta_id,omitempty"`, `PromesaFecha *string json:"promesa_fecha,omitempty"`, `PromesaMontoCentavos *int64 json:"promesa_monto_centavos,omitempty"`, `CitaFecha *string json:"cita_fecha,omitempty"`, `CitaHora *string json:"cita_hora,omitempty"` (así `DisallowUnknownFields` ya no rechaza a un APK nuevo).
- `VisitaDetalleDTO` gana: los cinco campos (mismos nombres, `null` si no hay), `updated_at *string` (RFC3339Nano UTC).
- `GET /v2/visitas/by-ids?ids=…&detalle=1` → `[]VisitaExistenteDTO{ID, PromesaVentaID, PromesaFecha, PromesaMontoCentavos, CitaFecha, CitaHora *…, ImagenIDs []string}`; sin `detalle` responde **exactamente** lo de hoy (`[]string`). Huma no admite dos tipos de respuesta en una operación: registrar `detalle` como query `bool` y responder con `huma.StreamResponse`/`any`… **más simple y verificable:** mantener la operación Huma de hoy intacta y montar `GET /visitas/by-ids` con `detalle=1` como ruta chi aparte **antes** de la Huma, solo cuando el query trae `detalle=1` (middleware que despacha); prueba que fija que sin `detalle` la respuesta es byte a byte la de hoy.
- `GET /v2/visitas/capacidades` → `{"contrato":3,"compromisos":<cfg.Visitas.CompromisosEnabled>}`, permiso `PermCobranzaVerPagos`.
- Config: `CompromisosEnabled bool env:"VISITAS_COMPROMISOS_ENABLED" envDefault:"false"`; `VentanaBajadaDias int env:"VISITAS_BAJADA_VENTANA_DIAS" envDefault:"92"` (DA-3; cambiarlo **obliga** a subir el epoch `visitas`, dicho en el comentario).
- **Interfaz con el documento 1** (`internal/visitas/visitas_contracts.go`), el acuerdo que ambos planes usan:
```go
// Package visitas expone a otros módulos SOLO estos tipos (CLAUDE.md §2).
package visitas

// EstadoCompromiso — tabla 5.3.2 del spec de visitas. Derivado, nunca guardado.
type EstadoCompromiso string

const (
	EstadoCumplida          EstadoCompromiso = "cumplida"
	EstadoCitaCumplida      EstadoCompromiso = "cita_cumplida"
	EstadoVigenteFutura     EstadoCompromiso = "vigente_futura"
	EstadoVigenteHoy        EstadoCompromiso = "vigente_hoy"
	EstadoIncumplida        EstadoCompromiso = "incumplida"
	EstadoCerradaSinCumplir EstadoCompromiso = "cerrada_sin_cumplir"
)

// ColumnaSugerida es lo que el compromiso pide para la lista de 3 columnas del
// documento 1. "" = sin opinión. El documento 1 aplica SIEMPRE primero su regla
// de dinero (pagado/parcial → pagados) y solo si no aplica consulta esta.
type ColumnaSugerida string

const (
	ColumnaPorVisitar ColumnaSugerida = "por_visitar"
	ColumnaNoPaga     ColumnaSugerida = "no_paga"
	ColumnaSinOpinion ColumnaSugerida = ""
)

// CompromisoVigente, uno por venta que lo tenga. Ventas sin compromiso no aparecen.
type CompromisoVigente struct {
	VentaID       int
	VisitaID      string
	Tipo          string // "promesa" | "cita"
	Estado        EstadoCompromiso
	Dia           string  // YYYY-MM-DD civil
	Hora          *string // HH:MM civil
	MontoCentavos *int64
	Columna       ColumnaSugerida
}

// AgendaReader lo satisface el app.Service de visitas.
type AgendaReader interface {
	AgendaDeZona(ctx context.Context, zonaID int, hoy string) ([]CompromisoVigente, error)
}
```
  Regla de precedencia que el documento 1 implementa en `ColumnaDe` (se le entrega textual): `si trato ∈ {pagado, parcial} → pagados; si no, si compromiso != nil && compromiso.Columna != "" → compromiso.Columna; si no → la tabla de tratos de doc 1 §3.1`. Kotlin expone lo mismo (`ColumnaSugerida`, Tarea 17).

- [ ] **Step 1: Pruebas que fallan** (HTTP con fakes): (a) cuerpo con los cinco campos → 201 y `GET` los devuelve; (b) cuerpo con un campo desconocido sigue dando 422 `visita_body_json_invalido` (el estricto se conserva); (c) cada código de 5.2.2 como 422 con su código; (d) `capacidades` con flag false/true; (e) `by-ids` sin `detalle` responde `["id"]` idéntico a hoy (comparar bytes con un golden `testdata/by-ids-sin-detalle.golden.json`); con `detalle=1` objetos con bloques e `imagen_ids`; (f) `visitas_contracts_mapper_test.go`: `domain.CompromisoVigente` → contrato, incluido `Columna`.
- [ ] **Step 2: Implementar.** `AgendaDeZona` en el servicio se implementa en la Tarea 11; aquí solo la firma con `ErrAgendaNoConfigurada` si falta el repo.
- [ ] **Step 3: Verde, rojo al revertir** (quitar los cinco campos de `CrearVisitaBody` → (a) FAIL por 422). **Step 4: Commit** `feat(visitas): contrato de visitas 3 con compromisos, capacidades y la interfaz con el tablero de desempeño`.

---

### Task 9: Predicado único y marca segura por tiempo

**Files:**
- Create: `internal/visitas/infra/visitasfb/predicado.go`, `predicado_test.go`
- Create: `internal/visitas/infra/visitasfb/marca_segura.go`, `marca_segura_test.go`

**Interfaces:**
- Produces:
```go
// predicadoZona arma EL fragmento de spec §5.4.2. Lo usan sync, digest, ids,
// by-ids de zona y la agenda. Nadie más escribe este WHERE.
func predicadoZona(alias string) string // "v.ZONA_CLIENTE_ID = ? AND v.UPDATED_AT IS NOT NULL AND v.FECHA >= ?"
func argsPredicado(zonaID int, desde time.Time) []any
func desdePorOmision(hoy domain.FechaCivil, ventanaDias int, loc *time.Location) time.Time
type MarcaSegura struct{ Upper time.Time; ServerNow time.Time; RetenidaPor time.Duration }
func LeerMarcaSegura(ctx context.Context, pool *firebird.Pool) (MarcaSegura, error)
```
  `LeerMarcaSegura`: en `RunInReadTx`, `server_now` de `SELECT CURRENT_TIMESTAMP FROM RDB$DATABASE` (+`ScanUTCTime`, como `PH:226-244`) y el inicio de la RW más vieja con la expresión que validó **E-VIS-019** sobre
  ```sql
  SELECT MIN(MON$TIMESTAMP) FROM MON$TRANSACTIONS
  WHERE MON$STATE = 1 AND MON$TRANSACTION_ID <> CURRENT_TRANSACTION AND MON$READ_ONLY = 0
  ```
  `Upper = min(server_now − 1 s, inicio_rw_mas_viejo − 1 s)`; `RetenidaPor = server_now − Upper` (métrica `visitas.sync.marca_retenida_s`). Requisito de reloj: E-VIS-012 medido; si el desfase medido > 0, el margen sube a desfase + 1 s (constante `margenMarca` con la cita E-VIS-012).

- [ ] **Step 1: Pruebas que fallan**: `TestPredicadoZona_Fragmento` (el texto exacto y el orden de `argsPredicado`), `TestDesdePorOmision`, `TestLeerMarcaSegura_SinTransaccionesAbiertasEsServerNowMenos1s` y `TestLeerMarcaSegura_ConRWAbiertaRetrocede` (e2e en dev: abrir una RW con lectura trivial, leer la marca desde otra conexión, `Upper < inicio de la RW`). Las pruebas de forma sobre los canales (`TestPredicado_LoUsanLosTresCanalesYLaAgenda`, `TestPredicado_SinInnerJoin`) van en la Tarea 10 y 11, cuando esas funciones existen. `TestNingunaRutaBorraVisitas`: búsqueda de `DELETE FROM MSP_VISITAS` y `DELETE FROM MSP_TAREAS_VISITA` en `internal/` sin `_test.go` = 0 (control positivo: la cadena sí se encuentra en un archivo de prueba creado por la propia prueba en `t.TempDir()`). `TestSinEscriturasDeDinero` (I7): búsqueda en `internal/visitas` de `(INSERT INTO|UPDATE|DELETE FROM)\s+(DOCTOS_CC|IMPORTES_DOCTOS_CC|MSP_PAGOS_\w+|MSP_SALDOS_\w+)` = 0, con control positivo de la regex.
- [ ] **Step 2: Implementar** `predicado.go` y `marca_segura.go`. Unit test de `desdePorOmision` (hoy − 92 días a las 00:00 local, en UTC).
- [ ] **Step 3: Verde y commit** `feat(visitas): predicado único de bajada y marca segura por tiempo`.

---

### Task 10: Canal de bajada de visitas por zona (sync, digest, ids, by-ids) + paridad + commit tardío (E-VIS-013)

**Files:**
- Create: `internal/visitas/infra/visitasfb/sync_repo.go`
- Create: `internal/visitas/infra/visitasfb/epoch_repo.go`
- Create: `internal/visitas/app/sync.go` (+test)
- Create: `internal/visitas/infra/visitashttp/handlers_sync.go`, `dto_sync.go`
- Modify: `internal/visitas/infra/visitashttp/routes.go`
- Create: `internal/visitas/infra/visitasfb/sync_paridad_e2e_test.go`, `sync_commit_tardio_e2e_test.go`, `sync_empate_e2e_test.go`

**Interfaces:**
- Rutas (todas `PermCobranzaVerPagos`):
  - `GET /v2/visitas/sync/zona/{zona_id}?desde=&cursor=&after_id=&limit=` → `SyncVisitasBody{Items []VisitaZonaDTO json:"items"; Cursor *string json:"cursor"; AfterID *string json:"after_id"; HasMore bool json:"has_more"; SyncEpoch int json:"sync_epoch"; ServerNow string json:"server_now"}`. `limit` 1..2000, default 500. `after_id` UUID string.
  - `GET /v2/visitas/sync/zona/{zona_id}/digest?desde=` → `{count int, hash string, max_updated_at *string}`; `hash` = hex SHA-256 de las líneas `"<ID>|<updated_at>"` ordenadas por `ID` en bytes, unidas con `\n`, donde `updated_at` es **exactamente** el string RFC3339Nano UTC que el canal de sync entrega.
  - `GET /v2/visitas/sync/zona/{zona_id}/ids?desde=&after=&limit=` → `{ids []string, has_more bool}` ordenado por `ID`, `limit` 1..10000 default 5000.
  - `GET /v2/visitas/sync/zona/{zona_id}/by-ids?desde=&ids=` → `{items []VisitaZonaDTO}`, máx 500 ids.
  - `VisitaZonaDTO` = campos de `VisitaDTO` + los cinco de compromiso + `updated_at` (string). Sin nota (spec §5.4.8 dice "columnas de la visita + compromiso"; la nota puede traer datos personales y la agenda no la necesita — **hueco del spec**, decisión: **sí** se incluye `nota` porque el detalle de la venta en el teléfono la muestra hoy para las propias; reportar al dueño).
- `desde` **obligatorio con default de servidor** (`desdePorOmision`); se aplica en el caso de uso, no en un helper compartido (trampa de `COBRANZA-SYNC.md:218-221`).
- Página vacía: `Cursor` = el recibido, `AfterID = nil`. Con filas: `Cursor` = `UPDATED_AT` de la última, `AfterID` = su `ID`.
- Los tres canales aplican el mismo `Upper` de `LeerMarcaSegura` (I4 con la marca).
- `sync_epoch`: `epoch_repo.Efectivo(ctx, "visitas", zona)` = fila de zona si existe y > 0, si no la global; error → 0 y log `visitas.sync_epoch.read_failed` (misma semántica que `cobranza/app/service.go:165`; copia, no import).

- [ ] **Step 1: E2E que fallan**

`sync_paridad_e2e_test.go` — `TestE2E_ParidadCanales_Visitas`, patrón de `ventfb/ventana_paridad_integration_test.go:431` (filas **confirmadas** con `pool.ExecContext` y `t.Cleanup` que borra sin descartar error, porque un digest en REPEATABLE READ solo ve lo confirmado; `make fb-snapshot` antes): sembrar en una zona de prueba (usar `microsipseed.ClienteEnZona`) 30 visitas: 10 dentro de la ventana, 5 con `FECHA < desde`, 5 con `UPDATED_AT` nulo, 5 de otra zona, 5 con compromiso; esperar `esperado` = las 15 que cumplen el predicado; afirmar `assert.Equal(esperado, idsDelSync)`, `== idsDeIDs`, `== idsDeByIDs(esperado ∪ las 15 que no cumplen)`, y que el `hash` del digest recalculado en la prueba desde los items del sync coincide.
`sync_empate_e2e_test.go` — `TestE2E_SyncVisitas_EmpateDe2000`: 2,000 visitas con el mismo `UPDATED_AT`; paginar con `limit=500`; exactamente 2,000 ids distintos, ninguno repetido; página final vacía devuelve el cursor recibido y `after_id = null`.
`sync_commit_tardio_e2e_test.go` — **E-VIS-013**:
```go
// T1 empieza (lectura trivial), T2 empieza, T2 inserta con UPDATED_AT=t2 y confirma,
// sync entrega la fila de T2 y deja el cursor en t2. T1 inserta con UPDATED_AT=t1 < t2
// (estampado tras su inicio) y confirma al final. El siguiente sync DEBE entregar la de T1.
func TestE2E_EVIS013_CommitTardioSeEntrega(t *testing.T) { ... }
// Control positivo: con la marca desactivada (Upper = server_now − 1 s), la misma
// secuencia NO entrega la fila de T1 — la prueba DEBE ponerse roja así.
func TestE2E_EVIS013_ControlPositivo_SinMarcaSePierde(t *testing.T) { ... }
```
La forma de forzar el orden: abrir `tx1, tx2 := pool.BeginTx(...)`, ejecutar en cada una `SELECT CURRENT_TRANSACTION FROM RDB$DATABASE`, insertar con `UPDATED_AT` explícito vía SQL de prueba (los valores `t1 < t2` se toman de `time.Now()` **después** de su lectura trivial, en ese orden: `t1` tras iniciar T1, `t2` tras iniciar T2), `tx2.Commit()`, sync, `tx1.Commit()`, sync. La variante sin marca usa una opción de prueba `syncConUpperSoloReloj` del repo (exportada solo en `export_test.go`).

Pruebas de forma (sin base): `TestPredicado_LoUsanLosTresCanales` lee los `.go` de `visitasfb` (sin `_test`) y exige que `SyncPorZona`, `DigestZona`, `IDsZona`, `ByIDsZona` contengan `predicadoZona(` y que la cadena `ZONA_CLIENTE_ID = ?` aparezca **solo** en `predicado.go` (control positivo: la búsqueda sí la encuentra en `predicado.go`); la Tarea 11 agrega `VisitasDeAgenda` a la lista. `TestPredicado_SinInnerJoin`: ninguna sentencia de esas funciones contiene `INNER JOIN` ni un `JOIN` sin `LEFT` (`COBRANZA-SYNC.md:211-214`).

Run: `FB_DATABASE=... go test ./internal/visitas/infra/visitasfb/ -run 'ParidadCanales|Empate|EVIS013|Predicado' -p 1 -count=1 -v` → FAIL (no existe).

- [ ] **Step 2: Implementar `sync_repo.go`** — consulta del sync:
```go
func (r *Repo) SyncPorZona(ctx context.Context, zonaID int, desde time.Time, cursor *time.Time, afterID *string, limit int, upper time.Time) ([]domain.VisitaZona, error) {
	q := `SELECT FIRST ? ` + selectVisitaZonaCols + ` FROM MSP_VISITAS v WHERE ` + predicadoZona("v") + ` AND v.UPDATED_AT <= ?`
	args := append([]any{limit}, argsPredicado(zonaID, desde)...)
	args = append(args, firebird.ToWallClock(upper))
	if cursor != nil {
		q += ` AND (v.UPDATED_AT > ? OR (v.UPDATED_AT = ? AND v.ID > ?))`
		c := firebird.ToWallClock(*cursor)
		args = append(args, c, c, deref(afterID))
	}
	q += ` ORDER BY v.UPDATED_AT, v.ID`
	...
}
```
(`domain.VisitaZona` = struct de lectura en `domain/visita_zona.go`: visita + `UpdatedAt time.Time`; `afterID` nil con cursor no nil = `""`, que es menor que todo UUID). Digest/IDs/ByIDs con el mismo `predicadoZona` y `UPDATED_AT <= upper`. El `updated_at` del DTO se formatea con **una** función `formatearUpdatedAt(t time.Time) string` (`t.UTC().Format(time.RFC3339Nano)`) usada por sync, by-ids y digest.
`app/sync.go`: `SyncVisitasZona(ctx, zona, desde *time.Time, cursor *time.Time, afterID *string, limit int) (PaginaVisitas, error)` = lee marca (`LeerMarcaSegura`), aplica default de `desde`, consulta, arma página, loguea `visitas.sync.pagina zona=… filas=… ms=…` y `visitas.sync.marca_retenida_s`.

- [ ] **Step 3: Verde** (incluye `TestE2E_EVIS013_ControlPositivo_SinMarcaSePierde` en **rojo esperado**: esa prueba se escribe como `assert` de que la fila **no** llega, o sea pasa cuando la variante sin marca pierde la fila; así su verde es el control positivo). Registrar E-VIS-013 MEDIDO con las dos salidas.
- [ ] **Step 4: Rojo al revertir**: quitar `AND v.UPDATED_AT <= ?` del sync → paridad o EVIS013 FAIL; cambiar `v.ID > ?` por `v.ID >= ?` → empate FAIL (repite); en digest, quitar el `upper` → paridad FAIL.
- [ ] **Step 5: Commit** `feat(visitas): bajada de visitas por zona con cursor par, marca segura y paridad de los tres canales (I4). Cierra E-VIS-013.`

---

### Task 11: Agenda del servidor y conciliación directa (§5.5)

**Files:**
- Create: `internal/visitas/infra/visitasfb/agenda_repo.go` (+e2e)
- Create: `internal/visitas/app/agenda.go` (+test)
- Create: `internal/visitas/infra/visitashttp/handlers_agenda.go`, `dto_agenda.go` (+test)

**Interfaces:**
- `GET /v2/visitas/agenda?zona_id=&hoy=` (`PermCobranzaVerPagos`; `hoy` opcional, default `FechaCivilDe(clock.Now(), BusinessTZ)`) → `{zona_id, hoy, items: [{venta_id, visita_id, tipo, estado, dia, hora, monto_centavos, columna}]}`.
- `POST /v2/visitas/agenda/conciliacion` body `{zona_id, hoy, items: [{venta_id, visita_id, estado}]}` → `{diferencias: [{venta_id, telefono: {visita_id, estado}|null, servidor: {visita_id, estado}|null}]}`; log `visitas.agenda.diferencias zona=… n=…` (sin datos personales).
- Repo (**solo lectura**, copia deliberada de lecturas de desempeño, sin importarlas):
  - `CuentasDeZona(ctx, zona int) ([]domain.CuentaCompromiso, error)` — misma fuente que `desempenofb/cuentas_repo.go:95` (`FROM MSP_SALDOS_VENTAS s`), solo `VentaID, ClienteID`, filtro por zona; **ventas de clientes con visitas en el predicado**, no solo con saldo: unión con los `IMPTE_DOCTO_CC_ID` y clientes de las visitas del predicado.
  - `PagosDeVentas(ctx, ventas []int, desde time.Time) ([]domain.PagoCompromiso, error)` — aplicados (patrón `actividades_repo.go:72-114`: `MSP_PAGOS_VENTAS` + `LEFT JOIN`s, `CANCELADO='N'`, conceptos `87327, 27969`; instante de `MSP_PAGOS_RECIBIDOS` si existe, si no `Instante = nil` y `Fecha` de `DOCTOS_CC.FECHA` — DA-7) y pendientes (`:131-168`, `Rechazado = INTENTOS > 0`, definición DA-8 fijada en la Tarea 2). En tandas de ≤ 500 ventas por `IN`.
  - `VisitasDeAgenda(ctx, zona, desde, upper)` con `predicadoZona` (prueba de forma de la Tarea 9).
- `AgendaDeZona` (implementa `visitas.AgendaReader`): lee las tres, llama `domain.DerivarCompromisos(..., hoy, firebird.BusinessTZ())`, mapea a contrato. Nota: lo que el teléfono no ve (pagos fuera de su ventana de pagos) podría derivar distinto; la conciliación directa lo detecta (por eso existe).

- [ ] **Step 1: Pruebas que fallan**: unit del servicio con fakes (un caso de cada estado, reusando 5 casos de `testdata/compromisos` cargados por la prueba: la agenda del servidor con esas entradas da el `esperado` del caso); e2e en `WithTestTransaction` con `microsipseed.VentaCredito` + `AbonoAplicado` + visitas insertadas: promesa con pago posterior → `cumplida`. Medir `PLAN` de las tres consultas con `isql` en dev y registrar en `visitas.md`; si alguna es `NATURAL` sobre una tabla grande, **detener** y proponer índice (migración aparte, permitido por §1).
- [ ] **Step 2: Implementar.** **Step 3: Verde + I7** (`TestSinEscriturasDeDinero` de la Tarea 9 sigue verde). **Step 4: Commit** `feat(visitas): agenda por zona derivada en el servidor y conciliación con el teléfono`.

---

### Task 12: Simulación Go con compromisos y bajada + mutación + suite

**Files:**
- Modify: `internal/visitas/sim/*` (P1)

- [ ] **Step 1:** Extender el mundo de P1: el teléfono modelo gana `visita_zona` y cursor `(UPDATED_AT, ID)`; acciones nuevas `bajarPagina`, `subirEpoch`, `cambiarZona`, `relleno` (reenvío con compromiso del APK nuevo), `reenvioViejo` (sin bloques), `commitTardio` (dos transacciones del fake con orden invertido), `matarEntreAplicarPaginaYGuardarCursor`. Invariantes nuevos tras **cada** acción: **I4** (los tres canales del servidor fake dan el mismo conjunto para la misma zona/desde), **I6** (el cursor persistido nunca retrocede salvo reinicio por epoch/zona, y nunca está por delante de lo aplicado), **I8** (ninguna acción de bajada cambia una fila de la tabla de salida con `Guardado = false`), **I10** (ningún reenvío deja nulo un bloque que estaba), **I5** (para el estado final, la derivación del teléfono modelo — `DerivarCompromisos` sobre su unión — es igual a la agenda del servidor fake cuando el teléfono ya bajó todo).
- [ ] **Step 2:** Control positivo por invariante nuevo: una bandera de defecto por cada uno (`defectoAfterIDCero`, `defectoBajadaEscribeSalida`, `defectoReenvioBorra`, `defectoSinMarca`) y una prueba que exige que el verificador lo cace en ≤ 200 semillas.
- [ ] **Step 3:** `make test-mutation-visitas-domain && make test-mutation-visitas-app`; gremlins además sobre `./internal/visitas/infra/visitasfb` con `TMPDIR=/Volumes/M2-1TB/.go-cache/tmp ./scripts/gremlins-gate.sh ./internal/visitas/infra/visitasfb 70 80` (predicado, marca, digest). Mutantes vivos: matar o documentar.
- [ ] **Step 4:** `go clean -testcache && golangci-lint run ./... && go test -race -short ./... && make test-firebird-all && make coverage-visitas`.
- [ ] **Step 5: Commit** `test(visitas): simulación con compromisos, bajada, epoch y commit tardío`.

---

### Task 13: Integración con el documento 1 (servidor) — solo si `ColumnaDe` ya está fusionada

**Ejecutar** cuando el plan del documento 1 haya fusionado `ColumnaDe(r ResultadoCuenta, hoy Fecha) Columna`. Si no, esta tarea espera; no se adelanta tocando `internal/desempeno` (fuera de alcance de visitas).

**Files:**
- Create: `internal/desempeno/infra/clients/visitas_agenda_client.go` (+test) — patrón de `rutas_usuarios_client.go`
- Modify: `internal/desempeno/ports/outbound/*.go` (puerto `AgendaCompromisos`)
- Modify: `internal/desempeno/domain/presentacion.go` (`ColumnaDe` recibe `*CompromisoCuenta`) — **coordinar** con el dueño del documento 1; `cobro.go` y `formas_cobro.go` siguen vedados
- Modify: `cmd/api/desempeno_wiring.go`

- [ ] **Step 1:** Prueba de dominio de desempeño (rojo): para los 8 tratos × 4 fechas de doc 1 §3.1 × {sin compromiso, cada `ColumnaSugerida`}: `pagado`/`parcial` → `pagados` **siempre** (la regla de dinero no cambia, spec §5.6); en el resto, compromiso con columna → esa columna; sin opinión → la tabla de doc 1.
- [ ] **Step 2:** Implementar puerto + cliente (`visitas.AgendaReader` → `[]domain.CompromisoCuenta`) y la precedencia. **Step 3:** Verde; equivalencia del tablero de desempeño (`equivalencia_tablero_test.go`) sigue verde. **Step 4:** Commit `feat(desempeno): la columna de la cuenta considera el compromiso vigente de visitas`.

---

### Task 14: Room v36 — `visita_zona`, `visitas_sync_state`, columnas de relleno

**Base:** `feat/visitas-compromisos` desde `main` con P1 fusionado.

**Files:**
- Modify: `core/database/.../entities/VisitEntity.kt` (+`COMPROMISO_CONFIRMADO_EN TEXT?`, `COMPROMISO_RESGUARDADO_EN TEXT?`)
- Create: `core/database/.../entities/VisitaZonaEntity.kt`, `VisitasSyncStateEntity.kt`
- Create: `core/database/.../dao/visit/VisitaZonaDao.kt`
- Create: `core/database/.../migrations/Migration35to36.kt`, test `Migration35to36Test.kt`
- Modify: `AppDatabase.kt` (version 36, dos entidades, `visitaZonaDao()`, `MIGRATION_35_36`)

**Interfaces:**
- `VisitaZonaEntity` (tabla `visita_zona`, PK `ID`, índices `ZONA_CLIENTE_ID`, `IMPTE_DOCTO_CC_ID`, `CLIENTE_ID`): `ID, CLIENTE_ID, COBRADOR_ID: Int?, COBRADOR: String?, FECHA: String, TIPO_VISITA: String, ZONA_CLIENTE_ID: Int, IMPTE_DOCTO_CC_ID: Int?, NOTA: String?, PROMESA_VENTA_ID: Int?, PROMESA_FECHA: String?, PROMESA_MONTO_CENTAVOS: Long?, CITA_FECHA: String?, CITA_HORA: String?, UPDATED_AT: String` (string del servidor tal cual: el digest lo hashea).
- `VisitasSyncStateEntity` (tabla `visitas_sync_state`, PK `ZONA`): `ZONA: Int, CURSOR: String?, AFTER_ID: String?, EPOCH: Int?`.
- `VisitaZonaDao` (abstract class):
  - `@Transaction open suspend fun aplicarPagina(zona: Int, items: List<VisitaZonaEntity>, cursor: String?, afterId: String?)` — `upsertAll(items)` + `guardarEstado(zona, cursor, afterId)` **en la misma transacción**; si `cursor == null` exige `afterId == null` (`require`).
  - `@Transaction open suspend fun reiniciarZona(zonaNueva: Int)` — `DELETE FROM visita_zona WHERE ZONA_CLIENTE_ID <> :zonaNueva` + estado de `zonaNueva` con `CURSOR = NULL, AFTER_ID = NULL` (EPOCH se conserva).
  - `@Transaction open suspend fun reiniciarPorEpoch(zona: Int)` — `DELETE FROM visita_zona` + `CURSOR = NULL, AFTER_ID = NULL` (EPOCH **no** se persiste aquí: regla "solo al terminar").
  - `suspend fun guardarEpoch(zona: Int, epoch: Int)`; `suspend fun estado(zona: Int): VisitasSyncStateEntity?`; `suspend fun idsYUpdatedAt(zona: Int, desde: String): List<IdYUpdatedAt>`; `suspend fun borrar(ids: List<String>)` (troceado a 900); `suspend fun podarFueraDeVentana(desde: String)` = `DELETE FROM visita_zona WHERE FECHA < :desde`.
  - Ningún método de este DAO menciona la tabla `Visit` (prueba de forma).
- `VisitDao` gana: `confirmarCompromiso(id, ahora)`, `resguardarCompromiso(id, ahora)`, `compromisosPorRellenar(): List<String>` (`SELECT ID FROM Visit WHERE (PROMESA_FECHA IS NOT NULL OR CITA_FECHA IS NOT NULL) AND COMPROMISO_CONFIRMADO_EN IS NULL AND COMPROMISO_RESGUARDADO_EN IS NULL ORDER BY FECHA, ID`). **Hueco del spec**: §5.2.1 habla de `COMPROMISO_PENDIENTE = 1` y §5.2.5 de `COMPROMISO_CONFIRMADO_EN`; se usa solo la segunda (+ su par resguardado), porque la primera es derivable.

- [ ] **Step 1: Pruebas que fallan**: migración 35→36 (tablas nuevas existen, `Visit` conserva filas y las dos columnas nulas); DAO: `aplicarPagina` + matar el proceso a la mitad = ni items ni cursor (prueba que lanza dentro del `@Transaction` y verifica que nada quedó); `reiniciarZona` no toca `Visit` (sembrar `Visit` pendiente y verificar igualdad campo a campo antes y después: **I8**); `cursor == null` con `afterId != null` lanza.
- [ ] **Step 2: Implementar** + exportar `36.json`. **Step 3: Verde** (`./gradlew --no-parallel :core:database:testDebugUnitTest --rerun-tasks`). **Step 4: Commit** `feat(database): Room v36 con la agenda de zona separada de la tabla de salida (I8)`.

---

### Task 15: Capacidades, cuerpo v3, existencia completa con compromiso y relleno

**Files:**
- Create: `app/.../core/visitas/capacidades/CapacidadesDeVisitas.kt` (+test)
- Modify: `app/.../data/api/services/visits/V2VisitsApi.kt` (`capacidades()`, `CrearVisitaBody` + 5 campos nulables, `VisitaDetalleDTO` + 5 campos + `updated_at`, `visitasExistentesPorIdsDetalle(ids, detalle = 1)`)
- Modify: el archivo donde vive `toCrearVisitaBody()` (localizar con `grep -rn "fun VisitEntity.toCrearVisitaBody\|toCrearVisitaBody()" app/src/main`)
- Modify: `core/common/.../visitas/envio/ExistenciaCompleta.kt` (+compromiso)
- Modify: `app/.../workers/PendingVisitsWorker.kt` (camino v3)
- Modify: `app/.../workers/VisitsReconcileWorker.kt` (refresco de capacidades cada 6 h y encolado del relleno)

**Interfaces:**
- `data class Capacidades(val contrato: Int, val compromisos: Boolean)`; `CapacidadesDeVisitas.actual(): Capacidades` (SharedPreferences `visitas_capacidades`, default `Capacidades(2, false)`); `suspend fun refrescar(api)`: 200 → guarda; **404 → `Capacidades(2, false)`**; otro error → conserva la vigente; `necesitaRefresco(ahora)` si pasaron ≥ 6 h. `fun mandaCompromisos() = contrato >= 3 && compromisos`.
- `VisitEntity.toCrearVisitaBody(conCompromisos: Boolean)`: si `false`, los cinco campos van `null` (Gson los omite: `serializeNulls` apagado — verificar en `RetrofitClientFactory`/`GsonConverterFactory.create()` sin builder: por omisión **no** serializa nulos; prueba que lo fija).
- `ExistenciaCompleta`: `existenciaCompleta(enviada, respuesta, fotosPendientes, codigoGet, compromisoLocal: CompromisoLocal? = null)`, `VisitaEnServidor` gana `compromiso: CompromisoLocal?`; si `compromisoLocal` tiene un bloque y el servidor no lo tiene igual → `INCOMPLETA`.
- Worker v3: preflight "ya entregada" = `CONFIRMADA_EN != null && (sin compromiso || COMPROMISO_CONFIRMADO_EN != null || COMPROMISO_RESGUARDADO_EN != null || !mandaCompromisos())`. Si `CONFIRMADA_EN != null` y falta el compromiso (relleno): manda con compromisos; 2xx con eco → `confirmarCompromiso`; 422 `visita_compromiso_distinto` con custodia → `resguardarCompromiso` (la visita base sigue confirmada); sin custodia → reintenta.

- [ ] **Step 1: Pruebas que fallan** (MockWebServer + worker): `apkNuevoContraApiViejaNoMandaCampos` (capacidades 404 → el cuerpo no contiene `promesa_fecha`); `conCapacidadMandaLosCinco`; `rellenoConfirmaSoloElCompromiso`; `rellenoDistintoConCustodiaSeResguardaSinDesconfirmarLaVisita`; `existenciaSinCompromisoNoSuelta` (spec §4.2-5); `capacidadesErrorConservaLaVigente`; `gsonNoSerializaNulos`.
- [ ] **Step 2: Implementar.** `VisitsReconcileWorker`: si `necesitaRefresco` → `refrescar`; si `mandaCompromisos()` → `compromisosPorRellenar().forEach(encolar)` (mismo nombre único y `KEEP`). **Step 3: Verde, rojo al revertir** (quitar el gate de capacidad → `apkNuevoContraApiVieja…` FAIL). **Step 4: Commit** `feat(visitas): contrato 3 negociado, compromiso en el envío y relleno sin soltar sin prueba`.

---

### Task 16: Sync de visitas por zona en el teléfono (cursor par, epoch, cambio de zona)

**Files:**
- Create: `app/.../data/api/services/visits/VisitasZonaSyncApi.kt`
- Create: `app/.../core/sync/visitas/VisitasZonaSyncManager.kt`, `VisitasZonaSyncManagerTest.kt`
- Create: `app/.../core/visitas/agenda/KillSwitchAgenda.kt` (`config/visitas_compromisos`, campo `bajada`, **default `true`** solo si el documento existe con `bajada: true`; ausente o inválido = `false` hasta el paso 7 de §11 — la bajada es nueva, su modo seguro es apagada)
- Modify: `app/.../workers/VisitsReconcileWorker.kt` (llama al manager tras el reencolado) y el suscriptor SSE de visitas (`core/sync/visitas/VisitasSseSubscriber.kt`) para disparar una corrida

**Interfaces:**
- `class VisitasZonaSyncManager(api, dao: VisitaZonaDao, zonaActual: () -> Int?, killSwitch: () -> Boolean, telemetria)`: `suspend fun sincronizar(): ResultadoSync`.
- Reglas (copia de `CobranzaSyncManager`; leer `CobranzaSyncManagerTest` y replicar los nombres): un reinicio por corrida; epoch persistido solo con `has_more == false`; epoch inválido (`null`, `0`, negativo) = apagado; epoch que retrocede replica; epoch que cambia a media paginación no se persiste; página vacía → **no** toca `AFTER_ID` (`afterId == null` = sin posición nueva); `desde` se envía en **todas** las páginas (`syncEnviaDesdeEnTodasLasPaginas`); `desde` = hoy − 92 días (la misma constante que el servidor; si el servidor cambia V sube epoch).
- Pruebas obligatorias con estos nombres (las de `COBRANZA-SYNC.md` §2–§3 en su versión de visitas): `segundaCorridaRetomaDelAfterIdPersistidoConTodoElLoteEmpatado`, `paginaVaciaNoReiniciaElAfterIdPersistido`, `corridaInterrumpidaRetomaDesdeLaUltimaPaginaAplicada`, `replayPorEpochReiniciaElAfterIdJuntoConElCursor`, `cambioDeZonaNoTocaVisitYDejaCursorYAfterIdNulos`, `epochNuevoLimpiaElCursorYReplicaDesdeElInicio`, `epochIgualNoReplica`, `epochNoSePersisteHastaQueElReplayTermina`, `servidorSinEpochNoReplicaNiPierdeLaGeneracionAplicada`, `epochCeroSeIgnoraYNoEntraEnBucleDeReplays`, `epochQueRetrocedeReplicaUnaSolaVez`, `epochQueCambiaAMediaPaginacionNoSePersiste`, `syncEnviaDesdeEnTodasLasPaginas`, `resyncConVisitasPendientesNoTocaVisit` (**I8**), `killSwitchApagadoNoBaja`, `itemConCamposNulosNoRevientaElSync` (Gson).

- [ ] **Step 1:** Escribir las 16 pruebas (MockWebServer con respuestas encoladas; Room en memoria) → FAIL.
- [ ] **Step 2:** Implementar. **Step 3:** Verde; rojo al revertir dos de ellas (`afterId = items.lastOrNull()?.id ?: ""` → `paginaVacia…` FAIL; `guardarEpoch` dentro del bucle → `epochNoSePersiste…` FAIL). **Step 4:** Commit `feat(visitas): bajada de la agenda por zona con cursor par y generación, sin tocar lo pendiente (I6, I8)`.

---

### Task 17: `Compromisos.kt` + casos compartidos en Kotlin + unión local

**Files:**
- Create: `core/common/src/main/kotlin/com/example/msp_app/core/common/visitas/domain/Compromisos.kt`
- Create: `core/common/src/test/resources/compromisos/` (copia de `msp-api/testdata/compromisos/`)
- Create: `core/common/src/test/kotlin/.../visitas/domain/CompromisosCasosCompartidosTest.kt`, `JuegoDeCompromisosVersionTest.kt`
- Create: `scripts/casos-compromisos.sh`
- Create: `app/.../core/visitas/agenda/AgendaLocal.kt` (+test)

**Interfaces:**
- Kotlin espejo exacto de Go: `enum class EstadoCompromiso(val codigo: String)`, `enum class ColumnaSugerida(val codigo: String) { POR_VISITAR("por_visitar"), NO_PAGA("no_paga"), SIN_OPINION("") }`, `data class CompromisoVigente(...)`, `fun derivarCompromisos(cuentas, visitas, pagos, hoy: LocalDate, zona: ZoneId): Map<Int, CompromisoVigente>`, `fun columnaSugeridaDe(c: CompromisoVigente): ColumnaSugerida`, `fun alcanceDe(tipo: String, tieneCita: Boolean)` reutilizando `TipoVisitaCatalogo.alcanceDe` (`TipoVisitaCatalogo.kt:233-234`, que ya es la fuente que desempeño copió). Archivo **nuevo**, no dentro de `EstadoCuentaDeriver.kt` (vedado).
- `scripts/casos-compromisos.sh copiar|verificar` — copia de `${MSP_API_DIR:-../msp-api}/testdata/compromisos`; `verificar` = mismos bytes y mismo `VERSION` (`ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256`).
- `AgendaLocal.derivar(zona, hoy)`: visitas = `Visit` (todas las propias, pendientes o no) ∪ `visita_zona`, **gana `Visit` ante el mismo id**; pagos = `Payment` de esas ventas (lectura con métodos existentes de `PaymentDao`; **no** se modifica `dao/payment/**`), contando la captura local pendiente salvo rechazada (`DOCTO_CC_ID = -1`, DA-8 según E-COB-031); `local_pendiente` de los casos mapea a `GUARDADO_EN_MICROSIP = false && DOCTO_CC_ID != -1`.

- [ ] **Step 1:** `bash scripts/casos-compromisos.sh copiar`; pruebas (rojo): `VERSION coincide con el SHA-256 de los casos`, `cada caso contra la derivacion`, `cobertura minima`, `unionGanaVisitAnteElMismoId`, `capturaLocalPendienteCierraLaPromesa`, `capturaRechazadaNoCierra`.
- [ ] **Step 2:** Implementar. **Step 3:** Verde (30 casos). Prueba de paridad Go↔Kotlin con `rapid` del lado Go: `TestCompromisos_RapidGeneraCasosParaKotlin` escribe 200 historiales a `testdata/compromisos-generados/` (no versionados, `.gitignore`) con el resultado Go; una prueba Kotlin `generadosCoincidenConGo` los lee si `MSP_API_DIR` está definido; **toda discrepancia se congela** como caso nuevo `NNN-generado-<semilla>.json` en `testdata/compromisos/` (y se regenera `VERSION` en los dos repos).
- [ ] **Step 4:** PIT acotado a `com.example.msp_app.core.common.visitas.domain.*` (configuración de P1 Tarea 17). **Step 5:** Commit `feat(visitas): derivación de compromisos en el teléfono amarrada a los casos del servidor (I5)`.

---

### Task 18: Inventario de la agenda (digest/ids/by-ids) y conciliación directa diaria (capa 5)

**Files:**
- Create: `app/.../core/sync/visitas/VisitasZonaReconciler.kt` (+test)
- Modify: `app/.../workers/VisitsDailyReconcileWorker.kt`
- Modify: `app/.../core/visitas/envio/TelemetriaDeVisitas.kt` (claves nuevas)

**Interfaces:**
- `VisitasZonaReconciler.reconciliar(zona, desde)`: digest local (mismo hash de la Tarea 10 sobre `idsYUpdatedAt`) vs servidor; si difiere: `/ids` completo → faltantes por `by-ids` de zona (tandas de 500) → `upsert`; ids locales que el servidor no lista **y** cuya `FECHA >= desde` → se borran de `visita_zona` (nunca de `Visit`); ids con `FECHA < desde` → `podarFueraDeVentana(desde)`. Una consulta fallida no decide nada.
- Conciliación directa (spec §4.6-2): `POST /v2/visitas/agenda/conciliacion` con `AgendaLocal.derivar(...)` resumido a `(venta, visita_id, estado)`; respuesta → `visitas.conciliacion.diferencias_agenda` (conteo) y log de ventas con diferencia (solo ids numéricos de venta).
- Claves nuevas de telemetría: `visitas.conciliacion.diferencias_agenda`, `visitas_sync.cursor_stalled` (cierre contra cierre entre corridas, en memoria de la telemetría, `COBRANZA-SYNC.md:495-504`), `visitas.reconcilia.incompleta`.

- [ ] **Step 1:** Pruebas (rojo): `digestIgualNoPideIds`, `digestDistintoRescataFaltantes`, `idQueElServidorYaNoListaSeBorraSoloDeVisitaZona`, `consultaFallidaNoBorraNada`, `conciliacionSinDiferenciasReportaCero`, `cursorStalledComparaCierreContraCierre` (con el defecto "comparar arranque contra cierre" como control positivo que da falso negativo).
- [ ] **Step 2:** Implementar. **Step 3:** Verde. **Step 4:** Commit `feat(visitas): inventario de la agenda y conciliación diaria teléfono-servidor`.

---

### Task 19: Simulación Kotlin ampliada (pull + push + epoch con pendientes)

- [ ] **Step 1:** Extender `MundoSimulado` (P1 Tarea 17) con el servidor de bajada (sync/digest/ids/by-ids sobre el mismo mapa), epoch, cambio de zona, relleno, reenvío del APK viejo y muerte entre aplicar página y guardar cursor. Invariantes por paso: I4 (del lado servidor simulado), I6, I8, I10, y I5 al final.
- [ ] **Step 2:** Controles positivos (las cuatro banderas de defecto de la Tarea 12, versión Kotlin). **Step 3:** Historial JSONL y verificador Go (`TestVerificarHistorialesEnDisco` extendido con eventos `pull`, `cursor`, `epoch`). **Step 4:** Commit `test(visitas): simulación del teléfono con bajada, epoch y relleno`.

---

### Task 20: UI de §5.7 — citas y promesas visibles (B, C, E, F)

**Base:** `main` con el trabajo Android del documento 1 y `feat/badges-blancos-sin-al-corriente` fusionados (ambos tocan `EstadoCuentaUi.kt`; el documento 1 reemplaza segmentos por columnas en `SegmentoDeCobranza.kt`). Si no están fusionados, esta tarea espera.

**Files:**
- Create: `feature/pagos/src/main/kotlin/com/example/msp_app/feature/pagos/ui/PresentacionDeCompromiso.kt` (+test)
- Create: `feature/pagos/src/test/resources/presentacion-compromisos/*.json` (casos de presentación)
- Modify: `feature/pagos/.../ui/EstadoCuentaUi.kt:122-135, 168-260` (F), `components/PiezasDeLaLista.kt:438-478` (B, C), `DetalleVentaScreen.kt:401,429` (E), y el lugar donde el documento 1 decide la columna (A ya lo cubre la derivación sin ventana)

**Interfaces:**
- `data class CompromisoEnPantalla(val titulo: String, val detalle: String, val trato: TratoDelEstado, val icono: …)`; `fun presentacionDe(c: CompromisoVigente?, hoy: LocalDate): CompromisoEnPantalla?`.
- Reglas: **F** — una cita sin hora es una **cita con su día**: trato `CITA` (violeta), título `"Cita"`, detalle `"lun 20 oct, sin hora"`; nunca `REGRESAS`/"Sin hora, regresas". **B** — el chip de dinero ("Abonó parcial") y el compromiso se muestran **juntos**; la columna la sigue decidiendo el dinero (doc 1). **C** — en venta sin abonos, la tarjeta muestra el compromiso además de "Primer abono $X". **E** — renglón "Próxima cita / Promesa" en el detalle con día, hora y monto. Textos minimalistas (memoria `feedback_ui_text_minimalista`; nunca "ciclo").
- Casos de presentación compartidos (nuevo juego, solo Kotlin): uno por estado × {con hora, sin hora} × {con abonos, sin abonos}.

- [ ] **Step 1:** Pruebas puras (rojo): `citaSinHoraEsCitaConSuDia`, `citaSinHoraNoEsRegresas`, `chipDeDineroYCompromisoJuntos`, `ventaNuevaMuestraCompromiso`, `detalleTieneRenglonDeCompromiso`, `reemplazadaNoSeMuestra`, `cerradaSinCumplirNoSeMuestra`.
- [ ] **Step 2:** Implementar las funciones puras y conectarlas a los composables (los composables solo llaman `presentacionDe`). **Step 3:** Roborazzi: grabar (`./gradlew --no-parallel :feature:pagos:recordRoborazziDebug`), revisar el `git diff` de los PNG con PIL (memoria `feedback_roborazzi_compare_viejos_pil_rgba`: borrar `build/outputs/roborazzi` antes), y verificar contra el mock si el dueño lo dio (memoria `feedback_reporte_cobranza_fidelidad_mockup`); `verifyRoborazziDebug` verde. **Step 4:** Medición en el teléfono del dueño (DEPENDE DEL DUEÑO): repetir la consulta de spec §5.7 y una captura de pantalla de las tres ventas Y00002184, Y00002215, Y00001895. **Step 5:** Commit `fix(pagos): la cita y la promesa se ven en la tarjeta y el detalle, también sin hora y sin abonos (§5.7)`.

---

### Task 21: Escritorio — columna "Compromiso" en Cuentas

**Base:** `feat/agenda-compromisos` desde `feat/desempeno` (worktree `/Volumes/M2-1TB/Developer/sistema-cobro-web-desempeno`, del dueño: confirmar). Sin conflicto con `fix/mapas-y-permiso-fotos` (no toca cuentas). Coordinar con el escritorio del documento 1 (también cambia la lista de Cuentas a 3 columnas): esta tarea va **después** de la suya.

**Files:**
- Modify: `src/modules/desempeno/application/ports/DesempenoPort.ts` (`obtenerAgenda(zonaId: number, hoy: string | null, signal?: AbortSignal): Promise<Agenda>`)
- Modify: `src/modules/desempeno/infrastructure/http/dtos.ts`, `HttpDesempenoAdapter.ts`, `infrastructure/mappers/dtoADominio.ts`
- Create: `src/modules/desempeno/domain/entities/Agenda.ts`
- Create: `src/modules/desempeno/presentation/hooks/useAgenda.ts`
- Modify: `src/modules/desempeno/components/cuentas/TablaCuentas.tsx`, `TarjetaCuenta.tsx`, `CuentasTab.tsx`
- Modify: `src/test/msw/handlers/desempeno.ts`, `src/test/msw/fixtures/desempeno.ts`, `application/__tests__/fakeDesempenoPort.ts`
- Test: `HttpDesempenoAdapter.test.ts`, `components/cuentas/__tests__/CuentasTab.test.tsx`

**Interfaces:**
- DTO: `type AgendaDTO = { zona_id: number; hoy: string; items: { venta_id: number; visita_id: string; tipo: "promesa" | "cita"; estado: "cumplida" | "cita_cumplida" | "vigente_futura" | "vigente_hoy" | "incumplida" | "cerrada_sin_cumplir"; dia: string; hora: string | null; monto_centavos: number | null; columna: "por_visitar" | "no_paga" | "" }[] }`; ruta `"/visitas/agenda"` (el cliente ya tiene base `/v2`).
- Entidad: `type CompromisoDeVenta = { readonly ventaId: number; readonly tipo: "promesa" | "cita"; readonly estado: EstadoCompromiso; readonly dia: string; readonly hora: string | null; readonly montoCentavos: number | null }`; `type Agenda = ReadonlyMap<number, CompromisoDeVenta>`. El mapper rechaza un `estado` desconocido (lo convierte en `DomainError` de contrato, patrón de `dtos.ts`).
- UI: columna "Compromiso" (texto corto: `Promesa 20 oct · $150`, `Cita 18 oct 16:30`, `Cita 18 oct`, `Incumplida 12 oct`), sin recalcular nada (memoria `feedback_logica_siempre_en_servidor`). `zonaId` sale de la fila del tablero (`Tablero.ts:61-62`) que abrió la ficha.

- [ ] **Step 1:** Pruebas (rojo): adaptador llama `/visitas/agenda?zona_id=7` y mapea; estado desconocido → error de contrato; `CuentasTab` con MSW pinta "Promesa 20 oct · $150" en la fila de la venta 100 y nada en la venta sin compromiso; cerrada_sin_cumplir no se pinta.
- [ ] **Step 2:** Implementar (patrón `useCuentas` + `useCargaRemota`). **Step 3:** `npm test && npm run lint && npx tsc` verdes. **Step 4:** Commit `feat(desempeno): columna de compromiso en Cuentas desde la agenda del servidor`.

---

### Task 22: Caos con Toxiproxy (P2) y estado estable

- [ ] **Step 1:** Repetir el script de P1 Tarea 18 con dos fases nuevas: `latency` de 5 s en `api_firebird` durante la bajada (marca retenida) y `reset_peer` a mitad de paginación; capturar 30 visitas con promesa/cita y cambiar de zona una vez a mitad.
- [ ] **Step 2:** Hipótesis de estado estable tras 30 min sin fallas: paridad = 0 (digest local = digest servidor para la zona), conciliación directa con 0 diferencias, ninguna visita pendiente > 30 min, `Visit` intacta tras el cambio de zona (comparar un volcado antes/después por el depurador remoto), `MON$STATEMENTS` en estado 2 = 0. Registrar como entrada E-VIS nueva.
- [ ] **Step 3:** Commit `test(visitas): caos en dev de la bajada de agenda`.

---

### Task 23: Agente adversario (P2)

- [ ] Igual que P1 Tarea 19, con el encargo: "pierde, duplica o corrompe una visita **o un compromiso**; haz que el teléfono y el servidor deriven estados distintos para el mismo historial; haz que un resync o un cambio de zona toque `Visit`; haz que el cursor salte una fila." Cada escenario → semilla o prueba; si alguno pasa, se detiene y se arregla con su rojo al revertir.

---

### Task 24: Compuerta "después" de P2 y runbook de los pasos 4–8

- [ ] **Step 1: Suites completas** (los tres repos) como en P1 Tarea 21 + `bash scripts/casos-compromisos.sh verificar` (mismo `VERSION` en msp-api y msp-app-kt) + `npm test` en el escritorio.
- [ ] **Step 2: Compuerta "después"** — agente independiente: (1) rojo al revertir de cada arreglo (H4, H5, H6, I4 paridad, I6, I8, I10, marca segura, empate 2,000, página vacía conserva `AFTER_ID`, epoch con pendientes); (2) **enumerar** con grep y control positivo los consumidores de `MSP_VISITAS` (`visitasfb/repo.go`, `sync_repo.go`, `agenda_repo.go`, `desempenofb/actividades_repo.go:170-198` — que ahora ve columnas nuevas pero selecciona explícitas —, la bandeja, el escritorio), de `Visit` en Room (rehacer la búsqueda de `getPendingVisits`/`Visit`; spec §9.3 lista `Home.kt:166,202`, `AuthViewModel.kt:128`, `VisitsViewModel.kt:34-39`, `RoomPendingVisitsStore.kt:22`, `PendingWorkSyncFactory.kt:100`), de `visita_zona`, y de `ColumnaDe`; (3) decir en voz alta lo no verificado (por ejemplo: plan real en producción con datos de producción, desfase de reloj si Go y Firebird se separan, teléfonos Android ≤ 11 con tope de 999 parámetros en `IN`).
- [ ] **Step 3: Runbook** en `docs/evidencia/visitas.md` `## Runbook P2` con spec §11 pasos 4–8: snapshot; `000066` y `000067` **a mano** fuera de horario; relleno en tandas con `ATORADAS` tras cada una y vigilancia del sync de pagos (alerta §12-4); binario con `VISITAS_COMPROMISOS_ENABLED=false`; verificación "digest = conteo Q8 por zona"; bandera en `true`; APK canario (teléfono del dueño) con `config/visitas_compromisos.bajada = true` (**lo crea el dueño**); 7 días con cero diferencias antes de la flota. Restricción de orden: el APK con cursor par en la flota **antes** de cualquier subida del epoch `visitas` (spec §11, último párrafo).
- [ ] **Step 4: Commit** `docs(evidencia): compuerta después y runbook de P2`.

---

## Self-review (2026-10-16)

- **Cobertura del spec P2:** §5.1 (Tarea 6), §5.1.1 (1, 6, 7), §5.1.2 (6), §5.2.1 (8, 15), §5.2.2 (3, 8), §5.2.3 (4, 7), §5.2.4 (5 de P1 + 8), §5.2.5 (15), §5.3 (5, 17), §5.3.3 (17), §5.3.4 (5, 17), §5.4.1–§5.4.3 (9, 10, 16), §5.4.4 (1, 9, 10), §5.4.5 (6, 10, 16), §5.4.6 (10), §5.4.7 (9), §5.4.8 (14, 16), §5.5 (11, 18), §5.6 (8, 13), §5.7 (5 casos 028–030, 20), §4.6-2 (18), §7.2 I7 (9), §8 (12, 19, 22, 23), §9 (1, 2, 24), §10 Q2/Q3/Q5/Q7/Q8 (2), §11 pasos 4–8 (24), §12 (7, 10, 18).
- **Huecos del spec encontrados:** (1) visita con promesa **y** cita a la vez: se trata como cita (regla escrita en la Tarea 5); (2) `cita_cumplida` "por visita posterior" choca con "vigente = la visita más reciente": se resuelve con el punto 5 de la regla; (3) `COMPROMISO_PENDIENTE` (§5.2.1) vs `COMPROMISO_CONFIRMADO_EN` (§5.2.5): se usa el segundo + `COMPROMISO_RESGUARDADO_EN`, y el relleno no puede reusar `CONFIRMADA_EN` como "ya entregada" (si no, nunca se rellenaría); (4) `DerivarCompromisos` del spec recibe `tareas`, pero ninguna regla de 5.3.2 las usa: la firma las omite y las tareas se derivan aparte (P3); (5) `MON$TIMESTAMP` es `TIMESTAMP WITH TIME ZONE` en Firebird 4+: la marca segura depende de una medición nueva (E-VIS-019); (6) `nota` en `visita_zona`: el spec no la menciona; se incluye y se reporta; (7) `by-ids?detalle=1` con dos formas de respuesta no cabe en una operación Huma: se monta una ruta chi que despacha por el query; (8) el `scan` actual de `MSP_VISITAS` no tolera nulos en columnas que la tabla permite nulas; la bajada leerá filas legadas.
- **Placeholders:** ninguno de los prohibidos. Donde el ejecutor debe localizar código (archivo de `toCrearVisitaBody`, la fuente de cuentas en `cuentas_repo.go:95`) se da el `grep` o la línea.
- **Tipos:** `FechaCivil`, `HoraCivil`, `Promesa`, `Cita`, `DecisionReenvio`, `CompararReenvio`, `DerivarCompromisos`, `CompromisoVigente`, `EstadoCompromiso`, `ColumnaSugerida`, `ColumnaSugeridaDe`, `AgendaReader.AgendaDeZona`, `predicadoZona`, `LeerMarcaSegura`, `MarcaSegura`, `VisitaZonaEntity`, `VisitasSyncStateEntity`, `VisitaZonaDao.aplicarPagina/reiniciarZona/reiniciarPorEpoch`, `Capacidades`, `CapacidadesDeVisitas`, `VisitasZonaSyncManager`, `VisitasZonaReconciler`, `AgendaLocal` consistentes entre tareas y con P3.
