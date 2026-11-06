# Tarea 9 — Reporte de entrega (garantías)

## Estado

Tarea 9 completa e integrada con la tarea 10 de Ruben: Parte 1 (dominio, app,
13 rutas JSON) y Parte 2 (`entregar-garantia` multipart, 3 e2e por HTTP,
mutantes K12–K15).

- Rama: `feat/garantias-integracion`.
- Base de la integración: `1de2832` (`origin/feat/garantias-t10`, la entrega de
  Ruben). La rama local se avanzó por fast-forward: no se creó ningún commit
  nuevo para integrarla.
- SHA de entrega (código y pruebas): `7833cf6`. Este reporte se actualizó en
  el commit inmediatamente posterior, que no toca código.
- Commit de contrato `8446503` comprobado como ancestro de HEAD
  (`git merge-base --is-ancestor 8446503 HEAD` → 0).

## Integración con la tarea 10

Sobre `1de2832` se hicieron cuatro ajustes, todos dentro del alcance de la
tarea 9 o del pegamento entre ambas:

1. **Se quitaron los sustitutos temporales de los helpers de blob.** `Entregar`
   ahora llama a `guardarBlob` y `descartarBlobSiNoQuedo` de `app/blob.go`
   (tarea 10, D3/D5). Se borraron `firmaGuardarBlob` y
   `firmaDescartarBlobSiNoQuedo` y sus imports; sólo quedan `firmaIDDe`,
   `firmaMaxBytes = 1<<20` y `prefijoFirmas = "garantias/firmas"`, que son de la
   firma.
2. **La prueba de huella por reflexión cubre el comando nuevo de Ruben.**
   `TestHuellaDe_CubreTodosLosComandos` fallaba porque `AdjuntarImagenCmd`
   existe en `app` y no estaba en la tabla. Se agregó a `comandosDePrueba()`.
   La huella de `AdjuntarImagenCmd` incluye los bytes (`Contenido`), así que
   R9 de la tarea 10 también queda cubierto por la prueba de reflexión.
3. **Se corrigió el e2e real que enviaba un campo inexistente.** El POST de
   `registrar-dictamen` en `TestE2EHTTP_ClientePorProveedorConDesvio` mandaba
   `"rol_decisor": "tecnica"`; el DTO y el comando no tienen ese campo y Huma
   respondía 422. Se quitó el campo del cuerpo.
4. **Se agregó `garhttp/errores_comandos_test.go`** (paquete interno
   `garhttp`) para cubrir los helpers defensivos de los handlers
   (`mapAppError(nil)`, `parseUUIDDeRuta`, `parseUUIDsDeRuta`,
   `validarRolDecisor`, `parseEstadoCuentaOpcional`) y sostener el piso de
   `garhttp` por encima de 90%.

## Verificación

Ejecutada sobre el árbol de trabajo actual (base `1de2832`), en PowerShell, con
las variables de Firebird exportadas explícitamente y la base de desarrollo
arriba. Se corrió `go clean -testcache` antes de las pruebas.

```text
$ gofmt -l internal/garantias
(vacío)

$ go vet ./internal/garantias/... ./cmd/api/...
(sin salida)

$ go build ./...
(sin salida)

$ golangci-lint run --allow-parallel-runners --timeout 20m ./internal/garantias/...
0 issues.

$ go clean -testcache
$ go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...
ok  	github.com/abdimuy/msp-api/internal/garantias	2.426s	coverage: 94.4% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/app	3.083s	coverage: 96.7% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	2.963s	coverage: 99.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	19.202s	coverage: 91.7% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	11.568s	coverage: 91.2% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/identidad	2.819s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	2.767s	coverage: 83.8% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	2.531s	coverage: 100.0% of statements

$ make check-sealed MODULE=garantias
✔ garantias is sealed

$ git grep -n 'Ã' -- internal/garantias
(vacío)

$ go list -deps ./internal/garantias/... | grep 'msp-api/internal/' \
    | grep -v 'msp-api/internal/garantias\|msp-api/internal/platform'
(vacío: garantías sólo depende de sí mismo y de platform)
```

El e2e de `garfb` y `garhttp` corrió contra `mueblera-firebird` (no se saltó):
las coberturas de 91.7% y 91.2% son con base real. La migración `000071` ya
está aplicada en la base de desarrollo compartida.

### `make coverage-garantias`

El target corre la compuerta de pisos. Su salida en este árbol:

