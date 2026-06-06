package postgres_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentpg "github.com/abdimuy/msp-api/internal/platform/failedintent/postgres"
	"github.com/abdimuy/msp-api/internal/platform/testutil"
	"github.com/abdimuy/msp-api/internal/platform/transaction"
)

var testPool *pgxpool.Pool

// TestMain spins up the shared per-package test DB only when integration
// signals are present. Unit-test runs (go test -short ./...) still pass by
// skipping every test via requirePool.
func TestMain(m *testing.M) {
	if os.Getenv("INTEGRATION") != "" || os.Getenv("TEST_DATABASE_URL") != "" {
		testPool = testutil.NewTestDatabasePool()
	}
	code := m.Run()
	testutil.DropPackageDBs()
	os.Exit(code)
}

func requirePool(t *testing.T) {
	t.Helper()
	if testPool == nil {
		t.Skip("skipping integration test: set INTEGRATION=1 to run")
	}
}

// intentCounter provides unique suffixes for parallel tests.
var intentCounter atomic.Int64

// newIntent builds a complete Intent with unique fields per call. The suffix
// is appended to deterministic fields so parallel tests never collide on key
// columns.
func newIntent(suffix string) failedintent.Intent {
	n := intentCounter.Add(1)
	uid := fmt.Sprintf("firebase-uid-%s-%d", suffix, n)
	usuarioID := uuid.New()
	resolvedBy := uuid.New()
	resolvedAt := time.Now().UTC().Truncate(time.Millisecond)
	notes := fmt.Sprintf("test notes %s %d", suffix, n)
	body := json.RawMessage(fmt.Sprintf(`{"test":%d,"suffix":%q}`, n, suffix))

	return failedintent.Intent{
		ID:             uuid.New(),
		ReceivedAt:     time.Now().UTC().Truncate(time.Millisecond),
		Method:         "POST",
		Path:           "/v2/ventas",
		FirebaseUID:    uid,
		UsuarioID:      &usuarioID,
		IdempotencyKey: fmt.Sprintf("idem-%s-%d", suffix, n),
		RequestID:      uuid.New(),
		Body:           body,
		BodyTruncated:  false,
		HTTPStatus:     422,
		ErrorCode:      fmt.Sprintf("err_%d", n),
		ErrorMessage:   fmt.Sprintf("mensaje de error %d", n),
		RetryCount:     0,
		Status:         failedintent.StatusNew,
		ResolvedAt:     &resolvedAt,
		ResolvedBy:     &resolvedBy,
		Notes:          notes,
	}
}

// TestStore_Save_GetRoundTrip_Integration saves an intent with all columns
// populated, then loads it back and asserts every field matches.
func TestStore_Save_GetRoundTrip_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)

	t.Run("all fields set", func(t *testing.T) {
		t.Parallel()
		testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
			intent := newIntent(t.Name())
			require.NoError(t, s.Save(ctx, intent))

			got, err := s.Get(ctx, intent.ID)
			require.NoError(t, err)
			require.NotNil(t, got)

			assert.Equal(t, intent.ID, got.ID)
			assert.WithinDuration(t, intent.ReceivedAt, got.ReceivedAt, time.Second)
			assert.Equal(t, intent.Method, got.Method)
			assert.Equal(t, intent.Path, got.Path)
			assert.Equal(t, intent.FirebaseUID, got.FirebaseUID)
			require.NotNil(t, got.UsuarioID)
			assert.Equal(t, *intent.UsuarioID, *got.UsuarioID)
			assert.Equal(t, intent.IdempotencyKey, got.IdempotencyKey)
			assert.Equal(t, intent.RequestID, got.RequestID)
			assert.JSONEq(t, string(intent.Body), string(got.Body))
			assert.Equal(t, intent.BodyTruncated, got.BodyTruncated)
			assert.Equal(t, intent.HTTPStatus, got.HTTPStatus)
			assert.Equal(t, intent.ErrorCode, got.ErrorCode)
			assert.Equal(t, intent.ErrorMessage, got.ErrorMessage)
			assert.Equal(t, intent.RetryCount, got.RetryCount)
			assert.Equal(t, intent.Status, got.Status)
			require.NotNil(t, got.ResolvedAt)
			assert.WithinDuration(t, *intent.ResolvedAt, *got.ResolvedAt, time.Second)
			require.NotNil(t, got.ResolvedBy)
			assert.Equal(t, *intent.ResolvedBy, *got.ResolvedBy)
			assert.Equal(t, intent.Notes, got.Notes)
		})
	})

	t.Run("nil optional fields", func(t *testing.T) {
		t.Parallel()
		testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
			// Build an intent with all nullable fields zeroed/nil.
			base := newIntent(t.Name())
			base.FirebaseUID = ""
			base.UsuarioID = nil
			base.IdempotencyKey = ""
			base.ResolvedAt = nil
			base.ResolvedBy = nil
			base.Notes = ""

			require.NoError(t, s.Save(ctx, base))

			got, err := s.Get(ctx, base.ID)
			require.NoError(t, err)
			require.NotNil(t, got)

			assert.Empty(t, got.FirebaseUID, "empty FirebaseUID round-trips as empty string")
			assert.Nil(t, got.UsuarioID)
			assert.Empty(t, got.IdempotencyKey, "empty IdempotencyKey round-trips as empty string")
			assert.Nil(t, got.ResolvedAt)
			assert.Nil(t, got.ResolvedBy)
			assert.Empty(t, got.Notes, "nil notes round-trips as empty string via COALESCE")
		})
	})
}

