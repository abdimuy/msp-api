# Comprobantes — Tarea 2: value objects, catálogos y errores

> **Rama:** `feat/comprobantes-domain` (ya creada, sacada de `feat/comprobantes`)
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)

## Dónde encaja

El módulo de comprobantes manda al cliente un PDF por WhatsApp cuando su venta se registra en Microsip y cuando su pago se aplica. Esta tarea construye **los tipos base del dominio**: los que representan un valor con un conjunto cerrado de opciones válidas.

**Es la tarea que bloquea al resto del dominio.** La entidad `Envio` y su máquina de estados se construyen encima de estos tipos.

No hay base de datos, no hay HTTP. Es Go puro con la biblioteca estándar.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras, en especial la **#3: código en inglés, mensajes al usuario en español**.
2. `docs/module-standards/02-value-objects-errors.md` — las tres categorías de VO y sus checklists.
3. `internal/ventas/domain/tipo_venta.go` — **el molde de los enums** (categoría 1). Cópialo.
4. Para el State VO (categoría 2), el molde es la plantilla del propio `02-value-objects-errors.md` y el ejemplo real `internal/garantias/domain/estado_folio.go`. **No** `internal/ventas/domain/estado_registro.go`: ese archivo es un Enum VO de categoría 1 y no tiene mapa de transiciones — la cita estaba mal en el estándar y se corrigió el 2026-08-07.
5. `docs/superpowers/specs/2026-07-29-comprobantes-whatsapp-design.md` §4.3 y §5.1 — qué significa cada valor.

## El patrón, una vez

Los **enums** (`TipoComprobante`, `Canal`) son VOs categoría 1: tipo `string`
con constantes tipadas y **exactamente** `Parse`/`IsValid`/`String`:

```go
// TipoComprobante enumerates which event produced the receipt. Only "venta"
// and "pago" are valid.
type TipoComprobante string

// Canonical TipoComprobante values. The literals match MSP_CM_ENVIO.TIPO.
const (
	// TipoVenta identifies a receipt for a sale registered in Microsip.
	TipoVenta TipoComprobante = "venta"
	// TipoPago identifies a receipt for a payment applied in Microsip.
	TipoPago TipoComprobante = "pago"
)

// ParseTipoComprobante parses a string into a TipoComprobante or returns
// ErrTipoComprobanteInvalido.
func ParseTipoComprobante(s string) (TipoComprobante, error) { ... }

func (t TipoComprobante) IsValid() bool  { ... }
func (t TipoComprobante) String() string { ... }
func (t TipoComprobante) EsVenta() bool  { ... }
func (t TipoComprobante) EsPago() bool   { ... }
```

No hay `New*`, `Hydrate*`, `Value()`, `Equals()` ni `IsZero()`: hidratar desde
la base es un cast (`Canal(row.Canal)`), comparar es `==`, y el cero es `== ""`.

`EstadoEnvio` es un VO categoría 2 (state): a los métodos de arriba se suman
`CanTransitionTo`, `IsTerminal` y el mapa `validEstadoEnvioTransitions`.

Comentarios de doc **en inglés**. Mensajes de error **en español**.

---

## Los cuatro archivos de tipos

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
| `enviado` | WhatsApp lo aceptó. Terminal |
| `detenido` | Alguien lo detuvo a tiempo. Terminal |
| `fallido` | El canal lo rechazó. **No terminal**: puede reenviarse |
| `sin_telefono` | El cliente no tiene teléfono usable. **Terminal, no es una falla** |

Mapa de transiciones (spec §4.3 + `POST /reenviar`, spec §8):

```go
var validEstadoEnvioTransitions = map[EstadoEnvio][]EstadoEnvio{
	EstadoEnvioEnEspera: {EstadoEnvioEnviando, EstadoEnvioDetenido},
	EstadoEnvioEnviando: {EstadoEnvioEnviado, EstadoEnvioFallido},
	EstadoEnvioFallido:  {EstadoEnvioEnEspera}, // POST /reenviar (spec §8)
}
```

