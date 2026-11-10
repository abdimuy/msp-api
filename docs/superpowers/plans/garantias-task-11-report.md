# Tarea 11 — Reporte (Parte 1: `msp-api`)

> **Alcance.** Este reporte cubre sólo la **Parte 1** del brief
> (`docs/superpowers/plans/garantias-task-11-brief.md`): las pruebas que
> faltaban de la tarea 9 (P1), la decisión medida sobre la huella de
> `entregar` (P2) y la corrección del reporte de la tarea 9 (P3), todo en
> `msp-api`. La **Parte 2** (`msp-app-kt`, módulo `:feature:garantias`) va en
> su propio repositorio, su propio PR y su propio reporte
> (`docs/garantias-captura-report.md`).
>
> **Fecha del reporte:** 10 de noviembre de 2026. **Rama:**
> `feat/garantias-t11-pendientes`, creada desde `origin/main` (`6e0e44f`, el
> merge del PR #33).

## Niveles de evidencia

- **MEDIDO**: se ejecutó el comando que se cita, con fecha y lugar, y se
  transcribe su salida.
- **LEÍDO**: se leyó en el archivo y la línea que se citan.
- **INFERIDO**: no se midió; se dice qué lo volvería MEDIDO.

## Qué se hizo

1. **P1 — pruebas que matan los mutantes que sobrevivían.** Se agregaron o
   extendieron pruebas en `domain`, `app` y `garhttp` para los once pendientes
   del brief (T1–T11, con T4 partido en trece: uno por cada método del folio).
   Cada una se verificó contra su mutante: parche aplicado, prueba en rojo,
   parche revertido, prueba en verde. Son **23 pares rojo/verde** (T1–T3, T4a–T4m,
   T5–T11).
2. **P2 — la huella de `entregar`.** Se midieron los dos casos del brief con el
   router real y `httptest`; ambos devolvían 409 antes del cambio. Se decidió
   normalizar la huella (opción A): la hora del dispositivo a UTC y el
   `Content-Type` de la firma detectado por sus bytes. La decisión y sus dos
   mediciones están abajo, y una prueba la sostiene.
3. **P3 — el reporte de la tarea 9.** Se corrigió **en su lugar**
   (`garantias-task-9-report.md`), con nota de cambio al principio, niveles de
   evidencia, los quince mutantes K1–K15 con parche y salidas, los `git grep`
   de cada prueba citada en `7833cf6`, el cambio declarado de
   `garfb/guardar_test.go` y la línea de tiempo de los tres e2e leída de la
   base. No se reescribió ni se reemplazó por otro documento.
4. **La compuerta de mutación, en verde.** `make test-mutation-garantias-app`
   estaba en rojo ya en `main`, por un defecto de gremlins v0.6.0 con los
   `switch`/`case`, no por pruebas faltantes. Se pasaron tres `switch` a
   `if`/`else if` (mismo comportamiento) y la compuerta quedó verde; el
   defecto, la medición de antes y la de después están en «La compuerta de
   mutación».

## T1–T11: la prueba que mata cada mutante

**MEDIDO** el 10 de noviembre de 2026 en el árbol de la rama (los parches y las
salidas están abajo). La columna «Prueba» es la que se pone roja al aplicar el
mutante.

| # | Mutante | Prueba que lo mata | Archivo de la prueba |
|---|---|---|---|
| T1 | `Dictamen()` devuelve el puntero interno | `TestArticulo_Dictamen_DevuelveCopia` | `internal/garantias/domain/articulo_test.go:142` |
| T2 | `Ruta()` devuelve el puntero interno | `TestArticulo_Ruta_DevuelveCopia` | `internal/garantias/domain/articulo_test.go:124` |
| T3 | `Desenlace()` devuelve el puntero interno | `TestArticulo_Desenlace_DevuelveCopia` | `internal/garantias/domain/articulo_test.go:160` |
| T4a | `AgregarArticulo` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_AgregarArticulo` | `internal/garantias/domain/folio_updated_at_test.go:27` |
| T4b | `AvanzarArticulo` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_AvanzarArticulo` | `internal/garantias/domain/folio_updated_at_test.go:38` |
| T4c | `RegistrarDiagnostico` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_RegistrarDiagnostico` | `internal/garantias/domain/folio_updated_at_test.go:51` |
| T4d | `RegistrarDictamen` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_RegistrarDictamen` | `internal/garantias/domain/folio_updated_at_test.go:62` |
| T4e | `AutorizarCambioFisico` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_AutorizarCambioFisico` | `internal/garantias/domain/folio_updated_at_test.go:73` |
| T4f | `RegistrarDesenlace` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_RegistrarDesenlace` | `internal/garantias/domain/folio_updated_at_test.go:84` |
| T4g | `MarcarNoDevuelto` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_MarcarNoDevuelto` | `internal/garantias/domain/folio_updated_at_test.go:95` |
| T4h | `MoverArticulo` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_MoverArticulo` | `internal/garantias/domain/folio_updated_at_test.go:106` |
| T4i | `IniciarProceso` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_IniciarProceso` | `internal/garantias/domain/folio_updated_at_test.go:117` |
| T4j | `MarcarListoEntrega` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_MarcarListoEntrega` | `internal/garantias/domain/folio_updated_at_test.go:129` |
| T4k | `Entregar` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_Entregar` | `internal/garantias/domain/folio_updated_at_test.go:143` |
| T4l | `Cerrar` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_Cerrar` | `internal/garantias/domain/folio_updated_at_test.go:154` |
| T4m | `Cancelar` no llama `MarkUpdatedAt` | `TestFolioUpdatedAt_Cancelar` | `internal/garantias/domain/folio_updated_at_test.go:166` |
| T5 | `marcarNoDevuelto` marca modificado pero no mueve `UpdatedAt` | `TestMarcarNoDevuelto_MueveUpdatedAtDelArticulo` | `internal/garantias/domain/dirty_tracking_test.go:253` |
| T6 | `moverA` marca modificado pero no mueve `UpdatedAt` | `TestMoverArticulo_MueveUpdatedAtDelArticulo` | `internal/garantias/domain/dirty_tracking_test.go:266` |
| T7 | el actor sale de `g.AbiertoPor()` y no de Identity | `TestComandos_UsuarioSaleDeIdentity` | `internal/garantias/app/comandos_compartidos_test.go:578` |
| T8 | `resolverDuplicada` compara sólo el folio, no la huella | `TestResolverDuplicada_CarreraConClaveDeOtroComando`, `…_ClaveMismoComandoOtroArticulo`, `…_ClaveMismoComandoOtraEtapa` | `internal/garantias/app/resolver_duplicada_test.go:145,185,227` |
| T9 | `puntosDeAuditoria` sin el filtro `lat == nil \|\| lon == nil` | `TestCerrar_RecoleccionSinGPSNoParticipa` | `internal/garantias/app/cerrar_test.go:225` |
| T10 | `firmaIDDe` con clave inválida devuelve un UUID en vez de `uuid.Nil` | `TestFirmaIDDe_ClaveInvalidaEsNil` | `internal/garantias/app/entregar_internal_test.go:16` |
| T11 | se revierte la decisión de P2 (huella cruda en vez de normalizada) | `TestEntregar_ReintentoMismoContenidoOtroCliente_200` | `internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go:280` |

Cuatro notas sobre las filas donde una prueba mal hecha deja vivo al mutante:

- **T4 (trece métodos).** Cada prueba avanza el reloj a una hora **distinta**
  antes del paso que prueba y compara `g.UpdatedAt()` con esa hora. Una hora
  repetida entre pasos no distingue «no movió el reloj» de «movió al mismo
  valor», y el mutante sobreviviría. El mutante de cada fila aplica al método
  de `garantia.go` que corresponde.
- **T7 (Identity).** El nombre del usuario de Identity es
  `"Itzel Domínguez Ochoa"`, que no aparece en ningún otro fake ni en los
  eventos sembrados (esos siguen con `"Juan"`). Si el actor saliera de
  `g.AbiertoPor()`, la prueba lo vería. Está extendida a los **trece comandos
  compartidos** más `AbrirGarantia`.
- **T8 (carreras).** Portadas del commit `0c102ad4` del PR #26: fuerzan el
  camino de `resolverDuplicada` después del rollback por clave duplicada, con
  una huella distinta, y esperan 409 `warranty_idempotency_key_mismatch`. Se
  ponen rojas si `resolverDuplicada` compara sólo el folio.
- **T10 (dos pruebas, una sola mata).** `firmaIDDe` se prueba en el paquete
  interno (la función no se exporta) porque el mutante se mata ahí. La segunda
  prueba, `TestEntregar_ClaveInvalidaDescartaElBlob`, recorre la consecuencia
  completa desde `app` —el blob se escribe, la clave inválida falla y el archivo
  queda borrado—, pero **no** mata este mutante: se midió y con `uuid.New()` en
  lugar de `uuid.Nil` sigue en verde, porque el blob se descarta igual. Por eso
  la fila cita sólo la primera.

## Evidencia de los mutantes T1-T11

Cada sección trae el parche `diff` exacto que se aplicó, la salida **roja** de
la prueba que lo mata y la salida **verde** al revertirlo. **MEDIDO** el 10 de
noviembre de 2026 sobre el árbol de la rama (Windows, `go1.26.3`), con
`go test -count=1` en cada corrida (sin caché de resultados). Los parches se
capturaron con `git diff` y se revirtieron restaurando el archivo original; el
arnés no toca ninguna prueba. Las pruebas citadas usan dobles de prueba, no la
base ni la red, así que el mismo comando se reproduce en cualquier sistema.

**Nota de sanitización.** En las trazas de `testify` y de los `panic` el prefijo
absoluto del árbol se reemplazó por `<repo>/`; es lo único que se cambió: se
quitó la ruta del disco y el resto de cada línea es literal.

### T1 — `Dictamen()` devuelve el puntero interno

Prueba que lo mata: `TestArticulo_Dictamen_DevuelveCopia` (`internal/garantias/domain/articulo_test.go:142`).

Parche:

```diff
diff --git a/internal/garantias/domain/articulo.go b/internal/garantias/domain/articulo.go
index a1fca82..0cd7621 100644
--- a/internal/garantias/domain/articulo.go
+++ b/internal/garantias/domain/articulo.go
@@ -436,8 +436,7 @@ func (a *Articulo) Dictamen() *Dictamen {
 	if a.dictamen == nil {
 		return nil
 	}
-	d := *a.dictamen
-	return &d
+	return a.dictamen
 }
 
 // Desenlace returns the parallel-flow outcome after standby.
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestArticulo_Dictamen_DevuelveCopia' -count=1`:

```text
--- FAIL: TestArticulo_Dictamen_DevuelveCopia (0.00s)
    articulo_test.go:154: Dictamen() tras mutar el valor devuelto = rechazada, want aceptada
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.723s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.866s
```

### T2 — `Ruta()` devuelve el puntero interno

Prueba que lo mata: `TestArticulo_Ruta_DevuelveCopia` (`internal/garantias/domain/articulo_test.go:124`).

Parche:

```diff
diff --git a/internal/garantias/domain/articulo.go b/internal/garantias/domain/articulo.go
index a1fca82..82a59e7 100644
--- a/internal/garantias/domain/articulo.go
+++ b/internal/garantias/domain/articulo.go
@@ -421,8 +421,7 @@ func (a *Articulo) Ruta() *RutaReparacion {
 	if a.ruta == nil {
 		return nil
 	}
-	r := *a.ruta
-	return &r
+	return a.ruta
 }
 
 // Etapa returns the article's position in the stage machine.
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestArticulo_Ruta_DevuelveCopia' -count=1`:

```text
--- FAIL: TestArticulo_Ruta_DevuelveCopia (0.00s)
    articulo_test.go:136: Ruta() tras mutar el valor devuelto = proveedor, want taller
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.737s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.819s
```

### T3 — `Desenlace()` devuelve el puntero interno

Prueba que lo mata: `TestArticulo_Desenlace_DevuelveCopia` (`internal/garantias/domain/articulo_test.go:160`).

Parche:

```diff
diff --git a/internal/garantias/domain/articulo.go b/internal/garantias/domain/articulo.go
index a1fca82..b0847c4 100644
--- a/internal/garantias/domain/articulo.go
+++ b/internal/garantias/domain/articulo.go
@@ -445,8 +445,7 @@ func (a *Articulo) Desenlace() *Desenlace {
 	if a.desenlace == nil {
 		return nil
 	}
-	d := *a.desenlace
-	return &d
+	return a.desenlace
 }
 
 // CerradoEn returns when the article was dropped from the folio, if it was.
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestArticulo_Desenlace_DevuelveCopia' -count=1`:

```text
--- FAIL: TestArticulo_Desenlace_DevuelveCopia (0.00s)
    articulo_test.go:172: Desenlace() tras mutar el valor devuelto = devuelto, want merma
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.713s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.714s
```

### T4a — `AgregarArticulo` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_AgregarArticulo` (`internal/garantias/domain/folio_updated_at_test.go:27`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..4ab53ae 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -345,7 +345,6 @@ func (g *Garantia) AgregarArticulo(p AgregarArticuloParams, actor ActorParams, n
 	}
 	g.articulos = append(g.articulos, articulo)
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_AgregarArticulo' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_AgregarArticulo (0.00s)
    folio_updated_at_test.go:34: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.929s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.784s
