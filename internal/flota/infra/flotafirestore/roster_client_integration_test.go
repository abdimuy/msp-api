// Integration test against the REAL Firestore `users` collection.
//
// Read-only: it issues one GetAll and asserts invariants. It never writes, and
// it must never be pointed at the production project — point
// FIREBASE_PROJECT_ID at the dev project (msp-dev-96ff5).
//
// It skips unless both FIREBASE_PROJECT_ID and FIREBASE_SERVICE_ACCOUNT_PATH
// are set, so CI and anybody without credentials stay green.
//
// Run: set -a; source .env; set +a; go test ./internal/flota/infra/flotafirestore/...
//
//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package flotafirestore_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"

	"github.com/abdimuy/msp-api/internal/flota/domain"
	"github.com/abdimuy/msp-api/internal/flota/infra/flotafirestore"
)

// nuevoClienteReal builds a RosterClient against the configured project, or
// skips the test when there are no credentials.
func nuevoClienteReal(ctx context.Context, t *testing.T) *flotafirestore.RosterClient {
	t.Helper()
	proyecto := os.Getenv("FIREBASE_PROJECT_ID")
	credenciales := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH")
	if proyecto == "" || credenciales == "" {
		t.Skip("FIREBASE_PROJECT_ID / FIREBASE_SERVICE_ACCOUNT_PATH sin definir; se omite la prueba contra Firestore real")
	}
	app, err := firebasesdk.NewApp(ctx,
		&firebasesdk.Config{ProjectID: proyecto},
		option.WithCredentialsFile(credenciales))
	require.NoError(t, err)
	fs, err := app.Firestore(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, fs.Close()) })
	return flotafirestore.NewRosterClient(fs)
}

// TestRosterClient_ContraFirestoreReal reads the actual roster and checks the
// invariants the rest of the module depends on.
//
// The point is not that the numbers match a golden value — the dev roster is
// a live collection and people get added to it. The point is that every entry
// the adapter produces is USABLE: it has an identifier, and any camioneta it
// reports is a positive integer. If either broke, the snapshot would either
// abort on every pass or silently mis-key somebody's history.
func TestRosterClient_ContraFirestoreReal(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := nuevoClienteReal(ctx, t)

	roster, err := c.LeerRoster(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, roster, "el padrón real no puede estar vacío; si lo está, algo pasa con las credenciales")

	uids := make(map[string]struct{}, len(roster))
	conCamioneta := 0
	for _, u := range roster {
		assert.NotEmpty(t, strings.TrimSpace(u.UID),
			"todo documento debe aportar un id: sin él la bitácora no tiene a quién colgarse")
		_, repetido := uids[u.UID]
		assert.False(t, repetido, "uid repetido %q: Comparar aborta con ErrObservacionDuplicada", u.UID)
		uids[u.UID] = struct{}{}

		if u.Camioneta != nil {
			assert.Positive(t, *u.Camioneta,
				"una camioneta reportada tiene que ser un id positivo; %q trae %d", u.UID, *u.Camioneta)
			conCamioneta++
		}
	}

	// Every entry must survive normalisation, which is what the service does
	// before anything is written. A single failure here would abort the whole
	// pass in production.
	for _, u := range roster {
		_, err := domain.NuevaObservacion(
			u.UID, u.Email, u.Nombre, domain.CamionetaDesdeRoster(u.Camioneta),
		)
		require.NoError(t, err, "el padrón real debe convertirse íntegro en observaciones (uid %q)", u.UID)
	}

	// Logged rather than asserted: these are the numbers the cadence decision
	// rests on (one Firestore read per document per pass), and they move as
	// people join. Pinning them would make the test fail for the wrong reason.
	t.Logf("padrón real: %d documentos (= lecturas de Firestore por pasada), %d con camioneta asignada",
		len(roster), conCamioneta)
}
