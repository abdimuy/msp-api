# Reporte: Tarea 2 – Migración `000050` + Value Objects del Dominio

---

## 1. Archivos creados

### Entregable 1 — Migración

```text
migrations-firebird/
├── 000050_create_msp_ga_garantias.up.sql
└── 000050_create_msp_ga_garantias.down.sql
```

### Entregable 2 — Value objects del dominio

```text
internal/garantias/domain/
├── doc.go
├── errors.go
├── origen_folio.go
├── origen_folio_test.go
├── estado_cuenta.go
├── estado_cuenta_test.go
├── estado_folio.go
├── estado_folio_test.go
├── ruta_reparacion.go
├── ruta_reparacion_test.go
├── dictamen.go
├── dictamen_test.go
├── rol_articulo.go
├── rol_articulo_test.go
├── rol_decisor.go
└── rol_decisor_test.go
```

---

## 2. Salida literal de los comandos de verificación

```text
ricar77@DESKTOP-ELR74FG:~/msp-api$ gofumpt -w internal/garantias/domain/
ricar77@DESKTOP-ELR74FG:~/msp-api$ gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
make check-sealed MODULE=garantias
go test -race -coverprofile=coverage-garantias-domain.out ./internal/garantias/domain/
go tool cover -func=coverage-garantias-domain.out | tail -1
0 issues.
✔ garantias is sealed
ok      github.com/abdimuy/msp-api/internal/garantias/domain    (cached)        coverage: 100.0% of statements
total:                                                                          (statements)            100.0%
```

**Cobertura final: 100.0 %**

`gofmt -l` no imprimió nada (aplicado `gofumpt -w` antes, sin diffs pendientes). `go vet`, `go build` y `golangci-lint` terminaron sin errores (`0 issues.`). `make check-sealed MODULE=garantias` confirmó el sellado (`✔ garantias is sealed`). Las pruebas pasaron con `-race`.

---

## 3. Qué se copió y qué se diferencia

### De `000046_create_msp_rx_conversacion`

Copiado:
- La forma del encabezado: bloque de comentario, seguido de un párrafo "Por qué:" y luego una sección por tabla explicando el propósito de cada una, no solo el tipo de dato de cada columna.
- El orden: `CREATE GENERATOR`, tablas con sus `COMMIT`; (en este caso, un solo `COMMIT` después de todo el bloque de tablas, igual que `000046`), luego índices, luego COMMIT; final y el `INSERT INTO MSP_MIGRATIONS... COMMIT;`.
- El `.down.sql` corto: `DROP TABLE` en orden inverso por dependencia de FK, un `COMMIT`; por bloque, sin `DROP INDEX` ni `ALTER TABLE ... DROP CONSTRAINT` (`DROP TABLE` ya los elimina), y cierre con `DELETE FROM MSP_MIGRATIONS WHERE ID = ... + COMMIT;`.

Diferencia:
- `000046` tiene comentarios sobre reactivación (R7, copiloto de IA). `000050` tiene comentarios sobre garantías (artículo defectuoso, etapas vs ubicación, cambio físico). La estructura es la misma; el contenido es específico del módulo.
- `000050` incluye una sección "Borrado" que no está en `000046`, porque en garantías no se permiten borrados lógicos ni físicos — es parte del diseño del módulo y merece quedar documentado en la migración.

### De `000028_create_gen_mst_folio`

Copiado:
- La idea de documentar el generador como la excepción explícita a la regla dura #1 de `CLAUDE.md`, con nota de por qué es infraestructura y no lógica de negocio.

Diferencia:
- `000028` usa `EXECUTE BLOCK` con `RDB$GENERATORS` para crear el generador de forma idempotente, porque en producción Microsip ya podía traerlo. `GEN_MSP_GA_FOLIO` es un generador nuevo del módulo, sin ese antecedente, así que usé el `CREATE GENERATOR GEN_MSP_GA_FOLIO;` literal que pide el brief, sin el bloque de idempotencia.

