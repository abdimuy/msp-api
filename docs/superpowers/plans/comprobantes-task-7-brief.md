# Comprobantes — Tarea 7: el camino completo del documento

> **Rama:** `feat/comprobantes-infra` — **nueva**, sacada de `main` ya actualizado.
> **Spec:** [`2026-07-29-comprobantes-whatsapp-design.md`](../specs/2026-07-29-comprobantes-whatsapp-design.md), **§6** y **§9**
> **Plan:** tareas `2.1`, `2.4`, `2.2` y `2.3` — la tanda 2 completa salvo WhatsApp y HTTP
> **Plazo:** entrega el **jueves 3 de septiembre al final de tu jornada**. Ver el calendario al final.

> **Dos avisos de numeración, los dos importan.**
> 1. El `comprobantes-task-3-brief.md` describe **dos de estos cuatro entregables** —el almacenamiento y el canal local— y su contenido sigue vigente: úsalo para los requisitos y la lista de pruebas de esos dos. Lo que ya no vale de ese documento es el encabezado: la rama es otra, el «no hagas `git push`» quedó atrás, y te pide crear dos puertos que **ya están en `main`**. En todo lo que se contradigan, gobierna éste.
> 2. El `comprobantes-task-6-brief.md` —la migración `000049`— sigue en `main` pero **no es tuya**. La toma el líder. No la empieces.

## Dónde encaja

Los once puertos ya están. Esta tarea escribe **cuatro de las implementaciones**, y son las cuatro que no tocan base de datos ni dependen de la migración:

| # del plan | Qué | Dónde |
|---|---|---|
| `2.1` | `FilesystemProvider` — guardar y leer el PDF | `infra/storage/` |
| `2.4` | `LocalSender` — el canal de pruebas | `infra/sender/` |
| `2.2` | El PDF de **venta** | `infra/render/` |
| `2.3` | El PDF de **pago** | `infra/render/` |

Cuando las cuatro estén, el módulo puede **generar el documento real, guardarlo y entregarlo** de punta a punta. Todo el camino del comprobante menos el último salto a WhatsApp, que está detenido por un trámite con Meta y no por código.

Es también lo primero del módulo que se puede *ver*: hasta hoy todo lo que has escrito son tipos y contratos. Al terminar esto hay dos PDF que se abren.

---

## Las reglas duras

**Ningún archivo de esta tarea importa otro módulo.** Ni `ventas`, ni `clientes`, ni `cobranza` — tampoco sus paquetes de contratos. Sólo `internal/comprobantes/...`, `internal/platform/...`, la biblioteca estándar, `uuid`, `decimal` y `fpdf`.

Esto vale también para el molde del PDF: vas a leer `internal/clientes/infra/clientespdf/render.go` para aprender la forma, y **no vas a importar ni una línea de ahí**. Se copia el patrón, no el paquete.

**Ninguno de los cuatro toca base de datos.** Se prueban con `t.TempDir()` y con modelos construidos a mano. Si en algún momento necesitas Firebird para probar algo de esta tarea, algo se torció: para.

**Los tres puertos ya están escritos y no se tocan.** `ports/outbound/storage.go`, `sender.go` y `renderer.go` entraron a `main` en el PR #16 — los escribiste tú. Tu trabajo es implementarlos exactamente como están, no ajustarlos para que te queden cómodos. Si una firma te estorba, dilo antes de cambiarla.

---

# Entregable 1 — `FilesystemProvider`

`internal/comprobantes/infra/storage/{filesystem.go,doc.go,filesystem_test.go}`

Implementa `outbound.StorageProvider`. Los requisitos completos y la lista de pruebas están en la sección «Entregable 1» del `comprobantes-task-3-brief.md`; no los repito. Lo esencial:

