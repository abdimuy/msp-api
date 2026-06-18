"""
clv_harness.py — Offline CLV (Gamma-Gamma monetary + DET) Fitting & Validation Harness
======================================================================================
Sibling of analysis/recompra/recompra_scorecard.py. Fits the MONETARY sub-model
(Gamma-Gamma, Fader/Hardie/Lee 2005) and calibrates a gross-CLV pipeline:

    gross_CLV = MARGIN * E[M] * DET

then parameterizes the risk-adjusted served value (applied in Go at read time):

    CLV_final = MARGIN * E[M] * DET * P(paga) - perdida_esperada

- E[M]  = Gamma-Gamma debiased expected per-transaction monetary value (shrinks the
          client's observed mean ticket toward the population mean — critical because
          most clients have <8 purchases).
- DET   = discounted expected transactions over horizon H, from the SHARED BG/BB
          (btyd_params.json) — NOT refit here. Single shared BG/BB keeps the
          repurchase-propensity model and the CLV model consistent.
- MARGIN ≈ 0.528 (verified 2025 unit economics; see project_verified_unit_economics).
- P(paga)/perdida_esperada applied in Go (credit Score 1). Documented, not validated here.

PIT discipline & the V-only monthly grid are identical to the recompra harness.

Usage:
  .venv/bin/python clv_harness.py            # dry-run (metrics only)
  .venv/bin/python clv_harness.py --write    # also emit clv_params.json + gg_fixtures.json
"""

from __future__ import annotations

import argparse
import datetime
import json
import warnings
from pathlib import Path
from typing import Tuple

import numpy as np
import pandas as pd
from dateutil.relativedelta import relativedelta
from lifetimes import BetaGeoBetaBinomFitter, GammaGammaFitter
from scipy.stats import spearmanr

warnings.filterwarnings("ignore")

# ─── TUNABLES ────────────────────────────────────────────────────────────────

# Verified 2025 margin (project_verified_unit_economics): real margin 52.8%.
MARGIN = 0.528

# Gross-CLV horizon. With H=24m, holdout T <= 2024-06-30 keeps (T, T+H] inside data
# (data ends ~2026-06). H=36 reported as a secondary view (T <= 2023-06-30).
HORIZON_MONTHS = 24
HORIZON_SECONDARY = 36

# Monthly discount d. Annual ~12% → d = (1.12)^(1/12) - 1 ≈ 0.00949. A furniture
# credit retailer's cost of capital / opportunity rate is well above zero; 12%/yr is
# a conservative, interpretable choice and keeps far-out months from dominating CLV.
MONTHLY_DISCOUNT = (1.12) ** (1.0 / 12.0) - 1.0  # ≈ 0.00948879

# Holdout observation dates for gross-CLV validation (so T+H fits inside the data;
# data ends ~2026-06-11, so we use month-ends a touch earlier than 2024/2023-06-30).
T_HOLDOUT_24 = "2024-05-31"
T_HOLDOUT_36 = "2023-05-31"

# Gamma-Gamma holdout split for the E[M] validation (calibration cutoff + future window).
GG_VAL_T = "2024-05-31"
GG_VAL_WINDOW_MONTHS = 24

# Population-band observation date (the "now"-like snapshot used to set tercile cuts).
BAND_OBS_DATE = "2026-06-30"

# Shared BG/BB params live here — DO NOT refit. We set them onto the fitter directly.
DATA_DIR = Path(__file__).parent.parent / "creditscorecard" / ".data"
ANALYTICS_DIR = Path(__file__).parent.parent.parent / "internal" / "analytics" / "app"
BTYD_PARAMS_JSON = ANALYTICS_DIR / "btyd_params.json"
BTYD_FIXTURES_JSON = ANALYTICS_DIR / "btyd_fixtures.json"
CLV_PARAMS_JSON = ANALYTICS_DIR / "clv_params.json"
GG_FIXTURES_JSON = ANALYTICS_DIR / "gg_fixtures.json"


# ─── DATA LOADING ─────────────────────────────────────────────────────────────

