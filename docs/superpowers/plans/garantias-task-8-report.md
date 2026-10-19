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

*(El ciclo up/down se volvió a correr al cerrar la Entrega 2, con su salida
literal en §11.7.)*

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
6. **Una sola rama, un solo PR.** El brief (líneas 3–5) pide la Entrega 2 en una
   rama nueva `feat/garantias-comandos-v2`, creada desde la Entrega 1 cuando el
   #26 ya esté en `main`. Las dos entregas se hicieron en
   `feat/garantias-dominio-v2` (dos commits, un PR), por decisión tuya. Se
   declara aquí porque difiere del brief.
7. **`RegistrarDesenlace` comparte archivo con `RegistrarDiagnostico`**
   (`app/registrar_diagnostico.go`). El brief pide "cada uno en su archivo"
   (línea 235), pero los dos comparten la envoltura de comando y `dupl` —que el
   repo tiene activo en producción, con umbral 150— la marcaba como duplicada
   estando separados. Separarlos exigía incrustar la envoltura, lo que rompe los
   literales con nombre de campo en 25 puntos de llamada. Unirlos deja los
   llamadores intactos y el código se lee igual. Es la única desviación sobre el
   "cada uno en su archivo"; los otros ocho comandos sí están cada uno en el suyo.
8. **El ciclo up/down de la `000065`** se corrió con `docker exec … isql` en vez
   de `make fb-migrate-*`, porque `make` no existe en el shell de este entorno.
   Es el mismo comando que el Makefile arma dentro de esos targets; la salida
   literal está en §11.7 y en §2.

---

## 11. Entrega 2 (hecha)

### 11.1 Los 10 comandos (hecho)

Cada uno en su archivo, todos por `ejecutarFolio`, pasando el **tipo de evento**
que producen y parseando con el `Parse` del dominio los strings que vienen de
fuera (el error del dominio se devuelve tal cual):

| Comando | Archivo | Permiso | Tipo de evento |
|---|---|---|---|
| `RegistrarDiagnostico` | `app/registrar_diagnostico.go` | `actualizar` | `diagnostico_registrado` |
| `RegistrarDictamen` | `app/registrar_dictamen.go` | `actualizar` | `dictamen_registrado` |
| `AutorizarCambioFisico` | `app/autorizar_cambio_fisico.go` | `autorizar` | `cambio_autorizado` |
| `MarcarNoDevuelto` | `app/marcar_no_devuelto.go` | `autorizar` | `devolucion_fallida` |
| `RegistrarDesenlace` | `app/registrar_diagnostico.go` | `autorizar` | `desenlace_registrado` |
| `MoverArticulo` | `app/mover_articulo.go` | `actualizar` | `articulo_movido` |
| `MarcarListoEntrega` | `app/marcar_listo_entrega.go` | `actualizar` | `folio_listo_entrega` |
| `Entregar` | `app/entregar.go` | `cerrar` | `folio_entregado` (GPS obligatorio) |
| `Cerrar` | `app/cerrar.go` | `cerrar` | `folio_cerrado` |
| `Cancelar` | `app/cancelar.go` | `cerrar` | `folio_cancelado` (motivo obligatorio) |

`RegistrarDesenlace` vive en el mismo archivo que `RegistrarDiagnostico` porque
los dos comparten la misma envoltura de comando (folio, artículo, rol, clave,
GPS) y `dupl` la marca como duplicada cuando cada uno está en su propio archivo.
Separarlos exigiría incrustar la envoltura, lo que rompe los literales con
nombre de campo en 25 puntos de llamada; juntarlos deja todos los llamadores
intactos y mantiene la lectura.

Ayudantes nuevos en `app/service.go`: `actorDeConRol` (el rol viaja en el
comando, no en la identidad), `rolDecisorPtr` (vacío → `nil`, para que el
dominio levante `ErrRolDecisorObligatorio`; no vacío y desconocido →
`ErrRolDecisorInvalido` desde `app`), y `decisionCmd`/`nuevoDecisionCmd` (la
envoltura de los comandos de decisión, para que cada comando se lea como lo que
es —un parse, un permiso, un tipo de evento y el método del agregado— en vez de
repetir la misma fontanería de ocho líneas).

