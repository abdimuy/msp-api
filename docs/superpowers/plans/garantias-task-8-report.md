# Tarea 8 — Reporte único (Entrega 1 + Entrega 2)

Este es el **único** reporte de la tarea 8 y crece con las dos entregas. Cubre la
Entrega 1 (dominio + migración) y se completará con la Entrega 2 (los 10
comandos, el repositorio y las pruebas de punta a punta).

- **Rama:** `feat/garantias-dominio-v2`
- **Estado:** Entrega 1 implementada, verificada y **commiteada** en esta rama.
  Aún **sin push** y sin el PR abierto.
- **Brief:** `docs/superpowers/plans/garantias-task-8-brief.md`

Bloques de la Entrega 1, como los acordamos:
1. Migración `000065` + R1 + R2 + R7.
2. R3, R4, R5, R6 y R8 (+ R9 adelantado).
3. R9, R10, R11 y R12.

---

## 1. Alcance entregado

| Requisito | Archivo principal | Estado |
|---|---|---|
| R1 — filtro por origen sobre la máquina de etapas | `domain/transiciones.go` | ✅ |
| R2 — alcance limitado de `AvanzarArticulo` | `domain/transiciones.go` | ✅ |
| R3 — tabla etapa → ubicación (automática) | `domain/transiciones.go` | ✅ |
| R4 — una sola tabla estado × operación | `domain/estado_folio.go` | ✅ |
| R5 — no cancelar con el mueble fuera del taller | `domain/garantia.go` + `transiciones.go` | ✅ |
| R6 — desenlace automático derivado | `domain/articulo.go` | ✅ |
| R7 — cada comando de folio emite su tipo | `domain/garantia.go` | ✅ |
| R8 — guardia de `MarcarListoEntrega` | `domain/garantia.go` | ✅ |
| R9 — `Entregar` con GPS y desenlace | `domain/garantia.go` | ✅ |
| R10 — `nuevo` / `modificado` / limpio | `domain/articulo.go`, `garantia.go` | ✅ |
| R11 — catálogos estables (`Etapas`, `Ubicaciones`, `SiguientesPorAvanzar`) | `domain/etapa.go`, `ubicacion.go`, `transiciones.go` | ✅ |
| R12 — GPS del domicilio, NFC, `Description`, prueba de releído | `domain/garantia.go`, `texto.go`, `evento.go`, `app/` | ✅ |
| Migración `000065` | `migrations-firebird/000065_*` | ✅ aplicada y revertida |

---

## 2. Migración `000065_add_ubicacion_a_msp_ga_evento`

Agrega `UBICACION_DESDE` y `UBICACION_HASTA` a `MSP_GA_EVENTO`, ambas
`VARCHAR(28) CHARACTER SET ASCII`, nullable y sin `DEFAULT` (regla dura #1: el
valor lo escribe Go). El ancho y el charset son **idénticos** a
`ETAPA_DESDE`/`ETAPA_HASTA`.

**La columna de R6 no se agrega**: "pasó por reparado" se deriva de `RUTA` +
`DICTAMEN` en el propio agregado (ver §5), así que el artículo no necesita
recordarlo en una columna. Queda declarado también en el comentario de la
migración, como pide el brief.

### 2.1 Estado previo de la base (dev)

La base tenía migraciones hasta `000062` (la `000063`/`000064` existen sólo en
producción). La `000065` no estaba aplicada:

```
          ID NAME
============ ========================================================================================================================================================================================================
          62 000062_create_msp_flota_asignaciones
```

### 2.2 Aplicar la `up`

`make fb-migrate-*` no corre en esta máquina porque ezwinports `make` usa
`cmd.exe` y los recipes dependen de *here-strings* (`<<<`). Se ejecutó el mismo
`docker exec … isql` que arma el Makefile:

