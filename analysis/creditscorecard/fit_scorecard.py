"""
fit_scorecard.py — offline credit-scorecard fitting harness (R4).

Trains the logistic-regression credit-risk scorecard that is embedded in the
Go API at internal/analytics/app/scorecard.json and served via go:embed.  This
script is dev-only and runs on the developer's Mac; it is never deployed to the
production Windows server (CLAUDE.md rule #5).

Feature / label contract:
    Features are read from the SAME materialized columns that the Go scorer
    reads (buildCreditoFeatures in internal/analytics/app/scoring.go).  Do NOT
    recompute from raw Microsip tables — the materialized values are what Go
    scores at serve time, so training on them is the only way to guarantee that
    the trained score equals the served score.

Prerequisite:
    The cobranza refresh job must have been run so that PCT_PAGOS_A_TIEMPO and
    DIAS_ATRASO_PROM are populated in MSP_AN_WINBACK_CANDIDATOS.  Without that,
    two of four features are NULL, the script warns loudly, and you should abort
    the run rather than fitting a degenerate model.

Usage:
    # Export DB creds from .env first:
    set -a; . ./.env; set +a
    python analysis/creditscorecard/fit_scorecard.py --version v1-YYYYMMDD
    python analysis/creditscorecard/fit_scorecard.py --version v1-YYYYMMDD --dry-run
"""

from __future__ import annotations

import argparse
import json
import math
import os
import sys
import warnings
from datetime import date
from typing import Any

# ---------------------------------------------------------------------------
# Feature definitions — single source of truth mapping Python → scorecard.json
# ---------------------------------------------------------------------------
# Each dict contains:
#   name  : the scorecard feature name (must match Go's buildCreditoFeatures key)
#   label : Spanish display label used in risk-driver explanations
#   raw   : name of the column in the SELECT result that holds the raw value
# ---------------------------------------------------------------------------
FEATURES: list[dict[str, str]] = [
    {
        "name": "SALDO_FRAC",
        "label": "saldo alto pendiente",
        "raw": "POR_LIQUIDAR_PCT",
    },
    {
        "name": "COBERTURA_PLAN",
        "label": "baja cobertura del plan de pago",
        "raw": "_COBERTURA_PLAN_derived",  # derived from POR_LIQUIDAR_PCT
    },
    {
        "name": "PCT_PAGOS_A_TIEMPO_6M",
        "label": "pocos pagos a tiempo recientes",
        "raw": "PCT_PAGOS_A_TIEMPO",
    },
    {
        "name": "DIAS_ATRASO_PROM",
        "label": "atraso promedio elevado",
        "raw": "DIAS_ATRASO_PROM",
    },
]

# Label: write-off / charge-off concepts (Mal Cliente = 27968, Fugas = 27967)
CASTIGO_CONCEPTO_IDS: tuple[int, ...] = (27968, 27967)

# Scorecard output path relative to the repo root (Go embeds this at compile time)
SCORECARD_JSON_PATH = "internal/analytics/app/scorecard.json"

# Default band thresholds (score in [0,100]; higher = safer payer)
#   ≥ bajo_min  → BAJO    (low risk)
#   ≥ medio_min → MEDIO
#   ≥ alto_min  → ALTO
#   else        → CRITICO (highest risk)
# The Go Scorecard parser enforces bajo_min > medio_min > alto_min.
DEFAULT_BANDS = {"bajo_min": 75, "medio_min": 50, "alto_min": 25}

# Out-of-time holdout cutoff: clients whose last payment is on or after this
# date are held out from training and used as the out-of-time validation set.
# Chosen so the later period contains at least 6 months of data.  Adjust if
# the payment history in the dev DB has a different tail.
OOT_CUTOFF_DATE = "2024-07-01"


# ---------------------------------------------------------------------------
# Pure helpers (no DB, no sklearn — importable without deps for unit testing)
# ---------------------------------------------------------------------------

def clamp01(x: float) -> float:
    """Clamp x to [0, 1] — mirrors Go's clamp01."""
    if x < 0.0:
        return 0.0
    if x > 1.0:
        return 1.0
    return x


def _coerce_float(val: Any) -> float:
    """Coerce a nullable numeric DB value to float, treating NULL as 0.0."""
    if val is None:
        return 0.0
    try:
        return float(val)
    except (TypeError, ValueError):
        return 0.0


