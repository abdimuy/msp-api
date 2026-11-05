# Reporte Tarea 1: Medición de borrado de comprobantes al reintentar pago

**Commit base (SHA):** `f2bd21ba74dbbbb634cbee230bcccac60b0af9aa`

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

Se creó la prueba `TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal` (Evidencia: **E-COB-019**) que documenta el comportamiento actual (borrado del archivo en reintento).

**Prueba inicial en verde (demostrando el defecto):**

```
=== RUN   TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== PAUSE TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== CONT  TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
--- PASS: TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal (0.00s)
PASS
ok      github.com/abdimuy/msp-api/internal/cobranza/app        0.019s

```

**Control Positivo (Prueba en rojo):**
Al comentar temporalmente la línea `s.cleanupBlobs(ctx, storedKeys)` (simulando que el defecto fue arreglado), la prueba falló como se esperaba:

```
=== RUN   TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== PAUSE TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
=== CONT  TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
    crear_pago_con_imagenes_reintento_test.go:137:
                Error Trace:    /mnt/c/Users/User/Documents/msp-api/internal/cobranza/app/crear_pago_con_imagenes_reintento_test.go:137
                Error:          Should be true
                Test:           TestCrearPagoConImagenes_Reintento_BorraElComprobanteOriginal
                Messages:       Evidencia E-COB-019: el archivo físico ya no debe existir tras el reintento debido al borrado de cleanupBlobs
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

**Pruebas de la herramienta (Regla de oro y control positivo):**

```
=== RUN   TestMain_ReglaDeOro_NoContieneMutaciones
--- PASS: TestMain_ReglaDeOro_NoContieneMutaciones (0.00s)
=== RUN   TestContarArchivos_ControlPositivo_ReportaFaltante
--- PASS: TestContarArchivos_ControlPositivo_ReportaFaltante (0.00s)
PASS
ok      github.com/abdimuy/msp-api/scripts/medicion/fotos-pagos-faltantes       0.018s

```

**Ejecución contra dev (Mueblera Firebird):**

```
Total de filas: 0
Con archivo: 0
Sin archivo: 0

```

*(Nota: La base de datos local de dev no contiene registros en la tabla de imágenes, pero la consulta y la conexión fueron exitosas. Pendiente ejecución en producción por el dueño).*

## 4. Investigación en otros módulos

Revisión del código de otros módulos para identificar si comparten el mismo defecto.

* **Ventas:** \[LEÍDO\] **Sí presenta el problema.** Se encuentra en `internal/ventas/app/crear_venta_con_imagenes.go:101`. Si la persistencia en base de datos falla por clave primaria duplicada (reintento), se desencadena el borrado de las imágenes.

* **Visitas:** \[LEÍDO\] **No presenta el problema.** Verificado en `internal/visitas/app/registrar_visita_con_imagenes.go:110-117`.

## 5. Lo que NO se verificó

* **No se midió la base de datos de producción:** La ejecución del script contra la base de datos productiva está pendiente y debe ser realizada por el dueño (sin acceso para este entorno).

* **No se crearon pruebas para Ventas ni Visitas:** La revisión de estos módulos fue estrictamente estática (lectura de código).

* **No se implementó ninguna corrección (fix):** Se mantuvo estrictamente el alcance de medición y documentación del defecto según las instrucciones.

## 6. Verificación de Reglas (Pre-PR)

## 6. Verificación de Reglas (Pre-PR)

```
user@MSP:/mnt/c/Users/User/Documents/msp-api\$ gofmt -l ./internal/cobranza ./scripts/medicion
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