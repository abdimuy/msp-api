# Credit Scorecard Fitting Harness (R4)

Offline Python tool that trains the logistic-regression credit-risk scorecard
and writes `internal/analytics/app/scorecard.json`, which the Go API embeds at
compile time via `go:embed`.

**Dev-only.** Never deployed — the production Windows server runs only the Go
binary (CLAUDE.md rule #5).

---

## What it does

1. Reads the materialized feature columns from `MSP_AN_WINBACK_CANDIDATOS` (same
   table the Go scorer reads at serve time).
2. Builds the same four-feature vector that `buildCreditoFeatures` in
   `internal/analytics/app/scoring.go` builds:

   | Scorecard key | Source column | Transform |
   |---|---|---|
   | `SALDO_FRAC` | `POR_LIQUIDAR_PCT` | `clamp01(v / 100)` |
   | `COBERTURA_PLAN` | `POR_LIQUIDAR_PCT` | `clamp01(1 − SALDO_FRAC)` |
   | `PCT_PAGOS_A_TIEMPO_6M` | `PCT_PAGOS_A_TIEMPO` | `clamp01(v / 100)` |
   | `DIAS_ATRASO_PROM` | `DIAS_ATRASO_PROM` | raw (not clamped) |

   NULL columns → 0.0 (matches Go reading a zero decimal/int).

3. Derives the label from `MSP_PAGOS_VENTAS`: label=1 if a client has any
   write-off payment (`CONCEPTO_CC_ID IN (27968, 27967)`).

4. Fits `sklearn.linear_model.LogisticRegression` with `class_weight="balanced"`
   (the label rate is ~6%, balancing prevents the model from predicting "good"
   for everything).

5. Reports AUC, Gini, and KS on a 30% random holdout and on an out-of-time
   holdout (clients whose last payment is after `OOT_CUTOFF_DATE = 2024-07-01`).
   Also prints a 10-decile vintage table.

6. Writes `internal/analytics/app/scorecard.json` in the exact schema the Go
   `Scorecard` type parses. Commit the updated file to embed the new model.

---

## Prerequisite — cobranza signals must be materialized

`PCT_PAGOS_A_TIEMPO` and `DIAS_ATRASO_PROM` are computed by the analytics full
refresh job (`leerCobranzaBase` query in
`internal/analytics/infra/analyticsfb/queries.go`). If those columns are NULL
in the dev DB, two of four features are zero for every client, and the resulting
scorecard is degenerate.

The script warns loudly when this is the case:

```
WARNING: feature 'PCT_PAGOS_A_TIEMPO_6M' is NULL for 100.0% of the population.
The cobranza signals (PCT_PAGOS_A_TIEMPO, DIAS_ATRASO_PROM) are NOT yet
materialized in the dev DB.  Run the analytics full refresh first.
```

**Run the analytics full refresh before fitting.** This is the known-pending
"materializar cobranza en dev" item from the Fase 2 backlog.

---

## Setup

```bash
cd /path/to/msp-api       # repo root

python3 -m venv analysis/creditscorecard/.venv
source analysis/creditscorecard/.venv/bin/activate
pip install -r analysis/creditscorecard/requirements.txt
```

---

## Run

Export DB credentials from `.env`, then run from the repo root:

```bash
set -a; . ./.env; set +a

# Full fit + write scorecard.json
python analysis/creditscorecard/fit_scorecard.py --version v1-20250616

# Preview metrics without writing scorecard.json
python analysis/creditscorecard/fit_scorecard.py --version v1-20250616 --dry-run
```

The script reads credentials from the environment:

| Env var | Default |
|---|---|
| `FB_HOST` | `localhost` |
| `FB_PORT` | `3050` |
| `FB_DATABASE` | `/firebird/data/MUEBLERA.FDB` |
| `FB_USER` | `SYSDBA` |
| `FB_PASSWORD` | `masterkey` |

The dev DB is the Dockerized Firebird container on `localhost:3050`.

---

## Recalibration cadence

Recalibrate approximately quarterly (every 3 months) or sooner if population
stability (PSI) drifts.

Steps:

1. Run the analytics full refresh so cobranza signals are current.
2. Run `fit_scorecard.py --dry-run` and inspect KS / Gini / AUC. Target:
   Gini ≥ 0.70 (expected ~0.74 per the validation spike). If Gini drops below
   0.60, investigate data quality before writing a new scorecard.
3. Check the decile table: worst 2 deciles should capture ~70% of losses; top
   4 deciles should show near-zero bad rate.
4. Check PSI if you have the previous score distribution saved: PSI < 0.10 is
   stable, 0.10–0.25 warrants monitoring, > 0.25 indicates significant drift.
5. If metrics are acceptable, run without `--dry-run`, bump `--version`, and
   commit the new `internal/analytics/app/scorecard.json`.

---

## Consistency contract

Features are read from `MSP_AN_WINBACK_CANDIDATOS` — the same materialized
columns the Go scorer reads. Do NOT recompute features from raw Microsip tables
(`DOCTOS_CC`, `DOCTOS_PV`, `MSP_PAGOS_VENTAS`). Recomputing from raw data
would introduce drift between training-time and serve-time feature values,
invalidating the model calibration.
