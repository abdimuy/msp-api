//nolint:misspell // domain vocabulary is Spanish (cantidad, etc.) per project convention.
package domain

import "github.com/shopspring/decimal"

// maxCantidadDecimalPlaces is the maximum allowed decimal places for cantidad
// values in the inventario module. Storage column is NUMERIC(10,4); 4
// decimals is the declared scale.
const maxCantidadDecimalPlaces = 4

// validateCantidadScale rejects cantidad inputs that carry more than
// maxCantidadDecimalPlaces decimal places. Without this guard, the driver
// would silently round extra digits on write, producing undetectable data loss.
func validateCantidadScale(v decimal.Decimal) error {
	if v.Exponent() < -maxCantidadDecimalPlaces {
		return ErrCantidadEscalaInvalida
	}
	return nil
}
