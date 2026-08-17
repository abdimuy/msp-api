#!/usr/bin/env python3
"""db-test-pii-scan.py — busca datos personales residuales en un volcado de texto
de la base de pruebas.

No adivina: cruza el volcado contra el padrón REAL de la base de desarrollo.
Si un nombre de cliente de verdad sobrevivió en cualquier columna de texto de
cualquier tabla, aquí sale, con el nombre de la columna donde quedó.

Uso (los dos archivos los produce scripts/make-test-db.sh):

    python3 scripts/db-test-pii-scan.py <corpus.txt> <realnames.txt>

  corpus.txt     una línea por valor: "TABLA.COLUMNA::<valor>"
  realnames.txt  un nombre real por línea (CLIENTES.NOMBRE de la base viva)

Salida: las columnas donde aparece algún nombre real, y patrones de correo,
teléfono, RFC y CURP agrupados por columna para revisión manual.

Sale con código 1 si encuentra un nombre real que no esté en EXCEPCIONES. Los
patrones NO hacen fallar la corrida —los sintéticos también son correos y
teléfonos— pero se imprimen para que una persona los mire.

Límite conocido: sólo detecta nombres que estén en CLIENTES de la base viva.
Un nombre de empleado, o el de un cliente ya borrado, no lo va a ver. Por eso
la lista de columnas con frases de 2 a 5 palabras se imprime completa: es la
parte que exige ojo humano.
"""

import collections
import re
import sys

PATRONES = {
    "correo": re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"),
    "telefono_10": re.compile(r"(?<!\d)\d{10}(?!\d)"),
    "telefono_fmt": re.compile(r"\(\d{3}\)\s?\d{3}-\d{4}"),
    "curp": re.compile(r"\b[A-Z]{4}\d{6}[HM][A-Z]{5}[A-Z0-9]\d\b"),
    "rfc": re.compile(r"\b[A-ZÑ&]{3,4}\d{6}[A-Z0-9]{3}\b"),
}

FRASE = re.compile(
    r"^[A-ZÁÉÍÓÚÑÜ][A-ZÁÉÍÓÚÑÜa-záéíóúñü]+(?: [A-ZÁÉÍÓÚÑÜ][A-ZÁÉÍÓÚÑÜa-záéíóúñü]+){1,4}$"
)

# Aciertos conocidos que NO son datos personales. Cada línea necesita
# justificación: esta lista es la única forma de que el escaneo mienta, así que
# se revisa como se revisa un `nolint`.
#
# Formato: (TABLA.COLUMNA, valor exacto).
EXCEPCIONES = {
    # Catálogo de tipos de cliente de Microsip. Coincide con el nombre del
    # "cliente" genérico de mostrador que hay en el padrón. No es de nadie.
    ("TIPOS_CLIENTES.NOMBRE", "PUBLICO EN GENERAL"),
}


def cargar_nombres(ruta):
    """Devuelve los nombres reales normalizados, sólo los distintivos."""
    nombres = set()
    for linea in open(ruta, encoding="latin-1"):
        n = " ".join(linea.strip().upper().split())
        if len(n) >= 12 and len(n.split()) >= 2:
            nombres.add(n)
    return nombres


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        return 2
    corpus_path, names_path = sys.argv[1], sys.argv[2]

    nombres = cargar_nombres(names_path)
    # Índice por los dos primeros tokens: convierte la búsqueda de 40k nombres
    # dentro de 60k valores en una consulta de diccionario por bigrama.
    idx = collections.defaultdict(list)
    for n in nombres:
        t = n.split()
        idx[(t[0], t[1])].append(n)

    residuo = collections.defaultdict(list)
    patrones = collections.defaultdict(lambda: collections.defaultdict(list))
    frases = collections.defaultdict(set)

    for linea in open(corpus_path, encoding="latin-1"):
        linea = linea.rstrip()
        if "::" not in linea:
            continue
        col, val = linea.split("::", 1)
        col, val = col.strip(), val.strip()
        if not val:
            continue

        norm = " ".join(val.upper().split())
        toks = re.split(r"[^A-ZÁÉÍÓÚÑÜ]+", norm)
        for i in range(len(toks) - 1):
            for cand in idx.get((toks[i], toks[i + 1]), ()):
                if cand in norm and (col, cand) not in EXCEPCIONES:
                    residuo[col].append(cand)

        for nombre, pat in PATRONES.items():
            for m in pat.finditer(val):
                if len(patrones[nombre][col]) < 3:
                    patrones[nombre][col].append(m.group(0))

        if FRASE.match(val):
            frases[col].add(val)

    print("=" * 72)
    print("1. NOMBRES REALES DEL PADRÓN ENCONTRADOS EN LA BASE DE PRUEBAS")
    print("=" * 72)
    if not residuo:
        print(f"  ninguno ({len(EXCEPCIONES)} excepción(es) declarada(s) en el script)")
    for col, vals in sorted(residuo.items(), key=lambda x: -len(x[1])):
        print(f"  {col}: {len(vals)} ocurrencias, p.ej. {sorted(set(vals))[:3]}")

    print()
    print("=" * 72)
    print("2. PATRONES (correo / teléfono / RFC / CURP) — revisión manual")
    print("=" * 72)
    for nombre in PATRONES:
        cols = patrones[nombre]
        if not cols:
            continue
        print(f"  [{nombre}]")
        for col, ej in sorted(cols.items()):
            print(f"     {col}: {ej}")

    print()
    print("=" * 72)
    print("3. COLUMNAS CON FRASES DE 2 A 5 PALABRAS — revisión manual")
    print("=" * 72)
    for col in sorted(frases, key=lambda c: -len(frases[c])):
        vals = sorted(frases[col])
        print(f"  {col} ({len(vals)}): {vals[:4]}")

    return 1 if residuo else 0


if __name__ == "__main__":
    sys.exit(main())
