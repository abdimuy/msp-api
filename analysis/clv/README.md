# CLV Harness — Gamma-Gamma monetary sub-model + DET gross-CLV calibration (offline)

Offline Python tool that fits the **monetary** sub-model (Gamma-Gamma) and calibrates
a **gross-CLV** pipeline, then parameterizes the **risk-adjusted** served value. It is the
sibling of `analysis/recompra/recompra_scorecard.py` and reuses the **same shared BG/BB**.

It emits two JSON artifacts consumed by the Go API:

- `internal/analytics/app/clv_params.json` — margin, horizon, discount, the DET definition
  (a Python↔Go consistency contract), the fitted Gamma-Gamma `(p,q,v)`, the gross-CLV band
  cuts (terciles, pesos), and the risk-adjustment / expected-loss parameterization for Go.
- `internal/analytics/app/gg_fixtures.json` — reference `(frequency, monetary) → E[M]`
  values for the Go Gamma-Gamma closed-form port's unit tests.

**Dev-only.** Never deployed — the production Windows server runs only the Go binary
(CLAUDE.md rule #5). The CSVs are read-only research data; no DB is touched.

---

## The CLV formula (Fader/Hardie, risk-adjusted)

The final value Go serves per client:

```
CLV_final = MARGIN × E[M] × DET × P(paga) − pérdida_esperada
            └────────────────────────┘
                   gross_CLV
```

- **`E[M]`** — Gamma-Gamma debiased expected per-transaction monetary value. It **shrinks**
  the client's observed mean ticket toward the population mean. Critical here because most
  clients have **<8 purchases** — the raw mean of 1–3 tickets is a noisy estimate, and the
  GG posterior pulls extreme/scarce observations back toward the book average.
- **`DET`** — discounted expected transactions over a horizon `H` months, from the **shared**
  BG/BB (`btyd_params.json`). Definition below.
- **`MARGIN ≈ 0.528`** — verified 2025 unit economics (`project_verified_unit_economics`:
  real margin **52.8%**, not the 54.4% headline). Documented as a param.
- **`P(paga)`** — from credit **Score 1** (`scorecard.json`), applied in **Go at read time**.
  `P(paga)=1.0` when the client has no open credit exposure.
- **`pérdida_esperada`** — expected default loss, applied in **Go at read time** (parameterized
  below). The harness does **not** apply the risk adjustment offline — it only fits and
  validates the **gross** pipeline and emits the params.

---

## Shared BG/BB — reuse, do NOT refit (the consistency rule)

The repurchase-propensity harness already fitted BG/BB (Fader/Hardie/Shang 2010, discrete-time
noncontractual) on the monthly V grid; the params live in `btyd_params.json`
(`alpha, beta, gamma, delta`). **This harness sets those exact params onto a
`BetaGeoBetaBinomFitter` (`fitter.params_ = pd.Series(...)`) and never calls `.fit()`.**

A single shared BG/BB is what keeps the propensity model and the CLV model consistent — both
the "will they come back?" probability and the "how many transactions?" expectation come from
the same generative model. On startup the harness **reproduces every case in
`btyd_fixtures.json`** (`exp_12m`, `p_alive`) within `1e-6` (observed max error ≈ `4.7e-8`),
proving the params were set cleanly.

The **monthly V grid** `(x, t_x, n)` is built identically to the recompra harness (V-only
substrate, monthly collapse, acquisition = first V month). See that README for the grid spec.

---

## DET — discounted expected transactions (the Python↔Go contract)

```
DET = Σ_{m=1..H} ( E[X(m)] − E[X(m−1)] ) / (1+d)^m        E[X(0)] = 0
```

where `E[X(m)] = conditional_expected_number_of_purchases_up_to_time(m, x, t_x, n)` is the
BG/BB **cumulative** expected transactions through month `m`. `( E[X(m)] − E[X(m−1)] )` is the
**incremental** expected transactions occurring in month `m`, discounted by `(1+d)^m`. The
period unit is **month**.

This exact string is stored in `clv_params.json["det_definition"]`. **Go must replicate it
bit-for-bit** — same monthly cumulative BG/BB closed form (validated against `btyd_fixtures.json`),
same incremental differencing, same per-month discounting. Any drift breaks Python↔Go agreement.

---

## Gamma-Gamma fit & the shrinkage rationale

- Per client at an observation date `T` (PIT, V purchases with date ≤ T): `frequency` = the BG/BB
  `x` (monthly-collapsed repeat count); `monetary` = mean `IMPORTE_NETO` of the client's V
  purchases ≤ T. Gamma-Gamma requires `frequency > 0` **and** `monetary > 0`, so clients with no
  repeat signal are **dropped from the GG fit** (they keep the fallback below for scoring).
- Fit `lifetimes.GammaGammaFitter`. **penalizer_coef = 0.0** converged cleanly (params finite &
  positive; the escalation `0 → 0.001 → 0.01 → 0.1` was not needed).
- **Fitted params (lifetimes naming `p, q, v`):** `p = 7.3549`, `q = 14.7458`, `v = 11712.37`
  on **19,408** clients (V book at `2026-06-30`).
- **Independence assumption** (GG assumes `frequency ⟂ monetary`): `corr(frequency, monetary) =
  +0.067` — comfortably low, the assumption holds.
- **Shrinkage evidence:** raw observed mean ticket has CV ≈ **0.400** (std ≈ $2,485); the debiased
  `E[M]` has CV ≈ **0.211** (std ≈ $1,322). `E[M]` is materially **less dispersed** — extreme
  single-ticket observations are pulled toward the ~$6,217 book mean.

### Gamma-Gamma holdout validation (predicted `E[M]` vs realized future mean ticket)

Calibration cutoff `T_cal = 2024-05-31`; future window 24m. GG re-fit on calibration data only
(no leakage), `E[M]` compared to each client's realized mean V ticket in the future window:

| metric | `E[M]` (debiased) | raw mean ticket |
|---|---:|---:|
| Spearman vs realized | 0.242 | 0.245 |
| MAPE vs realized | **41.2%** | 44.4% |
| MAPE, low-freq (`freq ≤ 2`, n≈3,104) | **39.8%** | 43.5% |

Rank correlation is essentially identical (shrinkage preserves ordering), but **`E[M]` tracks the
realized ticket better on absolute error**, and the gap is largest exactly where it matters —
**low-frequency clients** (the majority), where the raw 1–2-ticket mean is noisiest. This is the
expected Gamma-Gamma behavior and the reason to debias rather than use the raw mean.

---

## Gross-CLV validation (the key novel-math check)

`gross_CLV = MARGIN × E[M] × DET` per client at a holdout `T` where `(T, T+H]` fits inside the
data (data ends ~2026-06-11). Realized gross margin = `MARGIN × (actual V spend in (T, T+H])`.
For `freq = 0` clients `E[M]` uses the fallback (own mean ticket; see below).

**Primary, H = 24m, T = 2024-05-31** (35,462 eligible; 20,668 freq=0 fallback):

- **Spearman(gross_CLV, realized_margin) = 0.382**
- **MAPE = 78.7%** (per-client absolute error is high — individual purchase timing is irreducibly
  noisy — but the **ranking and the portfolio total are what CLV is used for**).
- **Portfolio bias = +3.0%** (Σpred $64.4M vs Σreal $62.5M) — the aggregate forecast is nearly
  unbiased.
- **Decile lift** (deciles by predicted gross-CLV; realized future margin captured):

  | decile | N | pred CLV (mean) | realized margin | realized % | cum % |
  |---:|---:|---:|---:|---:|---:|
  | 10 | 3,547 | $8,379 | $26.55M | 42.5% | 42.5% |
  | 9 | 3,546 | $3,650 | $9.81M | 15.7% | 58.2% |
  | 8 | 3,546 | $2,356 | $7.28M | 11.7% | 69.9% |
  | 7 | 3,546 | $1,534 | $5.46M | 8.7% | 78.6% |
  | 6 | 3,546 | $995 | $4.29M | 6.9% | 85.5% |
  | 5 | 3,546 | $576 | $2.88M | 4.6% | 90.1% |
  | 4 | 3,546 | $300 | $1.88M | 3.0% | 93.1% |
  | 3 | 3,546 | $176 | $1.27M | 2.0% | 95.1% |
  | 2 | 3,546 | $119 | $0.98M | 1.6% | 96.7% |
  | 1 | 3,547 | $60 | $2.07M | 3.3% | 100.0% |

  **The top CLV decile captures ~42.5% of all realized future margin**; the top three deciles
  capture ~70%. (Decile 1's small uptick is the freq=0 fallback floor — single-purchase clients
  with no repeat signal, a few of whom buy again; immaterial to targeting.)

**Secondary, H = 36m, T = 2023-05-31:** Spearman 0.384, MAPE 86.9%, bias −7.2%, top decile 39.8%.

### Horizon & discount decisions

- **HORIZON = 24 months.** H=24 and H=36 are near-identical on rank (Spearman 0.382 vs 0.384) and
  decile lift, but **H=24 has lower MAPE (79% vs 87%) and a much smaller portfolio bias (+3% vs
  −7%)**, leaves more holdout data, and targets the **near-term** value the winback engine acts on.
  H=36 is reported as a longer-horizon view, not adopted.
- **monthly_discount `d = (1.12)^(1/12) − 1 ≈ 0.00949`** (annual ~12%). A furniture-credit
  retailer's opportunity cost of capital is well above zero; 12%/yr is conservative and
  interpretable, and it keeps far-out months from dominating the sum (month 24 weight ≈ 0.80).
  It is a param in `clv_params.json` — change it and Go's DET re-weights accordingly.

---

## Band cuts (gross-CLV terciles)

`gross_CLV` is computed for the **full eligible population** (43,368 clients with ≥1 V purchase
at `2026-06-30`; freq=0 clients use the fallback). **Terciles** set the cuts:

- `medio_min = p33 ≈ $200.66`, `alto_min = p66 ≈ $1,225.94`.
- Coverage: **BAJO** 33% (clv < $201), **MEDIO** 33% ($201–$1,226), **ALTO** 34% (clv ≥ $1,226).

Bands are on **gross** CLV (a documented v1 choice). Go applies the per-client risk multiplier
(`P(paga)`, loss) to get `CLV_final`; bands could later be recomputed on **risk-adjusted** CLV if
the risk distribution materially reshuffles clients.

### `freq = 0` fallback

Gamma-Gamma is undefined without a repeat signal (`frequency > 0` required). For single-purchase
clients (`freq = 0`) the harness sets **`E[M] = the client's observed mean V ticket`** (their own
single/mean ticket, no debiasing). DET still applies — BG/BB gives such clients a low but nonzero
expected-transaction count — so their gross_CLV is small but defined. This is recorded in
`clv_params.json["monetary_fallback_when_freq0"]`; **Go must apply the same fallback**.

---

## Risk adjustment & expected loss (applied in Go — parameterized here)

`clv_params.json["riesgo"]` specifies exactly what Go computes at serve time:

```
CLV_final = margin × E[M] × DET × P_paga − perdida_esperada
perdida_esperada = (1 − P_paga) × SALDO_ACTUAL × LGD,   LGD = 1.0 (v1)
```

- **`P_paga`** ← credit **Score 1** (`scorecard.json`). `P_paga = 1.0` when the client has no open
  credit exposure (nothing to default on).
- **`SALDO_ACTUAL`** ← current open balance (`MSP_SALDOS_VENTAS`). When `SALDO_ACTUAL = 0`,
  `perdida_esperada = 0` and `P_paga = 1.0`.
- **`LGD = 1.0`** (loss given default) for v1, because in this book a **castigo zeroes the balance
  with ~0 recovery** (`project_credito_scorecard_pit`: the castigo leakage). Tune LGD **down** if
  recovery data later appears — it is a single, interpretable knob.

The harness does **not** apply or validate this offline (no `P_paga` join); it parameterizes it so
Go's implementation is unambiguous.

---

## Migration note — what Go needs at serve time

CLV needs **NO new materialized fact beyond what the recompra harness already requires.** Concretely:

1. **The monthly V grid** `(x, t_x, n)` per client — already the recompra migration ask
   (`MSP_AN_RECOMPRA_GRID` with `ACQ_MONTH, LAST_V_MONTH, X_REPEAT_MONTHS`, 3 ints/client). DET and
   the GG `frequency` both read from this. **Shared, no addition.**
2. **Mean V ticket / total monetary** per client — **already materialized** in
   `MSP_AN_WINBACK_CANDIDATOS` as `MONETARY` / `FRECUENCIA` (confirm `MONETARY` = **mean** V ticket;
   the recompra README flagged the same confirmation). This is the GG `monetary` input.
3. **Credit Score 1 inputs** — **already materialized** (the 6 behavioral features feeding
   `scorecard.json`); `SALDO_ACTUAL` from `MSP_SALDOS_VENTAS`.

So Go needs to **port two closed forms**: the BG/BB conditional methods (already required for
recompra; validate against `btyd_fixtures.json`) and the **Gamma-Gamma
`conditional_expected_average_profit`** (validate against `gg_fixtures.json`). At serve time Go
computes, per client: `E[M]` (GG, or fallback when `freq=0`), `DET` (the discounted-incremental BG/BB
sum over H=24 at `d`), multiplies by `MARGIN`, then risk-adjusts with `P_paga` and `perdida_esperada`.

**Verdict: 0 new materialized facts. 1 new Go closed-form port (Gamma-Gamma) + the BG/BB port that
recompra already needs.**

---

## Emitted artifacts

### `clv_params.json`
Self-describing: `margin` (+ source), `horizon_months`, `monthly_discount` (+ note),
`det_definition` (the Python↔Go contract string), `gamma_gamma` `(p,q,v)` + fit metadata,
`monetary_fallback_when_freq0`, `bands_pesos` (`alto_min`, `medio_min`), and the `riesgo` block
(formula, `p_paga_source`, `perdida_esperada` parameterization).

### `gg_fixtures.json`
25 `(frequency, monetary)` cases spanning low/high freq and ticket (incl. edge cases: `freq=1` with
$100 / $100k tickets) with `lifetimes`' `conditional_expected_average_profit` `E[M]`. The Go GG port
asserts its `E[M]` matches within tolerance.

---

## Workflow

```bash
cd /path/to/msp-api
# venv is shared with the credit & recompra harnesses (pandas, numpy, sklearn, scipy, lifetimes 0.11.3)
analysis/creditscorecard/.venv/bin/python analysis/clv/clv_harness.py            # dry-run (metrics only)
analysis/creditscorecard/.venv/bin/python analysis/clv/clv_harness.py --write    # emit the 2 JSONs
```

Reads the SAME read-only `ventas.csv` as the recompra harness from `../creditscorecard/.data/`
(filtered to `TIPO_DOCTO == 'V'`). Tunables are UPPERCASE constants at the top (`MARGIN`,
`HORIZON_MONTHS`, `MONTHLY_DISCOUNT`, the holdout/band observation dates).

---

## Results summary

- **Gamma-Gamma:** `p=7.355, q=14.746, v=11712.37`; `corr(freq,monetary)=+0.067` (independence OK);
  shrinkage CV 0.400 → 0.211; holdout `E[M]` MAPE 41.2% vs raw 44.4% (and 39.8% vs 43.5% for
  low-freq clients).
- **Gross-CLV (H=24m):** Spearman 0.382, top decile captures **42.5%** of realized future margin,
  portfolio bias **+3.0%**. The BG/BB × GG × margin pipeline ranks and totals well; per-client MAPE
  is high by nature (purchase timing is noisy) — CLV is a ranking/portfolio instrument.
- **Bands:** terciles at $200.66 / $1,225.94 (BAJO/MEDIO/ALTO ≈ 33/33/34%).

---

## Recalibration cadence

Approximately quarterly, in lockstep with the recompra/credit harnesses (they share the substrate).

1. Re-export `ventas.csv`; **re-run recompra first** so `btyd_params.json` / `btyd_fixtures.json`
   are fresh, then run this harness with `--write` (it reuses those params, never refits BG/BB).
2. Inspect the GG independence corr (want < ~0.2), the shrinkage CVs, and the holdout `E[M]` MAPE.
3. Check the gross-CLV decile lift (top decile should capture ≳40% of future margin) and the
   portfolio bias (target |bias| < ~10%).
4. Re-run the Go Gamma-Gamma unit tests against the refreshed `gg_fixtures.json`, and the BG/BB
   tests against `btyd_fixtures.json`.
5. Commit the two date-stamped JSONs.
```
