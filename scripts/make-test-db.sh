#!/usr/bin/env bash
#
# make-test-db.sh — genera la base de pruebas SIN DATOS PERSONALES a partir de
# la base de desarrollo, y produce el material con el que se demuestra que no
# quedaron datos personales.
#
# Lo que hace, en orden:
#   1. respalda la base viva (gbak -b; no hay que detener nada)
#   2. restaura omitiendo los datos de scripts/db-test-skip-tables.txt
#   3. sustituye por datos sintéticos lo que sobrevive (db-test-anonymize.sql)
#   4. respalda y comprime el artefacto
#   5. lo restaura a una base APARTE y vuelca de ahí todo el texto, para
#      auditarlo con db-test-pii-scan.py
#
# NUNCA escribe sobre la base de desarrollo ni la vuelve a leer después del
# paso 1. La única entrada es el respaldo.
#
# Uso:
#   ./scripts/make-test-db.sh
#   SRC_DB=/firebird/data/MUEBLERA.FDB ./scripts/make-test-db.sh
#
# Al terminar deja en el directorio actual:
#   msp-test-db.fbk.gz   el artefacto
#   pii-corpus.txt       todo el texto de la base restaurada, "TABLA.COL::valor"
#   pii-padron.txt       los nombres reales de CLIENTES (SÓLO LOCAL — no subir)
#
# Después:
#   python3 scripts/db-test-pii-scan.py pii-corpus.txt pii-padron.txt
#
# ⚠ El artefacto no se publica desde aquí. Quién lo publica y dónde lo decide
#   quien sea dueño del repositorio.

set -euo pipefail

CONTAINER="${CONTAINER:-mueblera-firebird}"
SRC_DB="${SRC_DB:-/firebird/data/MUEBLERA.FDB}"
FULL_FBK="${FULL_FBK:-/firebird/data/full.fbk}"
WORK_DB="${WORK_DB:-/firebird/data/CAT.FDB}"
VERIFY_DB="${VERIFY_DB:-/firebird/data/CATVERIF.FDB}"
OUT="${OUT:-msp-test-db.fbk.gz}"
FBUSER="${FBUSER:-sysdba}"
FBPASS="${FBPASS:-masterkey}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GBAK=/usr/local/firebird/bin/gbak
ISQL=/usr/local/firebird/bin/isql

fb() { docker exec -i "$CONTAINER" "$@"; }

if [ "$SRC_DB" = "$WORK_DB" ] || [ "$SRC_DB" = "$VERIFY_DB" ]; then
  echo "❌ WORK_DB/VERIFY_DB no pueden ser la base de origen" >&2
  exit 1
fi

# REUSE_BACKUP=1 salta el respaldo y reutiliza $FULL_FBK. Sólo para iterar
# sobre la lista de omisión o el script de sustitución sin esperar cinco
# minutos por un respaldo que no cambió.
if [ "${REUSE_BACKUP:-0}" = "1" ] && fb test -f "$FULL_FBK"; then
  echo "▸ 1/5 reutilizando el respaldo $FULL_FBK (REUSE_BACKUP=1)"
else
  echo "▸ 1/5 respaldo de $SRC_DB"
  fb rm -f "$FULL_FBK"
  fb "$GBAK" -b -user "$FBUSER" -password "$FBPASS" "$SRC_DB" "$FULL_FBK"
fi

echo "▸ 2/5 restauración omitiendo los datos de db-test-skip-tables.txt"
fb rm -f "$WORK_DB"
fb "$GBAK" -c \
  -skip_data "$(cat "$SCRIPT_DIR/db-test-skip-tables.txt")" \
  -user "$FBUSER" -password "$FBPASS" \
  "$FULL_FBK" "$WORK_DB"

echo "▸ 3/5 sustitución por datos sintéticos"
docker cp "$SCRIPT_DIR/db-test-anonymize.sql" "$CONTAINER:/tmp/db-test-anonymize.sql" >/dev/null
fb "$ISQL" -user "$FBUSER" -password "$FBPASS" -i /tmp/db-test-anonymize.sql "$WORK_DB"

echo "▸ 4/5 respaldo y compresión del artefacto"
fb rm -f /firebird/data/msp-test-db.fbk /firebird/data/msp-test-db.fbk.gz
fb "$GBAK" -b -user "$FBUSER" -password "$FBPASS" "$WORK_DB" /firebird/data/msp-test-db.fbk
fb gzip -9 /firebird/data/msp-test-db.fbk
docker cp "$CONTAINER:/firebird/data/msp-test-db.fbk.gz" "$OUT" >/dev/null

# Si aquí sale "database already exists" o "database might be in use" pese al
# rm, el servidor conserva una conexión contra $VERIFY_DB — típicamente una
# corrida de `go test` que se interrumpió. El archivo ya no está pero la
# attachment sigue viva y gbak no puede crear encima. Sale más barato usar otro
# nombre que reiniciar el contenedor:
#
#   VERIFY_DB=/firebird/data/CATVERIF2.FDB ./scripts/make-test-db.sh
echo "▸ 5/5 restauración del artefacto a $VERIFY_DB y volcado de texto"
fb rm -f "$VERIFY_DB"
fb sh -c "gunzip -c /firebird/data/msp-test-db.fbk.gz > /tmp/verify.fbk"
fb "$GBAK" -c -user "$FBUSER" -password "$FBPASS" /tmp/verify.fbk "$VERIFY_DB"

# El volcado sale de la base RESTAURADA, no de la que se generó: es el archivo
# que se repartiría, y es el que hay que auditar.
"$SCRIPT_DIR/db-test-dump-text.sh" "$VERIFY_DB" > pii-corpus.txt

# El padrón real se lee de la base VIVA sólo para poder cruzarlo. Este archivo
# SÍ contiene datos personales: es local, no se sube y se puede borrar al
# terminar la auditoría.
cat > /tmp/padron.sql <<'SQL'
SET HEADING OFF;
SELECT NOMBRE FROM CLIENTES;
SQL
docker cp /tmp/padron.sql "$CONTAINER:/tmp/padron.sql" >/dev/null
fb "$ISQL" -user "$FBUSER" -password "$FBPASS" -i /tmp/padron.sql "$SRC_DB" \
  | LC_ALL=C sed 's/^ *//; s/ *$//' | LC_ALL=C grep -v '^$' > pii-padron.txt

echo
echo "✔ artefacto:      $OUT"
echo "✔ base verificada: $VERIFY_DB"
echo
echo "Ahora:"
echo "  python3 scripts/db-test-pii-scan.py pii-corpus.txt pii-padron.txt"
echo "  make FB_DATABASE=$VERIFY_DB test-firebird-all"
echo
echo "⚠ pii-padron.txt contiene datos personales reales. Bórralo al terminar."