// TestStore_Save_GetRoundTripWithBlobFields_Integration verifies that the
// new BodyBlobPath/BodyContentType columns persist and re-hydrate correctly,
// and that an empty string round-trips as NULL → "" via NULLIF/COALESCE.
func TestStore_Save_GetRoundTripWithBlobFields_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)

	t.Run("blob fields set", func(t *testing.T) {
		t.Parallel()
		testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
			intent := newIntent(t.Name())
			intent.BodyBlobPath = "/var/lib/msp/failed-intents/abc.bin"
			intent.BodyContentType = "multipart/form-data; boundary=----WebKitFormBoundary"
			require.NoError(t, s.Save(ctx, intent))

			got, err := s.Get(ctx, intent.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, intent.BodyBlobPath, got.BodyBlobPath)
			assert.Equal(t, intent.BodyContentType, got.BodyContentType)
		})
	})

	t.Run("blob fields empty round-trip as empty strings", func(t *testing.T) {
		t.Parallel()
		testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
			intent := newIntent(t.Name())
			intent.BodyBlobPath = ""
			intent.BodyContentType = ""
			require.NoError(t, s.Save(ctx, intent))

			got, err := s.Get(ctx, intent.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Empty(t, got.BodyBlobPath)
			assert.Empty(t, got.BodyContentType)
		})
	})
}

// TestStore_ReferencedPaths_Integration verifies that ReferencedPaths returns
// exactly the non-empty body_blob_path values currently in failed_intents.
func TestStore_ReferencedPaths_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		withBlob1 := newIntent(t.Name() + "-blob-1")
		withBlob1.BodyBlobPath = "/var/lib/msp/failed-intents/blob-1.bin"
		require.NoError(t, s.Save(ctx, withBlob1))

		withBlob2 := newIntent(t.Name() + "-blob-2")
		withBlob2.BodyBlobPath = "/var/lib/msp/failed-intents/blob-2.bin"
		require.NoError(t, s.Save(ctx, withBlob2))

		// A row without a blob must NOT appear.
		withoutBlob := newIntent(t.Name() + "-no-blob")
		withoutBlob.BodyBlobPath = ""
		require.NoError(t, s.Save(ctx, withoutBlob))

		paths, err := s.ReferencedPaths(ctx)
		require.NoError(t, err)
		assert.ElementsMatch(t,
			[]string{withBlob1.BodyBlobPath, withBlob2.BodyBlobPath},
			paths,
		)
	})
}

// TestStore_Save_OnConflictIsNoOp_Integration verifies that saving an intent
// with the same ID as an existing row is silently ignored.
func TestStore_Save_OnConflictIsNoOp_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		first := newIntent(t.Name())
		require.NoError(t, s.Save(ctx, first))

		// Build a second intent sharing the same ID but with different payload.
		second := newIntent(t.Name())
		second.ID = first.ID
		second.Body = json.RawMessage(`{"overwrite":true}`)
		second.ErrorCode = "should_not_appear"
		second.Status = failedintent.StatusIgnored
		require.NoError(t, s.Save(ctx, second), "duplicate save must not return an error")

		got, err := s.Get(ctx, first.ID)
		require.NoError(t, err)
		require.NotNil(t, got)

		assert.JSONEq(t, string(first.Body), string(got.Body), "first save must win on conflict")
		assert.Equal(t, first.ErrorCode, got.ErrorCode)
		assert.Equal(t, first.Status, got.Status)
	})
}

