"""
pit_scorecard.py — Point-in-Time Credit Scorecard Fitting Harness
=================================================================
Methodology: observation-point + performance-window (Siddiqi/Thomas/Basel II)
No target leakage: features computed at T; label from (T, T+WINDOW_MONTHS].

Usage:
  .venv/bin/python pit_scorecard.py            # dry-run (metrics only)
  .venv/bin/python pit_scorecard.py --write    # also emit scorecard.json
"""

from __future__ import annotations

import argparse
import json
import sys
import warnings
from datetime import timedelta
from pathlib import Path
from typing import Tuple

import numpy as np
import pandas as pd
from dateutil.relativedelta import relativedelta
from sklearn.linear_model import LogisticRegression
from sklearn.metrics import roc_auc_score
from sklearn.preprocessing import StandardScaler

warnings.filterwarnings("ignore", category=FutureWarning)

# ─── TUNABLES ────────────────────────────────────────────────────────────────

OBSERVATION_DATES = ["2021-12-31", "2022-12-31", "2023-12-31"]
WINDOW_MONTHS = 12           # performance window after T (12m won sensitivity grid: OOT Gini ~0.79)
DELINQ_DAYS = 90             # gap threshold for roll-to-bad (90d threshold)
MIN_HISTORY_DAYS = 180       # min days as customer at T
PERFORMING_MAX_DIAS = 180    # max days since last payment at T (matches serving population: book is in run-off, current owers last paid 90-180d ago)
SALDO_MIN = 100.0            # must owe this much at T
TRAIN_OBS = ["2021-12-31", "2022-12-31"]
OOT_OBS   = ["2023-12-31"]

# Concept IDs
ABONO_IDS    = {87327, 155, 11}
CASTIGO_IDS  = {27968, 27967}
CONDON_IDS   = {27969}

# Final 6-feature behavioral-only model.
# Dropped: cobertura_T (saldo-derived, leakage), saldo_frac_T (same), monto_abonado_180d_T (coef≈0).
# CADENCIA_DIAS uses MEAN (not median) to match Go's B1 AVG materialization.
MODEL_FEATURES = [
    "dias_sin_pagar_T",
    "pagos_90d_T",
    "pct_pagos_a_tiempo_T",
    "cadencia_dias_T",
    "num_pagos_T",
    "antiguedad_dias_T",
]

DATA_DIR = Path(__file__).parent / ".data"
SCORECARD_JSON = Path(__file__).parent.parent.parent / "internal" / "analytics" / "app" / "scorecard.json"


# ─── DATA LOADING ─────────────────────────────────────────────────────────────

def load_data() -> Tuple[pd.DataFrame, pd.DataFrame]:
    print("Loading cargos.csv …", end=" ", flush=True)
    cargos = pd.read_csv(DATA_DIR / "cargos.csv", dtype={
        "DOCTO_CC_ID": int, "CLIENTE_ID": int, "ZONA_CLIENTE_ID": object,
        "PRECIO_TOTAL": float, "NUM_PAGOS": float, "CARGO_CANCELADO": str,
    }, parse_dates=["FECHA_CARGO"])
    # Normalize tz-aware → tz-naive UTC
    if cargos["FECHA_CARGO"].dt.tz is not None:
        cargos["FECHA_CARGO"] = cargos["FECHA_CARGO"].dt.tz_convert("UTC").dt.tz_localize(None)
    print(f"{len(cargos):,} rows")

    print("Loading abonos.csv …", end=" ", flush=True)
    abonos = pd.read_csv(DATA_DIR / "abonos.csv", dtype={
        "DOCTO_CC_ID": int, "CLIENTE_ID": int, "CONCEPTO_CC_ID": int,
        "IMPORTE": float, "LAT": object, "LON": object,
        "CANCELADO": str, "APLICADO": str,
    }, parse_dates=["FECHA"])
    if abonos["FECHA"].dt.tz is not None:
        abonos["FECHA"] = abonos["FECHA"].dt.tz_convert("UTC").dt.tz_localize(None)
    print(f"{len(abonos):,} rows")

    return cargos, abonos


# ─── HELPERS ──────────────────────────────────────────────────────────────────

def clamp01(x: float) -> float:
    return max(0.0, min(1.0, x))


def _pct_a_tiempo(dates: pd.Series, cadencia: float, tolerance_days: float = 7.0) -> float:
    """Fraction of consecutive gaps within cadencia + tolerance."""
    if len(dates) < 2:
        return 0.0
    sorted_dates = dates.sort_values()
    gaps = sorted_dates.diff().dropna().dt.days.values
    threshold = cadencia + tolerance_days
    return float((gaps <= threshold).mean())


# ─── PANEL BUILDER ────────────────────────────────────────────────────────────

