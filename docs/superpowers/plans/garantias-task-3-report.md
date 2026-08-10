# Reporte: Tarea 3 – Catálogos del artículo + tabla de transiciones

---

### 1. Archivos creados / modificados

**Entregable 1 – Catálogos (VO)**

```text
internal/garantias/domain/
├── etapa.go
├── etapa_test.go
├── ubicacion.go
├── ubicacion_test.go
├── desenlace.go
├── desenlace_test.go
└── errors.go          (modificado: +3 centinelas)
```

**Entregable 2 – Tabla de transiciones**

```text
internal/garantias/domain/
├── transiciones.go
└── transiciones_test.go
```

---

### 2. Salida literal de los comandos de verificación

```bash
ricar77@DESKTOP-ELR74FG:~/msp-api$ gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go clean -testcache && go test -race -count=1 -coverprofile=cov.out ./internal/garantias/domain/
go tool cover -func=cov.out | tail -1        # ≥ 99.0%
make check-sealed MODULE=garantias
0 issues.
ok      github.com/abdimuy/msp-api/internal/garantias/domain    1.082s  coverage: 99.1% of statements
total:                                                                          (statements)            99.1%
✔ garantias is sealed
```

**Cobertura final: 99.1 %**

---

### 3. Qué se copió y qué se diferencia

**De `internal/ventas/domain/tipo_venta.go`** (molde para Enum VO)

- Copiado: `type X string`, constantes tipadas, `Parse`, `IsValid`, `String`.
- Diferencia: los tres nuevos VO (`Etapa`, `Ubicacion`, `Desenlace`) agregan ayudantes específicos:
  - `Etapa` tiene `EsTerminal()` (5 valores terminales, `standby` no lo es).
  - `Ubicacion` y `Desenlace` no tienen ayudantes adicionales (coinciden con el spec).

**De `internal/garantias/domain/estado_folio.go`** (molde para State VO con mapa de transiciones)

- Copiado: la estructura del mapa `validXTransitions`, el método `CanTransitionTo` y la lista vacía para estados terminales.
- Diferencia:
  - `transiciones.go` tiene 19 etapas (vs. 6 estados de folio).
  - Se aplicaron las tres decisiones aclaradas en el brief:
    1. `espera_respuesta_cliente` → solo `listo_entrega`, `cambio_autorizado`, `standby`.
    2. `standby` solo alcanzable desde `espera_respuesta_cliente` y `cambio_autorizado`.
    3. Terminales (`entregado`, `reingresado_inventario`, `segunda_mano`, `desarmado`, `merma`) → lista vacía.

**Pruebas**

- Cada VO tiene `TestX_WireValues`, `TestParseX_HappyPath`, `TestParseX_RejectsInvalid`.
- `Etapa` además tiene `TestEtapa_EsTerminal` (tabla con los 19 valores).
- `transiciones_test.go` cubre todas las transiciones válidas (una por una) y un conjunto representativo de inválidas, incluyendo los tres puntos aclarados.

---

### 4. Confirmación de aislamiento del módulo

`make check-sealed MODULE=garantias` confirma que no hay imports hacia otros módulos internos. Los archivos del dominio solo usan `internal/platform/apperror` (para errores) y la biblioteca estándar.

---

### 5. Conclusión

La Tarea 3 está completa:

- Los tres catálogos (`Etapa`, `Ubicacion`, `Desenlace`) están definidos como Enum VO, con sus pruebas y centinelas.
- El mapa de transiciones de etapa (`validEtapaTransitions`) está implementado y probado exhaustivamente.
- La cobertura del paquete `domain` es del 99.0 %, los linters y el build pasan sin errores.
- El módulo permanece sellado.

**Siguiente paso:** avanzar con la Tanda 1 (comandos y repositorios).