**Bug R7 corregido (mutante C6).** `IniciarProceso` pasaba a `ejecutarFolio` el
tipo de la etapa (`etapa_avanzada`) mientras el dominio escribía
`folio_en_proceso`. Como `revisarClave` compara el tipo guardado contra el que
recibe el comando, **toda repetición** de `IniciarProceso` caía en
"clave de otro folio". Ahora pasa `TipoEventoFolioEnProceso`.

### 11.2 Pruebas de la Entrega 2 (hecho)

Estructura, para no repetir la mecánica en 14 archivos:

- `app/helpers_comandos_test.go` — `sembrar`: arma un folio **hidratado**
  (`HydrateGarantia`/`HydrateArticulo`, los mismos constructores que usa el
  repositorio real) ya parado en el estado que cada comando necesita. La máquina
  de etapas se prueba en `domain`; caminarla aquí haría fallar la prueba del
  comando por un motivo ajeno a él.
- `app/comandos_compartidos_test.go` — una **tabla única** de los 13 comandos
  mutantes con la que se cubren los casos compartidos: sin usuario (401), sin
  permiso (403, **sin ninguna escritura**), clave de otro folio, clave de otro
  tipo y repetición por clave. La repetición se registra con el tipo que la
  tabla declara, así que un comando que pase otro tipo no puede repetirse: ahí
  muere C6.
- `app/comandos_permisos_test.go` — la **matriz completa 14 × 5**: cada comando
  contra cada uno de los cinco permisos. Afirma las dos direcciones (negar el
  permiso propio lo detiene antes de la transacción; negar cualquier otro no lo
  detiene), que es lo que hace que un comando que pida el permiso equivocado
  falle por cualquiera de los dos lados.
- Un archivo por comando con su camino feliz, el rol faltante/inválido donde
  aplica, el error del dominio propagado y el parseo de los strings.
- `app/ejecutar_folio_test.go` y `app/resolver_duplicada_test.go` — las guardas
  comunes: clave obligatoria/inválida/no canónica, errores de identidad y de
  permiso, folio inexistente, error al leer la clave, fallo al guardar, y las
  cuatro ramas de `resolverDuplicada` (gana el gemelo, clave de otro folio, sin
  dueño, error al releer).
- `app/abrir_garantia_errores_test.go` y `app/abrir_garantia_lecturas_test.go` —
  las ramas de error de `AbrirGarantia`, que no pasa por `ejecutarFolio`.

**Cobertura:** `app` **100.0 %** de sentencias (piso pedido: 95 %).

```
ok  github.com/abdimuy/msp-api/internal/garantias/app  coverage: 100.0% of statements
```

### 11.3 El repositorio `infra/garfb` (R10 y R3)

**R10 — `Guardar` deja de reescribir todos los artículos.** Antes iteraba
`ArticulosForRepo()` e insertaba en silencio cuando el UPDATE afectaba 0 filas.
Ahora:

- `ArticulosNuevosForRepo()` → INSERT;
- `ArticulosModificadosForRepo()` → UPDATE;
- un UPDATE sobre un artículo modificado que afecta 0 filas devuelve
  `domain.ErrArticuloNoEncontrado`, nunca un INSERT silencioso.

**R3 — la ubicación viaja en el evento.** `MSP_GA_EVENTO.UBICACION_DESDE` y
`UBICACION_HASTA` (migración `000065`) no estaban en el INSERT ni en los SELECT,
y `eventoRow`/`scanEvento`/`hydrateEvento` no las leían. Se agregaron a
`insertarEventoSQL`, `listarEventosPorGarantiaSQL` y
`obtenerEventoPorClaveIdempotenciaSQL`, y a la hidratación (`parseUbicacionPtr`).

