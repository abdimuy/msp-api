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
internal/comprobantes/domain/motivo_supresion.go
internal/comprobantes/domain/tipo_comprobante_test.go
internal/comprobantes/domain/estado_envio_test.go
internal/comprobantes/domain/canal_test.go
internal/comprobantes/domain/motivo_supresion_test.go
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

Todos los tests pasan con el detector de carreras activo: 28 funciones de test,
incluyendo la tabla exhaustiva de `EstadoEnvio` (6 valores ×
`EsDetenible`/`EsTerminal`/`EsFalla`) y la comprobación explícita de que
`sin_telefono` **no** es falla.

### 6. `go tool cover -func=coverage-comprobantes-domain.out | tail -1`

```
total:										(statements)		100.0%
```

Cobertura de `domain`: **100.0%** (piso del gate: 99.0%).

## Qué se tomó de `tipo_movimiento.go` y diferencias por tipo

Molde estructural copiado de `internal/inventario/domain/tipo_movimiento.go`:
dos constantes públicas por valor, struct privado `{ value string }`,
constructor validador `New*`, `Hydrate*` sin validación, y los métodos
`Value()`, `String()`, `Equals()`, `IsZero()` con la misma semántica.

| Tipo | Constantes | Ayudantes | Diferencias frente al molde |
|---|---|---|---|
| `TipoComprobante` | `TipoVenta`, `TipoPago` | `EsVenta()`, `EsPago()` | Idéntico al molde; dos valores (`venta`/`pago`) en vez de `S`/`E`. |
| `EstadoEnvio` | 6 constantes `EstadoEnvio*` | `EsDetenible()`, `EsTerminal()`, `EsFalla()` | Tres ayudantes en vez de dos; `EsTerminal()` usa un `switch` explícito sobre los 4 estados finales para que quede documentado en código cuáles son. `EsFalla()` es `== fallido` a propósito (ver abajo). |
| `Canal` | `CanalLocal`, `CanalWhatsappBusiness` | `EsReal()` | Un ayudante. `EsReal()` solo es true para `whatsapp_business` para que un envío de prueba nunca cuente como entregado. |
| `MotivoSupresion` | `MotivoSupresionRebote` | `EsRebote()` | Un solo valor válido por ahora; el constructor rechaza todo lo que no sea `rebote`. Se modela como VO (y no constante suelta) porque aparecerán más motivos. |

**Nota de diseño de `EstadoEnvio`:** `EsFalla()` devuelve true **solo** para
`fallido`. `sin_telefono` es terminal pero no falla: con ~68.8% de cobertura de
teléfono, un tercio de los envíos cae en `sin_telefono`, y contarlo como falla
llenaría la bitácora de ruido y dispararía reintentos a clientes sin teléfono.
La prueba `TestEstadoEnvio_ExhaustiveHelpers` verifica explícitamente que
`sin_telefono` → `EsFalla() == false`.

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

Commit único en `feat/comprobantes-domain`, sin push (no hay acceso de escritura
al remoto):

```
feat(comprobantes): value objects y errores del dominio
```
