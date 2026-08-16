# Plan — Fase 2: "Lectura del analista (IA)" + Rasgos conductuales (IA) en la ficha (LLM local)

## Context
La Fase 1 (ya mergeada) dio, por cada score, un **titular determinista** + viñetas cuantificadas. Lo que las plantillas NO pueden hacer es **conectar los tres scores** (crédito/recompra/CLV), resolver la contradicción ("compra bien pero vale $0"), recomendar una acción interna, y asignar **rasgos conductuales matizados** que serían frágiles de codificar como reglas. Eso es criterio de analista — el caso del LLM.

Esta fase agrega, en la ficha (análisis interno de oficina; nada hacia el cliente), generado por un LLM **local** (Qwen3 4B vía Ollama en dev / `llama-server.exe` en el server) **anclado a los hechos ya calculados** (nunca inventa números):
1. **Lectura del analista (IA)** — un párrafo de **síntesis + acción recomendada interna**.
2. **Rasgos (IA)** — la IA **elige 1-3 rasgos de un catálogo curado por el usuario** (p.ej. "Leal pero estancado", "Recuperable con promo", "Sensible al enganche"), con salida estructurada `enum` validada contra el catálogo.

Ambos salen de **una sola llamada** al LLM: `{ narrativa, rasgos: [códigos del catálogo] }`.

Principios:
- **El LLM narra y SELECCIONA, nunca calcula ni clasifica el riesgo.** Los **badges deterministas existentes (riesgo/crédito/segmento/tier) se quedan intactos y son la verdad auditable**; los **rasgos IA son una categoría nueva, descriptiva, claramente etiquetada como IA, que complementa (no reemplaza)**.
- **Salida acotada al catálogo** → el espacio de salida es finito y validable (se descarta cualquier rasgo fuera del catálogo). "Lo determinista filtra al LLM": **chequeo de dirección** sobre la narrativa + validación `⊆ catálogo` sobre los rasgos.
- **Caché materializada + generación lazy-encolar-en-vista + worker de fondo.** La ficha lee de una tabla (0 latencia/costo por vista); solo se genera para clientes que alguien abre. El **`input_hash` comparado en la vista ES la invalidación**.
- Si el LLM falla, devuelve rasgo inválido, o está apagado → **se degrada al titular determinista de Fase 1 + sin rasgos IA** (sin regresión).

**Restricción operativa del usuario:** construir TODO menos *correr el modelo de AI* (su Mac está cargada). El cliente LLM viene **`disabled` por defecto** y todo se prueba con un **`NarrativeGenerator` fake**. Levantar Ollama + correr el modelo real es el **último paso, gateado, con autorización explícita**.

Datos reales (Firebird): 43,399 clientes con pulso, 9,686 con saldo. Con lazy-on-view solo se generan los vistos.

## Global Constraints (binding)
- CLAUDE.md §1: NO logic in DB. Migrations structural only. UUID via `uuid.New()`, timestamps via `time.Now()` + `firebird.ToWallClock`. Every INSERT passes ID/CREATED_AT/UPDATED_AT explicitly. No DEFAULT/trigger/procedure/CHECK-business-rule on MSP_* tables.
- CLAUDE.md §2: vertical slices. Cross-module access ONLY via contracts package. `clientes` reaches `analytics` only through `analytics_contracts.go`.
- CLAUDE.md §3: code/comments/error-codes in English; user-facing messages in Spanish (lowercase, no trailing period).
- CLAUDE.md §6/§5: filesystem blob only; Windows Server 2016 target; cross-compile clean (no cgo).
- Dates: UTC in Go, `firebird.ToWallClock` on write, `firebird.ScanUTCTime` on read (DATETIME_HANDLING.md).
- Encoding: MSP_* columns UTF8; NFC in domain (ENCODING_HANDLING.md).
- **LLM `disabled` by default** (`LLM_ENABLED=false`). All Go tests use a fake `NarrativeGenerator`. Running the real model is the gated final step (NOT in scope of automated execution).
- Degrade gracefully: LLM off / failure / invalid trait → Fase 1 deterministic titular, no IA traits, NO regression.
- LIST path (`ObtenerPulsosClientes`) must NOT touch narrativa/rasgos.