**Pruebas de integración nuevas** (`guardar_r10_test.go`):

- `TestGarantiaRepo_GuardarNoReescribeArticuloLimpio` (mata C3): se corrompe a
  mano la `ETAPA` del artículo limpio y se comprueba que `Guardar` no la pisa.
- `TestGarantiaRepo_GuardarSoloTocaLoModificado` (mata C4): se borra la fila del
  artículo modificado y `Guardar` debe fallar con `ErrArticuloNoEncontrado`, no
  resucitarla.

**Pruebas internas de mappers** (`mappers_test.go`, `package garfb`): un
`rowScanner` falso conduce `scanGarantia`/`scanArticulo`/`scanEvento` y
`hydrate*` con filas corruptas (UUID inválido, enum inventado, timestamp con
tipo equivocado) para cubrir las ramas de error de hidratación, que la BD no
puede producir por sí sola. Sube `garfb` de 81.1 % a **92.7 %**.

### 11.4 Las tres pruebas de punta a punta del §8 (hecho)

`e2e_test.go` (`package garfb_test`) monta el `app.Service` **real** contra
Firebird **real**, sin dobles, dentro de `WithTestTransaction` (rollback). El
`TxRunner` envuelve `firebird.RunInTx`, que es re-entrante: detecta la
transacción del test ya inyectada en el contexto y ejecuta dentro de ella.

- `TestE2E_PisoDeLaRecepcionAlCierre`
- `TestE2E_ClientePorProveedor`
- `TestE2E_ClientePorTallerConCambioFisico`

**Línea de tiempo literal, leída de la base** (columnas:
`evento | etapa desde→hasta | ubicación desde→hasta | created_at`):

Historia 1 — piso, de la recepción al cierre:

```
 1 | folio_abierto              | -                      -> -                      | -                    -> -                    | 2026-10-20T15:02:00Z
 2 | articulo_agregado          | -                      -> -                      | -                    -> -                    | 2026-10-20T15:04:00Z
 3 | folio_en_proceso           | -                      -> -                      | -                    -> -                    | 2026-10-20T15:06:00Z
 4 | etapa_avanzada             | registrado             -> en_revision            | -                    -> -                    | 2026-10-20T15:08:00Z
 5 | diagnostico_registrado     | en_revision            -> en_taller              | almacen_revision     -> taller               | 2026-10-20T15:10:00Z
 6 | etapa_avanzada             | en_taller              -> reparado_taller        | -                    -> -                    | 2026-10-20T15:12:00Z
 7 | etapa_avanzada             | reparado_taller        -> listo_entrega          | -                    -> -                    | 2026-10-20T15:14:00Z
 8 | folio_listo_entrega        | -                      -> -                      | -                    -> -                    | 2026-10-20T15:16:00Z
 9 | etapa_avanzada             | listo_entrega          -> reingresado_inventario | taller               -> almacen_revision     | 2026-10-20T15:18:00Z
```

Historia 2 — cliente por proveedor:

```
 1 | folio_abierto              | -                      -> -                      | -                    -> -                    | 2026-10-20T16:02:00Z
 2 | articulo_agregado          | -                      -> -                      | -                    -> -                    | 2026-10-20T16:04:00Z
 3 | folio_en_proceso           | -                      -> -                      | -                    -> -                    | 2026-10-20T16:06:00Z
 4 | etapa_avanzada             | registrado             -> pendiente_recoleccion  | -                    -> -                    | 2026-10-20T16:08:00Z
 5 | etapa_avanzada             | pendiente_recoleccion  -> recolectado            | domicilio_cliente    -> en_transito          | 2026-10-20T16:10:00Z
 6 | etapa_avanzada             | recolectado            -> en_revision            | en_transito          -> almacen_revision     | 2026-10-20T16:12:00Z
 7 | diagnostico_registrado     | en_revision            -> orden_generada         | -                    -> -                    | 2026-10-20T16:14:00Z
 8 | etapa_avanzada             | orden_generada         -> enviado_proveedor      | almacen_revision     -> proveedor            | 2026-10-20T16:16:00Z
 9 | etapa_avanzada             | enviado_proveedor      -> dictamen_recibido      | -                    -> -                    | 2026-10-20T16:18:00Z
10 | dictamen_registrado        | dictamen_recibido      -> reparado_proveedor     | -                    -> -                    | 2026-10-20T16:20:00Z
11 | articulo_movido            | -                      -> -                      | proveedor            -> almacen_revision     | 2026-10-20T16:22:00Z
12 | etapa_avanzada             | reparado_proveedor     -> listo_entrega          | -                    -> -                    | 2026-10-20T16:24:00Z
13 | folio_listo_entrega        | -                      -> -                      | -                    -> -                    | 2026-10-20T16:26:00Z
14 | etapa_avanzada             | listo_entrega          -> entregado              | almacen_revision     -> entregado            | 2026-10-20T16:28:00Z
15 | folio_entregado            | -                      -> -                      | -                    -> -                    | 2026-10-20T16:28:00Z
16 | folio_cerrado              | -                      -> -                      | -                    -> -                    | 2026-10-20T16:30:00Z
```

Historia 3 — cliente por taller con cambio físico:

```
 1 | folio_abierto              | -                      -> -                      | -                    -> -                    | 2026-10-20T17:02:00Z
 2 | articulo_agregado          | -                      -> -                      | -                    -> -                    | 2026-10-20T17:04:00Z
 3 | folio_en_proceso           | -                      -> -                      | -                    -> -                    | 2026-10-20T17:06:00Z
 4 | etapa_avanzada             | registrado             -> pendiente_recoleccion  | -                    -> -                    | 2026-10-20T17:08:00Z
 5 | etapa_avanzada             | pendiente_recoleccion  -> recolectado            | domicilio_cliente    -> en_transito          | 2026-10-20T17:10:00Z
 6 | etapa_avanzada             | recolectado            -> en_revision            | en_transito          -> almacen_revision     | 2026-10-20T17:12:00Z
 7 | diagnostico_registrado     | en_revision            -> en_taller              | almacen_revision     -> taller               | 2026-10-20T17:14:00Z
 8 | cambio_autorizado          | en_taller              -> standby                | -                    -> -                    | 2026-10-20T17:16:00Z
 9 | folio_listo_entrega        | -                      -> -                      | -                    -> -                    | 2026-10-20T17:18:00Z
10 | etapa_avanzada             | listo_entrega          -> entregado              | almacen_revision     -> entregado            | 2026-10-20T17:20:00Z
11 | folio_entregado            | -                      -> -                      | -                    -> -                    | 2026-10-20T17:20:00Z
12 | folio_cerrado              | -                      -> -                      | -                    -> -                    | 2026-10-20T17:22:00Z
13 | desenlace_registrado       | standby                -> segunda_mano           | taller               -> almacen_segunda_mano | 2026-10-20T17:24:00Z
```

Lo que cuentan las tres, y que es justo lo que el brief pedía ver:

- El evento de ubicación **aparece sólo cuando la ubicación cambia** (R3): el
  avance a `en_revision` de un folio de piso no la mueve y el par va vacío;
  `diagnostico_registrado`, `enviado_proveedor`, `articulo_movido`, `entregado`
  y `desenlace_registrado` sí lo traen.
- Un folio de piso **nunca** pasa por recolección (historia 1): va
  `registrado → en_revision` directo.
- Un folio de cliente **sí** pasa por recolección (historias 2 y 3), y el
  `entregado` sólo existe en cliente.
- El cierre (historia 3) va **antes** del `desenlace_registrado` del original:
  el folio se cerró con el mueble todavía en `standby`, y el desenlace posterior
  no reabre el folio.
- Toda etapa avanzada lleva su `desde`/`hasta`; ningún evento queda sin su clave
  de idempotencia.

### 11.5 Mutantes C1–C6 (hecho)

