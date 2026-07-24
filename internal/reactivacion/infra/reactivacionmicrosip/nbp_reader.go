// Package reactivacionmicrosip is the reactivación module's first cross-module
// client: it composes the microsip catalog contract with the module's own
// CategoriasClienteReader to resolve a personalized next-best-product.
//
// It imports ONLY the microsip contract root package (internal/microsip) — never
// internal/microsip/app or internal/microsip/domain (depguard enforces this) —
// plus the reactivación outbound ports it satisfies and consumes.
//
//nolint:misspell // Spanish domain vocabulary (categoria, precio, credito) by project convention.
package reactivacionmicrosip

import (
	"context"
	"log/slog"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/microsip"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// NBPConfig carries the deterministic knobs of the next-best-product selection.
type NBPConfig struct {
	// AlmacenID is the Microsip ALMACEN_ID whose in-stock catalog is queried.
	AlmacenID int
	// PisoPrecio is the minimum credit-list price a product must reach to be
	// offered — keeps suggestions above a demo-worthy floor.
	PisoPrecio decimal.Decimal
	// ListaCredito is the PRECIOS_EMPRESA name whose price funds the payment
	// plan (credit price, e.g. "MUEBLERIAS").
	ListaCredito string
}

// NBPReader resolves a personalized next-best-product for a cliente by
// composing the microsip catalog (in-stock articles + prices) with the
// cliente's purchased categorías. It suggests an in-stock product, above the
// price floor, in a product line the cliente does NOT already own — falling
// back to the best global candidate when no missing-category candidate exists.
type NBPReader struct {
	cat        microsip.Catalogo
	categorias outbound.CategoriasClienteReader
	cfg        NBPConfig
	logger     *slog.Logger
}

// NewNBPReader builds an NBPReader from the catalog contract, the categorías
// reader, and the selection config. A nil logger falls back to slog.Default().
func NewNBPReader(
	cat microsip.Catalogo,
	categorias outbound.CategoriasClienteReader,
	cfg NBPConfig,
	logger *slog.Logger,
) *NBPReader {
	if logger == nil {
		logger = slog.Default()
	}
	return &NBPReader{cat: cat, categorias: categorias, cfg: cfg, logger: logger}
}

// Compile-time check: NBPReader satisfies the copiloto's NBP port.
var _ outbound.NextBestProductReader = (*NBPReader)(nil)

// GetNBP returns the suggested product for clienteID, or (nil, nil) when no
// in-stock product reaches the price floor. Selection:
//  1. owned  = categorías the cliente already bought (degrades to empty on error
//     — no personalization, but selection still proceeds).
//  2. arts   = in-stock catalog of the configured almacén.
//  3. cands  = arts priceable on ListaCredito AND ≥ PisoPrecio.
//  4. pick   = the highest-credit-price cand in a NON-owned categoría; if none,
//     the highest-credit-price cand overall (global fallback).
//  5. no cand → (nil, nil): the copiloto degrades and offers no product.
func (r *NBPReader) GetNBP(ctx context.Context, clienteID int) (*outbound.NextBestProduct, error) {
	owned := r.ownedCategorias(ctx, clienteID)

	arts, err := r.cat.ListarEnStock(ctx, r.cfg.AlmacenID, "")
	if err != nil {
		return nil, err
	}

	var bestMissing, bestGlobal *candidato
	for i := range arts {
		precio, ok := arts[i].Precios[r.cfg.ListaCredito]
		if !ok || precio.LessThan(r.cfg.PisoPrecio) {
			continue
		}
		c := candidato{art: &arts[i], precio: precio}
		bestGlobal = mejor(bestGlobal, &c)
		if !owned[arts[i].LineaArticuloID] {
			bestMissing = mejor(bestMissing, &c)
		}
	}

	pick := bestMissing
	if pick == nil {
		pick = bestGlobal // global fallback: no missing-category candidate
	}
	if pick == nil {
		return nil, nil //nolint:nilnil // no suitable product is a valid, non-error outcome
	}
	return &outbound.NextBestProduct{Nombre: pick.art.Nombre, Precio: pick.precio}, nil
}

// ownedCategorias returns the set of LINEA_ARTICULO_IDs the cliente already
// bought. A reader error degrades to an empty set (logged, not fatal): the
// copiloto still suggests a product, just without the "missing category" bias.
func (r *NBPReader) ownedCategorias(ctx context.Context, clienteID int) map[int]bool {
	cats, err := r.categorias.CategoriasCompradas(ctx, clienteID)
	if err != nil {
		r.logger.WarnContext(ctx, "reactivacion_nbp.categorias_reader_failed",
			slog.Int("cliente_id", clienteID), slog.String("error", err.Error()))
		return map[int]bool{}
	}
	owned := make(map[int]bool, len(cats))
	for _, id := range cats {
		owned[id] = true
	}
	return owned
}

// candidato pairs an in-stock article with its resolved credit price.
type candidato struct {
	art    *microsip.ArticuloCatalogo
	precio decimal.Decimal
}

// mejor returns the better of two candidates: higher credit price wins, ties
// broken by the lower ArticuloID for deterministic selection. A nil incumbent
// is always replaced by the challenger.
func mejor(actual, challenger *candidato) *candidato {
	if actual == nil {
		return challenger
	}
	switch {
	case challenger.precio.GreaterThan(actual.precio):
		return challenger
	case challenger.precio.Equal(actual.precio) && challenger.art.ArticuloID < actual.art.ArticuloID:
		return challenger
	default:
		return actual
	}
}
