package firebird_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// The requester half of the merge.
//
// Since capture also runs OUTSIDE the authentication chain, a row can now be
// born without a requester: a 401 has nobody to name. Those rows carry the
// same Idempotency-Key as the work they belong to, so the next authenticated
// attempt at that work merges into them — and if the merge did not adopt the
// requester, the row would keep the null forever and the admin screen would
// refuse to replay it (http/handlers.go:397, "intent_has_no_usuario"). Work
// that used to be recoverable with one click would become manual.

// TestSave_Dedup_TheMergeAdoptsTheRequesterTheRowLacked walks the field
// sequence: session expires, the venta is captured anonymously, the seller
// logs back in and retries, and the surviving row must end up naming him.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestSave_Dedup_TheMergeAdoptsTheRequesterTheRowLacked(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		key := "requester-" + uuid.NewString()

		// 1) Rejected at the auth boundary: captured outside the chain, so
		//    there is no CurrentUser to record.
		anonymous := capturaConClave(key, `{"n":1}`)
		anonymous.HTTPStatus = 401
		anonymous.ErrorCode = "missing_authorization"
		anonymous.ErrorMessage = "encabezado authorization ausente"
		anonymous.UsuarioID = nil
		anonymous.FirebaseUID = ""
		require.NoError(t, saveOK(s.Save(ctx, anonymous)))

		// 2) The seller re-authenticates and retries the same venta: same
		//    idempotency key, this time with a requester.
		usuario := uuid.New()
		retried := capturaConClave(key, `{"n":1}`)
		retried.UsuarioID = &usuario
		retried.FirebaseUID = "fb-uid-vendedor"
		require.NoError(t, saveOK(s.Save(ctx, retried)))

		require.Equal(t, 1, contarFilas(ctx, t, pool, key), "still one row for one job")

		got, err := s.Get(ctx, anonymous.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.NotNil(t, got.UsuarioID,
			"the merged row must name the requester, or /replay-with answers intent_has_no_usuario")
		assert.Equal(t, usuario, *got.UsuarioID)
		assert.Equal(t, "fb-uid-vendedor", got.FirebaseUID)
	})
}

// TestSave_Dedup_AnAnonymousRetryDoesNotEraseTheRequester is the other
// direction, and it is the one that must never happen: a capture without a
// requester arriving after one with it cannot blank the row. Same rule that
// already governs the body and the resumen — never replace something good
// with something worse.
//
//nolint:paralleltest // serial: shares the rollback-only tx.
func TestSave_Dedup_AnAnonymousRetryDoesNotEraseTheRequester(t *testing.T) {
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		key := "requester-back-" + uuid.NewString()

		usuario := uuid.New()
		authenticated := capturaConClave(key, `{"n":1}`)
		authenticated.UsuarioID = &usuario
		authenticated.FirebaseUID = "fb-uid-vendedor"
		require.NoError(t, saveOK(s.Save(ctx, authenticated)))

		// The session expires mid-retry: this capture has no requester.
		anonymous := capturaConClave(key, `{"n":1}`)
		anonymous.HTTPStatus = 401
		anonymous.UsuarioID = nil
		anonymous.FirebaseUID = ""
		require.NoError(t, saveOK(s.Save(ctx, anonymous)))

		got, err := s.Get(ctx, authenticated.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.NotNil(t, got.UsuarioID, "the requester already on the row must survive")
		assert.Equal(t, usuario, *got.UsuarioID)
		assert.Equal(t, "fb-uid-vendedor", got.FirebaseUID)
	})
}