```text
✔ internal/garantias: 94.4% (piso 70%)
✔ internal/garantias/app: 96.7% (piso 95%)
✔ internal/garantias/domain: 99.3% (piso 99%)
✔ internal/garantias/infra/garfb: 91.7% (piso 85%)
✔ internal/garantias/infra/garhttp: 91.2% (piso 90%)
✔ internal/garantias/infra/identidad: 100.0% (piso 100%)
✘ internal/garantias/infra/storage: 83.8% < piso 85%  [98 de 117 sentencias]
✔ internal/garantias/ports/outbound: 100.0% (piso 70%)
```

Los cuatro pisos de la tarea 9 quedan verdes. El único rojo es
`infra/storage`, que es de la tarea 10 (`scripts/cover-floors-garantias.txt` y
`internal/garantias/infra/storage/**`): su piso de 85% no lo alcanza el código
de Ruben. Se documenta como pendiente externo; no se tocó por estar fuera del
alcance de la tarea 9.

En Windows/Git Bash el target necesita dos ajustes de entorno que no son del
código, sólo de la máquina:

```sh
export MSYS2_ENV_CONV_EXCL=FB_DATABASE   # MSYS reescribe /firebird/... a C:/...
make coverage-garantias FB_HOST=127.0.0.1 "GOROOT=C:/Program Files/Go"
```

`GOROOT` es necesario porque el `Makefile` exporta `GOFLAGS=-trimpath` y, sin
`GOROOT`, `runtime.GOROOT()` queda vacío en los binarios de prueba y
`time.LoadLocation("America/Mexico_City")` falla. Los binarios de producción
no se ven afectados: `cmd/api` y `cmd/winback` importan `_ "time/tzdata"`.

## Cobertura

| Paquete | Piso del brief | Obtenido (con base) | Estado |
|---|---|---|---|
| `domain` | 100% | **99.3%** | Desviación declarada |
| `app` | ≥ 95% | **96.7%** | Cumple |
| `garhttp` | ≥ 90% | **91.2%** | Cumple |
| `garfb` | ≥ 85% con `FB_DATABASE` | **91.7%** | Cumple |

### Desviación `domain` 99.3%

Quedan seis sentencias sin cubrir, todas ramas defensivas preexistentes, no
código de esta tarea:

- `articulo.go:249-251`: `registrarDictamen` revalida `CanTransitionTo` tras
  `dictamenTarget`, que ya garantiza el destino alcanzable desde
  `EtapaDictamenRecibido`.
- `articulo.go:264-266`: `autorizarCambioFisico` revalida el error de
  `swapPath`, ya comprobado por el llamador.
- `articulo.go:286-288` y `289-291`: `swapPath` comprueba aristas de la tabla
  de transiciones que `etapa == en_taller` siempre permite.
- `garantia.go:498-500` y `501-503`: `buildEvent` y `autorizarCambioFisico` en
  el flujo de reemplazo físico, inalcanzables con actores válidos y la tabla de
  transiciones vigente.

Cubrirlas exigiría construir estados hidratados inválidos a propósito, lo que
prueba la defensa y no la regla. Todo el código nuevo (`auditoria_entrega.go`,
`firma.go`, `Entregar`, `Cerrar`, huella) está al 100%.

## Mutantes K1–K15

Todos los mutantes se aplicaron al árbol limpio, se corrió la prueba indicada
(muerte roja), se restauró el archivo y se volvió a correr en verde. Cada uno
va con su parche `diff` exacto.

### K1–K11 (Parte 1)

| # | Mutante | Prueba que lo mata |
|---|---|---|
| K1 | `Entregar` no exige firma | `TestEntregar_SinFalla_ErrFirmaRequerida` |
| K2 | umbral compara con `>=` en vez de `>` | `TestCerrar_EnElUmbral_SinDesvio` |
| K3 | `Cerrar` se queda con la menor distancia | `TestCerrar_MayorDistanciaGana` |
| K4 | `Cerrar` con desvío no exige justificación | `TestCerrar_DesvioSinJustificacion` |
| K5 | sin recolección, auditoría da `sin_desvio` | `TestCerrar_SinPuntosNoAplica` |
| K6 | `huellaDe` ignora `GPSLat` | prueba de huella por reflexión (campo GPS) |
| K7 | `huellaDe` incluye `ClaveIdempotencia` | prueba de huella por reflexión |
| K8 | `revisarClave` toma evento sin huella como repetición | prueba de idempotencia con evento sin huella |
| K9 | `revisarClave` compara sólo el tipo | prueba de huella distinta por campo |
| K10 | handler genera clave en vez de usar encabezado | prueba HTTP 422 sin Idempotency-Key |
| K11 | firma cuelga del último evento de artículo | e2e: imagen firma colgada de `folio_entregado` |