// TestStore_Get_NotFound_ReturnsNilNil_Integration confirms the (nil, nil)
// contract for a missing ID.
func TestStore_Get_NotFound_ReturnsNilNil_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		got, err := s.Get(ctx, uuid.New())
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

// TestStore_List_CursorPagination_Integration inserts 25 intents with
// monotonically increasing received_at and walks three pages of 10.
func TestStore_List_CursorPagination_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		// Insert 25 intents with received_at spaced 1 second apart so ordering is
		// deterministic. Each test runs in its own tx so there is no leakage from
		// parallel tests — no scoping workaround needed.
		base := time.Now().UTC().Add(-30 * time.Minute)
		allIDs := make([]uuid.UUID, 0, 25)
		for i := range 25 {
			intent := newIntent(fmt.Sprintf("%s-%d", t.Name(), i))
			intent.ReceivedAt = base.Add(time.Duration(i) * time.Second).Truncate(time.Millisecond)
			require.NoError(t, s.Save(ctx, intent))
			allIDs = append(allIDs, intent.ID)
		}

		seenIDs := make(map[uuid.UUID]int)

		// Page 1: no cursor.
		p1, err := s.List(ctx, failedintent.ListParams{PageSize: 10})
		require.NoError(t, err)
		assert.Len(t, p1.Items, 10)
		assert.True(t, p1.HasMore, "page 1 must have more")
		assert.False(t, p1.NextReceivedAt.IsZero(), "NextReceivedAt must be set")
		assert.NotEqual(t, uuid.Nil, p1.NextID, "NextID must be set")
		// Items must be in DESC order by received_at.
		for i := 1; i < len(p1.Items); i++ {
			assert.False(t, p1.Items[i].ReceivedAt.After(p1.Items[i-1].ReceivedAt),
				"page 1: items must be sorted DESC by received_at")
		}
		for _, item := range p1.Items {
			seenIDs[item.ID]++
		}

		// Page 2: use cursor from page 1.
		p2, err := s.List(ctx, failedintent.ListParams{
			PageSize:         10,
			CursorReceivedAt: p1.NextReceivedAt,
			CursorID:         p1.NextID,
		})
		require.NoError(t, err)
		assert.Len(t, p2.Items, 10)
		assert.True(t, p2.HasMore, "page 2 must have more")
		for i := 1; i < len(p2.Items); i++ {
			assert.False(t, p2.Items[i].ReceivedAt.After(p2.Items[i-1].ReceivedAt),
				"page 2: items must be sorted DESC by received_at")
		}
		for _, item := range p2.Items {
			seenIDs[item.ID]++
		}

		// Page 3: use cursor from page 2.
		p3, err := s.List(ctx, failedintent.ListParams{
			PageSize:         10,
			CursorReceivedAt: p2.NextReceivedAt,
			CursorID:         p2.NextID,
		})
		require.NoError(t, err)
		assert.Len(t, p3.Items, 5, "last page must contain the remaining 5 items")
		assert.False(t, p3.HasMore, "last page must not have more")
		for i := 1; i < len(p3.Items); i++ {
			assert.False(t, p3.Items[i].ReceivedAt.After(p3.Items[i-1].ReceivedAt),
				"page 3: items must be sorted DESC by received_at")
		}
		for _, item := range p3.Items {
			seenIDs[item.ID]++
		}

		// All 25 IDs we inserted must appear exactly once across the three pages.
		for _, id := range allIDs {
			assert.Equal(t, 1, seenIDs[id],
				"intent %s must appear exactly once across all pages", id)
		}
	})
}

// TestStore_List_FilterByStatus_Integration verifies the Status filter only
// returns intents with the requested status.
func TestStore_List_FilterByStatus_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		// Insert 6 StatusNew + 4 StatusIgnored. No other rows are visible inside
		// this tx, so the filtered page contains exactly these 4 ignored rows.
		ignoredIDs := make([]uuid.UUID, 0, 4)
		for i := range 6 {
			intent := newIntent(fmt.Sprintf("%s-new-%d", t.Name(), i))
			intent.Status = failedintent.StatusNew
			require.NoError(t, s.Save(ctx, intent))
		}
		for i := range 4 {
			intent := newIntent(fmt.Sprintf("%s-ignored-%d", t.Name(), i))
			intent.Status = failedintent.StatusIgnored
			require.NoError(t, s.Save(ctx, intent))
			ignoredIDs = append(ignoredIDs, intent.ID)
		}

		page, err := s.List(ctx, failedintent.ListParams{
			Status:   failedintent.StatusIgnored,
			PageSize: 20,
		})
		require.NoError(t, err)

		// Tx isolation guarantees only this test's rows are visible — assert exact count.
		assert.Len(t, page.Items, 4, "exactly 4 ignored rows must be returned")
		returnedSet := make(map[uuid.UUID]bool, len(page.Items))
		for _, item := range page.Items {
			assert.Equal(t, failedintent.StatusIgnored, item.Status,
				"all returned items must have StatusIgnored")
			returnedSet[item.ID] = true
		}
		for _, id := range ignoredIDs {
			assert.True(t, returnedSet[id],
				"ignored intent %s must appear in filtered list", id)
		}
	})
}

