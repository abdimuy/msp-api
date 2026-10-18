#!/usr/bin/env bash
# makefile-migrate_test.sh — guarda los targets Firebird del Makefile.
#
# Qué comprueba (contra un contenedor Firebird DESECHABLE y una base vacía;
# nunca contra mueblera-firebird ni la base de dev):
#
#   1. fb-migrate-up: una migración que falla en su 2.ª sentencia NO queda en
#      MSP_MIGRATIONS, el ciclo se detiene (la siguiente no se intenta), el exit
#      es distinto de 0 y el mensaje nombra el archivo que falló.
#   2. fb-migrate-down: un .down.sql que falla antes de su DELETE deja la fila
#      en MSP_MIGRATIONS y sale con exit distinto de 0.
#   3. fb-restore: contestar algo distinto de "restore" aborta de verdad (la
#      base no se reemplaza).
#   4. FB_SNAPSHOTS_DIR: desde un worktree, el default es absoluto y apunta al
#      checkout principal; un snapshot escrito con FB_SNAPSHOTS_DIR=<ruta>
#      sobrevive a borrar el worktree desde el que se tomó.
#   5. fb-snapshot no sobreescribe un NAME existente salvo con FORCE=1.
#   6. fb-restore sin NAME aborta y lista los snapshots (aunque se teclee
#      "restore"): el directorio lo comparten todos los worktrees.
#   7. FB_SNAPSHOTS_DIR con espacios funciona (rutas entre comillas).
#   Además, el error de fb-migrate-up indica `make fb-restore NAME=…`.
#
# CONTAINER debe terminar en -desechable (se le hace docker rm -f).
#
# Uso:
#   scripts/makefile-migrate_test.sh                 # prueba ./Makefile
#   MAKEFILE_UNDER_TEST=/ruta/Makefile scripts/makefile-migrate_test.sh
#   FB_IMAGE=firebirdsql/firebird:5.0.4 scripts/makefile-migrate_test.sh
#
# Se salta (exit 0) si no hay docker o el daemon no responde.
set -uo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
MK="${MAKEFILE_UNDER_TEST:-$REPO_ROOT/Makefile}"
MK="$(cd "$(dirname "$MK")" && pwd)/$(basename "$MK")"
FB_IMAGE="${FB_IMAGE:-jacobalberty/firebird:v4.0}"
CONTAINER="${CONTAINER:-msp-fix-makefile-desechable}"
DB=/firebird/data/PRUEBA.FDB

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
	echo "SKIP: docker no disponible"
	exit 0
fi
# A este contenedor se le hace `docker rm -f`: sólo nombres que terminen en
# -desechable, para que nunca se borre mueblera-firebird ni otro ajeno.
case "$CONTAINER" in
*-desechable) ;;
*) echo "ABORT: CONTAINER=$CONTAINER no termina en -desechable; no se borra nada" >&2; exit 2 ;;
esac

TMP="$(mktemp -d "${TMPDIR:-/tmp}/makefile-migrate-test.XXXXXX")"
WT=""
fails=0
cleanup() {
	docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
	if [ -n "$WT" ] && [ -d "$WT" ]; then
		git -C "$REPO_ROOT" worktree remove --force "$WT" >/dev/null 2>&1 || true
	fi
	rm -rf "$TMP"
}
trap cleanup EXIT

ok() { echo "  OK    $*"; }
bad() { echo "  FALLA $*"; fails=$((fails + 1)); }

# Corre make desde un directorio sin .env (el Makefile hace `include .env`, que
# le ganaría al entorno). Todo se pasa por línea de make.
mk() {
	(cd "$TMP/cwd" && make --no-print-directory -f "$MK" "$@" \
		FB_CONTAINER="$CONTAINER" FB_DATABASE="$DB" FB_USER=SYSDBA FB_PASSWORD=masterkey)
}
sql() {
	docker exec -i "$CONTAINER" /usr/local/firebird/bin/isql -u SYSDBA -p masterkey -q "$DB"
}
migrated_ids() {
	sql <<<'SELECT ID FROM MSP_MIGRATIONS ORDER BY ID;' 2>/dev/null |
		awk '$1 ~ /^[0-9]+$/ {print $1}' | tr '\n' ' ' | sed 's/ $//'
}

mkdir -p "$TMP/cwd" "$TMP/up" "$TMP/down"
echo "▶ contenedor $CONTAINER ($FB_IMAGE)"
docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$CONTAINER" -e ISC_PASSWORD=masterkey \
	-e FIREBIRD_ROOT_PASSWORD=masterkey "$FB_IMAGE" >/dev/null || { echo "no se pudo arrancar"; exit 1; }
# La imagen oficial de FB5 instala en /opt/firebird; el Makefile usa /usr/local/firebird.
docker exec "$CONTAINER" sh -c '[ -d /usr/local/firebird ] || ln -s /opt/firebird /usr/local/firebird'
docker exec "$CONTAINER" mkdir -p /firebird/data
created=0
for _ in $(seq 1 30); do
	if docker exec -i "$CONTAINER" /usr/local/firebird/bin/isql -q >/dev/null 2>&1 <<<"CREATE DATABASE '$DB' USER 'SYSDBA' PASSWORD 'masterkey' DEFAULT CHARACTER SET UTF8;"; then
		created=1
		break
	fi
	sleep 2
