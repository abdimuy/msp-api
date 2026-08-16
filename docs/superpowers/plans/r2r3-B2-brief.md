# Task B2 — Endpoint de predicciones: contrato analytics + consumo clientes + HTTP

## Dónde encaja
Repo **msp-api**. La tarea B1 ya agregó `func (b BTYD) Posteriors(in PosteriorInput) Predicciones`
en `internal/analytics/app/btyd_posteriors.go` (intervalos creíbles de PAlive, compras 12m, CLV,
próxima compra). B2 **expone** eso por un endpoint nuevo `GET /v2/clientes/{id}/predicciones`,
mirroring EXACTAMENTE el seam del "pulso": analytics calcula → contrato cross-module → puerto outbound
de clientes → adapter en `cmd/api/clientes_wiring.go` → clientesapp → `clienteshttp`. Lee primero estos
archivos de referencia (son tu plantilla):
- `internal/analytics/app/pulso_query.go` (cómo el service carga el candidato y computa).
- `internal/analytics/app/scoring.go` líneas ~509–660 (`clvVGrid`, `computeCLVConRazones`) — de aquí
  sacas x,tx,n y los inputs de CLV.
- `internal/analytics/analytics_contracts.go` + `analytics_contracts_mapper.go` (patrón de contrato).
- `internal/clientes/ports/outbound/analytics_client.go` (puerto a extender).
- `cmd/api/clientes_wiring.go` (`clientesAnalyticsAdapter` a extender).
- `internal/clientes/infra/clienteshttp/{routes.go,dto.go,dto_mapper.go,handlers_clientes.go}` —
  mirror del endpoint `ObtenerRitmoPago` (GET /clientes/{id}/ritmo-pago).

## Restricciones (CLAUDE.md)
- Hexagonal por slices: cross-module SOLO vía contrato + puerto. Código/comentarios EN inglés;
  vocabulario de dominio español permitido con `//nolint:misspell` (como los archivos vecinos).
- Dinero hacia el cliente como **string** (2 decimales, `decimal.StringFixed(2)`), igual que el resto.
- Fechas UTC RFC3339 (no aplica directamente aquí salvo si agregas alguna fecha).
- **OJO dirección de dependencias:** el paquete raíz `analytics` NO puede importar `analytics/app`
  (app importa a analytics, no al revés). Por eso el **tipo** de contrato vive en
  `analytics_contracts.go` (paquete raíz) pero el **mapper** `app.Predicciones → analytics.PrediccionesContract`
  vive en el paquete `app` (junto al service). NO crees ciclo de imports.

## Parte 1 — analytics (cálculo + contrato)

### 1a. Contrato (en `internal/analytics/analytics_contracts.go`, paquete raíz `analytics`)
```go
// IntervaloContract is a point estimate with its credible interval [Lo, Hi].
type IntervaloContract struct {
    Punto float64
    Lo    float64
    Hi    float64
}

// PrediccionesContract is the cross-module view of a client's Bayesian predictions.
// CLV values are pesos (the HTTP layer formats them as money strings).
type PrediccionesContract struct {
    Disponible          bool
    PAlive              IntervaloContract // probability in [0,1]
    ComprasEsperadas12m IntervaloContract // expected repeat purchases next 12 months
    CLV                 IntervaloContract // pesos
    ProximaCompraDias   IntervaloContract // days until next purchase
    Draws               int
}
```

### 1b. Service method (NUEVO archivo `internal/analytics/app/predicciones_query.go`, paquete `app`)
`func (s *Service) ObtenerPredicciones(ctx context.Context, clienteID int) (analytics.PrediccionesContract, error)`

Lógica (replica el gating y los inputs de CLV para que las predicciones sean consistentes con el score):
1. `c, err := s.repo.GetCandidato(ctx, clienteID)`.
   - Si `err` es `domain.ErrWinbackCandidatoNotFound` (usa `errors.Is` / `apperror.As`) →
     **degrada**: `return analytics.PrediccionesContract{Disponible: false}, nil` (NO es error).
   - Otro error → envuelve `apperror.NewInternal("predicciones_cliente_failed", "error al obtener predicciones del cliente").WithSource(...).WithError(err)`.
2. Gate de historial (igual que `computeCLVConRazones`): si `!s.btyd.Loaded() || !s.clvParams.Loaded()
   || c.FechaPrimerVenta().IsZero() || c.VentasMesesDistintos() < 1` →
   `return analytics.PrediccionesContract{Disponible: false}, nil`.