Cada mutante se aplicó al código, se corrió la suite, se confirmó el fallo y se
revirtió. Todos murieron.

| # | Mutante | Qué rompe | Test que lo mata | Resultado |
|---|---|---|---|---|
| C1 | `AutorizarCambioFisico` pide `PermisoActualizar` en vez de `PermisoAutorizar` | un cambio físico —la decisión que gasta dinero— con el permiso barato | `TestComandos_SinPermiso`, `TestComandos_TablaDePermisos` | **muerto** |
| C2 | `Entregar` no exige GPS del agente | se entrega sin ubicación y la transacción se abre igual | `TestEntregar_SinGps` (`no debe abrir transaccion: tx=1`) | **muerto** |
| C3 | `Guardar` actualiza todos los artículos como antes | reescribe artículos limpios; pierde cambios ajenos | `TestGarantiaRepo_GuardarNoReescribeArticuloLimpio` | **muerto** |
| C4 | `Guardar` inserta en silencio cuando el UPDATE afecta 0 filas | resucita un artículo borrado por debajo del agregado | `TestGarantiaRepo_GuardarSoloTocaLoModificado` | **muerto** |
| C5 | `insertarEvento` no escribe `UBICACION_HASTA` | la ubicación no queda en la línea de tiempo (el defecto central del §1) | las tres `TestE2E_*` | **muerto** |
| C6 | `IniciarProceso` pasa `etapa_avanzada` en vez de `folio_en_proceso` | el evento de arranque no se distingue; el replay por clave falla | `TestComandos_RepetidaPorClave/IniciarProceso` | **muerto** |

Salida representativa de un mutante vivo antes de morir (C1):

```
--- FAIL: TestComandos_SinPermiso (0.00s)
    --- FAIL: TestComandos_SinPermiso/AutorizarCambioFisico (0.00s)
--- FAIL: TestComandos_TablaDePermisos (0.00s)
    --- FAIL: TestComandos_TablaDePermisos/AutorizarCambioFisico/actualizar (0.00s)
        comandos_permisos_test.go:89: pide garantias:autorizar y no actualizar: quedó bloqueado al negarle actualizar
    --- FAIL: TestComandos_TablaDePermisos/AutorizarCambioFisico/autorizar (0.00s)
        comandos_permisos_test.go:80: al negarle autorizar debe rechazar con ErrPermisoDenegado, got warranty_stage_transition_forbidden: transición de etapa no permitida
        comandos_permisos_test.go:83: sin permiso no escribe: tx=1 saved=0 created=0
FAIL
```

### 11.6 Verificación literal (Entrega 2)

```sh
$ gofmt -l internal/garantias
# (vacío)

$ go vet ./internal/garantias/...
# (vacío)

$ go build ./...
# (vacío)

$ golangci-lint run ./internal/garantias/...
0 issues.
```

`go clean -testcache && go test -race -count=1 -p 1 -coverprofile=cov.out ./internal/garantias/...`
(con `FB_*` exportadas):

```
ok  github.com/abdimuy/msp-api/internal/garantias                    2.107s  coverage: 100.0% of statements
ok  github.com/abdimuy/msp-api/internal/garantias/app                2.413s  coverage: 100.0% of statements
ok  github.com/abdimuy/msp-api/internal/garantias/domain             2.570s  coverage: 100.0% of statements
ok  github.com/abdimuy/msp-api/internal/garantias/infra/garfb       10.639s  coverage:  92.7% of statements
ok  github.com/abdimuy/msp-api/internal/garantias/infra/storage      2.493s  coverage:  83.8% of statements
ok  github.com/abdimuy/msp-api/internal/garantias/ports/outbound     2.363s  coverage: 100.0% of statements
```

Pisos del brief: `app` ≥ 95 % → **100.0 %**; `garfb` ≥ 85 % → **92.7 %**;
`domain` 100 % → **100.0 %**. Cumplidos.

