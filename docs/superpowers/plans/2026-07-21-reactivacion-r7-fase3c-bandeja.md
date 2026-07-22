# Reactivación R7 · Fase 3c — Bandeja del operador — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this
> plan task-by-task (fresh subagent per task + spec/quality review between tasks). Steps use `- [ ]` tracking.
> Visual tasks additionally use superpowers:frontend-design for the SOTA look.

**Goal:** Build the operator bandeja — a 3-panel review-queue inbox that operates the copiloto's shadow mode
(AI drafts, human approves/edits/dictates/escalates) — consuming the Fase 3a API, plus the small backend DTO
enrichment it needs.

**Architecture:** Two repos, two worktrees. Backend (msp-api, worktree `fase3c-api`, branch
`feat/reactivacion-r7-fase3c`): enrich the 3a DTOs. Frontend (sistema-cobro-web, worktree `fase3c-web`, branch
`feat/reactivacion-r7-fase3c-bandeja`): a new hexagonal module `bandeja` calqued on `configuracion`. Visual
direction is locked by the mockups (`.superpowers/brainstorm/23612-1784662562/content/bandeja-sota.html` +
`bandeja-escalada.html`).

**Tech Stack:** Go + Huma (backend); React + TypeScript + Vite + Tailwind (class-based dark mode) + vitest +
MSW (frontend). Firebase auth bearer. shadcn primitives for shared dialog/toast; bespoke Tailwind/CSS for the
SOTA look.

**Spec:** `docs/superpowers/specs/2026-07-21-reactivacion-r7-fase3c-bandeja-design.md`.

## Global Constraints

- **Backend (CLAUDE.md):** no logic in DB (facts come from MSP_RX_COHORTE UTF8 via ClienteFactsReader; no new
  writes); code English / user messages Spanish lowercase no period; vertical slice; gofumpt; conventional
  commits WITHOUT Claude attribution; no `--no-verify`; `golangci-lint run ./...` = 0; `go test -race`;
  Windows cross-build; coverage gates domain ≥99 / app ≥90 / infra ≥80. NO regression to the 7 existing 3a
  endpoints.
- **Frontend:** hexagonal module mirroring `src/modules/configuracion` (domain / application{ports,usecases} /
  infrastructure{http,mappers} / presentation{context,composition,hooks} / components). apiClient copied from
  `rutas` (has `await auth.authStateReady()`). DTOs snake_case mirroring the Go DTOs; mappers throw
  `DomainError`. No i18n — inline Spanish. Money/dates formatted at the edge. Toasts = `sonner`; confirm =
  existing `ConfirmActionDialog`. Gate by module-key + role (backend enforces real security). Tests on ALL new
  code (adapters, hooks, pure helpers, one screen flow). `tsc --noEmit` clean; eslint no NEW warnings (repo has
  a pre-existing baseline). Conventional commits WITHOUT Claude attribution; no `--no-verify`.
- **Safety (from 3a, preserved in UI):** the bandeja NEVER shows debt figures or raw cobrador-note text; it
  shows only the distilled `contexto_nota` + `banderas`. Violet accent is EXCLUSIVE to AI; blue = human action.
  Binary confidence to the operator (Alta ≥65 solid / Baja <65 dashed); % on hover.
- **Theme:** mockup tokens as CSS custom properties with a dark AND a light set, flipped by the app's global
  theme class (`darkMode: ["class"]` + `ThemeContext`/`ThemeToggle`).

## File structure

**Backend (msp-api):**
- Modify `internal/reactivacion/app/listar_conversaciones.go` — hydrate Nombre/Segmento/UltimoMensaje.
- Modify `internal/reactivacion/app/obtener_conversacion.go` — hydrate Nombre/Segmento/Telefono.
- Modify `internal/reactivacion/infra/reactivacionhttp/copiloto_dto.go` + `copiloto_handlers.go` — DTO fields + mapping.
- Possibly add a small `ConversacionRepo` read (last inbound turno per cliente) OR reuse `ListarTurnos`.
- Tests: `copiloto_test.go`, `listar_conversaciones_test.go`, `obtener_conversacion_test.go`.

