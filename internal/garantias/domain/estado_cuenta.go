package domain

// EstadoCuenta represents the client account state snapshot taken when a
// client-origin warranty folio is opened.
type EstadoCuenta string

// EstadoCuenta values represent the client account state snapshot.
const (
	EstadoCuentaLiquidada      EstadoCuenta = "liquidada"
	EstadoCuentaSaldoPendiente EstadoCuenta = "saldo_pendiente"
)

// ParseEstadoCuenta validates and returns an EstadoCuenta.
// Returns ErrEstadoCuentaInvalido if s is not "liquidada" or "saldo_pendiente".
func ParseEstadoCuenta(s string) (EstadoCuenta, error) {
	e := EstadoCuenta(s)
	if !e.IsValid() {
		return "", ErrEstadoCuentaInvalido
	}
	return e, nil
}

// IsValid reports whether e is a known EstadoCuenta value.
func (e EstadoCuenta) IsValid() bool {
	switch e {
	case EstadoCuentaLiquidada, EstadoCuentaSaldoPendiente:
		return true
	}
	return false
}

// String returns the string representation of e.
func (e EstadoCuenta) String() string { return string(e) }

// EsLiquidada reports whether the account had no outstanding balance.
func (e EstadoCuenta) EsLiquidada() bool { return e == EstadoCuentaLiquidada }
