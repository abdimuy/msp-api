"""
eda.py — exploratory analysis to fix the point-in-time scorecard design params.

Answers (with real data) the open questions from the methodology research:
  - Vintage spread of cargos (FECHA_CARGO) and maturity.
  - Plan length (NUM_PAGOS, PRECIO_TOTAL) and payment cadence (route collection).
  - Castigo timing: months-on-book (MOB) when Mal Cliente/Fugas hits → sets the
    performance-window length and the observation point.
  - Bad rate by vintage maturity; condonación prevalence.

Run:  source .env; analysis/creditscorecard/.venv/bin/python analysis/creditscorecard/eda.py
Reads the CSVs from cmd/analytics-export-creditdata (analysis/creditscorecard/.data).
"""
from __future__ import annotations

import os

import numpy as np
import pandas as pd

DATA = os.path.join(os.path.dirname(__file__), ".data")

ABONO_CONCEPTS = (87327, 155, 11)   # real payments
CASTIGO_CONCEPTS = (27968, 27967)   # Mal Cliente, Fugas
CONDONACION_CONCEPTS = (27969,)


def load() -> tuple[pd.DataFrame, pd.DataFrame]:
    cargos = pd.read_csv(
        os.path.join(DATA, "cargos.csv"),
        parse_dates=["FECHA_CARGO"],
    )
    abonos = pd.read_csv(
        os.path.join(DATA, "abonos.csv"),
        parse_dates=["FECHA"],
    )
    # Strip tz so all comparisons are naive.
    for df, col in ((cargos, "FECHA_CARGO"), (abonos, "FECHA")):
        if isinstance(df[col].dtype, pd.DatetimeTZDtype):
            df[col] = df[col].dt.tz_convert("UTC").dt.tz_localize(None)
    return cargos, abonos


def section(title: str) -> None:
    print(f"\n{'='*72}\n{title}\n{'='*72}")


def main() -> None:
    cargos, abonos = load()
    print(f"cargos={len(cargos):,}  abonos={len(abonos):,}")

    # ── Vintages ────────────────────────────────────────────────────────────
    section("1. Vintages (FECHA_CARGO)")
    cargos["vint_y"] = cargos["FECHA_CARGO"].dt.year
    print(cargos["vint_y"].value_counts().sort_index().to_string())
    print(f"\nrango FECHA_CARGO: {cargos['FECHA_CARGO'].min()} … {cargos['FECHA_CARGO'].max()}")
    print(f"FECHA abono max (≈ hoy en datos): {abonos['FECHA'].max()}")

    # ── Plan length ─────────────────────────────────────────────────────────
    section("2. Plan length (NUM_PAGOS, PRECIO_TOTAL)")
    print("NUM_PAGOS (pagos esperados) describe:")
    print(cargos["NUM_PAGOS"].describe(percentiles=[.1, .25, .5, .75, .9]).to_string())
    print("\nPRECIO_TOTAL describe:")
    print(cargos["PRECIO_TOTAL"].describe(percentiles=[.25, .5, .75, .9]).to_string())

    # ── Payment cadence (route collection) ──────────────────────────────────
    section("3. Payment cadence — gaps between real abonos (días)")
    real = abonos[
        (abonos["CONCEPTO_CC_ID"].isin(ABONO_CONCEPTS))
        & (abonos["CANCELADO"] == "N")
        & (abonos["APLICADO"] == "S")
    ].sort_values(["CLIENTE_ID", "FECHA"])
    real["gap"] = real.groupby("CLIENTE_ID")["FECHA"].diff().dt.days
    g = real["gap"].dropna()
    g = g[(g >= 0) & (g <= 365)]
    print(g.describe(percentiles=[.25, .5, .75, .9]).to_string())
    print(f"\nmoda aprox (gap más común, días): {g.round().mode().head(3).tolist()}")

    # ── Castigo timing (months on book) ─────────────────────────────────────
    section("4. Castigo timing — MOB (meses desde FECHA_CARGO hasta el castigo)")
    cast = abonos[abonos["CONCEPTO_CC_ID"].isin(CASTIGO_CONCEPTS)].copy()
    # First castigo per cargo
    cast = cast.sort_values("FECHA").groupby("DOCTO_CC_ID", as_index=False).first()
    cast = cast.merge(
        cargos[["DOCTO_CC_ID", "FECHA_CARGO"]], on="DOCTO_CC_ID", how="inner"
    )
    cast["mob"] = (cast["FECHA"] - cast["FECHA_CARGO"]).dt.days / 30.44
    print(f"cargos castigados (con FECHA_CARGO): {len(cast):,}")
    print(cast["mob"].describe(percentiles=[.1, .25, .5, .75, .9]).to_string())

    # ── Bad rate by vintage maturity ────────────────────────────────────────
    section("5. Bad rate (castigo) por vintage de cargo")
    castigo_cargos = set(cast["DOCTO_CC_ID"])
    cargos["bad"] = cargos["DOCTO_CC_ID"].isin(castigo_cargos).astype(int)
    by_v = cargos.groupby("vint_y").agg(
        n=("DOCTO_CC_ID", "size"), bad=("bad", "sum")
    )
    by_v["bad_rate_%"] = (by_v["bad"] / by_v["n"] * 100).round(1)
    print(by_v.to_string())

    # ── Condonación prevalence ──────────────────────────────────────────────
    section("6. Condonación (27969) — prevalencia por cargo")
    cond_cargos = set(abonos.loc[abonos["CONCEPTO_CC_ID"].isin(CONDONACION_CONCEPTS), "DOCTO_CC_ID"])
    cargos["cond"] = cargos["DOCTO_CC_ID"].isin(cond_cargos).astype(int)
    print(f"cargos con condonación: {cargos['cond'].sum():,} ({cargos['cond'].mean()*100:.1f}%)")
    print("\ncruce condonación × castigo (cargos):")
    print(pd.crosstab(cargos["cond"], cargos["bad"], margins=True).to_string())


if __name__ == "__main__":
    main()
