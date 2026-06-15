# Step 07 — Repository

> Applies to: Type A (CRUD), Type B (Pipeline), Type C (Microsip-synced).
> Depends on: Step 06 (ports outbound).
> Parallel with: Step 08 (HTTP handler).
> Scope: el repositorio Firebird del módulo — struct, queries, rowmappers, paginación, tests.

> **Adaptado de `ancla-api`, reconciliado con el código real de msp-api (Firebird).**
> Diferencias clave vs. ancla: (1) **sin sqlc** — las queries son strings Go en `queries.go`,
> el binding es posicional (`?`); (2) **sin optimistic locking** — no hay campo `version` ni
> `WHERE version = $current`; (3) **sin pgtype** — la normalización de tipos la hacen
> `firebird.ScanDecimal`/`ScanUTCTime`/`ToWallClock` sobre `any`; (4) **el pool es
> `*firebird.Pool`**, no pgxpool; (5) transacciones vía `firebird.TxManager` y
> `firebird.GetQuerier(ctx, pool.DB)`, no `transaction.FromContext`.

---

## ancla vs. msp-api (Firebird, real)

| Dimensión | ancla-api (Postgres) | msp-api (Firebird, real) |
|---|---|---|
| Driver | `pgx/v5` + pgxpool | `nakagami/firebirdsql` + `database/sql` |
| Query gen | sqlc + `.sql` en `queries/` | strings Go en `{module}fb/queries.go` |
| Binding | `$1`, `$2`, … | `?` posicional (Firebird convención) |
| Pool type | `*pgxpool.Pool` | `*firebird.Pool` (embebe `*sql.DB`) |
| Querier | `sqlc.Queries` | `firebird.Querier` (interfaz `ExecContext`/`QueryContext`/`QueryRowContext`) |
| Tx context | `transaction.FromContext` | `firebird.GetQuerier(ctx, pool.DB)` |
| Tx manager | — | `firebird.TxManager.RunInTx` / `RunInReadTx` / `RunInSnapshotTx` |
| Decimal | `pgtype.Numeric` → helpers | `firebird.ScanDecimal(src, scale)` sobre `any` |
| Tiempo | `pgtype.Timestamptz` → helpers | `firebird.ScanUTCTime(src)` al leer; `firebird.ToWallClock(t)` al escribir |
| UUID | `pgtype.UUID` | `CHAR(36)` leído como `string`, parseado con `uuid.Parse` |
| No rows | `pgx.ErrNoRows` | `sql.ErrNoRows` |
| Optimistic lock | `version` + `RowsAffected() == 0` → `ErrConcurrentModification` | sin versión — `RowsAffected() == 0` → sentinel `domain.Err{Entity}NotFound` |
| Tests | testcontainers Postgres | `fbtestutil.NewTestFirebirdPool` + `fbtestutil.WithTestTransaction` |

---

## Archivos a crear

```
internal/{module}/infra/{module}fb/
├─ {entity}_repo.go       # struct del repo + métodos públicos
├─ queries.go             # todas las constantes SQL del módulo
├─ rowmappers.go          # rowScanner + struct raw + scan helpers + assembleEntity
└─ pagination.go          # encodeCursor / decodeCursor / clampPageSize (si paginado)
```

Package name: `{module}fb` (p. ej. `ventfb`, `authfb`). El sufijo `fb` distingue del paquete
platform `firebird` y evita colisiones entre módulos.

---

## Struct del repositorio

```go
package ventfb

import (
    "github.com/abdimuy/msp-api/internal/platform/firebird"
    "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// VentaRepo is the Firebird-backed implementation of outbound.VentaRepo.
//
// Every method routes queries through firebird.GetQuerier so it transparently
// joins an ambient transaction installed in the context (used by application
// services and the test harness) and otherwise falls back to the shared pool.
type VentaRepo struct {
    pool *firebird.Pool
}

// NewVentaRepo builds a VentaRepo wired to the given pool.
func NewVentaRepo(pool *firebird.Pool) *VentaRepo {
    return &VentaRepo{pool: pool}
}

// Compile-time check: VentaRepo satisfies the outbound port.
var _ outbound.VentaRepo = (*VentaRepo)(nil)
```