- `NewFilesystemProvider(baseDir string) (*FilesystemProvider, error)` — resuelve a ruta absoluta, crea el árbol, verifica que se pueda escribir, rechaza `baseDir` vacío.
- **Validación de la clave antes de cualquier E/S**: nada de `..`, bytes nulos, rutas absolutas ni barras invertidas; ni vacía ni de más de 500 caracteres.
- **Escritura atómica**: temporal en el mismo directorio y `rename` al final.
- Archivo lateral `<clave>.meta` con content-type y tamaño; `Get` los devuelve.
- Permisos `0o600` para archivos, `0o700` para directorios.
- `Delete` idempotente, y borra **el blob y su `.meta`**.
- Errores con `apperror`, centinelas a nivel de paquete.

**El molde es `internal/ventas/infra/storage/filesystem.go`** (346 líneas) con su prueba (513 líneas). Es el mismo puerto palabra por palabra, así que esto es adaptación, no diseño.

**El termómetro de este entregable:** en cada caso de clave inválida hay que verificar **además que no se creó ningún archivo** en el directorio base. Que devuelva error no basta — un provider que rechaza la clave después de haber escrito el temporal pasa esa prueba y deja basura en disco.

---

# Entregable 2 — `LocalSender`

`internal/comprobantes/infra/sender/{local.go,doc.go,local_test.go}`

Implementa `outbound.Sender`. Existe porque aprovisionar la cuenta de WhatsApp Business y aprobar la plantilla toma semanas, y sin este canal el módulo no se puede probar de punta a punta. **No es desechable:** se queda para siempre como modo de prueba, y es lo que va a usar cualquiera que quiera verificar el flujo sin gastar mensajes de pago.

Decisiones cerradas, para que no adivines ninguna:

1. `NewLocalSender(baseDir string) (*LocalSender, error)` — misma validación de `baseDir` que el provider.
2. `Enviar` escribe **dos** archivos: el documento con los bytes exactos que recibió, y un lateral `<mismo-nombre>.envio.json` con `ClienteID`, `Telefono`, `Plantilla` y `Variables`. Ese lateral es el registro de "a quién se le mandó qué"; sin él, el canal local no prueba nada.
3. El nombre del archivo lleva un prefijo `uuid` para que dos envíos del mismo `doc.Nombre` no se pisen.
4. El id que devuelve es `"local:" + uuid`. **El prefijo no es adorno:** es lo que impide que un id de prueba se confunda con uno de WhatsApp cuando alguien lea la columna `MENSAJE_EXTERNO_ID` dentro de seis meses.
5. `Canal()` devuelve `string(domain.CanalLocal)`, **no** el literal `"local"` escrito a mano. Así el día que alguien renombre el canal en el dominio, esto no se queda mintiendo en silencio.
6. **No valida el teléfono.** El puerto dice que `Destino.Telefono` viene "already validated as usable", y quién no tiene teléfono se decide antes, en el dominio, con el estado `sin_telefono`. Validar otra vez aquí duplicaría la regla en dos lugares.
7. **No cierra `doc.Body`.** El puerto lo dice explícitamente: lo cierra quien llama.

Pruebas, además de las del `task-3-brief`:

- `Enviar` deja los dos archivos, y el documento tiene **los bytes exactos** que se le pasaron.
- El lateral trae el destino, la plantilla y las variables en el orden recibido.
- Dos envíos con el mismo `doc.Nombre` no se pisan.
- Los ids de dos llamadas son distintos y ambos empiezan con `local:`.
- `Canal()` coincide con `domain.CanalLocal`.
- **Que no cierre el cuerpo:** pásale un lector que además sea `io.Closer` y lleve una bandera; después de `Enviar`, la bandera sigue en falso. Es un renglón del contrato del puerto que ninguna otra prueba cubre.

---

# Entregables 3 y 4 — los dos PDF

`internal/comprobantes/infra/render/{render.go,doc.go,render_test.go}` y `fonts/`

Implementa `outbound.Renderer`, cuyos dos métodos ya están escritos:

```go
Venta(ctx context.Context, c domain.ComprobanteVenta) ([]byte, error)
Pago(ctx context.Context, c domain.ComprobantePago) ([]byte, error)
```