```

### T4b — `AvanzarArticulo` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_AvanzarArticulo` (`internal/garantias/domain/folio_updated_at_test.go:38`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..957eeea 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -373,7 +373,6 @@ func (g *Garantia) AvanzarArticulo(articuloID uuid.UUID, hasta Etapa, actor Acto
 		return err
 	}
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_AvanzarArticulo' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_AvanzarArticulo (0.00s)
    folio_updated_at_test.go:47: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.793s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.714s
```

### T4c — `RegistrarDiagnostico` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_RegistrarDiagnostico` (`internal/garantias/domain/folio_updated_at_test.go:51`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..7653ad4 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -404,7 +404,6 @@ func (g *Garantia) RegistrarDiagnostico(articuloID uuid.UUID, ruta RutaReparacio
 		return err
 	}
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_RegistrarDiagnostico' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_RegistrarDiagnostico (0.00s)
    folio_updated_at_test.go:58: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.190s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.725s
```

### T4d — `RegistrarDictamen` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_RegistrarDictamen` (`internal/garantias/domain/folio_updated_at_test.go:62`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..28e5ea5 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -433,7 +433,6 @@ func (g *Garantia) RegistrarDictamen(articuloID uuid.UUID, dictamen Dictamen, ac
 		return err
 	}
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_RegistrarDictamen' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_RegistrarDictamen (0.00s)
    folio_updated_at_test.go:69: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.729s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.725s
```

### T4e — `AutorizarCambioFisico` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_AutorizarCambioFisico` (`internal/garantias/domain/folio_updated_at_test.go:73`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..9a4635e 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -504,7 +504,6 @@ func (g *Garantia) AutorizarCambioFisico(articuloID uuid.UUID, actor ActorParams
 	g.articulos = append(g.articulos, reemplazo)
 	g.eventosPendientes = append(g.eventosPendientes, eventos...)
 	g.eventosPendientes = append(g.eventosPendientes, eReemplazo)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_AutorizarCambioFisico' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_AutorizarCambioFisico (0.00s)
    folio_updated_at_test.go:80: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.682s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.206s
```

### T4f — `RegistrarDesenlace` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_RegistrarDesenlace` (`internal/garantias/domain/folio_updated_at_test.go:84`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..a93b112 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -535,7 +535,6 @@ func (g *Garantia) RegistrarDesenlace(articuloID uuid.UUID, desenlace Desenlace,
 		return err
 	}
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_RegistrarDesenlace' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_RegistrarDesenlace (0.00s)
    folio_updated_at_test.go:91: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.739s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.795s
```

### T4g — `MarcarNoDevuelto` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_MarcarNoDevuelto` (`internal/garantias/domain/folio_updated_at_test.go:95`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..60063a4 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -564,7 +564,6 @@ func (g *Garantia) MarcarNoDevuelto(articuloID uuid.UUID, actor ActorParams, now
 		return err
 	}
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_MarcarNoDevuelto' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_MarcarNoDevuelto (0.00s)
    folio_updated_at_test.go:102: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.809s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.775s
```

### T4h — `MoverArticulo` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_MoverArticulo` (`internal/garantias/domain/folio_updated_at_test.go:106`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..c69e8e5 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -590,7 +590,6 @@ func (g *Garantia) MoverArticulo(articuloID uuid.UUID, hasta Ubicacion, actor Ac
 		return err
 	}
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_MoverArticulo' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_MoverArticulo (0.00s)
    folio_updated_at_test.go:113: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.724s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.771s
```

### T4i — `IniciarProceso` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_IniciarProceso` (`internal/garantias/domain/folio_updated_at_test.go:117`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..f4a166b 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -610,7 +610,6 @@ func (g *Garantia) IniciarProceso(actor ActorParams, now time.Time) error {
 	}
 	g.estado = EstadoFolioEnProceso
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_IniciarProceso' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_IniciarProceso (0.00s)
    folio_updated_at_test.go:125: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.745s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.714s
```

### T4j — `MarcarListoEntrega` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_MarcarListoEntrega` (`internal/garantias/domain/folio_updated_at_test.go:129`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..e726efa 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -634,7 +634,6 @@ func (g *Garantia) MarcarListoEntrega(actor ActorParams, now time.Time) error {
 	}
 	g.estado = EstadoFolioListoEntrega
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_MarcarListoEntrega' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_MarcarListoEntrega (0.00s)
    folio_updated_at_test.go:139: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.706s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.734s
```

### T4k — `Entregar` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_Entregar` (`internal/garantias/domain/folio_updated_at_test.go:143`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..f7a5d79 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -751,7 +751,6 @@ func (g *Garantia) Entregar(firma string, actor ActorParams, now time.Time) erro
 	g.estado = EstadoFolioEntregado
 	g.eventosPendientes = append(g.eventosPendientes, e)
 	g.imagenesPendientes = append(g.imagenesPendientes, imagen)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_Entregar' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_Entregar (0.00s)
    folio_updated_at_test.go:150: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.689s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.699s
```

### T4l — `Cerrar` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_Cerrar` (`internal/garantias/domain/folio_updated_at_test.go:154`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..2946701 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -775,7 +775,6 @@ func (g *Garantia) Cerrar(entrega *PuntoGPS, recolecciones []PuntoGPS, justifica
 	g.cerradoEn = &cerrado
 	g.estado = EstadoFolioCerrado
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_Cerrar' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_Cerrar (0.00s)
    folio_updated_at_test.go:162: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.776s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.788s
```

### T4m — `Cancelar` no llama `MarkUpdatedAt`

Prueba que lo mata: `TestFolioUpdatedAt_Cancelar` (`internal/garantias/domain/folio_updated_at_test.go:166`).

Parche:

```diff
diff --git a/internal/garantias/domain/garantia.go b/internal/garantias/domain/garantia.go
index 6a1d866..81f3945 100644
--- a/internal/garantias/domain/garantia.go
+++ b/internal/garantias/domain/garantia.go
@@ -800,7 +800,6 @@ func (g *Garantia) Cancelar(motivo string, actor ActorParams, now time.Time) err
 	}
 	g.estado = EstadoFolioCancelado
 	g.eventosPendientes = append(g.eventosPendientes, e)
-	g.audit.MarkUpdatedAt(now)
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestFolioUpdatedAt_Cancelar' -count=1`:

```text
--- FAIL: TestFolioUpdatedAt_Cancelar (0.00s)
    folio_updated_at_test.go:173: UpdatedAt() = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 11:00:00 +0000 UTC (el mutador no llamó MarkUpdatedAt)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.808s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.829s
```

### T5 — `marcarNoDevuelto` marca modificado pero no mueve `UpdatedAt`

Prueba que lo mata: `TestMarcarNoDevuelto_MueveUpdatedAtDelArticulo` (`internal/garantias/domain/dirty_tracking_test.go:253`).

Parche:

```diff
diff --git a/internal/garantias/domain/articulo.go b/internal/garantias/domain/articulo.go
index a1fca82..3baa429 100644
--- a/internal/garantias/domain/articulo.go
+++ b/internal/garantias/domain/articulo.go
@@ -339,7 +339,7 @@ func (a *Articulo) marcarNoDevuelto(now time.Time) error {
 		return ErrTransicionEtapaNoPermitida
 	}
 	a.etapa = EtapaStandby
-	a.tocar(now)
+	a.modificado = true
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestMarcarNoDevuelto_MueveUpdatedAtDelArticulo' -count=1`:

```text
--- FAIL: TestMarcarNoDevuelto_MueveUpdatedAtDelArticulo (0.00s)
    dirty_tracking_test.go:261: UpdatedAt() del artículo = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 12:00:00 +0000 UTC (marcó modificado pero no movió la hora)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.781s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.717s
```

### T6 — `moverA` marca modificado pero no mueve `UpdatedAt`

Prueba que lo mata: `TestMoverArticulo_MueveUpdatedAtDelArticulo` (`internal/garantias/domain/dirty_tracking_test.go:266`).

Parche:

```diff
diff --git a/internal/garantias/domain/articulo.go b/internal/garantias/domain/articulo.go
index a1fca82..e875c1e 100644
--- a/internal/garantias/domain/articulo.go
+++ b/internal/garantias/domain/articulo.go
@@ -370,7 +370,7 @@ func (a *Articulo) moverA(hasta Ubicacion, now time.Time) error {
 		return ErrUbicacionSinCambio
 	}
 	a.ubicacion = hasta
-	a.tocar(now)
+	a.modificado = true
 	return nil
 }
```

Rojo — `go test ./internal/garantias/domain/ -run 'TestMoverArticulo_MueveUpdatedAtDelArticulo' -count=1`:

```text
--- FAIL: TestMoverArticulo_MueveUpdatedAtDelArticulo (0.00s)
    dirty_tracking_test.go:274: UpdatedAt() del artículo = 2026-08-15 10:00:00 +0000 UTC, want 2026-08-15 12:00:00 +0000 UTC (marcó modificado pero no movió la hora)
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/domain	1.829s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.765s
```

### T7 — el actor sale de `g.AbiertoPor()` y no de Identity

Prueba que lo mata: `TestComandos_UsuarioSaleDeIdentity` (`internal/garantias/app/comandos_compartidos_test.go:578`).

Parche:

```diff
diff --git a/internal/garantias/app/service.go b/internal/garantias/app/service.go
index 31e45ba..5f65ca7 100644
--- a/internal/garantias/app/service.go
+++ b/internal/garantias/app/service.go
@@ -121,6 +121,7 @@ func (s *Service) ejecutarFolio(
 		if err != nil {
 			return err
 		}
+		actor.Usuario = g.AbiertoPor()
 		vista, err := s.eventos.ObtenerPorClaveIdempotencia(ctx, actor.ClaveIdempotencia)
 		if err != nil {
 			return err
```

Rojo — `go test ./internal/garantias/app/ -run 'TestComandos_UsuarioSaleDeIdentity' -count=1`:

```text
--- FAIL: TestComandos_UsuarioSaleDeIdentity (0.00s)
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/AgregarArticulo (0.00s)
        fakes_test.go:314: evento "articulo_agregado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/RegistrarDesenlace (0.00s)
        fakes_test.go:314: evento "desenlace_registrado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/MarcarListoEntrega (0.00s)
        fakes_test.go:314: evento "folio_listo_entrega" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/IniciarProceso (0.00s)
        fakes_test.go:314: evento "folio_en_proceso" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/Entregar (0.00s)
        fakes_test.go:314: evento "etapa_avanzada" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
        fakes_test.go:314: evento "folio_entregado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/Cerrar (0.00s)
        fakes_test.go:314: evento "folio_cerrado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/RegistrarDictamen (0.00s)
        fakes_test.go:314: evento "dictamen_registrado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/Cancelar (0.00s)
        fakes_test.go:314: evento "folio_cancelado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/AvanzarArticulo (0.00s)
        fakes_test.go:314: evento "etapa_avanzada" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/MoverArticulo (0.00s)
        fakes_test.go:314: evento "articulo_movido" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/RegistrarDiagnostico (0.00s)
        fakes_test.go:314: evento "diagnostico_registrado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/MarcarNoDevuelto (0.00s)
        fakes_test.go:314: evento "devolucion_fallida" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
    --- FAIL: TestComandos_UsuarioSaleDeIdentity/AutorizarCambioFisico (0.00s)
        fakes_test.go:314: evento "cambio_autorizado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
        fakes_test.go:314: evento "cambio_autorizado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
        fakes_test.go:314: evento "articulo_agregado" con usuario "Juan", want "Itzel Domínguez Ochoa" the one from Identity
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.965s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.072s
```

### T8 — `resolverDuplicada` compara sólo el folio, no la huella

Prueba que lo mata: `TestResolverDuplicada_(CarreraConClaveDeOtroComando|ClaveMismoComandoOtroArticulo|ClaveMismoComandoOtraEtapa)` (`internal/garantias/app/resolver_duplicada_test.go:145,185,227`).

Parche:

```diff
diff --git a/internal/garantias/app/service.go b/internal/garantias/app/service.go
index 31e45ba..902bb77 100644
--- a/internal/garantias/app/service.go
+++ b/internal/garantias/app/service.go
@@ -208,8 +208,8 @@ func (s *Service) resolverDuplicada(
 	if vista == nil {
 		return nil, domain.ErrClaveIdempotenciaDuplicada
 	}
-	if _, err := revisarClave(vista, garantiaID, huella); err != nil {
-		return nil, err
+	if garantiaID != uuid.Nil && vista.GarantiaID() != garantiaID {
+		return nil, domain.ErrClaveIdempotenciaDeOtroFolio
 	}
 	return s.garantias.Obtener(ctx, vista.GarantiaID())
 }
```

Rojo — `go test ./internal/garantias/app/ -run 'TestResolverDuplicada_(CarreraConClaveDeOtroComando|ClaveMismoComandoOtroArticulo|ClaveMismoComandoOtraEtapa)' -count=1`:

```text
--- FAIL: TestResolverDuplicada_CarreraConClaveDeOtroComando (0.00s)
    resolver_duplicada_test.go:172: want ErrClaveIdempotenciaMismatch, got <nil>
--- FAIL: TestResolverDuplicada_ClaveMismoComandoOtraEtapa (0.00s)
    resolver_duplicada_test.go:254: want ErrClaveIdempotenciaMismatch, got <nil>
--- FAIL: TestResolverDuplicada_ClaveMismoComandoOtroArticulo (0.00s)
    resolver_duplicada_test.go:214: want ErrClaveIdempotenciaMismatch, got <nil>
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	2.002s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.116s
```

### T9 — `puntosDeAuditoria` sin el filtro `lat == nil || lon == nil`

Prueba que lo mata: `TestCerrar_RecoleccionSinGPSNoParticipa` (`internal/garantias/app/cerrar_test.go:225`).

Parche:

```diff
diff --git a/internal/garantias/app/cerrar.go b/internal/garantias/app/cerrar.go
index a79ab27..073d1c6 100644
--- a/internal/garantias/app/cerrar.go
+++ b/internal/garantias/app/cerrar.go
@@ -53,9 +53,6 @@ func puntosDeAuditoria(eventos []*domain.Evento) (*domain.PuntoGPS, []domain.Pun
 	var recolecciones []domain.PuntoGPS
 	for _, e := range eventos {
 		lat, lon := e.GPSLat(), e.GPSLon()
-		if lat == nil || lon == nil {
-			continue
-		}
 		punto := domain.PuntoGPS{Lat: *lat, Lon: *lon}
 		// if/else if y no switch: mismo motivo que revisarClave, la cobertura
 		// de un `case` no cubre la condición para gremlins v0.6.0.
```

Rojo — `go test ./internal/garantias/app/ -run 'TestCerrar_RecoleccionSinGPSNoParticipa' -count=1`:

```text
--- FAIL: TestCerrar_RecoleccionSinGPSNoParticipa (0.00s)
panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]
[signal 0xc0000005 code=0x0 addr=0x0 pc=0x7ff7a2bd390d]

goroutine 8 [running]:
testing.tRunner.func1.2({0x7ff7a2ca8740, 0x7ff7a2f9f3c0})
	C:/Program Files/Go/src/testing/testing.go:1974 +0x239
testing.tRunner.func1()
	C:/Program Files/Go/src/testing/testing.go:1977 +0x349
panic({0x7ff7a2ca8740?, 0x7ff7a2f9f3c0?})
	C:/Program Files/Go/src/runtime/panic.go:860 +0x13a
github.com/abdimuy/msp-api/internal/garantias/app.puntosDeAuditoria({0x2c7c13e70b0, 0x2, 0x2c7c1430ac0?})
	<repo>/internal/garantias/app/cerrar.go:56 +0xcd
github.com/abdimuy/msp-api/internal/garantias/app.(*Service).Cerrar.func1({0x7ff7a2d282f0?, 0x7ff7a2ffe580?}, 0x2c7c1474300, {{0x7ff7a2d088ea, 0x16}, {0x2c7c13d8810, 0x24}, {0x0, 0xee228b540, 0x0}, ...})
	<repo>/internal/garantias/app/cerrar.go:42 +0x8d
github.com/abdimuy/msp-api/internal/garantias/app.(*Service).ejecutarFolio.func1({0x7ff7a2d282f0, 0x7ff7a2ffe580})
	<repo>/internal/garantias/app/service.go:138 +0x24e
github.com/abdimuy/msp-api/internal/garantias/app_test.(*fakeTxRunner).RunInTx(0x2c7c15023c0, {0x7ff7a2d282f0, 0x7ff7a2ffe580}, 0x2c7c1445040)
	<repo>/internal/garantias/app/fakes_test.go:44 +0x15c
github.com/abdimuy/msp-api/internal/garantias/app.(*Service).ejecutarFolio(0x2c7c145a500, {0x7ff7a2d282f0, 0x7ff7a2ffe580}, {0x71, 0x32, 0xd, 0xee, 0xf0, 0x52, 0x4c, ...}, ...)
	<repo>/internal/garantias/app/service.go:115 +0x262
github.com/abdimuy/msp-api/internal/garantias/app.(*Service).Cerrar(0x2c7c145a500, {0x7ff7a2d282f0, 0x7ff7a2ffe580}, {{0x71, 0x32, 0xd, 0xee, 0xf0, 0x52, 0x4c, ...}, ...})
	<repo>/internal/garantias/app/cerrar.go:34 +0x253
github.com/abdimuy/msp-api/internal/garantias/app_test.TestCerrar_RecoleccionSinGPSNoParticipa(0x2c7c13c6600)
	<repo>/internal/garantias/app/cerrar_test.go:243 +0x6d9
testing.tRunner(0x2c7c13c6600, 0x7ff7a2d20760)
	C:/Program Files/Go/src/testing/testing.go:2036 +0xc3
created by testing.(*T).Run in goroutine 1
	C:/Program Files/Go/src/testing/testing.go:2101 +0x4a9
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.936s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.002s
```

### T10 — `firmaIDDe` con clave inválida devuelve un UUID en vez de `uuid.Nil`

Prueba que lo mata: `TestFirmaIDDe_ClaveInvalidaEsNil` (`internal/garantias/app/entregar_internal_test.go:16`).

Parche:

```diff
diff --git a/internal/garantias/app/entregar.go b/internal/garantias/app/entregar.go
index 6bab911..37cb8b4 100644
--- a/internal/garantias/app/entregar.go
+++ b/internal/garantias/app/entregar.go
@@ -73,7 +73,7 @@ func (s *Service) Entregar(ctx context.Context, cmd EntregarCmd) (*domain.Garant
 func firmaIDDe(clave string) uuid.UUID {
 	canonica, err := normalizarClave(clave)
 	if err != nil {
-		return uuid.Nil
+		return uuid.New()
 	}
 	return domain.FirmaImagenID(canonica)
 }
```

Rojo — `go test ./internal/garantias/app/ -run 'TestFirmaIDDe_ClaveInvalidaEsNil' -count=1`:

```text
--- FAIL: TestFirmaIDDe_ClaveInvalidaEsNil (0.00s)
    --- FAIL: TestFirmaIDDe_ClaveInvalidaEsNil/clave=___ (0.00s)
        entregar_internal_test.go:22: firmaIDDe("   ") = 6b1adbea-63d9-4824-9afe-4c7dbc220e88, want uuid.Nil
    --- FAIL: TestFirmaIDDe_ClaveInvalidaEsNil/clave=no-es-un-uuid (0.00s)
        entregar_internal_test.go:22: firmaIDDe("no-es-un-uuid") = 20f161a5-6eaf-488f-b97b-594d5b6ea6b6, want uuid.Nil
    --- FAIL: TestFirmaIDDe_ClaveInvalidaEsNil/clave= (0.00s)
        entregar_internal_test.go:22: firmaIDDe("") = 0646db4b-176e-4afe-8150-1b6f64a6cd80, want uuid.Nil
    --- FAIL: TestFirmaIDDe_ClaveInvalidaEsNil/clave={no-es-un-uuid} (0.00s)
        entregar_internal_test.go:22: firmaIDDe("{no-es-un-uuid}") = 321b32c7-ce53-4505-b3f9-df718b2088b9, want uuid.Nil
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	1.964s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	1.982s
```

### T11 — se revierte la decisión de P2 (huella cruda en vez de normalizada)

Prueba que lo mata: `TestEntregar_ReintentoMismoContenidoOtroCliente_200` (`internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go:280`).

Parche:

```diff
diff --git a/internal/garantias/app/entregar.go b/internal/garantias/app/entregar.go
--- a/internal/garantias/app/entregar.go
+++ b/internal/garantias/app/entregar.go
@@ -47,7 +47,7 @@ func (s *Service) Entregar(ctx context.Context, cmd EntregarCmd) (*domain.Garant
 	if len(cmd.Firma) == 0 {
 		return nil, domain.ErrFirmaRequerida
 	}
-	huella := huellaDeEntregar(cmd)
+	huella := huellaDe(cmd)
 	// The image id comes from the canonical key, exactly like the domain
 	// derives it. An invalid key yields uuid.Nil on purpose: the blob is still
 	// written and then discarded after ejecutarFolio fails with the key
```

Rojo — `go test ./internal/garantias/infra/garhttp/ -run 'TestEntregar_ReintentoMismoContenidoOtroCliente_200' -count=1`:

```text
--- FAIL: TestEntregar_ReintentoMismoContenidoOtroCliente_200 (0.00s)
    --- FAIL: TestEntregar_ReintentoMismoContenidoOtroCliente_200/otro_content_type (0.00s)
        handlers_entregar_comandos_test.go:308: 
            	Error Trace:	<repo>/internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go:308
            	Error:      	Not equal: 
            	            	expected: 200
            	            	actual  : 409
            	Test:       	TestEntregar_ReintentoMismoContenidoOtroCliente_200/otro_content_type
            	Messages:   	{"$schema":"https://example.com/v2/schemas/ErrorModel.json","title":"Conflict","status":409,"detail":"la clave de idempotencia ya se usó con otro contenido","errors":[{"message":"code=warranty_idempotency_key_mismatch"}]}
    --- FAIL: TestEntregar_ReintentoMismoContenidoOtroCliente_200/mismo_instante_otro_offset (0.02s)
        handlers_entregar_comandos_test.go:308: 
            	Error Trace:	<repo>/internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go:308
            	Error:      	Not equal: 
            	            	expected: 200
            	            	actual  : 409
            	Test:       	TestEntregar_ReintentoMismoContenidoOtroCliente_200/mismo_instante_otro_offset
            	Messages:   	{"$schema":"https://example.com/v2/schemas/ErrorModel.json","title":"Conflict","status":409,"detail":"la clave de idempotencia ya se usó con otro contenido","errors":[{"message":"code=warranty_idempotency_key_mismatch"}]}
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	2.876s
FAIL
```

Verde al revertir:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	2.963s
```

## La compuerta de mutación

El brief pide `make test-mutation-garantias-app` **en verde**, y no lo estaba. No
por las pruebas que faltaban: **ya estaba en rojo en `main`**. Este apartado mide
el rojo, encuentra la causa y deja la compuerta verde sin bajar ningún piso.

### Estaba en rojo en `main`

**MEDIDO** el 10 de noviembre de 2026 en un worktree limpio de `6e0e44f` (el
commit desde el que nace esta rama), con su propia caché de pruebas. Recortado a
los conteos: no se pegan las 129 líneas de mutantes, sólo el veredicto.

```text
SHA=6e0e44f8595e53fcff71410291e37c447b8f828f
Mutation testing completed in 4 minutes 5 seconds
Killed: 103, Lived: 9, Not covered: 17
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 91.96%
Mutator coverage: 86.82%
   mutantes: 129 · evaluados: 112 (86%) · expirados: 0 · eficacia: 91.96%
✘ ./internal/garantias/app: solo se evaluó el 86% de los mutantes (112 de 129),
  bajo el piso de 90%. Los 0 expirados NO cuentan en la eficacia,
  así que el porcentaje de arriba se calcula sobre una muestra chica y variable.
  Subí GREMLINS_TIMEOUT_COEFFICIENT (hoy 60) o achicá el paquete.
make: *** [Makefile:519: test-mutation-garantias-app] Error 1
```

El árbol de esta tarea, **antes** del cambio de forma de abajo, tampoco pasaba:
131 mutantes (los 129 de `main` más dos de `huellaDeEntregar`), 114 evaluados
(87%), eficacia 90.35%, los mismos 17 no cubiertos.

```text
Mutation testing completed in 4 minutes 59 seconds
Killed: 103, Lived: 11, Not covered: 17
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 90.35%
Mutator coverage: 87.02%
   mutantes: 131 · evaluados: 114 (87%) · expirados: 0 · eficacia: 90.35%
✘ ./internal/garantias/app: solo se evaluó el 87% de los mutantes (114 de 131),
  bajo el piso de 90%. Los 0 expirados NO cuentan en la eficacia,
  así que el porcentaje de arriba se calcula sobre una muestra chica y variable.
  Subí GREMLINS_TIMEOUT_COEFFICIENT (hoy 60) o achicá el paquete.
make: *** [Makefile:519: test-mutation-garantias-app] Error 1
```

### La causa: gremlins no ve la condición de un `case`

**MEDIDO** sobre el mismo árbol, con el perfil de cobertura de Go. Go marca el
bloque del `case` como ejecutado, pero lo hace **empezar después de los dos
puntos**:

```text
$ go test ./internal/garantias/app -coverprofile=cov.out
$ grep 'service.go:1[78][0-9]\.' cov.out
github.com/abdimuy/msp-api/internal/garantias/app/service.go:176.20,177.20 1 1
github.com/abdimuy/msp-api/internal/garantias/app/service.go:178.66,179.55 1 1
github.com/abdimuy/msp-api/internal/garantias/app/service.go:180.48,181.19 1 1
```

El mutante que gremlins reporta en `service.go:176:13` cae sobre el `==` de
`case vista == nil`, que está en la columna 13; el primer bloque de esa línea
empieza en la **20**, o sea después del `:`. gremlins decide si un mutante está
cubierto comparando línea y columna contra los bloques del perfil
(`gremlins@v0.6.0/internal/coverage/profile.go:53`, `isPositionCovered`), así
que el operador queda fuera de todos y lo declara `NOT COVERED` **aunque las
pruebas lo maten**.

Los 17 no cubiertos de `main` son exactamente eso:

- **14 condiciones de `case`**: siete en `service.go` (`176:13`, `178:18`,
  `178:30`, `178:52`, `180:14`, `180:20`, `180:38`), seis en `cerrar.go`
  (`61:17`, `63:17`, `63:51`, `64:19`, `64:26`, `64:45`) y una en
  `adjuntar_imagen.go` (`97:12`).
- **3 `const`**: `adjuntar_imagen.go:12:36`, `adjuntar_imagen.go:12:43` y
  `entregar.go:14:25`. Ninguno se puede instrumentar; quedan igual y son los
  únicos tres que siguen sin cubrirse después del cambio.

### El cambio: `switch` → `if`/`else if`

**LEÍDO** en el diff de `40f1073`: las tres condiciones pasan a `if`/`else if`
con **las mismas condiciones y los mismos cuerpos**, sin una rama nueva:

- `revisarClave` (`app/service.go`): los cuatro `case`/`default` en cuatro `if`
  con los mismos `return`.
- `puntosDeAuditoria` (`app/cerrar.go`): los dos `case` en `if`/`else if`.
- el `switch` de error de `AdjuntarImagen` (`app/adjuntar_imagen.go`): `if err
  == nil`, `if !errors.Is(err, domain.ErrImagenNoEncontrada)` y el mismo
  comentario del caso que sigue.

Es un cambio de forma, no de comportamiento: la suite completa da lo mismo antes
y después, y está declarado como desviación (el brief no listaba esos tres
archivos de producción). Cada archivo lleva un comentario que dice por qué está
así, para que nadie lo «arregle» de vuelta a `switch` y reviva el falso `NOT
COVERED`.

### Verde

**MEDIDO** sobre `40f1073` (el commit de código de esta tarea), mismo comando y
misma máquina:

```text
SHA=40f10736a59eb29cea3cf478dc2044b1308f5e8d
./scripts/gremlins-gate.sh ./internal/garantias/app 80 90
Mutation testing completed in 4 minutes 32 seconds
Killed: 116, Lived: 12, Not covered: 3
Timed out: 0, Not viable: 0, Skipped: 0
Test efficacy: 90.62%
Mutator coverage: 97.71%
   mutantes: 131 · evaluados: 128 (97%) · expirados: 0 · eficacia: 90.62%
EXIT_MUTATION=0
```

**Nota de transcripción.** El separador `·` de la línea de conteos se
restauró: `make` en Windows lo entrega como dos caracteres mal codificados. La
línea que imprime el visto bueno por paquete no se transcribe por la misma
razón; el hecho está en `EXIT_MUTATION=0`.

97% de los mutantes evaluados y 90.62% de eficacia: los dos pisos del script
(90% y 80%) se cumplen sin tocarlos. Los 12 que sobreviven son, casi todos,
mutantes que ninguna prueba de esta tarea observa (aritmética de `blob.go`,
umbrales de `consultas.go` y de `huella.go`, un `&&` del filtro de `cerrar.go`);
el piso no se movió para esconderlos. Uno de ellos es además un falso
sobreviviente, como se ve abajo.

### Dos mutantes que el refactor destapó

El refactor hizo visibles catorce mutantes que antes se contaban como no
cubiertos. Doce los matan las pruebas que ya estaban; dos caían sobre la cadena
`&&` de `puntosDeAuditoria`, que ninguna prueba ejercitaba con un evento que no
fuera recolección. Se agregó `TestCerrar_SoloLaRecoleccionEsPuntoDeComparacion`
(`app/cerrar_test.go`): el folio se entrega y se recolecta en la misma esquina,
pero un artículo pasó por un taller en Guadalajara; si ese paso entrara a la
auditoría, la entrega saldría con 460 km de desvío.

**MEDIDO** sobre `40f1073`: cada parche aplica con `git apply --check` en 0, la
prueba se pone roja con el mutante y vuelve a verde al revertirlo. Los dos
parches son de una línea y cambian sólo el operador.

**T12 — `cerrar.go:64`, el `&&` que une el tipo de evento con la etapa**

```diff
diff --git a/internal/garantias/app/cerrar.go b/internal/garantias/app/cerrar.go
index a79ab27..81d998b 100644
--- a/internal/garantias/app/cerrar.go
+++ b/internal/garantias/app/cerrar.go
@@ -61,7 +61,7 @@ func puntosDeAuditoria(eventos []*domain.Evento) (*domain.PuntoGPS, []domain.Pun
 		// de un `case` no cubre la condición para gremlins v0.6.0.
 		if e.Tipo() == domain.TipoEventoFolioEntregado {
 			entrega = &punto
-		} else if e.Tipo() == domain.TipoEventoEtapaAvanzada &&
+		} else if e.Tipo() == domain.TipoEventoEtapaAvanzada ||
 			e.EtapaHasta() != nil && *e.EtapaHasta() == domain.EtapaRecolectado {
 			recolecciones = append(recolecciones, punto)
 		}
```

Rojo, `go test ./internal/garantias/app -run '^TestCerrar_SoloLaRecoleccionEsPuntoDeComparacion$' -count=1`:

```text
--- FAIL: TestCerrar_SoloLaRecoleccionEsPuntoDeComparacion (0.00s)
    cerrar_test.go:281: Cerrar: warranty_deviation_justification_required: la entrega se desvió del domicilio de recolección: se requiere justificación
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	6.124s
FAIL
```

Verde al revertir, el mismo comando:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	5.803s
```

**T13 — `cerrar.go:65`, el `&&` que exige que la etapa destino exista**

```diff
diff --git a/internal/garantias/app/cerrar.go b/internal/garantias/app/cerrar.go
index a79ab27..1aa640e 100644
--- a/internal/garantias/app/cerrar.go
+++ b/internal/garantias/app/cerrar.go
@@ -62,7 +62,7 @@ func puntosDeAuditoria(eventos []*domain.Evento) (*domain.PuntoGPS, []domain.Pun
 		if e.Tipo() == domain.TipoEventoFolioEntregado {
 			entrega = &punto
 		} else if e.Tipo() == domain.TipoEventoEtapaAvanzada &&
-			e.EtapaHasta() != nil && *e.EtapaHasta() == domain.EtapaRecolectado {
+			e.EtapaHasta() != nil || *e.EtapaHasta() == domain.EtapaRecolectado {
 			recolecciones = append(recolecciones, punto)
 		}
 	}
```

Rojo, el mismo comando:

```text
--- FAIL: TestCerrar_SoloLaRecoleccionEsPuntoDeComparacion (0.00s)
    cerrar_test.go:281: Cerrar: warranty_deviation_justification_required: la entrega se desvió del domicilio de recolección: se requiere justificación
FAIL
FAIL	github.com/abdimuy/msp-api/internal/garantias/app	2.619s
FAIL
```

Verde al revertir, el mismo comando:

```text
ok  	github.com/abdimuy/msp-api/internal/garantias/app	2.116s
```

gremlins acredita el de la línea 65 (`KILLED`) y al de la 64 lo sigue contando
`LIVED` aunque el paquete se ponga rojo con él; es el mismo defecto de medición
de arriba, esta vez regalando un sobreviviente en vez de un no cubierto. En la
dirección que importa —la compuerta— el error es conservador: la eficacia real
es mayor que la reportada.

## P2 - la huella de `entregar`

### El problema, en dos casos

`huellaDe(cmd)` hashea el JSON completo del comando, así que dos reintentos
legítimos del **mismo** comando desde otro teléfono podían parecer contenido
distinto y recibir 409 `warranty_idempotency_key_mismatch`:

- **Caso A, el `Content-Type` de la firma.** El mismo archivo viaja como
  `image/png` en un cliente y como `application/octet-stream` en otro. Los
  bytes son idénticos; lo que cambia es la etiqueta que declara el transporte.
- **Caso B, el `device_created_at`.** El mismo instante escrito como
  `2026-11-10T18:00:00Z` y como `2026-11-10T12:00:00-06:00`. Es el mismo
  momento en dos zonas horarias.

### Medición antes del cambio (`7833cf6`)

**MEDIDO** el 10 de noviembre de 2026 en el contenedor de desarrollo, con el
router real (`nuevoRouterCompleto("entregar-garantia")`) y un archivo de
medición temporal en `internal/garantias/infra/garhttp` que imprime el código
de cada POST. Los dos casos daban 409:

```text
=== RUN   TestMedicionP2_HuellaEntrega
=== RUN   TestMedicionP2_HuellaEntrega/a_mismo_contenido_otro_content_type
[a_mismo_contenido_otro_content_type] firma image/png y luego application/octet-stream
[a_mismo_contenido_otro_content_type] POST 1 (mismos bytes, misma clave) -> 200
[a_mismo_contenido_otro_content_type] POST 2 (mismos bytes, misma clave) -> 409 code=warranty_idempotency_key_mismatch
=== RUN   TestMedicionP2_HuellaEntrega/b_mismo_instante_otro_offset
[b_mismo_instante_otro_offset] device_created_at 2026-11-10T18:00:00Z y luego 2026-11-10T12:00:00-06:00
[b_mismo_instante_otro_offset] POST 1 (mismos bytes, misma clave) -> 200
[b_mismo_instante_otro_offset] POST 2 (mismos bytes, misma clave) -> 409 code=warranty_idempotency_key_mismatch
--- PASS: TestMedicionP2_HuellaEntrega (0.03s)
PASS
ok  github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	2.908s
```

### Medición después del cambio (`40f1073`)

**MEDIDO** sobre el commit de entrega, sin instrumentación temporal: la prueba
permanente `TestEntregar_ReintentoMismoContenidoOtroCliente_200` recorre los dos
mismos casos por el router real y exige 200 en el reintento.

```text
SHA=40f10736a59eb29cea3cf478dc2044b1308f5e8d
=== RUN   TestEntregar_ReintentoMismoContenidoOtroCliente_200
=== PAUSE TestEntregar_ReintentoMismoContenidoOtroCliente_200
=== CONT  TestEntregar_ReintentoMismoContenidoOtroCliente_200
=== RUN   TestEntregar_ReintentoMismoContenidoOtroCliente_200/otro_content_type
=== PAUSE TestEntregar_ReintentoMismoContenidoOtroCliente_200/otro_content_type
=== RUN   TestEntregar_ReintentoMismoContenidoOtroCliente_200/mismo_instante_otro_offset
=== PAUSE TestEntregar_ReintentoMismoContenidoOtroCliente_200/mismo_instante_otro_offset
=== CONT  TestEntregar_ReintentoMismoContenidoOtroCliente_200/otro_content_type
=== CONT  TestEntregar_ReintentoMismoContenidoOtroCliente_200/mismo_instante_otro_offset
--- PASS: TestEntregar_ReintentoMismoContenidoOtroCliente_200 (0.00s)
    --- PASS: TestEntregar_ReintentoMismoContenidoOtroCliente_200/otro_content_type (0.00s)
    --- PASS: TestEntregar_ReintentoMismoContenidoOtroCliente_200/mismo_instante_otro_offset (0.00s)
PASS
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	0.021s
EXIT_P2=0
```

### Decisión: opción A, normalizar la huella

**Elegida** (con el visto bueno de quien pidió la tarea). La huella sigue
cubriendo todo el comando, pero dos campos se normalizan antes de hashear:

- `DeviceCreatedAt` se pasa a UTC: el instante es el mismo aunque el offset no.
- `FirmaContentType` se reemplaza por `http.DetectContentType(cmd.Firma)`: lo
  que identifica la firma son los bytes, no la etiqueta del transporte. Es la
  misma detección que `guardarBlob` ya aplica al guardar, así que la huella y
  el archivo guardado coinciden.

La normalización se hace sobre una **copia** del comando
(`huellaDeEntregar`), igual que `huellaDe` ya hacía con
`ClaveIdempotencia`: el comando del llamador nunca se muta. `huellaDe` no
cambia, y los demás comandos siguen con la huella cruda. El único llamador es
`app/entregar.go:50`.

```diff
@@ -47,7 +47,7 @@ func (s *Service) Entregar(ctx context.Context, cmd EntregarCmd) (*domain.Garant
 	if len(cmd.Firma) == 0 {
 		return nil, domain.ErrFirmaRequerida
 	}
-	huella := huellaDe(cmd)
+	huella := huellaDeEntregar(cmd)
```

```go
func huellaDeEntregar(cmd EntregarCmd) string {
	copia := cmd
	copia.DeviceCreatedAt = copia.DeviceCreatedAt.UTC()
	if len(copia.Firma) > 0 {
		copia.FirmaContentType = http.DetectContentType(copia.Firma)
	}
	return huellaDe(copia)
}
```

**Por qué no la opción B** (comparar sólo el folio y la clave, sin huella): esa
opción convierte un reintento con **otro contenido** en un 200 silencioso y
deja viva la regla que el mutante T8 y `resolverDuplicada` protegen. La clave
de idempotencia identifica el comando, no el folio: si el contenido cambia, el
cliente se equivocó y debe enterarse con 409. La opción A conserva esa regla y
sólo quita las dos diferencias que no son contenido.

**La prueba que sostiene la decisión** es
`TestEntregar_ReintentoMismoContenidoOtroCliente_200`
(`internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go:280`):
manda el mismo comando dos veces cambiando el `Content-Type` declarado y espera
200 la segunda. Es también el mutante **T11**: revertir la decisión (volver a
`huellaDe(cmd)`) la pone roja, y la evidencia está en la sección de mutantes.

**No contradice la regla de la Parte 2.** El §D3 del brief dice que el API
responde 409 a "cualquier campo distinto, incluida la hora del dispositivo con
otro formato", y esa regla sigue en pie para un instante **distinto**. Lo que
se normaliza es la **representación del mismo instante** (`18:00:00Z` y
`12:00:00-06:00` son el mismo momento), y el teléfono de la Parte 2 ni siquiera
puede aprovecharlo: fija el campo en UTC con `Z` y reenvía los mismos bytes. La
normalización protege a los otros clientes del API sin aflojar la regla del
teléfono.

## Cobertura

**MEDIDO** sobre el commit de entrega `40f1073` en Linux (`golang:1.25`,
`go1.25.14`), con `FB_DATABASE` y el script de la compuerta
(`./scripts/go-cover-gate.sh`). Los pisos son los que pide el brief; entre
paréntesis, cuando difieren, los del archivo `scripts/cover-floors-garantias.txt`.

| Paquete | Piso del brief | Obtenido | Sentencias | Estado |
|---|---|---|---|---|
| `domain` | 99% | **99.3%** | 825/831 | Cumple |
| `app` | ≥ 96% (archivo: 95%) | **97.3%** | 366/376 | Cumple ambos |
| `garhttp` | ≥ 90% | **91.2%** | 384/421 | Cumple |
| `garfb` | ≥ 85% con `FB_DATABASE` | **91.7%** | 605/660 | Cumple |
| `infra/storage` | ≥ 85% | **85.5%** | 100/117 | Cumple en Linux |
| `infra/identidad` | — (archivo: 100%) | **100.0%** | 12/12 | Cumple |
| `ports/outbound` | — (archivo: 70%) | **100.0%** | 2/2 | Cumple |
| `internal/garantias` (raíz) | — (archivo: 70%) | **94.4%** | — | Cumple |

Ninguno de los paquetes que toca esta tarea bajó de su piso. Las pruebas nuevas
**matan mutantes**, pero casi no mueven el contador porque ejercitan ramas que
ya se cubrían por otro camino; `app` sube por `huellaDeEntregar` (5 sentencias
nuevas, todas cubiertas) y por la prueba que descarta el blob de una clave
inválida. Lo que cambia sobre todo no es el número: es que ahora hay una prueba
que se pone roja al quitar cada regla.

### La diferencia de `infra/storage` entre Windows y Linux

**MEDIDO** en los dos sistemas. En Windows, la misma compuerta sobre el mismo
árbol da `infra/storage` **83.8%** (98 de 117 sentencias) en vez de 85.5%
(100 de 117), y ahí la compuerta marca `✘` y sale con error. La causa está
medida: dos pruebas de permisos POSIX se saltan a propósito en Windows
(`filesystem_test.go:422` y `:442`), y no es un defecto de esta tarea. El resto
de los paquetes da el mismo porcentaje en los dos sistemas; la cifra que manda
es la de Linux, la plataforma del runner; la compuerta con base todavía no
corre en CI (el job `integracion-firebird` está desactivado a propósito).

## Verificación

**MEDIDO** el 10 de noviembre de 2026 sobre `40f1073`
(`40f10736a59eb29cea3cf478dc2044b1308f5e8d`), el commit de código de esta
rama, en un *worktree* aparte de ese SHA, con la caché de pruebas vacía
(`go clean -testcache`) y dentro de `golang:1.25` (`go1.25.14`, linux/amd64),
contra la copia restaurada `/firebird/data/DEVTEST.FDB` de la base de
desarrollo; el porqué está al pie del bloque. Se transcribe la salida completa,
sin recortar ninguna línea.

```text
== entorno ==
go version go1.25.14 linux/amd64
git version 2.47.3
uid=1000
SHA=40f10736a59eb29cea3cf478dc2044b1308f5e8d rama=HEAD
GOFLAGS=[] GOROOT=/usr/local/go
FB_DATABASE=/firebird/data/DEVTEST.FDB (copia restaurada con gbak de MUEBLERA.FDB)

== 0) go clean -testcache ==
EXIT_CLEAN=0

== 1) gofmt -l internal/garantias cmd (vacio = limpio) ==
EXIT_GOFMT=0

== 2) go vet ./internal/garantias/... ./cmd/api/... ==
EXIT_VET=0

== 3) go build ./... ==
EXIT_BUILD=0

== 4) grep de mojibake (debe salir vacio) ==
EXIT_GREP=1 (1 = sin coincidencias)

== 5) compuerta de pisos (./scripts/go-cover-gate.sh, con base) ==
── cobertura: ./internal/garantias/...
ok  	github.com/abdimuy/msp-api/internal/garantias	0.007s	coverage: 94.4% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/app	0.104s	coverage: 97.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	0.037s	coverage: 99.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	14.911s	coverage: 91.7% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	4.509s	coverage: 91.2% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/identidad	0.009s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	0.029s	coverage: 85.5% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	0.019s	coverage: 100.0% of statements
✔ internal/garantias: 94.4% (piso 70%)
✔ internal/garantias/app: 97.3% (piso 95%)
✔ internal/garantias/domain: 99.3% (piso 99%)
✔ internal/garantias/infra/garfb: 91.7% (piso 85%)
✔ internal/garantias/infra/garhttp: 91.2% (piso 90%)
✔ internal/garantias/infra/identidad: 100.0% (piso 100%)
✔ internal/garantias/infra/storage: 85.5% (piso 85%)
✔ internal/garantias/ports/outbound: 100.0% (piso 70%)
✔ cobertura: 8 paquetes sobre su piso
EXIT_GATE=0

== 6) go test -race -count=1 -p 1 -coverprofile (con base) ==
ok  	github.com/abdimuy/msp-api/internal/garantias	1.040s	coverage: 94.4% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/app	1.185s	coverage: 97.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	1.155s	coverage: 99.3% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	16.845s	coverage: 91.7% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	7.629s	coverage: 91.2% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/identidad	1.038s	coverage: 100.0% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	1.070s	coverage: 85.5% of statements
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	1.036s	coverage: 100.0% of statements
EXIT_TEST_CON_DB=0

-- cobertura por paquete (perfil de la corrida anterior) --
github.com/abdimuy/msp-api/internal/garantias/domain/   99.3%  (825 de 831 sentencias)
github.com/abdimuy/msp-api/internal/garantias/app/   97.3%  (366 de 376 sentencias)
github.com/abdimuy/msp-api/internal/garantias/infra/garfb/   91.7%  (605 de 660 sentencias)
github.com/abdimuy/msp-api/internal/garantias/infra/garhttp/   91.2%  (384 de 421 sentencias)
github.com/abdimuy/msp-api/internal/garantias/infra/storage/   85.5%  (100 de 117 sentencias)
github.com/abdimuy/msp-api/internal/garantias/infra/identidad/  100.0%  (12 de 12 sentencias)
github.com/abdimuy/msp-api/internal/garantias/ports/outbound/  100.0%  (2 de 2 sentencias)

== 7) sin FB_DATABASE: los tests de Firebird se saltan y nada falla ==
ok  	github.com/abdimuy/msp-api/internal/garantias	0.007s
ok  	github.com/abdimuy/msp-api/internal/garantias/app	0.070s
ok  	github.com/abdimuy/msp-api/internal/garantias/domain	0.028s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garfb	0.013s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/garhttp	0.191s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/identidad	0.011s
ok  	github.com/abdimuy/msp-api/internal/garantias/infra/storage	0.053s
ok  	github.com/abdimuy/msp-api/internal/garantias/ports/outbound	0.019s
EXIT_TEST_SIN_DB=0
```

La corrida 6 (`-race -count=1 -p 1`) da los mismos porcentajes que la 5. La 7
repite todo sin `FB_DATABASE`: los tests de Firebird se saltan y el comando sale
con `EXIT_TEST_SIN_DB=0`.

**La base: una copia restaurada, y por qué.** Las corridas previas de este mismo
bloque contra la base de desarrollo (`MUEBLERA.FDB`) fallaron de forma
intermitente, cada vez en una prueba distinta y siempre por el mismo motivo: los
eventos de `MSP_GA_EVENTO` que comparten `DEVICE_CREATED_AT` y `CREATED_AT`
salen ordenados por `RDB$DB_KEY`, la dirección física de la fila
(`infra/garfb/queries.go:197-205`), y en una base que arrastra filas muertas de
transacciones revertidas esa dirección deja de coincidir con el orden de
inserción. Se vio en `TestE2E_ClientePorProveedor` (el `folio_entregado` salió
antes del `etapa_avanzada` de la entrega) y en
`TestEventoRepo_ListarDesempataPorInsercion` (los tres eventos insertados en
orden salieron al revés). Ninguna de esas pruebas ni el código de orden son de
esta tarea.

Para medir sobre un árbol limpio, la verificación corrió contra
`/firebird/data/DEVTEST.FDB`, una copia de la base de desarrollo hecha con
`gbak -b` y `gbak -c` en el mismo contenedor de Firebird, como indica
`docs/base-de-datos-de-pruebas.md`; `MUEBLERA.FDB` no se tocó. La copia tiene el
mismo esquema y los mismos datos, y tras la restauración el desempate físico
vuelve a coincidir con el orden de escritura, que es lo que la prueba mide. En
la base de desarrollo esa coincidencia se pierde con el uso y el fallo queda
anotado como intermitente conocido.

### En Windows

**MEDIDO** en la misma máquina, en Git Bash con Go 1.26.3 (el módulo declara
1.25.0): `golangci-lint` sobre `./internal/garantias/...`, el `git grep` de
mojibake y `make check-sealed MODULE=garantias`.

```text
== entorno (Windows / Git Bash) ==
go version go1.26.3 windows/amd64
SHA=40f10736a59eb29cea3cf478dc2044b1308f5e8d rama=feat/garantias-t11-pendientes

== golangci-lint run ./internal/garantias/... ==
0 issues.
EXIT_LINT=0

== git grep de mojibake en el arbol de trabajo ==
EXIT_GREP=1 (1 = sin coincidencias)

== make check-sealed MODULE=garantias ==
garantias is sealed
EXIT_SEALED=0

== git status --porcelain (solo los cambios declarados) ==
 M docs/superpowers/plans/garantias-task-9-report.md
?? docs/superpowers/plans/garantias-task-11-report.md
```

La marca de visto bueno que imprime `make` al inicio de esa línea no se
transcribe porque en este Windows llega mal codificada; el texto y el código de
salida sí son literales.

La compuerta de `infra/storage` es la única que no se puede poner verde en
Windows, por los dos `t.Skip` de permisos POSIX ya explicados en «Cobertura»;
el resto de la verificación da lo mismo que en Linux.

### Las pruebas citadas existen en el SHA

**MEDIDO** con `git grep -n "func <Nombre>(" 40f1073` desde la raíz del repo,
una vez por cada prueba que este reporte cita:

```text
$ git grep -n "func TestArticulo_Dictamen_DevuelveCopia(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/articulo_test.go:142:func TestArticulo_Dictamen_DevuelveCopia(t *testing.T) {

$ git grep -n "func TestArticulo_Ruta_DevuelveCopia(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/articulo_test.go:124:func TestArticulo_Ruta_DevuelveCopia(t *testing.T) {

$ git grep -n "func TestArticulo_Desenlace_DevuelveCopia(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/articulo_test.go:160:func TestArticulo_Desenlace_DevuelveCopia(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_AgregarArticulo(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:27:func TestFolioUpdatedAt_AgregarArticulo(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_AvanzarArticulo(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:38:func TestFolioUpdatedAt_AvanzarArticulo(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_RegistrarDiagnostico(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:51:func TestFolioUpdatedAt_RegistrarDiagnostico(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_RegistrarDictamen(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:62:func TestFolioUpdatedAt_RegistrarDictamen(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_AutorizarCambioFisico(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:73:func TestFolioUpdatedAt_AutorizarCambioFisico(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_RegistrarDesenlace(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:84:func TestFolioUpdatedAt_RegistrarDesenlace(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_MarcarNoDevuelto(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:95:func TestFolioUpdatedAt_MarcarNoDevuelto(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_MoverArticulo(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:106:func TestFolioUpdatedAt_MoverArticulo(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_IniciarProceso(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:117:func TestFolioUpdatedAt_IniciarProceso(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_MarcarListoEntrega(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:129:func TestFolioUpdatedAt_MarcarListoEntrega(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_Entregar(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:143:func TestFolioUpdatedAt_Entregar(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_Cerrar(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:154:func TestFolioUpdatedAt_Cerrar(t *testing.T) {

$ git grep -n "func TestFolioUpdatedAt_Cancelar(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/folio_updated_at_test.go:166:func TestFolioUpdatedAt_Cancelar(t *testing.T) {

$ git grep -n "func TestMarcarNoDevuelto_MueveUpdatedAtDelArticulo(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/dirty_tracking_test.go:253:func TestMarcarNoDevuelto_MueveUpdatedAtDelArticulo(t *testing.T) {

$ git grep -n "func TestMoverArticulo_MueveUpdatedAtDelArticulo(" 40f1073 -- internal/garantias
40f1073:internal/garantias/domain/dirty_tracking_test.go:266:func TestMoverArticulo_MueveUpdatedAtDelArticulo(t *testing.T) {

$ git grep -n "func TestComandos_UsuarioSaleDeIdentity(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/comandos_compartidos_test.go:578:func TestComandos_UsuarioSaleDeIdentity(t *testing.T) {

$ git grep -n "func TestResolverDuplicada_CarreraConClaveDeOtroComando(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/resolver_duplicada_test.go:145:func TestResolverDuplicada_CarreraConClaveDeOtroComando(t *testing.T) {

$ git grep -n "func TestResolverDuplicada_ClaveMismoComandoOtroArticulo(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/resolver_duplicada_test.go:185:func TestResolverDuplicada_ClaveMismoComandoOtroArticulo(t *testing.T) {

$ git grep -n "func TestResolverDuplicada_ClaveMismoComandoOtraEtapa(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/resolver_duplicada_test.go:227:func TestResolverDuplicada_ClaveMismoComandoOtraEtapa(t *testing.T) {

$ git grep -n "func TestCerrar_RecoleccionSinGPSNoParticipa(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/cerrar_test.go:225:func TestCerrar_RecoleccionSinGPSNoParticipa(t *testing.T) {

$ git grep -n "func TestCerrar_SoloLaRecoleccionEsPuntoDeComparacion(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/cerrar_test.go:267:func TestCerrar_SoloLaRecoleccionEsPuntoDeComparacion(t *testing.T) {

$ git grep -n "func TestFirmaIDDe_ClaveInvalidaEsNil(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/entregar_internal_test.go:16:func TestFirmaIDDe_ClaveInvalidaEsNil(t *testing.T) {

$ git grep -n "func TestEntregar_ClaveInvalidaDescartaElBlob(" 40f1073 -- internal/garantias
40f1073:internal/garantias/app/entregar_test.go:356:func TestEntregar_ClaveInvalidaDescartaElBlob(t *testing.T) {

$ git grep -n "func TestEntregar_ReintentoMismoContenidoOtroCliente_200(" 40f1073 -- internal/garantias
40f1073:internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go:280:func TestEntregar_ReintentoMismoContenidoOtroCliente_200(t *testing.T) {
```

`TestMedicionP2_HuellaEntrega` no aparece a propósito: es la instrumentación
temporal de la medición «antes» de P2 (desviación 3) y no está en el árbol.

### Archivos que cambian

**MEDIDO** con `git diff --name-only origin/main...40f1073` (`origin/main` es
`6e0e44f`):

```text
internal/garantias/app/abrir_garantia_test.go
internal/garantias/app/adjuntar_imagen.go
internal/garantias/app/cerrar.go
internal/garantias/app/cerrar_test.go
internal/garantias/app/comandos_compartidos_test.go
internal/garantias/app/entregar.go
internal/garantias/app/entregar_internal_test.go
internal/garantias/app/entregar_test.go
internal/garantias/app/huella.go
internal/garantias/app/resolver_duplicada_test.go
internal/garantias/app/service.go
internal/garantias/domain/articulo_test.go
internal/garantias/domain/dirty_tracking_test.go
internal/garantias/domain/folio_updated_at_test.go
internal/garantias/infra/garhttp/e2e_comandos_test.go
internal/garantias/infra/garhttp/handlers_entregar_comandos_test.go
```

Son dieciséis archivos: once de pruebas y cinco de producción. De los de
producción, `app/huella.go` y `app/entregar.go` son los que autoriza P2;
`app/service.go`, `app/cerrar.go` y `app/adjuntar_imagen.go` son el cambio de
forma declarado en la desviación 5. El commit de entrega de este reporte, que
sólo agrega los dos documentos, deja el diff completo de la rama en dieciocho
archivos.

## Desviaciones declaradas

1. **`e2e_comandos_test.go`: los e2e ahora sellan cada comando con una hora
   distinta.** El brief no lo pedía; lo pedí yo porque el gate de Windows
   encontró un fallo intermitente real: con el reloj congelado, todos los
   eventos del e2e compartían `DEVICE_CREATED_AT` y `CREATED_AT`, y el
   desempate caía en `RDB$DB_KEY` (una dirección física que las transacciones
   revertidas de otras pruebas reutilizan). Se agregó `avance
   time.Duration` y `marcaDeDispositivo()` a `entornoE2EHTTP`; cada comando
   pide una marca y los tres intentos de entrega reutilizan la misma, que es lo
   que hace el teléfono (sella una vez y reenvía los mismos bytes). Sólo toca
   pruebas: la producción no genera la marca, la recibe.
2. **`GET /v2/garantias/catalogos` no entra en esta entrega.** Es la ruta que
   el teléfono necesita para abrir un folio y quedó fuera de la Parte 1; se
   declara como dependencia de la Parte 2 / tarea 12.
3. **La medición de P2 *antes* del cambio se hace con un archivo de prueba
   temporal** que no se commitea (se agrega al worktree de `7833cf6`, se corre y
   se borra): imprime el código de cada POST y no afirma nada, porque en ese SHA
   la prueba permanente todavía no existe. La medición *después* es la prueba
   permanente de T11, que sí está en el árbol.
4. **En Windows/Git Bash, la compuerta de `infra/storage` no se puede poner
   verde** por dos `t.Skip` de permisos POSIX
   (`filesystem_test.go:422` y `:442`); en Linux, la plataforma del runner,
   da 85.5% y pasa. Está medido en los dos sistemas y explicado en «Verificación».
5. **Tres archivos de producción cambian aunque el brief no los liste:**
   `app/service.go`, `app/cerrar.go` y `app/adjuntar_imagen.go`. El cambio es
   de forma, no de comportamiento: tres `switch { case … }` pasan a
   `if`/`else if` con las mismas condiciones y los mismos cuerpos, para que
   `make test-mutation-garantias-app` pueda evaluar los mutantes de esas
   condiciones (ver «La compuerta de mutación»). No hay una sola rama nueva ni
   un resultado distinto: lo prueba la suite completa, que da lo mismo antes y
   después, y el reporte lo declara aquí porque el brief sólo autorizaba tocar
   producción por P2.