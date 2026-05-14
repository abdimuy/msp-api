//nolint:misspell // domain vocabulary is Spanish (cantidad, etc.) per project convention.
package domain

import "github.com/shopspring/decimal"

// montoScaleMax is the maximum allowed decimal places for monetary values.
// Storage column is NUMERIC(14,2); 2 decimals is the declared scale.
const montoScaleMax = 2

// cantidadScaleMax is the maximum allowed decimal places for cantidad
// values. Storage column is NUMERIC(10,4); 4 decimals is the declared scale.
const cantidadScaleMax = 4

// MaxMontoVenta is the declared upper bound for a monetary field. It
// matches the NUMERIC(14,2) declared precision (12 integer digits + 2
// decimals). Firebird's NUMERIC is INT64-backed internally so the column
// would actually accept much larger values — we cap explicitly to catch
// frontend magnitude bugs (e.g., cents-as-pesos) at the boundary.
var MaxMontoVenta = decimal.RequireFromString("999999999999.99")

// validateMontoScale rejects monetary inputs that carry more than
// montoScaleMax decimal places. Without this, the driver silently rounds
// extra digits (100.999 → 101) which is impossible to debug from the FE.
func validateMontoScale(v decimal.Decimal) error {
	if v.Exponent() < -montoScaleMax {
		return ErrMontoDemasiadosDecimales
	}
	return nil
}

// validateMontoCap rejects monetary inputs that exceed MaxMontoVenta.
// Catches order-of-magnitude bugs (e.g., 100000000000000.00 pesos) before
// they corrupt downstream reporting.
func validateMontoCap(v decimal.Decimal) error {
	if v.GreaterThan(MaxMontoVenta) {
		return ErrMontoDemasiadoGrande
	}
	return nil
}

// validateCantidadScale rejects cantidad inputs that carry more than
// cantidadScaleMax decimal places.
func validateCantidadScale(v decimal.Decimal) error {
	if v.Exponent() < -cantidadScaleMax {
		return ErrCantidadDemasiadosDecimales
	}
	return nil
}

// validateMontoSnapshotScale runs validateMontoScale on every component of
// a MontoSnapshot. Used by child entities (Producto, Combo) that receive a
// pre-built MontoSnapshot at construction time and need to re-check it
// against the producto/combo column's own NUMERIC(14,2) scale.
func validateMontoSnapshotScale(m MontoSnapshot) error {
	for _, v := range [...]decimal.Decimal{m.Anual(), m.CortoPlazo(), m.Contado()} {
		if err := validateMontoScale(v); err != nil {
			return err
		}
		if err := validateMontoCap(v); err != nil {
			return err
		}
	}
	return nil
}
