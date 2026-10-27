# Visitas P3 — Tareas de oficina y avisos: plan de implementación

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Que la oficina asigne "visitar esta venta" desde el escritorio, que la tarea llegue a toda la zona por el canal con cursor (FCM solo despierta), que se cierre sola (atendida / no atendida / cancelada, derivado y nunca guardado), y que el teléfono avise con notificación inmediata y alarmas locales reprogramables desde Room.

**Architecture:** Tabla greenfield `MSP_TAREAS_VISITA` (`000068`, CLAUDE.md §1 sin excepciones) dentro del módulo `internal/visitas`; endpoints de escritorio (`POST /v2/visitas/tareas`, `/cancelar`, `GET /v2/visitas/tareas`) con permiso nuevo `visitas:asignar_tareas`; canal de bajada `/v2/visitas/sync/tareas/zona/{zona_id}` + digest/ids/by-ids con la **misma** maquinaria de P2 (predicado único, marca segura, cursor par, epoch `tareas`); estado de la tarea derivado con casos compartidos; push FCM v1 al tema `zona-<id>` **después del commit**, solo `{"tipo":"sync"}`. Teléfono: Room v37 (`tarea_zona`, `tareas_sync_state`, `aviso_emitido`, `aviso_programado`), sync de tareas sobre un motor de cursor par extraído del de P2, aviso inmediato idempotente, alarmas como proyección de una tabla, receptores de reinicio/actualización/hora, `firebase-messaging` como despertador, kill-switch `config/visitas_avisos`. Escritorio: "Asignar visita" y su lista.

**Tech Stack:** Go 1.25, Huma, Firebird 5, `firebase.google.com/go/v4/messaging` (el SDK ya es dependencia directa, `go.mod`; hoy solo se usa auth); Kotlin, Room, WorkManager (expedited), AlarmManager, NotificationManager, `com.google.firebase:firebase-messaging` (BOM `33.12.0`, nuevo — DA-14 aprobado), Robolectric (`ShadowAlarmManager`); React 18 + Vitest + MSW.

**Spec:** `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/specs/2026-10-15-visitas-compromisos-avisos-design.md` (§6 completo, §5.4 como base del canal, §7, §8, §9.2 E-INF, §11 pasos 9–11, §12 alertas 6, §13 DA-2, DA-10, DA-11, DA-13, DA-14, DA-15).

**Depende de:** P2 fusionado en los tres repos (predicado, marca segura, `epoch_repo`, `VisitasZonaSyncManager`, `AgendaLocal`, casos `testdata/compromisos/`, migración `000067` con la fila `tareas`).

---

## Global Constraints

- Todas las de P1 y P2 (guardia, sin `--no-verify`, sin push/deploy, sin atribución, producción solo lectura, Gradle `--no-parallel`, Gson nulable, sin `@Singleton` sobre servicios con baseURL, mensajes en español minúsculas).
- **`MSP_TAREAS_VISITA` greenfield, CLAUDE.md §1 sin excepciones:** `ID CHAR(36) ASCII` lo genera el **escritorio** al abrir el formulario; `CREATED_AT`, `UPDATED_AT`, `UPDATED_BY`, `CANCELADA_AT`, `CANCELADA_BY` desde Go; `UPDATED_AT` estampado **dentro** de la transacción tras una lectura trivial (spec §5.4.4). **El estado no tiene columna.**
- Migración siguiente libre esperada: **000068** (P2 usó 000066 y 000067); si el documento 1 tomó números, el siguiente libre al escribirla.
- **FCM solo despertador** (spec §6.4): mensaje de **datos** al tema `zona-<id>`, `collapse_key = tareas-zona-<id>`, prioridad alta, TTL 4 h, payload exactamente `{"tipo":"sync"}`; se manda **tras el COMMIT**, nunca dentro; si falla: log + métrica, sin reintentos en bucle. El teléfono siempre hace pull.
- **Teléfono:** `setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST)`, **nunca** `DROP_WORK_REQUEST`. Declarar `SCHEDULE_EXACT_ALARM`, **nunca** `USE_EXACT_ALARM`. `POST_NOTIFICATIONS` se pide en el alta. Alarmas: comodidad, nunca fuente de verdad.
- **Horas de aviso:** 30 min antes de cita o tarea con hora; **8:00** del día (DA-2) para promesa, cita sin hora (DA-15) y tarea sin hora.
- **"No atendida 14 días"** se cuenta desde la `FECHA` de la tarea (DA-11).
- **FCM por tema de zona** (DA-10), sin tabla de tokens.
- **Permiso** `visitas:asignar_tareas` para oficina y dueño (DA-13). Un permiso nuevo se sincroniza solo al arrancar y se concede a `super_admin` (memoria `reference_msp_api_permission_boot_sync`); concederlo a otros roles es manual en `MSP_ROLES_PERMISOS` (**dueño**).
- **Credencial FCM** (cuenta de servicio) la instala **el dueño** en el servidor (memoria `feedback_no_manejar_credenciales_del_usuario`); el agente nunca la lee, copia ni pide su contenido.
- **Evidencia:** spec E-INF-017 → **E-INF-024** (alarmas tras reinicio/actualización/revocación), spec E-INF-018 → **E-INF-025** (FCM: llegada y colapso en el canario, 7 días). Nuevos hechos de teléfono desde el siguiente libre de `app-android.md` al empezar.
- **Room:** v36 (P2) → **v37**.

## Review Focus

1. **Doble clic o reintento del escritorio al asignar**: el mismo UUID (generado al abrir el formulario) → 201 igual, una sola fila. Pruebas en Tareas 3 (servidor) y 9 (escritorio: `dobleClicNoCreaDosTareas`).
2. **La misma tarea llega por push, por el periódico y por un resync**: una sola notificación "Visita asignada". Prueba `mismaTareaPorTresCaminosNotificaUnaVez` en la Tarea 12.
3. **El teléfono se reinicia, la app se actualiza o cambia la zona horaria con alarmas futuras**: todas vuelven a existir con el mismo id, ninguna duplicada. Pruebas `reprogramarEsIdempotente` y `receptoresReprogramanTodo` en la Tarea 13, medición E-INF-024 en emulador.
4. **El usuario niega notificaciones o el permiso de alarmas exactas**: la lista sigue mostrando la tarea y el compromiso; la alarma degrada a ventana de 10 min y se reporta. Prueba `sinExactasDegradaAVentanaYReporta` en la Tarea 13 y `notificacionesNegadasNoRompenElSync` en la Tarea 12.
5. **FCM caído o credencial ausente en el servidor**: crear/cancelar responde igual (201/200) y la tarea llega por el periódico de 15 min. Prueba `TestCrearTarea_FCMFallaNoAfectaLaRespuesta` en la Tarea 6.

---

## Mapa de archivos

**msp-api** (rama `feat/visitas-p3-tareas` desde `main` con P2 fusionado):

| Archivo | Responsabilidad |
|---|---|
| `migrations-firebird/000068_create_msp_tareas_visita.{up,down}.sql` | Tabla e índices. |
| `internal/visitas/domain/tarea.go` (+test) | Entidad `Tarea`, reglas, igualdad de contenido, cancelación. |
| `internal/visitas/domain/derivar_tareas.go` (+test) | `DerivarEstadoTarea`. |
| `testdata/compromisos/` (mod) | Casos con tareas (mismo juego, spec §6.3). |
| `internal/visitas/infra/visitasfb/tareas_repo.go` (+e2e) | CRUD + sync/digest/ids/by-ids de tareas. |
| `internal/visitas/infra/visitasfb/predicado.go` (mod) | `predicadoTareasZona`. |
| `internal/visitas/app/tareas.go` (+test) | Crear, cancelar, listar, sync; aviso tras commit. |
| `internal/visitas/ports/outbound/avisador.go` | Puerto `AvisadorDeZona`. |
| `internal/visitas/infra/visitasfcm/avisador.go` (+test) | FCM v1 por tema. |
| `internal/visitas/infra/visitashttp/handlers_tareas.go`, `dto_tareas.go` (+tests) | Rutas. |
| `internal/auth/domain/permission_codes.go`, `internal/auth/auth_contracts.go`, `permission_codes_test.go` (mod) | `PermVisitasAsignarTareas`. |
| `internal/platform/config/config.go` (mod) | `TAREAS_FCM_ENABLED`, `FCM_CREDENTIALS_FILE`. |
| `cmd/api/visitas_wiring.go` (mod) | Cableado. |