// TestStore_UpdateStatus_Optimistic_Integration exercises the optimistic-lock
// semantics of UpdateStatus.
func TestStore_UpdateStatus_Optimistic_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		intent := newIntent(t.Name())
		// Start with clean nil resolver fields so the update is the only source.
		intent.ResolvedAt = nil
		intent.ResolvedBy = nil
		intent.Notes = ""
		intent.Status = failedintent.StatusNew
		require.NoError(t, s.Save(ctx, intent))

		resolverID := uuid.New()
		now := time.Now().UTC().Truncate(time.Millisecond)

		// Successful transition: new → ignored.
		err := s.UpdateStatus(ctx, intent.ID, failedintent.StatusNew, failedintent.StatusIgnored, resolverID, "test note", now)
		require.NoError(t, err)

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, failedintent.StatusIgnored, got.Status)
		require.NotNil(t, got.ResolvedAt)
		assert.WithinDuration(t, now, *got.ResolvedAt, time.Second)
		require.NotNil(t, got.ResolvedBy)
		assert.Equal(t, resolverID, *got.ResolvedBy)
		assert.Equal(t, "test note", got.Notes)

		// Conflicting transition: expected=new but current is already ignored.
		err = s.UpdateStatus(ctx, intent.ID, failedintent.StatusNew, failedintent.StatusIgnored, resolverID, "retry note", now)
		require.Error(t, err, "stale expected status must return an error")

		appErr, ok := apperror.As(err)
		require.True(t, ok, "error must be an *apperror.Error")
		assert.Equal(t, apperror.KindConflict, appErr.Kind)
		assert.Equal(t, "failed_intent_status_conflict", appErr.Code)

		// Row must remain unchanged after the conflict.
		unchanged, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, unchanged)
		assert.Equal(t, failedintent.StatusIgnored, unchanged.Status, "status must still be ignored after conflict")
	})
}

// TestStore_UpdateStatus_NotesNullWhenEmpty_Integration confirms that passing
// an empty notes string stores NULL and is read back as "".
func TestStore_UpdateStatus_NotesNullWhenEmpty_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		intent := newIntent(t.Name())
		intent.ResolvedAt = nil
		intent.ResolvedBy = nil
		intent.Notes = ""
		intent.Status = failedintent.StatusNew
		require.NoError(t, s.Save(ctx, intent))

		resolverID := uuid.New()
		now := time.Now().UTC()

		err := s.UpdateStatus(ctx, intent.ID, failedintent.StatusNew, failedintent.StatusIgnored, resolverID, "", now)
		require.NoError(t, err)

		// Confirm via the tx querier that the column is NULL (not empty string).
		var notesIsNull bool
		row := transaction.GetQuerier(ctx, testPool).QueryRow(ctx, `SELECT notes IS NULL FROM failed_intents WHERE id = $1`, intent.ID)
		require.NoError(t, row.Scan(&notesIsNull))
		assert.True(t, notesIsNull, "notes column must be NULL when empty string is passed")

		// Confirm the domain Get returns empty string (COALESCE).
		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Empty(t, got.Notes)
	})
}

// TestStore_IncrementRetry_Integration verifies that IncrementRetry bumps the
// retry counter on each call.
func TestStore_IncrementRetry_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		intent := newIntent(t.Name())
		intent.RetryCount = 0
		require.NoError(t, s.Save(ctx, intent))

		require.NoError(t, s.IncrementRetry(ctx, intent.ID))
		require.NoError(t, s.IncrementRetry(ctx, intent.ID))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 2, got.RetryCount)
	})
}

