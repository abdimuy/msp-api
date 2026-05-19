//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
)

// inMemIdempotencyStore is a minimal idempotency.Store backed by a map for
// E2E tests. Production uses the Postgres-backed store.
type inMemIdempotencyStore struct {
	mu      sync.Mutex
	records map[string]idempotency.Record
}

func newInMemIdempotencyStore() *inMemIdempotencyStore {
	return &inMemIdempotencyStore{records: map[string]idempotency.Record{}}
}

func (s *inMemIdempotencyStore) Get(_ context.Context, key string) (*idempotency.Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.records[key]
	if !ok {
		return nil, nil //nolint:nilnil // (nil, nil) means "not found"
	}
	clone := rec
	return &clone, nil
}

func (s *inMemIdempotencyStore) Save(_ context.Context, rec idempotency.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records[rec.Key] = rec
	return nil
}

// ─── Bulk ──────────────────────────────────────────────────────────────────

// TestE2E_Firebird_Bulk_ManyProductos verifies the INSERT loop holds up
// under a realistic-but-large producto list. 100 rows is comfortably above
// what a vendor types in a normal capture, well below any pathological N.
//
//nolint:paralleltest // E2E tests share a tx and must run serially.
func TestE2E_Firebird_Bulk_ManyProductos(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Productos = make([]venthttp.ProductoDTO, 0, 100)
		for i := range 100 {
			body.Productos = append(body.Productos, venthttp.ProductoDTO{
				ID: uuid.NewString(), ArticuloID: i + 1,
				Articulo: "Producto-" + strconv.Itoa(i),
				Cantidad: "1", PrecioAnual: "100", PrecioCorto: "90", PrecioContado: "80",
				AlmacenOrigenID: intPtr(1), AlmacenDestinoID: intPtr(2),
			})
		}
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Len(t, got.Productos, 100, "all 100 productos must round-trip")
	})
}

// ─── Plan credito transitions ──────────────────────────────────────────────

// TestE2E_Firebird_PlanCredito_FrecuenciaCambio_Happy verifies that an
// edit changing FrecPago from SEMANAL to MENSUAL with a coherent
// dia_cobranza succeeds.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_PlanCredito_FrecuenciaCambio_Happy(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Seed CREDITO + SEMANAL.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.TipoVenta = "CREDITO"
		body.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses: 12, Enganche: "100", Parcialidad: "50", FrecPago: "SEMANAL",
		}
		lunes := "LUNES"
		body.DiaCobranza = &venthttp.DiaCobranzaDTO{Semana: &lunes}
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// PATCH to MENSUAL with a coherent dia_cobranza.
		edit := validHeaderBody()
		edit.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses: 12, Enganche: "100", Parcialidad: "200", FrecPago: "MENSUAL",
		}
		mes15 := 15
		edit.DiaCobranza = &venthttp.DiaCobranzaDTO{Mes: &mes15}
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID, edit)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var out venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
		require.NotNil(t, out.PlanCredito)
		assert.Equal(t, "MENSUAL", out.PlanCredito.FrecPago)
		require.NotNil(t, out.DiaCobranza)
		require.NotNil(t, out.DiaCobranza.Mes)
		assert.Equal(t, 15, *out.DiaCobranza.Mes)
	})
}

// TestE2E_Firebird_PlanCredito_FrecuenciaIncoherente_Rejected verifies the
// (frec_pago, dia_cobranza) coherence rule is enforced on edit.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_PlanCredito_FrecuenciaIncoherente_Rejected(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Seed CREDITO + SEMANAL.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.TipoVenta = "CREDITO"
		body.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses: 12, Enganche: "100", Parcialidad: "50", FrecPago: "SEMANAL",
		}
		lunes := "LUNES"
		body.DiaCobranza = &venthttp.DiaCobranzaDTO{Semana: &lunes}
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// Keep SEMANAL but switch dia_cobranza to a day-of-month — the
		// domain rule requires SEMANAL → dia_semana, not dia_mes.
		edit := validHeaderBody()
		edit.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses: 12, Enganche: "100", Parcialidad: "50", FrecPago: "SEMANAL",
		}
		mes15 := 15
		edit.DiaCobranza = &venthttp.DiaCobranzaDTO{Mes: &mes15} // wrong axis for SEMANAL
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID, edit)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
			"incoherent (frec_pago=SEMANAL, dia=mes) must be rejected; body=%s", rec.Body.String())
	})
}

