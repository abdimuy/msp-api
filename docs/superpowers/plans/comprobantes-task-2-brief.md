# Comprobantes — Tarea 2: value objects, catálogos y errores

> **Rama:** `feat/comprobantes-domain` (ya creada, sacada de `feat/comprobantes`)
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)

## Dónde encaja

El módulo de comprobantes manda al cliente un PDF por WhatsApp cuando su venta se registra en Microsip y cuando su pago se aplica. Esta tarea construye **los tipos base del dominio**: los que representan un valor con un conjunto cerrado de opciones válidas.

**Es la tarea que bloquea al resto del dominio.** La entidad `Envio` y su máquina de estados se construyen encima de estos tipos.

No hay base de datos, no hay HTTP. Es Go puro con la biblioteca estándar.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras, en especial la **#3: código en inglés, mensajes al usuario en español**.
2. `internal/inventario/domain/tipo_movimiento.go` — **el molde exacto.** Cópialo estructuralmente.
3. `internal/inventario/domain/tipo_movimiento_test.go` — el estilo de prueba.
4. `internal/inventario/domain/errors.go` — cómo se declaran los errores centinela.
5. `docs/superpowers/specs/2026-07-29-comprobantes-whatsapp-design.md` §4.3 y §5.1 — qué significa cada valor.

## El patrón, una vez

Todos los tipos siguen la misma forma:

```go
// TipoVenta identifies a receipt for a sale registered in Microsip.
const TipoVenta = "venta"

// TipoPago identifies a receipt for a payment applied in Microsip.
const TipoPago = "pago"

// TipoComprobante is a value object wrapping which event produced the
// receipt. Only "venta" and "pago" are valid.
type TipoComprobante struct{ value string }

// NewTipoComprobante validates and constructs a TipoComprobante. Rejects
// anything else with ErrTipoComprobanteInvalido.
func NewTipoComprobante(s string) (TipoComprobante, error) { ... }

// HydrateTipoComprobante rebuilds one from persistence without validation.
// Intended for repository use only.
func HydrateTipoComprobante(s string) TipoComprobante { ... }

func (t TipoComprobante) Value() string                     { ... }
func (t TipoComprobante) String() string                    { ... }
func (t TipoComprobante) Equals(other TipoComprobante) bool { ... }
func (t TipoComprobante) IsZero() bool                      { ... }
func (t TipoComprobante) EsVenta() bool                     { ... }
func (t TipoComprobante) EsPago() bool                      { ... }
```

Comentarios de doc **en inglés**. Mensajes de error **en español**.

---

## Los cinco archivos de tipos

### 1. `tipo_comprobante.go` — `TipoComprobante`

| Valor | Significado |
|---|---|
| `venta` | La venta se registró en Microsip |
| `pago` | El pago se aplicó en Microsip |

Ayudantes: `EsVenta()`, `EsPago()`.

### 2. `estado_envio.go` — `EstadoEnvio`

**El tipo más importante de la tarea.** Gobierna si el botón de "detener" se muestra y si el envío ya salió.

| Valor | Significado |
|---|---|
| `en_espera` | Dentro de la ventana. **Todavía se puede detener** |
| `enviando` | Ya se decidió mandar. **Ya no se puede detener** |
| `enviado` | WhatsApp lo aceptó |
| `detenido` | Alguien lo detuvo a tiempo |
| `fallido` | El canal lo rechazó |
| `sin_telefono` | El cliente no tiene teléfono usable. **Terminal, no es una falla** |

Ayudantes:
- `EsDetenible()` — verdadero **solo** para `en_espera`. Es la pregunta que hace la pantalla.
- `EsTerminal()` — verdadero para `enviado`, `detenido`, `fallido` y `sin_telefono`.
- `EsFalla()` — verdadero **solo** para `fallido`. Ojo: `sin_telefono` **no** es falla.

> Con 68.8% de cobertura de teléfono, uno de cada tres clientes cae en `sin_telefono`. Si eso contara como falla, llenaría la bitácora de ruido y taparía los errores reales.

### 3. `canal.go` — `Canal`

Qué implementación respondió al enviar.

| Valor | Significado |
|---|---|
| `local` | El sender de pruebas: escribió el PDF a disco |
| `whatsapp_business` | La API oficial de Meta |

Ayudante: `EsReal()` — verdadero solo para `whatsapp_business`.

> Sirve para que **un envío de prueba nunca se cuente como entregado de verdad**. Es integridad de la medición, no un detalle.