# ---------------------------------------------------------------------------
# 1. Data export
# ---------------------------------------------------------------------------

def load_dataset(conn: Any) -> "pd.DataFrame":  # type: ignore[name-defined]
    """
    Execute a single read-only SELECT that joins the materialized feature
    columns with a LABEL_CASTIGO subquery and returns a pandas DataFrame.

    conn : a live firebird-driver connection.
    """
    import pandas as pd  # noqa: PLC0415

    sql = """
        SELECT
            c.CLIENTE_ID,
            c.POR_LIQUIDAR_PCT,
            c.PCT_PAGOS_A_TIEMPO,
            c.DIAS_ATRASO_PROM,
            c.SALDO,
            c.NUM_PAGOS,
            c.FECHA_ULTIMO_PAGO,
            c.MONETARY,
            c.COHORTE_FECHA,
            CASE
                WHEN EXISTS (
                    SELECT 1 FROM MSP_PAGOS_VENTAS p
                    WHERE p.CLIENTE_ID = c.CLIENTE_ID
                      AND p.CONCEPTO_CC_ID IN (27968, 27967)
                ) THEN 1 ELSE 0
            END AS LABEL_CASTIGO
        FROM MSP_AN_WINBACK_CANDIDATOS c
    """

    cursor = conn.cursor()
    cursor.execute(sql)
    columns = [desc[0].upper() for desc in cursor.description]
    rows = cursor.fetchall()
    cursor.close()

    df = pd.DataFrame(rows, columns=columns)
    print(f"[load_dataset] rows fetched: {len(df):,}")
    return df


# ---------------------------------------------------------------------------
# 2. Feature engineering — exact mirror of buildCreditoFeatures in scoring.go
# ---------------------------------------------------------------------------

def build_features(df: "pd.DataFrame") -> "tuple[pd.DataFrame, pd.Series]":  # type: ignore[name-defined]
    """
    Apply the same transforms as Go's buildCreditoFeatures and filter the
    population to credit clients only.

    Returns
    -------
    X : DataFrame with exactly 4 feature columns (in FEATURES order)
    y : Series (int, 0=good, 1=bad/castigo)
    """
    import pandas as pd  # noqa: PLC0415

    df = df.copy()

    # ── Credit population filter (mirrors computeCreditoScore / estadoPagoFor) ──
    # A client is "credit" if: SALDO > 0 OR they have payment history.
    # Contado-only clients (SALDO=0 and no payment history) are excluded because
    # computeCreditoScore returns aplica=false for EstadoPago==SIN_CREDITO.
    saldo_num = df["SALDO"].apply(_coerce_float)
    has_payment_history = (
        df["FECHA_ULTIMO_PAGO"].notna()
        | (df["NUM_PAGOS"].apply(_coerce_float) > 0)
    )
    is_credit = (saldo_num > 0) | has_payment_history
    n_total = len(df)
    df = df[is_credit].reset_index(drop=True)
    print(
        f"[build_features] population: {len(df):,} credit clients "
        f"(excluded {n_total - len(df):,} contado-only)"
    )

    # ── Feature transforms (exact semantics of buildCreditoFeatures) ──
    # NULL columns → 0.0 (matches Go reading a zero decimal/int from the DB)

    por_liq = df["POR_LIQUIDAR_PCT"].apply(_coerce_float)
    saldo_frac = por_liq.apply(lambda v: clamp01(v / 100.0))
    cobertura_plan = saldo_frac.apply(lambda v: clamp01(1.0 - v))
    pct_a_tiempo_6m = df["PCT_PAGOS_A_TIEMPO"].apply(
        lambda v: clamp01(_coerce_float(v) / 100.0)
    )
    dias_atraso_prom = df["DIAS_ATRASO_PROM"].apply(_coerce_float)  # raw, not clamped

    # ── Degenerate feature check ──
    # Warn loudly if a column is entirely NULL or constant — the model cannot
    # learn from it and the resulting scorecard will be misleading.
    _check_feature_signal(
        "PCT_PAGOS_A_TIEMPO_6M", df["PCT_PAGOS_A_TIEMPO"], pct_a_tiempo_6m
    )
    _check_feature_signal(
        "DIAS_ATRASO_PROM", df["DIAS_ATRASO_PROM"], dias_atraso_prom
    )
    _check_feature_signal("SALDO_FRAC", df["POR_LIQUIDAR_PCT"], saldo_frac)
    _check_feature_signal("COBERTURA_PLAN", df["POR_LIQUIDAR_PCT"], cobertura_plan)

    X = pd.DataFrame(
        {
            "SALDO_FRAC": saldo_frac,
            "COBERTURA_PLAN": cobertura_plan,
            "PCT_PAGOS_A_TIEMPO_6M": pct_a_tiempo_6m,
            "DIAS_ATRASO_PROM": dias_atraso_prom,
        }
    )

    y = df["LABEL_CASTIGO"].astype(int)

    # Include FECHA_ULTIMO_PAGO in X so callers can use it for OOT splitting
    X["_FECHA_ULTIMO_PAGO"] = pd.to_datetime(df["FECHA_ULTIMO_PAGO"])

    label_rate = y.mean() * 100
    print(
        f"[build_features] label distribution: "
        f"{y.sum():,} bad ({label_rate:.1f}%), {(y == 0).sum():,} good"
    )

    return X, y


