package domain

import (
	"strings"

	"github.com/google/uuid"
)

// Maximum widths for vendedor snapshot fields.
const (
	maxVendedorEmailLength  = 255
	maxVendedorNombreLength = 200
)

// VendedorSnapshot captures one of the vendedores assigned to a venta at the
// moment of sale. The usuarioID references MSP_USUARIOS but the email/nombre
// are held as snapshots so historical receipts remain readable even if the
// underlying usuario changes.
type VendedorSnapshot struct {
	usuarioID uuid.UUID
	email     string
	nombre    string
}

// NewVendedorSnapshotParams carries the inputs to NewVendedorSnapshot.
type NewVendedorSnapshotParams struct {
	UsuarioID uuid.UUID
	Email     string
	Nombre    string
}

// NewVendedorSnapshot validates and constructs a VendedorSnapshot.
func NewVendedorSnapshot(p NewVendedorSnapshotParams) (VendedorSnapshot, error) {
	email := strings.TrimSpace(p.Email)
	if email == "" {
		return VendedorSnapshot{}, ErrVendedorEmailRequerido
	}
	if len(email) > maxVendedorEmailLength {
		return VendedorSnapshot{}, ErrVendedorEmailDemasiadoLargo
	}
	if err := validateSafeChars(email); err != nil {
		return VendedorSnapshot{}, err
	}
	nombre := strings.TrimSpace(p.Nombre)
	if nombre == "" {
		return VendedorSnapshot{}, ErrVendedorNombreRequerido
	}
	if len(nombre) > maxVendedorNombreLength {
		return VendedorSnapshot{}, ErrVendedorNombreDemasiadoLargo
	}
	if err := validateSafeChars(nombre); err != nil {
		return VendedorSnapshot{}, err
	}
	return VendedorSnapshot{usuarioID: p.UsuarioID, email: email, nombre: nombre}, nil
}

// HydrateVendedorSnapshot rebuilds a VendedorSnapshot from persistence without
// validation.
func HydrateVendedorSnapshot(p NewVendedorSnapshotParams) VendedorSnapshot {
	return VendedorSnapshot{usuarioID: p.UsuarioID, email: p.Email, nombre: p.Nombre}
}

// UsuarioID returns the vendedor usuario UUID.
func (v VendedorSnapshot) UsuarioID() uuid.UUID { return v.usuarioID }

// Email returns the vendedor email snapshot.
func (v VendedorSnapshot) Email() string { return v.email }

// Nombre returns the vendedor nombre snapshot.
func (v VendedorSnapshot) Nombre() string { return v.nombre }

// Equals reports whether two VendedorSnapshot values are identical.
func (v VendedorSnapshot) Equals(other VendedorSnapshot) bool {
	return v.usuarioID == other.usuarioID && v.email == other.email && v.nombre == other.nombre
}
