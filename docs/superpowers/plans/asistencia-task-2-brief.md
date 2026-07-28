# Asistencia — Tarea 2: value objects, catálogos y errores del dominio

> **Rama:** `feat/asistencia-domain` (ya creada, sacada de `feat/asistencia`)
> **Spec del módulo:** [`docs/superpowers/specs/2026-07-28-asistencia-design.md`](../specs/2026-07-28-asistencia-design.md)

## Dónde encaja

El módulo de asistencia registra la jornada laboral de los trabajadores. Esta tarea construye **los tipos base del dominio**: los que representan un valor con un conjunto cerrado de opciones válidas.

**Es la tarea que bloquea a todas las demás del dominio.** Las entidades (`Empleado`, `Marcaje`, `Horario`, `Jornada`) se construyen encima de estos tipos, así que hasta que esto no esté, nadie más puede avanzar en el módulo.

No hay base de datos, no hay HTTP, no hay dependencias externas. Es Go puro con la biblioteca estándar.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras. En particular la **#3: código en inglés, mensajes al usuario en español**.
2. `internal/inventario/domain/tipo_movimiento.go` — **este es el molde exacto.** Cópialo estructuralmente: constantes, struct con campo privado, constructor que valida, `Hydrate`, `Value`, `String`, `Equals`, `IsZero`, más los ayudantes propios del tipo.
3. `internal/inventario/domain/tipo_movimiento_test.go` — el estilo de prueba.
4. `internal/inventario/domain/errors.go` — cómo se declaran los errores centinela.
5. `docs/superpowers/specs/2026-07-28-asistencia-design.md` §3 y §4 — para entender qué significa cada valor.
6. `docs/module-standards/AGGREGATE_PATTERNS.md` — sección de value objects.

## El patrón, una vez

Todos los tipos siguen la misma forma. Ejemplo con `Metodo`:

```go
// MetodoHuella identifies a punch made with a fingerprint.
const MetodoHuella = "huella"

// MetodoNumero identifies a punch made by typing the employee number.
const MetodoNumero = "numero"

// Metodo is a value object wrapping how the employee identified themselves
// when punching. Only "huella" and "numero" are valid.
type Metodo struct{ value string }

// NewMetodo validates and constructs a Metodo. Rejects anything else with
// ErrMetodoInvalido.
func NewMetodo(s string) (Metodo, error) { ... }

// HydrateMetodo rebuilds a Metodo from persistence without validation.
// Intended for repository use only.
func HydrateMetodo(s string) Metodo { ... }

func (m Metodo) Value() string          { ... }
func (m Metodo) String() string         { ... }
func (m Metodo) Equals(other Metodo) bool { ... }
func (m Metodo) IsZero() bool           { ... }
func (m Metodo) EsHuella() bool         { ... }
func (m Metodo) EsNumero() bool         { ... }
```

Los comentarios de doc van **en inglés** (CLAUDE.md §3). Los mensajes de error van **en español**.

---

## Los siete tipos

### 1. `metodo.go` — `Metodo`

Cómo se identificó la persona al marcar.

| Valor | Significado |
|---|---|
| `huella` | Puso el dedo en el lector |
| `numero` | Tecleó su número de empleado (respaldo cuando el lector falla) |

Ayudantes: `EsHuella()`, `EsNumero()`.

### 2. `origen.go` — `Origen`

De dónde salió el marcaje. **Es el tipo más importante de esta tarea**: es lo que distingue un dato que atestiguó una persona de uno que inventó el sistema, y la consola los pinta distinto.

| Valor | Significado |
|---|---|
| `lector` | Lo generó el lector. Alguien estuvo ahí |
| `capturado` | Lo dio de alta administración a mano |
| `sistema` | Lo generó el cierre automático porque faltaba |

Ayudantes: `EsReal()` (solo `lector`), `EsCapturado()`, `EsGenerado()` (solo `sistema`).

### 3. `estado_jornada.go` — `EstadoJornada`

Cómo terminó el día de una persona.