## Decisiones de arquitectura
- **Analytics es dueño** de narrativa+rasgos (derivan del pulso). Se exponen en `ClientePulsoContract.Narrativa string` y `ClientePulsoContract.RasgosIA []string` (etiquetas display ya resueltas del catálogo), igual que los `*Resumen` de Fase 1. `clientes` solo mapea contrato → DTO.
- **Catálogo de rasgos** = artefacto curado en Go (`app/rasgos_catalogo.go`): lista de `{Codigo, Etiqueta, Definicion (~50 palabras)}`. **El usuario es dueño del contenido**; arranco con una lista inicial (~12) que él refina. Los rasgos son **conductuales/descriptivos** y NO duplican los badges deterministas (nada de "Moroso"/"CRÍTICO" — eso ya es badge).
- **Una sola llamada LLM**, salida estructurada `{ narrativa: string, rasgos: [codigo enum] }`. Local: usar JSON-schema/`format` de Ollama o gramática GBNF de llama.cpp para constreñir; si no, prompt estricto + parse + validar. Se descartan códigos fuera del catálogo y se capan a 3.
- **Cliente LLM** = paquete platform OpenAI-compat con **factory + fallback disabled** (patrón `internal/platform/meilisearch/`). `LLM_ENABLED=false` por defecto → worker no-op, ficha muestra titulares Fase 1, sin rasgos.
- **Generación lazy-encolar-en-vista** (NO síncrona): el read-path no bloquea; encola y deja narrativa/rasgos vacíos. Worker de fondo serializado drena la cola.
- **Invalidación por `input_hash`** = `sha256(bandaCredito|bandaRecompra|bandaClv|creditoResumen|recompraResumen|clvResumen)` — cambia exactamente cuando narrativa/rasgos deberían cambiar.

## Tablas nuevas — migración `migrations-firebird/000040_*.up.sql`
Convención (ver `000039_*`/`000035_*`): CHAR(36) ASCII UUID, TIMESTAMP sin DEFAULT/trigger, valores desde Go (CLAUDE.md §1), header "Por qué", trailer `INSERT INTO MSP_MIGRATIONS`.
- **`MSP_AN_CLIENTE_NARRATIVA`** — `ID CHAR(36) PK`, `CLIENTE_ID INTEGER UNIQUE`, `NARRATIVA BLOB SUB_TYPE TEXT CHARSET UTF8`, `RASGOS BLOB SUB_TYPE TEXT CHARSET UTF8` (JSON array de códigos validados), `INPUT_HASH CHAR(64) ASCII`, `MODELO VARCHAR(64)`, `GENERADA_EN TIMESTAMP`, `CREATED_AT`, `UPDATED_AT`.
- **`MSP_AN_NARRATIVA_PENDIENTE`** — `CLIENTE_ID INTEGER PK` (idempotente → cola acotada), `INPUT_HASH CHAR(64)`, `ENCOLADA_EN TIMESTAMP`. (down migration drops both.)

