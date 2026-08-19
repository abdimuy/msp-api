# Task 16 — Frontend: "Lectura del analista (IA)" panel + "Rasgos (IA)" chips

## Repo & branch
Work in **`/Volumes/M2-1TB/Developer/sistema-cobro-web`** (separate repo). Branch `feat/analytics-narrativa-ia` is already created and checked out. This is React + TypeScript + Vite + Tailwind, tested with Vitest + @testing-library/react.

## Read first — the anchor doc has verbatim code
`/Volumes/M2-1TB/Developer/sistema-cobro-web/NARRATIVA_ANCHORS.md` — exact file paths, type bodies, component props, Tailwind classes, and test patterns. Use it as your map.

## Backend contract (what the API now returns inside ficha `pulso`)
Two new fields (added on the Go side): `"narrativa": string` (analyst paragraph; "" when LLM off / not generated / fallback) and `"rasgos_ia": string[]` (Spanish display labels; [] when none). Both are often empty — the UI must render NOTHING when they're empty (no empty panel, no regression).

## Changes

### 1. DTO — `src/modules/clientes/infrastructure/http/dtos.ts` (`PulsoDTO`)
Add after `clv_resumen: string`:
```ts
  narrativa: string;
  rasgos_ia: string[];
```

### 2. Entity — `src/modules/clientes/domain/entities/FichaCliente.ts` (`Pulso`)
Add after `clvResumen?: string`:
```ts
  narrativa?: string;
  rasgosIA?: readonly string[];
```

### 3. Mapper — `mapPulso()` in `src/modules/clientes/infrastructure/mappers/dtoToFichaCliente.ts`
Map both, mirroring how the existing `*_resumen` / `clv_drivers` fields are mapped (e.g. coerce empty/undefined cleanly — empty string → `undefined` or `""` consistent with how `creditoResumen` is handled; map `rasgos_ia` → `rasgosIA` as a readonly array, `[]`/undefined tolerated). Follow the file's existing coercion conventions exactly.

### 4. Reuse Panel + Titular (avoid duplication)
`Panel` and `Titular` are currently LOCAL sub-components inside `src/modules/clientes/components/ficha/FichaInteligenciaScores.tsx`. Extract them into a shared module (e.g. `src/modules/clientes/components/ficha/lib/Panel.tsx` exporting `Panel` + `Titular`) as a PURE move (identical markup/props/classes), and import them in BOTH `FichaInteligenciaScores.tsx` and the new component. `FichaInteligenciaScores` must render byte-identically (its existing tests must pass unchanged). If extraction proves risky, fall back to replicating the exact Tailwind classes in the new component and note why — but prefer extraction.

### 5. New component — `src/modules/clientes/components/ficha/FichaLecturaAnalista.tsx`
Props: `{ pulso?: Pulso }` (match how `FichaInteligenciaScores` receives `pulso`).
- Render NOTHING (return `null`) when there is neither a non-empty `pulso.narrativa` NOR any `pulso.rasgosIA?.length`.
- Otherwise render a `Panel` titled **"Lectura del analista (IA)"** with a `titleHint` via `InfoHint` (`src/modules/clientes/components/ficha/lib/InfoHint.tsx`) text **"Asignado por IA"** (short — memory: UI text minimalista 2-4 words, tono profesional neutro).
  - If `narrativa` non-empty: render it through `Titular` (`text={pulso.narrativa}`).
  - If `rasgosIA?.length`: render a labeled row **"Rasgos (IA)"** (subtitle style from the anchor doc) containing the chips — one `RasgoBadge` per label. Render the row only when there is ≥1 rasgo.

### 6. New component — `src/modules/clientes/components/ficha/RasgoBadge.tsx`
A chip for an AI-assigned trait, VISUALLY DISTINCT from the deterministic badges (e.g. `SegmentoBadge`) so users never confuse an AI trait with an auditable badge. Reuse the `ColorConfig` pattern from `SegmentoBadge.tsx` but with a DISTINCT accent (pick one AI accent — e.g. a violet/indigo family — and keep ALL ai-trait chips that single accent; they are descriptive, not graded). Props: `{ label: string }`. Keep it small and consistent with the design system (rounded, border, dot or a subtle "IA"/sparkle marker is fine but optional — do not overdesign). The deterministic badges/components MUST NOT change.

### 7. Mount — `src/modules/clientes/components/ficha/ClienteFicha.tsx` (Zona 2)
Immediately AFTER `<FichaInteligenciaScores pulso={ficha.pulso} />` add:
```tsx
        <FichaLecturaAnalista pulso={ficha.pulso} />
```
Nothing else in ClienteFicha changes. The deterministic scores/badges stay exactly as they are.

## Tests
### `src/modules/clientes/components/ficha/FichaLecturaAnalista.test.tsx` (Vitest + @testing-library/react — match the preamble/style of an existing `*.test.tsx`)
- narrativa + rasgos present → paragraph text rendered AND each rasgo label rendered as a chip; the "Asignado por IA" hint present.
- narrativa present, rasgos empty → paragraph rendered, NO "Rasgos (IA)" row.
- narrativa empty, rasgos present → chips rendered, no paragraph.
- both empty/undefined → component renders nothing (assert the panel title "Lectura del analista (IA)" is NOT in the document).
### Extend `src/modules/clientes/infrastructure/mappers/dtoToFichaCliente.test.ts`
- Using the `buildValidDTO()` fixture, assert `narrativa` and `rasgosIA` map through (a populated case mirroring the existing "maps clv_drivers…" assertion) AND an empty/absent case maps to empty/undefined.
### Guard the extraction
- Run the existing `FichaInteligenciaScores` test (if present) and confirm it still passes after the Panel/Titular extraction (no visual/behavior change).

## Constraints
- Match the existing ficha aesthetic precisely (Tailwind classes in the anchor doc). AI section clearly labeled "(IA)" and the chips visually distinct from deterministic badges.
- Memory: UI strings short (2-4 words), neutral professional Spanish, no explanatory banners/reassurance text.
- Deterministic badges/scores unchanged. Render nothing when data absent (no regression vs current ficha).
- No new heavy dependencies; use what the repo already has.

## Verification
- `npx tsc --noEmit` (or `npm run build`'s tsc step) — clean
- `npx eslint <the files you touched> --ext ts,tsx --max-warnings 0`
- `npx vitest run src/modules/clientes` — all green (new + existing)

## Commit (in the sistema-cobro-web repo, on feat/analytics-narrativa-ia)
`feat(clientes): panel de lectura del analista (IA) y rasgos conductuales en la ficha`. No --no-verify. No Claude attribution footer.
Note: `NARRATIVA_ANCHORS.md` is an untracked scratch doc — do NOT commit it (leave it untracked or delete it; do not stage it).

## Report
Full report to `/Volumes/M2-1TB/Developer/msp-api/docs/superpowers/plans/task-16-report.md` (state whether you extracted Panel/Titular or replicated styles, and confirm FichaInteligenciaScores tests still pass). Reply ≤15 lines: status, commit SHA+subject, one-line test summary (tsc + eslint + vitest), concerns, report path.
