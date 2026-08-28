// End-to-end test of one full snapshot against BOTH real systems: the actual
// Firestore `users` collection (read-only) and the actual Firebird database.
//
// Everything it writes rolls back. fbtestutil.WithTestTransaction injects a tx
// into the context, and firebird.TxManager.RunInTx is re-entrant — it detects
// the existing tx and runs inside it without committing — so the service's own
// transaction boundary nests into the test's and unwinds with it. That is the
// same code path production uses, not a stand-in for it.
//
// Skips unless FB_DATABASE, FIREBASE_PROJECT_ID and
// FIREBASE_SERVICE_ACCOUNT_PATH are all set. Point FIREBASE_PROJECT_ID at the
// dev project (msp-dev-96ff5); this test must never run against production.
//
// Run: set -a; source .env; set +a; go test ./internal/flota/infra/flotafb/...
//
//nolint:misspell // flota vocabulary is Spanish (camioneta, roster) per project convention.
package flotafb_test

import (
	"context"
	"os"
	"testing"
	"time"

	firebasesdk "firebase.google.com/go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/api/option"

	flotaapp "github.com/abdimuy/msp-api/internal/flota/app"
	flotafb "github.com/abdimuy/msp-api/internal/flota/infra/flotafb"
	"github.com/abdimuy/msp-api/internal/flota/infra/flotafirestore"
	flotaoutbound "github.com/abdimuy/msp-api/internal/flota/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// TestE2E_UnaFotoRealDePuntaAPunta wires the production graph — real roster,
// real repo, real transaction manager — and takes one snapshot.
//
// What it proves that the unit tests cannot: that the shapes the live roster
// actually contains survive every layer, all the way into Firebird columns.
// The dev roster holds people with a camioneta, people without the field, and
// one with the field explicitly null; a NOT NULL constraint or a bad NULL
// binding anywhere in that path would surface here and nowhere else.
func TestE2E_UnaFotoRealDePuntaAPunta(t *testing.T) {
	t.Parallel()
	requireFBEnv(t)

	proyecto := os.Getenv("FIREBASE_PROJECT_ID")
	credenciales := os.Getenv("FIREBASE_SERVICE_ACCOUNT_PATH")
	if proyecto == "" || credenciales == "" {
		t.Skip("FIREBASE_PROJECT_ID / FIREBASE_SERVICE_ACCOUNT_PATH sin definir; se omite el e2e")
	}

	ctxFS, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	app, err := firebasesdk.NewApp(ctxFS,
		&firebasesdk.Config{ProjectID: proyecto},
		option.WithCredentialsFile(credenciales))
	require.NoError(t, err)
	fs, err := app.Firestore(ctxFS)
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, fs.Close()) })

	pool := fbtestutil.NewTestFirebirdPool(t)
	roster := flotafirestore.NewRosterClient(fs)
	repo := flotafb.New(pool)
	// The real TxManager. Inside WithTestTransaction it nests instead of
	// committing, so the whole snapshot rolls back.
	tx := firebird.NewTxManager(pool.DB)
	svc := flotaapp.NewService(roster, repo, tx, flotaoutbound.ProductionClock{})

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		res, err := svc.TomarFoto(ctx)
		require.NoError(t, err, "una foto contra el padrón real debe completarse")

		assert.Positive(t, res.UsuariosObservados,
			"el padrón real no está vacío; si esto es cero, el riel de foto vacía habría abortado")

		// Whether this is the baseline depends on the state of the dev
		// database, so the assertion has to hold either way — which is
		// exactly the invariant that matters: the baseline emits no history.
		if res.LineaBase {
			assert.Equal(t, res.UsuariosObservados, res.Altas,
				"la línea base retrata a todo el padrón")
			assert.Equal(t, 0, res.CambiosDetectados,
				"la línea base no puede afirmar que alguien se movió")
		}

		// The mirror now holds at least everybody the roster showed. Reading
		// it back exercises the scan path over rows this very test wrote,
		// including the NULL camionetas.
		espejo, err := repo.ListarAsignaciones(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(espejo), res.UsuariosObservados)

		conCamioneta := 0
		for _, a := range espejo {
			assert.NotEmpty(t, a.UsuarioUID())
			assert.Equal(t, time.UTC, a.Audit().UpdatedAt().Location(),
				"toda marca de tiempo vuelve en UTC")
			if a.Camioneta().Asignada() {
				assert.Positive(t, a.Camioneta().ID())
				conCamioneta++
			}
		}
		t.Logf("foto real: %d usuarios observados (%d con camioneta en el espejo), línea base=%v, cambios=%d",
			res.UsuariosObservados, conCamioneta, res.LineaBase, res.CambiosDetectados)

		// Second pass, same window, same roster: it must be a no-op. This is
		// the property that keeps 96 passes a day from writing 96 times.
		antes := len(espejo)
		res2, err := svc.TomarFoto(ctx)
		require.NoError(t, err)
		assert.False(t, res2.LineaBase, "la segunda pasada ya tiene foto previa")
		assert.Equal(t, 0, res2.Altas+res2.Actualizaciones+res2.Bajas,
			"un padrón sin cambios no debe tocar el espejo")
		assert.Equal(t, 0, res2.CambiosDetectados,
			"un padrón sin cambios no debe escribir bitácora")

		despues, err := repo.ListarAsignaciones(ctx)
		require.NoError(t, err)
		assert.Len(t, despues, antes)
	})
}