// ─── Outbox payload verification ───────────────────────────────────────────

// e2eRecordingOutbox captures every Enqueue invocation made during the
// test so assertions can inspect event types and payloads end-to-end.
type e2eRecordingOutbox struct {
	mu    sync.Mutex
	calls []recordedEvent
}

type recordedEvent struct {
	aggregate   string
	aggregateID uuid.UUID
	eventType   string
	payload     any
}

func (o *e2eRecordingOutbox) Enqueue(_ context.Context, aggregate string, aggregateID uuid.UUID, eventType string, payload any) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls = append(o.calls, recordedEvent{aggregate, aggregateID, eventType, payload})
	return nil
}

func (o *e2eRecordingOutbox) snapshot() []recordedEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]recordedEvent, len(o.calls))
	copy(out, o.calls)
	return out
}

// TestE2E_Firebird_EditEvents_PayloadIntegrity drives every edit endpoint
// against the real Firebird stack and asserts each Enqueue carries the
// correct event_type, venta_id, and (when applicable) counts.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_EditEvents_PayloadIntegrity(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		recorder := &e2eRecordingOutbox{}
		svc := buildE2EServiceWithOutbox(pool, recorder)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		id := e2eSeedVenta(t, r, usuarioID)
		ventaUUID, err := uuid.Parse(id)
		require.NoError(t, err)

		// 1. PATCH header.
		req := jsonRequest(t, http.MethodPatch, "/ventas/"+id, validHeaderBody())
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// 2. PATCH cliente.
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+id+"/cliente", validClienteBody())
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// 3. PUT productos (2 items).
		two, three := 2, 3
		productosBody := venthttp.ReemplazarProductosBody{Productos: []venthttp.ProductoDTO{
			{
				ID: uuid.NewString(), ArticuloID: 1, Articulo: "A",
				Cantidad: "1", PrecioAnual: "1", PrecioCorto: "1", PrecioContado: "1",
				AlmacenOrigenID: &two, AlmacenDestinoID: &three,
			},
			{
				ID: uuid.NewString(), ArticuloID: 2, Articulo: "B",
				Cantidad: "1", PrecioAnual: "1", PrecioCorto: "1", PrecioContado: "1",
				AlmacenOrigenID: &two, AlmacenDestinoID: &three,
			},
		}}
		req = jsonRequest(t, http.MethodPut, "/ventas/"+id+"/productos", productosBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// 4. PUT combos (1 item).
		req = jsonRequest(t, http.MethodPut, "/ventas/"+id+"/combos", validCombosBody())
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// 5. PUT vendedores (1 item) — use a real usuario (FK) and a unique
		// (venta, usuario) pair. We can replace with the SAME usuario that's
		// already assigned (idempotent reassign).
		vendBody := venthttp.ReemplazarVendedoresBody{Vendedores: []venthttp.VendedorDTO{{
			ID: uuid.NewString(), UsuarioID: usuarioID.String(),
			Email: "vend-" + uuid.NewString() + "@x.com", Nombre: "Vend",
		}}}
		req = jsonRequest(t, http.MethodPut, "/ventas/"+id+"/vendedores", vendBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// Inspect captured events.
		captured := recorder.snapshot()
		byType := map[string]recordedEvent{}
		for _, c := range captured {
			byType[c.eventType] = c
			assert.Equal(t, "venta", c.aggregate)
			assert.Equal(t, ventaUUID, c.aggregateID)
		}
		// Every edit must fire its corresponding event.
		for _, expected := range []string{
			"venta.creada",
			"venta.header_actualizado",
			"venta.cliente_actualizado",
			"venta.productos_reemplazados",
			"venta.combos_reemplazados",
			"venta.vendedores_reemplazados",
		} {
			_, ok := byType[expected]
			assert.True(t, ok, "missing event type %s in outbox (got %v)", expected, captured)
		}

		// Payload integrity: counts on Replace events.
		if ev, ok := byType["venta.productos_reemplazados"]; ok {
			payload, _ := ev.payload.(map[string]any)
			assert.Equal(t, ventaUUID.String(), payload["venta_id"])
			assert.Equal(t, 2, payload["productos_count"])
		}
		if ev, ok := byType["venta.combos_reemplazados"]; ok {
			payload, _ := ev.payload.(map[string]any)
			assert.Equal(t, 1, payload["combos_count"])
		}
		if ev, ok := byType["venta.vendedores_reemplazados"]; ok {
			payload, _ := ev.payload.(map[string]any)
			assert.Equal(t, 1, payload["vendedores_count"])
		}
	})
}

