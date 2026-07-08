# Reactivar un cliente en Microsip — receta B→A

> Receta de escritura para que **msp-api** reactive un cliente dado de baja
> directamente en la base Firebird de Microsip (tabla `CLIENTES`).
>
> Complementa [`microsip-crear-cliente-paso-a-paso.md`](./microsip-crear-cliente-paso-a-paso.md)
> (alta) y [`research/inteligencia-cliente-diccionario-datos.md`](./research/inteligencia-cliente-diccionario-datos.md)
> (diccionario de datos, dominio de `ESTATUS`, `FECHA_SUSP`, `CAUSA_SUSP`).
>
> **Advertencia de evidencia:** esta receta es **inferencia por patrón de datos**
> (5,407 casos de reactivación, 100% consistentes contra la base dev), no una
> traza directa del GUI Delphi capturada con `fbtracemgr` (ver
> [`microsip-trace-runbook.md`](./microsip-trace-runbook.md) para el
> procedimiento). Es suficiente para implementar el flip en Go, pero si se
> quiere la prueba irrefutable de qué UPDATE exacto emite el GUI al reactivar,
> hay que correr el trace.

## TL;DR — el modelo en una frase

**Un solo `UPDATE CLIENTES`** que pone `ESTATUS='A'` y refresca los dos
campos de auditoría (`USUARIO_ULT_MODIF`, `FECHA_HORA_ULT_MODIF`), con guard
`WHERE ESTATUS='B'`. **No se tocan** `FECHA_SUSP` ni `CAUSA_SUSP` — se
conservan como historial de la baja anterior.

## El dominio real de `ESTATUS` (no `'S'`)

`CLIENTES.ESTATUS` es `CHAR(1)`, default `'A'`, con el CHECK constraint
`CHECK_432`:

```sql
CHECK (ESTATUS IN ('A','C','V','B'))
```

**No existe `'S'` (suspendido)** en esta base — corrige la nota anterior en
`microsip-crear-cliente-paso-a-paso.md` y en el diccionario de datos. Valores
reales, verificados contra la base dev:

| `ESTATUS` | Significado | % de la base |
|---|---|---|
| `A` | Activo (con saldo vigente) | 22.6% |
| `B` | **Baja/bloqueado** — lo pone la oficina | 63.0% |
| `V` | Cliente real sin saldo vigente (liquidado/inactivo, con historial de ventas) | 14.3% |
| `C` | Cancelado | 0.2% |

La reactivación que documenta este archivo es **`B` → `A`** — el caso
operativo real (un cliente al que la oficina le puso baja y luego se le
vuelve a otorgar crédito). No hay evidencia de reactivación `V` → `A` ni
`C` → `A` en los datos.

## Evidencia: 5,407 casos de reactivación detectados

De los clientes con `ESTATUS='A'`, solo ~53% tiene `FECHA_SUSP` poblado.
Esos **5,407 clientes `A` con `FECHA_SUSP` no nulo** son ex-suspendidos que
fueron reactivados — quedaron con `ESTATUS='A'` pero conservan la fecha de
su baja anterior. En **el 100% de esos 5,407 casos**, `FECHA_SUSP <=
FECHA_HORA_ULT_MODIF` — es decir, la baja siempre es anterior a la última
modificación registrada, consistente con "se reactivó después de haberse
dado de baja".

**Conclusión verificada: Microsip NO limpia `FECHA_SUSP` (ni `CAUSA_SUSP`)
al reactivar un cliente.** El GUI solo cambia `ESTATUS` y refresca los
campos de auditoría; la fecha y causa de la baja quedan como registro
histórico, visibles aunque el cliente ya esté activo.

## Qué columnas escribe la receta

| Columna | Valor | Notas |
|---|---|---|
| `ESTATUS` | `'A'` | El único cambio de estado |
| `USUARIO_ULT_MODIF` | usuario que ejecuta la acción | Poblada 100% en la base, pero por la app — **ningún trigger la llena**; hay que pasarla desde Go |
| `FECHA_HORA_ULT_MODIF` | `time.Now()` en Go, vía `firebird.ToWallClock` | Idem — sin trigger que la calcule |

