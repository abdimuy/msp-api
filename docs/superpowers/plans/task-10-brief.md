# Task 10 — Pulso refactor + narrativa helpers + in-memory repo fake

## Where this fits
Prep for the worker (Task 11) and read-path (Task 12). The pulso `comp` is currently built by a ~25-line block DUPLICATED in `ObtenerPulsoCliente` and `ObtenerPulsosClientes`. This task extracts it (behavior-preserving), adds two helpers the worker needs, and provides a reusable in-memory `NarrativaRepo` fake for worker/read-path tests. No worker, no read-path narrativa logic yet.

## Read first
- `internal/analytics/app/pulso_query.go` — the two duplicated comp blocks (single ~lines 65-92, list ~126-152) and the `pagos90Recientes`/`pagos90dFor` helpers.
- `internal/analytics/ports/outbound/narrative_generator.go` (Task 6) — `NarrativeInput`.
- `internal/analytics/app/rasgos_catalogo.go` (Task 4) — `CatalogoRasgos`.
- `internal/analytics/analytics_contracts_mapper.go` — `PulsoComputado`.
- The `domain.WinbackCandidato` accessors (`ClienteID()`, `Nombre()`, `Zona()`, `Frecuencia()`, `Monetary()`, `Saldo()`, `CadenciaDias()`, etc.).

## Changes

### 1. Extract `computePulso` (behavior-preserving refactor) — `pulso_query.go`
```go
// computePulso assembles the read-time PulsoComputado for one candidate. Shared
// by ObtenerPulsoCliente and ObtenerPulsosClientes (and the narrativa worker) so
// the scoring assembly lives in exactly one place.
func (s *Service) computePulso(c *domain.WinbackCandidato, now time.Time, p90 int) analytics.PulsoComputado
```
Move the EXACT comp-assembly block (the `seg, score, ... := compute*`; the `comp := analytics.PulsoComputado{...}`; `return comp`) into this method. Then both call sites become:
```go
p90 := pagos90dFor(live, ok, c)
comp := s.computePulso(c, now, p90)
```
(Keep the per-call-site `pagos90Recientes`/`pagos90dFor` exactly where they are — only the assembly moves.) This MUST be behavior-preserving: the existing `pulso_query_test.go` and related tests must still pass unchanged. Do not alter any computed value.

### 2. `candidatoYPulso` helper — `pulso_query.go`
```go
// candidatoYPulso loads a candidate and computes its pulso, mirroring the load
// path of ObtenerPulsoCliente without the contract projection. Used by the
// narrativa worker. Returns the same not-found error semantics as GetCandidato.
func (s *Service) candidatoYPulso(ctx context.Context, clienteID int) (*domain.WinbackCandidato, analytics.PulsoComputado, error)
```
Body: `c, err := s.repo.GetCandidato(ctx, clienteID)` (wrap errors the same way ObtenerPulsoCliente does — reuse the apperror pattern); `now := s.clock.Now()`; `live, ok := s.pagos90Recientes(ctx, []int{c.ClienteID()}, now)`; `p90 := pagos90dFor(live, ok, c)`; `return c, s.computePulso(c, now, p90), nil`.

### 3. `buildNarrativeInput` helper — new file `internal/analytics/app/narrativa_input.go`
```go
// buildNarrativeInput maps a candidate + its computed pulso + the trait catalog
// into the generator's fact-anchored input DTO.
func buildNarrativeInput(c *domain.WinbackCandidato, comp analytics.PulsoComputado, catalogo []domain.Rasgo) outbound.NarrativeInput
```
Map every `outbound.NarrativeInput` field from `c` accessors and `comp` fields (ClienteID, Nombre, Zona, Segmento, TierRiesgo, EstadoPago, BandaCredito, ScoreCredito, BandaRecompra, ScoreRecompra, BandaCLV, Saldo, Monetary, MontoCLV, Frecuencia, RecenciaDias, CadenciaDias, DiasAtrasoProm, PctPagosATiempo, CreditoResumen, RecompraResumen, CLVResumen, CreditoDrivers, RecompraDrivers, CLVDrivers, Catalogo=catalogo). Match field names exactly to the port struct.

### 4. In-memory `NarrativaRepo` fake — new package `internal/analytics/infra/narrativamem/repo.go`
```go
package narrativamem

// Repo is an in-memory outbound.NarrativaRepo for tests (worker, read-path). It
// is concurrency-safe and mirrors the Firebird adapter's contract: GetNarrativa
// miss returns (nil,nil); Encolar is idempotent by CLIENTE_ID; Upsert is one row
// per CLIENTE_ID.
type Repo struct { ... } // sync.Mutex + map[int]*outbound.NarrativaRow + map[int]outbound.PendienteRow
func New() *Repo
```
Implement all five methods with the same semantics as the real adapter (Get miss → nil,nil; Upsert replace-or-insert; Encolar idempotent overwrite by clienteID; ListarPendientes respects limit and returns deterministically e.g. sorted by clienteID; BorrarPendiente removes). Compile assertion `var _ outbound.NarrativaRepo = (*Repo)(nil)`. Optionally expose tiny inspection helpers (e.g. `Snapshot()`/counts) for test assertions — keep minimal.

## Tests
- `narrativa_input_test.go`: build a candidate + a fully-populated `PulsoComputado`, call `buildNarrativeInput(c, comp, CatalogoRasgos)`, assert representative fields map correctly (a band, a decimal magnitude, a drivers slice, and that `Catalogo` equals `CatalogoRasgos`).
- `internal/analytics/infra/narrativamem/repo_test.go`: exercise the fake — Get miss → nil; Upsert then Get round-trips; Upsert twice → one row; Encolar twice → ListarPendientes returns once; limit caps; BorrarPendiente removes. (This makes the double trustworthy.)
- The `computePulso`/`candidatoYPulso` extraction is covered by the EXISTING pulso tests — run them and confirm they still pass (cite this as the parity evidence). Do not write a redundant assertion-free parity test.
Pristine output.

## Constraints
- CLAUDE.md §2: `app` may import `domain`, `ports/outbound`, root `analytics`, stdlib, decimal. `infra/narrativamem` may import `ports/outbound` + `domain` + stdlib. No cycles.
- §3: English identifiers/comments.
- Behavior-preserving refactor — ZERO change to any computed pulso value. If any existing test changes output, you did it wrong; stop and report.

## Verification
- `go build ./...`
- `go test ./internal/analytics/...`  (existing pulso tests MUST still pass — that is the refactor's safety net)
- `golangci-lint run ./internal/analytics/...`

## Commit
`refactor(analytics): extrae computePulso y agrega helpers + fake de repo para narrativa`. No --no-verify. No Claude attribution footer.

## Report
Full report to `docs/superpowers/plans/task-10-report.md` (confirm existing pulso tests pass unchanged = parity proof). Reply ≤15 lines: status, commit SHA+subject, one-line test summary, concerns, report path.
