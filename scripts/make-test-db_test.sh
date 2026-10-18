#!/usr/bin/env bash
# make-test-db_test.sh — guarda scripts/make-test-db.sh contra un Firebird
# DESECHABLE con una base mínima SINTÉTICA (sin datos reales). Nunca toca
# mueblera-firebird, la base de dev ni el artefacto publicado.
#
# Qué comprueba:
#   A. una anonimización que falla en su 2.ª sentencia detiene el script con
#      exit distinto de 0, no deja artefacto (ni $OUT ni $OUT.partial) y no
#      confirma las sentencias posteriores en la base de trabajo (-bail).
#   B. si falla la verificación (paso 5), el artefacto ya generado NO queda en
#      la ruta final ni a medias.
#   C. control positivo: con una anonimización buena el artefacto sí aparece
#      en $OUT y no queda el .partial.
#   D/E. si db-test-anonymize.sql está vacío o no tiene ningún UPDATE, el
#      script aborta sin generar nada.
#   F. (estática, sin docker) el script sólo lee la anonimización de
#      "$SCRIPT_DIR/db-test-anonymize.sql": cualquier otra vía para sustituirla
#      (variable, argumento, otra ruta) hace fallar la prueba. Un archivo de
#      anonimización vacío llegó a dejar el artefacto con datos reales y exit 0.
#
# Cada caso corre una COPIA del script en su propio directorio, con su propio
# db-test-anonymize.sql al lado (SCRIPT_DIR lo toma de ahí).
#
# Uso:
#   scripts/make-test-db_test.sh
#   MAKE_TEST_DB_UNDER_TEST=/ruta/make-test-db.sh scripts/make-test-db_test.sh
#   FB_IMAGE=firebirdsql/firebird:5.0.4 scripts/make-test-db_test.sh
#
# Se salta (exit 0) si no hay docker o el daemon no responde.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
UNDER_TEST="${MAKE_TEST_DB_UNDER_TEST:-$REPO_ROOT/scripts/make-test-db.sh}"
# Absoluta. db-test-skip-tables.txt y db-test-dump-text.sh se toman de su
# mismo directorio (una copia revertida los necesita al lado).
UNDER_TEST_DIR="$(cd "$(dirname "$UNDER_TEST")" && pwd)"
UNDER_TEST="$UNDER_TEST_DIR/$(basename "$UNDER_TEST")"
FB_IMAGE="${FB_IMAGE:-jacobalberty/firebird:v4.0}"
CONTAINER="${CONTAINER:-msp-make-test-db-desechable}"
ISQL=/usr/local/firebird/bin/isql

fails=0
ok() { echo "  OK    $*"; }
bad() { echo "  FALLA $*"; fails=$((fails + 1)); }

# F. Las únicas líneas de código (no comentario) que pueden mencionar la
# anonimización son éstas, al pie de la letra.
ALLOWED_ANON_LINES='if ! grep -qiE '"'"'^[[:space:]]*UPDATE[[:space:]]'"'"' "$SCRIPT_DIR/db-test-anonymize.sql" 2>/dev/null; then
echo "❌ $SCRIPT_DIR/db-test-anonymize.sql falta, está vacío o no tiene ningún UPDATE: no se genera el artefacto" >&2
docker cp "$SCRIPT_DIR/db-test-anonymize.sql" "$CONTAINER:/tmp/db-test-anonymize.sql" >/dev/null
fb "$ISQL" -bail -user "$FBUSER" -password "$FBPASS" -i /tmp/db-test-anonymize.sql "$WORK_DB"'
anon_lines_ok() {
	local extra
	extra="$(grep -iE 'anonymi|anonimi' "$1" | sed 's/^[[:space:]]*//' | grep -v '^#' |
		grep -vxF -f <(printf '%s\n' "$ALLOWED_ANON_LINES"))"
	[ -z "$extra" ] || { echo "$extra" | sed 's/^/        > /'; return 1; }
	# y el guardia de archivo vacío / sin UPDATE sigue ahí
	grep -qF 'grep -qiE '"'"'^[[:space:]]*UPDATE[[:space:]]'"'"' "$SCRIPT_DIR/db-test-anonymize.sql"' "$1"
}
echo "▶ F. el script sólo lee \$SCRIPT_DIR/db-test-anonymize.sql"
anon_lines_ok "$UNDER_TEST" && ok "ninguna otra vía para sustituir la anonimización" ||
	bad "el script lee la anonimización de otro lado o perdió el guardia"