def _check_feature_signal(name: str, raw_col: "pd.Series", computed: "pd.Series") -> None:  # type: ignore[name-defined]
    """
    Emit a loud WARNING (not an error) if a feature column is entirely NULL or
    constant.  This surfaces the known-pending 'materializar cobranza en dev'
    issue early, before the model silently fits on zero-variance data.
    """
    null_frac = raw_col.isna().mean()
    if null_frac > 0.999:
        warnings.warn(
            f"\n{'='*70}\n"
            f"WARNING: feature '{name}' is NULL for {null_frac:.1%} of the population.\n"
            f"The cobranza signals (PCT_PAGOS_A_TIEMPO, DIAS_ATRASO_PROM) are NOT\n"
            f"yet materialized in the dev DB.  Run the analytics full refresh first.\n"
            f"Fitting with this column will produce a DEGENERATE model.\n"
            f"{'='*70}",
            stacklevel=3,
        )
        return
    std = computed.std()
    if std < 1e-9:
        warnings.warn(
            f"\n{'='*70}\n"
            f"WARNING: feature '{name}' has near-zero variance (std={std:.2e}).\n"
            f"It carries no signal and will not contribute to the scorecard.\n"
            f"{'='*70}",
            stacklevel=3,
        )


# ---------------------------------------------------------------------------
# 3. Train / evaluate helpers
# ---------------------------------------------------------------------------

def _oot_mask(X: "pd.DataFrame", cutoff: str) -> "pd.Series":  # type: ignore[name-defined]
    """
    Return a boolean Series: True for rows whose last payment date is >= cutoff.
    Rows where FECHA_ULTIMO_PAGO is NaT are placed in the training set (False).
    """
    import pandas as pd  # noqa: PLC0415

    cutoff_ts = pd.Timestamp(cutoff)
    oot = X["_FECHA_ULTIMO_PAGO"].notna() & (X["_FECHA_ULTIMO_PAGO"] >= cutoff_ts)
    return oot


def _drop_meta(X: "pd.DataFrame") -> "pd.DataFrame":  # type: ignore[name-defined]
    """Drop internal meta columns before passing X to sklearn."""
    return X.drop(columns=["_FECHA_ULTIMO_PAGO"], errors="ignore")


