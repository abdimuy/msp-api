# Reporte: Tarea 1 – Almacenamiento de Imágenes (`FilesystemProvider`)

---

## 1. Archivos creados

Se han creado los siguientes archivos dentro del módulo `garantias`, siguiendo la estructura de **vertical slices** y el patrón **puerto‑adaptador**:

```text
internal/garantias/
├── ports/
│   └── outbound/
│       └── storage.go                # Definición del puerto StorageProvider
└── infra/
    └── storage/
        ├── doc.go                    # Documentación del paquete
        ├── filesystem.go             # Implementación concreta (FilesystemProvider)
        └── filesystem_test.go        # Pruebas unitarias (cobertura ≥ 85%)
```

---

## 2. Salida de los comandos de verificación

Todos los comandos se ejecutaron en el entorno **Ubuntu/WSL** y finalizaron con éxito.

```bash
ricar77@DESKTOP-ELR74FG:~/msp-api$ gofumpt -w internal/garantias/infra/storage/filesystem_test.go
ricar77@DESKTOP-ELR74FG:~/msp-api$ gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go test -race -coverprofile=coverage-garantias-storage.out ./internal/garantias/infra/storage/
go tool cover -func=coverage-garantias-storage.out | tail -1
0 issues.
ok      github.com/abdimuy/msp-api/internal/garantias/infra/storage     1.055s  coverage: 85.5% of statements
total:  
```

**Cobertura final: 85.5 %** (supera el mínimo exigido del 85 %).

---

## 3. Cambios respecto a la implementación de `ventas`

- **Lógica central**: se copió íntegramente desde `internal/ventas/infra/storage/filesystem.go` (escritura atómica, sidecar `.meta`, validación de claves y permisos).
- **Únicos cambios en producción**:
  - Ajuste del import del puerto: de `.../ventas/ports/outbound` a `.../garantias/ports/outbound`.
  - Eliminación de cualquier referencia residual al módulo `ventas`.
- **Cambios en pruebas**:
  - Se conservaron todos los casos mínimos exigidos por el brief (ida y vuelta, sobrescritura, claves inválidas, etc.).
  - Se añadieron **pruebas adicionales** para cubrir ramas de error no ejercitadas anteriormente:
    - fallo de `os.Rename` (destino es directorio),
    - fallo de `os.Open` por permisos,
    - fallo de `os.Remove` por permisos,
    - error de `bufio.Scanner` por línea demasiado larga en el sidecar.
  - Esto permitió elevar la cobertura hasta el 85.5 %, superando el umbral exigido.


---

## 4. Confirmación de aislamiento del módulo

El módulo `garantias` es **sellado** según ADR‑0009.  
Se ha verificado que **ningún archivo importa** ningún otro módulo de `internal/` que no sea `internal/platform`.

Los únicos imports utilizados son:

- Biblioteca estándar de Go (`context`, `io`, `os`, `path/filepath`, `strings`, `testing`, etc.)
- `github.com/stretchr/testify/...` (solo en pruebas)
- `internal/platform/apperror` (para errores tipados)

No se importa `ventas`, `cobranza` ni ningún otro módulo de dominio.

---

## 5. Conclusión

La tarea se da por **completada**:

 Los 4 archivos requeridos están creados y cumplen su función. Los comandos de verificación (formato, vet, build, lint, test con race, cobertura) pasan sin errores. La cobertura (85.5 %) supera el umbral mínimo del 85 %. El módulo está aislado y no rompe la regla de sellado.

El adaptador `FilesystemProvider` está listo para ser utilizado por el resto del módulo de garantías cuando se integre en futuras tareas.