Reglas:
- Un único campo `pool *firebird.Pool`; el repo no guarda `*sql.DB` ni `*sql.Tx` directamente.
- El compile-time check es obligatorio. Falla la compilación si el port cambia sin actualizar el repo.
- Sin `*sql.DB` embebido directamente — el pool ya lo embebe; se pasa como `pool.DB` a `GetQuerier`.

---

## GetQuerier — la única forma de hablar con la DB

Todos los métodos del repo obtienen el querier via `firebird.GetQuerier`. Esta llamada honra el
ambient tx instalado por `firebird.TxManager.RunInTx` (o el harness de test), y si no hay tx
activa, usa `pool.DB` directamente.

```go
q := firebird.GetQuerier(ctx, r.pool.DB)
```

**Nunca** llames `r.pool.DB.ExecContext` / `r.pool.QueryContext` directamente — siempre `GetQuerier`.

### Firma real de GetQuerier

```go
// GetQuerier returns the active *sql.Tx if ctx carries one, otherwise fallback.
func GetQuerier(ctx context.Context, fallback Querier) Querier
```

```go
// Querier es la superficie común a *sql.DB y *sql.Tx.
type Querier interface {
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
```

---

## Reglas operacionales

- El repo honra el contexto del caller: nunca crea `context.Background()` propio.
- Las operaciones DB se acotan al tiempo del contexto del caller.
- Sin reintentos ocultos dentro de los métodos individuales. Si se necesita retry ante lock-conflict
  de Firebird, usa `pool.ExecRetry(ctx, fn)` en el método `Save` del agregado raíz.
- Las transacciones son cortas: las llamadas de red externas no pertenecen dentro de una tx.
- Si un comando escribe estado y además debe publicar un evento durable, inserta la fila de outbox
  en la misma transacción (ver ADR-0008).

---

## Tipo A — métodos

### Save (insert de un agregado multi-tabla)

El `Save` del agregado raíz es el método más complejo: inserta la fila principal y todos los
child rows en orden FK, usando el mismo querier en todos para que sean atómicos cuando el caller
ya abrió una tx.

```go
func (r *VentaRepo) Save(ctx context.Context, v *domain.Venta) error {
    return r.pool.ExecRetry(ctx, func(ctx context.Context) error {
        q := firebird.GetQuerier(ctx, r.pool.DB)
        if err := r.insertHeader(ctx, q, v); err != nil {
            return err
        }
        if err := r.insertCombos(ctx, q, v); err != nil {
            return err
        }
        if err := r.insertProductos(ctx, q, v); err != nil {
            return err
        }
        if err := r.insertVendedores(ctx, q, v); err != nil {
            return err
        }
        return r.insertImagenes(ctx, q, v)
    })
}
```

El INSERT explicita todos los campos incluyendo `ID`, `CREATED_AT` y `UPDATED_AT` — nunca se
delegan a la DB:

```go
const insertVenta = `
INSERT INTO MSP_VENTAS
    (ID, NOMBRE_CLIENTE, ...,
     CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY, ...)
VALUES (?, ?, ..., ?, ?, ?, ?, ...)`
```

Los argumentos de tiempo siempre pasan por `firebird.ToWallClock`:

```go
func (r *VentaRepo) insertHeader(ctx context.Context, q firebird.Querier, v *domain.Venta) error {
    args := headerInsertArgs(v) // ensambla el slice positional
    _, err := q.ExecContext(ctx, insertVenta, args...)
    return firebird.MapError(err)
}

// Dentro de headerInsertArgs:
firebird.ToWallClock(v.FechaVenta()),
firebird.ToWallClock(a.CreatedAt()),
firebird.ToWallClock(a.UpdatedAt()),
```

### FindByID

```go
func (r *VentaRepo) FindByID(ctx context.Context, id uuid.UUID) (*domain.Venta, error) {
    q := firebird.GetQuerier(ctx, r.pool.DB)
    raw, err := loadHeaderRaw(ctx, q, id)
    if err != nil {
        return nil, err
    }
    combos, err := loadCombos(ctx, q, id)
    if err != nil {
        return nil, err
    }
    // ... idem loadProductos, loadVendedores, loadImagenes
    return assembleVenta(raw, combos, productos, vendedores, imagenes)
}

func loadHeaderRaw(ctx context.Context, q firebird.Querier, id uuid.UUID) (*ventaRowRaw, error) {
    row := q.QueryRowContext(ctx, selectVentaByID, id.String())
    raw, err := scanVentaRowRaw(row)
    if errors.Is(err, sql.ErrNoRows) {
        return nil, domain.ErrVentaNotFound // mapea sql.ErrNoRows → sentinel de dominio
    }
    if err != nil {
        return nil, firebird.MapError(err)
    }
    return raw, nil
}
```

