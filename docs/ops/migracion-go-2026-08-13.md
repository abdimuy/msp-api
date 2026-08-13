# Migración al API Go — registro del cutover del 2026-08-13

Qué es esto: el **registro de lo que realmente pasó** la madrugada del
2026-08-13, cuando la operación pasó del Node legacy al API Go, y la guía de
dónde mirar cuando algo falle. El diseño está en
[`docs/superpowers/specs/2026-08-12-coexistencia-go-node-design.md`](../superpowers/specs/2026-08-12-coexistencia-go-node-design.md);
esto es el diario de a bordo. Van a seguir saliendo bugs de esta migración y
este documento existe para que el conocimiento no viva solo en una
conversación.

Documentos hermanos: [`legacy-api-notas.md`](legacy-api-notas.md) (cómo no
tirar el Node) e [`inventario-cutover.md`](inventario-cutover.md).

---

## 1. Topología de los tres sistemas

| | Node legacy | Go producción | Imágenes | Meilisearch |
|---|---|---|---|---|
| Puerto | `3001` | `3011` | `3010` | `7700` |
| Ruta | `C:\projects\sys_msp_backend` (corre desde `dist/`) | `C:\msp-api\` | — | — |
| Base | `MUEBLERA_SNP.FDB` + MongoDB (sync 30 s) | `MUEBLERA_SNP.FDB` | — | índice propio |
| Público | `msp2025.loclx.io` | `apidev.loclx.io` | `mspimagenes.loclx.io` | interno |

**`apidev.loclx.io` ya no es el túnel de pruebas: ES producción.** No se
consiguió un túnel adicional, así que el que servía al entorno de pruebas se
reasignó al Go de producción. A cambio, el entorno de pruebas remoto quedó
retirado — el flavor `devserver` de la app apunta a un host inválido a
propósito, para que ningún APK de prueba en campo pueda escribir pagos en la
base de producción. Para probar contra un servidor se usa `devlocal`, que toma
host y puerto de `local.properties`.

La base migró con 52 migraciones aplicadas.

Arranque: `LEVANTAR-SERVICIOS.bat` en el servidor, o `run.bat` a mano por
servicio. **`msp-api-prod` todavía no tiene disparador de arranque** — ver §4.

> El invariante que ordenó todo el cutover sigue vigente: *nunca dos sistemas
> escribiendo el mismo dato*. Cuando un dominio migra, se apaga el del Node
> **antes** de encender el del Go, nunca solapado.

---

## 2. Qué consume cada cliente

No hay proxy inverso y no lo habrá. Cada cliente elige su API por
configuración.

### App Android (2.14.0 en adelante)

| Función | API | Interruptor |
|---|---|---|
| Ventas locales | Go `POST /v2/ventas` | ninguno — siempre Go |
| Pagos (captura) | Go `POST /v2/cobranza/pagos` | `PAGOS_USE_V2 = true` |
| Visitas (alta) | Go `POST /v2/visitas` | `VISITAS_USE_V2 = true` |
| Sync de cobranza (ventas, pagos, saldos, SSE) | Go `/v2/cobranza/sync/*` | ninguno |
| Todo lo demás | Node | `LEGACY_BASE_URL` es el default de `ApiProvider` |

El Node sigue sirviendo `getAllVentasByZona` — hoy ya solo para garantías y sus
eventos; ventas, pagos y productos dejaron de bajar por ahí — y
`visitas/pagos-visitas`.

### Escritorio

Estrena el Go en la **1.14.1**. Antes autenticaba contra el Firebase de
desarrollo: `firebase.ts` tenía los valores de dev commiteados. Ya está
parametrizado con fallback a producción.

---

## 3. Catálogo de trampas, con su síntoma

La parte que más va a servir cuando salga el próximo bug. Ordenado por dónde
se manifiesta.

| Síntoma | Causa real |
|---|---|
| NPE ofuscado (`g3.u.X`) en un merge de sync | Gson ignora los defaults de Kotlin: un campo no-nulo ausente en el JSON llega `null`. El servidor no mandaba `productos` porque **el binario salió de una rama sin ese commit** |
| `firebase_token_wrong_audience` | La API de pruebas se apoderó del `3011`; valida contra `msp-dev-96ff5` en vez del proyecto de producción |
| 401 suelto al abrir una pantalla | `auth.currentUser?` dispara la petición antes de que Firebase restaure la sesión |
| 403 en todo tras entrar | Usuario aprovisionado sin rol. El catálogo se salta si `MSP_USUARIOS` está vacía |
| 500 de 8–21 s en el sync | Espera del pool de Firebird. Conocido, ~0.6% con reintento |
| El arranque muere con log vacío | `fx.NopLogger` se traga el error de config. Con `APP_ENV=production`, `MEILISEARCH_URL` es obligatoria y no tiene escotilla |
| El escritorio autentica contra dev | `firebase.ts` con valores de dev commiteados |
| Un servicio "ya estaba arriba" pero no responde | El lanzador comprueba el **puerto**, no **quién** lo ocupa |
| Cxc.exe truena al cobrar | Procedimientos nuestros sin `GRANT` a los usuarios de Microsip (migs 000051–000053) |

---

## 4. Bugs posteriores al cutover

### 4.1 Todos los totales del cobrador al doble (2026-08-13, resuelto en app 2.15.0)

**Síntoma.** Al actualizar, cada cobrador vio sus números exactamente al doble.
Mismo teléfono, antes y después:

| | ayer | hoy | factor |
|---|---|---|---|
| Total semanal | $37,400 | $74,800 | 2.00 |
| Pagos | 190 | 380 | 2.00 |
| Porcentaje de cuentas | 60.51% | 121.02% | 2.00 |

El dinero en `MSP_PAGOS_RECIBIDOS` y en Microsip siempre estuvo bien: lo que
sumaba doble era la vista local del teléfono.

**Causa.** Los dos canales guardan el mismo pago con llaves distintas, y esa
llave es la clave primaria de `Payment` en Room:

```
Node    ID = MSP_PAGOS_RECIBIDOS.ID           (UUID de la captura), o
             "<DOCTO_CC_ID>-<IMPTE_DOCTO_CC_ID>"  si se capturó en oficina
Go v2   ID = IMPTE_DOCTO_CC_ID                 (numérico puro)
```

La app ya colapsaba el gemelo, pero por `pago_recibido_id` — y **ese campo es
NULL para todo el histórico**. El backend Go lo resuelve con

```sql
(SELECT MIN(pr.ID) FROM MSP_PAGOS_RECIBIDOS pr
  WHERE pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID)
```

y **el Node nunca escribió `MSP_PAGOS_RECIBIDOS.IMPTE_DOCTO_CC_ID`**: su propio
query enlazaba esa tabla por `DOCTO_CC_ID`. Medido sobre la base: 348,012 de
348,012 filas con esa columna en NULL. Sobre una semana real de 6,689 pagos, el
Go manda `pago_recibido_id` NULL en 6,689 de 6,689 — el 100% falla el colapso,
de ahí el factor 2.00 exacto en vez de un puñado de duplicados.

**Arreglo** (app 2.15.0, `CobranzaSyncManager` + `PaymentDao`). El gemelo se
ubica por `DOCTO_CC_ID`, el documento de pago, que sí es el mismo en ambos
canales:

- `mergePagos` borra la fila legacy de cada documento que baja.
- Una purga one-time limpia el histórico que el sync incremental ya no vuelve a
  mandar, y **solo donde el gemelo numérico ya está en local**: un pago legacy
  sin gemelo es histórico real fuera de ventana, y borrarlo desplomaría los
  totales en vez de arreglarlos.

Tres cerrojos: `GUARDADO_EN_MICROSIP = 1` (una captura pendiente de subir jamás
se toca), `ID LIKE '%-%'` (la fila canónica del canal v2 es numérica pura) y
`DOCTO_CC_ID > 0`.

**La trampa transferible:** cuando dos canales sirven la misma entidad, la
pregunta no es "¿migré los datos?" sino **"¿la identidad del registro es la
misma en los dos?"**. Aquí no lo era, y el cliente usa esa identidad como clave
primaria. Antes de apagar un canal, comparar el ID que emite cada uno para la
misma fila.

---

## 5. Qué queda pendiente

- **Cobro de prueba en Microsip sin verificar.**
- **Desinstalar los APK `msp-app DEV` que quedaron en campo.** Su flavor ya
  apunta a un host inválido, pero conviene sacarlos.
- **2 usuarios sin rol** (403 en todo hasta que se les asigne).
- **Los de oficina sin `analytics:*` ni `config:administrar`.**
- **`msp-api-prod` sin disparador de arranque:** si el servidor se reinicia, no
  levanta solo.
- **Los 500 del pool de Firebird** (8–21 s, ~0.6% con reintento).

---

## 6. Reglas aprendidas

1. **Verificar el contrato entre app y API en la rama que se compila**, no en
   `main` ni en el archivo fuente que tienes abierto. El bug de `productos`
   salió de un binario construido desde una rama sin ese commit.
2. **Leer el esquema real de la base**, no la primera migración que lo creó.
   Las columnas se agregan después y las que nadie llenó siguen en NULL — como
   `IMPTE_DOCTO_CC_ID`, que costó el 2× de §4.1.
3. **Comprobar el bundle compilado, no el archivo fuente.** El escritorio
   autenticaba contra dev con el fuente ya corregido.
4. **Deshabilitar, no solo detener**, las tareas con disparador de arranque. Un
   servicio detenido revive en el próximo reinicio y se apodera del puerto.
5. **Un puerto ocupado no es un servicio sano.** Comprobar *quién* responde, no
   que algo responda.
6. **Cuando dos sistemas sirven la misma entidad, comparar identidades**, no
   solo contenidos (§4.1).