**msp-app-kt** (rama `feat/visitas-tareas-avisos` desde `main` con P2 fusionado):

| Archivo | Responsabilidad |
|---|---|
| `core/database/.../entities/{TareaZonaEntity,TareasSyncStateEntity,AvisoEmitidoEntity,AvisoProgramadoEntity}.kt` | Tablas v37. |
| `core/database/.../dao/tareas/TareaZonaDao.kt`, `dao/avisos/AvisosDao.kt` | DAOs. |
| `core/database/.../migrations/Migration36to37.kt` (+test) | |
| `app/.../core/sync/zona/CursorParSyncEngine.kt` (crear, extraído de P2) | Motor común de cursor par + epoch. |
| `app/.../core/sync/visitas/VisitasZonaSyncManager.kt` (mod) | Usa el motor. |
| `app/.../core/sync/tareas/TareasZonaSyncManager.kt` (+test) | Sync de tareas. |
| `core/common/.../visitas/domain/Tareas.kt` (+casos) | Estado derivado. |
| `app/.../core/avisos/AvisoInmediato.kt` (+test) | Notificación "Visita asignada" idempotente. |
| `app/.../core/avisos/PlanDeAvisos.kt` (+test) | Cálculo puro de instantes. |
| `app/.../core/avisos/ReprogramadorDeAlarmas.kt`, `AvisoAlarmReceiver.kt`, `ReprogramarAlarmasReceiver.kt` (+tests) | Proyección a AlarmManager. |
| `app/.../core/avisos/KillSwitchAvisos.kt` | `config/visitas_avisos`. |
| `app/.../core/fcm/MspMessagingService.kt`, `SuscripcionDeZona.kt` (+tests) | Despertador. |
| `app/src/main/AndroidManifest.xml` (mod) | Permisos, receptores, servicio. |
| `app/build.gradle.kts`, `gradle/libs.versions.toml` (mod) | `firebase-messaging`. |

**sistema-cobro-web** (rama `feat/asignar-visita` desde `feat/agenda-compromisos` de P2):

| Archivo | Responsabilidad |
|---|---|
| `src/modules/desempeno/application/ports/DesempenoPort.ts`, `infrastructure/http/*` (mod) | `asignarTarea`, `cancelarTarea`, `listarTareas`. |
| `src/modules/desempeno/domain/entities/Tarea.ts` | Entidad. |
| `src/modules/desempeno/presentation/hooks/useAsignarTarea.ts`, `useTareas.ts` | Hooks (patrón `useAccionesBorrador`). |
| `src/modules/desempeno/components/tareas/{AsignarVisitaDialog,ListaTareas}.tsx` (+tests) | UI. |
| `src/modules/desempeno/components/modales/ModalesFicha.tsx`, `components/cuentas/TablaCuentas.tsx` (mod) | Puntos de entrada. |

## Paralelismo

| Carril | Tareas | Notas |
|---|---|---|
| A — servidor | 2–7 | En serie salvo 6 (FCM), que puede ir en paralelo con 4–5 en su propio worktree (archivos distintos). |
| B — escritorio | 9 | Tras 5 (contrato HTTP). Paralelo con C. |
| C — teléfono | 10–16 | Tras 5. En serie (Room/KSP). |
| D — compuertas/mediciones | 1, 8, 17, 18, 19 | |

Camino crítico: 1 → 2 → 3 → 4 → 5 → 10 → 11 → 13 → 16 (medición E-INF-024) → 18 (compuerta después) → canario (dueño) → E-INF-025.

---

### Task 1: Compuerta "antes" de P3 y preparativos del dueño

- [ ] **Step 1: Verificar que P2 sostiene lo que P3 usa** (agente independiente, solo preguntas): (1) ¿el canal de bajada de visitas entrega el commit tardío y los tres canales coinciden? (E-VIS-013 y `TestE2E_ParidadCanales_Visitas`); (2) ¿la fila `tareas` de `MSP_CFG_SYNC_EPOCH` existe en dev y en producción con `EPOCH = 0`?; (3) ¿qué hace Android con las alarmas al reiniciar, actualizar la app y revocar `SCHEDULE_EXACT_ALARM`? (LEÍDO en documentación oficial con URL y fecha; **no** MEDIDO todavía: se mide en la Tarea 16 antes del canario); (4) ¿FCM garantiza entrega? (respuesta esperada: no — 15 % de pérdida en inactivos, tope de 100 pendientes, colapso; por eso es solo despertador). SOSTIENE en 1–2 y respuestas documentadas en 3–4 permiten seguir; E-INF-024/019 se miden después y **condicionan el canario**, no el código (el diseño no depende de que FCM llegue). Registrar en `docs/evidencia/visitas.md` `## Compuerta antes P3`.
- [ ] **Step 2: Preparativos que hace el dueño (DEPENDE DEL DUEÑO), listados en el runbook:** (a) crear la cuenta de servicio de Firebase con rol "Firebase Cloud Messaging API Admin" en el proyecto de producción y dejar el JSON en el servidor en la ruta que pondrá en `FCM_CREDENTIALS_FILE` (fuera del repo); (b) decidir qué roles reciben `visitas:asignar_tareas` (DA-13: oficina y dueño) y concederlo en `MSP_ROLES_PERMISOS` tras el primer arranque; (c) crear `config/visitas_avisos` en Firestore con `{fcm: false, alarmas: false}` hasta el canario.

---

### Task 2: Migración 000068 `MSP_TAREAS_VISITA`

**Files:**
- Create: `migrations-firebird/000068_create_msp_tareas_visita.up.sql`, `.down.sql`
- Modify: `internal/visitas/infra/visitasfb/migracion_000066_test.go` (agregar 000068 a la prueba de forma)

- [ ] **Step 1: Prueba de forma (rojo)**: agregar `"000068_create_msp_tareas_visita.up.sql"` a la lista de `TestMigracion000066_SinLogicaEnLaBase` (renombrarla `TestMigracionesDeVisitas_SinLogicaEnLaBase`). Run → FAIL (archivo no existe).
- [ ] **Step 2: Escribir** (fecha civil `DATE` o `CHAR(10)` según la decisión E-VIS-014 de P2; abajo `DATE`):
```sql
-- 000068_create_msp_tareas_visita
-- Spec visitas §6.1. Tarea de oficina "visitar esta venta". Tabla greenfield:
-- CLAUDE.md §1 sin excepciones. ID lo genera el escritorio al abrir el
-- formulario (idempotencia); todos los timestamps los pone Go; UPDATED_AT se
-- estampa dentro de la transacción (§5.4.4). El ESTADO no tiene columna: se
-- deriva (cancelada / atendida / no_atendida / pendiente).
CREATE TABLE MSP_TAREAS_VISITA (
  ID               CHAR(36)      CHARACTER SET ASCII  NOT NULL,
  VENTA_ID         INTEGER                            NOT NULL,
  CLIENTE_ID       INTEGER                            NOT NULL,
  ZONA_CLIENTE_ID  INTEGER                            NOT NULL,
  FECHA            DATE                               NOT NULL,
  HORA             CHAR(5)       CHARACTER SET ASCII,
  NOTA             VARCHAR(500)  CHARACTER SET UTF8,
  REEMPLAZA_A      CHAR(36)      CHARACTER SET ASCII,
  CREATED_AT       TIMESTAMP                          NOT NULL,
  CREATED_BY       CHAR(36)      CHARACTER SET ASCII  NOT NULL,
  UPDATED_AT       TIMESTAMP                          NOT NULL,
  UPDATED_BY       CHAR(36)      CHARACTER SET ASCII  NOT NULL,
  CANCELADA_AT     TIMESTAMP,
  CANCELADA_BY     CHAR(36)      CHARACTER SET ASCII,
  CONSTRAINT PK_MSP_TAREAS_VISITA PRIMARY KEY (ID),
  CONSTRAINT FK_MSP_TAREAS_VISITA_REEMPLAZA FOREIGN KEY (REEMPLAZA_A) REFERENCES MSP_TAREAS_VISITA (ID)
);
COMMIT;
CREATE INDEX IDX_MSP_TAREAS_ZONA_UPD ON MSP_TAREAS_VISITA (ZONA_CLIENTE_ID, UPDATED_AT, ID);
CREATE INDEX IDX_MSP_TAREAS_VENTA ON MSP_TAREAS_VISITA (VENTA_ID);
COMMIT;
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (68, '000068_create_msp_tareas_visita', CURRENT_TIMESTAMP);
COMMIT;
```
`.down.sql`: `DROP TABLE MSP_TAREAS_VISITA; DELETE FROM MSP_MIGRATIONS WHERE ID = 68; COMMIT;`.
- [ ] **Step 3:** `make fb-snapshot NAME=antes-000068 && make fb-migrate-up`; prueba de forma → PASS. **Step 4: Commit** `feat(visitas): tabla MSP_TAREAS_VISITA sin lógica en la base (migración 000068)`.