done
[ "$created" = 1 ] || { echo "no se pudo crear la base"; exit 1; }

cat >"$TMP/up/000001_buena.up.sql" <<'EOF'
CREATE TABLE MSP_MIGRATIONS (ID INTEGER NOT NULL PRIMARY KEY, NAME VARCHAR(200) NOT NULL, APPLIED_AT TIMESTAMP NOT NULL);
COMMIT;
CREATE TABLE PRUEBA_BUENA (ID INTEGER NOT NULL PRIMARY KEY);
COMMIT;
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (1, '000001_buena', CURRENT_TIMESTAMP);
COMMIT;
EOF
cat >"$TMP/up/000002_mala.up.sql" <<'EOF'
CREATE TABLE PRUEBA_MALA (ID INTEGER NOT NULL PRIMARY KEY);
CREATE INDEX IDX_INEXISTENTE ON TABLA_QUE_NO_EXISTE (ID);
COMMIT;
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (2, '000002_mala', CURRENT_TIMESTAMP);
COMMIT;
EOF
cat >"$TMP/up/000003_posterior.up.sql" <<'EOF'
CREATE TABLE PRUEBA_POSTERIOR (ID INTEGER NOT NULL PRIMARY KEY);
COMMIT;
INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (3, '000003_posterior', CURRENT_TIMESTAMP);
COMMIT;
EOF

echo "▶ 1. fb-migrate-up con una migración que falla en su segunda sentencia"
out="$(mk fb-migrate-up FB_MIGRATIONS_DIR="$TMP/up" 2>&1)"
rc=$?
echo "$out" | sed 's/^/        | /'
ids="$(migrated_ids)"
echo "        MSP_MIGRATIONS = [$ids]  exit=$rc"
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
[ "$ids" = "1" ] && ok "sólo la buena (1) quedó registrada" || bad "registradas [$ids], se esperaba [1]"
echo "$out" | grep -q 'Failed at .*000002_mala' && ok "el mensaje nombra 000002_mala" || bad "el mensaje no nombra 000002_mala"
echo "$out" | grep -q 'make fb-restore NAME=' && ok "el mensaje indica restaurar el snapshot" || bad "el mensaje no indica make fb-restore NAME="
echo "$out" | grep -q '000003_posterior' && bad "intentó 000003 después del fallo" || ok "no intentó 000003 tras el fallo"

echo "▶ 2. fb-migrate-down que falla antes de su DELETE"
cat >"$TMP/down/000001_buena.down.sql" <<'EOF'
DROP TABLE PRUEBA_BUENA;
DROP TABLE TABLA_QUE_NO_EXISTE;
COMMIT;
DELETE FROM MSP_MIGRATIONS WHERE ID = 1;
COMMIT;
EOF
out="$(mk fb-migrate-down N=000001 FB_MIGRATIONS_DIR="$TMP/down" 2>&1)"
rc=$?
echo "$out" | sed 's/^/        | /'
ids="$(migrated_ids)"
echo "        MSP_MIGRATIONS = [$ids]  exit=$rc"
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
[ "$ids" = "1" ] && ok "la fila 1 sigue en MSP_MIGRATIONS" || bad "registradas [$ids], se esperaba [1]"

echo "▶ 3. fb-restore abortado con una respuesta distinta de 'restore'"
SNAPS="$TMP/snaps-fuera-del-worktree"
mk fb-snapshot NAME=antes FB_SNAPSHOTS_DIR="$SNAPS" >/dev/null 2>&1
# Control positivo: sin snapshot, fb-restore fallaría por "not found" y el caso
# pasaría sin probar el aborto.
[ -s "$SNAPS/antes.fbk" ] && ok "snapshot previo creado (control positivo)" || bad "no se creó $SNAPS/antes.fbk"
sql <<<'CREATE TABLE PRUEBA_DESPUES_DEL_SNAPSHOT (ID INTEGER); COMMIT;' >/dev/null 2>&1
out="$(echo no | mk fb-restore NAME=antes FB_SNAPSHOTS_DIR="$SNAPS" 2>&1)"
rc=$?
echo "$out" | sed 's/^/        | /'
exists="$(sql <<<"SELECT COUNT(*) FROM RDB\$RELATIONS WHERE RDB\$RELATION_NAME = 'PRUEBA_DESPUES_DEL_SNAPSHOT';" 2>/dev/null | awk '$1 ~ /^[0-9]+$/ {print $1}')"
echo "        tabla posterior al snapshot presente=$exists  exit=$rc"
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
[ "$exists" = "1" ] && ok "la base NO se reemplazó" || bad "la base se reemplazó pese a contestar 'no'"