def load_ventas() -> pd.DataFrame:
    print("Loading ventas.csv …", end=" ", flush=True)
    ventas = pd.read_csv(DATA_DIR / "ventas.csv", dtype={
        "CLIENTE_ID": int, "IMPORTE_NETO": float, "TIPO_DOCTO": str,
    }, parse_dates=["FECHA"])
    if ventas["FECHA"].dt.tz is not None:
        ventas["FECHA"] = ventas["FECHA"].dt.tz_convert("UTC").dt.tz_localize(None)
    n_total = len(ventas)
    # ── V-ONLY FILTER (critical — same rule as the recompra harness) ──────────
    ventas = ventas[ventas["TIPO_DOCTO"] == "V"].copy()
    print(f"{n_total:,} rows → {len(ventas):,} after TIPO_DOCTO=='V' filter "
          f"({ventas['CLIENTE_ID'].nunique():,} clients)")
    # Monthly bucket: period index (year*12 + month) for the BG/BB grid.
    ventas["month_idx"] = ventas["FECHA"].dt.year * 12 + (ventas["FECHA"].dt.month - 1)
    return ventas


def month_idx_of(ts: pd.Timestamp) -> int:
    return ts.year * 12 + (ts.month - 1)


# ─── SHARED BG/BB (NO REFIT) ──────────────────────────────────────────────────

def load_shared_bgbb() -> Tuple[BetaGeoBetaBinomFitter, dict]:
    """Construct a BetaGeoBetaBinomFitter and SET its params to btyd_params.json.

    We do NOT call .fit(). lifetimes stores params in fitter.params_ as a pandas
    Series indexed [alpha, beta, gamma, delta]; setting it makes all conditional_*
    methods work. We verify against btyd_fixtures.json before using it.
    """
    with open(BTYD_PARAMS_JSON, encoding="utf-8") as f:
        params = json.load(f)["bgbb"]
    fitter = BetaGeoBetaBinomFitter()
    fitter.params_ = pd.Series({
        "alpha": params["alpha"], "beta": params["beta"],
        "gamma": params["gamma"], "delta": params["delta"],
    })
    return fitter, params


def verify_bgbb_against_fixtures(fitter: BetaGeoBetaBinomFitter) -> None:
    """Reproduce btyd_fixtures.json exp_12m / p_alive within 1e-6 — the consistency anchor."""
    with open(BTYD_FIXTURES_JSON, encoding="utf-8") as f:
        fx = json.load(f)
    horizon = fx["horizon_periods"]
    max_err = 0.0
    for case in fx["cases"]:
        x = np.array([case["x"]]); tx = np.array([case["t_x"]]); n = np.array([case["n"]])
        exp = float(np.atleast_1d(
            fitter.conditional_expected_number_of_purchases_up_to_time(horizon, x, tx, n))[0])
        pa = float(np.atleast_1d(fitter.conditional_probability_alive(0, x, tx, n))[0])
        max_err = max(max_err, abs(exp - case["exp_12m"]), abs(pa - case["p_alive"]))
    assert max_err < 1e-6, f"BG/BB params do NOT reproduce btyd_fixtures.json (max_err={max_err:.2e})"
    print(f"  ✓ shared BG/BB reproduces btyd_fixtures.json ({len(fx['cases'])} cases, max_err={max_err:.2e})")


# ─── BG/BB GRID (x, t_x, n) at observation date T ─────────────────────────────