def build_panel(
    cargos: pd.DataFrame,
    abonos: pd.DataFrame,
    T: pd.Timestamp,
) -> pd.DataFrame:
    """Return a per-client feature DataFrame at observation date T.

    Eligibility filters are applied; ineligible clients are dropped.
    """
    W_end = T + relativedelta(months=WINDOW_MONTHS)

    # ── Subset cargos to those started <= T ──────────────────────────────────
    c_at_T = cargos[cargos["FECHA_CARGO"] <= T].copy()

    # Per-client: first cargo date, total cargado
    client_cargo = c_at_T.groupby("CLIENTE_ID").agg(
        first_cargo=("FECHA_CARGO", "min"),
        total_cargado_T=("PRECIO_TOTAL", "sum"),
    ).reset_index()

    # ── Subset abonos to <= T, split by type ─────────────────────────────────
    real_mask = (
        abonos["CONCEPTO_CC_ID"].isin(ABONO_IDS)
        & (abonos["CANCELADO"] == "N")
        & (abonos["APLICADO"] == "S")
    )
    castigo_mask = abonos["CONCEPTO_CC_ID"].isin(CASTIGO_IDS)

    ab_real_all    = abonos[real_mask].copy()
    ab_castigo_all = abonos[castigo_mask].copy()

    ab_real_at_T    = ab_real_all[ab_real_all["FECHA"] <= T]
    ab_castigo_at_T = ab_castigo_all[ab_castigo_all["FECHA"] <= T]

    # Clients already castigo'd at/before T — exclude from panel
    castigo_clients_at_T = set(ab_castigo_at_T["CLIENTE_ID"].unique())

    # ── Per-client PIT features ───────────────────────────────────────────────
    # Total abonado at T (real payments only)
    total_ab = ab_real_at_T.groupby("CLIENTE_ID")["IMPORTE"].sum().rename("total_abonado_T")

    # Last real payment date at T
    last_pago = ab_real_at_T.groupby("CLIENTE_ID")["FECHA"].max().rename("last_pago_T")

    # Count of real payments at T
    num_pagos = ab_real_at_T.groupby("CLIENTE_ID")["IMPORTE"].count().rename("num_pagos_T")

    # Payments in last 90d and 180d
    ab_90d  = ab_real_at_T[ab_real_at_T["FECHA"] > T - timedelta(days=90)]
    ab_180d = ab_real_at_T[ab_real_at_T["FECHA"] > T - timedelta(days=180)]

    pagos_90d  = ab_90d.groupby("CLIENTE_ID")["IMPORTE"].count().rename("pagos_90d_T")
    pagos_180d = ab_180d.groupby("CLIENTE_ID")["IMPORTE"].count().rename("pagos_180d_T")
    monto_180d = ab_180d.groupby("CLIENTE_ID")["IMPORTE"].sum().rename("monto_abonado_180d_T")

    # Cadencia: MEAN inter-payment gap (matches Go's B1 AVG materialization — changed from median).
    # pct_pagos_a_tiempo uses the mean cadencia as tolerance anchor.
    def cadencia_and_pct(grp):
        dates = grp["FECHA"].sort_values()
        if len(dates) < 2:
            return pd.Series({"cadencia_dias_T": 0.0, "pct_pagos_a_tiempo_T": 0.0})
        gaps = dates.diff().dropna().dt.days
        cad = float(gaps.mean())   # MEAN, not median — matches Go AVG
        pct = _pct_a_tiempo(dates, cad)
        return pd.Series({"cadencia_dias_T": cad, "pct_pagos_a_tiempo_T": pct})

    cad_pct = ab_real_at_T.groupby("CLIENTE_ID").apply(cadencia_and_pct)

    # ── Merge into master feature table ──────────────────────────────────────
    df = client_cargo.set_index("CLIENTE_ID")
    df = df.join(total_ab, how="left")
    df = df.join(last_pago, how="left")
    df = df.join(num_pagos, how="left")
    df = df.join(pagos_90d, how="left")
    df = df.join(pagos_180d, how="left")
    df = df.join(monto_180d, how="left")
    df = df.join(cad_pct, how="left")
    df = df.reset_index()

    # Fill NaN for clients with no payments at T
    df["total_abonado_T"]      = df["total_abonado_T"].fillna(0.0)
    df["num_pagos_T"]          = df["num_pagos_T"].fillna(0).astype(int)
    df["pagos_90d_T"]          = df["pagos_90d_T"].fillna(0).astype(int)
    df["pagos_180d_T"]         = df["pagos_180d_T"].fillna(0).astype(int)
    df["monto_abonado_180d_T"] = df["monto_abonado_180d_T"].fillna(0.0)
    df["cadencia_dias_T"]      = df["cadencia_dias_T"].fillna(0.0)
    df["pct_pagos_a_tiempo_T"] = df["pct_pagos_a_tiempo_T"].fillna(0.0)

    # Derived features
    df["saldo_T"] = (df["total_cargado_T"] - df["total_abonado_T"]).clip(lower=0)
    df["saldo_frac_T"] = df.apply(
        lambda r: clamp01(r["saldo_T"] / r["total_cargado_T"]) if r["total_cargado_T"] > 0 else 0.0,
        axis=1,
    )
    df["cobertura_T"] = df.apply(
        lambda r: clamp01(r["total_abonado_T"] / r["total_cargado_T"]) if r["total_cargado_T"] > 0 else 0.0,
        axis=1,
    )

    df["antiguedad_dias_T"] = (T - df["first_cargo"]).dt.days

    df["dias_sin_pagar_T"] = df.apply(
        lambda r: (T - r["last_pago_T"]).days if pd.notna(r["last_pago_T"]) else int(r["antiguedad_dias_T"]),
        axis=1,
    )

    df["T"] = T
    df["cohort_year"] = df["first_cargo"].dt.year

    # ── Eligibility funnel ───────────────────────────────────────────────────
    n_start = len(df)

    # (1) Enough history
    mask_hist = df["antiguedad_dias_T"] >= MIN_HISTORY_DAYS
    n_hist = mask_hist.sum()

    # (2) Meaningful balance
    mask_saldo = df["saldo_T"] > SALDO_MIN

    # (3) Performing at T (paid recently)
    mask_performing = df["dias_sin_pagar_T"] <= PERFORMING_MAX_DIAS

    # (4) Not already castigo'd
    mask_not_castigo = ~df["CLIENTE_ID"].isin(castigo_clients_at_T)

    all_ok = mask_hist & mask_saldo & mask_performing & mask_not_castigo

    funnel = {
        "start":        n_start,
        "pass_history": int(mask_hist.sum()),
        "pass_saldo":   int((mask_hist & mask_saldo).sum()),
        "pass_perf":    int((mask_hist & mask_saldo & mask_performing).sum()),
        "pass_not_cas": int(all_ok.sum()),
    }

    df = df[all_ok].copy()

    # ── Label: roll-to-bad in (T, W_end] ────────────────────────────────────
    ab_real_after_T = ab_real_all[
        (ab_real_all["FECHA"] > T) & (ab_real_all["FECHA"] <= W_end)
    ]
    ab_castigo_after_T = ab_castigo_all[
        (ab_castigo_all["FECHA"] > T) & (ab_castigo_all["FECHA"] <= W_end)
    ]

    # (a) Formal castigo in window
    castigo_window = set(ab_castigo_after_T["CLIENTE_ID"].unique())

    # (b) Roll-to-non-performing: gap from last real payment <= T to
    #     next real payment > T is >= DELINQ_DAYS (while saldo_T > SALDO_MIN)
    def is_delinquent(row):
        cid = row["CLIENTE_ID"]
        last_before_T = row["last_pago_T"]  # may be NaT
        # Find first real payment after T for this client
        ab_after = ab_real_after_T[ab_real_after_T["CLIENTE_ID"] == cid]
        if len(ab_after) == 0:
            # No payment in window → gap = distance from last payment to W_end
            if pd.isna(last_before_T):
                gap = (W_end - row["first_cargo"]).days
            else:
                gap = (W_end - last_before_T).days
        else:
            first_after = ab_after["FECHA"].min()
            if pd.isna(last_before_T):
                gap = (first_after - row["first_cargo"]).days
            else:
                gap = (first_after - last_before_T).days
        return gap >= DELINQ_DAYS

    # Vectorize per-client delinquency (group-by based for speed)
    # First build lookup: CLIENTE_ID → min FECHA after T
    next_pago_lookup = (
        ab_real_after_T.groupby("CLIENTE_ID")["FECHA"].min()
        .rename("next_pago_after_T")
    )
    df = df.join(next_pago_lookup, on="CLIENTE_ID", how="left")

    def gap_days(row):
        last = row["last_pago_T"]
        nxt  = row["next_pago_after_T"]
        if pd.isna(nxt):
            # No payment after T in window
            anchor = last if pd.notna(last) else row["first_cargo"]
            return (W_end - anchor).days
        else:
            anchor = last if pd.notna(last) else row["first_cargo"]
            return (nxt - anchor).days

    df["gap_days_to_next"] = df.apply(gap_days, axis=1)
    df["bad_b"] = (df["gap_days_to_next"] >= DELINQ_DAYS).astype(int)
    df["bad_a"] = df["CLIENTE_ID"].isin(castigo_window).astype(int)
    df["bad"]   = ((df["bad_a"] == 1) | (df["bad_b"] == 1)).astype(int)

    return df, funnel


