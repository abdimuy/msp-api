# Comprobantes — Tarea 7: reporte de ejecución

Fecha: 2026-09-03
Estado: **ENTREGADO**
Rama: `feat/comprobantes-infra`

Escrito abriendo los archivos y contando lo que hay, después de dejar los
cuatro entregables sobre su commit. Los dos PDF se abrieron y se revisaron a
mano antes de cerrar la tarea.

## Qué se entregó

Los cuatro entregables de la tanda 2 que implementan los puertos del camino
completo del documento — generar, guardar y entregar — dejando fuera solo el
último salto a WhatsApp (detenido por trámite con Meta).

| # | Entregable | Paquete | Commit |
|---|---|---|---|
| 1 | `FilesystemProvider` — guardar/leer el PDF | `infra/storage/` | `708617c` |
| 2 | `LocalSender` — el canal de pruebas | `infra/sender/` | `cce2baa` |
| 3 | PDF de venta | `infra/render/` | `4a7f461` |
| 4 | PDF de pago | `infra/render/` | `4a7f461` |

Los entregables 3 y 4 comparten un solo tipo, `PDFRenderer`, que implementa
`outbound.Renderer` con sus dos métodos `Venta` y `Pago`. No hay un commit por
renders separado y mezclado: el render lleva la resolución de la fecha.

## Archivos (contados abriendo los paquetes)

### `infra/storage/`

```
filesystem.go          FilesystemProvider — Store/Get/Delete + .meta lateral
doc.go                 package doc
filesystem_test.go     prueba del provider
```

### `infra/sender/`

```
local.go               LocalSender — Enviar/Canal, lateral .envio.json, id "local:"+uuid
doc.go                 package doc
local_test.go          prueba del sender
local_internal_test.go inyección de os.Stat para cubrir baseDir inválido
```

### `infra/render/`

```
render.go              PDFRenderer — Venta y Pago, fuentes, helpers de layout
doc.go                 package doc
render_test.go         pruebas de contrato (Venta/Pago renderizan, determinismo, acentos, 20 artículos, saldo en cero)
render_internal_test.go pruebas internas (money, thousands, formatFechaCorta, propagación de error fpdf)
fonts/
  Poppins-Regular.ttf
  Poppins-SemiBold.ttf
  IBMPlexMono-Regular.ttf
  Poppins-OFL.txt       (licencia, requisito de la OFL)
  IBMPlexMono-OFL.txt   (licencia)
examples/
  comprobante-venta.pdf  (muestra generada, adjunta al PR según §137)
  comprobante-pago.pdf   (muestra generada, adjunta al PR según §137)
```

## Cobertura de los tres paquetes (meta ≥ 85.0 %)

```
ok   github.com/abdimuy/msp-api/internal/comprobantes/infra/render   1.543s   coverage: 95.2% of statements
ok   github.com/abdimuy/msp-api/internal/comprobantes/infra/sender   1.371s   coverage: 89.1% of statements
ok   github.com/abdimuy/msp-api/internal/comprobantes/infra/storage  1.391s   coverage: 89.7% of statements
```

Las tres por encima de la meta. La más alta es el render, que además es la
entrega con más lógica de presentación.

## Salida literal de las siete compuertas, sobre el árbol entregado

```
=== gofmt -l internal/comprobantes ===
(sin salida)

=== go vet ./internal/comprobantes/... ===
(exit 0, sin salida)

=== go build ./... ===
(exit 0, sin salida)

=== golangci-lint run ./internal/comprobantes/... ===
0 issues.

=== make check-sealed MODULE=comprobantes ===
✔ comprobantes is sealed

=== go test -race ./internal/comprobantes/... -count=1 ===
ok  	github.com/abdimuy/msp-api/internal/comprobantes/domain	3.763s
ok  	github.com/abdimuy/msp-api/internal/comprobantes/infra/render	4.414s
ok  	github.com/abdimuy/msp-api/internal/comprobantes/infra/sender	3.729s
ok  	github.com/abdimuy/msp-api/internal/comprobantes/infra/storage	3.607s
?   	github.com/abdimuy/msp-api/internal/comprobantes/ports/outbound	[no test files]
```

La cobertura (`go test -cover ./internal/comprobantes/infra/...`) está en la
sección anterior. Las siete compuertas pasan en verde sobre el commit final.

## Qué copié de los moldes y qué cambié, con el motivo

### Entregable 1 — del molde `internal/ventas/infra/storage/filesystem.go`

Es el mismo puerto palabra por palabra, así que es adaptación, no diseño.

- Copiados tal cual: la escritura atómica (temporal en el mismo directorio +
  `rename`), el archivo lateral `.meta` con content-type y tamaño, `Delete`
  idempotente que borra blob + `.meta`, algoritmos de validación de clave.