**Frontend (sistema-cobro-web) — `src/modules/bandeja/`:**
- `domain/entities/*.ts` — types + `DomainError` + pure helpers (cola bucket, binary confidence, draft-vs-briefing, estado label).
- `infrastructure/http/{apiClient.ts,HttpBandejaAdapter.ts,dtos.ts}` + `infrastructure/mappers/*.ts`.
- `application/ports/*.ts` + `application/usecases/*.ts`.
- `presentation/context/BandejaContext.tsx` + `presentation/composition/BandejaContainer.tsx` + `presentation/hooks/*.ts`.
- `components/{cola,conversacion,ficha,comunes}/*.tsx` + `bandeja.tokens.css` (or Tailwind theme layer).
- `presentation/BandejaScreen.tsx`.
- Modify `src/constants/modules.ts` + `src/App.tsx` (module `BANDEJA` + route).
- Colocated `*.test.ts(x)` throughout.

---

### Task 1 — Backend: enrich the copiloto DTOs (worktree fase3c-api)

**Files:**
- Modify: `internal/reactivacion/app/listar_conversaciones.go` (add Nombre/Segmento/UltimoMensaje to `ConversacionResumen`), `internal/reactivacion/app/obtener_conversacion.go` (add Nombre/Segmento/Telefono to the detalle), the app fakes/tests.
- Modify: `internal/reactivacion/infra/reactivacionhttp/copiloto_dto.go` (`ConversacionResumenDTO` += `nombre`,`segmento`,`ultimo_mensaje`; `ConversacionDTO` += `nombre`,`segmento`,`telefono`), `copiloto_handlers.go` (map the new fields).
- Test: `internal/reactivacion/infra/reactivacionhttp/copiloto_test.go`, `internal/reactivacion/app/{listar_conversaciones,obtener_conversacion}_test.go`.

**Interfaces:**
- Consumes: `outbound.ClienteFactsReader.GetFacts(ctx, clienteID) (*ClienteFacts{Nombre,Segmento,Telefono}, error)` (nil→empty); `ConversacionRepo.ListarTurnos(ctx, clienteID)` for the last inbound turno.
- Produces (the enriched JSON the frontend consumes):
  - `GET /v2/reactivacion/conversaciones` item: `{cliente_id, nombre, segmento, estado, asignado_a, updated_at, ultimo_mensaje, ultima_decision:{intencion,confianza,accion,resultado,razon_escalamiento}|null}`.
  - `GET /v2/reactivacion/conversaciones/{cliente_id}` `.conversacion`: existing fields + `{nombre, segmento, telefono}`.

- [ ] **Step 1 (test first):** In `listar_conversaciones_test.go`, add a case asserting each `ConversacionResumen` carries `Nombre`/`Segmento`/`UltimoMensaje` hydrated from the fake `ClienteFactsReader` + the fake conv repo's last inbound turno (empty when facts nil / no inbound). Run → FAIL.
- [ ] **Step 2:** Add `Nombre string`, `Segmento string`, `UltimoMensaje string` to `ConversacionResumen`; in `ListarConversaciones`, for each conversation call `factsReader.GetFacts` (tolerate nil) and take the last `direccion=entrante` turno's cuerpo (truncated to 120 runes) via `ListarTurnos`. Degrade to "" on any per-cliente error (log, don't fail the list). Run → PASS.
- [ ] **Step 3 (test first):** In `obtener_conversacion_test.go`, assert the detalle's conversacion carries `Nombre`/`Segmento`/`Telefono`. Run → FAIL.
- [ ] **Step 4:** Add those fields to the app detalle struct + hydrate from `GetFacts`. Run → PASS.
- [ ] **Step 5 (test first):** In `copiloto_test.go`, extend the list + detail happy-path tests to assert the new JSON fields (snake_case) with a `copilotofake`/fake facts returning realistic MX data (e.g. "María López", "recien_liquidado", "238 100 4521"). Run → FAIL.
- [ ] **Step 6:** Add `nombre`,`segmento`,`ultimo_mensaje` to `ConversacionResumenDTO` and `nombre`,`segmento`,`telefono` to `ConversacionDTO` (snake_case tags + `doc:`); map them in `copiloto_handlers.go`. Run → PASS.
- [ ] **Step 7 (verify):** `go build ./...`; `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/api`; `golangci-lint run ./...` = 0; `FB_DATABASE=… go test -race ./internal/reactivacion/... ./cmd/...`; coverage app ≥90 / http ≥80. No regression on the other endpoints.
- [ ] **Step 8 (commit):** `feat(reactivacion): enriquecer DTOs del copiloto para la bandeja (fase 3c)`.

