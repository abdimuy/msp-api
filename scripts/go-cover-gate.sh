#!/usr/bin/env bash
#
# El piso de cobertura, que hasta ahora no era un piso.
#
# `go test -cover ./internal/visitas/...` decía `ok` con `infra/visitasfb` en
# **0.0%** contra un piso declarado de 80%, porque sin `FB_DATABASE` sus diez y
# pico de tests de integración se saltan solos. El skip es RUIDOSO —imprime
# "FB_DATABASE not set; skipping"— y aun así el comando devuelve 0 y el número
# no lo compara nadie. Un piso que se salta solo cuando falta una variable de
# entorno es peor que no tener piso: da confianza falsa.
#
# Este script cierra las dos mitades:
#
#  1. **Sin `FB_DATABASE` falla, no se salta.** Es la mitad que importa: la
#     ausencia de la base dejaba de ser un fallo y pasaba a ser un `ok`.
#  2. **El alcance se DESCUBRE.** Los paquetes salen de `go list` del patrón,
#     no de una lista escrita a mano; un paquete nuevo entra al piso el día que
#     existe. Los pisos por paquete se declaran en el archivo que se pasa como
#     argumento, y **todo paquete sin entrada propia cae al `DEFAULT`** — o sea
#     que olvidarse de declararlo no lo exime, lo mete.
#
# Uso: go-cover-gate.sh <patrón> <archivo-de-pisos>
#   p. ej. go-cover-gate.sh ./internal/visitas/... scripts/cover-floors-visitas.txt
set -euo pipefail

PATRON="${1:?falta el patrón, p. ej. ./internal/visitas/...}"
PISOS="${2:?falta el archivo de pisos}"

if [ ! -f "$PISOS" ]; then
    echo "✘ no existe el archivo de pisos '$PISOS'"
    exit 1
fi

if [ -z "${FB_DATABASE:-}" ]; then
    cat <<'EOF'
✘ FB_DATABASE no está definida.

  Los tests de integración de Firebird se saltan solos cuando falta, y ese skip
  dejaba la cobertura de `infra/visitasfb` en 0.0% mientras el comando decía
  `ok`. Medir sin base y llamarlo verde es justo el modo de falla que esta
  compuerta existe para cerrar, así que acá la ausencia es un FALLO.

  Levantá el contenedor (`mueblera-firebird`) y cargá el entorno:
      set -a && source .env && set +a && make coverage-visitas
EOF
    exit 1
fi

PERFIL="$(mktemp)"
SALIDA="$(mktemp)"
trap 'rm -f "$PERFIL" "$SALIDA"' EXIT

MODULO="$(go list -m)"

# Descubrimiento del alcance: los paquetes son los que el patrón resuelve HOY.
PAQUETES="$(go list "$PATRON")"
if [ -z "$PAQUETES" ]; then
    echo "✘ '$PATRON' no resolvió ningún paquete: el barrido no midió nada."
    exit 1
fi

echo "── cobertura: $PATRON"
go test -count=1 -covermode=atomic -coverprofile="$PERFIL" "$PATRON" 2>&1 | tee "$SALIDA"

# Un SKIP acá significa que algo se saltó pese a tener la base: ruidoso, no
# silencioso. No falla la compuerta (hay skips legítimos por plataforma), pero
# queda impreso junto al resultado en vez de perderse en el scroll.
if grep -q -- "--- SKIP" "$SALIDA"; then
    echo "⚠ hubo tests SKIP en esta corrida (ver arriba)"
fi

piso_de() {
    local paquete="$1"
    local valor
    valor="$(awk -v p="$paquete" '$1 == p { print $2 }' "$PISOS" | head -1)"
    if [ -z "$valor" ]; then
        valor="$(awk '$1 == "DEFAULT" { print $2 }' "$PISOS" | head -1)"
    fi
    echo "$valor"
}

FALLO=0
MEDIDOS=0

for PAQUETE in $PAQUETES; do
    CORTO="${PAQUETE#"$MODULO"/}"

    # Sentencias y sentencias cubiertas, leídas del perfil. Cada línea del
    # perfil es `archivo:ini,fin numSentencias vecesEjecutado`.
    LEIDO="$(awk -v pkg="$PAQUETE/" '
        NR > 1 {
            split($0, campos, ":")
            n = split(campos[1], partes, "/")
            dir = ""
            for (i = 1; i < n; i++) dir = dir partes[i] "/"
            if (dir == pkg) { total += $2; if ($3 > 0) cubiertas += $2 }
        }
        END { print total + 0, cubiertas + 0 }
    ' "$PERFIL")"
    TOTAL="$(echo "$LEIDO" | cut -d' ' -f1)"
    CUBIERTAS="$(echo "$LEIDO" | cut -d' ' -f2)"

    PISO="$(piso_de "$CORTO")"
    if [ -z "$PISO" ]; then
        echo "✘ $CORTO: sin piso declarado y sin DEFAULT en $PISOS."
        echo "  Un paquete sin piso es un paquete sin compuerta: declaralo, o poné un DEFAULT."
        FALLO=1
        continue
    fi

    if [ "$TOTAL" -eq 0 ]; then
        if [ "$PISO" = "NOSTMT" ]; then
            echo "· $CORTO: sin sentencias (declarado NOSTMT)"
        else
            echo "✘ $CORTO: el perfil no trae NI UNA sentencia y el piso declarado es $PISO."
            echo "  O el paquete es solo interfaces —y entonces se declara NOSTMT con su razón—"
            echo "  o la corrida no lo midió, que es exactamente el verde vacío a evitar."
            FALLO=1
        fi
        continue
    fi

    if [ "$PISO" = "NOSTMT" ]; then
        echo "✘ $CORTO: está declarado NOSTMT pero tiene $TOTAL sentencias."
        echo "  La declaración envejeció: cambiala por un piso de verdad."
        FALLO=1
        continue
    fi

    MEDIDOS=$((MEDIDOS + 1))
    PCT="$(awk -v c="$CUBIERTAS" -v t="$TOTAL" 'BEGIN { printf "%.1f", (c * 100) / t }')"
    if awk -v p="$PCT" -v m="$PISO" 'BEGIN { exit !(p + 0 >= m + 0) }'; then
        echo "✔ $CORTO: ${PCT}% (piso ${PISO}%)"
    else
        echo "✘ $CORTO: ${PCT}% < piso ${PISO}%  [$CUBIERTAS de $TOTAL sentencias]"
        FALLO=1
    fi
done

if [ "$MEDIDOS" -eq 0 ]; then
    echo "✘ ningún paquete con sentencias fue medido: el verde no valdría nada."
    exit 1
fi

if [ "$FALLO" -ne 0 ]; then
    exit 1
fi

echo "✔ cobertura: $MEDIDOS paquetes sobre su piso"