---

### Task 3: Dominio de la tarea + estado derivado + casos compartidos

**Files:**
- Create: `internal/visitas/domain/tarea.go`, `tarea_test.go`, `derivar_tareas.go`, `derivar_tareas_test.go`
- Modify: `internal/visitas/domain/errors.go`, `casos_compromisos_test.go`, `testdata/compromisos/` (+casos `031`…`045`), `VERSION`

**Interfaces:**
- Produces:
```go
type Tarea struct{ /* privados */ }
type NuevaTareaParams struct {
	ID uuid.UUID; VentaID, ClienteID, ZonaClienteID int; Fecha string; Hora, Nota *string
	ReemplazaA *uuid.UUID; Por uuid.UUID; Hoy FechaCivil; Ahora time.Time
}
func NuevaTarea(p NuevaTareaParams) (*Tarea, error)
func RehydrateTarea(p RehydrateTareaParams) *Tarea
func (t *Tarea) MismoContenido(o *Tarea) bool   // venta, fecha, hora, nota (NFC), reemplaza_a
func (t *Tarea) Cancelar(por uuid.UUID, ahora time.Time) bool // false si ya estaba cancelada (idempotente)
// getters: ID, VentaID, ClienteID, ZonaClienteID, Fecha FechaCivil, Hora *HoraCivil, Nota *string,
//          ReemplazaA *uuid.UUID, CreatedAt, CreatedBy, UpdatedAt, UpdatedBy, CanceladaAt *time.Time, CanceladaBy *uuid.UUID

type EstadoTarea string // "cancelada" | "atendida" | "no_atendida" | "pendiente"
type AtendidaPor struct{ Tipo string /* "pago" | "visita" */; ID string; Por *int /* cobrador_id */; Instante time.Time }
type EstadoDeTarea struct{ Estado EstadoTarea; Atendida *AtendidaPor }
func DerivarEstadoTarea(t TareaCompromiso, visitas []VisitaCompromiso, pagos []PagoCompromiso, cuenta CuentaCompromiso, hoy FechaCivil, loc *time.Location) EstadoDeTarea
type TareaCompromiso struct{ ID string; VentaID int; Fecha FechaCivil; Hora *HoraCivil; CreatedAt time.Time; CanceladaAt *time.Time }
```
- Errores: `ErrTareaFechaPasada` (`tarea_fecha_pasada`, "la fecha de la tarea ya pasó"), `ErrTareaHoraInvalida` (`tarea_hora_invalida`, "la hora de la tarea no es válida"), `ErrTareaContenidoDistinto` (`tarea_contenido_distinto`, "esta tarea ya existe con otros datos"), `ErrTareaReemplazaInexistente` (`tarea_reemplaza_inexistente`, "la tarea que se reemplaza no existe"), `ErrTareaVentaNoEncontrada` (`tarea_venta_no_encontrada`, "la venta no existe"), `ErrTareaNoEncontrada` (NotFound, `tarea_no_encontrada`, "la tarea no existe"), `ErrTareaNotaDemasiadoLarga` (`tarea_nota_demasiado_larga`, "la nota no puede pasar de 500 caracteres").
- Regla de estado (spec §6.3, en este orden): `cancelada` si `CanceladaAt != nil`; `atendida` si existe pago a la venta (no rechazado) o visita **aplicable** (misma regla de alcance de P2, DA-6) con instante **> `CreatedAt`** — se informa el **primero** (menor instante; empate menor id); pago sin hora (DA-7) cuenta si su fecha civil ≥ fecha civil de `CreatedAt`; `no_atendida` si `Fecha.DiasHasta(hoy) > 14` (DA-11); si no, `pendiente`.

- [ ] **Step 1: Pruebas (rojo)**: `NuevaTarea` con fecha de ayer → `ErrTareaFechaPasada`; hoy y mañana OK; hora `"25:00"` → `ErrTareaHoraInvalida`; nota de 501 codepoints → error; `MismoContenido` con NFD vs NFC → igual; `Cancelar` dos veces → la segunda `false` y `CanceladaAt` no cambia. Casos compartidos nuevos (formato P2 con dos claves opcionales `tareas: [{id, venta_id, fecha, hora, created_at, cancelada_at}]` y `esperado_tareas: [{id, estado, atendida_por: {tipo, id} | null}]` — extender `parsearCaso` en Go **y** en Kotlin en la Tarea 11): 031 pendiente · 032 atendida por pago posterior · 033 pago anterior a la asignación no atiende · 034 atendida por "No se encontraba" (alcance cliente) · 035 atendida por visita a otra venta del mismo cliente con alcance venta **no** atiende · 036 cancelada gana a atendida · 037 no_atendida a hoy−15 · 038 pendiente a hoy−14 · 039 pago de oficina sin hora del mismo día atiende · 040 pago rechazado no atiende · 041 dos visitas posteriores: informa la primera · 042 reemplazada (la nueva pendiente, la vieja cancelada) · 043 captura local pendiente atiende (teléfono) · 044 visita a las 23:30 local del día de la asignación · 045 empate de instante (menor id).
- [ ] **Step 2: Implementar.** **Step 3:** regenerar `VERSION`; `go test ./internal/visitas/domain/ -count=1 -race` → PASS; `TestCasosCompromisos_CoberturaMinima` exige ahora también un caso por cada `EstadoTarea`. **Step 4: Commit** `feat(visitas): tarea de oficina con su estado derivado y casos compartidos (§6.3)`.

---

### Task 4: Repositorio de tareas + canal de bajada de tareas con paridad

**Files:**
- Create: `internal/visitas/infra/visitasfb/tareas_repo.go`, `tareas_repo_e2e_test.go`, `tareas_paridad_e2e_test.go`
- Modify: `internal/visitas/infra/visitasfb/predicado.go` (`predicadoTareasZona`), su prueba de forma
- Modify: `internal/visitas/ports/outbound/repo.go` (`TareasRepo`)

