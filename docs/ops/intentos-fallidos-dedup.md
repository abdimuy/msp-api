# Intentos fallidos — dedup, cierre y retención

> Acompaña a la migración `000059` y al binario que trae la dedup de capturas.
> Aditivo y sin riesgo para el sync: se despliega **después** de que la parte
> del predicado de ventas esté verificada
> (`docs/ops/lapidas-huerfanas-despliegue.md`).

## Qué cambió

| Antes | Ahora |
|---|---|
| `Save` era un INSERT puro: fila nueva **y copia nueva del cuerpo con sus fotos** por cada reintento | Funde por `(PATH, IDEMPOTENCY_KEY)` contra filas pendientes: `RETRY_COUNT + 1` y el blob descartado se borra |
| `RECEIVED_AT` era "cuándo pasó esto" | Es **el primer intento**; `LAST_SEEN_AT` es el último |
| Un intento cuyo trabajo entró después seguía pendiente para siempre | Lo cierra el middleware al ver el 2xx con la misma clave, y el janitor preguntándole a la fuente |
| Todo vivía 90 días | Los **cerrados** se purgan a los 7; los pendientes conservan los 90 |

Medido en producción sobre 7 días antes del cambio: **609 filas para 130
ventas distintas**, 685 archivos y **896 MB**. Una sola venta dejó 13 copias de
2.8 MB.

## Orden de despliegue — la migración va PRIMERO

1. `make fb-snapshot NAME=antes-000059-last-seen`
2. **Migración `000059`** (`make fb-migrate-up`). Agrega `LAST_SEEN_AT` y el
   índice `(IDEMPOTENCY_KEY, PATH, STATUS)`.
3. **Binario con la parte 2.**

El orden no es intercambiable: el binario nuevo **escribe** `LAST_SEEN_AT` en
cada captura repetida. Desplegarlo antes de la columna hace que toda captura
repetida falle con *column unknown* — y perder la evidencia de una venta
fallida es exactamente lo que este módulo existe para evitar.

`ALTER TABLE ADD` de una columna nullable en Firebird es un cambio de
metadatos: no reescribe filas y no necesita ventana. El `CREATE INDEX` sí
recorre la tabla, que hoy son cientos de filas.

## Reversión — al revés, y por la misma razón

1. Revertir el **binario** primero.
2. Después la migración (`000059` down).

Correr el down con el binario nuevo vivo rompe cada captura repetida.

## Qué mirar después de desplegar (sólo lectura)

```sql
-- Filas por venta distinta: debe tender a 1.
SELECT COUNT(*) AS FILAS, COUNT(DISTINCT IDEMPOTENCY_KEY) AS TRABAJOS
FROM MSP_FAILED_INTENTS WHERE PATH = '/v2/ventas';

-- Los reintentos ahora se cuentan en vez de duplicarse.
SELECT MAX(RETRY_COUNT) AS MAX_REINTENTOS FROM MSP_FAILED_INTENTS;

-- El rezago cerrándose solo.
SELECT STATUS, COUNT(*) FROM MSP_FAILED_INTENTS GROUP BY 1;
```

Y en disco: el directorio de blobs (`STORAGE_DIR`) deja de crecer con cada
reintento. El barrido de huérfanos del arranque sigue ahí como red de abajo,
pero ya no es lo que recupera el disco.

## Lo que este cambio NO arregla

**La app reintenta igual un `422` que un corte de red.** Con la dedup deja de
costar disco, pero una venta que necesita corrección humana seguirá golpeando
el servidor cada ciclo. Eso es de `msp-app-kt` y va aparte.

## Una dependencia que no está escrita en ningún contrato

El conciliador del janitor se apoya en que **la `Idempotency-Key` que manda la
app en `POST /v2/ventas` es el id de la venta**. No hay columna, ni cabecera,
ni documento que lo obligue: es una propiedad de cómo el cliente genera la
clave.

Si algún día la app la genera de otro modo, el conciliador responderá "ninguna
aterrizó" **en silencio** y el rezago dejará de bajar sin que nada se ponga
rojo en producción. Lo único que lo delata es
`TestResolutionChecker_DistingueLaQueAterrizoDeLaQueNo`, que lo comprueba
contra la base con control positivo. Si esa prueba empieza a saltarse o a
fallar, esto es lo primero que hay que mirar.
