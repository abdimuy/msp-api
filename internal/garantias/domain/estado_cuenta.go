package domain

// EstadoCuentaLiquidada represents a client account with no outstanding
// balance at the time the warranty folio was opened.
const EstadoCuentaLiquidada = "liquidada"

// EstadoCuentaSaldoPendiente represents a client account that still has a
// balance owed at the time the warranty folio was opened.
const EstadoCuentaSaldoPendiente = "saldo_pendiente"

// EstadoCuenta is a value object wrapping the client account state snapshot
// taken when a client-origin warranty folio is opened. Only "liquidada" and
// "saldo_pendiente" are valid. Does not apply to piso-origin folios.
type EstadoCuenta struct{ value string }

// NewEstadoCuenta validates and constructs an EstadoCuenta. Accepts only
// "liquidada" or "saldo_pendiente"; rejects anything else with
// ErrEstadoCuentaInvalido.
func NewEstadoCuenta(s string) (EstadoCuenta, error) {
	if s != EstadoCuentaLiquidada && s != EstadoCuentaSaldoPendiente {
		return EstadoCuenta{}, ErrEstadoCuentaInvalido
	}
	return EstadoCuenta{value: s}, nil
}

// HydrateEstadoCuenta rebuilds an EstadoCuenta from persistence without
// validation. Intended for repository use only.
func HydrateEstadoCuenta(s string) EstadoCuenta { return EstadoCuenta{value: s} }

// Value returns the raw account state string ("liquidada" or
// "saldo_pendiente").
func (e EstadoCuenta) Value() string { return e.value }

// String returns the account state string representation.
func (e EstadoCuenta) String() string { return e.value }

// Equals reports whether two EstadoCuenta values are identical.
func (e EstadoCuenta) Equals(other EstadoCuenta) bool { return e.value == other.value }

// IsZero reports whether the EstadoCuenta has its zero value (empty string).
func (e EstadoCuenta) IsZero() bool { return e.value == "" }

// EsLiquidada reports whether the account had no outstanding balance.
func (e EstadoCuenta) EsLiquidada() bool { return e.value == EstadoCuentaLiquidada }
