//nolint:misspell // domain vocabulary is Spanish (clientes, responsable, etc.) per project convention.
package domain

import (
	"strings"

	platform "github.com/abdimuy/msp-api/internal/platform/domain"
)

// Maximum widths for the cliente snapshot fields mirror MSP_VENTAS columns.
const (
	maxNombreClienteLength = 200
	maxAvalLength          = 200
)

// NombreCliente is a length-bounded, trimmed person name used in the cliente
// and aval snapshots stored on every venta. It is intentionally permissive
// about characters — the canonical Nombre VO lives in the auth/clientes
// modules; here we only need a snapshot string.
type NombreCliente struct{ value string }

// NewNombreCliente validates and constructs a NombreCliente.
func NewNombreCliente(s string) (NombreCliente, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return NombreCliente{}, ErrNombreClienteRequerido
	}
	if len(s) > maxNombreClienteLength {
		return NombreCliente{}, ErrNombreClienteDemasiadoLargo
	}
	if err := validateSafeChars(s); err != nil {
		return NombreCliente{}, err
	}
	return NombreCliente{value: s}, nil
}

// HydrateNombreCliente rebuilds a NombreCliente from persistence without
// validation.
func HydrateNombreCliente(s string) NombreCliente { return NombreCliente{value: s} }

// Value returns the trimmed nombre string.
func (n NombreCliente) Value() string { return n.value }

// String returns the trimmed nombre string.
func (n NombreCliente) String() string { return n.value }

// Equals reports whether two NombreCliente values are identical.
func (n NombreCliente) Equals(other NombreCliente) bool { return n.value == other.value }

// IsZero reports whether the NombreCliente has its zero value.
func (n NombreCliente) IsZero() bool { return n.value == "" }

// ClienteSnapshot is the immutable snapshot of the cliente embedded in every
// venta. The aval and telefono are optional; the nombre is required.
type ClienteSnapshot struct {
	nombre   NombreCliente
	telefono *platform.Telefono
	aval     *NombreCliente
}

// NewClienteSnapshotParams carries the inputs to NewClienteSnapshot.
type NewClienteSnapshotParams struct {
	Nombre   NombreCliente
	Telefono *platform.Telefono
	Aval     *NombreCliente
}

// NewClienteSnapshot validates and constructs a ClienteSnapshot. Required
// VOs must already be valid; the aval is accepted as-is when non-nil.
func NewClienteSnapshot(p NewClienteSnapshotParams) (ClienteSnapshot, error) {
	if p.Nombre.IsZero() {
		return ClienteSnapshot{}, ErrNombreClienteRequerido
	}
	if p.Aval != nil && len(p.Aval.Value()) > maxAvalLength {
		return ClienteSnapshot{}, ErrAvalDemasiadoLargo
	}
	return ClienteSnapshot{
		nombre:   p.Nombre,
		telefono: p.Telefono,
		aval:     p.Aval,
	}, nil
}

// HydrateClienteSnapshot rebuilds a ClienteSnapshot from persistence without
// validation.
func HydrateClienteSnapshot(p NewClienteSnapshotParams) ClienteSnapshot {
	return ClienteSnapshot{nombre: p.Nombre, telefono: p.Telefono, aval: p.Aval}
}

// Nombre returns the cliente's name snapshot.
func (c ClienteSnapshot) Nombre() NombreCliente { return c.nombre }

// Telefono returns the optional cliente telefono snapshot.
func (c ClienteSnapshot) Telefono() *platform.Telefono { return c.telefono }

// Aval returns the optional aval/responsable name snapshot.
func (c ClienteSnapshot) Aval() *NombreCliente { return c.aval }
