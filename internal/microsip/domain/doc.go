// Package domain contains read-only data records exposed by the microsip
// catalog module. These structs are not aggregates — there are no invariants
// to protect, no constructors, and no mutating behavior. They model rows
// returned from Microsip's Firebird tables (ALMACENES, ZONAS_CLIENTES,
// ARTICULOS) so the rest of the module can speak in named fields instead of
// untyped sql.Rows columns.
//
//nolint:misspell // Spanish vocabulary (clientes) per project convention.
package domain
