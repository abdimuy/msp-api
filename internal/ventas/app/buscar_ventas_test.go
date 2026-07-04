//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// ─── Validation ─────────────────────────────────────────────────────────────

func TestBuscarVentas_Validation(t *testing.T) {
	t.Parallel()

	t.Run("rejects_invalid_sort_by", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{SortBy: "no_existe"})
		require.ErrorIs(t, err, app.ErrVentaSortByInvalido)
	})

	t.Run("rejects_invalid_sort_order", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{SortOrder: "sideways"})
		require.ErrorIs(t, err, app.ErrVentaSortOrderInvalido)
	})

	t.Run("accepts_empty_sort_fields", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.NoError(t, err)
	})

	t.Run("accepts_every_allowed_sort_by", func(t *testing.T) {
		t.Parallel()
		for _, sb := range []string{"fecha_venta", "precio_total", "nombre_cliente"} {
			h := newHarness(t)
			_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{SortBy: sb, SortOrder: "asc"})
			require.NoError(t, err, "sort_by=%s", sb)
		}
	})
}

// ─── Fallback path (no index wired) ────────────────────────────────────────

func TestBuscarVentas_Fallback(t *testing.T) {
	t.Parallel()

	t.Run("calls_List_with_mapped_filters_and_drops_meili_only_fields", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.seedVenta(t)

		desde := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		hasta := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
		vend := uuid.New()
		clienteID := 123
		zona := 55
		vendedorEmail := "vendedor@example.com"
		precioMin := decimal.NewFromInt(100)

		in := app.BuscarVentasInput{
			Q:                 "buscar algo", // Meili-only, must NOT leak
			Desde:             &desde,
			Hasta:             &hasta,
			VendedorUsuarioID: &vend,
			ClienteID:         &clienteID,
			TipoVenta:         "CONTADO",
			Situacion:         "aprobada",
			Sincronizacion:    "aplicada",
			IncluirCanceladas: true,
			ZonaClienteID:     &zona,          // Meili-only, must NOT leak
			VendedorEmail:     &vendedorEmail, // Meili-only, must NOT leak
			PrecioMin:         &precioMin,     // Meili-only, must NOT leak
			SortBy:            "precio_total", // Meili-only, must NOT leak
			SortOrder:         "desc",
			Cursor:            "some-cursor",
			Limit:             7,
		}

		page, err := h.svc.BuscarVentas(t.Context(), in)
		require.NoError(t, err)
		assert.NotNil(t, page.Items)

		require.Equal(t, 1, h.ventas.ListCalls)
		assert.Equal(t, outbound.ListParams{Cursor: "some-cursor", PageSize: 7}, h.ventas.LastListParams)
		assert.Equal(t, outbound.ListVentasFilters{
			Desde:             &desde,
			Hasta:             &hasta,
			VendedorUsuarioID: &vend,
			ClienteID:         &clienteID,
			TipoVenta:         "CONTADO",
			Situacion:         "aprobada",
			Sincronizacion:    "aplicada",
			IncluirCanceladas: true,
		}, h.ventas.LastListFilters)
	})

	t.Run("repo_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("list failed")
		h.ventas.ListErr = boom

		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.ErrorIs(t, err, boom)
	})
}

// ─── Meilisearch path (index wired) ────────────────────────────────────────

// buscarHarness extends testHarness with a wired fakeVentaSearchIndex.
type buscarHarness struct {
	*testHarness
	index *fakeVentaSearchIndex
}

func newBuscarHarness(t *testing.T) *buscarHarness {
	t.Helper()
	h := newHarness(t)
	idx := &fakeVentaSearchIndex{}
	h.svc = h.svc.WithSearchIndex(idx)
	return &buscarHarness{testHarness: h, index: idx}
}

