# Comprobantes — Tarea 3: almacenamiento del PDF + canal local

> **Rama:** `feat/comprobantes-infra` (ya creada, sacada de `feat/comprobantes`)
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md)

## Dónde encaja

El módulo de comprobantes manda al cliente un PDF por WhatsApp cuando su venta se registra en Microsip y cuando su pago se aplica. Esta tarea son **dos entregables independientes**, ninguno depende del resto del módulo:

1. **El almacenamiento** del PDF en disco local (ADR-0003).
2. **El canal local**: el sender con el que se prueba todo mientras se aprovisiona la cuenta de WhatsApp Business.

**Ninguno de los dos toca base de datos.** Se prueban con directorios temporales, así que puedes avanzar sin Firebird.

> Si ya hiciste el `FilesystemProvider` de garantías, el primer entregable es el mismo patrón. Aprovéchalo: lo que aprendiste ahí aplica casi línea por línea.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` — reglas duras, en especial la **#3 (código en inglés, mensajes de usuario en español)** y la **#6 (el almacenamiento es filesystem local, sin nubes)**.
2. `internal/ventas/ports/outbound/storage.go` — el puerto de referencia.
3. `internal/ventas/infra/storage/filesystem.go` — la implementación de referencia, la que hay que adaptar.
4. `internal/ventas/infra/storage/filesystem_test.go` — el estilo de prueba.
5. `docs/superpowers/specs/2026-07-29-comprobantes-whatsapp-design.md` §9 — por qué el canal tiene dos implementaciones.

---

# Entregable 1 — Almacenamiento del PDF

## Archivos

```
internal/comprobantes/ports/outbound/storage.go
internal/comprobantes/infra/storage/filesystem.go
internal/comprobantes/infra/storage/doc.go
internal/comprobantes/infra/storage/filesystem_test.go
```

## El puerto — copiar exactamente

`internal/comprobantes/ports/outbound/storage.go`:

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

// StorageProvider abstracts the binary blob backing store for the rendered
// receipt PDFs. The only implementation is FilesystemProvider, which writes
// blobs under a local directory. If another backend is ever required, add a
// concrete implementation rather than reintroducing a selector abstraction.
//
// Implementations must reject keys that contain path-traversal segments
// (`..`), null bytes, absolute paths, or backslashes. The key shape is
// caller-defined and stable across reads/writes.
type StorageProvider interface {
	// Store writes a new blob under the given key. If a blob already exists
	// at the same key it is overwritten — callers ensure key uniqueness via
	// uuid prefixes. SizeBytes is supplied by the caller because some
	// upstream sources do not expose a cheap length check.
	Store(ctx context.Context, key, contentType string, sizeBytes int64, body io.Reader) error

	// Get fetches a blob by key. The caller MUST close obj.Body.
	Get(ctx context.Context, key string) (StorageObject, error)

	// Delete removes the blob at the given key. Idempotent: returns nil when
	// the key is already absent.
	Delete(ctx context.Context, key string) error
}
```

## La implementación

`FilesystemProvider` en el paquete `storage`. Requisitos:

- `NewFilesystemProvider(baseDir string) (*FilesystemProvider, error)` — resuelve `baseDir` a ruta absoluta, crea el árbol si no existe, verifica que se pueda escribir. Rechaza `baseDir` vacío o solo espacios.
- **Validación de la clave antes de cualquier E/S.** Se rechaza toda clave con `..`, bytes nulos, rutas absolutas o barras invertidas, y toda clave vacía o mayor a 500 caracteres.
- **Escritura atómica:** archivo temporal en el mismo directorio y `rename` al final. Una escritura interrumpida no debe dejar un PDF a medias.
- **Archivo lateral `<clave>.meta`** con content-type y tamaño declarados en `Store`, que `Get` devuelve.
- Permisos `0o600` para archivos, `0o700` para directorios.
- `Delete` idempotente: `nil` si la clave ya no existe.
- Errores con `internal/platform/apperror`. Código en inglés snake_case, mensaje en español minúsculas sin punto final. Centinelas a nivel de paquete, no dentro de funciones.

## Pruebas

Paquete `storage_test`, con `t.TempDir()`. No se escribe fuera del temporal y no se toca base de datos.

- Ida y vuelta: `Store` y luego `Get` devuelve los mismos bytes, content-type y tamaño.
- Sobrescritura: dos `Store` con la misma clave dejan el segundo contenido.
- `Get` de clave inexistente devuelve error.
- `Delete` borra el blob **y su `.meta`**.
- `Delete` de clave inexistente devuelve `nil`.
- Claves inválidas, una por caso: con `..`, con byte nulo, absoluta, con barra invertida, vacía, y mayor a 500 caracteres. **En todos hay que verificar además que no se creó ningún archivo en el directorio base** — no basta con que devuelva error.
- `NewFilesystemProvider` con `baseDir` vacío devuelve error.
- `NewFilesystemProvider` crea el directorio cuando no existe.
- Un blob grande (por ejemplo 2 MB, tamaño realista de un PDF con imágenes): ida y vuelta correcta.

---

# Entregable 2 — Canal local

## Por qué existe

El canal real es la API de WhatsApp Business, y aprovisionar esa cuenta más aprobar la plantilla toma semanas. `LocalSender` permite construir y probar el módulo completo mientras tanto, y **se queda para siempre como modo de pruebas**.

Que sea de pruebas no lo hace desechable: es lo que va a usar cualquiera que quiera verificar el flujo sin gastar mensajes de pago.