3. `now := s.clock.Now()`; `x, tx, n := clvVGrid(c, now)` (misma función del paquete).
4. `monetary := c.MonetaryVProm().InexactFloat64()`.
5. P(paga) y pérdida, replicando `computeCLVConRazones`:
   - `live, ok := s.pagos90Recientes(ctx, []int{c.ClienteID()}, now)`; `p90 := pagos90dFor(live, ok, c)`.
   - `cScore, _, _, cAplica := computeCreditoScore(c, now, s.scorecard, p90)`.
   - `pPaga := 1.0; if cAplica { pPaga = float64(cScore.Int()) / 100.0 }`.
   - `saldo := c.Saldo().InexactFloat64()`; `perdida := (1 - pPaga) * saldo * s.clvParams.LGD()`.
6. `in := PosteriorInput{X:x, Tx:tx, N:n, Frequency:x, Monetary:monetary, Draws:0,
      Margin: s.clvParams.Margin(), PPaga: pPaga, PerdidaEsperada: perdida,
      HorizonCLV: s.clvParams.HorizonMonths(), Discount: s.clvParams.MonthlyDiscount()}`.
   (Draws:0 → B1 usa su default 2000.)
7. `p := s.btyd.Posteriors(in)`.
8. Mapea `p` (`app.Predicciones`) → `analytics.PrediccionesContract` con un helper privado en este
   archivo (`func toPrediccionesContract(p Predicciones) analytics.PrediccionesContract`), copiando
   `Disponible`, los 4 intervalos (Punto/Lo/Hi) y `Draws`.

**Nota documentada (ponla en el reporte, no es bug):** para `x==0` (cliente con un solo mes de
compra) `Posteriors` usa `ExpectedAvgProfit(0, monetary)` que devuelve la media poblacional de ticket
(no el observado), levemente optimista. Es aceptable: el endpoint de predicciones es un estimador
propio y su punto puede diferir del score materializado. No lo "arregles" tocando B1.

Verifica los nombres exactos de los campos del `Service` (`s.repo`, `s.clock`, `s.btyd`, `s.scorecard`,
`s.clvParams`) leyendo `service.go` y `pulso_query.go`.

## Parte 2 — clientes (puerto + adapter + app)
1. `internal/clientes/ports/outbound/analytics_client.go`: agrega al interface
   `ObtenerPredicciones(ctx context.Context, clienteID int) (analytics.PrediccionesContract, error)`.
2. `cmd/api/clientes_wiring.go`: en `clientesAnalyticsAdapter` agrega
   `func (a *clientesAnalyticsAdapter) ObtenerPredicciones(ctx, clienteID) (analytics.PrediccionesContract, error) { return a.svc.ObtenerPredicciones(ctx, clienteID) }`.
3. `internal/clientes/app`: agrega método al `Service`
   `func (s *Service) ObtenerPredicciones(ctx context.Context, clienteID int) (analytics.PrediccionesContract, error)`
   que delega al puerto analytics (busca el nombre del campo, p.ej. `s.analytics`). Es un passthrough
   fino (mismo estilo que el resto). Nuevo archivo `internal/clientes/app/obtener_predicciones.go`.

## Parte 3 — clienteshttp (DTO + handler + ruta)
Mirror de `ObtenerRitmoPago`:
1. `dto.go`:
   ```go
   type ObtenerPrediccionesInput struct {
       ID int `path:"id" doc:"ID de Microsip del cliente"`
   }
   type ObtenerPrediccionesOutput struct{ Body PrediccionesDTO }

   type IntervaloDTO struct {
       Punto float64 `json:"punto"`
       Lo    float64 `json:"lo"`
       Hi    float64 `json:"hi"`
   }
   // CLV en pesos como string (2 decimales).
   type IntervaloMoneyDTO struct {
       Punto string `json:"punto"`
       Lo    string `json:"lo"`
       Hi    string `json:"hi"`
   }
   type PrediccionesDTO struct {
       Disponible          bool              `json:"disponible"           doc:"true si hay historial suficiente para predecir"`
       PAlive              IntervaloDTO      `json:"p_alive"              doc:"Probabilidad de seguir activo [0,1] con intervalo creíble"`
       ComprasEsperadas12m IntervaloDTO      `json:"compras_esperadas_12m" doc:"Compras repetidas esperadas próximos 12 meses"`
       CLV                 IntervaloMoneyDTO `json:"clv"                  doc:"Valor de vida en pesos (string, 2 decimales) con intervalo"`
       ProximaCompraDias   IntervaloDTO      `json:"proxima_compra_dias"  doc:"Días estimados hasta la próxima compra"`
       Draws               int               `json:"draws"                doc:"Número de muestras Monte Carlo"`
   }
   ```
