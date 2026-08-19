# Task 2 Report — Campo `aplicaPonderado` + columna "Aplica"

## Qué se implementó

5 archivos fuente editados + 1 archivo de tests actualizado, todos en `src/modules/rutas/`:

1. **`domain/entities/VentaCobranza.ts`** — agregado `aplicaPonderado: boolean` al final del interface.
2. **`infrastructure/http/dtos.ts`** — agregado `aplica_ponderado: boolean` a `VentaCobranzaDTO`.
3. **`infrastructure/mappers/dtoToVentaCobranza.ts`** — guarda `typeof dto.aplica_ponderado !== "boolean"` → `DomainError("aplica_ponderado_invalido", ...)` después de la guarda de `saldo`; mapeo `aplicaPonderado: dto.aplica_ponderado` en el objeto de retorno.
4. **`application/__tests__/fakeRutasPort.ts`** — `aplicaPonderado: true` en el objeto `base` de `makeFakeVentaCobranza`.
5. **`infrastructure/mappers/__tests__/dtoToVentaCobranza.test.ts`** — `aplica_ponderado: true` en `buildValidDTO`; `expect(venta.aplicaPonderado).toBe(true)` en el happy-path existente; nuevo test happy-path con `aplica_ponderado: false`; nuevo test de error con `aplica_ponderado: "true" as unknown as boolean`.
6. **`components/RutasScreen.tsx`** — columna "Aplica" en `DesglosePanel` (posición: después de "Aporte", antes de "Saldo"); `<TableHead>` con estilo `font-mono text-[11px] uppercase tracking-[0.18em] text-muted-foreground` alineado a la izquierda; `<TableCell>` con `<span>` Sí/No siguiendo el patrón del brief; skeleton de `length: 7` → `length: 8`; `colSpan={7}` → `colSpan={8}`.

Arquitectura hexagonal intacta: no se tocó `RutasPort`, `useDesgloseCobranza`, `desgloseCobranza` usecase ni `HttpRutasAdapter`.

## Resultados de verificación

### `npx tsc --noEmit`
Sin salida (exitCode 0) — sin errores de tipos.

### `npx vitest run src/modules/rutas`
```
 ✓ src/modules/rutas/infrastructure/mappers/__tests__/dtoToVentaCobranza.test.ts (8 tests) 6ms
 ✓ src/modules/rutas/infrastructure/mappers/__tests__/dtoToRuta.test.ts (11 tests) 6ms
 ✓ src/modules/rutas/presentation/hooks/useDesgloseCobranza.test.tsx (4 tests) 128ms
 ✓ src/modules/rutas/presentation/hooks/useRutas.test.tsx (4 tests) 175ms

 Test Files  4 passed (4)
      Tests  27 passed (27)
   Duration  1.91s
```

27 tests verdes, incluyendo los 2 nuevos (`aplica_ponderado: false` happy-path y `aplica_ponderado_invalido` DomainError).

## Commit

SHA: `4c92f1f`
Mensaje: `feat(rutas): columna aplica ponderado en desglose de cobranza`
Rama: `main` (no pusheado)

## Archivos modificados

- `/Volumes/M2-1TB/Developer/sistema-cobro-web/src/modules/rutas/domain/entities/VentaCobranza.ts`
- `/Volumes/M2-1TB/Developer/sistema-cobro-web/src/modules/rutas/infrastructure/http/dtos.ts`
- `/Volumes/M2-1TB/Developer/sistema-cobro-web/src/modules/rutas/infrastructure/mappers/dtoToVentaCobranza.ts`
- `/Volumes/M2-1TB/Developer/sistema-cobro-web/src/modules/rutas/application/__tests__/fakeRutasPort.ts`
- `/Volumes/M2-1TB/Developer/sistema-cobro-web/src/modules/rutas/infrastructure/mappers/__tests__/dtoToVentaCobranza.test.ts`
- `/Volumes/M2-1TB/Developer/sistema-cobro-web/src/modules/rutas/components/RutasScreen.tsx`

## Self-review

- Posición de la columna "Aplica" (después de Aporte, antes de Saldo): la posición más legible dado que Aporte y Aplica son conceptualmente adyacentes (ambos relacionados con el cálculo ponderado).
- El badge `<span>` no requiere dependencias nuevas; el componente Badge de shadcn no estaba siendo usado en `DesglosePanel` por lo que el `<span>` es consistente con el minimalismo existente.
- Todos los count de columnas actualizados consistentemente (TableHead ×1, skeleton length, colSpan).
- Los hooks tests pasan sin cambio porque `makeFakeVentaCobranza` ya incluye `aplicaPonderado: true`.

## Concerns

Ninguno. Cambio quirúrgico, sin side-effects en otras rutas.
