# Manejo de Fechas y Horas

Estándar **obligatorio** para cualquier código que toque fechas en msp-api. La inconsistencia en zonas horarias es una fuente común de bugs invisibles (reportes con días corridos, cobranzas que cierran un día tarde, ventas que aparecen en la fecha equivocada). Esta guía cierra el problema de raíz.

> **Decisión de arquitectura:** todo el negocio opera en `America/Mexico_City`. Microsip — el sistema legacy que precede a msp-api — guarda sus columnas `TIMESTAMP` como wall-clock en esa zona. msp-api debe ser compatible con esa convención para que las filas que escribimos se vean coherentes desde la UI de Microsip Windows.

---

## TL;DR — Las 3 reglas

1. **En Go (domain, app, dominio):** todo `time.Time` está en **UTC**. Siempre. Nunca uses `time.Now().Local()` ni `time.Local`.
2. **Al escribir a Firebird:** wrappea cada `time.Time` con `firebird.ToWallClock(t)`. Sin excepción.
3. **Al leer de Firebird:** usa `firebird.ScanUTCTime(raw)` o `firebird.ScanNullUTCTime(raw)`. Devuelven UTC.

Si sigues estas tres reglas, no hay drift. Si te las saltas, se rompe en algún `Mac/Linux/Windows` con TZ distinto y nadie se da cuenta hasta que un cliente pregunta por qué "la venta del lunes aparece como domingo".

---

## Glosario

| Término | Definición |
|---|---|
| **Instante** | Punto absoluto en la línea del tiempo (microsegundos desde Unix epoch). Independiente de TZ. |
| **Wall-clock** | Los campos `Year, Month, Day, Hour, Minute, Second` por separado. **Ambiguo sin TZ**. |
| **BusinessTZ** | `America/Mexico_City`. Constante hardcoded en `internal/platform/firebird/tz.go`. |
| **UTC** | Coordinated Universal Time. La única zona que usamos en código y en la API. |

---

## Por qué este modelo

Tres restricciones inamovibles:

1. **Microsip guarda wall-clock en CDMX** sin TZ metadata. Si abro `MSP_VENTAS.FECHA_VENTA` con isql desde un cliente en cualquier máquina, el número que veo es la hora local del momento en que se capturó. Cambiar esto requeriría migrar **toda la base histórica de Microsip** — fuera de scope.

2. **Firebird 4 soporta `TIMESTAMP WITH TIME ZONE`**, pero el cliente Microsip Windows no lo entiende. Usar el tipo nuevo en nuestras tablas crearía un schema partido (legacy sin TZ, nuevo con TZ) que confunde a todos.

3. **Go corre en distintos hosts** (Mac dev en CST, contenedor Linux en UTC, Windows server en CDT). Si dejamos que cada uno asuma su `time.Local`, el mismo instante se persiste con wall-clocks distintos según dónde se ejecutó el proceso → datos corruptos silenciosamente.

La solución: forzar **BusinessTZ explícita en la frontera Firebird**. El dominio nunca la ve. La API JSON tampoco. Solo el adaptador Firebird traduce.

---

## Helpers canónicos

Definidos en `internal/platform/firebird/`:

```go
// BusinessTZ es America/Mexico_City. Lazy + cached.
func BusinessTZ() *time.Location

// ToWallClock convierte UTC → time.Time cuyo wall-clock representa el
// mismo instante en BusinessTZ. Úsalo al escribir.
func ToWallClock(t time.Time) time.Time

// FromWallClock toma un time.Time leído del driver (wall-clock en BusinessTZ
// pero etiquetado con time.Local), reinterpreta los campos como BusinessTZ
// y devuelve el UTC equivalente. Úsalo al leer (lo hace ScanUTCTime por ti).
func FromWallClock(t time.Time) time.Time

// ScanUTCTime acepta time.Time/string/[]byte desde el driver y devuelve UTC.
// Aplica FromWallClock por dentro.
func ScanUTCTime(src any) (time.Time, error)
func ScanNullUTCTime(src any) (sql.NullTime, error)
```

---

## Layer por layer — qué hacer

### Domain

```go
// internal/{module}/domain/...

type Venta struct {
    fechaVenta time.Time // SIEMPRE UTC
}

func (v *Venta) FechaVenta() time.Time { return v.fechaVenta }
```

- Recibe `time.Time` en parámetros (asume UTC).
- No imports de `firebird` package. La capa domain no sabe que existe Firebird.
- No conviertas. No formatees. No compares con `time.Now()`. El dominio recibe tiempo desde fuera.

### App

```go
// internal/{module}/app/...

// Clock es el port que abstrae time.Now() para tests.
type Service struct {
    clock outbound.Clock
}

func (s *Service) DoStuff(ctx context.Context) {
    now := s.clock.Now() // SIEMPRE UTC — ProductionClock devuelve time.Now().UTC()
    venta.HacerCosa(now)
}
```

