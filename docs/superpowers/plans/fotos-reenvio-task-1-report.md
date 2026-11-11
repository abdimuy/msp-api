# Reporte Tarea 1: Medición de borrado de comprobantes al reintentar pago

**Commit de código y pruebas (SHA medido):** `72270ff` (`joaov@VAZHER`, Ubuntu sobre WSL2 en Windows)

**Obtención y trazabilidad del SHA:**
- El SHA del último commit que modifica código y pruebas Go es `72270ff`, obtenido con `git rev-parse --short HEAD` antes del commit posterior de documentación.
- Los archivos Go (`crear_pago_con_imagenes_reintento_test.go`, `scripts/medicion/fotos-pagos-faltantes/main.go` y `main_test.go`) permanecen idénticos byte a byte desde `72270ff` (comprobado con `git diff 72270ff HEAD -- "*.go"` sin salida).
- Frente a `main` (`f2bd21b`), `git diff --name-only f2bd21b...HEAD` incluye únicamente los 5 archivos permitidos de la Parte 1:
  - `docs/evidencia/cobranza.md`
  - `docs/superpowers/plans/fotos-reenvio-task-1-report.md`
  - `internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go`
  - `scripts/medicion/fotos-pagos-faltantes/main.go`
  - `scripts/medicion/fotos-pagos-faltantes/main_test.go`

## 1. Verificación Inicial (Día 1)

Antes de realizar cualquier modificación, se comprobó que la suite de cobranza corriera exitosamente en verde tras limpiar la caché de pruebas.

Comando ejecutado:

```
go clean -testcache
CGO_ENABLED=1 go test -race -short ./internal/cobranza/...
```

Salida:

```
ok  	github.com/abdimuy/msp-api/internal/cobranza	1.043s
ok  	github.com/abdimuy/msp-api/internal/cobranza/app	2.259s
ok  	github.com/abdimuy/msp-api/internal/cobranza/domain	1.191s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp	1.458s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents	1.025s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/microsip	1.028s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb	9.829s
?   	github.com/abdimuy/msp-api/internal/cobranza/ports/outbound	[no test files]
```

## 2. Prueba del Defecto y Control Positivo (Día 2)

Se creó la prueba `TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal` (Evidencia: **E-COB-060**) que documenta el comportamiento actual (borrado del archivo en reintento).

**Nota sobre el Storage de la prueba:** Se utilizó `localTestStorage` (escribiendo físicamente sobre `t.TempDir()`) en lugar del `FilesystemProvider` real porque `depguard` impide importar `infra` desde `app` (`app-no-infra`, `.golangci.yml:459`). En la Parte 2 se cierra con una prueba en `cmd/api` usando el proveedor real.

**Prueba inicial en verde (demostrando el defecto):**

```
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go clean -testcache
go test -run TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal -v ./internal/cobranza/app/
=== RUN   TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== PAUSE TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== CONT  TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
--- PASS: TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal (0.00s)
PASS
ok      github.com/abdimuy/msp-api/internal/cobranza/app        0.015s
```

**Control Positivo (Prueba en rojo):**
Al comentar temporalmente la línea `s.cleanupBlobs(ctx, storedKeys)` del caso `errIdempotentReplay` (`internal/cobranza/app/crear_pago_con_imagenes.go:132`), la prueba falló como se esperaba:

```
=== RUN   TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== PAUSE TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== CONT  TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
    crear_pago_con_imagenes_reintento_test.go:150:
                Error Trace:    /mnt/c/Users/joaov/Documents/msp-api/internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go:150
                Error:          An error is expected but got nil.
                Test:           TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
                Messages:       Evidencia E-COB-060: os.Stat debe devolver error porque el archivo físico ya no existe tras el reintento debido al borrado de cleanupBlobs
--- FAIL: TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal (0.00s)
FAIL
FAIL    github.com/abdimuy/msp-api/internal/cobranza/app        0.017s
FAIL
```

**Restauración (Prueba nuevamente en verde):**
Al restaurar la línea de código, la prueba volvió a pasar:

```
=== RUN   TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== PAUSE TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== CONT  TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
--- PASS: TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal (0.00s)
PASS
ok      github.com/abdimuy/msp-api/internal/cobranza/app        0.016s
```

## 3. Script de Medición (Día 3)