// ─── Concurrency: last-write-wins ──────────────────────────────────────────

// TestE2E_Firebird_ConcurrentEdit_LastWriteWins documents the
// no-optimistic-locking behavior. Two operators load the same venta, edit
// different fields, and both PATCH. The second write clobbers the first —
// no error, no conflict surface. If we ever add a row version column,
// this test will start failing and we'll need to revisit.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_ConcurrentEdit_LastWriteWins(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		id := e2eSeedVenta(t, r, usuarioID)

		// Operator A: edits direccion.
		bodyA := validHeaderBody()
		bodyA.Direccion.Calle = "Calle de A"
		reqA := jsonRequest(t, http.MethodPatch, "/ventas/"+id, bodyA)
		recA := httptest.NewRecorder()
		r.ServeHTTP(recA, reqA)
		require.Equal(t, http.StatusOK, recA.Code, recA.Body.String())

		// Operator B: edits direccion to a different value (was based on
		// the same initial state as A would have loaded — no row version).
		bodyB := validHeaderBody()
		bodyB.Direccion.Calle = "Calle de B"
		reqB := jsonRequest(t, http.MethodPatch, "/ventas/"+id, bodyB)
		recB := httptest.NewRecorder()
		r.ServeHTTP(recB, reqB)
		require.Equal(t, http.StatusOK, recB.Code, recB.Body.String())

		// Persisted state reflects B's write (last-write-wins).
		reqG := httptest.NewRequest(http.MethodGet, "/ventas/"+id, nil)
		recG := httptest.NewRecorder()
		r.ServeHTTP(recG, reqG)
		require.Equal(t, http.StatusOK, recG.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(recG.Body.Bytes(), &got))
		assert.Equal(t, "Calle de B", got.Direccion.Calle,
			"last-write-wins: B's value must clobber A's silently")
	})
}

// ─── Empty collections ─────────────────────────────────────────────────────

// TestE2E_Firebird_ReemplazarCombos_EmptyOK verifies that dropping every
// combo from a venta whose productos do NOT reference any combo is a happy
// path (no orphan FK violation).
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_ReemplazarCombos_EmptyOK(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Seed venta + 1 combo (productos do NOT reference it).
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.Combos = []venthttp.ComboDTO{{
			ID: uuid.NewString(), Nombre: "C", PrecioAnual: "1", PrecioCorto: "1", PrecioContado: "1",
			Cantidad: "1", AlmacenOrigenID: 1, AlmacenDestinoID: 2,
		}}
		// Productos remain stand-alone (carry their own almacenes, no combo_id).
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// PUT combos = []  → must succeed.
		emptyBody := venthttp.ReemplazarCombosBody{Combos: []venthttp.ComboDTO{}}
		req = jsonRequest(t, http.MethodPut, "/ventas/"+body.ID+"/combos", emptyBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// Verify: zero combos persisted.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Empty(t, got.Combos)
		assert.Len(t, got.Productos, 1, "stand-alone producto must survive untouched")
	})
}

// ─── Nullable fields round-trip ────────────────────────────────────────────

// TestE2E_Firebird_NullableFields_RoundTrip exercises the optional columns
// (nota, numero_exterior, zona_cliente_id, telefono, aval, cliente_id):
// create with all set → edit clearing them → read back with all null.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_NullableFields_RoundTrip(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// Seed with everything populated.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		nota := "nota inicial"
		numExt := "42-A"
		zona := 7
		tel := "+15551234567"
		aval := "Avalista"
		body.Nota = &nota
		body.Direccion.NumeroExterior = &numExt
		body.Direccion.ZonaClienteID = &zona
		body.Cliente.Telefono = &tel
		body.Cliente.Aval = &aval
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

		// PATCH header clearing nota + numero_exterior + zona_cliente_id.
		edit := validHeaderBody()
		edit.Nota = nil
		edit.Direccion.NumeroExterior = nil
		edit.Direccion.ZonaClienteID = nil
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID, edit)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// PATCH cliente clearing telefono + aval.
		clearCliente := venthttp.ActualizarClienteBody{
			Cliente: venthttp.ClienteSnapshotDTO{Nombre: "Cliente Limpio"},
		}
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cliente", clearCliente)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		// GET back — every optional must be null.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var got venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Nil(t, got.Nota)
		assert.Nil(t, got.Direccion.NumeroExterior)
		assert.Nil(t, got.Direccion.ZonaClienteID)
		assert.Nil(t, got.Cliente.Telefono)
		assert.Nil(t, got.Cliente.Aval)
		assert.Nil(t, got.Cliente.ClienteID, "cliente_id never set → must be null")
	})
}

