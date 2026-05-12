// Package ventfb is the Firebird-backed adapter implementing
// [outbound.VentaRepo]. The package persists [domain.Venta] aggregates across
// five physical tables — MSP_VENTAS, MSP_VENTAS_COMBOS, MSP_VENTAS_PRODUCTOS,
// MSP_VENTAS_VENDEDORES, MSP_VENTAS_IMAGENES — keeping the writes inside the
// caller's ambient transaction (via [firebird.GetQuerier]) so the aggregate
// is saved atomically.
//
// SQL identifiers carry the Spanish column names from the Firebird schema in
// migrations-firebird/000002_create_ventas_tables.up.sql; every column matches
// 1:1 with the entity field set rebuilt via [domain.HydrateVenta].
//
//nolint:misspell // Spanish vocabulary (productos, descripcion, etc.) by convention.
package ventfb
