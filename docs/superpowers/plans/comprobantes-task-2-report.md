# Comprobantes — Tarea 2: reporte

> **Rama:** `feat/comprobantes-domain`
> **Brief:** `docs/superpowers/plans/comprobantes-task-2-brief.md`
> **Estado:** completada — gate verde con los 6 comandos

## Archivos creados

```
internal/comprobantes/domain/doc.go
internal/comprobantes/domain/errors.go
internal/comprobantes/domain/tipo_comprobante.go
internal/comprobantes/domain/estado_envio.go
internal/comprobantes/domain/canal.go
internal/comprobantes/domain/tipo_comprobante_test.go
internal/comprobantes/domain/estado_envio_test.go
internal/comprobantes/domain/canal_test.go
```

**Archivo adicional fuera de la lista del brief (aprobado por el responsable):**

```
.golangci.yml — se agregó `- github.com/abdimuy/msp-api/internal/comprobantes/domain`
al allow list de la regla `domain-pure` (depguard). Sin ese registro los tests
`domain_test` no pueden importar el paquete `domain` y el gate de lint es
imposible de pasar. Es el patrón establecido para cada módulo nuevo (ver
`docs/module-standards/09-module-wiring.md`).
```

## Salida literal de los comandos del gate

### 1. `gofmt -l internal/comprobantes`

```
(sin salida)
```

### 2. `go vet ./internal/comprobantes/...`

```
(sin salida, exit 0)
```

### 3. `go build ./...`

```
(exit 0)
```

### 4. `golangci-lint run ./internal/comprobantes/...`

```
0 issues.
```

### 5. `go test -race -coverprofile=... ./internal/comprobantes/domain/`

```
ok  	github.com/abdimuy/msp-api/internal/comprobantes/domain	2.943s	coverage: 100.0% of statements
PASS
```

Todos los tests pasan con el detector de carreras activo: 16 funciones de test
(6 en `estado_envio_test.go`, 5 en `canal_test.go` y 5 en
`tipo_comprobante_test.go`), incluyendo la tabla exhaustiva de `EstadoEnvio`
(6 valores × `EsDetenible`/`IsTerminal`/`EsFalla`), la cobertura de
`CanTransitionTo` sobre todas las aristas del mapa (incluida `fallido →
en_espera`) y la comprobación explícita de que `sin_telefono` **no** es falla.

### 6. `go tool cover -func=coverage-comprobantes-domain.out | tail -1`

```
total:										(statements)		100.0%
```

Cobertura de `domain`: **100.0%** (piso del gate: 99.0%).

## Qué se tomó de `ventas` y diferencias por tipo

Molde tomado de `docs/module-standards/02-value-objects-errors.md` (categoría 1
enum y categoría 2 state), con las referencias reales
`internal/ventas/domain/tipo_venta.go` y `estado_registro.go`: tipo
`string`-backed, constantes tipadas por valor, y para los enums
exactamente `Parse`/`IsValid`/`String`; para el estado, además
`CanTransitionTo`, `IsTerminal` y el mapa de transiciones. Se eliminaron los
métodos `New*`/`Hydrate*`/`Value()`/`Equals()`/`IsZero()` del molde original
del brief (`tipo_movimiento.go`): hidratar es un cast (`Canal(row.Canal)`),
comparar es `==`, y el valor cero es `== ""`.

| Tipo | Constantes | Métodos | Diferencias frente al molde |
|---|---|---|---|
| `TipoComprobante` | `TipoVenta`, `TipoPago` | `Parse`, `IsValid`, `String`, `EsVenta()`, `EsPago()` | Enum VO; dos valores (`venta`/`pago`). |
| `EstadoEnvio` | 6 constantes `EstadoEnvio*` | `Parse`, `IsValid`, `String`, `CanTransitionTo`, `IsTerminal`, `EsDetenible()`, `EsFalla()` | State VO con `validEstadoEnvioTransitions` (spec §4.3 + `fallido → en_espera` por `/reenviar`). `EsFalla()` es `== fallido` a propósito (ver abajo). |
| `Canal` | `CanalLocal`, `CanalWhatsappBusiness` | `Parse`, `IsValid`, `String`, `EsReal()` | Enum VO. `EsReal()` solo es true para `whatsapp_business` para que un envío de prueba nunca cuente como entregado. |

`MotivoSupresion` **no está**: no aparece en el spec (solo en el brief), y la
única fuente de rebotes son los webhooks de Meta, fuera de v1 (§12). Se eliminó
junto con su sentinela `ErrMotivoSupresionInvalido` en la corrección post-revisión.

**Nota de diseño de `EstadoEnvio`:** `EsFalla()` devuelve true **solo** para
`fallido`. `sin_telefono` es terminal pero no falla: con ~68.8% de cobertura de
teléfono, un tercio de los envíos cae en `sin_telefono`, y contarlo como falla
llenaría la bitácora de ruido y dispararía reintentos a clientes sin teléfono.
La prueba `TestEstadoEnvio_ExhaustiveHelpers` verifica explícitamente que
`sin_telefono` → `EsFalla() == false`.

`fallido` **no** es terminal: el `UNIQUE (TIPO, REFERENCIA)` de `MSP_CM_ENVIO`
(§5.1) obliga a que `POST /{id}/reenviar` reúse la fila y vuelva a `en_espera`
incrementando `INTENTOS`, así que `fallido → en_espera` es una transición válida
del mapa y `IsTerminal()` solo cubre `enviado`, `detenido` y `sin_telefono`.

## Confirmación de imports

`internal/comprobantes/domain` importa **solo** la biblioteca estándar y
`github.com/abdimuy/msp-api/internal/platform/apperror`. Lo verifica `depguard`
(regla `domain-pure`): `golangci-lint run ./internal/comprobantes/...` → 0 issues.

## Notas del entorno

- Se instaló **gcc 16.1.0** (WinLibs/MinGW-w64) vía `winget` en scope de
  usuario (`C:\Users\User\AppData\Local\Microsoft\WinGet\Packages\...\mingw64\bin`)
  para poder correr `go test -race` con `CGO_ENABLED=1`. El PATH se modificó de
  forma persistente por winget; para nuevas sesiones conviene reiniciar el shell.
- `golangci-lint run ./...` (repo completo, sin `--new-from-rev`) reporta 1084
  issues de `gofumpt` preexistentes en todo el repo: el checkout de este Windows
  tiene `core.autocrlf=true` y los archivos del repo están en CRLF mientras
  gofumpt exige LF. No afecta a los archivos de esta tarea (LF, 0 issues) ni al
  gate del brief. El pre-commit hook usa `--new-from-rev=origin/main`, que pasa
  con 0 issues.
- `golangci-lint` se instaló en `C:\Users\User\go\bin\golangci-lint.exe` v2.11.3
  (versión que fija el README).

## Rama y commit

La rama `feat/comprobantes-domain` se subió al remoto y es el PR #7. El commit
de esta tarea es:

```
feat(comprobantes): add domain value objects and sentinel errors
```

Este reporte se corrigió tras la revisión del PR #7 (CHANGES_REQUESTED):
los VOs se convirtieron a las categorías 1 y 2 del estándar
(`docs/module-standards/02-value-objects-errors.md`), se eliminó
`MotivoSupresion` y su sentinela, y este documento refleja el estado final.
