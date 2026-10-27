# Desempeño de cobradores — diseño (fase 1)

Fecha: 2026-10-14 · Estado: aprobado por el dueño (2026-10-14), incluidos los mocks v3
Repos: `msp-api` (la mayor parte), `sistema-cobro-web` (escritorio), `msp-app-kt` (solo el juego de casos compartido)

## 1. Propósito

Oficina analiza el trabajo diario de cada cobrador leyendo, en orden cronológico, los pagos y
visitas que registró: a qué hora empezó y terminó, dónde dejó de registrar (pausas), a qué
ventas ya se les puso algo y a cuáles no. Hoy lo hace en la pantalla "Pagos y visitas" (Inicio
del escritorio), que:

- lee del API Node legado (`/visitas/pagos-visitas`), sin token;
- filtra por **zona** con un select más un rango de fechas, y el primer día del rango queda fuera
  (`dateInit` con `endOf("day")`);
- atribuye el cobrador asignado hoy al cliente, no al que cobró;
- no muestra fotos, detalle, historial ni estado por venta.

La pantalla nueva sirve a dos lectores: las personas de oficina y el propio sistema, que debe
poder detectar solo lo que merece revisión.

### Decisiones del dueño (2026-10-13/14)

| # | Decisión |
|---|---|
| D1 | Una fila **por usuario** (usuario de Firestore `users`), nunca por cobrador ni por zona. |
| D2 | Todo lo de un usuario se limita a **la zona de ese usuario**, sin excepción. |
| D3 | Pagos y visitas juntos en orden cronológico (así analizan hoy). |
| D4 | Se ven y se filtran **todos** los pagos y visitas de la zona del usuario: filtrar, ordenar, agrupar, exportar. |
| D5 | Cobertura **por venta**: saber a cuáles ya se les puso pago o visita y a cuáles nada. |
| D6 | El estado por venta y la cobertura se marcan **igual que en el teléfono** del usuario (incluida la regla de alcance cliente: "No estaba" marca todas las ventas del cliente). |
| D7 | El umbral de pausa y el horario opcional van **en la consulta al API**; nada se guarda como configuración. |
| D8 | El tablero del equipo abre en **Hoy**, con selector a Semana. |
| D9 | Reemplaza a Inicio, con permiso propio "Desempeño de cobradores"; sin el permiso se ve un inicio sencillo. |
| D10 | **Toda la lógica en el servidor, siempre.** El escritorio solo pinta. |
| D11 | Se reutilizan los modales de Clientes (ficha del cliente, detalle de venta, detalle de pago). |
| D12 | El GPS continuo queda fuera por ahora. Promesas y citas sí deben llegar al servidor (fase 2). |
| D13 | Todo lo que se pueda, en paralelo. |
| D14 | El tablero **no muestra totales sumados de varios usuarios** (ni banda de resumen ni pies de tabla): solo cifras por usuario. El API del tablero tampoco los devuelve. |
| D15 | Estilo visual elegido en los mocks: **"Audaz 2026"** (https://claude.ai/artifact/8onDaCe8muASEe18vFB9vy). |
| D16 | La línea de actividad la elige cada quien en la pantalla (Puntos, Bloques, Por hora, Bloques + hora; predeterminado Bloques; se recuerda por computadora). Los cálculos de cada vista (tramos, conteos por hora) vienen del API. |
| D17 | **Popovers de información rápida** en nombre del usuario, estado ahora, tramos y pausas de la línea, celdas de pausas, pagos, cobertura y % cobro, chips de estado, fotos, señales y renglones de actividad. Su contenido viene del API (sin totales de varios usuarios). |

### Fuera de alcance

- Seguimiento GPS continuo (decisión D12). Las pausas se deducen de huecos entre actividades; la
  pantalla lo dice así: no distingue "detenido" de "en movimiento sin registrar".
- Crear, editar o borrar pagos o visitas desde esta pantalla: es solo lectura.
- Sanciones o nómina automáticas: las señales son "para revisar".
- Promesas y citas en pantalla hasta que exista la fase 2 (§9).
- Semanas pasadas en "Cuentas": solo la semana en curso de cada usuario. La Jornada y la
  Actividad sí sirven para cualquier fecha pasada porque cada actividad trae su hora.

## 2. Conceptos

- **Actividad**: un pago, una visita o una condonación. Tiene hora (la que registró el teléfono),
  hora de llegada al servidor (si existe), coordenadas (0,0 = sin dato), cliente, venta,
  quién la registró, número de fotos y, si es pago, importe, forma de cobro y estado de
  aplicación.
- **Jornada**: las actividades de un usuario en un día calendario (zona `America/Mexico_City`),
  con las pausas y señales calculadas.
- **Pausa**: hueco entre dos actividades consecutivas de la jornada con duración **≥ umbral**
  (`pausa_min`). Si llega horario (`desde_hora`, `hasta_hora`), se agregan "empezó tarde" (primera
  actividad después de `desde_hora` + umbral) y "terminó temprano" (última antes de
  `hasta_hora` − umbral).
- **Semana del usuario**: `[FECHA_CARGA_INICIAL del usuario, ahora]`, inclusiva en ambos extremos,
  igual que `VentanaCobro` del teléfono y que `rutas` (`app/cobranza_semanal.go`).
- **Universo de ventas**: el mismo que el teléfono sincroniza para la zona: ventas a crédito de
  la zona con saldo > 0, más las saldadas que recibieron pago dentro de la semana.
- **Estado por venta**: los 8 estados del teléfono (`PAGO`, `ABONO_PARCIAL`, `VISITE_VUELVO`,
  `PROMETIO_PROXIMA`, `SE_NEGO`, `CITA_A_UNA_HORA`, `NO_ESTABA`, `SIN_TOCAR`) con su trato,
  etiqueta, detalle y segmento (Sin visitar, Volver, Después, Pagados). Fuente:
  `msp-app-kt/core/common/.../cobranza/domain/EstadoCuentaDeriver.kt`, `TipoVisitaCatalogo.kt`,
  `feature/pagos/.../ui/EstadoCuentaUi.kt`, `SegmentoDeCobranza.kt`.
  - El dinero manda: abono de la semana = suma de pagos con forma de cobro ∈ {157, 158, 52569};
    condonación (137026) no cuenta como dinero; pagos rechazados (`DOCTO_CC_ID = -1`) no cuentan.
  - Sin dinero: la visita más reciente de la semana que aplique a la venta (las de alcance venta
    con esa venta, más las de alcance cliente del mismo cliente; en empate gana la de venta).
  - Sin nada: `SIN_TOCAR`.
- **Cobertura**: venta "con algo" si en la semana tiene al menos un pago (de cualquier forma,
  incluido rechazado o condonación) o una visita que le aplique según la regla anterior.
  "Faltan N de M" = ventas del universo sin nada.
- **Quién lo registró**:
  - pago capturado por la app: el usuario/cobrador que lo capturó (`MSP_PAGOS_RECIBIDOS.CREATED_BY`
    y `COBRADOR_ID`), no `CLIENTES.COBRADOR_ID`;
  - pago capturado directo en Microsip (sin fila en `MSP_PAGOS_RECIBIDOS`): "Oficina";
  - visita: `MSP_VISITAS.COBRADOR_ID`.
  - La fila de un usuario en el tablero cuenta solo lo que **ese** usuario registró. La pestaña
    Actividad muestra toda la zona, con la columna y el filtro "quién lo registró".

Diferencia esperada y correcta contra el teléfono: el teléfono solo conoce las visitas capturadas
en ese aparato; el servidor ve todas las de la zona. Con dos personas en la misma zona, el
escritorio da el estado real.

## 3. Arquitectura

### 3.1 API — módulo nuevo `internal/desempeno` (solo lectura)

Vertical slice según CLAUDE.md §2; referencia de forma: `internal/rutas`.

```
internal/desempeno/
  domain/          actividad.go, jornada.go (pausas, señales), estado_cuenta.go (port del
                   deriver del teléfono), tipo_visita.go (catálogo), cobertura.go, errors.go
  app/             tablero.go, jornada.go, actividad.go, cuentas.go, exportar.go
  ports/outbound/  actividades_repo.go, ventas_repo.go, usuarios_client.go (Firestore)
  infra/
    desempenofb/   consultas propias a Firebird (no reutiliza ni modifica las del sync)
    desempenohttp/ handlers Huma + rutas + DTOs
    clients/       adaptador de usuarios Firestore (reutiliza el de rutas vía contrato)
  desempeno_contracts.go
```

Reglas:
- **No toca el sync de cobranza**: consultas y constantes nuevas; no se modifican
  `pagoFromClause`, `selectPagoColsP` ni el `WHERE` de sync/digest/by-ids
  (`docs/COBRANZA-SYNC.md`).
- Lo que ya calcula `rutas` (% de cobro, aporte, atrasos) se consume vía su contrato, no se
  reimplementa.
- Dinero como string decimal en DTOs; fechas RFC3339 UTC (`DATETIME_HANDLING.md`).

### 3.2 API — cambios en módulos existentes

1. **Fotos de pagos** (`internal/cobranza`):
   - Arreglar `ObtenerImagenPago`: hoy ignora el pago (`_ = pagoID`,
     `app/adjuntar_imagen_pago.go:198`); debe exigir que la imagen pertenezca al pago (404 si no).
   - Separar permisos: lectura de fotos con permiso de lectura; subir/borrar fotos y crear pagos
     con un permiso de escritura nuevo. Hoy todo usa `cobranza:ver_pagos`.
2. **Fotos de visitas** (`internal/visitas`): `GET /v2/visitas/{id}/imagenes` (lista) y
   `GET /v2/visitas/{id}/imagenes/{img_id}` (binario con ETag, mismo patrón que pagos y ventas),
   verificando pertenencia. Repo: `ListImagenes`, `FindImagenByID`.
3. **Ligar pago con sus fotos**: el detalle de pago de clientes
   (`GET /v2/clientes/{id}/pagos/{doctoCcId}`) expone `pago_recibido_id` (UUID de
   `MSP_PAGOS_RECIBIDOS`) y `num_fotos`. Pagos sin fila en `MSP_PAGOS_RECIBIDOS` → `null`.
4. **Permisos nuevos**: `desempeno:ver` y el de escritura de cobranza (§6).

### 3.3 Escritorio — módulo nuevo `src/modules/desempeno`

Arquitectura hexagonal del escritorio (documento base `src/modules/ventasLocales/README.md`;
referencia de forma `src/modules/rutas`): `domain`, `application` (puerto + casos de uso),
`infrastructure` (adapter HTTP con `URL_API_V2`, DTOs, mappers, `apperrorToDomainError`),
`presentation` (container, contexto, hooks `{data, isLoading, error}`), `components`.

Reutiliza:
- de `modules/clientes`: `ClienteFicha`, `VentaModal`, `PagoModal` (este último gana la sección
  de fotos en lugar de "Sin comprobante"); se exportan desde el índice público del módulo.
- de `modules/ventasLocales`: galería y lightbox de fotos; `AuthenticatedImage` se generaliza
  para recibir un descargador en lugar de `ventaId/imagenId`.
- fechas con `src/utils/tiempoDeNegocio.ts`.

Alrededor:
- `/` pasa a ser esta pantalla con el módulo `DESEMPENO_COBRADORES` en `src/constants/modules.ts`;
  sin el permiso, un inicio sencillo.
- Se retiran `Home.tsx` viejo y `getPagosAndVisitas.ts` (Node).

## 4. Contratos del API

Todas piden `desempeno:ver`. Todo se limita a la zona del usuario `{uid}` (Firestore). Respuestas
con forma Huma y errores `apperror`.

### 4.1 Tablero
`GET /v2/desempeno/usuarios?periodo=hoy|semana&pausa_min=45[&desde_hora=08:00&hasta_hora=18:00]`

Por usuario: `uid, nombre, zona_id, zona_nombre, configurado (bool + motivo), estado_ahora
(activo|sin_actividad|no_ha_empezado) + desde, primera_actividad, ultima_actividad,
minutos_activos, pausas {n, minutos}, pagos {n, importe}, visitas {n}, condonaciones {n},
cobertura {con_algo, total}, pct_cobro, linea [{hora, tipo}]`.
- `estado_ahora`: activo si la última actividad es de hace menos de `pausa_min`; sin actividad si
  ya pasó el umbral; no ha empezado si hoy no hay actividad.
- `pausa_min` acotado (5–240); por defecto 45.
- Para las vistas de la línea y los popovers (D16, D17), cada fila trae además: `tramos [{desde,
  hasta, pagos {n, importe}, visitas, condonaciones, clientes}]`, `por_hora [{hora, pagos, visitas,
  condonaciones}]`, `pausas_detalle [{desde, hasta, minutos, anterior, siguiente}]`,
  `pagos_por_forma {efectivo, transferencia, cheque}` (solo esas tres; no existe "depósito"), `no_aplicados`, `cobertura_por_segmento`,
  `cobro {cobrado, esperado}` y `ultima {tipo, cliente, hora}`. Nunca totales de varios usuarios
  (D14).

### 4.2 Jornada
`GET /v2/desempeno/usuarios/{uid}/jornada?fecha=YYYY-MM-DD&pausa_min=45[&desde_hora&hasta_hora]`

`actividades[]` en orden (con lo de §2), `pausas[] {desde, hasta, minutos}`,
`senales[] {tipo, actividad_ids, detalle}`, `resumen`.

Señales ("para revisar"):
- `pausa_larga` (cada pausa);
- `inicio_tarde`, `fin_temprano` (solo con horario);
- `mismo_punto`: ≥ 4 actividades de clientes distintos dentro de 25 m en ≤ 10 min
  (umbrales como constantes con nombre);
- `captura_tardia`: llegada al servidor − hora del teléfono ≥ 2 h;
- `sin_gps`: coordenadas (0,0) o nulas.

### 4.3 Actividad de la zona
`GET /v2/desempeno/usuarios/{uid}/actividad?desde&hasta&tipo[]&forma_cobro[]&tipo_visita[]&registrado_por[]&monto_min&monto_max&con_fotos&con_gps&q&orden&dir&cursor&limit`

Página de actividades + `totales {n, importe}` del filtro completo. Paginación por cursor estable
`(hora, id)`. `orden` admite las columnas visibles. `limit` ≤ 500.

`GET /v2/desempeno/usuarios/{uid}/actividad.xlsx?<mismos filtros>`: exportación generada en el
servidor (tope de filas con mensaje si se excede).

### 4.4 Cuentas de la semana
`GET /v2/desempeno/usuarios/{uid}/cuentas?segmento=sin_visitar|volver|despues|pagados|sin_nada&q&orden`

Por venta: `venta_id (DOCTO_CC_ID), docto_pv_id, folio, cliente_id, cliente, producto, estado,
trato, etiqueta, detalle, segmento, con_algo, ultima_actividad, saldo, parcialidad,
num_atrasos, nueva`. Más conteos por segmento.

### 4.5 Detalles
- Pago: `GET /v2/clientes/{id}/pagos/{doctoCcId}` (+ `pago_recibido_id`, `num_fotos`) y
  `GET /v2/cobranza/pagos/{pago_recibido_id}/imagenes[/{img_id}]`.
- Visita: `GET /v2/visitas/{id}` y `GET /v2/visitas/{id}/imagenes[/{img_id}]`.
- Venta y cliente: los endpoints existentes de clientes.

## 5. Pantallas (escritorio)

Estructura; el estilo visual se elige con los mocks.

- **Tablero (Inicio)**: barra con Hoy/Semana, umbral (15/30/45/60/90), horario opcional,
  búsqueda y hora de actualización. Fila por usuario: nombre y zona con punto de estado, mini
  barra 7–20 h (pago, visita, condonación, pausas sombreadas), horas activas, pausas, pagos/$,
  visitas, "faltan N de M" con barra, % cobro. Ordenable. Se actualiza cada minuto. Clic →
  ficha. Usuario sin zona o sin inicio de semana: "sin configurar" con enlace a Configuración.
- **Ficha del usuario**: encabezado con zona, "desde jue 9 oct · hace 5 d", estado y números.
  - *Jornada*: día con flechas, eje horario, lista cronológica con pausas intercaladas
    ("Pausa · 1 h 12 min (11:05–12:17)"), mapa con puntos numerados, panel "Para revisar".
  - *Actividad*: tabla de toda la zona con filtros, orden, agrupar (día, cliente, venta, tipo),
    columnas, vistas guardadas, Excel; renglón con tipo, hora, cliente, venta, monto/resultado,
    quién, fotos, GPS.
  - *Cuentas*: pastillas Sin visitar, Volver, Después, Pagados y "Sin nada"; lista por venta con
    chip de estado (ícono + texto + color del teléfono), saldo, atrasos, última actividad.
- **Modales**: pago (de Clientes, con fotos), visita (nuevo, mismo estilo), venta y cliente (de
  Clientes). Fotos en el lightbox de Ventas Locales.
- **Estados**: esqueletos, error con "Reintentar", vacío, sin configurar, aviso en pantalla angosta.

## 6. Errores, casos raros y seguridad

- Pagos sin `pago_recibido_id` (anteriores a 2.18.0, Microsip, legado): "Sin comprobante".
- Pago no aplicado o rechazado: se muestra con su estado real; no cuenta como dinero; sí como
  cobertura.
- Captura sin señal: se usa la hora del teléfono; señal `captura_tardia` si aplica.
- Actividad sin GPS: en lista y tabla, no en el mapa.
- Usuario que cambió de zona a media semana: se usa la zona actual (como el teléfono). Aviso
  solo si existe el dato de desde cuándo; si no existe, no se muestra.
- Firestore caído: error con "Reintentar"; nunca zonas o semanas inventadas.
- Permisos:
  - consultas nuevas: `desempeno:ver`;
  - fotos de pagos y visitas: lectura con permiso de lectura y verificación de pertenencia;
  - escritura de cobranza (crear pago, subir y borrar foto): permiso nuevo. **Despliegue en dos
    pasos**: primero se concede a todos los roles que hoy crean pagos desde el teléfono y se
    mide que todos lo tengan (control positivo: un usuario de la flota crea un pago en dev); solo
    después se exige. Si se invierte el orden, la flota deja de poder mandar pagos.
- Dinero (CLAUDE.md §8): la pantalla muestra dinero y no lo mueve. Antes de darla por buena se
  mide en producción, solo lectura, que cobrado por usuario y día, % de cobro y cobertura
  coincidan con Cobranza semanal (`/v2/rutas/...`) y con Microsip. Cada afirmación en
  `docs/evidencia/cobranza.md` con identificador nuevo (`E-COB-020` en adelante).
- Ambigüedad por medir antes de implementar: `rutas` cuenta solo el concepto 87327; el sync y Node
  incluyen también 27969. El estado por venta usa formas de cobro (157/158/52569), no conceptos.
  Se documenta cuál cifra muestra cada columna.
- Por medir en producción (solo lectura) antes de las consultas: índices de `MSP_VISITAS` por
  zona y fecha (`RDB$INDICES`); si faltan, migración que agrega el índice (permitido por §1).

## 7. Pruebas

- **Dominio Go**: pausas (umbral exacto 44/45 min, bordes del horario, una sola actividad, día
  vacío, medianoche), señales con sus constantes, cobertura (alcance cliente marca todas las
  ventas; pago solo su venta; rechazado cuenta como algo y no como dinero).
- **Juego de casos compartido** (estado por venta): archivos JSON en un directorio versionado
  (`testdata/estado-cuenta/*.json` en msp-api, copia byte a byte en msp-app-kt) con entrada y
  resultado esperado. Corren en Go (`internal/desempeno/domain`) y en Kotlin
  (`core/common` tests). Cada repo guarda el hash SHA-256 del juego en `testdata/estado-cuenta/VERSION`
  y una prueba falla si los archivos no corresponden a ese hash: cambiar un caso obliga a
  actualizar el hash, y el mismo hash en los dos repos demuestra que el juego es idéntico.
- **Integración** (Firebird de desarrollo, `fbtestutil.WithTestTransaction`, datos sembrados con
  `microsipseed`): filtros, orden, paginación, totales, exportación, aislamiento por zona.
- **Seguridad**: foto de otro pago o visita → 404; sin permiso → 403; con permiso de lectura no se
  crea pago ni se borra foto.
- **Escritorio**: patrón de `rutas` (puerto falso, hooks, adapters, mappers con MSW), componentes
  de las tres pestañas y estados.
- **Medición final** en producción, solo lectura (§6).

## 8. Orden de entrega y paralelismo

```
Carril A (API, seguridad)   A1 permiso de escritura concedido y medido → A2 exigirlo + arreglo de pertenencia
Carril B (API, fotos)       B1 fotos de visitas · B2 pago_recibido_id en detalle de pago
Carril C (API, desempeño)   C0 mediciones (índices, 87327 vs 27969) → C1 dominio + casos compartidos
                            → C2 consultas Firebird → C3 endpoints + exportación → C4 medición de cifras
Carril D (escritorio)       D1 módulo con puerto falso y componentes (contra contratos de §4)
                            → D2 adapter real → D3 reemplaza Inicio detrás del permiso
Carril E (app)              E1 casos compartidos corren en Kotlin
```

- A, B, C y D arrancan en paralelo: D trabaja contra los contratos de §4 con un puerto falso.
- Dependencias duras: A2 después de A1 medido; D2 después de C3 y B; D3 después de C4.
- Despliegue: API primero (A1, luego B y C, luego A2), escritorio después. Pantalla primero solo
  para el dueño; luego reemplaza Inicio.

## 9. Fase 2 (spec propia): promesas y citas al servidor

Las promesas y citas solo viven en Room (`PROMESA_FECHA`, `PROMESA_MONTO_CENTAVOS`,
`PROMESA_VENTA_ID`, `CITA_FECHA`, `CITA_HORA`); ni `CrearVisitaBody` ni `MSP_VISITAS` las llevan.
Fase 2: columnas nuevas opcionales en `MSP_VISITAS`, el API las acepta y devuelve, la app las
manda (API primero, app después; teléfonos viejos siguen funcionando). Cuando existan, el estado
por venta del servidor distingue Diferido y Cita como el teléfono, sin rediseño de pantalla.

## 10. Notas de los mocks (v3)

- El mock muestra "Depósito" como forma de pago y el modelo del teléfono en el popover del nombre:
  ninguno de los dos entra. Formas de pago: efectivo, transferencia y cheque (más condonación
  aparte). El modelo del teléfono no está en el servidor y queda fuera.
- Los popovers de la línea de actividad se abren con el ratón; los de nombre y celdas también con
  teclado.