### Update

Sin optimistic locking. `RowsAffected() == 0` mapea a `ErrVentaNotFound`:

```go
func (r *VentaRepo) Update(ctx context.Context, v *domain.Venta) error {
    q := firebird.GetQuerier(ctx, r.pool.DB)
    a := v.Audit()
    res, err := q.ExecContext(ctx, updateVentaHeader,
        v.Estado().String(), v.Situacion().String(), /* … campos de estado … */
        firebird.ToWallClock(a.UpdatedAt()), a.UpdatedBy().String(),
        v.ID().String(), // WHERE ID = ?
    )
    if err != nil {
        return firebird.MapError(err)
    }
    return ensureRowAffected(res, domain.ErrVentaNotFound)
}

// ensureRowAffected mapea RowsAffected==0 al sentinel notFound dado.
func ensureRowAffected(res sql.Result, notFound error) error {
    n, err := res.RowsAffected()
    if err != nil {
        return firebird.MapError(err)
    }
    if n == 0 {
        return notFound
    }
    return nil
}
```

### List con paginación cursor

```go
func (r *VentaRepo) List(
    ctx context.Context,
    p outbound.ListParams,
    f outbound.ListVentasFilters,
) (outbound.Page[*domain.Venta], error) {
    size := clampPageSize(p.PageSize)
    curT, curID, err := decodeCursor(p.Cursor)
    if err != nil {
        return outbound.Page[*domain.Venta]{}, err
    }
    q := firebird.GetQuerier(ctx, r.pool.DB)
    // Los parámetros de tiempo en filtros y cursor también necesitan ToWallClock
    // porque Firebird compara wall-clock contra wall-clock.
    curTWall := firebird.ToWallClock(curT)
    query, args := buildListQuery(size+1, p.Cursor != "", curTWall, curID, f)
    rows, err := q.QueryContext(ctx, query, args...)
    if err != nil {
        return outbound.Page[*domain.Venta]{}, firebird.MapError(err)
    }
    // Scan → trim al size → cargar children → construir Page
    // …
}
```

El query dinámico construye condiciones con `strings.Join` y un slice de args creciente.
Los filtros de fecha también pasan por `firebird.ToWallClock(*f.Desde)` antes de ir al slice.

---

## Tipo B — métodos adicionales (máquina de estados)

### Save con log de transición (sin tabla platform compartida)

A diferencia de ancla-api (que usa una tabla `state_transitions` de platform), msp-api registra las
transiciones en tablas propias del módulo o simplemente en los campos del aggregate root. No hay
`statetransition.Repository` de platform. El repo persiste el cambio de estado como cualquier otro
UPDATE sobre el mismo querier; la lógica de log queda en el módulo si se necesita.

```go
func (r *TraspasoRepo) Save(ctx context.Context, t *domain.Traspaso) error {
    q := firebird.GetQuerier(ctx, r.pool.DB)
    res, err := q.ExecContext(ctx, updateTraspasoEstado,
        t.Estado().String(),
        firebird.ToWallClock(t.Timestamps().UpdatedAt()),
        t.ID().String(),
    )
    if err != nil {
        return firebird.MapError(err)
    }
    return ensureRowAffected(res, domain.ErrTraspasoNotFound)
}
```

### LockByID — bloqueo pesimista

Cuando el servicio de aplicación necesita serializar operaciones concurrentes sobre el mismo
agregado (p. ej. `AplicarVenta`), el repo expone un método de lock explícito que debe llamarse
dentro de una tx:

```go
func (r *VentaRepo) LockByID(ctx context.Context, id uuid.UUID) error {
    q := firebird.GetQuerier(ctx, r.pool.DB)
    var one int
    err := q.QueryRowContext(ctx, lockVentaByID, id.String()).Scan(&one)
    if errors.Is(err, sql.ErrNoRows) {
        return domain.ErrVentaNotFound
    }
    if err != nil {
        return firebird.MapError(err)
    }
    return nil
}
// lockVentaByID = "SELECT 1 FROM MSP_VENTAS WHERE ID = ? WITH LOCK"
```