```powershell
Get-Content -Raw migrations-firebird\000065_add_ubicacion_a_msp_ga_evento.up.sql |
  docker exec -i mueblera-firebird /usr/local/firebird/bin/isql `
    -u SYSDBA -p masterkey -ch UTF8 /firebird/data/MUEBLERA.FDB
```

Salida: **(ninguna)** — `isql` no imprime nada cuando el DDL pasa.

Verificación (columnas + fila de control):

```
RDB$FIELD_NAME
===============================================================
ETAPA_DESDE
ETAPA_HASTA
UBICACION_DESDE
UBICACION_HASTA

          ID NAME
============ ========================================================================================================================================================================================================
          65 000065_add_ubicacion_a_msp_ga_evento
```

Tipo y charset, idénticos a `ETAPA_DESDE`:

```
RDB$FIELD_NAME                                                  RDB$FIELD_TYPE RDB$CHARACTER_LENGTH RDB$CHARACTER_SET_ID
=============================================================== ============== ==================== ====================
ETAPA_DESDE                                                                 37                   28                    2
UBICACION_DESDE                                                             37                   28                    2
UBICACION_HASTA                                                             37                   28                    2
```

(`37` = `VARCHAR`, `28` = longitud, `2` = `ASCII`.)

### 2.3 Revertir la `down` y volver a aplicar la `up`

```powershell
Get-Content -Raw migrations-firebird\000065_add_ubicacion_a_msp_ga_evento.down.sql |
  docker exec -i mueblera-firebird /usr/local/firebird/bin/isql `
    -u SYSDBA -p masterkey -ch UTF8 /firebird/data/MUEBLERA.FDB
```

Tras la `down`, la consulta de columnas `UBICACION%` y la de `ID = 65` devuelven
**ambas vacío** (sin error): las columnas y la fila de control se fueron.

Después se volvió a aplicar la `up` para dejar la base de desarrollo migrada:

```
UBICACION_DESDE
UBICACION_HASTA

          ID NAME
============ ========================================================================================================================================================================================================
          65 000065_add_ubicacion_a_msp_ga_evento
```

---

## 3. Cómo se resolvió el "pasó por reparado" de R6 (y por qué)

**Se deriva, no se guarda.** `Articulo.pasoPorReparado()` (`domain/articulo.go`)
responde `true` cuando se cumple alguna de estas dos:

- `RUTA == taller` → el artículo sólo llega a `listo_entrega` pasando por
  `reparado_taller`, así que la ruta taller **implica** reparación; o
- `RUTA == proveedor` **y** `DICTAMEN == aceptada` → en la ruta proveedor sólo el
  dictamen `aceptada` avanza a `reparado_proveedor`.

**Por qué:** una columna dedicada sería un dato derivable que podría quedar
desincronizado de `RUTA`/`DICTAMEN` (dos fuentes de verdad para lo mismo). La
regla ya es función de dos columnas que siempre están en el row, y el orden de
prioridad de `desenlaceAutomatico` (reemplazo → devuelto → reparado) queda
cubierto por prueba (`TestDesenlaceAutomatico/reemplazo_gana_a_rechazada`). El
brief permitía esta decisión ("la columna de R6 si la agregaste").

---

## 4. Pruebas de la Entrega 1 (brief §"Pruebas de la entrega 1")

`domain` con **100.0 %** de cobertura. Mapa de las 10 pruebas pedidas:

| # | Qué pide el brief | Prueba(s) |
|---|---|---|
| 1 | Máquina completa, exhaustiva (origen × etapa × destino) | `TestAvanzarArticulo_Exhaustivo`, `TestAvanzarArticulo_ReglaDeOrigen`, `TestAvanzarArticulo_SalidasReservadas` (`avanzar_origen_test.go`) |
| 2 | Ubicación: cada fila de R3, el "no cambia", `MoverArticulo` completo, el evento con desde/hasta | `TestUbicacionAutomatica_PorEtapa`, `_NoCambia`, `_Entregado`, `_Paralelo`, `TestMoverArticulo_DestinosPermitidos`, `_DestinosProhibidos`, `_MismaUbicacion`, `_Terminal`, `_Errores` |
| 3 | R4: tabla estado × operación, incluida la excepción standby con folio cerrado | `TestFolioOperacion_Matriz`, `_ExcepcionStandby`, `_EstadoInvalido` |
| 4 | R5: cancelar con artículo en `proveedor`, `en_transito`, `taller` | `TestCancelar_ArticuloFuera`, `TestCancelar` |
| 5 | R6: cada fila + orden de prioridad | `TestDesenlaceAutomatico`, `TestDesenlaceAutomatico_Piso` |
| 6 | R7: cada comando de folio emite su tipo | `TestEventosDeFolio_ConTipoPropio` |
| 7 | R8 y R9 con sus rechazos (y standby que no se entrega) | `TestMarcarListoEntrega_Guardia`, `TestEntregar`, `TestFolio_EntregaYCierra`, `TestUbicacionAutomatica_Entregado` |
| 8 | R10: nuevo / modificado / limpio en cada mutador y tras hidratar | `TestR10_HidratadoEsLimpio`, `TestR10_ArticuloNuevo`, `TestR10_NuevoSigueNuevoTrasMutar`, `TestR10_MutadorMarcaModificado`, `TestR10_ListadosSonCopias` (`dirty_tracking_test.go`) |
| 9 | R11: `SiguientesPorAvanzar` coincide con la tabla de la prueba 1 | `TestR11_SiguientesPorAvanzar_CoincideConLaMaquina`, `_NuncaNil`, `_OrdenEstable`, `TestR11_Etapas_*`, `TestR11_Ubicaciones_*`, `TestR11_CatalogosSonCopias` (`catalogos_test.go`) |
| 10 | En cada rechazo, las dos mitades (estado intacto **y** sin evento) | `TestEventosDecision_SinRolRechazadoSinMutarNiEncolar`, `TestEventosPendientes_CancelaTemprano`, `TestMutaciones_RechazanActorInvalido`, `TestMutaciones_RechazanRolDecisorInvalido`, `TestAgregarArticulo_FolioNoAdmite` |

**Discrepancia del brief en R9 (declarada).** La prosa del brief dice "un evento
por artículo, más el `folio_entregado`"; la prueba #7 del mismo brief habla de
"dos eventos de artículo". Manda la prosa: `Entregar` emite **un**
`etapa_avanzada` por artículo que pasa a `entregado` (sólo los `listo_entrega`
del origen cliente) **más** un `folio_entregado`. Un artículo en `standby` no se
mueve ni emite evento.

---

## 5. R12 — los cuatro pendientes

- **GPS del domicilio**: `AbrirGarantia` valida `GPSLat`/`GPSLon` con el mismo
  `gpsValido` del evento (par completo o nada, en rango, finito), justo después
  de `validarDomicilioLargo`. Sentinela nueva `ErrDomicilioGPSInvalido`
  (`warranty_address_gps_invalid`). En folios de piso gana antes
  `ErrDomicilioNoPermitido`, porque `validarOrigenPiso` corre primero.
  Pruebas: `TestR12_DomicilioGPS_SeValidaComoElEvento`,
  `_AusenteOCompletoSeAcepta`, `_PisoSigueRechazandoElDomicilio`.
- **NFC**: helper `normalizarNFC` (`domain/texto.go`, `norm.NFC.String`). Se
  aplica en el constructor/mutador que **recibe** el texto — `AbrirGarantia`
  (description, abiertoPor y los 6 campos de domicilio), `newArticulo`
  (description) y `newEvento` (usuario + description) —, no en la hidratación
  (esas filas ya salieron normalizadas). Pruebas: `TestR12_NFC_FolioNormalizaElTextoLibre`,
  `_ArticuloNormalizaLaDescripcion`, `_EventoNormalizaLaDescripcion`.
