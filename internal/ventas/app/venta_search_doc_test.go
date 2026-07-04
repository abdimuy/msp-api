//nolint:misspell // domain vocabulary is Spanish (ventas) per project convention.
package app_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/ventas/app"
)

// ─── PrecioTotal decision ───────────────────────────────────────────────────

func TestVentaToSearchDoc_PrecioTotal_Contado_UsesContadoTier(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput() // PrecioContado=1000, PrecioAnual=1200, PrecioCorto=1100
	v, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	doc := app.VentaToSearchDoc(v)
	assert.True(t, decimal.NewFromInt(1000).Equal(doc.PrecioTotal),
		"CONTADO ventas should surface the Contado tier as precio_total, got %s", doc.PrecioTotal)
}

func TestVentaToSearchDoc_PrecioTotal_Credito_UsesAnualTier(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validCreditoInput() // PrecioAnual=1200, PrecioCorto=1100, PrecioContado=1000
	v, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	doc := app.VentaToSearchDoc(v)
	assert.True(t, decimal.NewFromInt(1200).Equal(doc.PrecioTotal),
		"CREDITO ventas should surface the Anual tier as precio_total, got %s", doc.PrecioTotal)
}

// ─── VendedorEmail decision ─────────────────────────────────────────────────

func TestVentaToSearchDoc_VendedorEmail_FirstVendedorInIterationOrder(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput()
	in.Vendedores = append(in.Vendedores, app.CrearVentaVendedorInput{
		ID: uuid.New(), UsuarioID: uuid.New(),
		Email: "segundo@example.com", Nombre: "Segundo Vendedor",
	})
	v, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	doc := app.VentaToSearchDoc(v)
	assert.Equal(t, "vendedor@example.com", doc.VendedorEmail, "must be the FIRST vendedor's email")
	assert.Contains(t, doc.Vendedor, "Ana Vendedora")
	assert.Contains(t, doc.Vendedor, "Segundo Vendedor")
}

func TestVentaToSearchDoc_VendedorEmail_EmptyWhenNoVendedores(t *testing.T) {
	t.Parallel()
	// CrearVenta requires at least one vendedor (domain invariant), so this
	// case — a venta with an empty vendedores collection — is exercised via
	// HydrateVenta, mirroring how the ventfb repository would rebuild a row
	// whose vendedores query happened to return nothing.
	v := newMinimalVenta(t)

	doc := app.VentaToSearchDoc(v)
	assert.Empty(t, doc.VendedorEmail)
	assert.Empty(t, doc.Vendedor)
}

// ─── Remaining field mapping (spot checks) ─────────────────────────────────

func TestVentaToSearchDoc_FieldMapping(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	in := validContadoInput()
	v, err := h.svc.CrearVenta(t.Context(), in, uuid.New())
	require.NoError(t, err)

	doc := app.VentaToSearchDoc(v)
	assert.Equal(t, v.ID(), doc.ID)
	assert.Equal(t, "JUAN PEREZ", doc.NombreCliente)
	assert.Equal(t, "AV. REFORMA CENTRO CUAUHTEMOC CDMX", doc.Direccion)
	assert.Empty(t, doc.Folio, "folio is empty until aplicada")
	assert.Equal(t, "CONTADO", doc.TipoVenta)
	assert.Equal(t, "borrador", doc.Situacion)
	assert.Equal(t, "pendiente", doc.Sincronizacion)
	assert.Equal(t, "active", doc.Estado)
	assert.Equal(t, 0, doc.ZonaClienteID, "no zona set on this fixture")
	assert.Equal(t, 0, doc.ClienteID, "no cliente_id link on this fixture")
	assert.Equal(t, v.FechaVenta(), doc.FechaVenta)
}