echo "▶ 5. fb-snapshot no sobreescribe un NAME existente salvo con FORCE=1"
before="$(cksum <"$SNAPS/antes.fbk" 2>/dev/null)"
sql <<<'CREATE TABLE PRUEBA_CAMBIO_PARA_FORCE (ID INTEGER); COMMIT;' >/dev/null 2>&1
out="$(mk fb-snapshot NAME=antes FB_SNAPSHOTS_DIR="$SNAPS" 2>&1)"
rc=$?
echo "$out" | sed 's/^/        | /'
after="$(cksum <"$SNAPS/antes.fbk" 2>/dev/null)"
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
[ -n "$before" ] && [ "$before" = "$after" ] && ok "antes.fbk no cambió" || bad "antes.fbk se sobreescribió"
mk fb-snapshot NAME=antes FB_SNAPSHOTS_DIR="$SNAPS" FORCE=1 >/dev/null 2>&1
rc=$?
forced="$(cksum <"$SNAPS/antes.fbk" 2>/dev/null)"
[ "$rc" -eq 0 ] && [ "$forced" != "$before" ] && ok "con FORCE=1 sí se sobreescribe" || bad "FORCE=1 no sobreescribió (exit=$rc)"

echo "▶ 6. fb-restore sin NAME aborta y lista los snapshots (aunque se teclee 'restore')"
sql <<<'CREATE TABLE PRUEBA_SIN_NAME (ID INTEGER); COMMIT;' >/dev/null 2>&1
out="$(echo restore | mk fb-restore FB_SNAPSHOTS_DIR="$SNAPS" 2>&1)"
rc=$?
echo "$out" | sed 's/^/        | /'
exists="$(sql <<<"SELECT COUNT(*) FROM RDB\$RELATIONS WHERE RDB\$RELATION_NAME = 'PRUEBA_SIN_NAME';" 2>/dev/null | awk '$1 ~ /^[0-9]+$/ {print $1}')"
[ "$rc" -ne 0 ] && ok "exit distinto de 0 ($rc)" || bad "exit=$rc, se esperaba != 0"
[ "$exists" = "1" ] && ok "la base NO se reemplazó" || bad "restauró el más reciente sin NAME"
echo "$out" | grep -q 'antes.fbk' && ok "lista los snapshots disponibles" || bad "no listó antes.fbk"

echo "▶ 7. FB_SNAPSHOTS_DIR con espacios"
ESP="$TMP/con espacios/snaps"
mk fb-snapshot NAME=esp FB_SNAPSHOTS_DIR="$ESP" >/dev/null 2>&1
rc=$?
[ "$rc" -eq 0 ] && [ -s "$ESP/esp.fbk" ] && ok "snapshot en ruta con espacios" || bad "falló con espacios (exit=$rc)"
out="$(mk fb-snapshot-list FB_SNAPSHOTS_DIR="$ESP" 2>&1)"
echo "$out" | grep -q 'esp.fbk' && ok "fb-snapshot-list la ve" || bad "fb-snapshot-list no la ve"

echo "▶ 4. FB_SNAPSHOTS_DIR desde un worktree"
WT="$TMP/worktree-de-prueba"
if git -C "$REPO_ROOT" worktree add --detach "$WT" HEAD >/dev/null 2>&1; then
	cp "$MK" "$WT/Makefile"
	main_root="$(cd "$(git -C "$REPO_ROOT" rev-parse --git-common-dir)/.." && pwd)"
	# make 3.81 (el de macOS) no tiene --eval: el target auxiliar entra por stdin.
	def="$(cd "$WT" && printf 'print-snapdir: ; @echo $(FB_SNAPSHOTS_DIR)\n' |
		make --no-print-directory -f Makefile -f - print-snapdir 2>/dev/null)"
	echo "        default desde el worktree = $def"
	case "$def" in
	/*) ok "el default es absoluto" ;;
	*) bad "el default es relativo ($def): un snapshot tomado aquí se pierde con el worktree" ;;
	esac
	[ "$def" = "$main_root/data/firebird-snapshots" ] && ok "apunta al checkout principal" ||
		bad "no apunta a $main_root/data/firebird-snapshots"
	ESTABLE="$TMP/estable/no/existe/aun"
	(cd "$WT" && make --no-print-directory -f Makefile fb-snapshot NAME=sobrevive \
		FB_CONTAINER="$CONTAINER" FB_DATABASE="$DB" FB_SNAPSHOTS_DIR="$ESTABLE") >/dev/null 2>&1
	git -C "$REPO_ROOT" worktree remove --force "$WT" >/dev/null 2>&1
	[ -d "$WT" ] && bad "no se pudo borrar el worktree" || ok "worktree borrado"
	[ -s "$ESTABLE/sobrevive.fbk" ] && ok "el snapshot sobrevive en FB_SNAPSHOTS_DIR (dir creado por el target)" ||
		bad "no está $ESTABLE/sobrevive.fbk"
else
	bad "no se pudo crear el worktree de prueba"
fi

echo
if [ "$fails" -eq 0 ]; then
	echo "PASS ($FB_IMAGE)"
	exit 0
fi
echo "FAIL: $fails comprobaciones ($FB_IMAGE)"
exit 1
