"""
eda2.py — client-level analysis (castigo/condonación link only at CLIENTE_ID,
not DOCTO_CC_ID, per eda.py finding). Fixes the timing + bad-rate views and
checks whether real abonos link to cargos.
"""
from __future__ import annotations

import os

import pandas as pd

DATA = os.path.join(os.path.dirname(__file__), ".data")
ABONO = (87327, 155, 11)
CASTIGO = (27968, 27967)
COND = (27969,)


def load():
    cargos = pd.read_csv(os.path.join(DATA, "cargos.csv"), parse_dates=["FECHA_CARGO"])
    abonos = pd.read_csv(os.path.join(DATA, "abonos.csv"), parse_dates=["FECHA"])
    for df, c in ((cargos, "FECHA_CARGO"), (abonos, "FECHA")):
        if isinstance(df[c].dtype, pd.DatetimeTZDtype):
            df[c] = df[c].dt.tz_convert("UTC").dt.tz_localize(None)
    return cargos, abonos


def sec(t):
    print(f"\n{'='*72}\n{t}\n{'='*72}")


def main():
    cargos, abonos = load()

    # Link diagnostic: do REAL abonos match a cargo DOCTO_CC_ID?
    sec("0. ¿Los abonos reales enlazan a cargos por DOCTO_CC_ID?")
    cargo_ids = set(cargos["DOCTO_CC_ID"])
    for label, ids in (("real", ABONO), ("castigo", CASTIGO), ("cond", COND)):
        sub = abonos[abonos["CONCEPTO_CC_ID"].isin(ids)]
        match = sub["DOCTO_CC_ID"].isin(cargo_ids).mean() * 100 if len(sub) else 0
        print(f"  {label:8s}: {len(sub):>9,} filas, {match:5.1f}% enlazan a un cargo")

    # Client-level first cargo + first castigo
    first_cargo = cargos.groupby("CLIENTE_ID")["FECHA_CARGO"].min().rename("first_cargo")
    last_cargo = cargos.groupby("CLIENTE_ID")["FECHA_CARGO"].max().rename("last_cargo")
    cast = abonos[abonos["CONCEPTO_CC_ID"].isin(CASTIGO)]
    first_castigo = cast.groupby("CLIENTE_ID")["FECHA"].min().rename("first_castigo")

    cli = pd.concat([first_cargo, last_cargo, first_castigo], axis=1)
    cli["bad"] = cli["first_castigo"].notna().astype(int)
    n_cli = len(cli)
    print(f"\nclientes con cargo: {n_cli:,}  | castigados: {cli['bad'].sum():,} "
          f"({cli['bad'].mean()*100:.1f}%)")

    # Castigo timing relative to FIRST and LAST cargo
    sec("1. Timing del castigo (meses desde primer / último cargo)")
    bad = cli[cli["bad"] == 1].copy()
    bad["mob_first"] = (bad["first_castigo"] - bad["first_cargo"]).dt.days / 30.44
    bad["mob_last"] = (bad["first_castigo"] - bad["last_cargo"]).dt.days / 30.44
    print("meses desde PRIMER cargo al castigo:")
    print(bad["mob_first"].describe(percentiles=[.1, .25, .5, .75, .9]).to_string())
    print("\nmeses desde ÚLTIMO cargo al castigo:")
    print(bad["mob_last"].describe(percentiles=[.1, .25, .5, .75, .9]).to_string())

    # Bad rate by first-cargo cohort year (maturity)
    sec("2. Bad rate por cohorte (año del PRIMER cargo del cliente)")
    cli["cohort_y"] = cli["first_cargo"].dt.year
    by = cli.groupby("cohort_y").agg(n=("bad", "size"), bad=("bad", "sum"))
    by["bad_rate_%"] = (by["bad"] / by["n"] * 100).round(1)
    print(by[by["n"] >= 50].to_string())

    # Días sin pagar at "now" for active owers — to calibrate the delinquency threshold
    sec("3. Última actividad de pago real (días desde último abono real a la fecha tope)")
    real = abonos[(abonos["CONCEPTO_CC_ID"].isin(ABONO)) & (abonos["CANCELADO"] == "N")
                  & (abonos["APLICADO"] == "S")]
    last_pay = real.groupby("CLIENTE_ID")["FECHA"].max()
    asof = abonos["FECHA"].max()
    dias = (asof - last_pay).dt.days
    j = cli.join(dias.rename("dias_sin_pagar"))
    print("días desde último abono real (todos los clientes con cargo):")
    print(j["dias_sin_pagar"].describe(percentiles=[.5, .75, .9, .95]).to_string())
    print("\npor estatus bad/good (mediana días sin pagar):")
    print(j.groupby("bad")["dias_sin_pagar"].median().to_string())


if __name__ == "__main__":
    main()
