# Tarea 9 — Reporte de entrega (garantías)

## Estado

Tarea 9 completa: Parte 1 (dominio, app, 13 rutas JSON) y Parte 2
(`entregar-garantia` multipart, 3 e2e por HTTP, mutantes K12–K15).

- Rama: `feat/garantias-integracion`.
- SHA de entrega: <se asigna al commitear este reporte como último commit>.
- Commit de contrato `8446503` comprobado como ancestro de HEAD.
- Los cambios fueron verificados sobre árbol limpio con `go clean -testcache`.

## Verificación (ejecutada sobre SHA limpio)

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
ok  	github.com/abdimuy/msp-api/internal/garantias		coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/app		coverage: 97.1% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	coverage: 99.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	coverage: 35.5% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	coverage: 90.4% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/identidad	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	coverage: 83.8% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	coverage: 100.0% of statements

$ grep -rn 'Ã' internal/garantias
(vacío)

$ go list -deps ./internal/garantias/... | grep 'msp-api/internal/' \
    | grep -v 'msp-api/internal/garantias\|msp-api/internal/platform'
(vacío: garantías sólo depende de sí mismo y de platform)
```

## Cobertura

| Paquete | Piso del brief | Obtenido | Estado |
|---|---|---|---|
| `domain` | 100% | **99.3%** | Desviación declarada |
| `app` | ≥ 95% | **97.1%** | Cumple |
| `garhttp` | ≥ 90% | **90.4%** | Cumple |
| `garfb` | ≥ 85% con `FB_DATABASE` | **35.5%** sin base | No medible |

### Desviación `domain` 99.3%

Quedan seis sentencias sin cubrir, todas ramas defensivas preexistentes, no código de esta tarea:

- `articulo.go:249-251`: `registrarDictamen` revalida `CanTransitionTo` tras `dictamenTarget`, que ya garantiza el destino alcanzable desde `EtapaDictamenRecibido`.
- `articulo.go:264-266`: `autorizarCambioFisico` revalida el error de `swapPath`, ya comprobado por el llamador.
- `articulo.go:286-288` y `289-291`: `swapPath` comprueba aristas de la tabla de transiciones que `etapa == en_taller` siempre permite.
- `garantia.go:498-500` y `501-503`: `buildEvent` y `autorizarCambioFisico` en el flujo de reemplazo físico, inalcanzables con actores válidos y la tabla de transiciones vigente.

Cubrirlas exigiría construir estados hidratados inválidos a propósito, lo que prueba la defensa y no la regla. Todo el código nuevo (`auditoria_entrega.go`, `firma.go`, `Entregar`, `Cerrar`, huella) está al 100%.

### `garfb` 35.5%

`FB_DATABASE` no está definido y no hay `mueblera-firebird` corriendo en esta máquina; las pruebas de integración se saltan. El piso de 85% con base queda pendiente para cuando se disponga de la base de desarrollo.

## Mutantes K1–K15

Todos los mutantes se aplicaron al árbol limpio, se corrió la prueba indicada (muerte roja), se restauró el archivo y se volvió a correr en verde. Cada uno va con su parche `diff` exacto.

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

Cada mutante incluye el parche `diff` exacto, la salida roja de la prueba que falla y la verde al revertirlo.

## Prueba de huella por reflexión

La prueba table-driven recorre cada `*Cmd` exportado de `app` (listado obtenido con `go/parser` de `app/*.go`, sin `_test.go`). Para cada campo salvo `ClaveIdempotencia`, cambia ese campo a otro valor válido y comprueba que la huella cambia; cambiar sólo `ClaveIdempotencia` no la cambia. Dos comandos de distinta forma con mismos valores dan huellas distintas.

## Alcance de Parte 2

- Ruta multipart `entregar-garantia`: `POST /garantias/{id}/entregar` con `gps_lat`, `gps_lon`, `device_created_at` y archivo `firma`.
- Pruebas multipart: sin firma → 422 `warranty_signature_required`; firma 0 bytes / >1 MB / no JPEG-PNG → 422; replay misma firma → 200 sin blob huérfano; replay otra firma → 409 y blob del reintento borrado; error de base → blob borrado; éxito → blob se conserva; 401/403 en las 14 operaciones.
- Tres e2e HTTP en `garhttp/e2e_comandos_test.go`: piso `no_aplica`; proveedor `con_desvio` con justificación; taller `sin_desvio`; replay del mismo POST dos veces (200, una sola fila) y tercero con misma clave y otro cuerpo (409, nada escrito).
- `infra/garfb/e2e_test.go` actualizado con firma y auditoría.

## Pendiente externo (tarea 10)

Fuera del alcance de esta tarea, pero necesario para la entrega conjunta con Ruben:

- Migración `000071` y columnas `HUELLA` / `AUDITORIA_GPS` / `DISTANCIA_ENTREGA_M` / `TIPO`.
- `app/blob.go` (de Ruben) con `guardarBlob` y `descartarBlobSiNoQuedo`.
- Inserción de imágenes en `Guardar` de `garfb`.

## Desviaciones declaradas respecto al brief

1. Cobertura `domain` 99.3% en vez de 100% (ramas defensivas preexistentes, justificada arriba).
2. Cobertura `garfb` no medible sin `FB_DATABASE`; se documenta como pendiente.
3. Los números de cobertura y verificación se obtuvieron sobre el árbol limpio antes de este commit; el SHA de entrega se asigna al commitear este reporte.

## Archivos tocados

Solamente archivos permitidos por el brief:

- `internal/garantias/domain/**` (excepto `tipo_imagen.go`, `auditoria_gps.go`, `huella.go`, `contrato_*_test.go`)
- `internal/garantias/app/**` (excepto `adjuntar_imagen*.go`, `blob*.go`, `consultas*.go`, `adjunto_*_test.go`)
- `internal/garantias/infra/garhttp/routes_comandos.go`
- `internal/garantias/infra/garhttp/handlers_comandos.go`, `dto_comandos.go`
- `internal/garantias/infra/garhttp/*_comandos_test.go`, `ejemplos_comandos_test.go`
- `internal/garantias/infra/garfb/e2e_test.go`
- `docs/superpowers/plans/garantias-task-9-report.md`

Nada en `routes.go`, `dto.go`, `dto_mapper.go`, `errores.go`, `handlers_consultas.go`, resto de `garfb/`, migraciones, `cmd/api/`, `internal/auth/` ni `Makefile`.