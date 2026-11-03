# Tarea 9 — Reporte de la Parte 1

## Estado

Parte 1 terminada: reglas de dominio de firma y auditoría GPS, huella completa de la
petición en `app`, las trece rutas JSON de comandos y sus pruebas. **No se hizo commit
ni push**, por la instrucción de la sesión. La Parte 2 (multipart `entregar-garantia`,
e2e por HTTP, K12–K15) queda fuera de esta entrega.

- Rama: `feat/garantias-integracion`.
- HEAD: `2e9df69457bf798ab751e152cafcaa22acc8dec8`.
- Commit de contrato `8446503` comprobado como ancestro de HEAD.
- Los cambios están en el árbol de trabajo sobre ese HEAD.

> **Desviación declarada:** el brief original pide que cada número salga de un SHA
> commiteado. La instrucción de esta sesión prohíbe commitear, así que todos los
> números de abajo son del árbol de trabajo sobre `2e9df69`. No se presenta como
> evidencia de un SHA entregado.

## Verificación

Ejecutada el 3 de noviembre de 2026 sobre el árbol de arriba.

```text
$ gofmt -l internal/garantias
(vacío)

$ go vet ./internal/garantias/...
(sin salida)

$ go build ./...
(sin salida)

$ golangci-lint run --allow-parallel-runners --timeout 20m ./internal/garantias/...
0 issues.

$ go clean -testcache
$ go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
ok  	github.com/abdimuy/msp-api/internal/garantias	1.759s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.152s	coverage: 97.1% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	2.686s	coverage: 99.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	2.169s	coverage: 35.5% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	3.372s	coverage: 90.1% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/identidad	2.416s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	2.545s	coverage: 83.8% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	2.324s	coverage: 100.0% of statements

$ grep -rn 'Ã' internal/garantias
(vacío)

$ go list -deps ./internal/garantias/... | grep 'msp-api/internal/' \
    | grep -v 'msp-api/internal/garantias\|msp-api/internal/platform'
(vacío: garantías sólo depende de sí mismo y de platform)
```

`make check-sealed MODULE=garantias` no corre en esta máquina porque `make` invoca
`sh`, que no está en el PATH de PowerShell. Se ejecutó el mismo filtro del target
directamente con `go list -deps` y el resultado es el de arriba. `gofmt`, `go vet`,
`go build` y `golangci-lint` sí se corrieron literalmente.

No hay `fmt.Print*` en código de aplicación de `internal/garantias` ni `TODO/FIXME/XXX/HACK`
en Go que no sea de prueba. La búsqueda de `XXX` sólo encuentra el formato documentado
`GA-XXXXXX` de los folios, que es preexistente.

## Cobertura

| Paquete | Piso del brief | Obtenido | Estado |
|---|---|---|---|
| `domain` | 100% | **99.3%** | Desviación declarada (ver abajo) |
| `app` | ≥ 95% | **97.1%** | Cumple |
| `garhttp` | ≥ 90% | **90.1%** | Cumple |
| `garfb` | ≥ 85% con `FB_DATABASE` | **35.5%** sin base | No medible en esta máquina |

Notas:

- **`domain` 99.3%:** quedan seis sentencias sin cubrir, todas ramas defensivas
  preexistentes, no código de esta tarea:
  - `articulo.go:249-251`: `registrarDictamen` revalida una transición que
    `dictamenTarget` ya garantiza.
  - `articulo.go:264-266`: `autorizarCambioFisico` revalida una transición ya
    garantizada por la tabla.
  - `articulo.go:286-288` y `289-291`: `swapPath` comprueba dos aristas de la
    tabla que la etapa `en_taller` siempre permite.
  - `garantia.go:498-500` y `501-503`: errores de `buildEvent` y
    `autorizarCambioFisico` en el cambio físico, inalcanzables con actores
    válidos y la tabla de transiciones vigente.
  Cubrirlas exigiría construir estados hidratados inválidos a propósito, lo que
  prueba la defensa y no la regla. Todo el código nuevo (`auditoria_entrega.go`,
  `firma.go`, `Entregar`, `Cerrar`) está al 100%.