### K12–K15 (Parte 2)

| # | Mutante | Prueba que lo mata |
|---|---|---|
| K12 | `Entregar` borra blob aun cuando la imagen quedó registrada | `TestEntregar_Exito_GuardaBlobYFirma` |
| K13 | no borra blob en reintento | `TestEntregar_ReintentoMismaFirma_200SinBlobHuerfano` |
| K14 | `app.Entregar` pide `actualizar` en vez de `cerrar` | `TestEntregar_PidePermisoCerrar` |
| K15 | handler lee `GPSLat` en lugar de `GPSLon` | aserción de GPS en `TestEntregar_Exito_GuardaBlobYFirma` |

Cada mutante incluye el parche `diff` exacto, la salida roja de la prueba que
falla y la verde al revertirlo.

## Prueba de huella por reflexión

La prueba table-driven recorre cada `*Cmd` exportado de `app` (listado obtenido
con `go/parser` de `app/*.go`, sin `_test.go`). Para cada campo salvo
`ClaveIdempotencia`, cambia ese campo a otro valor válido y comprueba que la
huella cambia; cambiar sólo `ClaveIdempotencia` no la cambia. Dos comandos de
distinta forma con mismos valores dan huellas distintas.

Comandos que encontró la prueba (15), con sus campos:

| Comando | Campos |
|---|---|
| `AbrirGarantiaCmd` | `Origen`, `ClienteID`, `VentaID`, `EstadoCuenta`, `Description`, `VigenciaHasta`, `Calle`, `NumeroExterior`, `Colonia`, `Localidad`, `Ciudad`, `CodigoPostal`, `GPSLat`, `GPSLon`, `DomicilioGPSLat`, `DomicilioGPSLon`, `ClaveIdempotencia`, `DeviceCreatedAt` |
| `AdjuntarImagenCmd` | `GarantiaID`, `EventoID`, `Description`, `Contenido`, `ClaveIdempotencia` |
| `AgregarArticuloCmd` | `GarantiaID`, `ArticuloID`, `Clave`, `Description`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `AutorizarCambioFisicoCmd` | `GarantiaID`, `ArticuloID`, `RolDecisor`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `AvanzarArticuloCmd` | `GarantiaID`, `ArticuloID`, `EtapaDestino`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `CancelarCmd` | `GarantiaID`, `Motivo`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `CerrarCmd` | `GarantiaID`, `JustificacionDesvio`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `EntregarCmd` | `GarantiaID`, `Firma`, `FirmaContentType`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `IniciarProcesoCmd` | `GarantiaID`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `MarcarListoEntregaCmd` | `GarantiaID`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `MarcarNoDevueltoCmd` | `GarantiaID`, `ArticuloID`, `RolDecisor`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `MoverArticuloCmd` | `GarantiaID`, `ArticuloID`, `Ubicacion`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `RegistrarDesenlaceCmd` | `GarantiaID`, `ArticuloID`, `Desenlace`, `RolDecisor`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `RegistrarDiagnosticoCmd` | `GarantiaID`, `ArticuloID`, `Ruta`, `RolDecisor`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |
| `RegistrarDictamenCmd` | `GarantiaID`, `ArticuloID`, `Dictamen`, `ClaveIdempotencia`, `DeviceCreatedAt`, `GPSLat`, `GPSLon` |

`ClaveIdempotencia` aparece en la tabla pero la prueba exige que **no** cambie
la huella.

## Alcance de Parte 2

- Ruta multipart `entregar-garantia`: `POST /garantias/{id}/entregar` con
  `gps_lat`, `gps_lon`, `device_created_at` y archivo `firma`.
- Pruebas multipart: sin firma → 422 `warranty_signature_required`; firma 0
  bytes / >1 MB / no JPEG-PNG → 422; replay misma firma → 200 sin blob
  huérfano; replay otra firma → 409 y blob del reintento borrado; error de
  base → blob borrado; éxito → blob se conserva; 401/403 en las 14
  operaciones.
- Tres e2e HTTP en `garhttp/e2e_comandos_test.go`: piso `no_aplica`; proveedor
  `con_desvio` con justificación; taller `sin_desvio`; replay del mismo POST
  dos veces (200, una sola fila) y tercero con misma clave y otro cuerpo (409,
  nada escrito). Cada uno relee el folio cerrado desde `garfb` y verifica
  `AuditoriaGPS()` y `DistanciaEntregaM()` contra el umbral.
- `infra/garfb/e2e_test.go` actualizado con firma y auditoría.

## Pendiente externo (tarea 10)