`fallido` **no** es terminal: el `UNIQUE (TIPO, REFERENCIA)` de `MSP_CM_ENVIO`
(§5.1) obliga a que `/reenviar` reúse la fila y vuelva a `en_espera`
incrementando `INTENTOS`. Por eso `IsTerminal()` cubre solo `enviado`,
`detenido` y `sin_telefono`.

Métodos: `Parse`, `IsValid`, `String`, `CanTransitionTo`, `IsTerminal`
(prefijo inglés, como manda el estándar) y los ayudantes `EsDetenible()` y
`EsFalla()`:

- `EsDetenible()` — verdadero **solo** para `en_espera`. Es la pregunta que hace la pantalla.
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

### 4. `errors.go` — errores centinela

Declarados **a nivel de paquete**, nunca dentro de una función, con `internal/platform/apperror`:

| Variable | Código | Mensaje |
|---|---|---|
| `ErrTipoComprobanteInvalido` | `receipt_type_invalid` | `tipo de comprobante inválido` |
| `ErrEstadoEnvioInvalido` | `receipt_delivery_state_invalid` | `estado de envío inválido` |
| `ErrCanalInvalido` | `receipt_channel_invalid` | `canal de envío inválido` |

### 5. `doc.go`

Comentario de paquete. Mira `internal/inventario/domain/` para el estilo.

> Si `doc.go` ya existe porque otra tarea lo creó, **no lo dupliques ni lo reescribas** — déjalo como está.

---

## Pruebas

Paquete `domain_test`. **El piso de cobertura de `domain` es 99%**, así que aquí está la mayor parte del trabajo, no en los tipos.

Por cada tipo, tabla con:

- Cada valor válido: `Parse` lo acepta, el ayudante correspondiente da verdadero y los otros falso.
- Valor inválido: `Parse` devuelve el error centinela correcto, verificado con `errors.Is`.
- Cadena vacía: inválida.
- `IsValid()` verdadero en cada valor canónico, falso en cadenas fuera del conjunto (incluyendo mayúsculas, espacios adosados y sinónimos plausibles).
- `String()` devuelve el valor canónico.

Y específicamente para `EstadoEnvio`, tabla exhaustiva de los seis valores contra los tres predicados y cobertura del mapa de transiciones:

- `EsDetenible()` verdadero **solo** en `en_espera`.
- `IsTerminal()` verdadero en `enviado`, `detenido` y `sin_telefono`; falso en `en_espera`, `enviando` y `fallido`.
- `EsFalla()` verdadero **solo** en `fallido` — comprobar explícitamente que `sin_telefono` da falso.
- `CanTransitionTo()` verdadero para cada arista del mapa (incluida `fallido → en_espera`) y falso para cada no-arista (terminales sin salida, retrocesos no permitidos).

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
internal/comprobantes/domain/tipo_comprobante_test.go
internal/comprobantes/domain/estado_envio_test.go
internal/comprobantes/domain/canal_test.go
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

`docs/superpowers/plans/comprobantes-task-2-report.md`, con: archivos creados, salida literal de los seis comandos, qué tomaste de `internal/ventas/domain/tipo_venta.go` y de `internal/garantias/domain/estado_folio.go` (y el estándar `02-value-objects-errors.md`) y en qué se diferencia cada tipo tuyo, y confirmación de que `domain` no importa nada fuera de la biblioteca estándar y `apperror`.

## Rama y commit

Estás en `feat/comprobantes-domain`. Un commit:

```
feat(comprobantes): add domain value objects and sentinel errors
```

Cuando el brief dicta un mensaje de commit, úsalo textual; si te apartás, que el reporte documente el mensaje que de verdad quedó en la rama.

La rama se sube al remoto como PR contra `main`. Sin `--no-verify` bajo ninguna circunstancia, y sin pie de atribución a ninguna herramienta de IA en el mensaje del commit.