**Pruebas de la herramienta (Regla de oro, validación de `STORAGE_DIR` y control positivo):**

```
=== RUN   TestMain_ReglaDeOro_NoContieneMutaciones
--- PASS: TestMain_ReglaDeOro_NoContieneMutaciones (0.00s)
=== RUN   TestStorageDir_FallaSiNoExisteOVacio
--- PASS: TestStorageDir_FallaSiNoExisteOVacio (0.00s)
=== RUN   TestStorageDir_FallaSiNoEsDirectorio
--- PASS: TestStorageDir_FallaSiNoEsDirectorio (0.00s)
=== RUN   TestStorageDir_ImprimeRutaAbsolutaResuelta
--- PASS: TestStorageDir_ImprimeRutaAbsolutaResuelta (0.00s)
=== RUN   TestContarArchivos_ControlPositivo_ReportaFaltante
--- PASS: TestContarArchivos_ControlPositivo_ReportaFaltante (0.00s)
PASS
ok      github.com/abdimuy/msp-api/scripts/medicion/fotos-pagos-faltantes       0.018s
```

**Control Positivo de la Guarda Anti-Escritura (Mutante `db.Exec("delete from MSP_PAGOS_IMAGENES")`):**

Parche temporal aplicado sobre `scripts/medicion/fotos-pagos-faltantes/main.go`:

```diff
--- a/scripts/medicion/fotos-pagos-faltantes/main.go
+++ b/scripts/medicion/fotos-pagos-faltantes/main.go
@@ -110,6 +110,7 @@
 // LeerRegistros consulta la base de datos para obtener las fotos de pagos registradas.
 func LeerRegistros(ctx context.Context, db *sql.DB) ([]RegistroImagen, error) {
+	_, _ = db.Exec("delete from MSP_PAGOS_IMAGENES")
 	const consulta = "SELECT ID, PAGO_ID, STORAGE_KEY, CREATED_AT FROM MSP_PAGOS_IMAGENES"
 	rows, err := db.QueryContext(ctx, consulta)
 	if err != nil {
```

Salida en rojo al correr la guarda anti-escritura con el mutante inyectado:

```
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go clean -testcache && go test -run TestMain_ReglaDeOro_NoContieneMutaciones -v ./scripts/medicion/fotos-pagos-faltantes/
=== RUN   TestMain_ReglaDeOro_NoContieneMutaciones
=== PAUSE TestMain_ReglaDeOro_NoContieneMutaciones
=== CONT  TestMain_ReglaDeOro_NoContieneMutaciones
    main_test.go:63: REGLA DE ORO VIOLADA: main.go contiene la cadena prohibida "delete" (case-insensitive)
    main_test.go:63: REGLA DE ORO VIOLADA: main.go contiene la cadena prohibida "exec" (case-insensitive)
    main_test.go:79: REGLA DE ORO VIOLADA: main.go invoca o referencia el metodo prohibido "Exec"
--- FAIL: TestMain_ReglaDeOro_NoContieneMutaciones (0.01s)
FAIL
FAIL    github.com/abdimuy/msp-api/scripts/medicion/fotos-pagos-faltantes       0.024s
FAIL
```

**Ejecución contra dev (Mueblera Firebird):**

```
/mnt/c/Users/joaov/Documents/msp-api/var/uploads
Total de filas: 0
ConArchivo: 0
Sin archivo: 0
Error al revisar: 0
```

*(Nota: La base de datos local de dev no contiene registros en la tabla de imágenes, pero la consulta y la conexión fueron exitosas. Pendiente ejecución en producción por el dueño).*

## 4. Investigación en otros módulos

Revisión del código de otros módulos para identificar si comparten el mismo defecto.

* **Ventas:** \[INFERIDO\] Se encuentra el flujo de limpieza en `internal/ventas/app/crear_venta_con_imagenes.go:101` y la ruta del archivo es estable (`internal/ventas/infra/venthttp/multipart.go:24-30`). Sin embargo, **no está demostrado** que un reintento llegue a `storeAllImagenBlobs` (`internal/ventas/app/crear_venta_con_imagenes.go:84`): antes están las validaciones (`internal/ventas/app/crear_venta_con_imagenes.go:61, 68, 80`) y el middleware de idempotencia (`cmd/api/server.go:221` y `internal/ventas/infra/venthttp/handlers_ventas.go:54-57`). La Parte 2 lo mide.