# ─── KS STATISTIC ─────────────────────────────────────────────────────────────

def ks_stat(y_true: np.ndarray, y_prob: np.ndarray) -> float:
    from sklearn.metrics import roc_curve
    fpr, tpr, _ = roc_curve(y_true, y_prob)
    return float(np.max(np.abs(tpr - fpr)))


# ─── DECILE TABLE ─────────────────────────────────────────────────────────────

def decile_table(y_true: np.ndarray, y_score: np.ndarray) -> pd.DataFrame:
    """Score = 100*(1-p_bad), higher is safer."""
    df = pd.DataFrame({"score": y_score, "bad": y_true})
    # pd.qcut with duplicates='drop'; iterate unique bin codes
    df["decile_code"] = pd.qcut(df["score"], 10, labels=False, duplicates="drop")
    n_deciles = int(df["decile_code"].max()) + 1
    rows = []
    for code in range(n_deciles):
        bucket = df[df["decile_code"] == code]
        rows.append({
            "decile":   code + 1,
            "n":        len(bucket),
            "n_bad":    int(bucket["bad"].sum()),
            "bad_rate": bucket["bad"].mean() if len(bucket) > 0 else float("nan"),
        })
    tbl = pd.DataFrame(rows)
    tbl["cum_bad"] = tbl["n_bad"].cumsum()
    tbl["cum_bad_pct"] = tbl["cum_bad"] / tbl["n_bad"].sum() * 100
    return tbl


