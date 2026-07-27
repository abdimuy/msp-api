# Garantías — Tarea 1: almacenamiento de imágenes (`FilesystemProvider`)

## Dónde encaja

El módulo de garantías guarda evidencia fotográfica de cada evento (recolección, diagnóstico, entrega). Las imágenes viven en el disco local del servidor bajo `STORAGE_DIR`, por [ADR-0003](../../adr/0003-storage-deferred.md) — no hay almacenamiento en la nube, no hay selector de backend.

Esta tarea implementa **solo** el puerto de almacenamiento y su única implementación. Nada más del módulo existe todavía, y esta tarea no necesita que exista: se compila y se prueba sola.

Garantías es un **módulo sellado** ([ADR-0009](../../adr/0009-asistencia-sealed-module.md)): no puede importar ningún otro módulo, ni siquiera sus paquetes de contratos. Solo se permite la biblioteca estándar y `internal/platform/**`. Copiar código de `ventas` está bien; **importar de `ventas` no**.

## Leer antes de escribir (obligatorio)

En este orden:

1. `CLAUDE.md` — reglas duras del proyecto, en particular §2 (vertical slices) y §3 (código en inglés, mensajes al usuario en español).
2. `internal/ventas/ports/outbound/storage.go` — el puerto de referencia.
3. `internal/ventas/infra/storage/filesystem.go` — la implementación de referencia. Es la que hay que adaptar.
4. `internal/ventas/infra/storage/filesystem_test.go` — las pruebas de referencia. El estilo de prueba sale de aquí.
5. `internal/ventas/infra/storage/doc.go` — el comentario de paquete.
6. `docs/module-standards/TESTING_REQUIREMENTS.md` — sección de compuertas de cobertura.

## Qué construir

### Archivo 1 — `internal/garantias/ports/outbound/storage.go`

Crear el paquete `outbound` con **exactamente** este contenido. La firma está fijada por el diseño del módulo; no cambiar nombres, tipos ni orden de parámetros.

```go
package outbound

import (
	"context"
	"io"
)

// StorageObject is the result of a Get call from a StorageProvider.
//
// The caller MUST close Body. ContentType and SizeBytes mirror what was
// passed to Store; providers persist them as sidecar metadata.
type StorageObject struct {
	// Body is the opaque blob payload, ready to stream.
	Body io.ReadCloser
	// ContentType is the MIME type provided at Store time.
	ContentType string
	// SizeBytes is the number of bytes in Body.
	SizeBytes int64
}

// StorageProvider abstracts the binary blob backing store for garantias
// evidence uploads. The only implementation is FilesystemProvider, which
// writes blobs under a local directory. If another backend is ever required,
// add a concrete implementation rather than reintroducing a selector
// abstraction.
//
// Implementations must reject keys that contain path-traversal segments
// (`..`), null bytes, absolute paths, or backslashes. The key shape is
// caller-defined and stable across reads/writes.
type StorageProvider interface {
	// Store writes a new blob under the given key. If a blob already exists
	// at the same key it is overwritten — callers ensure key uniqueness via
	// uuid prefixes. SizeBytes is supplied by the caller because some
	// upstream sources (multipart) do not expose a cheap length check.
	Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error

	// Get fetches a blob by key. The caller MUST close obj.Body.
	Get(ctx context.Context, key string) (StorageObject, error)

	// Delete removes the blob at the given key. Idempotent: returns nil when
	// the key is already absent.
	Delete(ctx context.Context, key string) error
}
```

### Archivo 2 — `internal/garantias/infra/storage/filesystem.go`

`FilesystemProvider`, que implementa el puerto de arriba. Adaptado de `internal/ventas/infra/storage/filesystem.go`, con estos requisitos:

