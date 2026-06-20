# Diseño — Reporte PDF de cliente (info + ventas con pagos)

**Fecha:** 2026-06-20
**Módulo:** `internal/clientes` (msp-api)
**Estado:** aprobado en brainstorming, pendiente de plan de implementación

## Objetivo

Generar, **en la API (Go puro)**, un PDF imprimible con la información de un cliente y sus
ventas con los pagos de cada una. El resultado debe verse **muy profesional** (tipografía y
estilo) — requisito no negociable. Es un reporte de análisis de oficina, factual (sin gráficas).

## No-objetivos (YAGNI)

- Sin gráficas (heatmap / comparativa) en el PDF — solo tablas y texto.
- Sin analítica (scores de crédito/recompra/CLV, puntualidad/riesgo).
- Sin HTML→PDF / navegador headless (viola el stack Windows Server legacy sin binarios externos).
- Sin generación en el frontend (se pidió explícitamente "en la API").
- Sin batch query optimizado de pagos (clientes tienen 1-3 ventas/año; N+1 es trivial). Se anota
  como mejora futura si el volumen crece.

## Decisiones del brainstorming

1. **Contenido:** datos del cliente + **resumen financiero** (total comprado, abonado, saldo, %
   liquidado, # ventas, # pagos) + por cada venta su encabezado (folio, fecha, almacén, total,
   estado liquidada/debe + saldo) y su tabla de pagos.
2. **Formato:** solo tablas/texto (sin gráficas).
3. **Alcance:** por defecto **todas** las ventas del cliente; con opción de incluir solo ventas
   específicas vía query `venta=<doctoPvId>` repetible.
4. **Estilo:** "Moderno minimal" — sans geométrica + cifras alineadas + acentos de color sutiles.
5. **Generación:** server-side, librería pure-Go (`go-pdf/fpdf`), fuentes TTF embebidas.

## Sistema visual (NO NEGOCIABLE)

**Tipografías (OFL, **TTF estáticos** embebidos vía `go:embed` + `fpdf.AddUTF8FontFromBytes`):**
- **Títulos / masthead / encabezados / cuerpo:** **Poppins** (Regular / SemiBold / Bold) —
  geométrica, moderna.
- **Cifras (dinero, fechas, conteos):** **IBM Plex Mono** (Regular / Medium), monoespaciada →
  alineación tabular real; además **alineadas a la derecha** en columnas numéricas.
- Unicode nativo por embeber TTF → acentos y ñ correctos sin el truco cp1252.
- Nota: se eligieron Poppins + IBM Plex Mono (no Manrope/Inter) porque estas últimas solo se
  publican como *variable fonts*, que fpdf no embebe de forma confiable. Misma dirección
  "moderno minimal". Archivos en `internal/clientes/infra/clientespdf/fonts/` (+ `OFL.txt`).

**Color (sobrio, acentos mínimos):**
- Tinta principal casi-negra `#1A1A1A` sobre blanco.
- Reglas/hairlines gris claro `#E5E7EB`.
- Un acento neutro-oscuro (p.ej. slate/indigo `#334155`) para el masthead, etiquetas de sección
  y la regla superior.
- Semánticos solo donde aportan: estado **liquidada** en verde `#16A34A`, **debe** en ámbar
  `#D97706`. Nada de color decorativo.

**Página y retícula:**
- Tamaño **Carta (Letter, 215.9×279.4 mm)** — estándar en México.
- Márgenes ~18 mm; ancho de contenido único, jerarquía por tamaño/peso y whitespace generoso.

**Layout (de arriba a abajo):**
1. **Masthead:** "MUEBLERÍA MSP" en label mayúsculas tracked (pequeño) + regla de acento; título
   "Reporte de cliente" en Manrope bold grande; a la derecha, fecha de generación (y folio de
   reporte opcional).
2. **Bloque cliente:** nombre del cliente (destacado) + datos en pares clave/valor a 2 columnas
   (ID, dirección, teléfono, zona/cobrador — según expone `ObtenerCliente`).