- **`garhttp` 90.1%:** las ramas de error de los parsers de los handlers
  (`parseUUIDDeRuta`, `parseEstadoCuentaOpcional`, `parse*` de enums) no se pueden
  alcanzar por HTTP: Huma valida `format:"uuid"` y `enum` antes del handler y
  responde 422. Quedan como defensa. El piso se alcanzó cubriendo además el 500
  genérico de `mapAppError`.
- **`garfb` 35.5%:** `FB_DATABASE` no está definido y no hay Docker/Firebird
  corriendo; las pruebas de integración se saltan. El piso de 85% con base queda
  pendiente para la Parte 2.

## Mutantes K1–K11

Cada mutante se aplicó al árbol, se corrió la prueba indicada, se restauró el
archivo y se volvió a correr en verde. El parche es el `diff` exacto entre el
archivo original y el mutado. Todos los mutantes mueren.

### K1 — `Entregar` no exige firma

```diff
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -694,9 +694,6 @@
 		return ErrEntregaGPSRequerido
 	}
 	firma = normalizarNFC(strings.TrimSpace(firma))
-	if firma == "" {
-		return ErrFirmaRequerida
-	}
 	if err := g.verificarListosParaEntrega(); err != nil {
 		return err
 	}
```

```text
--- FAIL: TestEntregar_SinFirma (0.00s)
    --- FAIL: TestEntregar_SinFirma/firma= (0.00s)
        entregar_firma_test.go:42: want ErrFirmaRequerida, got warranty_image_path_invalid: ruta de imagen inválida
    --- FAIL: TestEntregar_SinFirma/firma=___ (0.00s)
        entregar_firma_test.go:42: want ErrFirmaRequerida, got warranty_image_path_invalid: ruta de imagen inválida
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.701s
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.697s
```

### K2 — el umbral compara con `>=`

```diff
--- a/internal/garantias/domain/auditoria_entrega.go
+++ b/internal/garantias/domain/auditoria_entrega.go
@@ -79,7 +79,7 @@
 // with an exact number instead of a coordinate pair that floating point may or
 // may not land on.
 func superaUmbralDesvio(metros float64) bool {
-	return metros > UmbralDesvioMetros
+	return metros >= UmbralDesvioMetros
 }
```

```text
--- FAIL: TestSuperaUmbralDesvio_Estricto (0.00s)
    --- FAIL: TestSuperaUmbralDesvio_Estricto/exactamente (0.00s)
        auditoria_entrega_internal_test.go:26: superaUmbralDesvio(300) = true, want false
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.674s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.711s
```

### K3 — `Cerrar` se queda con la menor distancia

```diff
--- a/internal/garantias/domain/auditoria_entrega.go
+++ b/internal/garantias/domain/auditoria_entrega.go
@@ -47,10 +47,10 @@
 		}, nil
 	}
 
-	var mayor float64
+	mayor := math.Inf(1)
 	for _, recoleccion := range recolecciones {
 		d := distanciaHaversineMetros(*entrega, recoleccion)
-		if d > mayor {
+		if d < mayor {
 			mayor = d
 		}
 	}
```

```text
--- FAIL: TestAuditarEntrega_GanaLaMayor (0.00s)
    auditoria_entrega_test.go:136: resultado = "sin_desvio", want con_desvio
    auditoria_entrega_test.go:139: distancia = 0x1d615bc9e388, want la mayor (~500 m)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.674s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.709s
```

### K4 — el desvío no exige justificación

```diff
--- a/internal/garantias/domain/auditoria_entrega.go
+++ b/internal/garantias/domain/auditoria_entrega.go
@@ -56,9 +56,6 @@
 	}
 
 	if superaUmbralDesvio(mayor) {
-		if justificacion == "" {
-			return AuditoriaEntrega{}, ErrJustificacionDesvioRequerida
-		}
 		return AuditoriaEntrega{
 			DistanciaM:    &mayor,
 			Resultado:     AuditoriaGPSConDesvio,
```

