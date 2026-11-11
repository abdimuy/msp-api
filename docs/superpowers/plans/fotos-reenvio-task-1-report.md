# Reporte Tarea 1: Medición de borrado de comprobantes al reintentar pago

**Commit base (SHA):** `f622305`

## 1. Verificación Inicial (Día 1)

Antes de realizar cualquier modificación, se comprobó que la suite de cobranza corriera exitosamente en verde.

Comando ejecutado:

```
CGO_ENABLED=1 go test -race -short ./internal/cobranza/...

```

Salida:

```
ok      github.com/abdimuy/msp-api/internal/cobranza    (cached)
ok      github.com/abdimuy/msp-api/internal/cobranza/app        2.251s
ok      github.com/abdimuy/msp-api/internal/cobranza/domain     (cached)
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp (cached)
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents        (cached)
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/microsip     (cached)
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb       (cached)
?       github.com/abdimuy/msp-api/internal/cobranza/ports/outbound     [no test files]

```

## 2. Prueba del Defecto y Control Positivo (Día 2)

Se creó la prueba `TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal` (Evidencia: **E-COB-060**) que documenta el comportamiento actual (borrado del archivo en reintento).

**Nota sobre el Storage de la prueba:** Se utilizó `localTestStorage` (escribiendo físicamente sobre `t.TempDir()`) en lugar del `FilesystemProvider` real porque el linter `depguard` prohíbe importar infraestructura (`infra`) desde el paquete de aplicación (`app`).

**Prueba inicial en verde (demostrando el defecto):**

```
=== RUN   TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== PAUSE TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== CONT  TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
--- PASS: TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal (0.00s)
PASS
ok      github.com/abdimuy/msp-api/internal/cobranza/app        0.015s

```

**Control Positivo (Prueba en rojo):**
Al comentar temporalmente la línea `s.cleanupBlobs(ctx, storedKeys)` (simulando que el defecto fue arreglado), la prueba falló como se esperaba:

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

* **Ventas:** \[INFERIDO\] Se encuentra el flujo de limpieza en `internal/ventas/app/crear_venta_con_imagenes.go:101` y la ruta del archivo es estable (`venthttp/multipart.go:24-30`). Sin embargo, **no está demostrado** que un reintento llegue vivo hasta `storeAllImagenBlobs`, ya que puede ser detenido antes por las validaciones de stock/cargo o por el middleware de idempotencia de la capa HTTP.

* **Visitas:** \[LEÍDO\] **No presenta el problema.** Verificado en `internal/visitas/app/registrar_visita_con_imagenes.go:110-117`.

## 5. Lo que NO se verificó

* **No se midió la base de datos de producción:** La ejecución del script contra la base de datos productiva está pendiente y debe ser realizada por el dueño (sin acceso para este entorno).

* **No se crearon pruebas para Ventas ni Visitas:** La revisión de estos módulos fue estrictamente estática (lectura de código).

* **No se implementó ninguna corrección (fix):** Se mantuvo estrictamente el alcance de medición y documentación del defecto según las instrucciones.

## 6. Verificación de Reglas (Pre-PR)

```
joaov@VAZHER:/mnt/c/Users/joaov/Documents/msp-api$ gofmt -l ./internal/cobranza ./scripts/medicion
go vet ./internal/cobranza/... ./scripts/medicion/...
golangci-lint run ./internal/cobranza/... ./scripts/medicion/...
go clean -testcache
go test -race -short ./internal/cobranza/... ./scripts/medicion/...
0 issues.
ok      github.com/abdimuy/msp-api/internal/cobranza    1.020s
ok      github.com/abdimuy/msp-api/internal/cobranza/app        2.238s
ok      github.com/abdimuy/msp-api/internal/cobranza/domain     1.054s
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp 1.513s
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/failedintents        1.028s
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/microsip     1.029s
ok      github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb       7.856s
?       github.com/abdimuy/msp-api/internal/cobranza/ports/outbound     [no test files]
ok      github.com/abdimuy/msp-api/scripts/medicion/fotos-pagos-faltantes       1.029s
```