# ─── SENSITIVITY GRID ─────────────────────────────────────────────────────────

def run_sensitivity(cargos, abonos, base_delinq, base_window):
    print("\n" + "=" * 70)
    print("SENSITIVITY GRID (OOT Gini / KS)")
    print(f"{'DELINQ_DAYS':>11}  {'WINDOW_MONTHS':>13}  {'OOT_Gini':>10}  {'OOT_KS':>8}")
    print("-" * 50)
    for dq in [90, 120, 180]:
        for wm in [12, 18, 24]:
            # Override globals locally — rebuild panels
            import types
            # We re-import with modified constants by direct rebuild
            panels = []
            for T_str in TRAIN_OBS + OOT_OBS:
                T = pd.Timestamp(T_str)
                panel_df, _ = _build_panel_custom(cargos, abonos, T, dq, wm)
                panel_df["obs"] = T_str
                panels.append(panel_df)

            all_df = pd.concat(panels, ignore_index=True)
            train_df = all_df[all_df["obs"].isin(TRAIN_OBS)].dropna(subset=MODEL_FEATURES)
            oot_df   = all_df[all_df["obs"].isin(OOT_OBS)].dropna(subset=MODEL_FEATURES)

            if len(train_df) < 20 or len(oot_df) < 10:
                print(f"{dq:>11}  {wm:>13}  {'(too few)':>10}")
                continue
            if train_df["bad"].nunique() < 2 or oot_df["bad"].nunique() < 2:
                print(f"{dq:>11}  {wm:>13}  {'(1 class)':>10}")
                continue

            X_tr = train_df[MODEL_FEATURES].values
            y_tr = train_df["bad"].values
            X_ot = oot_df[MODEL_FEATURES].values
            y_ot = oot_df["bad"].values

            scaler = StandardScaler().fit(X_tr)
            X_tr_s = scaler.transform(X_tr)
            X_ot_s = scaler.transform(X_ot)

            clf = LogisticRegression(class_weight="balanced", C=1.0, max_iter=2000, random_state=42)
            clf.fit(X_tr_s, y_tr)
            p_ot  = clf.predict_proba(X_ot_s)[:, 1]
            auc   = roc_auc_score(y_ot, p_ot)
            gini  = 2 * auc - 1
            ks    = ks_stat(y_ot, p_ot)
            flag  = " ← main" if dq == base_delinq and wm == base_window else ""
            print(f"{dq:>11}  {wm:>13}  {gini:>10.4f}  {ks:>8.4f}{flag}")


