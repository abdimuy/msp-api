package firebase

import (
	"context"
	"strings"

	"cloud.google.com/go/firestore"
	firebasesdk "firebase.google.com/go/v4"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/abdimuy/msp-api/internal/auth/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// usersCollection is the Firestore collection holding usuario profiles,
// keyed by Firebase uid. The desktop/legacy app owns it.
const usersCollection = "users"

// nombreField is the Firestore document field carrying the canonical display
// name. Upper-case by the legacy app's convention.
const nombreField = "NOMBRE"

// docReader is the minimal Firestore surface FirestoreNombreResolver consumes.
// Defined here so unit tests substitute a fake without a live Firestore.
type docReader interface {
	nombreByUID(ctx context.Context, uid string) (string, error)
}

// FirestoreNombreResolver reads users/{uid}.NOMBRE from Firestore.
type FirestoreNombreResolver struct {
	reader docReader
}

// Compile-time check.
var _ outbound.NombreResolver = (*FirestoreNombreResolver)(nil)

// ResolveNombre returns the trimmed NOMBRE for uid, or "" when the document or
// field is absent. An empty uid short-circuits to "".
func (r *FirestoreNombreResolver) ResolveNombre(ctx context.Context, uid string) (string, error) {
	if strings.TrimSpace(uid) == "" {
		return "", nil
	}
	return r.reader.nombreByUID(ctx, uid)
}

// firestoreDocReader is the production docReader backed by the Firestore SDK.
type firestoreDocReader struct {
	client *firestore.Client
}

// nombreByUID fetches users/{uid} and returns its NOMBRE field. A missing
// document or missing/empty field returns ("", nil) — the caller treats that
// as "no better name available" and falls back to the token name. Only true
// transport failures surface as a non-nil error.
func (r *firestoreDocReader) nombreByUID(ctx context.Context, uid string) (string, error) {
	snap, err := r.client.Collection(usersCollection).Doc(uid).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return "", nil
		}
		return "", err
	}
	raw, err := snap.DataAt(nombreField)
	if err != nil {
		// Field absent — not an error for our purposes.
		return "", nil //nolint:nilerr // missing field is "no name", not a failure
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s), nil
	}
	return "", nil
}

// noopNombreResolver is used when there is no Firestore to read (dev mode,
// unconfigured). It always returns "" so the sync falls back to the token
// name exactly as it did before this resolver existed.
type noopNombreResolver struct{}

// Compile-time check.
var _ outbound.NombreResolver = noopNombreResolver{}

// ResolveNombre always returns "".
func (noopNombreResolver) ResolveNombre(context.Context, string) (string, error) {
	return "", nil
}

// NewNombreResolver selects the NombreResolver implementation from config,
// mirroring NewFirebaseClient's gating:
//
//	DevMode=true OR ProjectID unset → noopNombreResolver (no Firestore)
//	ProjectID set                   → FirestoreNombreResolver (real Firestore)
//
// The real path builds its own Firebase app + Firestore client from the same
// service-account credential the auth client uses. A Firestore init failure is
// fatal at boot (surfaced as an apperror) so a misconfigured deployment does
// not silently lose every usuario's name.
func NewNombreResolver(
	ctx context.Context, cfg config.Firebase,
) (outbound.NombreResolver, error) {
	if cfg.DevMode || cfg.ProjectID == "" {
		return noopNombreResolver{}, nil
	}
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: cfg.ProjectID},
		option.WithCredentialsFile(cfg.ServiceAccountPath),
	)
	if err != nil {
		return nil, apperror.NewInternal(
			"firebase_app_init_failed",
			"no se pudo inicializar firebase para el resolver de nombres",
		).WithError(err)
	}
	fs, err := app.Firestore(ctx)
	if err != nil {
		return nil, apperror.NewInternal(
			"firestore_init_failed",
			"no se pudo inicializar el cliente de firestore",
		).WithError(err)
	}
	return &FirestoreNombreResolver{reader: &firestoreDocReader{client: fs}}, nil
}