// TestStore_PurgeOlderThan_Integration verifies that PurgeOlderThan deletes
// exactly the rows whose received_at is strictly before the cutoff.
func TestStore_PurgeOlderThan_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		now := time.Now().UTC().Truncate(time.Millisecond)

		oldest := newIntent(t.Name() + "-oldest")
		oldest.ReceivedAt = now.Add(-2 * time.Hour)
		oldest.BodyBlobPath = "/tmp/failed-intents/oldest.bin"
		oldest.BodyContentType = "multipart/form-data; boundary=xxx"
		require.NoError(t, s.Save(ctx, oldest))

		middle := newIntent(t.Name() + "-middle")
		middle.ReceivedAt = now.Add(-1 * time.Hour)
		require.NoError(t, s.Save(ctx, middle))

		newest := newIntent(t.Name() + "-newest")
		newest.ReceivedAt = now
		require.NoError(t, s.Save(ctx, newest))

		cutoff := now.Add(-90 * time.Minute) // between oldest and middle
		result, err := s.PurgeOlderThan(ctx, cutoff)
		require.NoError(t, err)
		assert.Equal(t, int64(1), result.RowsDeleted, "only the row older than cutoff must be deleted")
		assert.Equal(t, []string{"/tmp/failed-intents/oldest.bin"}, result.BlobPaths,
			"blob path of purged row must be returned")

		// oldest must be gone.
		gotOldest, err := s.Get(ctx, oldest.ID)
		require.NoError(t, err)
		assert.Nil(t, gotOldest, "oldest intent must have been purged")

		// middle must still be present.
		gotMiddle, err := s.Get(ctx, middle.ID)
		require.NoError(t, err)
		assert.NotNil(t, gotMiddle, "middle intent must still be present")

		// newest must still be present.
		gotNewest, err := s.Get(ctx, newest.ID)
		require.NoError(t, err)
		assert.NotNil(t, gotNewest, "newest intent must still be present")
	})
}

// TestStore_List_FilterByUsuario_Integration verifies that the UsuarioID filter
// restricts results to intents owned by the specified user.
func TestStore_List_FilterByUsuario_Integration(t *testing.T) {
	t.Parallel()
	requirePool(t)

	s := failedintentpg.New(testPool)
	testutil.WithTestTransaction(t, testPool, func(ctx context.Context) {
		userA := uuid.New()
		userB := uuid.New()

		// Insert 5 intents for user A.
		userAIDs := make([]uuid.UUID, 0, 5)
		for i := range 5 {
			intent := newIntent(fmt.Sprintf("%s-userA-%d", t.Name(), i))
			intent.UsuarioID = &userA
			require.NoError(t, s.Save(ctx, intent))
			userAIDs = append(userAIDs, intent.ID)
		}

		// Insert 3 intents for user B.
		userBIDs := make([]uuid.UUID, 0, 3)
		for i := range 3 {
			intent := newIntent(fmt.Sprintf("%s-userB-%d", t.Name(), i))
			intent.UsuarioID = &userB
			require.NoError(t, s.Save(ctx, intent))
			userBIDs = append(userBIDs, intent.ID)
		}

		// List with UsuarioID = A — must return exactly the 5 A intents.
		pageA, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &userA,
			PageSize:  20,
		})
		require.NoError(t, err)
		assert.Len(t, pageA.Items, 5, "exactly 5 intents for user A must be returned")

		returnedA := make(map[uuid.UUID]bool, len(pageA.Items))
		for _, item := range pageA.Items {
			require.NotNil(t, item.UsuarioID, "all returned items must have a UsuarioID")
			assert.Equal(t, userA, *item.UsuarioID, "all returned items must belong to user A")
			returnedA[item.ID] = true
		}
		for _, id := range userAIDs {
			assert.True(t, returnedA[id], "intent %s for user A must appear in filtered list", id)
		}
		// User B's IDs must not appear.
		for _, id := range userBIDs {
			assert.False(t, returnedA[id], "intent %s for user B must not appear in user A list", id)
		}

		// List with UsuarioID = B — must return exactly the 3 B intents.
		pageB, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &userB,
			PageSize:  20,
		})
		require.NoError(t, err)
		assert.Len(t, pageB.Items, 3, "exactly 3 intents for user B must be returned")

		returnedB := make(map[uuid.UUID]bool, len(pageB.Items))
		for _, item := range pageB.Items {
			require.NotNil(t, item.UsuarioID, "all returned items must have a UsuarioID")
			assert.Equal(t, userB, *item.UsuarioID, "all returned items must belong to user B")
			returnedB[item.ID] = true
		}
		for _, id := range userBIDs {
			assert.True(t, returnedB[id], "intent %s for user B must appear in filtered list", id)
		}
	})
}