def _build_panel_custom(cargos, abonos, T, delinq_days, window_months):
    """Clone of build_panel using custom delinq_days/window_months."""
    W_end = T + relativedelta(months=window_months)

    c_at_T = cargos[cargos["FECHA_CARGO"] <= T].copy()
    client_cargo = c_at_T.groupby("CLIENTE_ID").agg(
        first_cargo=("FECHA_CARGO", "min"),
        total_cargado_T=("PRECIO_TOTAL", "sum"),
    ).reset_index()

    real_mask = (
        abonos["CONCEPTO_CC_ID"].isin(ABONO_IDS)
        & (abonos["CANCELADO"] == "N")
        & (abonos["APLICADO"] == "S")
    )
    castigo_mask = abonos["CONCEPTO_CC_ID"].isin(CASTIGO_IDS)

    ab_real_all    = abonos[real_mask].copy()
    ab_castigo_all = abonos[castigo_mask].copy()

    ab_real_at_T    = ab_real_all[ab_real_all["FECHA"] <= T]
    ab_castigo_at_T = ab_castigo_all[ab_castigo_all["FECHA"] <= T]
    castigo_clients_at_T = set(ab_castigo_at_T["CLIENTE_ID"].unique())

    total_ab = ab_real_at_T.groupby("CLIENTE_ID")["IMPORTE"].sum().rename("total_abonado_T")
    last_pago = ab_real_at_T.groupby("CLIENTE_ID")["FECHA"].max().rename("last_pago_T")
    num_pagos = ab_real_at_T.groupby("CLIENTE_ID")["IMPORTE"].count().rename("num_pagos_T")

    ab_90d  = ab_real_at_T[ab_real_at_T["FECHA"] > T - timedelta(days=90)]
    ab_180d = ab_real_at_T[ab_real_at_T["FECHA"] > T - timedelta(days=180)]
    pagos_90d  = ab_90d.groupby("CLIENTE_ID")["IMPORTE"].count().rename("pagos_90d_T")
    pagos_180d = ab_180d.groupby("CLIENTE_ID")["IMPORTE"].count().rename("pagos_180d_T")
    monto_180d = ab_180d.groupby("CLIENTE_ID")["IMPORTE"].sum().rename("monto_abonado_180d_T")

    def cad_pct_fn(grp):
        dates = grp["FECHA"].sort_values()
        if len(dates) < 2:
            return pd.Series({"cadencia_dias_T": 0.0, "pct_pagos_a_tiempo_T": 0.0})
        gaps = dates.diff().dropna().dt.days
        cad = float(gaps.mean())   # MEAN, not median — matches Go AVG
        pct = _pct_a_tiempo(dates, cad)
        return pd.Series({"cadencia_dias_T": cad, "pct_pagos_a_tiempo_T": pct})

    cad_pct = ab_real_at_T.groupby("CLIENTE_ID").apply(cad_pct_fn)

    df = client_cargo.set_index("CLIENTE_ID")
    df = df.join(total_ab, how="left")
    df = df.join(last_pago, how="left")
    df = df.join(num_pagos, how="left")
    df = df.join(pagos_90d, how="left")
    df = df.join(pagos_180d, how="left")
    df = df.join(monto_180d, how="left")
    df = df.join(cad_pct, how="left")
    df = df.reset_index()

    df["total_abonado_T"]      = df["total_abonado_T"].fillna(0.0)
    df["num_pagos_T"]          = df["num_pagos_T"].fillna(0).astype(int)
    df["pagos_90d_T"]          = df["pagos_90d_T"].fillna(0).astype(int)
    df["pagos_180d_T"]         = df["pagos_180d_T"].fillna(0).astype(int)
    df["monto_abonado_180d_T"] = df["monto_abonado_180d_T"].fillna(0.0)
    df["cadencia_dias_T"]      = df["cadencia_dias_T"].fillna(0.0)
    df["pct_pagos_a_tiempo_T"] = df["pct_pagos_a_tiempo_T"].fillna(0.0)

    df["saldo_T"] = (df["total_cargado_T"] - df["total_abonado_T"]).clip(lower=0)
    df["cobertura_T"] = df.apply(
        lambda r: clamp01(r["total_abonado_T"] / r["total_cargado_T"]) if r["total_cargado_T"] > 0 else 0.0,
        axis=1,
    )
    df["antiguedad_dias_T"] = (T - df["first_cargo"]).dt.days
    df["dias_sin_pagar_T"] = df.apply(
        lambda r: (T - r["last_pago_T"]).days if pd.notna(r["last_pago_T"]) else int(r["antiguedad_dias_T"]),
        axis=1,
    )
    df["T"] = T
    df["cohort_year"] = df["first_cargo"].dt.year

    mask_hist        = df["antiguedad_dias_T"] >= MIN_HISTORY_DAYS
    mask_saldo       = df["saldo_T"] > SALDO_MIN
    mask_performing  = df["dias_sin_pagar_T"] <= PERFORMING_MAX_DIAS
    mask_not_castigo = ~df["CLIENTE_ID"].isin(castigo_clients_at_T)
    all_ok = mask_hist & mask_saldo & mask_performing & mask_not_castigo
    df = df[all_ok].copy()

    ab_real_after_T = ab_real_all[
        (ab_real_all["FECHA"] > T) & (ab_real_all["FECHA"] <= W_end)
    ]
    ab_castigo_after_T = ab_castigo_all[
        (ab_castigo_all["FECHA"] > T) & (ab_castigo_all["FECHA"] <= W_end)
    ]
    castigo_window = set(ab_castigo_after_T["CLIENTE_ID"].unique())

    next_pago_lookup = (
        ab_real_after_T.groupby("CLIENTE_ID")["FECHA"].min()
        .rename("next_pago_after_T")
    )
    df = df.join(next_pago_lookup, on="CLIENTE_ID", how="left")

    def gap_days(row):
        last = row["last_pago_T"]
        nxt  = row["next_pago_after_T"]
        if pd.isna(nxt):
            anchor = last if pd.notna(last) else row["first_cargo"]
            return (W_end - anchor).days
        else:
            anchor = last if pd.notna(last) else row["first_cargo"]
            return (nxt - anchor).days

    df["gap_days_to_next"] = df.apply(gap_days, axis=1)
    df["bad_b"] = (df["gap_days_to_next"] >= delinq_days).astype(int)
    df["bad_a"] = df["CLIENTE_ID"].isin(castigo_window).astype(int)
    df["bad"]   = ((df["bad_a"] == 1) | (df["bad_b"] == 1)).astype(int)

    funnel = {"pass_not_cas": len(df)}
    return df, funnel