3. **Resumen financiero:** fila compacta de métricas (Comprado · Abonado · Saldo · % liquidado ·
   # ventas · # pagos) separadas por hairlines; cifras a la derecha.
4. **Por cada venta:** encabezado de sección (Folio · Fecha · Almacén · Total · **estado**
   liquidada/debe + saldo) seguido de la **tabla de pagos**: columnas `Fecha · Concepto ·
   Cobrador · Importe`, filas con hairline (o zebra muy sutil), `Importe` a la derecha, y una
   fila de **subtotal** (total abonado de esa venta). Si una tabla no cabe, salto de página
   automático y el encabezado de la venta se repite con la nota "(continúa)".
5. **Pie:** "Mueblería MSP" · "Página X de Y" · timestamp de generación, en gris tenue.

**Formatos:** dinero MXN `$1,200.00` (es-MX, 2 decimales); fechas `12 mar 2024` (es-MX).

## Arquitectura (vertical slice en `internal/clientes`)

**Endpoint** — `GET /clientes/{id}/reporte`
- Respuesta `application/pdf`, `Content-Disposition: inline; filename="reporte-cliente-{id}.pdf"`.
- Query opcional `venta=<doctoPvId>` (repetible): subconjunto de ventas; ausente ⇒ todas.
- **Ruta chi cruda** registrada en `MountRouter` sobre el mismo `r chi.Router` (la
  autenticación ya está aplicada por middleware; el handler lee `auth.CurrentUser`). No pasa por
  Huma (evita la serialización JSON). Queda fuera del OpenAPI auto-generado por Huma (aceptable;
  se documenta en el README/openapi manual si hace falta).

**App** — `internal/clientes/app`
- `GenerarReporteCliente(ctx, clienteID int, ventaIDs []int) (outbound.ReporteCliente, error)`:
  arma el read-model reutilizando métodos de repo existentes (cliente, resumen, ventas, pagos por
  venta). Si `ventaIDs` no vacío, filtra; si vacío, todas. Error de cliente inexistente se
  propaga como `apperror` mapeable a 404.

**Infra**
- `internal/clientes/infra/clientespdf/render.go`: `Render(rep outbound.ReporteCliente, gen time.Time) ([]byte, error)` — pura presentación con `fpdf`. `gen` (timestamp de generación) inyectado para que los tests sean deterministas. Carga las fuentes embebidas una vez.
- `internal/clientes/infra/clientespdf/fonts/`: TTFs OFL embebidos (`//go:embed`).
- Handler en `clienteshttp`: parsea `{id}` + `venta[]`, llama app, renderiza, hace stream con los headers; errores → 404 / 400 / 500 vía el mapeo de errores del proyecto.

**Read-model** (en `ports/outbound`, mismo patrón que `VentaDetalle`, sin entidades de dominio
nuevas):
```
ReporteCliente {
  Cliente  <datos del cliente: nombre, id, dirección, teléfono, zona/cobrador>
  Resumen  <ResumenFicha: comprado, abonado, saldo, pctLiquidado, numVentas, numPagos>
  Ventas   []ReporteVenta
}
ReporteVenta { DoctoPvID, Folio, Fecha, Almacen, Total, Saldo, Liquidada bool, Pagos []ReportePago }
ReportePago { Fecha, Concepto, Cobrador, Importe }
```

## Manejo de errores
- Cliente inexistente → 404 (mensaje en español).
- `venta` con formato inválido → 400.
- Fallo de render → 500 (log en español para soporte; cuerpo genérico).

## Tests
- **Unit (app, repo fake):** estructura correcta del read-model; filtrado por `ventaIDs`
  (subconjunto vs todas); cliente inexistente → error esperado.
- **Infra pdf:** `Render` con `gen` fijo produce bytes que empiezan con `%PDF`, tamaño no
  trivial, sin error; idempotente para la misma entrada (timestamp fijo). Verifica que las
  fuentes embebidas cargan.
- **Handler:** 200, `Content-Type: application/pdf`, `Content-Disposition` esperado, cuerpo
  `%PDF`; 404 para cliente inexistente.
- **Integración read-only (opcional, `WithTestTransaction`):** genera el reporte de un cliente
  real de dev y asegura que produce un PDF válido no vacío.

## Dependencias y stack
- Agregar `github.com/go-pdf/fpdf` (pure Go) a `go.mod`. Verificar cross-compile
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`.
- Fuentes: Manrope e Inter, ambas **OFL** (permiten embeber/redistribuir) — se versionan los
  `.ttf` en el repo bajo `infra/clientespdf/fonts/` con su `OFL.txt`. Incremento de binario
  ~0.8-1.2 MB (aceptable).

## Supuestos / a confirmar en el plan
- Campos exactos del cliente que expone `ObtenerCliente` (nombre, dirección, teléfono, zona,
  cobrador) — se pinta lo disponible; los faltantes se omiten con gracia.
- Pagos por venta: se reutiliza el método existente que ya devuelve los movimientos enriquecidos
  (concepto + cobrador) de una venta. Si conviene, se acota a movimientos relevantes para el
  reporte.
- Nombre de la mueblería en el masthead: "Mueblería MSP" (configurable).
