#!/usr/bin/env bash
#
# La compuerta de mutación, que hasta ahora no lo era.
#
# `gremlins unleash --threshold-efficacy 80` **sale 0 pase lo que pase**.
# Medido: con eficacia 100% y umbral 101 el binario devuelve 0, por flag y por
# archivo de configuración (el binario SÍ sabe devolver 1 — con un flag
# inexistente devuelve 1 —, simplemente no aplica este umbral). O sea que el
# `≥80%` que el Makefile promete en su ayuda no lo hacía cumplir nadie: aunque
# TODOS los mutantes sobrevivieran, `make test-mutation-visitas` salía verde.
#
# Y hay un segundo agujero, peor porque es invisible: la eficacia se calcula
# como `Killed / (Killed + Lived)`, así que **ignora los mutantes que ni
# llegaron a evaluarse**. Dos corridas del MISMO código dieron 32 y 1 mutantes
# evaluados de 33, con el porcentaje clavado en 100.00% las dos veces. Un
# número que no se mueve cuando la medición cae 97% no es una medición.
#
# Este script arregla las dos cosas: parsea la salida real y falla —de verdad,
# con exit != 0— si la eficacia queda bajo el piso O si la fracción de mutantes
# EVALUADOS queda bajo el suyo. Y falla también si no pudo parsear nada, porque
# una compuerta que no entiende lo que midió no puede decir que pasó.
#
# Uso: gremlins-gate.sh <paquete> <min-eficacia> <min-evaluados-pct>
set -euo pipefail

PAQUETE="${1:?falta el paquete, p. ej. ./internal/visitas/domain}"
MIN_EFICACIA="${2:?falta el piso de eficacia, p. ej. 80}"
# El tercer argumento admite `NOMUTANTS`: "de este paquete NO se espera ni un
# mutante". Es una declaración, no un permiso — si algún día genera mutantes, la
# compuerta falla pidiendo que se le ponga un piso de verdad, igual que hace
# `go-cover-gate.sh` con `NOSTMT`. Sin esto, un paquete sin operadores mutables
# (plomería de `context`, por ejemplo) obligaría a elegir entre un rojo perpetuo
# y un verde que no mide nada, que son las dos salidas equivocadas.
MIN_EVALUADOS="${3:?falta el piso de mutantes evaluados en % (o NOMUTANTS)}"

SALIDA="$(mktemp)"
trap 'rm -f "$SALIDA"' EXIT

echo "── mutación: $PAQUETE (eficacia ≥ ${MIN_EFICACIA}%, evaluados ≥ ${MIN_EVALUADOS}%)"

# El coeficiente de timeout va por FLAG y no por `.gremlins.yaml`, y no es un
# detalle de estilo.
#
# Medido en este repo, mismo commit, sin tocar una línea: con el valor puesto
# SOLO en el archivo de configuración, `visitas/domain` evaluaba entre 2 y 10
# mutantes de 52 y el resto expiraba — y daba lo mismo escribir 3, 30 o 200,
# porque **el binario no aplica esa clave del archivo** (es el mismo defecto que
# `threshold.efficacy`, que tampoco se aplica ni por flag ni por config). Con el
# flag explícito, 51 de 52, y estable en tres corridas seguidas con la caché de
# `go test` caliente y fría.
#
# Esa es la explicación de los números que la revisión no pudo reproducir (32
# evaluados en una corrida, 1 en la siguiente, eficacia clavada en 100.00% las
# dos veces): no era la carga de la máquina, era el coeficiente por defecto —2—
# actuando siempre, porque el archivo nunca mandó.
COEFICIENTE_TIMEOUT="${GREMLINS_TIMEOUT_COEFFICIENT:-60}"

# `|| true`: el exit code de gremlins no dice nada (ese es el hallazgo). Lo que
# manda es el parseo de abajo. Un fallo REAL del binario —no compila, paquete
# inexistente— se detecta igual: no imprime el bloque de conteos y el parseo
# falla ruidosamente.
gremlins unleash --timeout-coefficient "$COEFICIENTE_TIMEOUT" \
    --threshold-efficacy "$MIN_EFICACIA" "$PAQUETE" 2>&1 | tee "$SALIDA" || true

leer() {
    # Los conteos vienen como `Killed: 5, Lived: 0, Not covered: 1`.
    sed -n "s/.*$1: \\([0-9][0-9]*\\).*/\\1/p" "$SALIDA" | head -1
}

