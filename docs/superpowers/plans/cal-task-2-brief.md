# Task 2 (Frontend) — Campo `aplicaPonderado` + columna "Aplica"

This file is your requirements. Repo: `/Volumes/M2-1TB/Developer/sistema-cobro-web`,
module `src/modules/rutas/` (hexagonal). Work on branch `main` (do NOT push).

## Why
El backend del reporte de cobranza semanal ahora expone, en el drill-down
`GET /v2/rutas/{zona}/cobranza`, un nuevo campo por venta:
`aplica_ponderado: boolean` — indica si esa venta cuenta en el denominador del
"% ponderado" esta semana (hay un vencimiento del calendario en la ventana).
Hay que pasarlo por el pipeline existente (entidad→DTO→mapper→fake→pantalla) y
mostrarlo como columna "Aplica" (badge Sí/No) en el panel de desglose. Read-only.

**Cambio quirúrgico.** NO cambies la firma de `RutasPort`, `useDesgloseCobranza`,
`desgloseCobranza` usecase, ni `HttpRutasAdapter` (el campo fluye solo). El
contrato JSON del backend es `aplica_ponderado` (snake_case, boolean).

## Cambios exactos

### 1. `domain/entities/VentaCobranza.ts`
Agregar al interface: `aplicaPonderado: boolean;` (ponlo después de `saldo` o
donde encaje; estilo del archivo: campos camelCase, una propiedad por línea).

### 2. `infrastructure/http/dtos.ts` (`VentaCobranzaDTO`)
Agregar: `aplica_ponderado: boolean;` al interface `VentaCobranzaDTO`.

### 3. `infrastructure/mappers/dtoToVentaCobranza.ts`
- Agregar una guarda al estilo de las existentes (lanza `DomainError`):
  ```ts
  if (typeof dto.aplica_ponderado !== "boolean") {
    throw new DomainError(
      "aplica_ponderado_invalido",
      "aplica_ponderado debe ser un booleano",
    );
  }
  ```
  Ponla junto a las otras guardas (p.ej. después de la de `saldo`).
- En el objeto de retorno, mapear: `aplicaPonderado: dto.aplica_ponderado,`.

### 4. `application/__tests__/fakeRutasPort.ts` (`makeFakeVentaCobranza`)
Agregar `aplicaPonderado: true,` al objeto `base`.

### 5. `components/RutasScreen.tsx` (componente `DesglosePanel`)
La tabla de desglose hoy tiene 7 columnas. Agrega una columna **"Aplica"**
(sugerencia: justo después de "Frecuencia", o al final antes/después de "Saldo"
— elige la posición más legible; recomendado después de "Aporte"). Pasa a 8
columnas. Debes actualizar TODO lo siguiente para que cuadre:
- Un `<TableHead>` nuevo con el rótulo "Aplica" (copia exactamente el estilo de
  los `<TableHead>` vecinos: `font-mono text-[11px] uppercase tracking-[0.18em]
  text-muted-foreground`; alineación a la izquierda como "Frecuencia", no `text-right`).
- Una `<TableCell>` nueva por fila que renderice un badge **Sí/No** según
  `venta.aplicaPonderado`. Mantén el minimalismo del resto (sin oraciones).
  Sugerencia de badge inline (sin dependencias nuevas):
  ```tsx
  <TableCell className="px-3 py-2 text-sm">
    <span
      className={
        venta.aplicaPonderado
          ? "font-mono text-[11px] text-foreground"
          : "font-mono text-[11px] text-muted-foreground/60"
      }
    >
      {venta.aplicaPonderado ? "Sí" : "No"}
    </span>
  </TableCell>
  ```
  (Si el proyecto ya tiene un componente Badge en `@/components/ui/badge`, puedes
  usarlo; si no, el `<span>` anterior es suficiente y consistente con el estilo
  actual. No agregues dependencias.)
- En el estado de **carga** (skeleton): el `Array.from({ length: 7 })` de celdas
  por fila pasa a `length: 8`.
- En el estado **vacío** ("Sin ventas"): `colSpan={7}` pasa a `colSpan={8}`.

No toques la tabla del listado de zonas (la de arriba) — solo `DesglosePanel`.

## Tests
### `infrastructure/mappers/__tests__/dtoToVentaCobranza.test.ts`
- En `buildValidDTO`, agrega `aplica_ponderado: true,` al objeto base.
- En el test happy-path, agrega `expect(venta.aplicaPonderado).toBe(true);`.
- Agrega un test nuevo: DTO con `aplica_ponderado` no-boolean (p.ej.
  `{ aplica_ponderado: "true" as unknown as boolean }`) → debe lanzar
  `DomainError` con code `aplica_ponderado_invalido` (usa el mismo patrón
  `expect.objectContaining({ code: ... })`).
- Considera agregar un caso happy-path con `aplica_ponderado: false` que
  verifique `venta.aplicaPonderado === false` (para no asumir siempre true).

Los tests de hooks (`useDesgloseCobranza.test.tsx`, etc.) ya pasan vía
`makeFakeVentaCobranza` (que ahora trae el campo).

## Verificación (ejecútala y reporta la salida)
1. `cd /Volumes/M2-1TB/Developer/sistema-cobro-web && npx tsc --noEmit`
2. `npx vitest run src/modules/rutas` (todo verde, incl. el nuevo caso
   `aplica_ponderado`)

## Commit
Un commit a `main`, NO push. Conventional, scope `rutas`, subject en español
neutro, SIN footer de atribución a Claude. Ej:
`feat(rutas): columna aplica ponderado en desglose de cobranza`

## Reglas (del proyecto)
- Strings de UI minimalistas (2-4 palabras), sin oraciones explicativas.
- Español neutro/profesional, sin coloquialismos.
- Arquitectura hexagonal intacta: no cambies puertos/usecases/adapter/hook.