## Qué NO se toca

| Columna | Por qué no se toca |
|---|---|
| `FECHA_SUSP` | Se conserva como historial de la baja anterior (verificado: Microsip no la limpia) |
| `CAUSA_SUSP` | Texto libre opcional (ej. `FUGA`, `FALLECIMIENTO`, `MAL CLIENTE`, `Limite de credito excedido`) — no es un código de estado, se conserva igual |
| `USUARIO_AUT_MODIF` | Siempre `NULL` en toda la base — campo legacy, no se usa |

## SQL

```sql
UPDATE CLIENTES
SET ESTATUS = 'A',
    USUARIO_ULT_MODIF = ?,
    FECHA_HORA_ULT_MODIF = ?
WHERE CLIENTE_ID = ?
  AND ESTATUS = 'B';
```

El guard `WHERE ESTATUS='B'` es la invariante de la operación: solo
reactiva clientes que efectivamente estaban en baja. Si el `UPDATE` afecta
0 filas, el cliente no estaba en `'B'` (ya activo, cancelado, o `'V'`) y el
llamador debe tratarlo como no-op o error de precondición, según el flujo
que lo dispare.

## Triggers y lógica alrededor (nada que replicar)

No hay ningún trigger en `CLIENTES` que toque `ESTATUS`, `FECHA_SUSP`,
`CAUSA_SUSP` ni los campos de auditoría — esa lógica vive enteramente en el
GUI Delphi de Microsip (`Cxc.exe`), no en la base. Los triggers reales sobre
`CLIENTES` son:

| Trigger | Cuándo | Efecto |
|---|---|---|
| `CLIENTES_BEFINS` | BEFORE INSERT | Asigna `CLIENTE_ID` vía `GEN_ID` (solo altas, no aplica a reactivación) |
| `MSP_CLIENTES_LISTEN` | AFTER INSERT/UPDATE/DELETE | Escribe a `MSP_CHANGE_LOG` (legacy sync Node→Mongo; sin consumer en msp-api Go) |
| `MSP_SALDOS_CLIENTES_AU` | AFTER UPDATE | Si cambia `ZONA_CLIENTE_ID`, propaga a `MSP_SALDOS_VENTAS` (no aplica — la reactivación no cambia zona) |
| `MSP_PAGOS_CLIENTES_AU` | AFTER UPDATE | Idem para `MSP_PAGOS_VENTAS` |

`CHECK_432` es el CHECK constraint de `ESTATUS`, no un trigger — actúa como
defensa en profundidad a nivel de esquema, pero la regla canónica (quién
puede reactivar, bajo qué condición) vive en el dominio Go (ADR-0006: la
base es un dummy store, la decisión del flip la toma la aplicación).

## Quién dispara el flip

La reactivación **no es un endpoint administrativo aislado**: la dispara el
flujo de **aplicar-venta-a-crédito** desde Go, cuando un cliente en `'B'`
recibe una nueva venta a crédito aprobada por la oficina. Queda detrás de:

- Flag `MICROSIP_REACTIVAR_CLIENTE_ENABLED` (mismo patrón que
  `MICROSIP_VENTA_JUEGOS_ENABLED` para el gap de kits/juegos).
- El guard `WHERE ESTATUS='B'` en el `UPDATE` (arriba) como invariante de
  base, además de cualquier validación en el dominio Go antes de emitir el
  `UPDATE`.

## Referencias

- [`microsip-crear-cliente-paso-a-paso.md`](./microsip-crear-cliente-paso-a-paso.md) — alta de cliente, mismo estilo de receta
- [`research/inteligencia-cliente-diccionario-datos.md`](./research/inteligencia-cliente-diccionario-datos.md) — diccionario de datos, dominio de `ESTATUS`/`FECHA_SUSP`/`CAUSA_SUSP`
- [`microsip-trace-runbook.md`](./microsip-trace-runbook.md) — procedimiento de trace `fbtracemgr` para verificar el UPDATE exacto del GUI
- ADR-0006 — exención de triggers para el adapter Microsip; la decisión del flip vive en Go