Las 12 sentencias sin cubrir de `garfb` son las ramas de error que sólo el driver
puede producir (`firebird.MapError` tras un fallo de I/O y los `RowsAffected`
que devuelven error); no se alcanzan sin inyectar fallos en el driver.

Comprobación de módulo sellado (ADR-0009), equivalente a
`make check-sealed MODULE=garantias`:

```sh
$ go list -deps ./internal/garantias/... | grep 'msp-api/internal/' \
    | grep -v 'msp-api/internal/garantias' | grep -v 'msp-api/internal/platform'
# (vacío): garantías importa sólo a sí mismo y a platform
```

Control de encoding (`grep -rn 'Ã' internal/garantias`): vacío.

### 11.7 Migración `000065`: el up y el down, con la salida literal

Ciclo completo corrido sobre la base de desarrollo
(`mueblera-firebird`, `/firebird/data/MUEBLERA.FDB`) tras el cierre de la
Entrega 2. `make fb-migrate-down` y `make fb-migrate-up` no se pueden invocar
desde el shell de este entorno (PowerShell no tiene `make`), así que se ejecutó
el mismo `docker exec … isql` que el Makefile usa dentro de esos targets, con el
mismo contenedor, las mismas credenciales y el mismo `-ch UTF8`.

#### 0. Estado de partida (la 000065 ya aplicada)

```
$ docker exec -i mueblera-firebird /usr/local/firebird/bin/isql -u SYSDBA -p masterkey -ch UTF8 -q /firebird/data/MUEBLERA.FDB
  <<< 'SELECT ID, NAME FROM MSP_MIGRATIONS WHERE ID = 65;'

          ID NAME
============ ========================================================================================================================
          65 000065_add_ubicacion_a_msp_ga_evento
```

#### 1. `make fb-migrate-down N=000065`

```
$ docker exec -i mueblera-firebird /usr/local/firebird/bin/isql -u SYSDBA -p masterkey -ch UTF8 /firebird/data/MUEBLERA.FDB \
      < migrations-firebird/000065_add_ubicacion_a_msp_ga_evento.down.sql
exit=0
```

Tras el down, la fila de `MSP_MIGRATIONS` desaparece y las columnas también:

```
--- MSP_MIGRATIONS (id 65) tras el down ---
(vacío)

--- columnas UBICACION* tras el down (vacío = reversibles) ---
(vacío)
```

#### 2. `make fb-migrate-up`

```
$ docker exec -i mueblera-firebird /usr/local/firebird/bin/isql -u SYSDBA -p masterkey -ch UTF8 /firebird/data/MUEBLERA.FDB \
      < migrations-firebird/000065_add_ubicacion_a_msp_ga_evento.up.sql
exit=0
```

Tras el up, la fila vuelve y las columnas existen con la forma que pide el
brief:

```
--- MSP_MIGRATIONS (id 65) tras el up ---

          ID NAME
============ ========================================================================================================================
          65 000065_add_ubicacion_a_msp_ga_evento

--- columnas UBICACION* tras el up ---
COLUMNA                                                         NULABLE  CARACTERES CHARSET     POS
=============================================================== ======== ========== =========== =====
UBICACION_DESDE                                                 NULL             28 ASCII         14
UBICACION_HASTA                                                 NULL             28 ASCII         15
```

`VARCHAR(28) CHARACTER SET ASCII`, nulas y sin `DEFAULT`, en las posiciones 14
y 15 de `MSP_GA_EVENTO`. El ciclo es idempotente: down borra, up recrea, y
`MSP_MIGRATIONS` queda consistente en los dos sentidos.

#### Y además, la prueba por la vía de los datos

Que las columnas existen y se usan no lo dice sólo el catálogo: las tres pruebas
de punta a punta **escriben y leen** `UBICACION_DESDE` y `UBICACION_HASTA`
contra Firebird real (§11.4), y sin ellas el `INSERT` falla con
`Count of read-write columns does not equal count of values` — exactamente el
error que apareció al dejar el `INSERT` mutado en C5.