| Valor | Significado |
|---|---|
| `completa` | Marcó todo lo que le tocaba |
| `incompleta` | Faltó algún marcaje y el sistema lo completó |
| `falta` | No marcó nada en un día laborable |
| `sin_horario` | Marcó en un día que tenía de descanso |
| `descanso` | Día de descanso y no marcó. Es lo normal, no una excepción |

Ayudantes: `EsFalta()`, `EsDescanso()`, `RequiereRevision()` — verdadero para `incompleta` y `sin_horario`, falso para el resto.

### 4. `tipo_correccion.go` — `TipoCorreccion`

Qué hizo administración al corregir.

| Valor | Significado |
|---|---|
| `alta` | Capturó a mano un marcaje que faltaba |
| `ajuste` | Cambió la hora de uno existente |
| `supresion` | Sacó uno del cálculo (no lo borró) |

Sin ayudantes.

### 5. `dia_semana.go` — `DiaSemana`

**El único tipo con lógica de verdad, y donde está la trampa de esta tarea.**

Envuelve un entero del **1 al 7**, donde **1 = lunes** y **7 = domingo**. Cualquier otro número es inválido.

Además de los métodos habituales, lleva:

```go
// DiaSemanaDeTiempo converts a Go time to the module's day numbering.
func DiaSemanaDeTiempo(t time.Time) DiaSemana
```

> ⚠️ **Aquí está la trampa.** El tipo `time.Weekday` de Go usa **domingo = 0**, lunes = 1, … sábado = 6. Nosotros usamos **lunes = 1, … domingo = 7**. La conversión **no** es `int(t.Weekday())`: el domingo hay que mapearlo a 7 en vez de 0.
>
> Esta es la clase de error que no revienta — simplemente corre los horarios un día y nadie se entera hasta que alguien reclama que le marcaron falta un lunes. **Pruébalo con los siete días.**

Ayudantes: `EsFinDeSemana()` (sábado o domingo), `Numero() int`, `Nombre() string` — el nombre en español: `lunes`, `martes`, `miércoles`, `jueves`, `viernes`, `sábado`, `domingo`.

`Nombre()` es lo único que devuelve texto en español porque va a pantalla. Los valores internos siguen siendo números.

### 6. `dedo.go` — `Dedo`

Cuál dedo se enroló. Se enrolan varios por persona: uno cortado o sucio no debe dejar a nadie fuera.

Diez valores válidos:

```
pulgar_izquierdo · indice_izquierdo · medio_izquierdo · anular_izquierdo · menique_izquierdo
pulgar_derecho   · indice_derecho   · medio_derecho   · anular_derecho   · menique_derecho
```

Ayudantes: `EsIzquierdo()`, `EsDerecho()`.

### 7. `errors.go` — errores centinela

Uno por tipo, declarados **a nivel de paquete** (nunca dentro de una función), con `internal/platform/apperror`:

```go
var ErrMetodoInvalido = apperror.NewValidation(
	"attendance_method_invalid",              // código en inglés, snake_case
	"método de identificación inválido",      // mensaje en español, minúsculas, sin punto final
)
```

Los seis:

| Variable | Código | Mensaje |
|---|---|---|
| `ErrMetodoInvalido` | `attendance_method_invalid` | `método de identificación inválido` |
| `ErrOrigenInvalido` | `attendance_origin_invalid` | `origen de marcaje inválido` |
| `ErrEstadoJornadaInvalido` | `attendance_workday_state_invalid` | `estado de jornada inválido` |
| `ErrTipoCorreccionInvalido` | `attendance_correction_type_invalid` | `tipo de corrección inválido` |
| `ErrDiaSemanaInvalido` | `attendance_weekday_invalid` | `día de la semana inválido` |
| `ErrDedoInvalido` | `attendance_finger_invalid` | `dedo inválido` |

### 8. `doc.go`

Comentario de paquete. Mira `internal/inventario/domain/` para el estilo.

---

## Pruebas

Paquete `domain_test`. **El piso de cobertura de `domain` es 99%**, así que aquí está la mayor parte del trabajo — no en los tipos, que son cortos.

