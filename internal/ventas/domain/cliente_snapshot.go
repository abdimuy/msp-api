//nolint:misspell // domain vocabulary is Spanish (clientes, responsable, etc.) per project convention.
package domain

import (
	platform "github.com/abdimuy/msp-api/internal/platform/domain"
)

// Maximum widths for the cliente snapshot fields mirror MSP_VENTAS columns.
const (
	maxNombreClienteLength = 200
	maxAvalLength          = 200
	maxReferenciaLength    = 99
)

// NombreCliente is a length-bounded, trimmed person name used in the cliente
// and aval snapshots stored on every venta. It is intentionally permissive
// about characters — the canonical Nombre VO lives in the auth/clientes
// modules; here we only need a snapshot string.
type NombreCliente struct{ value string }

// NewNombreCliente validates and constructs a NombreCliente. The input is
// trimmed, normalized to Unicode NFC, length-checked (in codepoints, not
// bytes), and screened for NUL / ASCII control characters.
func NewNombreCliente(s string) (NombreCliente, error) {
	v, err := requireBounded(s, maxNombreClienteLength, ErrNombreClienteRequerido, ErrNombreClienteDemasiadoLargo)
	if err != nil {
		return NombreCliente{}, err
	}
	return NombreCliente{value: v}, nil
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
// venta. The aval, telefono, and referencia are optional; the nombre is
// required.
type ClienteSnapshot struct {
	nombre     NombreCliente
	telefono   *platform.Telefono
	aval       *NombreCliente
	referencia *string
}

// NewClienteSnapshotParams carries the inputs to NewClienteSnapshot.
type NewClienteSnapshotParams struct {
	Nombre     NombreCliente
	Telefono   *platform.Telefono
	Aval       *NombreCliente
	Referencia *string
}

// NewClienteSnapshot validates and constructs a ClienteSnapshot. Required
// VOs must already be valid; the aval is accepted as-is when non-nil.
// Referencia is trimmed, NFC-normalized, and length-checked (max 99 runes).
func NewClienteSnapshot(p NewClienteSnapshotParams) (ClienteSnapshot, error) {
	if p.Nombre.IsZero() {
		return ClienteSnapshot{}, ErrNombreClienteRequerido
	}
	if p.Aval != nil && len(p.Aval.Value()) > maxAvalLength {
		return ClienteSnapshot{}, ErrAvalDemasiadoLargo
	}
	ref, err := trimOptionalBounded(p.Referencia, maxReferenciaLength, ErrClienteReferenciaDemasiadoLarga)
	if err != nil {
		return ClienteSnapshot{}, err
	}
	return ClienteSnapshot{
		nombre:     p.Nombre,
		telefono:   p.Telefono,
		aval:       p.Aval,
		referencia: ref,
	}, nil
}

// HydrateClienteSnapshot rebuilds a ClienteSnapshot from persistence without
// validation.
func HydrateClienteSnapshot(p NewClienteSnapshotParams) ClienteSnapshot {
	return ClienteSnapshot{nombre: p.Nombre, telefono: p.Telefono, aval: p.Aval, referencia: p.Referencia}
}

// Nombre returns the cliente's name snapshot.
func (c ClienteSnapshot) Nombre() NombreCliente { return c.nombre }

// Telefono returns the optional cliente telefono snapshot.
func (c ClienteSnapshot) Telefono() *platform.Telefono { return c.telefono }

// Aval returns the optional aval/responsable name snapshot.
func (c ClienteSnapshot) Aval() *NombreCliente { return c.aval }

// Referencia returns the optional location reference for the cliente (e.g.
// "casa azul esquina"). Persisted in the venta snapshot and copied to
// LIBRES_CLIENTES.REFERENCIA when the auto-create branch fires.
func (c ClienteSnapshot) Referencia() *string { return c.referencia }