La columna de R6 **no** se agrega: "pasó por reparado" se deriva de `RUTA` +
`DICTAMEN` en el propio agregado (§3), que es una de las dos salidas que el
brief permitía (líneas 133–135).

---

## 12. Estado del árbol de trabajo

La tarea son **dos entregas, una rama** (`feat/garantias-dominio-v2`) y por tanto
**un PR** con dos commits. La Entrega 1 quedó en `a564973` (ya empujado). La
Entrega 2 va en el commit inmediatamente siguiente de esa misma rama. Cada
commit incluye **sólo** lo del entregable.

**Commit de la Entrega 1** (`a564973`, 38 archivos, +4445/−75):

- Modificados: `domain/{articulo,errors,estado_folio,etapa,evento,garantia,tipo_evento,transiciones,ubicacion}.go`,
  y sus `_test.go`; `garantias_contracts.go` + `_test.go`;
  `docs/module-standards/ENCODING_HANDLING.md`.
- Nuevos: `domain/{texto.go,avanzar_origen_test.go,cancelar_fuera_test.go,catalogos_test.go,desenlace_automatico_test.go,dirty_tracking_test.go,entregar_test.go,folio_eventos_test.go,folio_operacion_test.go,marcar_no_devuelto_test.go,r12_pendientes_test.go,ubicacion_movimiento_test.go}`.
- Nuevos (del PR #26): `app/{service.go,abrir_garantia.go,abrir_garantia_test.go,agregar_articulo.go,avanzar_articulo.go,iniciar_proceso.go,fakes_test.go,fakes_repo_test.go}`.
- Nuevos: `migrations-firebird/000065_add_ubicacion_a_msp_ga_evento.{up,down}.sql`.

**Commit de la Entrega 2** (este):

- Nuevos en `app/`: los 10 comandos (`registrar_diagnostico.go` —que también
  contiene `RegistrarDesenlace`—, `registrar_dictamen.go`,
  `autorizar_cambio_fisico.go`, `marcar_no_devuelto.go`, `mover_articulo.go`,
  `marcar_listo_entrega.go`, `entregar.go`, `cerrar.go`, `cancelar.go`) y sus
  `_test.go`.
- Nuevos en `app/` (pruebas compartidas): `comandos_compartidos_test.go`,
  `comandos_permisos_test.go`, `ejecutar_folio_test.go`,
  `helpers_comandos_test.go`, y el resto de `_test.go` por comando.
- Modificados: `app/service.go` (`actorDeConRol`, `rolDecisorPtr`,
  `decisionCmd`/`nuevoDecisionCmd`), `app/iniciar_proceso.go`
  (`TipoEventoFolioEnProceso`).
- Modificados en `domain/`: `evento.go` (`claveDerivada`/`espacioDeEventos`) y
  `garantia.go` (claves derivadas por discriminador en `Entregar`).
- Nuevos en `infra/garfb/`: `mappers_test.go`, `e2e_test.go`,
  `guardar_r10_test.go`.
- Modificados en `infra/garfb/`: `garantia_repo.go` (R10 en `Guardar`;
  `insertarEvento` con ubicaciones), `queries.go` (`UBICACION_DESDE`/`UBICACION_HASTA`
  en el INSERT y en los dos SELECT de eventos), `mappers.go`
  (`eventoRow`/`scanEvento`/`parseUbicacionPtr`/`hydrateEvento`),
  `guardar_test.go`.
- Modificados/actualizados: `app/abrir_garantia_test.go`,
  `app/fakes_repo_test.go`.

Archivos preexistentes fuera de esta tarea y **no tocados** por mí:
`Dockerfile.dev`, `internal/auth/infra/firebird/pagination.go`,
`internal/cobranza/infra/ventfb/fb_event_source.go` (modificados), y
`AGENTS.md`, `t.txt`, `tmp_fix.py`, `tmp_gps.py` (sin seguimiento). Todos
quedaron **fuera** de ambos commits y siguen en el árbol de trabajo.