def build_grid(ventas: pd.DataFrame, T: pd.Timestamp) -> pd.DataFrame:
    """Per-client (x, t_x, n) on the monthly grid + monetary, PIT (V purchases <= T).

    Identical monthly-collapse rule as the recompra harness:
      n   = T_month - acquisition_month
      x   = # post-acquisition months (> acq, <= T) with >=1 V purchase
      t_x = last_purchase_month - acquisition_month (0 if x=0)
    monetary = mean IMPORTE_NETO of the client's V purchases <= T.
    """
    T_month = month_idx_of(T)
    v_at_T = ventas[ventas["FECHA"] <= T].copy()

    grp = v_at_T.groupby("CLIENTE_ID")
    mean_importe = grp["IMPORTE_NETO"].mean().rename("monetary")
    total_v = grp.size().rename("total_v_count")

    months_per_client = (
        v_at_T[["CLIENTE_ID", "month_idx"]].drop_duplicates()
        .groupby("CLIENTE_ID")["month_idx"].apply(lambda s: sorted(s.tolist()))
    )

    def summarize(months):
        acq = months[0]
        n = T_month - acq
        post = [m for m in months if m > acq]
        x = len(post)
        t_x = (max(post) - acq) if post else 0
        return n, x, t_x

    summ = pd.DataFrame(
        months_per_client.apply(summarize).tolist(),
        index=months_per_client.index, columns=["n", "x", "t_x"],
    )
    df = pd.DataFrame({"monetary": mean_importe}).join(total_v).join(summ).reset_index()
    df["frequency"] = df["x"].astype(int)          # BG/BB freq = monthly-collapsed repeat count
    df["recency"] = df["t_x"].astype(int)
    df["n_periods"] = df["n"].astype(int)
    return df


# ─── DET (discounted expected transactions) — the Python↔Go contract ──────────

DET_DEFINITION = (
    "DET = sum_{m=1..H} (E[X(m)] - E[X(m-1)]) / (1+d)^m, "
    "E[X(m)] = conditional_expected_number_of_purchases_up_to_time(m, x, t_x, n) "
    "from shared BG/BB (btyd_params.json); E[X(0)]=0; period=month"
)


def compute_det(
    fitter: BetaGeoBetaBinomFitter,
    x: np.ndarray, t_x: np.ndarray, n: np.ndarray,
    horizon: int, d: float,
) -> np.ndarray:
    """Vectorized DET per client over `horizon` months with monthly discount `d`.

    DET = Σ_{m=1..H} (E[X(m)] − E[X(m−1)]) / (1+d)^m   [E[X(0)]=0]
    """
    x = np.asarray(x, dtype=float)
    t_x = np.asarray(t_x, dtype=float)
    n = np.asarray(n, dtype=float)
    prev = np.zeros_like(x, dtype=float)  # E[X(0)] = 0
    det = np.zeros_like(x, dtype=float)
    for m in range(1, horizon + 1):
        cum = np.asarray(
            fitter.conditional_expected_number_of_purchases_up_to_time(m, x, t_x, n),
            dtype=float,
        )
        incr = cum - prev
        det += incr / ((1.0 + d) ** m)
        prev = cum
    return det


# ─── METRICS ──────────────────────────────────────────────────────────────────

def mape(pred: np.ndarray, actual: np.ndarray) -> float:
    """Mean absolute percentage error over rows with actual > 0."""
    mask = actual > 0
    if mask.sum() == 0:
        return float("nan")
    return float(np.mean(np.abs((pred[mask] - actual[mask]) / actual[mask])) * 100.0)


def decile_lift_table(pred_clv: np.ndarray, realized: np.ndarray) -> pd.DataFrame:
    """Deciles by predicted gross-CLV; report realized future revenue captured per decile."""
    d = pd.DataFrame({"pred": pred_clv, "realized": realized})
    d["decile_code"] = pd.qcut(d["pred"].rank(method="first"), 10, labels=False)
    rows = []
    total_real = d["realized"].sum()
    for code in range(int(d["decile_code"].max()) + 1):
        b = d[d["decile_code"] == code]
        rows.append({
            "decile": code + 1,
            "n": len(b),
            "pred_clv_mean": b["pred"].mean(),
            "realized_sum": b["realized"].sum(),
            "realized_mean": b["realized"].mean(),
            "realized_share": (b["realized"].sum() / total_real * 100.0) if total_real > 0 else float("nan"),
        })
    tbl = pd.DataFrame(rows).sort_values("decile", ascending=False).reset_index(drop=True)
    tbl["cum_realized_share"] = tbl["realized_share"].cumsum()
    return tbl