def fit_model(
    X_train: "pd.DataFrame",
    y_train: "pd.Series",
) -> "tuple[Any, dict[str, float], dict[str, float]]":  # type: ignore[name-defined]
    """
    Standardize features and fit LogisticRegression.

    Design choices:
    - class_weight='balanced': the label is heavily skewed (~6% positives).
      Without balancing the model would learn to predict 'good' for almost
      every client and produce a near-useless scorecard with poor KS/Gini.
      'balanced' adjusts class weights inversely proportional to frequencies
      and is the standard approach for imbalanced binary classification.
    - C=1e9 (no effective regularization): the feature set is small (4 features)
      and all are domain-motivated.  Strong regularization would shrink weights
      toward zero, which would make the score distribution collapse toward 50.
      Re-fit with C=1.0 or smaller if out-of-sample AUC degrades significantly.
    - solver='lbfgs': efficient for small datasets, supports class_weight.

    Returns
    -------
    model   : fitted LogisticRegression
    means   : {feature_name: mean} from StandardScaler
    stds    : {feature_name: std} from StandardScaler
    """
    from sklearn.linear_model import LogisticRegression  # noqa: PLC0415
    from sklearn.preprocessing import StandardScaler  # noqa: PLC0415

    feature_names = [f["name"] for f in FEATURES]
    X_feat = _drop_meta(X_train)[feature_names]

    scaler = StandardScaler()
    X_scaled = scaler.fit_transform(X_feat)

    model = LogisticRegression(
        C=1e9,              # effectively no regularization (pure MLE)
        class_weight="balanced",
        solver="lbfgs",
        max_iter=1000,
        random_state=42,
    )
    model.fit(X_scaled, y_train)

    means = dict(zip(feature_names, scaler.mean_.tolist()))
    stds = dict(zip(feature_names, scaler.scale_.tolist()))

    print(
        f"[fit_model] trained on {len(y_train):,} samples "
        f"(bad={y_train.sum():,}, good={(y_train==0).sum():,})"
    )
    print(f"[fit_model] intercept: {model.intercept_[0]:.4f}")
    for name, coef in zip(feature_names, model.coef_[0]):
        print(f"[fit_model]   {name}: weight={coef:.4f}  mean={means[name]:.4f}  std={stds[name]:.4f}")

    return model, means, stds


# ---------------------------------------------------------------------------
# 4. Validation — AUC, Gini, KS, decile table, out-of-time
# ---------------------------------------------------------------------------

def evaluate(
    model: Any,
    X: "pd.DataFrame",
    y: "pd.Series",
    means: dict[str, float],
    stds: dict[str, float],
    label: str = "test",
) -> dict[str, float]:
    """
    Compute AUC, Gini (=2*AUC-1), and KS statistic on the given split.

    The model expects standardized inputs, so we apply the training-set
    mean/std here (not a re-fit scaler) to avoid data leakage.

    Returns a dict with keys: auc, gini, ks.
    """
    import numpy as np  # noqa: PLC0415
    from sklearn.metrics import roc_auc_score, roc_curve  # noqa: PLC0415

    feature_names = [f["name"] for f in FEATURES]
    X_feat = _drop_meta(X)[feature_names]

    # Manual standardization using training statistics (prevents leakage)
    X_std = (X_feat - [means[n] for n in feature_names]) / [
        (stds[n] if stds[n] > 1e-12 else 1.0) for n in feature_names
    ]

    # model.predict_proba[:,1] = P(y=1) = P(bad) — matching our sign convention
    proba = model.predict_proba(X_std)[:, 1]

    auc = roc_auc_score(y, proba)
    gini = 2 * auc - 1

    fpr, tpr, _ = roc_curve(y, proba)
    ks = float(np.max(np.abs(tpr - fpr)))

    n_bad = int(y.sum())
    n_good = int((y == 0).sum())
    print(
        f"\n[evaluate:{label}] n={len(y):,}  bad={n_bad:,}  good={n_good:,}"
    )
    print(f"[evaluate:{label}] AUC  = {auc:.4f}")
    print(f"[evaluate:{label}] Gini = {gini:.4f}")
    print(f"[evaluate:{label}] KS   = {ks:.4f}")

    return {"auc": auc, "gini": gini, "ks": ks}