// ─── A7: Full lifecycle ────────────────────────────────────────────────────

// TestE2E_Firebird_FullLifecycle drives one venta through every editing
// endpoint we expose, in sequence, against the real Firebird DB inside a
// rollback-only tx. It exists to catch regressions where individual
// endpoints work in isolation but interact badly when composed.
//
//nolint:paralleltest,funlen // long sequential journey against shared tx.
func TestE2E_Firebird_FullLifecycle(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		altVendedor := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		// 1. POST — create CREDITO venta.
		body := validCreateBody()
		body.Vendedores[0].UsuarioID = usuarioID.String()
		body.TipoVenta = "CREDITO"
		body.PlanCredito = &venthttp.PlanCreditoDTO{
			PlazoMeses: 12, Enganche: "100.00", Parcialidad: "150.00", FrecPago: "SEMANAL",
		}
		lunes := "LUNES"
		body.DiaCobranza = &venthttp.DiaCobranzaDTO{Semana: &lunes}
		req := jsonRequest(t, http.MethodPost, "/ventas", body)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusCreated, rec.Code, "create: %s", rec.Body.String())

		ventaID := body.ID

		// 2. GET — verify creation.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get after create: %s", rec.Body.String())
		var created venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "CREDITO", created.TipoVenta)
		assert.Equal(t, "borrador", created.Status)

		// 3. PATCH header — change direccion, fechaVenta, montos.
		hdr := validHeaderBody()
		hdr.Direccion.Calle = "Calle Lifecycle"
		hdr.PlanCredito = body.PlanCredito
		hdr.DiaCobranza = body.DiaCobranza
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+ventaID, hdr)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "patch header: %s", rec.Body.String())

		// 4. PATCH cliente — change nombre.
		clienteBody := validClienteBody()
		clienteBody.Cliente.Nombre = "Cliente Lifecycle"
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+ventaID+"/cliente", clienteBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "patch cliente: %s", rec.Body.String())

		// 5. PUT productos — 3 stand-alone productos across 2 almacenes.
		two, three := 2, 3
		productosBody := venthttp.ReemplazarProductosBody{Productos: []venthttp.ProductoDTO{
			{
				ID: uuid.NewString(), ArticuloID: 10, Articulo: "Cama",
				Cantidad: "1", PrecioAnual: "100", PrecioCorto: "90", PrecioContado: "80",
				AlmacenOrigenID: intPtr(1), AlmacenDestinoID: &two,
			},
			{
				ID: uuid.NewString(), ArticuloID: 11, Articulo: "Mesa",
				Cantidad: "1", PrecioAnual: "50", PrecioCorto: "45", PrecioContado: "40",
				AlmacenOrigenID: intPtr(1), AlmacenDestinoID: &two,
			},
			{
				ID: uuid.NewString(), ArticuloID: 12, Articulo: "Silla",
				Cantidad: "2", PrecioAnual: "30", PrecioCorto: "28", PrecioContado: "25",
				AlmacenOrigenID: &two, AlmacenDestinoID: &three,
			},
		}}
		req = jsonRequest(t, http.MethodPut, "/ventas/"+ventaID+"/productos", productosBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "put productos: %s", rec.Body.String())

		// 6. PUT combos — add a combo, then PUT productos again moving one
		// producto into the combo.
		comboID := uuid.NewString()
		combosBody := venthttp.ReemplazarCombosBody{Combos: []venthttp.ComboDTO{{
			ID: comboID, Nombre: "Set Cocina",
			PrecioAnual: "500", PrecioCorto: "450", PrecioContado: "400",
			Cantidad: "1", AlmacenOrigenID: 1, AlmacenDestinoID: 2,
		}}}
		req = jsonRequest(t, http.MethodPut, "/ventas/"+ventaID+"/combos", combosBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "put combos: %s", rec.Body.String())

		// Re-PUT productos: silla now lives inside the combo (no almacenes).
		productosBody.Productos[2].ComboID = &comboID
		productosBody.Productos[2].AlmacenOrigenID = nil
		productosBody.Productos[2].AlmacenDestinoID = nil
		req = jsonRequest(t, http.MethodPut, "/ventas/"+ventaID+"/productos", productosBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "put productos (combo-child move): %s", rec.Body.String())

		// 7. PUT vendedores — replace with a different usuario.
		vendBody := venthttp.ReemplazarVendedoresBody{Vendedores: []venthttp.VendedorDTO{{
			ID: uuid.NewString(), UsuarioID: altVendedor.String(),
			Email: "lifecycle-" + uuid.NewString() + "@x.com", Nombre: "Vend Lifecycle",
		}}}
		req = jsonRequest(t, http.MethodPut, "/ventas/"+ventaID+"/vendedores", vendBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "put vendedores: %s", rec.Body.String())

		// 8. POST imagen — upload via multipart.
		uploadBody := multipartImageBytes(t, 0x42)
		uploadReq := httptest.NewRequest(http.MethodPost,
			"/ventas/"+ventaID+"/imagenes", uploadBody)
		uploadReq.Header.Set("Content-Type", multipartContentType())
		uploadRec := httptest.NewRecorder()
		r.ServeHTTP(uploadRec, uploadReq)
		require.Equal(t, http.StatusCreated, uploadRec.Code, "upload imagen: %s", uploadRec.Body.String())
		var uploaded venthttp.ImagenDTO
		require.NoError(t, json.Unmarshal(uploadRec.Body.Bytes(), &uploaded))

		// 9. GET imagen — verify bytes.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID+"/imagenes/"+uploaded.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "get imagen bytes: %d", rec.Code)
		require.Equal(t, []byte{0x42, 0x42, 0x42, 0x42}, rec.Body.Bytes())

		// 10. DELETE imagen.
		req = httptest.NewRequest(http.MethodDelete, "/ventas/"+ventaID+"/imagenes/"+uploaded.ID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNoContent, rec.Code, "delete imagen: %s", rec.Body.String())

		// 11. GET — verify aggregate state after edits.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var mid venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &mid))
		assert.Equal(t, "Calle Lifecycle", mid.Direccion.Calle)
		assert.Equal(t, "Cliente Lifecycle", mid.Cliente.Nombre)
		assert.Len(t, mid.Productos, 3)
		assert.Len(t, mid.Combos, 1)
		assert.Len(t, mid.Vendedores, 1)
		assert.Empty(t, mid.Imagenes, "imagen must be deleted")
		assert.Equal(t, "borrador", mid.Status)

		// 12. PATCH cancel.
		cancelBody, err := json.Marshal(venthttp.CancelarVentaBody{Reason: "lifecycle cancel"})
		require.NoError(t, err)
		req = httptest.NewRequest(http.MethodPatch, "/ventas/"+ventaID+"/cancel", bytes.NewReader(cancelBody))
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "cancel: %s", rec.Body.String())

		// 13. PATCH header after cancel → 409.
		req = jsonRequest(t, http.MethodPatch, "/ventas/"+ventaID, hdr)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusConflict, rec.Code, "patch header after cancel must be 409: %s", rec.Body.String())

		// 14. PUT productos after cancel → 409.
		req = jsonRequest(t, http.MethodPut, "/ventas/"+ventaID+"/productos", productosBody)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusConflict, rec.Code, "put productos after cancel must be 409: %s", rec.Body.String())

		// 15. GET — final state cancelled.
		req = httptest.NewRequest(http.MethodGet, "/ventas/"+ventaID, nil)
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		var final venthttp.VentaDTO
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &final))
		assert.Equal(t, "cancelada", final.Status)
		require.NotNil(t, final.Cancelacion)
		assert.Equal(t, "lifecycle cancel", final.Cancelacion.Reason)
	})
}