* **Visitas:** \[LEÍDO\] **No presenta el problema.** Verificado en `internal/visitas/app/registrar_visita_con_imagenes.go:106-117` y `planImagenes` (`:139-148`), donde primero consulta qué existe antes de escribir en almacenamiento.

## 5. Lo que NO se verificó

* **No se midió la base de datos de producción:** La ejecución del script contra la base de datos productiva está pendiente y debe ser realizada por el dueño (sin acceso desde este entorno).

* **No se ejecutaron aún las pruebas de medición para Ventas ni Visitas en esta rama:** La revisión de estos módulos en la Parte 1 es estática (lectura de código); la medición con pruebas entra en la Parte 2.

* **No se implementó ninguna corrección (fix) en la Parte 1:** Se mantuvo estrictamente el alcance de medición y documentación del defecto (`E-COB-060`) según las instrucciones.

## 6. Verificación de Reglas (Pre-PR)

Verificación ejecutada en WSL (`joaov@VAZHER`) con árbol limpio, verificando la equivalencia exacta de todo el código Go con `72270ff`:

```text
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ git rev-parse --short 72270ff
72270ff
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ git diff 72270ff HEAD -- "*.go"
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ git diff --name-only f2bd21b...HEAD
docs/evidencia/cobranza.md
docs/superpowers/plans/fotos-reenvio-task-1-report.md
internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go
scripts/medicion/fotos-pagos-faltantes/main.go
scripts/medicion/fotos-pagos-faltantes/main_test.go
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ gofmt -l ./internal/cobranza ./internal/ventas ./internal/visitas ./cmd/api ./scripts/medicion
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go vet ./internal/cobranza/... ./internal/ventas/... ./internal/visitas/... ./cmd/api/... ./scripts/medicion/...
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go build ./...
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ golangci-lint run --timeout 15m ./...
0 issues.
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go clean -testcache
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go test -race -short ./internal/cobranza/... ./internal/ventas/... ./internal/visitas/... ./cmd/api/... ./scripts/medicion/...
ok  	github.com/abdimuy/msp-api/internal/cobranza	1.042s
ok  	github.com/abdimuy/msp-api/internal/cobranza/app	2.253s
ok  	github.com/abdimuy/msp-api/internal/cobranza/domain	1.095s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp	1.620s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents	1.030s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/microsip	1.028s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb	7.872s
?   	github.com/abdimuy/msp-api/internal/cobranza/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/ventas/app	4.079s
ok  	github.com/abdimuy/msp-api/internal/ventas/domain	1.097s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/failedintents	1.023s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/microsip	1.025s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/storage	1.030s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventfb	1.046s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventfirestore	1.052s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/venthttp	4.934s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventoutbox	1.039s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch	1.055s
?   	github.com/abdimuy/msp-api/internal/ventas/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/visitas/app	1.546s
ok  	github.com/abdimuy/msp-api/internal/visitas/domain	1.054s
ok  	github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb	1.035s
ok  	github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp	1.249s
ok  	github.com/abdimuy/msp-api/internal/visitas/ports/outbound	1.042s
ok  	github.com/abdimuy/msp-api/cmd/api	1.098s
ok  	github.com/abdimuy/msp-api/scripts/medicion/fotos-pagos-faltantes	1.044s
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./scripts/medicion/fotos-pagos-faltantes/
```

### 6.1 Salida de los hooks `pre-commit` y `commit-msg` (`lefthook`)

Al tratarse de un commit únicamente documental (`docs/superpowers/plans/fotos-reenvio-task-1-report.md`), las reglas de archivos `.go` se omiten automáticamente por glob y se ejecutan `secrets-check` y `commit-msg` (`format`); las verificaciones de código Go (`gofmt`, `go vet`, `go build`, `golangci-lint`, `go test`) se ejecutaron íntegramente sobre el código de `72270ff` en la sección anterior y en `pre-push`:

```text
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ lefthook run pre-commit
╭──────────────────────────────────────╮
│ 🥊 lefthook v2.1.3  hook: pre-commit │
╰──────────────────────────────────────╯
│  vet (skip) no matching staged files
│  build-check (skip) no matching staged files
│  mod-tidy (skip) no matching staged files
│  format-check (skip) no files for inspection
│  lint-staged (skip) no matching staged files
│  no-debug (skip) no files for inspection
│  no-todo-fixme (skip) no files for inspection
┃  secrets-check ❯ 


                                      
  ────────────────────────────────────
summary: (done in 34.98 seconds)      
✔️ secrets-check (0.03 seconds)

joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ lefthook run commit-msg /tmp/commit_msg.txt
╭──────────────────────────────────────╮
│ 🥊 lefthook v2.1.3  hook: commit-msg │
╰──────────────────────────────────────╯
┃  format ❯ 


                                      
  ────────────────────────────────────
summary: (done in 0.01 seconds)       
✔️ format (0.01 seconds)
```

### 6.2 Salida en verde de `cobranza` y exclusión autorizada de `test-firebird-all` en `pre-push`

**Exclusión autorizada para este `push`:** En este `push` se utilizó únicamente `LEFTHOOK_EXCLUDE=test-firebird-all git push origin test/fotos-reenvio-medicion` (sin `--no-verify` y manteniendo activos `check-sealed`, `lint-full` y `test-unit`), por autorización expresa del dueño del repositorio tras medir el 13-nov que sobre `main` (`6e0e44f8`, sin este cambio y contra la misma base de pruebas) fallan los paquetes dependientes de datos históricos que `msp-test-db.fbk.gz` no incluye, mientras que `cobranza` pasa completo.

#### Salida en verde de todos los paquetes de `cobranza` (unitarios e integración contra Firebird)

```text
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go clean -testcache
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go test -race -short ./internal/cobranza/...
ok  	github.com/abdimuy/msp-api/internal/cobranza	1.032s
ok  	github.com/abdimuy/msp-api/internal/cobranza/app	2.269s
ok  	github.com/abdimuy/msp-api/internal/cobranza/domain	1.070s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp	1.504s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents	1.033s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/microsip	1.032s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb	8.889s
?   	github.com/abdimuy/msp-api/internal/cobranza/ports/outbound	[no test files]

joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ go test ./internal/cobranza/infra/ventfb/... ./internal/cobranza/infra/cobranzahttp/... -p 1 -race -count=1 -timeout 600s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb	209.191s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp	4.902s
```

#### Salida literal completa previa del hook `pre-push` (sin exclusiones)

Al ejecutar el hook `pre-push` completo (sin `--no-verify` ni `LEFTHOOK_EXCLUDE`), con la caché de `golangci-lint` caliente y el contenedor `mueblera-firebird` levantado con `msp-test-db.fbk.gz` migrado hasta `000065`:

- `check-sealed`: **PASS** (`garantias`, `flota` y `canal` sellados).
- `lint-full`: **PASS** (`0 issues.`, 86.70s).
- `test-unit`: **PASS** (113.28s, todos los paquetes en verde).
- `test-firebird-all`: **15 de 18 patrones con pruebas pasan en verde** (incluyendo todo `cobranza/infra/ventfb`, `cobranza/infra/cobranzahttp`, `ventas/infra/ventfb`, `ventas/infra/venthttp`, `auth/infra/firebird`, `clientes/infra/clientesfb`, etc.). Únicamente **3 paquetes** fallan (`visitasfb`, `rutasfb` y `desempenofb`) porque `msp-test-db.fbk.gz` (generado con `gbak -skip_data` de `scripts/db-test-skip-tables.txt`) omite todas las filas de `MSP_VISITAS` (`0` filas, la prueba exige `> 200000`) y `MSP_SALDOS_VENTAS` (`0` filas / `2` zonas, las pruebas exigen `>= 30` zonas y abonos de feb-2026). Salida literal completa:

