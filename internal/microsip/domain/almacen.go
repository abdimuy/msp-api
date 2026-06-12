package domain

// Almacen mirrors a row from Microsip's ALMACENES table joined against
// SALDOS_IN to surface the current total unit balance for the warehouse.
// Existencias is the SUM(ENTRADAS_UNIDADES) - SUM(SALIDAS_UNIDADES) across
// all articulos in the almacen; it is signed so a negative number means
// more units were dispatched than received (a data-quality flag in legacy
// Microsip installs, not a bug in this layer).
type Almacen struct {
	ID          int
	Nombre      string
	Existencias int64
}