### 4. `motivo_supresion.go` — `MotivoSupresion`

Por ahora un solo valor válido: `rebote`. Se modela como tipo y no como constante suelta porque van a aparecer más motivos.

Ayudante: `EsRebote()`.

### 5. `errors.go` — errores centinela

Declarados **a nivel de paquete**, nunca dentro de una función, con `internal/platform/apperror`:

| Variable | Código | Mensaje |
|---|---|---|
| `ErrTipoComprobanteInvalido` | `receipt_type_invalid` | `tipo de comprobante inválido` |
| `ErrEstadoEnvioInvalido` | `receipt_delivery_state_invalid` | `estado de envío inválido` |
| `ErrCanalInvalido` | `receipt_channel_invalid` | `canal de envío inválido` |
| `ErrMotivoSupresionInvalido` | `receipt_suppression_reason_invalid` | `motivo de supresión inválido` |

### 6. `doc.go`

Comentario de paquete. Mira `internal/inventario/domain/` para el estilo.

> Si `doc.go` ya existe porque otra tarea lo creó, **no lo dupliques ni lo reescribas** — déjalo como está.

---

## Pruebas

Paquete `domain_test`. **El piso de cobertura de `domain` es 99%**, así que aquí está la mayor parte del trabajo, no en los tipos.

Por cada tipo, tabla con:

- Cada valor válido: se construye, `Value()` lo devuelve, el ayudante correspondiente da verdadero y los otros falso.
- Valor inválido: devuelve el error centinela correcto, verificado con `errors.Is`.
- Cadena vacía: inválida.
- `IsZero()` verdadero en el valor cero, falso en uno construido.
- `Equals()`: verdadero contra sí mismo, falso contra otro.
- `Hydrate` acepta basura sin error — es a propósito, sirve para reconstruir desde la base y no valida.

Y específicamente para `EstadoEnvio`, tabla exhaustiva de los seis valores contra los tres ayudantes:

- `EsDetenible()` verdadero **solo** en `en_espera`.
- `EsTerminal()` verdadero en los cuatro terminales, falso en `en_espera` y `enviando`.
- `EsFalla()` verdadero **solo** en `fallido` — comprobar explícitamente que `sin_telefono` da falso.

Esa última es la que hay que escribir con cuidado: si `sin_telefono` se cuela como falla, el sistema va a reintentar envíos a clientes que no tienen teléfono.

## Restricciones

- `domain` no importa nada fuera de la biblioteca estándar y `internal/platform/apperror`. Si necesitas algo más, algo está mal planteado — pregunta antes.
- No agregues dependencias al `go.mod`.
- No uses `--no-verify` al commitear.

## Archivos que puedes tocar

Solo estos. Cualquier cambio fuera de la lista se rechaza sin revisar:

```
internal/comprobantes/domain/doc.go
internal/comprobantes/domain/errors.go
internal/comprobantes/domain/tipo_comprobante.go
internal/comprobantes/domain/estado_envio.go
internal/comprobantes/domain/canal.go
internal/comprobantes/domain/motivo_supresion.go
internal/comprobantes/domain/tipo_comprobante_test.go
internal/comprobantes/domain/estado_envio_test.go
internal/comprobantes/domain/canal_test.go
internal/comprobantes/domain/motivo_supresion_test.go
```

## Verificación

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
go test -race -coverprofile=coverage-comprobantes-domain.out ./internal/comprobantes/domain/
go tool cover -func=coverage-comprobantes-domain.out | tail -1
```

Criterios, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` sin errores.
- Pruebas en verde con `-race`.
- Cobertura de `domain` **≥ 99.0%**. No 95, no 98.

Si algún comando falla, la tarea no está terminada. No la entregues para que alguien la revise: entrégala cuando pase.

## Si te atoras

Más de **dos horas** trabado en una sola cosa: avisa. No sigas. Llegar al 99% suele ser lo que más cuesta la primera vez.

## Reporte

`docs/superpowers/plans/comprobantes-task-2-report.md`, con: archivos creados, salida literal de los seis comandos, qué tomaste de `tipo_movimiento.go` y en qué se diferencia cada tipo tuyo, y confirmación de que `domain` no importa nada fuera de la biblioteca estándar y `apperror`.

## Rama y commit

Estás en `feat/comprobantes-domain`. Un commit:

```
feat(comprobantes): value objects y errores del dominio
```

Al terminar: `git push`. Sin `--no-verify` y sin pie de atribución a ninguna herramienta de IA.
