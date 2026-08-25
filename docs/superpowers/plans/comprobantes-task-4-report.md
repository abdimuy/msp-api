# comprobantes-task-4 — Reporte de ejecución

Fecha: 2026-08-20
Estado: **ENTREGADO**
Rama: `feat/comprobantes-domain` → PR #12

## Contenido entregado

### 1. Modelos de contenido (aprobados por líder)

| Archivo | Tipos | Estado |
|---|---|---|
| `comprobante_venta.go` | `ComprobanteVenta` (VO compuesto, 10 campos), `ArticuloComprobante` (VO hijo, 4 campos) | Aprobado |
| `comprobante_pago.go` | `ComprobantePago` (VO compuesto, 8 campos) | Aprobado |

Cada tipo tiene constructor validado (`New*`), hydrate sin validación (`Hydrate*`), params struct, y getters con accessors privados. `ArticuloComprobante` se revalida dentro del constructor padre.

### 2. Entidad Envio (rework post-review)

| Archivo | Contenido |
|---|---|
| `envio.go` | Entidad Type B pipeline: `CrearEnvio` (birth-only sin_telefono), `HydrateEnvio`, 5 transiciones de estado |
| `estado_envio.go` | `EstadoEnvio` + mapa de transiciones válidas (sin sin_telefono como transición) |
| `errors.go` | 21 sentinels via `apperror.New*` |
| `envio_test.go` | 14 funciones Test* (tabla-driven, cobertura de la matriz de treinta pares) |
| `estado_envio_test.go` | 6 funciones Test*: parse, helpers exhaustivos, CanTransitionTo, constants |
| `tipo_comprobante_test.go` | 5 funciones Test*: parse, isValid, constants |
| `canal_test.go` | 5 funciones Test*: parse, isValid, constants |
| `comprobante_venta_test.go` | 7 funciones Test*: happy path, validation, defensive copy, hydrate, ArticuloComprobante |
| `comprobante_pago_test.go` | 3 funciones Test*: happy path, validation, hydrate |

#### Cambios del rework (post-review PR #12)

1. **Constructor renombrado** `NewEnvio` → `CrearEnvio` con `now time.Time` como parámetro
2. **Restricción de nacimiento**: solo `en_espera` (telefono != nil) o `sin_telefono` (telefono == nil)
3. **`MarcarSinTelefono()` eliminada**: sin_telefono es estado de nacimiento, no objetivo de transición
4. **`MarcarEnviado(mensajeExternoID string, now time.Time)`**: recibe ambos parámetros desde el caller
5. **Todos los métodos de transición reciben `now time.Time`**: determinismo en pruebas
6. **Nueva validación**: `clienteID > 0` → `ErrEnvioClienteIDInvalido`
7. **sin_telefono eliminada del mapa de transiciones**: en_espera ya no transiciona a sin_telefono

#### Segunda ronda (B/C/D findings)

| Finding | Fix |
|---|---|
| B — `MensajeExternoID` huérfano en `CrearEnvioParams` | Eliminado del struct de creación, se mantiene en `HydrateEnvioParams` |
| C — Coverage bajó a 99.4% por getter sin cubrir | Assertion `ProgramadoPara()` agregada en happy path |
| D — Transiciones solo probadas desde algunos estados | Tests reescritos como tablas: cada transición desde **todos** los estados |
| gofmt alignment | `gofmt -w` corrigió alineación de `CrearEnvioParams` |

### 3. Matriz de los treinta pares

5 transiciones × 6 estados = 30 casos. Cada transición se prueba desde **todos** los estados posibles:

| Transición | Desde | Resultado esperado |
|---|---|---|
| `Reclamar` | en_espera | ✅ enviando |
| `Reclamar` | sin_telefono, enviando, enviado, fallido, detenido | ❌ `ErrEnvioTransicionInvalido` + estado sin mutar |
| `MarcarEnviado` | enviando | ✅ enviado + mensajeExternoID + enviadoEn |
| `MarcarEnviado` | en_espera, sin_telefono, enviado, fallido, detenido | ❌ `ErrEnvioTransicionInvalido` + estado sin mutar |
| `MarcarFallido` | enviando | ✅ fallido + ultimoError |
| `MarcarFallido` | en_espera, sin_telefono, enviado, fallido, detenido | ❌ `ErrEnvioTransicionInvalido` + estado sin mutar |
| `Reenviar` | fallido | ✅ en_espera + intentos++ + ultimoError=nil |
| `Reenviar` | en_espera, sin_telefono, enviando, enviado, detenido | ❌ `ErrEnvioTransicionInvalido` + estado sin mutar |
| `Detener` | en_espera | ✅ detenido + detenidoPor |
| `Detener` | sin_telefono, enviando, enviado, fallido, detenido | ❌ `ErrEnvioTransicionInvalido` + estado sin mutar |

