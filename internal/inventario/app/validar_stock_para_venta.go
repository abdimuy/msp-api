//nolint:misspell // domain vocabulary is Spanish (artículo, almacén, etc.) per project convention.
package app

import (
	"context"

	"github.com/shopspring/decimal"

	"github.com/abdimuy/msp-api/internal/inventario/domain"
)

// ValidarStockItem describes one article-quantity pair to validate.
type ValidarStockItem struct {
	ArticuloID    int
	AlmacenOrigen int
	Cantidad      decimal.Decimal
}

// ValidarStockParaVenta checks that every item has sufficient stock in its
// source warehouse. Runs inside a READ COMMITTED NO WAIT transaction so
// simultaneous last-item sales fail fast with a clear conflict instead of
// blocking.
//
// Returns domain.ErrArticuloSinExistencia (with details attached) for the
// first item whose existencia is below the requested cantidad. Returns nil
// when all items pass or when items is empty.
func (s *Service) ValidarStockParaVenta(ctx context.Context, items []ValidarStockItem) error {
	if len(items) == 0 {
		return nil
	}
	return s.runInTxNoWait(ctx, func(ctx context.Context) error {
		for _, item := range items {
			existencia, err := s.existencia.Existencia(ctx, item.ArticuloID, item.AlmacenOrigen)
			if err != nil {
				return err
			}
			if existencia.LessThan(item.Cantidad) {
				return domain.ErrArticuloSinExistencia.
					WithField("articulo_id", item.ArticuloID).
					WithField("almacen_id", item.AlmacenOrigen).
					WithField("cantidad_requerida", item.Cantidad.String()).
					WithField("existencia_disponible", existencia.String())
			}
		}
		return nil
	})
}