```text
╭────────────────────────────────────╮
│ 🥊 lefthook v2.1.3  hook: pre-push │
╰────────────────────────────────────╯
┃  check-sealed ❯ 

· internal/asistencia does not exist yet — skipped
✔ garantias is sealed
✔ flota is sealed
✔ canal is sealed

┃  lint-full ❯ 

0 issues.

┃  test-unit ❯ 

?   	github.com/abdimuy/msp-api/cmd/analytics-export-creditdata	[no test files]
?   	github.com/abdimuy/msp-api/cmd/analytics-refresh	[no test files]
ok  	github.com/abdimuy/msp-api/cmd/api	1.259s
?   	github.com/abdimuy/msp-api/cmd/fscfg	[no test files]
?   	github.com/abdimuy/msp-api/cmd/seed-cobrador	[no test files]
ok  	github.com/abdimuy/msp-api/cmd/winback	2.894s
ok  	github.com/abdimuy/msp-api/internal/analytics	1.033s
ok  	github.com/abdimuy/msp-api/internal/analytics/app	1.411s
ok  	github.com/abdimuy/msp-api/internal/analytics/domain	1.169s
ok  	github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb	1.052s
ok  	github.com/abdimuy/msp-api/internal/analytics/infra/analyticshttp	1.275s
ok  	github.com/abdimuy/msp-api/internal/analytics/infra/llm	1.035s
?   	github.com/abdimuy/msp-api/internal/analytics/infra/llm/llmfake	[no test files]
ok  	github.com/abdimuy/msp-api/internal/analytics/infra/narrativamem	1.040s
?   	github.com/abdimuy/msp-api/internal/analytics/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/auth	10.678s
ok  	github.com/abdimuy/msp-api/internal/auth/app	1.034s
ok  	github.com/abdimuy/msp-api/internal/auth/domain	1.069s
ok  	github.com/abdimuy/msp-api/internal/auth/infra/authhttp	1.940s
ok  	github.com/abdimuy/msp-api/internal/auth/infra/authoutbox	1.049s
ok  	github.com/abdimuy/msp-api/internal/auth/infra/firebase	1.093s
ok  	github.com/abdimuy/msp-api/internal/auth/infra/firebird	1.054s
?   	github.com/abdimuy/msp-api/internal/auth/ports/outbound	[no test files]
?   	github.com/abdimuy/msp-api/internal/canal	[no test files]
ok  	github.com/abdimuy/msp-api/internal/canal/app	1.093s
ok  	github.com/abdimuy/msp-api/internal/canal/domain	1.053s
ok  	github.com/abdimuy/msp-api/internal/canal/infra/canalhttp	1.093s
ok  	github.com/abdimuy/msp-api/internal/canal/infra/canalsqlite	1.211s
?   	github.com/abdimuy/msp-api/internal/canal/ports/outbound	[no test files]
?   	github.com/abdimuy/msp-api/internal/clientes	[no test files]
ok  	github.com/abdimuy/msp-api/internal/clientes/app	1.088s
ok  	github.com/abdimuy/msp-api/internal/clientes/domain	1.128s
ok  	github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb	1.066s
ok  	github.com/abdimuy/msp-api/internal/clientes/infra/clienteshttp	1.924s
ok  	github.com/abdimuy/msp-api/internal/clientes/infra/clientespdf	7.041s
ok  	github.com/abdimuy/msp-api/internal/clientes/infra/clientessearch	1.052s
?   	github.com/abdimuy/msp-api/internal/clientes/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/cobranza	1.042s
ok  	github.com/abdimuy/msp-api/internal/cobranza/app	2.306s
ok  	github.com/abdimuy/msp-api/internal/cobranza/domain	1.210s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp	1.814s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents	1.040s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/microsip	1.028s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb	7.875s
?   	github.com/abdimuy/msp-api/internal/cobranza/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/comprobantes/domain	1.065s
?   	github.com/abdimuy/msp-api/internal/comprobantes/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/config/app	1.032s
ok  	github.com/abdimuy/msp-api/internal/config/domain	1.035s
ok  	github.com/abdimuy/msp-api/internal/config/infra/clients	1.029s
ok  	github.com/abdimuy/msp-api/internal/config/infra/configfb	1.023s
ok  	github.com/abdimuy/msp-api/internal/config/infra/confighttp	1.053s
?   	github.com/abdimuy/msp-api/internal/config/ports/outbound	[no test files]
?   	github.com/abdimuy/msp-api/internal/desempeno	[no test files]
ok  	github.com/abdimuy/msp-api/internal/desempeno/app	1.069s
ok  	github.com/abdimuy/msp-api/internal/desempeno/domain	1.255s
ok  	github.com/abdimuy/msp-api/internal/desempeno/infra/clients	1.030s
ok  	github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb	1.034s
ok  	github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp	1.486s
ok  	github.com/abdimuy/msp-api/internal/desempeno/ports/outbound	1.025s
ok  	github.com/abdimuy/msp-api/internal/flota/app	1.066s
ok  	github.com/abdimuy/msp-api/internal/flota/domain	1.029s
ok  	github.com/abdimuy/msp-api/internal/flota/infra/flotafb	1.055s
ok  	github.com/abdimuy/msp-api/internal/flota/infra/flotafirestore	1.063s
?   	github.com/abdimuy/msp-api/internal/flota/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/garantias	1.023s
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.060s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	1.035s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	1.047s
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	1.045s
?   	github.com/abdimuy/msp-api/internal/inventario	[no test files]
ok  	github.com/abdimuy/msp-api/internal/inventario/app	1.027s
ok  	github.com/abdimuy/msp-api/internal/inventario/domain	1.037s
ok  	github.com/abdimuy/msp-api/internal/inventario/infra/invfb	1.025s
ok  	github.com/abdimuy/msp-api/internal/inventario/infra/invhttp	1.071s
ok  	github.com/abdimuy/msp-api/internal/inventario/infra/invoutbox	1.033s
?   	github.com/abdimuy/msp-api/internal/inventario/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/microsip	1.025s
?   	github.com/abdimuy/msp-api/internal/microsip/app	[no test files]
?   	github.com/abdimuy/msp-api/internal/microsip/domain	[no test files]
ok  	github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb	1.030s
ok  	github.com/abdimuy/msp-api/internal/microsip/infra/microsiphttp	1.049s
?   	github.com/abdimuy/msp-api/internal/microsip/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/platform/apperror	1.035s
ok  	github.com/abdimuy/msp-api/internal/platform/audit	1.025s
ok  	github.com/abdimuy/msp-api/internal/platform/config	1.099s
ok  	github.com/abdimuy/msp-api/internal/platform/domain	1.040s
ok  	github.com/abdimuy/msp-api/internal/platform/eventbus	1.212s
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent	1.295s
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs	1.042s
?   	github.com/abdimuy/msp-api/internal/platform/failedintent/clientesfb	[no test files]
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent/firebird	1.032s
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent/http	1.230s
ok  	github.com/abdimuy/msp-api/internal/platform/fbcharset	13.976s
ok  	github.com/abdimuy/msp-api/internal/platform/fbtestutil	1.044s
ok  	github.com/abdimuy/msp-api/internal/platform/firebird	1.719s
ok  	github.com/abdimuy/msp-api/internal/platform/healthcheck	1.052s
ok  	github.com/abdimuy/msp-api/internal/platform/httpdispatch	1.046s
ok  	github.com/abdimuy/msp-api/internal/platform/httptesting	1.046s
ok  	github.com/abdimuy/msp-api/internal/platform/idempotency	1.034s
ok  	github.com/abdimuy/msp-api/internal/platform/idempotency/firebird	1.073s
ok  	github.com/abdimuy/msp-api/internal/platform/imageprocessor	6.989s
ok  	github.com/abdimuy/msp-api/internal/platform/lifecycle	1.038s
ok  	github.com/abdimuy/msp-api/internal/platform/llm	1.055s
ok  	github.com/abdimuy/msp-api/internal/platform/logger	1.046s
ok  	github.com/abdimuy/msp-api/internal/platform/meilisearch	1.049s
?   	github.com/abdimuy/msp-api/internal/platform/microsipseed	[no test files]
ok  	github.com/abdimuy/msp-api/internal/platform/middleware	1.066s
ok  	github.com/abdimuy/msp-api/internal/platform/outboxfb	1.062s
ok  	github.com/abdimuy/msp-api/internal/platform/pagination	1.097s
ok  	github.com/abdimuy/msp-api/internal/platform/reliability	1.630s
ok  	github.com/abdimuy/msp-api/internal/platform/response	1.053s
ok  	github.com/abdimuy/msp-api/internal/platform/validator	1.064s
ok  	github.com/abdimuy/msp-api/internal/platform/whatsapp	1.075s
ok  	github.com/abdimuy/msp-api/internal/reactivacion/app	1.174s
ok  	github.com/abdimuy/msp-api/internal/reactivacion/domain	1.051s
ok  	github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionfb	1.052s
ok  	github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionhttp	1.251s
ok  	github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm	1.038s
?   	github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionllm/copilotofake	[no test files]
ok  	github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionmicrosip	1.036s
ok  	github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionsender	1.078s
?   	github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/reactivacion/smoke	1.040s
?   	github.com/abdimuy/msp-api/internal/rutas	[no test files]
ok  	github.com/abdimuy/msp-api/internal/rutas/app	1.056s
ok  	github.com/abdimuy/msp-api/internal/rutas/domain	1.045s
ok  	github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb	1.050s
ok  	github.com/abdimuy/msp-api/internal/rutas/infra/rutasfirestore	1.046s
?   	github.com/abdimuy/msp-api/internal/rutas/infra/rutashttp	[no test files]
?   	github.com/abdimuy/msp-api/internal/rutas/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/ventas/app	4.159s
ok  	github.com/abdimuy/msp-api/internal/ventas/domain	1.188s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/failedintents	1.039s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/microsip	1.041s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/storage	1.050s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventfb	1.066s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventfirestore	1.052s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/venthttp	5.390s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventoutbox	1.048s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventsearch	1.076s
?   	github.com/abdimuy/msp-api/internal/ventas/ports/outbound	[no test files]
ok  	github.com/abdimuy/msp-api/internal/visitas/app	1.597s
ok  	github.com/abdimuy/msp-api/internal/visitas/domain	1.071s
ok  	github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb	1.036s
ok  	github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp	1.319s
ok  	github.com/abdimuy/msp-api/internal/visitas/ports/outbound	1.033s
ok  	github.com/abdimuy/msp-api/scripts/backfill-usuarios-firestore	1.187s
ok  	github.com/abdimuy/msp-api/scripts/medicion/fotos-pagos-faltantes	1.050s

┃  test-firebird-all ❯ 

go test ./internal/platform/firebird/... ./internal/platform/fbtestutil/... \
          ./internal/platform/failedintent/... ./internal/platform/fbcharset/... \
          ./cmd/api/... \
          ./internal/auth/infra/firebird/... \
          ./internal/ventas/infra/ventfb/... ./internal/ventas/infra/venthttp/... \
          ./internal/ventas/infra/failedintents/... \
          ./internal/cobranza/infra/ventfb/... ./internal/cobranza/infra/cobranzahttp/... \
          ./internal/visitas/infra/visitasfb/... ./internal/visitas/infra/visitashttp/... \
          ./internal/clientes/infra/clientesfb/... ./internal/config/infra/configfb/... \
          ./internal/rutas/infra/rutasfb/... ./internal/microsip/infra/microsipfb/... \
          ./internal/desempeno/infra/desempenofb/... ./internal/desempeno/infra/desempenohttp/... \
          -p 1 -race -count=1 -timeout 600s
ok  	github.com/abdimuy/msp-api/internal/platform/firebird	10.353s
ok  	github.com/abdimuy/msp-api/internal/platform/fbtestutil	1.166s
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent	1.355s
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs	1.026s
?   	github.com/abdimuy/msp-api/internal/platform/failedintent/clientesfb	[no test files]
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent/firebird	3.142s
ok  	github.com/abdimuy/msp-api/internal/platform/failedintent/http	1.116s
ok  	github.com/abdimuy/msp-api/internal/platform/fbcharset	13.438s
ok  	github.com/abdimuy/msp-api/cmd/api	1.391s
ok  	github.com/abdimuy/msp-api/internal/auth/infra/firebird	2.285s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/ventfb	52.283s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/venthttp	35.257s
ok  	github.com/abdimuy/msp-api/internal/ventas/infra/failedintents	1.212s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb	212.169s
ok  	github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp	5.517s
2026/11/14 09:30:46 INFO firebird: connected host=localhost database=/firebird/data/MUEBLERA.FDB charset=UTF8 pool_size=10
--- FAIL: TestE2E_VisitasImagenes_TablaExisteYNoTocoLasVisitasLegacy (0.12s)
    imagenes_repo_test.go:66: 
        	Error Trace:	github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb/imagenes_repo_test.go:66
        	            				github.com/abdimuy/msp-api/internal/platform/fbtestutil/fbtesttx.go:34
        	            				github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb/imagenes_repo_test.go:60
        	Error:      	"0" is not greater than "200000"
        	Test:       	TestE2E_VisitasImagenes_TablaExisteYNoTocoLasVisitasLegacy
        	Messages:   	the legacy table must still hold its rows; the migration is a no-op on it
FAIL
FAIL	github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb	1.070s
ok  	github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp	2.507s
ok  	github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb	17.357s
ok  	github.com/abdimuy/msp-api/internal/config/infra/configfb	1.402s
2026/11/14 09:31:12 INFO firebird: connected host=localhost database=/firebird/data/MUEBLERA.FDB charset=UTF8 pool_size=10
--- FAIL: TestCobranzaRepo_VentasPorZonas_EquivaleZonaPorZona (0.06s)
    cobranza_lote_equivalencia_test.go:121: hasta=2026-02-19T02:00:00Z: 8 consultas (2 zonas × 4 inicios), 0 filas idénticas; 0 con abono en la ventana, 0 de ellas saldadas
    cobranza_lote_equivalencia_test.go:126: 
        	Error Trace:	github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb/cobranza_lote_equivalencia_test.go:126
        	Error:      	"0" is not positive
        	Test:       	TestCobranzaRepo_VentasPorZonas_EquivaleZonaPorZona
        	Messages:   	sin abonos en la ventana: la equivalencia del abono no se probó
--- FAIL: TestCobranzaDeUsuarios_EquivaleACobranzaDeUsuarioYAlDesglose (0.05s)
    cobranza_lote_equivalencia_test.go:230: 6 usuarios, 0 ventas idénticas entre lote, CobranzaDeUsuario y DesglosePorUsuario; 0 con abono
    cobranza_lote_equivalencia_test.go:231: 
        	Error Trace:	github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb/cobranza_lote_equivalencia_test.go:231
        	Error:      	"0" is not positive
        	Test:       	TestCobranzaDeUsuarios_EquivaleACobranzaDeUsuarioYAlDesglose
        	Messages:   	sin abonos: la equivalencia del abono no se probó
--- FAIL: TestCobranzaRepo_VentasPorZonas_CuarentaYOchoGrupos (0.00s)
    cobranza_lote_grupos_test.go:25: 
        	Error Trace:	github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb/cobranza_lote_grupos_test.go:25
        	Error:      	"2" is not greater than or equal to "30"
        	Test:       	TestCobranzaRepo_VentasPorZonas_CuarentaYOchoGrupos
FAIL
FAIL	github.com/abdimuy/msp-api/internal/rutas/infra/rutasfb	2.355s
ok  	github.com/abdimuy/msp-api/internal/microsip/infra/microsipfb	2.076s
2026/11/14 09:31:18 INFO firebird: connected host=localhost database=/firebird/data/MUEBLERA.FDB charset=UTF8 pool_size=10
2026/11/14 09:31:29 WARN desempenofb: pago pendiente sin cliente o sin importe; se omite pago_recibido_id=6c38e914-b072-465c-a901-a8a5ee3988e0 cliente_nulo=true importe_nulo=false
2026/11/14 09:31:29 WARN desempenofb: pago pendiente sin cliente o sin importe; se omite pago_recibido_id=02eb490d-fc75-4e0d-9ff3-7fe32f4ebfde cliente_nulo=false importe_nulo=true
--- FAIL: TestActividadesYUniverso_CuarentaYOchoGruposEquivalenAConsultaPorConsulta (0.01s)
    lote_grupos_test.go:76: 
        	Error Trace:	github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb/lote_grupos_test.go:76
        	Error:      	"2" is not greater than or equal to "30"
        	Test:       	TestActividadesYUniverso_CuarentaYOchoGruposEquivalenAConsultaPorConsulta
        	Messages:   	hacen falta muchas zonas para pasar del límite de contextos
FAIL
FAIL	github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb	20.595s
ok  	github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp	2.674s
FAIL
make: *** [Makefile:369: test-firebird-all] Error 1

exit status 2                                      
  ────────────────────────────────────
summary: (done in 466.96 seconds)     
✔️ check-sealed (30.31 seconds)
✔️ lint-full (86.70 seconds)
✔️ test-unit (113.28 seconds)
🥊 test-firebird-all (466.73 seconds)
```