**Interfaces:**
```go
type TareasRepo interface {
	Insertar(ctx context.Context, t *domain.Tarea) error                       // estampa CREATED_AT/UPDATED_AT en transacción; unique → ErrTareaYaExiste (interno)
	Buscar(ctx context.Context, id uuid.UUID) (*domain.Tarea, error)
	Cancelar(ctx context.Context, id uuid.UUID, por uuid.UUID) (bool, error)   // UPDATE … SET CANCELADA_AT=?, CANCELADA_BY=?, UPDATED_AT=?, UPDATED_BY=? WHERE ID=? AND CANCELADA_AT IS NULL
	VentaParaTarea(ctx context.Context, ventaID int) (clienteID, zonaID int, err error) // solo lectura; zona AL ASIGNAR (DA-9)
	ListarPorZona(ctx context.Context, zonaID int, desde time.Time) ([]*domain.Tarea, error)
	SyncTareasPorZona(ctx context.Context, zonaID int, desde time.Time, cursor *time.Time, afterID *string, limit int, upper time.Time) ([]*domain.Tarea, error)
	DigestTareasZona(ctx context.Context, zonaID int, desde, upper time.Time) (Digest, error)
	IDsTareasZona(ctx context.Context, zonaID int, desde, upper time.Time, after string, limit int) ([]string, bool, error)
	ByIDsTareasZona(ctx context.Context, zonaID int, desde, upper time.Time, ids []string) ([]*domain.Tarea, error)
}
```
- `predicadoTareasZona(alias) = "t.ZONA_CLIENTE_ID = ? AND t.FECHA >= ?"` con `desde` default = hoy − 92 días (misma V; **hueco del spec**: §6.2 no fija la ventana de tareas; se usa V por simetría y se reporta). Tareas canceladas **sí** viajan (el teléfono necesita saber que se canceló: no hay tombstones porque la cancelación es un cambio con `UPDATED_AT` nuevo).
- `VentaParaTarea`: `SELECT s.CLIENTE_ID, s.ZONA_CLIENTE_ID FROM MSP_SALDOS_VENTAS s WHERE s.DOCTO_CC_ID = ?` (verificar el nombre de columna de venta en `desempenofb/cuentas_repo.go:95`; solo lectura; `sql.ErrNoRows` → `ErrTareaVentaNoEncontrada`).

- [ ] **Step 1: E2E (rojo)**: insertar/buscar (bordes de fecha civil); cancelar dos veces → `true`, luego `false`; `TestE2E_ParidadCanales_Tareas` (patrón P2 Tarea 10, filas confirmadas con cleanup sin descartar error); `TestE2E_SyncTareas_CommitTardioSeEntrega` (mismo esquema de dos transacciones con la marca segura de P2; control positivo sin marca); la prueba de forma de P2 extendida a las cuatro funciones de tareas (`predicadoTareasZona(` presente; `ZONA_CLIENTE_ID = ?` solo en `predicado.go`; sin `INNER JOIN`); `TestNingunaRutaBorraVisitas` ya cubre `DELETE FROM MSP_TAREAS_VISITA`.
- [ ] **Step 2: Implementar** (copiar la forma de `sync_repo.go`; `LeerMarcaSegura` compartida). **Step 3: Verde, rojos al revertir** (quitar `AND CANCELADA_AT IS NULL` → cancelar dos veces cambia `CANCELADA_AT`: FAIL). **Step 4: Commit** `feat(visitas): tareas persistidas y su canal de bajada por zona con paridad (I4)`.

---

### Task 5: Servicio y HTTP de tareas + permiso `visitas:asignar_tareas`

**Files:**
- Create: `internal/visitas/app/tareas.go`, `tareas_test.go`
- Create: `internal/visitas/infra/visitashttp/handlers_tareas.go`, `dto_tareas.go`, `handlers_tareas_test.go`
- Modify: `internal/visitas/infra/visitashttp/routes.go`
- Modify: `internal/auth/domain/permission_codes.go` (constante + `AllPermissions()`), `internal/auth/auth_contracts.go` (re-export), `internal/auth/domain/permission_codes_test.go` (`TestAllPermissions_NonEmpty` lista esperada)

**Interfaces:**
- `PermVisitasAsignarTareas Permission = "visitas:asignar_tareas"`, descripción "asignar y cancelar visitas a cobradores", categoría la de cobranza o visitas existente (`:150-166`).
- Rutas:
  - `POST /v2/visitas/tareas` body `{id, venta_id, fecha, hora?, nota?, reemplaza_a?}` → 201 `TareaDTO`; mismo id y mismo contenido → 201 igual; distinto → 422 `tarea_contenido_distinto`; errores de la Tarea 3.
  - `POST /v2/visitas/tareas/{id}/cancelar` → 200 `TareaDTO` (repetida → 200 igual); inexistente → 404 `tarea_no_encontrada`.
  - `GET /v2/visitas/tareas?zona_id=&estado=&desde=` (`PermVisitasAsignarTareas` **o** `PermCobranzaVerPagos`; es lectura) → `{items: [TareaConEstadoDTO]}` con `estado` y `atendida_por {tipo, id, cobrador_id, instante}`; `estado` filtra después de derivar.
  - `GET /v2/visitas/sync/tareas/zona/{zona_id}` + `/digest`, `/ids`, `/by-ids` (`PermCobranzaVerPagos`) con las mismas formas de P2 (`after_id` UUID; página vacía conserva cursor; `sync_epoch` de `epoch_repo.Efectivo(ctx, "tareas", zona)`).
  - `TareaDTO{id, venta_id, cliente_id, zona_cliente_id, fecha, hora, nota, reemplaza_a, created_at, created_by, updated_at, cancelada_at}` (todas las fechas instante RFC3339Nano UTC; `fecha` civil).
- Servicio: `CrearTarea(ctx, in, por) (*domain.Tarea, error)`: valida `reemplaza_a` existe (si no, `ErrTareaReemplazaInexistente`); si `reemplaza_a` viene, **en la misma transacción** cancela la reemplazada (spec: "editar = cancelar + asignar otra") e inserta la nueva; tras el commit → `avisador.AvisarSync(ctx, zona)`. `CancelarTarea` igual con aviso tras commit si cambió algo. Métricas `tareas.creada`, `tareas.cancelada`, `tareas.fcm {ok|error}`.

- [ ] **Step 1: Pruebas (rojo)**: con fakes: idempotencia (dos `POST` iguales → 1 inserción, mismo cuerpo), contenido distinto → 422, reemplazo cancela la vieja en la misma transacción (fake de tx cuenta 1 llamada; si el insert falla, la vieja **no** queda cancelada), aviso **después** del commit (fake de tx con `failAfterFn` → **no** se avisa), permiso: usuario sin `visitas:asignar_tareas` → 403 en POST; `TestPermissionConstantsAreReExported` y `TestAllPermissions_FitFirebirdColumnWidths` verdes.
- [ ] **Step 2: Implementar.** **Step 3: Verde.** **Step 4: Commit** `feat(visitas): la oficina asigna y cancela visitas, con permiso propio (DA-13)`.

---

### Task 6: FCM v1 por tema de zona (despertador)

**Files:**
- Create: `internal/visitas/ports/outbound/avisador.go`
- Create: `internal/visitas/infra/visitasfcm/avisador.go`, `avisador_test.go`
- Modify: `internal/platform/config/config.go` (struct `Visitas`)
- Modify: `cmd/api/visitas_wiring.go`

**Interfaces:**
```go
// outbound
type AvisadorDeZona interface{ AvisarSync(ctx context.Context, zonaID int) error }
type AvisadorNulo struct{} // AvisarSync devuelve nil; se usa con TAREAS_FCM_ENABLED=false

// visitasfcm
type enviador interface{ Send(ctx context.Context, m *messaging.Message) (string, error) }
func New(e enviador, log *slog.Logger) *Avisador
func NuevoDesdeCredencial(ctx context.Context, rutaCredencial string, log *slog.Logger) (*Avisador, error) // firebase.NewApp + app.Messaging
func MensajeDeZona(zonaID int) *messaging.Message // puro, para la prueba
```
- `MensajeDeZona(7)` = `&messaging.Message{Topic: "zona-7", Data: map[string]string{"tipo": "sync"}, Android: &messaging.AndroidConfig{CollapseKey: "tareas-zona-7", Priority: "high", TTL: ptr(4 * time.Hour)}}`. Sin `Notification` (mensaje de datos). Sin ids ni nombres.
- Config: `TareasFCMEnabled bool env:"TAREAS_FCM_ENABLED" envDefault:"false"`, `FCMCredentialsFile string env:"FCM_CREDENTIALS_FILE"`. Wiring: si `false` o ruta vacía → `AvisadorNulo`; si la credencial no carga → log `ERROR tareas.fcm credencial_invalida` y `AvisadorNulo` (el API arranca igual).
- `AvisarSync`: un intento con `context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)` (HTTP, no Firebird: el envenenamiento del pool no aplica), sin bucle de reintentos; error → log + métrica `tareas.fcm error`, devuelve el error (el servicio lo ignora para la respuesta).