# ─── GAMMA-GAMMA FIT ───────────────────────────────────────────────────────────

def fit_gamma_gamma(frequency: np.ndarray, monetary: np.ndarray) -> Tuple[GammaGammaFitter, float]:
    """Fit GammaGammaFitter, escalating the penalizer until params are finite & positive.

    GG requires frequency > 0 AND monetary > 0 (clients with no repeat signal are dropped).
    """
    import contextlib
    import io
    last_err: Exception | None = None
    for pen in (0.0, 0.001, 0.01, 0.1):
        try:
            gg = GammaGammaFitter(penalizer_coef=pen)
            with contextlib.redirect_stdout(io.StringIO()):
                gg.fit(frequency=frequency, monetary_value=monetary)
            p = dict(gg.params_)
            if all(np.isfinite(v) and v > 0 for v in p.values()):
                return gg, pen
        except Exception as exc:  # noqa: BLE001
            last_err = exc
            continue
    raise RuntimeError(f"Gamma-Gamma failed to converge at all penalizer levels: {last_err!r}")


# ─── MAIN ─────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--write", action="store_true", help="Write clv_params.json + gg_fixtures.json")
    args = parser.parse_args()

    ventas = load_ventas()

    # ── Shared BG/BB (no refit) ───────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("SHARED BG/BB (set params from btyd_params.json — NO refit)")
    fitter, bgbb_params = load_shared_bgbb()
    print(f"  params: {bgbb_params}")
    verify_bgbb_against_fixtures(fitter)

    # ──────────────────────────────────────────────────────────────────────────
    # 1. GAMMA-GAMMA FIT (on the full V book at the band obs date, freq>0 clients)
    # ──────────────────────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("GAMMA-GAMMA FIT (monetary sub-model)")
    band_T = pd.Timestamp(BAND_OBS_DATE)
    grid_all = build_grid(ventas, band_T)
    gg_fit_df = grid_all[(grid_all["frequency"] > 0) & (grid_all["monetary"] > 0)].copy()
    print(f"  Eligible for GG fit (frequency>0 & monetary>0): {len(gg_fit_df):,} clients "
          f"(of {len(grid_all):,} with >=1 V purchase)")

    # GG independence assumption: corr(frequency, monetary) should be LOW.
    corr_fm = float(np.corrcoef(gg_fit_df["frequency"], gg_fit_df["monetary"])[0, 1])
    print(f"  corr(frequency, monetary) = {corr_fm:+.4f}  (GG assumes independence → want low)")

    gg, gg_pen = fit_gamma_gamma(gg_fit_df["frequency"].values, gg_fit_df["monetary"].values)
    gg_params = {k: float(v) for k, v in gg.params_.items()}
    print(f"  penalizer_coef={gg_pen}")
    print(f"  Gamma-Gamma params: p={gg_params['p']:.6f}  q={gg_params['q']:.6f}  v={gg_params['v']:.6f}")
    pop_mean_ticket = float(gg_fit_df["monetary"].mean())
    print(f"  population mean ticket (shrinkage target proxy): ${pop_mean_ticket:,.0f}")

    # E[M] for the GG-eligible population.
    gg_fit_df["e_m"] = gg.conditional_expected_average_profit(
        gg_fit_df["frequency"], gg_fit_df["monetary"]
    )
    # Shrinkage evidence: E[M] less dispersed than the raw observed mean ticket.
    print("\n  SHRINKAGE (E[M] should be LESS dispersed than raw observed mean ticket):")
    print(f"    raw monetary   std=${gg_fit_df['monetary'].std():,.0f}  "
          f"cv={gg_fit_df['monetary'].std()/gg_fit_df['monetary'].mean():.3f}")
    print(f"    E[M] (debiased) std=${gg_fit_df['e_m'].std():,.0f}  "
          f"cv={gg_fit_df['e_m'].std()/gg_fit_df['e_m'].mean():.3f}")

    # ──────────────────────────────────────────────────────────────────────────
    # 1b. GAMMA-GAMMA HOLDOUT VALIDATION (predicted E[M] vs realized future ticket)
    # ──────────────────────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print(f"GAMMA-GAMMA HOLDOUT VALIDATION  (T_cal={GG_VAL_T}, future window={GG_VAL_WINDOW_MONTHS}m)")
    T_cal = pd.Timestamp(GG_VAL_T)
    W_end = T_cal + relativedelta(months=GG_VAL_WINDOW_MONTHS)
    grid_cal = build_grid(ventas, T_cal)
    cal_df = grid_cal[(grid_cal["frequency"] > 0) & (grid_cal["monetary"] > 0)].copy()

    # Fit GG on calibration data ONLY (PIT — no leakage into the validation).
    gg_cal, _ = fit_gamma_gamma(cal_df["frequency"].values, cal_df["monetary"].values)
    cal_df["e_m"] = gg_cal.conditional_expected_average_profit(
        cal_df["frequency"], cal_df["monetary"]
    )

    # Realized mean ticket in the future window.
    v_future = ventas[(ventas["FECHA"] > T_cal) & (ventas["FECHA"] <= W_end)]
    realized_ticket = v_future.groupby("CLIENTE_ID")["IMPORTE_NETO"].mean().rename("realized_ticket")
    val = cal_df.merge(realized_ticket, on="CLIENTE_ID", how="inner")
    print(f"  Clients with calibration freq>0 AND >=1 future V purchase: {len(val):,}")

    sp_em, _ = spearmanr(val["e_m"], val["realized_ticket"])
    sp_raw, _ = spearmanr(val["monetary"], val["realized_ticket"])
    mape_em = mape(val["e_m"].values, val["realized_ticket"].values)
    mape_raw = mape(val["monetary"].values, val["realized_ticket"].values)
    print(f"  Spearman( E[M],          realized ) = {sp_em:.4f}")
    print(f"  Spearman( raw mean ticket, realized) = {sp_raw:.4f}")
    print(f"  MAPE    ( E[M],          realized ) = {mape_em:.1f}%")
    print(f"  MAPE    ( raw mean ticket, realized) = {mape_raw:.1f}%")

    # Shrinkage benefit for LOW-frequency clients (freq in {1,2} — the scarce-data regime).
    low = val[val["frequency"] <= 2]
    if len(low) > 20:
        mape_em_low = mape(low["e_m"].values, low["realized_ticket"].values)
        mape_raw_low = mape(low["monetary"].values, low["realized_ticket"].values)
        print(f"  LOW-FREQ (freq<=2, n={len(low):,}):  MAPE E[M]={mape_em_low:.1f}%  "
              f"vs raw={mape_raw_low:.1f}%  "
              f"({'E[M] better' if mape_em_low < mape_raw_low else 'raw better'})")

    # ──────────────────────────────────────────────────────────────────────────
    # 2. GROSS-CLV VALIDATION (MARGIN × E[M] × DET vs realized future margin)
    # ──────────────────────────────────────────────────────────────────────────
    def gross_clv_validation(horizon: int, T_str: str, label: str) -> dict:
        print("\n" + "=" * 70)
        print(f"GROSS-CLV VALIDATION [{label}]  (T={T_str}, H={horizon}m, "
              f"realized window=(T, T+{horizon}m])")
        T = pd.Timestamp(T_str)
        W = T + relativedelta(months=horizon)
        data_end = ventas["FECHA"].max()
        fits = "YES" if W <= data_end else f"NO (W={W.date()} > data_end={data_end.date()})"
        print(f"  Window fits inside data (data_end={data_end.date()}): {fits}")

        g = build_grid(ventas, T)
        # Eligible: >=1 V purchase <= T. E[M] via GG where freq>0; fallback = own ticket.
        elig = g[g["monetary"] > 0].copy()
        gg_h, _ = fit_gamma_gamma(
            elig.loc[elig["frequency"] > 0, "frequency"].values,
            elig.loc[elig["frequency"] > 0, "monetary"].values,
        )
        elig["e_m"] = elig["monetary"]  # fallback for freq=0 (GG not applicable)
        mask_pos = elig["frequency"] > 0
        elig.loc[mask_pos, "e_m"] = gg_h.conditional_expected_average_profit(
            elig.loc[mask_pos, "frequency"], elig.loc[mask_pos, "monetary"]
        )
        elig["det"] = compute_det(
            fitter, elig["frequency"].values, elig["recency"].values,
            elig["n_periods"].values, horizon, MONTHLY_DISCOUNT,
        )
        elig["gross_clv"] = MARGIN * elig["e_m"] * elig["det"]

        # Realized gross margin = MARGIN × actual V spend in (T, T+H].
        v_win = ventas[(ventas["FECHA"] > T) & (ventas["FECHA"] <= W)]
        realized_spend = v_win.groupby("CLIENTE_ID")["IMPORTE_NETO"].sum().rename("realized_spend")
        elig = elig.merge(realized_spend, on="CLIENTE_ID", how="left")
        elig["realized_spend"] = elig["realized_spend"].fillna(0.0)
        elig["realized_margin"] = MARGIN * elig["realized_spend"]

        sp, _ = spearmanr(elig["gross_clv"], elig["realized_margin"])
        mp = mape(elig["gross_clv"].values, elig["realized_margin"].values)
        total_pred = elig["gross_clv"].sum()
        total_real = elig["realized_margin"].sum()
        bias = (total_pred - total_real) / total_real * 100.0 if total_real > 0 else float("nan")
        print(f"  Eligible clients: {len(elig):,}  "
              f"(freq=0 fallback: {(~mask_pos).sum():,} / {len(elig):,})")
        print(f"  Spearman(gross_CLV, realized_margin) = {sp:.4f}")
        print(f"  MAPE  (rows with realized>0)         = {mp:.1f}%")
        print(f"  Portfolio bias (Σpred-Σreal)/Σreal   = {bias:+.1f}%  "
              f"(Σpred=${total_pred:,.0f}  Σreal=${total_real:,.0f})")

        tbl = decile_lift_table(elig["gross_clv"].values, elig["realized_margin"].values)
        print(f"\n  DECILE LIFT (by predicted gross-CLV; decile 10 = highest predicted):")
        print(f"  {'Dec':>3}  {'N':>6}  {'PredCLV$':>10}  {'RealMrg$':>11}  {'Real%':>7}  {'CumReal%':>8}")
        for _, r in tbl.iterrows():
            print(f"  {int(r['decile']):>3}  {int(r['n']):>6}  {r['pred_clv_mean']:>10,.0f}  "
                  f"{r['realized_sum']:>11,.0f}  {r['realized_share']:>6.1f}%  {r['cum_realized_share']:>7.1f}%")
        return {"spearman": sp, "mape": mp, "bias": bias,
                "top_decile_share": float(tbl.iloc[0]["realized_share"])}

    res24 = gross_clv_validation(HORIZON_MONTHS, T_HOLDOUT_24, "H=24m PRIMARY")
    res36 = gross_clv_validation(HORIZON_SECONDARY, T_HOLDOUT_36, "H=36m secondary")

    print("\n" + "=" * 70)
    print("HORIZON DECISION")
    print(f"  H=24m: Spearman={res24['spearman']:.3f}  MAPE={res24['mape']:.0f}%  "
          f"bias={res24['bias']:+.0f}%  top-decile captures {res24['top_decile_share']:.0f}% of future margin")
    print(f"  H=36m: Spearman={res36['spearman']:.3f}  MAPE={res36['mape']:.0f}%  "
          f"bias={res36['bias']:+.0f}%  top-decile captures {res36['top_decile_share']:.0f}% of future margin")
    print(f"  → CHOSEN H={HORIZON_MONTHS}m (more holdout data; near-term value the winback "
          f"engine acts on; 36m offered as a longer-horizon view).")
    print(f"  → monthly_discount d={MONTHLY_DISCOUNT:.6f} (annual 12%); far-out months discounted, "
          f"interpretable.")

    # ──────────────────────────────────────────────────────────────────────────
    # 3. BAND CUTS (terciles on gross-CLV for the full eligible population at "now")
    # ──────────────────────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print(f"BAND CUTS (terciles on gross-CLV, full eligible population at {BAND_OBS_DATE})")
    pop = grid_all[grid_all["monetary"] > 0].copy()
    pop["e_m"] = pop["monetary"]  # freq=0 fallback: observed single/mean ticket
    mask_pos = pop["frequency"] > 0
    pop.loc[mask_pos, "e_m"] = gg.conditional_expected_average_profit(
        pop.loc[mask_pos, "frequency"], pop.loc[mask_pos, "monetary"]
    )
    pop["det"] = compute_det(
        fitter, pop["frequency"].values, pop["recency"].values,
        pop["n_periods"].values, HORIZON_MONTHS, MONTHLY_DISCOUNT,
    )
    pop["gross_clv"] = MARGIN * pop["e_m"] * pop["det"]

    alto_min = float(np.percentile(pop["gross_clv"], 66))
    medio_min = float(np.percentile(pop["gross_clv"], 33))
    if alto_min <= medio_min:
        alto_min = medio_min + 1.0
    alto_min = max(0.0, alto_min)
    medio_min = max(0.0, medio_min)
    bands_pesos = {"alto_min": round(alto_min, 2), "medio_min": round(medio_min, 2)}
    n_freq0 = int((~mask_pos).sum())
    print(f"  Eligible population: {len(pop):,}  (freq=0 fallback: {n_freq0:,})")
    print(f"  gross_CLV: min=${pop['gross_clv'].min():,.0f}  "
          f"p33=${medio_min:,.0f}  median=${pop['gross_clv'].median():,.0f}  "
          f"p66=${alto_min:,.0f}  max=${pop['gross_clv'].max():,.0f}")
    s = pop["gross_clv"]
    print(f"  BAJO  (clv < {medio_min:,.0f}):              {(s < medio_min).mean():.1%}")
    print(f"  MEDIO ({medio_min:,.0f} <= clv < {alto_min:,.0f}): "
          f"{((s >= medio_min) & (s < alto_min)).mean():.1%}")
    print(f"  ALTO  (clv >= {alto_min:,.0f}):             {(s >= alto_min).mean():.1%}")

    # ──────────────────────────────────────────────────────────────────────────
    # ARTIFACTS
    # ──────────────────────────────────────────────────────────────────────────
    version_date = datetime.date.today().strftime("%Y%m%d")

    clv_params = {
        "version": f"v1-clv-{version_date}",
        "margin": MARGIN,
        "margin_source": "project_verified_unit_economics (real 2025 margin 52.8%)",
        "horizon_months": HORIZON_MONTHS,
        "monthly_discount": round(MONTHLY_DISCOUNT, 8),
        "monthly_discount_note": "annual ~12% cost of capital → d = (1.12)^(1/12) - 1",
        "det_definition": DET_DEFINITION,
        "bgbb_source": "btyd_params.json (shared with recompra; NOT refit here)",
        "gamma_gamma": {
            "p": round(gg_params["p"], 8),
            "q": round(gg_params["q"], 8),
            "v": round(gg_params["v"], 8),
        },
        "gamma_gamma_fit": {
            "penalizer_coef": gg_pen,
            "n_clientes": int(len(gg_fit_df)),
            "obs_date": BAND_OBS_DATE,
            "corr_frequency_monetary": round(corr_fm, 6),
            "population_mean_ticket": round(pop_mean_ticket, 2),
        },
        "monetary_fallback_when_freq0": "observed mean V ticket (Gamma-Gamma not applicable without repeat signal)",
        "bands_pesos": {"alto_min": bands_pesos["alto_min"], "medio_min": bands_pesos["medio_min"]},
        "riesgo": {
            "formula": "CLV_final = margin*E[M]*DET*P_paga - perdida_esperada",
            "p_paga_source": "credit Score 1 (scorecard.json); P_paga=1.0 when the client has no open credit exposure",
            "perdida_esperada": (
                "perdida_esperada = (1 - P_paga) * SALDO_ACTUAL * LGD, with LGD=1.0 (v1). "
                "SALDO_ACTUAL = current open balance (MSP_SALDOS_VENTAS); when SALDO_ACTUAL=0 "
                "(no exposure) perdida_esperada=0 and P_paga=1.0. LGD=1.0 because castigos zero "
                "the balance with ~0 recovery (project_credito_scorecard_pit: castigo leakage). "
                "Go computes this per client at serve time; tune LGD down if recovery data appears."
            ),
        },
    }

    # ── Gamma-Gamma fixtures for the Go GG port ───────────────────────────────
    fixture_cases = [
        (1, 1000.0), (1, 3000.0), (1, 5000.0), (1, 8000.0), (1, 15000.0), (1, 30000.0),
        (2, 2000.0), (2, 5000.0), (2, 10000.0), (2, 25000.0),
        (3, 3000.0), (3, 6000.0), (3, 12000.0),
        (5, 4000.0), (5, 8000.0), (5, 20000.0),
        (8, 5000.0), (8, 12000.0),
        (12, 6000.0), (12, 18000.0),
        (20, 5000.0), (20, 15000.0),
        (1, 100.0), (1, 100000.0), (30, 50000.0),
    ]
    gg_cases = []
    for freq, mon in fixture_cases:
        em = float(np.atleast_1d(
            gg.conditional_expected_average_profit(np.array([freq]), np.array([mon])))[0])
        gg_cases.append({"frequency": freq, "monetary": mon, "e_m": round(em, 8)})
    gg_fixtures = {
        "gamma_gamma": clv_params["gamma_gamma"],
        "method": "conditional_expected_average_profit (lifetimes GammaGammaFitter)",
        "cases": gg_cases,
    }

    # ── Validation before write (mirror the recompra harness) ─────────────────
    print("\n" + "=" * 70)
    print("ARTIFACT VALIDATION")
    cp = json.loads(json.dumps(clv_params, ensure_ascii=False))
    b = cp["bands_pesos"]
    assert b["alto_min"] > b["medio_min"] >= 0, f"bands not monotonic/non-negative: {b}"
    for k in ("p", "q", "v"):
        v_ = cp["gamma_gamma"][k]
        assert np.isfinite(v_) and v_ > 0, f"GG param {k} not finite/positive: {v_}"
    assert 0.0 < cp["margin"] < 1.0, "margin out of (0,1)"
    assert cp["horizon_months"] > 0 and cp["monthly_discount"] >= 0, "bad horizon/discount"
    gf = json.loads(json.dumps(gg_fixtures))
    assert len(gf["cases"]) >= 25, "need >=25 GG fixture cases"
    assert all(np.isfinite(c["e_m"]) and c["e_m"] > 0 for c in gf["cases"]), "GG fixture e_m non-positive"
    print("  ✓ clv_params.json re-loads; bands monotonic (alto>medio>=0); GG params finite & positive")
    print(f"  ✓ margin in (0,1); horizon>0; discount>=0")
    print(f"  ✓ gg_fixtures.json re-loads; {len(gf['cases'])} cases, all e_m finite & positive")

    print("\n" + "=" * 70)
    print("ARTIFACT PREVIEW")
    print("\n--- clv_params.json ---")
    print(json.dumps(clv_params, indent=2, ensure_ascii=False))
    print("\n--- gg_fixtures.json ---")
    print(json.dumps(gg_fixtures, indent=2, ensure_ascii=False))

    if args.write:
        for path, obj in [(CLV_PARAMS_JSON, clv_params), (GG_FIXTURES_JSON, gg_fixtures)]:
            with open(path, "w", encoding="utf-8") as f:
                f.write(json.dumps(obj, indent=2, ensure_ascii=False))
                f.write("\n")
            print(f"\nWrote {path}")
    else:
        print("\nDRY-RUN: artifacts NOT written (pass --write to persist)")

    print("\n" + "=" * 70)
    print("DONE")


if __name__ == "__main__":
    main()