- Usa `s.clock.Now()`, **no** `time.Now()`.
- `ProductionClock.Now()` ya devuelve UTC. Confía.

### Infra Firebird (repos)

```go
// internal/{module}/infra/{module}fb/repo.go
import "github.com/abdimuy/msp-api/internal/platform/firebird"

func (r *Repo) Save(ctx context.Context, v *domain.Venta) error {
    q := firebird.GetQuerier(ctx, r.pool.DB)
    _, err := q.ExecContext(ctx, insertVenta,
        v.ID().String(),
        firebird.ToWallClock(v.FechaVenta()),     // ← AL ESCRIBIR
        firebird.ToWallClock(v.Audit().CreatedAt()),
        firebird.ToWallClock(v.Audit().UpdatedAt()),
        // ...
    )
    return firebird.MapError(err)
}

func scanVenta(row rowScanner) (*domain.Venta, error) {
    var fechaVentaRaw, createdAtRaw, updatedAtRaw any
    if err := row.Scan(&fechaVentaRaw, &createdAtRaw, &updatedAtRaw); err != nil {
        return nil, err
    }
    fechaVenta, err := firebird.ScanUTCTime(fechaVentaRaw) // ← AL LEER
    if err != nil {
        return nil, err
    }
    // ...
}
```

**Reglas mecánicas:**

| Patrón | Forma correcta |
|---|---|
| `q.ExecContext(ctx, sql, t)` donde `t` es `time.Time` | `q.ExecContext(ctx, sql, firebird.ToWallClock(t))` |
| `q.ExecContext(ctx, sql, *tPtr)` donde `tPtr` es `*time.Time` | wrappea solo si no-nil, pasa `nil` cuando es nil |
| `row.Scan(&someRaw)` para una columna `TIMESTAMP` | declarar `var someRaw any`, luego `firebird.ScanUTCTime(someRaw)` |
| Columna `TIMESTAMP` nullable | `var someRaw any`, luego `firebird.ScanNullUTCTime(someRaw)` |
| Filtros `WHERE FECHA_X >= ?` en queries dinámicas | `args = append(args, firebird.ToWallClock(*f.Desde))` |

**Anti-patrones (rechazar en review):**

```go
// ❌ MAL — pasa el time directo, el driver lo escribirá como wall-clock local
q.ExecContext(ctx, sql, v.FechaVenta())

// ❌ MAL — convierte a string a mano
q.ExecContext(ctx, sql, v.FechaVenta().Format("2006-01-02 15:04:05"))

// ❌ MAL — usa time.Local al leer
t := raw.(time.Time).In(time.Local)

// ❌ MAL — confiar en el wall-clock del driver
var t time.Time
row.Scan(&t)
return t // su Location es time.Local, su instante es incorrecto

// ✓ BIEN
var raw any
row.Scan(&raw)
t, _ := firebird.ScanUTCTime(raw)
return t
```

### Infra HTTP (DTOs)

```go
// internal/{module}/infra/{module}http/dto.go

type VentaDTO struct {
    FechaVenta string `json:"fecha_venta"` // RFC3339 UTC, ej: "2026-05-13T18:00:00Z"
}
```

- **Entrada (request body):** acepta RFC3339 con cualquier TZ. Parsea con `parseTimeField` que usa `time.Parse(time.RFC3339, raw)`. El resultado ya viene con TZ correcta; conviértelo a UTC con `.UTC()` antes de pasarlo al app layer.
- **Salida (response body):** siempre RFC3339 UTC, ej. `"2026-05-13T18:00:00Z"`. Usar `formatTime(t)` que hace `t.UTC().Format(time.RFC3339Nano)`.

---

## Contrato con el frontend

> **TL;DR del cliente:** todo en RFC3339 con `Z` (UTC). No mandes hora local. No mandes sin TZ.

### Cómo el frontend manda fechas a la API

**Formato obligatorio:** **RFC3339 / ISO-8601 con marcador de TZ explícito**, preferentemente UTC (`Z`).

✓ **Aceptable:**

```json
{
  "fecha_venta": "2026-05-13T18:00:00Z"
}
```

```json
{
  "fecha_venta": "2026-05-13T12:00:00-06:00"
}
```

(El backend convierte ambos al mismo instante UTC internamente.)

✗ **Rechazado por el backend (HTTP 422):**

```json
{ "fecha_venta": "2026-05-13" }                  // falta hora y TZ
{ "fecha_venta": "2026-05-13 18:00:00" }         // falta TZ (ambigua)
{ "fecha_venta": "13/05/2026 12:00 PM" }         // formato local Mx — no soportado
{ "fecha_venta": 1747260000 }                    // unix epoch como número — no soportado
```

### Cómo recibe fechas del backend

**Formato garantizado de respuesta:** RFC3339 UTC con sufijo `Z`. Ejemplos:

```json
{
  "id": "…",
  "fecha_venta": "2026-05-13T18:00:00Z",
  "created_at": "2026-05-13T18:05:23.142Z",
  "cancelacion": {
    "at": "2026-05-13T19:00:00Z",
    "by": "…",
    "reason": "…"
  }
}
```

**Cómo presentarlo al usuario en la UI:**

- **Convierte a hora local del navegador** o **fuerza CDMX** según el caso de uso.
- En JavaScript/TypeScript:

  ```typescript
  // El usuario está en cualquier parte; mostrar en hora del negocio (CDMX):
  const fmt = new Intl.DateTimeFormat('es-MX', {
    timeZone: 'America/Mexico_City',
    dateStyle: 'short',
    timeStyle: 'short',
  });
  fmt.format(new Date(venta.fecha_venta)); // "13/5/2026, 12:00"

  // O en hora local del dispositivo (más raro — solo si el usuario lo pide):
  new Date(venta.fecha_venta).toLocaleString();
  ```

- **No parses manualmente la string**. Pásala directo a `new Date(...)` que entiende RFC3339.
- **No asumas que `Z` significa "ya está en hora local"** — significa UTC. Si lo muestras sin convertir, mostrarás 6h adelantadas.

### Cuándo mandar la fecha actual

Si el frontend necesita mandar "ahora mismo" (ej. fecha de captura de una venta):

```typescript
const body = {
  fecha_venta: new Date().toISOString(), // "2026-05-13T18:00:00.000Z"
};
```

`new Date().toISOString()` siempre devuelve RFC3339 UTC con `Z`. Ese es el formato canónico.

### Pickers de fecha

Si el usuario elige "13 de mayo a las 12:00 PM" en un date picker:

```typescript
// Wrong: ambiguous, depende de la TZ del navegador
const picked = new Date('2026-05-13T12:00:00');

// Right: anclar a CDMX explícitamente
const picked = new Date('2026-05-13T12:00:00-06:00'); // o construir vía Temporal API
body.fecha_venta = picked.toISOString();
```

Para evitar bugs sutiles, **siempre construye la fecha anclada a CDMX**, no a la TZ del navegador. Un vendedor remoto en otra zona horaria capturando una venta debe ver la misma fecha que el admin en CDMX.

---

## Filtros de fecha en queries (GET /v2/ventas?desde=…)

Mismo contrato: el frontend manda RFC3339 UTC.

```
GET /v2/ventas?desde=2026-05-01T00:00:00Z&hasta=2026-06-01T00:00:00Z
```

- **Inclusivo en `desde`, exclusivo en `hasta`** (semántica `[desde, hasta)`).
- Si quieres "todas las ventas del 13 de mayo" anclado a CDMX:
  - `desde=2026-05-13T06:00:00Z` (00:00 CDMX = 06:00 UTC)
  - `hasta=2026-05-14T06:00:00Z`

---

## Tests

Cualquier test que toque tiempos debe construir `time.Time` con `time.Date(..., time.UTC)`. **Nunca** `time.Now()` en assertions — usa un `fixedClock`.

```go
// ✓ Correcto
fixed := time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)
clock := fixedClock{T: fixed}

// ✓ Correcto — comparar round-trip
assert.WithinDuration(t, expectedUTC, got, time.Second)

// ❌ Mal — depende de la TZ del host
assert.Equal(t, time.Now().Format("..."), got.Format("..."))
```

Para tests Firebird-backed: la migración 000003 ya garantiza que los wall-clocks en DB son CDMX. El round-trip `Save → FindByID` debe preservar el instante UTC exactamente (ver `TestVentaRepo_UpdateHeader_PersistsFields` como referencia).

---

## Checklist de PR

Antes de mergear cualquier código que toque fechas, verificar:

- [ ] Domain: `time.Time` solo en parámetros / accessors. No imports de `time.Local`.
- [ ] App: uses `s.clock.Now()`, nunca `time.Now()` directo.
- [ ] Repo: cada `time.Time` que va a `q.ExecContext` está envuelto en `firebird.ToWallClock`.
- [ ] Repo: cada `Scan` de columna `TIMESTAMP` usa `firebird.ScanUTCTime` o `ScanNullUTCTime`.
- [ ] DTO: campos de fecha son `string` con `json:"…"` y se serializan vía `formatTime(t)`.
- [ ] DTO: entrada parsea con `time.Parse(time.RFC3339, raw)` y convierte a `.UTC()` antes de cruzar a app.
- [ ] Tests: usan `time.UTC` para construir tiempos. No comparan formatos locales.

---

## ADR relacionada

Si alguna vez tenemos que migrar el modelo (ej. abrir operación en otro país, o Microsip migra a TZ-aware), documenta el cambio como ADR. **No** cambies `BusinessTZ` sin migración de datos — reinterpreta silenciosamente toda la historia.