```text
--- FAIL: TestAuditarEntrega_DesvioSinJustificacion (0.00s)
    auditoria_entrega_test.go:150: want ErrJustificacionDesvioRequerida, got <nil>
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.696s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.738s
```

### K5 — sin recolección da `sin_desvio` en vez de `no_aplica`

```diff
--- a/internal/garantias/domain/auditoria_entrega.go
+++ b/internal/garantias/domain/auditoria_entrega.go
@@ -42,7 +42,7 @@
 	justificacion = normalizarNFC(strings.TrimSpace(justificacion))
 	if entrega == nil || len(recolecciones) == 0 {
 		return AuditoriaEntrega{
-			Resultado:     AuditoriaGPSNoAplica,
+			Resultado:     AuditoriaGPSSinDesvio,
 			Justificacion: justificacion,
 		}, nil
 	}
```

```text
--- FAIL: TestAuditarEntrega_NoAplica (0.00s)
    --- FAIL: TestAuditarEntrega_NoAplica/sin_recolecciones (0.00s)
        auditoria_entrega_test.go:47: resultado = "sin_desvio", want no_aplica
    --- FAIL: TestAuditarEntrega_NoAplica/lista_vacia (0.00s)
        auditoria_entrega_test.go:47: resultado = "sin_desvio", want no_aplica
    --- FAIL: TestAuditarEntrega_NoAplica/sin_entrega (0.00s)
        auditoria_entrega_test.go:47: resultado = "sin_desvio", want no_aplica
    --- FAIL: TestAuditarEntrega_NoAplica/ambos_vacios (0.00s)
        auditoria_entrega_test.go:47: resultado = "sin_desvio", want no_aplica
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.719s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.709s
```

### K6 — `huellaDe` ignora `GPSLat`

```diff
--- a/internal/garantias/app/huella.go
+++ b/internal/garantias/app/huella.go
@@ -33,6 +33,9 @@
 		campo.Kind() == reflect.String && campo.CanSet() {
 		campo.SetString("")
 	}
+	if campo := copia.FieldByName("GPSLat"); campo.IsValid() && campo.CanSet() {
+		campo.Set(reflect.Zero(campo.Type()))
+	}
 	contenido, err := json.Marshal(copia.Interface())
 	if err != nil {
 		return ""
```

```text
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/EntregarCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/IniciarProcesoCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/AutorizarCambioFisicoCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/AgregarArticuloCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/MarcarNoDevueltoCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/RegistrarDictamenCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/RegistrarDiagnosticoCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/MoverArticuloCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/AbrirGarantiaCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/MarcarListoEntregaCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
    --- FAIL: TestHuellaDe_TodosLosCamposCuentan/RegistrarDesenlaceCmd (0.00s)
        huella_test.go:99: el campo GPSLat no cuenta en la huella
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.914s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	1.876s
```

### K7 — `huellaDe` incluye `ClaveIdempotencia`

```diff
--- a/internal/garantias/app/huella.go
+++ b/internal/garantias/app/huella.go
@@ -29,10 +29,6 @@
 	}
 	copia := reflect.New(v.Type()).Elem()
 	copia.Set(v)
-	if campo := copia.FieldByName("ClaveIdempotencia"); campo.IsValid() &&
-		campo.Kind() == reflect.String && campo.CanSet() {
-		campo.SetString("")
-	}
 	contenido, err := json.Marshal(copia.Interface())
 	if err != nil {
 		return ""
```

```text
--- FAIL: TestHuellaDe_ClaveNoCuenta (0.00s)
    huella_test.go:122: la clave de idempotencia no debe entrar en la huella
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.904s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	1.890s
```

### K8 — un evento sin huella se toma como repetición

```diff
--- a/internal/garantias/app/service.go
+++ b/internal/garantias/app/service.go
@@ -177,7 +177,7 @@
 		return false, nil
 	case garantiaID != uuid.Nil && vista.GarantiaID() != garantiaID:
 		return false, domain.ErrClaveIdempotenciaDeOtroFolio
-	case huella != "" && vista.Huella() == huella:
+	case vista.Huella() == "" || (huella != "" && vista.Huella() == huella):
 		return true, nil
 	default:
 		return false, domain.ErrClaveIdempotenciaMismatch
```