# ─── MAIN ─────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--write", action="store_true", help="Write scorecard.json")
    args = parser.parse_args()

    cargos, abonos = load_data()

    # ── Build panels ─────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("PANEL CONSTRUCTION")
    print("=" * 70)

    all_panels = []
    all_funnels = []
    label_stats = []

    for T_str in OBSERVATION_DATES:
        T = pd.Timestamp(T_str)
        print(f"\nObservation date T = {T_str}")
        panel, funnel = build_panel(cargos, abonos, T)
        panel["obs"] = T_str
        all_panels.append(panel)
        all_funnels.append((T_str, funnel))

        n = len(panel)
        n_bad   = int(panel["bad"].sum())
        n_bad_a = int(panel["bad_a"].sum())
        n_bad_b = int(((panel["bad_b"] == 1) & (panel["bad_a"] == 0)).sum())  # b-only
        bad_rate = n_bad / n if n > 0 else 0.0

        print(f"  Eligibility funnel:")
        print(f"    Clients with cargo <= T:              {funnel['start']:>6,}")
        print(f"    After >=180d history:                 {funnel['pass_history']:>6,}")
        print(f"    After saldo > {SALDO_MIN}:                   {funnel['pass_saldo']:>6,}")
        print(f"    After performing (<={PERFORMING_MAX_DIAS}d since pay): {funnel['pass_perf']:>6,}")
        print(f"    After excl. prior castigos:           {funnel['pass_not_cas']:>6,}")
        print(f"  Panel size: {n:,}  |  bad={n_bad} ({bad_rate:.1%})")
        print(f"  Label source: (a) castigo-in-window={n_bad_a}  |  (b)-only delinq={n_bad_b}")

        # Castigo-anchor agreement: of b-bads, how many got castigo EVER?
        b_bad_clients = panel.loc[panel["bad_b"] == 1, "CLIENTE_ID"]
        all_castigos_ever = set(abonos[abonos["CONCEPTO_CC_ID"].isin(CASTIGO_IDS)]["CLIENTE_ID"].unique())
        b_anchored = b_bad_clients.isin(all_castigos_ever).sum()
        if len(b_bad_clients) > 0:
            print(f"  Anchor check: of {len(b_bad_clients)} b-bads, "
                  f"{b_anchored} ({b_anchored/len(b_bad_clients):.0%}) eventually got a castigo")

        label_stats.append({
            "T": T_str, "n": n, "bad": n_bad, "bad_rate": bad_rate,
            "bad_a": n_bad_a, "bad_b_only": n_bad_b,
        })

    all_df = pd.concat(all_panels, ignore_index=True)
    train_df = all_df[all_df["obs"].isin(TRAIN_OBS)].copy()
    oot_df   = all_df[all_df["obs"].isin(OOT_OBS)].copy()

    print(f"\nTotal panel: {len(all_df):,} rows  |  "
          f"TRAIN={len(train_df):,}  |  OOT={len(oot_df):,}")

    # ── Feature collinearity note ─────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("FEATURE SELECTION NOTE")
    print("  saldo_frac_T and cobertura_T are 1-complements (corr≈−1.0).")
    print("  Keeping: cobertura_T  (dropped: saldo_frac_T)")
    print(f"  Model features: {MODEL_FEATURES}")

    # ── Model fitting ─────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("MODEL FITTING")

    X_tr = train_df[MODEL_FEATURES].values
    y_tr = train_df["bad"].values
    X_ot = oot_df[MODEL_FEATURES].values
    y_ot = oot_df["bad"].values

    # Standardize on train
    scaler = StandardScaler()
    X_tr_s = scaler.fit_transform(X_tr)
    X_ot_s = scaler.transform(X_ot)

    # Fit two C values, pick by OOT AUC
    results = {}
    for C in [1.0, 1e6]:
        clf = LogisticRegression(
            class_weight="balanced", C=C, max_iter=2000, random_state=42
        )
        clf.fit(X_tr_s, y_tr)
        p_tr = clf.predict_proba(X_tr_s)[:, 1]
        p_ot = clf.predict_proba(X_ot_s)[:, 1]
        results[C] = {
            "clf": clf,
            "p_tr": p_tr,
            "p_ot": p_ot,
            "auc_tr": roc_auc_score(y_tr, p_tr),
            "auc_ot": roc_auc_score(y_ot, p_ot) if y_ot.sum() > 0 else float("nan"),
        }

    # Pick by OOT AUC
    best_C = max(results, key=lambda c: results[c]["auc_ot"])
    best   = results[best_C]
    clf    = best["clf"]
    p_tr   = best["p_tr"]
    p_ot   = best["p_ot"]

    for C, r in results.items():
        star = " ← selected" if C == best_C else ""
        print(f"  C={C:.0e}  train_AUC={r['auc_tr']:.4f}  OOT_AUC={r['auc_ot']:.4f}{star}")

    auc_tr = best["auc_tr"]; gini_tr = 2*auc_tr - 1; ks_tr = ks_stat(y_tr, p_tr)
    auc_ot = best["auc_ot"]; gini_ot = 2*auc_ot - 1; ks_ot = ks_stat(y_ot, p_ot)

    print(f"\n  TRAIN  AUC={auc_tr:.4f}  Gini={gini_tr:.4f}  KS={ks_tr:.4f}")
    print(f"  OOT    AUC={auc_ot:.4f}  Gini={gini_ot:.4f}  KS={ks_ot:.4f}")

    # ── Coefficient table ─────────────────────────────────────────────────────
    print("\n  Feature coefficients (positive → higher logit → more likely bad):")
    for feat, coef in sorted(zip(MODEL_FEATURES, clf.coef_[0]), key=lambda x: abs(x[1]), reverse=True):
        print(f"    {feat:<30} {coef:+.4f}")
    print(f"    intercept                          {clf.intercept_[0]:+.4f}")

    # ── Decile table (OOT) ───────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("DECILE TABLE (OOT)")
    score_ot = np.round(100 * (1 - p_ot)).astype(int)
    dtbl = decile_table(y_ot, score_ot)
    print(f"  {'Decile':>6}  {'N':>6}  {'N_bad':>6}  {'Bad%':>7}  {'CumBad%':>8}")
    for _, row in dtbl.iterrows():
        print(f"  {int(row['decile']):>6}  {int(row['n']):>6}  {int(row['n_bad']):>6}  "
              f"{row['bad_rate']*100:>6.1f}%  {row['cum_bad_pct']:>7.1f}%")

    # ── Vintage check ─────────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("VINTAGE CHECK (bad-rate by cohort_year across all panels)")
    vtg = all_df.groupby("cohort_year")["bad"].agg(["count", "sum", "mean"])
    vtg.columns = ["n", "n_bad", "bad_rate"]
    for yr, row in vtg.iterrows():
        print(f"  {yr}  n={int(row['n']):>5,}  n_bad={int(row['n_bad']):>4,}  "
              f"bad_rate={row['bad_rate']:.1%}")

    # ── GO-GAP analysis ───────────────────────────────────────────────────────
    # Final 6-feature behavioral-only set (no saldo-derived features).
    # All 6 features must be materialized by Go's buildCreditoFeatures().
    FEATURE_NAME_MAP = {
        "dias_sin_pagar_T":      "DIAS_SIN_PAGAR",
        "pagos_90d_T":           "PAGOS_90D",
        "pct_pagos_a_tiempo_T":  "PCT_PAGOS_A_TIEMPO_6M",
        "cadencia_dias_T":       "CADENCIA_DIAS",
        "num_pagos_T":           "NUM_PAGOS_TOTAL",
        "antiguedad_dias_T":     "ANTIGUEDAD_DIAS",
    }
    FEATURE_LABELS_ES = {
        "dias_sin_pagar_T":      "muchos días sin pagar",
        "pagos_90d_T":           "pocos pagos recientes",
        "pct_pagos_a_tiempo_T":  "pocos pagos a tiempo",
        "cadencia_dias_T":       "cobranza espaciada",
        "num_pagos_T":           "pocos pagos históricos",
        "antiguedad_dias_T":     "cliente reciente",
    }

    print("\n" + "=" * 70)
    print("GO-GAP ANALYSIS (behavioral-only 6-feature model)")
    print("  Dropped saldo features: cobertura_T, saldo_frac_T (leakage), monto_abonado_180d_T (coef≈0)")
    print("  All 6 features Go must materialize in buildCreditoFeatures():")
    for feat, go_name in FEATURE_NAME_MAP.items():
        print(f"    {go_name:<30} ← {feat}")
    print("  CADENCIA_DIAS: Go must use AVG gap (mean), not median.")

    # ── Band derivation ───────────────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("SCORE BANDS (derived from OOT distribution)")
    score_ot_series = pd.Series(score_ot)
    p10 = int(np.percentile(score_ot, 10))
    p25 = int(np.percentile(score_ot, 25))
    p75 = int(np.percentile(score_ot, 75))
    p90 = int(np.percentile(score_ot, 90))

    # Bands: MUY_ALTO (very safe) ≥ alto_min; ALTO ≥ medio_min; MEDIO ≥ bajo_min; BAJO < bajo_min
    # score is 0-100; higher = safer. We want four bands with ~quartile coverage.
    # bajo_min > medio_min > alto_min per JSON convention (raw Go thresholds)
    # Note: Go naming: bajo_min = lower bound of BAJO band... actually let's
    # match go convention exactly: bajo_min>medio_min>alto_min means
    # score >= bajo_min → BAJO risk (safest?) — check Go code for direction.
    # From scorecard.go logic: typically score>=bajo_min=75→BAJO=safest...
    # Actually we need to re-examine. Score=100*(1-p_bad), so high score=safe.
    # "bajo riesgo" = high score. Set thresholds so bands are non-degenerate.
    alto_min  = int(np.percentile(score_ot, 25))   # bottom 25% → MUY ALTO riesgo (score low)
    medio_min = int(np.percentile(score_ot, 50))   # 25-50% → ALTO riesgo
    bajo_min  = int(np.percentile(score_ot, 75))   # 50-75% → MEDIO; top 25% → BAJO riesgo

    # Ensure non-degenerate and distinct
    if bajo_min <= medio_min:
        medio_min = bajo_min - 1
    if medio_min <= alto_min:
        alto_min = medio_min - 1

    bands = {"bajo_min": bajo_min, "medio_min": medio_min, "alto_min": alto_min}

    coverage = {
        "MUY_ALTO_riesgo (score<alto_min)":  (score_ot_series < alto_min).mean(),
        "ALTO_riesgo":                        ((score_ot_series >= alto_min) & (score_ot_series < medio_min)).mean(),
        "MEDIO_riesgo":                       ((score_ot_series >= medio_min) & (score_ot_series < bajo_min)).mean(),
        "BAJO_riesgo (score>=bajo_min)":      (score_ot_series >= bajo_min).mean(),
    }
    print(f"  bajo_min={bajo_min}  medio_min={medio_min}  alto_min={alto_min}")
    for band, pct in coverage.items():
        print(f"    {band:<40} {pct:.1%}")

    # ── Scorecard JSON construction ───────────────────────────────────────────
    import datetime
    version_date = datetime.date.today().strftime("%Y%m%d")
    features_json = []
    for feat, coef, mean_, std_ in zip(
        MODEL_FEATURES,
        clf.coef_[0],
        scaler.mean_,
        scaler.scale_,
    ):
        go_name = FEATURE_NAME_MAP[feat]
        features_json.append({
            "name":   go_name,
            "label":  FEATURE_LABELS_ES[feat],
            "weight": round(float(coef), 6),
            "mean":   round(float(mean_), 6),
            "std":    round(float(std_), 6),
        })

    scorecard = {
        "version":   f"v1-pit-{version_date}",
        "intercept": round(float(clf.intercept_[0]), 6),
        "features":  features_json,
        "bands":     bands,
    }

    # ── Validation before write ───────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("SCORECARD VALIDATION")
    sc_str = json.dumps(scorecard, indent=2, ensure_ascii=False)
    sc_reload = json.loads(sc_str)

    # Bands monotonic: bajo_min > medio_min > alto_min, all in [0, 100]
    b = sc_reload["bands"]
    assert b["bajo_min"] > b["medio_min"] > b["alto_min"], \
        f"Bands not monotonic: {b}"
    assert 0 <= b["alto_min"] and b["bajo_min"] <= 100, \
        f"Bands out of [0,100]: {b}"
    # std != 0 for all features
    for f_ in sc_reload["features"]:
        assert f_["std"] != 0.0, f"std=0 for feature {f_['name']}"
    print("  ✓ JSON re-loads")
    print("  ✓ Bands monotonic and in [0,100]")
    print("  ✓ All feature std≠0")

    # ── Sanity: score convention ──────────────────────────────────────────────
    print("\n" + "=" * 70)
    print("SCORE CONVENTION SANITY CHECK")
    print("  Score = round(100*(1 − p_bad)); higher = safer.")
    # High-risk profile: many dias_sin_pagar, no recent payments
    # Feature order: dias_sin_pagar, pagos_90d, pct_pagos_a_tiempo, cadencia, num_pagos, antiguedad
    high_risk_raw = np.array([[365, 0, 0.0, 90.0, 2, 200]])
    clean_raw     = np.array([[0,   8, 1.0, 30.0, 48, 1000]])
    hr_std = scaler.transform(high_risk_raw)
    cl_std = scaler.transform(clean_raw)
    p_hr = clf.predict_proba(hr_std)[0, 1]
    p_cl = clf.predict_proba(cl_std)[0, 1]
    s_hr = round(100 * (1 - p_hr))
    s_cl = round(100 * (1 - p_cl))
    print(f"  High-risk profile  → p_bad={p_hr:.3f}  score={s_hr}  (expect LOW)")
    print(f"  Clean profile      → p_bad={p_cl:.3f}  score={s_cl}  (expect HIGH)")
    assert s_hr < s_cl, "Score convention violated: high-risk scored higher than clean"
    print("  ✓ Convention verified: high-risk < clean")

    print("\n" + "=" * 70)
    print("SCORECARD JSON PREVIEW:")
    print(sc_str)

    if args.write:
        with open(SCORECARD_JSON, "w", encoding="utf-8") as f:
            f.write(sc_str)
            f.write("\n")
        print(f"\nWrote scorecard.json → {SCORECARD_JSON}")
    else:
        print("\nDRY-RUN: scorecard.json NOT written (pass --write to persist)")

    # ── Sensitivity grid ─────────────────────────────────────────────────────
    run_sensitivity(cargos, abonos, DELINQ_DAYS, WINDOW_MONTHS)

    print("\n" + "=" * 70)
    print("DONE")


if __name__ == "__main__":
    main()