- **`PermisoInfo.Descripcion` → `Description`** en `garantias_contracts.go` (+ su
  prueba). Se quitó el `//nolint:misspell` que ya no aplica.
- **Prueba del folio releído vs. el agregado en memoria** (nota 1 de tu última
  revisión): `TestAbrirGarantia_DevuelveElFolioReleido` en
  `internal/garantias/app/abrir_garantia_test.go`. Distingue ambos: el agregado
  que recibe `Crear` todavía trae su evento `folio_abierto` pendiente; la
  respuesta del comando no trae ninguno (es el folio releído). Si el comando
  devolviera el agregado de memoria, esta prueba lo delata.

---

## 6. Mutantes de la Entrega 1 (D1–D10)

Cada mutante se aplicó a mano, se corrió `go test ./internal/garantias/domain/...`,
se anotó la prueba que falla y se **revirtió con la edición inversa** (nunca con
`git checkout`, porque en ese momento el trabajo estaba sin commitear). Todos los
mutantes matan al menos una prueba.

| # | Mutante aplicado | Prueba(s) que falla(n) | Evidencia |
|---|---|---|---|
| D1 | `transicionPermitidaPorOrigen` ignora el origen (devuelve sólo `CanTransitionTo`) | `TestAvanzarArticulo_ReglaDeOrigen`, `TestR11_SiguientesPorAvanzar_CoincideConLaMaquina`, `TestAvanzarArticulo_Exhaustivo` | `FAIL github.com/abdimuy/msp-api/internal/garantias/domain` |
| D2 | `etapaConSalidaReservada` deja de incluir `dictamen_recibido` | `TestAvanzarArticulo_SalidasReservadas`, `TestR11_SiguientesPorAvanzar_CoincideConLaMaquina`, `TestAvanzarArticulo_Exhaustivo` | idem |
| D3 | se quita la fila `EtapaEnTaller → UbicacionTaller` de `ubicacionPorEtapa` | `TestUbicacionAutomatica_PorEtapa` | idem |
| D4 | `ubicacionesManualesPermitidas` permite `UbicacionEntregado` | `TestMoverArticulo_DestinosProhibidos` | idem |
| D5 | el estado terminal de R4 devuelve `false` (sin excepción de standby) | `TestFolioOperacion_ExcepcionStandby`, `TestFolioOperacion_Matriz` | idem |
| D6 | `ubicacionFueraDelTaller` devuelve `false` | `TestCancelar_ArticuloFuera` | idem |
| D7 | `desenlaceAutomatico` evalúa `dictamen` antes que `reemplazo` | `TestDesenlaceAutomatico` (fila `reemplazo_gana_a_rechazada`) | idem |
| D8 | `Entregar` sin el chequeo de GPS | `TestEntregar` | idem |
| D9 | `tocar` no marca `modificado` | `TestR10_MutadorMarcaModificado` (los 8 subtests) y `TestR10_ListadosSonCopias` | idem |
| D10 | `AbrirGarantia` no normaliza NFC la `description` | `TestR12_NFC_FolioNormalizaElTextoLibre` | idem |

Nota de D9: al correrlo, `TestR10_ListadosSonCopias` fallaba con un `panic`
(índice fuera de rango sobre la lista vacía). Endurecí esa prueba para que falle
limpio (`t.Fatalf`) en vez de reventar; en código correcto pasa igual.

Los mutantes C1–C6 son de la Entrega 2 y quedan pendientes.

---

## 7. Verificación literal (Entrega 1)

```
### gofmt -l ./internal/garantias/ ###
### go vet ./internal/garantias/... ###
### go build ./... ###
### golangci-lint run ./internal/garantias/... ###
0 issues.

### go test -race -count=1 -p 1 ./internal/garantias/... ###
ok  	github.com/abdimuy/msp-api/internal/garantias	1.651s
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.198s
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	2.399s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	2.597s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	2.392s
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	2.224s

### go test -count=1 -cover ./internal/garantias/... ###
ok  	github.com/abdimuy/msp-api/internal/garantias	2.262s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.355s	coverage: 42.2% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	2.112s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	1.672s	coverage: 0.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	0.843s	coverage: 83.8% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	0.774s	coverage: 100.0% of statements
```