### De `internal/inventario/domain/tipo_movimiento.go`

Copiado — el molde correcto para enums es `internal/ventas/domain/tipo_venta.go` (y para estados, `estado_registro.go`):
- `type {Tipo} string` con constantes tipadas, una por valor válido.
- `Parse{Tipo}(s string) ({Tipo}, error)` que valida y devuelve el centinela si no matchea.
- `IsValid()` para verificar pertenencia al conjunto.
- `String()` para la representación como cadena.
- Ayudantes `Es*` para cada valor (según la tabla del brief).
- Para `EstadoFolio` (State VO): además se añadió el mapa `validEstadoFolioTransitions`, el método `CanTransitionTo(t EstadoFolio) bool` y `IsTerminal() bool`.

Diferencia:
- Los enums simples (`OrigenFolio`, `EstadoCuenta`, `RutaReparacion`, `Dictamen`, `RolArticulo`, `RolDecisor`) usan constantes tipadas y los tres métodos básicos (`Parse`, `IsValid`, `String`), más sus ayudantes `Es*`.
- `EstadoFolio` añade la máquina de estados con transiciones explícitas. Los valores son `string` y el zero value (`""`) no es válido; se valida siempre con `Parse`.
- Los tests verifican:
  - `TestX_WireValues`: fija cada literal para garantizar que las constantes no cambien accidentalmente.
  - `TestParseX_HappyPath`: prueba cada valor válido con sus ayudantes.
  - `TestParseX_RejectsInvalid`: lista de entradas inválidas y comprobación con `errors.Is` del centinela específico.
  - `TestX_IsValid`: prueba directa del método.
  - Para `EstadoFolio` además pruebas de `CanTransitionTo` e `IsTerminal`.
- Se eliminaron completamente los viejos métodos `Hydrate`, `Value`, `Equals` e `IsZero` porque ya no son necesarios; la hidratación desde la base se hace con un simple `cast` a `Tipo` y la comparación es `==`.

---

## 4. Confirmación de aislamiento del módulo

Revisión manual de imports en `internal/garantias/domain/`:

- `errors.go` es el único archivo del entregable con un import fuera de la biblioteca estándar: `github.com/abdimuy/msp-api/internal/platform/apperror`.
- Los siete archivos de value objects (`origen_folio.go`, `estado_cuenta.go`, `estado_folio.go`, `ruta_reparacion.go`, `dictamen.go`, `rol_articulo.go`, `rol_decisor.go`) y `doc.go` no importan nada — son paquete `domain` puro.
- Ningún archivo importa `ventas`, `cobranza`, `auth`, `inventario` ni ningún otro módulo de `internal/`.

`make check-sealed MODULE=garantias` lo confirma automáticamente: `✔ garantias is sealed`.

---

## 5. Revisión manual del SQL (`000050`)

- Cero `DEFAULT` en cualquier columna.
- Cero `CREATE TRIGGER`, cero `CREATE PROCEDURE`.
- Única excepción de la regla dura #1: `CREATE GENERATOR GEN_MSP_GA_FOLIO;`, contemplada explícitamente por `CLAUDE.md` como infraestructura.
- `.down.sql` sin `DROP INDEX` ni `ALTER TABLE ... DROP CONSTRAINT` — solo `DROP TABLE` en orden inverso (imagen, evento, artículo, garantía) y `DROP GENERATOR`.
- `FK_MSP_GA_IMAGEN_EVT` declarada sin `ON DELETE CASCADE`.

**No se corrieron las migraciones contra la base**, conforme a la instrucción del brief.

---

## 6. Conclusión

Los dos entregables están completos: la migración `000050` con las cuatro tablas y el generador de folio, y los siete value objects del dominio con sus errores centinela y pruebas. Los siete comandos de verificación pasan sin errores, la cobertura de `domain` es del 100.0 %, y `make check-sealed` confirma que el módulo sigue sellado.