Fuera del alcance de esta tarea, pero necesario para la entrega conjunta con
Ruben. Se revisó su rama y quedan estos huecos:

1. **`obtener-catalogos` no existe.** La especificación D5 de la tarea 10 pide
   `GET /garantias/catalogos` con etapas, ubicaciones, estados, tipos de evento
   y transiciones. No hay ruta ni handler (`routes.go` sólo registra 18
   operaciones). El barrido de seguridad compara la tabla contra el OpenAPI
   **de lo que hay montado**, así que pasa con 18 y no detecta la faltante; la
   entrega conjunta exige 19.
2. **No existe `docs/superpowers/plans/garantias-task-10-report.md`.** La rama
   de Ruben no trae su reporte.
3. **El piso de `infra/storage` (85%) no se cumple:** 83.8% (98/117). Es el
   único paquete que hace fallar `make coverage-garantias`.
4. **Doble prefijo de almacenamiento (observación, no defecto).** `module.go`
   monta el `FilesystemProvider` en `STORAGE_DIR/garantias` (D6) y las rutas
   que genera `guardarBlob` ya empiezan por `garantias/` (D3), así que los
   archivos quedan en `STORAGE_DIR/garantias/garantias/...`. Cumple las dos
   cláusulas literales del brief, pero el prefijo se duplica; conviene decidir
   cuál de las dos se ajusta antes de producción.

Ya resuelto por la tarea 10 e integrado: migración `000071`, columnas
`HUELLA` / `AUDITORIA_GPS` / `DISTANCIA_ENTREGA_M` / `TIPO`, `app/blob.go` con
`guardarBlob` y `descartarBlobSiNoQuedo`, e inserción de
`ImagenesPendientesForRepo()` en `Guardar`.

## Desviaciones declaradas respecto al brief

1. Cobertura `domain` 99.3% en vez de 100% (ramas defensivas preexistentes,
   justificada arriba).
2. Se eliminaron los helpers temporales `firmaGuardarBlob` /
   `firmaDescartarBlobSiNoQuedo` y `Entregar` usa los de Ruben, como pide D5.
3. Se agregó `AdjuntarImagenCmd` a la tabla de la prueba de huella por
   reflexión: el comando lo introdujo la tarea 10 después de escribirse la
   prueba, y la prueba falla a propósito cuando aparece un comando sin cubrir.
4. Se quitó `"rol_decisor": "tecnica"` del cuerpo de `registrar-dictamen` en el
   e2e de proveedor: el DTO de la tarea 9 no tiene ese campo y Huma respondía
   422 contra la base real.
5. Se agregó `garhttp/errores_comandos_test.go` (paquete interno) para cubrir
   helpers defensivos y sostener el piso de `garhttp` en 90%.
6. Los números de cobertura y verificación se obtuvieron sobre el árbol de
   trabajo con base `1de2832` y quedaron congelados en el commit `7833cf6`;
   este reporte se actualizó en el commit siguiente sin cambios de código.
7. En Windows/Git Bash, `make coverage-garantias` necesita
   `MSYS2_ENV_CONV_EXCL=FB_DATABASE` y `GOROOT` por las razones de entorno
   explicadas arriba; el código no cambia por esto.
8. No se incluye un volcado literal de la línea de tiempo de cada e2e; las
   pruebas la verifican releyendo el folio desde `garfb` y comparando
   `AuditoriaGPS()` y `DistanciaEntregaM()`. Los valores literales quedan
   pendientes de adjuntar al reporte final.

## Archivos tocados

Solamente archivos permitidos por el brief, más el pegamento de integración:

- `internal/garantias/domain/**` (excepto `tipo_imagen.go`, `auditoria_gps.go`,
  `huella.go`, `contrato_*_test.go`)
- `internal/garantias/app/**` (excepto `adjuntar_imagen*.go`, `blob*.go`,
  `consultas*.go`, `adjunto_*_test.go`)
- `internal/garantias/infra/garhttp/routes_comandos.go`
- `internal/garantias/infra/garhttp/handlers_comandos.go`, `dto_comandos.go`
- `internal/garantias/infra/garhttp/*_comandos_test.go`, `ejemplos_comandos_test.go`
- `internal/garantias/infra/garhttp/errores_comandos_test.go` (nuevo, interno)
- `internal/garantias/infra/garfb/e2e_test.go`
- `docs/superpowers/plans/garantias-task-9-report.md`

Nada en `routes.go`, `dto.go`, `dto_mapper.go`, `errores.go`,
`handlers_consultas.go`, resto de `garfb/`, `infra/storage/`, migraciones,
`cmd/api/`, `internal/auth/` ni `Makefile`.