### Task 2 — Frontend data layer: domain + infrastructure + application (worktree fase3c-web)

**Files (create under `src/modules/bandeja/`):**
- `domain/entities/{conversacion,turno,decision,bandeja}.ts` — types + `DomainError`.
- `domain/entities/helpers.ts` — pure: `colaBucket(item): 'te_necesitan'|'al_dia'`, `confianzaBinaria(n): 'alta'|'baja'` (umbral 65), `modoConversacion(detalle): 'borrador'|'briefing'|'ninguno'` (from the latest decision's accion/resultado), `estadoLabel(estado)`.
- `infrastructure/http/apiClient.ts` (copy `rutas`), `infrastructure/http/dtos.ts` (snake_case mirroring Task 1's JSON), `infrastructure/http/HttpBandejaAdapter.ts`, `infrastructure/mappers/*.ts`.
- `application/ports/BandejaPort.ts`, `application/usecases/*.ts`.
- Tests: `domain/entities/__tests__/helpers.test.ts`, `infrastructure/**/__tests__/*.test.ts`.

**Interfaces:**
- Consumes: the enriched endpoints from Task 1.
- Produces: `BandejaPort` interface — `listarCola(): Promise<ConversacionResumen[]>`, `obtenerConversacion(clienteId): Promise<ConversacionDetalle>`, `aprobar(clienteId)`, `editar(clienteId, texto)`, `dictar(clienteId, intencion): Promise<{borrador}>`, `escalar(clienteId, asignadoA)`, `simularEntrante(clienteId, mensaje): Promise<DecisionResult>`. Domain types `ConversacionResumen`, `ConversacionDetalle{conversacion,turnos,decisiones}`, `Turno`, `Decision`, `DecisionResult`.

- [ ] **Step 1 (test first):** `helpers.test.ts` — assert `colaBucket` (escalada/señal-compra/confianza-baja → te_necesitan; else al_dia), `confianzaBinaria` (64→baja, 65→alta), `modoConversacion` (latest decision responder+propuesto→'borrador'; escalado→'briefing'), `estadoLabel`. Run → FAIL.
- [ ] **Step 2:** Implement the domain types + pure helpers. Run → PASS.
- [ ] **Step 3 (test first):** `HttpBandejaAdapter.test.ts` with a `vi.fn()` stub axios client — assert each method hits the right URL/verb/body and maps dto→domain (and throws `DomainError` on a malformed payload). Run → FAIL.
- [ ] **Step 4:** Implement `apiClient` (copy rutas), `dtos.ts`, `HttpBandejaAdapter`, mappers, port. Run → PASS.
- [ ] **Step 5 (verify):** `npx tsc --noEmit` clean; `npx vitest run src/modules/bandeja` green.
- [ ] **Step 6 (commit):** `feat(bandeja): capa de datos (dominio + adapter + puertos) (fase 3c)`.

### Task 3 — Frontend plumbing, gating, theme (worktree fase3c-web)

**Files:**
- Create: `presentation/context/BandejaContext.tsx`, `presentation/composition/BandejaContainer.tsx`, `presentation/hooks/{useCola,useConversacion,useAccionesBorrador}.ts`, `presentation/BandejaScreen.tsx` (shell), `components/bandeja.tokens.css` (dark + light custom properties from the mockup).
- Modify: `src/constants/modules.ts` (add `BANDEJA` to DESKTOP_MODULES w/ `requiredRole:[SUPER_ADMIN,ADMIN]`, ROUTE_TO_MODULE, PROTECTED_MODULES), `src/App.tsx` (route with `<ProtectedRoute requiredModule="BANDEJA" requiredRole={[SUPER_ADMIN,ADMIN]}>`).
- Tests: `presentation/hooks/__tests__/*.test.ts`.

**Interfaces:**
- Consumes: `BandejaPort` (Task 2).
- Produces: `useCola()` → `{items, loading, error, refetch}` with ~20s polling; `useConversacion(clienteId)` → `{detalle, loading, refetch}`; `useAccionesBorrador(clienteId, refetchAll)` → `{aprobar,editar,dictar,escalar,simular, estado:'idle'|'enviando'|'hecho'|'error'}`. `BandejaContext` provides the port + selected clienteId. Route path `/bandeja`.

- [ ] **Step 1 (test first):** hook tests with a hand-rolled fake `BandejaPort` + `renderHook` — assert `useCola` polls (fake timers) and refetches after an action; `useAccionesBorrador` transitions idle→enviando→hecho and calls `refetchAll`. Run → FAIL.
- [ ] **Step 2:** Implement context, container, hooks. Run → PASS.
- [ ] **Step 3:** Define `bandeja.tokens.css` — the mockup's tokens as `:root`/`.dark` custom properties (dark + light), Inter, tabular-nums utility. Wire `BandejaScreen` shell (3-column grid, empty panels) reading the port via context.
- [ ] **Step 4:** Register the module (`modules.ts`) + route (`App.tsx`). Sidebar renders automatically.
- [ ] **Step 5 (verify):** `npx tsc --noEmit` clean; `npx vitest run src/modules/bandeja` green; app boots (`npm run dev`) and `/bandeja` renders the (empty) 3-column shell gated to admin. (Controller does the live visual check.)
- [ ] **Step 6 (commit):** `feat(bandeja): plumbing + gating + tokens de tema (fase 3c)`.

### Task 4 — Frontend core panels: Cola + Conversación (hilo + borrador) + Ficha (worktree fase3c-web)

> Use superpowers:frontend-design for the SOTA look (match the `bandeja-sota.html` mockup: true-grey layers,
> violet AI accent, Inter, tabular numbers, the exact card/composer treatment). Both themes.

**Files:**
- Create: `components/cola/{ColaPanel,QueueItem}.tsx`, `components/conversacion/{ConversacionPanel,Hilo,Burbuja,BorradorComposer}.tsx`, `components/ficha/{FichaPanel,NotaCard,BanderasCard,RecomiendaCard}.tsx`.
- Modify: `presentation/BandejaScreen.tsx` (mount the three panels).
- Tests: colocated component tests + one screen-flow test (MSW): select a conversation → see the draft composer → Aprobar → refetch.

**Interfaces:**
- Consumes: hooks from Task 3 + domain helpers from Task 2.
- Produces: the rendered 3-panel screen for the "responder/borrador" path (escalada briefing is Task 5).

- [ ] **Step 1 (test first):** `ColaPanel.test.tsx` — given fake items, renders `Te necesitan` (escaladas first) and `Al día` sections with segment chips, name, `ultimo_mensaje` preview, binary confidence dot; clicking an item selects it. Run → FAIL.
- [ ] **Step 2:** Implement `ColaPanel` + `QueueItem` (frontend-design). Run → PASS.
- [ ] **Step 3 (test first):** `BorradorComposer.test.tsx` — given a detalle whose latest decision is responder+propuesto, renders the borrador, binary confianza (Alta/Baja + % on hover), "Por qué" (razon) + evidencia chips, and Aprobar/Editar/Dictar/Escalar; Aprobar calls the action + on success the composer clears (shadow "drains"). Editar reveals an inline textarea → EditarYAprobar. Run → FAIL.
- [ ] **Step 4:** Implement `Hilo`/`Burbuja` (cliente left neutral, ia/humano right; day separators; timestamps) + `BorradorComposer` (violet spine, ✦ label) wired to `useAccionesBorrador`. Confirm destructive via `ConfirmActionDialog`. Run → PASS.
- [ ] **Step 5 (test first):** `FichaPanel.test.tsx` — renders identity (nombre/segmento/telefono/estado chips), the distilled `contexto_nota` card, `banderas` danger card, and "La IA recomienda" (from latest decision). Asserts NO raw note text / NO debt figures rendered. Run → FAIL.
- [ ] **Step 6:** Implement the ficha components. Run → PASS.
- [ ] **Step 7 (screen flow, MSW):** `BandejaScreen.test.tsx` — mount the container with MSW stubbing the enriched endpoints; select a conversation → composer shows → click Aprobar → assert the approve endpoint was called and the queue/detail refetched. Run → PASS.
- [ ] **Step 8 (verify):** `tsc` clean; `vitest run src/modules/bandeja` green; live check `/bandeja` renders the SOTA inbox for a proposed-draft conversation (controller drives the live visual verification with a simulated inbound).
- [ ] **Step 9 (commit):** `feat(bandeja): paneles cola + conversación + ficha (SOTA) (fase 3c)`.

### Task 5 — Frontend escalation briefing + Dictar + Simular-entrante (worktree fase3c-web)

> Use superpowers:frontend-design for the amber briefing (match `bandeja-escalada.html`). Both themes.

**Files:**
- Create: `components/conversacion/BriefingEscalada.tsx`, `components/conversacion/DictarControl.tsx`, `components/comunes/SimularEntranteControl.tsx`.
- Modify: `components/conversacion/ConversacionPanel.tsx` (render Briefing when `modoConversacion==='briefing'`), `presentation/BandejaScreen.tsx` (mount `SimularEntranteControl` for admins).
- Tests: colocated component tests + a screen-flow test (simulate a buy-signal inbound → escalates → briefing appears).

**Interfaces:**
- Consumes: hooks from Task 3, helpers from Task 2.
- Produces: the escalation briefing path + the dictar + simulate-inbound affordances. Completes the screen.

- [ ] **Step 1 (test first):** `BriefingEscalada.test.tsx` — given an escalated detalle, renders the amber briefing: structured fields (intención, por qué escaló=razon_escalamiento, confianza, señales/entidades) + suggested next step (derived from decision, never invented) + Tomar/Dictar/Escalar. Run → FAIL.
- [ ] **Step 2:** Implement `BriefingEscalada` (frontend-design; amber spine) + wire `ConversacionPanel` to switch composer↔briefing by `modoConversacion`. Run → PASS.
- [ ] **Step 3 (test first):** `DictarControl.test.tsx` — an intent input; submit → calls `dictar` → the returned borrador appears as a pending draft (composer). Run → FAIL.
- [ ] **Step 4:** Implement `DictarControl`. Run → PASS.
- [ ] **Step 5 (test first):** `SimularEntranteControl.test.tsx` — admin-only; input + send → calls `simularEntrante` → refetches the conversation + cola. Renders nothing for a non-admin role. Run → FAIL.
- [ ] **Step 6:** Implement `SimularEntranteControl` (gated by role). Run → PASS.
- [ ] **Step 7 (screen flow, MSW):** simulate a buy-signal inbound → the conversation escalates → the briefing renders (not a draft). Run → PASS.
- [ ] **Step 8 (verify):** `tsc` clean; `vitest run src/modules/bandeja` green; full-repo `vitest run` no regressions; eslint no NEW warnings. Live check: simulate inbound → escalate briefing + dictar path (controller drives).
- [ ] **Step 9 (commit):** `feat(bandeja): briefing de escalada + dictar + simular entrante (fase 3c)`.

---

## Self-review

- **Spec coverage:** §4 enrichment→Task 1; §5 domain/infra/app→Task 2, presentation/gating/theme→Task 3,
  cola/conversación/ficha→Task 4, briefing/dictar/simular→Task 5; §6 freshness→Task 3 hooks; §7 theme→Task 3
  tokens + Task 4/5 frontend-design; §8 gating→Task 3; §9 tests→every task. Out-of-scope (accuracy header,
  deep identity, 3b, voice) correctly absent.
- **Placeholder scan:** none — each task names exact files, the enriched JSON contract, and concrete test
  assertions. Visual component code is produced via frontend-design at implementation (the plan fixes the
  contracts + test requirements, which is the reviewable boundary).
- **Type consistency:** `ConversacionResumen`/`ConversacionDetalle`/`Turno`/`Decision`/`BandejaPort` names are
  used consistently across Tasks 2–5; the enriched fields (nombre/segmento/telefono/ultimo_mensaje) defined in
  Task 1 are consumed by the DTOs in Task 2.

## Execution

Backend Task 1 in worktree `fase3c-api`; Frontend Tasks 2–5 in worktree `fase3c-web`. Subagent-driven:
fresh subagent per task, spec+quality review between tasks, whole-branch review per repo at the end, then the
controller's final verification (build/lint/tests/coverage + live visual check of the bandeja driven by a
simulated inbound). No `main` touched; user decides the merge.
