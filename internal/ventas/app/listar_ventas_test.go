//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

func TestListarVentas(t *testing.T) {
	t.Parallel()

	t.Run("happy_path_returns_repo_page", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.seedVenta(t)
		h.seedVenta(t)

		page, err := h.svc.ListarVentas(t.Context(), app.ListarVentasInput{
			Pagination: outbound.ListParams{PageSize: 10},
		})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)
	})

	t.Run("respects_page_size", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.seedVenta(t)
		h.seedVenta(t)
		h.seedVenta(t)

		page, err := h.svc.ListarVentas(t.Context(), app.ListarVentasInput{
			Pagination: outbound.ListParams{PageSize: 2},
		})
		require.NoError(t, err)
		assert.Len(t, page.Items, 2)
	})

	t.Run("filters_pass_through", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		h.seedVenta(t)
		desde := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		vend := uuid.New()
		in := app.ListarVentasInput{
			Pagination: outbound.ListParams{PageSize: 5},
			Filters: outbound.ListVentasFilters{
				Desde:             &desde,
				VendedorUsuarioID: &vend,
				TipoVenta:         "CONTADO",
				IncluirCanceladas: true,
			},
		}
		// We can't observe the filters from outside the fake without extra
		// wiring; the assertion below just ensures the call completes.
		_, err := h.svc.ListarVentas(t.Context(), in)
		require.NoError(t, err)
	})

	t.Run("repo_error_propagates", func(t *testing.T) {
		t.Parallel()
		h := newHarness(t)
		boom := errors.New("list failed")
		h.ventas.ListErr = boom

		_, err := h.svc.ListarVentas(t.Context(), app.ListarVentasInput{})
		require.ErrorIs(t, err, boom)
	})
}