---

## Queries — `queries.go`

Todas las constantes SQL del módulo viven en un único `queries.go` en el paquete `{module}fb`.
Sin sub-archivos por entidad, sin sqlc.

```go
package ventfb

const insertVenta = `
INSERT INTO MSP_VENTAS (ID, NOMBRE_CLIENTE, ..., CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
VALUES (?, ?, ..., ?, ?, ?, ?)`

const selectVentaByID = `
SELECT ` + ventaColumns + `
FROM MSP_VENTAS v
WHERE v.ID = ?`

const updateVentaHeader = `
UPDATE MSP_VENTAS
SET STATUS = ?, SITUACION = ?, ..., UPDATED_AT = ?, UPDATED_BY = ?
WHERE ID = ?`
```

Reglas:
- Binding siempre con `?` posicional.
- Los SELECTs usan un `const ventaColumns` para garantizar que el orden de columnas en el `Scan`
  del rowmapper coincide exactamente.
- Sin `DEFAULT now()`, sin `RETURNING`, sin funciones SQL que generen IDs.

---

## Rowmappers — `rowmappers.go`

### Struct raw intermedio

Para lecturas con muchas columnas, usa un struct intermedio que acumula los `any` de timestamps
y decimals antes de parsearlos:

```go
// ventaRowRaw es el scan target intermedio para una fila de MSP_VENTAS.
type ventaRowRaw struct {
    idRaw         string
    nombreCliente string
    // ...
    fechaVentaRaw any   // TIMESTAMP → firebird.ScanUTCTime
    montoAnualRaw any   // NUMERIC(14,2) → firebird.ScanDecimal(src, 2)
    createdAtRaw  any
    updatedAtRaw  any
    // campos nullable:
    telefono      sql.NullString
    plazoMeses    sql.NullInt32
    canceledAtRaw any
}
```

Los campos `TIMESTAMP` y `NUMERIC` se declaran como `any` para que el driver entregue su tipo
nativo sin forzar conversión anticipada; los campos de texto y enteros sin escala decimal usan
`string`, `int`, `sql.NullString`, `sql.NullInt32` directamente.

### rowScanner — interfaz compartida

```go
// rowScanner es la superficie mínima satisfecha por *sql.Row y *sql.Rows.
// Los helpers de scan lo aceptan para que funcionen en lectura única e iteración.
type rowScanner interface {
    Scan(dest ...any) error
}
```

### Scan de timestamps y decimals

```go
func parseVentaTimes(r *ventaRowRaw) (ventaTimes, error) {
    createdAt, err := firebird.ScanUTCTime(r.createdAtRaw)  // TIMESTAMP → UTC time.Time
    if err != nil {
        return ventaTimes{}, err
    }
    updatedAt, err := firebird.ScanUTCTime(r.updatedAtRaw)
    if err != nil {
        return ventaTimes{}, err
    }
    canceledAt, err := firebird.ScanNullUTCTime(r.canceledAtRaw) // nullable TIMESTAMP
    if err != nil {
        return ventaTimes{}, err
    }
    // ...
}

func parseVentaMontos(r *ventaRowRaw) (domain.MontoSnapshot, error) {
    anual, err := firebird.ScanDecimal(r.montoAnualRaw, 2)  // NUMERIC(14,2), scale=2
    // ...
}
```

> **Nota sobre agregados NUMERIC:** el driver nakagami/firebirdsql v0.9.19 devuelve `SUM(col)` y
> `AVG(col)` como `*big.Int` sin escalar. `firebird.ScanDecimal` maneja este caso aplicando el
> `scale` para recuperar el punto decimal. En los queries usa `CAST(SUM(...) AS NUMERIC(18,s))`
> para pre-escalar el resultado si necesitas certeza absoluta del tipo devuelto.

### assembleEntity — reconstruye el agregado

```go
func assembleVenta(
    r *ventaRowRaw,
    combos []*domain.Combo,
    productos []*domain.Producto,
    vendedores []*domain.Vendedor,
    imagenes []*domain.Imagen,
) (*domain.Venta, error) {
    ids, err := parseVentaUUIDs(r)
    if err != nil {
        return nil, err
    }
    times, err := parseVentaTimes(r)
    if err != nil {
        return nil, err
    }
    montos, err := parseVentaMontos(r)
    if err != nil {
        return nil, err
    }
    // ... buildClienteSnapshot, buildDireccion, buildPlanCredito, etc.
    return domain.HydrateVenta(domain.HydrateVentaParams{
        ID:         ids.id,
        FechaVenta: times.fechaVenta,
        Montos:     montos,
        Estado:     domain.EstadoRegistro(r.status), // cast directo — DB es fuente de verdad
        Combos:     combos,
        // ...
        CreatedAt:  times.createdAt,
        UpdatedAt:  times.updatedAt,
        CreatedBy:  ids.createdBy,
        UpdatedBy:  ids.updatedBy,
    }), nil
}
```

Reglas del mapper:
- Siempre llama `domain.Hydrate{Entity}(domain.Hydrate{Entity}Params{...})` — cero validación.
- Los VO del enum (`EstadoRegistro`, `TipoVenta`, …) se castean directamente: `domain.{VO}(row.Field)`.
  La DB es la fuente confiable; no llames `Parse{VO}`.
- Los UUID vienen como `CHAR(36)` string — parsear con `parseUUIDColumn("COLUMNA", raw)`, que
  envuelve `uuid.Parse` en un apperror con contexto de columna.
- Los VOs compuestos (`Direccion`, `PlanCredito`) se reconstruyen con su `Hydrate{VO}` sin validación.

---

## Convenciones de manejo de errores

```go
// Fila no encontrada → sentinel de dominio (nunca apperror.NewNotFound aquí)
if errors.Is(err, sql.ErrNoRows) {
    return nil, domain.ErrVentaNotFound
}

// Cualquier otro error de driver → MapError (mapea violaciones de FK/UQ a apperror)
if err != nil {
    return nil, firebird.MapError(err)
}

// RowsAffected == 0 en UPDATE/DELETE → sentinel de dominio
return ensureRowAffected(res, domain.ErrVentaNotFound)
```

Qué hace `firebird.MapError`:
- Violación de unicidad (SQLCODE -803) → `apperror` con código `"firebird_unique_violation"`.
- Violación de FK (SQLCODE -530) → `apperror` con código `"firebird_fk_violation"`.
- Cualquier otro error de driver → lo envuelve con contexto Firebird.

**Nunca** uses `apperror.NewInternal` directamente en el repo. El servicio de aplicación
enriquece los errores de infraestructura con `.WithSource()` si hace falta. El repo solo devuelve
sentinelas de dominio o errores envueltos por `firebird.MapError`.

---

## Helpers de args nullables

Para campos opcionales que mapean a `NULL` en Firebird:

```go
func nullableStringArg(s *string) any {
    if s == nil {
        return nil
    }
    return *s
}

func nullableIntArg(p *int) any {
    if p == nil {
        return nil
    }
    return *p
}

// Para timestamps opcionales — nil → NULL, no-nil → ToWallClock
func nullableWallClockArg(t *time.Time) any {
    if t == nil {
        return nil
    }
    return firebird.ToWallClock(*t)
}
```

Estos helpers viven en el mismo paquete `{module}fb` (no en platform). Son helpers privados del
paquete, no del repo struct.

---

## Transacciones — cuándo usar cada variante

| Variante | Cuándo |
|---|---|
| `firebird.TxManager.RunInTx` | Comando que escribe (Save, Update, outbox) |
| `firebird.TxManager.RunInTxNoWait` | Hot paths donde preferimos fallar rápido ante lock conflict |
| `firebird.RunInReadTx(ctx, pool.DB, fn)` | Lecturas que necesitan un tx explícito para evitar el implicit-tx leak del driver |
| `firebird.RunInSnapshotTx` | Par list+digest que debe ver un point-in-time consistente |
| `pool.ExecRetry(ctx, fn)` | Save de agregado raíz — reintenta ante lock-conflict de Firebird automáticamente |

La re-entrancia es automática: si el contexto ya tiene una tx activa, `RunInTx` / `RunInReadTx`
ejecutan el `fn` dentro de ella sin emitir `BEGIN`/`COMMIT`.

---

## Tests de integración Firebird

### Pool de test

```go
pool := fbtestutil.NewTestFirebirdPool(t)
```

Conecta a la instancia Firebird local configurada via `FB_DATABASE`. Requiere el contenedor
`mueblera-firebird` levantado.

### Rollback automático por test

```go
func TestVentaRepo_Save_PersistsHeader(t *testing.T) {
    pool := fbtestutil.NewTestFirebirdPool(t)
    fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
        repo := NewVentaRepo(pool)
        v, _ := domain.CrearVenta(/* ... */)
        err := repo.Save(ctx, v)
        require.NoError(t, err)
        got, err := repo.FindByID(ctx, v.ID())
        require.NoError(t, err)
        assert.Equal(t, v.ID(), got.ID())
    }) // rollback automático — el shared DB nunca acumula estado
}
```

`WithTestTransaction` abre una tx, inyecta el ctx, ejecuta el test, y hace rollback al terminar.
La DB compartida de dev nunca acumula filas de prueba.

### Convenciones de tiempo en tests

```go
// Siempre time.Date con time.UTC para fixtures.
fixed := time.Date(2026, 5, 13, 18, 0, 0, 0, time.UTC)

// Para round-trip Save → FindByID, comparar con WithinDuration (truncamiento de subsegundo).
assert.WithinDuration(t, expected, got.FechaVenta(), time.Second)
```

Nunca `time.Now()` en assertions — el resultado varía entre hosts.

---

## Notas de encoding (columnas MSP_*)

Las columnas de las tablas `MSP_*` son `CHARACTER SET UTF8`. El driver entrega los strings ya en
UTF-8 como `string` de Go. Usa `string` / `sql.NullString` como scan target — no necesitas
decodificación extra ni `firebird.Win1252`. (El encoding Win1252 solo se usa para las tablas
legacy de Microsip, no para las nuestras.)

---

## Agent checklist

- [ ] Package name `{module}fb`; un struct `{Entity}Repo` con un único campo `pool *firebird.Pool`.
- [ ] Compile-time check: `var _ outbound.{Entity}Repo = (*{Entity}Repo)(nil)`.
- [ ] Todos los métodos obtienen el querier via `firebird.GetQuerier(ctx, r.pool.DB)`.
- [ ] INSERT explicita `ID`, `CREATED_AT`, `UPDATED_AT` como parámetros — sin defaults en DB.
- [ ] Cada `time.Time` pasado a `q.ExecContext` está envuelto en `firebird.ToWallClock(t)`.
- [ ] Cada columna `TIMESTAMP` leída usa `firebird.ScanUTCTime(raw)` o `ScanNullUTCTime(raw)`.
- [ ] Cada columna `NUMERIC` leída usa `firebird.ScanDecimal(raw, scale)` con el scale correcto.
- [ ] UUIDs leídos como `string` y parseados con `parseUUIDColumn("COL", raw)`.
- [ ] `sql.ErrNoRows` mapeado a `domain.Err{Entity}NotFound` — nunca `apperror.NewNotFound` en el repo.
- [ ] Otros errores de driver envueltos con `firebird.MapError(err)`.
- [ ] `ensureRowAffected` usado en UPDATE/DELETE para detectar la fila ausente.
- [ ] Sin `apperror.NewInternal` en el repo — solo en la capa de servicio.
- [ ] El mapper `assembleEntity` llama `domain.Hydrate{Entity}(Hydrate{Entity}Params{...})` — cero validación.
- [ ] VOs de enum desde DB: cast directo `domain.{VO}(row.Field)`, sin `Parse{VO}`.
- [ ] Aggregados NUMERIC vía `SUM`/`AVG`: usar `CAST(SUM(...) AS NUMERIC(18,s))` en SQL, o confiar en `ScanDecimal` con `*big.Int`.
- [ ] Sin optimistic locking — sin campo `version`, sin `WHERE version = $current`.
- [ ] Tests usan `fbtestutil.WithTestTransaction` (rollback automático); fixtures con `time.UTC`.
- [ ] Columnas de tablas `MSP_*` leídas como `string`/`sql.NullString` sin decodificación Win1252.
- [ ] Queries en `queries.go`, rowmappers en `rowmappers.go` — no en el mismo archivo del repo struct.