KILLED="$(leer 'Killed')"
LIVED="$(leer 'Lived')"
NOT_COVERED="$(leer 'Not covered')"
TIMED_OUT="$(leer 'Timed out')"
NOT_VIABLE="$(leer 'Not viable')"
SKIPPED="$(leer 'Skipped')"
EFICACIA="$(sed -n 's/.*Test efficacy: \([0-9][0-9]*\)\.\([0-9][0-9]*\)%.*/\1.\2/p' "$SALIDA" | head -1)"

# gremlins imprime esto y nada más cuando el paquete no tiene un solo operador
# mutable. Es un estado legítimo, pero solo si está DECLARADO.
if grep -q "No results to report" "$SALIDA"; then
    if [ "$MIN_EVALUADOS" = "NOMUTANTS" ]; then
        echo "· $PAQUETE: cero mutantes generados (declarado NOMUTANTS)"
        exit 0
    fi
    echo "✘ $PAQUETE: gremlins no generó NI UN mutante, así que este target no mide nada."
    echo "  Venía saliendo 0 igual, que es el verde vacío que esta compuerta cierra."
    echo "  Si el paquete de verdad no tiene nada mutable, declaralo con NOMUTANTS."
    exit 1
fi

if [ "$MIN_EVALUADOS" = "NOMUTANTS" ] && [ -n "$KILLED" ]; then
    echo "✘ $PAQUETE: está declarado NOMUTANTS pero gremlins SÍ generó mutantes."
    echo "  La declaración envejeció: cambiala por un piso de evaluados de verdad."
    exit 1
fi

# Control positivo del propio parseo: sin estos números el veredicto de abajo
# sería una opinión. Es exactamente el modo de falla que este script existe
# para cerrar, así que no puede reproducirlo.
if [ -z "$KILLED" ] || [ -z "$LIVED" ] || [ -z "$TIMED_OUT" ] || [ -z "$EFICACIA" ]; then
    echo "✘ $PAQUETE: no se pudo leer el resultado de gremlins (Killed/Lived/Timed out/eficacia)."
    echo "  Una compuerta que no entiende lo que midió no puede decir que pasó."
    exit 1
fi

: "${NOT_COVERED:=0}"
: "${NOT_VIABLE:=0}"
: "${SKIPPED:=0}"

EVALUADOS=$((KILLED + LIVED))
TOTAL=$((KILLED + LIVED + NOT_COVERED + TIMED_OUT + NOT_VIABLE + SKIPPED))

if [ "$TOTAL" -eq 0 ]; then
    echo "✘ $PAQUETE: gremlins generó CERO mutantes. Un 100% sobre nada es el verde vacío"
    echo "  que esta compuerta existe para impedir."
    exit 1
fi

PCT_EVALUADOS=$((EVALUADOS * 100 / TOTAL))

echo "   mutantes: $TOTAL · evaluados: $EVALUADOS (${PCT_EVALUADOS}%) · expirados: $TIMED_OUT · eficacia: ${EFICACIA}%"

FALLO=0

# `awk` porque la eficacia trae decimales y `[ ]` de POSIX solo compara enteros.
if ! awk -v e="$EFICACIA" -v m="$MIN_EFICACIA" 'BEGIN { exit !(e + 0 >= m + 0) }'; then
    echo "✘ $PAQUETE: eficacia ${EFICACIA}% < ${MIN_EFICACIA}%."
    echo "  Sobrevivieron $LIVED mutantes: hay comportamiento que ningún test observa."
    FALLO=1
fi

if [ "$PCT_EVALUADOS" -lt "$MIN_EVALUADOS" ]; then
    echo "✘ $PAQUETE: solo se evaluó el ${PCT_EVALUADOS}% de los mutantes ($EVALUADOS de $TOTAL),"
    echo "  bajo el piso de ${MIN_EVALUADOS}%. Los $TIMED_OUT expirados NO cuentan en la eficacia,"
    echo "  así que el porcentaje de arriba se calcula sobre una muestra chica y variable."
    echo "  Subí GREMLINS_TIMEOUT_COEFFICIENT (hoy $COEFICIENTE_TIMEOUT) o achicá el paquete."
    FALLO=1
fi

if [ "$FALLO" -ne 0 ]; then
    exit 1
fi

echo "✔ $PAQUETE"
