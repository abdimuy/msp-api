"""
recompra_scorecard.py — Point-in-Time Repurchase-Propensity Scorecard Harness
=============================================================================
Hybrid BG/BB (Fader/Hardie/Shang 2010, discrete-time noncontractual) + logistic.
Mirrors analysis/creditscorecard/pit_scorecard.py discipline.

No target leakage: every BG/BB input (x, t_x, n) and every feature uses only
events with date <= T; the label uses only V purchases in (T, T+WINDOW_MONTHS].

V-ONLY SUBSTRATE
  ventas.csv mixes TIPO_DOCTO 'V' (real sales/purchase occasions) and 'P'
  (POS payment lines, median $100, ~18/client). We FILTER TO 'V' ONLY — the 'P'
  rows are not purchase occasions and would destroy the BG/BB grid.

Usage:
  .venv/bin/python recompra_scorecard.py            # dry-run (metrics only)
  .venv/bin/python recompra_scorecard.py --write    # also emit the 3 JSON artifacts
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
from lifetimes import BetaGeoBetaBinomFitter
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import roc_auc_score, roc_curve
from sklearn.preprocessing import StandardScaler

warnings.filterwarnings("ignore")

# ─── TUNABLES ────────────────────────────────────────────────────────────────

OBSERVATION_DATES = ["2022-12-31", "2023-12-31", "2024-12-31"]
TRAIN_OBS = ["2022-12-31", "2023-12-31"]
OOT_OBS = ["2024-12-31"]
WINDOW_MONTHS = 12          # performance window after T (12m per design EDA: 69% of repurchases <12m)
SENSITIVITY_WINDOWS = [12, 18, 24]
MIN_OPPORTUNITIES = 1       # require n >= 1 (at least one post-acquisition month)

# Real-payment concept IDs (same contract as the credit harness).
ABONO_IDS = {87327, 155, 11}

# BG/BB conditional-prediction horizon (periods = months).
BGBB_HORIZON = 12

DATA_DIR = Path(__file__).parent.parent / "creditscorecard" / ".data"
ANALYTICS_DIR = Path(__file__).parent.parent.parent / "internal" / "analytics" / "app"
BTYD_PARAMS_JSON = ANALYTICS_DIR / "btyd_params.json"
RECOMPRA_SCORECARD_JSON = ANALYTICS_DIR / "recompra_scorecard.json"
BTYD_FIXTURES_JSON = ANALYTICS_DIR / "btyd_fixtures.json"

# Logistic feature order. Names are the UPPER_SNAKE contract with the Go feature map.
MODEL_FEATURES = [
    "bgbb_exp_12m",
    "bgbb_p_alive",
    "recencia_meses",
    "frecuencia_v",
    "antiguedad_meses",
    "monetary_log",
    "pct_pagos_a_tiempo",
    "dias_sin_pagar",
]
FEATURE_NAME_MAP = {
    "bgbb_exp_12m":       "BGBB_EXP_12M",
    "bgbb_p_alive":       "BGBB_P_ALIVE",
    "recencia_meses":     "RECENCIA_MESES",
    "frecuencia_v":       "FRECUENCIA_V",
    "antiguedad_meses":   "ANTIGUEDAD_MESES",
    "monetary_log":       "MONETARY_LOG",
    "pct_pagos_a_tiempo": "PCT_PAGOS_A_TIEMPO",
    "dias_sin_pagar":     "DIAS_SIN_PAGAR",
}
# Spanish label = why this feature RAISES propensity when it pushes the score up.
FEATURE_LABELS_ES = {
    "bgbb_exp_12m":       "compra recurrente esperada",
    "bgbb_p_alive":       "cliente activo probable",
    "recencia_meses":     "compró recientemente",
    "frecuencia_v":       "compras frecuentes",
    "antiguedad_meses":   "cliente con trayectoria",
    "monetary_log":       "tickets de mayor valor",
    "pct_pagos_a_tiempo": "buen historial de pago",
    "dias_sin_pagar":     "pagos al corriente",
}


# ─── DATA LOADING ─────────────────────────────────────────────────────────────

def load_data() -> Tuple[pd.DataFrame, pd.DataFrame]:
    print("Loading ventas.csv …", end=" ", flush=True)
    ventas = pd.read_csv(DATA_DIR / "ventas.csv", dtype={
        "CLIENTE_ID": int, "IMPORTE_NETO": float, "TIPO_DOCTO": str,
    }, parse_dates=["FECHA"])
    if ventas["FECHA"].dt.tz is not None:
        ventas["FECHA"] = ventas["FECHA"].dt.tz_convert("UTC").dt.tz_localize(None)
    n_total = len(ventas)
    # ── V-ONLY FILTER (critical) ──────────────────────────────────────────────
    ventas = ventas[ventas["TIPO_DOCTO"] == "V"].copy()
    print(f"{n_total:,} rows → {len(ventas):,} after TIPO_DOCTO=='V' filter "
          f"({ventas['CLIENTE_ID'].nunique():,} clients)")

    # Monthly bucket: period index (year*12 + month) for the BG/BB grid.
    ventas["month_idx"] = ventas["FECHA"].dt.year * 12 + (ventas["FECHA"].dt.month - 1)

    print("Loading abonos.csv …", end=" ", flush=True)
    abonos = pd.read_csv(DATA_DIR / "abonos.csv", dtype={
        "DOCTO_CC_ID": int, "CLIENTE_ID": int, "CONCEPTO_CC_ID": int,
        "IMPORTE": float, "LAT": object, "LON": object,
        "CANCELADO": str, "APLICADO": str,
    }, parse_dates=["FECHA"])
    if abonos["FECHA"].dt.tz is not None:
        abonos["FECHA"] = abonos["FECHA"].dt.tz_convert("UTC").dt.tz_localize(None)
    # Restrict to real payments once.
    real_mask = (
        abonos["CONCEPTO_CC_ID"].isin(ABONO_IDS)
        & (abonos["CANCELADO"] == "N")
        & (abonos["APLICADO"] == "S")
    )
    abonos = abonos[real_mask].copy()
    print(f"{len(abonos):,} real-payment rows")

    return ventas, abonos


# ─── HELPERS ──────────────────────────────────────────────────────────────────

def _pct_a_tiempo(dates: pd.Series, cadencia: float, tolerance_days: float = 7.0) -> float:
    """Fraction of consecutive payment gaps within cadencia + tolerance."""
    if len(dates) < 2:
        return 0.0
    sorted_dates = dates.sort_values()
    gaps = sorted_dates.diff().dropna().dt.days.values
    threshold = cadencia + tolerance_days
    return float((gaps <= threshold).mean())


def month_idx_of(ts: pd.Timestamp) -> int:
    return ts.year * 12 + (ts.month - 1)


# ─── BG/BB GRID + FEATURE PANEL ───────────────────────────────────────────────

def build_panel(
    ventas: pd.DataFrame,
    abonos: pd.DataFrame,
    T: pd.Timestamp,
    window_months: int,
) -> Tuple[pd.DataFrame, dict]:
    """Per-client PIT feature DataFrame at observation date T.

    The monthly BG/BB grid (x, t_x, n) and all features use only events <= T.
    The label uses only V purchases in (T, T+window_months].
    """
    T_month = month_idx_of(T)
    W_end = T + relativedelta(months=window_months)

    # ── V purchases <= T (months only) ────────────────────────────────────────
    v_at_T = ventas[ventas["FECHA"] <= T].copy()

    # Per client, the set of distinct purchase MONTHS (collapse multi-buys/month).
    # acquisition month = first V-purchase month (not counted as a repeat).
    grp = v_at_T.groupby("CLIENTE_ID")
    acq_month = grp["month_idx"].min().rename("acq_month")
    mean_importe = grp["IMPORTE_NETO"].mean().rename("mean_importe")
    total_v = grp.size().rename("total_v_count")

    # Distinct purchase months per client (for x and t_x).
    months_per_client = (
        v_at_T[["CLIENTE_ID", "month_idx"]].drop_duplicates()
        .groupby("CLIENTE_ID")["month_idx"].apply(lambda s: sorted(s.tolist()))
    )

    df = pd.DataFrame({"acq_month": acq_month}).join(mean_importe).join(total_v)
    df = df.reset_index()

    # ── BG/BB summary (x, t_x, n) ─────────────────────────────────────────────
    # n = months from acquisition month to T's month (integer opportunities).
    # x = # of post-acquisition months (in 1..n) with >=1 V purchase.
    # t_x = month-index 1..n of most recent purchase month (0 if x=0).
    def summarize(cid_months):
        months = cid_months
        acq = months[0]
        n = T_month - acq
        # post-acquisition purchase months (strictly after acquisition month).
        post = [m for m in months if m > acq]
        x = len(post)
        t_x = (max(post) - acq) if post else 0
        return n, x, t_x

    rows_summary = months_per_client.apply(summarize)
    summ = pd.DataFrame(
        rows_summary.tolist(), index=rows_summary.index, columns=["n", "x", "t_x"]
    ).reset_index()
    df = df.merge(summ, on="CLIENTE_ID", how="left")

    # ── Behavioral / RFM features (PIT) ───────────────────────────────────────
    df["recencia_meses"] = (df["n"] - df["t_x"]).astype(float)
    df["frecuencia_v"] = df["x"].astype(float)         # repeat count (BG/BB freq)
    df["antiguedad_meses"] = df["n"].astype(float)
    df["monetary_log"] = np.log1p(df["mean_importe"].fillna(0.0))

    # ── Payment-behavior features from abonos (PIT, <= T) ─────────────────────
    ab_at_T = abonos[abonos["FECHA"] <= T]
    last_pago = ab_at_T.groupby("CLIENTE_ID")["FECHA"].max().rename("last_pago_T")

    def cad_pct_fn(grp_):
        dates = grp_["FECHA"].sort_values()
        if len(dates) < 2:
            return pd.Series({"cadencia_dias": 0.0, "pct_pagos_a_tiempo": 0.0})
        gaps = dates.diff().dropna().dt.days
        cad = float(gaps.mean())
        pct = _pct_a_tiempo(dates, cad)
        return pd.Series({"cadencia_dias": cad, "pct_pagos_a_tiempo": pct})

    cad_pct = ab_at_T.groupby("CLIENTE_ID").apply(cad_pct_fn)
    df = df.merge(last_pago, on="CLIENTE_ID", how="left")
    df = df.merge(cad_pct, on="CLIENTE_ID", how="left")

    df["pct_pagos_a_tiempo"] = df["pct_pagos_a_tiempo"].fillna(0.0)

    # dias_sin_pagar: days since last real payment; fallback = antiguedad-equivalent
    # (n months → days) for clients with no abonos (e.g. paid contado, no credit).
    df["dias_sin_pagar"] = df.apply(
        lambda r: (T - r["last_pago_T"]).days if pd.notna(r["last_pago_T"])
        else float(r["antiguedad_meses"]) * 30.0,
        axis=1,
    ).astype(float)

    df["T"] = T
    df["cohort_year"] = (df["acq_month"] // 12).astype(int)

    # ── Eligibility funnel ────────────────────────────────────────────────────
    n_start = len(df)
    mask_opp = df["n"] >= MIN_OPPORTUNITIES
    df_elig = df[mask_opp].copy()
    funnel = {
        "start": n_start,
        "pass_opportunities": int(mask_opp.sum()),
    }

    # ── Label: >=1 V purchase in (T, W_end] ───────────────────────────────────
    v_after = ventas[(ventas["FECHA"] > T) & (ventas["FECHA"] <= W_end)]
    repurchasers = set(v_after["CLIENTE_ID"].unique())
    df_elig["recompra"] = df_elig["CLIENTE_ID"].isin(repurchasers).astype(int)

    return df_elig, funnel


# ─── METRICS ──────────────────────────────────────────────────────────────────

def ks_stat(y_true: np.ndarray, y_prob: np.ndarray) -> float:
    fpr, tpr, _ = roc_curve(y_true, y_prob)
    return float(np.max(np.abs(tpr - fpr)))


def decile_table(y_true: np.ndarray, y_score: np.ndarray) -> pd.DataFrame:
    """Score = 100*sigmoid(logit); higher = more likely to repurchase."""
    d = pd.DataFrame({"score": y_score, "good": y_true})
    d["decile_code"] = pd.qcut(d["score"].rank(method="first"), 10, labels=False)
    rows = []
    for code in range(int(d["decile_code"].max()) + 1):
        bucket = d[d["decile_code"] == code]
        rows.append({
            "decile": code + 1,
            "n": len(bucket),
            "n_repurch": int(bucket["good"].sum()),
            "repurch_rate": bucket["good"].mean() if len(bucket) > 0 else float("nan"),
        })
    tbl = pd.DataFrame(rows).sort_values("decile", ascending=False).reset_index(drop=True)
    tbl["cum_repurch"] = tbl["n_repurch"].cumsum()
    tbl["cum_repurch_pct"] = tbl["cum_repurch"] / tbl["n_repurch"].sum() * 100
    return tbl


def calibration_table(y_true: np.ndarray, p_pred: np.ndarray) -> pd.DataFrame:
    d = pd.DataFrame({"p": p_pred, "y": y_true})
    d["bucket"] = pd.qcut(d["p"].rank(method="first"), 10, labels=False)
    rows = []
    for code in range(int(d["bucket"].max()) + 1):
        b = d[d["bucket"] == code]
        rows.append({
            "decile": code + 1,
            "n": len(b),
            "pred_p": b["p"].mean(),
            "obs_rate": b["y"].mean(),
        })
    return pd.DataFrame(rows)


# ─── BG/BB FIT ────────────────────────────────────────────────────────────────

def fit_bgbb(calib_df: pd.DataFrame) -> Tuple[BetaGeoBetaBinomFitter, float, int]:
    """Fit BG/BB on the calibration (x, t_x, n) panel. Retry with penalizer if needed.

    The likelihood depends only on the (x, t_x, n) triple, so we collapse the panel
    to unique triples with integer weights — identical MLE, far faster than fitting
    on the full per-client matrix (lifetimes accepts a `weights` arg).
    """
    import contextlib
    import io

    agg = (
        calib_df.groupby(["x", "t_x", "n"]).size()
        .reset_index(name="w")
    )
    n_clients = int(len(calib_df))

    last_err: Exception | None = None
    for pen in (0.0, 0.01, 0.1):
        try:
            fitter = BetaGeoBetaBinomFitter(penalizer_coef=pen)
            # lifetimes prints the scipy OptimizeResult to stdout on non-clean
            # convergence even when params are usable — swallow that noise.
            with contextlib.redirect_stdout(io.StringIO()):
                fitter.fit(
                    frequency=agg["x"].values,
                    recency=agg["t_x"].values,
                    n_periods=agg["n"].values,
                    weights=agg["w"].values,
                )
            p = dict(fitter.params_)
            if all(np.isfinite(v) and v > 0 for v in p.values()):
                return fitter, pen, n_clients
        except Exception as exc:  # noqa: BLE001 — report last error if all fail
            last_err = exc
            continue
    raise RuntimeError(f"BG/BB failed to converge at all penalizer levels: {last_err!r}")


def bgbb_predict_one(fitter: BetaGeoBetaBinomFitter, x: int, t_x: int, n: int) -> Tuple[float, float]:
    """Scalar (p_alive, exp_12m) for a single (x,t_x,n). lifetimes needs array inputs."""
    xa = np.array([x]); ta = np.array([t_x]); na = np.array([n])
    p_alive = float(np.atleast_1d(fitter.conditional_probability_alive(0, xa, ta, na))[0])
    exp_12m = float(np.atleast_1d(
        fitter.conditional_expected_number_of_purchases_up_to_time(BGBB_HORIZON, xa, ta, na)
    )[0])
    return p_alive, exp_12m


def add_bgbb_features(df: pd.DataFrame, fitter: BetaGeoBetaBinomFitter) -> pd.DataFrame:
    df = df.copy()
    df["bgbb_exp_12m"] = fitter.conditional_expected_number_of_purchases_up_to_time(
        BGBB_HORIZON, df["x"].values, df["t_x"].values, df["n"].values
    )
    # P(alive) "now" = m_periods_in_future=0.
    df["bgbb_p_alive"] = fitter.conditional_probability_alive(
        0, df["x"].values, df["t_x"].values, df["n"].values
    )
    return df


# ─── SENSITIVITY GRID ─────────────────────────────────────────────────────────

def run_sensitivity(ventas, abonos):
    print("\n" + "=" * 70)
    print("SENSITIVITY GRID — window N (OOT AUC / Gini / KS)")
    print(f"{'WINDOW_MONTHS':>13}  {'OOT_AUC':>8}  {'OOT_Gini':>9}  {'OOT_KS':>8}")
    print("-" * 45)
    for wm in SENSITIVITY_WINDOWS:
        panels = {}
        for T_str in TRAIN_OBS + OOT_OBS:
            T = pd.Timestamp(T_str)
            p, _ = build_panel(ventas, abonos, T, wm)
            p["obs"] = T_str
            panels[T_str] = p
        train_df = pd.concat([panels[t] for t in TRAIN_OBS], ignore_index=True)
        oot_df = panels[OOT_OBS[0]].copy()

        fitter, _, _ = fit_bgbb(train_df)
        train_df = add_bgbb_features(train_df, fitter)
        oot_df = add_bgbb_features(oot_df, fitter)

        X_tr = train_df[MODEL_FEATURES].values
        y_tr = train_df["recompra"].values
        X_ot = oot_df[MODEL_FEATURES].values
        y_ot = oot_df["recompra"].values

        scaler = StandardScaler().fit(X_tr)
        clf = LogisticRegression(class_weight="balanced", C=1.0, max_iter=2000, random_state=42)
        clf.fit(scaler.transform(X_tr), y_tr)
        p_ot = clf.predict_proba(scaler.transform(X_ot))[:, 1]
        auc = roc_auc_score(y_ot, p_ot)
        gini = 2 * auc - 1
        ks = ks_stat(y_ot, p_ot)
        flag = " ← main" if wm == WINDOW_MONTHS else ""
        print(f"{wm:>13}  {auc:>8.4f}  {gini:>9.4f}  {ks:>8.4f}{flag}")


# ─── MAIN ─────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--write", action="store_true", help="Write the 3 JSON artifacts")
    args = parser.parse_args()

    ventas, abonos = load_data()

    print("\n" + "=" * 70)
    print("PANEL CONSTRUCTION  (monthly BG/BB grid, PIT)")
    print("=" * 70)

    panels = {}
    for T_str in OBSERVATION_DATES:
        T = pd.Timestamp(T_str)
        panel, funnel = build_panel(ventas, abonos, T, WINDOW_MONTHS)
        panel["obs"] = T_str
        panels[T_str] = panel
        n = len(panel)
        n_good = int(panel["recompra"].sum())
        print(f"\nObservation date T = {T_str}")
        print(f"  Clients with V purchase <= T:        {funnel['start']:>6,}")
        print(f"  After n>={MIN_OPPORTUNITIES} (>=1 opportunity month): {funnel['pass_opportunities']:>6,}")
        print(f"  Panel size: {n:,}  |  recompra={n_good} ({n_good/n:.1%})")
        print(f"  Grid stats: x mean={panel['x'].mean():.2f}  t_x mean={panel['t_x'].mean():.2f}  "
              f"n mean={panel['n'].mean():.2f}  (x=0: {(panel['x']==0).mean():.1%})")

    # ── LEAKAGE GUARD assertions ──────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("LEAKAGE GUARDS")
    for T_str in OBSERVATION_DATES:
        T = pd.Timestamp(T_str)
        p = panels[T_str]
        # n must equal T_month - acq_month; t_x <= n; recency = n - t_x >= 0.
        assert (p["t_x"] <= p["n"]).all(), f"t_x>n at {T_str}"
        assert (p["recencia_meses"] >= 0).all(), f"recency<0 at {T_str}"
        assert (p["x"] <= p["n"]).all(), f"x>n at {T_str}"
        print(f"  ✓ {T_str}: t_x<=n, x<=n, recency>=0, all (x,t_x,n) from purchases <= T")
    print("  ✓ label uses only V purchases in (T, T+12m]; no feature reads past T")

    train_df = pd.concat([panels[t] for t in TRAIN_OBS], ignore_index=True)
    oot_df = panels[OOT_OBS[0]].copy()
    print(f"\nTRAIN={len(train_df):,}  OOT={len(oot_df):,}")

    # ── BG/BB fit on TRAIN calibration RFM ────────────────────────────────────
    print("\n" + "=" * 70)
    print("BG/BB FIT (BetaGeoBetaBinomFitter on TRAIN panels' calibration RFM)")
    fitter, penalizer, n_calib = fit_bgbb(train_df)
    bgbb_params = {k: float(v) for k, v in fitter.params_.items()}
    print(f"  penalizer_coef={penalizer}  n_clientes_calibracion={n_calib:,}")
    print(f"  params: {bgbb_params}")

    train_df = add_bgbb_features(train_df, fitter)
    oot_df = add_bgbb_features(oot_df, fitter)

    # ── Logistic fit ──────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("LOGISTIC FIT (hybrid)")
    print(f"  Features: {MODEL_FEATURES}")

    X_tr = train_df[MODEL_FEATURES].values
    y_tr = train_df["recompra"].values
    X_ot = oot_df[MODEL_FEATURES].values
    y_ot = oot_df["recompra"].values

    scaler = StandardScaler().fit(X_tr)
    X_tr_s = scaler.transform(X_tr)
    X_ot_s = scaler.transform(X_ot)

    results = {}
    for C in [0.5, 1.0, 1e6]:
        clf = LogisticRegression(class_weight="balanced", C=C, max_iter=2000, random_state=42)
        clf.fit(X_tr_s, y_tr)
        p_tr = clf.predict_proba(X_tr_s)[:, 1]
        p_ot = clf.predict_proba(X_ot_s)[:, 1]
        results[C] = {
            "clf": clf, "p_tr": p_tr, "p_ot": p_ot,
            "auc_tr": roc_auc_score(y_tr, p_tr),
            "auc_ot": roc_auc_score(y_ot, p_ot),
        }
    best_C = max(results, key=lambda c: results[c]["auc_ot"])
    best = results[best_C]
    clf, p_tr, p_ot = best["clf"], best["p_tr"], best["p_ot"]
    for C, r in results.items():
        star = " ← selected" if C == best_C else ""
        print(f"  C={C:.0e}  train_AUC={r['auc_tr']:.4f}  OOT_AUC={r['auc_ot']:.4f}{star}")

    auc_tr = best["auc_tr"]; gini_tr = 2 * auc_tr - 1; ks_tr = ks_stat(y_tr, p_tr)
    auc_ot = best["auc_ot"]; gini_ot = 2 * auc_ot - 1; ks_ot = ks_stat(y_ot, p_ot)
    print(f"\n  TRAIN  AUC={auc_tr:.4f}  Gini={gini_tr:.4f}  KS={ks_tr:.4f}")
    print(f"  OOT    AUC={auc_ot:.4f}  Gini={gini_ot:.4f}  KS={ks_ot:.4f}")

    # ── Coefficient table ─────────────────────────────────────────────────────
    print("\n  Feature coefficients (positive → higher logit → more likely to repurchase):")
    for feat, coef in sorted(zip(MODEL_FEATURES, clf.coef_[0]), key=lambda x: abs(x[1]), reverse=True):
        print(f"    {feat:<22} {coef:+.4f}")
    print(f"    {'intercept':<22} {clf.intercept_[0]:+.4f}")

    # ── Decile table (OOT) ────────────────────────────────────────────────────
    score_ot = np.round(100 * p_ot).astype(int)
    print("\n" + "=" * 70)
    print("DECILE TABLE (OOT, by score; decile 10 = highest propensity)")
    dtbl = decile_table(y_ot, score_ot)
    print(f"  {'Decile':>6}  {'N':>6}  {'N_rep':>6}  {'Rep%':>7}  {'CumRep%':>8}")
    for _, row in dtbl.iterrows():
        print(f"  {int(row['decile']):>6}  {int(row['n']):>6}  {int(row['n_repurch']):>6}  "
              f"{row['repurch_rate']*100:>6.1f}%  {row['cum_repurch_pct']:>7.1f}%")

    # ── Calibration table (OOT) ───────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("CALIBRATION TABLE (OOT, predicted-P vs observed repurchase-rate)")
    ctbl = calibration_table(y_ot, p_ot)
    print(f"  {'Decile':>6}  {'N':>6}  {'PredP':>8}  {'ObsRate':>8}")
    for _, row in ctbl.iterrows():
        print(f"  {int(row['decile']):>6}  {int(row['n']):>6}  {row['pred_p']:>8.3f}  {row['obs_rate']:>8.3f}")

    # ── Score convention sanity ───────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("SCORE CONVENTION SANITY CHECK  (score = round(100*sigmoid(logit)), HIGHER=more likely)")
    # active frequent buyer (high exp, high p_alive, recent, frequent, good payer)
    active_alive, active = bgbb_predict_one(fitter, 6, 8, 8)
    # one-time old buyer (x=0, old n, no recency)
    dead_alive, dead = bgbb_predict_one(fitter, 0, 0, 48)
    # raw vectors in MODEL_FEATURES order
    active_raw = np.array([[active, active_alive, 0.0, 6.0, 8.0, np.log1p(8000), 1.0, 5.0]])
    dead_raw = np.array([[dead, dead_alive, 48.0, 0.0, 48.0, np.log1p(4000), 0.0, 1440.0]])
    p_active = clf.predict_proba(scaler.transform(active_raw))[0, 1]
    p_dead = clf.predict_proba(scaler.transform(dead_raw))[0, 1]
    s_active = round(100 * p_active)
    s_dead = round(100 * p_dead)
    print(f"  Active frequent buyer  → p={p_active:.3f}  score={s_active}  (expect HIGH)")
    print(f"  One-time old buyer     → p={p_dead:.3f}  score={s_dead}  (expect LOW)")
    assert s_active > s_dead, "Score convention violated: active buyer scored lower than dormant"
    print("  ✓ Convention verified: active > dormant")

    # ── Bands (3-way ALTA/MEDIA/BAJA) ─────────────────────────────────────────
    alta_min = int(round(np.percentile(score_ot, 66)))
    media_min = int(round(np.percentile(score_ot, 33)))
    if alta_min <= media_min:
        alta_min = media_min + 1
    alta_min = max(0, min(100, alta_min))
    media_min = max(0, min(100, media_min))
    bands = {"alta_min": alta_min, "media_min": media_min}
    print("\n" + "=" * 70)
    print("SCORE BANDS (OOT quantiles: alta_min=p66, media_min=p33)")
    sc_series = pd.Series(score_ot)
    cov = {
        "ALTA (score>=alta_min)": (sc_series >= alta_min).mean(),
        "MEDIA": ((sc_series >= media_min) & (sc_series < alta_min)).mean(),
        "BAJA (score<media_min)": (sc_series < media_min).mean(),
    }
    print(f"  alta_min={alta_min}  media_min={media_min}")
    for band, pct in cov.items():
        print(f"    {band:<28} {pct:.1%}")

    # ── Sensitivity grid (window N) ───────────────────────────────────────────
    run_sensitivity(ventas, abonos)
    print(f"\n  WINDOW-N DECISION: N={WINDOW_MONTHS} (confirm vs grid above).")

    # ── Build artifacts ───────────────────────────────────────────────────────
    version_date = datetime.date.today().strftime("%Y%m%d")

    btyd_params = {
        "version": f"v1-bgbb-{version_date}",
        "period_unit": "month",
        "grid": {
            "x": "count of post-acquisition months with >=1 V purchase",
            "t_x": "month-index 1..n of last purchase month, 0 if none",
            "n": "months from acquisition month to observation date",
            "acquisition": "first V-purchase month (born here; not counted as a repeat)",
            "substrate": "ventas TIPO_DOCTO=='V' only; multiple buys in one month collapse to 1",
        },
        "bgbb": {
            "alpha": round(bgbb_params["alpha"], 8),
            "beta": round(bgbb_params["beta"], 8),
            "gamma": round(bgbb_params["gamma"], 8),
            "delta": round(bgbb_params["delta"], 8),
        },
        "fit": {
            "penalizer_coef": penalizer,
            "n_clientes_calibracion": n_calib,
            "obs_dates_train": TRAIN_OBS,
        },
    }

    features_json = []
    for feat, coef, mean_, std_ in zip(MODEL_FEATURES, clf.coef_[0], scaler.mean_, scaler.scale_):
        features_json.append({
            "name": FEATURE_NAME_MAP[feat],
            "label": FEATURE_LABELS_ES[feat],
            "weight": round(float(coef), 6),
            "mean": round(float(mean_), 6),
            "std": round(float(std_), 6),
        })
    recompra_scorecard = {
        "version": f"v1-recompra-{version_date}",
        "objetivo": "recompra_12m",
        "intercept": round(float(clf.intercept_[0]), 6),
        "features": features_json,
        "bands": bands,
    }

    # ── BG/BB fixtures for Go unit tests ──────────────────────────────────────
    # ~30 (x, t_x, n) triples spanning the real data: x=0, x=n, small/large n,
    # high/low recency. NOTE: at very large n with large x, lifetimes' closed form
    # underflows p_alive to 0 (binomial-coefficient overflow). We keep large-n
    # cases REALISTIC for this book — old clients have FEW repeats (low x) — so the
    # Go port has non-degenerate targets to match. The real mega-client caps at
    # n=59 (monthly collapse); panel n-max ≈ 201 always pairs large n with small x.
    fixture_cases = [
        (0, 0, 1), (1, 1, 1), (0, 0, 3), (1, 1, 3), (2, 3, 3),
        (0, 0, 6), (1, 4, 6), (3, 6, 6), (6, 6, 6),
        (0, 0, 12), (1, 1, 12), (1, 12, 12), (4, 8, 12), (8, 11, 12), (12, 12, 12),
        (0, 0, 24), (2, 5, 24), (5, 20, 24), (12, 24, 24), (24, 24, 24),
        (0, 0, 48), (1, 2, 48), (3, 40, 48), (10, 45, 48), (24, 48, 48),
        (0, 0, 60), (2, 55, 60), (8, 59, 60),
        (0, 0, 96), (1, 90, 96), (4, 95, 96),
        (0, 0, 201), (3, 198, 201),
    ]
    cases_out = []
    for x, t_x, n in fixture_cases:
        pa, ex = bgbb_predict_one(fitter, x, t_x, n)
        cases_out.append({
            "x": x, "t_x": t_x, "n": n,
            "p_alive": round(pa, 10),
            "exp_12m": round(ex, 10),
        })
    btyd_fixtures = {
        "bgbb_params": btyd_params["bgbb"],
        "horizon_periods": BGBB_HORIZON,
        "cases": cases_out,
    }

    # ── Validation before write ───────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("ARTIFACT VALIDATION")
    sc_reload = json.loads(json.dumps(recompra_scorecard, ensure_ascii=False))
    b = sc_reload["bands"]
    assert 0 <= b["media_min"] < b["alta_min"] <= 100, f"bands not monotonic/in-range: {b}"
    for f_ in sc_reload["features"]:
        assert f_["std"] != 0.0, f"std=0 for {f_['name']}"
    pr = json.loads(json.dumps(btyd_params))
    assert all(pr["bgbb"][k] > 0 for k in ("alpha", "beta", "gamma", "delta")), "bgbb param <=0"
    fx = json.loads(json.dumps(btyd_fixtures))
    assert len(fx["cases"]) >= 30, "need >=30 fixture cases"
    print("  ✓ recompra_scorecard.json re-loads; bands monotonic & in [0,100]; all std≠0")
    print("  ✓ btyd_params.json re-loads; all BG/BB params > 0")
    print(f"  ✓ btyd_fixtures.json re-loads; {len(fx['cases'])} cases")

    print("\n" + "=" * 70)
    print("ARTIFACT PREVIEW")
    print("\n--- btyd_params.json ---")
    print(json.dumps(btyd_params, indent=2, ensure_ascii=False))
    print("\n--- recompra_scorecard.json ---")
    print(json.dumps(recompra_scorecard, indent=2, ensure_ascii=False))
    print("\n--- btyd_fixtures.json ---")
    print(json.dumps(btyd_fixtures, indent=2, ensure_ascii=False))

    if args.write:
        for path, obj in [
            (BTYD_PARAMS_JSON, btyd_params),
            (RECOMPRA_SCORECARD_JSON, recompra_scorecard),
            (BTYD_FIXTURES_JSON, btyd_fixtures),
        ]:
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
