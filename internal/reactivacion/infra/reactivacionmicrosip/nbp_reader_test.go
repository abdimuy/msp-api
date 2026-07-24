//nolint:misspell // Spanish domain vocabulary (categoria, precio, credito) by project convention.
package reactivacionmicrosip_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/microsip"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionmicrosip"
)

const listaCredito = "MUEBLERIAS"

// ── fakes ────────────────────────────────────────────────────────────────────

type fakeCatalogo struct {
	arts []microsip.ArticuloCatalogo
	err  error
}

func (f fakeCatalogo) ListarEnStock(context.Context, int, string) ([]microsip.ArticuloCatalogo, error) {
	return f.arts, f.err
}

type fakeCategorias struct {
	cats []int
	err  error
}

func (f fakeCategorias) CategoriasCompradas(context.Context, int) ([]int, error) {
	return f.cats, f.err
}

func art(id, linea int, nombre, precioCredito string) microsip.ArticuloCatalogo {
	precios := map[string]decimal.Decimal{}
	if precioCredito != "" {
		precios[listaCredito] = decimal.RequireFromString(precioCredito)
	}
	return microsip.ArticuloCatalogo{
		ArticuloID:      id,
		LineaArticuloID: linea,
		Nombre:          nombre,
		Existencias:     5,
		Precios:         precios,
	}
}

func newReader(cat microsip.Catalogo, cats *fakeCategorias) *reactivacionmicrosip.NBPReader {
	return reactivacionmicrosip.NewNBPReader(cat, cats, reactivacionmicrosip.NBPConfig{
		AlmacenID:    19,
		PisoPrecio:   decimal.RequireFromString("3000"),
		ListaCredito: listaCredito,
	}, nil)
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestGetNBP_ChoosesInMissingCategory(t *testing.T) {
	t.Parallel()

	cat := fakeCatalogo{arts: []microsip.ArticuloCatalogo{
		art(1, 10, "Sala en línea 10 (comprada)", "12000"), // owned line — pricier
		art(2, 20, "Refrigerador en línea 20", "8500"),     // missing line
		art(3, 20, "Estufa en línea 20", "6000"),           // missing line, cheaper
	}}
	// Cliente already bought line 10.
	r := newReader(cat, &fakeCategorias{cats: []int{10}})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, nbp)
	// Highest-priced product in a NON-owned line (20), not the pricier owned one.
	assert.Equal(t, "Refrigerador en línea 20", nbp.Nombre)
	assert.True(t, nbp.Precio.Equal(decimal.RequireFromString("8500")))
}

func TestGetNBP_AppliesPriceFloor(t *testing.T) {
	t.Parallel()

	cat := fakeCatalogo{arts: []microsip.ArticuloCatalogo{
		art(1, 20, "Barato bajo el piso", "2500"), // below 3000 floor → excluded
		art(2, 30, "Sobre el piso", "3500"),
	}}
	r := newReader(cat, &fakeCategorias{cats: nil})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, nbp)
	assert.Equal(t, "Sobre el piso", nbp.Nombre)
}

func TestGetNBP_GlobalFallbackWhenAllCategoriesOwned(t *testing.T) {
	t.Parallel()

	cat := fakeCatalogo{arts: []microsip.ArticuloCatalogo{
		art(1, 10, "Producto A línea 10", "5000"),
		art(2, 10, "Producto B línea 10", "9000"),
	}}
	// Cliente owns the only line present (10) → no missing-category candidate,
	// so fall back to the best global candidate.
	r := newReader(cat, &fakeCategorias{cats: []int{10}})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, nbp)
	assert.Equal(t, "Producto B línea 10", nbp.Nombre) // highest price
	assert.True(t, nbp.Precio.Equal(decimal.RequireFromString("9000")))
}

func TestGetNBP_NilWhenNoCandidateAbovePiso(t *testing.T) {
	t.Parallel()

	cat := fakeCatalogo{arts: []microsip.ArticuloCatalogo{
		art(1, 20, "Barato", "1000"),
		art(2, 30, "Sin precio de crédito", ""), // no MUEBLERIAS price at all
	}}
	r := newReader(cat, &fakeCategorias{cats: nil})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.NoError(t, err)
	assert.Nil(t, nbp)
}

func TestGetNBP_DegradesWhenCategoriasReaderFails(t *testing.T) {
	t.Parallel()

	cat := fakeCatalogo{arts: []microsip.ArticuloCatalogo{
		art(1, 20, "Refrigerador", "8500"),
	}}
	// Categorías reader errors → no personalization, but selection proceeds
	// treating everything as a missing category.
	r := newReader(cat, &fakeCategorias{err: errors.New("boom")})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, nbp)
	assert.Equal(t, "Refrigerador", nbp.Nombre)
}

func TestGetNBP_PropagatesCatalogError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("catalog down")
	r := newReader(fakeCatalogo{err: sentinel}, &fakeCategorias{cats: nil})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.ErrorIs(t, err, sentinel)
	assert.Nil(t, nbp)
}

func TestGetNBP_TieBreakByLowerArticuloID(t *testing.T) {
	t.Parallel()

	cat := fakeCatalogo{arts: []microsip.ArticuloCatalogo{
		art(9, 20, "Empate id alto", "8500"),
		art(3, 30, "Empate id bajo", "8500"),
	}}
	r := newReader(cat, &fakeCategorias{cats: nil})

	nbp, err := r.GetNBP(context.Background(), 1)
	require.NoError(t, err)
	require.NotNil(t, nbp)
	assert.Equal(t, "Empate id bajo", nbp.Nombre) // lower ArticuloID wins the tie
}