## Backend — `internal/analytics`
1. **Migración 000040** (las dos tablas) + down.
2. **Catálogo** (`app/rasgos_catalogo.go`): `var CatalogoRasgos = []Rasgo{{Codigo, Etiqueta, Definicion}, ...}` (~12 conductuales: "Leal pero estancado", "Recuperable con promo", "Sensible al enganche", "Comprador de temporada", "Paga en rachas", "Cumplido constante", "Dormido valioso", "Alto valor en riesgo", "Contado confiable", "Riesgo de fuga", etc. — lista inicial; el usuario la refina). Helpers: `EsRasgoValido(codigo) bool`, `EtiquetaDe(codigo) string`.
3. **Domain/app** (`domain/narrativa.go` + helper en `app/`): VO `Narrativa{ClienteID, Texto, Rasgos []string, InputHash, Modelo, GeneradaEn}` + `NarrativaInputHash(comp analytics.PulsoComputado) string`.
4. **Port generador** (`ports/outbound/narrative_generator.go`): `NarrativeGenerator interface { Generar(ctx, NarrativeInput) (NarrativeOutput, error) }`; `NarrativeInput` (hechos: bandas, scores, saldo, díasSinPagar, cadencia, ticket, recenciaMeses, drivers, 3 resúmenes, **+ el catálogo de rasgos con definiciones**); `NarrativeOutput{ Narrativa string; Rasgos []string }`.
5. **Port repo** (`ports/outbound/narrativa_repo.go`): `GetNarrativa`, `UpsertNarrativa`, `Encolar`, `ListarPendientes(limit)`, `BorrarPendiente`. Impl en `infra/analyticsfb/` (UUID/time desde Go; serializa `Rasgos` a JSON).
6. **Platform LLM** (`internal/platform/llm/`): `client.go` (interface `Chat(ctx, ChatReq) (string, error)`, con soporte de `response_format`/json-schema), `openai_compatible.go` (raw `net/http`, clasificación error transitorio/permanente), `disabled.go` (`ErrLLMDisabled`), `factory.go`.
7. **Config** (`internal/platform/config/config.go`): struct `LLM{ BaseURL; Model envDefault:"qwen3:4b"; Enabled envDefault:"false"; Timeout envDefault:"30s" }` + en `Config` + `validate()`.
8. **Generador** (`infra/llm/generator.go`): arma prompt anclado (system: "analista interno; narra sobre estos hechos, NO inventes números ni cambies bandas; un párrafo español neutro síntesis+recomendación; y ELIGE 1-3 rasgos SOLO de este catálogo"). Pide salida estructurada `{narrativa, rasgos:[codigo]}`. Parsea.
9. **Validación** (`app/narrativa_validate.go`): (a) **dirección** de la narrativa (banda coherente; sin "buen pagador" si CRÍTICO; longitud) → on fail, fallback = concatenar titulares; (b) **rasgos**: filtrar `EsRasgoValido`, capar a 3, dedup. Devuelve `Narrativa` validada.
10. **Worker** (`app/narrativa_worker.go`): `lifecycle.Hooks` (patrón `refresh_worker.go`), ticker, `BatchSize` chico, **serializado**. Tick: `ListarPendientes` → por cliente: `GetCandidato` + recomputar pulso → `NarrativaInputHash` → `NarrativeInput` (con catálogo) → `Generar` → validar → `UpsertNarrativa` → `BorrarPendiente`. `LLM_ENABLED=false` → no-op.
11. **Read-path** (`app/pulso_query.go` `ObtenerPulsoCliente`, tras `comp`): `hash := NarrativaInputHash(comp)`; `GetNarrativa` → hit y hash igual → `comp.Narrativa = row.Texto`, `comp.RasgosIA = EtiquetaDe(row.Rasgos)`; si no y `LLM_ENABLED` → `Encolar`. **`ObtenerPulsosClientes` (LIST) NO toca narrativa/rasgos.**
12. **Contrato** (`analytics_contracts.go` + mapper): `ClientePulsoContract.Narrativa string` + `RasgosIA []string`; `PulsoComputado` + `ToClientePulsoContract`.
13. **Wiring** (`cmd/api/analytics_wiring.go` + `main.go`): `fx.Provide` platform LLM client, generador, worker; `fx.Invoke` lifecycle del worker.

## Backend — `internal/clientes`
14. **DTO** (`infra/clienteshttp/dto.go` `PulsoDTO`): `Narrativa string json:"narrativa"`, `RasgosIA []string json:"rasgos_ia"`. **Mapper** (`dto_mapper.go` `toFichaDTO`): mapear ambos desde el contrato.