- **Cambio 1 · validación de clave estricta antes de toda E/S.** El molde
  valida, pero a esta tarea el brief le exige además que en cada caso de clave
  inválida **no quede basura en disco**: el provider debe rechazar la clave
  antes de haber escrito el temporal. Se fija esa garantía con los centinelas
  a nivel de paquete (`apperror`) y la prueba lo verifica por caso (chequea
  que el directorio base quedó vacío).
- **Cambio 2 · permisos.** `0o600` archivos, `0o700` directorios, como pide el
  brief.
- **Cambio 3 · `baseDir` vacío rechazado** en `NewFilesystemProvider`, que
  además resuelve ruta absoluta, crea el árbol y verifica que se pueda
  escribir.

### Entregable 2 — molde de decisiones del propio brief (no hay paquete exacto)

No hay un `LocalSender` previo salvo la semántica del puerto `outbound.Sender`
y las decisiones cerradas del brief (§67-84). Se implementó al pie de esas
siete decisiones:

- `Enviar` escribe el documento + lateral `<nombre>.envio.json` con
  `ClienteID`, `Telefono`, `Plantilla`, `Variables` **y `enviado_en`** (el
  instante, en UTC) — la traza de «a quién se mandó qué, y cuándo». El
  instante es bookkeeping del canal de pruebas, no una fecha visible al
  usuario, así que no aplica conversión de zona de negocio.
- Prefijo `uuid` en el nombre para no pisar dos envíos del mismo `doc.Nombre`.
- Id `"local:" + uuid`, y `Canal()` devuelve `string(domain.CanalLocal)` — no
  el literal a mano, para que el día que se renombre el canal no quede
  mintiendo.
- **No valida el teléfono** (el dominio decide `sin_telefono` antes) y **no
  cierra `doc.Body`** (lo cierra quien llama). Ambas decisiones del puerto, sin
  duplicarlas.
- Prueba extra pedida por el brief: un lector que además sea `io.Closer`
  sigue con su bandera en falso después de `Enviar` — es el renglón del
  contrato que ninguna otra prueba cubre.
- **Ajuste en pruebas por cobertura.** El `NewLocalSender` original rechazaba
  un `baseDir` inescribible vía `os.Stat`; para cubrir `sender_basedir_unreadable`
  y `sender_basedir_not_directory` se añadió un constructor interno con
  inyección de `os.Stat`, sin cambiar la firma pública del puerto.

### Entregables 3 y 4 — molde `internal/clientes/infra/clientespdf/render.go`

De ahí se copia la **forma**, nunca el paquete (regla dura §31-33): `fpdf.New`
en `Letter` portrait, registro de fuentes TTF vía `AddUTF8FontFromBytes`, y la
estructura de helpers de layout (masthead, etiqueta, celdas).

- **Fuentes copiadas (tres + `*-OFL.txt`).** `Poppins-Regular`,
  `Poppins-SemiBold`, `IBMPlexMono-Regular`, con sus licencias OFL. Sin fuentes
  core de fpdf: Helvetica es Latin-1 y rompería los acentos.
- **Decisión 3 del brief — PDF determinista.** `SetCreationDate(c.Fecha())`,
  nunca `time.Now()`. Además, para que dos renders del mismo modelo sean
  byte-idénticos: `SetCatalogSort(true)` y `SetModificationDate(creation)`
  (el `ModDate` por defecto se estampa con `time.Now()` en `Output`). La prueba
  `TestVenta_Deterministic` lo fija: dos llamadas devuelven los mismos bytes.
- **Cambio por hallazgo · la fecha ahora sí se muestra.** El primer corte del
  render usaba `c.Fecha()` solo para fijar el *creation date* del PDF, así que
  el documento no mostraba la fecha por ningún lado — incumplía el §6.2
  («folio **y fecha**»). Se deshizo ese commit y se resolvió antes de rehacerlo:
  una helper nueva `formatFechaCorta` convierte el instante UTC a la zona de
  negocio (`firebird.BusinessTZ()`, `America/Mexico_City`) y lo imprime en
  `DD/MM/YYYY` bajo la masthead, en ambos comprobantes. El motivo de la zona:
  leer el día UTC crudo mostraría el día siguiente para una captura de la
  noche CDMX — el defecto de día corrido que `DATETIME_HANDLING.md` existe
  para evitar. La prueba `TestFormatFechaCorta` lo clava con el caso frontera
  «22:30 CDMX = 00:30 UTC del día siguiente».
- **Formato de dinero.** Helper `money`/`thousands`: dos decimales y separador
  de miles. `decimal.Decimal` no se imprime crudo.
- **Saldo restante prominente** en el pago (decisión §107): va en grande y en
  rojo bajo «Saldo restante», que es el dato por el que existe el documento.