// ─── B5: extreme dates ─────────────────────────────────────────────────────

// TestE2E_Firebird_ExtremeDates documents how fecha_venta survives the
// round-trip when set to the extremes of a sensible business range, plus
// the platform's own absolute limits. We don't pin "must succeed" for the
// year-1 case (Firebird's TIMESTAMP range starts in 0001-01-01 on paper
// but drivers vary); we just confirm the response is sane (4xx or 2xx,
// never 5xx) and any accepted value round-trips exactly.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_ExtremeDates(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		cases := []struct {
			name       string
			fechaVenta string
		}{
			{"year 1900", "1900-01-01T00:00:00Z"},
			{"year 9999", "9999-12-31T23:59:59Z"},
			{"year 1 (Go zero +1)", "0001-01-01T00:00:00Z"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				body := validCreateBody()
				body.Vendedores[0].UsuarioID = usuarioID.String()
				body.FechaVenta = tc.fechaVenta

				req := jsonRequest(t, http.MethodPost, "/ventas", body)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				t.Logf("%s POST status=%d body=%s", tc.name, rec.Code, rec.Body.String())
				require.Less(t, rec.Code, 500, "must NOT 5xx: %s", rec.Body.String())

				if rec.Code != http.StatusCreated {
					return
				}
				// Round-trip: GET and compare fechaVenta.
				req = httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
				rec = httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				require.Equal(t, http.StatusOK, rec.Code)
				var got venthttp.VentaDTO
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
				assert.Equal(t, tc.fechaVenta, got.FechaVenta,
					"extreme fecha_venta must round-trip exactly via BusinessTZ wall-clock contract")
			})
		}
	})
}

