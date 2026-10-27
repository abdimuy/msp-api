# Desempeño de cobradores — API (msp-api) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar en msp-api el módulo de solo lectura `internal/desempeno` (tablero, jornada, actividad de la zona, cuentas de la semana y catálogos, con permiso `desempeno:ver`), las fotos de visitas, el arreglo de pertenencia de la foto de pago y `pago_recibido_id`/`num_fotos` en el detalle de pago de Clientes.

**Architecture:** Vertical slice nuevo `internal/desempeno` (domain → app → ports → infra) que lee Firebird con consultas propias (no toca el sync de cobranza) y obtiene usuarios de Firestore y la cobranza semanal que ya calcula `rutas` a través de un contrato nuevo `internal/rutas/rutas_contracts.go`. El estado por venta es un port fiel de `EstadoCuentaDeriver.kt` validado por un juego de casos JSON compartido con msp-app-kt. Los cambios a `cobranza`, `visitas` y `clientes` son aditivos y van en un carril aparte.

**Tech Stack:** Go 1.25, Firebird 5 (`nakagami/firebirdsql`), Huma v2 sobre chi, fx, `shopspring/decimal`, `golang.org/x/text` (ya en go.mod), testify. Sin dependencias nuevas.

**Spec:** `docs/superpowers/specs/2026-10-14-desempeno-cobradores-design.md` y `docs/superpowers/specs/2026-10-14-desempeno-contratos.md` (**contratos v2**, commit `ceb3640`: son ley, nombres byte a byte). Plan hermano que consume el juego de casos: `docs/superpowers/plans/2026-10-14-desempeno-android-casos.md`.

**Cómo se verificó este plan (2026-10-14):** todo el código de abajo se escribió y se corrió en un worktree desechable sobre `cd89dde` antes de copiarlo aquí: `golangci-lint run ./...` → 0 issues; `go test -short ./...` verde; integración contra la base de desarrollo (`mueblera-firebird`, `WithTestTransaction`, todo revierte) verde en `desempenofb`, `visitasfb`, `clientesfb`, `ventfb` (imágenes), `rutasfb` y `fbcharset`. Cada carril se aplicó también **por separado** sobre su base (Tarea 0 + A; Tarea 0 + B; C sobre `main`) y compila, pasa y lintea solo. Nada se commiteó.

## Global Constraints

- Contrato: claves JSON exactamente las de `2026-10-14-desempeno-contratos.md` v2; `null` explícito, nunca `omitempty`; dinero string con 2 decimales; horas RFC3339 UTC (`"2026-10-14T16:31:00Z"`); días `YYYY-MM-DD` de `America/Mexico_City`.
- Códigos de error: `desempeno_usuario_not_found` (404), `desempeno_usuario_sin_zona` (409), `desempeno_usuario_sin_inicio_semana` (409), `desempeno_parametro_invalido` (422), `desempeno_firestore_unavailable` (503). (`desempeno_export_demasiadas_filas` queda para la fase del xlsx.)
- Permiso `desempeno:ver` en todo `/v2/desempeno/*`; fotos de visitas y de pagos se leen con `cobranza:ver_pagos`.
- `pausa_min` entero 5–240 (default 45); `desde_hora`/`hasta_hora` `HH:MM`, ambos o ninguno, `desde_hora < hasta_hora`.
- `limit` 1–500 (default 100); rango de actividad máximo 92 días; `orden=hora|cliente|monto|tipo|registrado_por`, `dir` default `desc`.
- Dinero del estado por venta: formas {157, 158, 52569}; condonación 137026 no es dinero; pago rechazado no cuenta como dinero pero sí como `con_algo`.
- CLAUDE.md §1: el módulo no escribe; ninguna migración en esta entrega (el índice de `MSP_VISITAS` solo si lo pide la medición del Apéndice 1).
- CLAUDE.md §2: entre módulos solo por contratos (`internal/rutas/rutas_contracts.go`, `internal/auth`); depguard lo vigila.
- CLAUDE.md §3: identificadores y códigos en inglés/snake_case; mensajes al usuario en español, minúsculas, sin punto final.
- CLAUDE.md §7: integración siempre dentro de `fbtestutil.WithTestTransaction`; datos sembrados con `microsipseed`; nombres mexicanos y correos `@muebleriamsp.mx`.
- CLAUDE.md §8: la pantalla muestra dinero y no lo mueve; las cifras se miden en producción (solo lectura) al final (Apéndice 1) y se registran en `docs/evidencia/cobranza.md`.
- No se tocan `pagoFromClause`, `selectPagoColsP` ni el `WHERE` de sync/digest/by-ids (`docs/COBRANZA-SYNC.md`).
- Juego de casos: `testdata/estado-cuenta/NNN-nombre.json` (`^[0-9]{3}-[a-z0-9-]+\.json$`) + `VERSION` = 64 hex minúsculas (con `\n` final opcional) del SHA-256 de los `.json` en orden `LC_ALL=C sort` concatenados sin separador; nada más en el directorio; `.gitattributes` con `testdata/estado-cuenta/** -text`.
- Comandos: `go test ./...`, `golangci-lint run ./...`, `make test-firebird-all` (con `.env` cargado). Nunca `--no-verify`. Commits conventional en español **sin** atribución de Claude.
- Go cache en el disco externo (`GOCACHE`/`GOMODCACHE` ya apuntan a `/Volumes/M2-1TB/.go-cache/`).

## Review Focus

1. **Dos usuarios con el mismo COBRADOR_ID** (pasa hoy en producción, lo documenta `rutas`): una visita sin uid no debe asignarse a uno al azar; aparece para los dos y su `registrado_por.usuario_uid` va `null`. Lo fijan `TestIndiceUsuarios_Completar` ("COBRADOR_ID compartido: no se adivina") y `TestRegistradaPor` (Tarea A2).
2. **Semana que empieza hoy a media jornada** (FECHA_CARGA_INICIAL de hoy 10:00): el tablero de "hoy" no debe perder lo cobrado en la mañana. Lo fija `TestTablero_SemanaQueEmpiezaHoyConsultaDesdeMedianoche` (Tarea A3).
3. **Una fila desaparece entre dos páginas** de la actividad (se cancela un pago): el cursor no debe saltar ni repetir filas. Lo fija `TestActividad_ElCursorSobreviveAQueDesaparezcaSuFila` (Tarea A4).
4. **Firestore caído vs Firebird caído**: el primero es 503 `desempeno_firestore_unavailable`, el segundo no se disfraza de Firestore. Lo fijan `TestService_UsuariosCobradores_FirestoreCaidoEsErrorTipado` (B1) y `TestRutasUsuariosClient_FirestoreCaidoEs503` (B2).
5. **Pedir la foto de otro pago o de otra visita** cambiando solo el id de la ruta: 404 sin un byte del blob, y el borrado no toca nada. Lo fijan `TestHTTP_ObtenerImagenPagoHandler_ImagenDeOtroPagoEs404`, `TestHTTP_EliminarImagenPago_ImagenDeOtroPagoEs404` (C1) y `TestHTTP_ObtenerImagenVisita_DeOtraVisitaEs404` (C2).

---

## Carriles, ramas y orden de merge

Tres agentes en tres worktrees, con archivos **disjuntos**:

| Rama | Tareas | Parte de | Depende de |
|---|---|---|---|
| `feat/desempeno` | **Tarea 0** (corta, primero) y **Tarea F** (al final) | `main` | — |
| `feat/desempeno-a-dominio` | A1 → A2 → A3 → A4 (dominio, casos compartidos, app) | `feat/desempeno` después de la Tarea 0 | Tarea 0 |
| `feat/desempeno-b-repos` | B1 → B2 → B3 → B4 (contrato de rutas, adaptador, repos Firebird) | `feat/desempeno` después de la Tarea 0 | Tarea 0 |
| `feat/desempeno-c-fotos` | C1, C2, C3 (independientes entre sí) | `main` (no necesita la Tarea 0) | — |

```
main ── Tarea 0 ──┬── carril A (A1→A2→A3→A4) ──┐
   │              ├── carril B (B1→B2→B3→B4) ──┼── merge a feat/desempeno ── Tarea F ── PR feat/desempeno → main
   └── carril C (C1, C2, C3) ──────────────────┘
```

Orden de merge a `feat/desempeno` (sin conflictos esperados: ningún archivo se repite entre carriles):
1. `feat/desempeno-c-fotos` (en cuanto esté; no depende de nada).
2. `feat/desempeno-b-repos`.
3. `feat/desempeno-a-dominio`.
4. Tarea F sobre `feat/desempeno` (necesita A y B). Luego un solo PR `feat/desempeno` → `main`.

Archivos compartidos y quién los toca (una sola vez): `.golangci.yml`, `.gitattributes`, `Makefile` → Tarea 0; `internal/platform/fbcharset/fbcharset.go` → B3; `internal/auth/*` (permiso), `cmd/api/*` (wiring y montaje) → Tarea F.

Comando de partida de cada worktree (desde el repo principal):

```bash
git worktree add ../msp-api-desempeno-a -b feat/desempeno-a-dominio feat/desempeno   # tras la Tarea 0
git worktree add ../msp-api-desempeno-b -b feat/desempeno-b-repos   feat/desempeno   # tras la Tarea 0
git worktree add ../msp-api-desempeno-c -b feat/desempeno-c-fotos   main
```

## Decisiones que este plan tomó (el dueño puede revertirlas antes de la Tarea F)

| # | Decisión | Por qué / qué la cambiaría |
|---|---|---|
| D-API-1 | `cobro.pct` = % ponderado de rutas (`CalcularResumenPonderado`); `cobrado` = Σ `AbonoSemana` (concepto 87327); `esperado` = Σ parcialidad de las ventas que aplican esta semana; `falta` = max(0, esperado − cobrado). | El spec §3.1 manda consumir el % de rutas; rutas no calcula "esperado" en pesos. El ejemplo del contrato (8600/13050 = 65.90 %) sugiere pct = cobrado/esperado: **contradicción reportada**. Si el dueño elige cobrado/esperado, se cambia solo `domain/cobro.go`. |
| D-API-2 | `rechazado` en el servidor = fila de `MSP_PAGOS_RECIBIDOS` con `ESTADO='P'` e `INTENTOS > 0` (Microsip lo rechazó al menos una vez, `aplicar_pago.go:87-88`). | El spec define "rechazado" con `DOCTO_CC_ID = -1`, que solo existe en el teléfono. Lo mide la consulta M7 del Apéndice 1. |
| D-API-3 | Id de pago aplicado = `pago:<IMPTE_DOCTO_CC_ID>`; pendiente = `pago:<uuid>`. | El contrato muestra `pago:15082369`; un abono puede repartirse entre cargos, y `IMPTE_DOCTO_CC_ID` es la PK única de `MSP_PAGOS_VENTAS`. |
| D-API-4 | Una visita (sin `CREATED_BY`) se atribuye por `COBRADOR_ID`; si dos usuarios lo comparten, cuenta para los dos y `usuario_uid` va null; el filtro `registrado_por=<uid>` no la encuentra. | `MSP_VISITAS` no guarda quién la creó (solo `COBRADOR_ID`). |
| D-API-5 | La cobertura y los segmentos del tablero usan TODA la actividad de la zona (igual que Cuentas); los conteos `pagos/visitas/pausas` solo lo del usuario. | Spec §2 dice "la fila cuenta solo lo que ese usuario registró" y a la vez "cobertura igual que Cuentas". Elegido para que la pastilla del tablero y la de Cuentas coincidan. |
| D-API-6 | Meses cortos de etiquetas: `ene feb mar abr may jun jul ago sept oct nov dic`. | "sept" está LEÍDO en `EstadoCuentaUiTest.kt:128,150`; el resto es INFERIDO de CLDR es-MX. Hoy el servidor nunca produce fecha de promesa/cita (fase 2), así que no se ve en producción. |
| D-API-7 | `nueva` = la venta se cargó dentro de la semana (`FECHA_CARGO >= inicio`). `producto` = primer artículo (ROL N/J) por `POSICION`. `num_atrasos` = parte entera de `Vencidas` de rutas. | El spec los nombra sin definirlos. |
| D-API-8 | `CuentaComoDinero` (pagos del tablero/jornada y `totales.importe`) = tipo pago, forma 157/158/52569, **aplicado** y no rechazado. En el **estado por venta** un pago pendiente no rechazado sí cuenta (como en el teléfono). | Contrato §4: "solo dinero (formas 157/158/52569, no rechazados)"; spec §6: "no aplicado no cuenta como dinero". |
| D-API-9 | Población del tablero = usuarios de Firestore con `COBRADOR_ID > 0` (los mismos que `rutas.ListarCobradores`), configurados o no. | Spec D1 dice "una fila por usuario de Firestore" sin decir cuáles. |
| D-API-10 | Las respuestas de `/v2/desempeno/*` NO llevan el `"$schema"` que Huma agrega por defecto (`cfg.CreateHooks = nil`). | El contrato fija las claves byte a byte; los demás módulos sí lo llevan. |

---

## Tarea 0: andamiaje, tipos compartidos y puertos (rama `feat/desempeno`, primero)

Fija los tipos y puertos que A y B usan, para que los dos carriles arranquen a la vez sin pisarse.

**Files:**
- Create: `.gitattributes`
- Modify: `.golangci.yml` (alias `desempeno*` y `internal/desempeno/domain` en `domain-pure`)
- Modify: `Makefile` (`test-firebird-all` corre `desempenofb`)
- Create: `internal/desempeno/desempeno_contracts.go`
- Create: `internal/desempeno/domain/{errors,calendario,formas_cobro,actividad,usuario,cuenta_universo}.go`
- Test: `internal/desempeno/domain/{helpers,calendario,formas_cobro,actividad,usuario,cuenta_universo}_test.go`
- Create: `internal/desempeno/ports/outbound/ports.go`; Test: `ports_test.go`
- Create: `internal/desempeno/infra/desempenofb/doc.go`, `fb_test.go`

**Interfaces:**
- Consumes: nada.
- Produces (los usan A, B y F):
  - `domain.Fecha` (`ParseFecha(string) (Fecha, error)`, `NuevaFecha`, `DiaDe(time.Time, *time.Location) Fecha`, métodos `InicioDelDia(loc) time.Time`, `Siguiente()`, `Antes/Despues(Fecha) bool`, `String()`, `Anio/Mes/Dia`, `EsCero`), `domain.HoraDelDia` (`ParseHoraDelDia`, `Minutos()`, `EnDia(Fecha, loc)`, `String()`).
  - Errores `domain.ErrUsuarioNoEncontrado`, `ErrUsuarioSinZona`, `ErrUsuarioSinInicioSemana`, `ErrParametroInvalido`, `ErrFirestoreNoDisponible`.
  - Constantes `FormaCobroEfectivo=157`, `FormaCobroCheque=158`, `FormaCobroTransferencia=52569`, `FormaCobroCondonacion=137026`, `ConceptoCobranzaEnRuta=87327`, `ConceptoCondonaciones=27969`, `EtiquetaFormaCobro(int) string`, `EsFormaDeDinero(int) bool`, `CatalogoFormasCobro() []FormaCobroCatalogo`.
  - `domain.Actividad{ID, Tipo, Hora, RecibidoAt *time.Time, ClienteID, ClienteNombre, VentaID, DoctoPVID, Folio, RegistradoPor, Lat, Lng *float64, NumFotos, Pago *DatosPago, Visita *DatosVisita}` con `ConGPS()`, `CuentaComoDinero()`, `NoAplicado()`, `Importe()`, `CapturaTardia()`; `IDPagoAplicado(int)`, `IDPagoPendiente(string)`, `IDVisita(string)`, `ClasificarTipoPago(forma, concepto int) TipoActividad`, `CapturaTardiaUmbral`.
  - `domain.UsuarioCobranza{UID, Nombre, Email, CobradorID, ZonaID, ZonaNombre, InicioSemana *time.Time}` con `Configuracion() (bool, string)`, `ExigirConfigurado() error`; `domain.CobroVenta`, `domain.CobroSemana`.
  - `domain.CuentaUniverso{VentaID, DoctoPVID, Folio, ClienteID, ClienteNombre, Producto, Parcialidad, Saldo, FechaCargo}` con `EsNueva(time.Time) bool`.
  - Puertos `outbound.UsuariosCobranza` (`ListarUsuarios`, `CobroSemana`), `outbound.ActividadesRepo` (`ActividadesZona(ctx, zonaID int, desde, hasta time.Time)`, rango inclusivo), `outbound.CuentasRepo` (`UniversoSemana(ctx, zonaID int, inicio time.Time)`), `outbound.Clock` + `ProductionClock`.
  - Ayudantes de prueba del paquete `domain_test`: `cdmx(t)`, `dinero(s)`, `f64(v)` (A1/A2 los usan y **no** los redefinen). `requireFBEnv(t)` en `desempenofb_test`.

- [ ] **Step 1: Configuración del repo**

`.golangci.yml` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/.golangci.yml b/.golangci.yml
index e53939e..4964d71 100644
--- a/.golangci.yml
+++ b/.golangci.yml
@@ -360,6 +360,18 @@ linters:
           alias: visitasfb
         - pkg: github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp
           alias: visitashttp
+        - pkg: github.com/abdimuy/msp-api/internal/desempeno/app
+          alias: desempenoapp
+        - pkg: github.com/abdimuy/msp-api/internal/desempeno/domain
+          alias: desempenodomain
+        - pkg: github.com/abdimuy/msp-api/internal/desempeno/ports/outbound
+          alias: desempenooutbound
+        - pkg: github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb
+          alias: desempenofb
+        - pkg: github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp
+          alias: desempenohttp
+        - pkg: github.com/abdimuy/msp-api/internal/desempeno/infra/clients
+          alias: desempenoclients
         - pkg: github.com/abdimuy/msp-api/internal/canal/domain
           alias: canaldomain
         - pkg: github.com/abdimuy/msp-api/internal/canal/app
@@ -440,6 +452,7 @@ linters:
             - github.com/abdimuy/msp-api/internal/asistencia/domain
             - github.com/abdimuy/msp-api/internal/garantias/domain
             - github.com/abdimuy/msp-api/internal/visitas/domain
+            - github.com/abdimuy/msp-api/internal/desempeno/domain
             - github.com/abdimuy/msp-api/internal/flota/domain
             - github.com/abdimuy/msp-api/internal/canal/domain
```

`Makefile` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/Makefile b/Makefile
index d861419..f0af9fb 100644
--- a/Makefile
+++ b/Makefile
@@ -313,6 +313,7 @@ test-firebird-all: ## Run ALL Firebird-backed tests: platform, module repos (aut
 	          ./internal/visitas/infra/visitasfb/... ./internal/visitas/infra/visitashttp/... \
 	          ./internal/clientes/infra/clientesfb/... ./internal/config/infra/configfb/... \
 	          ./internal/rutas/infra/rutasfb/... ./internal/microsip/infra/microsipfb/... \
+	          ./internal/desempeno/infra/desempenofb/... \
 	          -p 1 -race -count=1 -timeout 600s
 
 test-firebird-ventas: ## Run Firebird-backed tests for the venta creation flow (platform + ventas)
```

`.gitattributes` (archivo nuevo, completo):

```
# Juego de casos compartido con msp-app-kt: el SHA-256 de VERSION se calcula
# sobre los bytes crudos; git no debe tocar fines de línea.
testdata/estado-cuenta/** -text
```

`internal/desempeno/desempeno_contracts.go` (archivo nuevo, completo):

```go
// Package desempeno es la superficie entre módulos del contexto "Desempeño de
// cobradores". Hoy no exporta nada: es un módulo de solo lectura que ningún
// otro módulo consume. Si alguno lo necesita, sus tipos van aquí y en ningún
// otro lado (depguard prohíbe importar domain/app/infra de otro módulo).
package desempeno
```


- [ ] **Step 2: Escribir las pruebas que fallan**

`internal/desempeno/domain/helpers_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Ayudantes compartidos por todas las pruebas del dominio (Tarea 0). Las
// tareas A1/A2 los usan y NO los redefinen.

// cdmx es la zona de negocio, America/Mexico_City.
func cdmx(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	return loc
}

func dinero(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func f64(v float64) *float64 { return &v }
```

`internal/desempeno/domain/calendario_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestParseFecha(t *testing.T) {
	t.Parallel()
	f, err := domain.ParseFecha("2026-10-14")
	require.NoError(t, err)
	assert.Equal(t, "2026-10-14", f.String())

	for _, malo := range []string{"", "2026-10-32", "14/10/2026", "2026-10-14T00:00:00Z", "2026-2-1"} {
		_, err := domain.ParseFecha(malo)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "entrada %q", malo)
	}
}

func TestNuevaFecha_RechazaDiasInexistentes(t *testing.T) {
	t.Parallel()
	_, err := domain.NuevaFecha(2026, time.February, 30)
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
	f, err := domain.NuevaFecha(2028, time.February, 29)
	require.NoError(t, err)
	assert.Equal(t, "2028-02-29", f.String())
}

func TestDiaDe_UsaLaZonaDeNegocio(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	// 2026-10-15T05:30Z son las 23:30 del 14 en CDMX (UTC-6).
	assert.Equal(t, "2026-10-14", domain.DiaDe(time.Date(2026, 10, 15, 5, 30, 0, 0, time.UTC), loc).String())
	// 06:00Z ya es medianoche del 15.
	assert.Equal(t, "2026-10-15", domain.DiaDe(time.Date(2026, 10, 15, 6, 0, 0, 0, time.UTC), loc).String())
}

func TestFecha_InicioDelDiaYSiguiente(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	f, err := domain.ParseFecha("2026-12-31")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 12, 31, 6, 0, 0, 0, time.UTC), f.InicioDelDia(loc))
	assert.Equal(t, "2027-01-01", f.Siguiente().String())
	assert.True(t, f.Antes(f.Siguiente()))
	assert.True(t, f.Siguiente().Despues(f))
	assert.False(t, f.Antes(f))
	assert.False(t, f.EsCero())
	assert.True(t, domain.Fecha{}.EsCero())
}

func TestParseHoraDelDia(t *testing.T) {
	t.Parallel()
	h, err := domain.ParseHoraDelDia("08:05")
	require.NoError(t, err)
	assert.Equal(t, 8*60+5, h.Minutos())
	assert.Equal(t, "08:05", h.String())

	for _, malo := range []string{"", "8:05", "24:00", "12:60", "08:05:00", "0805"} {
		_, err := domain.ParseHoraDelDia(malo)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "entrada %q", malo)
	}
}

func TestHoraDelDia_EnDia(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	f, _ := domain.ParseFecha("2026-10-14")
	h, _ := domain.ParseHoraDelDia("18:00")
	assert.Equal(t, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), h.EnDia(f, loc))
}
```

`internal/desempeno/domain/formas_cobro_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestEtiquetaFormaCobro(t *testing.T) {
	t.Parallel()
	casos := map[int]string{
		157:    "efectivo",
		52569:  "transferencia",
		158:    "cheque",
		137026: "condonacion",
		0:      "otra",
		67:     "otra", // "Efectivo" del catálogo FORMAS_COBRO, que miente (E-COB-017)
	}
	for id, esperado := range casos {
		assert.Equal(t, esperado, domain.EtiquetaFormaCobro(id), "forma %d", id)
	}
}

func TestEsFormaDeDinero(t *testing.T) {
	t.Parallel()
	for _, id := range []int{157, 158, 52569} {
		assert.True(t, domain.EsFormaDeDinero(id), "forma %d es dinero", id)
	}
	for _, id := range []int{137026, 0, 67, 68, 71, 999} {
		assert.False(t, domain.EsFormaDeDinero(id), "forma %d no es dinero", id)
	}
}

func TestCatalogoFormasCobro_CodigosDelFiltro(t *testing.T) {
	t.Parallel()
	got := domain.CatalogoFormasCobro()
	codigos := make([]string, 0, len(got))
	for _, f := range got {
		codigos = append(codigos, f.Codigo)
		assert.NotEmpty(t, f.Etiqueta)
	}
	assert.Equal(t, []string{"efectivo", "transferencia", "cheque", "condonacion", "otra"}, codigos)
}
```

`internal/desempeno/domain/actividad_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestClasificarTipoPago(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.TipoCondonacion, domain.ClasificarTipoPago(137026, 87327))
	assert.Equal(t, domain.TipoCondonacion, domain.ClasificarTipoPago(157, 27969))
	assert.Equal(t, domain.TipoPago, domain.ClasificarTipoPago(157, 87327))
	assert.Equal(t, domain.TipoPago, domain.ClasificarTipoPago(0, 87327))
}

func TestActividad_ConGPS(t *testing.T) {
	t.Parallel()
	assert.False(t, domain.Actividad{}.ConGPS(), "sin coordenadas")
	assert.False(t, domain.Actividad{Lat: f64(0), Lng: f64(0)}.ConGPS(), "centinela 0,0")
	assert.False(t, domain.Actividad{Lat: f64(18.46)}.ConGPS(), "solo latitud")
	assert.True(t, domain.Actividad{Lat: f64(18.46), Lng: f64(-97.39)}.ConGPS())
	assert.True(t, domain.Actividad{Lat: f64(0), Lng: f64(-97.39)}.ConGPS(), "solo uno en cero sí es dato")
}

func TestActividad_DineroYNoAplicado(t *testing.T) {
	t.Parallel()
	pago := func(aplicado, rechazado bool) domain.Actividad {
		return domain.Actividad{Tipo: domain.TipoPago, Pago: &domain.DatosPago{
			Importe: dinero("150"), FormaCobroID: domain.FormaCobroEfectivo, Aplicado: aplicado, Rechazado: rechazado,
		}}
	}
	assert.True(t, pago(true, false).CuentaComoDinero())
	assert.False(t, pago(true, false).NoAplicado())
	assert.False(t, pago(false, false).CuentaComoDinero())
	assert.True(t, pago(false, false).NoAplicado())
	assert.False(t, pago(false, true).CuentaComoDinero())
	assert.True(t, pago(false, true).NoAplicado())
	otra := pago(true, false)
	otra.Pago.FormaCobroID = 999
	assert.False(t, otra.CuentaComoDinero(), "forma desconocida no es dinero (contrato §4)")
	cond := domain.Actividad{Tipo: domain.TipoCondonacion, Pago: &domain.DatosPago{Importe: dinero("150"), Aplicado: true}}
	assert.False(t, cond.CuentaComoDinero(), "la condonación no es dinero")
	assert.False(t, cond.NoAplicado())
	assert.True(t, domain.Actividad{Tipo: domain.TipoVisita}.Importe().IsZero())
}

func TestIDs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "pago:15082369", domain.IDPagoAplicado(15082369))
	assert.Equal(t, "pago:7a1f39e4-0000-0000-0000-000000000000", domain.IDPagoPendiente("7a1f39e4-0000-0000-0000-000000000000"))
	assert.Equal(t, "visita:412cc2fc", domain.IDVisita("412cc2fc"))
}

func TestActividad_CapturaTardia(t *testing.T) {
	t.Parallel()
	h := time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)
	en := func(d time.Duration) *time.Time { r := h.Add(d); return &r }
	assert.False(t, domain.Actividad{Hora: h}.CapturaTardia(), "sin recibido_at")
	assert.False(t, domain.Actividad{Hora: h, RecibidoAt: en(2*time.Hour - time.Second)}.CapturaTardia())
	assert.True(t, domain.Actividad{Hora: h, RecibidoAt: en(2 * time.Hour)}.CapturaTardia(), "2 h exactas")
}
```

`internal/desempeno/domain/usuario_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestUsuarioCobranza_Configuracion(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	ok, motivo := domain.UsuarioCobranza{ZonaID: 21563, InicioSemana: &inicio}.Configuracion()
	assert.True(t, ok)
	assert.Empty(t, motivo)

	ok, motivo = domain.UsuarioCobranza{ZonaID: 0, InicioSemana: nil}.Configuracion()
	assert.False(t, ok)
	assert.Equal(t, "sin_zona", motivo, "la zona se reporta primero")

	ok, motivo = domain.UsuarioCobranza{ZonaID: 21563}.Configuracion()
	assert.False(t, ok)
	assert.Equal(t, "sin_inicio_semana", motivo)

	require.ErrorIs(t, domain.UsuarioCobranza{}.ExigirConfigurado(), domain.ErrUsuarioSinZona)
	require.ErrorIs(t, domain.UsuarioCobranza{ZonaID: 1}.ExigirConfigurado(), domain.ErrUsuarioSinInicioSemana)
	require.NoError(t, domain.UsuarioCobranza{ZonaID: 1, InicioSemana: &inicio}.ExigirConfigurado())
}
```

`internal/desempeno/domain/cuenta_universo_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestCuentaUniverso_EsNueva(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	assert.True(t, domain.CuentaUniverso{FechaCargo: inicio}.EsNueva(inicio), "cargada justo al inicio")
	assert.True(t, domain.CuentaUniverso{FechaCargo: inicio.Add(time.Hour)}.EsNueva(inicio))
	assert.False(t, domain.CuentaUniverso{FechaCargo: inicio.Add(-time.Second)}.EsNueva(inicio))
}
```

`internal/desempeno/ports/outbound/ports_test.go` (archivo nuevo, completo):

```go
package outbound_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
)

func TestProductionClock_DevuelveUTC(t *testing.T) {
	t.Parallel()
	antes := time.Now().UTC()
	got := outbound.ProductionClock{}.Now()
	assert.Equal(t, time.UTC, got.Location())
	assert.WithinDuration(t, antes, got, time.Second)
}
```

`internal/desempeno/infra/desempenofb/fb_test.go` (archivo nuevo, completo):

```go
package desempenofb_test

import (
	"os"
	"testing"
)

// requireFBEnv salta la prueba si no hay base Firebird (CI corre sin
// FB_DATABASE; CLAUDE.md §7).
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}
```


- [ ] **Step 3: Correr y ver que fallan**

Run: `go test ./internal/desempeno/...`
Expected: FAIL de compilación (`undefined: domain.ParseFecha`, `undefined: outbound.ProductionClock`, …).

- [ ] **Step 4: Implementación**

`internal/desempeno/domain/errors.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "github.com/abdimuy/msp-api/internal/platform/apperror"

// Errores del módulo. Los códigos son los del contrato §0
// (docs/superpowers/specs/2026-10-14-desempeno-contratos.md) byte a byte.
var (
	// ErrUsuarioNoEncontrado: el uid no existe en Firestore `users` (o no es cobrador).
	ErrUsuarioNoEncontrado = apperror.NewNotFound(
		"desempeno_usuario_not_found",
		"el usuario no existe o no es cobrador",
	)
	// ErrUsuarioSinZona: el usuario no tiene ZONA_CLIENTE_ID en Firestore.
	ErrUsuarioSinZona = apperror.NewConflict(
		"desempeno_usuario_sin_zona",
		"el usuario no tiene zona asignada",
	)
	// ErrUsuarioSinInicioSemana: el usuario no tiene FECHA_CARGA_INICIAL.
	ErrUsuarioSinInicioSemana = apperror.NewConflict(
		"desempeno_usuario_sin_inicio_semana",
		"el usuario no tiene inicio de semana",
	)
	// ErrParametroInvalido: cualquier parámetro de consulta fuera de contrato.
	// El nombre del parámetro viaja en WithField("parametro", ...).
	ErrParametroInvalido = apperror.NewValidation(
		"desempeno_parametro_invalido",
		"parámetro inválido",
	)
	// ErrFirestoreNoDisponible: no se pudo leer `users`. Nunca se inventan
	// zonas ni semanas (spec §6).
	ErrFirestoreNoDisponible = apperror.NewServiceUnavailable(
		"desempeno_firestore_unavailable",
		"no se pudo consultar a los usuarios, intente de nuevo",
	)
)
```

`internal/desempeno/domain/calendario.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"fmt"
	"time"
)

// Fecha es un día del calendario civil, sin hora ni zona. Se usa para el
// `fecha` de la jornada, para el `dia` de la línea y para las fechas de cita y
// promesa del juego de casos. Nunca se compara contra un time.Time sin pasar
// por DiaDe/InicioDelDia con la zona de negocio explícita.
type Fecha struct {
	anio int
	mes  time.Month
	dia  int
}

// NuevaFecha construye una Fecha y rechaza días que no existen (30 de febrero).
func NuevaFecha(anio int, mes time.Month, dia int) (Fecha, error) {
	t := time.Date(anio, mes, dia, 0, 0, 0, 0, time.UTC)
	if t.Year() != anio || t.Month() != mes || t.Day() != dia {
		return Fecha{}, ErrParametroInvalido.WithField("fecha", fmt.Sprintf("%04d-%02d-%02d", anio, mes, dia))
	}
	return Fecha{anio: anio, mes: mes, dia: dia}, nil
}

// ParseFecha lee "YYYY-MM-DD" (contrato §0). Cualquier otra forma es
// ErrParametroInvalido.
func ParseFecha(s string) (Fecha, error) {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return Fecha{}, ErrParametroInvalido.WithField("fecha", s).WithError(err)
	}
	return Fecha{anio: t.Year(), mes: t.Month(), dia: t.Day()}, nil
}

// DiaDe devuelve el día del calendario en loc al que pertenece el instante t.
func DiaDe(t time.Time, loc *time.Location) Fecha {
	l := t.In(loc)
	return Fecha{anio: l.Year(), mes: l.Month(), dia: l.Day()}
}

// InicioDelDia es el instante (UTC) de las 00:00 de f en loc.
func (f Fecha) InicioDelDia(loc *time.Location) time.Time {
	return time.Date(f.anio, f.mes, f.dia, 0, 0, 0, 0, loc).UTC()
}

// Siguiente es el día calendario posterior.
func (f Fecha) Siguiente() Fecha {
	t := time.Date(f.anio, f.mes, f.dia+1, 0, 0, 0, 0, time.UTC)
	return Fecha{anio: t.Year(), mes: t.Month(), dia: t.Day()}
}

// Anio del día.
func (f Fecha) Anio() int { return f.anio }

// Mes del día.
func (f Fecha) Mes() time.Month { return f.mes }

// Dia del mes.
func (f Fecha) Dia() int { return f.dia }

// EsCero dice si es el valor cero (sin fecha).
func (f Fecha) EsCero() bool { return f.anio == 0 }

// String devuelve "YYYY-MM-DD".
func (f Fecha) String() string { return fmt.Sprintf("%04d-%02d-%02d", f.anio, f.mes, f.dia) }

func (f Fecha) clave() int { return f.anio*10000 + int(f.mes)*100 + f.dia }

// Antes dice si f es un día anterior a o.
func (f Fecha) Antes(o Fecha) bool { return f.clave() < o.clave() }

// Despues dice si f es un día posterior a o.
func (f Fecha) Despues(o Fecha) bool { return f.clave() > o.clave() }

// HoraDelDia es una hora de reloj "HH:MM" sin día ni zona: el horario opcional
// de la jornada (`desde_hora`, `hasta_hora`) y la hora de una cita.
type HoraDelDia struct {
	hora   int
	minuto int
}

// ParseHoraDelDia lee exactamente "HH:MM" con 00 ≤ HH ≤ 23 y 00 ≤ MM ≤ 59.
func ParseHoraDelDia(s string) (HoraDelDia, error) {
	if len(s) != len("15:04") {
		return HoraDelDia{}, ErrParametroInvalido.WithField("hora", s)
	}
	t, err := time.Parse("15:04", s)
	if err != nil {
		return HoraDelDia{}, ErrParametroInvalido.WithField("hora", s).WithError(err)
	}
	return HoraDelDia{hora: t.Hour(), minuto: t.Minute()}, nil
}

// Minutos desde la medianoche.
func (h HoraDelDia) Minutos() int { return h.hora*60 + h.minuto }

// EnDia es el instante (UTC) de h en el día f de loc.
func (h HoraDelDia) EnDia(f Fecha, loc *time.Location) time.Time {
	return time.Date(f.anio, f.mes, f.dia, h.hora, h.minuto, 0, 0, loc).UTC()
}

// String devuelve "HH:MM".
func (h HoraDelDia) String() string { return fmt.Sprintf("%02d:%02d", h.hora, h.minuto) }
```

`internal/desempeno/domain/formas_cobro.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// Formas de cobro de los pagos de cobranza. Los IDs NO salen del catálogo
// FORMAS_COBRO (que miente: E-COB-017) sino de FORMAS_COBRO_DOCTOS cruzado con
// los pagos reales; coinciden con msp-app-kt VentanaCobro.FORMAS_COBRO_COBRANZA
// y PaymentDao.kt:17-23.
const (
	FormaCobroEfectivo      = 157
	FormaCobroCheque        = 158
	FormaCobroTransferencia = 52569
	// FormaCobroCondonacion es "Condonacion" en FORMAS_COBRO_CC; la app la
	// manda con el concepto 27969 (Condonaciones). No es dinero.
	FormaCobroCondonacion = 137026
)

// Conceptos de Microsip que viajan al teléfono (pagoConceptoFilter del sync).
// Los nombres son los de CONCEPTOS_CC, la autoridad (COBRANZA-SYNC.md §7.2):
// 27969 es "Condonaciones", no "abono mostrador".
const (
	ConceptoCobranzaEnRuta = 87327
	ConceptoCondonaciones  = 27969
)

// Etiquetas de forma de cobro del contrato §1 (`pago.forma_cobro`).
const (
	EtiquetaEfectivo      = "efectivo"
	EtiquetaTransferencia = "transferencia"
	EtiquetaCheque        = "cheque"
	EtiquetaCondonacion   = "condonacion"
	EtiquetaOtra          = "otra"
)

// EtiquetaFormaCobro traduce el ID al literal del contrato. Todo lo que no sea
// una de las cuatro formas conocidas es "otra" (incluido 0 = sin dato).
func EtiquetaFormaCobro(formaCobroID int) string {
	switch formaCobroID {
	case FormaCobroEfectivo:
		return EtiquetaEfectivo
	case FormaCobroTransferencia:
		return EtiquetaTransferencia
	case FormaCobroCheque:
		return EtiquetaCheque
	case FormaCobroCondonacion:
		return EtiquetaCondonacion
	default:
		return EtiquetaOtra
	}
}

// EsFormaDeDinero dice si la forma cuenta como dinero cobrado en el estado por
// venta (spec §2: 157, 158, 52569). La condonación y "otra" no.
func EsFormaDeDinero(formaCobroID int) bool {
	switch formaCobroID {
	case FormaCobroEfectivo, FormaCobroCheque, FormaCobroTransferencia:
		return true
	default:
		return false
	}
}

// FormaCobroCatalogo es una entrada de GET /desempeno/catalogos (contrato §4b).
type FormaCobroCatalogo struct {
	Codigo   string
	Etiqueta string
}

// CatalogoFormasCobro lista los códigos que acepta el filtro `forma_cobro`, en el
// orden en que se ofrecen.
func CatalogoFormasCobro() []FormaCobroCatalogo {
	return []FormaCobroCatalogo{
		{EtiquetaEfectivo, "Efectivo"},
		{EtiquetaTransferencia, "Transferencia"},
		{EtiquetaCheque, "Cheque"},
		{EtiquetaCondonacion, "Condonación"},
		{EtiquetaOtra, "Otra"},
	}
}
```

`internal/desempeno/domain/actividad.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// TipoActividad es uno de los tres tipos del contrato §1.
type TipoActividad string

// Tipos de actividad.
const (
	TipoPago        TipoActividad = "pago"
	TipoVisita      TipoActividad = "visita"
	TipoCondonacion TipoActividad = "condonacion"
)

// OrigenRegistro es "app" (capturado en el teléfono) u "oficina" (capturado
// directo en Microsip, sin fila en MSP_PAGOS_RECIBIDOS).
type OrigenRegistro string

// Orígenes.
const (
	OrigenApp     OrigenRegistro = "app"
	OrigenOficina OrigenRegistro = "oficina"
)

// NombreOficina es lo que muestra `registrado_por.nombre` para origen oficina.
const NombreOficina = "Oficina"

// RegistradoPor es quién registró la actividad (spec §2 "Quién lo registró").
// UsuarioUID vacío = no se pudo resolver; CobradorID 0 = sin dato.
type RegistradoPor struct {
	UsuarioUID string
	CobradorID int
	Nombre     string
	Origen     OrigenRegistro
}

// DatosPago acompaña a una actividad de tipo pago o condonación.
type DatosPago struct {
	Importe      decimal.Decimal
	FormaCobroID int
	// Aplicado: el pago ya está en Microsip (fila en MSP_PAGOS_VENTAS).
	Aplicado bool
	// Rechazado: MSP_PAGOS_RECIBIDOS en 'P' con INTENTOS > 0 — Microsip lo
	// rechazó al menos una vez (aplicar_pago.go:87-88). Ver la decisión D-API-2.
	Rechazado      bool
	DoctoCCID      int    // 0 si no está aplicado
	PagoRecibidoID string // "" si no hay fila en MSP_PAGOS_RECIBIDOS
}

// DatosVisita acompaña a una actividad de tipo visita.
type DatosVisita struct {
	VisitaID   string
	TipoVisita string
	Nota       string // "" se serializa como null
}

// Actividad es un pago, una visita o una condonación (contrato §1).
type Actividad struct {
	ID            string
	Tipo          TipoActividad
	Hora          time.Time
	RecibidoAt    *time.Time
	ClienteID     int
	ClienteNombre string
	VentaID       int    // 0 = sin venta ligada
	DoctoPVID     int    // DOCTO_PV_ID de la venta (puente DOCTOS_ENTRE_SIS); 0 = sin dato
	Folio         string // folio de la venta (DOCTOS_PV.FOLIO); "" = sin dato
	RegistradoPor RegistradoPor
	Lat           *float64
	Lng           *float64
	NumFotos      int
	Pago          *DatosPago
	Visita        *DatosVisita
}

// CapturaTardiaUmbral marca que recibido_at − hora ≥ 2 h es captura tardía (spec §4.2).
const CapturaTardiaUmbral = 2 * time.Hour

// IDPagoAplicado es el id estable de un pago en Microsip: el
// IMPTE_DOCTO_CC_ID (PK de MSP_PAGOS_VENTAS, único aunque un abono se reparta
// entre varios cargos). Ver la decisión D-API-3.
func IDPagoAplicado(impteDoctoCCID int) string { return "pago:" + strconv.Itoa(impteDoctoCCID) }

// IDPagoPendiente es el id de un pago que aún no llega a Microsip: su UUID de
// MSP_PAGOS_RECIBIDOS.
func IDPagoPendiente(pagoRecibidoID string) string { return "pago:" + pagoRecibidoID }

// IDVisita es el id de una visita: su UUID de MSP_VISITAS.
func IDVisita(visitaID string) string { return "visita:" + visitaID }

// ClasificarTipoPago devuelve condonación si la forma es 137026 o el concepto 27969
// (Condonaciones en CONCEPTOS_CC); si no, pago. C0 mide que las dos señales
// coinciden en producción (consulta 3 de desempeno-c0.sql).
func ClasificarTipoPago(formaCobroID, conceptoCCID int) TipoActividad {
	if formaCobroID == FormaCobroCondonacion || conceptoCCID == ConceptoCondonaciones {
		return TipoCondonacion
	}
	return TipoPago
}

// ConGPS dice si hay coordenadas y no son el centinela (0,0) (spec §6).
func (a Actividad) ConGPS() bool {
	if a.Lat == nil || a.Lng == nil {
		return false
	}
	return *a.Lat != 0 || *a.Lng != 0
}

// CuentaComoDinero dice si es dinero cobrado: un pago (no condonación) con
// forma 157/158/52569, no rechazado (contrato §4 `totales.importe`) y aplicado
// (spec §6: "no aplicado o rechazado no cuenta como dinero"). Es el predicado
// de `pagos {n, importe}` del tablero, la jornada, los tramos y la hora.
func (a Actividad) CuentaComoDinero() bool {
	return a.Tipo == TipoPago && a.Pago != nil && EsFormaDeDinero(a.Pago.FormaCobroID) &&
		a.Pago.Aplicado && !a.Pago.Rechazado
}

// CapturaTardia dice si llegó al servidor ≥ 2 h después de la hora del teléfono
// (contrato §1 `captura_tardia`). Sin recibido_at no se puede saber: false.
func (a Actividad) CapturaTardia() bool {
	return a.RecibidoAt != nil && a.RecibidoAt.Sub(a.Hora) >= CapturaTardiaUmbral
}

// NoAplicado dice si es un pago (no condonación) que no cuenta como dinero.
func (a Actividad) NoAplicado() bool {
	return a.Tipo == TipoPago && a.Pago != nil && (!a.Pago.Aplicado || a.Pago.Rechazado)
}

// Importe devuelve el importe del pago o condonación; cero para visitas.
func (a Actividad) Importe() decimal.Decimal {
	if a.Pago == nil {
		return decimal.Zero
	}
	return a.Pago.Importe
}
```

`internal/desempeno/domain/usuario.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// UsuarioCobranza es un usuario de Firestore `users` con COBRADOR_ID > 0,
// visto por desempeño (lo provee el contrato de rutas). Una fila del tablero
// por cada uno (D1).
type UsuarioCobranza struct {
	UID          string
	Nombre       string
	Email        string
	CobradorID   int
	ZonaID       int // 0 = sin zona
	ZonaNombre   string
	InicioSemana *time.Time // nil = sin FECHA_CARGA_INICIAL
}

// Motivos de "sin configurar" (contrato §2 `motivo_no_configurado`).
const (
	MotivoSinZona         = "sin_zona"
	MotivoSinInicioSemana = "sin_inicio_semana"
)

// Configuracion dice configurado si tiene zona e inicio de semana; si no, el
// motivo (la zona se reporta primero).
func (u UsuarioCobranza) Configuracion() (bool, string) {
	switch {
	case u.ZonaID <= 0:
		return false, MotivoSinZona
	case u.InicioSemana == nil:
		return false, MotivoSinInicioSemana
	default:
		return true, ""
	}
}

// ExigirConfigurado traduce Configuracion a los errores 409 del contrato.
func (u UsuarioCobranza) ExigirConfigurado() error {
	ok, motivo := u.Configuracion()
	if ok {
		return nil
	}
	if motivo == MotivoSinZona {
		return ErrUsuarioSinZona
	}
	return ErrUsuarioSinInicioSemana
}

// CobroVenta es lo que rutas ya calcula por venta (se consume, no se
// reimplementa: spec §3.1).
type CobroVenta struct {
	VentaID         int
	Parcialidad     decimal.Decimal
	AbonoSemana     decimal.Decimal
	Vencidas        decimal.Decimal
	AplicaPonderado bool
}

// CobroSemana es el desglose de rutas para la semana del usuario.
type CobroSemana struct {
	Ventas       []CobroVenta
	PctPonderado *decimal.Decimal
}
```

`internal/desempeno/domain/cuenta_universo.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// CuentaUniverso es una venta del universo de la semana: crédito (no contado),
// de la zona, no cancelada, con saldo > 0 o con FECHA_ULT_PAGO dentro de la
// semana (el mismo predicado de ventana que el sync del teléfono,
// COBRANZA-SYNC.md §5, con desde = inicio de semana).
type CuentaUniverso struct {
	VentaID       int    // MSP_SALDOS_VENTAS.DOCTO_CC_ID (cargo)
	DoctoPVID     int    // 0 = no se resolvió el puente DOCTOS_ENTRE_SIS
	Folio         string // DOCTOS_PV.FOLIO; "" = sin dato
	ClienteID     int
	ClienteNombre string
	Producto      string // primer artículo (ROL N/J) por POSICION; "" = sin dato
	Parcialidad   decimal.Decimal
	Saldo         decimal.Decimal
	FechaCargo    time.Time
}

// EsNueva dice si la venta se cargó dentro de la semana (decisión D-API-7:
// el spec nombra `nueva` sin definirla).
func (c CuentaUniverso) EsNueva(inicioSemana time.Time) bool {
	return !c.FechaCargo.Before(inicioSemana)
}
```

`internal/desempeno/ports/outbound/ports.go` (archivo nuevo, completo):

```go
// Package outbound declara lo que el módulo desempeno necesita de afuera. Las
// implementaciones viven en internal/desempeno/infra/* y se arman en cmd/api.
//
//nolint:misspell // vocabulario español por convención del proyecto.
package outbound

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// UsuariosCobranza lee los usuarios de Firestore y la cobranza semanal que ya
// calcula rutas (vía su contrato; implementación en infra/clients).
type UsuariosCobranza interface {
	// ListarUsuarios: todos los usuarios con COBRADOR_ID > 0, configurados o
	// no. Firestore caído → domain.ErrFirestoreNoDisponible.
	ListarUsuarios(ctx context.Context) ([]domain.UsuarioCobranza, error)
	// CobroSemana: desglose de rutas sobre la semana del usuario.
	CobroSemana(ctx context.Context, u domain.UsuarioCobranza) (domain.CobroSemana, error)
}

// ActividadesRepo lee pagos, condonaciones y visitas de una zona. Consultas
// propias: no usa ni modifica las del sync (COBRANZA-SYNC.md).
type ActividadesRepo interface {
	// ActividadesZona devuelve las actividades con hora en [desde, hasta]
	// (ambos incluidos) de la zona, en cualquier orden. RegistradoPor viene a
	// medio llenar (uid por CREATED_BY, COBRADOR_ID y nombre crudo); el
	// servicio lo completa con domain.IndiceUsuarios.
	ActividadesZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]domain.Actividad, error)
}

// CuentasRepo lee el universo de ventas de la semana.
type CuentasRepo interface {
	UniversoSemana(ctx context.Context, zonaID int, inicioSemana time.Time) ([]domain.CuentaUniverso, error)
}

// Clock abstrae time.Now() para las pruebas.
type Clock interface {
	Now() time.Time
}

// ProductionClock devuelve la hora actual en UTC.
type ProductionClock struct{}

// Now devuelve time.Now().UTC().
func (ProductionClock) Now() time.Time { return time.Now().UTC() }
```

`internal/desempeno/infra/desempenofb/doc.go` (archivo nuevo, completo):

```go
// Package desempenofb implementa los puertos de lectura del módulo desempeno
// contra Firebird. Solo lee. Sus consultas son PROPIAS: no usa ni modifica
// pagoFromClause, selectPagoColsP ni el WHERE de sync/digest/by-ids
// (docs/COBRANZA-SYNC.md).
//
// Fechas: firebird.ToWallClock al enlazar y firebird.ScanUTCTime al leer
// (DATETIME_HANDLING.md). Texto: el escaneo depende del charset DE LA COLUMNA
// (ENCODING_HANDLING.md), anotado en cada consulta.
package desempenofb
```


- [ ] **Step 5: Correr y ver que pasan; lint**

Run: `go test ./internal/desempeno/... && golangci-lint run ./internal/desempeno/... && make -n test-firebird-all | grep desempenofb`
Expected: `ok` en `domain` y `ports/outbound`; `0 issues.`; la línea de `make -n` muestra `./internal/desempeno/infra/desempenofb/...`.

- [ ] **Step 6: Commit (en `feat/desempeno`)**

```bash
git checkout -b feat/desempeno main
git add .gitattributes .golangci.yml Makefile internal/desempeno
git commit -m "feat(desempeno): andamiaje del módulo, tipos de dominio compartidos y puertos"
```

---

## Carril A — dominio, casos compartidos y casos de uso (rama `feat/desempeno-a-dominio`)

### Tarea A1: estado por venta (port del deriver) y juego de casos compartido

**Files:**
- Create: `internal/desempeno/domain/estado_cuenta.go`, `tipo_visita.go`, `deriver.go`, `presentacion.go`
- Test: `internal/desempeno/domain/estado_cuenta_test.go`, `deriver_test.go`, `presentacion_test.go`, `casos_compartidos_test.go`
- Create: `testdata/estado-cuenta/001-…040-*.json` (40 casos) y `testdata/estado-cuenta/VERSION`

**Interfaces:**
- Consumes: Tarea 0 (`ErrParametroInvalido`, `EsFormaDeDinero`, `FormaCobroCondonacion`, `Fecha`, `HoraDelDia`, ayudantes `dinero`).
- Produces: `EstadoCuenta` (8 códigos), `Alcance`, `Trato` (8), `Segmento` (4), `EstadosCuenta()`, `ParseEstadoCuenta`, constantes `TipoVisita*` (14 literales), `EsTipoVisitaConocido`, `EstadoDeTipoVisita(lit, tieneCita) EstadoCuenta`, `AlcanceDeTipoVisita`, `GrupoDeEstado`, `CatalogoTiposVisita() []TipoVisitaCatalogo{Literal, Estado, Grupo}`; `Ventana` (`NuevaVentana`, `Contiene`, `Inicio`, `Fin`), `CuentaPeriodo`, `PagoPeriodo{…, Rechazado, ActividadID}`, `VisitaPeriodo{…, VentaID *int, FechaPromesa/FechaCita *Fecha, HoraCita *HoraDelDia, ActividadID}`, `ResultadoCuenta{Estado, AbonoVentana, Parcialidad, FechaPromesa, FechaCita, HoraCita, ConAlgo, DeterminadoPor, UltimaActividadID, UltimaActividadHora}`, `Derivacion{PorVenta map[int]ResultadoCuenta, Incidencias}`, `Derivar(cuentas, pagos, visitas, ventana) Derivacion`, `EstadoPorDinero`, `TratoDe`, `EtiquetaDe`, `DetalleDe`, `SegmentoDe(r, hoy Fecha) (Segmento, bool)`.

Reglas que este port copia de msp-app-kt (LEÍDO 2026-10-14) y del plan Android: el dinero manda (`abono <= 0` → sin dinero; `parcialidad <= 0` → `abono_parcial`; `abono >= parcialidad` → `pago`, con `>=`); la visita más reciente aplicable (alcance venta con esa venta + alcance cliente del cliente; en empate gana la de venta; entre iguales, la primera de la entrada); día de cita gana al literal (→ `cita_a_una_hora`, alcance cliente); literal fuera de catálogo → `visite_vuelvo` de alcance venta; visita de alcance venta sin venta ligada no cuenta; `rechazado` se excluye del abono pero cuenta para `con_algo`; condonación (137026) no es dinero pero sí `con_algo`; `monto_prometido` no existe en el contrato y no afecta nada.

- [ ] **Step 1: Escribir las pruebas que fallan**

`internal/desempeno/domain/estado_cuenta_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestEstadosCuenta_SonLosOchoCodigosDelContrato(t *testing.T) {
	t.Parallel()
	codigos := []string{
		"pago", "abono_parcial", "visite_vuelvo", "prometio_proxima",
		"se_nego", "cita_a_una_hora", "no_estaba", "sin_tocar",
	}
	got := make([]string, 0, 8)
	for _, e := range domain.EstadosCuenta() {
		got = append(got, string(e))
	}
	assert.Equal(t, codigos, got)
	for _, c := range codigos {
		e, err := domain.ParseEstadoCuenta(c)
		require.NoError(t, err)
		assert.Equal(t, c, string(e))
	}
	_, err := domain.ParseEstadoCuenta("NO_ESTABA")
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
}

func TestEstadoCuenta_Alcance(t *testing.T) {
	t.Parallel()
	for _, e := range domain.EstadosCuenta() {
		esperado := domain.AlcanceVenta
		if e == domain.EstadoCitaAUnaHora || e == domain.EstadoNoEstaba {
			esperado = domain.AlcanceCliente
		}
		assert.Equal(t, esperado, e.Alcance(), "estado %s", e)
	}
}

func TestEstadoDeTipoVisita_CatalogoCompleto(t *testing.T) {
	t.Parallel()
	casos := map[string]domain.EstadoCuenta{
		"No se encontraba":                 domain.EstadoNoEstaba,
		"Casa cerrada con candado":         domain.EstadoNoEstaba,
		"Solo había menores":               domain.EstadoNoEstaba,
		"Se asomó pero no salió":           domain.EstadoVisiteVuelvo,
		"No responde aunque está":          domain.EstadoVisiteVuelvo,
		"Se escuchan ruidos pero no abre":  domain.EstadoVisiteVuelvo,
		"Pidió que regrese otro día":       domain.EstadoVisiteVuelvo,
		"No pagará esta ocasión":           domain.EstadoSeNego,
		"Tiene dinero pero no quiso pagar": domain.EstadoSeNego,
		"Fue grosero o agresivo":           domain.EstadoSeNego,
		"Pidió reagendar visita":           domain.EstadoPrometioProxima,
		"No va a dar pago":                 domain.EstadoSeNego,
		"Dijo que no va a pagar":           domain.EstadoSeNego,
		"Se esconde y no sale":             domain.EstadoVisiteVuelvo,
	}
	require.Len(t, casos, 14, "el catálogo cerrado tiene 14 literales")
	for lit, esperado := range casos {
		assert.True(t, domain.EsTipoVisitaConocido(lit), "%q es conocido", lit)
		assert.Equal(t, esperado, domain.EstadoDeTipoVisita(lit, false), "literal %q", lit)
		assert.Equal(t, domain.EstadoCitaAUnaHora, domain.EstadoDeTipoVisita(lit, true),
			"con día de cita %q es cita", lit)
	}
}

func TestEstadoDeTipoVisita_FueraDeCatalogo(t *testing.T) {
	t.Parallel()
	for _, lit := range []string{"", "no se encontraba", "Pidio reagendar visita", "texto libre"} {
		assert.False(t, domain.EsTipoVisitaConocido(lit), "%q no es conocido", lit)
		assert.Equal(t, domain.EstadoVisiteVuelvo, domain.EstadoDeTipoVisita(lit, false), "%q", lit)
		assert.Equal(t, domain.AlcanceVenta, domain.AlcanceDeTipoVisita(lit, false), "%q", lit)
	}
}

func TestCatalogoTiposVisita(t *testing.T) {
	t.Parallel()
	cat := domain.CatalogoTiposVisita()
	require.Len(t, cat, 14)
	assert.Equal(t, domain.TipoVisitaCatalogo{Literal: "No se encontraba", Estado: domain.EstadoNoEstaba, Grupo: "No estaba"}, cat[0])
	assert.Equal(t, "Se esconde y no sale", cat[13].Literal, "los retirados van al final")
	for _, c := range cat {
		assert.True(t, domain.EsTipoVisitaConocido(c.Literal), c.Literal)
		assert.NotEmpty(t, c.Grupo, c.Literal)
	}
	assert.Empty(t, domain.GrupoDeEstado(domain.EstadoPago))
	assert.Empty(t, domain.GrupoDeEstado(domain.EstadoCuenta("x")))
}
```

`internal/desempeno/domain/deriver_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

var (
	ventanaInicio = time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	ventanaFin    = time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	dentro        = time.Date(2026, 10, 11, 16, 0, 0, 0, time.UTC)
)

func ventana(t *testing.T) domain.Ventana {
	t.Helper()
	v, err := domain.NuevaVentana(ventanaInicio, ventanaFin)
	require.NoError(t, err)
	return v
}

func ventaPtr(id int) *int { return &id }

func TestNuevaVentana_RechazaInvertida(t *testing.T) {
	t.Parallel()
	_, err := domain.NuevaVentana(ventanaFin, ventanaInicio)
	require.ErrorIs(t, err, domain.ErrParametroInvalido)
}

func TestVentana_InclusivaEnLosDosExtremos(t *testing.T) {
	t.Parallel()
	v := ventana(t)
	assert.True(t, v.Contiene(ventanaInicio))
	assert.True(t, v.Contiene(ventanaFin))
	assert.False(t, v.Contiene(ventanaInicio.Add(-time.Second)))
	assert.False(t, v.Contiene(ventanaFin.Add(time.Second)))
}

func TestEstadoPorDinero(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre, abono, parcialidad string
		estado                     domain.EstadoCuenta
		ok                         bool
	}{
		{"sin abono", "0", "150", "", false},
		{"abono negativo", "-5", "150", "", false},
		{"parcialidad cero", "50", "0", domain.EstadoAbonoParcial, true},
		{"parcialidad negativa", "50", "-1", domain.EstadoAbonoParcial, true},
		{"borde exacto con escalas distintas", "150", "150.00", domain.EstadoPago, true},
		{"un centavo menos", "149.99", "150.00", domain.EstadoAbonoParcial, true},
		{"más de la parcialidad", "300", "150", domain.EstadoPago, true},
	}
	for _, c := range casos {
		e, ok := domain.EstadoPorDinero(dinero(c.abono), dinero(c.parcialidad))
		assert.Equal(t, c.ok, ok, c.nombre)
		assert.Equal(t, c.estado, e, c.nombre)
	}
}

func TestDerivar_UnaEntradaPorCuentaSiempre(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{
		{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")},
		{VentaID: 2, ClienteID: 11, Parcialidad: dinero("200")},
	}
	d := domain.Derivar(cuentas, nil, nil, ventana(t))
	require.Len(t, d.PorVenta, 2)
	for _, r := range d.PorVenta {
		assert.Equal(t, domain.EstadoSinTocar, r.Estado)
		assert.False(t, r.ConAlgo)
		assert.Empty(t, r.DeterminadoPor)
		assert.Empty(t, r.UltimaActividadID)
	}
}

func TestDerivar_RechazadoNoEsDineroPeroSiCobertura(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")}}
	pagos := []domain.PagoPeriodo{{
		VentaID: 1, Importe: dinero("150"), FormaCobroID: domain.FormaCobroEfectivo,
		FechaHora: dentro, Rechazado: true, ActividadID: "pago:7",
	}}
	r := domain.Derivar(cuentas, pagos, nil, ventana(t)).PorVenta[1]
	assert.Equal(t, domain.EstadoSinTocar, r.Estado)
	assert.True(t, r.AbonoVentana.IsZero())
	assert.True(t, r.ConAlgo)
	assert.Equal(t, "pago:7", r.UltimaActividadID)
	assert.Empty(t, r.DeterminadoPor)
}

func TestDerivar_DeterminadoPorYUltimaActividad(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")}}
	pagos := []domain.PagoPeriodo{
		{VentaID: 1, Importe: dinero("100"), FormaCobroID: 157, FechaHora: dentro, ActividadID: "pago:1"},
		{VentaID: 1, Importe: dinero("50"), FormaCobroID: 52569, FechaHora: dentro.Add(time.Hour), ActividadID: "pago:2"},
	}
	visitas := []domain.VisitaPeriodo{{
		ClienteID: 10, TipoVisita: domain.TipoVisitaNoSeEncontraba,
		FechaHora: dentro.Add(2 * time.Hour), ActividadID: "visita:a",
	}}
	r := domain.Derivar(cuentas, pagos, visitas, ventana(t)).PorVenta[1]
	assert.Equal(t, domain.EstadoPago, r.Estado, "100 + 50 = parcialidad exacta")
	assert.Equal(t, "pago:2", r.DeterminadoPor, "el pago de dinero más reciente")
	assert.Equal(t, "visita:a", r.UltimaActividadID, "lo más reciente de todo")
	assert.Equal(t, dentro.Add(2*time.Hour), r.UltimaActividadHora)
}

func TestDerivar_Incidencias(t *testing.T) {
	t.Parallel()
	cuentas := []domain.CuentaPeriodo{
		{VentaID: 1, ClienteID: 10, Parcialidad: dinero("0")},
		{VentaID: 2, ClienteID: 10, Parcialidad: dinero("0")},
	}
	pagos := []domain.PagoPeriodo{{VentaID: 1, Importe: dinero("50"), FormaCobroID: 157, FechaHora: dentro}}
	visitas := []domain.VisitaPeriodo{
		{ClienteID: 10, TipoVisita: "texto libre", FechaHora: dentro, VentaID: ventaPtr(2)},
		{ClienteID: 10, TipoVisita: domain.TipoVisitaNoVaADarPago, FechaHora: dentro},
		{ClienteID: 10, TipoVisita: "otro texto", FechaHora: ventanaInicio.Add(-time.Hour)},
	}
	d := domain.Derivar(cuentas, pagos, visitas, ventana(t))
	assert.Equal(t, domain.Incidencias{
		TipoVisitaFueraDeCatalogo: 1, // la de fuera de ventana no se cuenta
		ParcialidadNoPositiva:     1, // solo la que tuvo abono
		VisitaSinVenta:            1,
	}, d.Incidencias)
	assert.Equal(t, domain.EstadoAbonoParcial, d.PorVenta[1].Estado)
	assert.Equal(t, domain.EstadoVisiteVuelvo, d.PorVenta[2].Estado)
}

func TestDerivar_UltimaActividadEsElPagoSiEsPosterior(t *testing.T) {
	t.Parallel()
	v := ventana(t)
	assert.Equal(t, ventanaInicio, v.Inicio())
	assert.Equal(t, ventanaFin, v.Fin())
	cuentas := []domain.CuentaPeriodo{{VentaID: 1, ClienteID: 10, Parcialidad: dinero("150")}}
	visitas := []domain.VisitaPeriodo{{
		ClienteID: 10, VentaID: ventaPtr(1), TipoVisita: domain.TipoVisitaFueGrosero,
		FechaHora: dentro, ActividadID: "visita:a",
	}}
	pagos := []domain.PagoPeriodo{{
		VentaID: 1, Importe: dinero("50"), FormaCobroID: domain.FormaCobroCondonacion,
		FechaHora: dentro.Add(time.Hour), ActividadID: "pago:9",
	}}
	r := domain.Derivar(cuentas, pagos, visitas, v).PorVenta[1]
	assert.Equal(t, domain.EstadoSeNego, r.Estado, "la condonación no tapa la visita")
	assert.Equal(t, "visita:a", r.DeterminadoPor)
	assert.Equal(t, "pago:9", r.UltimaActividadID, "la condonación es lo más reciente")
}
```

`internal/desempeno/domain/presentacion_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func fecha(t *testing.T, s string) *domain.Fecha {
	t.Helper()
	f, err := domain.ParseFecha(s)
	require.NoError(t, err)
	return &f
}

func hora(t *testing.T, s string) *domain.HoraDelDia {
	t.Helper()
	h, err := domain.ParseHoraDelDia(s)
	require.NoError(t, err)
	return &h
}

// Cadenas copiadas de EstadoCuentaUi.kt y de EstadoCuentaUiTest.kt
// (LEÍDO 2026-10-14). Si el teléfono cambia una, esta tabla se pone roja.
func TestPresentacion_TratoEtiquetaDetalleSegmento(t *testing.T) {
	t.Parallel()
	hoy := *fecha(t, "2026-09-20")
	casos := []struct {
		nombre            string
		r                 domain.ResultadoCuenta
		trato             domain.Trato
		etiqueta, detalle string
		segmento          domain.Segmento
		haySegmento       bool
	}{
		{
			"pago",
			domain.ResultadoCuenta{Estado: domain.EstadoPago},
			domain.TratoPagado, "Pagó esta semana", "No hace falta volver", domain.SegmentoPagados, true,
		},
		{
			"abono parcial",
			domain.ResultadoCuenta{Estado: domain.EstadoAbonoParcial},
			domain.TratoParcial, "Abonó parcial", "Falta por confirmar", domain.SegmentoVolver, true,
		},
		{
			"visité vuelvo",
			domain.ResultadoCuenta{Estado: domain.EstadoVisiteVuelvo},
			domain.TratoRegresas, "Visité, vuelvo", "Regresas esta semana", domain.SegmentoVolver, true,
		},
		{
			"promesa sin fecha",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima},
			domain.TratoRegresas, "Prometió sin fecha", "Sin fecha, regresas", domain.SegmentoVolver, true,
		},
		{
			"promesa a futuro",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-09-25")},
			domain.TratoDiferido, "Prometió el 25 sept", "No cae esta semana", domain.SegmentoDespues, true,
		},
		{
			"promesa de hoy",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-09-20")},
			domain.TratoDiferido, "Prometió el 20 sept", "No cae esta semana", domain.SegmentoVolver, true,
		},
		{
			"promesa vencida",
			domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: fecha(t, "2026-09-02")},
			domain.TratoDiferido, "Prometió el 2 sept", "No cae esta semana", domain.SegmentoVolver, true,
		},
		{
			"se negó",
			domain.ResultadoCuenta{Estado: domain.EstadoSeNego},
			domain.TratoEscalar, "Se negó", "Escalar esta cuenta", domain.SegmentoDespues, true,
		},
		{
			"cita sin hora",
			domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, FechaCita: fecha(t, "2026-09-24")},
			domain.TratoRegresas, "Cita sin hora", "Sin hora, regresas", domain.SegmentoVolver, true,
		},
		{
			"cita con día y hora a futuro",
			domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, FechaCita: fecha(t, "2026-09-24"), HoraCita: hora(t, "16:30")},
			domain.TratoCita, "Cita 24 sept 16:30", "Quedaron de verse", domain.SegmentoDespues, true,
		},
		{
			"cita con hora sin día no cae en ningún chip",
			domain.ResultadoCuenta{Estado: domain.EstadoCitaAUnaHora, HoraCita: hora(t, "16:30")},
			domain.TratoCita, "Cita 16:30", "Quedaron de verse", "", false,
		},
		{
			"no estaba",
			domain.ResultadoCuenta{Estado: domain.EstadoNoEstaba},
			domain.TratoNadie, "No estaba", "Regresas esta semana", domain.SegmentoVolver, true,
		},
		{
			"sin tocar",
			domain.ResultadoCuenta{Estado: domain.EstadoSinTocar},
			domain.TratoSinTrabajar, "Falta pasar", "Todavía no vas", domain.SegmentoSinVisitar, true,
		},
	}
	for _, c := range casos {
		assert.Equal(t, c.trato, domain.TratoDe(c.r), c.nombre)
		assert.Equal(t, c.etiqueta, domain.EtiquetaDe(c.r), c.nombre)
		assert.Equal(t, c.detalle, domain.DetalleDe(c.r), c.nombre)
		seg, ok := domain.SegmentoDe(c.r, hoy)
		assert.Equal(t, c.haySegmento, ok, c.nombre)
		assert.Equal(t, c.segmento, seg, c.nombre)
	}
}

func TestPresentacion_MesesCortos(t *testing.T) {
	t.Parallel()
	esperado := []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"}
	for i, mes := range esperado {
		f := fecha(t, "2026-"+[]string{"01", "02", "03", "04", "05", "06", "07", "08", "09", "10", "11", "12"}[i]+"-03")
		r := domain.ResultadoCuenta{Estado: domain.EstadoPrometioProxima, FechaPromesa: f}
		assert.Equal(t, "Prometió el 3 "+mes, domain.EtiquetaDe(r))
	}
}

func TestPresentacion_EstadoDesconocidoEsSinTrabajar(t *testing.T) {
	t.Parallel()
	r := domain.ResultadoCuenta{Estado: domain.EstadoCuenta("inventado")}
	assert.Equal(t, domain.TratoSinTrabajar, domain.TratoDe(r))
	assert.Equal(t, "Falta pasar", domain.EtiquetaDe(r))
	assert.Equal(t, "Todavía no vas", domain.DetalleDe(r))
	seg, ok := domain.SegmentoDe(r, *fecha(t, "2026-10-14"))
	assert.True(t, ok)
	assert.Equal(t, domain.SegmentoSinVisitar, seg)
}
```

`internal/desempeno/domain/casos_compartidos_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Juego de casos compartido con msp-app-kt (contrato §7). La fuente es este
// repo; msp-app-kt guarda una copia byte a byte en
// core/common/src/test/resources/estado-cuenta/ y comprueba el MISMO VERSION.
const dirCasos = "../../../testdata/estado-cuenta"

var (
	nombreCaso = regexp.MustCompile(`^[0-9]{3}-[a-z0-9-]+\.json$`)
	hexVersion = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// listarCasos devuelve los .json en orden de bytes (= LC_ALL=C sort) y falla
// si hay cualquier archivo que no sea un caso bien nombrado o VERSION.
func listarCasos(dir string) ([]string, error) {
	entradas, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var casos, intrusos []string
	for _, e := range entradas {
		n := e.Name()
		switch {
		case strings.HasPrefix(n, "."), n == "VERSION":
			continue
		case !e.IsDir() && nombreCaso.MatchString(n):
			casos = append(casos, n)
		default:
			intrusos = append(intrusos, n)
		}
	}
	if len(intrusos) > 0 {
		return nil, fmt.Errorf("archivos fuera de formato en %s: %v", dir, intrusos)
	}
	sort.Strings(casos)
	return casos, nil
}

func archivosDeCasos(t *testing.T, dir string) []string {
	t.Helper()
	casos, err := listarCasos(dir)
	require.NoError(t, err)
	return casos
}

func hashDeCasos(t *testing.T, dir string, casos []string) string {
	t.Helper()
	h := sha256.New()
	for _, n := range casos {
		b, err := os.ReadFile(filepath.Join(dir, n))
		require.NoError(t, err)
		_, _ = h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func versionDeclarada(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	require.NoError(t, err)
	v := strings.TrimSuffix(string(b), "\n")
	require.Regexp(t, hexVersion, v, "VERSION debe ser 64 hex minúsculas con \\n final opcional")
	return v
}

func TestCasosEstadoCuenta_VersionCorrespondeAlHash(t *testing.T) {
	t.Parallel()
	casos := archivosDeCasos(t, dirCasos)
	require.GreaterOrEqual(t, len(casos), 25, "el juego exige al menos 25 casos")
	assert.Equal(t, versionDeclarada(t, dirCasos), hashDeCasos(t, dirCasos, casos),
		"cambió un caso sin actualizar VERSION: "+
			"cd testdata/estado-cuenta && ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1 > VERSION")
}

// Control positivo del verificador: un byte de más en una copia cambia el hash.
func TestCasosEstadoCuenta_ControlPositivoDelHash(t *testing.T) {
	t.Parallel()
	casos := archivosDeCasos(t, dirCasos)
	tmp := t.TempDir()
	for i, n := range casos {
		b, err := os.ReadFile(filepath.Join(dirCasos, n))
		require.NoError(t, err)
		if i == 0 {
			b = append(b, ' ')
		}
		require.NoError(t, os.WriteFile(filepath.Join(tmp, n), b, 0o600))
	}
	assert.NotEqual(t, versionDeclarada(t, dirCasos), hashDeCasos(t, tmp, casos))
}

func TestCasosEstadoCuenta_ArchivoIntrusoSeRechaza(t *testing.T) {
	t.Parallel()
	for _, intruso := range []string{"1-caso.json", "README.md", "002-Con-Mayusculas.json", "003-con espacio.json"} {
		tmp := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(tmp, "001-bueno.json"), []byte("{}"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(tmp, intruso), []byte("{}"), 0o600))
		_, err := listarCasos(tmp)
		require.Error(t, err, intruso)
		assert.Contains(t, err.Error(), intruso)
	}
	tmp := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "001-bueno.json"), []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "VERSION"), []byte("x"), 0o600))
	casos, err := listarCasos(tmp)
	require.NoError(t, err, "control positivo: un caso bien nombrado y VERSION pasan")
	assert.Equal(t, []string{"001-bueno.json"}, casos)
}

// ─── Parseo estricto ─────────────────────────────────────────────────────────

type casoJSON struct {
	Nombre   string
	Ventana  domain.Ventana
	Cuentas  []domain.CuentaPeriodo
	Pagos    []domain.PagoPeriodo
	Visitas  []domain.VisitaPeriodo
	Esperado map[int]esperadoJSON
}

type esperadoJSON struct {
	Estado  domain.EstadoCuenta
	ConAlgo bool
}

// objeto decodifica un objeto JSON exigiendo EXACTAMENTE las claves dadas:
// una clave desconocida o una ausente es error (un campo mal escrito no puede
// leerse como null y dar verde por casualidad).
func objeto(raw json.RawMessage, donde string, claves ...string) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("%s: no es un objeto: %w", donde, err)
	}
	permitidas := map[string]bool{}
	for _, c := range claves {
		permitidas[c] = true
		if _, ok := m[c]; !ok {
			return nil, fmt.Errorf("%s: falta la clave %q", donde, c)
		}
	}
	for c := range m {
		if !permitidas[c] {
			return nil, fmt.Errorf("%s: clave desconocida %q", donde, c)
		}
	}
	return m, nil
}

func lista(raw json.RawMessage, donde string) ([]json.RawMessage, error) {
	var l []json.RawMessage
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("%s: no es una lista: %w", donde, err)
	}
	return l, nil
}

func cadena(raw json.RawMessage, donde string) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("%s: debe ser string: %w", donde, err)
	}
	return s, nil
}

func cadenaONula(raw json.RawMessage, donde string) (*string, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil //nolint:nilnil // null explícito es un valor válido del contrato.
	}
	s, err := cadena(raw, donde)
	return &s, err
}

func entero(raw json.RawMessage, donde string) (int, error) {
	var n int
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, fmt.Errorf("%s: debe ser entero: %w", donde, err)
	}
	return n, nil
}

func booleano(raw json.RawMessage, donde string) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, fmt.Errorf("%s: debe ser booleano: %w", donde, err)
	}
	return b, nil
}

// dineroJSON: SOLO desde string; un número JSON es error.
func dineroJSON(raw json.RawMessage, donde string) (decimal.Decimal, error) {
	s, err := cadena(raw, donde+" (dinero como string decimal, no como número JSON)")
	if err != nil {
		return decimal.Decimal{}, err
	}
	return decimal.NewFromString(s)
}

func instante(raw json.RawMessage, donde string) (time.Time, error) {
	s, err := cadena(raw, donde)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, s)
}

//nolint:gocognit,cyclop,funlen // parseo lineal de un formato fijo; partirlo esconde el contrato.
func parsearCaso(archivo string, texto []byte) (casoJSON, error) {
	var c casoJSON
	raiz, err := objeto(texto, archivo, "nombre", "ventana", "cuentas", "pagos", "visitas", "esperado")
	if err != nil {
		return c, err
	}
	if c.Nombre, err = cadena(raiz["nombre"], archivo+".nombre"); err != nil {
		return c, err
	}
	vent, err := objeto(raiz["ventana"], archivo+".ventana", "inicio", "fin")
	if err != nil {
		return c, err
	}
	ini, err := instante(vent["inicio"], archivo+".ventana.inicio")
	if err != nil {
		return c, err
	}
	fin, err := instante(vent["fin"], archivo+".ventana.fin")
	if err != nil {
		return c, err
	}
	if c.Ventana, err = domain.NuevaVentana(ini, fin); err != nil {
		return c, err
	}

	cuentas, err := lista(raiz["cuentas"], archivo+".cuentas")
	if err != nil {
		return c, err
	}
	vistas := map[int]bool{}
	for i, raw := range cuentas {
		donde := fmt.Sprintf("%s.cuentas[%d]", archivo, i)
		m, err := objeto(raw, donde, "venta_id", "cliente_id", "parcialidad")
		if err != nil {
			return c, err
		}
		var cp domain.CuentaPeriodo
		if cp.VentaID, err = entero(m["venta_id"], donde+".venta_id"); err != nil {
			return c, err
		}
		if cp.ClienteID, err = entero(m["cliente_id"], donde+".cliente_id"); err != nil {
			return c, err
		}
		if cp.Parcialidad, err = dineroJSON(m["parcialidad"], donde+".parcialidad"); err != nil {
			return c, err
		}
		if vistas[cp.VentaID] {
			return c, fmt.Errorf("%s: venta_id %d repetido en cuentas", archivo, cp.VentaID)
		}
		vistas[cp.VentaID] = true
		c.Cuentas = append(c.Cuentas, cp)
	}

	pagos, err := lista(raiz["pagos"], archivo+".pagos")
	if err != nil {
		return c, err
	}
	for i, raw := range pagos {
		donde := fmt.Sprintf("%s.pagos[%d]", archivo, i)
		m, err := objeto(raw, donde, "venta_id", "importe", "forma_cobro_id", "fecha_hora", "rechazado")
		if err != nil {
			return c, err
		}
		var p domain.PagoPeriodo
		if p.VentaID, err = entero(m["venta_id"], donde+".venta_id"); err != nil {
			return c, err
		}
		if p.Importe, err = dineroJSON(m["importe"], donde+".importe"); err != nil {
			return c, err
		}
		if p.FormaCobroID, err = entero(m["forma_cobro_id"], donde+".forma_cobro_id"); err != nil {
			return c, err
		}
		if p.FechaHora, err = instante(m["fecha_hora"], donde+".fecha_hora"); err != nil {
			return c, err
		}
		if p.Rechazado, err = booleano(m["rechazado"], donde+".rechazado"); err != nil {
			return c, err
		}
		p.ActividadID = fmt.Sprintf("pago:%d", i)
		c.Pagos = append(c.Pagos, p)
	}

	visitas, err := lista(raiz["visitas"], archivo+".visitas")
	if err != nil {
		return c, err
	}
	for i, raw := range visitas {
		donde := fmt.Sprintf("%s.visitas[%d]", archivo, i)
		m, err := objeto(raw, donde, "cliente_id", "venta_id", "tipo_visita", "fecha_hora",
			"fecha_cita", "hora_cita", "fecha_promesa")
		if err != nil {
			return c, err
		}
		var v domain.VisitaPeriodo
		if v.ClienteID, err = entero(m["cliente_id"], donde+".cliente_id"); err != nil {
			return c, err
		}
		if !bytes.Equal(bytes.TrimSpace(m["venta_id"]), []byte("null")) {
			id, err := entero(m["venta_id"], donde+".venta_id")
			if err != nil {
				return c, err
			}
			v.VentaID = &id
		}
		if v.TipoVisita, err = cadena(m["tipo_visita"], donde+".tipo_visita"); err != nil {
			return c, err
		}
		if v.FechaHora, err = instante(m["fecha_hora"], donde+".fecha_hora"); err != nil {
			return c, err
		}
		for clave, dst := range map[string]**domain.Fecha{"fecha_cita": &v.FechaCita, "fecha_promesa": &v.FechaPromesa} {
			s, err := cadenaONula(m[clave], donde+"."+clave)
			if err != nil {
				return c, err
			}
			if s != nil {
				f, err := domain.ParseFecha(*s)
				if err != nil {
					return c, fmt.Errorf("%s.%s: %w", donde, clave, err)
				}
				*dst = &f
			}
		}
		hs, err := cadenaONula(m["hora_cita"], donde+".hora_cita")
		if err != nil {
			return c, err
		}
		if hs != nil {
			h, err := domain.ParseHoraDelDia(*hs)
			if err != nil {
				return c, fmt.Errorf("%s.hora_cita: %w", donde, err)
			}
			v.HoraCita = &h
		}
		v.ActividadID = fmt.Sprintf("visita:%d", i)
		c.Visitas = append(c.Visitas, v)
	}

	esperados, err := lista(raiz["esperado"], archivo+".esperado")
	if err != nil {
		return c, err
	}
	c.Esperado = map[int]esperadoJSON{}
	for i, raw := range esperados {
		donde := fmt.Sprintf("%s.esperado[%d]", archivo, i)
		m, err := objeto(raw, donde, "venta_id", "estado", "con_algo")
		if err != nil {
			return c, err
		}
		id, err := entero(m["venta_id"], donde+".venta_id")
		if err != nil {
			return c, err
		}
		cod, err := cadena(m["estado"], donde+".estado")
		if err != nil {
			return c, err
		}
		estado, err := domain.ParseEstadoCuenta(cod)
		if err != nil {
			return c, fmt.Errorf("%s.estado: estado desconocido %q", donde, cod)
		}
		conAlgo, err := booleano(m["con_algo"], donde+".con_algo")
		if err != nil {
			return c, err
		}
		if _, dup := c.Esperado[id]; dup {
			return c, fmt.Errorf("%s: esperado repite venta_id %d", archivo, id)
		}
		c.Esperado[id] = esperadoJSON{Estado: estado, ConAlgo: conAlgo}
	}
	if len(c.Esperado) != len(c.Cuentas) {
		return c, fmt.Errorf("%s: esperado debe traer exactamente una entrada por cuenta", archivo)
	}
	for _, cp := range c.Cuentas {
		if _, ok := c.Esperado[cp.VentaID]; !ok {
			return c, fmt.Errorf("%s: esperado no trae la venta %d", archivo, cp.VentaID)
		}
	}
	return c, nil
}

func cargarCasos(t *testing.T) map[string]casoJSON {
	t.Helper()
	out := map[string]casoJSON{}
	for _, n := range archivosDeCasos(t, dirCasos) {
		b, err := os.ReadFile(filepath.Join(dirCasos, n))
		require.NoError(t, err)
		c, err := parsearCaso(n, b)
		require.NoError(t, err)
		out[n] = c
	}
	return out
}

func TestCasosEstadoCuenta_CadaCasoContraElDeriver(t *testing.T) {
	t.Parallel()
	for archivo, c := range cargarCasos(t) {
		t.Run(archivo, func(t *testing.T) {
			t.Parallel()
			d := domain.Derivar(c.Cuentas, c.Pagos, c.Visitas, c.Ventana)
			require.Len(t, d.PorVenta, len(c.Cuentas), c.Nombre)
			for ventaID, e := range c.Esperado {
				r := d.PorVenta[ventaID]
				assert.Equal(t, e.Estado, r.Estado, "%s — venta %d: estado", c.Nombre, ventaID)
				assert.Equal(t, e.ConAlgo, r.ConAlgo, "%s — venta %d: con_algo", c.Nombre, ventaID)
			}
		})
	}
}

// Cobertura mínima exigida por el plan Android (Task 4) y por el spec §7.
func TestCasosEstadoCuenta_CoberturaMinimaDelJuego(t *testing.T) {
	t.Parallel()
	estados := map[domain.EstadoCuenta]bool{}
	conAlgo := map[bool]bool{}
	var rechazado, condonacion, bordeExacto, parcialidadCero, fueraDeCatalogo, alcanceCliente, sinVenta bool
	for _, c := range cargarCasos(t) {
		for _, e := range c.Esperado {
			estados[e.Estado] = true
			conAlgo[e.ConAlgo] = true
		}
		for _, p := range c.Pagos {
			rechazado = rechazado || p.Rechazado
			condonacion = condonacion || p.FormaCobroID == domain.FormaCobroCondonacion
			for _, cp := range c.Cuentas {
				if cp.VentaID == p.VentaID && p.Importe.Equal(cp.Parcialidad) && !p.Rechazado &&
					domain.EsFormaDeDinero(p.FormaCobroID) {
					bordeExacto = true
				}
			}
		}
		for _, cp := range c.Cuentas {
			parcialidadCero = parcialidadCero || cp.Parcialidad.IsZero()
		}
		for _, v := range c.Visitas {
			fueraDeCatalogo = fueraDeCatalogo || !domain.EsTipoVisitaConocido(v.TipoVisita)
			alcanceCliente = alcanceCliente || domain.AlcanceDeTipoVisita(v.TipoVisita, v.FechaCita != nil) == domain.AlcanceCliente
			sinVenta = sinVenta || v.VentaID == nil
		}
	}
	for _, e := range domain.EstadosCuenta() {
		assert.True(t, estados[e], "ningún caso espera el estado %s", e)
	}
	assert.True(t, conAlgo[true] && conAlgo[false], "faltan casos con con_algo true y false")
	assert.True(t, rechazado, "falta un pago rechazado")
	assert.True(t, condonacion, "falta una condonación (forma 137026)")
	assert.True(t, bordeExacto, "falta el borde exacto abono == parcialidad")
	assert.True(t, parcialidadCero, "falta parcialidad 0")
	assert.True(t, fueraDeCatalogo, "falta un literal fuera de catálogo")
	assert.True(t, alcanceCliente, "falta una visita de alcance cliente")
	assert.True(t, sinVenta, "falta una visita sin venta ligada")
}

// ─── El parseo estricto se prueba a sí mismo ─────────────────────────────────

const casoValido = `{ "nombre": "x",
  "ventana": { "inicio": "2026-10-09T13:46:00Z", "fin": "2026-10-14T22:40:00Z" },
  "cuentas": [ { "venta_id": 1, "cliente_id": 10, "parcialidad": "150.00" } ],
  "pagos":   [ { "venta_id": 1, "importe": "150.00", "forma_cobro_id": 157,
                 "fecha_hora": "2026-10-10T15:00:00Z", "rechazado": false } ],
  "visitas": [ { "cliente_id": 10, "venta_id": null, "tipo_visita": "No se encontraba",
                 "fecha_hora": "2026-10-11T16:00:00Z", "fecha_cita": null, "hora_cita": null,
                 "fecha_promesa": null } ],
  "esperado": [ { "venta_id": 1, "estado": "pago", "con_algo": true } ] }`

func TestParsearCaso_Estricto(t *testing.T) {
	t.Parallel()
	_, err := parsearCaso("999-valido.json", []byte(casoValido))
	require.NoError(t, err, "control positivo: el caso válido se parsea")

	malos := []struct{ nombre, de, a, menciona string }{
		{"dinero como número", `"importe": "150.00"`, `"importe": 150.0`, "importe"},
		{"clave desconocida", `"fecha_cita": null`, `"fecha_cit": null`, "fecha_cit"},
		{"clave ausente", `, "rechazado": false`, ``, "rechazado"},
		{"estado desconocido", `"estado": "pago"`, `"estado": "PAGO"`, "PAGO"},
		{"esperado incompleto", `[ { "venta_id": 1, "estado": "pago", "con_algo": true } ]`, `[]`, "esperado"},
		{"rechazado no booleano", `"rechazado": false`, `"rechazado": "false"`, "rechazado"},
		{"ventana invertida", `"fin": "2026-10-14T22:40:00Z"`, `"fin": "2026-10-01T00:00:00Z"`, "parámetro"},
	}
	for _, m := range malos {
		texto := strings.Replace(casoValido, m.de, m.a, 1)
		require.NotEqual(t, casoValido, texto, "control: el reemplazo de %q ocurrió", m.nombre)
		_, err := parsearCaso("999-malo.json", []byte(texto))
		require.Error(t, err, m.nombre)
		assert.Contains(t, err.Error(), m.menciona, m.nombre)
	}
}
```


- [ ] **Step 2: Generar los 40 casos JSON y su VERSION**

Generador de un solo uso (no se commitea): guardarlo como `/tmp/gen_casos.py`. Cada caso declara entrada y resultado esperado; los esperados se derivaron a mano de las reglas de arriba y se comprobaron contra el port.

```python
import json, os, sys

DEST = sys.argv[1]
V = {"inicio": "2026-10-09T13:46:00Z", "fin": "2026-10-14T22:40:00Z"}
D = "2026-10-11T16:00:00Z"   # dentro de la ventana

def cuenta(v, c, p): return {"venta_id": v, "cliente_id": c, "parcialidad": p}
def pago(v, imp, forma=157, fh=D, rech=False):
    return {"venta_id": v, "importe": imp, "forma_cobro_id": forma, "fecha_hora": fh, "rechazado": rech}
def visita(c, v, tipo, fh=D, cita=None, hora=None, promesa=None):
    return {"cliente_id": c, "venta_id": v, "tipo_visita": tipo, "fecha_hora": fh,
            "fecha_cita": cita, "hora_cita": hora, "fecha_promesa": promesa}
def esp(v, e, a): return {"venta_id": v, "estado": e, "con_algo": a}

C = []
def caso(slug, nombre, cuentas, pagos, visitas, esperado):
    C.append((slug, {"nombre": nombre, "ventana": V, "cuentas": cuentas, "pagos": pagos,
                     "visitas": visitas, "esperado": esperado}))

caso("ejemplo-contrato-no-estaba-marca-ambas", "cliente con dos ventas, no estaba marca ambas",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")],
     [pago(1, "150.00", fh="2026-10-10T15:00:00Z")],
     [visita(10, None, "No se encontraba")],
     [esp(1, "pago", True), esp(2, "no_estaba", True)])
caso("sin-nada-sin-tocar", "venta sin pagos ni visitas queda sin tocar y sin nada",
     [cuenta(1, 10, "150.00")], [], [], [esp(1, "sin_tocar", False)])
caso("pago-exacto-borde", "abono igual a la parcialidad con escala distinta es pago",
     [cuenta(1, 10, "150")], [pago(1, "150.00")], [], [esp(1, "pago", True)])
caso("abono-un-centavo-menos", "un centavo menos que la parcialidad es abono parcial",
     [cuenta(1, 10, "150.00")], [pago(1, "149.99")], [], [esp(1, "abono_parcial", True)])
caso("pagos-suman-parcialidad", "efectivo mas transferencia suman la parcialidad",
     [cuenta(1, 10, "150.00")],
     [pago(1, "100.00", 157, "2026-10-10T15:00:00Z"), pago(1, "50.00", 52569, "2026-10-12T15:00:00Z")],
     [], [esp(1, "pago", True)])
caso("cheque-cuenta-como-dinero", "el cheque cuenta como dinero",
     [cuenta(1, 10, "200.00")], [pago(1, "200.00", 158)], [], [esp(1, "pago", True)])
caso("condonacion-no-es-dinero", "la condonacion no es dinero pero si cuenta como algo",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", 137026)], [], [esp(1, "sin_tocar", True)])
caso("condonacion-no-tapa-la-visita", "con condonacion manda la visita",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", 137026, "2026-10-10T15:00:00Z")],
     [visita(10, 1, "Fue grosero o agresivo")], [esp(1, "se_nego", True)])
caso("rechazado-no-es-dinero", "un pago rechazado no es dinero pero si cuenta como algo",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", rech=True)], [], [esp(1, "sin_tocar", True)])
caso("rechazado-no-se-suma-al-aceptado", "el rechazado no se suma al aceptado",
     [cuenta(1, 10, "150.00")],
     [pago(1, "100.00"), pago(1, "100.00", fh="2026-10-12T10:00:00Z", rech=True)],
     [], [esp(1, "abono_parcial", True)])
caso("forma-desconocida-no-es-dinero", "una forma de cobro desconocida no es dinero",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", 999)], [], [esp(1, "sin_tocar", True)])
caso("parcialidad-cero-con-abono", "con parcialidad cero un abono positivo es abono parcial",
     [cuenta(1, 10, "0.00")], [pago(1, "50.00")], [], [esp(1, "abono_parcial", True)])
caso("parcialidad-cero-sin-nada", "con parcialidad cero y sin nada queda sin tocar",
     [cuenta(1, 10, "0.00")], [], [], [esp(1, "sin_tocar", False)])
caso("pago-antes-de-ventana", "un segundo antes del inicio no cuenta",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", fh="2026-10-09T13:45:59Z")], [], [esp(1, "sin_tocar", False)])
caso("pago-en-borde-inicial", "exactamente en el inicio cuenta",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", fh="2026-10-09T13:46:00Z")], [], [esp(1, "pago", True)])
caso("pago-en-borde-final", "exactamente en el fin cuenta",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", fh="2026-10-14T22:40:00Z")], [], [esp(1, "pago", True)])
caso("pago-despues-de-ventana", "un segundo despues del fin no cuenta",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", fh="2026-10-14T22:40:01Z")], [], [esp(1, "sin_tocar", False)])
caso("visita-fuera-de-ventana", "una visita antes del inicio no cuenta",
     [cuenta(1, 10, "150.00")], [], [visita(10, None, "No se encontraba", fh="2026-10-09T10:00:00Z")],
     [esp(1, "sin_tocar", False)])
caso("dinero-manda-sobre-visita-posterior", "pago completo en la manana y negativa en la tarde es pago",
     [cuenta(1, 10, "150.00")], [pago(1, "150.00", fh="2026-10-11T15:00:00Z")],
     [visita(10, 1, "No pagará esta ocasión", fh="2026-10-11T22:00:00Z")], [esp(1, "pago", True)])
caso("se-nego-solo-su-venta", "una negativa de alcance venta marca solo su venta",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, 1, "No pagará esta ocasión")],
     [esp(1, "se_nego", True), esp(2, "sin_tocar", False)])
caso("visite-vuelvo", "se asomo pero no salio es visite vuelvo",
     [cuenta(1, 10, "150.00")], [], [visita(10, 1, "Se asomó pero no salió")], [esp(1, "visite_vuelvo", True)])
caso("pide-tiempo-sin-cita-es-visite-vuelvo", "pidio que regrese otro dia sin dia de cita es visite vuelvo",
     [cuenta(1, 10, "150.00")], [], [visita(10, 1, "Pidió que regrese otro día")], [esp(1, "visite_vuelvo", True)])
caso("reagendar-es-prometio-proxima", "pidio reagendar visita es prometio proxima",
     [cuenta(1, 10, "150.00")], [], [visita(10, 1, "Pidió reagendar visita")], [esp(1, "prometio_proxima", True)])
caso("cita-con-dia-marca-todas", "una cita con dia gana al literal y marca todas las ventas del cliente",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, 1, "Pidió que regrese otro día", cita="2026-10-15", hora="16:30")],
     [esp(1, "cita_a_una_hora", True), esp(2, "cita_a_una_hora", True)])
caso("cita-sin-hora-sigue-siendo-cita", "una cita con dia y sin hora sigue siendo cita",
     [cuenta(1, 10, "150.00")], [], [visita(10, None, "Pidió que regrese otro día", cita="2026-10-15")],
     [esp(1, "cita_a_una_hora", True)])
caso("hora-sin-dia-no-es-cita", "una hora de cita sin dia no convierte la visita en cita",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, 1, "Pidió que regrese otro día", hora="10:00")],
     [esp(1, "visite_vuelvo", True), esp(2, "sin_tocar", False)])
caso("fecha-promesa-no-cambia-estado", "la fecha de promesa no cambia el estado",
     [cuenta(1, 10, "150.00")], [], [visita(10, 1, "Pidió reagendar visita", promesa="2026-10-20")],
     [esp(1, "prometio_proxima", True)])
caso("literal-fuera-de-catalogo-alcance-venta", "un literal fuera del catalogo es visite vuelvo y solo toca su venta",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, 1, "Regresar mañana temprano")],
     [esp(1, "visite_vuelvo", True), esp(2, "sin_tocar", False)])
caso("literal-vacio-sin-venta-no-cuenta", "un literal vacio sin venta ligada no le aplica a nada",
     [cuenta(1, 10, "150.00")], [], [visita(10, None, "")], [esp(1, "sin_tocar", False)])
caso("literal-retirado-dijo-que-no-va-a-pagar", "el literal retirado dijo que no va a pagar es se nego",
     [cuenta(1, 10, "150.00")], [], [visita(10, 1, "Dijo que no va a pagar")], [esp(1, "se_nego", True)])
caso("literal-retirado-se-esconde", "el literal retirado se esconde y no sale es visite vuelvo",
     [cuenta(1, 10, "150.00")], [], [visita(10, 1, "Se esconde y no sale")], [esp(1, "visite_vuelvo", True)])
caso("literal-en-minusculas-es-desconocido", "no se encontraba en minusculas no es el literal del catalogo",
     [cuenta(1, 10, "150.00")], [], [visita(10, None, "no se encontraba")], [esp(1, "sin_tocar", False)])
caso("negativa-sin-venta-no-cuenta", "una visita de alcance venta sin venta ligada no cuenta",
     [cuenta(1, 10, "150.00")], [], [visita(10, None, "No pagará esta ocasión")], [esp(1, "sin_tocar", False)])
caso("visita-mas-reciente-gana", "gana la visita mas reciente aunque sea de venta",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, None, "No se encontraba", fh="2026-10-11T15:00:00Z"),
      visita(10, 1, "Se asomó pero no salió", fh="2026-10-11T18:00:00Z")],
     [esp(1, "visite_vuelvo", True), esp(2, "no_estaba", True)])
caso("empate-gana-la-de-venta", "en empate exacto gana la visita de alcance venta",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, 1, "No pagará esta ocasión"), visita(10, None, "No se encontraba")],
     [esp(1, "se_nego", True), esp(2, "no_estaba", True)])
caso("empate-cliente-primero-en-la-entrada", "en empate gana la de venta aunque la de cliente venga primero",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, None, "No se encontraba"), visita(10, 1, "No pagará esta ocasión")],
     [esp(1, "se_nego", True), esp(2, "no_estaba", True)])
caso("no-estaba-de-otro-cliente", "no estaba de otro cliente no aplica",
     [cuenta(1, 10, "150.00")], [], [visita(11, None, "No se encontraba")], [esp(1, "sin_tocar", False)])
caso("pago-de-otra-venta", "un pago de una venta fuera del universo no cuenta",
     [cuenta(1, 10, "150.00")], [pago(99, "150.00")], [], [esp(1, "sin_tocar", False)])
caso("tres-ventas-pago-en-una-no-estaba-despues", "pago completo en una venta y no estaba despues marca las otras dos",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00"), cuenta(3, 10, "100.00")],
     [pago(1, "150.00", fh="2026-10-11T15:00:00Z")],
     [visita(10, None, "Casa cerrada con candado", fh="2026-10-11T19:00:00Z")],
     [esp(1, "pago", True), esp(2, "no_estaba", True), esp(3, "no_estaba", True)])
caso("visita-de-cliente-posterior-pisa-la-de-venta", "una visita de cliente posterior pisa la de venta anterior",
     [cuenta(1, 10, "150.00"), cuenta(2, 10, "200.00")], [],
     [visita(10, 1, "No pagará esta ocasión", fh="2026-10-10T15:00:00Z"),
      visita(10, None, "Solo había menores", fh="2026-10-12T15:00:00Z")],
     [esp(1, "no_estaba", True), esp(2, "no_estaba", True)])

os.makedirs(DEST, exist_ok=True)
for i, (slug, data) in enumerate(C, start=1):
    with open(os.path.join(DEST, "%03d-%s.json" % (i, slug)), "w", encoding="utf-8", newline="\n") as fh:
        fh.write(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
print(len(C), "casos")
```


```bash
python3 /tmp/gen_casos.py testdata/estado-cuenta
cd testdata/estado-cuenta && ls *.json | LC_ALL=C sort | xargs cat | shasum -a 256 | cut -d' ' -f1 > VERSION && cd -
cat testdata/estado-cuenta/VERSION
```
Expected: `40 casos` y `VERSION` = `c37e982d1f95dd8cca800dff3d9caaa7b81563429564fced37161f2c885b4c22` (el valor medido al escribir este plan; si difiere, los bytes del generador no son los de aquí).

- [ ] **Step 3: Correr y ver que fallan**

Run: `go test ./internal/desempeno/domain/`
Expected: FAIL de compilación (`undefined: domain.Derivar`, `undefined: domain.EstadoPago`, …).

- [ ] **Step 4: Implementación**

`internal/desempeno/domain/estado_cuenta.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// EstadoCuenta es uno de los ocho estados por venta del teléfono
// (msp-app-kt core/common/.../cobranza/domain/EstadoCuenta.kt). Los valores son
// los códigos del contrato §1, byte a byte.
type EstadoCuenta string

// Los ocho estados.
const (
	EstadoPago            EstadoCuenta = "pago"
	EstadoAbonoParcial    EstadoCuenta = "abono_parcial"
	EstadoVisiteVuelvo    EstadoCuenta = "visite_vuelvo"
	EstadoPrometioProxima EstadoCuenta = "prometio_proxima"
	EstadoSeNego          EstadoCuenta = "se_nego"
	EstadoCitaAUnaHora    EstadoCuenta = "cita_a_una_hora"
	EstadoNoEstaba        EstadoCuenta = "no_estaba"
	EstadoSinTocar        EstadoCuenta = "sin_tocar"
)

// EstadosCuenta devuelve los ocho, en el orden del enum de Kotlin.
func EstadosCuenta() []EstadoCuenta {
	return []EstadoCuenta{
		EstadoPago, EstadoAbonoParcial, EstadoVisiteVuelvo, EstadoPrometioProxima,
		EstadoSeNego, EstadoCitaAUnaHora, EstadoNoEstaba, EstadoSinTocar,
	}
}

// ParseEstadoCuenta valida un código del contrato.
func ParseEstadoCuenta(s string) (EstadoCuenta, error) {
	for _, e := range EstadosCuenta() {
		if string(e) == s {
			return e, nil
		}
	}
	return "", ErrParametroInvalido.WithField("estado", s)
}

// Alcance de una visita sobre las ventas del cliente (VisitScope.kt).
type Alcance int

// AlcanceVenta toca solo la venta ligada; AlcanceCliente toca todas las del cliente.
const (
	AlcanceVenta Alcance = iota
	AlcanceCliente
)

// Alcance de cada estado (columna "Alcance" de EstadoCuenta.kt): solo
// CITA_A_UNA_HORA y NO_ESTABA son de cliente.
func (e EstadoCuenta) Alcance() Alcance {
	if e == EstadoCitaAUnaHora || e == EstadoNoEstaba {
		return AlcanceCliente
	}
	return AlcanceVenta
}

// Trato es cómo se pinta un estado (EstadoCuentaUi.kt TratoDelEstado).
type Trato string

// Los ocho tratos, con los códigos del contrato §1.
const (
	TratoPagado      Trato = "pagado"
	TratoParcial     Trato = "parcial"
	TratoRegresas    Trato = "regresas"
	TratoDiferido    Trato = "diferido"
	TratoEscalar     Trato = "escalar"
	TratoCita        Trato = "cita"
	TratoNadie       Trato = "nadie"
	TratoSinTrabajar Trato = "sin_trabajar"
)

// Segmento es el chip de la lista de cuentas (SegmentoDeCobranza.kt).
type Segmento string

// Los cuatro segmentos del contrato §1. Una cuenta puede no caer en ninguno
// (cita o promesa con trato propio pero sin fecha): entonces el JSON lleva null.
const (
	SegmentoSinVisitar Segmento = "sin_visitar"
	SegmentoVolver     Segmento = "volver"
	SegmentoDespues    Segmento = "despues"
	SegmentoPagados    Segmento = "pagados"
)
```

`internal/desempeno/domain/tipo_visita.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// Catálogo cerrado de TIPO_VISITA y su estado, copia de
// msp-app-kt TipoVisitaCatalogo.kt (que a su vez copia
// internal/visitas/domain/tipo_visita.go). Los literales viajan por el cable y
// no se editan: la comparación es byte a byte, con acentos.
const (
	TipoVisitaNoSeEncontraba        = "No se encontraba"
	TipoVisitaCasaCerrada           = "Casa cerrada con candado"
	TipoVisitaSoloMenores           = "Solo había menores"
	TipoVisitaNoVaADarPago          = "No pagará esta ocasión"
	TipoVisitaPideTiempo            = "Pidió que regrese otro día"
	TipoVisitaTienePeroNoPaga       = "Tiene dinero pero no quiso pagar"
	TipoVisitaFueGrosero            = "Fue grosero o agresivo"
	TipoVisitaSeEsconde             = "Se asomó pero no salió"
	TipoVisitaNoResponde            = "No responde aunque está"
	TipoVisitaSeEscuchanRuidos      = "Se escuchan ruidos pero no abre"
	TipoVisitaPideReagendar         = "Pidió reagendar visita"
	TipoVisitaLegacyNoVaADarPago    = "No va a dar pago"
	TipoVisitaLegacyDijoQueNoPagara = "Dijo que no va a pagar"
	TipoVisitaLegacySeEscondeNoSale = "Se esconde y no sale"
)

// EstadoTipoVisitaDesconocido es el estado de un literal fuera del catálogo
// (incluida la cadena vacía): el más angosto que sigue siendo cierto. Igual
// que TipoVisitaCatalogo.ESTADO_DESCONOCIDO. Su alcance es VENTA.
const EstadoTipoVisitaDesconocido = EstadoVisiteVuelvo

var estadoPorTipoVisita = map[string]EstadoCuenta{
	TipoVisitaNoSeEncontraba:        EstadoNoEstaba,
	TipoVisitaCasaCerrada:           EstadoNoEstaba,
	TipoVisitaSoloMenores:           EstadoNoEstaba,
	TipoVisitaSeEsconde:             EstadoVisiteVuelvo,
	TipoVisitaNoResponde:            EstadoVisiteVuelvo,
	TipoVisitaSeEscuchanRuidos:      EstadoVisiteVuelvo,
	TipoVisitaPideTiempo:            EstadoVisiteVuelvo,
	TipoVisitaNoVaADarPago:          EstadoSeNego,
	TipoVisitaTienePeroNoPaga:       EstadoSeNego,
	TipoVisitaFueGrosero:            EstadoSeNego,
	TipoVisitaPideReagendar:         EstadoPrometioProxima,
	TipoVisitaLegacyNoVaADarPago:    EstadoSeNego,
	TipoVisitaLegacyDijoQueNoPagara: EstadoSeNego,
	TipoVisitaLegacySeEscondeNoSale: EstadoVisiteVuelvo,
}

// EsTipoVisitaConocido dice si el literal está en el catálogo de 14.
func EsTipoVisitaConocido(tipoVisita string) bool {
	_, ok := estadoPorTipoVisita[tipoVisita]
	return ok
}

// EstadoDeTipoVisita es TipoVisitaCatalogo.estadoDe: función TOTAL. Una visita
// con día de cita es CITA_A_UNA_HORA venga con el literal que venga.
func EstadoDeTipoVisita(tipoVisita string, tieneCita bool) EstadoCuenta {
	if tieneCita {
		return EstadoCitaAUnaHora
	}
	if e, ok := estadoPorTipoVisita[tipoVisita]; ok {
		return e
	}
	return EstadoTipoVisitaDesconocido
}

// AlcanceDeTipoVisita se deriva del estado, igual que en Kotlin, para que nunca
// se contradigan.
func AlcanceDeTipoVisita(tipoVisita string, tieneCita bool) Alcance {
	return EstadoDeTipoVisita(tipoVisita, tieneCita).Alcance()
}

// TipoVisitaCatalogo es una entrada de GET /desempeno/catalogos (contrato §4b).
type TipoVisitaCatalogo struct {
	Literal string
	Estado  EstadoCuenta
	Grupo   string
}

// literalesEnOrden lista los 14, vigentes primero y retirados después (el orden
// estable de internal/visitas/domain/tipo_visita.go y del teléfono).
var literalesEnOrden = []string{
	TipoVisitaNoSeEncontraba, TipoVisitaCasaCerrada, TipoVisitaSoloMenores, TipoVisitaNoVaADarPago,
	TipoVisitaPideTiempo, TipoVisitaTienePeroNoPaga, TipoVisitaFueGrosero, TipoVisitaSeEsconde,
	TipoVisitaNoResponde, TipoVisitaSeEscuchanRuidos, TipoVisitaPideReagendar,
	TipoVisitaLegacyNoVaADarPago, TipoVisitaLegacyDijoQueNoPagara, TipoVisitaLegacySeEscondeNoSale,
}

// GrupoDeEstado nombra el grupo de un estado alcanzable desde un literal
// (comentarios de agrupación de TipoVisitaCatalogo.kt). Para el resto, "".
func GrupoDeEstado(e EstadoCuenta) string {
	switch e {
	case EstadoNoEstaba:
		return "No estaba"
	case EstadoVisiteVuelvo:
		return "Visité, vuelvo"
	case EstadoSeNego:
		return "Se negó"
	case EstadoPrometioProxima:
		return "Prometió"
	case EstadoPago, EstadoAbonoParcial, EstadoCitaAUnaHora, EstadoSinTocar:
		return ""
	}
	return ""
}

// CatalogoTiposVisita devuelve los 14 literales con su estado y grupo.
func CatalogoTiposVisita() []TipoVisitaCatalogo {
	out := make([]TipoVisitaCatalogo, 0, len(literalesEnOrden))
	for _, l := range literalesEnOrden {
		e := EstadoDeTipoVisita(l, false)
		out = append(out, TipoVisitaCatalogo{Literal: l, Estado: e, Grupo: GrupoDeEstado(e)})
	}
	return out
}
```

`internal/desempeno/domain/deriver.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"time"

	"github.com/shopspring/decimal"
)

// Ventana es el periodo de cobro [Inicio, Fin], AMBOS extremos incluidos
// (VentanaCobro.kt y rutas cobranza_repo.go: FECHA >= ? AND FECHA <= ?).
type Ventana struct {
	inicio time.Time
	fin    time.Time
}

// NuevaVentana rechaza una ventana invertida.
func NuevaVentana(inicio, fin time.Time) (Ventana, error) {
	if fin.Before(inicio) {
		return Ventana{}, ErrParametroInvalido.WithField("ventana", "fin anterior a inicio")
	}
	return Ventana{inicio: inicio.UTC(), fin: fin.UTC()}, nil
}

// Inicio del periodo.
func (v Ventana) Inicio() time.Time { return v.inicio }

// Fin del periodo.
func (v Ventana) Fin() time.Time { return v.fin }

// Contiene es inclusivo en los dos extremos.
func (v Ventana) Contiene(t time.Time) bool { return !t.Before(v.inicio) && !t.After(v.fin) }

// CuentaPeriodo es una venta a crédito del universo de la semana.
type CuentaPeriodo struct {
	VentaID     int
	ClienteID   int
	Parcialidad decimal.Decimal
}

// PagoPeriodo es un pago (o condonación) de la zona. ActividadID enlaza con la
// Actividad que lo originó ("pago:..."); en el juego de casos va vacío.
type PagoPeriodo struct {
	VentaID      int
	Importe      decimal.Decimal
	FormaCobroID int
	FechaHora    time.Time
	Rechazado    bool
	ActividadID  string
}

// VisitaPeriodo es una visita de la zona. VentaID nil = sin venta ligada
// (MSP_VISITAS.IMPTE_DOCTO_CC_ID nulo o 0). FechaPromesa, FechaCita y HoraCita
// existen para el juego de casos y la fase 2; hoy el servidor no tiene esas
// columnas y siempre llegan nil (spec §9).
type VisitaPeriodo struct {
	ClienteID    int
	VentaID      *int
	TipoVisita   string
	FechaHora    time.Time
	FechaPromesa *Fecha
	FechaCita    *Fecha
	HoraCita     *HoraDelDia
	ActividadID  string
}

// ResultadoCuenta es el estado derivado de una venta y lo que lo justifica.
type ResultadoCuenta struct {
	Estado       EstadoCuenta
	AbonoVentana decimal.Decimal
	Parcialidad  decimal.Decimal
	FechaPromesa *Fecha
	FechaCita    *Fecha
	HoraCita     *HoraDelDia
	// ConAlgo: al menos un pago de cualquier forma (rechazado y condonación
	// incluidos) o una visita que le aplique, dentro de la ventana (spec §2).
	ConAlgo bool
	// DeterminadoPor es el ActividadID que fijó el estado: el pago de dinero más
	// reciente o la visita aplicable más reciente. Vacío en SIN_TOCAR.
	DeterminadoPor string
	// UltimaActividadID / UltimaActividadHora: lo más reciente que le aplica
	// (cualquier pago de la venta o visita aplicable). Vacío si ConAlgo es false.
	UltimaActividadID   string
	UltimaActividadHora time.Time
}

// Incidencias espeja IncidenciaCobranza.kt: conteos sin PII.
type Incidencias struct {
	TipoVisitaFueraDeCatalogo int
	ParcialidadNoPositiva     int
	VisitaSinVenta            int
}

// Derivacion trae una entrada por cada cuenta de la entrada, siempre.
type Derivacion struct {
	PorVenta    map[int]ResultadoCuenta
	Incidencias Incidencias
}

// EstadoPorDinero es EstadoCuentaDeriver.estadoPorDinero. ok=false significa
// "no entró dinero": quien pregunta debe seguir con las visitas.
func EstadoPorDinero(abono, parcialidad decimal.Decimal) (EstadoCuenta, bool) {
	switch {
	case abono.Sign() <= 0:
		return "", false
	case parcialidad.Sign() <= 0:
		return EstadoAbonoParcial, true
	case abono.GreaterThanOrEqual(parcialidad):
		return EstadoPago, true
	default:
		return EstadoAbonoParcial, true
	}
}

// Derivar es el port de EstadoCuentaDeriver.derivar más la cobertura
// (CoberturaDelPeriodo.ventasConAlgo del plan Android). Precedencia:
//  1. El dinero manda: suma de pagos de la ventana con forma 157/158/52569 y
//     no rechazados.
//  2. Sin dinero, la visita más reciente de la ventana que le aplique (las de
//     alcance venta con esa venta + las de alcance cliente de su cliente; en
//     empate exacto gana la de venta, y entre iguales la primera de la entrada).
//  3. SIN_TOCAR.
func Derivar(cuentas []CuentaPeriodo, pagos []PagoPeriodo, visitas []VisitaPeriodo, v Ventana) Derivacion {
	sp := sumarPagos(pagos, v)
	delPeriodo := make([]VisitaPeriodo, 0, len(visitas))
	for _, vis := range visitas {
		if v.Contiene(vis.FechaHora) {
			delPeriodo = append(delPeriodo, vis)
		}
	}
	ix := indexarVisitas(delPeriodo)

	out := Derivacion{PorVenta: make(map[int]ResultadoCuenta, len(cuentas))}
	for _, c := range cuentas {
		out.PorVenta[c.VentaID] = resolverCuenta(c, sp, ix)
	}
	out.Incidencias = contarIncidencias(cuentas, sp, delPeriodo, ix)
	return out
}

// pagosSumados: lo que la derivación necesita de los pagos de la ventana.
type pagosSumados struct {
	abonos           map[int]decimal.Decimal
	ultimoDinero     map[int]PagoPeriodo
	ultimoCualquiera map[int]PagoPeriodo
}

func masReciente(m map[int]PagoPeriodo, p PagoPeriodo) {
	if prev, ok := m[p.VentaID]; !ok || p.FechaHora.After(prev.FechaHora) {
		m[p.VentaID] = p
	}
}

func sumarPagos(pagos []PagoPeriodo, v Ventana) pagosSumados {
	sp := pagosSumados{
		abonos:           map[int]decimal.Decimal{},
		ultimoDinero:     map[int]PagoPeriodo{},
		ultimoCualquiera: map[int]PagoPeriodo{},
	}
	for _, p := range pagos {
		if !v.Contiene(p.FechaHora) {
			continue
		}
		masReciente(sp.ultimoCualquiera, p)
		if p.Rechazado || !EsFormaDeDinero(p.FormaCobroID) {
			continue
		}
		sp.abonos[p.VentaID] = sp.abonos[p.VentaID].Add(p.Importe)
		masReciente(sp.ultimoDinero, p)
	}
	return sp
}

func resolverCuenta(c CuentaPeriodo, sp pagosSumados, ix indiceVisitas) ResultadoCuenta {
	abono := sp.abonos[c.VentaID]
	r := ResultadoCuenta{AbonoVentana: abono, Parcialidad: c.Parcialidad, Estado: EstadoSinTocar}
	visita, hayVisita := ix.masRecientePara(c)
	if e, ok := EstadoPorDinero(abono, c.Parcialidad); ok {
		r.Estado = e
		r.DeterminadoPor = sp.ultimoDinero[c.VentaID].ActividadID
	} else if hayVisita {
		r.Estado = EstadoDeTipoVisita(visita.TipoVisita, visita.FechaCita != nil)
		r.FechaPromesa, r.FechaCita, r.HoraCita = visita.FechaPromesa, visita.FechaCita, visita.HoraCita
		r.DeterminadoPor = visita.ActividadID
	}

	pago, hayPago := sp.ultimoCualquiera[c.VentaID]
	r.ConAlgo = hayPago || hayVisita
	switch {
	case hayVisita && (!hayPago || visita.FechaHora.After(pago.FechaHora)):
		r.UltimaActividadID, r.UltimaActividadHora = visita.ActividadID, visita.FechaHora
	case hayPago:
		r.UltimaActividadID, r.UltimaActividadHora = pago.ActividadID, pago.FechaHora
	}
	return r
}

func contarIncidencias(cuentas []CuentaPeriodo, sp pagosSumados, delPeriodo []VisitaPeriodo, ix indiceVisitas) Incidencias {
	inc := Incidencias{VisitaSinVenta: ix.huerfanas}
	for _, vis := range delPeriodo {
		if !EsTipoVisitaConocido(vis.TipoVisita) {
			inc.TipoVisitaFueraDeCatalogo++
		}
	}
	for _, c := range cuentas {
		if c.Parcialidad.Sign() <= 0 && sp.abonos[c.VentaID].Sign() > 0 {
			inc.ParcialidadNoPositiva++
		}
	}
	return inc
}

// indiceVisitas es IndiceVisitas.kt: visitas del periodo por cómo se propagan.
type indiceVisitas struct {
	porVenta   map[int][]VisitaPeriodo
	porCliente map[int][]VisitaPeriodo
	huerfanas  int
}

func indexarVisitas(delPeriodo []VisitaPeriodo) indiceVisitas {
	ix := indiceVisitas{porVenta: map[int][]VisitaPeriodo{}, porCliente: map[int][]VisitaPeriodo{}}
	for _, vis := range delPeriodo {
		switch {
		case AlcanceDeTipoVisita(vis.TipoVisita, vis.FechaCita != nil) == AlcanceCliente:
			ix.porCliente[vis.ClienteID] = append(ix.porCliente[vis.ClienteID], vis)
		case vis.VentaID != nil:
			ix.porVenta[*vis.VentaID] = append(ix.porVenta[*vis.VentaID], vis)
		default:
			ix.huerfanas++
		}
	}
	return ix
}

// masRecientePara: propias primero y luego las del cliente; gana el PRIMER
// máximo (maxByOrNull de Kotlin), así que en empate gana la de venta.
func (ix indiceVisitas) masRecientePara(c CuentaPeriodo) (VisitaPeriodo, bool) {
	var mejor VisitaPeriodo
	hay := false
	for _, lista := range [][]VisitaPeriodo{ix.porVenta[c.VentaID], ix.porCliente[c.ClienteID]} {
		for _, vis := range lista {
			if !hay || vis.FechaHora.After(mejor.FechaHora) {
				mejor, hay = vis, true
			}
		}
	}
	return mejor, hay
}
```

`internal/desempeno/domain/presentacion.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "strconv"

// Presentación de un estado, port de msp-app-kt feature/pagos/.../ui/
// EstadoCuentaUi.kt (tratoDe, etiquetaDe, detalleDe) y SegmentoDeCobranza.kt.
// Las cadenas son las del teléfono, byte a byte: oficina y cobrador leen lo mismo.

// mesesCortos es el formato "MMM" de es-MX que usa el teléfono
// (DateTimeFormatter "d MMM", BUSINESS_LOCALE). LEÍDO: "sept" en
// EstadoCuentaUiTest.kt:128,150; el resto de los meses es INFERIDO de CLDR
// es-MX (ver la decisión D-API-6).
var mesesCortos = [...]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"}

func diaYMes(f Fecha) string {
	return strconv.Itoa(f.Dia()) + " " + mesesCortos[f.Mes()-1]
}

// TratoDe es EstadoCuentaUi.tratoDe. Una promesa sin fecha y una cita sin hora
// se tratan como "regresas": nada sostiene sacarlas de la ruta. Un código fuera
// de los ocho se trata como sin trabajar.
func TratoDe(r ResultadoCuenta) Trato {
	switch r.Estado {
	case EstadoPago:
		return TratoPagado
	case EstadoAbonoParcial:
		return TratoParcial
	case EstadoVisiteVuelvo:
		return TratoRegresas
	case EstadoPrometioProxima:
		if r.FechaPromesa == nil {
			return TratoRegresas
		}
		return TratoDiferido
	case EstadoSeNego:
		return TratoEscalar
	case EstadoCitaAUnaHora:
		if r.HoraCita == nil {
			return TratoRegresas
		}
		return TratoCita
	case EstadoNoEstaba:
		return TratoNadie
	case EstadoSinTocar:
		return TratoSinTrabajar
	}
	return TratoSinTrabajar
}

// textoRegresas distingue los tres caminos a "regresas" (promesa sin fecha,
// cita sin hora, el resto).
func textoRegresas(e EstadoCuenta, promesa, cita, otro string) string {
	if e == EstadoPrometioProxima {
		return promesa
	}
	if e == EstadoCitaAUnaHora {
		return cita
	}
	return otro
}

// EtiquetaDe es EstadoCuentaUi.etiquetaDe. El monto prometido no existe en el
// servidor (contrato §7), así que la promesa se lee "Prometió el 25 sept".
func EtiquetaDe(r ResultadoCuenta) string {
	switch TratoDe(r) {
	case TratoPagado:
		return "Pagó esta semana"
	case TratoParcial:
		return "Abonó parcial"
	case TratoRegresas:
		return textoRegresas(r.Estado, "Prometió sin fecha", "Cita sin hora", "Visité, vuelvo")
	case TratoDiferido:
		return "Prometió el " + diaYMes(*r.FechaPromesa)
	case TratoEscalar:
		return "Se negó"
	case TratoCita:
		dia := ""
		if r.FechaCita != nil {
			dia = diaYMes(*r.FechaCita) + " "
		}
		return "Cita " + dia + r.HoraCita.String()
	case TratoNadie:
		return "No estaba"
	case TratoSinTrabajar:
		return "Falta pasar"
	}
	return "Falta pasar"
}

// DetalleDe es EstadoCuentaUi.detalleDe.
func DetalleDe(r ResultadoCuenta) string {
	switch TratoDe(r) {
	case TratoPagado:
		return "No hace falta volver"
	case TratoParcial:
		return "Falta por confirmar"
	case TratoRegresas:
		return textoRegresas(r.Estado, "Sin fecha, regresas", "Sin hora, regresas", "Regresas esta semana")
	case TratoDiferido:
		return "No cae esta semana"
	case TratoEscalar:
		return "Escalar esta cuenta"
	case TratoCita:
		return "Quedaron de verse"
	case TratoNadie:
		return "Regresas esta semana"
	case TratoSinTrabajar:
		return "Todavía no vas"
	}
	return "Todavía no vas"
}

// SegmentoDe es SegmentoDeCobranza.contiene invertido: el segmento de la cuenta
// o ok=false si no cae en ninguno (compromiso con trato propio y sin fecha).
// hoy es el día de negocio (America/Mexico_City).
func SegmentoDe(r ResultadoCuenta, hoy Fecha) (Segmento, bool) {
	switch TratoDe(r) {
	case TratoSinTrabajar:
		return SegmentoSinVisitar, true
	case TratoParcial, TratoRegresas, TratoNadie:
		return SegmentoVolver, true
	case TratoEscalar:
		return SegmentoDespues, true
	case TratoDiferido:
		return segmentoPorFecha(r.FechaPromesa, hoy)
	case TratoCita:
		return segmentoPorFecha(r.FechaCita, hoy)
	case TratoPagado:
		return SegmentoPagados, true
	}
	return SegmentoSinVisitar, true
}

func segmentoPorFecha(f *Fecha, hoy Fecha) (Segmento, bool) {
	if f == nil {
		return "", false
	}
	if f.Despues(hoy) {
		return SegmentoDespues, true
	}
	return SegmentoVolver, true
}
```


- [ ] **Step 5: Correr y ver que pasan**

Run: `go test -cover ./internal/desempeno/domain/`
Expected: PASS; los 40 subtests de `TestCasosEstadoCuenta_CadaCasoContraElDeriver` en verde.

- [ ] **Step 6: Control positivo (la prueba se pone roja al revertir la regla)**

En `deriver.go`, cambiar `if p.Rechazado || !EsFormaDeDinero(p.FormaCobroID) {` por `if !EsFormaDeDinero(p.FormaCobroID) {` y correr `go test ./internal/desempeno/domain/ 2>&1 | grep -- '--- FAIL'`.
Expected: fallan `009-rechazado-no-es-dinero.json`, `010-rechazado-no-se-suma-al-aceptado.json` y `TestDerivar_RechazadoNoEsDineroPeroSiCobertura`. Restaurar la línea, correr otra vez (PASS) y `git diff --stat` para confirmar que no quedó el cambio. El hash tiene su propio control positivo automático (`TestCasosEstadoCuenta_ControlPositivoDelHash`: agrega un byte a una copia y exige otro hash).

- [ ] **Step 7: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/domain testdata/estado-cuenta
git commit -m "feat(desempeno): estado por venta portado del teléfono y juego de casos compartido"
```

### Tarea A2: jornada (pausas, señales, tramos, por hora), registrador y cobro

**Files:**
- Create: `internal/desempeno/domain/duracion.go`, `jornada.go`, `senales.go`, `registrador.go`, `cobro.go`
- Test: `internal/desempeno/domain/jornada_test.go`, `senales_test.go`, `registrador_test.go`, `cobro_test.go`

**Interfaces:**
- Consumes: Tarea 0 (`Actividad`, `UsuarioCobranza`, `CobroSemana`, `Fecha`, `HoraDelDia`, `CapturaTardiaUmbral`).
- Produces: `ParametrosJornada{PausaMin, Horario *Horario}` + `NuevosParametrosJornada(pausaMin, desdeHora, hastaHora string)`, `Umbral()`; `Pausa`, `ConteoImporte`, `Tramo`, `Hora`, `Punto{ID, Tipo, Hora, ClienteNombre, Importe *decimal.Decimal, Resultado, NumFotos}`, `LineaDelDia`, `ResumenPausas`, `ResumenJornada`, `Jornada`; `OrdenarActividades`, `CalcularJornada(acts, p, dia, loc, ahora) Jornada`; `EstadoAhora` + `CalcularEstadoAhora(hoy Jornada, p, ahora) (EstadoAhora, *time.Time)`; `TipoSenal`, `Senal`, `DetectarSenales`, `DistanciaMetros`, constantes `MismoPuntoMinClientes=4`, `MismoPuntoRadioMetros=25`, `MismoPuntoVentana=10 min`; `FormatearDuracion`; `IndiceUsuarios` (`NuevoIndiceUsuarios`, `Completar(Actividad) Actividad`), `RegistradaPor(Actividad, UsuarioCobranza) bool`; `Cobro{Cobrado, Esperado, Falta, Pct}` + `CalcularCobro(CobroSemana) Cobro`.

- [ ] **Step 1: Escribir las pruebas que fallan**

`internal/desempeno/domain/jornada_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// hl construye un instante a la hora local de CDMX del 2026-10-14.
func hl(t *testing.T, hhmm string) time.Time {
	t.Helper()
	tt, err := time.ParseInLocation("2006-01-02 15:04:05", "2026-10-14 "+hhmm, cdmx(t))
	require.NoError(t, err)
	return tt.UTC()
}

func act(id string, tipo domain.TipoActividad, h time.Time, cliente int) domain.Actividad {
	a := domain.Actividad{
		ID: id, Tipo: tipo, Hora: h, ClienteID: cliente,
		Lat: f64(18.4617), Lng: f64(-97.3928),
	}
	switch tipo {
	case domain.TipoPago:
		a.Pago = &domain.DatosPago{Importe: dinero("150.00"), FormaCobroID: 157, Aplicado: true}
	case domain.TipoCondonacion:
		a.Pago = &domain.DatosPago{Importe: dinero("50.00"), FormaCobroID: 137026, Aplicado: true}
	case domain.TipoVisita:
		a.Visita = &domain.DatosVisita{TipoVisita: domain.TipoVisitaNoSeEncontraba}
	}
	return a
}

func params(t *testing.T, pausa, desde, hasta string) domain.ParametrosJornada {
	t.Helper()
	p, err := domain.NuevosParametrosJornada(pausa, desde, hasta)
	require.NoError(t, err)
	return p
}

var dia14 = func() domain.Fecha { f, _ := domain.ParseFecha("2026-10-14"); return f }()

func TestNuevosParametrosJornada(t *testing.T) {
	t.Parallel()
	p := params(t, "", "", "")
	assert.Equal(t, 45, p.PausaMin)
	assert.Nil(t, p.Horario)
	assert.Equal(t, 45*time.Minute, p.Umbral())

	p = params(t, "5", "08:00", "18:00")
	assert.Equal(t, 5, p.PausaMin)
	require.NotNil(t, p.Horario)
	assert.Equal(t, "08:00", p.Horario.Desde.String())

	assert.Equal(t, 240, params(t, "240", "", "").PausaMin)

	malos := [][3]string{
		{"4", "", ""},
		{"241", "", ""},
		{"abc", "", ""},
		{"45.5", "", ""},
		{"", "08:00", ""},
		{"", "", "18:00"},
		{"", "18:00", "08:00"},
		{"", "08:00", "08:00"},
		{"", "8:00", "18:00"},
		{"", "08:00", "25:00"},
	}
	for _, m := range malos {
		_, err := domain.NuevosParametrosJornada(m[0], m[1], m[2])
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "entrada %v", m)
	}
}

func TestCalcularJornada_UmbralExacto(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	ahora := hl(t, "23:00:00")
	p := params(t, "45", "", "")
	casos := []struct {
		nombre string
		hueco  time.Duration
		pausas int
	}{
		{"44 min no es pausa", 44 * time.Minute, 0},
		{"44 min 59 s no es pausa", 44*time.Minute + 59*time.Second, 0},
		{"45 min exactos sí es pausa", 45 * time.Minute, 1},
		{"46 min es pausa", 46 * time.Minute, 1},
	}
	for _, c := range casos {
		a1 := act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)
		a2 := act("pago:2", domain.TipoPago, hl(t, "10:00:00").Add(c.hueco), 2)
		// el día ya terminó para la pausa en curso: se evalúa con "ahora" de otro día
		j := domain.CalcularJornada([]domain.Actividad{a2, a1}, p, dia14, loc, ahora.Add(24*time.Hour))
		require.Len(t, j.Linea.Pausas, c.pausas, c.nombre)
		if c.pausas == 1 {
			pa := j.Linea.Pausas[0]
			assert.Equal(t, "pago:1", pa.AnteriorID, c.nombre)
			assert.Equal(t, "pago:2", pa.SiguienteID, c.nombre)
			assert.Equal(t, int(c.hueco/time.Minute), pa.Minutos, c.nombre)
			assert.False(t, pa.EnCurso, c.nombre)
		}
	}
}

func TestCalcularJornada_DiaVacioYUnaSolaActividad(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "", "", "")
	ayer := hl(t, "12:00:00").Add(24 * time.Hour)

	vacio := domain.CalcularJornada(nil, p, dia14, loc, ayer)
	assert.Empty(t, vacio.Actividades)
	assert.Empty(t, vacio.Linea.Pausas)
	assert.Empty(t, vacio.Linea.Tramos)
	assert.Len(t, vacio.Linea.PorHora, 13)
	assert.Nil(t, vacio.Resumen.PrimeraActividad)
	assert.Equal(t, 0, vacio.Resumen.MinutosActivos)
	assert.Empty(t, vacio.Senales)

	una := domain.CalcularJornada([]domain.Actividad{act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)}, p, dia14, loc, ayer)
	assert.Empty(t, una.Linea.Pausas)
	require.Len(t, una.Linea.Tramos, 1)
	assert.Equal(t, 0, una.Resumen.MinutosActivos)
	assert.Equal(t, 1, una.Resumen.Pagos.N)
}

func TestCalcularJornada_MedianocheParteLosDias(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "", "", "")
	noche := act("pago:1", domain.TipoPago, hl(t, "23:50:00"), 1)
	madrugada := act("pago:2", domain.TipoPago, hl(t, "23:50:00").Add(20*time.Minute), 2) // 00:10 del 15
	j := domain.CalcularJornada([]domain.Actividad{noche, madrugada}, p, dia14, loc, hl(t, "23:59:00").Add(48*time.Hour))
	require.Len(t, j.Actividades, 1, "la de las 00:10 es del día 15")
	assert.Equal(t, "pago:1", j.Actividades[0].ID)
	assert.Empty(t, j.Linea.Pausas)
}

func TestCalcularJornada_PausaEnCursoSoloHoy(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)}

	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "10:45:00"))
	require.Len(t, j.Linea.Pausas, 1)
	assert.True(t, j.Linea.Pausas[0].EnCurso)
	assert.Empty(t, j.Linea.Pausas[0].SiguienteID)
	assert.Equal(t, 45, j.Linea.Pausas[0].Minutos)
	assert.True(t, j.Resumen.Pausas.EnCurso)

	j = domain.CalcularJornada(acts, p, dia14, loc, hl(t, "10:44:00"))
	assert.Empty(t, j.Linea.Pausas, "44 min después todavía no es pausa")

	j = domain.CalcularJornada(acts, p, dia14, loc, hl(t, "10:00:00").Add(30*time.Hour))
	assert.Empty(t, j.Linea.Pausas, "un día pasado no tiene pausa en curso")
}

func TestCalcularJornada_TramosPorHoraYResumen(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{
		act("pago:1", domain.TipoPago, hl(t, "06:59:00"), 1),
		act("visita:a", domain.TipoVisita, hl(t, "07:10:00"), 2),
		act("pago:2", domain.TipoPago, hl(t, "07:30:00"), 2),
		act("pago:3", domain.TipoCondonacion, hl(t, "07:40:00"), 3),
		// pausa de 80 min
		act("pago:4", domain.TipoPago, hl(t, "09:00:00"), 4),
		act("visita:b", domain.TipoVisita, hl(t, "19:59:00"), 5),
		act("pago:5", domain.TipoPago, hl(t, "20:00:00"), 6),
	}
	noAplicado := act("pago:6", domain.TipoPago, hl(t, "09:10:00"), 4)
	noAplicado.Pago.Aplicado = false
	acts = append(acts, noAplicado)

	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "12:00:00").Add(24*time.Hour))
	require.Len(t, j.Actividades, 8)
	require.Len(t, j.Linea.Pausas, 2, "07:40→09:00 y 09:10→19:59")
	require.Len(t, j.Linea.Tramos, 3)

	t1 := j.Linea.Tramos[0]
	assert.Equal(t, hl(t, "06:59:00"), t1.Desde)
	assert.Equal(t, hl(t, "07:40:00"), t1.Hasta)
	assert.Equal(t, 2, t1.Pagos.N)
	assert.Equal(t, "300.00", t1.Pagos.Importe.StringFixed(2))
	assert.Equal(t, 1, t1.Visitas)
	assert.Equal(t, 1, t1.Condonaciones)
	assert.Equal(t, 3, t1.Clientes)

	t2 := j.Linea.Tramos[1]
	assert.Equal(t, 1, t2.Pagos.N, "el no aplicado no cuenta como dinero")
	assert.Equal(t, 1, t2.Clientes)

	require.Len(t, j.Linea.PorHora, 13)
	assert.Equal(t, 7, j.Linea.PorHora[0].Hora)
	assert.Equal(t, 19, j.Linea.PorHora[12].Hora)
	assert.Equal(t, 1, j.Linea.PorHora[0].Pagos, "06:59 queda fuera; 07:30 entra")
	assert.Equal(t, 1, j.Linea.PorHora[0].Visitas)
	assert.Equal(t, 1, j.Linea.PorHora[0].Condonaciones)
	assert.Equal(t, "150.00", j.Linea.PorHora[0].Importe.StringFixed(2))
	assert.Equal(t, 1, j.Linea.PorHora[12].Visitas, "19:59 entra en la hora 19")
	assert.Equal(t, 0, j.Linea.PorHora[12].Pagos, "20:00 queda fuera")

	r := j.Resumen
	assert.Equal(t, hl(t, "06:59:00"), *r.PrimeraActividad)
	assert.Equal(t, hl(t, "20:00:00"), *r.UltimaActividad)
	// 06:59→20:00 = 781 min, menos pausas de 80 y 649 min.
	assert.Equal(t, 781-80-649, r.MinutosActivos)
	assert.Equal(t, domain.ResumenPausas{N: 2, Minutos: 80 + 649}, r.Pausas)
	assert.Equal(t, 4, r.Pagos.N)
	assert.Equal(t, "600.00", r.Pagos.Importe.StringFixed(2))
	assert.Equal(t, 1, r.NoAplicados.N)
	assert.Equal(t, 1, r.Condonaciones.N)
	assert.Equal(t, 2, r.Visitas)
	assert.Equal(t, 6, r.Clientes)
}

func TestCalcularEstadoAhora(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{
		act("pago:1", domain.TipoPago, hl(t, "08:00:00"), 1),
		act("pago:2", domain.TipoPago, hl(t, "09:30:00"), 2),
		act("pago:3", domain.TipoPago, hl(t, "09:50:00"), 3),
	}

	ahora := hl(t, "10:20:00")
	e, desde := domain.CalcularEstadoAhora(domain.CalcularJornada(acts, p, dia14, loc, ahora), p, ahora)
	assert.Equal(t, domain.EstadoAhoraActivo, e)
	assert.Equal(t, hl(t, "09:30:00"), *desde, "inicio del tramo actual")

	ahora = hl(t, "10:35:00")
	e, desde = domain.CalcularEstadoAhora(domain.CalcularJornada(acts, p, dia14, loc, ahora), p, ahora)
	assert.Equal(t, domain.EstadoAhoraSinActividad, e)
	assert.Equal(t, hl(t, "09:50:00"), *desde, "desde la última actividad")

	e, desde = domain.CalcularEstadoAhora(domain.CalcularJornada(nil, p, dia14, loc, ahora), p, ahora)
	assert.Equal(t, domain.EstadoAhoraNoHaEmpezado, e)
	assert.Nil(t, desde)
}

func TestFormatearDuracion(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "0 min", domain.FormatearDuracion(0))
	assert.Equal(t, "45 min", domain.FormatearDuracion(45))
	assert.Equal(t, "1 h", domain.FormatearDuracion(60))
	assert.Equal(t, "1 h 12 min", domain.FormatearDuracion(72))
	assert.Equal(t, "10 h 49 min", domain.FormatearDuracion(649))
}

func TestOrdenarActividades_EmpateDeHoraPorID(t *testing.T) {
	t.Parallel()
	h := hl(t, "10:00:00")
	acts := []domain.Actividad{
		act("visita:b", domain.TipoVisita, h, 1),
		act("pago:9", domain.TipoPago, h.Add(-time.Minute), 1),
		act("pago:1", domain.TipoPago, h, 1),
	}
	domain.OrdenarActividades(acts)
	assert.Equal(t, []string{"pago:9", "pago:1", "visita:b"}, []string{acts[0].ID, acts[1].ID, acts[2].ID})
	assert.Equal(t, 2026, dia14.Anio())
}

func TestCalcularJornada_PuntosLlevanElPopover(t *testing.T) {
	t.Parallel()
	pago := act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)
	pago.ClienteNombre, pago.NumFotos = "JUANA ANDRADE LECHUGA", 2
	visita := act("visita:a", domain.TipoVisita, hl(t, "10:10:00"), 2)
	visita.ClienteNombre = "ROSARIO VILLA"
	j := domain.CalcularJornada([]domain.Actividad{visita, pago}, params(t, "", "", ""), dia14, cdmx(t), hl(t, "11:00:00"))
	require.Len(t, j.Linea.Puntos, 2)
	p, v := j.Linea.Puntos[0], j.Linea.Puntos[1]
	assert.Equal(t, "JUANA ANDRADE LECHUGA", p.ClienteNombre)
	require.NotNil(t, p.Importe)
	assert.Equal(t, "150.00", p.Importe.StringFixed(2))
	assert.Empty(t, p.Resultado)
	assert.Equal(t, 2, p.NumFotos)
	assert.Nil(t, v.Importe, "importe null en visitas")
	assert.Equal(t, "No se encontraba", v.Resultado)
}
```

`internal/desempeno/domain/senales_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func senalesDe(js domain.Jornada, tipo domain.TipoSenal) []domain.Senal {
	var out []domain.Senal
	for _, s := range js.Senales {
		if s.Tipo == tipo {
			out = append(out, s)
		}
	}
	return out
}

func TestSenales_PausaLarga(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	acts := []domain.Actividad{
		act("pago:1", domain.TipoPago, hl(t, "11:05:00"), 1),
		act("pago:2", domain.TipoPago, hl(t, "12:17:00"), 2),
	}
	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "12:30:00"))
	s := senalesDe(j, domain.SenalPausaLarga)
	require.Len(t, s, 1)
	assert.Equal(t, []string{"pago:1", "pago:2"}, s[0].ActividadIDs)
	assert.Equal(t, "Pausa de 1 h 12 min (11:05–12:17)", s[0].Detalle)
	assert.Equal(t, "≥ 45 min sin registrar", s[0].Regla)

	j = domain.CalcularJornada(acts, p, dia14, loc, hl(t, "13:10:00"))
	s = senalesDe(j, domain.SenalPausaLarga)
	require.Len(t, s, 2)
	assert.Equal(t, "Pausa en curso de 53 min (desde 12:17)", s[1].Detalle)
	assert.Equal(t, []string{"pago:2"}, s[1].ActividadIDs)
}

func TestSenales_Horario(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "08:00", "18:00")
	finDelDia := hl(t, "18:00:00")
	casos := []struct {
		nombre                string
		primera, ultima       string
		ahora                 time.Time
		inicioTarde, finTempr bool
	}{
		{"empezó justo al umbral", "08:45:00", "17:30:00", finDelDia, true, false},
		{"un minuto antes del umbral", "08:44:00", "17:30:00", finDelDia, false, false},
		{"terminó justo al umbral", "08:10:00", "17:15:00", finDelDia, false, true},
		{"terminó un minuto después del umbral", "08:10:00", "17:16:00", finDelDia, false, false},
		{"antes de la hora de fin no se evalúa", "08:10:00", "12:00:00", hl(t, "17:59:00"), false, false},
	}
	for _, c := range casos {
		acts := []domain.Actividad{
			act("pago:1", domain.TipoPago, hl(t, c.primera), 1),
			act("pago:2", domain.TipoPago, hl(t, c.ultima), 2),
		}
		j := domain.CalcularJornada(acts, p, dia14, loc, c.ahora)
		assert.Len(t, senalesDe(j, domain.SenalInicioTarde), map[bool]int{true: 1, false: 0}[c.inicioTarde], c.nombre)
		assert.Len(t, senalesDe(j, domain.SenalFinTemprano), map[bool]int{true: 1, false: 0}[c.finTempr], c.nombre)
	}

	acts := []domain.Actividad{act("pago:1", domain.TipoPago, hl(t, "09:20:00"), 1)}
	j := domain.CalcularJornada(acts, p, dia14, loc, hl(t, "09:30:00"))
	s := senalesDe(j, domain.SenalInicioTarde)
	require.Len(t, s, 1)
	assert.Equal(t, "Primera actividad a las 09:20, 1 h 20 min después de las 08:00", s[0].Detalle)

	sinHorario := domain.CalcularJornada(acts, params(t, "45", "", ""), dia14, loc, hl(t, "09:30:00"))
	assert.Empty(t, senalesDe(sinHorario, domain.SenalInicioTarde), "sin horario no hay señal de horario")
}

// puntoA metros al norte del ancla (1° de latitud ≈ 111,195 m).
func alNorte(a domain.Actividad, metros float64) domain.Actividad {
	lat := *a.Lat + metros/111195.0
	a.Lat = &lat
	return a
}

func TestSenales_MismoPunto(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	base := hl(t, "10:00:00")
	grupo := func(minutosUltimo time.Duration, metrosUltimo float64, clienteUltimo int) []domain.Actividad {
		return []domain.Actividad{
			act("pago:1", domain.TipoPago, base, 1),
			alNorte(act("pago:2", domain.TipoPago, base.Add(2*time.Minute), 2), 10),
			alNorte(act("pago:3", domain.TipoPago, base.Add(4*time.Minute), 3), 20),
			alNorte(act("pago:4", domain.TipoPago, base.Add(minutosUltimo), clienteUltimo), metrosUltimo),
		}
	}
	casos := []struct {
		nombre string
		acts   []domain.Actividad
		senal  bool
	}{
		{"4 clientes en 24 m y 6 min", grupo(6*time.Minute, 24, 4), true},
		{"el cuarto a 10 min exactos", grupo(10*time.Minute, 5, 4), true},
		{"el cuarto a 10 min 1 s", grupo(10*time.Minute+time.Second, 5, 4), false},
		{"el cuarto a 26 m", grupo(6*time.Minute, 26, 4), false},
		{"solo 3 clientes distintos", grupo(6*time.Minute, 5, 1), false},
	}
	for _, c := range casos {
		j := domain.CalcularJornada(c.acts, p, dia14, loc, base.Add(time.Hour))
		s := senalesDe(j, domain.SenalMismoPunto)
		if !c.senal {
			assert.Empty(t, s, c.nombre)
			continue
		}
		require.Len(t, s, 1, c.nombre)
		assert.Len(t, s[0].ActividadIDs, 4, c.nombre)
		assert.Equal(t, "≥ 4 clientes distintos en 25 m dentro de 10 min", s[0].Regla)
	}
	j := domain.CalcularJornada(grupo(6*time.Minute, 24, 4), p, dia14, loc, base.Add(time.Hour))
	assert.Equal(t, "4 cuentas desde el mismo punto en 6 min", senalesDe(j, domain.SenalMismoPunto)[0].Detalle)

	sinGPS := grupo(6*time.Minute, 5, 4)
	sinGPS[3].Lat, sinGPS[3].Lng = f64(0), f64(0)
	j = domain.CalcularJornada(sinGPS, p, dia14, loc, base.Add(time.Hour))
	assert.Empty(t, senalesDe(j, domain.SenalMismoPunto), "una actividad sin GPS no entra al grupo")
}

func TestSenales_CapturaTardiaYSinGPS(t *testing.T) {
	t.Parallel()
	loc := cdmx(t)
	p := params(t, "45", "", "")
	a1 := act("pago:1", domain.TipoPago, hl(t, "10:00:00"), 1)
	llegada := a1.Hora.Add(2 * time.Hour)
	a1.RecibidoAt = &llegada
	a2 := act("pago:2", domain.TipoPago, hl(t, "10:10:00"), 2)
	casi := a2.Hora.Add(2*time.Hour - time.Minute)
	a2.RecibidoAt = &casi
	a3 := act("visita:c", domain.TipoVisita, hl(t, "10:20:00"), 3)
	a3.Lat, a3.Lng = f64(0), f64(0)
	a4 := act("visita:d", domain.TipoVisita, hl(t, "10:30:00"), 4)
	a4.Lat, a4.Lng = nil, nil

	j := domain.CalcularJornada([]domain.Actividad{a1, a2, a3, a4}, p, dia14, loc, hl(t, "11:00:00"))
	ct := senalesDe(j, domain.SenalCapturaTardia)
	require.Len(t, ct, 1, "2 h exactas sí; 1 h 59 min no")
	assert.Equal(t, []string{"pago:1"}, ct[0].ActividadIDs)
	assert.Equal(t, "Llegó al servidor 2 h después de capturarse", ct[0].Detalle)

	sg := senalesDe(j, domain.SenalSinGPS)
	require.Len(t, sg, 1)
	assert.Equal(t, []string{"visita:c", "visita:d"}, sg[0].ActividadIDs)
	assert.Equal(t, "2 actividades sin GPS", sg[0].Detalle)

	j = domain.CalcularJornada([]domain.Actividad{a3}, p, dia14, loc, hl(t, "11:00:00"))
	assert.Equal(t, "1 actividad sin GPS", senalesDe(j, domain.SenalSinGPS)[0].Detalle)
}

func TestDistanciaMetros(t *testing.T) {
	t.Parallel()
	d := domain.DistanciaMetros(18.4617, -97.3928, 18.4617+25/111195.0, -97.3928)
	assert.InDelta(t, 25.0, d, 0.05)
	assert.InDelta(t, 0.0, domain.DistanciaMetros(18.46, -97.39, 18.46, -97.39), 1e-9)
}
```

`internal/desempeno/domain/registrador_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestIndiceUsuarios_Completar(t *testing.T) {
	t.Parallel()
	juan := domain.UsuarioCobranza{UID: "uid-juan", Nombre: "Juan Carlos Méndez", CobradorID: 11502}
	rosa := domain.UsuarioCobranza{UID: "uid-rosa", Nombre: "Rosa Elena Pérez", CobradorID: 11600}
	lupe := domain.UsuarioCobranza{UID: "uid-lupe", Nombre: "Guadalupe Torres", CobradorID: 11600}
	ix := domain.NuevoIndiceUsuarios([]domain.UsuarioCobranza{juan, rosa, lupe})

	con := func(rp domain.RegistradoPor) domain.RegistradoPor {
		return ix.Completar(domain.Actividad{RegistradoPor: rp}).RegistradoPor
	}

	assert.Equal(t, domain.RegistradoPor{CobradorID: 11502, Nombre: "Oficina", Origen: domain.OrigenOficina},
		con(domain.RegistradoPor{UsuarioUID: "x", CobradorID: 11502, Nombre: "JUAN", Origen: domain.OrigenOficina}))

	assert.Equal(t, domain.RegistradoPor{UsuarioUID: "uid-juan", CobradorID: 11502, Nombre: "Juan Carlos Méndez", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{UsuarioUID: "uid-juan", Nombre: "JUAN CARLOS", Origen: domain.OrigenApp}),
		"uid resuelto por CREATED_BY toma nombre y cobrador de Firestore")

	assert.Equal(t, domain.RegistradoPor{UsuarioUID: "uid-juan", CobradorID: 11502, Nombre: "Juan Carlos Méndez", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{CobradorID: 11502, Nombre: "JUAN CARLOS", Origen: domain.OrigenApp}),
		"sin uid pero COBRADOR_ID de un solo usuario")

	assert.Equal(t, domain.RegistradoPor{CobradorID: 11600, Nombre: "ROSA", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{CobradorID: 11600, Nombre: "ROSA", Origen: domain.OrigenApp}),
		"COBRADOR_ID compartido: no se adivina")

	assert.Equal(t, domain.RegistradoPor{UsuarioUID: "uid-baja", Nombre: "PEDRO", Origen: domain.OrigenApp},
		con(domain.RegistradoPor{UsuarioUID: "uid-baja", Nombre: "PEDRO", Origen: domain.OrigenApp}),
		"uid que ya no está en Firestore conserva el nombre crudo")
}

func TestRegistradaPor(t *testing.T) {
	t.Parallel()
	rosa := domain.UsuarioCobranza{UID: "uid-rosa", CobradorID: 11600}
	lupe := domain.UsuarioCobranza{UID: "uid-lupe", CobradorID: 11600}
	porUID := domain.Actividad{RegistradoPor: domain.RegistradoPor{UsuarioUID: "uid-rosa", CobradorID: 11600, Origen: domain.OrigenApp}}
	compartida := domain.Actividad{RegistradoPor: domain.RegistradoPor{CobradorID: 11600, Origen: domain.OrigenApp}}
	oficina := domain.Actividad{RegistradoPor: domain.RegistradoPor{CobradorID: 11600, Origen: domain.OrigenOficina}}

	assert.True(t, domain.RegistradaPor(porUID, rosa))
	assert.False(t, domain.RegistradaPor(porUID, lupe), "con uid manda el uid aunque compartan cobrador")
	assert.True(t, domain.RegistradaPor(compartida, rosa))
	assert.True(t, domain.RegistradaPor(compartida, lupe))
	assert.False(t, domain.RegistradaPor(oficina, rosa), "lo de oficina no es de nadie")
	assert.False(t, domain.RegistradaPor(compartida, domain.UsuarioCobranza{UID: "x"}), "cobrador 0 no empata")
}
```

`internal/desempeno/domain/cobro_test.go` (archivo nuevo, completo):

```go
package domain_test

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestCalcularCobro(t *testing.T) {
	t.Parallel()
	pct := decimal.RequireFromString("65.90")
	c := domain.CalcularCobro(domain.CobroSemana{
		Ventas: []domain.CobroVenta{
			{VentaID: 1, Parcialidad: dinero("150"), AbonoSemana: dinero("150"), AplicaPonderado: true},
			{VentaID: 2, Parcialidad: dinero("200"), AbonoSemana: dinero("0"), AplicaPonderado: true},
			{VentaID: 3, Parcialidad: dinero("300"), AbonoSemana: dinero("100"), AplicaPonderado: false},
		},
		PctPonderado: &pct,
	})
	assert.Equal(t, "250.00", c.Cobrado.StringFixed(2))
	assert.Equal(t, "350.00", c.Esperado.StringFixed(2))
	assert.Equal(t, "100.00", c.Falta.StringFixed(2), "esperado − cobrado")
	require.NotNil(t, c.Pct)
	assert.Equal(t, "65.90", c.Pct.StringFixed(2))

	vacio := domain.CalcularCobro(domain.CobroSemana{})
	assert.Equal(t, "0.00", vacio.Cobrado.StringFixed(2))
	assert.Equal(t, "0.00", vacio.Falta.StringFixed(2))

	sobrado := domain.CalcularCobro(domain.CobroSemana{Ventas: []domain.CobroVenta{
		{VentaID: 1, Parcialidad: dinero("150"), AbonoSemana: dinero("400"), AplicaPonderado: true},
	}})
	assert.Equal(t, "0.00", sobrado.Falta.StringFixed(2), "cobrar de más no da falta negativa")
	assert.Nil(t, vacio.Pct)
}
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test ./internal/desempeno/domain/`
Expected: FAIL de compilación (`undefined: domain.CalcularJornada`, `undefined: domain.NuevoIndiceUsuarios`, …).

- [ ] **Step 3: Implementación**

`internal/desempeno/domain/duracion.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"strconv"
	"time"
)

// FormatearDuracion devuelve "45 min", "1 h", "1 h 12 min" (texto de la pantalla).
func FormatearDuracion(minutos int) string {
	if minutos < 60 {
		return strconv.Itoa(minutos) + " min"
	}
	h, m := minutos/60, minutos%60
	if m == 0 {
		return strconv.Itoa(h) + " h"
	}
	return strconv.Itoa(h) + " h " + strconv.Itoa(m) + " min"
}

// horaLocal da "HH:MM" del instante en loc.
func horaLocal(t time.Time, loc *time.Location) string { return t.In(loc).Format("15:04") }

// minutosEntre cuenta los minutos completos (piso) desde a hasta b.
func minutosEntre(a, b time.Time) int { return int(b.Sub(a) / time.Minute) }
```

`internal/desempeno/domain/jornada.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
)

// Umbral de pausa (contrato §0): entero 5–240, 45 por omisión.
const (
	PausaMinPorOmision = 45
	PausaMinMinimo     = 5
	PausaMinMaximo     = 240
)

// Horas de la vista "Por hora": una entrada por hora 7..19 ([h, h+1)).
const (
	PrimeraHoraDelDia = 7
	UltimaHoraDelDia  = 19
)

// Horario es el horario opcional de la jornada.
type Horario struct {
	Desde HoraDelDia
	Hasta HoraDelDia
}

// ParametrosJornada lleva el umbral de pausa y horario opcional. Viajan en la consulta
// y nunca se guardan (D7).
type ParametrosJornada struct {
	PausaMin int
	Horario  *Horario
}

// NuevosParametrosJornada valida los parámetros crudos de la consulta:
// pausa_min vacío = 45; desde_hora y hasta_hora ambos o ninguno y desde < hasta.
func NuevosParametrosJornada(pausaMin, desdeHora, hastaHora string) (ParametrosJornada, error) {
	p := ParametrosJornada{PausaMin: PausaMinPorOmision}
	if pausaMin != "" {
		n, err := strconv.Atoi(pausaMin)
		if err != nil || n < PausaMinMinimo || n > PausaMinMaximo {
			return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "pausa_min")
		}
		p.PausaMin = n
	}
	if (desdeHora == "") != (hastaHora == "") {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "desde_hora/hasta_hora")
	}
	if desdeHora == "" {
		return p, nil
	}
	d, err := ParseHoraDelDia(desdeHora)
	if err != nil {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "desde_hora")
	}
	h, err := ParseHoraDelDia(hastaHora)
	if err != nil {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "hasta_hora")
	}
	if d.Minutos() >= h.Minutos() {
		return ParametrosJornada{}, ErrParametroInvalido.WithField("parametro", "desde_hora/hasta_hora")
	}
	p.Horario = &Horario{Desde: d, Hasta: h}
	return p, nil
}

// Umbral devuelve el umbral de pausa como duración.
func (p ParametrosJornada) Umbral() time.Duration { return time.Duration(p.PausaMin) * time.Minute }

// Pausa es un hueco ≥ umbral entre dos actividades consecutivas del mismo día, o
// desde la última actividad hasta ahora si el día es hoy (EnCurso).
type Pausa struct {
	Desde       time.Time
	Hasta       time.Time
	Minutos     int
	AnteriorID  string
	SiguienteID string // "" en la pausa en curso
	EnCurso     bool
}

// ConteoImporte es `{n, importe}`.
type ConteoImporte struct {
	N       int
	Importe decimal.Decimal
}

func (c *ConteoImporte) sumar(importe decimal.Decimal) {
	c.N++
	c.Importe = c.Importe.Add(importe)
}

// Tramo agrupa actividades seguidas sin pausa entre ellas (vista Bloques).
type Tramo struct {
	Desde         time.Time
	Hasta         time.Time
	Pagos         ConteoImporte
	Visitas       int
	Condonaciones int
	Clientes      int
}

// Hora cuenta de [h, h+1) en la zona de negocio (vista Por hora).
type Hora struct {
	Hora          int
	Pagos         int
	Visitas       int
	Condonaciones int
	Importe       decimal.Decimal
}

// Punto es una marca de la línea con lo que pinta su popover (contrato §1):
// Importe nil en visitas; Resultado = literal de la visita, "" en pagos.
type Punto struct {
	ID            string
	Tipo          TipoActividad
	Hora          time.Time
	ClienteNombre string
	Importe       *decimal.Decimal
	Resultado     string
	NumFotos      int
}

// LineaDelDia es lo que pinta la mini barra y la línea de actividad.
type LineaDelDia struct {
	Dia     Fecha
	Puntos  []Punto
	Tramos  []Tramo
	PorHora []Hora
	Pausas  []Pausa
}

// ResumenPausas es `{n, minutos, en_curso}`.
type ResumenPausas struct {
	N       int
	Minutos int
	EnCurso bool
}

// ResumenJornada es el `resumen` del contrato §3.
type ResumenJornada struct {
	PrimeraActividad *time.Time
	UltimaActividad  *time.Time
	MinutosActivos   int
	Pausas           ResumenPausas
	Pagos            ConteoImporte
	Condonaciones    ConteoImporte
	NoAplicados      ConteoImporte
	Visitas          int
	Clientes         int
}

// Jornada reúne las actividades de un usuario en un día calendario de la zona de
// negocio, con pausas, línea, señales y resumen.
type Jornada struct {
	Dia         Fecha
	Actividades []Actividad
	Linea       LineaDelDia
	Senales     []Senal
	Resumen     ResumenJornada
}

// OrdenarActividades ordena por hora y, en empate, por id (orden total estable).
func OrdenarActividades(acts []Actividad) {
	sort.SliceStable(acts, func(i, j int) bool {
		if !acts[i].Hora.Equal(acts[j].Hora) {
			return acts[i].Hora.Before(acts[j].Hora)
		}
		return acts[i].ID < acts[j].ID
	})
}

// CalcularJornada arma la jornada de dia. acts puede traer actividades de
// otros días: se descartan. ahora decide la pausa en curso (solo si dia es hoy)
// y si "terminó temprano" ya puede evaluarse.
func CalcularJornada(acts []Actividad, p ParametrosJornada, dia Fecha, loc *time.Location, ahora time.Time) Jornada {
	delDia := make([]Actividad, 0, len(acts))
	for _, a := range acts {
		if DiaDe(a.Hora, loc) == dia {
			delDia = append(delDia, a)
		}
	}
	OrdenarActividades(delDia)
	esHoy := DiaDe(ahora, loc) == dia

	pausas := calcularPausas(delDia, p.Umbral(), esHoy, ahora)
	j := Jornada{Dia: dia, Actividades: delDia}
	j.Linea = LineaDelDia{
		Dia:     dia,
		Puntos:  puntos(delDia),
		Tramos:  calcularTramos(delDia, pausas),
		PorHora: calcularPorHora(delDia, loc),
		Pausas:  pausas,
	}
	j.Resumen = resumir(delDia, pausas)
	j.Senales = DetectarSenales(delDia, pausas, p, dia, loc, ahora)
	return j
}

func calcularPausas(acts []Actividad, umbral time.Duration, esHoy bool, ahora time.Time) []Pausa {
	pausas := []Pausa{}
	for i := 1; i < len(acts); i++ {
		prev, next := acts[i-1], acts[i]
		if next.Hora.Sub(prev.Hora) >= umbral {
			pausas = append(pausas, Pausa{
				Desde: prev.Hora, Hasta: next.Hora, Minutos: minutosEntre(prev.Hora, next.Hora),
				AnteriorID: prev.ID, SiguienteID: next.ID,
			})
		}
	}
	if esHoy && len(acts) > 0 {
		ultima := acts[len(acts)-1]
		if ahora.Sub(ultima.Hora) >= umbral {
			pausas = append(pausas, Pausa{
				Desde: ultima.Hora, Hasta: ahora, Minutos: minutosEntre(ultima.Hora, ahora),
				AnteriorID: ultima.ID, EnCurso: true,
			})
		}
	}
	return pausas
}

func puntos(acts []Actividad) []Punto {
	out := make([]Punto, 0, len(acts))
	for _, a := range acts {
		p := Punto{ID: a.ID, Tipo: a.Tipo, Hora: a.Hora, ClienteNombre: a.ClienteNombre, NumFotos: a.NumFotos}
		if a.Pago != nil {
			imp := a.Pago.Importe
			p.Importe = &imp
		}
		if a.Visita != nil {
			p.Resultado = a.Visita.TipoVisita
		}
		out = append(out, p)
	}
	return out
}

func calcularTramos(acts []Actividad, pausas []Pausa) []Tramo {
	cortes := map[string]bool{} // id de la actividad que abre un tramo nuevo
	for _, p := range pausas {
		if !p.EnCurso {
			cortes[p.SiguienteID] = true
		}
	}
	tramos := []Tramo{}
	var actual *Tramo
	clientes := map[int]bool{}
	cerrar := func() {
		if actual != nil {
			actual.Clientes = len(clientes)
			tramos = append(tramos, *actual)
		}
	}
	for _, a := range acts {
		if actual == nil || cortes[a.ID] {
			cerrar()
			actual = &Tramo{Desde: a.Hora, Pagos: ConteoImporte{Importe: decimal.Zero}}
			clientes = map[int]bool{}
		}
		actual.Hasta = a.Hora
		clientes[a.ClienteID] = true
		switch {
		case a.CuentaComoDinero():
			actual.Pagos.sumar(a.Importe())
		case a.Tipo == TipoVisita:
			actual.Visitas++
		case a.Tipo == TipoCondonacion:
			actual.Condonaciones++
		}
	}
	cerrar()
	return tramos
}

func calcularPorHora(acts []Actividad, loc *time.Location) []Hora {
	out := make([]Hora, 0, UltimaHoraDelDia-PrimeraHoraDelDia+1)
	for h := PrimeraHoraDelDia; h <= UltimaHoraDelDia; h++ {
		out = append(out, Hora{Hora: h, Importe: decimal.Zero})
	}
	for _, a := range acts {
		h := a.Hora.In(loc).Hour()
		if h < PrimeraHoraDelDia || h > UltimaHoraDelDia {
			continue
		}
		e := &out[h-PrimeraHoraDelDia]
		switch {
		case a.CuentaComoDinero():
			e.Pagos++
			e.Importe = e.Importe.Add(a.Importe())
		case a.Tipo == TipoVisita:
			e.Visitas++
		case a.Tipo == TipoCondonacion:
			e.Condonaciones++
		}
	}
	return out
}

func resumir(acts []Actividad, pausas []Pausa) ResumenJornada {
	r := ResumenJornada{
		Pagos:         ConteoImporte{Importe: decimal.Zero},
		Condonaciones: ConteoImporte{Importe: decimal.Zero},
		NoAplicados:   ConteoImporte{Importe: decimal.Zero},
	}
	if len(acts) == 0 {
		return r
	}
	primera, ultima := acts[0].Hora, acts[len(acts)-1].Hora
	r.PrimeraActividad, r.UltimaActividad = &primera, &ultima
	cerradas := 0
	for _, p := range pausas {
		r.Pausas.N++
		r.Pausas.Minutos += p.Minutos
		if p.EnCurso {
			r.Pausas.EnCurso = true
		} else {
			cerradas += p.Minutos
		}
	}
	// Las pausas cerradas caen entre primera y última: la resta nunca es negativa.
	r.MinutosActivos = minutosEntre(primera, ultima) - cerradas
	clientes := map[int]bool{}
	for _, a := range acts {
		clientes[a.ClienteID] = true
		switch {
		case a.CuentaComoDinero():
			r.Pagos.sumar(a.Importe())
		case a.NoAplicado():
			r.NoAplicados.sumar(a.Importe())
		case a.Tipo == TipoCondonacion:
			r.Condonaciones.sumar(a.Importe())
		case a.Tipo == TipoVisita:
			r.Visitas++
		}
	}
	r.Clientes = len(clientes)
	return r
}

// EstadoAhora es el estado del usuario en el tablero (contrato §2).
type EstadoAhora string

// Estados ahora.
const (
	EstadoAhoraActivo       EstadoAhora = "activo"
	EstadoAhoraSinActividad EstadoAhora = "sin_actividad"
	EstadoAhoraNoHaEmpezado EstadoAhora = "no_ha_empezado"
)

// CalcularEstadoAhora sobre la jornada de HOY: activo si la última actividad
// es de hace menos del umbral (desde = inicio del tramo actual); sin actividad
// si ya pasó (desde = última actividad); no ha empezado si hoy no hay nada.
func CalcularEstadoAhora(hoy Jornada, p ParametrosJornada, ahora time.Time) (EstadoAhora, *time.Time) {
	acts := hoy.Actividades
	if len(acts) == 0 {
		return EstadoAhoraNoHaEmpezado, nil
	}
	ultima := acts[len(acts)-1].Hora
	if ahora.Sub(ultima) >= p.Umbral() {
		return EstadoAhoraSinActividad, &ultima
	}
	desde := acts[0].Hora
	if n := len(hoy.Linea.Tramos); n > 0 {
		desde = hoy.Linea.Tramos[n-1].Desde
	}
	return EstadoAhoraActivo, &desde
}
```

`internal/desempeno/domain/senales.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import (
	"math"
	"strconv"
	"time"
)

// Umbrales de las señales "para revisar" (spec §4.2), con nombre.
const (
	MismoPuntoMinClientes = 4
	MismoPuntoRadioMetros = 25.0
	MismoPuntoVentana     = 10 * time.Minute
	radioTierraMetros     = 6371000.0
	reglaMismoPunto       = "≥ 4 clientes distintos en 25 m dentro de 10 min"
	reglaCapturaTardia    = "≥ 2 h entre la captura y la llegada al servidor"
	reglaSinGPS           = "coordenadas 0,0 o ausentes"
)

// TipoSenal es uno de los seis tipos del contrato §1.
type TipoSenal string

// Tipos de señal.
const (
	SenalPausaLarga    TipoSenal = "pausa_larga"
	SenalInicioTarde   TipoSenal = "inicio_tarde"
	SenalFinTemprano   TipoSenal = "fin_temprano"
	SenalMismoPunto    TipoSenal = "mismo_punto"
	SenalCapturaTardia TipoSenal = "captura_tardia"
	SenalSinGPS        TipoSenal = "sin_gps"
)

// Senal es algo "para revisar", nunca una sanción.
type Senal struct {
	Tipo         TipoSenal
	ActividadIDs []string
	Desde        *time.Time
	Hasta        *time.Time
	Detalle      string
	Regla        string
}

// DetectarSenales corre todas las reglas sobre las actividades del día (ya
// ordenadas) y sus pausas.
func DetectarSenales(acts []Actividad, pausas []Pausa, p ParametrosJornada, dia Fecha, loc *time.Location, ahora time.Time) []Senal {
	out := []Senal{}
	out = append(out, senalesDePausa(pausas, p, loc)...)
	out = append(out, senalesDeHorario(acts, p, dia, loc, ahora)...)
	out = append(out, senalesMismoPunto(acts)...)
	out = append(out, senalesCapturaTardia(acts)...)
	if s, ok := senalSinGPS(acts); ok {
		out = append(out, s)
	}
	return out
}

func ptr(t time.Time) *time.Time { return &t }

func idsNoVacios(ids ...string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func senalesDePausa(pausas []Pausa, p ParametrosJornada, loc *time.Location) []Senal {
	regla := "≥ " + strconv.Itoa(p.PausaMin) + " min sin registrar"
	out := make([]Senal, 0, len(pausas))
	for _, pa := range pausas {
		detalle := "Pausa de " + FormatearDuracion(pa.Minutos) +
			" (" + horaLocal(pa.Desde, loc) + "–" + horaLocal(pa.Hasta, loc) + ")"
		if pa.EnCurso {
			detalle = "Pausa en curso de " + FormatearDuracion(pa.Minutos) + " (desde " + horaLocal(pa.Desde, loc) + ")"
		}
		out = append(out, Senal{
			Tipo: SenalPausaLarga, ActividadIDs: idsNoVacios(pa.AnteriorID, pa.SiguienteID),
			Desde: ptr(pa.Desde), Hasta: ptr(pa.Hasta), Detalle: detalle, Regla: regla,
		})
	}
	return out
}

// senalesDeHorario: solo con horario y con al menos una actividad. "Empezó
// tarde": primera − desde_hora ≥ umbral. "Terminó temprano": hasta_hora −
// última ≥ umbral, y solo cuando hasta_hora ya pasó (si no, el día sigue).
func senalesDeHorario(acts []Actividad, p ParametrosJornada, dia Fecha, loc *time.Location, ahora time.Time) []Senal {
	if p.Horario == nil || len(acts) == 0 {
		return nil
	}
	out := []Senal{}
	inicio := p.Horario.Desde.EnDia(dia, loc)
	fin := p.Horario.Hasta.EnDia(dia, loc)
	primera, ultima := acts[0], acts[len(acts)-1]
	if primera.Hora.Sub(inicio) >= p.Umbral() {
		out = append(out, Senal{
			Tipo: SenalInicioTarde, ActividadIDs: []string{primera.ID},
			Desde: ptr(inicio), Hasta: ptr(primera.Hora),
			Detalle: "Primera actividad a las " + horaLocal(primera.Hora, loc) + ", " +
				FormatearDuracion(minutosEntre(inicio, primera.Hora)) + " después de las " + p.Horario.Desde.String(),
			Regla: "≥ " + strconv.Itoa(p.PausaMin) + " min después de la hora de inicio",
		})
	}
	if !ahora.Before(fin) && fin.Sub(ultima.Hora) >= p.Umbral() {
		out = append(out, Senal{
			Tipo: SenalFinTemprano, ActividadIDs: []string{ultima.ID},
			Desde: ptr(ultima.Hora), Hasta: ptr(fin),
			Detalle: "Última actividad a las " + horaLocal(ultima.Hora, loc) + ", " +
				FormatearDuracion(minutosEntre(ultima.Hora, fin)) + " antes de las " + p.Horario.Hasta.String(),
			Regla: "≥ " + strconv.Itoa(p.PausaMin) + " min antes de la hora de fin",
		})
	}
	return out
}

// DistanciaMetros calcula con haversine la distancia entre dos coordenadas.
func DistanciaMetros(lat1, lng1, lat2, lng2 float64) float64 {
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLng := (lng2 - lng1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * radioTierraMetros * math.Asin(math.Sqrt(a))
}

// senalesMismoPunto: ancla en cada actividad con GPS; junta las siguientes
// (con GPS) a ≤ 10 min y ≤ 25 m del ancla; si hay ≥ 4 clientes distintos, una
// señal y se salta lo ya incluido (las señales no se traslapan).
func senalesMismoPunto(acts []Actividad) []Senal {
	conGPS := make([]Actividad, 0, len(acts))
	for _, a := range acts {
		if a.ConGPS() {
			conGPS = append(conGPS, a)
		}
	}
	out := []Senal{}
	for i := 0; i < len(conGPS); {
		ancla := conGPS[i]
		grupo := []Actividad{ancla}
		ultimo := i
		for k := i + 1; k < len(conGPS) && conGPS[k].Hora.Sub(ancla.Hora) <= MismoPuntoVentana; k++ {
			if DistanciaMetros(*ancla.Lat, *ancla.Lng, *conGPS[k].Lat, *conGPS[k].Lng) <= MismoPuntoRadioMetros {
				grupo = append(grupo, conGPS[k])
				ultimo = k
			}
		}
		clientes := map[int]bool{}
		for _, a := range grupo {
			clientes[a.ClienteID] = true
		}
		if len(clientes) < MismoPuntoMinClientes {
			i++
			continue
		}
		ids := make([]string, 0, len(grupo))
		for _, a := range grupo {
			ids = append(ids, a.ID)
		}
		desde, hasta := grupo[0].Hora, grupo[len(grupo)-1].Hora
		out = append(out, Senal{
			Tipo: SenalMismoPunto, ActividadIDs: ids, Desde: ptr(desde), Hasta: ptr(hasta),
			Detalle: strconv.Itoa(len(clientes)) + " cuentas desde el mismo punto en " +
				FormatearDuracion(minutosEntre(desde, hasta)),
			Regla: reglaMismoPunto,
		})
		i = ultimo + 1
	}
	return out
}

func senalesCapturaTardia(acts []Actividad) []Senal {
	out := []Senal{}
	for _, a := range acts {
		if a.RecibidoAt == nil {
			continue
		}
		atraso := a.RecibidoAt.Sub(a.Hora)
		if atraso < CapturaTardiaUmbral {
			continue
		}
		out = append(out, Senal{
			Tipo: SenalCapturaTardia, ActividadIDs: []string{a.ID},
			Desde: ptr(a.Hora), Hasta: ptr(*a.RecibidoAt),
			Detalle: "Llegó al servidor " + FormatearDuracion(int(atraso/time.Minute)) + " después de capturarse",
			Regla:   reglaCapturaTardia,
		})
	}
	return out
}

func senalSinGPS(acts []Actividad) (Senal, bool) {
	var sin []Actividad
	for _, a := range acts {
		if !a.ConGPS() {
			sin = append(sin, a)
		}
	}
	if len(sin) == 0 {
		return Senal{}, false
	}
	ids := make([]string, 0, len(sin))
	for _, a := range sin {
		ids = append(ids, a.ID)
	}
	detalle := strconv.Itoa(len(sin)) + " actividades sin GPS"
	if len(sin) == 1 {
		detalle = "1 actividad sin GPS"
	}
	return Senal{
		Tipo: SenalSinGPS, ActividadIDs: ids, Desde: ptr(sin[0].Hora), Hasta: ptr(sin[len(sin)-1].Hora),
		Detalle: detalle, Regla: reglaSinGPS,
	}, true
}
```

`internal/desempeno/domain/registrador.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

// IndiceUsuarios resuelve quién registró cada actividad.
type IndiceUsuarios struct {
	porUID      map[string]UsuarioCobranza
	porCobrador map[int][]UsuarioCobranza
}

// NuevoIndiceUsuarios indexa por uid y por COBRADOR_ID.
func NuevoIndiceUsuarios(us []UsuarioCobranza) IndiceUsuarios {
	ix := IndiceUsuarios{porUID: map[string]UsuarioCobranza{}, porCobrador: map[int][]UsuarioCobranza{}}
	for _, u := range us {
		ix.porUID[u.UID] = u
		if u.CobradorID > 0 {
			ix.porCobrador[u.CobradorID] = append(ix.porCobrador[u.CobradorID], u)
		}
	}
	return ix
}

// Completar llena RegistradoPor:
//   - oficina: nombre "Oficina", sin uid;
//   - app con uid (MSP_PAGOS_RECIBIDOS.CREATED_BY → MSP_USUARIOS.FIREBASE_UID):
//     nombre de Firestore si el uid está en el índice;
//   - app sin uid: si UN SOLO usuario tiene ese COBRADOR_ID, ése; si varios lo
//     comparten, queda sin uid (no se adivina) y conserva el nombre crudo.
func (ix IndiceUsuarios) Completar(a Actividad) Actividad {
	rp := a.RegistradoPor
	if rp.Origen == OrigenOficina {
		rp.UsuarioUID = ""
		rp.Nombre = NombreOficina
		a.RegistradoPor = rp
		return a
	}
	if rp.UsuarioUID != "" {
		if u, ok := ix.porUID[rp.UsuarioUID]; ok {
			rp.Nombre = u.Nombre
			if rp.CobradorID == 0 {
				rp.CobradorID = u.CobradorID
			}
		}
		a.RegistradoPor = rp
		return a
	}
	if cands := ix.porCobrador[rp.CobradorID]; rp.CobradorID > 0 && len(cands) == 1 {
		rp.UsuarioUID = cands[0].UID
		rp.Nombre = cands[0].Nombre
	}
	a.RegistradoPor = rp
	return a
}

// RegistradaPor dice si la actividad (ya completada) la registró u: por uid
// cuando se resolvió; si no, por COBRADOR_ID (dos usuarios que comparten
// COBRADOR_ID ven ambos esas actividades — decisión D-API-4).
func RegistradaPor(a Actividad, u UsuarioCobranza) bool {
	rp := a.RegistradoPor
	if rp.Origen != OrigenApp {
		return false
	}
	if rp.UsuarioUID != "" {
		return rp.UsuarioUID == u.UID
	}
	return u.CobradorID > 0 && rp.CobradorID == u.CobradorID
}
```

`internal/desempeno/domain/cobro.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package domain

import "github.com/shopspring/decimal"

// Cobro es el `cobro {cobrado, esperado, pct}` del tablero. Decisión D-API-1:
// pct = % ponderado de rutas (el que oficina ya conoce); cobrado = Σ abono de
// la semana (concepto 87327) y esperado = Σ parcialidad de las ventas que
// aplican esta semana. pct NO es cobrado/esperado; el contrato §2 muestra un
// ejemplo que sugiere lo contrario y queda reportado.
type Cobro struct {
	Cobrado  decimal.Decimal
	Esperado decimal.Decimal
	// Falta = max(0, esperado − cobrado) (contrato v2 `cobro.falta`).
	Falta decimal.Decimal
	Pct   *decimal.Decimal
}

// CalcularCobro proyecta CobroSemana a Cobro.
func CalcularCobro(cs CobroSemana) Cobro {
	c := Cobro{Cobrado: decimal.Zero, Esperado: decimal.Zero, Pct: cs.PctPonderado}
	for _, v := range cs.Ventas {
		c.Cobrado = c.Cobrado.Add(v.AbonoSemana)
		if v.AplicaPonderado {
			c.Esperado = c.Esperado.Add(v.Parcialidad)
		}
	}
	c.Falta = decimal.Max(decimal.Zero, c.Esperado.Sub(c.Cobrado))
	return c
}
```


- [ ] **Step 4: Correr y ver que pasan (cobertura del dominio ≥ 99 %)**

Run: `go test -cover ./internal/desempeno/domain/`
Expected: `ok … coverage: 99.4% of statements` (lo que falta son las ramas imposibles tras un `switch` exhaustivo).

- [ ] **Step 5: Control positivo**

En `jornada.go`, cambiar `if next.Hora.Sub(prev.Hora) >= umbral {` por `> umbral` → debe fallar `TestCalcularJornada_UmbralExacto/45 min exactos sí es pausa`. En `senales.go`, cambiar `<= MismoPuntoVentana` por `< MismoPuntoVentana` → debe fallar `TestSenales_MismoPunto` ("el cuarto a 10 min exactos"). Restaurar ambos y verificar PASS y `git diff --stat` limpio.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/domain
git commit -m "feat(desempeno): jornada con pausas, señales, tramos y por hora; registrador y cobro"
```

### Tarea A3: servicio, semana del usuario, tablero y jornada

**Files:**
- Create: `internal/desempeno/app/service.go`, `semana.go`, `tablero.go`, `jornada.go`
- Test: `internal/desempeno/app/fakes_test.go`, `escenario_test.go`, `tablero_test.go`, `jornada_test.go`

**Interfaces:**
- Consumes: Tarea 0 (puertos), A1 (`Derivar`, `SegmentoDe`), A2 (`CalcularJornada`, `CalcularEstadoAhora`, `IndiceUsuarios`, `RegistradaPor`, `CalcularCobro`).
- Produces (los usa F): `app.NewService(usuarios outbound.UsuariosCobranza, actividades outbound.ActividadesRepo, cuentas outbound.CuentasRepo, clock outbound.Clock, loc *time.Location) *Service`; `(*Service).Tablero(ctx, TableroInput{Periodo, PausaMin, DesdeHora, HastaHora string}) (Tablero, error)` con `Tablero{GeneradoAt, Periodo, PausaMin, Filas []FilaTablero}` y `FilaTablero{Usuario, Configurado, MotivoNoConfigurado, VentasUniverso, EstadoAhora, EstadoDesde, Ultima *UltimaActividad, PrimeraActividad, UltimaActividadHora *time.Time, MinutosActivos, Pausas, Pagos, PagosPorForma, Condonaciones, NoAplicados, Visitas, CoberturaConAlgo, CoberturaTotal, PorSegmento CoberturaPorSegmento, Cobro domain.Cobro, Lineas}`; `(*Service).Jornada(ctx, uid, JornadaInput{Fecha, PausaMin, DesdeHora, HastaHora}) (VistaJornada{UID, PausaMin, GeneradoAt, Jornada}, error)`. Ayudantes de prueba de `app_test`: fakes, `local(t, dia, "HH:MM")`, `nuevoMundo(t)` (A4 los usa).

- [ ] **Step 1: Escribir las pruebas que fallan**

`internal/desempeno/app/fakes_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// ─── fakes de los puertos (sin librerías de mocks) ───────────────────────────

type fakeUsuarios struct {
	usuarios []domain.UsuarioCobranza
	cobro    map[string]domain.CobroSemana
	err      error
	errCobro error
}

func (f *fakeUsuarios) ListarUsuarios(context.Context) ([]domain.UsuarioCobranza, error) {
	return f.usuarios, f.err
}

func (f *fakeUsuarios) CobroSemana(_ context.Context, u domain.UsuarioCobranza) (domain.CobroSemana, error) {
	return f.cobro[u.UID], f.errCobro
}

type llamadaActividades struct {
	zona         int
	desde, hasta time.Time
}

type fakeActividades struct {
	porZona  map[int][]domain.Actividad
	err      error
	llamadas []llamadaActividades
}

// ActividadesZona respeta el contrato del puerto: [desde, hasta] inclusivo.
func (f *fakeActividades) ActividadesZona(_ context.Context, zona int, desde, hasta time.Time) ([]domain.Actividad, error) {
	f.llamadas = append(f.llamadas, llamadaActividades{zona, desde, hasta})
	if f.err != nil {
		return nil, f.err
	}
	var out []domain.Actividad
	for _, a := range f.porZona[zona] {
		if !a.Hora.Before(desde) && !a.Hora.After(hasta) {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeCuentas struct {
	porZona map[int][]domain.CuentaUniverso
	err     error
}

func (f *fakeCuentas) UniversoSemana(_ context.Context, zona int, _ time.Time) ([]domain.CuentaUniverso, error) {
	return f.porZona[zona], f.err
}

type relojFijo struct{ t time.Time }

func (r relojFijo) Now() time.Time { return r.t }

// ─── fixtures ────────────────────────────────────────────────────────────────

func cdmx(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	return loc
}

// local: instante a una hora local de CDMX de un día de octubre de 2026.
func local(t *testing.T, dia int, hhmm string) time.Time {
	t.Helper()
	tt, err := time.ParseInLocation("2006-01-02 15:04", "2026-10-"+dos(dia)+" "+hhmm, cdmx(t))
	require.NoError(t, err)
	return tt.UTC()
}

func dos(n int) string { return fmt.Sprintf("%02d", n) }

func dinero(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func f64(v float64) *float64 { return &v }

const (
	zonaRuta21 = 21563
	zonaRuta34 = 34001
)

// usuarios del escenario: Juan y Rosa comparten la zona 21 desde el jueves 9
// de oct 08:46 (CDMX); Lupe no tiene zona.
func usuariosEscenario(t *testing.T) []domain.UsuarioCobranza {
	t.Helper()
	inicio := local(t, 9, "08:46")
	return []domain.UsuarioCobranza{
		{
			UID: "uid-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
			CobradorID: 11502, ZonaID: zonaRuta21, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
		},
		{
			UID: "uid-rosa", Nombre: "Rosa Elena Pérez", Email: "rosa.perez@muebleriamsp.mx",
			CobradorID: 11600, ZonaID: zonaRuta21, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
		},
		{UID: "uid-lupe", Nombre: "Guadalupe Torres", CobradorID: 11700},
	}
}

func pagoDe(id string, u domain.UsuarioCobranza, h time.Time, ventaID, clienteID int, importe string, forma int) domain.Actividad {
	return domain.Actividad{
		ID: id, Tipo: domain.ClasificarTipoPago(forma, 0), Hora: h, ClienteID: clienteID,
		ClienteNombre: "CLIENTE " + dos(clienteID%100), VentaID: ventaID, Folio: "Z" + dos(ventaID%100),
		RegistradoPor: domain.RegistradoPor{UsuarioUID: u.UID, CobradorID: u.CobradorID, Origen: domain.OrigenApp},
		Lat:           f64(18.4617), Lng: f64(-97.3928),
		Pago: &domain.DatosPago{Importe: dinero(importe), FormaCobroID: forma, Aplicado: true, DoctoCCID: 1},
	}
}

func visitaDe(id string, u domain.UsuarioCobranza, h time.Time, ventaID, clienteID int, tipo string) domain.Actividad {
	return domain.Actividad{
		ID: id, Tipo: domain.TipoVisita, Hora: h, ClienteID: clienteID,
		ClienteNombre: "CLIENTE " + dos(clienteID%100), VentaID: ventaID,
		RegistradoPor: domain.RegistradoPor{CobradorID: u.CobradorID, Nombre: "CRUDO", Origen: domain.OrigenApp},
		Lat:           f64(18.4617), Lng: f64(-97.3928),
		Visita: &domain.DatosVisita{VisitaID: id, TipoVisita: tipo},
	}
}

func oficinaDe(id string, h time.Time, ventaID, clienteID int, importe string) domain.Actividad {
	a := pagoDe(id, domain.UsuarioCobranza{}, h, ventaID, clienteID, importe, domain.FormaCobroEfectivo)
	a.RegistradoPor = domain.RegistradoPor{Origen: domain.OrigenOficina}
	return a
}

func cuenta(ventaID, clienteID int, parcialidad, saldo string) domain.CuentaUniverso {
	return domain.CuentaUniverso{
		VentaID: ventaID, ClienteID: clienteID, ClienteNombre: "CLIENTE " + dos(clienteID%100),
		Folio: "Z" + dos(ventaID%100), Parcialidad: dinero(parcialidad), Saldo: dinero(saldo),
		FechaCargo: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC),
	}
}

func nuevoServicio(t *testing.T, u *fakeUsuarios, a *fakeActividades, c *fakeCuentas, ahora time.Time) *desempenoapp.Service {
	t.Helper()
	return desempenoapp.NewService(u, a, c, relojFijo{ahora}, cdmx(t))
}
```

`internal/desempeno/app/escenario_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// mundo es el escenario compartido por las pruebas del tablero, la jornada,
// la actividad y las cuentas. Zona 21, semana desde el jueves 9 de oct 08:46;
// hoy martes 14 de oct 16:40 (CDMX).
type mundo struct {
	juan, rosa, lupe domain.UsuarioCobranza
	ahora            time.Time
	usuarios         *fakeUsuarios
	actividades      *fakeActividades
	cuentas          *fakeCuentas
}

func nuevoMundo(t *testing.T) mundo {
	t.Helper()
	us := usuariosEscenario(t)
	juan, rosa, lupe := us[0], us[1], us[2]
	ahora := local(t, 14, "16:40") // hoy: martes 14 de oct
	noAplicado := pagoDe("pago:p-80", juan, local(t, 14, "16:20"), 6, 16, "80.00", domain.FormaCobroEfectivo)
	noAplicado.Pago.Aplicado = false
	noAplicado.Pago.DoctoCCID = 0
	acts := []domain.Actividad{
		pagoDe("pago:1001", juan, local(t, 14, "09:00"), 1, 10, "150.00", domain.FormaCobroEfectivo),
		pagoDe("pago:1002", juan, local(t, 14, "09:20"), 2, 11, "200.00", domain.FormaCobroTransferencia),
		visitaDe("visita:v-1", juan, local(t, 14, "10:30"), 0, 12, domain.TipoVisitaNoSeEncontraba),
		pagoDe("pago:1003", rosa, local(t, 14, "11:00"), 3, 13, "150.00", domain.FormaCobroEfectivo),
		oficinaDe("pago:1004", local(t, 14, "12:00"), 4, 14, "100.00"),
		pagoDe("pago:1005", juan, local(t, 14, "15:50"), 5, 15, "50.00", domain.FormaCobroCondonacion),
		noAplicado,
		pagoDe("pago:1007", juan, local(t, 10, "10:00"), 7, 17, "300.00", domain.FormaCobroCheque),
		pagoDe("pago:1000", juan, local(t, 8, "10:00"), 1, 10, "999.00", domain.FormaCobroEfectivo), // antes de la semana
	}
	universo := []domain.CuentaUniverso{
		cuenta(1, 10, "150.00", "1350.00"), cuenta(2, 11, "150.00", "800.00"),
		cuenta(3, 13, "150.00", "600.00"), cuenta(4, 14, "150.00", "900.00"),
		cuenta(5, 15, "150.00", "450.00"), cuenta(6, 16, "150.00", "300.00"),
		cuenta(7, 17, "150.00", "0.00"), cuenta(8, 18, "150.00", "1500.00"),
	}
	pct := decimal.RequireFromString("65.90")
	return mundo{
		juan: juan, rosa: rosa, lupe: lupe, ahora: ahora,
		usuarios: &fakeUsuarios{
			usuarios: []domain.UsuarioCobranza{rosa, lupe, juan},
			cobro: map[string]domain.CobroSemana{
				juan.UID: {PctPonderado: &pct, Ventas: []domain.CobroVenta{
					{VentaID: 1, Parcialidad: dinero("150"), AbonoSemana: dinero("150"), AplicaPonderado: true},
					{VentaID: 8, Parcialidad: dinero("150"), AbonoSemana: dinero("0"), Vencidas: dinero("2"), AplicaPonderado: true},
				}},
			},
		},
		actividades: &fakeActividades{porZona: map[int][]domain.Actividad{zonaRuta21: acts}},
		cuentas:     &fakeCuentas{porZona: map[int][]domain.CuentaUniverso{zonaRuta21: universo}},
	}
}
```

`internal/desempeno/app/tablero_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func filaDe(t *testing.T, tb desempenoapp.Tablero, uid string) desempenoapp.FilaTablero {
	t.Helper()
	for _, f := range tb.Filas {
		if f.Usuario.UID == uid {
			return f
		}
	}
	t.Fatalf("no hay fila para %s", uid)
	return desempenoapp.FilaTablero{}
}

func TestTablero_Hoy_CifrasDeJuan(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	assert.Equal(t, desempenoapp.PeriodoHoy, tb.Periodo)
	assert.Equal(t, 45, tb.PausaMin)
	assert.Equal(t, m.ahora, tb.GeneradoAt)
	require.Len(t, tb.Filas, 3)
	assert.Equal(t, []string{"uid-lupe", "uid-juan", "uid-rosa"},
		[]string{tb.Filas[0].Usuario.UID, tb.Filas[1].Usuario.UID, tb.Filas[2].Usuario.UID},
		"orden: zona y luego nombre")

	f := filaDe(t, tb, "uid-juan")
	assert.True(t, f.Configurado)
	assert.Equal(t, domain.EstadoAhoraActivo, f.EstadoAhora)
	assert.Equal(t, local(t, 14, "15:50"), *f.EstadoDesde, "inicio del tramo actual")
	assert.Equal(t, local(t, 14, "09:00"), *f.PrimeraActividad)
	assert.Equal(t, local(t, 14, "16:20"), *f.UltimaActividadHora)
	require.NotNil(t, f.Ultima)
	assert.Equal(t, "pago:p-80", f.Ultima.ID)
	assert.Equal(t, domain.ResumenPausas{N: 2, Minutos: 70 + 320}, f.Pausas)
	assert.Equal(t, 440-390, f.MinutosActivos)
	assert.Equal(t, 2, f.Pagos.N, "la condonación y el no aplicado no son pagos")
	assert.Equal(t, "350.00", f.Pagos.Importe.StringFixed(2))
	assert.Equal(t, "150.00", f.PagosPorForma.Efectivo.Importe.StringFixed(2))
	assert.Equal(t, "200.00", f.PagosPorForma.Transferencia.Importe.StringFixed(2))
	assert.Equal(t, 0, f.PagosPorForma.Cheque.N, "el cheque fue el día 10")
	assert.Equal(t, 1, f.Condonaciones.N)
	assert.Equal(t, "80.00", f.NoAplicados.Importe.StringFixed(2))
	assert.Equal(t, 1, f.Visitas, "la visita sin uid se le atribuye por COBRADOR_ID único")
	require.Len(t, f.Lineas, 1)
	assert.Equal(t, "2026-10-14", f.Lineas[0].Dia.String())

	// Cobertura de la semana sobre TODA la zona (D-API-5).
	assert.Equal(t, 8, f.VentasUniverso)
	assert.Equal(t, 7, f.CoberturaConAlgo)
	assert.Equal(t, 8, f.CoberturaTotal)
	assert.Equal(t, desempenoapp.CoberturaPorSegmento{SinVisitar: 2, Volver: 2, Despues: 0, Pagados: 4, SinNada: 1}, f.PorSegmento)

	// Cobro: lo que calcula rutas.
	assert.Equal(t, "150.00", f.Cobro.Cobrado.StringFixed(2))
	assert.Equal(t, "300.00", f.Cobro.Esperado.StringFixed(2))
	assert.Equal(t, "150.00", f.Cobro.Falta.StringFixed(2))
	assert.Equal(t, "65.90", f.Cobro.Pct.StringFixed(2))
}

func TestTablero_Semana_SumaLosDiasDesdeElInicio(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "semana"})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-juan")
	require.Len(t, f.Lineas, 6, "del jueves 9 al martes 14")
	assert.Equal(t, "2026-10-09", f.Lineas[0].Dia.String())
	assert.Equal(t, 3, f.Pagos.N, "suma el cheque del día 10; el pago del 8 queda fuera")
	assert.Equal(t, "650.00", f.Pagos.Importe.StringFixed(2))
	assert.Equal(t, 1, f.PagosPorForma.Cheque.N)
	assert.Equal(t, local(t, 10, "10:00"), *f.PrimeraActividad)
	assert.Equal(t, domain.EstadoAhoraActivo, f.EstadoAhora, "el estado ahora siempre es de hoy")
}

func TestTablero_UsuarioSinConfigurar(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-lupe")
	assert.False(t, f.Configurado)
	assert.Equal(t, domain.MotivoSinZona, f.MotivoNoConfigurado)
	assert.Equal(t, domain.EstadoAhoraNoHaEmpezado, f.EstadoAhora)
	assert.Empty(t, f.Lineas)
	assert.NotNil(t, f.Lineas, "lineas = [] y no null")
	assert.Equal(t, 0, f.Pagos.N)
	assert.Nil(t, f.Cobro.Pct)
	for _, ll := range m.actividades.llamadas {
		assert.NotEqual(t, 0, ll.zona, "nunca se consulta la zona 0")
	}
}

func TestTablero_Errores(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)

	_, err := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{Periodo: "mes"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	_, err = nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{PausaMin: "3"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	caido := &fakeUsuarios{err: domain.ErrFirestoreNoDisponible}
	_, err = nuevoServicio(t, caido, m.actividades, m.cuentas, m.ahora).Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, domain.ErrFirestoreNoDisponible)

	boom := errors.New("firebird caído")
	_, err = nuevoServicio(t, m.usuarios, &fakeActividades{err: boom}, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)

	_, err = nuevoServicio(t, m.usuarios, m.actividades, &fakeCuentas{err: boom}, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)

	m.usuarios.errCobro = boom
	_, err = nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora).
		Tablero(context.Background(), desempenoapp.TableroInput{})
	require.ErrorIs(t, err, boom)
}

func TestTablero_SemanaQueEmpiezaHoyConsultaDesdeMedianoche(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	inicioHoy := local(t, 14, "10:00")
	m.juan.InicioSemana = &inicioHoy
	m.usuarios.usuarios = []domain.UsuarioCobranza{m.juan}
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	tb, err := svc.Tablero(context.Background(), desempenoapp.TableroInput{})
	require.NoError(t, err)
	f := filaDe(t, tb, "uid-juan")
	assert.Equal(t, local(t, 14, "09:00"), *f.PrimeraActividad, "las de la mañana cuentan para hoy")
	assert.Equal(t, 2, f.Pagos.N)
}
```

`internal/desempeno/app/jornada_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func TestJornada_SoloLoQueRegistroEsteUsuario(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	v, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{})
	require.NoError(t, err)
	assert.Equal(t, "uid-juan", v.UID)
	assert.Equal(t, 45, v.PausaMin)
	assert.Equal(t, m.ahora, v.GeneradoAt)
	assert.Equal(t, "2026-10-14", v.Jornada.Dia.String(), "sin fecha es hoy en CDMX")
	ids := make([]string, 0, len(v.Jornada.Actividades))
	for _, a := range v.Jornada.Actividades {
		ids = append(ids, a.ID)
	}
	assert.Equal(t, []string{"pago:1001", "pago:1002", "visita:v-1", "pago:1005", "pago:p-80"}, ids,
		"sin lo de Rosa ni lo de oficina")
	assert.Equal(t, "Juan Carlos Méndez", v.Jornada.Actividades[2].RegistradoPor.Nombre, "nombre resuelto")

	ll := m.actividades.llamadas[len(m.actividades.llamadas)-1]
	assert.Equal(t, local(t, 14, "00:00"), ll.desde)
	assert.Equal(t, local(t, 15, "00:00"), ll.hasta)
}

func TestJornada_FechaPasadaYParametros(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	v, err := svc.Jornada(context.Background(), "uid-juan", desempenoapp.JornadaInput{Fecha: "2026-10-10", PausaMin: "30"})
	require.NoError(t, err)
	require.Len(t, v.Jornada.Actividades, 1)
	assert.Equal(t, "pago:1007", v.Jornada.Actividades[0].ID)
	assert.Equal(t, 30, v.PausaMin)
	assert.Empty(t, v.Jornada.Linea.Pausas, "un día pasado no tiene pausa en curso")
}

func TestJornada_Errores(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()

	_, err := svc.Jornada(ctx, "uid-nadie", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioNoEncontrado)

	_, err = svc.Jornada(ctx, "uid-lupe", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinZona)

	_, err = svc.Jornada(ctx, "uid-juan", desempenoapp.JornadaInput{Fecha: "14/10/2026"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	_, err = svc.Jornada(ctx, "uid-juan", desempenoapp.JornadaInput{DesdeHora: "08:00"})
	require.ErrorIs(t, err, domain.ErrParametroInvalido)

	_, err = nuevoServicio(t, &fakeUsuarios{err: domain.ErrFirestoreNoDisponible}, m.actividades, m.cuentas, m.ahora).
		Jornada(ctx, "uid-juan", desempenoapp.JornadaInput{})
	require.ErrorIs(t, err, domain.ErrFirestoreNoDisponible)
}
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test ./internal/desempeno/app/`
Expected: FAIL de compilación (`undefined: desempenoapp.NewService`).

- [ ] **Step 3: Implementación**

`internal/desempeno/app/service.go` (archivo nuevo, completo):

```go
// Package app es la capa de casos de uso del módulo desempeno (solo lectura).
// Un archivo por consulta: tablero.go, jornada.go, actividad.go, cuentas.go,
// exportar.go (CQRS_PATTERN.md).
//
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
)

// Service reúne las dependencias del módulo.
type Service struct {
	usuarios    outbound.UsuariosCobranza
	actividades outbound.ActividadesRepo
	cuentas     outbound.CuentasRepo
	clock       outbound.Clock
	loc         *time.Location
}

// NewService arma el servicio. loc es la zona de negocio
// (firebird.BusinessTZ() en producción, America/Mexico_City).
func NewService(
	usuarios outbound.UsuariosCobranza,
	actividades outbound.ActividadesRepo,
	cuentas outbound.CuentasRepo,
	clock outbound.Clock,
	loc *time.Location,
) *Service {
	return &Service{usuarios: usuarios, actividades: actividades, cuentas: cuentas, clock: clock, loc: loc}
}

// contexto es lo que toda consulta por usuario necesita: el usuario, el índice
// para resolver registradores y "ahora".
type contexto struct {
	usuario domain.UsuarioCobranza
	indice  domain.IndiceUsuarios
	ahora   time.Time
}

// resolverUsuario lista los usuarios (una sola lectura de Firestore), busca uid
// y devuelve el índice para completar registradores.
func (s *Service) resolverUsuario(ctx context.Context, uid string) (contexto, error) {
	us, err := s.usuarios.ListarUsuarios(ctx)
	if err != nil {
		return contexto{}, err
	}
	for _, u := range us {
		if u.UID == uid {
			return contexto{usuario: u, indice: domain.NuevoIndiceUsuarios(us), ahora: s.clock.Now()}, nil
		}
	}
	return contexto{}, domain.ErrUsuarioNoEncontrado
}

// actividadesCompletas trae las actividades de la zona y completa quién las
// registró.
func (s *Service) actividadesCompletas(
	ctx context.Context, ix domain.IndiceUsuarios, zonaID int, desde, hasta time.Time,
) ([]domain.Actividad, error) {
	acts, err := s.actividades.ActividadesZona(ctx, zonaID, desde, hasta)
	if err != nil {
		return nil, err
	}
	for i := range acts {
		acts[i] = ix.Completar(acts[i])
	}
	return acts, nil
}

// registradasPor filtra las actividades que registró u.
func registradasPor(acts []domain.Actividad, u domain.UsuarioCobranza) []domain.Actividad {
	out := make([]domain.Actividad, 0, len(acts))
	for _, a := range acts {
		if domain.RegistradaPor(a, u) {
			out = append(out, a)
		}
	}
	return out
}
```

`internal/desempeno/app/semana.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// semana es el cálculo de la semana del usuario que comparten el tablero
// (cobertura) y Cuentas: universo, actividades de TODA la zona en la ventana y
// la derivación del estado por venta.
type semana struct {
	ventana     domain.Ventana
	universo    []domain.CuentaUniverso
	actividades []domain.Actividad
	derivacion  domain.Derivacion
}

// calcularSemana exige usuario configurado. La ventana es
// [FECHA_CARGA_INICIAL, ahora], inclusiva (VentanaCobro.kt).
func (s *Service) calcularSemana(ctx context.Context, c contexto) (semana, error) {
	if err := c.usuario.ExigirConfigurado(); err != nil {
		return semana{}, err
	}
	v, err := domain.NuevaVentana(*c.usuario.InicioSemana, c.ahora)
	if err != nil {
		return semana{}, err
	}
	universo, err := s.cuentas.UniversoSemana(ctx, c.usuario.ZonaID, v.Inicio())
	if err != nil {
		return semana{}, err
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, v.Inicio(), v.Fin())
	if err != nil {
		return semana{}, err
	}
	pagos, visitas := aPeriodo(acts)
	d := domain.Derivar(cuentasPeriodo(universo), pagos, visitas, v)
	return semana{ventana: v, universo: universo, actividades: acts, derivacion: d}, nil
}

func cuentasPeriodo(universo []domain.CuentaUniverso) []domain.CuentaPeriodo {
	out := make([]domain.CuentaPeriodo, 0, len(universo))
	for _, c := range universo {
		out = append(out, domain.CuentaPeriodo{VentaID: c.VentaID, ClienteID: c.ClienteID, Parcialidad: c.Parcialidad})
	}
	return out
}

// aPeriodo traduce actividades a las entradas del deriver. Una condonación
// siempre entra con la forma 137026, aunque Microsip le haya puesto otra forma:
// así nunca cuenta como dinero (spec §2).
func aPeriodo(acts []domain.Actividad) ([]domain.PagoPeriodo, []domain.VisitaPeriodo) {
	pagos := make([]domain.PagoPeriodo, 0, len(acts))
	visitas := make([]domain.VisitaPeriodo, 0, len(acts))
	for _, a := range acts {
		switch {
		case a.Pago != nil && a.VentaID > 0:
			forma := a.Pago.FormaCobroID
			if a.Tipo == domain.TipoCondonacion {
				forma = domain.FormaCobroCondonacion
			}
			pagos = append(pagos, domain.PagoPeriodo{
				VentaID: a.VentaID, Importe: a.Pago.Importe, FormaCobroID: forma,
				FechaHora: a.Hora, Rechazado: a.Pago.Rechazado, ActividadID: a.ID,
			})
		case a.Visita != nil:
			vp := domain.VisitaPeriodo{
				ClienteID: a.ClienteID, TipoVisita: a.Visita.TipoVisita, FechaHora: a.Hora, ActividadID: a.ID,
			}
			if a.VentaID > 0 {
				id := a.VentaID
				vp.VentaID = &id
			}
			visitas = append(visitas, vp)
		}
	}
	return pagos, visitas
}
```

`internal/desempeno/app/tablero.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Periodo del tablero (contrato §2).
type Periodo string

// Periodos.
const (
	PeriodoHoy    Periodo = "hoy"
	PeriodoSemana Periodo = "semana"
)

// TableroInput lleva los parámetros crudos de GET /desempeno/usuarios.
type TableroInput struct {
	Periodo   string
	PausaMin  string
	DesdeHora string
	HastaHora string
}

// Tablero tiene una fila por usuario, sin totales del equipo (D14).
type Tablero struct {
	GeneradoAt time.Time
	Periodo    Periodo
	PausaMin   int
	Filas      []FilaTablero
}

// PagosPorForma cuenta solo efectivo, transferencia y cheque (no existe "depósito").
type PagosPorForma struct {
	Efectivo      domain.ConteoImporte
	Transferencia domain.ConteoImporte
	Cheque        domain.ConteoImporte
}

// CoberturaPorSegmento cuenta de la semana por segmento + sin nada.
type CoberturaPorSegmento struct {
	SinVisitar int
	Volver     int
	Despues    int
	Pagados    int
	SinNada    int
}

// UltimaActividad es `ultima {id, tipo, hora, cliente_nombre}`.
type UltimaActividad struct {
	ID            string
	Tipo          domain.TipoActividad
	Hora          time.Time
	ClienteNombre string
}

// FilaTablero lleva las cifras de UN usuario. Pagos, visitas y pausas son del
// periodo y solo de lo que ESTE usuario registró; cobertura y cobro son de la
// semana del usuario (contrato §2) sobre toda la actividad de su zona
// (decisión D-API-5).
type FilaTablero struct {
	Usuario             domain.UsuarioCobranza
	Configurado         bool
	MotivoNoConfigurado string
	VentasUniverso      int
	EstadoAhora         domain.EstadoAhora
	EstadoDesde         *time.Time
	Ultima              *UltimaActividad
	PrimeraActividad    *time.Time
	UltimaActividadHora *time.Time
	MinutosActivos      int
	Pausas              domain.ResumenPausas
	Pagos               domain.ConteoImporte
	PagosPorForma       PagosPorForma
	Condonaciones       domain.ConteoImporte
	NoAplicados         domain.ConteoImporte
	Visitas             int
	CoberturaConAlgo    int
	CoberturaTotal      int
	PorSegmento         CoberturaPorSegmento
	Cobro               domain.Cobro
	Lineas              []domain.LineaDelDia
}

func parsePeriodo(s string) (Periodo, error) {
	switch Periodo(s) {
	case "", PeriodoHoy:
		return PeriodoHoy, nil
	case PeriodoSemana:
		return PeriodoSemana, nil
	}
	return "", domain.ErrParametroInvalido.WithField("parametro", "periodo")
}

// Tablero arma una fila por usuario. Un error de Firebird en cualquier fila
// falla la consulta completa: el tablero nunca muestra cifras a medias.
func (s *Service) Tablero(ctx context.Context, in TableroInput) (Tablero, error) {
	periodo, err := parsePeriodo(in.Periodo)
	if err != nil {
		return Tablero{}, err
	}
	p, err := domain.NuevosParametrosJornada(in.PausaMin, in.DesdeHora, in.HastaHora)
	if err != nil {
		return Tablero{}, err
	}
	us, err := s.usuarios.ListarUsuarios(ctx)
	if err != nil {
		return Tablero{}, err
	}
	ix := domain.NuevoIndiceUsuarios(us)
	ahora := s.clock.Now()
	t := Tablero{GeneradoAt: ahora, Periodo: periodo, PausaMin: p.PausaMin, Filas: make([]FilaTablero, 0, len(us))}
	for _, u := range us {
		fila, err := s.filaTablero(ctx, contexto{usuario: u, indice: ix, ahora: ahora}, periodo, p)
		if err != nil {
			return Tablero{}, err
		}
		t.Filas = append(t.Filas, fila)
	}
	sort.SliceStable(t.Filas, func(i, j int) bool {
		a, b := t.Filas[i].Usuario, t.Filas[j].Usuario
		if a.ZonaNombre != b.ZonaNombre {
			return a.ZonaNombre < b.ZonaNombre
		}
		return a.Nombre < b.Nombre
	})
	return t, nil
}

func filaVacia(u domain.UsuarioCobranza, motivo string) FilaTablero {
	cero := domain.ConteoImporte{Importe: decimal.Zero}
	return FilaTablero{
		Usuario: u, MotivoNoConfigurado: motivo, EstadoAhora: domain.EstadoAhoraNoHaEmpezado,
		Pagos: cero, Condonaciones: cero, NoAplicados: cero,
		PagosPorForma: PagosPorForma{Efectivo: cero, Transferencia: cero, Cheque: cero},
		Cobro:         domain.Cobro{Cobrado: decimal.Zero, Esperado: decimal.Zero},
		Lineas:        []domain.LineaDelDia{},
	}
}

func (s *Service) filaTablero(ctx context.Context, c contexto, periodo Periodo, p domain.ParametrosJornada) (FilaTablero, error) {
	u := c.usuario
	if ok, motivo := u.Configuracion(); !ok {
		return filaVacia(u, motivo), nil
	}
	fila := filaVacia(u, "")
	fila.Configurado = true

	sem, err := s.calcularSemana(ctx, c)
	if err != nil {
		return FilaTablero{}, err
	}
	cs, err := s.usuarios.CobroSemana(ctx, u)
	if err != nil {
		return FilaTablero{}, err
	}
	fila.Cobro = domain.CalcularCobro(cs)
	s.llenarCobertura(&fila, sem, c.ahora)

	hoy := domain.DiaDe(c.ahora, s.loc)
	propiasSemana := registradasPor(sem.actividades, u)
	propiasHoy, err := s.actividadesDeHoy(ctx, c, sem, hoy)
	if err != nil {
		return FilaTablero{}, err
	}
	jHoy := domain.CalcularJornada(propiasHoy, p, hoy, s.loc, c.ahora)
	fila.EstadoAhora, fila.EstadoDesde = domain.CalcularEstadoAhora(jHoy, p, c.ahora)

	if periodo == PeriodoHoy {
		fila.Lineas = []domain.LineaDelDia{jHoy.Linea}
		acumularResumen(&fila, jHoy.Resumen)
		fila.Pausas = jHoy.Resumen.Pausas
		llenarPeriodo(&fila, jHoy.Actividades)
		return fila, nil
	}
	for d := domain.DiaDe(sem.ventana.Inicio(), s.loc); !d.Despues(hoy); d = d.Siguiente() {
		j := domain.CalcularJornada(propiasSemana, p, d, s.loc, c.ahora)
		fila.Lineas = append(fila.Lineas, j.Linea)
		acumularResumen(&fila, j.Resumen)
		fila.Pausas.N += j.Resumen.Pausas.N
		fila.Pausas.Minutos += j.Resumen.Pausas.Minutos
		fila.Pausas.EnCurso = fila.Pausas.EnCurso || j.Resumen.Pausas.EnCurso
	}
	domain.OrdenarActividades(propiasSemana)
	llenarPeriodo(&fila, propiasSemana)
	return fila, nil
}

// actividadesDeHoy resuelve las de hoy: si la semana empezó antes de hoy, las de hoy ya están en
// la semana; si empezó hoy a media jornada, se consultan desde las 00:00.
func (s *Service) actividadesDeHoy(ctx context.Context, c contexto, sem semana, hoy domain.Fecha) ([]domain.Actividad, error) {
	inicioHoy := hoy.InicioDelDia(s.loc)
	if !sem.ventana.Inicio().After(inicioHoy) {
		return registradasPor(sem.actividades, c.usuario), nil
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, inicioHoy, c.ahora)
	if err != nil {
		return nil, err
	}
	return registradasPor(acts, c.usuario), nil
}

func (s *Service) llenarCobertura(fila *FilaTablero, sem semana, ahora time.Time) {
	hoy := domain.DiaDe(ahora, s.loc)
	fila.VentasUniverso = len(sem.universo)
	fila.CoberturaTotal = len(sem.universo)
	for _, r := range sem.derivacion.PorVenta {
		if r.ConAlgo {
			fila.CoberturaConAlgo++
		} else {
			fila.PorSegmento.SinNada++
		}
		seg, ok := domain.SegmentoDe(r, hoy)
		if !ok {
			continue
		}
		switch seg {
		case domain.SegmentoSinVisitar:
			fila.PorSegmento.SinVisitar++
		case domain.SegmentoVolver:
			fila.PorSegmento.Volver++
		case domain.SegmentoDespues:
			fila.PorSegmento.Despues++
		case domain.SegmentoPagados:
			fila.PorSegmento.Pagados++
		}
	}
}

func acumularResumen(fila *FilaTablero, r domain.ResumenJornada) {
	fila.MinutosActivos += r.MinutosActivos
	fila.Pagos.N += r.Pagos.N
	fila.Pagos.Importe = fila.Pagos.Importe.Add(r.Pagos.Importe)
	fila.Condonaciones.N += r.Condonaciones.N
	fila.Condonaciones.Importe = fila.Condonaciones.Importe.Add(r.Condonaciones.Importe)
	fila.NoAplicados.N += r.NoAplicados.N
	fila.NoAplicados.Importe = fila.NoAplicados.Importe.Add(r.NoAplicados.Importe)
	fila.Visitas += r.Visitas
}

// llenarPeriodo llena primera/última actividad, `ultima` y pagos por forma sobre las
// actividades propias del periodo (ya ordenadas).
func llenarPeriodo(fila *FilaTablero, acts []domain.Actividad) {
	if len(acts) == 0 {
		return
	}
	primera, ultima := acts[0], acts[len(acts)-1]
	fila.PrimeraActividad = &primera.Hora
	fila.UltimaActividadHora = &ultima.Hora
	fila.Ultima = &UltimaActividad{ID: ultima.ID, Tipo: ultima.Tipo, Hora: ultima.Hora, ClienteNombre: ultima.ClienteNombre}
	for _, a := range acts {
		if !a.CuentaComoDinero() {
			continue
		}
		var dst *domain.ConteoImporte
		switch a.Pago.FormaCobroID {
		case domain.FormaCobroEfectivo:
			dst = &fila.PagosPorForma.Efectivo
		case domain.FormaCobroTransferencia:
			dst = &fila.PagosPorForma.Transferencia
		case domain.FormaCobroCheque:
			dst = &fila.PagosPorForma.Cheque
		default:
			continue
		}
		dst.N++
		dst.Importe = dst.Importe.Add(a.Importe())
	}
}
```

`internal/desempeno/app/jornada.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// JornadaInput lleva los parámetros crudos de GET /desempeno/usuarios/{uid}/jornada.
// Fecha vacía = hoy (America/Mexico_City).
type JornadaInput struct {
	Fecha     string
	PausaMin  string
	DesdeHora string
	HastaHora string
}

// VistaJornada es lo que el handler serializa (contrato §3).
type VistaJornada struct {
	UID        string
	PausaMin   int
	GeneradoAt time.Time
	Jornada    domain.Jornada
}

// Jornada devuelve las actividades que registró ESTE usuario en el día, con pausas,
// línea y señales. Sirve para cualquier fecha (spec §1): solo exige zona.
func (s *Service) Jornada(ctx context.Context, uid string, in JornadaInput) (VistaJornada, error) {
	p, err := domain.NuevosParametrosJornada(in.PausaMin, in.DesdeHora, in.HastaHora)
	if err != nil {
		return VistaJornada{}, err
	}
	c, err := s.resolverUsuario(ctx, uid)
	if err != nil {
		return VistaJornada{}, err
	}
	if c.usuario.ZonaID <= 0 {
		return VistaJornada{}, domain.ErrUsuarioSinZona
	}
	dia := domain.DiaDe(c.ahora, s.loc)
	if in.Fecha != "" {
		if dia, err = domain.ParseFecha(in.Fecha); err != nil {
			return VistaJornada{}, domain.ErrParametroInvalido.WithField("parametro", "fecha")
		}
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID,
		dia.InicioDelDia(s.loc), dia.Siguiente().InicioDelDia(s.loc))
	if err != nil {
		return VistaJornada{}, err
	}
	j := domain.CalcularJornada(registradasPor(acts, c.usuario), p, dia, s.loc, c.ahora)
	return VistaJornada{UID: uid, PausaMin: p.PausaMin, GeneradoAt: c.ahora, Jornada: j}, nil
}
```


- [ ] **Step 4: Correr y ver que pasan**

Run: `go test -cover ./internal/desempeno/app/`
Expected: PASS.

- [ ] **Step 5: Control positivo**

En `tablero.go` (`actividadesDeHoy`), reemplazar el cuerpo por `return registradasPor(sem.actividades, c.usuario), nil` → debe fallar `TestTablero_SemanaQueEmpiezaHoyConsultaDesdeMedianoche`. Restaurar; PASS; `git diff --stat` limpio.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/app
git commit -m "feat(desempeno): tablero por usuario y jornada del día"
```

### Tarea A4: actividad de la zona (filtros, orden, cursor, totales) y cuentas de la semana

**Files:**
- Create: `internal/desempeno/app/texto.go`, `actividad.go`, `cuentas.go`
- Test: `internal/desempeno/app/texto_internal_test.go`, `actividad_test.go`, `cuentas_test.go`

**Interfaces:**
- Consumes: A3 (`Service`, `resolverUsuario`, `actividadesCompletas`, `calcularSemana`, fakes y `nuevoMundo`).
- Produces (los usa F): `(*Service).Actividad(ctx, uid, FiltroActividadInput{Desde, Hasta string; Tipos, FormasCobro, TiposVisita, RegistradoPor []string; MontoMin, MontoMax, ConFotos, ConGPS, Q, Orden, Dir, Cursor, Limit string}) (PaginaActividad{Items, SiguienteCursor string, Totales{N, Importe, PorTipo map[TipoActividad]int}}, error)`; `(*Service).Cuentas(ctx, uid, CuentasInput{Segmento, Q, Orden, Dir}) (VistaCuentas{InicioSemana, Conteos ConteosCuentas, Cuentas []CuentaVista}, error)` con `CuentaVista{Cuenta, Resultado, Trato, Etiqueta, Detalle, Segmento *domain.Segmento, UltimaActividad *ActividadDeCuenta, NumAtrasos, Nueva}`.

- [ ] **Step 1: Escribir las pruebas que fallan**

`internal/desempeno/app/texto_internal_test.go` (archivo nuevo, completo):

```go
package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizar(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "juana andrade lechuga", normalizar("  JUANA ANDRADE LECHUGA "))
	assert.Equal(t, "mendez nino", normalizar("MÉNDEZ NIÑO"))
	assert.Equal(t, normalizar("Pérez"), normalizar("perez"))
	assert.Empty(t, normalizar(""))
}
```

`internal/desempeno/app/actividad_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func idsDe(acts []domain.Actividad) []string {
	out := make([]string, 0, len(acts))
	for _, a := range acts {
		out = append(out, a.ID)
	}
	return out
}

func TestActividad_TodaLaZonaPorOmision(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	pag, err := svc.Actividad(context.Background(), "uid-juan", desempenoapp.FiltroActividadInput{})
	require.NoError(t, err)
	// semana del 9 al 14, cualquier registrador, hora desc.
	assert.Equal(t, []string{
		"pago:p-80", "pago:1005", "pago:1004", "pago:1003", "visita:v-1", "pago:1002", "pago:1001", "pago:1007",
	}, idsDe(pag.Items))
	assert.Empty(t, pag.SiguienteCursor)
	assert.Equal(t, 8, pag.Totales.N)
	assert.Equal(t, "900.00", pag.Totales.Importe.StringFixed(2),
		"solo dinero (contrato §4): 150+200+150+100+300; ni la condonación (50) ni el no aplicado (80)")
	assert.Equal(t, map[domain.TipoActividad]int{domain.TipoPago: 6, domain.TipoVisita: 1, domain.TipoCondonacion: 1}, pag.Totales.PorTipo)

	ll := m.actividades.llamadas[0]
	assert.Equal(t, *m.juan.InicioSemana, ll.desde)
	assert.Equal(t, m.ahora, ll.hasta)
}

func TestActividad_Filtros(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		in     desempenoapp.FiltroActividadInput
		ids    []string
	}{
		{"tipo visita", desempenoapp.FiltroActividadInput{Tipos: []string{"visita"}}, []string{"visita:v-1"}},
		{"tipo pago y condonación", desempenoapp.FiltroActividadInput{Tipos: []string{"condonacion"}}, []string{"pago:1005"}},
		{"forma transferencia", desempenoapp.FiltroActividadInput{FormasCobro: []string{"transferencia"}}, []string{"pago:1002"}},
		{"forma cheque o condonación", desempenoapp.FiltroActividadInput{FormasCobro: []string{"cheque", "condonacion"}}, []string{"pago:1005", "pago:1007"}},
		{"tipo de visita literal", desempenoapp.FiltroActividadInput{TiposVisita: []string{"No se encontraba"}}, []string{"visita:v-1"}},
		{"registrado por oficina", desempenoapp.FiltroActividadInput{RegistradoPor: []string{"oficina"}}, []string{"pago:1004"}},
		{"registrado por Rosa", desempenoapp.FiltroActividadInput{RegistradoPor: []string{"uid-rosa"}}, []string{"pago:1003"}},
		{"monto entre 150 y 200", desempenoapp.FiltroActividadInput{MontoMin: "150", MontoMax: "200"}, []string{"pago:1003", "pago:1002", "pago:1001"}},
		{"con fotos", desempenoapp.FiltroActividadInput{ConFotos: "true"}, []string{}},
		{"sin GPS", desempenoapp.FiltroActividadInput{ConGPS: "false"}, []string{}},
		{"q sin acentos ni mayúsculas", desempenoapp.FiltroActividadInput{Q: "cliente 17"}, []string{"pago:1007"}},
		{"q por folio", desempenoapp.FiltroActividadInput{Q: "z03"}, []string{"pago:1003"}},
		{"rango explícito", desempenoapp.FiltroActividadInput{Desde: "2026-10-10T00:00:00Z", Hasta: "2026-10-11T00:00:00Z"}, []string{"pago:1007"}},
	}
	for _, c := range casos {
		m := nuevoMundo(t)
		svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
		pag, err := svc.Actividad(context.Background(), "uid-juan", c.in)
		require.NoError(t, err, c.nombre)
		assert.Equal(t, c.ids, idsDe(pag.Items), c.nombre)
		assert.Equal(t, len(c.ids), pag.Totales.N, c.nombre)
	}
}

func TestActividad_OrdenYCursorEstable(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()

	for _, orden := range []string{"hora", "cliente", "monto", "tipo", "registrado_por"} {
		for _, dir := range []string{"asc", "desc"} {
			todo, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: orden, Dir: dir, Limit: "500"})
			require.NoError(t, err)
			var paginado []domain.Actividad
			cursor := ""
			for range 10 {
				pag, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: orden, Dir: dir, Limit: "3", Cursor: cursor})
				require.NoError(t, err)
				paginado = append(paginado, pag.Items...)
				assert.Equal(t, 8, pag.Totales.N, "los totales son del filtro completo")
				if pag.SiguienteCursor == "" {
					break
				}
				cursor = pag.SiguienteCursor
			}
			assert.Equal(t, idsDe(todo.Items), idsDe(paginado), "orden=%s dir=%s: páginas = lista completa", orden, dir)
		}
	}

	monto, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: "monto", Dir: "desc", Limit: "2"})
	require.NoError(t, err)
	assert.Equal(t, []string{"pago:1007", "pago:1002"}, idsDe(monto.Items))
}

func TestActividad_ElCursorSobreviveAQueDesaparezcaSuFila(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()
	pag, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Limit: "3"})
	require.NoError(t, err)
	require.Equal(t, []string{"pago:p-80", "pago:1005", "pago:1004"}, idsDe(pag.Items))

	// pago:1004 (la última de la página) se borra antes de pedir la siguiente.
	acts := m.actividades.porZona[zonaRuta21]
	for i, a := range acts {
		if a.ID == "pago:1004" {
			m.actividades.porZona[zonaRuta21] = append(acts[:i:i], acts[i+1:]...)
			break
		}
	}
	sig, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Limit: "3", Cursor: pag.SiguienteCursor})
	require.NoError(t, err)
	assert.Equal(t, []string{"pago:1003", "visita:v-1", "pago:1002"}, idsDe(sig.Items), "ni salta ni repite")
}

func TestActividad_ParametrosInvalidos(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()
	malos := []desempenoapp.FiltroActividadInput{
		{Tipos: []string{"llamada"}},
		{FormasCobro: []string{"deposito"}},
		{MontoMin: "cien"},
		{ConFotos: "si"},
		{ConGPS: "1"},
		{Orden: "folio"},
		{Dir: "arriba"},
		{Limit: "0"},
		{Limit: "501"},
		{Cursor: "no-es-base64!"},
		{Desde: "2026-10-10"},
		{Hasta: "ayer"},
		{Desde: "2026-07-01T00:00:00Z", Hasta: "2026-10-14T00:00:00Z"}, // > 92 días
		{Desde: "2026-10-14T00:00:00Z", Hasta: "2026-10-10T00:00:00Z"},
	}
	for _, in := range malos {
		_, err := svc.Actividad(ctx, "uid-juan", in)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "%+v", in)
	}

	pag, err := svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: "hora", Limit: "2"})
	require.NoError(t, err)
	_, err = svc.Actividad(ctx, "uid-juan", desempenoapp.FiltroActividadInput{Orden: "cliente", Limit: "2", Cursor: pag.SiguienteCursor})
	require.ErrorIs(t, err, domain.ErrParametroInvalido, "un cursor de otro orden se rechaza")

	_, err = svc.Actividad(ctx, "uid-lupe", desempenoapp.FiltroActividadInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinZona)

	sinInicio := m.juan
	sinInicio.UID, sinInicio.InicioSemana = "uid-sin-inicio", nil
	m.usuarios.usuarios = append(m.usuarios.usuarios, sinInicio)
	_, err = svc.Actividad(ctx, "uid-sin-inicio", desempenoapp.FiltroActividadInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinInicioSemana, "sin desde y sin inicio de semana no hay rango")
	_, err = svc.Actividad(ctx, "uid-sin-inicio", desempenoapp.FiltroActividadInput{Desde: "2026-10-10T00:00:00Z"})
	require.NoError(t, err, "con desde explícito sí")
}
```

`internal/desempeno/app/cuentas_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

func ventasDe(cs []desempenoapp.CuentaVista) []int {
	out := make([]int, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Cuenta.VentaID)
	}
	return out
}

func TestCuentas_EstadoPorVentaYConteos(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)

	v, err := svc.Cuentas(context.Background(), "uid-juan", desempenoapp.CuentasInput{})
	require.NoError(t, err)
	assert.Equal(t, *m.juan.InicioSemana, v.InicioSemana)
	assert.Equal(t, desempenoapp.ConteosCuentas{SinVisitar: 2, Volver: 2, Despues: 0, Pagados: 4, SinNada: 1, Total: 8}, v.Conteos)
	require.Len(t, v.Cuentas, 8)
	assert.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8}, ventasDe(v.Cuentas), "orden cliente asc")

	por := map[int]desempenoapp.CuentaVista{}
	for _, c := range v.Cuentas {
		por[c.Cuenta.VentaID] = c
	}
	assert.Equal(t, domain.EstadoPago, por[1].Resultado.Estado)
	assert.Equal(t, "Pagó esta semana", por[1].Etiqueta)
	assert.Equal(t, "pago:1001", por[1].Resultado.DeterminadoPor)
	assert.Equal(t, domain.EstadoAbonoParcial, por[4].Resultado.Estado, "lo de oficina también cuenta")
	assert.Equal(t, domain.EstadoSinTocar, por[5].Resultado.Estado, "la condonación no es dinero")
	assert.True(t, por[5].Resultado.ConAlgo)
	require.NotNil(t, por[5].UltimaActividad)
	assert.Equal(t, domain.TipoCondonacion, por[5].UltimaActividad.Tipo)
	assert.Equal(t, domain.EstadoAbonoParcial, por[6].Resultado.Estado, "un pago aún no aplicado sí es dinero en el estado (como el teléfono)")
	assert.False(t, por[8].Resultado.ConAlgo)
	assert.Nil(t, por[8].UltimaActividad)
	assert.Equal(t, 2, por[8].NumAtrasos, "de rutas")
	require.NotNil(t, por[8].Segmento)
	assert.Equal(t, domain.SegmentoSinVisitar, *por[8].Segmento)
	assert.False(t, por[1].Nueva)
}

func TestCuentas_FiltrosYOrden(t *testing.T) {
	t.Parallel()
	casos := []struct {
		nombre string
		in     desempenoapp.CuentasInput
		ventas []int
	}{
		{"pagados", desempenoapp.CuentasInput{Segmento: "pagados"}, []int{1, 2, 3, 7}},
		{"volver", desempenoapp.CuentasInput{Segmento: "volver"}, []int{4, 6}},
		{"sin nada", desempenoapp.CuentasInput{Segmento: "sin_nada"}, []int{8}},
		{"q", desempenoapp.CuentasInput{Q: "cliente 1"}, []int{1, 2, 3, 4, 5, 6, 7, 8}},
		{"q por folio", desempenoapp.CuentasInput{Q: "Z07"}, []int{7}},
		{"saldo desc", desempenoapp.CuentasInput{Orden: "saldo", Dir: "desc"}, []int{8, 1, 4, 2, 3, 5, 6, 7}},
		{"atrasos desc: el empate también se invierte", desempenoapp.CuentasInput{Orden: "atrasos", Dir: "desc"}, []int{8, 7, 6, 5, 4, 3, 2, 1}},
		{"última actividad asc", desempenoapp.CuentasInput{Orden: "ultima_actividad"}, []int{8, 7, 1, 2, 3, 4, 5, 6}},
	}
	for _, c := range casos {
		m := nuevoMundo(t)
		svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
		v, err := svc.Cuentas(context.Background(), "uid-juan", c.in)
		require.NoError(t, err, c.nombre)
		assert.Equal(t, c.ventas, ventasDe(v.Cuentas), c.nombre)
		assert.Equal(t, 8, v.Conteos.Total, "%s: los conteos no dependen del filtro", c.nombre)
	}
}

func TestCuentas_Errores(t *testing.T) {
	t.Parallel()
	m := nuevoMundo(t)
	svc := nuevoServicio(t, m.usuarios, m.actividades, m.cuentas, m.ahora)
	ctx := context.Background()
	for _, in := range []desempenoapp.CuentasInput{{Segmento: "todos"}, {Orden: "folio"}, {Dir: "abajo"}} {
		_, err := svc.Cuentas(ctx, "uid-juan", in)
		require.ErrorIs(t, err, domain.ErrParametroInvalido, "%+v", in)
	}
	_, err := svc.Cuentas(ctx, "uid-lupe", desempenoapp.CuentasInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioSinZona)
	_, err = svc.Cuentas(ctx, "uid-nadie", desempenoapp.CuentasInput{})
	require.ErrorIs(t, err, domain.ErrUsuarioNoEncontrado)
}
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test ./internal/desempeno/app/`
Expected: FAIL de compilación (`svc.Actividad undefined`).

- [ ] **Step 3: Implementación**

`internal/desempeno/app/texto.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// normalizar quita acentos y mayúsculas para buscar "sin acentos ni
// mayúsculas" (contrato §4 `q`): "MÉNDEZ" y "mendez" son iguales.
func normalizar(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		_, _ = b.WriteRune(unicode.ToLower(r))
	}
	return strings.TrimSpace(b.String())
}
```

`internal/desempeno/app/actividad.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Límites de GET /desempeno/usuarios/{uid}/actividad (contrato §4).
const (
	LimitePorOmision = 100
	LimiteMaximo     = 500
	RangoMaximo      = 92 * 24 * time.Hour
)

// Órdenes admitidos.
const (
	OrdenHora          = "hora"
	OrdenCliente       = "cliente"
	OrdenMonto         = "monto"
	OrdenTipo          = "tipo"
	OrdenRegistradoPor = "registrado_por"
	registradoOficina  = "oficina"
)

// FiltroActividadInput lleva los parámetros crudos de la consulta. Los repetibles
// llegan como lista.
type FiltroActividadInput struct {
	Desde         string
	Hasta         string
	Tipos         []string
	FormasCobro   []string
	TiposVisita   []string
	RegistradoPor []string
	MontoMin      string
	MontoMax      string
	ConFotos      string
	ConGPS        string
	Q             string
	Orden         string
	Dir           string
	Cursor        string
	Limit         string
}

// TotalesActividad son del filtro completo, no de la página.
type TotalesActividad struct {
	N       int
	Importe decimal.Decimal
	PorTipo map[domain.TipoActividad]int
}

// PaginaActividad es la respuesta de la consulta.
type PaginaActividad struct {
	Items           []domain.Actividad
	SiguienteCursor string // "" = no hay más
	Totales         TotalesActividad
}

type filtroActividad struct {
	desde, hasta time.Time
	tipos        map[string]bool
	formas       map[string]bool
	tiposVisita  map[string]bool
	registrado   map[string]bool
	montoMin     *decimal.Decimal
	montoMax     *decimal.Decimal
	conFotos     *bool
	conGPS       *bool
	q            string
	orden        string
	desc         bool
	cursor       *cursorActividad
	limit        int
}

type cursorActividad struct {
	Orden string `json:"o"`
	Dir   string `json:"d"`
	Hora  string `json:"h"`
	Texto string `json:"t"`
	Monto string `json:"m"`
	ID    string `json:"i"`
}

func invalido(param string) error { return domain.ErrParametroInvalido.WithField("parametro", param) }

func conjunto(valores []string, param string, permitidos ...string) (map[string]bool, error) {
	if len(valores) == 0 {
		return nil, nil //nolint:nilnil // sin filtro = nil
	}
	ok := map[string]bool{}
	for _, p := range permitidos {
		ok[p] = true
	}
	out := map[string]bool{}
	for _, v := range valores {
		if len(permitidos) > 0 && !ok[v] {
			return nil, invalido(param)
		}
		out[v] = true
	}
	return out, nil
}

func boolOpcional(s, param string) (*bool, error) {
	switch s {
	case "":
		return nil, nil //nolint:nilnil // sin filtro = nil
	case "true":
		v := true
		return &v, nil
	case "false":
		v := false
		return &v, nil
	}
	return nil, invalido(param)
}

func montoOpcional(s, param string) (*decimal.Decimal, error) {
	if s == "" {
		return nil, nil //nolint:nilnil // sin filtro = nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil, invalido(param)
	}
	return &d, nil
}

func (s *Service) parseFiltro(in FiltroActividadInput, c contexto) (filtroActividad, error) {
	f := filtroActividad{orden: OrdenHora, desc: true, limit: LimitePorOmision}
	var err error
	if f.desde, f.hasta, err = parseRango(in.Desde, in.Hasta, c); err != nil {
		return f, err
	}
	if err := f.parseConjuntos(in); err != nil {
		return f, err
	}
	if err := f.parseOpcionales(in); err != nil {
		return f, err
	}
	if err := f.parseOrden(in.Orden, in.Dir); err != nil {
		return f, err
	}
	err = f.parsePaginacion(in.Limit, in.Cursor)
	return f, err
}

// parseRango: por omisión [inicio de semana, ahora]; máximo 92 días.
func parseRango(desdeS, hastaS string, c contexto) (time.Time, time.Time, error) {
	var desde, hasta time.Time
	var err error
	switch {
	case desdeS != "":
		if desde, err = time.Parse(time.RFC3339, desdeS); err != nil {
			return desde, hasta, invalido("desde")
		}
	case c.usuario.InicioSemana == nil:
		return desde, hasta, domain.ErrUsuarioSinInicioSemana
	default:
		desde = *c.usuario.InicioSemana
	}
	hasta = c.ahora
	if hastaS != "" {
		if hasta, err = time.Parse(time.RFC3339, hastaS); err != nil {
			return desde, hasta, invalido("hasta")
		}
	}
	desde, hasta = desde.UTC(), hasta.UTC()
	if hasta.Before(desde) || hasta.Sub(desde) > RangoMaximo {
		return desde, hasta, invalido("desde/hasta")
	}
	return desde, hasta, nil
}

func (f *filtroActividad) parseConjuntos(in FiltroActividadInput) error {
	var err error
	if f.tipos, err = conjunto(in.Tipos, "tipo",
		string(domain.TipoPago), string(domain.TipoVisita), string(domain.TipoCondonacion)); err != nil {
		return err
	}
	if f.formas, err = conjunto(in.FormasCobro, "forma_cobro", domain.EtiquetaEfectivo, domain.EtiquetaTransferencia,
		domain.EtiquetaCheque, domain.EtiquetaCondonacion, domain.EtiquetaOtra); err != nil {
		return err
	}
	if f.tiposVisita, err = conjunto(in.TiposVisita, "tipo_visita"); err != nil {
		return err
	}
	f.registrado, err = conjunto(in.RegistradoPor, "registrado_por")
	return err
}

func (f *filtroActividad) parseOpcionales(in FiltroActividadInput) error {
	var err error
	if f.montoMin, err = montoOpcional(in.MontoMin, "monto_min"); err != nil {
		return err
	}
	if f.montoMax, err = montoOpcional(in.MontoMax, "monto_max"); err != nil {
		return err
	}
	if f.conFotos, err = boolOpcional(in.ConFotos, "con_fotos"); err != nil {
		return err
	}
	if f.conGPS, err = boolOpcional(in.ConGPS, "con_gps"); err != nil {
		return err
	}
	f.q = normalizar(in.Q)
	return nil
}

func (f *filtroActividad) parseOrden(orden, dir string) error {
	switch orden {
	case "":
	case OrdenHora, OrdenCliente, OrdenMonto, OrdenTipo, OrdenRegistradoPor:
		f.orden = orden
	default:
		return invalido("orden")
	}
	switch dir {
	case "", "desc":
	case "asc":
		f.desc = false
	default:
		return invalido("dir")
	}
	return nil
}

func (f *filtroActividad) parsePaginacion(limit, cursor string) error {
	if limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n < 1 || n > LimiteMaximo {
			return invalido("limit")
		}
		f.limit = n
	}
	if cursor == "" {
		return nil
	}
	cur, err := decodificarCursor(cursor)
	if err != nil || cur.Orden != f.orden || cur.Dir != dirDe(f.desc) {
		return invalido("cursor")
	}
	f.cursor = &cur
	return nil
}

func dirDe(desc bool) string {
	if desc {
		return "desc"
	}
	return "asc"
}

//nolint:cyclop // un filtro por rama; todos son opcionales.
func (f *filtroActividad) acepta(a domain.Actividad) bool {
	if a.Hora.Before(f.desde) || a.Hora.After(f.hasta) {
		return false
	}
	if f.tipos != nil && !f.tipos[string(a.Tipo)] {
		return false
	}
	if f.formas != nil && (a.Pago == nil || !f.formas[domain.EtiquetaFormaCobro(a.Pago.FormaCobroID)]) {
		return false
	}
	if f.tiposVisita != nil && (a.Visita == nil || !f.tiposVisita[a.Visita.TipoVisita]) {
		return false
	}
	if f.registrado != nil && !f.registrado[claveRegistrador(a)] {
		return false
	}
	if (f.montoMin != nil || f.montoMax != nil) && a.Pago == nil {
		return false
	}
	if f.montoMin != nil && a.Importe().LessThan(*f.montoMin) {
		return false
	}
	if f.montoMax != nil && a.Importe().GreaterThan(*f.montoMax) {
		return false
	}
	if f.conFotos != nil && (a.NumFotos > 0) != *f.conFotos {
		return false
	}
	if f.conGPS != nil && a.ConGPS() != *f.conGPS {
		return false
	}
	if f.q != "" && !strings.Contains(normalizar(a.ClienteNombre), f.q) && !strings.Contains(normalizar(a.Folio), f.q) {
		return false
	}
	return true
}

// claveRegistrador devuelve "oficina" o el uid (vacío si no se resolvió, y entonces
// ningún filtro por uid lo encuentra — decisión D-API-4).
func claveRegistrador(a domain.Actividad) string {
	if a.RegistradoPor.Origen == domain.OrigenOficina {
		return registradoOficina
	}
	return a.RegistradoPor.UsuarioUID
}

type claveOrden struct {
	hora  time.Time
	texto string
	monto decimal.Decimal
	id    string
}

func claveDe(a domain.Actividad, orden string) claveOrden {
	k := claveOrden{id: a.ID, monto: decimal.Zero}
	switch orden {
	case OrdenHora:
		k.hora = a.Hora
	case OrdenCliente:
		k.texto = normalizar(a.ClienteNombre)
	case OrdenMonto:
		k.monto = a.Importe()
	case OrdenTipo:
		k.texto = string(a.Tipo)
	case OrdenRegistradoPor:
		k.texto = normalizar(a.RegistradoPor.Nombre)
	}
	return k
}

// comparar es un orden total: la clave del orden y, en empate, el id.
func comparar(a, b claveOrden) int {
	if c := a.hora.Compare(b.hora); c != 0 {
		return c
	}
	if c := strings.Compare(a.texto, b.texto); c != 0 {
		return c
	}
	if c := a.monto.Cmp(b.monto); c != 0 {
		return c
	}
	return strings.Compare(a.id, b.id)
}

func (f *filtroActividad) ordenar(acts []domain.Actividad) {
	sort.SliceStable(acts, func(i, j int) bool {
		c := comparar(claveDe(acts[i], f.orden), claveDe(acts[j], f.orden))
		if f.desc {
			return c > 0
		}
		return c < 0
	})
}

func codificarCursor(a domain.Actividad, f *filtroActividad) string {
	k := claveDe(a, f.orden)
	cur := cursorActividad{Orden: f.orden, Dir: dirDe(f.desc), Texto: k.texto, Monto: k.monto.String(), ID: k.id}
	if !k.hora.IsZero() {
		cur.Hora = k.hora.UTC().Format(time.RFC3339Nano)
	}
	b, _ := json.Marshal(cur) //nolint:errchkjson // struct de strings: no puede fallar.
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodificarCursor(s string) (cursorActividad, error) {
	var cur cursorActividad
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cur, err
	}
	if err := json.Unmarshal(b, &cur); err != nil {
		return cur, err
	}
	return cur, nil
}

func (cur cursorActividad) clave() (claveOrden, error) {
	k := claveOrden{texto: cur.Texto, id: cur.ID}
	var err error
	if cur.Hora != "" {
		if k.hora, err = time.Parse(time.RFC3339Nano, cur.Hora); err != nil {
			return k, err
		}
	}
	if k.monto, err = decimal.NewFromString(cur.Monto); err != nil {
		return k, err
	}
	return k, nil
}

// filtrar trae TODA la zona del usuario en el rango y aplica el filtro.
func (s *Service) filtrar(ctx context.Context, uid string, in FiltroActividadInput) ([]domain.Actividad, filtroActividad, error) {
	c, err := s.resolverUsuario(ctx, uid)
	if err != nil {
		return nil, filtroActividad{}, err
	}
	if c.usuario.ZonaID <= 0 {
		return nil, filtroActividad{}, domain.ErrUsuarioSinZona
	}
	f, err := s.parseFiltro(in, c)
	if err != nil {
		return nil, f, err
	}
	acts, err := s.actividadesCompletas(ctx, c.indice, c.usuario.ZonaID, f.desde, f.hasta)
	if err != nil {
		return nil, f, err
	}
	out := make([]domain.Actividad, 0, len(acts))
	for _, a := range acts {
		if f.acepta(a) {
			out = append(out, a)
		}
	}
	f.ordenar(out)
	return out, f, nil
}

// Actividad devuelve una página de la actividad de TODA la zona del usuario (cualquier
// registrador) con totales del filtro completo y cursor estable.
func (s *Service) Actividad(ctx context.Context, uid string, in FiltroActividadInput) (PaginaActividad, error) {
	acts, f, err := s.filtrar(ctx, uid, in)
	if err != nil {
		return PaginaActividad{}, err
	}
	pag := PaginaActividad{Totales: TotalesActividad{
		N: len(acts), Importe: decimal.Zero,
		PorTipo: map[domain.TipoActividad]int{domain.TipoPago: 0, domain.TipoVisita: 0, domain.TipoCondonacion: 0},
	}}
	for _, a := range acts {
		pag.Totales.PorTipo[a.Tipo]++
		if a.CuentaComoDinero() {
			pag.Totales.Importe = pag.Totales.Importe.Add(a.Importe())
		}
	}
	inicio := 0
	if f.cursor != nil {
		k, err := f.cursor.clave()
		if err != nil {
			return PaginaActividad{}, invalido("cursor")
		}
		inicio = sort.Search(len(acts), func(i int) bool {
			c := comparar(claveDe(acts[i], f.orden), k)
			if f.desc {
				return c < 0
			}
			return c > 0
		})
	}
	fin := inicio + f.limit
	if fin > len(acts) {
		fin = len(acts)
	}
	pag.Items = acts[inicio:fin]
	if fin < len(acts) && fin > inicio {
		pag.SiguienteCursor = codificarCursor(acts[fin-1], &f)
	}
	return pag, nil
}
```

`internal/desempeno/app/cuentas.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

// Órdenes de Cuentas (contrato §5).
const (
	OrdenCuentasCliente         = "cliente"
	OrdenCuentasSaldo           = "saldo"
	OrdenCuentasAtrasos         = "atrasos"
	OrdenCuentasUltimaActividad = "ultima_actividad"
	segmentoSinNada             = "sin_nada"
)

// CuentasInput lleva los parámetros crudos de GET /desempeno/usuarios/{uid}/cuentas.
type CuentasInput struct {
	Segmento string
	Q        string
	Orden    string
	Dir      string
}

// ConteosCuentas cuenta TODO el universo, antes de filtrar (pastillas).
type ConteosCuentas struct {
	SinVisitar int
	Volver     int
	Despues    int
	Pagados    int
	SinNada    int
	Total      int
}

// ActividadDeCuenta es `ultima_actividad {id, tipo, hora}`.
type ActividadDeCuenta struct {
	ID   string
	Tipo domain.TipoActividad
	Hora time.Time
}

// CuentaVista es una venta del universo con su estado de la semana.
type CuentaVista struct {
	Cuenta          domain.CuentaUniverso
	Resultado       domain.ResultadoCuenta
	Trato           domain.Trato
	Etiqueta        string
	Detalle         string
	Segmento        *domain.Segmento
	UltimaActividad *ActividadDeCuenta
	NumAtrasos      int
	Nueva           bool
}

// VistaCuentas es la respuesta de Cuentas.
type VistaCuentas struct {
	InicioSemana time.Time
	Conteos      ConteosCuentas
	Cuentas      []CuentaVista
}

type filtroCuentas struct {
	segmento string
	q        string
	orden    string
	desc     bool
}

func parseFiltroCuentas(in CuentasInput) (filtroCuentas, error) {
	f := filtroCuentas{segmento: in.Segmento, q: normalizar(in.Q), orden: OrdenCuentasCliente}
	switch in.Segmento {
	case "", string(domain.SegmentoSinVisitar), string(domain.SegmentoVolver),
		string(domain.SegmentoDespues), string(domain.SegmentoPagados), segmentoSinNada:
	default:
		return f, invalido("segmento")
	}
	switch in.Orden {
	case "":
	case OrdenCuentasCliente, OrdenCuentasSaldo, OrdenCuentasAtrasos, OrdenCuentasUltimaActividad:
		f.orden = in.Orden
	default:
		return f, invalido("orden")
	}
	switch in.Dir {
	case "", "asc":
	case "desc":
		f.desc = true
	default:
		return f, invalido("dir")
	}
	return f, nil
}

func (f filtroCuentas) acepta(c CuentaVista) bool {
	switch {
	case f.segmento == segmentoSinNada && c.Resultado.ConAlgo:
		return false
	case f.segmento != "" && f.segmento != segmentoSinNada &&
		(c.Segmento == nil || string(*c.Segmento) != f.segmento):
		return false
	}
	if f.q == "" {
		return true
	}
	return strings.Contains(normalizar(c.Cuenta.ClienteNombre), f.q) || strings.Contains(normalizar(c.Cuenta.Folio), f.q)
}

// Cuentas calcula el estado por venta de la semana del usuario, igual que el teléfono
// pero con todas las visitas de la zona (spec §2).
func (s *Service) Cuentas(ctx context.Context, uid string, in CuentasInput) (VistaCuentas, error) {
	f, err := parseFiltroCuentas(in)
	if err != nil {
		return VistaCuentas{}, err
	}
	c, err := s.resolverUsuario(ctx, uid)
	if err != nil {
		return VistaCuentas{}, err
	}
	sem, err := s.calcularSemana(ctx, c)
	if err != nil {
		return VistaCuentas{}, err
	}
	cs, err := s.usuarios.CobroSemana(ctx, c.usuario)
	if err != nil {
		return VistaCuentas{}, err
	}
	atrasos := make(map[int]int, len(cs.Ventas))
	for _, v := range cs.Ventas {
		atrasos[v.VentaID] = int(v.Vencidas.IntPart())
	}
	tipoPorID := make(map[string]domain.TipoActividad, len(sem.actividades))
	for _, a := range sem.actividades {
		tipoPorID[a.ID] = a.Tipo
	}

	hoy := domain.DiaDe(c.ahora, s.loc)
	vista := VistaCuentas{InicioSemana: sem.ventana.Inicio(), Cuentas: []CuentaVista{}}
	for _, cu := range sem.universo {
		r := sem.derivacion.PorVenta[cu.VentaID]
		cv := CuentaVista{
			Cuenta: cu, Resultado: r, Trato: domain.TratoDe(r),
			Etiqueta: domain.EtiquetaDe(r), Detalle: domain.DetalleDe(r),
			NumAtrasos: atrasos[cu.VentaID], Nueva: cu.EsNueva(sem.ventana.Inicio()),
		}
		if seg, ok := domain.SegmentoDe(r, hoy); ok {
			cv.Segmento = &seg
			contarSegmento(&vista.Conteos, seg)
		}
		if !r.ConAlgo {
			vista.Conteos.SinNada++
		}
		if r.UltimaActividadID != "" {
			cv.UltimaActividad = &ActividadDeCuenta{ID: r.UltimaActividadID, Tipo: tipoPorID[r.UltimaActividadID], Hora: r.UltimaActividadHora}
		}
		vista.Conteos.Total++
		if f.acepta(cv) {
			vista.Cuentas = append(vista.Cuentas, cv)
		}
	}
	ordenarCuentas(vista.Cuentas, f)
	return vista, nil
}

func contarSegmento(c *ConteosCuentas, seg domain.Segmento) {
	switch seg {
	case domain.SegmentoSinVisitar:
		c.SinVisitar++
	case domain.SegmentoVolver:
		c.Volver++
	case domain.SegmentoDespues:
		c.Despues++
	case domain.SegmentoPagados:
		c.Pagados++
	}
}

func ordenarCuentas(cs []CuentaVista, f filtroCuentas) {
	sort.SliceStable(cs, func(i, j int) bool {
		c := compararCuentas(cs[i], cs[j], f.orden)
		if f.desc {
			return c > 0
		}
		return c < 0
	})
}

func compararCuentas(a, b CuentaVista, orden string) int {
	var c int
	switch orden {
	case OrdenCuentasSaldo:
		c = a.Cuenta.Saldo.Cmp(b.Cuenta.Saldo)
	case OrdenCuentasAtrasos:
		c = a.NumAtrasos - b.NumAtrasos
	case OrdenCuentasUltimaActividad:
		c = horaDe(a).Compare(horaDe(b))
	default:
		c = strings.Compare(normalizar(a.Cuenta.ClienteNombre), normalizar(b.Cuenta.ClienteNombre))
	}
	if c != 0 {
		return c
	}
	return a.Cuenta.VentaID - b.Cuenta.VentaID
}

// horaDe devuelve la hora; sin actividad ordena como el instante cero (primero en asc).
func horaDe(c CuentaVista) time.Time {
	if c.UltimaActividad == nil {
		return time.Time{}
	}
	return c.UltimaActividad.Hora
}
```


- [ ] **Step 4: Correr y ver que pasan (cobertura de app ≥ 90 %)**

Run: `go test -cover ./internal/desempeno/app/`
Expected: `ok … coverage: 95.5% of statements`.

- [ ] **Step 5: Control positivo**

En `actividad.go` (`Actividad`), cambiar el `sort.Search` para que use `c >= 0` / `c <= 0` en vez de `c > 0` / `c < 0` → debe fallar `TestActividad_OrdenYCursorEstable` (repite la última fila de cada página). Restaurar; PASS; diff limpio.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/app
git commit -m "feat(desempeno): actividad de la zona con filtros y cursor estable; cuentas de la semana"
```

---

## Carril B — contrato de rutas y repositorios Firebird (rama `feat/desempeno-b-repos`)

Antes de empezar: `set -a && . ./.env && set +a` (el Firebird de desarrollo `mueblera-firebird` debe estar arriba) y `make fb-snapshot NAME=antes-desempeno-b` por costumbre (las pruebas revierten, pero el snapshot es barato).

### Tarea B1: contrato de rutas `CobranzaUsuariosReader`

**Files:**
- Create: `internal/rutas/rutas_contracts.go`
- Create: `internal/rutas/app/cobranza_usuarios.go`
- Test: `internal/rutas/app/cobranza_usuarios_test.go`

**Interfaces:**
- Consumes: `rutasapp.Service` existente (`repo.ListarRutas`, `calendario.ListarCobradores`, `cobranza.VentasPorZona`, `enrichVentas`, `CalcularResumenPonderado`).
- Produces: paquete `rutas` con `UsuarioCobrador{UID, Nombre, Email, CobradorID, ZonaID, ZonaNombre, InicioSemana *time.Time}`, `VentaCobro{VentaID, ClienteID, Parcialidad, AbonoSemana, Saldo, Vencidas, Aporte, AplicaPonderado}`, `CobranzaUsuario{Usuario, Ventas, PctPonderado *decimal.Decimal}`, interfaz `CobranzaUsuariosReader{UsuariosCobradores(ctx) ([]UsuarioCobrador, error); CobranzaDeUsuario(ctx, UsuarioCobrador) (CobranzaUsuario, error)}`, `ErrCalendarioNoDisponible` (503). `*rutasapp.Service` la satisface.

- [ ] **Step 1: Escribir la prueba que falla**

`internal/rutas/app/cobranza_usuarios_test.go` (archivo nuevo, completo):

```go
//nolint:misspell // Spanish vocabulary per project convention.
package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/rutas"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
	"github.com/abdimuy/msp-api/internal/rutas/ports/outbound"
)

func TestService_UsuariosCobradores_IncluyeNoConfiguradosYNombreDeZona(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	svc := NewService(
		&fakeRutasRepo{rows: []rutasdomain.RutaResumen{{ZonaID: 21563, ZonaNombre: "RUTA 21"}}},
		&fakeCobranzaRepo{},
		&fakeCalendario{usuarios: []outbound.UsuarioCobrador{
			{UID: "juan", Nombre: "Juan Carlos Méndez", CobradorID: 11502, ZonaID: 21563, FechaInicio: inicio},
			{UID: "sinzona", Nombre: "Rosa Elena Pérez", CobradorID: 11600},
			{UID: "sinfecha", Nombre: "Guadalupe Torres", CobradorID: 11700, ZonaID: 21563},
		}},
	)

	got, err := svc.UsuariosCobradores(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 3, "los no configurados también salen: el tablero los marca")
	assert.Equal(t, "RUTA 21", got[0].ZonaNombre)
	require.NotNil(t, got[0].InicioSemana)
	assert.Equal(t, inicio, *got[0].InicioSemana)
	assert.Equal(t, 0, got[1].ZonaID)
	assert.Empty(t, got[1].ZonaNombre)
	assert.Nil(t, got[2].InicioSemana)
}

func TestService_UsuariosCobradores_FirestoreCaidoEsErrorTipado(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{},
		&fakeCalendario{usuariosE: errors.New("firestore: deadline exceeded")})

	_, err := svc.UsuariosCobradores(context.Background())
	require.ErrorIs(t, err, rutas.ErrCalendarioNoDisponible)
}

func TestService_UsuariosCobradores_ErrorDeZonasSePropaga(t *testing.T) {
	t.Parallel()
	svc := NewService(&fakeRutasRepo{err: errors.New("firebird caído")}, &fakeCobranzaRepo{},
		&fakeCalendario{usuarios: []outbound.UsuarioCobrador{{UID: "juan", CobradorID: 1}}})

	_, err := svc.UsuariosCobradores(context.Background())
	require.Error(t, err)
	require.NotErrorIs(t, err, rutas.ErrCalendarioNoDisponible)
}

func TestService_CobranzaDeUsuario_IgualQueDesglosePorUsuario(t *testing.T) {
	t.Parallel()
	zonaID := 21563
	inicio := time.Date(2026, 10, 9, 13, 46, 0, 0, time.UTC)
	ventas := []rutasdomain.VentaCobranza{{
		VentaID: 1, ClienteID: 10, ZonaID: zonaID,
		Parcialidad: decimal.NewFromInt(100), Frecuencia: rutasdomain.Semanal,
		AbonoSemana: decimal.NewFromInt(100), Saldo: decimal.NewFromInt(900),
		TotalImporte: decimal.NewFromInt(4000), FechaCargo: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}}
	cal := &fakeCalendario{usuarios: []outbound.UsuarioCobrador{
		{UID: "juan", CobradorID: 11502, ZonaID: zonaID, FechaInicio: inicio},
	}}
	repo := &fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: ventas}}
	svc := NewService(&fakeRutasRepo{}, repo, cal)

	got, err := svc.CobranzaDeUsuario(context.Background(), rutas.UsuarioCobrador{UID: "juan", CobradorID: 11502, ZonaID: zonaID, InicioSemana: &inicio})
	require.NoError(t, err)

	// Mismo resultado que el endpoint que oficina ya usa (/v2/rutas/usuarios/{uid}/cobranza).
	desglose, _, _, err := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{rows: map[int][]rutasdomain.VentaCobranza{zonaID: {ventas[0]}}}, cal).
		DesglosePorUsuario(context.Background(), "juan")
	require.NoError(t, err)
	require.Len(t, got.Ventas, 1)
	require.Len(t, desglose, 1)
	assert.Equal(t, desglose[0].VentaID, got.Ventas[0].VentaID)
	assert.True(t, desglose[0].Aporte.Equal(got.Ventas[0].Aporte))
	assert.True(t, desglose[0].Vencidas.Equal(got.Ventas[0].Vencidas))
	assert.Equal(t, desglose[0].AplicaPonderado, got.Ventas[0].AplicaPonderado)
	assert.Equal(t, rutasdomain.CalcularResumenPonderado(desglose).Pct, got.PctPonderado)
}

func TestService_CobranzaDeUsuario_SinConfigurarNoConsulta(t *testing.T) {
	t.Parallel()
	repo := &fakeCobranzaRepo{err: errors.New("no debió consultarse")}
	svc := NewService(&fakeRutasRepo{}, repo, &fakeCalendario{})
	inicio := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)

	for _, u := range []rutas.UsuarioCobrador{{UID: "a"}, {UID: "b", ZonaID: 1}, {UID: "c", InicioSemana: &inicio}} {
		got, err := svc.CobranzaDeUsuario(context.Background(), u)
		require.NoError(t, err, u.UID)
		assert.Empty(t, got.Ventas, u.UID)
		assert.Nil(t, got.PctPonderado, u.UID)
	}
}

func TestService_CobranzaDeUsuario_ErrorDeFirebirdSePropaga(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	svc := NewService(&fakeRutasRepo{}, &fakeCobranzaRepo{err: errors.New("firebird caído")}, &fakeCalendario{})
	_, err := svc.CobranzaDeUsuario(context.Background(), rutas.UsuarioCobrador{UID: "a", ZonaID: 1, InicioSemana: &inicio})
	require.Error(t, err)
}
```


- [ ] **Step 2: Correr y ver que falla**

Run: `go test ./internal/rutas/app/`
Expected: FAIL de compilación (`undefined: rutas.UsuarioCobrador`).

- [ ] **Step 3: Implementación**

`internal/rutas/rutas_contracts.go` (archivo nuevo, completo):

```go
// Package rutas is the cross-module surface of the rutas bounded context.
// Other modules import only this package — never internal/rutas/domain,
// internal/rutas/app or internal/rutas/infra (depguard).
//
// The contract exports the per-user weekly cobranza that rutas already
// computes (window = FECHA_CARGA_INICIAL of the Firestore user), so consumers
// such as desempeno reuse it instead of re-implementing aporte/atrasos.
//
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutas

import (
	"context"
	"time"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// UsuarioCobrador is one Firestore `users` document with COBRADOR_ID > 0.
// ZonaID 0 = no zona; InicioSemana nil = no FECHA_CARGA_INICIAL.
type UsuarioCobrador struct {
	UID          string
	Nombre       string
	Email        string
	CobradorID   int
	ZonaID       int
	ZonaNombre   string
	InicioSemana *time.Time
}

// VentaCobro is the per-venta weekly breakdown rutas computes
// (VentaCobranza enriched by enrichVentas).
type VentaCobro struct {
	VentaID         int
	ClienteID       int
	Parcialidad     decimal.Decimal
	AbonoSemana     decimal.Decimal
	Saldo           decimal.Decimal
	Vencidas        decimal.Decimal
	Aporte          decimal.Decimal
	AplicaPonderado bool
}

// CobranzaUsuario is the weekly cobranza of one user over THEIR window.
type CobranzaUsuario struct {
	Usuario      UsuarioCobrador
	Ventas       []VentaCobro
	PctPonderado *decimal.Decimal
}

// CobranzaUsuariosReader is satisfied by rutas app.Service.
type CobranzaUsuariosReader interface {
	// UsuariosCobradores lists every Firestore user with COBRADOR_ID > 0,
	// configured or not. A Firestore failure is ErrCalendarioNoDisponible.
	UsuariosCobradores(ctx context.Context) ([]UsuarioCobrador, error)
	// CobranzaDeUsuario computes the weekly breakdown for an already-resolved
	// user (no second Firestore read). A user without zona or window returns
	// an empty breakdown, not an error.
	CobranzaDeUsuario(ctx context.Context, u UsuarioCobrador) (CobranzaUsuario, error)
}

// ErrCalendarioNoDisponible wraps any failure reading Firestore `users`.
var ErrCalendarioNoDisponible = apperror.NewServiceUnavailable(
	"rutas_calendario_unavailable",
	"no se pudo leer el calendario de cobradores",
)
```

`internal/rutas/app/cobranza_usuarios.go` (archivo nuevo, completo):

```go
//nolint:misspell // rutas vocabulary is Spanish per project convention.
package app

import (
	"context"
	"time"

	"github.com/abdimuy/msp-api/internal/rutas"
	rutasdomain "github.com/abdimuy/msp-api/internal/rutas/domain"
)

// Compile-time check: *Service satisfies the cross-module contract.
var _ rutas.CobranzaUsuariosReader = (*Service)(nil)

// UsuariosCobradores implements rutas.CobranzaUsuariosReader. Unlike
// ListarReporteUsuarios it does NOT swallow a calendar failure: desempeno must
// answer 503 instead of inventing an empty fleet (spec §6).
func (s *Service) UsuariosCobradores(ctx context.Context) ([]rutas.UsuarioCobrador, error) {
	usuarios, err := s.calendario.ListarCobradores(ctx)
	if err != nil {
		return nil, rutas.ErrCalendarioNoDisponible.WithError(err)
	}
	zonas, err := s.repo.ListarRutas(ctx)
	if err != nil {
		return nil, err
	}
	nombrePorZona := make(map[int]string, len(zonas))
	for _, z := range zonas {
		nombrePorZona[z.ZonaID] = z.ZonaNombre
	}
	out := make([]rutas.UsuarioCobrador, 0, len(usuarios))
	for _, u := range usuarios {
		c := rutas.UsuarioCobrador{
			UID: u.UID, Nombre: u.Nombre, Email: u.Email,
			CobradorID: u.CobradorID, ZonaID: u.ZonaID, ZonaNombre: nombrePorZona[u.ZonaID],
		}
		if !u.FechaInicio.IsZero() {
			fi := u.FechaInicio
			c.InicioSemana = &fi
		}
		out = append(out, c)
	}
	return out, nil
}

// CobranzaDeUsuario implements rutas.CobranzaUsuariosReader with the same
// computation as DesglosePorUsuario (VentasPorZona + enrichVentas +
// CalcularResumenPonderado), over the user's own window.
func (s *Service) CobranzaDeUsuario(ctx context.Context, u rutas.UsuarioCobrador) (rutas.CobranzaUsuario, error) {
	out := rutas.CobranzaUsuario{Usuario: u, Ventas: []rutas.VentaCobro{}}
	if u.ZonaID <= 0 || u.InicioSemana == nil {
		return out, nil
	}
	now := time.Now().UTC()
	ventas, err := s.cobranza.VentasPorZona(ctx, u.ZonaID, *u.InicioSemana, now)
	if err != nil {
		return rutas.CobranzaUsuario{}, err
	}
	enrichVentas(ventas, *u.InicioSemana, now)
	for _, v := range ventas {
		out.Ventas = append(out.Ventas, rutas.VentaCobro{
			VentaID: v.VentaID, ClienteID: v.ClienteID,
			Parcialidad: v.Parcialidad, AbonoSemana: v.AbonoSemana, Saldo: v.Saldo,
			Vencidas: v.Vencidas, Aporte: v.Aporte, AplicaPonderado: v.AplicaPonderado,
		})
	}
	out.PctPonderado = rutasdomain.CalcularResumenPonderado(ventas).Pct
	return out, nil
}
```


- [ ] **Step 4: Correr y ver que pasa**

Run: `go test ./internal/rutas/... && golangci-lint run ./internal/rutas/...`
Expected: PASS; `0 issues.`

- [ ] **Step 5: Control positivo**

En `UsuariosCobradores`, devolver `nil, err` en lugar de `nil, rutas.ErrCalendarioNoDisponible.WithError(err)` → falla `TestService_UsuariosCobradores_FirestoreCaidoEsErrorTipado`. Restaurar.

- [ ] **Step 6: Commit**

```bash
git add internal/rutas
git commit -m "feat(rutas): contrato de cobranza por usuario para otros módulos"
```

### Tarea B2: adaptador de usuarios de desempeño sobre el contrato de rutas

**Files:**
- Create: `internal/desempeno/infra/clients/rutas_usuarios_client.go`
- Test: `internal/desempeno/infra/clients/rutas_usuarios_client_test.go`

**Interfaces:**
- Consumes: B1 (`rutas.CobranzaUsuariosReader`, `rutas.ErrCalendarioNoDisponible`), Tarea 0 (`outbound.UsuariosCobranza`, `domain.ErrFirestoreNoDisponible`).
- Produces: `desempenoclients.NewRutasUsuariosClient(rutas.CobranzaUsuariosReader) outbound.UsuariosCobranza` (lo usa F en el wiring).

- [ ] **Step 1: Escribir la prueba que falla**

`internal/desempeno/infra/clients/rutas_usuarios_client_test.go` (archivo nuevo, completo):

```go
package clients_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	desempenoclients "github.com/abdimuy/msp-api/internal/desempeno/infra/clients"
	"github.com/abdimuy/msp-api/internal/rutas"
)

type fakeRutas struct {
	usuarios []rutas.UsuarioCobrador
	cobranza rutas.CobranzaUsuario
	errUs    error
	errCob   error
	pedido   rutas.UsuarioCobrador
}

func (f *fakeRutas) UsuariosCobradores(context.Context) ([]rutas.UsuarioCobrador, error) {
	return f.usuarios, f.errUs
}

func (f *fakeRutas) CobranzaDeUsuario(_ context.Context, u rutas.UsuarioCobrador) (rutas.CobranzaUsuario, error) {
	f.pedido = u
	return f.cobranza, f.errCob
}

func TestRutasUsuariosClient_ListarUsuarios(t *testing.T) {
	t.Parallel()
	inicio := time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC)
	f := &fakeRutas{usuarios: []rutas.UsuarioCobrador{{
		UID: "uid-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
		CobradorID: 11502, ZonaID: 21563, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
	}}}
	got, err := desempenoclients.NewRutasUsuariosClient(f).ListarUsuarios(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []domain.UsuarioCobranza{{
		UID: "uid-juan", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
		CobradorID: 11502, ZonaID: 21563, ZonaNombre: "RUTA 21", InicioSemana: &inicio,
	}}, got)
}

func TestRutasUsuariosClient_FirestoreCaidoEs503(t *testing.T) {
	t.Parallel()
	f := &fakeRutas{errUs: rutas.ErrCalendarioNoDisponible.WithError(errors.New("deadline"))}
	_, err := desempenoclients.NewRutasUsuariosClient(f).ListarUsuarios(context.Background())
	require.ErrorIs(t, err, domain.ErrFirestoreNoDisponible)

	otro := errors.New("firebird caído")
	_, err = desempenoclients.NewRutasUsuariosClient(&fakeRutas{errUs: otro}).ListarUsuarios(context.Background())
	require.ErrorIs(t, err, otro)
	require.NotErrorIs(t, err, domain.ErrFirestoreNoDisponible, "un error de Firebird no se disfraza de Firestore")
}

func TestRutasUsuariosClient_CobroSemana(t *testing.T) {
	t.Parallel()
	pct := decimal.RequireFromString("65.9")
	f := &fakeRutas{cobranza: rutas.CobranzaUsuario{
		PctPonderado: &pct,
		Ventas: []rutas.VentaCobro{{
			VentaID: 1, Parcialidad: decimal.NewFromInt(150), AbonoSemana: decimal.NewFromInt(150),
			Vencidas: decimal.NewFromInt(2), AplicaPonderado: true,
		}},
	}}
	u := domain.UsuarioCobranza{UID: "uid-juan", CobradorID: 11502, ZonaID: 21563}
	got, err := desempenoclients.NewRutasUsuariosClient(f).CobroSemana(context.Background(), u)
	require.NoError(t, err)
	assert.Equal(t, "uid-juan", f.pedido.UID)
	assert.Equal(t, 21563, f.pedido.ZonaID)
	require.Len(t, got.Ventas, 1)
	assert.True(t, got.Ventas[0].Vencidas.Equal(decimal.NewFromInt(2)))
	assert.Equal(t, &pct, got.PctPonderado)

	boom := errors.New("boom")
	_, err = desempenoclients.NewRutasUsuariosClient(&fakeRutas{errCob: boom}).CobroSemana(context.Background(), u)
	require.ErrorIs(t, err, boom)
}
```


- [ ] **Step 2: Correr y ver que falla**

Run: `go test ./internal/desempeno/infra/clients/`
Expected: FAIL de compilación (`undefined: desempenoclients.NewRutasUsuariosClient`).

- [ ] **Step 3: Implementación**

`internal/desempeno/infra/clients/rutas_usuarios_client.go` (archivo nuevo, completo):

```go
// Package clients tiene los adaptadores del módulo desempeno hacia otros
// módulos, solo a través de sus contratos (CLAUDE.md §2).
//
//nolint:misspell // vocabulario español por convención del proyecto.
package clients

import (
	"context"
	"errors"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/rutas"
)

// rutasUsuariosClient adapta rutas.CobranzaUsuariosReader (lo satisface el
// app.Service de rutas) al puerto outbound.UsuariosCobranza.
type rutasUsuariosClient struct {
	rutas rutas.CobranzaUsuariosReader
}

// NewRutasUsuariosClient envuelve el lector de rutas.
func NewRutasUsuariosClient(r rutas.CobranzaUsuariosReader) outbound.UsuariosCobranza {
	return &rutasUsuariosClient{rutas: r}
}

var _ outbound.UsuariosCobranza = (*rutasUsuariosClient)(nil)

// ListarUsuarios traduce la falla de Firestore al 503 del contrato.
func (c *rutasUsuariosClient) ListarUsuarios(ctx context.Context) ([]domain.UsuarioCobranza, error) {
	us, err := c.rutas.UsuariosCobradores(ctx)
	if err != nil {
		if errors.Is(err, rutas.ErrCalendarioNoDisponible) {
			return nil, domain.ErrFirestoreNoDisponible.WithError(err)
		}
		return nil, err
	}
	out := make([]domain.UsuarioCobranza, 0, len(us))
	for _, u := range us {
		out = append(out, aDominio(u))
	}
	return out, nil
}

// CobroSemana pide a rutas el desglose de la semana del usuario.
func (c *rutasUsuariosClient) CobroSemana(ctx context.Context, u domain.UsuarioCobranza) (domain.CobroSemana, error) {
	cu, err := c.rutas.CobranzaDeUsuario(ctx, rutas.UsuarioCobrador{
		UID: u.UID, Nombre: u.Nombre, Email: u.Email, CobradorID: u.CobradorID,
		ZonaID: u.ZonaID, ZonaNombre: u.ZonaNombre, InicioSemana: u.InicioSemana,
	})
	if err != nil {
		return domain.CobroSemana{}, err
	}
	out := domain.CobroSemana{PctPonderado: cu.PctPonderado, Ventas: make([]domain.CobroVenta, 0, len(cu.Ventas))}
	for _, v := range cu.Ventas {
		out.Ventas = append(out.Ventas, domain.CobroVenta{
			VentaID: v.VentaID, Parcialidad: v.Parcialidad, AbonoSemana: v.AbonoSemana,
			Vencidas: v.Vencidas, AplicaPonderado: v.AplicaPonderado,
		})
	}
	return out, nil
}

func aDominio(u rutas.UsuarioCobrador) domain.UsuarioCobranza {
	return domain.UsuarioCobranza{
		UID: u.UID, Nombre: u.Nombre, Email: u.Email, CobradorID: u.CobradorID,
		ZonaID: u.ZonaID, ZonaNombre: u.ZonaNombre, InicioSemana: u.InicioSemana,
	}
}
```


- [ ] **Step 4: Correr y ver que pasa**

Run: `go test -cover ./internal/desempeno/infra/clients/`
Expected: `ok … coverage: 100.0%`.

- [ ] **Step 5: Commit**

```bash
git add internal/desempeno/infra/clients
git commit -m "feat(desempeno): usuarios y cobranza semanal vía el contrato de rutas"
```

### Tarea B3: repositorio de actividades (pagos aplicados, pendientes y visitas)

**Files:**
- Create: `internal/desempeno/infra/desempenofb/actividades_repo.go`
- Modify: `internal/platform/fbcharset/fbcharset.go` (registrar las columnas NONE nuevas)
- Test: `internal/desempeno/infra/desempenofb/actividades_seed_test.go`, `actividades_repo_test.go`

**Interfaces:**
- Consumes: Tarea 0 (`outbound.ActividadesRepo`, `domain.Actividad`, `IDPagoAplicado`, `IDPagoPendiente`, `IDVisita`, `ClasificarTipoPago`, `ConceptoCobranzaEnRuta`, `ConceptoCondonaciones`, `FormaCobro*`).
- Produces: `desempenofb.NewActividadesRepo(*firebird.Pool) *ActividadesRepo` (F); sembradores de prueba `usuarioSembrado`, `impteDe`, `sembrarPagoRecibido`, `sembrarImagenPago`, `sembrarVisita`, `sembrarImagenVisita`, `segundaZona` (B4 usa `segundaZona`).

Charsets medidos en dev (`RDB$RELATION_FIELDS × RDB$FIELDS`, 2026-10-14): `DOCTOS_CC.LAT/LON` NONE, `DOCTOS_PV.FOLIO` NONE, `MSP_PAGOS_RECIBIDOS.LAT/LON` NONE, `MSP_PAGOS_RECIBIDOS.ID` NONE (UUID ASCII), `MSP_PAGOS_RECIBIDOS.COBRADOR` UTF8, `MSP_USUARIOS.FIREBASE_UID` ASCII, `CLIENTES.NOMBRE` ISO8859_1, `MSP_VISITAS.{ID,COBRADOR,TIPO_VISITA,NOTA}` NONE con bytes UTF-8 (se leen en plano, como `visitasfb`). La prueba de fuentes de `fbcharset` exige anotar cada destino `firebird.Win1252` con `TABLA.COLUMNA` en su línea y registrarlo.

- [ ] **Step 1: Escribir las pruebas que fallan**

`internal/desempeno/infra/desempenofb/actividades_seed_test.go` (archivo nuevo, completo):

```go
package desempenofb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// Sembradores propios de este paquete. Todo corre dentro de
// fbtestutil.WithTestTransaction, que siempre revierte.

// usuarioSembrado inserta un MSP_USUARIOS autorreferenciado (CREATED_BY es FK
// a sí misma) y devuelve su ID y su FIREBASE_UID.
func usuarioSembrado(ctx context.Context, t *testing.T, q firebird.Querier) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	uid := "fb-desempeno-" + id.String()[:8]
	now := firebird.ToWallClock(time.Now().UTC())
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_USUARIOS
		 (ID, FIREBASE_UID, EMAIL, NOMBRE, ACTIVO, ESTATUS, CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, ?, 'Juan Carlos Méndez', TRUE, 'FIREBASE_USER', ?, ?, ?, ?)`,
		id.String(), uid, "juan."+id.String()[:8]+"@muebleriamsp.mx", now, now, id.String(), id.String())
	require.NoError(t, err, "sembrar MSP_USUARIOS")
	return id, uid
}

// impteDe devuelve el IMPTE_DOCTO_CC_ID del abono sembrado por microsipseed.
func impteDe(ctx context.Context, t *testing.T, q firebird.Querier, doctoCCID int) int {
	t.Helper()
	var id int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT IMPTE_DOCTO_CC_ID FROM IMPORTES_DOCTOS_CC WHERE DOCTO_CC_ID = ?`, doctoCCID).Scan(&id))
	return id
}

type pagoRecibidoSembrado struct {
	id           uuid.UUID
	impteID      int // 0 = pendiente (sin Microsip)
	doctoCCID    int
	cargoID      int
	clienteID    int
	cobradorID   int
	importe      decimal.Decimal
	formaCobroID int
	conceptoID   int
	fecha        time.Time
	recibido     time.Time
	estado       string
	intentos     int
	createdBy    uuid.UUID
}

func sembrarPagoRecibido(ctx context.Context, t *testing.T, q firebird.Querier, p pagoRecibidoSembrado) {
	t.Helper()
	var impte, docto any
	if p.impteID != 0 {
		impte, docto = p.impteID, p.doctoCCID
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_RECIBIDOS
		 (ID, FECHA, IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, CARGO_DOCTO_CC_ID, CLIENTE_ID, COBRADOR_ID, COBRADOR,
		  IMPORTE, FORMA_COBRO_ID, CONCEPTO_CC_ID, LAT, LON, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT, CREATED_BY)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 'JUAN CARLOS MENDEZ', ?, ?, ?, '18.4617', '-97.3928', ?, ?, ?, ?, ?)`,
		p.id.String(), firebird.ToWallClock(p.fecha), impte, docto, p.cargoID, p.clienteID, p.cobradorID,
		p.importe, p.formaCobroID, p.conceptoID, p.estado, p.intentos,
		firebird.ToWallClock(p.recibido), firebird.ToWallClock(p.recibido), p.createdBy.String())
	require.NoError(t, err, "sembrar MSP_PAGOS_RECIBIDOS")
}

func sembrarImagenPago(ctx context.Context, t *testing.T, q firebird.Querier, pagoID uuid.UUID) {
	t.Helper()
	now := firebird.ToWallClock(time.Now().UTC())
	id := uuid.New()
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_PAGOS_IMAGENES (ID, PAGO_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
		  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 1024, ?, ?, ?, ?)`,
		id.String(), pagoID.String(), "pagos/"+pagoID.String()+"/"+id.String()+".jpg", now, now, pagoID.String(), pagoID.String())
	require.NoError(t, err, "sembrar MSP_PAGOS_IMAGENES")
}

type visitaSembrada struct {
	id         uuid.UUID
	clienteID  int
	zonaID     int
	ventaID    int // 0 = sin venta
	cobradorID int
	tipo       string
	fecha      time.Time
	lat, lng   float64
}

func sembrarVisita(ctx context.Context, t *testing.T, q firebird.Querier, v visitaSembrada) {
	t.Helper()
	var venta any
	if v.ventaID != 0 {
		venta = v.ventaID
	}
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_VISITAS (ID, COBRADOR, COBRADOR_ID, FECHA, FORMA_COBRO_ID, LAT, LNG, NOTA,
		  TIPO_VISITA, ZONA_CLIENTE_ID, CLIENTE_ID, IMPTE_DOCTO_CC_ID)
		 VALUES (?, 'JUAN CARLOS MÉNDEZ', ?, ?, 0, ?, ?, 'Vecina dice que regresa el lunes', ?, ?, ?, ?)`,
		v.id.String(), v.cobradorID, firebird.ToWallClock(v.fecha), v.lat, v.lng, v.tipo, v.zonaID, v.clienteID, venta)
	require.NoError(t, err, "sembrar MSP_VISITAS")
}

func sembrarImagenVisita(ctx context.Context, t *testing.T, q firebird.Querier, visitaID uuid.UUID) uuid.UUID {
	t.Helper()
	now := firebird.ToWallClock(time.Now().UTC())
	id := uuid.New()
	_, err := q.ExecContext(ctx,
		`INSERT INTO MSP_VISITAS_IMAGENES (ID, VISITA_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
		  DESCRIPCION, CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
		 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 2048, 'fachada', ?, ?, ?, ?)`,
		id.String(), visitaID.String(), "visitas/"+visitaID.String()+"/"+id.String()+".jpg", now, now,
		visitaID.String(), visitaID.String())
	require.NoError(t, err, "sembrar MSP_VISITAS_IMAGENES")
	return id
}

// segundaZona devuelve una zona del catálogo distinta de la primera.
func segundaZona(ctx context.Context, t *testing.T, q firebird.Querier, distintaDe int) int {
	t.Helper()
	var id int
	require.NoError(t, q.QueryRowContext(ctx,
		`SELECT FIRST 1 ZONA_CLIENTE_ID FROM ZONAS_CLIENTES WHERE ZONA_CLIENTE_ID <> ? ORDER BY ZONA_CLIENTE_ID`,
		distintaDe).Scan(&id), "el catálogo ZONAS_CLIENTES necesita al menos dos zonas")
	return id
}
```

`internal/desempeno/infra/desempenofb/actividades_repo_test.go` (archivo nuevo, completo):

```go
//nolint:paralleltest // serial: cada prueba comparte una transacción que se revierte.
package desempenofb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// Fecha de los abonos sembrados. DOCTOS_CC.FECHA es DATE: el abono de oficina
// queda a las 00:00 de CDMX (06:00Z).
var diaAbono = time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)

func ventanaDelDia() (time.Time, time.Time) {
	return time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC), time.Date(2024, 3, 21, 12, 0, 0, 0, time.UTC)
}

func delCliente(acts []domain.Actividad, clienteID int) []domain.Actividad {
	var out []domain.Actividad
	for _, a := range acts {
		if a.ClienteID == clienteID {
			out = append(out, a)
		}
	}
	return out
}

func TestActividadesRepo_PagoDeOficina(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "JUANA ANDRADE NÚÑEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(150), Fecha: diaAbono,
			ConCobrador: true, ConFormaCobro: true, FormaCobroID: domain.FormaCobroEfectivo,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1)
		a := mias[0]
		assert.Equal(t, domain.IDPagoAplicado(impteDe(ctx, t, q, abono.DoctoCCID)), a.ID)
		assert.Equal(t, domain.TipoPago, a.Tipo)
		assert.Equal(t, "JUANA ANDRADE NÚÑEZ", a.ClienteNombre, "ISO8859_1 transliterado, sin doble decode")
		assert.Equal(t, venta.CargoCCID, a.VentaID)
		assert.Equal(t, venta.Folio, a.Folio)
		assert.Equal(t, venta.DoctoPVID, a.DoctoPVID, "contrato v2 docto_pv_id")
		assert.Equal(t, domain.OrigenOficina, a.RegistradoPor.Origen)
		assert.Equal(t, abono.CobradorID, a.RegistradoPor.CobradorID)
		assert.Nil(t, a.RecibidoAt)
		require.NotNil(t, a.Pago)
		assert.True(t, a.Pago.Importe.Equal(decimal.NewFromInt(150)))
		assert.Equal(t, domain.FormaCobroEfectivo, a.Pago.FormaCobroID)
		assert.True(t, a.Pago.Aplicado)
		assert.False(t, a.Pago.Rechazado)
		assert.Equal(t, abono.DoctoCCID, a.Pago.DoctoCCID)
		assert.Empty(t, a.Pago.PagoRecibidoID)
		assert.Equal(t, 0, a.NumFotos)
		assert.Equal(t, time.Date(2024, 3, 20, 6, 0, 0, 0, time.UTC), a.Hora, "medianoche de CDMX en UTC")
	})
}

func TestActividadesRepo_PagoDeLaAppConFotos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ROSARIO VILLA LÓPEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		abono := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: decimal.NewFromInt(200), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroTransferencia,
		})
		usuarioID, uid := usuarioSembrado(ctx, t, q)
		pagoID := uuid.New()
		capturado := time.Date(2024, 3, 20, 16, 31, 0, 0, time.UTC)
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: pagoID, impteID: impteDe(ctx, t, q, abono.DoctoCCID), doctoCCID: abono.DoctoCCID,
			cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(200), formaCobroID: domain.FormaCobroTransferencia,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado.Add(3 * time.Hour),
			estado: "A", createdBy: usuarioID,
		})
		sembrarImagenPago(ctx, t, q, pagoID)
		sembrarImagenPago(ctx, t, q, pagoID)

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "sin fan-out por el JOIN a MSP_PAGOS_RECIBIDOS ni por las fotos")
		a := mias[0]
		assert.Equal(t, domain.OrigenApp, a.RegistradoPor.Origen)
		assert.Equal(t, uid, a.RegistradoPor.UsuarioUID, "CREATED_BY → MSP_USUARIOS.FIREBASE_UID")
		assert.Equal(t, 11502, a.RegistradoPor.CobradorID)
		assert.Equal(t, "JUAN CARLOS MENDEZ", a.RegistradoPor.Nombre)
		require.NotNil(t, a.RecibidoAt)
		assert.Equal(t, capturado.Add(3*time.Hour), *a.RecibidoAt)
		assert.True(t, a.CapturaTardia(), "3 h entre captura y llegada")
		assert.Equal(t, capturado, a.Hora, "la hora real del cobro vive en MSP_PAGOS_RECIBIDOS (COBRANZA-SYNC.md §7.5)")
		assert.Equal(t, pagoID.String(), a.Pago.PagoRecibidoID)
		assert.Equal(t, 2, a.NumFotos)
		assert.Equal(t, domain.FormaCobroTransferencia, a.Pago.FormaCobroID)
	})
}

func TestActividadesRepo_CondonacionYConceptosExcluidos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		const cobroEnMostrador = 155
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCondonaciones, cobroEnMostrador)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "MARTHA ELENA RUIZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCondonaciones, Importe: decimal.NewFromInt(50), Fecha: diaAbono,
			ConFormaCobro: true, FormaCobroID: domain.FormaCobroCondonacion,
		})
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: cobroEnMostrador, Importe: decimal.NewFromInt(300), Fecha: diaAbono,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1, "el cobro en mostrador (155) no es actividad de ruta")
		assert.Equal(t, domain.TipoCondonacion, mias[0].Tipo)
	})
}

func TestActividadesRepo_PagoPendienteRechazado(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "PEDRO SALINAS GÓMEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		usuarioID, uid := usuarioSembrado(ctx, t, q)
		pagoID := uuid.New()
		capturado := time.Date(2024, 3, 20, 18, 0, 0, 0, time.UTC)
		sembrarPagoRecibido(ctx, t, q, pagoRecibidoSembrado{
			id: pagoID, cargoID: venta.CargoCCID, clienteID: cliente, cobradorID: 11502,
			importe: decimal.NewFromInt(80), formaCobroID: domain.FormaCobroEfectivo,
			conceptoID: domain.ConceptoCobranzaEnRuta, fecha: capturado, recibido: capturado,
			estado: "P", intentos: 2, createdBy: usuarioID,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 1)
		a := mias[0]
		assert.Equal(t, domain.IDPagoPendiente(pagoID.String()), a.ID)
		assert.Equal(t, venta.CargoCCID, a.VentaID)
		assert.Equal(t, venta.DoctoPVID, a.DoctoPVID)
		assert.Equal(t, uid, a.RegistradoPor.UsuarioUID)
		assert.False(t, a.Pago.Aplicado)
		assert.True(t, a.Pago.Rechazado, "INTENTOS > 0")
		assert.Equal(t, capturado, a.Hora)
		assert.InDelta(t, 18.4617, *a.Lat, 1e-9)
	})
}

func TestActividadesRepo_VisitaConFotoYAislamientoPorZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zonaA := microsipseed.PrimeraZona(t, q)
		zonaB := segundaZona(ctx, t, q, zonaA)
		clienteA := microsipseed.ClienteEnZona(t, q, "LUCÍA FERNÁNDEZ ORTIZ", zonaA)
		clienteB := microsipseed.ClienteEnZona(t, q, "ANA MARÍA CRUZ", zonaB)
		venta := microsipseed.VentaCredito(t, q, clienteA, microsipseed.OpcionesVenta{})
		visita := uuid.New()
		cuando := time.Date(2024, 3, 20, 17, 0, 0, 0, time.UTC)
		sembrarVisita(ctx, t, q, visitaSembrada{
			id: visita, clienteID: clienteA, zonaID: zonaA, ventaID: venta.CargoCCID, cobradorID: 11502,
			tipo: "No se encontraba", fecha: cuando, lat: 18.4617, lng: -97.3928,
		})
		sembrarImagenVisita(ctx, t, q, visita)
		sembrarVisita(ctx, t, q, visitaSembrada{
			id: uuid.New(), clienteID: clienteB, zonaID: zonaB, cobradorID: 11502,
			tipo: "No se encontraba", fecha: cuando,
		})

		desde, hasta := ventanaDelDia()
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zonaA, desde, hasta)
		require.NoError(t, err)
		assert.Empty(t, delCliente(acts, clienteB), "la visita de la zona B no aparece en la zona A")
		mias := delCliente(acts, clienteA)
		require.Len(t, mias, 1)
		a := mias[0]
		assert.Equal(t, domain.IDVisita(visita.String()), a.ID)
		assert.Equal(t, domain.TipoVisita, a.Tipo)
		assert.Equal(t, venta.CargoCCID, a.VentaID)
		assert.Equal(t, venta.Folio, a.Folio)
		assert.Equal(t, venta.DoctoPVID, a.DoctoPVID)
		assert.Equal(t, 1, a.NumFotos)
		assert.Equal(t, "JUAN CARLOS MÉNDEZ", a.RegistradoPor.Nombre, "NONE con bytes UTF-8, sin Win1252")
		assert.Equal(t, 11502, a.RegistradoPor.CobradorID)
		assert.Empty(t, a.RegistradoPor.UsuarioUID)
		require.NotNil(t, a.Visita)
		assert.Equal(t, "Vecina dice que regresa el lunes", a.Visita.Nota)
		assert.Equal(t, cuando, a.Hora)
		assert.True(t, a.ConGPS())
	})
}

func TestActividadesRepo_RangoInclusivo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "SILVIA CARRANZA MEÑACA", zona)
		desde := time.Date(2024, 3, 20, 14, 0, 0, 0, time.UTC)
		hasta := time.Date(2024, 3, 20, 15, 0, 0, 0, time.UTC)
		for _, h := range []time.Time{desde.Add(-time.Second), desde, hasta, hasta.Add(time.Second)} {
			sembrarVisita(ctx, t, q, visitaSembrada{
				id: uuid.New(), clienteID: cliente, zonaID: zona, cobradorID: 11502,
				tipo: "Casa cerrada con candado", fecha: h,
			})
		}
		acts, err := desempenofb.NewActividadesRepo(pool).ActividadesZona(ctx, zona, desde, hasta)
		require.NoError(t, err)
		mias := delCliente(acts, cliente)
		require.Len(t, mias, 2, "entran los dos bordes y nada más")
		for _, a := range mias {
			assert.False(t, a.ConGPS(), "0,0 es el centinela de sin GPS")
		}
	})
}
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test -count=1 -p 1 ./internal/desempeno/infra/desempenofb/`
Expected: FAIL de compilación (`undefined: desempenofb.NewActividadesRepo`).

- [ ] **Step 3: Implementación**

`internal/desempeno/infra/desempenofb/actividades_repo.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.ActividadesRepo = (*ActividadesRepo)(nil)

// ActividadesRepo lee pagos aplicados (MSP_PAGOS_VENTAS), pagos pendientes
// (MSP_PAGOS_RECIBIDOS en 'P') y visitas (MSP_VISITAS) de una zona.
type ActividadesRepo struct {
	pool *firebird.Pool
}

// NewActividadesRepo arma el repositorio.
func NewActividadesRepo(pool *firebird.Pool) *ActividadesRepo { return &ActividadesRepo{pool: pool} }

// doctoPVDe es el DOCTO_PV_ID de la venta (contrato v2 `docto_pv_id`).
func doctoPVDe(columnaCargo string) string {
	return `(SELECT des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE = 'PV' AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID = ` + columnaCargo + ` ROWS 1)`
}

// folioVentaDe es el folio de punto de venta del cargo, por el puente
// DOCTOS_ENTRE_SIS (mismo patrón que rutas). Subconsultas escalares con
// ROWS 1 y SIN parámetros: firebirdsql v0.9.19 no enlaza ? dentro de tablas
// derivadas. DOCTOS_PV.FOLIO es CHARACTER SET NONE → firebird.Win1252.
func folioVentaDe(columnaCargo string) string {
	return `(SELECT pv.FOLIO FROM DOCTOS_PV pv WHERE pv.DOCTO_PV_ID = (
      SELECT des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE = 'PV' AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID = ` + columnaCargo + ` ROWS 1) ROWS 1)`
}

// queryPagosAplicados: pagos y condonaciones en Microsip. Conceptos 87327
// (Cobranza en ruta) y 27969 (Condonaciones): los que el teléfono ve
// (pagoConceptoFilter), declarados aquí por separado a propósito.
//
// Sin fan-out: la forma de cobro y el pago de la app se resuelven con
// subconsultas escalares MIN(...) (FORMAS_COBRO_DOCTOS y MSP_PAGOS_RECIBIDOS
// no garantizan 1:1). Charsets: CLIENTES.NOMBRE ISO8859_1 (LEFT JOIN →
// sql.NullString); DOCTOS_CC.LAT/LON NONE (Win1252); MSP_PAGOS_RECIBIDOS.ID
// NONE con UUID ASCII y COBRADOR UTF8 (sql.NullString); MSP_USUARIOS.
// FIREBASE_UID ASCII. Usa IDX_MSP_PAGOS_ZONA_FECHA (ZONA_CLIENTE_ID, FECHA).
var queryPagosAplicados = `
SELECT
  p.IMPTE_DOCTO_CC_ID,
  p.DOCTO_CC_ID,
  p.DOCTO_CC_ACR_ID,
  p.CLIENTE_ID,
  c.NOMBRE,
  p.CONCEPTO_CC_ID,
  p.FECHA,
  CAST(p.IMPORTE + p.IMPUESTO AS NUMERIC(18,2)),
  p.APLICADO,
  dc.LAT,
  dc.LON,
  dc.COBRADOR_ID,
  (SELECT MIN(fcd.FORMA_COBRO_ID) FROM FORMAS_COBRO_DOCTOS fcd
    WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC' AND fcd.DOCTO_ID = p.DOCTO_CC_ID),
  ` + folioVentaDe("p.DOCTO_CC_ACR_ID") + `,
  ` + doctoPVDe("p.DOCTO_CC_ACR_ID") + `,
  pr.ID,
  pr.COBRADOR_ID,
  pr.COBRADOR,
  pr.FORMA_COBRO_ID,
  pr.LAT,
  pr.LON,
  pr.RECEIVED_AT,
  u.FIREBASE_UID,
  (SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES im WHERE im.PAGO_ID = pr.ID)
FROM MSP_PAGOS_VENTAS p
LEFT JOIN DOCTOS_CC dc ON dc.DOCTO_CC_ID = p.DOCTO_CC_ID
LEFT JOIN CLIENTES c   ON c.CLIENTE_ID  = p.CLIENTE_ID
LEFT JOIN MSP_PAGOS_RECIBIDOS pr ON pr.ID = (
  SELECT MIN(pr2.ID) FROM MSP_PAGOS_RECIBIDOS pr2 WHERE pr2.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID)
LEFT JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE p.ZONA_CLIENTE_ID = ?
  AND p.FECHA >= ?
  AND p.FECHA <= ?
  AND p.CANCELADO = 'N'
  AND p.CONCEPTO_CC_ID IN (87327, 27969)`

// queryPagosPendientes: capturas de la app que todavía no llegan a Microsip
// (ESTADO 'P'). La zona sale del cargo (MSP_SALDOS_VENTAS), porque
// MSP_PAGOS_RECIBIDOS no la guarda. INTENTOS > 0 = Microsip ya lo rechazó.
var queryPagosPendientes = `
SELECT
  pr.ID,
  pr.CARGO_DOCTO_CC_ID,
  pr.CLIENTE_ID,
  c.NOMBRE,
  pr.CONCEPTO_CC_ID,
  pr.FECHA,
  CAST(pr.IMPORTE AS NUMERIC(18,2)),
  pr.FORMA_COBRO_ID,
  pr.LAT,
  pr.LON,
  pr.COBRADOR_ID,
  pr.COBRADOR,
  pr.INTENTOS,
  pr.RECEIVED_AT,
  u.FIREBASE_UID,
  (SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES im WHERE im.PAGO_ID = pr.ID),
  ` + folioVentaDe("pr.CARGO_DOCTO_CC_ID") + `,
  ` + doctoPVDe("pr.CARGO_DOCTO_CC_ID") + `
FROM MSP_PAGOS_RECIBIDOS pr
JOIN MSP_SALDOS_VENTAS s ON s.DOCTO_CC_ID = pr.CARGO_DOCTO_CC_ID
LEFT JOIN CLIENTES c     ON c.CLIENTE_ID  = pr.CLIENTE_ID
LEFT JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
WHERE pr.ESTADO = 'P'
  AND s.ZONA_CLIENTE_ID = ?
  AND pr.FECHA >= ?
  AND pr.FECHA <= ?`

// queryVisitas: MSP_VISITAS es legacy (Node). Sus columnas de texto son
// CHARACTER SET NONE con bytes UTF-8: se escanean como string plano, nunca
// Win1252 (visitasfb/repo.go). IMPTE_DOCTO_CC_ID es la VENTA (cargo) a la que
// se ligó la visita, como en el teléfono (DerivacionModelos.kt): el nombre
// miente y C0 lo mide (consulta 5).
var queryVisitas = `
SELECT
  v.ID,
  v.CLIENTE_ID,
  c.NOMBRE,
  v.FECHA,
  v.TIPO_VISITA,
  v.NOTA,
  v.LAT,
  v.LNG,
  v.COBRADOR_ID,
  v.COBRADOR,
  v.IMPTE_DOCTO_CC_ID,
  (SELECT COUNT(*) FROM MSP_VISITAS_IMAGENES im WHERE im.VISITA_ID = v.ID),
  ` + folioVentaDe("v.IMPTE_DOCTO_CC_ID") + `,
  ` + doctoPVDe("v.IMPTE_DOCTO_CC_ID") + `
FROM MSP_VISITAS v
LEFT JOIN CLIENTES c ON c.CLIENTE_ID = v.CLIENTE_ID
WHERE v.ZONA_CLIENTE_ID = ?
  AND v.FECHA >= ?
  AND v.FECHA <= ?`

// ActividadesZona implementa outbound.ActividadesRepo. Las tres lecturas van
// en una transacción de solo lectura (una misma foto de la base).
func (r *ActividadesRepo) ActividadesZona(ctx context.Context, zonaID int, desde, hasta time.Time) ([]domain.Actividad, error) {
	var out []domain.Actividad
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		args := []any{zonaID, firebird.ToWallClock(desde), firebird.ToWallClock(hasta)}
		for _, lectura := range []struct {
			sql  string
			scan func(*sql.Rows) (domain.Actividad, error)
		}{
			{queryPagosAplicados, scanPagoAplicado},
			{queryPagosPendientes, scanPagoPendiente},
			{queryVisitas, scanVisita},
		} {
			acts, err := leer(ctx, q, lectura.sql, args, lectura.scan)
			if err != nil {
				return err
			}
			out = append(out, acts...)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func leer(ctx context.Context, q firebird.Querier, query string, args []any,
	scan func(*sql.Rows) (domain.Actividad, error),
) ([]domain.Actividad, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, firebird.MapError(err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Actividad
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, firebird.MapError(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, firebird.MapError(err)
	}
	return out, nil
}

// coordenada lee un texto decimal ("18.4617"); vacío o ilegible = sin dato.
func coordenada(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func primeraCoordenada(a, b string) *float64 {
	if c := coordenada(a); c != nil {
		return c
	}
	return coordenada(b)
}

func instanteNulo(raw any) (*time.Time, error) {
	if raw == nil {
		return nil, nil //nolint:nilnil // NULL es "sin dato"
	}
	t, err := firebird.ScanUTCTime(raw)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func scanPagoAplicado(rows *sql.Rows) (domain.Actividad, error) {
	var (
		impteID, doctoCCID, cargoID, clienteID, conceptoID int
		clienteNombre                                      sql.NullString
		fechaRaw, importeRaw, recibidoRaw                  any
		aplicado                                           string
		dcLat                                              firebird.Win1252 // DOCTOS_CC.LAT — NONE
		dcLon                                              firebird.Win1252 // DOCTOS_CC.LON — NONE
		folio                                              firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
		prLat                                              firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LAT — NONE
		prLon                                              firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LON — NONE
		dcCobradorID, fcdForma, prCobradorID, prForma      sql.NullInt64
		doctoPV                                            sql.NullInt64
		prID, prCobrador, uid                              sql.NullString
		numFotos                                           int
	)
	if err := rows.Scan(&impteID, &doctoCCID, &cargoID, &clienteID, &clienteNombre, &conceptoID,
		&fechaRaw, &importeRaw, &aplicado, &dcLat, &dcLon, &dcCobradorID, &fcdForma, &folio,
		&doctoPV, &prID, &prCobradorID, &prCobrador, &prForma, &prLat, &prLon, &recibidoRaw, &uid, &numFotos); err != nil {
		return domain.Actividad{}, err
	}
	hora, err := firebird.ScanUTCTime(fechaRaw)
	if err != nil {
		return domain.Actividad{}, err
	}
	importe, err := firebird.ScanDecimal(importeRaw, 2)
	if err != nil {
		return domain.Actividad{}, err
	}
	forma := int(fcdForma.Int64)
	if !fcdForma.Valid {
		forma = int(prForma.Int64)
	}
	a := domain.Actividad{
		ID: domain.IDPagoAplicado(impteID), Tipo: domain.ClasificarTipoPago(forma, conceptoID),
		Hora: hora, ClienteID: clienteID, ClienteNombre: clienteNombre.String,
		VentaID: cargoID, DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: primeraCoordenada(string(dcLat), string(prLat)), Lng: primeraCoordenada(string(dcLon), string(prLon)),
		Pago: &domain.DatosPago{
			Importe: importe, FormaCobroID: forma, Aplicado: aplicado == "S", DoctoCCID: doctoCCID,
		},
		RegistradoPor: domain.RegistradoPor{CobradorID: int(dcCobradorID.Int64), Origen: domain.OrigenOficina},
	}
	if prID.Valid {
		a.Pago.PagoRecibidoID = strings.TrimSpace(prID.String)
		a.RegistradoPor = domain.RegistradoPor{
			UsuarioUID: uid.String, CobradorID: int(prCobradorID.Int64),
			Nombre: prCobrador.String, Origen: domain.OrigenApp,
		}
		if a.RecibidoAt, err = instanteNulo(recibidoRaw); err != nil {
			return domain.Actividad{}, err
		}
	}
	return a, nil
}

func scanPagoPendiente(rows *sql.Rows) (domain.Actividad, error) {
	var (
		id                                     string
		cargoID, clienteID                     int
		clienteNombre, cobrador, uid           sql.NullString
		conceptoID, forma, cobradorID, doctoPV sql.NullInt64
		fechaRaw, importeRaw, recibido         any
		lat                                    firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LAT — NONE
		lon                                    firebird.Win1252 // MSP_PAGOS_RECIBIDOS.LON — NONE
		folio                                  firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
		intentos, numFotos                     int
	)
	if err := rows.Scan(&id, &cargoID, &clienteID, &clienteNombre, &conceptoID, &fechaRaw, &importeRaw,
		&forma, &lat, &lon, &cobradorID, &cobrador, &intentos, &recibido, &uid, &numFotos, &folio, &doctoPV); err != nil {
		return domain.Actividad{}, err
	}
	hora, err := firebird.ScanUTCTime(fechaRaw)
	if err != nil {
		return domain.Actividad{}, err
	}
	importe, err := firebird.ScanDecimal(importeRaw, 2)
	if err != nil {
		return domain.Actividad{}, err
	}
	id = strings.TrimSpace(id)
	a := domain.Actividad{
		ID: domain.IDPagoPendiente(id), Tipo: domain.ClasificarTipoPago(int(forma.Int64), int(conceptoID.Int64)),
		Hora: hora, ClienteID: clienteID, ClienteNombre: clienteNombre.String,
		VentaID: cargoID, DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: coordenada(string(lat)), Lng: coordenada(string(lon)),
		RegistradoPor: domain.RegistradoPor{
			UsuarioUID: uid.String, CobradorID: int(cobradorID.Int64), Nombre: cobrador.String, Origen: domain.OrigenApp,
		},
		Pago: &domain.DatosPago{
			Importe: importe, FormaCobroID: int(forma.Int64), Aplicado: false,
			Rechazado: intentos > 0, PagoRecibidoID: id,
		},
	}
	if a.RecibidoAt, err = instanteNulo(recibido); err != nil {
		return domain.Actividad{}, err
	}
	return a, nil
}

func scanVisita(rows *sql.Rows) (domain.Actividad, error) {
	var (
		id, tipoVisita, cobrador string
		clienteID, cobradorID    int
		clienteNombre, nota      sql.NullString
		fechaRaw                 any
		lat, lng                 float64
		ventaID, doctoPV         sql.NullInt64
		numFotos                 int
		folio                    firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
	)
	if err := rows.Scan(&id, &clienteID, &clienteNombre, &fechaRaw, &tipoVisita, &nota, &lat, &lng,
		&cobradorID, &cobrador, &ventaID, &numFotos, &folio, &doctoPV); err != nil {
		return domain.Actividad{}, err
	}
	hora, err := firebird.ScanUTCTime(fechaRaw)
	if err != nil {
		return domain.Actividad{}, err
	}
	id = strings.TrimSpace(id)
	return domain.Actividad{
		ID: domain.IDVisita(id), Tipo: domain.TipoVisita, Hora: hora,
		ClienteID: clienteID, ClienteNombre: clienteNombre.String,
		VentaID: int(ventaID.Int64), DoctoPVID: int(doctoPV.Int64), Folio: strings.TrimSpace(string(folio)), NumFotos: numFotos,
		Lat: &lat, Lng: &lng,
		RegistradoPor: domain.RegistradoPor{CobradorID: cobradorID, Nombre: cobrador, Origen: domain.OrigenApp},
		Visita:        &domain.DatosVisita{VisitaID: id, TipoVisita: tipoVisita, Nota: nota.String},
	}, nil
}
```

`internal/platform/fbcharset/fbcharset.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/platform/fbcharset/fbcharset.go b/internal/platform/fbcharset/fbcharset.go
index d61f0a9..4f1266f 100644
--- a/internal/platform/fbcharset/fbcharset.go
+++ b/internal/platform/fbcharset/fbcharset.go
@@ -204,9 +204,29 @@ var registro = []Columna{
 	},
 	{
 		"DOCTOS_PV", "FOLIO", FamiliaNone,
-		[]string{"rutas/infra/rutasfb"},
+		[]string{"rutas/infra/rutasfb", "desempeno/infra/desempenofb"},
 		"CHAR(9) generado por el sistema; 0 filas con no-ASCII.",
 	},
+	{
+		"DOCTOS_CC", "LAT", FamiliaNone,
+		[]string{"desempeno/infra/desempenofb"},
+		"VARCHAR(100) con un decimal en texto (E-COB-018); se parsea a float.",
+	},
+	{
+		"DOCTOS_CC", "LON", FamiliaNone,
+		[]string{"desempeno/infra/desempenofb"},
+		"VARCHAR(100) con un decimal en texto (E-COB-018); se parsea a float.",
+	},
+	{
+		"MSP_PAGOS_RECIBIDOS", "LAT", FamiliaNone,
+		[]string{"desempeno/infra/desempenofb"},
+		"VARCHAR(20) legado (migración 000016); decimal en texto.",
+	},
+	{
+		"MSP_PAGOS_RECIBIDOS", "LON", FamiliaNone,
+		[]string{"desempeno/infra/desempenofb"},
+		"VARCHAR(20) legado (migración 000016); decimal en texto.",
+	},
 	{
 		"CAJAS", "NOMBRE", FamiliaNone,
 		[]string{"config/infra/configfb"},
```


- [ ] **Step 4: Correr y ver que pasan (integración real, todo revierte)**

Run: `go test -count=1 -p 1 ./internal/desempeno/infra/desempenofb/ ./internal/platform/fbcharset/`
Expected: `ok` en los dos (con `FB_DATABASE` cargado). Sin `FB_DATABASE` las de `desempenofb` se saltan.

- [ ] **Step 5: Control positivo**

En `queryPagosAplicados`, quitar `AND p.CONCEPTO_CC_ID IN (87327, 27969)` → falla `TestActividadesRepo_CondonacionYConceptosExcluidos` ("el cobro en mostrador (155) no es actividad de ruta"). En `scanPagoAplicado`, cambiar `if prID.Valid {` por `if false {` → falla `TestActividadesRepo_PagoDeLaAppConFotos`. Restaurar; PASS; diff limpio.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/... ./internal/platform/fbcharset/...
git add internal/desempeno/infra/desempenofb internal/platform/fbcharset/fbcharset.go
git commit -m "feat(desempeno): actividades de la zona desde Firebird con consultas propias"
```

### Tarea B4: repositorio del universo de ventas de la semana

**Files:**
- Create: `internal/desempeno/infra/desempenofb/cuentas_repo.go`
- Test: `internal/desempeno/infra/desempenofb/cuentas_repo_test.go`

**Interfaces:**
- Consumes: Tarea 0 (`outbound.CuentasRepo`, `domain.CuentaUniverso`), B3 (`segundaZona`, `requireFBEnv`).
- Produces: `desempenofb.NewCuentasRepo(*firebird.Pool) *CuentasRepo` (F).

- [ ] **Step 1: Escribir la prueba que falla**

`internal/desempeno/infra/desempenofb/cuentas_repo_test.go` (archivo nuevo, completo):

```go
//nolint:paralleltest // serial: cada prueba comparte una transacción que se revierte.
package desempenofb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

func cuentaDe(cs []domain.CuentaUniverso, ventaID int) (domain.CuentaUniverso, bool) {
	for _, c := range cs {
		if c.VentaID == ventaID {
			return c, true
		}
	}
	return domain.CuentaUniverso{}, false
}

func TestCuentasRepo_VentaConSaldo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "JUANA ANDRADE NÚÑEZ", zona)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{
			Total: decimal.NewFromInt(8800), ArticuloID: microsipseed.ArticuloConAcento(t, q),
		})

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zona, time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC))
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok, "la venta con saldo está en el universo")
		assert.Equal(t, cliente, c.ClienteID)
		assert.Equal(t, "JUANA ANDRADE NÚÑEZ", c.ClienteNombre)
		assert.Equal(t, venta.DoctoPVID, c.DoctoPVID)
		assert.Equal(t, venta.Folio, c.Folio)
		assert.Equal(t, venta.ArticuloNombre, c.Producto, "ISO8859_1 con Ñ, sin doble decode")
		assert.Equal(t, "733.00", c.Parcialidad.StringFixed(2), "8800 / 12 meses, entero (LIBRES_CARGOS_CC)")
		assert.True(t, c.Saldo.Equal(decimal.NewFromInt(8800)))
	})
}

func TestCuentasRepo_SaldadaEntraSoloSiPagoEnLaSemana(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, domain.ConceptoCobranzaEnRuta)
		zona := microsipseed.PrimeraZona(t, q)
		cliente := microsipseed.ClienteEnZona(t, q, "ROSARIO VILLA LÓPEZ", zona)
		total := decimal.NewFromInt(1200)
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{Total: total})
		pagoDia := time.Date(2024, 3, 20, 0, 0, 0, 0, time.UTC)
		microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: domain.ConceptoCobranzaEnRuta, Importe: total, Fecha: pagoDia,
		})
		repo := desempenofb.NewCuentasRepo(pool)

		cs, err := repo.UniversoSemana(ctx, zona, time.Date(2024, 3, 18, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		c, ok := cuentaDe(cs, venta.CargoCCID)
		require.True(t, ok, "saldada con pago dentro de la semana: sigue en el universo")
		assert.True(t, c.Saldo.IsZero())

		cs, err = repo.UniversoSemana(ctx, zona, time.Date(2024, 3, 25, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		_, ok = cuentaDe(cs, venta.CargoCCID)
		assert.False(t, ok, "saldada antes de la semana: fuera")
	})
}

func TestCuentasRepo_AislamientoPorZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		zonaA := microsipseed.PrimeraZona(t, q)
		zonaB := segundaZona(ctx, t, q, zonaA)
		ventaB := microsipseed.VentaCredito(t, q, microsipseed.ClienteEnZona(t, q, "ANA MARÍA CRUZ", zonaB), microsipseed.OpcionesVenta{})

		cs, err := desempenofb.NewCuentasRepo(pool).UniversoSemana(ctx, zonaA, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		_, ok := cuentaDe(cs, ventaB.CargoCCID)
		assert.False(t, ok)
	})
}
```


- [ ] **Step 2: Correr y ver que falla**

Run: `go test -count=1 -p 1 -run TestCuentasRepo ./internal/desempeno/infra/desempenofb/`
Expected: FAIL de compilación (`undefined: desempenofb.NewCuentasRepo`).

- [ ] **Step 3: Implementación**

`internal/desempeno/infra/desempenofb/cuentas_repo.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenofb

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

var _ outbound.CuentasRepo = (*CuentasRepo)(nil)

// CuentasRepo lee el universo de ventas de la semana de una zona.
type CuentasRepo struct {
	pool *firebird.Pool
}

// NewCuentasRepo arma el repositorio.
func NewCuentasRepo(pool *firebird.Pool) *CuentasRepo { return &CuentasRepo{pool: pool} }

// puentePV resuelve el DOCTO_PV_ID del cargo (sin parámetros: ver folioVentaDe).
const puentePV = `(SELECT des.DOCTO_FTE_ID FROM DOCTOS_ENTRE_SIS des
      WHERE des.CLAVE_SIS_FTE = 'PV' AND des.CLAVE_SIS_DEST = 'CC'
        AND des.DOCTO_DEST_ID = s.DOCTO_CC_ID ROWS 1)`

// queryUniverso: ventas a crédito (no CONTADO, como rutas) de la zona, no
// canceladas, con saldo > 0 o con FECHA_ULT_PAGO dentro de la semana: el mismo
// predicado de ventana que el sync del teléfono (COBRANZA-SYNC.md §5) con
// desde = inicio de semana. Usa IDX_MSP_SALDOS_ZONA_FUP.
//
// CAST a NUMERIC(18,2): firebirdsql v0.9.x devuelve sin escala si no.
// Charsets: CLIENTES.NOMBRE ISO8859_1 y ARTICULOS.NOMBRE ISO8859_1 →
// sql.NullString; DOCTOS_PV.FOLIO NONE → firebird.Win1252 (sin COALESCE con
// literal: revienta con acentos, ENCODING_HANDLING.md).
var queryUniverso = `
SELECT
  s.DOCTO_CC_ID,
  s.CLIENTE_ID,
  c.NOMBRE,
  CAST(COALESCE(l.PARCIALIDAD, 0) AS NUMERIC(18,2)),
  CAST(s.SALDO AS NUMERIC(18,2)),
  s.FECHA_CARGO,
  COALESCE(` + puentePV + `, 0),
  (SELECT pv.FOLIO FROM DOCTOS_PV pv WHERE pv.DOCTO_PV_ID = ` + puentePV + ` ROWS 1),
  (SELECT FIRST 1 a.NOMBRE FROM DOCTOS_PV_DET det JOIN ARTICULOS a ON a.ARTICULO_ID = det.ARTICULO_ID
    WHERE det.DOCTO_PV_ID = ` + puentePV + ` AND det.ROL IN ('N', 'J') ORDER BY det.POSICION)
FROM MSP_SALDOS_VENTAS s
LEFT JOIN LIBRES_CARGOS_CC l   ON l.DOCTO_CC_ID      = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
LEFT JOIN CLIENTES c           ON c.CLIENTE_ID       = s.CLIENTE_ID
WHERE s.ZONA_CLIENTE_ID = ?
  AND s.CARGO_CANCELADO <> 'S'
  AND COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL') <> 'CONTADO'
  AND (s.SALDO > 0 OR s.FECHA_ULT_PAGO >= ?)
ORDER BY s.DOCTO_CC_ID`

// UniversoSemana implementa outbound.CuentasRepo.
func (r *CuentasRepo) UniversoSemana(ctx context.Context, zonaID int, inicioSemana time.Time) ([]domain.CuentaUniverso, error) {
	var out []domain.CuentaUniverso
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, err := q.QueryContext(ctx, queryUniverso, zonaID, firebird.ToWallClock(inicioSemana))
		if err != nil {
			return firebird.MapError(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			c, err := scanCuenta(rows)
			if err != nil {
				return firebird.MapError(err)
			}
			out = append(out, c)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanCuenta(rows *sql.Rows) (domain.CuentaUniverso, error) {
	var (
		c                                  domain.CuentaUniverso
		clienteNombre, producto            sql.NullString
		parcialidadRaw, saldoRaw, cargoRaw any
		folio                              firebird.Win1252 // DOCTOS_PV.FOLIO — NONE
	)
	if err := rows.Scan(&c.VentaID, &c.ClienteID, &clienteNombre, &parcialidadRaw, &saldoRaw,
		&cargoRaw, &c.DoctoPVID, &folio, &producto); err != nil {
		return c, err
	}
	var err error
	if c.Parcialidad, err = firebird.ScanDecimal(parcialidadRaw, 2); err != nil {
		return c, err
	}
	if c.Saldo, err = firebird.ScanDecimal(saldoRaw, 2); err != nil {
		return c, err
	}
	if c.FechaCargo, err = firebird.ScanUTCTime(cargoRaw); err != nil {
		return c, err
	}
	c.ClienteNombre = clienteNombre.String
	c.Producto = producto.String
	c.Folio = strings.TrimSpace(string(folio))
	return c, nil
}
```


- [ ] **Step 4: Correr y ver que pasa**

Run: `go test -count=1 -p 1 ./internal/desempeno/infra/desempenofb/ ./internal/platform/fbcharset/`
Expected: PASS.

- [ ] **Step 5: Control positivo**

Quitar `OR s.FECHA_ULT_PAGO >= ?` (y su argumento) → falla `TestCuentasRepo_SaldadaEntraSoloSiPagoEnLaSemana`. Restaurar.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/desempeno/...
git add internal/desempeno/infra/desempenofb
git commit -m "feat(desempeno): universo de ventas de la semana con la ventana del sync"
```

---

## Carril C — fotos y detalle de pago (rama `feat/desempeno-c-fotos`, desde `main`)

Las tres tareas son independientes; el agente puede hacerlas en cualquier orden. Cargar `.env` para la integración.

### Tarea C1: la foto de pago debe pertenecer al pago (sin cambiar permisos)

`ObtenerImagenPago` y `EliminarImagenPago` ignoraban `pagoID` (`_ = pagoID`, `app/adjuntar_imagen_pago.go:162,198`): con el id de cualquier pago se leía **o se borraba** la foto de otro. Los permisos (`cobranza:ver_pagos`) no cambian en esta entrega (Apéndice 2).

**Files:**
- Modify: `internal/cobranza/ports/outbound/pagos_recibidos_repo.go` (`FindImagenByID`/`DeleteImagen` → `FindImagenDePago`/`DeleteImagenDePago` con `pagoID`)
- Modify: `internal/cobranza/infra/ventfb/pagos_recibidos_repo.go` (`WHERE ID = ? AND PAGO_ID = ?`)
- Modify: `internal/cobranza/app/adjuntar_imagen_pago.go`
- Modify (fakes y pruebas): `internal/cobranza/app/adjuntar_imagen_pago_test.go`, `internal/cobranza/infra/cobranzahttp/handlers_pago_recibido_test.go`, `internal/cobranza/infra/cobranzahttp/handlers_pago_imagenes_test.go`, `internal/cobranza/infra/ventfb/pagos_recibidos_repo_test.go`

**Interfaces:**
- Consumes: nada nuevo.
- Produces: puerto `PagosImagenesRepo` con `FindImagenDePago(ctx, pagoID, imagenID uuid.UUID) (*domain.Imagen, error)` y `DeleteImagenDePago(ctx, pagoID, imagenID uuid.UUID) error` (únicos consumidores: `adjuntar_imagen_pago.go` y los dos fakes; enumerado con `grep -rn "FindImagenByID\|DeleteImagen(" internal/` antes del cambio).

- [ ] **Step 1: Pruebas que fallan (y fakes que replican el `AND PAGO_ID = ?`)**

`internal/cobranza/app/adjuntar_imagen_pago_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/app/adjuntar_imagen_pago_test.go b/internal/cobranza/app/adjuntar_imagen_pago_test.go
index fdeb1a2..4d080c8 100644
--- a/internal/cobranza/app/adjuntar_imagen_pago_test.go
+++ b/internal/cobranza/app/adjuntar_imagen_pago_test.go
@@ -51,12 +51,12 @@ func (f *fakePagosImagenesRepo) InsertImagen(_ context.Context, pagoID uuid.UUID
 	return nil
 }
 
-func (f *fakePagosImagenesRepo) DeleteImagen(_ context.Context, imagenID uuid.UUID) error {
+func (f *fakePagosImagenesRepo) DeleteImagenDePago(_ context.Context, pagoID, imagenID uuid.UUID) error {
 	if f.deleteErr != nil {
 		return f.deleteErr
 	}
 	img, ok := f.images[imagenID]
-	if !ok {
+	if !ok || !f.esDe(pagoID, imagenID) {
 		return domain.ErrImagenNoEncontrada
 	}
 	// Remove from byPago slices.
@@ -72,17 +72,27 @@ func (f *fakePagosImagenesRepo) DeleteImagen(_ context.Context, imagenID uuid.UU
 	return nil
 }
 
-func (f *fakePagosImagenesRepo) FindImagenByID(_ context.Context, imagenID uuid.UUID) (*domain.Imagen, error) {
+func (f *fakePagosImagenesRepo) FindImagenDePago(_ context.Context, pagoID, imagenID uuid.UUID) (*domain.Imagen, error) {
 	if f.findErr != nil {
 		return nil, f.findErr
 	}
 	img, ok := f.images[imagenID]
-	if !ok {
+	if !ok || !f.esDe(pagoID, imagenID) {
 		return nil, domain.ErrImagenNoEncontrada
 	}
 	return img, nil
 }
 
+// esDe replica el WHERE ID = ? AND PAGO_ID = ? del repositorio real.
+func (f *fakePagosImagenesRepo) esDe(pagoID, imagenID uuid.UUID) bool {
+	for _, id := range f.byPago[pagoID] {
+		if id == imagenID {
+			return true
+		}
+	}
+	return false
+}
+
 func (f *fakePagosImagenesRepo) ListImagenes(_ context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error) {
 	ids := f.byPago[pagoID]
 	out := make([]*domain.Imagen, 0, len(ids))
@@ -801,3 +811,44 @@ func TestListarImagenesPago_DepsMissing(t *testing.T) {
 	_, err := svc.ListarImagenesPago(context.Background(), uuid.New())
 	require.Error(t, err)
 }
+
+// ─── Pertenencia ──────────────────────────────────────────────────────────────
+
+func TestObtenerImagenPago_DeOtroPagoEs404(t *testing.T) {
+	t.Parallel()
+
+	pagosRepo := newFakePagosRecibidosRepo()
+	imagenes := newFakePagosImagenesRepo()
+	storage := newFakeStorageProvider()
+	pagoA := seedPago(t, pagosRepo)
+	pagoB := seedPago(t, pagosRepo)
+	img := seedImagen(t, imagenes, pagoA)
+	storage.objects[img.Storage().Key()] = []byte("data")
+	storage.mimes[img.Storage().Key()] = domain.MimeJPEG
+	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)
+
+	_, err := svc.ObtenerImagenPago(context.Background(), pagoB, img.ID())
+	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
+
+	res, err := svc.ObtenerImagenPago(context.Background(), pagoA, img.ID())
+	require.NoError(t, err, "control positivo: con su pago sí")
+	_ = res.Object.Body.Close()
+}
+
+func TestEliminarImagenPago_DeOtroPagoNoBorraNada(t *testing.T) {
+	t.Parallel()
+
+	pagosRepo := newFakePagosRecibidosRepo()
+	imagenes := newFakePagosImagenesRepo()
+	storage := newFakeStorageProvider()
+	pagoA := seedPago(t, pagosRepo)
+	pagoB := seedPago(t, pagosRepo)
+	img := seedImagen(t, imagenes, pagoA)
+	storage.objects[img.Storage().Key()] = []byte("data")
+	svc := newImagenSvc(t, pagosRepo, imagenes, storage, nil)
+
+	err := svc.EliminarImagenPago(context.Background(), pagoB, img.ID(), uuid.New())
+	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
+	assert.Len(t, imagenes.images, 1, "la fila de A sigue")
+	assert.Equal(t, 0, storage.deleteCalls, "el blob de A no se tocó")
+}
```

`internal/cobranza/infra/cobranzahttp/handlers_pago_recibido_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/infra/cobranzahttp/handlers_pago_recibido_test.go b/internal/cobranza/infra/cobranzahttp/handlers_pago_recibido_test.go
index 61812e4..2c455f9 100644
--- a/internal/cobranza/infra/cobranzahttp/handlers_pago_recibido_test.go
+++ b/internal/cobranza/infra/cobranzahttp/handlers_pago_recibido_test.go
@@ -140,12 +140,12 @@ func (f *fakePagosImagenesRepo) InsertImagen(_ context.Context, pagoID uuid.UUID
 	return nil
 }
 
-func (f *fakePagosImagenesRepo) DeleteImagen(_ context.Context, imagenID uuid.UUID) error {
+func (f *fakePagosImagenesRepo) DeleteImagenDePago(_ context.Context, pagoID, imagenID uuid.UUID) error {
 	if f.deleteErr != nil {
 		return f.deleteErr
 	}
 	img, ok := f.images[imagenID]
-	if !ok {
+	if !ok || !f.esDe(pagoID, imagenID) {
 		return domain.ErrImagenNoEncontrada
 	}
 	for pagoID, ids := range f.byPago {
@@ -160,17 +160,27 @@ func (f *fakePagosImagenesRepo) DeleteImagen(_ context.Context, imagenID uuid.UU
 	return nil
 }
 
-func (f *fakePagosImagenesRepo) FindImagenByID(_ context.Context, imagenID uuid.UUID) (*domain.Imagen, error) {
+func (f *fakePagosImagenesRepo) FindImagenDePago(_ context.Context, pagoID, imagenID uuid.UUID) (*domain.Imagen, error) {
 	if f.findErr != nil {
 		return nil, f.findErr
 	}
 	img, ok := f.images[imagenID]
-	if !ok {
+	if !ok || !f.esDe(pagoID, imagenID) {
 		return nil, domain.ErrImagenNoEncontrada
 	}
 	return img, nil
 }
 
+// esDe replica el WHERE ID = ? AND PAGO_ID = ? del repositorio real.
+func (f *fakePagosImagenesRepo) esDe(pagoID, imagenID uuid.UUID) bool {
+	for _, id := range f.byPago[pagoID] {
+		if id == imagenID {
+			return true
+		}
+	}
+	return false
+}
+
 func (f *fakePagosImagenesRepo) ListImagenes(_ context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error) {
 	ids := f.byPago[pagoID]
 	out := make([]*domain.Imagen, 0, len(ids))
```

`internal/cobranza/infra/cobranzahttp/handlers_pago_imagenes_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/infra/cobranzahttp/handlers_pago_imagenes_test.go b/internal/cobranza/infra/cobranzahttp/handlers_pago_imagenes_test.go
index 2d0c8c2..dd40ee9 100644
--- a/internal/cobranza/infra/cobranzahttp/handlers_pago_imagenes_test.go
+++ b/internal/cobranza/infra/cobranzahttp/handlers_pago_imagenes_test.go
@@ -584,3 +584,45 @@ func TestHTTP_ObtenerImagenPagoHandler_NotFound(t *testing.T) {
 
 	assert.Equal(t, http.StatusNotFound, rec.Code)
 }
+
+// ─── Pertenencia (spec desempeño §3.2) ────────────────────────────────────────
+
+func TestHTTP_ObtenerImagenPagoHandler_ImagenDeOtroPagoEs404(t *testing.T) {
+	t.Parallel()
+
+	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
+	pagosRepo := newFakePagosRecibidosRepo()
+	imagenesRepo := newFakePagosImagenesRepo()
+	storage := newFakeStorageProvider()
+	pagoA := seedHTTPPago(t, pagosRepo)
+	pagoB := seedHTTPPago(t, pagosRepo)
+	img := seedHTTPImagen(t, imagenesRepo, storage, pagoA)
+	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})
+
+	rec := httptest.NewRecorder()
+	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec,
+		httptest.NewRequest(http.MethodGet, fmt.Sprintf("/pagos/%s/imagenes/%s", pagoB, img.ID()), nil))
+	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
+	assert.Contains(t, rec.Body.String(), "code=pago_imagen_no_encontrada")
+	assert.NotContains(t, rec.Body.String(), "XXXX", "no se filtra ni un byte del blob")
+}
+
+func TestHTTP_EliminarImagenPago_ImagenDeOtroPagoEs404(t *testing.T) {
+	t.Parallel()
+
+	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
+	pagosRepo := newFakePagosRecibidosRepo()
+	imagenesRepo := newFakePagosImagenesRepo()
+	storage := newFakeStorageProvider()
+	pagoA := seedHTTPPago(t, pagosRepo)
+	pagoB := seedHTTPPago(t, pagosRepo)
+	img := seedHTTPImagen(t, imagenesRepo, storage, pagoA)
+	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, imagenesRepo, nil, storage, nil, fakeTxRunner{})
+
+	rec := httptest.NewRecorder()
+	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec,
+		httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/pagos/%s/imagenes/%s", pagoB, img.ID()), nil))
+	assert.Equal(t, http.StatusNotFound, rec.Code)
+	assert.Len(t, imagenesRepo.images, 1, "la imagen de A sigue")
+	assert.Equal(t, 0, storage.deleteCalls)
+}
```

`internal/cobranza/infra/ventfb/pagos_recibidos_repo_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/infra/ventfb/pagos_recibidos_repo_test.go b/internal/cobranza/infra/ventfb/pagos_recibidos_repo_test.go
index a080f23..fd8bdc5 100644
--- a/internal/cobranza/infra/ventfb/pagos_recibidos_repo_test.go
+++ b/internal/cobranza/infra/ventfb/pagos_recibidos_repo_test.go
@@ -684,11 +684,11 @@ func TestE2E_PagosImagenes_Insert_HappyPath(t *testing.T) {
 	})
 }
 
-// TestE2E_PagosImagenes_FindImagenByID_HappyPath verifies that FindImagenByID
+// TestE2E_PagosImagenes_FindImagenDePago_HappyPath verifies that FindImagenByID
 // returns the correct imagen with all fields bit-exact.
 //
 //nolint:paralleltest // serial: shares rollback-only tx.
-func TestE2E_PagosImagenes_FindImagenByID_HappyPath(t *testing.T) {
+func TestE2E_PagosImagenes_FindImagenDePago_HappyPath(t *testing.T) {
 	requireFBEnv(t)
 	pool := fbtestutil.NewTestFirebirdPool(t)
 
@@ -700,7 +700,7 @@ func TestE2E_PagosImagenes_FindImagenByID_HappyPath(t *testing.T) {
 		img := buildAndAttachImagen(t, pago, "pagos/2026/recibo.jpg")
 		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img))
 
-		found, err := repo.FindImagenByID(ctx, img.ID())
+		found, err := repo.FindImagenDePago(ctx, pago.ID(), img.ID())
 		require.NoError(t, err)
 
 		assert.Equal(t, img.ID(), found.ID())
@@ -715,28 +715,28 @@ func TestE2E_PagosImagenes_FindImagenByID_HappyPath(t *testing.T) {
 	})
 }
 
-// TestE2E_PagosImagenes_FindImagenByID_NotFound verifies that looking up a
+// TestE2E_PagosImagenes_FindImagenDePago_NotFound verifies that looking up a
 // random UUID returns ErrImagenNoEncontrada.
 //
 //nolint:paralleltest // serial: shares rollback-only tx.
-func TestE2E_PagosImagenes_FindImagenByID_NotFound(t *testing.T) {
+func TestE2E_PagosImagenes_FindImagenDePago_NotFound(t *testing.T) {
 	requireFBEnv(t)
 	pool := fbtestutil.NewTestFirebirdPool(t)
 
 	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
 		repo := newPagosRecibidosRepo(pool)
 
-		_, err := repo.FindImagenByID(ctx, uuid.New())
+		_, err := repo.FindImagenDePago(ctx, uuid.New(), uuid.New())
 		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada,
 			"FindImagenByID for unknown UUID must return ErrImagenNoEncontrada; got: %v", err)
 	})
 }
 
-// TestE2E_PagosImagenes_DeleteImagen_HappyPath verifies that DeleteImagen
+// TestE2E_PagosImagenes_DeleteImagenDePago_HappyPath verifies that DeleteImagen
 // removes the row and a subsequent FindImagenByID returns ErrImagenNoEncontrada.
 //
 //nolint:paralleltest // serial: shares rollback-only tx.
-func TestE2E_PagosImagenes_DeleteImagen_HappyPath(t *testing.T) {
+func TestE2E_PagosImagenes_DeleteImagenDePago_HappyPath(t *testing.T) {
 	requireFBEnv(t)
 	pool := fbtestutil.NewTestFirebirdPool(t)
 
@@ -748,26 +748,26 @@ func TestE2E_PagosImagenes_DeleteImagen_HappyPath(t *testing.T) {
 		img := buildAndAttachImagen(t, pago, "pagos/2026/a-borrar.jpg")
 		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img))
 
-		require.NoError(t, repo.DeleteImagen(ctx, img.ID()), "DeleteImagen must succeed")
+		require.NoError(t, repo.DeleteImagenDePago(ctx, pago.ID(), img.ID()), "DeleteImagen must succeed")
 
-		_, err := repo.FindImagenByID(ctx, img.ID())
+		_, err := repo.FindImagenDePago(ctx, pago.ID(), img.ID())
 		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada,
 			"FindImagenByID after DeleteImagen must return ErrImagenNoEncontrada; got: %v", err)
 	})
 }
 
-// TestE2E_PagosImagenes_DeleteImagen_NotFound verifies that deleting a
+// TestE2E_PagosImagenes_DeleteImagenDePago_NotFound verifies that deleting a
 // non-existent imagen returns ErrImagenNoEncontrada.
 //
 //nolint:paralleltest // serial: shares rollback-only tx.
-func TestE2E_PagosImagenes_DeleteImagen_NotFound(t *testing.T) {
+func TestE2E_PagosImagenes_DeleteImagenDePago_NotFound(t *testing.T) {
 	requireFBEnv(t)
 	pool := fbtestutil.NewTestFirebirdPool(t)
 
 	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
 		repo := newPagosRecibidosRepo(pool)
 
-		err := repo.DeleteImagen(ctx, uuid.New())
+		err := repo.DeleteImagenDePago(ctx, uuid.New(), uuid.New())
 		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada,
 			"DeleteImagen for unknown UUID must return ErrImagenNoEncontrada; got: %v", err)
 	})
@@ -875,3 +875,33 @@ func TestE2E_PagosRecibidos_UTC_Roundtrip(t *testing.T) {
 			"FechaHoraPago must be in UTC after round-trip")
 	})
 }
+
+// TestE2E_PagosImagenes_ImagenDeOtroPagoEs404 fija el arreglo de pertenencia:
+// pedir o borrar la imagen de un pago A con el id de un pago B es "no
+// encontrada", y la fila de A sigue ahí. Antes de la corrección se buscaba
+// solo por ID de imagen.
+//
+//nolint:paralleltest // serial: shares rollback-only tx.
+func TestE2E_PagosImagenes_ImagenDeOtroPagoEs404(t *testing.T) {
+	requireFBEnv(t)
+	pool := fbtestutil.NewTestFirebirdPool(t)
+
+	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
+		repo := newPagosRecibidosRepo(pool)
+		pagoA := buildValidPagoRecibido(t)
+		require.NoError(t, repo.Insert(ctx, pagoA))
+		pagoB := buildValidPagoRecibido(t)
+		require.NoError(t, repo.Insert(ctx, pagoB))
+		img := buildAndAttachImagen(t, pagoA, "pagos/2026/de-a.jpg")
+		require.NoError(t, repo.InsertImagen(ctx, pagoA.ID(), img))
+
+		_, err := repo.FindImagenDePago(ctx, pagoB.ID(), img.ID())
+		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada)
+		require.ErrorIs(t, repo.DeleteImagenDePago(ctx, pagoB.ID(), img.ID()), domain.ErrImagenNoEncontrada)
+
+		q := firebird.GetQuerier(ctx, pool.DB)
+		assert.Equal(t, 1, directCountImagen(ctx, t, q, img.ID()), "la imagen de A no se borró")
+		_, err = repo.FindImagenDePago(ctx, pagoA.ID(), img.ID())
+		require.NoError(t, err, "control positivo: con su pago sí se encuentra")
+	})
+}
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test ./internal/cobranza/app/ ./internal/cobranza/infra/cobranzahttp/`
Expected: FAIL de compilación (los fakes ya no satisfacen el puerto viejo / `FindImagenDePago` no existe en el puerto).

- [ ] **Step 3: Implementación**

`internal/cobranza/ports/outbound/pagos_recibidos_repo.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/ports/outbound/pagos_recibidos_repo.go b/internal/cobranza/ports/outbound/pagos_recibidos_repo.go
index 80fb51a..6443d33 100644
--- a/internal/cobranza/ports/outbound/pagos_recibidos_repo.go
+++ b/internal/cobranza/ports/outbound/pagos_recibidos_repo.go
@@ -53,13 +53,14 @@ type PagosImagenesRepo interface {
 	// InsertImagen persists a new comprobante row in MSP_PAGOS_IMAGENES.
 	InsertImagen(ctx context.Context, pagoID uuid.UUID, img *domain.Imagen) error
 
-	// DeleteImagen removes a comprobante row. Returns
-	// [domain.ErrImagenNoEncontrada] when the row does not exist.
-	DeleteImagen(ctx context.Context, imagenID uuid.UUID) error
+	// DeleteImagenDePago removes a comprobante row ONLY if it belongs to
+	// pagoID. Returns [domain.ErrImagenNoEncontrada] when no row matches both
+	// ids — an image of another pago is indistinguishable from a missing one.
+	DeleteImagenDePago(ctx context.Context, pagoID, imagenID uuid.UUID) error
 
-	// FindImagenByID loads a single comprobante row by its UUID, without
-	// loading its parent pago. Returns [domain.ErrImagenNoEncontrada] on miss.
-	FindImagenByID(ctx context.Context, imagenID uuid.UUID) (*domain.Imagen, error)
+	// FindImagenDePago loads a comprobante row ONLY if it belongs to pagoID.
+	// Returns [domain.ErrImagenNoEncontrada] when no row matches both ids.
+	FindImagenDePago(ctx context.Context, pagoID, imagenID uuid.UUID) (*domain.Imagen, error)
 
 	// ListImagenes returns every comprobante attached to pagoID, ordered by
 	// CREATED_AT ascending.
```

`internal/cobranza/infra/ventfb/pagos_recibidos_repo.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/infra/ventfb/pagos_recibidos_repo.go b/internal/cobranza/infra/ventfb/pagos_recibidos_repo.go
index 1cc2063..33a4cd1 100644
--- a/internal/cobranza/infra/ventfb/pagos_recibidos_repo.go
+++ b/internal/cobranza/infra/ventfb/pagos_recibidos_repo.go
@@ -272,7 +272,9 @@ INSERT INTO MSP_PAGOS_IMAGENES (
 	CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY
 ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
 
-const deletePagoImagenSQL = `DELETE FROM MSP_PAGOS_IMAGENES WHERE ID = ?`
+// La imagen se busca y se borra por (ID, PAGO_ID): pedirla por otro pago es
+// un 404, no una fuga (arreglo de pertenencia, spec desempeño §3.2).
+const deletePagoImagenSQL = `DELETE FROM MSP_PAGOS_IMAGENES WHERE ID = ? AND PAGO_ID = ?`
 
 const selectPagoImagenCols = `
 	ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
@@ -281,7 +283,7 @@ const selectPagoImagenCols = `
 const findPagoImagenByIDSQL = `
 SELECT ` + selectPagoImagenCols + `
 FROM MSP_PAGOS_IMAGENES
-WHERE ID = ?`
+WHERE ID = ? AND PAGO_ID = ?`
 
 const listPagoImagenesSQL = `
 SELECT ` + selectPagoImagenCols + `
@@ -316,11 +318,12 @@ func (r *PagosRecibidosRepo) InsertImagen(ctx context.Context, pagoID uuid.UUID,
 	})
 }
 
-// DeleteImagen removes a single row from MSP_PAGOS_IMAGENES.
-func (r *PagosRecibidosRepo) DeleteImagen(ctx context.Context, imagenID uuid.UUID) error {
+// DeleteImagenDePago removes a single row from MSP_PAGOS_IMAGENES when it
+// belongs to pagoID.
+func (r *PagosRecibidosRepo) DeleteImagenDePago(ctx context.Context, pagoID, imagenID uuid.UUID) error {
 	return firebird.RunInTx(ctx, r.pool.DB, func(ctx context.Context) error {
 		q := firebird.GetQuerier(ctx, r.pool.DB)
-		res, err := q.ExecContext(ctx, deletePagoImagenSQL, imagenID.String())
+		res, err := q.ExecContext(ctx, deletePagoImagenSQL, imagenID.String(), pagoID.String())
 		if err != nil {
 			return firebird.MapError(err)
 		}
@@ -328,12 +331,12 @@ func (r *PagosRecibidosRepo) DeleteImagen(ctx context.Context, imagenID uuid.UUI
 	})
 }
 
-// FindImagenByID loads a single comprobante row.
-func (r *PagosRecibidosRepo) FindImagenByID(ctx context.Context, imagenID uuid.UUID) (*domain.Imagen, error) {
+// FindImagenDePago loads a single comprobante row when it belongs to pagoID.
+func (r *PagosRecibidosRepo) FindImagenDePago(ctx context.Context, pagoID, imagenID uuid.UUID) (*domain.Imagen, error) {
 	var result *domain.Imagen
 	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
 		q := firebird.GetQuerier(ctx, r.pool.DB)
-		row := q.QueryRowContext(ctx, findPagoImagenByIDSQL, imagenID.String())
+		row := q.QueryRowContext(ctx, findPagoImagenByIDSQL, imagenID.String(), pagoID.String())
 		var serr error
 		result, serr = scanPagoImagenRow(row)
 		return serr
```

`internal/cobranza/app/adjuntar_imagen_pago.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/cobranza/app/adjuntar_imagen_pago.go b/internal/cobranza/app/adjuntar_imagen_pago.go
index e6816e6..5bdd9c5 100644
--- a/internal/cobranza/app/adjuntar_imagen_pago.go
+++ b/internal/cobranza/app/adjuntar_imagen_pago.go
@@ -146,19 +146,20 @@ func (s *Service) EliminarImagenPago(ctx context.Context, pagoID, imagenID, by u
 		return errWriteDepsMissing("storage_provider")
 	}
 
-	img, err := s.pagosImagenes.FindImagenByID(ctx, imagenID)
+	// La imagen debe pertenecer a ESTE pago: una de otro pago es un 404 y no
+	// se toca (antes se ignoraba pagoID y se borraba la imagen de cualquiera).
+	img, err := s.pagosImagenes.FindImagenDePago(ctx, pagoID, imagenID)
 	if err != nil {
 		return err
 	}
 	key := img.Storage().Key()
 
-	if err := s.pagosImagenes.DeleteImagen(ctx, imagenID); err != nil {
+	if err := s.pagosImagenes.DeleteImagenDePago(ctx, pagoID, imagenID); err != nil {
 		return err
 	}
 	// Best-effort blob removal; if the blob fails the row is gone and a
 	// reconciler can clean up dangling files later.
 	s.bestEffortDeleteBlob(ctx, key)
-	_ = pagoID // pagoID validated by the handler via the imagen's FK; no extra check needed here.
 	_ = by
 	return nil
 }
@@ -183,7 +184,8 @@ func (s *Service) ObtenerImagenPago(ctx context.Context, pagoID, imagenID uuid.U
 		return nil, errWriteDepsMissing("storage_provider")
 	}
 
-	img, err := s.pagosImagenes.FindImagenByID(ctx, imagenID)
+	// Pertenencia: una imagen de otro pago es 404 (antes `_ = pagoID`).
+	img, err := s.pagosImagenes.FindImagenDePago(ctx, pagoID, imagenID)
 	if err != nil {
 		return nil, err
 	}
@@ -195,7 +197,6 @@ func (s *Service) ObtenerImagenPago(ctx context.Context, pagoID, imagenID uuid.U
 	out.Object.Body = obj.Body
 	out.Object.ContentType = obj.ContentType
 	out.Object.SizeBytes = obj.SizeBytes
-	_ = pagoID
 	return out, nil
 }
```


- [ ] **Step 4: Correr y ver que pasan**

Run: `go test -count=1 ./internal/cobranza/app/ ./internal/cobranza/infra/cobranzahttp/ && go test -count=1 -p 1 -run 'TestE2E_PagosImagenes' ./internal/cobranza/infra/ventfb/`
Expected: PASS (la segunda con `FB_DATABASE`).

- [ ] **Step 5: Control positivo**

En `ventfb/pagos_recibidos_repo.go`, quitar `AND PAGO_ID = ?` de `findPagoImagenByIDSQL` y el segundo argumento → falla `TestE2E_PagosImagenes_ImagenDeOtroPagoEs404`. En el fake HTTP, hacer que `esDe` devuelva `true` → fallan `TestHTTP_ObtenerImagenPagoHandler_ImagenDeOtroPagoEs404` y `TestHTTP_EliminarImagenPago_ImagenDeOtroPagoEs404`. Restaurar; diff limpio.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/cobranza/...
git add internal/cobranza
git commit -m "fix(cobranza): la foto de un pago solo se lee o borra con su propio pago"
```

### Tarea C2: fotos de visitas (lista y binario con pertenencia)

**Files:**
- Modify: `internal/visitas/domain/errors.go` (`ErrImagenNoEncontrada`)
- Modify: `internal/visitas/ports/outbound/repo.go` (`ListImagenes`, `FindImagenDeVisita`)
- Create: `internal/visitas/infra/visitasfb/imagenes_lectura.go`
- Create: `internal/visitas/app/imagenes_visita.go`
- Create: `internal/visitas/infra/visitashttp/handlers_imagenes.go`
- Modify: `internal/visitas/infra/visitashttp/routes.go` (registrar las dos rutas)
- Modify (fakes): `internal/visitas/app/registrar_visita_con_imagenes_test.go`, `internal/visitas/infra/visitashttp/handlers_multipart_test.go`
- Test: `internal/visitas/app/imagenes_visita_test.go`, `internal/visitas/infra/visitashttp/handlers_imagenes_test.go`, `internal/visitas/infra/visitasfb/imagenes_repo_test.go` (prueba nueva al final)

**Interfaces:**
- Produces: `GET /v2/visitas/{id}/imagenes` → `[]ImagenVisitaDTO{id, mime, size_bytes, descripcion (null explícito), created_at, updated_at}`; `GET /v2/visitas/{id}/imagenes/{img_id}` → binario con `ETag`, `Cache-Control: private, max-age=31536000, immutable`, 304 con `If-None-Match`, 404 `visita_imagen_no_encontrada` si no es de esa visita. Lectura con `cobranza:ver_pagos`. La captura de intents de visitas es solo `POST` (`cmd/api/failedintent_wiring.go:253-254`), así que estos `GET` no se capturan.

- [ ] **Step 1: Pruebas que fallan**

`internal/visitas/app/imagenes_visita_test.go` (archivo nuevo, completo):

```go
package app_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

func imagenDe(t *testing.T, visitaID uuid.UUID) *domain.Imagen {
	t.Helper()
	id := uuid.New()
	st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, "visitas/"+visitaID.String()+"/"+id.String()+".jpg")
	require.NoError(t, err)
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID: id, Storage: st, Mime: domain.MimeJPEG, SizeBytes: 4,
		CreatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC),
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
	})
}

func TestObtenerImagenVisita_Pertenencia(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	imgs := newFakeImagenesRepo()
	store := newFakeStorage()
	visitaA, visitaB := uuid.New(), uuid.New()
	img := imagenDe(t, visitaA)
	imgs.porVisita[visitaA] = []*domain.Imagen{img}
	require.NoError(t, store.Store(context.Background(), img.Storage().Key(), domain.MimeJPEG, 4, strings.NewReader("JPEG")))
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithImagenes(imgs, store, &fakeTxRunner{}))

	_, err := svc.ObtenerImagenVisita(context.Background(), visitaB, img.ID())
	require.ErrorIs(t, err, domain.ErrImagenNoEncontrada, "la foto de otra visita es 404")

	res, err := svc.ObtenerImagenVisita(context.Background(), visitaA, img.ID())
	require.NoError(t, err)
	defer func() { _ = res.Body.Close() }()
	b, _ := io.ReadAll(res.Body)
	assert.Equal(t, "JPEG", string(b))
	assert.Equal(t, domain.MimeJPEG, res.ContentType)
}

func TestListarImagenesVisita(t *testing.T) {
	t.Parallel()
	repo := newFakeVisitasRepo()
	imgs := newFakeImagenesRepo()
	svc := app.NewService(repo, &fakeClock{t: fixedNow}, app.WithImagenes(imgs, newFakeStorage(), &fakeTxRunner{}))

	_, err := svc.ListarImagenesVisita(context.Background(), uuid.New())
	require.ErrorIs(t, err, domain.ErrVisitaNoEncontrada, "visita inexistente es 404, no lista vacía")

	visita, err := domain.NewVisita(domain.CrearVisitaParams{
		ID: uuid.New(), Cobrador: "Ramírez García, Jorge", CobradorID: 42, Fecha: fixedNow.Add(-time.Hour),
		TipoVisita: string(domain.TipoVisitaNoSeEncontraba), ClienteID: 11486, ZonaClienteID: 7,
		CreatedBy: uuid.New(), Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Insert(context.Background(), visita))
	v := visita.ID()
	got, err := svc.ListarImagenesVisita(context.Background(), v)
	require.NoError(t, err)
	assert.Empty(t, got)
	imgs.porVisita[v] = []*domain.Imagen{imagenDe(t, v), imagenDe(t, v)}
	got, err = svc.ListarImagenesVisita(context.Background(), v)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestImagenesVisita_SinConfigurarEsErrorInterno(t *testing.T) {
	t.Parallel()
	svc := app.NewService(newFakeVisitasRepo(), &fakeClock{t: fixedNow})
	_, err := svc.ListarImagenesVisita(context.Background(), uuid.New())
	require.Error(t, err)
	_, err = svc.ObtenerImagenVisita(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}
```

`internal/visitas/infra/visitashttp/handlers_imagenes_test.go` (archivo nuevo, completo):

```go
package visitashttp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
)

// fotoSembrada guarda una imagen JPEG en la visita y su blob en storage.
func fotoSembrada(t *testing.T, f multipartFixture, visitaID uuid.UUID, desc *string) *domain.Imagen {
	t.Helper()
	id := uuid.New()
	key := "visitas/" + visitaID.String() + "/" + id.String() + ".jpg"
	st, err := domain.NewImagenStorage(domain.StorageKindFilesystem, key)
	require.NoError(t, err)
	img := domain.HydrateImagen(domain.HydrateImagenParams{
		ID: id, Storage: st, Mime: domain.MimeJPEG, SizeBytes: 4, Descripcion: desc,
		CreatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC),
		CreatedBy: uuid.New(), UpdatedBy: uuid.New(),
	})
	require.NoError(t, f.imgRepo.InsertImagen(context.Background(), visitaID, img))
	require.NoError(t, f.store.Store(context.Background(), key, domain.MimeJPEG, 4, strings.NewReader("JPEG")))
	return img
}

func TestHTTP_ListarImagenesVisita(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	fachada := "fachada"
	fotoSembrada(t, f, visita, &fachada)
	fotoSembrada(t, f, visita, nil)

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/visitas/"+visita.String()+"/imagenes", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var got []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Len(t, got, 2)
	for _, k := range []string{"id", "mime", "size_bytes", "descripcion", "created_at", "updated_at"} {
		assert.Contains(t, got[1], k, "clave %s del contrato §6", k)
	}
	assert.Nil(t, got[1]["descripcion"], "descripcion null explícita, no omitida")
	assert.Equal(t, "fachada", got[0]["descripcion"])
	assert.Equal(t, "2026-10-14T16:00:00Z", got[0]["created_at"])

	var dtos []visitashttp.ImagenVisitaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	assert.Equal(t, int64(4), dtos[0].SizeBytes)
}

func TestHTTP_ListarImagenesVisita_Errores(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	casos := []struct {
		nombre string
		h      http.Handler
		path   string
		status int
	}{
		{"sin permiso", mountWithUser(noPermUser(), f.svc), "/visitas/" + visita.String() + "/imagenes", http.StatusForbidden},
		{"visita inexistente", mountWithUser(visitaUser(), f.svc), "/visitas/" + uuid.NewString() + "/imagenes", http.StatusNotFound},
		{"id inválido", mountWithUser(visitaUser(), f.svc), "/visitas/1%20OR%201=1/imagenes", http.StatusUnprocessableEntity},
	}
	for _, c := range casos {
		rec := httptest.NewRecorder()
		c.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		assert.Equal(t, c.status, rec.Code, "%s: %s", c.nombre, rec.Body.String())
	}
}

func TestHTTP_ObtenerImagenVisita_BinarioETagY304(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	img := fotoSembrada(t, f, visita, nil)
	h := mountWithUser(visitaUser(), f.svc)
	path := "/visitas/" + visita.String() + "/imagenes/" + img.ID().String()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "JPEG", rec.Body.String())
	assert.Equal(t, domain.MimeJPEG, rec.Header().Get("Content-Type"))
	assert.Equal(t, "4", rec.Header().Get("Content-Length"))
	etag := `"` + img.ID().String() + `"`
	assert.Equal(t, etag, rec.Header().Get("ETag"))
	assert.Equal(t, "private, max-age=31536000, immutable", rec.Header().Get("Cache-Control"))

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotModified, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestHTTP_ObtenerImagenVisita_DeOtraVisitaEs404(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visitaA := seedVisita(t, f.svc, f.repo)
	visitaB := seedVisita(t, f.svc, f.repo)
	img := fotoSembrada(t, f, visitaA, nil)

	rec := httptest.NewRecorder()
	mountWithUser(visitaUser(), f.svc).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/visitas/"+visitaB.String()+"/imagenes/"+img.ID().String(), nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "code=visita_imagen_no_encontrada")
	assert.NotContains(t, rec.Body.String(), "JPEG", "ni un byte de la foto ajena")
}

func TestHTTP_ObtenerImagenVisita_AuthYParametros(t *testing.T) {
	t.Parallel()
	f := newMultipartFixture(t)
	visita := seedVisita(t, f.svc, f.repo)
	img := fotoSembrada(t, f, visita, nil)
	path := "/visitas/" + visita.String() + "/imagenes/" + img.ID().String()

	rec := httptest.NewRecorder()
	mountWithUser(noPermUser(), f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	rec = httptest.NewRecorder()
	mountBare(f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	for _, malo := range []string{"/visitas/no-uuid/imagenes/" + img.ID().String(), "/visitas/" + visita.String() + "/imagenes/no-uuid"} {
		rec = httptest.NewRecorder()
		mountWithUser(visitaUser(), f.svc).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, malo, nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code, malo)
	}
}

// mountBare monta el router sin usuario plantado (camino 401).
func mountBare(svc *app.Service) http.Handler {
	r := chi.NewRouter()
	visitashttp.MountRouter(r, svc)
	return r
}
```

`internal/visitas/infra/visitasfb/imagenes_repo_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/visitas/infra/visitasfb/imagenes_repo_test.go b/internal/visitas/infra/visitasfb/imagenes_repo_test.go
index 53ca444..1b56572 100644
--- a/internal/visitas/infra/visitasfb/imagenes_repo_test.go
+++ b/internal/visitas/infra/visitasfb/imagenes_repo_test.go
@@ -270,3 +270,55 @@ func TestE2E_VisitasImagenes_FindImagenIDsNoDevuelveLasDeOtraVisita(t *testing.T
 		assert.Equal(t, mineImg.ID(), got[0])
 	})
 }
+
+// TestE2E_VisitasImagenes_ListYPertenencia fija la lectura de fotos: la lista
+// de una visita trae solo las suyas, en orden de CREATED_AT, con todas sus
+// columnas; y pedir la foto de A con el id de B es ErrImagenNoEncontrada.
+//
+//nolint:paralleltest // serial: shares rollback-only tx.
+func TestE2E_VisitasImagenes_ListYPertenencia(t *testing.T) {
+	requireFBEnv(t)
+	pool := fbtestutil.NewTestFirebirdPool(t)
+
+	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
+		repo := newVisitasRepo(pool)
+		a := buildValidVisita(t)
+		b := buildValidVisita(t)
+		require.NoError(t, repo.Insert(ctx, a))
+		require.NoError(t, repo.Insert(ctx, b))
+		desc := "fachada con portón café"
+		primera := attachImagen(t, a, "visitas/"+a.ID().String()+"/1.jpg", func(p *domain.AdjuntarImagenParams) {
+			p.Descripcion = &desc
+			p.Now = time.Date(2026, 10, 14, 16, 0, 0, 0, time.UTC)
+		})
+		segunda := attachImagen(t, a, "visitas/"+a.ID().String()+"/2.jpg", func(p *domain.AdjuntarImagenParams) {
+			p.Now = time.Date(2026, 10, 14, 16, 5, 0, 0, time.UTC)
+		})
+		require.NoError(t, repo.InsertImagen(ctx, a.ID(), segunda))
+		require.NoError(t, repo.InsertImagen(ctx, a.ID(), primera))
+		deB := attachImagen(t, b, "visitas/"+b.ID().String()+"/b.jpg")
+		require.NoError(t, repo.InsertImagen(ctx, b.ID(), deB))
+
+		got, err := repo.ListImagenes(ctx, a.ID())
+		require.NoError(t, err)
+		require.Len(t, got, 2, "solo las de A")
+		assert.Equal(t, primera.ID(), got[0].ID(), "orden por CREATED_AT")
+		require.NotNil(t, got[0].Descripcion())
+		assert.Equal(t, desc, *got[0].Descripcion(), "UTF8 con acento")
+		assert.True(t, primera.Storage().Equals(got[0].Storage()))
+		creada, leida := primera.Audit(), got[0].Audit()
+		assert.Equal(t, creada.CreatedAt(), leida.CreatedAt(), "CREATED_AT de Go, ida y vuelta en UTC")
+		assert.Nil(t, got[1].Descripcion())
+
+		_, err = repo.FindImagenDeVisita(ctx, b.ID(), primera.ID())
+		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada, "la foto de A pedida por B")
+		img, err := repo.FindImagenDeVisita(ctx, a.ID(), primera.ID())
+		require.NoError(t, err, "control positivo")
+		assert.Equal(t, primera.ID(), img.ID())
+
+		vacia, err := repo.ListImagenes(ctx, uuid.New())
+		require.NoError(t, err)
+		assert.NotNil(t, vacia)
+		assert.Empty(t, vacia)
+	})
+}
```

`internal/visitas/app/registrar_visita_con_imagenes_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/visitas/app/registrar_visita_con_imagenes_test.go b/internal/visitas/app/registrar_visita_con_imagenes_test.go
index 69e6423..b75e7f4 100644
--- a/internal/visitas/app/registrar_visita_con_imagenes_test.go
+++ b/internal/visitas/app/registrar_visita_con_imagenes_test.go
@@ -73,6 +73,20 @@ func (r *fakeImagenesRepo) FindImagenIDs(_ context.Context, visitaID uuid.UUID)
 	return out, nil
 }
 
+func (r *fakeImagenesRepo) ListImagenes(_ context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
+	return append([]*domain.Imagen{}, r.porVisita[visitaID]...), nil
+}
+
+// FindImagenDeVisita replica el WHERE ID = ? AND VISITA_ID = ? del repo real.
+func (r *fakeImagenesRepo) FindImagenDeVisita(_ context.Context, visitaID, imagenID uuid.UUID) (*domain.Imagen, error) {
+	for _, img := range r.porVisita[visitaID] {
+		if img.ID() == imagenID {
+			return img, nil
+		}
+	}
+	return nil, domain.ErrImagenNoEncontrada
+}
+
 func (r *fakeImagenesRepo) count(visitaID uuid.UUID) int { return len(r.porVisita[visitaID]) }
 
 // fakeStorage is an in-memory blob store that records every Store and Delete
```

`internal/visitas/infra/visitashttp/handlers_multipart_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/visitas/infra/visitashttp/handlers_multipart_test.go b/internal/visitas/infra/visitashttp/handlers_multipart_test.go
index 84e40aa..8bf2ca0 100644
--- a/internal/visitas/infra/visitashttp/handlers_multipart_test.go
+++ b/internal/visitas/infra/visitashttp/handlers_multipart_test.go
@@ -57,6 +57,20 @@ func (r *httpFakeImagenesRepo) FindImagenIDs(_ context.Context, visitaID uuid.UU
 	return out, nil
 }
 
+func (r *httpFakeImagenesRepo) ListImagenes(_ context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
+	return append([]*domain.Imagen{}, r.porVisita[visitaID]...), nil
+}
+
+// FindImagenDeVisita replica el WHERE ID = ? AND VISITA_ID = ? del repo real.
+func (r *httpFakeImagenesRepo) FindImagenDeVisita(_ context.Context, visitaID, imagenID uuid.UUID) (*domain.Imagen, error) {
+	for _, img := range r.porVisita[visitaID] {
+		if img.ID() == imagenID {
+			return img, nil
+		}
+	}
+	return nil, domain.ErrImagenNoEncontrada
+}
+
 func (r *httpFakeImagenesRepo) count(visitaID uuid.UUID) int { return len(r.porVisita[visitaID]) }
 
 // httpFakeStorage records every blob written and every key deleted.
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test ./internal/visitas/...`
Expected: FAIL de compilación (`svc.ListarImagenesVisita undefined`, `domain.ErrImagenNoEncontrada undefined`).

- [ ] **Step 3: Implementación**

`internal/visitas/domain/errors.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/visitas/domain/errors.go b/internal/visitas/domain/errors.go
index bb56187..c24ce4a 100644
--- a/internal/visitas/domain/errors.go
+++ b/internal/visitas/domain/errors.go
@@ -114,6 +114,13 @@ var (
 		"no se encontró la visita",
 	)
 
+	// ErrImagenNoEncontrada: la imagen no existe o no es de ESA visita. Las
+	// dos se responden igual (404) para no revelar fotos de otra visita.
+	ErrImagenNoEncontrada = apperror.NewNotFound(
+		"visita_imagen_no_encontrada",
+		"imagen no encontrada",
+	)
+
 	// ─── Imagen (hija de Visita) ───────────────────────────────────────────.
 
 	// ErrImagenIDRequerido is returned when the imagen UUID is the zero value.
```

`internal/visitas/ports/outbound/repo.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/visitas/ports/outbound/repo.go b/internal/visitas/ports/outbound/repo.go
index 1d4d28d..43e6bb4 100644
--- a/internal/visitas/ports/outbound/repo.go
+++ b/internal/visitas/ports/outbound/repo.go
@@ -52,4 +52,11 @@ type VisitasImagenesRepo interface {
 	// visitaID, in no particular order. Used by the idempotent-replay path to
 	// decide which images of a retried request are genuinely new.
 	FindImagenIDs(ctx context.Context, visitaID uuid.UUID) ([]uuid.UUID, error)
+
+	// ListImagenes devuelve las imágenes de la visita en orden de CREATED_AT.
+	ListImagenes(ctx context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error)
+
+	// FindImagenDeVisita carga una imagen SOLO si pertenece a visitaID;
+	// si no, domain.ErrImagenNoEncontrada.
+	FindImagenDeVisita(ctx context.Context, visitaID, imagenID uuid.UUID) (*domain.Imagen, error)
 }
```

`internal/visitas/infra/visitasfb/imagenes_lectura.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package visitasfb

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// Lectura de MSP_VISITAS_IMAGENES (GET /v2/visitas/{id}/imagenes[/{img_id}]).
// Todas las columnas son nuestras: ID/VISITA_ID/STORAGE_*/MIME ASCII y
// DESCRIPCION UTF8 (migración 000063), así que se escanean como string.
const selectVisitaImagenCols = `
	ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES, DESCRIPCION,
	CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY`

const listVisitaImagenesSQL = `
SELECT ` + selectVisitaImagenCols + `
FROM MSP_VISITAS_IMAGENES
WHERE VISITA_ID = ?
ORDER BY CREATED_AT ASC, ID ASC`

// Pertenencia: (ID, VISITA_ID). Una imagen de otra visita es "no encontrada".
const findVisitaImagenSQL = `
SELECT ` + selectVisitaImagenCols + `
FROM MSP_VISITAS_IMAGENES
WHERE ID = ? AND VISITA_ID = ?`

type scanner interface{ Scan(dest ...any) error }

func scanVisitaImagen(row scanner) (*domain.Imagen, error) {
	var (
		id, kind, key, mime, createdBy, updatedBy string
		size                                      int64
		desc                                      sql.NullString
		createdRaw, updatedRaw                    any
	)
	if err := row.Scan(&id, &kind, &key, &mime, &size, &desc, &createdRaw, &updatedRaw, &createdBy, &updatedBy); err != nil {
		return nil, err
	}
	created, err := firebird.ScanUTCTime(createdRaw)
	if err != nil {
		return nil, err
	}
	updated, err := firebird.ScanUTCTime(updatedRaw)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, 3)
	for _, raw := range []string{id, createdBy, updatedBy} {
		u, err := uuid.Parse(raw)
		if err != nil {
			return nil, apperror.NewInternal("visita_imagen_id_invalido", "id inválido en MSP_VISITAS_IMAGENES").WithError(err)
		}
		ids = append(ids, u)
	}
	var d *string
	if desc.Valid {
		v := desc.String
		d = &v
	}
	return domain.HydrateImagen(domain.HydrateImagenParams{
		ID: ids[0], Storage: domain.HydrateImagenStorage(domain.StorageKind(kind), key),
		Mime: mime, SizeBytes: size, Descripcion: d,
		CreatedAt: created, UpdatedAt: updated,
		CreatedBy: ids[1], UpdatedBy: ids[2],
	}), nil
}

// ListImagenes implementa outbound.VisitasImagenesRepo.
func (r *Repo) ListImagenes(ctx context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
	out := []*domain.Imagen{}
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		rows, err := q.QueryContext(ctx, listVisitaImagenesSQL, visitaID.String())
		if err != nil {
			return firebird.MapError(err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			img, err := scanVisitaImagen(rows)
			if err != nil {
				return firebird.MapError(err)
			}
			out = append(out, img)
		}
		return firebird.MapError(rows.Err())
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FindImagenDeVisita implementa outbound.VisitasImagenesRepo.
func (r *Repo) FindImagenDeVisita(ctx context.Context, visitaID, imagenID uuid.UUID) (*domain.Imagen, error) {
	var img *domain.Imagen
	err := firebird.RunInReadTx(ctx, r.pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, r.pool.DB)
		var err error
		img, err = scanVisitaImagen(q.QueryRowContext(ctx, findVisitaImagenSQL, imagenID.String(), visitaID.String()))
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ErrImagenNoEncontrada
		}
		return firebird.MapError(err)
	})
	if err != nil {
		return nil, err
	}
	return img, nil
}
```

`internal/visitas/app/imagenes_visita.go` (archivo nuevo, completo):

```go
package app

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// errLecturaImagenesSinConfigurar: el servicio se armó sin WithImagenes.
var errLecturaImagenesSinConfigurar = apperror.NewInternal(
	"visitas_imagenes_no_configuradas", "el servicio de visitas no tiene imágenes configuradas",
)

// ImagenVisitaAbierta es la imagen más su blob abierto. Quien la recibe DEBE
// cerrar Body.
type ImagenVisitaAbierta struct {
	Imagen      *domain.Imagen
	Body        io.ReadCloser
	ContentType string
	SizeBytes   int64
}

// ListarImagenesVisita devuelve las fotos de una visita existente (404 si la
// visita no existe; lista vacía si no tiene fotos).
func (s *Service) ListarImagenesVisita(ctx context.Context, visitaID uuid.UUID) ([]*domain.Imagen, error) {
	if s.imagenes == nil {
		return nil, errLecturaImagenesSinConfigurar
	}
	if _, err := s.repo.FindByID(ctx, visitaID); err != nil {
		return nil, err
	}
	return s.imagenes.ListImagenes(ctx, visitaID)
}

// ObtenerImagenVisita abre el blob de una imagen que pertenece a la visita.
// Una imagen de otra visita es domain.ErrImagenNoEncontrada (404).
func (s *Service) ObtenerImagenVisita(ctx context.Context, visitaID, imagenID uuid.UUID) (*ImagenVisitaAbierta, error) {
	if s.imagenes == nil || s.storage == nil {
		return nil, errLecturaImagenesSinConfigurar
	}
	img, err := s.imagenes.FindImagenDeVisita(ctx, visitaID, imagenID)
	if err != nil {
		return nil, err
	}
	obj, err := s.storage.Get(ctx, img.Storage().Key())
	if err != nil {
		var ae *apperror.Error
		if errors.As(err, &ae) {
			return nil, err
		}
		return nil, apperror.NewInternal("visita_imagen_blob_error", "no se pudo leer la imagen").WithError(err)
	}
	ct := obj.ContentType
	if ct == "" {
		ct = img.Mime()
	}
	return &ImagenVisitaAbierta{Imagen: img, Body: obj.Body, ContentType: ct, SizeBytes: obj.SizeBytes}, nil
}
```

`internal/visitas/infra/visitashttp/handlers_imagenes.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package visitashttp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// ImagenVisitaDTO tiene la misma forma que ImagenPagoDTO (contrato §6), pero
// con `descripcion` como null explícito (contrato §0) en vez de omitida.
type ImagenVisitaDTO struct {
	ID          string  `json:"id"          format:"uuid"`
	Mime        string  `json:"mime"`
	SizeBytes   int64   `json:"size_bytes"`
	Descripcion *string `json:"descripcion"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ListarImagenesVisitaInput — GET /visitas/{id}/imagenes.
type ListarImagenesVisitaInput struct {
	ID string `path:"id"`
}

// ListarImagenesVisitaOutput envuelve la lista.
type ListarImagenesVisitaOutput struct {
	Body []ImagenVisitaDTO
}

func toImagenVisitaDTO(img *domain.Imagen) ImagenVisitaDTO {
	a := img.Audit()
	return ImagenVisitaDTO{
		ID: img.ID().String(), Mime: img.Mime(), SizeBytes: img.SizeBytes(), Descripcion: img.Descripcion(),
		CreatedAt: a.CreatedAt().UTC().Format(time.RFC3339), UpdatedAt: a.UpdatedAt().UTC().Format(time.RFC3339),
	}
}

// ListarImagenesVisita lista las fotos. Lectura con cobranza:ver_pagos
// (contrato §6), el mismo permiso que GET /visitas/{id}.
func (h *Handlers) ListarImagenesVisita(ctx context.Context, in *ListarImagenesVisitaInput) (*ListarImagenesVisitaOutput, error) {
	cu, err := currentUserOrError(ctx)
	if err != nil {
		return nil, err
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		return nil, err
	}
	visitaID, err := parseVisitaID(in.ID)
	if err != nil {
		return nil, mapAppError(err)
	}
	imgs, err := h.svc.ListarImagenesVisita(ctx, visitaID)
	if err != nil {
		return nil, mapAppError(err)
	}
	out := make([]ImagenVisitaDTO, 0, len(imgs))
	for _, img := range imgs {
		out = append(out, toImagenVisitaDTO(img))
	}
	return &ListarImagenesVisitaOutput{Body: out}, nil
}

// cacheImagenVisita: los blobs nunca se editan (solo se borran), así que se
// cachean "para siempre" y en privado (mismo valor que pagos y ventas).
const cacheImagenVisita = "private, max-age=31536000, immutable"

// obtenerImagenVisita sirve el binario por chi (no Huma): GET
// /visitas/{id}/imagenes/{img_id}, con ETag y 304 si If-None-Match coincide.
func (h *Handlers) obtenerImagenVisita(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		writePlainErrorVisitas(w, http.StatusUnauthorized, "no_autenticado", "no autenticado")
		return
	}
	if err := requirePerm(cu, auth.PermCobranzaVerPagos); err != nil {
		writePlainErrorVisitas(w, http.StatusForbidden, "permiso_denegado", "permiso denegado")
		return
	}
	visitaID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writePlainErrorVisitas(w, http.StatusBadRequest, "visita_id_invalido", "el id de la visita no es un UUID válido")
		return
	}
	imagenID, err := uuid.Parse(chi.URLParam(r, "img_id"))
	if err != nil {
		writePlainErrorVisitas(w, http.StatusBadRequest, "imagen_id_invalida", "el id de la imagen no es un UUID válido")
		return
	}
	res, err := h.svc.ObtenerImagenVisita(ctx, visitaID, imagenID)
	if err != nil {
		var ae *apperror.Error
		if errors.As(err, &ae) {
			writePlainErrorVisitas(w, ae.Kind.HTTPStatus(), ae.Code, ae.Message)
			return
		}
		writePlainErrorVisitas(w, http.StatusInternalServerError, "internal_error", "ocurrió un error interno")
		return
	}
	defer func() { _ = res.Body.Close() }()

	etag := `"` + res.Imagen.ID().String() + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", cacheImagenVisita)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", res.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(res.SizeBytes, 10))
	if _, err := io.Copy(w, res.Body); err != nil {
		slog.WarnContext(ctx, "visitas.imagen_stream_failed", "visita_id", visitaID, "imagen_id", imagenID, "error", err)
	}
}

func mountImagenesVisita(r chi.Router, h *Handlers) {
	r.Get("/visitas/{id}/imagenes/{img_id}", h.obtenerImagenVisita)
}

var _ func(context.Context, *ListarImagenesVisitaInput) (*ListarImagenesVisitaOutput, error) = (*Handlers)(nil).ListarImagenesVisita
```

`internal/visitas/infra/visitashttp/routes.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/visitas/infra/visitashttp/routes.go b/internal/visitas/infra/visitashttp/routes.go
index 41a53ae..cb40b8f 100644
--- a/internal/visitas/infra/visitashttp/routes.go
+++ b/internal/visitas/infra/visitashttp/routes.go
@@ -93,6 +93,7 @@ func MountRouter(r chi.Router, svc *visitasapp.Service, opts ...MountOption) hum
 	api := humachi.New(r, humaCfg)
 	handlers := NewHandlers(svc)
 	registerOperations(api, handlers)
+	mountImagenesVisita(r, handlers)
 	describeCrearVisitaBody(api)
 	// SSE push endpoint — raw chi, because Huma has no streaming response
 	// model. Registered unconditionally when a bus is wired: the feature flag
@@ -215,6 +216,16 @@ func registerOperations(api huma.API, h *Handlers) {
 		Security:    security,
 	}, h.ObtenerVisita)
 
+	huma.Register(api, huma.Operation{
+		OperationID: "listar-imagenes-visita",
+		Method:      http.MethodGet,
+		Path:        "/visitas/{id}/imagenes",
+		Summary:     "Listar fotos de una visita",
+		Description: "Devuelve las fotos de la visita (misma forma que las de pagos). El binario se pide en /visitas/{id}/imagenes/{img_id}.",
+		Tags:        tags,
+		Security:    security,
+	}, h.ListarImagenesVisita)
+
 	huma.Register(api, huma.Operation{
 		OperationID: "visitas-existentes-por-ids",
 		Method:      http.MethodGet,
```


- [ ] **Step 4: Correr y ver que pasan**

Run: `go test -count=1 ./internal/visitas/... && go test -count=1 -p 1 -run TestE2E_VisitasImagenes ./internal/visitas/infra/visitasfb/`
Expected: PASS.

- [ ] **Step 5: Control positivo**

Quitar `AND VISITA_ID = ?` de `findVisitaImagenSQL` (y su argumento) → falla `TestE2E_VisitasImagenes_ListYPertenencia`. Restaurar.

- [ ] **Step 6: Lint y commit**

```bash
golangci-lint run ./internal/visitas/...
git add internal/visitas
git commit -m "feat(visitas): fotos de una visita, lista y binario con verificación de pertenencia"
```

### Tarea C3: `pago_recibido_id` y `num_fotos` en el detalle de pago de Clientes

**Files:**
- Modify: `internal/clientes/infra/clientesfb/queries.go`, `rowmappers.go`
- Modify: `internal/clientes/ports/outbound/repos.go` (`PagoDetalle.PagoRecibidoID`, `NumFotos`)
- Modify: `internal/clientes/infra/clienteshttp/dto.go`, `dto_mapper.go`
- Test: `internal/clientes/infra/clientesfb/pago_detalle_fotos_integration_test.go`, `internal/clientes/infra/clienteshttp/dto_pago_detalle_internal_test.go`

**Interfaces:**
- Produces: `GET /v2/clientes/{id}/pagos/{doctoCcId}` agrega `"pago_recibido_id": "uuid" | null` y `"num_fotos": 0` (contrato §6). Por subconsulta escalar `MIN(...)` sobre `MSP_PAGOS_RECIBIDOS.DOCTO_CC_ID` (índice `IDX_MSP_PAGOS_RECIBIDOS_DOCTO_CC` existe en dev; M1 lo confirma en producción): sin fan-out aunque haya duplicados (M7 los cuenta).

- [ ] **Step 1: Pruebas que fallan**

`internal/clientes/infra/clientesfb/pago_detalle_fotos_integration_test.go` (archivo nuevo, completo):

```go
package clientesfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/infra/clientesfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/platform/microsipseed"
)

// TestClientesRepo_ObtenerPagoDetalle_PagoRecibidoYFotos: un pago que pasó por
// la app expone su UUID de MSP_PAGOS_RECIBIDOS y cuántas fotos tiene (spec
// desempeño §3.2.3); uno de oficina, null y 0.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestClientesRepo_ObtenerPagoDetalle_PagoRecibidoYFotos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)
	repo := clientesfb.NewClientesRepo(pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		q := firebird.GetQuerier(ctx, pool.DB)
		microsipseed.RequiereConceptos(t, q, conceptoCobranza)
		cliente := microsipseed.Cliente(t, q, "PAGO CON FOTOS PRUEBA Ñ")
		venta := microsipseed.VentaCredito(t, q, cliente, microsipseed.OpcionesVenta{})
		deLaApp := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza, Importe: decimal.NewFromInt(150),
		})
		deOficina := microsipseed.AbonoAplicado(t, q, venta, microsipseed.OpcionesAbono{
			ConceptoCCID: conceptoCobranza, Importe: decimal.NewFromInt(100),
		})

		pagoID := uuid.New()
		now := firebird.ToWallClock(time.Now().UTC())
		_, err := q.ExecContext(ctx,
			`INSERT INTO MSP_PAGOS_RECIBIDOS (ID, FECHA, DOCTO_CC_ID, ESTADO, INTENTOS, RECEIVED_AT, UPDATED_AT)
			 VALUES (?, ?, ?, 'A', 0, ?, ?)`,
			pagoID.String(), now, deLaApp.DoctoCCID, now, now)
		require.NoError(t, err)
		for range 2 {
			img := uuid.New()
			_, err = q.ExecContext(ctx,
				`INSERT INTO MSP_PAGOS_IMAGENES (ID, PAGO_ID, STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES,
				  CREATED_AT, UPDATED_AT, CREATED_BY, UPDATED_BY)
				 VALUES (?, ?, 'FILESYSTEM', ?, 'image/jpeg', 1024, ?, ?, ?, ?)`,
				img.String(), pagoID.String(), "pagos/"+pagoID.String()+"/"+img.String()+".jpg",
				now, now, pagoID.String(), pagoID.String())
			require.NoError(t, err)
		}

		detalle, err := repo.ObtenerPagoDetalle(ctx, deLaApp.DoctoCCID)
		require.NoError(t, err)
		assert.Equal(t, pagoID.String(), detalle.PagoRecibidoID)
		assert.Equal(t, 2, detalle.NumFotos)
		assert.Equal(t, "app", detalle.Origen)

		oficina, err := repo.ObtenerPagoDetalle(ctx, deOficina.DoctoCCID)
		require.NoError(t, err)
		assert.Empty(t, oficina.PagoRecibidoID, "sin fila en MSP_PAGOS_RECIBIDOS")
		assert.Equal(t, 0, oficina.NumFotos)
	})
}
```

`internal/clientes/infra/clienteshttp/dto_pago_detalle_internal_test.go` (archivo nuevo, completo):

```go
package clienteshttp

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/clientes/ports/outbound"
)

// Contrato §6: pago_recibido_id es "uuid" | null (nunca omitido) y num_fotos
// siempre presente.
func TestToPagoDetalleDTO_PagoRecibidoIDYNumFotos(t *testing.T) {
	t.Parallel()
	conApp := toPagoDetalleDTO(outbound.PagoDetalle{PagoRecibidoID: "7a1f39e4-0000-4000-8000-000000000001", NumFotos: 2})
	b, err := json.Marshal(conApp)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"pago_recibido_id":"7a1f39e4-0000-4000-8000-000000000001"`)
	assert.Contains(t, string(b), `"num_fotos":2`)

	oficina := toPagoDetalleDTO(outbound.PagoDetalle{})
	b, err = json.Marshal(oficina)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"pago_recibido_id":null`)
	assert.Contains(t, string(b), `"num_fotos":0`)
}
```


- [ ] **Step 2: Correr y ver que fallan**

Run: `go test ./internal/clientes/infra/clienteshttp/`
Expected: FAIL de compilación (`unknown field PagoRecibidoID in struct literal of type outbound.PagoDetalle`).

- [ ] **Step 3: Implementación**

`internal/clientes/infra/clientesfb/queries.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/clientes/infra/clientesfb/queries.go b/internal/clientes/infra/clientesfb/queries.go
index d835c06..78e3268 100644
--- a/internal/clientes/infra/clientesfb/queries.go
+++ b/internal/clientes/infra/clientesfb/queries.go
@@ -735,6 +735,12 @@ ORDER BY pago.FECHA`
 // while COBRADORES.NOMBRE is CHARACTER SET ISO8859_1 (sql.NullString — Firebird
 // already transliterated it). MSP_PAGOS_RECIBIDOS columns are UTF8.
 //
+// pago_recibido_id / num_fotos (spec desempeño §3.2.3): el UUID de
+// MSP_PAGOS_RECIBIDOS que la app generó y cuántas fotos tiene, por SUBCONSULTA
+// ESCALAR con MIN(...) — nunca fan-out aunque hubiera dos filas con el mismo
+// DOCTO_CC_ID. Pagos de oficina o legado: NULL y 0. MSP_PAGOS_RECIBIDOS.ID es
+// CHARACTER SET NONE con UUID ASCII → sql.NullString.
+//
 // GPS: native DOCTOS_CC has no LAT/LON columns; GPS comes only from
 // MSP_PAGOS_RECIBIDOS.LAT/LON (VARCHAR(20), ASCII decimal text).
 //
@@ -803,7 +809,12 @@ SELECT
   r.LAT                                         AS MSP_LAT,
   r.LON                                         AS MSP_LON,
   r.RECEIVED_AT                                 AS MSP_RECEIVED_AT,
-  r.APLICADO_AT                                 AS MSP_APLICADO_AT
+  r.APLICADO_AT                                 AS MSP_APLICADO_AT,
+  (SELECT MIN(pr2.ID) FROM MSP_PAGOS_RECIBIDOS pr2
+    WHERE pr2.DOCTO_CC_ID = p.DOCTO_CC_ID)      AS MSP_PAGO_RECIBIDO_ID,
+  (SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES im
+    WHERE im.PAGO_ID = (SELECT MIN(pr3.ID) FROM MSP_PAGOS_RECIBIDOS pr3
+                         WHERE pr3.DOCTO_CC_ID = p.DOCTO_CC_ID)) AS NUM_FOTOS
 FROM DOCTOS_CC p
 JOIN IMPORTES_DOCTOS_CC i ON i.DOCTO_CC_ID = p.DOCTO_CC_ID
   AND i.TIPO_IMPTE = 'R'
```

`internal/clientes/infra/clientesfb/rowmappers.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/clientes/infra/clientesfb/rowmappers.go b/internal/clientes/infra/clientesfb/rowmappers.go
index 125a9f1..0d14f03 100644
--- a/internal/clientes/infra/clientesfb/rowmappers.go
+++ b/internal/clientes/infra/clientesfb/rowmappers.go
@@ -756,6 +756,8 @@ type pagoDetalleRowRaw struct {
 	mspLon          sql.NullString
 	mspRecibidoAt   any
 	mspAplicadoAt   any
+	pagoRecibidoID  sql.NullString // MSP_PAGOS_RECIBIDOS.ID — NONE, UUID ASCII
+	numFotos        int
 }
 
 func (r *pagoDetalleRowRaw) scanFrom(s scannable) error {
@@ -785,6 +787,8 @@ func (r *pagoDetalleRowRaw) scanFrom(s scannable) error {
 		&r.mspLon,
 		&r.mspRecibidoAt,
 		&r.mspAplicadoAt,
+		&r.pagoRecibidoID,
+		&r.numFotos,
 	)
 }
 
@@ -914,6 +918,8 @@ func (r *pagoDetalleRowRaw) assemble() (outbound.PagoDetalle, error) {
 		RecibidoAt:     enr.recibidoAt,
 		AplicadoAt:     enr.aplicadoAt,
 		Origen:         enr.origen,
+		PagoRecibidoID: strings.TrimSpace(r.pagoRecibidoID.String),
+		NumFotos:       r.numFotos,
 	}, nil
 }
```

`internal/clientes/ports/outbound/repos.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/clientes/ports/outbound/repos.go b/internal/clientes/ports/outbound/repos.go
index 1590857..ec861fc 100644
--- a/internal/clientes/ports/outbound/repos.go
+++ b/internal/clientes/ports/outbound/repos.go
@@ -173,6 +173,11 @@ type PagoDetalle struct {
 	RecibidoAt     time.Time
 	AplicadoAt     time.Time
 	Origen         string // "app" | "microsip"
+	// PagoRecibidoID es el UUID de MSP_PAGOS_RECIBIDOS; "" cuando el pago no
+	// pasó por la app (oficina, legado anterior a 2.18.0).
+	PagoRecibidoID string
+	// NumFotos: comprobantes en MSP_PAGOS_IMAGENES de ese pago.
+	NumFotos int
 }
 
 // ClientesRepo is the primary read port for the clientes hub. Each method maps
```

`internal/clientes/infra/clienteshttp/dto.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/clientes/infra/clienteshttp/dto.go b/internal/clientes/infra/clienteshttp/dto.go
index f15c401..a02e372 100644
--- a/internal/clientes/infra/clienteshttp/dto.go
+++ b/internal/clientes/infra/clienteshttp/dto.go
@@ -428,6 +428,8 @@ type PagoDetalleDTO struct {
 	RecibidoAt     string  `json:"recibido_at,omitempty"  format:"date-time" doc:"Timestamp de recepción vía app (RFC3339 UTC); ausente en pagos nativos"`
 	AplicadoAt     string  `json:"aplicado_at,omitempty"  format:"date-time" doc:"Timestamp de aplicación vía app (RFC3339 UTC); ausente en pagos nativos"`
 	Origen         string  `json:"origen"                doc:"Origen del dato: 'app' cuando MSP_PAGOS_RECIBIDOS tiene registro; 'microsip' en caso contrario"`
+	PagoRecibidoID *string `json:"pago_recibido_id"      doc:"UUID del pago en MSP_PAGOS_RECIBIDOS (sus fotos: /v2/cobranza/pagos/{id}/imagenes); null si no pasó por la app"`
+	NumFotos       int     `json:"num_fotos"             doc:"Número de comprobantes del pago; 0 sin fotos o sin pago de la app"`
 }
 
 // RefrescarBusquedaOutput is the response for POST /clientes/_search/refresh.
```

`internal/clientes/infra/clienteshttp/dto_mapper.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/clientes/infra/clienteshttp/dto_mapper.go b/internal/clientes/infra/clienteshttp/dto_mapper.go
index 19ab349..0c0f331 100644
--- a/internal/clientes/infra/clienteshttp/dto_mapper.go
+++ b/internal/clientes/infra/clienteshttp/dto_mapper.go
@@ -337,6 +337,11 @@ func toPagoDetalleDTO(d outbound.PagoDetalle) PagoDetalleDTO {
 	if !d.AplicadoAt.IsZero() {
 		dto.AplicadoAt = formatTime(d.AplicadoAt)
 	}
+	if d.PagoRecibidoID != "" {
+		id := d.PagoRecibidoID
+		dto.PagoRecibidoID = &id
+	}
+	dto.NumFotos = d.NumFotos
 	return dto
 }
```


- [ ] **Step 4: Correr y ver que pasan**

Run: `go test -count=1 ./internal/clientes/... && go test -count=1 -p 1 -run ObtenerPagoDetalle ./internal/clientes/infra/clientesfb/`
Expected: PASS (las de `clientesfb` existentes también: `ObtenerPagoDetalle_Found` y `_Cobranza`).

- [ ] **Step 5: Lint y commit**

```bash
golangci-lint run ./internal/clientes/...
git add internal/clientes
git commit -m "feat(clientes): pago_recibido_id y num_fotos en el detalle de pago"
```

---

## Tarea F: integración — HTTP del contrato, catálogos, permiso y wiring (rama `feat/desempeno`, tras merge de A, B y C)

**Files:**
- Modify: `internal/auth/domain/permission_codes.go`, `internal/auth/domain/permission_codes_test.go`, `internal/auth/auth_contracts.go` (`desempeno:ver`)
- Create: `internal/desempeno/infra/desempenohttp/{auth,dto,mapper,handlers,routes}.go`
- Test: `internal/desempeno/infra/desempenohttp/{fakes,contrato,security}_test.go`
- Create: `cmd/api/desempeno_wiring.go`
- Modify: `cmd/api/main.go`, `cmd/api/server.go`

**Interfaces:**
- Consumes: A3/A4 (`app.NewService`, `Tablero`, `Jornada`, `Actividad`, `Cuentas` y sus tipos), B2 (`NewRutasUsuariosClient`), B3/B4 (`NewActividadesRepo`, `NewCuentasRepo`), A1 (`TratoDe`, `EtiquetaDe`, `CatalogoTiposVisita`), Tarea 0 (`CatalogoFormasCobro`).
- Produces: `GET /v2/desempeno/catalogos`, `GET /v2/desempeno/usuarios`, `GET /v2/desempeno/usuarios/{uid}/jornada`, `/actividad`, `/cuentas`; permiso `auth.PermDesempenoVer` (`"desempeno:ver"`, categoría `desempeno`). El catálogo de permisos se sincroniza solo al arrancar el API y se concede a `super_admin` (`invokeAuthCatalogSync`); a los roles de oficina se les asigna a mano (`MSP_ROLES_PERMISOS`) cuando el dueño lo decida.

- [ ] **Step 1: Permiso**

`internal/auth/domain/permission_codes.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/auth/domain/permission_codes.go b/internal/auth/domain/permission_codes.go
index 756bb29..5e41082 100644
--- a/internal/auth/domain/permission_codes.go
+++ b/internal/auth/domain/permission_codes.go
@@ -131,6 +131,10 @@ const (
 	// PermReactivacionAdministrar grants triggering a rebuild of the
 	// reactivación cohorte snapshot.
 	PermReactivacionAdministrar Permission = "reactivacion:administrar"
+
+	// PermDesempenoVer grants read access to the "Desempeño de cobradores"
+	// screen: tablero, jornada, actividad de la zona y cuentas de la semana.
+	PermDesempenoVer Permission = "desempeno:ver"
 )
 
 // PermissionMeta is a catalog entry pairing a permission code with the
@@ -157,6 +161,7 @@ const (
 	categoriaClientes      = "clientes"
 	categoriaConfig        = "configuracion"
 	categoriaReactivacion  = "reactivacion"
+	categoriaDesempeno     = "desempeno"
 )
 
 // AllPermissions returns every permission known to the auth module, sorted
@@ -216,6 +221,8 @@ func AllPermissions() []PermissionMeta {
 
 		{PermReactivacionLeer, "ver la cohorte y atribución del piloto de reactivación", categoriaReactivacion},
 		{PermReactivacionAdministrar, "reconstruir la cohorte del piloto de reactivación", categoriaReactivacion},
+
+		{PermDesempenoVer, "ver el desempeño de cobradores (tablero, jornada, actividad y cuentas)", categoriaDesempeno},
 	}
 	sort.Slice(perms, func(i, j int) bool { return perms[i].Code < perms[j].Code })
 	return perms
```

`internal/auth/domain/permission_codes_test.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/auth/domain/permission_codes_test.go b/internal/auth/domain/permission_codes_test.go
index a841562..55128e1 100644
--- a/internal/auth/domain/permission_codes_test.go
+++ b/internal/auth/domain/permission_codes_test.go
@@ -58,6 +58,7 @@ func TestAllPermissions_NonEmpty(t *testing.T) {
 		domain.PermConfigAdministrar,
 		domain.PermReactivacionLeer,
 		domain.PermReactivacionAdministrar,
+		domain.PermDesempenoVer,
 	}
 	got := make(map[domain.Permission]struct{}, len(perms))
 	for _, p := range perms {
```

`internal/auth/auth_contracts.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/internal/auth/auth_contracts.go b/internal/auth/auth_contracts.go
index 38872f3..3ba88e3 100644
--- a/internal/auth/auth_contracts.go
+++ b/internal/auth/auth_contracts.go
@@ -140,4 +140,7 @@ const (
 	PermReactivacionLeer = domain.PermReactivacionLeer
 	// PermReactivacionAdministrar — see domain.PermReactivacionAdministrar.
 	PermReactivacionAdministrar = domain.PermReactivacionAdministrar
+
+	// PermDesempenoVer — see domain.PermDesempenoVer.
+	PermDesempenoVer = domain.PermDesempenoVer
 )
```


Run: `go test ./internal/auth/...`
Expected: PASS (incluye `TestPermissionConstantsAreReExported` y `TestAllPermissions_FitFirebirdColumnWidths`).

- [ ] **Step 2: Pruebas de contrato, seguridad y errores (fallan)**

Las claves esperadas se copiaron **a mano del contrato v2**, no de los DTOs: si alguien renombra un campo del DTO, la prueba se pone roja.

`internal/desempeno/infra/desempenohttp/fakes_test.go` (archivo nuevo, completo):

```go
package desempenohttp_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp"
)

type fakeUsuarios struct {
	us  []domain.UsuarioCobranza
	err error
}

func (f fakeUsuarios) ListarUsuarios(context.Context) ([]domain.UsuarioCobranza, error) {
	return f.us, f.err
}

func (f fakeUsuarios) CobroSemana(context.Context, domain.UsuarioCobranza) (domain.CobroSemana, error) {
	pct := decimal.RequireFromString("65.9")
	return domain.CobroSemana{PctPonderado: &pct, Ventas: []domain.CobroVenta{
		{VentaID: 13785217, Parcialidad: decimal.NewFromInt(150), AbonoSemana: decimal.NewFromInt(150), Vencidas: decimal.NewFromInt(1), AplicaPonderado: true},
	}}, nil
}

type fakeActividades struct{ acts []domain.Actividad }

func (f fakeActividades) ActividadesZona(_ context.Context, _ int, desde, hasta time.Time) ([]domain.Actividad, error) {
	var out []domain.Actividad
	for _, a := range f.acts {
		if !a.Hora.Before(desde) && !a.Hora.After(hasta) {
			out = append(out, a)
		}
	}
	return out, nil
}

type fakeCuentas struct{ cs []domain.CuentaUniverso }

func (f fakeCuentas) UniversoSemana(context.Context, int, time.Time) ([]domain.CuentaUniverso, error) {
	return f.cs, nil
}

type reloj struct{ t time.Time }

func (r reloj) Now() time.Time { return r.t }

func f64(v float64) *float64 { return &v }

// Hoy: martes 14 de oct de 2026, 16:40 CDMX (22:40Z). Semana desde el jueves 9 08:46 CDMX.
var (
	ahora  = time.Date(2026, 10, 14, 22, 40, 0, 0, time.UTC)
	inicio = time.Date(2026, 10, 9, 14, 46, 0, 0, time.UTC)
)

func servicio(t *testing.T) *desempenoapp.Service {
	t.Helper()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	ini := inicio
	juan := domain.UsuarioCobranza{
		UID: "abc123", Nombre: "Juan Carlos Méndez", Email: "juan.mendez@muebleriamsp.mx",
		CobradorID: 11502, ZonaID: 21563, ZonaNombre: "RUTA 21", InicioSemana: &ini,
	}
	recibido := time.Date(2026, 10, 14, 16, 31, 5, 0, time.UTC)
	acts := []domain.Actividad{
		{
			ID: "pago:15082369", Tipo: domain.TipoPago, Hora: time.Date(2026, 10, 14, 16, 31, 0, 0, time.UTC),
			RecibidoAt: &recibido, ClienteID: 1544855, ClienteNombre: "JUANA ANDRADE LECHUGA",
			VentaID: 13785217, DoctoPVID: 9921, Folio: "Z1677035", NumFotos: 2, Lat: f64(18.4617), Lng: f64(-97.3928),
			RegistradoPor: domain.RegistradoPor{UsuarioUID: "abc123", CobradorID: 11502, Origen: domain.OrigenApp},
			Pago: &domain.DatosPago{
				Importe: decimal.NewFromInt(150), FormaCobroID: 157, Aplicado: true, DoctoCCID: 15082300,
				PagoRecibidoID: "7a1f39e4-0000-4000-8000-000000000001",
			},
		},
		{
			ID: "visita:412cc2fc-0000-4000-8000-000000000002", Tipo: domain.TipoVisita,
			Hora: time.Date(2026, 10, 14, 15, 0, 0, 0, time.UTC), ClienteID: 1544900, ClienteNombre: "ROSARIO VILLA",
			RegistradoPor: domain.RegistradoPor{CobradorID: 11502, Nombre: "JUAN CARLOS", Origen: domain.OrigenApp},
			Visita:        &domain.DatosVisita{VisitaID: "412cc2fc-0000-4000-8000-000000000002", TipoVisita: "No se encontraba", Nota: "Vecina dice que regresa el lunes"},
		},
	}
	cuentas := []domain.CuentaUniverso{{
		VentaID: 13785217, DoctoPVID: 9921, Folio: "Z1677035", ClienteID: 1544855, ClienteNombre: "JUANA ANDRADE LECHUGA",
		Producto: "ROPERO LABRADO CALIFORNIA VINO", Parcialidad: decimal.NewFromInt(150), Saldo: decimal.NewFromInt(16650),
		FechaCargo: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
	}}
	return desempenoapp.NewService(fakeUsuarios{us: []domain.UsuarioCobranza{juan}}, fakeActividades{acts}, fakeCuentas{cuentas}, reloj{ahora}, loc)
}

func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(auth.PlantCurrentUser(r.Context(), cu)))
		})
	}
}

func usuarioCon(perms ...auth.Permission) auth.CurrentUser {
	ps := make([]string, 0, len(perms))
	for _, p := range perms {
		ps = append(ps, string(p))
	}
	return auth.CurrentUser{
		ID: uuid.New(), FirebaseUID: "fb-araceli", Email: "araceli.soto@muebleriamsp.mx",
		Nombre: "Araceli Soto", Permisos: ps,
	}
}

func router(t *testing.T, cu *auth.CurrentUser) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	if cu != nil {
		r.Use(planter(*cu))
	}
	desempenohttp.MountRouter(r, servicio(t))
	return r
}
```

`internal/desempeno/infra/desempenohttp/contrato_test.go` (archivo nuevo, completo):

```go
package desempenohttp_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp"
)

// Claves de cada objeto del contrato (docs/superpowers/specs/
// 2026-10-14-desempeno-contratos.md), copiadas A MANO del documento y NO de
// los DTOs: si alguien renombra un campo del DTO, esta prueba se pone roja.
var (
	clavesActividad     = []string{"id", "tipo", "hora", "recibido_at", "cliente_id", "cliente_nombre", "venta_id", "folio", "registrado_por", "lat", "lng", "con_gps", "num_fotos", "docto_pv_id", "captura_tardia", "pago", "visita"}
	clavesRegistradoPor = []string{"usuario_uid", "cobrador_id", "nombre", "origen"}
	clavesPago          = []string{"importe", "forma_cobro", "forma_cobro_id", "aplicado", "rechazado", "docto_cc_id", "pago_recibido_id"}
	clavesVisita        = []string{"visita_id", "tipo_visita", "nota", "estado", "trato", "etiqueta"}
	clavesLinea         = []string{"dia", "puntos", "tramos", "por_hora", "pausas"}
	clavesPunto         = []string{"id", "tipo", "hora", "cliente_nombre", "importe", "resultado", "num_fotos"}
	clavesTramo         = []string{"desde", "hasta", "pagos", "visitas", "condonaciones", "clientes"}
	clavesHora          = []string{"hora", "pagos", "visitas", "condonaciones", "importe"}
	clavesPausa         = []string{"desde", "hasta", "minutos", "anterior_id", "siguiente_id", "en_curso"}
	clavesSenal         = []string{"tipo", "actividad_ids", "desde", "hasta", "detalle", "regla"}
	clavesTablero       = []string{"generado_at", "periodo", "pausa_min", "usuarios"}
	clavesUsuario       = []string{
		"uid", "nombre", "email", "zona_id", "zona_nombre", "configurado", "motivo_no_configurado", "inicio_semana",
		"ventas_universo", "estado_ahora", "estado_desde", "ultima", "primera_actividad", "ultima_actividad",
		"minutos_activos", "pausas", "pagos", "pagos_por_forma", "condonaciones", "no_aplicados", "visitas",
		"cobertura", "cobertura_por_segmento", "cobro", "lineas",
	}
	clavesJornada = []string{"uid", "fecha", "pausa_min", "generado_at", "actividades", "linea", "senales", "resumen"}
	clavesResumen = []string{"primera_actividad", "ultima_actividad", "minutos_activos", "pausas", "pagos", "visitas", "condonaciones", "clientes"}
	clavesPagina  = []string{"items", "siguiente_cursor", "totales"}
	clavesTotales = []string{"n", "importe", "por_tipo"}
	clavesCuentas = []string{"inicio_semana", "conteos", "cuentas"}
	clavesConteos = []string{"sin_visitar", "volver", "despues", "pagados", "sin_nada", "total"}
	clavesCuenta  = []string{"venta_id", "docto_pv_id", "folio", "cliente_id", "cliente_nombre", "producto", "estado", "trato", "etiqueta", "detalle", "segmento", "con_algo", "determinado_por", "ultima_actividad", "saldo", "parcialidad", "abono_semana", "num_atrasos", "nueva"}
)

func get(t *testing.T, path string) map[string]any {
	t.Helper()
	cu := usuarioCon(auth.PermDesempenoVer)
	rec := httptest.NewRecorder()
	router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m))
	return m
}

func claves(t *testing.T, v any, esperadas []string, donde string) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "%s no es objeto: %v", donde, v)
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	want := append([]string(nil), esperadas...)
	sort.Strings(got)
	sort.Strings(want)
	assert.Equal(t, want, got, "claves de %s", donde)
	return m
}

func primero(t *testing.T, v any, donde string) any {
	t.Helper()
	l, ok := v.([]any)
	require.True(t, ok, "%s no es lista", donde)
	require.NotEmpty(t, l, "%s vacía: el escenario debe llenarla", donde)
	return l[0]
}

func revisarActividad(t *testing.T, v any, donde string) {
	t.Helper()
	a := claves(t, v, clavesActividad, donde)
	claves(t, a["registrado_por"], clavesRegistradoPor, donde+".registrado_por")
	if a["pago"] != nil {
		claves(t, a["pago"], clavesPago, donde+".pago")
	}
	if a["visita"] != nil {
		claves(t, a["visita"], clavesVisita, donde+".visita")
	}
}

func revisarLinea(t *testing.T, v any, donde string) {
	t.Helper()
	l := claves(t, v, clavesLinea, donde)
	claves(t, primero(t, l["puntos"], donde+".puntos"), clavesPunto, donde+".puntos[0]")
	claves(t, primero(t, l["tramos"], donde+".tramos"), clavesTramo, donde+".tramos[0]")
	claves(t, primero(t, l["por_hora"], donde+".por_hora"), clavesHora, donde+".por_hora[0]")
	claves(t, primero(t, l["pausas"], donde+".pausas"), clavesPausa, donde+".pausas[0]")
}

func TestContrato_Tablero(t *testing.T) {
	t.Parallel()
	m := claves(t, get(t, "/desempeno/usuarios"), clavesTablero, "tablero")
	u := claves(t, primero(t, m["usuarios"], "usuarios"), clavesUsuario, "usuarios[0]")
	claves(t, u["ultima"], []string{"id", "tipo", "hora", "cliente_nombre"}, "ultima")
	claves(t, u["pausas"], []string{"n", "minutos", "en_curso"}, "pausas")
	claves(t, u["pagos"], []string{"n", "importe"}, "pagos")
	claves(t, u["pagos_por_forma"], []string{"efectivo", "transferencia", "cheque"}, "pagos_por_forma")
	claves(t, u["cobertura"], []string{"con_algo", "total"}, "cobertura")
	claves(t, u["cobertura_por_segmento"], []string{"sin_visitar", "volver", "despues", "pagados", "sin_nada"}, "cobertura_por_segmento")
	claves(t, u["cobro"], []string{"cobrado", "esperado", "falta", "pct"}, "cobro")
	revisarLinea(t, primero(t, u["lineas"], "lineas"), "lineas[0]")

	assert.Equal(t, "hoy", m["periodo"])
	assert.InDelta(t, 45, m["pausa_min"], 0)
	assert.Equal(t, "2026-10-14T22:40:00Z", m["generado_at"])
	assert.Equal(t, "150.00", u["pagos"].(map[string]any)["importe"], "dinero como string con 2 decimales")
	assert.Equal(t, "65.90", u["cobro"].(map[string]any)["pct"])
	assert.Equal(t, "2026-10-09T14:46:00Z", u["inicio_semana"])
	assert.Nil(t, u["motivo_no_configurado"], "null explícito, no omitido")
}

func TestContrato_Jornada(t *testing.T) {
	t.Parallel()
	m := claves(t, get(t, "/desempeno/usuarios/abc123/jornada?fecha=2026-10-14"), clavesJornada, "jornada")
	revisarActividad(t, primero(t, m["actividades"], "actividades"), "actividades[0]")
	revisarLinea(t, m["linea"], "linea")
	claves(t, primero(t, m["senales"], "senales"), clavesSenal, "senales[0]")
	claves(t, m["resumen"], clavesResumen, "resumen")
	assert.Equal(t, "2026-10-14", m["fecha"])
	assert.Equal(t, "2026-10-14T22:40:00Z", m["generado_at"])
	var visita map[string]any
	for _, a := range m["actividades"].([]any) {
		if v := a.(map[string]any)["visita"]; v != nil {
			visita = v.(map[string]any)
		}
	}
	require.NotNil(t, visita, "el escenario trae una visita")
	assert.Equal(t, "no_estaba", visita["estado"])
	assert.Equal(t, "nadie", visita["trato"])
	assert.Equal(t, "No estaba", visita["etiqueta"], "misma etiqueta que el teléfono")
	puntos := m["linea"].(map[string]any)["puntos"].([]any)
	pv, pp := puntos[0].(map[string]any), puntos[1].(map[string]any)
	assert.Nil(t, pv["importe"], "importe null en visitas")
	assert.Equal(t, "No se encontraba", pv["resultado"])
	assert.Equal(t, "150.00", pp["importe"])
	assert.Nil(t, pp["resultado"])
	assert.InDelta(t, 2, pp["num_fotos"], 0)
}

func TestContrato_Actividad(t *testing.T) {
	t.Parallel()
	m := claves(t, get(t, "/desempeno/usuarios/abc123/actividad?tipo=pago&tipo=visita&limit=1"), clavesPagina, "actividad")
	items := m["items"].([]any)
	require.Len(t, items, 1, "limit=1")
	revisarActividad(t, items[0], "items[0]")
	assert.NotNil(t, m["siguiente_cursor"], "hay una segunda página")
	tot := claves(t, m["totales"], clavesTotales, "totales")
	claves(t, tot["por_tipo"], []string{"pago", "visita", "condonacion"}, "totales.por_tipo")
	assert.InDelta(t, 2, tot["n"], 0, "tipo es repetible (explode): pago Y visita")

	a := items[0].(map[string]any)
	assert.Equal(t, "pago:15082369", a["id"])
	assert.Equal(t, "2026-10-14T16:31:05Z", a["recibido_at"])
	assert.Equal(t, "efectivo", a["pago"].(map[string]any)["forma_cobro"])
	assert.Equal(t, "Juan Carlos Méndez", a["registrado_por"].(map[string]any)["nombre"])
	assert.InDelta(t, 9921, a["docto_pv_id"], 0)
	assert.Equal(t, false, a["captura_tardia"], "5 s entre captura y llegada")
}

func TestContrato_Catalogos(t *testing.T) {
	t.Parallel()
	m := claves(t, get(t, "/desempeno/catalogos"), []string{"formas_cobro", "tipos_visita"}, "catalogos")
	claves(t, primero(t, m["formas_cobro"], "formas_cobro"), []string{"codigo", "etiqueta"}, "formas_cobro[0]")
	tv := claves(t, primero(t, m["tipos_visita"], "tipos_visita"), []string{"literal", "estado", "grupo"}, "tipos_visita[0]")
	assert.Equal(t, "No se encontraba", tv["literal"])
	assert.Equal(t, "no_estaba", tv["estado"])
	assert.Equal(t, "No estaba", tv["grupo"])
	assert.Len(t, m["formas_cobro"], 5)
	assert.Len(t, m["tipos_visita"], 14)
}

func TestContrato_Cuentas(t *testing.T) {
	t.Parallel()
	m := claves(t, get(t, "/desempeno/usuarios/abc123/cuentas"), clavesCuentas, "cuentas")
	claves(t, m["conteos"], clavesConteos, "conteos")
	c := claves(t, primero(t, m["cuentas"], "cuentas"), clavesCuenta, "cuentas[0]")
	claves(t, c["ultima_actividad"], []string{"id", "tipo", "hora"}, "cuentas[0].ultima_actividad")
	assert.Equal(t, "pago", c["estado"])
	assert.Equal(t, "pagado", c["trato"])
	assert.Equal(t, "Pagó esta semana", c["etiqueta"])
	assert.Equal(t, "pagados", c["segmento"])
	assert.Equal(t, "16650.00", c["saldo"])
	assert.InDelta(t, 1, c["num_atrasos"], 0)
}

// Regla §0 del contrato: null explícito, nunca se omite el campo.
func TestDTOs_SinOmitempty(t *testing.T) {
	t.Parallel()
	tipos := []any{
		desempenohttp.TableroDTO{},
		desempenohttp.UsuarioTableroDTO{},
		desempenohttp.JornadaDTO{},
		desempenohttp.ResumenJornadaDTO{},
		desempenohttp.ActividadDTO{},
		desempenohttp.PagoDTO{},
		desempenohttp.VisitaDTO{},
		desempenohttp.RegistradoPorDTO{},
		desempenohttp.LineaDelDiaDTO{},
		desempenohttp.TramoDTO{},
		desempenohttp.HoraDTO{},
		desempenohttp.PausaDTO{},
		desempenohttp.SenalDTO{},
		desempenohttp.PaginaActividadDTO{},
		desempenohttp.CuentasDTO{},
		desempenohttp.CuentaDTO{},
		desempenohttp.PuntoDTO{},
		desempenohttp.CobroDTO{},
		desempenohttp.CatalogosDTO{},
	}
	for _, v := range tipos {
		rt := reflect.TypeOf(v)
		for i := range rt.NumField() {
			tag := rt.Field(i).Tag.Get("json")
			assert.NotContains(t, tag, "omitempty", "%s.%s", rt.Name(), rt.Field(i).Name)
			assert.NotEmpty(t, strings.Split(tag, ",")[0], "%s.%s sin nombre JSON", rt.Name(), rt.Field(i).Name)
		}
	}
}
```

`internal/desempeno/infra/desempenohttp/security_test.go` (archivo nuevo, completo):

```go
package desempenohttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/abdimuy/msp-api/internal/auth"
)

var rutas = []string{
	"/desempeno/catalogos",
	"/desempeno/usuarios",
	"/desempeno/usuarios/abc123/jornada",
	"/desempeno/usuarios/abc123/actividad",
	"/desempeno/usuarios/abc123/cuentas",
}

func TestSecurity_SinUsuarioEs401(t *testing.T) {
	t.Parallel()
	for _, p := range rutas {
		rec := httptest.NewRecorder()
		router(t, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, p)
	}
}

// Con TODOS los permisos menos desempeno:ver → 403 (control: cobranza y rutas
// no abren esta pantalla).
func TestSecurity_SinPermisoEs403(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermCobranzaVerPagos, auth.PermCobranzaVerSaldos, auth.PermRutasLeer, auth.PermClientesLeer)
	for _, p := range rutas {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusForbidden, rec.Code, p)
	}
}

func TestSecurity_ConPermisoPasa(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	for _, p := range rutas {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "%s: %s", p, rec.Body.String())
	}
}

func TestErrores_CodigosDelContrato(t *testing.T) {
	t.Parallel()
	cu := usuarioCon(auth.PermDesempenoVer)
	casos := []struct {
		path   string
		status int
		code   string
	}{
		{"/desempeno/usuarios?pausa_min=4", http.StatusUnprocessableEntity, "code=desempeno_parametro_invalido"},
		{"/desempeno/usuarios?periodo=mes", http.StatusUnprocessableEntity, "code=desempeno_parametro_invalido"},
		{"/desempeno/usuarios/nadie/jornada", http.StatusNotFound, "code=desempeno_usuario_not_found"},
		{"/desempeno/usuarios/abc123/actividad?limit=501", http.StatusUnprocessableEntity, "code=desempeno_parametro_invalido"},
		{"/desempeno/usuarios/abc123/cuentas?segmento=todos", http.StatusUnprocessableEntity, "code=desempeno_parametro_invalido"},
		{"/desempeno/usuarios/1%20OR%201=1/cuentas", http.StatusNotFound, "code=desempeno_usuario_not_found"},
	}
	for _, c := range casos {
		rec := httptest.NewRecorder()
		router(t, &cu).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		assert.Equal(t, c.status, rec.Code, c.path)
		assert.Contains(t, rec.Body.String(), c.code, c.path)
	}
}
```


Run: `go test ./internal/desempeno/infra/desempenohttp/`
Expected: FAIL de compilación (`undefined: desempenohttp.MountRouter`).

- [ ] **Step 3: DTOs, mapeo, handlers y rutas**

`internal/desempeno/infra/desempenohttp/auth.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// autorizar exige usuario autenticado (401) con desempeno:ver (403). El
// chequeo vive en el handler porque depguard prohíbe importar authhttp
// (HUMA_WIRING.md).
func autorizar(ctx context.Context) error {
	cu, ok := auth.CurrentUserFromContext(ctx)
	if !ok {
		return huma.Error401Unauthorized("no autenticado")
	}
	for _, p := range cu.Permisos {
		if p == string(auth.PermDesempenoVer) {
			return nil
		}
	}
	return huma.NewError(http.StatusForbidden, "permiso denegado", &huma.ErrorDetail{
		Message:  "el principal no tiene el permiso requerido",
		Location: "header.authorization",
		Value:    string(auth.PermDesempenoVer),
	})
}

// mapAppError traduce apperror al formato Huma con "code=<código>" en el
// detalle, igual que rutas. Lo que no es apperror es 500.
func mapAppError(err error) error {
	var ae *apperror.Error
	if !errors.As(err, &ae) {
		return huma.NewError(http.StatusInternalServerError, "ocurrió un error interno",
			&huma.ErrorDetail{Message: err.Error()})
	}
	return huma.NewError(ae.Kind.HTTPStatus(), ae.Message, &huma.ErrorDetail{Message: "code=" + ae.Code})
}
```

`internal/desempeno/infra/desempenohttp/dto.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

// DTOs del contrato docs/superpowers/specs/2026-10-14-desempeno-contratos.md,
// byte a byte. Regla §0: null explícito, NUNCA omitempty (lo vigila
// TestDTOs_SinOmitempty). Dinero: string con 2 decimales. Horas: RFC3339 UTC.

// RegistradoPorDTO — contrato §1 RegistradoPor.
type RegistradoPorDTO struct {
	UsuarioUID *string `json:"usuario_uid"`
	CobradorID *int    `json:"cobrador_id"`
	Nombre     string  `json:"nombre"`
	Origen     string  `json:"origen"`
}

// PagoDTO — contrato §1 Actividad.pago.
type PagoDTO struct {
	Importe        string  `json:"importe"`
	FormaCobro     string  `json:"forma_cobro"`
	FormaCobroID   *int    `json:"forma_cobro_id"`
	Aplicado       bool    `json:"aplicado"`
	Rechazado      bool    `json:"rechazado"`
	DoctoCCID      *int    `json:"docto_cc_id"`
	PagoRecibidoID *string `json:"pago_recibido_id"`
}

// VisitaDTO — contrato §1 Actividad.visita.
type VisitaDTO struct {
	VisitaID   string  `json:"visita_id"`
	TipoVisita string  `json:"tipo_visita"`
	Nota       *string `json:"nota"`
	Estado     string  `json:"estado"`
	Trato      string  `json:"trato"`
	Etiqueta   string  `json:"etiqueta"`
}

// ActividadDTO — contrato §1 Actividad.
type ActividadDTO struct {
	ID            string           `json:"id"`
	Tipo          string           `json:"tipo"`
	Hora          string           `json:"hora"`
	RecibidoAt    *string          `json:"recibido_at"`
	ClienteID     int              `json:"cliente_id"`
	ClienteNombre string           `json:"cliente_nombre"`
	VentaID       *int             `json:"venta_id"`
	Folio         *string          `json:"folio"`
	RegistradoPor RegistradoPorDTO `json:"registrado_por"`
	Lat           *float64         `json:"lat"`
	Lng           *float64         `json:"lng"`
	ConGPS        bool             `json:"con_gps"`
	NumFotos      int              `json:"num_fotos"`
	DoctoPVID     *int             `json:"docto_pv_id"`
	CapturaTardia bool             `json:"captura_tardia"`
	Pago          *PagoDTO         `json:"pago"`
	Visita        *VisitaDTO       `json:"visita"`
}

// ConteoImporteDTO es `{n, importe}`.
type ConteoImporteDTO struct {
	N       int    `json:"n"`
	Importe string `json:"importe"`
}

// PausaDTO — contrato §1 Pausa.
type PausaDTO struct {
	Desde       string  `json:"desde"`
	Hasta       string  `json:"hasta"`
	Minutos     int     `json:"minutos"`
	AnteriorID  *string `json:"anterior_id"`
	SiguienteID *string `json:"siguiente_id"`
	EnCurso     bool    `json:"en_curso"`
}

// TramoDTO — contrato §1 Tramo.
type TramoDTO struct {
	Desde         string           `json:"desde"`
	Hasta         string           `json:"hasta"`
	Pagos         ConteoImporteDTO `json:"pagos"`
	Visitas       int              `json:"visitas"`
	Condonaciones int              `json:"condonaciones"`
	Clientes      int              `json:"clientes"`
}

// HoraDTO — contrato §1 Hora.
type HoraDTO struct {
	Hora          int    `json:"hora"`
	Pagos         int    `json:"pagos"`
	Visitas       int    `json:"visitas"`
	Condonaciones int    `json:"condonaciones"`
	Importe       string `json:"importe"`
}

// SenalDTO — contrato §1 Senal.
type SenalDTO struct {
	Tipo         string   `json:"tipo"`
	ActividadIDs []string `json:"actividad_ids"`
	Desde        *string  `json:"desde"`
	Hasta        *string  `json:"hasta"`
	Detalle      string   `json:"detalle"`
	Regla        string   `json:"regla"`
}

// PuntoDTO es un punto de LineaDelDia con los datos de su popover (contrato v2).
type PuntoDTO struct {
	ID            string  `json:"id"`
	Tipo          string  `json:"tipo"`
	Hora          string  `json:"hora"`
	ClienteNombre string  `json:"cliente_nombre"`
	Importe       *string `json:"importe"`
	Resultado     *string `json:"resultado"`
	NumFotos      int     `json:"num_fotos"`
}

// LineaDelDiaDTO — contrato §1 LineaDelDia.
type LineaDelDiaDTO struct {
	Dia     string     `json:"dia"`
	Puntos  []PuntoDTO `json:"puntos"`
	Tramos  []TramoDTO `json:"tramos"`
	PorHora []HoraDTO  `json:"por_hora"`
	Pausas  []PausaDTO `json:"pausas"`
}

// PausasResumenDTO es `{n, minutos, en_curso}`.
type PausasResumenDTO struct {
	N       int  `json:"n"`
	Minutos int  `json:"minutos"`
	EnCurso bool `json:"en_curso"`
}

// UltimaDTO es `ultima` del tablero.
type UltimaDTO struct {
	ID            string `json:"id"`
	Tipo          string `json:"tipo"`
	Hora          string `json:"hora"`
	ClienteNombre string `json:"cliente_nombre"`
}

// PagosPorFormaDTO cuenta solo efectivo, transferencia y cheque.
type PagosPorFormaDTO struct {
	Efectivo      ConteoImporteDTO `json:"efectivo"`
	Transferencia ConteoImporteDTO `json:"transferencia"`
	Cheque        ConteoImporteDTO `json:"cheque"`
}

// CoberturaDTO es `{con_algo, total}`.
type CoberturaDTO struct {
	ConAlgo int `json:"con_algo"`
	Total   int `json:"total"`
}

// CoberturaPorSegmentoDTO — conteos por segmento.
type CoberturaPorSegmentoDTO struct {
	SinVisitar int `json:"sin_visitar"`
	Volver     int `json:"volver"`
	Despues    int `json:"despues"`
	Pagados    int `json:"pagados"`
	SinNada    int `json:"sin_nada"`
}

// CobroDTO es `{cobrado, esperado, falta, pct}`.
type CobroDTO struct {
	Cobrado  string  `json:"cobrado"`
	Esperado string  `json:"esperado"`
	Falta    string  `json:"falta"`
	Pct      *string `json:"pct"`
}

// UsuarioTableroDTO — una fila del contrato §2.
type UsuarioTableroDTO struct {
	UID                  string                  `json:"uid"`
	Nombre               string                  `json:"nombre"`
	Email                string                  `json:"email"`
	ZonaID               *int                    `json:"zona_id"`
	ZonaNombre           *string                 `json:"zona_nombre"`
	Configurado          bool                    `json:"configurado"`
	MotivoNoConfigurado  *string                 `json:"motivo_no_configurado"`
	InicioSemana         *string                 `json:"inicio_semana"`
	VentasUniverso       int                     `json:"ventas_universo"`
	EstadoAhora          string                  `json:"estado_ahora"`
	EstadoDesde          *string                 `json:"estado_desde"`
	Ultima               *UltimaDTO              `json:"ultima"`
	PrimeraActividad     *string                 `json:"primera_actividad"`
	UltimaActividad      *string                 `json:"ultima_actividad"`
	MinutosActivos       int                     `json:"minutos_activos"`
	Pausas               PausasResumenDTO        `json:"pausas"`
	Pagos                ConteoImporteDTO        `json:"pagos"`
	PagosPorForma        PagosPorFormaDTO        `json:"pagos_por_forma"`
	Condonaciones        ConteoImporteDTO        `json:"condonaciones"`
	NoAplicados          ConteoImporteDTO        `json:"no_aplicados"`
	Visitas              int                     `json:"visitas"`
	Cobertura            CoberturaDTO            `json:"cobertura"`
	CoberturaPorSegmento CoberturaPorSegmentoDTO `json:"cobertura_por_segmento"`
	Cobro                CobroDTO                `json:"cobro"`
	Lineas               []LineaDelDiaDTO        `json:"lineas"`
}

// TableroDTO — contrato §2.
type TableroDTO struct {
	GeneradoAt string              `json:"generado_at"`
	Periodo    string              `json:"periodo"`
	PausaMin   int                 `json:"pausa_min"`
	Usuarios   []UsuarioTableroDTO `json:"usuarios"`
}

// ResumenJornadaDTO — contrato §3 resumen.
type ResumenJornadaDTO struct {
	PrimeraActividad *string          `json:"primera_actividad"`
	UltimaActividad  *string          `json:"ultima_actividad"`
	MinutosActivos   int              `json:"minutos_activos"`
	Pausas           PausasResumenDTO `json:"pausas"`
	Pagos            ConteoImporteDTO `json:"pagos"`
	Visitas          int              `json:"visitas"`
	Condonaciones    int              `json:"condonaciones"`
	Clientes         int              `json:"clientes"`
}

// JornadaDTO — contrato §3.
type JornadaDTO struct {
	UID         string            `json:"uid"`
	Fecha       string            `json:"fecha"`
	PausaMin    int               `json:"pausa_min"`
	GeneradoAt  string            `json:"generado_at"`
	Actividades []ActividadDTO    `json:"actividades"`
	Linea       LineaDelDiaDTO    `json:"linea"`
	Senales     []SenalDTO        `json:"senales"`
	Resumen     ResumenJornadaDTO `json:"resumen"`
}

// PorTipoDTO cuenta por tipo de actividad.
type PorTipoDTO struct {
	Pago        int `json:"pago"`
	Visita      int `json:"visita"`
	Condonacion int `json:"condonacion"`
}

// TotalesActividadDTO — totales del filtro completo.
type TotalesActividadDTO struct {
	N       int        `json:"n"`
	Importe string     `json:"importe"`
	PorTipo PorTipoDTO `json:"por_tipo"`
}

// PaginaActividadDTO — contrato §4.
type PaginaActividadDTO struct {
	Items           []ActividadDTO      `json:"items"`
	SiguienteCursor *string             `json:"siguiente_cursor"`
	Totales         TotalesActividadDTO `json:"totales"`
}

// ConteosCuentasDTO — contrato §5 conteos.
type ConteosCuentasDTO struct {
	SinVisitar int `json:"sin_visitar"`
	Volver     int `json:"volver"`
	Despues    int `json:"despues"`
	Pagados    int `json:"pagados"`
	SinNada    int `json:"sin_nada"`
	Total      int `json:"total"`
}

// ActividadCuentaDTO es `ultima_actividad {id, tipo, hora}`.
type ActividadCuentaDTO struct {
	ID   string `json:"id"`
	Tipo string `json:"tipo"`
	Hora string `json:"hora"`
}

// CuentaDTO — una venta del contrato §5.
type CuentaDTO struct {
	VentaID         int                 `json:"venta_id"`
	DoctoPVID       *int                `json:"docto_pv_id"`
	Folio           *string             `json:"folio"`
	ClienteID       int                 `json:"cliente_id"`
	ClienteNombre   string              `json:"cliente_nombre"`
	Producto        *string             `json:"producto"`
	Estado          string              `json:"estado"`
	Trato           string              `json:"trato"`
	Etiqueta        string              `json:"etiqueta"`
	Detalle         string              `json:"detalle"`
	Segmento        *string             `json:"segmento"`
	ConAlgo         bool                `json:"con_algo"`
	DeterminadoPor  *string             `json:"determinado_por"`
	UltimaActividad *ActividadCuentaDTO `json:"ultima_actividad"`
	Saldo           string              `json:"saldo"`
	Parcialidad     string              `json:"parcialidad"`
	AbonoSemana     string              `json:"abono_semana"`
	NumAtrasos      int                 `json:"num_atrasos"`
	Nueva           bool                `json:"nueva"`
}

// CuentasDTO — contrato §5.
type CuentasDTO struct {
	InicioSemana string            `json:"inicio_semana"`
	Conteos      ConteosCuentasDTO `json:"conteos"`
	Cuentas      []CuentaDTO       `json:"cuentas"`
}

// FormaCobroCatalogoDTO — contrato §4b.
type FormaCobroCatalogoDTO struct {
	Codigo   string `json:"codigo"`
	Etiqueta string `json:"etiqueta"`
}

// TipoVisitaCatalogoDTO — contrato §4b.
type TipoVisitaCatalogoDTO struct {
	Literal string `json:"literal"`
	Estado  string `json:"estado"`
	Grupo   string `json:"grupo"`
}

// CatalogosDTO — contrato §4b, GET /desempeno/catalogos.
type CatalogosDTO struct {
	FormasCobro []FormaCobroCatalogoDTO `json:"formas_cobro"`
	TiposVisita []TipoVisitaCatalogoDTO `json:"tipos_visita"`
}
```

`internal/desempeno/infra/desempenohttp/mapper.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"time"

	"github.com/shopspring/decimal"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	"github.com/abdimuy/msp-api/internal/desempeno/domain"
)

const escalaDinero = 2

func dinero(d decimal.Decimal) string { return d.StringFixed(escalaDinero) }

func hora(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func horaNula(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := hora(*t)
	return &s
}

func textoNulo(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func enteroNulo(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func conteo(c domain.ConteoImporte) ConteoImporteDTO {
	return ConteoImporteDTO{N: c.N, Importe: dinero(c.Importe)}
}

func aActividadDTO(a domain.Actividad) ActividadDTO {
	dto := ActividadDTO{
		ID: a.ID, Tipo: string(a.Tipo), Hora: hora(a.Hora), RecibidoAt: horaNula(a.RecibidoAt),
		ClienteID: a.ClienteID, ClienteNombre: a.ClienteNombre,
		VentaID: enteroNulo(a.VentaID), Folio: textoNulo(a.Folio),
		RegistradoPor: RegistradoPorDTO{
			UsuarioUID: textoNulo(a.RegistradoPor.UsuarioUID), CobradorID: enteroNulo(a.RegistradoPor.CobradorID),
			Nombre: a.RegistradoPor.Nombre, Origen: string(a.RegistradoPor.Origen),
		},
		ConGPS: a.ConGPS(), NumFotos: a.NumFotos,
		DoctoPVID: enteroNulo(a.DoctoPVID), CapturaTardia: a.CapturaTardia(),
	}
	if dto.ConGPS {
		dto.Lat, dto.Lng = a.Lat, a.Lng
	}
	if a.Pago != nil {
		dto.Pago = &PagoDTO{
			Importe: dinero(a.Pago.Importe), FormaCobro: domain.EtiquetaFormaCobro(a.Pago.FormaCobroID),
			FormaCobroID: enteroNulo(a.Pago.FormaCobroID), Aplicado: a.Pago.Aplicado, Rechazado: a.Pago.Rechazado,
			DoctoCCID: enteroNulo(a.Pago.DoctoCCID), PagoRecibidoID: textoNulo(a.Pago.PagoRecibidoID),
		}
	}
	if a.Visita != nil {
		// El estado, trato y etiqueta que esa visita sola le daría a su venta,
		// con las mismas reglas del teléfono (EstadoCuentaUi.kt).
		r := domain.ResultadoCuenta{Estado: domain.EstadoDeTipoVisita(a.Visita.TipoVisita, false)}
		dto.Visita = &VisitaDTO{
			VisitaID: a.Visita.VisitaID, TipoVisita: a.Visita.TipoVisita, Nota: textoNulo(a.Visita.Nota),
			Estado: string(r.Estado), Trato: string(domain.TratoDe(r)), Etiqueta: domain.EtiquetaDe(r),
		}
	}
	return dto
}

func aActividadesDTO(acts []domain.Actividad) []ActividadDTO {
	out := make([]ActividadDTO, 0, len(acts))
	for _, a := range acts {
		out = append(out, aActividadDTO(a))
	}
	return out
}

func aLineaDTO(l domain.LineaDelDia) LineaDelDiaDTO {
	dto := LineaDelDiaDTO{
		Dia: l.Dia.String(), Puntos: make([]PuntoDTO, 0, len(l.Puntos)), Tramos: make([]TramoDTO, 0, len(l.Tramos)),
		PorHora: make([]HoraDTO, 0, len(l.PorHora)), Pausas: make([]PausaDTO, 0, len(l.Pausas)),
	}
	for _, p := range l.Puntos {
		pd := PuntoDTO{
			ID: p.ID, Tipo: string(p.Tipo), Hora: hora(p.Hora), ClienteNombre: p.ClienteNombre,
			Resultado: textoNulo(p.Resultado), NumFotos: p.NumFotos,
		}
		if p.Importe != nil {
			s := dinero(*p.Importe)
			pd.Importe = &s
		}
		dto.Puntos = append(dto.Puntos, pd)
	}
	for _, t := range l.Tramos {
		dto.Tramos = append(dto.Tramos, TramoDTO{
			Desde: hora(t.Desde), Hasta: hora(t.Hasta), Pagos: conteo(t.Pagos),
			Visitas: t.Visitas, Condonaciones: t.Condonaciones, Clientes: t.Clientes,
		})
	}
	for _, h := range l.PorHora {
		dto.PorHora = append(dto.PorHora, HoraDTO{
			Hora: h.Hora, Pagos: h.Pagos, Visitas: h.Visitas, Condonaciones: h.Condonaciones, Importe: dinero(h.Importe),
		})
	}
	for _, p := range l.Pausas {
		dto.Pausas = append(dto.Pausas, PausaDTO{
			Desde: hora(p.Desde), Hasta: hora(p.Hasta), Minutos: p.Minutos,
			AnteriorID: textoNulo(p.AnteriorID), SiguienteID: textoNulo(p.SiguienteID), EnCurso: p.EnCurso,
		})
	}
	return dto
}

func aPausasDTO(p domain.ResumenPausas) PausasResumenDTO {
	return PausasResumenDTO{N: p.N, Minutos: p.Minutos, EnCurso: p.EnCurso}
}

func aTableroDTO(t desempenoapp.Tablero) TableroDTO {
	dto := TableroDTO{
		GeneradoAt: hora(t.GeneradoAt), Periodo: string(t.Periodo), PausaMin: t.PausaMin,
		Usuarios: make([]UsuarioTableroDTO, 0, len(t.Filas)),
	}
	for _, f := range t.Filas {
		dto.Usuarios = append(dto.Usuarios, aFilaDTO(f))
	}
	return dto
}

func aFilaDTO(f desempenoapp.FilaTablero) UsuarioTableroDTO {
	u := f.Usuario
	dto := UsuarioTableroDTO{
		UID: u.UID, Nombre: u.Nombre, Email: u.Email,
		ZonaID: enteroNulo(u.ZonaID), ZonaNombre: textoNulo(u.ZonaNombre),
		Configurado: f.Configurado, MotivoNoConfigurado: textoNulo(f.MotivoNoConfigurado),
		InicioSemana: horaNula(u.InicioSemana), VentasUniverso: f.VentasUniverso,
		EstadoAhora: string(f.EstadoAhora), EstadoDesde: horaNula(f.EstadoDesde),
		PrimeraActividad: horaNula(f.PrimeraActividad), UltimaActividad: horaNula(f.UltimaActividadHora),
		MinutosActivos: f.MinutosActivos, Pausas: aPausasDTO(f.Pausas),
		Pagos: conteo(f.Pagos),
		PagosPorForma: PagosPorFormaDTO{
			Efectivo: conteo(f.PagosPorForma.Efectivo), Transferencia: conteo(f.PagosPorForma.Transferencia),
			Cheque: conteo(f.PagosPorForma.Cheque),
		},
		Condonaciones: conteo(f.Condonaciones), NoAplicados: conteo(f.NoAplicados), Visitas: f.Visitas,
		Cobertura: CoberturaDTO{ConAlgo: f.CoberturaConAlgo, Total: f.CoberturaTotal},
		CoberturaPorSegmento: CoberturaPorSegmentoDTO{
			SinVisitar: f.PorSegmento.SinVisitar, Volver: f.PorSegmento.Volver, Despues: f.PorSegmento.Despues,
			Pagados: f.PorSegmento.Pagados, SinNada: f.PorSegmento.SinNada,
		},
		Cobro: CobroDTO{
			Cobrado: dinero(f.Cobro.Cobrado), Esperado: dinero(f.Cobro.Esperado), Falta: dinero(f.Cobro.Falta),
		},
		Lineas: make([]LineaDelDiaDTO, 0, len(f.Lineas)),
	}
	if f.Cobro.Pct != nil {
		s := dinero(*f.Cobro.Pct)
		dto.Cobro.Pct = &s
	}
	if f.Ultima != nil {
		dto.Ultima = &UltimaDTO{
			ID: f.Ultima.ID, Tipo: string(f.Ultima.Tipo), Hora: hora(f.Ultima.Hora), ClienteNombre: f.Ultima.ClienteNombre,
		}
	}
	for _, l := range f.Lineas {
		dto.Lineas = append(dto.Lineas, aLineaDTO(l))
	}
	return dto
}

func aJornadaDTO(v desempenoapp.VistaJornada) JornadaDTO {
	j := v.Jornada
	dto := JornadaDTO{
		UID: v.UID, Fecha: j.Dia.String(), PausaMin: v.PausaMin, GeneradoAt: hora(v.GeneradoAt),
		Actividades: aActividadesDTO(j.Actividades), Linea: aLineaDTO(j.Linea),
		Senales: make([]SenalDTO, 0, len(j.Senales)),
		Resumen: ResumenJornadaDTO{
			PrimeraActividad: horaNula(j.Resumen.PrimeraActividad), UltimaActividad: horaNula(j.Resumen.UltimaActividad),
			MinutosActivos: j.Resumen.MinutosActivos, Pausas: aPausasDTO(j.Resumen.Pausas),
			Pagos: conteo(j.Resumen.Pagos), Visitas: j.Resumen.Visitas,
			Condonaciones: j.Resumen.Condonaciones.N, Clientes: j.Resumen.Clientes,
		},
	}
	for _, s := range j.Senales {
		ids := make([]string, 0, len(s.ActividadIDs))
		ids = append(ids, s.ActividadIDs...)
		dto.Senales = append(dto.Senales, SenalDTO{
			Tipo: string(s.Tipo), ActividadIDs: ids, Desde: horaNula(s.Desde), Hasta: horaNula(s.Hasta),
			Detalle: s.Detalle, Regla: s.Regla,
		})
	}
	return dto
}

func aPaginaDTO(p desempenoapp.PaginaActividad) PaginaActividadDTO {
	return PaginaActividadDTO{
		Items: aActividadesDTO(p.Items), SiguienteCursor: textoNulo(p.SiguienteCursor),
		Totales: TotalesActividadDTO{
			N: p.Totales.N, Importe: dinero(p.Totales.Importe),
			PorTipo: PorTipoDTO{
				Pago:        p.Totales.PorTipo[domain.TipoPago],
				Visita:      p.Totales.PorTipo[domain.TipoVisita],
				Condonacion: p.Totales.PorTipo[domain.TipoCondonacion],
			},
		},
	}
}

func aCuentasDTO(v desempenoapp.VistaCuentas) CuentasDTO {
	dto := CuentasDTO{
		InicioSemana: hora(v.InicioSemana),
		Conteos: ConteosCuentasDTO{
			SinVisitar: v.Conteos.SinVisitar, Volver: v.Conteos.Volver, Despues: v.Conteos.Despues,
			Pagados: v.Conteos.Pagados, SinNada: v.Conteos.SinNada, Total: v.Conteos.Total,
		},
		Cuentas: make([]CuentaDTO, 0, len(v.Cuentas)),
	}
	for _, c := range v.Cuentas {
		cu := c.Cuenta
		cd := CuentaDTO{
			VentaID: cu.VentaID, DoctoPVID: enteroNulo(cu.DoctoPVID), Folio: textoNulo(cu.Folio),
			ClienteID: cu.ClienteID, ClienteNombre: cu.ClienteNombre, Producto: textoNulo(cu.Producto),
			Estado: string(c.Resultado.Estado), Trato: string(c.Trato), Etiqueta: c.Etiqueta, Detalle: c.Detalle,
			ConAlgo: c.Resultado.ConAlgo, DeterminadoPor: textoNulo(c.Resultado.DeterminadoPor),
			Saldo: dinero(cu.Saldo), Parcialidad: dinero(cu.Parcialidad), AbonoSemana: dinero(c.Resultado.AbonoVentana),
			NumAtrasos: c.NumAtrasos, Nueva: c.Nueva,
		}
		if c.Segmento != nil {
			s := string(*c.Segmento)
			cd.Segmento = &s
		}
		if c.UltimaActividad != nil {
			cd.UltimaActividad = &ActividadCuentaDTO{
				ID: c.UltimaActividad.ID, Tipo: string(c.UltimaActividad.Tipo), Hora: hora(c.UltimaActividad.Hora),
			}
		}
		dto.Cuentas = append(dto.Cuentas, cd)
	}
	return dto
}

func aCatalogosDTO() CatalogosDTO {
	dto := CatalogosDTO{}
	for _, f := range domain.CatalogoFormasCobro() {
		dto.FormasCobro = append(dto.FormasCobro, FormaCobroCatalogoDTO{Codigo: f.Codigo, Etiqueta: f.Etiqueta})
	}
	for _, t := range domain.CatalogoTiposVisita() {
		dto.TiposVisita = append(dto.TiposVisita, TipoVisitaCatalogoDTO{Literal: t.Literal, Estado: string(t.Estado), Grupo: t.Grupo})
	}
	return dto
}
```

`internal/desempeno/infra/desempenohttp/handlers.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"context"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
)

// Handlers agrupa las operaciones Huma del módulo. Todas exigen desempeno:ver.
type Handlers struct {
	svc *desempenoapp.Service
}

// TableroInput — GET /desempeno/usuarios. Todo llega como texto y se valida en
// el dominio para que el 422 lleve el código desempeno_parametro_invalido.
type TableroInput struct {
	Periodo   string `query:"periodo"    doc:"hoy | semana (por omisión hoy)"`
	PausaMin  string `query:"pausa_min"  doc:"umbral de pausa en minutos, 5–240 (por omisión 45)"`
	DesdeHora string `query:"desde_hora" doc:"HH:MM, con hasta_hora"`
	HastaHora string `query:"hasta_hora" doc:"HH:MM, con desde_hora"`
}

// TableroOutput envuelve TableroDTO.
type TableroOutput struct{ Body TableroDTO }

// JornadaInput — GET /desempeno/usuarios/{uid}/jornada.
type JornadaInput struct {
	UID       string `path:"uid"         doc:"uid de Firestore del usuario"`
	Fecha     string `query:"fecha"      doc:"YYYY-MM-DD (por omisión hoy, America/Mexico_City)"`
	PausaMin  string `query:"pausa_min"`
	DesdeHora string `query:"desde_hora"`
	HastaHora string `query:"hasta_hora"`
}

// JornadaOutput envuelve JornadaDTO.
type JornadaOutput struct{ Body JornadaDTO }

// ActividadInput — GET /desempeno/usuarios/{uid}/actividad. Los repetibles van
// "explode" (?tipo=pago&tipo=visita), como pide el contrato §4.
type ActividadInput struct {
	UID           string   `path:"uid"`
	Desde         string   `query:"desde"`
	Hasta         string   `query:"hasta"`
	Tipo          []string `query:"tipo,explode"`
	FormaCobro    []string `query:"forma_cobro,explode"`
	TipoVisita    []string `query:"tipo_visita,explode"`
	RegistradoPor []string `query:"registrado_por,explode"`
	MontoMin      string   `query:"monto_min"`
	MontoMax      string   `query:"monto_max"`
	ConFotos      string   `query:"con_fotos"`
	ConGPS        string   `query:"con_gps"`
	Q             string   `query:"q"`
	Orden         string   `query:"orden"`
	Dir           string   `query:"dir"`
	Cursor        string   `query:"cursor"`
	Limit         string   `query:"limit"`
}

// ActividadOutput envuelve PaginaActividadDTO.
type ActividadOutput struct{ Body PaginaActividadDTO }

// CuentasInput — GET /desempeno/usuarios/{uid}/cuentas.
type CuentasInput struct {
	UID      string `path:"uid"`
	Segmento string `query:"segmento"`
	Q        string `query:"q"`
	Orden    string `query:"orden"`
	Dir      string `query:"dir"`
}

// CuentasOutput envuelve CuentasDTO.
type CuentasOutput struct{ Body CuentasDTO }

// CatalogosInput — GET /desempeno/catalogos (sin parámetros).
type CatalogosInput struct{}

// CatalogosOutput envuelve CatalogosDTO.
type CatalogosOutput struct{ Body CatalogosDTO }

// Catalogos — formas de cobro y tipos de visita para armar los filtros: el
// escritorio no copia catálogos (contrato §4b).
func (h *Handlers) Catalogos(ctx context.Context, _ *CatalogosInput) (*CatalogosOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	return &CatalogosOutput{Body: aCatalogosDTO()}, nil
}

// Tablero — una fila por usuario, sin totales del equipo (D14).
func (h *Handlers) Tablero(ctx context.Context, in *TableroInput) (*TableroOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	t, err := h.svc.Tablero(ctx, desempenoapp.TableroInput{
		Periodo: in.Periodo, PausaMin: in.PausaMin, DesdeHora: in.DesdeHora, HastaHora: in.HastaHora,
	})
	if err != nil {
		return nil, mapAppError(err)
	}
	return &TableroOutput{Body: aTableroDTO(t)}, nil
}

// Jornada — actividades del usuario en un día, con pausas y señales.
func (h *Handlers) Jornada(ctx context.Context, in *JornadaInput) (*JornadaOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err := h.svc.Jornada(ctx, in.UID, desempenoapp.JornadaInput{
		Fecha: in.Fecha, PausaMin: in.PausaMin, DesdeHora: in.DesdeHora, HastaHora: in.HastaHora,
	})
	if err != nil {
		return nil, mapAppError(err)
	}
	return &JornadaOutput{Body: aJornadaDTO(v)}, nil
}

// Actividad — página de la actividad de toda la zona del usuario.
func (h *Handlers) Actividad(ctx context.Context, in *ActividadInput) (*ActividadOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	p, err := h.svc.Actividad(ctx, in.UID, desempenoapp.FiltroActividadInput{
		Desde: in.Desde, Hasta: in.Hasta, Tipos: in.Tipo, FormasCobro: in.FormaCobro, TiposVisita: in.TipoVisita,
		RegistradoPor: in.RegistradoPor, MontoMin: in.MontoMin, MontoMax: in.MontoMax,
		ConFotos: in.ConFotos, ConGPS: in.ConGPS, Q: in.Q, Orden: in.Orden, Dir: in.Dir,
		Cursor: in.Cursor, Limit: in.Limit,
	})
	if err != nil {
		return nil, mapAppError(err)
	}
	return &ActividadOutput{Body: aPaginaDTO(p)}, nil
}

// Cuentas — estado por venta de la semana del usuario.
func (h *Handlers) Cuentas(ctx context.Context, in *CuentasInput) (*CuentasOutput, error) {
	if err := autorizar(ctx); err != nil {
		return nil, err
	}
	v, err := h.svc.Cuentas(ctx, in.UID, desempenoapp.CuentasInput{
		Segmento: in.Segmento, Q: in.Q, Orden: in.Orden, Dir: in.Dir,
	})
	if err != nil {
		return nil, mapAppError(err)
	}
	return &CuentasOutput{Body: aCuentasDTO(v)}, nil
}
```

`internal/desempeno/infra/desempenohttp/routes.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
)

const securitySchemeName = "bearerAuth"

// MountRouter registra las cuatro operaciones sobre r. r ya trae el
// middleware de autenticación (cmd/api/server.go); el permiso se revisa en
// cada handler. Rutas finales: /v2/desempeno/usuarios[/{uid}/jornada|actividad|cuentas].
func MountRouter(r chi.Router, svc *desempenoapp.Service) huma.API {
	cfg := huma.DefaultConfig("MSP API · Desempeño de cobradores", "v2")
	cfg.DocsRenderer = huma.DocsRendererScalar
	// Sin el "$schema" que Huma agrega a cada respuesta: el contrato fija las
	// claves byte a byte (contrato §0) y TestContrato_* lo vigila.
	cfg.CreateHooks = nil
	if cfg.Components == nil {
		cfg.Components = &huma.Components{}
	}
	if cfg.Components.SecuritySchemes == nil {
		cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	cfg.Components.SecuritySchemes[securitySchemeName] = &huma.SecurityScheme{
		Type: "http", Scheme: "bearer", BearerFormat: "JWT",
		Description: "Token de Firebase ID propagado al backend como Bearer.",
	}
	if cfg.OpenAPI != nil {
		cfg.Servers = append(cfg.Servers, &huma.Server{URL: "/v2"})
	}
	api := humachi.New(r, cfg)
	h := &Handlers{svc: svc}
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"desempeno"}

	huma.Register(api, huma.Operation{
		OperationID: "desempeno-tablero", Method: http.MethodGet, Path: "/desempeno/usuarios",
		Summary: "Tablero de desempeño por usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Tablero)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-catalogos", Method: http.MethodGet, Path: "/desempeno/catalogos",
		Summary: "Catálogos para los filtros", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Catalogos)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-jornada", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}/jornada",
		Summary: "Jornada de un usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Jornada)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-actividad", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}/actividad",
		Summary: "Actividad de la zona del usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Actividad)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-cuentas", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}/cuentas",
		Summary: "Cuentas de la semana del usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Cuentas)
	return api
}
```


Run: `go test -cover ./internal/desempeno/infra/desempenohttp/`
Expected: `ok … coverage: 98.6%`.

- [ ] **Step 4: Wiring y montaje bajo `/v2`**

`cmd/api/desempeno_wiring.go` (archivo nuevo, completo):

```go
//nolint:misspell // vocabulario español por convención del proyecto.
package main

import (
	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
	desempenoclients "github.com/abdimuy/msp-api/internal/desempeno/infra/clients"
	desempenofb "github.com/abdimuy/msp-api/internal/desempeno/infra/desempenofb"
	desempenooutbound "github.com/abdimuy/msp-api/internal/desempeno/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
)

// provideDesempenoService arma el módulo de desempeño de cobradores (solo
// lectura). Los usuarios y la cobranza semanal llegan por el contrato de rutas
// (rutas.CobranzaUsuariosReader, que satisface *rutasapp.Service); la zona de
// negocio es la misma de la frontera Firebird (America/Mexico_City).
func provideDesempenoService(pool *firebird.Pool, rutasSvc *rutasapp.Service) *desempenoapp.Service {
	return desempenoapp.NewService(
		desempenoclients.NewRutasUsuariosClient(rutasSvc),
		desempenofb.NewActividadesRepo(pool),
		desempenofb.NewCuentasRepo(pool),
		desempenooutbound.ProductionClock{},
		firebird.BusinessTZ(),
	)
}
```

`cmd/api/main.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/cmd/api/main.go b/cmd/api/main.go
index a74c771..ae991fd 100644
--- a/cmd/api/main.go
+++ b/cmd/api/main.go
@@ -235,6 +235,8 @@ func appOptions() []fx.Option {
 			provideVisitasImagenesRepo,
 			provideVisitasStorage,
 			provideVisitasService,
+			// Desempeño de cobradores (solo lectura).
+			provideDesempenoService,
 			provideRootHandler,
 			provideHTTPServer,
 		),
```

`cmd/api/server.go` (cambio sobre el archivo existente; aplicar exactamente este diff):

```diff
diff --git a/cmd/api/server.go b/cmd/api/server.go
index 87f50d9..831865b 100644
--- a/cmd/api/server.go
+++ b/cmd/api/server.go
@@ -53,6 +53,8 @@ import (
 	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
 	reactivacionhttp "github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionhttp"
 
+	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
+	desempenohttp "github.com/abdimuy/msp-api/internal/desempeno/infra/desempenohttp"
 	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
 	visitashttp "github.com/abdimuy/msp-api/internal/visitas/infra/visitashttp"
 )
@@ -182,6 +184,7 @@ func provideRootHandler(
 	configSvc *configapp.Service,
 	reactivacionSvc *reactivacionapp.Service,
 	visitasSvc *visitasapp.Service,
+	desempenoSvc *desempenoapp.Service,
 	logger *slog.Logger,
 ) RootHandler {
 	r := chi.NewRouter()
@@ -369,6 +372,17 @@ func provideRootHandler(
 				visitashttp.WithSSE(eventBus, cfg.Visitas, logger))
 		})
 
+		// Desempeño de cobradores — solo lectura, authn únicamente (sin
+		// idempotencia ni captura de intents: no hay escrituras). desempeno:ver
+		// se exige dentro de cada handler. MountRouter registra rutas
+		// /desempeno/*, así que va en un Group (como rutas), NO en
+		// r.Route("/desempeno"), que duplicaría el prefijo.
+		// Rutas finales: GET /v2/desempeno/usuarios[/{uid}/jornada|actividad|cuentas].
+		r.Group(func(r chi.Router) {
+			r.Use(skipAuthForPublicDocs(authn.Handler))
+			desempenohttp.MountRouter(r, desempenoSvc)
+		})
+
 		// Cobranza endpoints — authn only. Read (saldos, pagos, sync) plus
 		// pago write (CrearPago, imágenes) share one chi router + huma.API.
 		// Idempotency for POST /pagos is enforced end-to-end via body.id as
```


Run: `go build ./... && go test -count=1 ./cmd/api/ -run TestAppGraph`
Expected: PASS (`TestAppGraph_IsValid` valida el grafo de fx con el proveedor nuevo).

- [ ] **Step 5: Control positivo del contrato**

En `dto.go`, renombrar el tag `json:"captura_tardia"` a `json:"capturaTardia"` → falla `TestContrato_Jornada` y `TestContrato_Actividad` ("claves de actividades[0]"). Quitar `cfg.CreateHooks = nil` de `routes.go` → falla `TestContrato_Tablero` (aparece `$schema`). Restaurar; PASS; diff limpio.

- [ ] **Step 6: Suite completa antes del PR**

```bash
golangci-lint run ./...
go test -race -short ./...
set -a && . ./.env && set +a && make test-firebird-all
```
Expected: `0 issues.`; todo `ok`; `make test-firebird-all` verde (incluye `desempenofb`).

- [ ] **Step 7: Commit y PR**

```bash
git add internal/auth internal/desempeno/infra/desempenohttp cmd/api
git commit -m "feat(desempeno): endpoints del contrato, catálogos, permiso desempeno:ver y wiring"
git push -u origin feat/desempeno
gh pr create --base main --head feat/desempeno --title "feat(desempeno): API de desempeño de cobradores (fase 1)" --body "Implementa docs/superpowers/plans/2026-10-14-desempeno-api.md. Mediciones de producción pendientes: Apéndice 1 del plan."
```

Smoke manual en dev (el dueño exporta su token; nunca se maneja la credencial aquí):

```bash
curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:3011/v2/desempeno/catalogos" | jq '.tipos_visita | length'      # 14
curl -s -H "Authorization: Bearer $TOKEN" "http://localhost:3011/v2/desempeno/usuarios?periodo=hoy" | jq '.usuarios | length'
```

---

## Apéndice 1 — mediciones en producción (C0 y C4, al final, juntas, SOLO LECTURA)

Se corren después de desplegar (o antes, la parte C0) contra `MUEBLERA_SNP.FDB` con `isql -i`, igual que las mediciones previas (ver memoria `reference_isql_produccion_trampas`: leer los errores antes que los números; un bloque vacío puede ser un error de truncado). Cada resultado se registra en `docs/evidencia/cobranza.md` con identificador nuevo: el archivo dice "siguiente libre `E-COB-019`" y el spec pide arrancar en `E-COB-020` — **contradicción reportada**; el registrador elige y corrige el encabezado. Primero se corre todo en dev (control positivo: las consultas devuelven filas), luego en producción.

Archivo `scripts/medicion/desempeno-produccion.sql` (solo `SELECT`; se sube por `scp` y se corre con `isql -u SYSDBA -p <de fábrica> -ch UTF8 -i desempeno-produccion.sql <ruta de MUEBLERA_SNP.FDB> > salida.txt`):

```sql
-- Mediciones de desempeño de cobradores. SOLO LECTURA: ningún INSERT/UPDATE/DELETE/DDL.
SET STATS ON;

-- M1. Índices de lo que leen los repositorios nuevos.
SELECT i.RDB$RELATION_NAME AS TABLA, i.RDB$INDEX_NAME AS INDICE, s.RDB$FIELD_NAME AS CAMPO,
       s.RDB$FIELD_POSITION AS POS, COALESCE(i.RDB$INDEX_INACTIVE, 0) AS INACTIVO
FROM RDB$INDICES i JOIN RDB$INDEX_SEGMENTS s ON s.RDB$INDEX_NAME = i.RDB$INDEX_NAME
WHERE i.RDB$RELATION_NAME IN ('MSP_VISITAS', 'MSP_VISITAS_IMAGENES', 'MSP_PAGOS_RECIBIDOS',
                              'MSP_PAGOS_IMAGENES', 'MSP_PAGOS_VENTAS', 'MSP_SALDOS_VENTAS')
ORDER BY 1, 2, 4;

-- M2. Plan y costo de la lectura de visitas del repositorio: zona con más visitas, 92 días.
SET PLAN ON;
SELECT COUNT(*) AS VISITAS_92_DIAS FROM MSP_VISITAS v
WHERE v.ZONA_CLIENTE_ID = (SELECT FIRST 1 x.ZONA_CLIENTE_ID FROM MSP_VISITAS x
                           WHERE x.FECHA >= DATEADD(-7 DAY TO CURRENT_DATE)
                           GROUP BY x.ZONA_CLIENTE_ID ORDER BY COUNT(*) DESC)
  AND v.FECHA >= DATEADD(-92 DAY TO CURRENT_DATE) AND v.FECHA <= CURRENT_TIMESTAMP;
-- M2b. Lo mismo para pagos aplicados (debe usar IDX_MSP_PAGOS_ZONA_FECHA).
SELECT COUNT(*) AS PAGOS_92_DIAS FROM MSP_PAGOS_VENTAS p
WHERE p.ZONA_CLIENTE_ID = (SELECT FIRST 1 x.ZONA_CLIENTE_ID FROM MSP_PAGOS_VENTAS x
                           WHERE x.FECHA >= DATEADD(-7 DAY TO CURRENT_DATE)
                           GROUP BY x.ZONA_CLIENTE_ID ORDER BY COUNT(*) DESC)
  AND p.FECHA >= DATEADD(-92 DAY TO CURRENT_DATE) AND p.CANCELADO = 'N'
  AND p.CONCEPTO_CC_ID IN (87327, 27969);
SET PLAN OFF;

-- M3. ¿Concepto y forma de cobro dicen lo mismo? (87327 vs 27969 contra 137026), 90 días.
SELECT CONCEPTO, FORMA, COUNT(*) AS N FROM (
  SELECT p.CONCEPTO_CC_ID AS CONCEPTO,
         (SELECT MIN(fcd.FORMA_COBRO_ID) FROM FORMAS_COBRO_DOCTOS fcd
           WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC' AND fcd.DOCTO_ID = p.DOCTO_CC_ID) AS FORMA
  FROM MSP_PAGOS_VENTAS p
  WHERE p.CANCELADO = 'N' AND p.CONCEPTO_CC_ID IN (87327, 27969)
    AND p.FECHA >= DATEADD(-90 DAY TO CURRENT_DATE))
GROUP BY CONCEPTO, FORMA ORDER BY CONCEPTO, FORMA;

-- M4. Diferencia por zona entre "solo 87327" (rutas) y "87327 + 27969" (sync/Node), últimos 7 días.
SELECT ZONA, SUM(CASE WHEN CONCEPTO = 87327 THEN IMPORTE ELSE 0 END) AS SOLO_87327,
       SUM(IMPORTE) AS CON_27969 FROM (
  SELECT p.ZONA_CLIENTE_ID AS ZONA, p.CONCEPTO_CC_ID AS CONCEPTO,
         CAST(p.IMPORTE + p.IMPUESTO AS NUMERIC(18,2)) AS IMPORTE
  FROM MSP_PAGOS_VENTAS p
  WHERE p.CANCELADO = 'N' AND p.CONCEPTO_CC_ID IN (87327, 27969)
    AND p.FECHA >= DATEADD(-7 DAY TO CURRENT_DATE))
GROUP BY ZONA ORDER BY ZONA;

-- M5. ¿MSP_VISITAS.IMPTE_DOCTO_CC_ID es la VENTA (cargo)? 90 días.
SELECT
  SUM(CASE WHEN v.IMPTE_DOCTO_CC_ID IS NULL OR v.IMPTE_DOCTO_CC_ID = 0 THEN 1 ELSE 0 END) AS SIN_VENTA,
  SUM(CASE WHEN EXISTS (SELECT 1 FROM MSP_SALDOS_VENTAS s WHERE s.DOCTO_CC_ID = v.IMPTE_DOCTO_CC_ID) THEN 1 ELSE 0 END) AS ES_CARGO,
  SUM(CASE WHEN EXISTS (SELECT 1 FROM IMPORTES_DOCTOS_CC i WHERE i.IMPTE_DOCTO_CC_ID = v.IMPTE_DOCTO_CC_ID) THEN 1 ELSE 0 END) AS ES_IMPORTE,
  COUNT(*) AS TOTAL
FROM MSP_VISITAS v WHERE v.FECHA >= DATEADD(-90 DAY TO CURRENT_DATE);

-- M6. Zona de la visita vs zona actual del cliente, 90 días.
SELECT SUM(CASE WHEN v.ZONA_CLIENTE_ID IS NULL THEN 1 ELSE 0 END) AS ZONA_NULA,
       SUM(CASE WHEN c.ZONA_CLIENTE_ID <> v.ZONA_CLIENTE_ID THEN 1 ELSE 0 END) AS ZONA_DISTINTA_HOY,
       COUNT(*) AS TOTAL
FROM MSP_VISITAS v LEFT JOIN CLIENTES c ON c.CLIENTE_ID = v.CLIENTE_ID
WHERE v.FECHA >= DATEADD(-90 DAY TO CURRENT_DATE);

-- M7. MSP_PAGOS_RECIBIDOS: estados (define "rechazado", D-API-2), CREATED_BY y duplicados.
SELECT ESTADO, INTENTOS, COUNT(*) AS N, SUM(CASE WHEN CREATED_BY IS NULL THEN 1 ELSE 0 END) AS SIN_CREATED_BY
FROM MSP_PAGOS_RECIBIDOS GROUP BY ESTADO, INTENTOS ORDER BY ESTADO, INTENTOS;
SELECT COUNT(*) AS DOCTO_CC_DUPLICADOS FROM (
  SELECT DOCTO_CC_ID FROM MSP_PAGOS_RECIBIDOS WHERE DOCTO_CC_ID IS NOT NULL
  GROUP BY DOCTO_CC_ID HAVING COUNT(*) > 1);
SELECT COUNT(*) AS IMPTE_DUPLICADOS FROM (
  SELECT IMPTE_DOCTO_CC_ID FROM MSP_PAGOS_RECIBIDOS WHERE IMPTE_DOCTO_CC_ID IS NOT NULL
  GROUP BY IMPTE_DOCTO_CC_ID HAVING COUNT(*) > 1);

-- M8. Tamaño del universo de desempeño por zona (semana = últimos 7 días) vs solo saldo > 0.
SELECT s.ZONA_CLIENTE_ID AS ZONA,
       SUM(CASE WHEN s.SALDO > 0 OR s.FECHA_ULT_PAGO >= DATEADD(-7 DAY TO CURRENT_DATE) THEN 1 ELSE 0 END) AS UNIVERSO,
       SUM(CASE WHEN s.SALDO > 0 THEN 1 ELSE 0 END) AS CON_SALDO
FROM MSP_SALDOS_VENTAS s
LEFT JOIN LIBRES_CARGOS_CC l ON l.DOCTO_CC_ID = s.DOCTO_CC_ID
LEFT JOIN LISTAS_ATRIBUTOS lfp ON lfp.LISTA_ATRIB_ID = l.FORMA_DE_PAGO
WHERE s.CARGO_CANCELADO <> 'S' AND COALESCE(UPPER(lfp.VALOR_DESPLEGADO), 'SEMANAL') <> 'CONTADO'
GROUP BY s.ZONA_CLIENTE_ID ORDER BY 1;

-- M9. Literales de TIPO_VISITA de los últimos 90 días (¿llegan fuera del catálogo de 14?).
SELECT v.TIPO_VISITA, COUNT(*) AS N FROM MSP_VISITAS v
WHERE v.FECHA >= DATEADD(-90 DAY TO CURRENT_DATE) GROUP BY v.TIPO_VISITA ORDER BY 2 DESC;
```

Qué hacer con cada resultado:

| Medición | Si… | Entonces |
|---|---|---|
| M1 + M2 | `MSP_VISITAS` no tiene índice que empiece por `ZONA_CLIENTE_ID` **y** M2 tarda > 1 s o lee > 100 000 registros (`SET STATS`) | Crear `migrations-firebird/000065_idx_msp_visitas_zona_fecha.up.sql` con `CREATE INDEX IDX_MSP_VISITAS_ZONA_FECHA ON MSP_VISITAS (ZONA_CLIENTE_ID, FECHA);` + `COMMIT;` + el `INSERT INTO MSP_MIGRATIONS (ID, NAME, APPLIED_AT) VALUES (65, '000065_idx_msp_visitas_zona_fecha', CURRENT_TIMESTAMP);` (mismo patrón que `000054`), su `.down.sql` con `DROP INDEX IDX_MSP_VISITAS_ZONA_FECHA;`, `make fb-snapshot` antes y `make fb-migrate-up` en dev; en producción, solo con autorización explícita del dueño. En dev hoy existe `IDX_MSP_VISITAS (FECHA, ZONA_CLIENTE_ID, ID, CLIENTE_ID)`: con rango de fecha corto puede bastar. |
| M3 | aparece `87327` con forma `137026`, o `27969` con forma distinta de `137026` | Registrar E-COB; revisar `ClasificarTipoPago` (hoy: condonación si forma 137026 **o** concepto 27969) antes de dar por buenas las cifras. |
| M4 | la diferencia por zona no es cero | Registrar E-COB; el tablero ya separa `pagos` (dinero) de `condonaciones`; documentar en la ayuda qué cifra muestra cada columna (spec §6). |
| M5 | `ES_CARGO` no es la gran mayoría de lo que no es `SIN_VENTA` | **Parar**: la regla de alcance venta del estado por venta está ligando visitas a lo que no es la venta. Avisar al dueño antes de usar Cuentas. |
| M6 | `ZONA_NULA` > 0 | Esas visitas no aparecen en ninguna zona (el repo filtra por `MSP_VISITAS.ZONA_CLIENTE_ID`); registrar y decidir si se filtra por la zona del cliente. |
| M7 | `DOCTO_CC_DUPLICADOS` > 0 | El detalle de pago y las actividades toman `MIN(ID)`: registrar cuántos; no hay fan-out. Las filas `P` con `INTENTOS > 0` son las que el tablero llama "rechazadas" (D-API-2). |
| M8 | — | Referencia de tamaño para la latencia del tablero (una consulta de universo por usuario configurado). |
| M9 | hay literales fuera del catálogo | Se pintan `visite_vuelvo` (como el teléfono); registrar el conteo. |

**C4 — cifras contra rutas y Microsip (después de desplegar, solo lectura):** para tres usuarios de zonas distintas, con `TOKEN` exportado por el dueño:

```bash
U=<uid de Firestore>
curl -s -H "Authorization: Bearer $TOKEN" "https://apidev.loclx.io/v2/desempeno/usuarios?periodo=semana" | jq --arg u "$U" '.usuarios[] | select(.uid==$u) | {cobro, cobertura, pagos}'
curl -s -H "Authorization: Bearer $TOKEN" "https://apidev.loclx.io/v2/rutas/usuarios/$U/cobranza" | jq '{suma_abono: ([.items[].abono_semana|tonumber]|add), pct: .resumen.pct_ponderado}'
```

1. `cobro.cobrado` == `suma_abono` y `cobro.pct` == `pct` (misma fuente: control positivo de la integración con rutas).
2. Cobrado por usuario y día contra Microsip (sustituir `<UID>` y la fecha):
   ```sql
   SELECT CAST(SUM(p.IMPORTE + p.IMPUESTO) AS NUMERIC(18,2)) AS COBRADO, COUNT(*) AS N
   FROM MSP_PAGOS_VENTAS p
   JOIN MSP_PAGOS_RECIBIDOS pr ON pr.IMPTE_DOCTO_CC_ID = p.IMPTE_DOCTO_CC_ID
   JOIN MSP_USUARIOS u ON u.ID = pr.CREATED_BY
   WHERE u.FIREBASE_UID = '<UID>' AND p.CANCELADO = 'N' AND p.APLICADO = 'S' AND p.CONCEPTO_CC_ID = 87327
     AND p.FECHA >= '2026-10-20 00:00:00' AND p.FECHA < '2026-10-21 00:00:00'
     AND (SELECT MIN(fcd.FORMA_COBRO_ID) FROM FORMAS_COBRO_DOCTOS fcd
           WHERE fcd.NOM_TABLA_DOCTOS = 'DOCTOS_CC' AND fcd.DOCTO_ID = p.DOCTO_CC_ID) IN (157, 158, 52569);
   ```
   == `pagos.importe` de la jornada de ese día (`GET /v2/desempeno/usuarios/<UID>/jornada?fecha=2026-10-20`).
3. `cobertura_por_segmento.sin_nada` == `cobertura.total − cobertura.con_algo`, y `conteos.total` de Cuentas == `ventas_universo`.
4. Cada igualdad o diferencia, con fecha, entorno y salida, como E-COB nuevo (MEDIDO, con su control positivo).

## Apéndice 2 — fase posterior (fuera de esta entrega)

- **Permiso de escritura `cobranza:registrar_pagos` en dos pasos** (spec §6). Paso 1: agregar el código al catálogo (`permission_codes.go` + `auth_contracts.go` + prueba), y una migración de datos que lo conceda a todo rol que hoy tenga `cobranza:ver_pagos` (en producción: `cobrador`, más `super_admin` por el sync de arranque): `UPDATE OR INSERT INTO MSP_PERMISOS (CODIGO, DESCRIPCION, CATEGORIA) VALUES ('cobranza:registrar_pagos', 'crear pagos y subir o borrar sus fotos', 'cobranza') MATCHING (CODIGO);` y `INSERT INTO MSP_ROLES_PERMISOS (ROL_ID, PERMISO_CODIGO, CREATED_AT, CREATED_BY) SELECT rp.ROL_ID, 'cobranza:registrar_pagos', CAST('<fecha literal>' AS TIMESTAMP), rp.CREATED_BY FROM MSP_ROLES_PERMISOS rp WHERE rp.PERMISO_CODIGO = 'cobranza:ver_pagos' AND NOT EXISTS (...)` (literal de fecha como `000056`). Medición: `SELECT COUNT(*)` de usuarios con `ver_pagos` y sin `registrar_pagos` = 0, y un usuario de la flota crea un pago en dev. Paso 2 (**no desplegar hasta medir el paso 1 en producción**): `CrearPago`, `AdjuntarImagenPago` y `EliminarImagenPago` exigen `cobranza:registrar_pagos`; enumerar y actualizar los fixtures que crean pagos solo con `ver_pagos` (`grep -rln PermCobranzaVerPagos internal --include='*_test.go'`); prueba "con permiso de lectura no se crea pago ni se borra foto" (403). Nota: `POST /v2/visitas` también escribe con `cobranza:ver_pagos` y el spec no lo menciona.
- **Exportación xlsx** (`GET /v2/desempeno/usuarios/{uid}/actividad.xlsx`, tope 50 000 filas, 422 `desempeno_export_demasiadas_filas`). Recomendación al retomarla: `github.com/xuri/excelize/v2` con `StreamWriter` (Go puro, sin cgo, cruza a `windows/amd64` con `CGO_ENABLED=0`, memoria plana para 50 000 filas); el handler reutiliza `app.Actividad` sin cursor ni límite.

## Self-Review

1. **Cobertura del spec/contrato v2:** §3.1 módulo → Tareas 0, A1–A4, B1–B4, F; §3.2.1 pertenencia → C1 (permisos → Apéndice 2); §3.2.2 fotos de visitas → C2; §3.2.3 `pago_recibido_id` → C3; §3.2.4 `desempeno:ver` → F; contrato §1 (Actividad con `docto_pv_id`, `captura_tardia`, visita con `trato`/`etiqueta`, puntos con popover) → Tarea 0/A2/F; §2 tablero (incl. `cobro.falta`, nulos de no configurado, `sin_nada = total − con_algo`) → A3/F; §3 jornada (`generado_at`) → A3/F; §4 actividad (filtro `forma_cobro` por códigos, `totales.importe` solo dinero) → A4/F; §4b catálogos → Tarea 0/A1/F; §5 cuentas → A4/F; §6 fotos → C1/C2/C3; §7 juego de casos → A1. §7 del spec (pruebas): dominio, integración, seguridad (404/403) y contrato → cada tarea. §6 mediciones y §8 C0/C4 → Apéndice 1. Fuera por decisión del dueño: xlsx, permiso de escritura.
2. **Placeholders:** ninguno en las tareas; los `<UID>`, `<fecha literal>` y `<de fábrica>` del Apéndice son datos que el operador sustituye al medir o que el dueño no quiere en el repositorio (credenciales).
3. **Tipos consistentes:** los nombres de las secciones Interfaces se tomaron del código compilado (mismo worktree en que pasaron las pruebas).
4. **Review Focus:** las cinco entradas tienen prueba en su tarea dueña (A2, A3, A4, B1/B2, C1/C2).

## Resoluciones del arquitecto (2026-10-14) sobre D-API-1 … D-API-10

- D-API-1: se acepta. `pct` = % ponderado de rutas (la cifra que oficina ya conoce en Cobranza semanal); `esperado` = Σ parcialidad de las ventas que aplican; `falta` = max(0, esperado − cobrado). El popover rotula cada cifra.
- D-API-2: se acepta (`ESTADO='P'` con `INTENTOS > 0`), se mide con M7 al final.
- D-API-4: se acepta atribución de visitas por `COBRADOR_ID`; si dos usuarios lo comparten, cuenta para ambos y `usuario_uid` = null.
- D-API-5: se acepta cobertura con toda la actividad de la zona (es la "más correcta" del spec §2).
- D-API-6: se acepta CLDR para meses; "sept" como en el teléfono.
- D-API-7: `nueva` = saldo == total − enganche (regla del teléfono, `OrdenDeCobranza.kt:103`); `producto` = descripción del primer artículo de la venta; `num_atrasos` = fórmula de `overdue_payments_view` del teléfono. Documentar en código con referencia al archivo del teléfono.
- D-API-8: estado por venta igual que el teléfono (pagos pendientes de aplicar cuentan como dinero para el estado); `cobrado`, `pagos.importe` y totales de dinero solo cuentan pagos aplicados y no rechazados; los no aplicados van en `no_aplicados`.
- D-API-9: se acepta población = usuarios Firestore con `COBRADOR_ID > 0`.
- D-API-10: se acepta apagar `$schema` solo en desempeño.
- Evidencia: usar el siguiente identificador libre real de `docs/evidencia/cobranza.md` al ejecutar C4.
- Latencia del tablero: los cálculos por usuario corren en paralelo acotado (errgroup con límite) y se mide en C4.
