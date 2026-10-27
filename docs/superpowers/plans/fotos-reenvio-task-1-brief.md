# Fotos de pagos — Tarea 1: medir si un reintento borra el comprobante original

> **Para:** Joao · **Entrega única:** viernes 30 de octubre, 12pm
> **Tipo de tarea:** medición. **No se arregla nada todavía.** El arreglo es la tarea 2 y depende de lo que midas aquí.

## Dónde encaja

Cuando un cobrador registra un pago con foto del comprobante, el teléfono manda el pago y la imagen en una sola petición. La imagen se guarda en el disco del servidor (`STORAGE_DIR`) **antes** de abrir la transacción, porque Firebird no puede meter una escritura de disco dentro de una transacción. Si después algo falla, `cleanupBlobs` borra lo que se escribió.

El problema es esta parte de `internal/cobranza/app/crear_pago_con_imagenes.go` (alrededor de la línea 129):

```go
existing, txErr := s.persistPagoTx(ctx, pago, processed, in.ID, by, now)
switch {
case errors.Is(txErr, errIdempotentReplay):
    s.cleanupBlobs(ctx, storedKeys)
    return existing, nil
```

`errIdempotentReplay` significa "este pago ya existía, es un reintento". Pero la ruta del archivo (`StorageKey`) es **estable**: el reintento escribe la imagen **en la misma ruta que la original** y después la borra. Resultado: el pago queda bien en la base, pero la foto del comprobante desaparece del disco.

Esto ya se midió una vez en dev, en septiembre, pero nunca se escribió una prueba que lo guarde ni se midió cuántas veces pasó en producción. Eso es lo que vas a hacer.

**¿Por qué importa?** El comprobante es la evidencia de que el cliente pagó. Si se pierde y luego hay una reclamación, no hay cómo demostrar el pago.

## Leer antes de escribir (obligatorio, en este orden)

1. `CLAUDE.md` completo. Pon atención especial a la **§8**: esto toca dinero, así que cada afirmación de tu reporte lleva su nivel de evidencia.
2. `docs/registro-de-evidencia.md`: cómo se registra una afirmación y sus cuatro niveles (MEDIDO, LEÍDO, INFERIDO, SUPUESTO).
3. `internal/cobranza/app/crear_pago_con_imagenes.go` completo.
4. `internal/cobranza/infra/cobranzahttp/multipart_helpers.go`: `newStorageKeyPago`, para entender por qué la ruta es estable.
5. `internal/cobranza/app/*_test.go`: busca cómo se arma el `Service` con fakes en las pruebas que ya existen, y copia ese estilo.
6. `internal/ventas/infra/storage/filesystem.go`: el `FilesystemProvider` real que vas a usar en la prueba.

## Día 1 (miércoles 28): entorno

- Clona el repo, instala `lefthook` (`lefthook install`) y levanta el entorno de desarrollo con Docker (`docker compose up -d`).
- Corre `go test -race -short ./internal/cobranza/...` y confirma que todo está en verde **antes** de tocar nada. Pega la salida en tu reporte.
- **Nunca** uses `git commit --no-verify` ni `git push --no-verify`. Si un hook falla, el hook tiene razón: arregla lo que dice.
- La base de desarrollo es **compartida**. Toda prueba que escriba en Firebird va dentro de `fbtestutil.WithTestTransaction`, para que se revierta sola. Si no estás seguro, pregunta antes de correrla.

## Día 2 (jueves 29): la prueba que guarda el defecto

Rama: `test/fotos-reenvio-medicion`, sacada de `main`.

Archivo nuevo: `internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go`

Escribe una prueba que:
1. Arme el `Service` con un **`FilesystemProvider` real** apuntando a `t.TempDir()`, no a un fake de storage: el defecto vive en el disco. Los repositorios y el escritor de Microsip pueden ser los fakes que ya usan las demás pruebas del paquete.
2. Llame a `CrearPagoConImagenes` con un pago y **una** imagen, y compruebe que el archivo existe en disco.
3. Llame **otra vez** con exactamente la misma entrada (mismo `ID` de pago, mismo `ImagenID`, misma `StorageKey`), como haría un reintento del teléfono, y compruebe que la respuesta es el pago existente y sin error.
4. Revise si el archivo de la imagen **sigue existiendo** en disco.

Nombre: `TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal`.

La prueba afirma **lo que pasa hoy**: que el archivo ya no existe. Así, la prueba documenta el defecto y queda en verde. Cuando se arregle en la tarea 2, se pondrá roja y se invertirá. Explícalo en un comentario arriba de la prueba, citando el identificador de evidencia (ver abajo).

**Control positivo, obligatorio.** Demuestra que tu prueba de verdad detecta el borrado:
- Comenta temporalmente la línea `s.cleanupBlobs(ctx, storedKeys)` del caso `errIdempotentReplay`.
- Corre la prueba: tiene que ponerse **roja**.
- Restaura la línea y vuelve a correrla: verde.
- Pega las dos salidas en el reporte.

