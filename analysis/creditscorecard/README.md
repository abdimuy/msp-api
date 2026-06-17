# Credit Scorecard Fitting Harness (R4) — point-in-time

Offline Python tool that trains the logistic-regression credit-risk scorecard
and writes `internal/analytics/app/scorecard.json`, which the Go API embeds at
compile time via `go:embed`.

**Dev-only.** Never deployed — the production Windows server runs only the Go
binary (CLAUDE.md rule #5).

---

## Why point-in-time (the leakage trap)

The "bad" label is a **formal castigo** (`CONCEPTO_CC_ID IN (27968 Mal Cliente,
27967 Fugas)`). A castigo **zeroes the client's balance**, and a separate massive
**condonación** (27969, ~70% of accounts) also zeroes balances. So the *current*
saldo cannot be a feature — it is an effect of the very outcome we predict
(circular). A naive snapshot fit produced Gini 0.41 with absurd collinear weights.

The correct design (Siddiqi / Thomas / Basel II) used here:

- **Observation point + performance window.** For each of several staggered
  historical dates `T`, features are reconstructed from data **≤ T only**; the
  label comes from the window **(T, T+WINDOW_MONTHS]**. Never the current state.
- **Roll-to-non-performing label** (not the lagged write-off, which trails the
  real default by a median ~25 months): bad = castigo in the window OR a stretch
  of ≥ `DELINQ_DAYS` with no real payment while still owing.
- **Performing population.** Eligible at `T` = owes (`saldo > SALDO_MIN`), has
  ≥ `MIN_HISTORY_DAYS` of history, paid within `PERFORMING_MAX_DIAS` (not already
  dead), and not yet castigo'd. This is the population the model serves.
- **Out-of-time validation** (train on early panels, validate on the latest) +
  vintage check.

OOT result: Gini ≈ 0.86, KS ≈ 0.73, monotonic deciles (riskiest decile ~98% bad,
safest ~2%).

---

## The 6 behavioral features (must match Go `buildCreditoFeatures`)

All client-level, point-in-time at `T` (serve-time: as-of `now`). Pure behavior —
**no saldo feature** (that was the leakage source).

| Scorecard key | Definition |
|---|---|
| `DIAS_SIN_PAGAR` | days since last real payment; falls back to `ANTIGUEDAD_DIAS` when never paid |
| `PAGOS_90D` | count of real payments in the trailing 90 days |
| `PCT_PAGOS_A_TIEMPO_6M` | fraction (0–1) of payment gaps within cadence+7d (= Go `PctPagosATiempo / 100`) |
| `CADENCIA_DIAS` | **mean** gap between consecutive real payments (matches Go B1 `AVG`) |
| `NUM_PAGOS_TOTAL` | total real payment count |
| `ANTIGUEDAD_DIAS` | days since first cargo (`MIN(FECHA_CARGO)`) |

Real payments = `CONCEPTO_CC_ID IN (87327, 155, 11)`, `CANCELADO='N'`, `APLICADO='S'`.
Everything is client-level: castigo/condonación rows do **not** join cargos by
`DOCTO_CC_ID` (0% match) — only by `CLIENTE_ID`.

---

## Workflow

The Python Firebird driver needs a native `fbclient` (absent on macOS), so the
data is exported to CSV by a Go tool (pure-Go driver), then the harness trains
on the CSVs.

```bash
cd /path/to/msp-api        # repo root
set -a; . ./.env; set +a

# 0. one-time: venv
python3 -m venv analysis/creditscorecard/.venv
analysis/creditscorecard/.venv/bin/pip install -r analysis/creditscorecard/requirements.txt

# 1. Make sure cobranza signals are materialized in dev (writes MSP_AN_*; snapshot first):
make fb-snapshot NAME=pre_recalibracion
go run ./cmd/analytics-refresh --full

# 2. Export raw cargos + abonos to analysis/creditscorecard/.data/ (read-only):
go run ./cmd/analytics-export-creditdata --dir analysis/creditscorecard/.data

# 3. Fit + write internal/analytics/app/scorecard.json:
analysis/creditscorecard/.venv/bin/python analysis/creditscorecard/pit_scorecard.py --write
#    (omit --write for a dry run that only prints metrics)

# 4. (optional) Explore the data:
analysis/creditscorecard/.venv/bin/python analysis/creditscorecard/eda2.py
```

Tunables live as UPPERCASE constants at the top of `pit_scorecard.py`
(`OBSERVATION_DATES`, `WINDOW_MONTHS`, `DELINQ_DAYS`, `MIN_HISTORY_DAYS`,
`PERFORMING_MAX_DIAS`, `SALDO_MIN`, train/OOT split). A sensitivity grid over
`DELINQ_DAYS` × `WINDOW_MONTHS` is printed each run.

**`PERFORMING_MAX_DIAS` must match the Go serving gate** `creditoPerformingMaxDias`
in `internal/analytics/app/scoring.go` (currently 180). If you change one, change
both — the bands are calibrated on the population that gate selects.

---

## Serve-time population & the stale-snapshot caveat

The score applies only to clients with `saldo > 0` who paid within
`creditoPerformingMaxDias` (= the trained "performing" population). Liquidated /
contado clients → "no aplica"; long-dormant owers are surfaced by the cobranza
`TierRiesgo`, not this forward-looking score.

⚠️ A dev DB whose cobranza data is **stale** (owers' last payments frozen months
before wall-clock) will inflate `DIAS_SIN_PAGAR` for every ower and saturate the
score to CRITICO. That is a data-freshness artifact, not a model defect — in
production with live data, performing owers sit near the training mean and the
bands spread normally. Validate against the OOT metrics (historical panels,
unaffected by staleness), not the live `--dist` over a stale dev DB.

`go run ./cmd/analytics-refresh --dist` logs the live band distribution.

---

## Recalibration cadence

Approximately quarterly, or sooner if population stability (PSI) drifts.

1. Run steps 1–3 above.
2. Inspect OOT KS / Gini / AUC. Target Gini ≥ 0.70 (achieved ~0.86). If it drops
   below ~0.60, investigate data quality before writing a new scorecard.
3. Check the decile table: riskiest deciles should concentrate the bads
   monotonically; safest deciles near-zero.
4. Check PSI vs the previous score distribution if saved (<0.10 stable,
   0.10–0.25 monitor, >0.25 drift).
5. If acceptable, run with `--write` and commit the new
   `internal/analytics/app/scorecard.json` (the `version` is date-stamped).

---

## Consistency contract

Features are computed identically in Python (training) and Go (serving). The Go
`buildCreditoFeatures` is the canonical definition; this harness mirrors it. Keep
units aligned: `CADENCIA_DIAS` = mean gap, `PCT_PAGOS_A_TIEMPO_6M` = fraction,
days in days. The `mean`/`std` in `scorecard.json` are the TRAIN-split
standardization stats; Go standardizes `(x − mean)/std` before applying weights.