# Control positivo del chequeo: una copia con la variable de antes debe fallar.
awk '{print} /^SCRIPT_DIR=/ {print "ANONYMIZE_SQL=\"${ANONYMIZE_SQL:-$SCRIPT_DIR/db-test-anonymize.sql}\""}' "$UNDER_TEST" >"${TMPDIR:-/tmp}/mtd-ctrl.$$"
anon_lines_ok "${TMPDIR:-/tmp}/mtd-ctrl.$$" >/dev/null && bad "el chequeo NO detecta ANONYMIZE_SQL (control positivo)" ||
	ok "el chequeo detecta una variable ANONYMIZE_SQL (control positivo)"
rm -f "${TMPDIR:-/tmp}/mtd-ctrl.$$"

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
	echo "SKIP: docker no disponible (sólo corrió F)"
	[ "$fails" -eq 0 ] || { echo "FAIL: $fails comprobaciones"; exit 1; }
	exit 0
fi
# Se le hace docker rm -f: sólo nombres *-desechable.
case "$CONTAINER" in
*-desechable) ;;
*) echo "ABORT: CONTAINER=$CONTAINER no termina en -desechable; no se borra nada" >&2; exit 2 ;;
esac

TMP="$(mktemp -d "${TMPDIR:-/tmp}/make-test-db-test.XXXXXX")"
cleanup() {
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup EXIT

sql() { docker exec -i "$CONTAINER" "$ISQL" -u SYSDBA -p masterkey -q "$1"; }

echo "▶ contenedor $CONTAINER ($FB_IMAGE)"
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$CONTAINER" -e ISC_PASSWORD=masterkey \
	-e FIREBIRD_ROOT_PASSWORD=masterkey "$FB_IMAGE" >/dev/null || { echo "no se pudo arrancar"; exit 1; }
docker exec "$CONTAINER" sh -c '[ -d /usr/local/firebird ] || ln -s /opt/firebird /usr/local/firebird; mkdir -p /firebird/data'
SRC=/firebird/data/SRC.FDB
created=0
for _ in $(seq 1 30); do
	if docker exec -i "$CONTAINER" "$ISQL" -q >/dev/null 2>&1 <<<"CREATE DATABASE '$SRC' USER 'SYSDBA' PASSWORD 'masterkey' DEFAULT CHARACTER SET UTF8;"; then
		created=1
		break
	fi
	sleep 2
done
[ "$created" = 1 ] || { echo "no se pudo crear la base"; exit 1; }
# Base mínima sintética: CLIENTES (el script lee el padrón de aquí) y una
# tabla de catálogo que la anonimización de prueba sustituye.
sql "$SRC" >/dev/null <<'EOF'
CREATE TABLE CLIENTES (CLIENTE_ID INTEGER NOT NULL PRIMARY KEY, NOMBRE VARCHAR(100));
CREATE TABLE COBRADORES (COBRADOR_ID INTEGER NOT NULL PRIMARY KEY, NOMBRE VARCHAR(100));
COMMIT;
INSERT INTO CLIENTES VALUES (1, 'CLIENTE SINTETICO UNO');
INSERT INTO CLIENTES VALUES (2, 'CLIENTE SINTETICO DOS');
INSERT INTO COBRADORES VALUES (1, 'ORIGINAL');
INSERT INTO COBRADORES VALUES (2, 'ORIGINAL');
COMMIT;
EOF

cat >"$TMP/anon-mala.sql" <<'EOF'
UPDATE COBRADORES SET NOMBRE = 'SINTETICO' WHERE COBRADOR_ID = 1;
UPDATE TABLA_QUE_NO_EXISTE SET X = 1;
UPDATE COBRADORES SET NOMBRE = 'SINTETICO' WHERE COBRADOR_ID = 2;
COMMIT;
EOF
cat >"$TMP/anon-buena.sql" <<'EOF'
UPDATE COBRADORES SET NOMBRE = 'SINTETICO';
COMMIT;
EOF
: >"$TMP/anon-vacia.sql"
cat >"$TMP/anon-sin-update.sql" <<'EOF'
-- UPDATE COBRADORES SET NOMBRE = 'SINTETICO';
COMMIT;
EOF

# run <caso> <anonimizacion> [VAR=valor ...]
run() {
	local caso="$1" anon="$2"
	shift 2
	local dir="$TMP/scripts-$caso"
	mkdir -p "$TMP/$caso" "$dir"
	cp "$UNDER_TEST" "$dir/make-test-db.sh"
	cp "$UNDER_TEST_DIR/db-test-skip-tables.txt" "$UNDER_TEST_DIR/db-test-dump-text.sh" "$dir/"
	cp "$anon" "$dir/db-test-anonymize.sql"
	(cd "$TMP/$caso" && env CONTAINER="$CONTAINER" SRC_DB="$SRC" \
		FULL_FBK=/firebird/data/full.fbk WORK_DB=/firebird/data/CAT.FDB \
		VERIFY_DB=/firebird/data/CATVERIF.FDB OUT="$TMP/$caso/msp-test-db.fbk.gz" \
		FBUSER=SYSDBA FBPASS=masterkey "$@" \
		bash "$dir/make-test-db.sh" 2>&1)
}
no_artifact() {
	local caso="$1"
	[ ! -e "$TMP/$caso/msp-test-db.fbk.gz" ] && ok "no quedó \$OUT" || bad "quedó $TMP/$caso/msp-test-db.fbk.gz"
	[ ! -e "$TMP/$caso/msp-test-db.fbk.gz.partial" ] && ok "no quedó \$OUT.partial" || bad "quedó un artefacto a medias (.partial)"
}

echo "▶ A. anonimización que falla en su segunda sentencia"
out="$(run A "$TMP/anon-mala.sql")"
rc=$?
echo "$out" | tail -6 | sed 's/^/        | /'
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
no_artifact A
rows="$(sql /firebird/data/CAT.FDB <<<"SET HEADING OFF; SELECT COBRADOR_ID||'='||TRIM(NOMBRE) FROM COBRADORES ORDER BY 1;" 2>/dev/null | tr -s ' \n' ' ' | sed 's/^ //; s/ $//')"
echo "        COBRADORES en la base de trabajo = [$rows]"
[ "$rows" = "1=ORIGINAL 2=ORIGINAL" ] && ok "no se confirmó nada después del error (-bail)" ||
	bad "se confirmó una anonimización a medias: [$rows]"

echo "▶ B. falla la verificación (paso 5) después de generar el artefacto"
out="$(run B "$TMP/anon-buena.sql" VERIFY_DB=/no/existe/CATVERIF.FDB)"
rc=$?
echo "$out" | tail -4 | sed 's/^/        | /'
echo "$out" | grep -q '4/5' && ok "llegó a generar el artefacto (control positivo)" || bad "no llegó al paso 4"
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
no_artifact B

echo "▶ C. control positivo: anonimización buena"
out="$(run C "$TMP/anon-buena.sql")"
rc=$?
echo "$out" | tail -4 | sed 's/^/        | /'
[ "$rc" -eq 0 ] && ok "exit 0" || bad "exit=$rc, se esperaba 0"
[ -s "$TMP/C/msp-test-db.fbk.gz" ] && ok "el artefacto está en \$OUT" || bad "no hay artefacto en \$OUT"
[ ! -e "$TMP/C/msp-test-db.fbk.gz.partial" ] && ok "no quedó .partial" || bad "quedó .partial"

for caso in D:anon-vacia E:anon-sin-update; do
	c="${caso%%:*}"
	f="${caso#*:}"
	echo "▶ $c. db-test-anonymize.sql = $f"
	out="$(run "$c" "$TMP/$f.sql")"
	rc=$?
	echo "$out" | tail -2 | sed 's/^/        | /'
	[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
	no_artifact "$c"
done

echo
if [ "$fails" -eq 0 ]; then
	echo "PASS ($FB_IMAGE)"
	exit 0
fi
echo "FAIL: $fails comprobaciones ($FB_IMAGE)"
exit 1