- [ ] **Step 1: Pruebas (rojo)**: `TestMensajeDeZona_SoloDespertador` (JSON del mensaje sin `notification`, `data == {"tipo":"sync"}`, collapse key, TTL 4 h, prioridad alta, tema); `TestAvisador_ErrorNoReintenta` (fake que falla: 1 llamada); `TestCrearTarea_FCMFallaNoAfectaLaRespuesta` (en `app`, avisador que falla → `CrearTarea` sin error); `TestWiring_SinCredencialArrancaConAvisadorNulo`.
- [ ] **Step 2: Implementar.** **Step 3: Verde** (`go mod tidy` no debe agregar módulos: `firebase.google.com/go/v4` ya está; si agrega, revisar que sean del mismo SDK y binarios Windows — CLAUDE.md §5). **Step 4: Commit** `feat(visitas): push FCM por tema de zona solo como despertador, apagado por omisión`.

---

### Task 7: Simulación y suite de servidor P3

- [ ] **Step 1:** Extender `internal/visitas/sim`: acciones `asignarTarea` (con doble envío), `cancelarTarea`, `reemplazarTarea`, `pullTareas`, `perderPush` (el push es opcional para el resultado); invariantes: I2 para tareas (un id = una fila), I4 para los canales de tareas, I9 (estado derivado igual tras borrar y recalcular), y "toda tarea creada llega al teléfono modelo en ≤ 1 periodo aunque se pierdan todos los push".
- [ ] **Step 2:** `make test-mutation-visitas-domain` (incluye `derivar_tareas.go`); `go clean -testcache && golangci-lint run ./... && go test -race -short ./... && make test-firebird-all && make coverage-visitas`.
- [ ] **Step 3: Commit** `test(visitas): simulación de tareas con push perdido y doble envío`.

---

### Task 8: Contrato verificado contra el escritorio y el teléfono

- [ ] **Step 1:** Golden JSON de `TareaDTO`, `TareaConEstadoDTO`, página de sync de tareas y digest en `internal/visitas/infra/visitashttp/testdata/tareas-*.golden.json`; prueba que compara la respuesta real (con fakes) contra el golden. Estos mismos archivos se copian como fixtures MSW del escritorio (Tarea 9) y del MockWebServer del teléfono (Tarea 11), para que los tres lados prueben **la misma forma**.
- [ ] **Step 2: Commit** `test(visitas): goldens del contrato de tareas compartidos con escritorio y teléfono`.

---

### Task 9: Escritorio — "Asignar visita", cancelar y lista

**Base:** `feat/asignar-visita` desde `feat/agenda-compromisos` (P2 Tarea 21), worktree del dueño `/Volumes/M2-1TB/Developer/sistema-cobro-web-desempeno` (confirmar).

**Files:** los del mapa.

**Interfaces:**
- Puerto: `asignarTarea(cuerpo: NuevaTarea): Promise<Tarea>`, `cancelarTarea(id: string): Promise<Tarea>`, `listarTareas(zonaId: number, estado: EstadoTarea | null, desde: string | null, signal?: AbortSignal): Promise<TareaConEstado[]>`.
- `AsignarVisitaDialog({ ventaId, clienteNombre, onHecho })`: el id se genera **al montar** (`const [id] = useState(() => crypto.randomUUID())`), no al enviar; campos fecha (calendar de shadcn, mínimo hoy), hora opcional, nota opcional (≤ 500); botón pequeño (memoria `feedback_accion_pequena_sin_caja`: `<Button size="sm">`), texto "Asignar visita"; estados `idle | enviando | hecho | error` con `enviandoRef` contra doble envío (patrón `useAccionesBorrador.ts:10-45`); errores del servidor por código (`tarea_fecha_pasada` → "la fecha ya pasó"); 403 → mensaje de permiso (si `src/lib/errorDePermiso.ts` de `fix/mapas-y-permiso-fotos` ya está fusionado, usarlo; si no, un `if (status === 403)` local y anotarlo).
- "Editar" = abrir el diálogo con `reemplazaA = tarea.id` (nuevo UUID para la nueva).
- Puntos de entrada: botón en la fila de `TablaCuentas.tsx` (con `e.stopPropagation()`, la fila entera es clicable, `TablaCuentas.tsx:57-77`) y en el pie de `ModalActividadDetalle.tsx:162-170`. `ListaTareas` en la ficha (`FichaScreen.tsx`), con estado y "atendida por <cobrador> a las <hora>".
- Permiso en UI: el escritorio solo tiene permisos por módulo (`AuthContext.tsx:199-208`); **hueco**: no hay permiso por acción; se muestra el botón con `DESEMPENO_COBRADORES` y el servidor decide (403 manejado).

- [ ] **Step 1: Pruebas (rojo)** con MSW usando los goldens de la Tarea 8: `dobleClicNoCreaDosTareas` (dos clics rápidos → 1 POST; dos POST con el mismo id si el primero tarda — el servidor es idempotente, pero la UI ni siquiera los manda), `elIdSeGeneraAlAbrirNoAlEnviar` (el id del cuerpo es el mismo tras un error y un reintento), `fechaPasadaMuestraMensaje`, `cancelarRepetidoEsIgual`, `listaPintaEstadoYQuienAtendio`, `editarMandaReemplazaA`.
- [ ] **Step 2: Implementar.** **Step 3:** `npm test && npm run lint && npx tsc`. **Step 4: Commit** `feat(desempeno): asignar, editar y cancelar visitas desde el escritorio`.

---

### Task 10: Room v37 + motor de cursor par compartido

**Files:**
- Create: `core/database/.../entities/TareaZonaEntity.kt`, `TareasSyncStateEntity.kt`, `AvisoEmitidoEntity.kt`, `AvisoProgramadoEntity.kt`
- Create: `core/database/.../dao/tareas/TareaZonaDao.kt`, `dao/avisos/AvisosDao.kt`
- Create: `core/database/.../migrations/Migration36to37.kt`, `Migration36to37Test.kt`
- Modify: `AppDatabase.kt` (v37, cuatro entidades, dos DAOs, `MIGRATION_36_37`)
- Create: `app/.../core/sync/zona/CursorParSyncEngine.kt`
- Modify: `app/.../core/sync/visitas/VisitasZonaSyncManager.kt` (delegar en el motor; **sus 16 pruebas de P2 no cambian y deben seguir verdes**)