- `NewFilesystemProvider(baseDir string) (*FilesystemProvider, error)` — resuelve `baseDir` a ruta absoluta, crea el árbol de directorios si no existe, y verifica que se pueda escribir. Rechaza `baseDir` vacío o solo espacios.
- **Validación de la clave antes de cualquier E/S.** Se rechaza toda clave que contenga `..`, bytes nulos, rutas absolutas o barras invertidas, y toda clave vacía o mayor a 500 caracteres.
- **Escritura atómica**: escribir a un archivo temporal en el mismo directorio y renombrar al final. Una escritura interrumpida no debe dejar un blob a medias.
- **Archivo lateral de metadatos** `<clave>.meta` con el content-type y el tamaño declarados en `Store`, que `Get` devuelve.
- Permisos: `0o600` para archivos, `0o700` para directorios.
- `Delete` es idempotente: devuelve `nil` si la clave ya no existe.
- Errores construidos con `internal/platform/apperror`. **Código en inglés y snake_case, mensaje en español, en minúsculas y sin punto final** (CLAUDE.md §3). Los errores centinela se declaran una sola vez a nivel de paquete, no dentro de las funciones.

### Archivo 3 — `internal/garantias/infra/storage/doc.go`

Comentario de paquete, siguiendo el estilo de `internal/ventas/infra/storage/doc.go`.

### Archivo 4 — `internal/garantias/infra/storage/filesystem_test.go`

Paquete `storage_test`. Se usa `t.TempDir()`; **no** se escribe fuera del directorio temporal y **no** se toca la base de datos.

Casos que deben estar cubiertos como mínimo:

- Ida y vuelta: `Store` seguido de `Get` devuelve los mismos bytes, content-type y tamaño.
- Sobrescritura: dos `Store` con la misma clave dejan el segundo contenido.
- `Get` de una clave inexistente devuelve error.
- `Delete` de una clave existente la borra, incluido su `.meta`.
- `Delete` de una clave inexistente devuelve `nil`.
- Claves inválidas rechazadas, una por caso: con `..`, con byte nulo, absoluta, con barra invertida, vacía, y mayor a 500 caracteres. **En todos ellos hay que verificar además que no se creó ningún archivo en el directorio base** — no basta con que devuelva error.
- `NewFilesystemProvider` con `baseDir` vacío devuelve error.
- `NewFilesystemProvider` crea el directorio cuando no existe.

## Restricciones

- **Prohibido importar cualquier `internal/` que no sea `internal/garantias/...` o `internal/platform/...`.** Ni `ventas`, ni `cobranza`, ni ningún otro módulo. Copiar el código está bien; importarlo rompe el sellado del módulo y es motivo de rechazo directo.
- No agregar dependencias nuevas al `go.mod`.
- No usar `--no-verify` al commitear bajo ninguna circunstancia.

## Archivos que puede tocar

Solo estos cuatro. Cualquier cambio fuera de esta lista se rechaza sin revisar:

```
internal/garantias/ports/outbound/storage.go
internal/garantias/infra/storage/filesystem.go
internal/garantias/infra/storage/doc.go
internal/garantias/infra/storage/filesystem_test.go
```

## Verificación

Correr esto y pegar la salida completa en el reporte. Los cuatro comandos tienen que pasar:

```sh
gofmt -l internal/garantias
go vet ./internal/garantias/...
go build ./...
golangci-lint run ./internal/garantias/...
go test -race -coverprofile=coverage-garantias-storage.out ./internal/garantias/infra/storage/
go tool cover -func=coverage-garantias-storage.out | tail -1
```

Criterios de aceptación, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` terminan sin errores.
- Las pruebas pasan con `-race`.
- La cobertura total de `infra/storage` es **≥ 85.0%**.

Si cualquiera de los seis comandos falla o la cobertura queda por debajo del 85%, la tarea no está terminada. No se entrega para que alguien más lo revise: se entrega cuando pasa.

## Reporte

Escribir el reporte en `docs/superpowers/plans/garantias-task-1-report.md`, con:

- Los archivos creados.
- La salida literal de los seis comandos de verificación.
- Qué se copió de la implementación de `ventas` y qué se cambió, con el motivo de cada cambio.
- Confirmación explícita de que ningún archivo importa otro módulo.

## Commit

En la rama `feat/garantias`, con mensaje convencional:

```
feat(garantias): almacenamiento de evidencia en filesystem local
```

Sin `--no-verify`. Sin pie de atribución a ninguna herramienta de IA en el mensaje del commit.