- `domain`, `garantias` y `ports/outbound`: **100 %**.
- `infra/garfb`: `0 %` en unitarios porque sus pruebas de integración se saltan
  sin `FB_DATABASE` (ver §8).
- `app`: **42.2 %** a propósito. La Entrega 1 sólo trae la prueba de `AbrirGarantia`
  (R12); el resto de los comandos y sus pruebas son de la Entrega 2.

**Módulo sellado (ADR-0009)** — equivalente a `make check-sealed` (no corre en
PowerShell por el mismo problema de `cmd.exe`):

```
$leaked = go list -deps ./internal/garantias/... |
  Select-String 'msp-api/internal/' |
  Where-Object { $_ -notmatch 'msp-api/internal/garantias|msp-api/internal/platform' } |
  Sort-Object -Unique
# => (vacío)
OK: garantias is sealed
```

---

## 8. Integración Firebird: hallazgo de R4 en `infra/garfb` (para la Entrega 2)

Con `FB_DATABASE` presente (Docker arriba) las pruebas de integración de
`infra/garfb` **sí** corren, y **5 fallan**:

```
--- FAIL: TestGarantiaRepo_GuardarAvanceYEvento
--- FAIL: TestGarantiaRepo_GuardarArticuloNuevoYReemplazo
--- FAIL: TestGarantiaRepo_GuardarEsAtomicoSiFallaEvento
--- FAIL: TestGarantiaRepo_PersisteRutaDictamenYDatosDeEvento
--- FAIL: TestGarantiaRepo_PersisteDesenlaceYCierre
FAIL	github.com/abdimuy/msp-api/internal/garantias/infra/garfb
```

**Causa exacta:** son pruebas que vienen de `main` y **preceden a R4**. Arman el
folio con `AbrirGarantia` + `AgregarArticulo` y después llaman `AvanzarArticulo`
/ `RegistrarDiagnostico` directamente sobre el agregado cargado, **sin**
`IniciarProceso`. Con R4, un folio en `abierto` sólo admite `opAgregarArticulo`,
así que el comando se rechaza con `warranty_folio_operation_not_allowed`
(`ErrFolioNoAdmiteOperacion`).

No es una regresión del repositorio (que todavía no referencia las columnas
nuevas) sino el **efecto esperado** de la tabla estado × operación sobre pruebas
que no conocían R4. Arreglarlas —"sus pruebas de integración al día"— es
exactamente el alcance de la Entrega 2 (brief, §"El repositorio"). El gate de
CI no las corre hoy: lefthook ejecuta `go test` sin cargar `.env`, y `make test`
usa `test-unit` (sin `FB_DATABASE`); por eso no aparecían antes.

---

## 9. Dependencia declarada: el PR #26 no está mergeado

El paquete `internal/garantias/app` **no existe** en esta rama: viene del PR #26
(`origin/feat/garantias-comandos-apertura`), que **todavía no está mergeado**.
Para que la prueba de R12 (`app/abrir_garantia_test.go`) compile y corra, se
trajo de ese PR, **textualmente**:

- `app/service.go`
- `app/abrir_garantia.go`
- `app/agregar_articulo.go`, `app/avanzar_articulo.go`, `app/iniciar_proceso.go`
  (los necesita `service.go`: sin ellos `ejecutarFolio`/`revisarClave`/`operacion`
  quedan sin uso y `golangci-lint` falla con `unused`)
- `app/fakes_test.go`, `app/fakes_repo_test.go`