func TestBuscarVentas_Meili_QueryBuilding(t *testing.T) {
	t.Parallel()

	t.Run("copies_every_field_pointer_filters_nil_when_unset", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)

		in := app.BuscarVentasInput{Q: "juan perez"}
		_, err := h.svc.BuscarVentas(t.Context(), in)
		require.NoError(t, err)

		got := h.index.LastQuery
		assert.Equal(t, "juan perez", got.Q)
		assert.Nil(t, got.TipoVenta)
		assert.Nil(t, got.Situacion)
		assert.Nil(t, got.Sincronizacion)
		assert.Nil(t, got.ZonaClienteID)
		assert.Nil(t, got.VendedorEmail)
		assert.Nil(t, got.ClienteID)
		assert.Nil(t, got.PrecioMin)
		assert.Nil(t, got.PrecioMax)
		assert.False(t, got.IncluirCanceladas)
		assert.Equal(t, 0, got.Offset)
		assert.Equal(t, 50, got.Limit) // default when Limit <= 0
	})

	t.Run("sets_every_pointer_filter_when_present", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)

		desde := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
		hasta := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
		clienteID := 77
		zona := 9
		vendedorEmail := "ana@example.com"
		precioMin := decimal.NewFromInt(500)
		precioMax := decimal.NewFromInt(5000)

		in := app.BuscarVentasInput{
			Desde:             &desde,
			Hasta:             &hasta,
			ClienteID:         &clienteID,
			TipoVenta:         "CREDITO",
			Situacion:         "revisada",
			Sincronizacion:    "pendiente",
			IncluirCanceladas: true,
			ZonaClienteID:     &zona,
			VendedorEmail:     &vendedorEmail,
			PrecioMin:         &precioMin,
			PrecioMax:         &precioMax,
			SortBy:            "nombre_cliente",
			SortOrder:         "asc",
			Cursor:            "o40",
			Limit:             10,
		}
		_, err := h.svc.BuscarVentas(t.Context(), in)
		require.NoError(t, err)

		got := h.index.LastQuery
		require.NotNil(t, got.TipoVenta)
		assert.Equal(t, "CREDITO", *got.TipoVenta)
		require.NotNil(t, got.Situacion)
		assert.Equal(t, "revisada", *got.Situacion)
		require.NotNil(t, got.Sincronizacion)
		assert.Equal(t, "pendiente", *got.Sincronizacion)
		require.NotNil(t, got.ZonaClienteID)
		assert.Equal(t, zona, *got.ZonaClienteID)
		require.NotNil(t, got.VendedorEmail)
		assert.Equal(t, vendedorEmail, *got.VendedorEmail)
		require.NotNil(t, got.ClienteID)
		assert.Equal(t, clienteID, *got.ClienteID)
		require.NotNil(t, got.PrecioMin)
		assert.True(t, precioMin.Equal(*got.PrecioMin))
		require.NotNil(t, got.PrecioMax)
		assert.True(t, precioMax.Equal(*got.PrecioMax))
		assert.True(t, got.IncluirCanceladas)
		assert.Equal(t, got.FechaDesde, &desde)
		assert.Equal(t, got.FechaHasta, &hasta)
		assert.Equal(t, "nombre_cliente", got.SortBy)
		assert.Equal(t, "asc", got.SortOrder)
		assert.Equal(t, 40, got.Offset)
		assert.Equal(t, 10, got.Limit)
	})

	t.Run("clamps_oversized_limit", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{Limit: 10_000})
		require.NoError(t, err)
		assert.Equal(t, 500, h.index.LastQuery.Limit)
	})
}

