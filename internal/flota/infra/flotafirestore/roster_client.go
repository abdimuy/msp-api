// Package flotafirestore is the flota module's adapter to the Firestore
// `users` collection — the roster that says who rides which camioneta.
//
// The collection belongs to the desktop/legacy app, not to this API, and
// three different clients write it (the desktop screen, the Android app, and
// people editing documents by hand in the Firebase console). Every read here
// is therefore defensive: a missing field, a null, or a value of an
// unexpected type is mapped to "no camioneta" and recorded as such, never
// turned into an error. Losing the whole photograph because one document is
// odd would lose the other sixty-one people too.
package flotafirestore

import (
	"context"

	"cloud.google.com/go/firestore"

	"github.com/abdimuy/msp-api/internal/flota/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

const (
	// usersCollection is the Firestore collection holding usuario profiles,
	// keyed by Firebase uid. The desktop/legacy app owns it.
	usersCollection = "users"
	// camionetaField holds the ALMACEN_ID of the camioneta a person is
	// assigned to. Measured against the dev project on 2026-08-28 over 62
	// documents: 35 hold an int64, 26 do not have the field, and 1 has it
	// present and null.
	camionetaField = "CAMIONETA_ASIGNADA"
	// emailField and nombreField are the identity fields. Upper-case by the
	// legacy app's convention; present on all 62 documents.
	emailField  = "EMAIL"
	nombreField = "NOMBRE"
)

// RosterClient reads the whole roster from Firestore.
type RosterClient struct {
	fs *firestore.Client
}

// Compile-time check.
var _ outbound.RosterReader = (*RosterClient)(nil)

// NewRosterClient builds a RosterClient over the given Firestore client.
func NewRosterClient(fs *firestore.Client) *RosterClient {
	return &RosterClient{fs: fs}
}

// LeerRoster returns every document in `users`.
//
// No Where clause and no Select. The absence of a filter is the design (see
// outbound.RosterReader: filtering on CAMIONETA_ASIGNADA would make "the
// camioneta was taken away" and "the document was deleted" look identical,
// and telling those apart is the point). The absence of a projection is
// because Firestore bills per document read regardless of which fields come
// back — a Select would save bandwidth on 62 small documents and buy a new
// failure mode, so it is not worth it.
//
// A transport failure is returned as-is: the caller reacts by writing
// nothing, which leaves a visible gap in the snapshots ledger.
func (c *RosterClient) LeerRoster(ctx context.Context) ([]outbound.RosterUsuario, error) {
	docs, err := c.fs.Collection(usersCollection).Documents(ctx).GetAll()
	if err != nil {
		return nil, apperror.NewInternal(
			"flota_roster_read_failed",
			"no se pudo leer el padrón de usuarios de firestore",
		).WithError(err)
	}

	roster := make([]outbound.RosterUsuario, 0, len(docs))
	for _, doc := range docs {
		data := doc.Data()
		roster = append(roster, outbound.RosterUsuario{
			UID:       doc.Ref.ID,
			Email:     comoTexto(data[emailField]),
			Nombre:    comoTexto(data[nombreField]),
			Camioneta: comoCamioneta(data[camionetaField]),
		})
	}
	return roster, nil
}

// comoTexto reads a string field, yielding "" for anything else (absent,
// null, wrong type).
func comoTexto(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// comoCamioneta reads the camioneta id.
//
// The Firestore Go client decodes integers as int64 and JSON-ish numbers as
// float64; the console lets somebody type either, and the field has been
// observed as an explicit null. All the shapes that do not yield a usable
// positive integer collapse to nil, which the domain reads as "no camioneta".
// A float is accepted but truncated, and a float with a fractional part is
// rejected rather than rounded — an id is not a measurement.
func comoCamioneta(v any) *int {
	var id int
	switch n := v.(type) {
	case int64:
		id = int(n)
	case float64:
		if n != float64(int64(n)) {
			return nil
		}
		id = int(int64(n))
	default:
		return nil
	}
	if id <= 0 {
		return nil
	}
	return &id
}

// NoopRosterClient is the stand-in used when Firestore is not configured
// (dev mode, missing project id).
//
// It fails loudly instead of returning an empty roster. An empty roster is a
// meaningful value in this module — it is what the mass-deletion rail checks
// for — and quietly handing one over would let a misconfigured deployment
// look like a legitimate observation. The worker is not started at all when
// Firestore is unavailable, so in practice this error is only ever seen by
// somebody who wired the graph wrong.
type NoopRosterClient struct{}

// Compile-time check.
var _ outbound.RosterReader = NoopRosterClient{}

// ErrRosterNoConfigurado is returned by NoopRosterClient.
var ErrRosterNoConfigurado = apperror.NewInternal(
	"flota_roster_no_configurado",
	"no hay firestore configurado para leer el padrón de usuarios",
)

// LeerRoster always fails. See the type doc for why it does not return an
// empty slice.
func (NoopRosterClient) LeerRoster(context.Context) ([]outbound.RosterUsuario, error) {
	return nil, ErrRosterNoConfigurado
}
