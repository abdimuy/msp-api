//nolint:misspell // Spanish vocabulary (ciudades, estados) per project convention.
package domain

// Ciudad mirrors a row from Microsip's CIUDADES table.
//
// EstadoID and Estado travel WITH the ciudad and are never chosen
// separately. The catalog spans several states — Oaxaca (11523) and Veracruz
// (11751) among them — so picking a ciudad and a estado independently produces
// clientes whose ciudad belongs to one state and whose estado says another.
//
// The catalog is Microsip's, shared with the office, and this module is
// read-only: nothing here ever inserts a ciudad.
type Ciudad struct {
	ID       int
	Nombre   string
	EstadoID int
	Estado   string
}