## Archivos

```
internal/comprobantes/ports/outbound/sender.go
internal/comprobantes/infra/sender/local.go
internal/comprobantes/infra/sender/doc.go
internal/comprobantes/infra/sender/local_test.go
```

## El puerto — copiar exactamente

`internal/comprobantes/ports/outbound/sender.go`:

```go
package outbound

import (
	"context"
	"io"
)

// Destino identifies who receives a rendered receipt.
type Destino struct {
	// ClienteID is the Microsip client id. Opaque to this module.
	ClienteID int
	// Telefono is the destination phone number, already validated as usable.
	Telefono string
}

// Documento is the rendered receipt to deliver.
type Documento struct {
	// Nombre is the file name the recipient sees, e.g. "comprobante-A123.pdf".
	Nombre string
	// ContentType is the MIME type, e.g. "application/pdf".
	ContentType string
	// SizeBytes is the payload length.
	SizeBytes int64
	// Body streams the payload. Senders MUST NOT close it; the caller does.
	Body io.Reader
}

// Sender delivers one Documento to Destino over a channel.
//
// The message body is NOT free text: the WhatsApp Business API only allows
// pre-approved templates for business-initiated messages, so implementations
// receive a template name plus its variables and never compose prose.
type Sender interface {
	// Enviar delivers doc to dest using the named template. Variables are
	// substituted into the template placeholders in order. It returns the
	// channel's message id when the channel accepts the message.
	//
	// A returned error means the channel rejected it; there is no partial
	// success.
	Enviar(ctx context.Context, dest Destino, doc Documento, plantilla string, variables []string) (string, error)

	// Canal identifies which implementation answered. It is persisted on the
	// delivery record so a simulated send can never be counted as a real one.
	Canal() string
}
```

## La implementación

`LocalSender` en el paquete `sender`. Requisitos:

- `NewLocalSender(baseDir string) (*LocalSender, error)` — mismas validaciones de `baseDir` que el `FilesystemProvider`.
- `Enviar` escribe **dos archivos** en `baseDir`:
  - El documento, con el nombre que trae `doc.Nombre` (validado igual que una clave de almacenamiento: sin `..`, sin rutas absolutas, sin barras invertidas).
  - Un `<nombre>.envio.json` con el destino, la plantilla, las variables y el instante — para poder inspeccionar qué se habría mandado.
- Devuelve un identificador de mensaje simulado y estable: `local-<uuid>`.
- `Canal()` devuelve `"local"`. **Nunca `"whatsapp_business"`** — de eso depende que un envío simulado no se cuente como entregado.
- No cierra `doc.Body`. Lo cierra quien llama.

## Pruebas

Paquete `sender_test`, con `t.TempDir()`.

- `Enviar` deja el documento con el contenido correcto.
- `Enviar` deja el `.envio.json` con destino, plantilla y variables.
- El identificador devuelto empieza con `local-` y **es distinto en dos llamadas**.
- `Canal()` devuelve `"local"`.
- Nombre de documento inválido (con `..`, absoluto, con barra invertida, vacío): devuelve error **y no crea archivo alguno**.
- `Enviar` **no cierra** `doc.Body`: pasa un lector que registre si le llamaron `Close` y verifica que no.
- Documento vacío: funciona.

---

## Restricciones

- **Prohibido importar cualquier `internal/` que no sea `internal/comprobantes/...` o `internal/platform/...`.** Copiar código de `ventas` está bien; importarlo no.
- No agregues dependencias al `go.mod`.
- No uses `--no-verify` al commitear.

## Archivos que puedes tocar

Solo estos ocho:

```
internal/comprobantes/ports/outbound/storage.go
internal/comprobantes/ports/outbound/sender.go
internal/comprobantes/infra/storage/filesystem.go
internal/comprobantes/infra/storage/doc.go
internal/comprobantes/infra/storage/filesystem_test.go
internal/comprobantes/infra/sender/local.go
internal/comprobantes/infra/sender/doc.go
internal/comprobantes/infra/sender/local_test.go
```

## Verificación

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
go test -race -coverprofile=coverage-comprobantes-infra.out ./internal/comprobantes/infra/...
go tool cover -func=coverage-comprobantes-infra.out | tail -1
```

Criterios, todos obligatorios:

- `gofmt -l` no imprime nada.
- `go vet`, `go build` y `golangci-lint` sin errores.
- Pruebas en verde con `-race`.
- Cobertura de `infra` **≥ 85.0%**.

Si algún comando falla, la tarea no está terminada. No la entregues para que alguien la revise: entrégala cuando pase.


## Si te atoras

Más de **dos horas** trabado en una sola cosa: avisa. No sigas.

## Reporte

`docs/superpowers/plans/comprobantes-task-3-report.md`, con: archivos creados, salida literal de los seis comandos, qué copiaste de la implementación de `ventas` y qué cambiaste con el motivo de cada cambio, y confirmación explícita de que ningún archivo importa otro módulo.

## Rama y commit

Estás en `feat/comprobantes-infra`. Dos commits, uno por entregable:

```
feat(comprobantes): almacenamiento de comprobantes en filesystem local
feat(comprobantes): canal local para pruebas de envío
```

**No hagas `git push`.** Por ahora no hay acceso de escritura al remoto: tus commits se quedan en tu rama local. Al terminar, avisa y coordinamos cómo se integra tu trabajo.

Sin `--no-verify` bajo ninguna circunstancia, y sin pie de atribución a ninguna herramienta de IA en el mensaje del commit.