def decile_table(
    model: Any,
    X: "pd.DataFrame",
    y: "pd.Series",
    means: dict[str, float],
    stds: dict[str, float],
) -> None:
    """
    Print a decile / vintage table: bucket the test set into 10 score deciles
    (decile 1 = lowest score = highest risk, decile 10 = best) and show the
    bad-rate per decile.

    Acceptance target: worst 2 deciles (1–2) should capture ~70% of all losses;
    top 4 deciles (7–10) should have near-zero bad-rate.
    """
    import numpy as np  # noqa: PLC0415
    import pandas as pd  # noqa: PLC0415

    feature_names = [f["name"] for f in FEATURES]
    X_feat = _drop_meta(X)[feature_names]
    X_std = (X_feat - [means[n] for n in feature_names]) / [
        (stds[n] if stds[n] > 1e-12 else 1.0) for n in feature_names
    ]

    # score = 100 * (1 - p_bad) — same formula as Go's logitToScore
    proba_bad = model.predict_proba(X_std)[:, 1]
    scores = 100.0 * (1.0 - proba_bad)

    df_eval = pd.DataFrame({"score": scores, "bad": y.values})
    df_eval["decile"] = pd.qcut(scores, q=10, labels=range(1, 11), duplicates="drop")

    total_bad = int(y.sum())
    print(
        "\n[decile_table] Decile | N     | Bad  | Bad%  | Cum.Bad% "
        "(decile 1=worst, 10=best)"
    )
    print("-" * 58)
    cum_bad = 0
    for dec in range(1, 11):
        grp = df_eval[df_eval["decile"] == dec]
        n = len(grp)
        bad = int(grp["bad"].sum())
        cum_bad += bad
        bad_pct = bad / n * 100 if n > 0 else 0.0
        cum_bad_pct = cum_bad / total_bad * 100 if total_bad > 0 else 0.0
        print(
            f"  {dec:6d} | {n:5,} | {bad:4,} | {bad_pct:5.1f}% | {cum_bad_pct:5.1f}%"
        )
    print("-" * 58)


# ---------------------------------------------------------------------------
# 5. Band derivation
# ---------------------------------------------------------------------------

def derive_bands(
    model: Any,
    X_train: "pd.DataFrame",
    means: dict[str, float],
    stds: dict[str, float],
) -> dict[str, int]:
    """
    Derive score-band cutoffs from the training distribution.

    Default: 75 / 50 / 25 (documenting the reasoning below).
    We check if these defaults produce degenerate bands (i.e. a band contains
    <5% of the population) and warn if so — the caller may want to adjust.

    Score = 100*(1 - p_bad); band logic (from Go scoreToBanda):
        score >= bajo_min  → BAJO    (lowest risk)
        score >= medio_min → MEDIO
        score >= alto_min  → ALTO
        else               → CRITICO (highest risk)

    75/50/25 anchors each band to a quarter of the [0,100] range, which is
    the least biased default for a new scorecard where the score distribution
    is not yet known.  After the first real fit, inspect the score histogram
    and adjust if >80% of clients cluster in one band.
    """
    import numpy as np  # noqa: PLC0415

    feature_names = [f["name"] for f in FEATURES]
    X_feat = _drop_meta(X_train)[feature_names]
    X_std = (X_feat - [means[n] for n in feature_names]) / [
        (stds[n] if stds[n] > 1e-12 else 1.0) for n in feature_names
    ]

    proba_bad = model.predict_proba(X_std)[:, 1]
    scores = 100.0 * (1.0 - proba_bad)

    bands = dict(DEFAULT_BANDS)

    # Warn if any band captures < 5% of clients
    n = len(scores)
    pct_bajo   = (scores >= bands["bajo_min"]).mean() * 100
    pct_medio  = ((scores >= bands["medio_min"]) & (scores < bands["bajo_min"])).mean() * 100
    pct_alto   = ((scores >= bands["alto_min"]) & (scores < bands["medio_min"])).mean() * 100
    pct_critico = (scores < bands["alto_min"]).mean() * 100

    print(
        f"\n[derive_bands] score distribution (training set, n={n:,}):"
        f"\n  p10={np.percentile(scores,10):.1f}  p25={np.percentile(scores,25):.1f}"
        f"  p50={np.percentile(scores,50):.1f}  p75={np.percentile(scores,75):.1f}"
        f"  p90={np.percentile(scores,90):.1f}"
    )
    print(
        f"[derive_bands] band coverage with defaults {bands}:"
        f"\n  BAJO={pct_bajo:.1f}%  MEDIO={pct_medio:.1f}%  "
        f"ALTO={pct_alto:.1f}%  CRITICO={pct_critico:.1f}%"
    )
    for name, pct in [("BAJO", pct_bajo), ("MEDIO", pct_medio), ("ALTO", pct_alto), ("CRITICO", pct_critico)]:
        if pct < 5.0:
            warnings.warn(
                f"Band '{name}' captures only {pct:.1f}% of clients. "
                f"Consider adjusting band thresholds from defaults {bands}.",
                stacklevel=2,
            )

    return bands


# ---------------------------------------------------------------------------
# 6. Scorecard JSON emitter
# ---------------------------------------------------------------------------