**Interfaces:**
- `TareaZonaEntity` (`tarea_zona`, PK `ID`): `ID, VENTA_ID: Int, CLIENTE_ID: Int, ZONA_CLIENTE_ID: Int, FECHA: String, HORA: String?, NOTA: String?, REEMPLAZA_A: String?, CREATED_AT: String, UPDATED_AT: String, CANCELADA_AT: String?`.
- `TareasSyncStateEntity` (`tareas_sync_state`, PK `ZONA`): `ZONA, CURSOR: String?, AFTER_ID: String?, EPOCH: Int?`.
- `AvisoEmitidoEntity` (`aviso_emitido`, PK `ID`): `ID: String /* id de la tarea */, EMITIDO_EN: String`.
- `AvisoProgramadoEntity` (`aviso_programado`, PK `ID`): `ID: String /* sha-256 hex de "tipo|origen|instante" */, INSTANTE: String, TIPO: String /* promesa|cita|tarea */, ORIGEN_ID: String`.
- `AvisosDao`: `@Transaction reemplazarProgramados(nuevos: List<AvisoProgramadoEntity>): List<AvisoProgramadoEntity> /* devuelve los anteriores */`, `programados()`, `marcarEmitido(id, en): Boolean` (`INSERT OR IGNORE` → `true` si insertó), `emitido(id): Boolean`, `podarEmitidosAnterioresA(limite)`.
- `CursorParSyncEngine<Dto>(pagina: suspend (cursor: String?, afterId: String?, desde: String) -> PaginaSync<Dto>, aplicar: suspend (List<Dto>, String?, String?) -> Unit, estado: suspend () -> EstadoSync, reiniciar: suspend () -> Unit, guardarEpoch: suspend (Int) -> Unit)`: `suspend fun correr(desde: String): ResultadoSync` con las seis reglas de epoch y la de página vacía (extraído **sin cambios de conducta** de `VisitasZonaSyncManager`).

- [ ] **Step 1:** Migración 36→37 (rojo → verde, patrón P1/P2). **Step 2:** Refactor del motor: mover la lógica, correr las 16 pruebas de `VisitasZonaSyncManagerTest` **sin tocarlas** → verdes (si alguna cambia, el refactor está mal). **Step 3:** DAO de avisos: `marcarEmitidoEsIdempotente`, `reemplazarProgramadosDevuelveLosAnteriores`. **Step 4: Commit** `feat(database): Room v37 con tareas de zona y avisos; motor común de cursor par`.

---

### Task 11: Sync de tareas en el teléfono + `Tareas.kt` con casos compartidos

**Files:**
- Create: `app/.../data/api/services/visits/TareasZonaSyncApi.kt`
- Create: `app/.../core/sync/tareas/TareasZonaSyncManager.kt`, `TareasZonaSyncManagerTest.kt`
- Create: `core/common/.../visitas/domain/Tareas.kt`, prueba de casos (extiende `CompromisosCasosCompartidosTest` con `tareas`/`esperado_tareas`)
- Modify: `scripts/casos-compromisos.sh copiar` (los casos 031–045)
- Modify: `app/.../workers/VisitsReconcileWorker.kt` (corre también el sync de tareas cada 15 min)

**Interfaces:**
- `TareasZonaSyncManager(api, dao: TareaZonaDao, zonaActual, killSwitch: () -> Boolean, alAplicar: suspend (List<TareaZonaEntity>) -> Unit)` sobre `CursorParSyncEngine`; `alAplicar` alimenta el aviso inmediato (Tarea 12).
- `fun derivarEstadoTarea(t, visitas, pagos, cuenta, hoy: LocalDate, zona: ZoneId): EstadoDeTarea` espejo exacto de Go; usa la unión de `AgendaLocal` (visitas propias ∪ `visita_zona`; pagos con captura local pendiente).
- Pruebas obligatorias: las 16 de P2 en su versión de tareas generadas por el mismo motor (al menos `paginaVaciaNoReiniciaElAfterIdPersistido`, `epochNoSePersisteHastaQueElReplayTermina`, `cambioDeZonaDejaCursorYAfterIdNulos`, `epochCeroSeIgnoraYNoEntraEnBucleDeReplays`), `tareaCanceladaLlegaComoCambio`, `itemConCamposNulosNoRevientaElSync`; casos 031–045 contra `derivarEstadoTarea` con el mismo `VERSION` que Go.
- [ ] **Step 1:** Pruebas (rojo, MockWebServer con los goldens de la Tarea 8). **Step 2:** Implementar. **Step 3:** Verde; `bash scripts/casos-compromisos.sh verificar`. **Step 4: Commit** `feat(visitas): bajada de tareas de la zona y su estado derivado en el teléfono`.

---

### Task 12: Aviso inmediato "Visita asignada" (idempotente) + canal de notificaciones

**Files:**
- Create: `app/.../core/avisos/AvisoInmediato.kt`, `AvisoInmediatoTest.kt`
- Create: `app/.../core/avisos/CanalesDeNotificacion.kt`
- Modify: `app/.../MspApplication.kt` (crear canales en `onCreate`)
- Modify: el flujo de alta del equipo / primer inicio de sesión para pedir `POST_NOTIFICATIONS` (localizar con `grep -rn "ACCESS_FINE_LOCATION" app/src/main/java | grep -i "request\|launcher"`: se pide junto a la ubicación)

**Interfaces:**
- `class AvisoInmediato(dao: AvisosDao, notificar: (titulo: String, texto: String, id: Int) -> Unit, permitido: () -> Boolean, ahora: () -> Instant)`: `suspend fun alAplicar(tareas: List<TareaZonaEntity>, estados: Map<String, EstadoTarea>)` → para cada tarea con estado `pendiente` y **no** emitida: `if (dao.marcarEmitido(id, ahora)) notificar("Visita asignada", "<folio o cliente> · <día>", hashEstable(id))`. Marca primero, notifica después: si la notificación falla o está negada, **no** se reintenta (comodidad, spec §6.6); se reporta `permiso.notificaciones = false`.
- Canal `visitas_asignadas` (importancia alta) y `avisos_compromisos` (alta).

- [ ] **Step 1: Pruebas (rojo)**: `mismaTareaPorTresCaminosNotificaUnaVez` (llamar `alAplicar` tres veces con la misma tarea), `canceladaNoNotifica`, `atendidaNoNotifica`, `notificacionesNegadasNoRompenElSync` (`permitido = false` → no lanza, marca emitido igual y reporta), `idDeNotificacionEstable`.
- [ ] **Step 2: Implementar.** **Step 3: Verde.** **Step 4: Commit** `feat(avisos): notificación inmediata de visita asignada, una sola vez por tarea`.

---

### Task 13: Alarmas locales — plan puro, tabla y proyección a AlarmManager

**Files:**
- Create: `app/.../core/avisos/PlanDeAvisos.kt`, `PlanDeAvisosTest.kt`
- Create: `app/.../core/avisos/ReprogramadorDeAlarmas.kt`, `ReprogramadorDeAlarmasTest.kt` (Robolectric `ShadowAlarmManager`)
- Create: `app/.../core/avisos/AvisoAlarmReceiver.kt`, `ReprogramarAlarmasReceiver.kt`, `ReceptoresTest.kt`
- Create: `app/.../core/avisos/KillSwitchAvisos.kt` (`config/visitas_avisos {fcm, alarmas}`, ausente o inválido = **apagado**)
- Modify: `app/src/main/AndroidManifest.xml`
- Create: `app/src/test/java/com/example/msp_app/ManifiestoDeAvisosTest.kt`

