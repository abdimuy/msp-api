# Task 16 Report — FichaLecturaAnalista (IA) panel + RasgoBadge chips

## Status
DONE. Committed on feat/analytics-narrativa-ia.

## Commit
- SHA: `84d58ad`
- Subject: `feat(clientes): panel de lectura del analista (IA) y rasgos conductuales en la ficha`
- 11 files changed, 260 insertions, 37 deletions
- 4 new files created

## Panel/Titular extraction choice
**Extracted** (not replicated). `Panel` and `Titular` were moved from `FichaInteligenciaScores.tsx` into `src/modules/clientes/components/ficha/lib/Panel.tsx` as a pure move — identical markup and props, no behavior change. `FichaInteligenciaScores.tsx` now imports them from `./lib/Panel`. The local `InfoHint` import in `FichaInteligenciaScores.tsx` was removed since `Panel.tsx` handles it internally.

## FichaInteligenciaScores tests
All 18 existing FichaInteligenciaScores tests passed unchanged after extraction. The extraction was truly byte-identical — no visual or behavior regression.

## Verification results
- **tsc --noEmit**: clean (0 errors). Initial run caught two issues: unused `container` variable in one test case, and the MSW handler fixture (`src/test/msw/handlers/clientes.ts`) missing `narrativa`/`rasgos_ia` in its hardcoded pulso object. Both fixed before commit.
- **eslint** (all touched files, --max-warnings 0): clean (0 warnings, 0 errors).
- **vitest run src/modules/clientes**: 54 test files, 584 tests, all passed. Includes 6 new FichaLecturaAnalista tests + 2 new mapper tests.

## Files changed
| File | Change |
|------|--------|
| `infrastructure/http/dtos.ts` | Added `narrativa: string` + `rasgos_ia: string[]` to PulsoDTO |
| `domain/entities/FichaCliente.ts` | Added `narrativa?: string` + `rasgosIA?: readonly string[]` to Pulso |
| `infrastructure/mappers/dtoToFichaCliente.ts` | Mapped both: `narrativa || undefined`, `rasgosIA` gated on non-empty array |
| `infrastructure/mappers/dtoToFichaCliente.test.ts` | Added `narrativa: ""` + `rasgos_ia: []` to buildValidDTO() fixture; 2 new test cases |
| `components/ficha/lib/Panel.tsx` | NEW — shared Panel + Titular extracted from FichaInteligenciaScores |
| `components/ficha/FichaInteligenciaScores.tsx` | Replaced local Panel/Titular definitions with import from lib/Panel; removed InfoHint import |
| `components/ficha/RasgoBadge.tsx` | NEW — indigo chip for AI-assigned traits (visually distinct from deterministic badges) |
| `components/ficha/FichaLecturaAnalista.tsx` | NEW — renders nothing when empty; Panel + Titular + RasgoBadge when data present |
| `components/ficha/FichaLecturaAnalista.test.tsx` | NEW — 6 tests covering all 4 scenarios (both/narrativa-only/rasgos-only/empty) |
| `components/ficha/ClienteFicha.tsx` | Mounted `<FichaLecturaAnalista pulso={ficha.pulso} />` after FichaInteligenciaScores in Zona 2 |
| `src/test/msw/handlers/clientes.ts` | Added `narrativa: ""` + `rasgos_ia: []` to MSW fixture pulso object |

## Concerns
None. The empty-guard (`!hasNarrativa && !hasRasgos → null`) is the first thing FichaLecturaAnalista checks, so the LLM-disabled default (empty strings + empty arrays) correctly renders nothing — no regression risk for existing ficha.