def write_scorecard(
    path: str,
    version: str,
    intercept: float,
    features_with_stats: list[dict[str, Any]],
    bands: dict[str, int],
    dry_run: bool = False,
) -> None:
    """
    Emit scorecard.json in the exact schema the Go Scorecard type parses.

    Validates structural constraints before writing:
    - bands are within [0, 100]
    - bajo_min > medio_min > alto_min (Go validateBands)
    - all features have non-zero std (Go featureZ returns 0 when std==0)

    Parameters
    ----------
    path                : destination path (relative or absolute)
    version             : version string, e.g. "v1-20250616"
    intercept           : logistic regression intercept (float)
    features_with_stats : list of dicts, each with keys:
                          name, label, weight, mean, std  (in FEATURES order)
    bands               : dict with bajo_min, medio_min, alto_min (int)
    dry_run             : if True, print JSON to stdout but do not write file
    """
    # ── Structural validation ──────────────────────────────────────────────
    if not version:
        raise ValueError("scorecard version must not be empty")
    if not features_with_stats:
        raise ValueError("scorecard must have at least one feature")

    bmin = bands["bajo_min"]
    mmin = bands["medio_min"]
    amin = bands["alto_min"]
    if not (0 <= amin <= 100 and 0 <= mmin <= 100 and 0 <= bmin <= 100):
        raise ValueError(f"band thresholds out of [0,100]: {bands}")
    if not (bmin > mmin > amin):
        raise ValueError(
            f"bands must satisfy bajo_min > medio_min > alto_min; got {bands}"
        )

    for feat in features_with_stats:
        if feat.get("std", 0.0) == 0.0:
            warnings.warn(
                f"Feature '{feat['name']}' has std=0; Go featureZ will return 0 "
                f"and the feature will carry no weight in scoring.",
                stacklevel=2,
            )

    # ── Build the output dict ──────────────────────────────────────────────
    doc: dict[str, Any] = {
        "version": version,
        "intercept": round(intercept, 6),
        "features": [
            {
                "name": f["name"],
                "label": f["label"],
                "weight": round(f["weight"], 6),
                "mean": round(f["mean"], 6),
                "std": round(f["std"], 6),
            }
            for f in features_with_stats
        ],
        "bands": {
            "bajo_min": bmin,
            "medio_min": mmin,
            "alto_min": amin,
        },
    }

    json_str = json.dumps(doc, indent=2, ensure_ascii=False) + "\n"

    # ── Smoke-check: re-parse what we are about to write ──────────────────
    reparsed = json.loads(json_str)
    assert reparsed["version"] == version, "round-trip version mismatch"
    assert len(reparsed["features"]) == len(features_with_stats), "round-trip feature count mismatch"

    if dry_run:
        print("\n[write_scorecard] DRY RUN — scorecard NOT written. Would write:")
        print(json_str)
        return

    with open(path, "w", encoding="utf-8") as fh:
        fh.write(json_str)
    print(f"\n[write_scorecard] written → {path}")


# ---------------------------------------------------------------------------
# Main pipeline
# ---------------------------------------------------------------------------

def _connect() -> Any:
    """
    Open a read-only Firebird connection using firebird-driver.

    Credentials are read from environment variables with sensible fallbacks.
    The caller must export DB credentials before running this script:
        set -a; . ./.env; set +a
    """
    import firebird.driver as fdb  # noqa: PLC0415

    host = os.environ.get("FB_HOST", "localhost")
    port = int(os.environ.get("FB_PORT", "3050"))
    database = os.environ.get("FB_DATABASE", "/firebird/data/MUEBLERA.FDB")
    user = os.environ.get("FB_USER", "SYSDBA")
    password = os.environ.get("FB_PASSWORD", "masterkey")

    dsn = f"{host}/{port}:{database}"
    print(f"[connect] DSN={dsn}  user={user}")

    conn = fdb.connect(
        database=dsn,
        user=user,
        password=password,
        charset="UTF8",
    )
    return conn