## Frontend — `sistema-cobro-web`
15. **DTO/Entity/Mapper**: `PulsoDTO.narrativa`, `PulsoDTO.rasgos_ia: string[]`; `Pulso.narrativa?`, `Pulso.rasgosIA?: readonly string[]`; mapear en `dtoToFichaCliente`.
16. **Componente** `components/ficha/FichaLecturaAnalista.tsx` (reusa `Panel`/`Titular`): título "Lectura del analista (IA)", el párrafo (`Titular`), y debajo una **fila "Rasgos (IA)"** de chips (reusar/crear un `RasgoBadge` con estilo claramente distinto de los deterministas + `InfoHint` "asignado por IA"). Solo renderiza lo que no esté vacío. Montar en `ClienteFicha.tsx` Zona 2 tras `FichaInteligenciaScores`. Los **badges deterministas no cambian**.
17. **Tests FE**: `FichaLecturaAnalista.test.tsx` (con narrativa/rasgos / sin ellos) + extender `dtoToFichaCliente.test.ts`.

## Infra dev — Ollama (definido, NO arrancado)
18. Agregar servicio `ollama` a `compose.yml` (imagen `ollama/ollama`, volumen `msp-ollama-data`, puerto 11434). **No** `up` ni `pull` (eso es el paso gateado). `.env` dev con `LLM_ENABLED=false`.

## Tests Go (sin modelo)
- **Fake `NarrativeGenerator`** devuelve `{narrativa, rasgos:[...incluyendo uno inválido...]}` → worker genera, validación descarta el inválido, upserta, read-path sirve, contract/DTO/FE renderiza — end-to-end sin LLM real.
- `NarrativaInputHash` determinista + invalidación (cambia banda → re-encola).
- Validación: dirección (rechaza contradictoria → fallback) + rasgos (`⊆ catálogo`, cap 3, dedup).
- Catálogo: `EsRasgoValido`/`EtiquetaDe`.
- Repos Firebird con `fbtestutil.WithTestTransaction` (rollback).
- Read-path: hit sirve caché; miss con `LLM_ENABLED` encola; bulk no toca; `disabled` no encola.

## PASO FINAL — GATEADO (requiere autorización explícita; no se ejecuta antes)
19. `docker compose up -d ollama` + `ollama pull qwen3:4b`; `LLM_ENABLED=true`, `LLM_BASE_URL=http://ollama:11434/v1`, `LLM_MODEL=qwen3:4b`. Abrir la ficha de **BRENDA** + 2-3 reales; ver calidad del párrafo y de los rasgos elegidos; **afinar prompt + catálogo + chequeo de dirección** iterando. Solo aquí se consume CPU/modelo en la Mac.

## Verificación
- **Backend (sin modelo)**: `go build ./...`, `golangci-lint run ./internal/analytics/... ./internal/clientes/...`, `go test ./internal/analytics/... ./internal/clientes/...` (fake generator). `make test-firebird-all` (rollback).
- **Frontend**: `npx tsc --noEmit`, `npx eslint <tocados>`, `npx vitest run src/modules/clientes`.
- **En vivo sin modelo**: ficha muestra titulares Fase 1, sin narrativa ni rasgos, **sin regresión**; worker idle.
- **En vivo con modelo (paso 19, gateado)**: BRENDA muestra párrafo síntesis+acción + 1-3 rasgos coherentes del catálogo.

## Gobernanza / fuera de alcance
- **Auditoría mensual** de asignaciones de rasgos por muestreo — proceso operativo, se documenta; no es código de esta fase.
- **Contrafactual** — fase posterior.
- **Pre-warm proactivo** por event-bus — opcional luego.
- Endpoint LIST `GET /clientes` (no muestra narrativa/rasgos).
- Deploy en server WS2016 (`llama-server.exe` vía nssm) — cuando el usuario lo decida.
