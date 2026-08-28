// Package ventfirestore is the ventas module's adapter to the Firestore
// `users` collection — the fleet roster that says who rides which camioneta.
//
// The collection is owned by the desktop/legacy app, not by this API. Every
// read here is therefore defensive: a missing field, a field of the wrong
// type, or a document that does not look like a person is skipped rather than
// turned into an error, because the caller's contract is that a bad roster
// degrades a venta's vendedor list, never the venta itself.
//
//nolint:misspell // ventas vocabulary is Spanish (vendedores, camioneta) per project convention.
package ventfirestore

import (
	"context"
	"strings"

	"cloud.google.com/go/firestore"

	"github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

const (
	// usersCollection is the Firestore collection holding usuario profiles,
	// keyed by Firebase uid. The desktop/legacy app owns it.
	usersCollection = "users"
	// camionetaField holds the ALMACEN_ID of the camioneta a person is
	// assigned to. Measured on 2026-08-27: it is an integer, and every value
	// present is a real row in Microsip's ALMACENES.
	camionetaField = "CAMIONETA_ASIGNADA"
	// emailField / nombreField are the identity fields. Upper-case by the
	// legacy app's convention.
	emailField  = "EMAIL"
	nombreField = "NOMBRE"
	// modulosField is the array of app modules a person may use
	// ('VENTAS', 'COBRO', 'GARANTIAS', 'ALMACEN').
	modulosField = "MODULOS"
	// moduloVentas is the entry that marks somebody as able to sell.
	moduloVentas = "VENTAS"
)

// VendedoresClient reads the fleet roster from Firestore.
type VendedoresClient struct {
	fs *firestore.Client
}

// NewVendedoresClient builds a VendedoresClient over the given Firestore client.
func NewVendedoresClient(fs *firestore.Client) *VendedoresClient {
	return &VendedoresClient{fs: fs}
}

// Compile-time check.
var _ outbound.VendedoresDeCamionetaResolver = (*VendedoresClient)(nil)

// VendedoresDeCamioneta returns everybody assigned to camionetaID who is
// allowed to sell.
//
// The query filters on CAMIONETA_ASIGNADA alone. The module check runs in Go
// on purpose: combining an equality filter with an array-contains would need
// a COMPOSITE INDEX, and a missing composite index does not degrade — it
// fails the query outright, which here would mean the fallback path silently
// becoming permanent. A single-field equality needs no deployed index, so
// this cannot be broken by forgetting to publish one.
//
// A person is dropped only when MODULOS is present, non-empty, and does not
// contain 'VENTAS'. A document with no MODULOS at all is KEPT: absence of
// evidence that somebody sells is not evidence that they do not, and dropping
// them would reintroduce the exact failure this feature exists to remove — a
// vendedor disappearing from a venta with nothing said about it. Measured on
// 2026-08-27 over the 35 people with a camioneta assigned, the filter
// excludes nobody; the count of exclusions rides on the result so that stays
// checkable instead of assumed.
func (c *VendedoresClient) VendedoresDeCamioneta(
	ctx context.Context, camionetaID int,
) (outbound.VendedoresDeCamioneta, error) {
	docs, err := c.fs.Collection(usersCollection).
		Where(camionetaField, "==", camionetaID).
		Documents(ctx).GetAll()
	if err != nil {
		return outbound.VendedoresDeCamioneta{}, err
	}

	out := outbound.VendedoresDeCamioneta{
		Vendedores: make([]outbound.VendedorDeCamioneta, 0, len(docs)),
	}
	for _, doc := range docs {
		data := doc.Data()
		email := domain.NormalizarEmail(toStr(data[emailField]))
		if email == "" {
			// A roster entry with no address cannot be matched to an
			// identity; there is nothing to resolve.
			continue
		}
		if !puedeVender(data[modulosField]) {
			out.ExcluidosSinModuloVentas++
			continue
		}
		out.Vendedores = append(out.Vendedores, outbound.VendedorDeCamioneta{
			Email:  email,
			Nombre: toStr(data[nombreField]),
		})
	}
	return out, nil
}

// puedeVender reports whether a MODULOS value permits selling. An absent,
// non-array or empty MODULOS returns true — see VendedoresDeCamioneta for why
// "unknown" must not mean "no".
func puedeVender(raw any) bool {
	mods, ok := raw.([]any)
	if !ok || len(mods) == 0 {
		return true
	}
	for _, m := range mods {
		if s, isStr := m.(string); isStr && strings.EqualFold(strings.TrimSpace(s), moduloVentas) {
			return true
		}
	}
	return false
}

// toStr converts a Firestore string value to string ("" when absent or of
// another type).
func toStr(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// NoopVendedoresClient is the resolver used when there is no Firestore to read
// (dev mode, unconfigured project). It always answers "nobody", which the
// caller reads as "roster has nothing to say" and resolves to the client's
// list — the behavior that predates this feature.
type NoopVendedoresClient struct{}

// Compile-time check.
var _ outbound.VendedoresDeCamionetaResolver = NoopVendedoresClient{}

// VendedoresDeCamioneta always returns an empty roster without error.
func (NoopVendedoresClient) VendedoresDeCamioneta(
	_ context.Context, _ int,
) (outbound.VendedoresDeCamioneta, error) {
	return outbound.VendedoresDeCamioneta{}, nil
}