Y **se agregó** lo propio de esta tarea:
`app/abrir_garantia_test.go` con `TestAbrirGarantia_DevuelveElFolioReleido`.

**No se trajeron** las pruebas de comando del PR #26
(`agregar_articulo_test.go`, `avanzar_articulo_test.go`, `iniciar_proceso_test.go`,
`errores_test.go`, `transaccion_test.go`): preceden a R4 y hoy fallan (mismo
motivo que §8). Pertenecen a la Entrega 2 / al rebase del PR #26.

**Para el líder:** el PR #26 no está mergeado; esta rama depende de él. Cuando se
mergee (o rebase), los archivos que trajimos son idénticos, así que no debería
haber conflicto, salvo la prueba que agregamos.

---

## 10. Desviaciones del brief (declaradas con el porqué)

1. **`internal/garantias/app` traído del PR #26** (ver §9). El brief asume que
   ese paquete ya existe; como el PR no está mergeado, lo trajimos para que la
   prueba de R12 corra.
2. **Pruebas de comando del PR #26 no traídas** (ver §9): preceden a R4.
3. **`ENCODING_HANDLING.md`**: se documentó ahí el helper `normalizarNFC` de
   garantías (el brief dice "va en `ENCODING_HANDLING.md`"). Es el único cambio
   fuera de `internal/garantias` y `migrations-firebird`.
4. **`TestR10_ListadosSonCopias`** se endureció para fallar limpio (no con
   `panic`) — ver §6, nota de D9.
5. **R9 adelantado** al Bloque 2 (estaba planeado en el Bloque 3), como acordamos.

---

## 11. Entrega 2 (pendiente, martes 20)

- Los 10 comandos, cada uno en su archivo, todos por `ejecutarFolio`.
- El repositorio `infra/garfb`: `insertarEvento`/`scanEvento` con
  `UBICACION_DESDE`/`UBICACION_HASTA`, `Guardar` con R10 (INSERT de nuevos,
  UPDATE sólo de modificados, y un UPDATE que afecta 0 filas = error), y sus
  **pruebas de integración al día** (incluidas las 5 de §8).
- Las tres pruebas de punta a punta del §8, con la línea de tiempo literal leída
  de la base.
- Mutantes C1–C6.

---

## 12. Estado del árbol de trabajo

La Entrega 1 quedó en **un commit** en `feat/garantias-dominio-v2` (sin push
todavía). El commit incluye **sólo** lo del entregable. Archivos de esta tarea
(nombres):

- Modificados: `domain/{articulo,errors,estado_folio,etapa,evento,garantia,tipo_evento,transiciones,ubicacion}.go`,
  y sus `_test.go`; `garantias_contracts.go` + `_test.go`;
  `docs/module-standards/ENCODING_HANDLING.md`.
- Nuevos: `domain/{texto.go,avanzar_origen_test.go,cancelar_fuera_test.go,catalogos_test.go,desenlace_automatico_test.go,dirty_tracking_test.go,entregar_test.go,folio_eventos_test.go,folio_operacion_test.go,marcar_no_devuelto_test.go,r12_pendientes_test.go,ubicacion_movimiento_test.go}`.
- Nuevos (del PR #26): `app/{service.go,abrir_garantia.go,abrir_garantia_test.go,agregar_articulo.go,avanzar_articulo.go,iniciar_proceso.go,fakes_test.go,fakes_repo_test.go}`.
- Nuevos: `migrations-firebird/000065_add_ubicacion_a_msp_ga_evento.{up,down}.sql`.

Archivos preexistentes fuera de esta tarea y **no tocados** por mí:
`Dockerfile.dev`, `internal/auth/infra/firebird/pagination.go`,
`internal/cobranza/infra/ventfb/fb_event_source.go` (modificados), y
`AGENTS.md`, `t.txt`, `tmp_fix.py`, `tmp_gps.py` (sin seguimiento). Todos
quedaron **fuera** del commit y siguen en el árbol de trabajo.
