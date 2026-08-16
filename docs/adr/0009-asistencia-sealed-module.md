# ADR 0009 — Asistencia as a sealed, extractable module

- **Status:** Accepted
- **Date:** 2026-07-22
- **Decision drivers:** the attendance domain overlaps with nothing else in msp-api; there is a concrete plan to offer MSP the bonanza-api + kollect stack within months; and the workday register became a legal obligation, so this module will outlive whichever API happens to host it today.

## Context

msp-api is a modular monolith: vertical slices under `internal/{module}/`, hexagonal ports, and `depguard` rules that already forbid reaching into another module's `domain`, `app` or `infra`. Cross-module access is sanctioned only through a module's root contracts package.

The attendance module is unusual in this codebase. Every other module is entangled with Microsip by construction — ventas writes to `DOCTOS_PV`, cobranza mirrors `LIBRES_CARGOS_CC`, clientes syncs both ways. Attendance needs none of it. Its entire domain is employees, devices, punches and expected schedules. It has no reason to import `clientes`, `ventas`, `cobranza`, `inventario` or the Microsip adapter, and no reason to be imported by them.

Two forces make that isolation worth formalising rather than leaving to good intentions:

**Commercial.** There is an intention to offer MSP the bonanza-api + kollect stack in the coming months. Per CLAUDE.md §5, bonanza-api is deliberately a separate project with different stack assumptions. Anything built into msp-api today is, by default, coupled to the stack we intend to replace. Attendance is also plausibly sellable on its own, to a business that runs neither msp-api nor bonanza.

**Legal.** The decree of 2026-05-01 added LFT art. 132 fr. XXXIV, which obliges the employer to *"registrar de manera electrónica la jornada laboral de cada persona trabajadora"* and to hand it to the authority on request, with a 250–5,000 UMA fine under art. 994 fr. IV Bis (see `docs/research/checador-biometrico-estado-del-arte.md` §5.7). The module is compliance infrastructure, not a convenience feature. It has to keep working across a stack migration, not be rewritten during one.

### Microservice considered and rejected

Extracting attendance into its own service was evaluated. Production is Windows Server 2016 with no Docker, no Kubernetes and no orchestrator; binaries run as Windows Services under `nssm` (CLAUDE.md §5). In that environment every additional service costs a binary, an nssm entry, a port, a certificate, a deploy script and an independent failure mode — while delivering none of the benefits that make microservices cheap elsewhere, because there is nothing to restart, discover or balance them.

Checked against the usual drivers for extraction:

| Driver | Applies? |
|---|---|
| Independent scaling | No — ~15 employees, ~60 punches/day |
| Independent deploy cadence | Not today |
| Different language or runtime | No |
| Team boundaries | No — single maintainer |
| Fault isolation | No — the terminal buffers punches locally and uploads on reconnect, so isolation already exists at the device |
| **Distinct commercial packaging** | **Yes** |

Only the last one applies, and it calls for *extractability*, not distribution. Those are different properties, and the second is free.

## Decision

Attendance is built as a **sealed vertical slice** inside msp-api, designed so that extraction is mechanical.

1. **Location.** `internal/asistencia/{domain,app,ports,infra}`, following `docs/module-standards/MODULE_TEMPLATE.md`.

2. **Zero imports of any other module — including contracts.** This is stricter than the repo-wide rule, which permits cross-module contracts. Enforced by a new `depguard` rule `asistencia-sealed` in `.golangci.yml`, which allows only stdlib, `uuid`, `decimal`, `internal/asistencia/…` and `internal/platform/…`.

3. **The module owns its own wiring.** `internal/asistencia/module.go` exposes an `fx.Option` bundling every provider the module needs; `cmd/api/main.go` includes it as a single line. This is a deliberate deviation from the flat provider list currently in `appOptions()` — that flat list is exactly what would turn extraction into archaeology.

4. **Own tables, no foreign keys out.** Prefix `MSP_AS_*`. Attendance owns its employee catalog; it does not reference `CLIENTES`, the auth user tables, or anything Microsip. Hard rule #1 of this project — no logic in the database — is what makes the schema portable to another engine almost line for line.

5. **Auth behind a small port.** Two operations: who is the current user, and does the user hold a permission. The Firebase-backed implementation lives in `infra/`. Migrating means writing the other adapter.

6. **Firebird stays behind the repositories.** `firebird.ToWallClock`, `firebird.ScanUTCTime` and driver workarounds may not appear in `domain` or `app`.

7. **The frontend speaks HTTP only** against this module's endpoints, so kollect can consume the same API unchanged.

8. **Verified, not assumed.** `make check-sealed` fails if the module's transitive dependency graph contains any `internal/` package outside `asistencia` and `platform`.

## Consequences

**What extraction costs afterwards:** copy one directory, run the DDL on the target engine, write one auth adapter, and add a `cmd/asistencia/main.go` whose body is `fx.New(asistencia.Module(), …).Run()`. Producing a second binary from this same repository requires no code change at all — only a new composition root.

**Accepted costs:**

- **Duplicate employee records.** A cobrador already registered in Firestore, or a vendedor in Microsip, will be registered again in attendance. Accepted: most of the ~15 people who will clock in exist in no other system, and the seal is worth more than the duplication. The module may store an optional external identifier, but must never resolve it by importing another module.
- **`module.go` diverges from the current composition style.** Deliberate; see decision 3.
- **No direct cross-module reporting.** Crossing attendance with cobranza or ventas requires HTTP or a separate read model. Not a current requirement.

**Revisit when:** extraction actually happens, or a requirement appears that genuinely needs another module's data — in which case the correct move is an HTTP call or a read model, not relaxing the depguard rule.

## References

- `docs/research/checador-biometrico-estado-del-arte.md` — research, legal framework, UI proposal
- `docs/research/checador-features-checklist.md` — scope decisions feature by feature
- CLAUDE.md §2 (vertical slices), §5 (stack constraints), hard rule #1 (no logic in the database)