Una prueba que no se pone roja al quitar lo que guarda no prueba nada.

**Segunda prueba, en el mismo archivo:** el caso que **no** debe borrar nada. Un pago nuevo con imagen, una sola llamada: el archivo existe al final. Es el control de que el camino normal funciona.

## Día 3 (viernes 30, hasta las 12): ¿pasó en producción?

Esto lo **mides tú en dev y lo corre el dueño en producción**. Tú no tienes ni necesitas acceso a producción.

Escribe una herramienta de **sólo lectura** en `scripts/medicion/fotos-pagos-faltantes/main.go`:
- Lee `ID, PAGO_ID, STORAGE_KEY, CREATED_AT` de `MSP_PAGOS_IMAGENES`. Sólo `SELECT`, nada más.
- Para cada fila, revisa con `os.Stat` si el archivo existe bajo `STORAGE_DIR`.
- Imprime cuántas filas hay, cuántas tienen archivo y cuántas no. Para las que no, imprime ID, pago, fecha y ruta.
- Recibe la conexión y `STORAGE_DIR` por variables de entorno, igual que el API. **No** escribas contraseñas en el código ni en el reporte.
- No usa `DELETE`, `UPDATE`, `INSERT` ni `os.Remove`. Agrega una prueba que lea el código fuente y falle si alguna de esas palabras aparece.

Córrela contra **dev** y pega la salida. Para que el conteo de dev sea creíble hace falta control positivo: en una copia local (`t.TempDir()`), borra a mano un archivo que sí tiene fila y demuestra que la herramienta lo reporta como faltante.

**Investiga también, sin correr nada:** ¿`internal/ventas/app/crear_venta_con_imagenes.go` y `internal/visitas/app/registrar_visita_con_imagenes.go` tienen el mismo problema? Responde en el reporte con nivel **LEÍDO** y `archivo:línea`. No escribas pruebas para esos dos módulos en esta tarea.

## Lo que NO se hace en esta tarea

- No cambies `cleanupBlobs` ni nada de `crear_pago_con_imagenes.go`, salvo el cambio temporal del control positivo, que se revierte.
- No toques producción ni corras nada contra ella.
- No escribas datos que se queden en la base de desarrollo.

## Evidencia

Agrega una entrada en `docs/evidencia/cobranza.md` con el **siguiente número libre** (hoy el último es `E-COB-019`; confirma con `git pull` antes de escribir, porque los números nunca se reutilizan). Debe decir:
- Qué se afirmó: "un reintento de un pago con imagen borra el archivo original".
- Nivel: MEDIDO en dev, con la fecha, el comando y la salida.
- Qué falta: el conteo de producción, que lo corre el dueño.

Cita ese identificador en el comentario de la prueba, en el nombre del commit y en tu reporte.

## Archivos que puedes tocar

- `internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go` (nuevo)
- `scripts/medicion/fotos-pagos-faltantes/main.go` y `main_test.go` (nuevos)
- `docs/evidencia/cobranza.md` (sólo agregar tu entrada)
- `docs/superpowers/plans/fotos-reenvio-task-1-report.md` (nuevo, tu reporte)

Nada más. Si crees que necesitas tocar otro archivo, pregunta primero.

## Verificación antes de abrir el PR

```sh
gofmt -l ./internal/cobranza ./scripts/medicion
go vet ./internal/cobranza/... ./scripts/medicion/...
golangci-lint run ./internal/cobranza/... ./scripts/medicion/...
go clean -testcache
go test -race -short ./internal/cobranza/... ./scripts/medicion/...
```

Las cinco tienen que salir limpias. Pega la salida **literal** en el reporte, no un resumen.

## Reporte

Escríbelo en `docs/superpowers/plans/fotos-reenvio-task-1-report.md`, con:
1. El SHA del commit sobre el que corriste todo. Cada salida que pegues tiene que poder reproducirse en ese SHA.
2. La salida de la prueba, en verde, y la del control positivo, en rojo y luego en verde.
3. La salida de la herramienta contra dev y su control positivo.
4. La respuesta sobre ventas y visitas (LEÍDO, `archivo:línea`).
5. Lo que **no** verificaste, dicho claramente. "No lo medí" es una respuesta válida; inventarlo no.

Abre el PR contra `main` con título `test(cobranza): medición del borrado del comprobante al reintentar un pago` y la descripción apuntando al reporte. Commits en formato `tipo(alcance): asunto` (lo revisa el hook).

## Calendario

| Día | Qué |
|---|---|
| Mié 28 | Entorno listo; suite de cobranza en verde |
| Jue 29 | Las dos pruebas y el control positivo |
| Vie 30, 12pm | Herramienta, evidencia, reporte y PR abierto |

Si te atoras más de una hora en algo, pregunta en el PR. No se pierde nada por preguntar; se pierde un día por no hacerlo.
