//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package outbound

import (
	"context"

	"github.com/google/uuid"
)

// VendedorDeCamioneta is one person the fleet roster assigns to a camioneta.
//
// The roster is whatever system holds the assignment — today a Firestore
// `users` collection keyed by CAMIONETA_ASIGNADA, tomorrow possibly a table.
// Neither the domain nor the app layer may learn that: they see two strings
// and nothing else.
type VendedorDeCamioneta struct {
	// Email is the person's address in CANONICAL form (trimmed, lowercased).
	// Implementations MUST normalize before returning — the whole point of
	// resolving server-side is that a capitalized address stops mattering.
	Email string
	// Nombre is the display name to snapshot on the venta. May be empty when
	// the roster has none; the caller then falls back to the name held next
	// to the usuario's identity.
	Nombre string
}

// VendedoresDeCamioneta is the full answer for one camioneta.
type VendedoresDeCamioneta struct {
	// Vendedores are the eligible people, in the roster's own order.
	Vendedores []VendedorDeCamioneta
	// ExcluidosSinModuloVentas counts roster entries that matched the
	// camioneta but were dropped because they do not carry the VENTAS
	// module. Reported as a number rather than swallowed so an over-eager
	// filter shows up in the evidence instead of looking like an empty
	// roster. Measured on 2026-08-27 against the 35 people who have a
	// camioneta assigned: zero. The filter is inert today and only ever
	// excludes somebody the roster explicitly says does not sell.
	ExcluidosSinModuloVentas int
}

// VendedoresDeCamionetaResolver answers "who sells from this camioneta?".
//
// It is consulted while creating a venta, and it is explicitly ALLOWED TO
// FAIL: the caller treats any error, any timeout and any empty answer as "use
// what the client sent". That is not defensive politeness, it is the
// requirement — on 2026-08-14 an unpaid MX$23.49 invoice silently dropped the
// Firebase project to the free plan; had creating a venta depended on the
// roster with no alternative, the business could not have sold that day.
//
// Implementations must therefore never hold the call open indefinitely on
// their own account; the caller additionally bounds it with its own deadline.
type VendedoresDeCamionetaResolver interface {
	// VendedoresDeCamioneta returns the eligible vendedores assigned to
	// camionetaID. An unknown camioneta is not an error — it returns an
	// empty result, which the caller reads as "roster has nothing to say".
	VendedoresDeCamioneta(ctx context.Context, camionetaID int) (VendedoresDeCamioneta, error)
}

// UsuarioDeVendedor is the identity a roster email resolves to.
type UsuarioDeVendedor struct {
	// ID is MSP_USUARIOS.ID — the value the venta's vendedor row references.
	ID uuid.UUID
	// Nombre is MSP_USUARIOS.NOMBRE, used when the roster carries no name.
	Nombre string
}

// VendedorUsuarioEmailResolver maps canonical emails to the usuario identities
// a venta's vendedor rows must reference.
//
// It is the server-side counterpart of the phone's call to
// POST /v2/usuarios/ensure-vendedores-by-email — with one deliberate
// difference: it only LOOKS UP, it never creates. Provisioning an identity is
// the auth module's decision and must not be a side effect of somebody
// selling a refrigerator. Emails with no row are simply absent from the map,
// and the caller reports them instead of inventing an id.
type VendedorUsuarioEmailResolver interface {
	// UsuariosPorEmail returns one entry per email that has a matching row in
	// MSP_USUARIOS, keyed by the CANONICAL email (see domain.NormalizarEmail).
	// Emails without a row are absent from the map.
	UsuariosPorEmail(ctx context.Context, emails []string) (map[string]UsuarioDeVendedor, error)
}