// ─── B7: combo cantidad fractional/extreme ────────────────────────────────

// TestE2E_Firebird_Combo_FractionalCantidad pins the semantics of
// non-integer / borderline cantidades on combos. The domain enforces
// `Cantidad > 0` (no integer requirement), so fractional values must be
// accepted; very-small (0.0001 NUMERIC(10,4) min) must round-trip; zero
// and negative must be rejected with a typed 4xx.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_Combo_FractionalCantidad(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		mk := func(t *testing.T, cantidad string) *httptest.ResponseRecorder {
			t.Helper()
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Combos = []venthttp.ComboDTO{{
				ID: uuid.NewString(), Nombre: "Frac",
				PrecioAnual: "100", PrecioCorto: "90", PrecioContado: "80",
				Cantidad: cantidad, AlmacenOrigenID: 1, AlmacenDestinoID: 2,
			}}
			req := jsonRequest(t, http.MethodPost, "/ventas", body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			return rec
		}

		t.Run("fractional 0.5 OK", func(t *testing.T) {
			rec := mk(t, "0.5")
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		})
		t.Run("min positive 0.0001 OK", func(t *testing.T) {
			rec := mk(t, "0.0001")
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		})
		t.Run("large 999999.9999 OK", func(t *testing.T) {
			rec := mk(t, "999999.9999")
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		})
		t.Run("zero rejected", func(t *testing.T) {
			rec := mk(t, "0")
			require.GreaterOrEqual(t, rec.Code, 400, "zero must be 4xx")
			require.Less(t, rec.Code, 500, "zero must not 5xx: %s", rec.Body.String())
		})
		t.Run("negative rejected", func(t *testing.T) {
			rec := mk(t, "-1")
			require.GreaterOrEqual(t, rec.Code, 400, "negative must be 4xx")
			require.Less(t, rec.Code, 500, "negative must not 5xx: %s", rec.Body.String())
		})
	})
}

// ─── B3: Idempotency-Key replay on PATCH ──────────────────────────────────

