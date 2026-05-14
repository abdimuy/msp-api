package domain

// VentaStatus is the lifecycle stage of a venta in the staging table.
// Mirrors the MSP_VENTAS.STATUS column. Codes are lowercase Spanish per the
// project's domain-vocabulary convention.
type VentaStatus string

// Canonical VentaStatus values. The string literals match the values
// persisted in MSP_VENTAS.STATUS — do not rename without a migration.
const (
	// StatusBorrador is the initial state. The vendor captured the venta in
	// the field; it can still be edited.
	StatusBorrador VentaStatus = "borrador"
	// StatusAprobada means the venta has been validated and is ready to be
	// promoted to Microsip's VENTAS ledger. Editing is forbidden.
	StatusAprobada VentaStatus = "aprobada"
	// StatusCancelada means the venta was soft-cancelled. Editing is
	// forbidden and the venta is treated as logically deleted.
	StatusCancelada VentaStatus = "cancelada"
)

// IsValid reports whether s is one of the canonical statuses.
func (s VentaStatus) IsValid() bool {
	switch s {
	case StatusBorrador, StatusAprobada, StatusCancelada:
		return true
	}
	return false
}

// String returns the underlying string.
func (s VentaStatus) String() string { return string(s) }

// ParseVentaStatus normalizes the input and validates it against the
// canonical set. Returns ErrStatusInvalido on miss.
func ParseVentaStatus(in string) (VentaStatus, error) {
	s := VentaStatus(in)
	if !s.IsValid() {
		return "", ErrStatusInvalido
	}
	return s, nil
}