**Interfaces:**
```kotlin
data class AvisoPlaneado(val id: String, val instante: Instant, val tipo: String, val origenId: String)
object PlanDeAvisos {
    val HORA_MANANA: LocalTime = LocalTime.of(8, 0)       // DA-2, DA-15
    const val MINUTOS_ANTES = 30L
    fun planear(compromisos: Collection<CompromisoVigente>, tareas: Collection<Pair<TareaZonaEntity, EstadoTarea>>,
                ahora: Instant, zona: ZoneId): List<AvisoPlaneado>
    fun idDe(tipo: String, origenId: String, instante: Instant): String // sha-256 hex de "tipo|origen|instante"
}
class ReprogramadorDeAlarmas(context, dao: AvisosDao, killSwitch: () -> Boolean, telemetria) {
    suspend fun reprogramarTodo(plan: List<AvisoPlaneado>)
}
```
- Reglas de `planear`: promesa `vigente_futura`/`vigente_hoy` → 8:00 del día; cita con hora → hora − 30 min; cita sin hora → 8:00; tarea `pendiente` con hora → hora − 30 min, sin hora → 8:00; **solo instantes futuros** (`> ahora`); estados `incumplida`, `cumplida`, `cita_cumplida`, `cerrada_sin_cumplir` y tareas no pendientes → nada.
- `reprogramarTodo`: `anteriores = dao.reemplazarProgramados(plan)`; cancela cada `anterior` (mismo `PendingIntent` con `requestCode = id.hashCode()` y `FLAG_IMMUTABLE`) y pone cada nuevo; `if (alarmManager.canScheduleExactAlarms()) setExactAndAllowWhileIdle(RTC_WAKEUP, …) else setWindow(RTC_WAKEUP, instante, 10 min, …)` y reporta `permiso.alarmas_exactas = false`; si `killSwitch()` es falso, cancela todo y no pone nada. Idempotente.
- Disparadores de `reprogramarTodo`: `BOOT_COMPLETED`, `MY_PACKAGE_REPLACED`, `TIME_SET`, `TIMEZONE_CHANGED`, `AlarmManager.ACTION_SCHEDULE_EXACT_ALARM_PERMISSION_STATE_CHANGED` (receptor `ReprogramarAlarmasReceiver`, que encola un `OneTimeWork` único `reprogramar_avisos` con `KEEP` — el receptor no hace trabajo largo), abrir la app, tras cada sync de visitas/tareas y tras cada captura de visita (en `RegistroDeVisitaAdapter.encolarEnvio`, `RVA:213`, agregar el encolado del mismo trabajo, total y sin propagar).
- Manifiesto: `SCHEDULE_EXACT_ALARM`, `RECEIVE_BOOT_COMPLETED`, `POST_NOTIFICATIONS`; receptor `ReprogramarAlarmasReceiver` `exported="false"` con los cinco `intent-filter` (`BOOT_COMPLETED` y `MY_PACKAGE_REPLACED` requieren `exported="true"` en algunas versiones: usar `exported="true"` solo con esos dos filtros y validar la acción en `onReceive`); `AvisoAlarmReceiver` `exported="false"`.

- [ ] **Step 1: Pruebas (rojo)**: `PlanDeAvisosTest` — una por regla con `ahora` fijo y zona `America/Mexico_City` (incluye cambio de horario: México ya no usa horario de verano desde 2022, pero `ZoneId` real lo resuelve; caso con `America/Tijuana` para verificar que la zona inyectada manda), `instantePasadoNoSePlanea`, `idEsDeterminista`; `ReprogramadorDeAlarmasTest` (Robolectric, `shadowOf(alarmManager).scheduledAlarms`): `reprogramarEsIdempotente` (dos veces → mismas alarmas, sin duplicados), `cancelaLasQueYaNoEstan`, `sinExactasDegradaAVentanaYReporta` (`ShadowAlarmManager.setCanScheduleExactAlarms(false)`), `killSwitchApagadoCancelaTodo`; `ReceptoresTest`: `receptoresReprogramanTodo` (cada una de las cinco acciones encola `reprogramar_avisos`); `ManifiestoDeAvisosTest`: el manifiesto **no** contiene `USE_EXACT_ALARM` y sí `SCHEDULE_EXACT_ALARM`, `POST_NOTIFICATIONS`, `RECEIVE_BOOT_COMPLETED` (control positivo: la misma lectura encuentra `ACCESS_FINE_LOCATION`).
- [ ] **Step 2: Implementar.** **Step 3: Verde** (`./gradlew --no-parallel :app:testDevlocalDebugUnitTest --tests '*Avisos*' --tests '*Alarmas*' --tests '*Receptores*' --tests '*ManifiestoDeAvisos*' --rerun-tasks`). **Step 4: Commit** `feat(avisos): alarmas locales como proyección de una tabla, reprogramables tras reinicio, actualización y cambio de hora`.

---

### Task 14: FCM en el teléfono (despertador) + suscripción por zona

**Files:**
- Modify: `gradle/libs.versions.toml` (`firebase-messaging = { group = "com.google.firebase", name = "firebase-messaging" }`, versión por BOM), `app/build.gradle.kts`
- Create: `app/.../core/fcm/MspMessagingService.kt`, `SuscripcionDeZona.kt`, tests
- Modify: `AndroidManifest.xml` (servicio con `com.google.firebase.MESSAGING_EVENT`, `exported="false"`)
- Modify: el lugar donde cambia la zona del usuario (localizar con `grep -rn "zonaClienteId\|ZONA_CLIENTE_ID" app/src/main/java/com/example/msp_app/data/local/datasource | grep -i "set\|guardar\|cambi"` y el mismo punto que hoy reinicia `cobranza_sync_state` por cambio de zona, `COBRANZA-SYNC.md:81`)

**Interfaces:**
- `MspMessagingService : FirebaseMessagingService`: `onMessageReceived(m)`: si `m.data["tipo"] == "sync"` y `KillSwitchAvisos.fcm` → `encolarSyncTareas(context)`; cualquier otro payload se ignora (log). `onDeletedMessages()` → `encolarSyncTareas`. `onNewToken` → `SuscripcionDeZona.resuscribir()`.
- `fun encolarSyncTareas(context)`: `OneTimeWorkRequestBuilder<SyncTareasWorker>().setExpedited(OutOfQuotaPolicy.RUN_AS_NON_EXPEDITED_WORK_REQUEST).setConstraints(red)` con `enqueueUniqueWork("sync_tareas_zona", ExistingWorkPolicy.KEEP, …)`. `SyncTareasWorker` corre `TareasZonaSyncManager` + aviso inmediato + `reprogramarTodo` y devuelve `success` siempre (patrón `VisitsReconcileWorker`), con `getForegroundInfo()` implementado (obligatorio para expedited en API < 31).
- `SuscripcionDeZona`: `suscribir(zona)`, `cambiarDeZona(vieja, nueva)` (desuscribe la vieja, suscribe la nueva; los dos con reintento propio de Firebase; errores → log), tema `"zona-$zona"`; solo si `KillSwitchAvisos.fcm`.

- [ ] **Step 1: Pruebas (rojo)**: `payloadSyncEncolaUnSoloTrabajoUnico` (dos mensajes → un trabajo `sync_tareas_zona`), `onDeletedMessagesHaceSyncCompleto`, `payloadDesconocidoSeIgnora`, `nuncaUsaDropWorkRequest` (inspección del `WorkSpec`: `outOfQuotaPolicy == RUN_AS_NON_EXPEDITED_WORK_REQUEST`), `cambioDeZonaDesuscribeLaVieja` (fake de `FirebaseMessaging` vía interfaz `Temas`), `killSwitchFcmApagadoNoSuscribe`.
- [ ] **Step 2: Implementar.** **Step 3: Verde.** **Step 4: Commit** `feat(fcm): push de zona solo despierta el sync de tareas; nunca trae la tarea`.

---

### Task 15: Permisos, batería y telemetría de avisos

**Files:**
- Modify: `app/.../core/visitas/envio/TelemetriaDeVisitas.kt` (claves `permiso.notificaciones`, `permiso.alarmas_exactas`, `bateria.sin_optimizar`, `avisos.programados`, `avisos.disparados_24h`)
- Modify: flujo de alta (mismo lugar de la Tarea 12)
- Create: `app/.../core/avisos/EstadoDePermisos.kt` (+test)

**Interfaces:**
- `data class EstadoDePermisos(val notificaciones: Boolean, val alarmasExactas: Boolean, val bateriaSinOptimizar: Boolean)`; `fun leer(context): EstadoDePermisos` (`NotificationManagerCompat.areNotificationsEnabled()`, `AlarmManager.canScheduleExactAlarms()` con `true` en API < 31, `PowerManager.isIgnoringBatteryOptimizations(packageName)`).
- Batería: en el alta se abre `Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS` (lista del sistema) con un texto corto. **No** se declara `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS` (Play restringe ese permiso a casos que esta app no cumple; **hueco del spec** §6.6, que no dice cómo; se reporta).
- `AvisoAlarmReceiver` cuenta `avisos.disparados_24h` en SharedPreferences (ventana móvil) y la telemetría lo lee.

