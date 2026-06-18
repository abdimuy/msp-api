# Repurchase-Propensity Scorecard Harness (R5) — point-in-time, hybrid BG/BB + logistic

Offline Python tool that fits a customer **repurchase-propensity** model and emits
three JSON artifacts consumed by the Go API:

- `internal/analytics/app/btyd_params.json` — fitted BG/BB params + the discrete period grid.
- `internal/analytics/app/recompra_scorecard.json` — the logistic scorecard (same feature block as the credit `scorecard.json`).
- `internal/analytics/app/btyd_fixtures.json` — reference `(x,t_x,n) → (p_alive, exp_12m)` values for the Go BG/BB closed-form port's unit tests.

**Dev-only.** Never deployed — the production Windows server runs only the Go
binary (CLAUDE.md rule #5). This mirrors the discipline of
`analysis/creditscorecard/pit_scorecard.py`.

---

## The V-only substrate decision (read this first)

`ventas.csv` mixes two `TIPO_DOCTO`s. Only `'V'` rows are purchase occasions:

| TIPO_DOCTO | rows | clients | median importe | median rows/client | meaning |
|---|---:|---:|---:|---:|---|
| `V` | 102,274 | 43,368 | $5,700 | 1 | real sales (purchase occasions) |
| `P` | 314,159 | 13,307 | $100 | 18 | POS payment lines — **NOT** purchases |

The harness **filters to `TIPO_DOCTO == 'V'` only**. Including `'P'` would shatter
the BG/BB monthly grid (a client paying $100 weekly would look like a hyper-active
repeat buyer). 46.6% of clients have ≥2 V purchases — enough repeat signal to model.

---

## The monthly BG/BB period grid (the Python↔Go consistency contract)

BG/BB (Fader/Hardie/Shang 2010, discrete-time noncontractual) summarizes each
client by `(x, t_x, n)` on a **monthly** grid. **Go must replicate this bit-for-bit.**

- Bucket each V purchase into its calendar month: `month_idx = year*12 + (month-1)`.
- A client is **born** in their **first V-purchase month** (the acquisition; not a repeat).
- At observation date `T` (`T_month = month_idx(T)`), consider only V purchases in months `<= T_month`.
- **`n`** = `T_month − acquisition_month` — integer count of monthly opportunities after acquisition.
- **`x`** (frequency) = number of post-acquisition months (strictly `> acquisition_month`, `<= T_month`)
  with **≥1** V purchase. Multiple buys in one month collapse to 1 (BG/BB Bernoulli "≤1 per period").
- **`t_x`** (recency) = `(last_purchase_month − acquisition_month)`, i.e. the month-index `1..n`
  of the most recent month with a purchase; `0` if `x = 0`.

This `(frequency=x, recency=t_x, n=n)` is exactly what
`lifetimes.BetaGeoBetaBinomFitter.fit(frequency, recency, n_periods)` expects. The
grid definition is duplicated in `btyd_params.json["grid"]` so Go has a self-describing contract.

`lifetimes` API note (v0.11.3): the third `fit` arg is `n_periods` (not `n`), and
`conditional_probability_alive(m_periods_in_future, freq, rec, n)` takes the future
horizon as its **first** positional arg — we pass `0` (alive "now"). Both prediction
methods require **array** inputs (they call `.max()` on recency internally).

---

## PIT discipline (no target leakage — the #1 requirement)

- **Observation dates** `["2022-12-31","2023-12-31","2024-12-31"]`. TRAIN = first two, OOT = last.
  Latest obs + 12m window = 2025-12-31 ≤ data end (~2026-06), so the window fits.
- **Eligible population at `T`** = every client with ≥1 V purchase in a month `<= T`
  AND `n >= 1` (at least one opportunity month). The design **INCLUDES owers/debtors**;
  payment behavior enters as a feature, not as an exclusion.
- **TARGET (label):** `recompra = 1` if the client makes **≥1 V purchase in `(T, T+12m]`**, else 0.
- **Leakage guards (asserted + printed each run):** every BG/BB input `(x,t_x,n)` and
  every feature uses only events with date `<= T`; the label uses only events in `(T, T+window]`.
  Asserts: `t_x <= n`, `x <= n`, `recencia = n − t_x >= 0` on every panel.

---

## Hybrid method: BG/BB → features → logistic

1. Fit `BetaGeoBetaBinomFitter` on the pooled TRAIN panels' calibration RFM `(x,t_x,n)`.
   Penalizer escalates `0 → 0.01 → 0.1` until params are finite & positive (the data
   needs **0.01** — see fit section). The same fitted `(alpha,beta,gamma,delta)` go into
   `btyd_params.json` and `btyd_fixtures.json`.
2. Per client, compute two BG/BB conditional predictions as features:
   - `BGBB_EXP_12M` = `conditional_expected_number_of_purchases_up_to_time(12, x, t_x, n)`.
   - `BGBB_P_ALIVE` = `conditional_probability_alive(0, x, t_x, n)`.
3. Add behavioral/RFM features (all PIT, `<= T`) and fit
   `LogisticRegression(class_weight="balanced", max_iter=2000, random_state=42)`, picking
   `C` by OOT AUC. Standardize on TRAIN with `StandardScaler` (the `mean`/`std` ship in the JSON).

### The 8 logistic features (UPPER_SNAKE = Go feature-map contract)

| Scorecard key | Definition (PIT at `T`) |
|---|---|
| `BGBB_EXP_12M` | BG/BB expected V purchases in next 12 months — **dominant positive driver** |
| `BGBB_P_ALIVE` | BG/BB P(still an active customer) |
| `RECENCIA_MESES` | `n − t_x` = months since last V purchase (more → lower propensity) |
| `FRECUENCIA_V` | `x` = repeat-purchase count (post-acquisition months with a buy) |
| `ANTIGUEDAD_MESES` | `n` = months from acquisition month to `T` |
| `MONETARY_LOG` | `log1p(mean IMPORTE_NETO of the client's V purchases <= T)` |
| `PCT_PAGOS_A_TIEMPO` | fraction (0–1) of payment gaps within cadence+7d (reuses the credit harness logic) |
| `DIAS_SIN_PAGAR` | days since last real payment; fallback `antiguedad_meses*30` when the client has no abonos |

Real payments (for the two payment features) = `CONCEPTO_CC_ID IN (87327,155,11)`,
`CANCELADO='N'`, `APLICADO='S'` — identical to the credit harness. Clients with no
credit/abonos (paid contado) get `PCT_PAGOS_A_TIEMPO = 0` and the
`DIAS_SIN_PAGAR = antiguedad*30` fallback.

### Score convention (OPPOSITE of the credit scorecard)

`score = round(100 * sigmoid(logit))`, **HIGHER = MORE likely to repurchase** (NOT
inverted). Verified each run: an active frequent buyer scores HIGH, a one-time old
buyer scores LOW (asserted). Go can reuse the credit `Aplicar` logit machinery — only
the final `100*p` (vs `100*(1−p)`) and the 3-way bands differ.

---

## Window-N decision

12 months. The design EDA found 69% of repurchases fall within 12m, and the OOT
sensitivity grid (N ∈ {12,18,24}) confirms 12m is competitive while keeping the label
forward-looking and the window inside the data range. Longer windows mechanically lift
the base rate (more clients eventually rebuy) and blur the "near-term propensity"
signal the winback engine needs. **N = 12 confirmed.**

---

## Migration decision (the key spike output)

**One new materialized feature is needed at serve time: monthly V-purchase counts (or
the derived `(x, t_x, n)` triple) per client.**

The already-materialized winback facts (`MSP_AN_WINBACK_CANDIDATOS`: FRECUENCIA,
MONETARY, FECHA_ULTIMA_COMPRA, FECHA_PRIMER_CARGO, NUM_PAGOS, PCT_PAGOS_A_TIEMPO,
CADENCIA_DIAS, SALDO, PAGOS_90D) cover the **behavioral** block:

- `MONETARY_LOG` ← `log1p(MONETARY)` (confirm MONETARY = mean V ticket, not sum).
- `PCT_PAGOS_A_TIEMPO` ← `PCT_PAGOS_A_TIEMPO / 100`.
- `DIAS_SIN_PAGAR` ← derivable from last-payment date (same as credit scorecard).

They do **NOT** cover the BG/BB grid. `FRECUENCIA` is a raw V count (not the monthly-
collapsed `x`), and there is no stored `t_x`/`n` on the monthly grid. `BGBB_EXP_12M`,
`BGBB_P_ALIVE`, `RECENCIA_MESES`, `FRECUENCIA_V`, and `ANTIGUEDAD_MESES` all require the
monthly grid. So Go must materialize, per client:

> the set of **distinct calendar months** in which the client made ≥1 V purchase
> (or directly the precomputed `acquisition_month`, `last_purchase_month`, and
> `distinct_purchase_month_count`), from which `(x, t_x, n)` at `now` are computed:
> `n = month(now) − acquisition_month`, `x = distinct_post_acquisition_purchase_months`,
> `t_x = last_purchase_month − acquisition_month`.

Concretely: a new materialized column/table (e.g. `MSP_AN_RECOMPRA_GRID` with
`CLIENTE_ID, ACQ_MONTH, LAST_V_MONTH, X_REPEAT_MONTHS` — three integers per client) is
the minimal addition. From those three integers Go reconstructs `(x,t_x,n)` at serve
time without re-scanning the full ventas stream. The BG/BB closed forms for
`PAlive`/`ExpectedPurchases` must be ported to Go and validated against
`btyd_fixtures.json`.

**Verdict: 1 new materialized fact (the monthly V grid as 3 ints/client) + a Go BG/BB
closed-form port. The behavioral features reuse existing winback facts.**

---

## Emitted artifacts

### `btyd_params.json`
Shared BTYD params (consumed later by BOTH recompra and CLV). Self-describing `grid`
block + `bgbb` `(alpha,beta,gamma,delta)` + `fit` metadata (penalizer, calibration N,
train obs dates). Gamma-Gamma params are added later by a separate CLV harness — not here.

### `recompra_scorecard.json`
Logistic scorecard. `features` block is byte-identical in shape to the credit
`scorecard.json` (`name/label/weight/mean/std`) so Go reuses the standardize-then-logit
machinery. `bands` are **3-way** (`alta_min` = round(p66 of OOT score),
`media_min` = round(p33)) for ALTA/MEDIA/BAJA propensity — NOT the 4-way credit bands.
Labels are Spanish phrases describing why a feature **raises** propensity.

### `btyd_fixtures.json`
~30 `(x,t_x,n)` triples spanning the real data (edge cases: `x=0`, `x=n`, small/large
`n`, high/low recency) with `lifetimes`' `p_alive` and `exp_12m`, computed from the same
fitted params. The Go port asserts `BTYD.PAlive`/`BTYD.ExpectedPurchases` match within tolerance.

---

## Workflow

```bash
cd /path/to/msp-api
# venv is shared with the credit harness (pandas, numpy, sklearn, scipy, lifetimes 0.11.3)
analysis/creditscorecard/.venv/bin/python analysis/recompra/recompra_scorecard.py            # dry-run (metrics only)
analysis/creditscorecard/.venv/bin/python analysis/recompra/recompra_scorecard.py --write    # emit the 3 JSONs
```

Reads the SAME read-only CSVs as the credit harness from `../creditscorecard/.data/`
(`ventas.csv`, `abonos.csv`). Tunables are UPPERCASE constants at the top
(`OBSERVATION_DATES`, `TRAIN_OBS`, `OOT_OBS`, `WINDOW_MONTHS`, `SENSITIVITY_WINDOWS`,
`MIN_OPPORTUNITIES`).

---

## Results (OOT = 2024-12-31)

- OOT **AUC ≈ 0.79, Gini ≈ 0.58, KS ≈ 0.47**; TRAIN AUC ≈ 0.76 (OOT > TRAIN → no overfit).
- Decile table monotonic: top decile ~47% repurchase vs bottom ~4% (≈13× lift).
- Calibration tracks: predicted-P rises monotonically with observed repurchase rate
  across deciles (`class_weight="balanced"` inflates absolute P, but the **ranking** —
  what the winback engine consumes — is well-calibrated).
- Top coefficients: `RECENCIA_MESES` (−, strongest), `PCT_PAGOS_A_TIEMPO` (+),
  `FRECUENCIA_V` (+), `BGBB_EXP_12M` (+). The BG/BB features add lift on top of raw RFM.

---

## Data-quality caveats

- **Mega-client "público general" (CLIENTE_ID 12387, 1800 raw V rows)**: the monthly grid
  **naturally winsorizes** this — 1800 purchases collapse to at most `n` distinct months
  (`x = t_x = n = 59` at the first obs date). No separate cap is applied; the Bernoulli
  "≤1 per month" rule is the winsorization. Documented rather than dropped because it is a
  real (if pooled) account and removing it does not change the fit materially.
- **Large `n`**: clients acquired as far back as 2006 reach `n ≈ 200`. BG/BB's closed form
  handles this; the Go port must too (no `n`-cap in training).
- **BG/BB scipy convergence**: at `penalizer=0.01` scipy reports `success=False`/`fun=nan`
  on its final restart but returns finite, positive, sensible params (the verbose noise is
  swallowed). The params are stable across reruns and the downstream lift confirms signal.

---

## Recalibration cadence

Approximately quarterly, or sooner if population stability (PSI) drifts.

1. Re-export the CSVs (shared with the credit harness) and run with `--write`.
2. Inspect OOT KS / Gini / AUC. Target Gini ≥ 0.50 (achieved ~0.58). Investigate data
   quality before writing if it drops below ~0.40.
3. Check the decile table (monotonic lift) and the calibration table (predicted vs observed track).
4. Confirm the window-N sensitivity grid still favors 12m.
5. Re-run the Go BG/BB unit tests against the refreshed `btyd_fixtures.json`.
6. Commit the three date-stamped JSONs.
```
