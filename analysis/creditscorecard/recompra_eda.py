"""
recompra_eda.py — patrones de RECOMPRA en nuestros datos, para aterrizar el
diseño del score de propensión a recompra (ventana N, target, base de repetición).

Cada cargo en cargos.csv (MSP_SALDOS_VENTAS, FECHA_CARGO) ≈ una venta a crédito.
Los gaps entre cargos consecutivos de un cliente = su cadencia de recompra.
"""
from __future__ import annotations

import os

import numpy as np
import pandas as pd

DATA = os.path.join(os.path.dirname(__file__), ".data")


def sec(t):
    print(f"\n{'='*70}\n{t}\n{'='*70}")


def main():
    cargos = pd.read_csv(os.path.join(DATA, "cargos.csv"), parse_dates=["FECHA_CARGO"])
    if isinstance(cargos["FECHA_CARGO"].dtype, pd.DatetimeTZDtype):
        cargos["FECHA_CARGO"] = cargos["FECHA_CARGO"].dt.tz_convert("UTC").dt.tz_localize(None)
    cargos = cargos[cargos["CARGO_CANCELADO"] == "N"].sort_values(["CLIENTE_ID", "FECHA_CARGO"])
    print(f"cargos (no cancelados): {len(cargos):,}  clientes: {cargos['CLIENTE_ID'].nunique():,}")
    print(f"rango: {cargos['FECHA_CARGO'].min().date()} … {cargos['FECHA_CARGO'].max().date()}")

    # ── Compras por cliente (tasa de repetición) ─────────────────────────────
    sec("1. Cargos (compras a crédito) por cliente")
    per = cargos.groupby("CLIENTE_ID").size()
    print(per.describe(percentiles=[.5, .75, .9, .95]).to_string())
    for k in (1, 2, 3, 5):
        share = (per >= k).mean() * 100
        print(f"  clientes con >= {k} compras: {(per>=k).sum():,} ({share:.1f}%)")
    print(f"  >> tasa de RECOMPRA (>=2 compras): {(per>=2).mean()*100:.1f}%")

    # ── Intervalo entre compras consecutivas ─────────────────────────────────
    sec("2. Intervalo entre compras consecutivas (meses)")
    cargos["gap_d"] = cargos.groupby("CLIENTE_ID")["FECHA_CARGO"].diff().dt.days
    g = cargos["gap_d"].dropna()
    g = g[g >= 0]
    gm = g / 30.44
    print(gm.describe(percentiles=[.1, .25, .5, .75, .9]).to_string())
    for n in (6, 12, 18, 24, 36):
        print(f"  recompras dentro de {n:>2} meses: {(gm<=n).mean()*100:.1f}% de los gaps")

    # ── Tiempo hasta la 2a compra (para los que recompraron) ─────────────────
    sec("3. Meses hasta la SEGUNDA compra (cohorte que recompró)")
    firsts = cargos.groupby("CLIENTE_ID")["FECHA_CARGO"].nth(0)
    seconds = cargos.groupby("CLIENTE_ID")["FECHA_CARGO"].nth(1)
    t2 = ((seconds - firsts).dt.days / 30.44).dropna()
    print(t2.describe(percentiles=[.25, .5, .75, .9]).to_string())

    # ── Recompra: ¿liquidados vs con saldo? (proxy) ──────────────────────────
    # Aproximación: ¿el cliente compró otra vez teniendo ya un cargo previo aún
    # "joven"? Mejor pregunta servible: de los que tienen >=2 compras, ¿la 2a
    # llegó antes o después de ~su plazo? (No tenemos saldo histórico aquí; lo
    # refinará el harness). Reportamos la fracción de recompras "rápidas" (<6m)
    # que sugieren compra estando aún pagando.
    sec("4. Recompras rápidas (<6m del cargo previo) — posible compra con saldo vivo")
    rapidas = (gm < 6).sum()
    print(f"  gaps < 6 meses: {rapidas:,} ({(gm<6).mean()*100:.1f}% de los gaps)")
    print(f"  gaps 6-24 meses: {((gm>=6)&(gm<=24)).mean()*100:.1f}%   > 24 meses: {(gm>24).mean()*100:.1f}%")

    # ── Estacionalidad (mes del año del cargo) ───────────────────────────────
    sec("5. Estacionalidad — cargos por mes del año (todo el historial)")
    bym = cargos["FECHA_CARGO"].dt.month.value_counts().sort_index()
    tot = bym.sum()
    for m, n in bym.items():
        bar = "#" * int(n / tot * 200)
        print(f"  mes {m:>2}: {n:>7,} ({n/tot*100:4.1f}%) {bar}")


if __name__ == "__main__":
    main()