- [ ] **Step 1: Pruebas (rojo)**: `telemetriaIncluyeLosCincoPermisosYNadaMas` (lista blanca), `apiMenorA31AlarmasExactasEsTrue`, `alarmaDisparadaSuma`. **Step 2: Implementar.** **Step 3: Verde.** **Step 4: Commit** `feat(avisos): reporta permisos de notificación, alarmas exactas y batería`.

---

### Task 16: Medición E-INF-024 en emulador (alarmas) — antes del canario

**Files:**
- Modify: `docs/evidencia/infra.md` (entrada E-INF-024; si el número ya existe al escribirla, el siguiente libre)

- [ ] **Step 1:** Emulador API 34 (si el agente no puede arrancarlo, **lo hace el dueño**), APK `devlocal` con `config/visitas_avisos` de dev en `{alarmas: true}`. Crear (vía API dev) una tarea con hora a +40 min y una promesa para mañana. Verificar `adb shell dumpsys alarm | grep com.example.msp_app.test` → 2 alarmas.
- [ ] **Step 2:** `adb reboot` → tras el arranque (sin abrir la app) `dumpsys alarm` → las 2 siguen (por `BOOT_COMPLETED`). `adb install -r` del mismo APK → siguen (por `MY_PACKAGE_REPLACED`; **es el INFERIDO de la investigación P8**). `adb shell appops set com.example.msp_app.test SCHEDULE_EXACT_ALARM deny` → reprogramadas como ventana (`dumpsys` muestra `window`), telemetría `permiso.alarmas_exactas=false`. `adb shell settings put global auto_time 0 && adb shell date <+1h>` → reprogramadas (`TIME_SET`). Control positivo: antes de instalar el receptor (APK de P2), el mismo `adb reboot` deja 0 alarmas.
- [ ] **Step 3:** Registrar salidas en E-INF-024 (MEDIDO, "emulador API 34, fecha"). Lo que no se mide aquí (fabricantes Samsung/Xiaomi/Huawei/OnePlus, dontkillmyapp) se dice en voz alta en la entrada.
- [ ] **Step 4: Commit** `docs(evidencia): alarmas de visitas tras reinicio, actualización y revocación en emulador (E-INF-024)`.

---

### Task 17: Agente adversario (P3)

- [ ] Encargo: "haz que una tarea asignada no llegue a la zona, llegue dos veces, notifique dos veces, se quede 'pendiente' cuando ya se atendió, o que una alarma suene para algo cancelado o atendido; haz que un push lleve datos". Cada escenario → prueba o semilla (Tareas 7, 11–14); si alguno pasa, se detiene y se arregla con su rojo al revertir.

---

### Task 18: Compuerta "después" de P3 + suites completas

- [ ] **Step 1: Suites**: msp-api (`go clean -testcache && golangci-lint run ./... && go test -race -short ./... && make test-firebird-all && make coverage-visitas`), msp-app-kt (`rm -rf app/build && ./gradlew --no-parallel prePushCheck --rerun-tasks`, guardia), escritorio (`npm test && npm run lint && npx tsc`), `bash scripts/casos-compromisos.sh verificar`.
- [ ] **Step 2: Compuerta "después"** (agente independiente): rojo al revertir de cada arreglo (idempotencia de creación, cancelación repetida, aviso tras commit, `{"tipo":"sync"}` sin datos, `RUN_AS_NON_EXPEDITED`, aviso inmediato una vez, reprogramación idempotente, degradación sin exactas, paridad de tareas, commit tardío de tareas); **enumerar** con grep y control positivo los consumidores de `MSP_TAREAS_VISITA`, `tarea_zona`, `aviso_programado`, `aviso_emitido`, el tema `zona-<id>`, el permiso `visitas:asignar_tareas`, y los receptores del manifiesto; decir en voz alta lo no verificado (entrega FCM real: E-INF-025 se mide en el canario; conducta por fabricante; Doze profundo).
- [ ] **Step 3: Commit** `docs(evidencia): compuerta después de P3`.

---

### Task 19: Runbook de los pasos 9–11 y medición E-INF-025 (DEPENDE DEL DUEÑO)

- [ ] **Step 1:** En `docs/evidencia/visitas.md` `## Runbook P3`: (paso 9) snapshot + `000068` a mano + binario con `TAREAS_FCM_ENABLED=false` + permiso concedido a los roles de oficina en `MSP_ROLES_PERMISOS` (el dueño, tras el arranque que sincroniza el catálogo) + escritorio con "Asignar visita"; criterio: tareas visibles en el teléfono canario por el periódico. (paso 10) el dueño instala la credencial FCM (`FCM_CREDENTIALS_FILE`), `TAREAS_FCM_ENABLED=true`, `config/visitas_avisos = {fcm: true, alarmas: true}` **solo** para el canario (el documento es global: mientras solo el teléfono del dueño tenga el APK de P3, equivale a canario); criterio: E-INF-024 medido, ningún aviso duplicado, ninguna tarea perdida. Reversa: `config/visitas_avisos.fcm = false, alarmas = false`. (paso 11) flota tras 7 días estables.
- [ ] **Step 2: E-INF-025** (FCM, 7 días en el canario): por cada tarea creada en el periodo, comparar `tareas.fcm ok` del servidor contra la llegada del sync expedito en el teléfono (log `sync_tareas_zona` disparado por push vs por periódico) y contar colapsos; registrar tasa de llegada y latencia. Alerta §12-6: permisos de notificación/alarma en `false` en algún teléfono → aviso al dueño.
- [ ] **Step 3: Commit** `docs(visitas): runbook de tareas y avisos (pasos 9-11)`.

---

## Self-review (2026-10-16)

- **Cobertura del spec P3:** §6.1 (Tarea 2), §6.2 (5, 9), §6.3 (3, 11), §6.4 (6, 14), §6.5 (12), §6.6 (13, 15, 16), §7 guardia (vigente desde P1), §8 simulación/adversario (7, 17), §9 E-INF-024/019 (16, 19), §11 pasos 9–11 (19), §12 alerta 6 (15, 19), DA-2/DA-15 (13), DA-10 (6, 14), DA-11 (3), DA-13 (5), DA-14 (6, 14).
- **Huecos del spec encontrados:** (1) ventana del canal de tareas no definida — se usa V = 92 días; (2) tareas canceladas viajan como cambio (no tombstone) — consecuencia de "nunca se borra"; (3) el escritorio no tiene permisos por acción — el botón se gatea por módulo y el servidor decide; (4) §6.6 pide excluir de la optimización de batería sin decir cómo — se usa la pantalla del sistema, no `REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`; (5) la compuerta "antes" de P3 cita E-INF-023/018, que solo pueden medirse con el código hecho — se condicionan al canario, no al código, porque ningún dato depende de que FCM o una alarma lleguen; (6) "editar = cancelar + asignar" se hace en una sola transacción (el spec no lo dice).
- **Placeholders:** ninguno de los prohibidos; los puntos de integración (alta del equipo, cambio de zona) llevan su `grep`.
- **Tipos:** `Tarea`, `NuevaTareaParams`, `EstadoTarea`, `EstadoDeTarea`, `AtendidaPor`, `DerivarEstadoTarea`/`derivarEstadoTarea`, `TareasRepo`, `AvisadorDeZona`, `MensajeDeZona`, `PermVisitasAsignarTareas`, `TareaZonaEntity`, `AvisosDao.marcarEmitido/reemplazarProgramados`, `CursorParSyncEngine`, `TareasZonaSyncManager`, `AvisoInmediato`, `PlanDeAvisos`, `ReprogramadorDeAlarmas`, `KillSwitchAvisos`, `SuscripcionDeZona`, `encolarSyncTareas` consistentes; reutilizan `CompromisoVigente`, `VisitaCompromiso`, `PagoCompromiso`, `CuentaCompromiso`, `LeerMarcaSegura`, `predicadoZona`, `epoch_repo` de P2.