def run_pipeline(version: str, dry_run: bool) -> None:
    """
    Full training pipeline:
      1. Connect + export data
      2. Build features
      3. Split train/test (random) + out-of-time split
      4. Fit model on train
      5. Evaluate on test + OOT
      6. Print decile table on test
      7. Derive bands
      8. Emit scorecard.json (unless dry_run)
    """
    from sklearn.model_selection import train_test_split  # noqa: PLC0415

    # 1. Load
    conn = _connect()
    try:
        df_raw = load_dataset(conn)
    finally:
        conn.close()

    # 2. Features
    X_all, y_all = build_features(df_raw)

    # 3. Splits
    # Out-of-time split: clients with FECHA_ULTIMO_PAGO >= OOT_CUTOFF_DATE
    oot_mask = _oot_mask(X_all, OOT_CUTOFF_DATE)
    n_oot = oot_mask.sum()
    print(
        f"\n[splits] OOT cutoff={OOT_CUTOFF_DATE}  "
        f"OOT n={n_oot:,}  in-time n={(~oot_mask).sum():,}"
    )

    X_intime = X_all[~oot_mask]
    y_intime = y_all[~oot_mask]
    X_oot    = X_all[oot_mask]
    y_oot    = y_all[oot_mask]

    if n_oot < 50:
        warnings.warn(
            f"OOT holdout has only {n_oot} samples (cutoff={OOT_CUTOFF_DATE}). "
            "OOT metrics will be unreliable. Consider adjusting OOT_CUTOFF_DATE.",
            stacklevel=1,
        )

    # Random train/test split on the in-time population
    X_train, X_test, y_train, y_test = train_test_split(
        X_intime, y_intime, test_size=0.3, stratify=y_intime, random_state=42
    )
    print(
        f"[splits] train={len(y_train):,}  test={len(y_test):,}"
    )

    # 4. Fit
    model, means, stds = fit_model(X_train, y_train)

    # 5. Evaluate
    metrics_test = evaluate(model, X_test, y_test, means, stds, label="test")
    if len(y_oot) >= 50:
        evaluate(model, X_oot, y_oot, means, stds, label="OOT")
    else:
        print("\n[evaluate:OOT] skipped — insufficient OOT samples")

    # 6. Decile table on test set
    decile_table(model, X_test, y_test, means, stds)

    # 7. Bands
    bands = derive_bands(model, X_train, means, stds)

    # 8. Assemble features_with_stats and emit
    feature_names = [f["name"] for f in FEATURES]
    coefs = model.coef_[0].tolist()
    features_with_stats = [
        {
            "name": FEATURES[i]["name"],
            "label": FEATURES[i]["label"],
            "weight": coefs[i],
            "mean": means[feature_names[i]],
            "std": stds[feature_names[i]],
        }
        for i in range(len(FEATURES))
    ]

    # Summary
    print(
        f"\n{'='*60}\n"
        f"SCORECARD SUMMARY  version={version}\n"
        f"{'='*60}\n"
        f"Test  AUC={metrics_test['auc']:.4f}  "
        f"Gini={metrics_test['gini']:.4f}  "
        f"KS={metrics_test['ks']:.4f}\n"
        f"Intercept: {model.intercept_[0]:.4f}\n"
    )
    for feat in features_with_stats:
        print(
            f"  {feat['name']:24s}  w={feat['weight']:+.4f}  "
            f"mean={feat['mean']:.4f}  std={feat['std']:.4f}"
        )

    write_scorecard(
        path=SCORECARD_JSON_PATH,
        version=version,
        intercept=model.intercept_[0],
        features_with_stats=features_with_stats,
        bands=bands,
        dry_run=dry_run,
    )


# ---------------------------------------------------------------------------
# CLI entry point
# ---------------------------------------------------------------------------

def _default_version() -> str:
    return f"v1-{date.today().strftime('%Y%m%d')}"


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Fit the credit-risk scorecard and write scorecard.json."
    )
    parser.add_argument(
        "--version",
        default=_default_version(),
        help="Scorecard version string, e.g. v1-20250616 (default: today's date).",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Print metrics and the would-be scorecard.json without writing it.",
    )
    args = parser.parse_args()

    warnings.simplefilter("always")

    try:
        run_pipeline(version=args.version, dry_run=args.dry_run)
    except ImportError as exc:
        print(
            f"\nERROR: missing dependency — {exc}\n"
            "Run: pip install -r analysis/creditscorecard/requirements.txt",
            file=sys.stderr,
        )
        sys.exit(1)


if __name__ == "__main__":
    main()