**El molde es `internal/clientes/infra/clientespdf/render.go`**, con `github.com/go-pdf/fpdf v0.9.0`, que ya está en el `go.mod`. Son 1,234 líneas y no necesitas nada parecido: ese reporte tiene tablas paginadas y el tuyo es una hoja. Lo que se copia de ahí es la forma — `fpdf.New("P", "mm", "Letter", "")`, el registro de fuentes, los helpers de posición.

### Qué lleva cada documento (§6.2, y no es negociable)

**Venta:** folio y fecha · nombre y domicilio del cliente · los artículos con cantidad y precio · total, enganche y saldo · el plan de pago en palabras claras · vendedor.

**Pago:** folio y fecha · nombre del cliente · monto y forma de cobro · a qué venta se aplicó · **el saldo restante después de este pago** · quién cobró.

Ese saldo restante es el dato por el que existe el comprobante de pago. Un papel que sólo diga "recibimos $500" deja abierta justo la pregunta que el documento venía a cerrar, y genera la llamada que venía a evitar. Va donde se vea, no en una esquina.

Los dos modelos ya están en `domain` y los escribiste tú: todo sale de sus getters. **El renderizador no lee nada de ningún lado** — recibe el modelo armado y lo dibuja. Si te falta un dato, el que está incompleto es el modelo y hay que hablarlo, no leerlo por atrás.

### La etiqueta obligatoria (§6.3)

Los dos documentos dicen, **visible**, que son un comprobante informativo y **no un CFDI**.

No es una formalidad: un papel con folio, monto y sello que llega por WhatsApp se parece lo suficiente a un comprobante fiscal para que alguien lo trate como tal. Y va en el renderizador, no en el modelo: es presentación, y el modelo no tiene presentación.

### Decisiones cerradas

1. **Las fuentes se copian.** `clientespdf` embebe sus `.ttf` con `//go:embed fonts/*.ttf`; tú necesitas las tuyas en `internal/comprobantes/infra/render/fonts/`. Copia **tres** —`Poppins-Regular`, `Poppins-SemiBold`, `IBMPlexMono-Regular`— y **con sus archivos de licencia `*-OFL.txt`**, que es un requisito de la licencia de las fuentes, no una costumbre. Son ~450 KB. Sí, quedan duplicadas con las de `clientes`: compartirlas exigiría moverlas a `platform`, que es una refactorización de otro módulo y no es de esta tarea.
2. **Nada de fuentes core de fpdf.** Helvetica y compañía son Latin-1: «teléfono», «dirección» y cualquier apellido con acento salen rotos. Van las TTF por `AddUTF8FontFromBytes`, igual que el molde.
3. **El PDF tiene que ser determinista.** `fpdf` estampa la fecha de creación en los metadatos, así que dos llamadas con el mismo modelo darían bytes distintos y ninguna prueba de contenido sería estable. Fija la fecha con `pdf.SetCreationDate(c.Fecha())` —el instante del comprobante, que ya viene en el modelo—, nunca con `time.Now()`. Es también lo que hace que el documento sea reproducible: regenerarlo dentro de un año da el mismo archivo.
4. **Un solo tipo para los dos métodos:** `type PDFRenderer struct{}` con `NewPDFRenderer()`. No hay razón para dos.
5. **Tamaño de página `Letter`**, como el molde. Es lo que imprime la oficina.
6. **Los importes se formatean en el renderizador**, a dos decimales y con separador de miles. `decimal.Decimal` no se imprime crudo: `1234.5` en un comprobante que ve un cliente se lee mal.

### Pruebas

No hay comparación byte a byte contra un archivo dorado: un cambio de una fuente lo rompería sin que nada esté mal. Lo que sí se prueba:

- Los bytes empiezan con `%PDF-` y pesan lo que pesa un documento real (más de unos pocos KB, no 300 bytes).
- **Dos llamadas con el mismo modelo dan bytes idénticos.** Ésta es la que verifica de verdad que fijaste la fecha de creación.
- Un modelo con acentos y con `ñ` en el nombre del cliente no devuelve error.
- Una venta con muchos artículos —pon veinte— no revienta ni devuelve error.
- Un comprobante de pago con saldo restante en cero se renderiza igual: es el caso del cliente que acaba de liquidar, y es el que más ilusión le hace ver.
- El error de `fpdf` se propaga: si `pdf.Error()` no es nil al final, `Venta`/`Pago` devuelven error y no bytes a medias.