Cada caso negativo verifica que el estado **no se muta** después del intento fallido.

## Sentinel errors (21)

| # | Sentinel | Código | Dominio |
|---|---|---|---|
| 1 | `ErrTipoComprobanteInvalido` | receipt_type_invalid | Envio |
| 2 | `ErrEstadoEnvioInvalido` | receipt_delivery_state_invalid | Envio |
| 3 | `ErrCanalInvalido` | receipt_channel_invalid | Envio |
| 4 | `ErrComprobanteVentaFolioRequerido` | receipt_comprobante_venta_folio_requerido | ComprobanteVenta |
| 5 | `ErrComprobanteVentaClienteRequerido` | receipt_comprobante_venta_cliente_requerido | ComprobanteVenta |
| 6 | `ErrComprobanteVentaTotalNegativo` | receipt_comprobante_venta_total_negativo | ComprobanteVenta |
| 7 | `ErrComprobanteVentaEngancheNegativo` | receipt_comprobante_venta_enganche_negativo | ComprobanteVenta |
| 8 | `ErrComprobanteVentaSaldoNegativo` | receipt_comprobante_venta_saldo_negativo | ComprobanteVenta |
| 9 | `ErrComprobanteVentaSinArticulos` | receipt_comprobante_venta_sin_articulos | ComprobanteVenta |
| 10 | `ErrArticuloDescripcionRequerida` | receipt_articulo_descripcion_requerida | ArticuloComprobante |
| 11 | `ErrArticuloCantidadNegativa` | receipt_articulo_cantidad_negativa | ArticuloComprobante |
| 12 | `ErrArticuloPrecioUnitarioNegativo` | receipt_articulo_precio_unitario_negativo | ArticuloComprobante |
| 13 | `ErrArticuloImporteNegativo` | receipt_articulo_importe_negativo | ArticuloComprobante |
| 14 | `ErrComprobantePagoFolioRequerido` | receipt_comprobante_pago_folio_requerido | ComprobantePago |
| 15 | `ErrComprobantePagoClienteRequerido` | receipt_comprobante_pago_cliente_requerido | ComprobantePago |
| 16 | `ErrComprobantePagoVentaFolioRequerido` | receipt_comprobante_pago_venta_folio_requerido | ComprobantePago |
| 17 | `ErrComprobantePagoMontoNegativo` | receipt_comprobante_pago_monto_negativo | ComprobantePago |
| 18 | `ErrComprobantePagoSaldoRestanteNegativo` | receipt_comprobante_pago_saldo_restante_negativo | ComprobantePago |
| 19 | `ErrEnvioTransicionInvalido` | receipt_envio_transicion_invalida | Envio |
| 20 | `ErrEnvioReferenciaRequerido` | receipt_envio_referencia_requerido | Envio |
| 21 | `ErrEnvioClienteIDInvalido` | receipt_envio_cliente_id_invalido | Envio |

## Salida de comandos

### gofmt (solo nuestros archivos)

```
(7 archivos preexistentes con CRLF local — no afectan CI en Linux)
```

### go vet

```
(errores de compilación: ninguno)
```

### go build

```
(errores de compilación: ninguno)
```

### golangci-lint (solo cambios nuevos)

```
0 issues.
```

### go test -race

```
ok  github.com/abdimuy/msp-api/internal/comprobantes/domain  2.809s  coverage: 100.0% of statements
```

### Coverage total

```
total:  (statements)  100.0%
```

Requisito mínimo: 99%. Resultado: **100.0%** ✓

## Archivos del dominio (10)

```
internal/comprobantes/domain/
  canal.go                    Canal (local, whatsapp_business)
  canal_test.go               5 Test*
  comprobante_pago.go         ComprobantePago (8 campos)
  comprobante_pago_test.go    3 Test*
  comprobante_venta.go        ComprobanteVenta (10 campos) + ArticuloComprobante (4 campos)
  comprobante_venta_test.go   7 Test*
  doc.go                      package doc
  estado_envio.go             EstadoEnvio (6 estados) + mapa de transiciones
  estado_envio_test.go        6 Test*
  envio.go                    Envio (Type B pipeline, 14 campos)
  envio_test.go               14 Test* (30 pares de transición)
  errors.go                   21 sentinels
  tipo_comprobante.go         TipoComprobante (venta, pago)
  tipo_comprobante_test.go    5 Test*
```

Total: **40 funciones Test***, ejecutando 144 `=== RUN` (incluye subtests de tablas).