2. `dto_mapper.go`: `func prediccionesToDTO(c analytics.PrediccionesContract) PrediccionesDTO`.
   Para CLV usa `decimal.NewFromFloat(c.CLV.Punto).StringFixed(moneyScale)` (y Lo/Hi). Reusa
   `moneyScale` (=2) ya declarado en ese archivo. Los otros intervalos pasan los float64 tal cual.
3. `handlers_clientes.go`: handler espejo de `ObtenerRitmoPago`:
   ```go
   func (h *Handlers) ObtenerPredicciones(ctx context.Context, input *ObtenerPrediccionesInput) (*ObtenerPrediccionesOutput, error) {
       cu, err := currentUserOrError(ctx); if err != nil { return nil, err }
       if err := requirePerm(cu, auth.PermClientesLeer); err != nil { return nil, err }
       pred, err := h.svc.ObtenerPredicciones(ctx, input.ID); if err != nil { return nil, mapAppError(err) }
       out := &ObtenerPrediccionesOutput{}; out.Body = prediccionesToDTO(pred); return out, nil
   }
   ```
   Agrega también la línea de aserción de tipo del handler (junto a las otras `_ func(...) = (*Handlers)(nil).ObtenerRitmoPago`).
4. `routes.go`: `huma.Register(api, huma.Operation{OperationID:"obtener-predicciones",
   Method: http.MethodGet, Path:"/clientes/{id}/predicciones", Summary:"Obtener predicciones del cliente",
   Description:"Predicciones bayesianas (probabilidad de actividad, compras esperadas, CLV y próxima compra) con intervalos creíbles.", ...}, handlers.ObtenerPredicciones)` — copia los campos
   de seguridad/tags de la operación `obtener-ritmo-pago`.

## Limpieza incidental (B1 minor)
En `internal/analytics/app/btyd_posteriors_test.go` hay un helper `newTestRNG` SIN usar (línea ~737).
Elimínalo (o úsalo). Confirma que el paquete sigue compilando y los tests verdes.

## Tests (mandato: robustos en todas las capas)
- **analytics app** (`predicciones_query_test.go`, fake repo, ≥90%): cliente con historial →
  `Disponible=true`, IC contiene el punto (Lo≤Punto≤Hi) en las 4 métricas, Draws>0; not-found →
  `Disponible=false`, err nil; `FechaPrimerVenta` cero o `VentasMesesDistintos<1` → `Disponible=false`;
  error de repo (no not-found) → error envuelto. Reusa el fake repo de los tests existentes del paquete.
- **clientes app** (`obtener_predicciones_test.go`, fake `AnalyticsClient` extendido, ≥90%): delega y
  devuelve el contrato; propaga error del puerto.
- **clienteshttp handler** (en `handlers_test.go` o nuevo, ≥70%): sin permiso → error de auth;
  con permiso → DTO correcto (CLV como strings de 2 decimales, intervalos serializados). Usa el
  patrón de los tests de handler existentes.
- **e2e composición** `TestE2E_ObtenerPredicciones_FullChain`: arma analytics Service real + fake repo,
  pásalo por el adapter (`clientesAnalyticsAdapter`) → clientesapp.Service → handler, y verifica el DTO
  de punta a punta. Mira si ya existe un e2e equivalente para pulso/ficha y síguelo. Si la composición
  exacta no es práctica en un test unitario, documenta y cubre el chain vía el adapter + app + handler
  con fakes mínimos (no inventes infraestructura).
- Output de tests pristino. Corre cobertura por paquete y repórtala.

## Verificación (pega salida en el reporte)
- `go build ./...` y cross-compile Windows: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./...`
- `go test ./internal/analytics/... ./internal/clientes/... -count=1`
- Cobertura: `-coverprofile` por paquete tocado; reporta % de los archivos nuevos.
- `golangci-lint run ./internal/analytics/... ./internal/clientes/... ./cmd/...` → 0 issues.

## Reporte
Escribe el reporte en `docs/superpowers/plans/r2r3-B2-report.md` (archivos creados/tocados, evidencia
TDD si aplica, salida de build/test/cobertura/lint, la nota de x==0, concerns). Devuelve solo (≤15
líneas): Status, commit(s) SHA+subject, resumen de tests en 1 línea, concerns, ruta del reporte.

## Commit
Rama `feat/cliente-360-r2-r3`. Mensaje:
`feat(clientes): endpoint de predicciones bayesianas del cliente`.
Sin `--no-verify`, sin footer de Claude. Puedes hacer 1 commit (o 2: analytics, luego clientes+http).
