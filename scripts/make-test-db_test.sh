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
# Absoluta: cada caso corre desde su propio directorio. Una copia revertida
# necesita a su lado db-test-skip-tables.txt y db-test-dump-text.sh.
UNDER_TEST="$(cd "$(dirname "$UNDER_TEST")" && pwd)/$(basename "$UNDER_TEST")"
FB_IMAGE="${FB_IMAGE:-jacobalberty/firebird:v4.0}"
CONTAINER="${CONTAINER:-msp-make-test-db-desechable}"
ISQL=/usr/local/firebird/bin/isql

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
	echo "SKIP: docker no disponible"
	exit 0
fi
# Se le hace docker rm -f: sólo nombres *-desechable.
case "$CONTAINER" in
*-desechable) ;;
*) echo "ABORT: CONTAINER=$CONTAINER no termina en -desechable; no se borra nada" >&2; exit 2 ;;
esac

TMP="$(mktemp -d "${TMPDIR:-/tmp}/make-test-db-test.XXXXXX")"
fails=0
cleanup() {
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
	rm -rf "$TMP"
}
trap cleanup EXIT

ok() { echo "  OK    $*"; }
bad() { echo "  FALLA $*"; fails=$((fails + 1)); }
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

# run <caso> <anonimizacion> [VAR=valor ...]
run() {
	local caso="$1" anon="$2"
	shift 2
	mkdir -p "$TMP/$caso"
	(cd "$TMP/$caso" && env CONTAINER="$CONTAINER" SRC_DB="$SRC" \
		FULL_FBK=/firebird/data/full.fbk WORK_DB=/firebird/data/CAT.FDB \
		VERIFY_DB=/firebird/data/CATVERIF.FDB OUT="$TMP/$caso/msp-test-db.fbk.gz" \
		ANONYMIZE_SQL="$anon" FBUSER=SYSDBA FBPASS=masterkey "$@" \
		bash "$UNDER_TEST" 2>&1)
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

echo
if [ "$fails" -eq 0 ]; then
	echo "PASS ($FB_IMAGE)"
	exit 0
fi
echo "FAIL: $fails comprobaciones ($FB_IMAGE)"
exit 1