- **Etiqueta obligatoria (§6.3).** Un recuadro con borde visible:
  «Comprobante informativo, no es un CFDI», en ambos. Va en el renderizador,
  no en el modelo.
- **Propagación del error de fpdf.** `output()` chequea `pdf.Error()` y
  devuelve error, nunca bytes a medias. La prueba usa una página de tamaño
  inexistente para forzar el estado de error de fpdf.

## La verificación que no es una prueba automática

Los dos PDF se generaron y **se abrieron**. Ambos de una página, legibles:

- `comprobante-venta.pdf` — folio, `FECHA 01/09/2026`, cliente con domicilio,
  tabla de artículos (cant/precio/importe), resumen (total/enganche/saldo),
  plan de pago, vendedor, etiqueta.
- `comprobante-pago.pdf` — folio, `FECHA 02/09/2026`, cliente, monto, forma de
  cobro, venta a la que aplica, saldo restante, cobrador, etiqueta.

Siguiendo el §137, los dos PDF van adjuntos a la entrega:
`internal/comprobantes/infra/render/examples/comprobante-{venta,pago}.pdf`.

Un acompañamiento de la revisión: el PDF de muestra se armó con folio y fecha
incoherentes (folio `V-2026-01-…`, fecha septiembre) — fue un artefacto del
generador de muestra, no del renderizador, que siempre pasa el folio tal cual
viene de Microsip. Se regeneraron las muestras con folio y fecha coherentes.

## Re-verificación contra el brief: hallazgos y cierre

A pedido del líder, se releyeron los briefs de las tareas 3 y 7 y se cotejaron
contra el árbol. La implementación ya cumplía todos los requisitos
funcionales y las siete compuertas; el repaso dejó tres brechas explícitas del
brief, todas cerradas en esta pasada:

1. **El lateral del sender no guardaba «el instante».** El task-3 §191 (vigente
   para storage/sender según el task-7 §9) pide que el `.envio.json` registre
   «el destino, la plantilla, las variables **y el instante**», para poder
   inspeccionar qué se habría mandado. El `envioRecord` tenía solo los cuatro
   campos. Se añadió `enviado_en` en UTC (bookkeeping del canal de pruebas, no
   fecha al usuario, así que sin conversión de zona). La prueba del lateral
   ahora verifica que el instante existe y es reciente.
2. **Faltaba la prueba de venta con 20 artículos** (task-7 §133). Se añadió
   `TestVenta_20Articulos`: una venta con veinte artículos renderiza sin error
   y produce un PDF real.
3. **Faltaba la prueba de pago con saldo restante en cero** (task-7 §134). Se
   añadió `TestPago_SaldoRestanteCero`: el cliente que acaba de liquidar se
   renderiza igual, sin error.

## Materializar el cambio en el commit

El brief pide «cuatro commits, uno por entregable, en ese orden, no mezcles»
(§177) y el reporte va después del último commit (§183). La rama se entrega así:

1. `feat(comprobantes)`: almacenamiento — `internal/comprobantes/infra/storage/`.
2. `feat(comprobantes)`: canal local — `internal/comprobantes/infra/sender/`.
3. `feat(comprobantes)`: renderizador PDF de venta y pago —
   `internal/comprobantes/infra/render/` + `fonts/`.
4. `docs(comprobantes)`: este reporte.

Los hallazgos de la re-verificación se absorbieron en el commit de su
entregable (el instante del lateral en el commit del canal; la prueba de 20
artículos y la de saldo en cero, más el `ñ` del fixture, en el commit del
render) y este reporte es el cuarto y último commit. El push lo hace el
titular de la rama, como en los empujones del calendario.

## Verificación de que ningún archivo importa otro módulo

`make check-sealed MODULE=comprobantes` responde `✔ comprobantes is sealed`:
las dependencias transitivas de `internal/comprobantes/...` solo contienen el
propio módulo y `internal/platform/`. Revisado así:

```
go list -deps ./internal/comprobantes/... | grep 'msp-api/internal/' \
  | grep -v 'msp-api/internal/comprobantes\|msp-api/internal/platform' \
  | sort -u
```

No devuelve nada. Los imports reales de los cuatro entregables se limitan a la
biblioteca estándar, `uuid`, `decimal`, `fpdf` y `internal/platform/firebird`
(este último solo para `BusinessTZ()` en el render, parte de `platform` y
permitido por el sellado). Ninguno toca `ventas`, `clientes`, `cobranza` ni
sus contratos — ni siquiera el molde `clientespdf`, que se lee pero no se
importa.

## Fuera de alcance

- La migración `000049` (task-6) no se tocó.
- Los puertos `outbound/storage.go`, `sender.go` y `renderer.go` no se tocaron.
- Nada de `internal/clientes`, ni `.golangci.yml`, ni migraciones, ni dominio.