// TestE2E_Firebird_IdempotencyKey_PATCH_Replays verifies that two identical
// PATCH requests carrying the same Idempotency-Key produce the same response
// AND only enqueue the outbox event once. The idempotency middleware is
// applied to POST and PATCH by default; the ventas E2E router doesn't mount
// it by default (production wires it in main.go), so this test mounts it
// explicitly to confirm the contract holds end-to-end.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_IdempotencyKey_PATCH_Replays(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		recorder := &e2eRecordingOutbox{}
		svc := buildE2EServiceWithOutbox(pool, recorder)

		store := newInMemIdempotencyStore()
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		r.Use(idempotency.Middleware(idempotency.Config{
			Store:      store,
			Methods:    []string{http.MethodPost, http.MethodPatch},
			RequireKey: false,
		}))
		venthttp.MountRouter(r, svc)

		ventaID := e2eSeedVenta(t, r, usuarioID)
		creationEvents := len(recorder.snapshot())

		key := "test-idem-" + uuid.NewString()
		bodyBytes, err := json.Marshal(validHeaderBody())
		require.NoError(t, err)

		// First PATCH with Idempotency-Key — should run.
		req1 := httptest.NewRequest(http.MethodPatch, "/ventas/"+ventaID, bytes.NewReader(bodyBytes))
		req1.Header.Set("Content-Type", "application/json")
		req1.Header.Set(idempotency.HeaderKey, key)
		rec1 := httptest.NewRecorder()
		r.ServeHTTP(rec1, req1)
		require.Equal(t, http.StatusOK, rec1.Code, "first PATCH: %s", rec1.Body.String())
		afterFirst := len(recorder.snapshot())

		// Second PATCH with the SAME key + body — should be replayed.
		req2 := httptest.NewRequest(http.MethodPatch, "/ventas/"+ventaID, bytes.NewReader(bodyBytes))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set(idempotency.HeaderKey, key)
		rec2 := httptest.NewRecorder()
		r.ServeHTTP(rec2, req2)
		require.Equal(t, http.StatusOK, rec2.Code, "second PATCH: %s", rec2.Body.String())

		// Bodies must match exactly.
		assert.Equal(t, rec1.Body.String(), rec2.Body.String(),
			"replay must return identical body")
		// Replay header set on replayed response.
		assert.Equal(t, "true", rec2.Header().Get("Idempotent-Replay"),
			"second call must be flagged as replay")

		// Outbox must NOT have fired again on the replay.
		afterSecond := len(recorder.snapshot())
		assert.Equal(t, afterFirst, afterSecond,
			"replay must not duplicate outbox enqueue: before=%d after_first=%d after_second=%d",
			creationEvents, afterFirst, afterSecond)
	})
}

// ─── B1: Unicode edge cases ────────────────────────────────────────────────

// TestE2E_Firebird_UnicodeEdgeCases pins the domain contract for cliente
// nombre against the WIN1252-charset Firebird connection. Domain validation
// rejects characters that can't be persisted exactly (emoji, NUL byte,
// supplementary planes) at the boundary, with a typed 422 — they never reach
// the driver. Extended-Latin (WIN1252-representable) characters round-trip
// exactly.
//
//nolint:paralleltest // serial.
func TestE2E_Firebird_UnicodeEdgeCases(t *testing.T) {
	pool := e2eTestPool(t)
	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		usuarioID := seedE2EUsuario(ctx, t, pool)
		svc := buildE2EService(pool)
		r := chi.NewRouter()
		r.Use(txInjector(ctx))
		r.Use(planter(e2eFullPermsUser(usuarioID)))
		venthttp.MountRouter(r, svc)

		post := func(t *testing.T, nombre string) *httptest.ResponseRecorder {
			t.Helper()
			body := validCreateBody()
			body.Vendedores[0].UsuarioID = usuarioID.String()
			body.Cliente.Nombre = nombre
			req := jsonRequest(t, http.MethodPost, "/ventas", body)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			return rec
		}

		t.Run("emoji rejected with 422", func(t *testing.T) {
			rec := post(t, "José 🎉")
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"emoji must be rejected with 422 (not WIN1252-representable): %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "string_unsafe_chars",
				"error code must identify the unsafe-chars rule")
		})

		t.Run("NUL byte rejected with 422", func(t *testing.T) {
			rec := post(t, "José\x00Pérez")
			require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"NUL byte must be rejected with 422: %s", rec.Body.String())
			assert.Contains(t, rec.Body.String(), "string_unsafe_chars")
		})

		t.Run("extended latin round-trips", func(t *testing.T) {
			nombre := "Müller Ñoño Pérez"
			rec := post(t, nombre)
			require.Equal(t, http.StatusCreated, rec.Code,
				"WIN1252-representable accents must succeed: %s", rec.Body.String())
			var created venthttp.VentaDTO
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
			assert.Equal(t, nombre, created.Cliente.Nombre, "must round-trip exactly")
		})
	})
}
