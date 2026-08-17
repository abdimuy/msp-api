#!/usr/bin/env bash
#
# db-test-dump-text.sh — vuelca a stdout TODO el texto de una base Firebird,
# una línea por valor distinto, con la forma:
#
#     TABLA.COLUMNA::<valor>
#
# Recorre todas las tablas de usuario con filas y todas sus columnas CHAR,
# VARCHAR y BLOB SUB_TYPE TEXT. Es la entrada de scripts/db-test-pii-scan.py.
#
# Vuelca la base entera a propósito: buscar datos personales sólo en las
# columnas que se llaman NOMBRE o TELEFONO es cómo se pasan por alto
# LISTAS_ATRIBUTOS.VALOR_DESPLEGADO y MOTIVOS_CANCELACION.MOTIVO, que traían
# nombres de clientes reales.
#
# Uso:
#   ./scripts/db-test-dump-text.sh /firebird/data/CATVERIF.FDB > pii-corpus.txt

set -euo pipefail

DB="${1:?uso: db-test-dump-text.sh <ruta de la base dentro del contenedor>}"
CONTAINER="${CONTAINER:-mueblera-firebird}"
FBUSER="${FBUSER:-sysdba}"
FBPASS="${FBPASS:-masterkey}"
ISQL=/usr/local/firebird/bin/isql

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

run_sql() {
  docker cp "$1" "$CONTAINER:/tmp/dump-query.sql" >/dev/null
  docker exec -i "$CONTAINER" "$ISQL" -user "$FBUSER" -password "$FBPASS" \
    -i /tmp/dump-query.sql "$DB"
}

# Paso 1 — tablas de usuario con al menos una fila.
cat > "$TMP/tables.sql" <<'SQL'
SET HEADING OFF;
SELECT TRIM(r.RDB$RELATION_NAME) FROM RDB$RELATIONS r
 WHERE COALESCE(r.RDB$SYSTEM_FLAG,0)=0 AND r.RDB$VIEW_BLR IS NULL
 ORDER BY 1;
SQL
run_sql "$TMP/tables.sql" | tr -d ' \r' | grep -v '^$' > "$TMP/tables.txt"

{
  echo "SET HEADING OFF;"
  while read -r t; do
    [ -n "$t" ] || continue
    printf "SELECT '%s='||COUNT(*) FROM %s;\n" "$t" "$t"
  done < "$TMP/tables.txt"
} > "$TMP/counts.sql"
run_sql "$TMP/counts.sql" | tr -d ' \r' | grep -v '^$' > "$TMP/counts.txt"

# Paso 2 — columnas de texto (14 CHAR, 37 VARCHAR, 40 CSTRING, 261/1 BLOB TEXT).
cat > "$TMP/cols.sql" <<'SQL'
SET HEADING OFF;
SELECT TRIM(rf.RDB$RELATION_NAME)||'|'||TRIM(rf.RDB$FIELD_NAME)||'|'||f.RDB$FIELD_TYPE
  FROM RDB$RELATION_FIELDS rf
  JOIN RDB$FIELDS f ON f.RDB$FIELD_NAME = rf.RDB$FIELD_SOURCE
  JOIN RDB$RELATIONS r ON r.RDB$RELATION_NAME = rf.RDB$RELATION_NAME
 WHERE COALESCE(r.RDB$SYSTEM_FLAG,0)=0 AND r.RDB$VIEW_BLR IS NULL
   AND (f.RDB$FIELD_TYPE IN (14,37,40)
        OR (f.RDB$FIELD_TYPE=261 AND f.RDB$FIELD_SUB_TYPE=1));
SQL
run_sql "$TMP/cols.sql" | tr -d ' \r' | grep -v '^$' > "$TMP/cols.txt"

# Paso 3 — un SELECT DISTINCT por columna de tabla no vacía.
python3 - "$TMP/counts.txt" "$TMP/cols.txt" > "$TMP/dump.sql" <<'PY'
import sys
counts = {}
for line in open(sys.argv[1]):
    line = line.strip()
    if "=" in line:
        t, n = line.rsplit("=", 1)
        try:
            counts[t] = int(n)
        except ValueError:
            pass
print("SET HEADING OFF;")
print("SET BLOBDISPLAY ON;")
for line in open(sys.argv[2]):
    p = line.strip().split("|")
    if len(p) != 3:
        continue
    t, c, ft = p[0], p[1], int(p[2])
    if counts.get(t, 0) == 0:
        continue
    # Los BLOB se truncan: interesa detectar el dato, no exportarlo completo.
    expr = (f'CAST(SUBSTRING("{c}" FROM 1 FOR 400) AS VARCHAR(400))' if ft == 261
            else f'CAST("{c}" AS VARCHAR(1000))')
    print(f"SELECT DISTINCT '{t}.{c}::' || {expr} FROM {t} "
          f"WHERE {expr} IS NOT NULL AND CHAR_LENGTH(TRIM({expr}))>0;")
PY

run_sql "$TMP/dump.sql"