func TestBuscarVentas_Meili_Hydration(t *testing.T) {
	t.Parallel()

	t.Run("findByIDs_called_with_buscar_ids_and_results_reordered", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		id0 := *h.seedVenta(t)
		id1 := *h.seedVenta(t)
		id2 := *h.seedVenta(t)

		// Meili result order deliberately different from insertion order.
		meiliOrder := []uuid.UUID{id2, id0, id1}
		h.index.Result = outbound.VentasSearchResultado{IDs: meiliOrder, Total: 3}

		page, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.NoError(t, err)

		require.Len(t, h.ventas.FindByIDsCalls, 1)
		assert.ElementsMatch(t, meiliOrder, h.ventas.FindByIDsCalls[0])

		require.Len(t, page.Items, 3)
		gotOrder := make([]uuid.UUID, len(page.Items))
		for i, v := range page.Items {
			gotOrder[i] = v.ID()
		}
		assert.Equal(t, meiliOrder, gotOrder, "hydrated ventas must be reordered to Meili's result order")
	})

	t.Run("indexed_but_missing_id_dropped_without_disordering", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		id0 := *h.seedVenta(t)
		id1 := *h.seedVenta(t)
		ghost := uuid.New() // never persisted — simulates indexed-but-deleted

		meiliOrder := []uuid.UUID{id1, ghost, id0}
		h.index.Result = outbound.VentasSearchResultado{IDs: meiliOrder, Total: 3}

		page, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.NoError(t, err)

		require.Len(t, page.Items, 2)
		assert.Equal(t, id1, page.Items[0].ID())
		assert.Equal(t, id0, page.Items[1].ID())
	})

	t.Run("buscar_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		boom := errors.New("meilisearch down")
		h.index.Err = boom

		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.ErrorIs(t, err, boom)
		assert.Empty(t, h.ventas.FindByIDsCalls, "FindByIDs must not be called when Buscar fails")
	})

	t.Run("findByIDs_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		h.index.Result = outbound.VentasSearchResultado{IDs: []uuid.UUID{uuid.New()}, Total: 1}
		boom := errors.New("firebird down")
		h.ventas.FindByIDsErr = boom

		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.ErrorIs(t, err, boom)
	})
}

func TestBuscarVentas_Meili_NextCursor(t *testing.T) {
	t.Parallel()

	t.Run("more_pages_available", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		ids := []uuid.UUID{*h.seedVenta(t), *h.seedVenta(t)}
		h.index.Result = outbound.VentasSearchResultado{IDs: ids, Total: 100}

		page, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{Limit: 2})
		require.NoError(t, err)
		assert.Equal(t, "o2", page.NextCursor)
	})

	t.Run("last_page_by_total", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		ids := []uuid.UUID{*h.seedVenta(t), *h.seedVenta(t)}
		h.index.Result = outbound.VentasSearchResultado{IDs: ids, Total: 2}

		page, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{Limit: 2})
		require.NoError(t, err)
		assert.Empty(t, page.NextCursor)
	})

	t.Run("capped_by_max_total_hits", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		ids := []uuid.UUID{*h.seedVenta(t)}
		// Total is huge (beyond the index cap) but offset+limit already sits at
		// the MaxTotalHitsVentas boundary, so no further cursor should be
		// emitted even though "more" would otherwise exist.
		offset := outbound.MaxTotalHitsVentas - 1
		h.index.Result = outbound.VentasSearchResultado{IDs: ids, Total: outbound.MaxTotalHitsVentas + 1000}

		page, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{
			Limit:  1,
			Cursor: "o" + strconv.Itoa(offset),
		})
		require.NoError(t, err)
		assert.Empty(t, page.NextCursor)
	})
}

func TestOffsetCursorCodec(t *testing.T) {
	t.Parallel()

	t.Run("round_trips", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		h.index.Result = outbound.VentasSearchResultado{IDs: nil, Total: 1000}
		page, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{Limit: 50, Cursor: "o50"})
		require.NoError(t, err)
		assert.Equal(t, 50, h.index.LastQuery.Offset)
		assert.Equal(t, "o100", page.NextCursor)
	})

	t.Run("empty_cursor_is_offset_zero", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{})
		require.NoError(t, err)
		assert.Equal(t, 0, h.index.LastQuery.Offset)
	})

	t.Run("malformed_cursor_rejected", func(t *testing.T) {
		t.Parallel()
		h := newBuscarHarness(t)
		for _, bad := range []string{"garbage", "o", "o-5", "oxyz", "x5"} {
			_, err := h.svc.BuscarVentas(t.Context(), app.BuscarVentasInput{Cursor: bad})
			require.ErrorIs(t, err, app.ErrVentaCursorInvalido, "cursor=%q", bad)
		}
	})
}