Por cada tipo, tabla con:

- Cada valor válido: se construye, `Value()` lo devuelve, el ayudante correspondiente da verdadero y los otros falso.
- Valor inválido: devuelve el error centinela correcto. Verifícalo con `errors.Is`.
- Cadena vacía: inválida.
- `IsZero()` verdadero en el valor cero del tipo, falso en uno construido.
- `Equals()`: verdadero contra sí mismo, falso contra otro.
- `Hydrate` acepta un valor basura sin error (es a propósito: es para reconstruir desde la base de datos, no valida).

Y específicamente para `DiaSemana`:

- **Los siete días convertidos desde `time.Time`**, uno por uno, con fechas reales. Verifica que un domingo dé 7 y no 0, y que un lunes dé 1.
- Los números 0, 8 y −1 son inválidos.
- `Nombre()` para los siete.
- `EsFinDeSemana()` verdadero solo en 6 y 7.

## Restricciones

- **`domain` no importa nada fuera de la biblioteca estándar y `internal/platform/apperror`.** Ni Firebird, ni HTTP, ni otros módulos. Si necesitas importar algo más, algo está mal planteado — pregunta antes de hacerlo.
- **Prohibido importar cualquier `internal/` que no sea `internal/asistencia/...` o `internal/platform/...`.** Asistencia es un módulo sellado; importar de otro módulo es motivo de rechazo directo.
- No agregues dependencias al `go.mod`.
- No uses `--no-verify` al commitear, bajo ninguna circunstancia.

## Archivos que puedes tocar

Solo estos. Cualquier cambio fuera de la lista se rechaza sin revisar:

```
internal/asistencia/domain/doc.go
internal/asistencia/domain/errors.go
internal/asistencia/domain/metodo.go
internal/asistencia/domain/origen.go
internal/asistencia/domain/estado_jornada.go
internal/asistencia/domain/tipo_correccion.go
internal/asistencia/domain/dia_semana.go
internal/asistencia/domain/dedo.go
internal/asistencia/domain/metodo_test.go
internal/asistencia/domain/origen_test.go
internal/asistencia/domain/estado_jornada_test.go
internal/asistencia/domain/tipo_correccion_test.go
internal/asistencia/domain/dia_semana_test.go
internal/asistencia/domain/dedo_test.go
```

## Verificación

Corre esto y pega la salida completa en el reporte. Todo tiene que pasar:

```sh
gofmt -l internal/asistencia
go vet ./internal/asistencia/...
go build ./...
golangci-lint run ./internal/asistencia/...
go test -race -coverprofile=coverage-asistencia-domain.out ./internal/asistencia/domain/
go tool cover -func=coverage-asistencia-domain.out | tail -1
```

Criterios de aceptación, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` terminan sin errores.
- Las pruebas pasan con `-race`.
- La cobertura de `domain` es **≥ 99.0%**. No 95, no 98. El piso de este paquete es 99 porque es donde vive el criterio del negocio.

Si algún comando falla, la tarea no está terminada. No la entregues para que alguien la revise: entrégala cuando pase.

## Si te atoras

Si llevas **más de dos horas** trabado en una sola cosa, avisa. No sigas. Llegar al 99% de cobertura suele ser lo que más cuesta la primera vez.

## Reporte

Escríbelo en `docs/superpowers/plans/asistencia-task-2-report.md`:

- Archivos creados.
- Salida literal de los seis comandos.
- Qué tomaste de `tipo_movimiento.go` y en qué se diferencia cada tipo tuyo.
- **Cómo resolviste la conversión de `time.Weekday` a nuestro numerado**, y qué fechas usaste para probar los siete días.
- Confirmación explícita de que `domain` no importa nada fuera de la biblioteca estándar y `apperror`.

## Commit

Estás en `feat/asistencia-domain`. Un commit:

```
feat(asistencia): value objects y errores del dominio
```

Al terminar: `git push`. Sin `--no-verify`, y sin pie de atribución a ninguna herramienta de IA en el mensaje.
