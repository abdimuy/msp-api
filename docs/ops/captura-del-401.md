# Captura del 401 — qué empieza a guardarse y qué hay que vigilar

> Acompaña al binario que monta la captura de intentos fallidos **fuera** de la
> cadena de autenticación. **No** hay migración: la tabla y las columnas son las
> de siempre.

## Qué cambió

| Antes | Ahora |
|---|---|
| La captura corría sólo **dentro** de `authn.Handler`, así que un 401 cortaba la cadena y **ningún** rechazo de autenticación dejaba fila | Cada módulo capturado (`ventas`, `cobranza/pagos`, `visitas`) monta **dos** instancias: la de dentro, igual que siempre, y una fuera acotada al **401** |
| Un pago mandado con la sesión vencida desaparecía: ni Microsip, ni `MSP_PAGOS_RECIBIDOS`, ni la pantalla | Deja fila en `MSP_FAILED_INTENTS` con su cuerpo completo en disco |
| — | Esas filas van con `USUARIO_ID` **nulo**: el rechazo ocurre antes de saber quién era |

La fusión de la dedup ahora **adopta** el usuario: si la misma clave de
idempotencia vuelve autenticada, la fila del 401 se queda con el `USUARIO_ID`
del reintento y vuelve a poder reproducirse. Nunca al revés.

## El riesgo conocido: reintentos del 401 en `/v2/cobranza/pagos`

Es el único punto de esta entrega que puede crecer sin freno, y conviene
mirarlo la primera semana.

Tres hechos que se juntan:

1. El teléfono **reintenta el 401 sin tope**
   (`docs/module-standards/ENTREGA_GARANTIZADA.md:112` — `401 → REINTENTA` en
   toda combinación; `:172` — WorkManager sin tope de reintentos).
2. `POST /v2/cobranza/pagos` **no manda `Idempotency-Key`**: su idempotencia es
   `body.id`, dentro del cuerpo. Sin clave, la dedup del Store **no dispara**
   (`internal/platform/failedintent/firebird/store.go:76-78`) y el conciliador
   del janitor tampoco los alcanza, porque opera sobre claves.
3. Retención de una fila `new`: **90 días**.

Resultado: un teléfono con la sesión vencida y un comprobante de 3 MB deja
**una fila y una copia completa del cuerpo por cada intento**, hasta que
alguien lo reautentique. Es la misma clase de incidente que la dedup cerró en
su día —609 filas para 130 ventas, 685 archivos, 896 MB medidos en
producción—, reabierta en la rama donde la dedup no llega.

Además, esa escritura ocurre **sin credencial válida**: cualquiera que alcance
la ruta con `Content-Type: multipart/*` provoca una escritura a disco (hasta
`MaxMultipartBytes`) y una fila. Antes, sin token no había ni I/O. No hay
rate-limit en la cadena raíz.

### Por qué no se cerró en el mismo cambio

- **Deduplicar por lo que el módulo expone no sirve.** El único ancla que el
  `ResumenExtractor` de cobranza saca del cuerpo es el **id del cliente**
  (`internal/cobranza/infra/failedintents/resumen.go:54,74`), no el del pago.
  Usarlo como clave fundiría **dos pagos distintos del mismo cliente en una
  sola fila** — perder evidencia de dinero es peor que gastar disco.
- **Deduplicar por "una pendiente equivalente" es circular.** Sin clave, lo
  único que distingue dos capturas anónimas es el cuerpo, que es justo lo que
  se querría no escribir. Y el cuerpo no sirve de huella: el `boundary`
  multipart es aleatorio en cada petición, así que el mismo pago reintentado
  produce bytes distintos.

Cerrarlo de verdad pide una de dos decisiones que no son de este cambio: que la
app mande `Idempotency-Key` también en pagos (y entonces la dedup existente lo
resuelve sola), o un límite de tasa por IP/dispositivo delante de la captura —
el mismo lugar donde ya vive el *version gate* por esta misma razón
(`cmd/api/server.go`, "otherwise a fleet of outdated phones writes one
MSP_FAILED_INTENTS row per attempt").

### La consulta para vigilarlo

Filas de 401 pendientes por ruta, y cuánto pesan sus cuerpos:

```sql
SELECT PATH,
       COUNT(*)                         AS FILAS,
       COUNT(BODY_BLOB_PATH)            AS CON_BLOB,
       MIN(RECEIVED_AT)                 AS DESDE,
       MAX(COALESCE(LAST_SEEN_AT, RECEIVED_AT)) AS HASTA
  FROM MSP_FAILED_INTENTS
 WHERE HTTP_STATUS = 401
   AND STATUS = 'new'
 GROUP BY PATH
 ORDER BY 2 DESC;
```

Y quién las está generando (el `USUARIO_ID` es nulo por definición, así que el
agrupador útil es la ruta + el día):

```sql
SELECT CAST(RECEIVED_AT AS DATE) AS DIA, PATH, COUNT(*)
  FROM MSP_FAILED_INTENTS
 WHERE HTTP_STATUS = 401
 GROUP BY 1, 2
 ORDER BY 1 DESC;
```

Umbral de alarma sugerido para la primera semana: **más de ~200 filas con
`HTTP_STATUS = 401` en un día**, o el directorio de blobs
(`STORAGE_DIR/failed-intents`) creciendo más de 1 GB. Cualquiera de las dos
significa que hay un teléfono atorado, y el arreglo inmediato es que esa
persona vuelva a entrar a la app.

## Un efecto que se nota en el teléfono

Un pago rechazado por sesión vencida **ya no recibe el 401 al instante**: la
captura lee el cuerpo antes de soltar la respuesta, así que la subida se
completa primero. Es deliberado —leer antes de responder es lo que garantiza
que la evidencia quede entera— pero cambia el costo de red del caso más común
de sesión caducada.