**Y una verificación que no es una prueba automática, pero es obligatoria:** genera los dos PDF, ábrelos, y **mándamelos** en el punto de control del miércoles. Un documento que pasa todas las pruebas y se ve mal sigue estando mal, y eso sólo se ve mirándolo.

---

## Las compuertas de esta entrega

```sh
gofmt -l internal/comprobantes
go vet ./internal/comprobantes/...
go build ./...
golangci-lint run ./internal/comprobantes/...
make check-sealed MODULE=comprobantes
go test -race ./internal/comprobantes/... -count=1
go test -cover ./internal/comprobantes/infra/...
```

**Cobertura de los tres paquetes de `infra` ≥ 85.0%.** Es la meta que el spec fija para storage y la que se aplicó en garantías.

Si algún comando queda en rojo, la tarea no está terminada: no la entregues para que alguien la revise, entrégala cuando pase.

---

## Archivos que puedes tocar

```
internal/comprobantes/infra/storage/filesystem.go
internal/comprobantes/infra/storage/doc.go
internal/comprobantes/infra/storage/filesystem_test.go
internal/comprobantes/infra/sender/local.go
internal/comprobantes/infra/sender/doc.go
internal/comprobantes/infra/sender/local_test.go
internal/comprobantes/infra/render/render.go
internal/comprobantes/infra/render/doc.go
internal/comprobantes/infra/render/render_test.go
internal/comprobantes/infra/render/fonts/*.ttf   (copiadas, con su *-OFL.txt)
docs/superpowers/plans/comprobantes-task-7-report.md
```

**Cualquier cambio fuera de esa lista se rechaza sin revisar.** No toques los puertos, ni el dominio, ni ninguna migración, ni `.golangci.yml`, ni nada de `internal/clientes`.

Cuatro commits, uno por entregable, en ese orden. No mezcles.

---

## El reporte

`docs/superpowers/plans/comprobantes-task-7-report.md`, con la regla que ya cumpliste bien la vez pasada: **se escribe al final, después del último commit, abriendo los archivos y contando lo que hay.**

Lleva la salida literal de las siete compuertas sobre el commit final, la cobertura de los tres paquetes por separado, qué copiaste de los moldes de `ventas` y `clientes` y qué cambiaste con el motivo de cada cambio, y la confirmación explícita de que ningún archivo importa otro módulo.

---

## Calendario y puntos de control

| Cuándo | Qué |
|---|---|
| **Viernes 28 (hoy)** | Leer: este brief, las dos secciones del `task-3-brief`, `ventas/infra/storage/filesystem.go` y el §6 del spec. |
| **Lunes 31** | Los dos entregables de adaptación: `FilesystemProvider` y `LocalSender`, con sus pruebas. |
| **Lunes 31, fin de jornada** | **Punto de control: storage y sender en verde.** Mándame la salida de `go test -race -cover` de los dos paquetes. |
| **Martes 1 de septiembre** | El PDF de venta. |
| **Miércoles 2** | El PDF de pago. |
| **Miércoles 2, fin de jornada** | **Punto de control: los dos PDF generados.** Mándame los dos archivos, no una captura. |
| **Jueves 3** | La cobertura de los tres paquetes al 85%, el reporte y el PR. |
| **Jueves 3, fin de jornada** | **Entrega.** |

Los dos entregables del lunes son adaptación de código que ya existe en el repositorio y que hace exactamente lo mismo; el trabajo de diseño de la semana está en los dos documentos.

De esto dependen tres tareas que arrancan en cuanto entre: el encolado desde `venta.aplicada` necesita el renderizador y el almacenamiento, y el worker de envío necesita el canal. Si algo se atora más de dos horas, avisa el mismo día — no el jueves.