```text
    --- FAIL: TestComandos_ClaveSinHuella/MarcarNoDevuelto (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/AutorizarCambioFisico (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/Cancelar (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/RegistrarDictamen (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/MarcarListoEntrega (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/RegistrarDesenlace (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/RegistrarDiagnostico (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/IniciarProceso (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/AvanzarArticulo (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/MoverArticulo (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
    --- FAIL: TestComandos_ClaveSinHuella/Cerrar (0.00s)
        comandos_compartidos_test.go:399: want ErrClaveIdempotenciaMismatch, got <nil>
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.879s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	1.875s
```

### K9 — `revisarClave` compara sólo el tipo

```diff
--- a/internal/garantias/app/service.go
+++ b/internal/garantias/app/service.go
@@ -177,7 +177,7 @@
 		return false, nil
 	case garantiaID != uuid.Nil && vista.GarantiaID() != garantiaID:
 		return false, domain.ErrClaveIdempotenciaDeOtroFolio
-	case huella != "" && vista.Huella() == huella:
+	case vista.Tipo() == domain.TipoEventoFolioAbierto:
 		return true, nil
 	default:
 		return false, domain.ErrClaveIdempotenciaMismatch
```

```text
--- FAIL: TestAbrirGarantia_ClaveRepetidaConOtroContenido (0.00s)
    abrir_garantia_errores_test.go:201: want ErrClaveIdempotenciaMismatch, got <nil>
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.897s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	1.874s
```

### K10 — un handler genera la clave en vez de usar el encabezado

```diff
--- a/internal/garantias/infra/garhttp/handlers_comandos.go
+++ b/internal/garantias/infra/garhttp/handlers_comandos.go
@@ -89,7 +89,7 @@
 
 	garantia, err := h.servicio.IniciarProceso(ctx, app.IniciarProcesoCmd{
 		GarantiaID:        id,
-		ClaveIdempotencia: in.IdempotencyKey,
+		ClaveIdempotencia: uuid.NewString(),
 		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
 		GPSLat:            in.Body.GPSLat,
 		GPSLon:            in.Body.GPSLon,
```

```text
--- FAIL: TestComandos_CaminoFeliz_JSONCompleto (0.00s)
    --- FAIL: TestComandos_CaminoFeliz_JSONCompleto/iniciar-proceso (0.01s)
        handlers_comandos_test.go:189: 
            	Error Trace:	C:/Users/User/msp-api/internal/garantias/infra/garhttp/handlers_comandos_test.go:189
            	Error:      	Not equal: 
            	            	expected: "44444444-4444-4444-4444-444444444444"
            	            	actual  : "aa53dcb5-5d10-46ce-9ebd-b411f8e2fe05"
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	2.907s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	2.911s
```

### K11 — la firma cuelga del último evento de artículo

```diff
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -732,9 +732,13 @@
 		return err
 	}
 	imagenID := FirmaImagenID(actor.ClaveIdempotencia)
+	eventoImagen := e.ID()
+	if n := len(entregas); n > 0 {
+		eventoImagen = entregas[n-1].evento.ID()
+	}
 	imagen, err := NewImagen(NewImagenParams{
 		ID:        &imagenID,
-		EventoID:  e.ID(),
+		EventoID:  eventoImagen,
 		Tipo:      TipoImagenFirma,
 		Ruta:      firma,
 		SubidaPor: actor.Usuario,
```

```text
--- FAIL: TestEntregar_ImagenDeFirma (0.00s)
    entregar_firma_test.go:127: la firma cuelga de 0086e78e-126b-4033-9cf9-bc5a45a08832, want el folio_entregado 6c08fb7c-388f-43e2-b5ed-b2fc2456fb4a
    entregar_firma_test.go:131: la firma cuelga de un evento de articulo 0086e78e-126b-4033-9cf9-bc5a45a08832, no del folio_entregado
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.679s
```

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.694s
```

## Huella: comandos y campos que cubre la prueba

La lista no está escrita a mano: `app/huella_test.go` parsea los `type …Cmd struct`
exportados de `app/*.go` (sin `_test.go`) con `go/parser` y falla si un comando del
código no está en su tabla o si un campo no cambia la huella. Encontró 14 comandos:

| Comando | Campos (además de `ClaveIdempotencia`, que no cuenta) |
|---|---|
| `AbrirGarantiaCmd` | `Origen, ClienteID, VentaID, EstadoCuenta, Description, VigenciaHasta, Calle, NumeroExterior, Colonia, Localidad, Ciudad, CodigoPostal, GPSLat, GPSLon, DomicilioGPSLat, DomicilioGPSLon, DeviceCreatedAt` |
| `AgregarArticuloCmd` | `GarantiaID, ArticuloID, Clave, Description, DeviceCreatedAt, GPSLat, GPSLon` |
| `AutorizarCambioFisicoCmd` | `GarantiaID, ArticuloID, RolDecisor, DeviceCreatedAt, GPSLat, GPSLon` |
| `AvanzarArticuloCmd` | `GarantiaID, ArticuloID, EtapaDestino, DeviceCreatedAt, GPSLat, GPSLon` |
| `CancelarCmd` | `GarantiaID, Motivo, DeviceCreatedAt, GPSLat, GPSLon` |
| `CerrarCmd` | `GarantiaID, JustificacionDesvio, DeviceCreatedAt, GPSLat, GPSLon` |
| `EntregarCmd` | `GarantiaID, Firma, FirmaContentType, DeviceCreatedAt, GPSLat, GPSLon` |
| `IniciarProcesoCmd` | `GarantiaID, DeviceCreatedAt, GPSLat, GPSLon` |
| `MarcarListoEntregaCmd` | `GarantiaID, DeviceCreatedAt, GPSLat, GPSLon` |
| `MarcarNoDevueltoCmd` | `GarantiaID, ArticuloID, RolDecisor, DeviceCreatedAt, GPSLat, GPSLon` |
| `MoverArticuloCmd` | `GarantiaID, ArticuloID, Ubicacion, DeviceCreatedAt, GPSLat, GPSLon` |
| `RegistrarDesenlaceCmd` | `GarantiaID, ArticuloID, Desenlace, RolDecisor, DeviceCreatedAt, GPSLat, GPSLon` |
| `RegistrarDiagnosticoCmd` | `GarantiaID, ArticuloID, Ruta, RolDecisor, DeviceCreatedAt, GPSLat, GPSLon` |
| `RegistrarDictamenCmd` | `GarantiaID, ArticuloID, Dictamen, DeviceCreatedAt, GPSLat, GPSLon` |

Además, `TestHuellaDe_MismaFormaDistintoTipo` demuestra que
`IniciarProcesoCmd` y `MarcarListoEntregaCmd`, con la misma forma y valores, dan
huellas distintas porque el nombre del tipo entra en el hash.

## Rutas JSON: ejemplo de petición y respuesta

Todas son `POST`, exigen `Idempotency-Key: 44444444-4444-4444-4444-444444444444`
y comparten la misma respuesta: el folio completo. Los ejemplos viven en
`garhttp/ejemplos_comandos_test.go` y son los que usan las pruebas de camino
feliz, 401 y 403.

| Operación | Ruta | Cuerpo |
|---|---|---|
| `abrir-garantia` | `/garantias` | `{"origen":"cliente","cliente_id":25,"venta_id":80,"estado_cuenta":"liquidada","description":"Garantía de sala","vigencia_hasta":"2027-01-31T00:00:00Z","calle":"Reforma","numero_exterior":"123","colonia":"Centro","localidad":"Centro","ciudad":"Ciudad de México","codigo_postal":"06000","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `iniciar-proceso` | `/garantias/{id}/iniciar-proceso` | `{"gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `marcar-listo-entrega` | `/garantias/{id}/listo-entrega` | igual que `iniciar-proceso` |
| `cerrar-garantia` | `/garantias/{id}/cerrar` | `{"justificacion_desvio":"el cliente cambió de domicilio","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `cancelar-garantia` | `/garantias/{id}/cancelar` | `{"motivo":"el cliente desistió","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `agregar-articulo` | `/garantias/{id}/articulos` | `{"articulo_id":151,"clave":"SALA-002","description":"Otra pieza de la sala","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `avanzar-articulo` | `/garantias/{id}/articulos/{aid}/avanzar` | `{"etapa_destino":"reparado_taller","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `registrar-diagnostico` | `/garantias/{id}/articulos/{aid}/diagnostico` | `{"ruta":"taller","rol_decisor":"tecnica","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `registrar-dictamen` | `/garantias/{id}/articulos/{aid}/dictamen` | `{"dictamen":"aceptada","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `mover-articulo` | `/garantias/{id}/articulos/{aid}/mover` | `{"ubicacion":"en_transito","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `autorizar-cambio-fisico` | `/garantias/{id}/articulos/{aid}/cambio-fisico` | `{"rol_decisor":"tecnica","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `marcar-no-devuelto` | `/garantias/{id}/articulos/{aid}/no-devuelto` | `{"rol_decisor":"tecnica","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |
| `registrar-desenlace` | `/garantias/{id}/articulos/{aid}/desenlace` | `{"desenlace":"segunda_mano","rol_decisor":"tecnica","gps_lat":19.4326,"gps_lon":-99.1332,"device_created_at":"2026-10-13T15:00:00Z"}` |

`{id}` = `11111111-1111-1111-1111-111111111111`,
`{aid}` = `22222222-2222-2222-2222-222222222222`.

Respuesta de las trece (Huma agrega `$schema`; la prueba lo verifica y lo quita
antes de comparar el resto):

```json
{
  "id": "11111111-1111-1111-1111-111111111111",
  "folio": "GA-000123",
  "origen": "cliente",
  "cliente_id": 25,
  "venta_id": 80,
  "estado_cuenta": "liquidada",
  "estado": "en_proceso",
  "description": "Garantía de sala",
  "vigencia_hasta": "2027-01-31",
  "calle": "Reforma",
  "numero_exterior": "123",
  "colonia": "Centro",
  "localidad": "Centro",
  "ciudad": "Ciudad de México",
  "codigo_postal": "06000",
  "gps_lat": 19.4326,
  "gps_lon": -99.1332,
  "abierto_por": "ruben",
  "created_at": "2026-10-13T15:00:00Z",
  "updated_at": "2026-10-13T15:30:00Z",
  "articulos": [
    {
      "id": "22222222-2222-2222-2222-222222222222",
      "garantia_id": "11111111-1111-1111-1111-111111111111",
      "rol": "original",
      "articulo_id": 150,
      "clave": "SALA-001",
      "description": "Respaldo dañado",
      "ruta": "taller",
      "etapa": "en_taller",
      "ubicacion": "taller",
      "created_at": "2026-10-13T15:00:00Z",
      "updated_at": "2026-10-13T15:30:00Z"
    }
  ]
}
```

Las pruebas por ruta cubren además: sin `Idempotency-Key` → 422; clave que no es
UUID → 422; UUID de ruta inválido → 422; enum inválido → 422; error de dominio
→ 404; clave de otro folio → 409; sin usuario → 401; sin permiso → 403; y que la
clave del encabezado es exactamente la que recibe el comando. El 500 genérico
también está cubierto para no filtrar el mensaje interno.

## e2e de `garfb`

**No se pudieron correr ni medir.** En esta máquina no hay Docker/Firebird
corriendo y `FB_DATABASE` no está definido, así que las pruebas de integración se
saltan. Además, la Parte 2 depende de la migración `000071` y del helper de blob
de la tarea 10 (Ruben), que no están en esta rama.

Lo que sí se hizo en `infra/garfb` fue dejar las pruebas compilando y ejecutables
cuando haya base:

- `e2e_test.go`: `Firma`/`FirmaContentType` en los tres `EntregarCmd`, más
  `storagePrueba` en memoria e `imagenRepoPrueba` para que `app.Entregar` no
  llame a un `Storage`/`ImagenRepo` nulo.
- `guardar_test.go`: la firma en la llamada directa a `Entregar` y los tres
  argumentos nuevos de `Cerrar`.

No hay tabla de línea de tiempo ni distancias de base que reportar: es material
de la Parte 2.

## Desviaciones del brief

1. **Sin commit ni push**, por instrucción de la sesión. La evidencia es del
   árbol de trabajo sobre `2e9df69`, no de un SHA entregado.
2. **`domain` 99.3% y no 100%.** Seis sentencias preexistentes, todas ramas
   defensivas inalcanzables con estados válidos (detalle en Cobertura). El código
   nuevo sí está al 100%.
3. **`garfb/guardar_test.go` tocado**, aunque el brief sólo autoriza
   `garfb/e2e_test.go`. Eran dos llamadas que dejaban de compilar por la firma
   nueva de `Entregar` y `Cerrar`.
4. **`garfb/e2e_test.go` con algo más que `Firma`:** se agregaron dobles en
   memoria de `Storage` e `ImagenRepo`. Sin ellos, el servicio real de la Parte 2
   llamaría a dependencias nulas. Se retirarán cuando exista el adaptador real.
5. **`ComandoBase` exportado** (no `comandoBase`). Huma sólo recorre campos
   anónimos exportados al construir el esquema; con el tipo sin exportar, todos
   los campos aplanados del cuerpo se rechazaban con 422 `unexpected property`.
6. **`superaUmbralDesvio` como función aparte.** El brief pide comparar con `>`;
   la comparación se extrajo para poder fijar el caso exacto de 300 m con un
   número exacto, en vez de depender de que una coordenada caiga justo en el
   umbral por punto flotante.
7. **Helpers temporales de blob en `app/entregar.go`** (`firmaGuardarBlob`,
   `firmaDescartarBlobSiNoQuedo`) en lugar del `app/blob.go` de la tarea 10, que
   aún no existe. Se eliminarán al integrar la Parte 2.
8. **El ejemplo multipart de `entregar-garantia` existe en
   `ejemplosComandos()`** (14 entradas) porque el contrato compartido lo pide,
   pero la ruta no se registra ni se prueba en la Parte 1: pertenece a la Parte 2.
9. **`make check-sealed` no corre en PowerShell** por la dependencia de `sh`; se
   ejecutó el filtro equivalente con `go list -deps`.

## Archivos nuevos y modificados

Nuevos:

- `internal/garantias/domain/auditoria_entrega.go`, `firma.go`
- `internal/garantias/domain/auditoria_entrega_test.go`,
  `auditoria_entrega_internal_test.go`, `cerrar_auditoria_test.go`,
  `entregar_firma_test.go`
- `internal/garantias/app/huella.go`, `huella_export_test.go`, `huella_test.go`
- `internal/garantias/infra/garhttp/dto_comandos.go`,
  `handlers_comandos.go`, `routes_comandos.go`, `ejemplos_comandos_test.go`,
  `fakes_comandos_test.go`, `handlers_comandos_test.go`

Modificados (además de los tests de `app`/`domain` ajustados a las firmas nuevas):

- `internal/garantias/domain/garantia.go`
- `internal/garantias/app/service.go`, `abrir_garantia.go`, `entregar.go`,
  `cerrar.go` y los comandos de artículo/folio que pasan la huella
- `internal/garantias/infra/garhttp/routes_comandos.go`
- `internal/garantias/infra/garfb/e2e_test.go`, `guardar_test.go` (desviación 3)

## Pendiente para la Parte 2

- Ruta multipart `entregar-garantia` con `firma`, `gps_lat`, `gps_lon`,
  `device_created_at` y el helper real de blob.
- Los tres e2e por HTTP de `garhttp/e2e_comandos_test.go` contra Firebird real.
- Mutantes K12–K15.
- Medir `garfb` con `FB_DATABASE` para el piso de 85%.
- Reemplazar los helpers temporales de firma por `app/blob.go` y los dobles de
  `garfb/e2e_test.go` por los adaptadores reales.
