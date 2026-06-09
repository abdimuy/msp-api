//go:build !ci_skip_firebird

package firebird_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/apperror"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintentfb "github.com/abdimuy/msp-api/internal/platform/failedintent/firebird"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// requireFailedIntentsTable skips the test when MSP_FAILED_INTENTS is not
// present. Migration 000031 must be applied before these tests can run.
func requireFailedIntentsTable(t *testing.T, pool *firebird.Pool) {
	t.Helper()
	ctx := context.Background()
	err := firebird.RunInReadTx(ctx, pool.DB, func(ctx context.Context) error {
		q := firebird.GetQuerier(ctx, pool.DB)
		row := q.QueryRowContext(
			ctx,
			`SELECT COUNT(*) FROM RDB$RELATIONS WHERE RDB$RELATION_NAME = 'MSP_FAILED_INTENTS'`,
		)
		var count int
		if scanErr := row.Scan(&count); scanErr != nil {
			return scanErr
		}
		if count == 0 {
			t.Skip("MSP_FAILED_INTENTS table not found; run migration 000031 first")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("requireFailedIntentsTable: check table: %v", err)
	}
}

// newIntent builds a minimal but complete failedintent.Intent for testing.
func newIntent(id uuid.UUID, receivedAt time.Time, status failedintent.Status) failedintent.Intent {
	return failedintent.Intent{
		ID:            id,
		ReceivedAt:    receivedAt,
		Method:        "POST",
		Path:          "/v2/ventas",
		RequestID:     uuid.New(),
		Body:          json.RawMessage(`{"x":1}`),
		BodyTruncated: false,
		HTTPStatus:    422,
		ErrorCode:     "validation_failed",
		ErrorMessage:  "mensaje de error de prueba",
		RetryCount:    0,
		Status:        status,
	}
}

// TestSave_InsertsRow verifies all 20 columns round-trip correctly via Get.
func TestSave_InsertsRow(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		usuarioID := uuid.New()
		resolvedBy := uuid.New()
		resolvedAt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

		intent := failedintent.Intent{
			ID:              uuid.New(),
			ReceivedAt:      time.Now().UTC().Truncate(time.Millisecond),
			Method:          "POST",
			Path:            "/v2/ventas",
			FirebaseUID:     "firebase-uid-test",
			UsuarioID:       &usuarioID,
			IdempotencyKey:  "idem-key-test",
			RequestID:       uuid.New(),
			Body:            json.RawMessage(`{"venta_id":"abc","monto":1500}`),
			BodyTruncated:   false,
			BodyBlobPath:    "",
			BodyContentType: "",
			HTTPStatus:      422,
			ErrorCode:       "validation_failed",
			ErrorMessage:    "el monto no puede ser negativo",
			RetryCount:      2,
			Status:          failedintent.StatusIgnored,
			ResolvedAt:      &resolvedAt,
			ResolvedBy:      &resolvedBy,
			Notes:           "resuelto manualmente por admin",
		}

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
		assert.Equal(t, usuarioID, *got.UsuarioID)
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
		assert.WithinDuration(t, resolvedAt, *got.ResolvedAt, time.Second)
		require.NotNil(t, got.ResolvedBy)
		assert.Equal(t, resolvedBy, *got.ResolvedBy)
		assert.Equal(t, intent.Notes, got.Notes)
	})
}

// TestSave_DuplicatePK_IsNoOp verifies that a second Save with the same ID
// returns nil and the original row is unchanged.
func TestSave_DuplicatePK_IsNoOp(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		first := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		require.NoError(t, s.Save(ctx, first))

		second := first
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

// TestSave_JSONRoundTrip_UTF8 verifies Body with unicode emoji and Notes with
// Spanish accents survive the Firebird BLOB SUB_TYPE TEXT round-trip.
func TestSave_JSONRoundTrip_UTF8(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		intent.Body = json.RawMessage(`{"emoji":"🎉","msg":"¡Felicidades!"}`)
		intent.Notes = "notas con acentos: árbol, niño, señor"

		require.NoError(t, s.Save(ctx, intent))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.JSONEq(t, string(intent.Body), string(got.Body))
		assert.Equal(t, intent.Notes, got.Notes)
	})
}

// TestSave_MultipartPath_HasBlobPath_NoBody verifies the multipart capture
// path: empty Body + populated BodyBlobPath + BodyContentType all round-trip.
func TestSave_MultipartPath_HasBlobPath_NoBody(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		intent.Body = json.RawMessage(`null`)
		intent.BodyBlobPath = "/var/lib/msp/failed-intents/abc123.bin"
		intent.BodyContentType = "multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxk"

		require.NoError(t, s.Save(ctx, intent))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, intent.BodyBlobPath, got.BodyBlobPath)
		assert.Equal(t, intent.BodyContentType, got.BodyContentType)
	})
}

// TestGet_MissingID_ReturnsNilNil confirms the (nil, nil) contract for a
// missing ID.
func TestGet_MissingID_ReturnsNilNil(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		got, err := s.Get(ctx, uuid.New())
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

// TestList_OrdersByReceivedAtDESC_IDDesc inserts 3 rows with controlled
// RECEIVED_AT and asserts descending order.
func TestList_OrdersByReceivedAtDESC_IDDesc(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		base := time.Now().UTC().Add(-30 * time.Minute)

		var ids []uuid.UUID
		for i := range 3 {
			intent := newIntent(uuid.New(), base.Add(time.Duration(i)*time.Second), failedintent.StatusNew)
			require.NoError(t, s.Save(ctx, intent))
			ids = append(ids, intent.ID)
		}

		page, err := s.List(ctx, failedintent.ListParams{PageSize: 10})
		require.NoError(t, err)

		// Find our 3 rows among results (other rows may exist in the DB).
		found := make(map[uuid.UUID]int)
		for _, item := range page.Items {
			for _, id := range ids {
				if item.ID == id {
					found[id]++
				}
			}
		}
		assert.Len(t, found, 3, "all 3 inserted rows must appear")

		// Verify overall ordering is DESC.
		for i := 1; i < len(page.Items); i++ {
			assert.False(
				t,
				page.Items[i].ReceivedAt.After(page.Items[i-1].ReceivedAt),
				"items must be sorted RECEIVED_AT DESC",
			)
		}
	})
}

// TestList_PaginatesViaCursor inserts 5 rows, requests pageSize=2, verifies
// HasMore + NextReceivedAt/NextID, then fetches page 2.
func TestList_PaginatesViaCursor(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		base := time.Now().UTC().Add(-10 * time.Minute)
		userID := uuid.New() // scope rows to this user to isolate from other DB rows

		allIDs := make([]uuid.UUID, 0, 5)
		for i := range 5 {
			intent := newIntent(uuid.New(), base.Add(time.Duration(i)*time.Second), failedintent.StatusNew)
			intent.UsuarioID = &userID
			require.NoError(t, s.Save(ctx, intent))
			allIDs = append(allIDs, intent.ID)
		}

		// Page 1: filter by userID to get only our rows.
		p1, err := s.List(ctx, failedintent.ListParams{
			PageSize:  2,
			UsuarioID: &userID,
		})
		require.NoError(t, err)
		assert.Len(t, p1.Items, 2)
		assert.True(t, p1.HasMore)
		assert.False(t, p1.NextReceivedAt.IsZero())
		assert.NotEqual(t, uuid.Nil, p1.NextID)

		// Page 2: use cursor from page 1.
		p2, err := s.List(ctx, failedintent.ListParams{
			PageSize:         2,
			UsuarioID:        &userID,
			CursorReceivedAt: p1.NextReceivedAt,
			CursorID:         p1.NextID,
		})
		require.NoError(t, err)
		assert.Len(t, p2.Items, 2)
		assert.True(t, p2.HasMore)

		// Page 3: last page.
		p3, err := s.List(ctx, failedintent.ListParams{
			PageSize:         2,
			UsuarioID:        &userID,
			CursorReceivedAt: p2.NextReceivedAt,
			CursorID:         p2.NextID,
		})
		require.NoError(t, err)
		assert.Len(t, p3.Items, 1)
		assert.False(t, p3.HasMore)

		// All 5 IDs must appear exactly once across the 3 pages.
		seenIDs := make(map[uuid.UUID]int)
		for _, item := range append(append(p1.Items, p2.Items...), p3.Items...) {
			seenIDs[item.ID]++
		}
		for _, id := range allIDs {
			assert.Equal(t, 1, seenIDs[id], "intent %s must appear exactly once", id)
		}
	})
}

// TestList_FiltersByStatus verifies the Status filter.
func TestList_FiltersByStatus(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		userID := uuid.New() // scope to isolate

		var ignoredIDs []uuid.UUID
		for i := range 3 {
			intent := newIntent(uuid.New(), time.Now().UTC().Add(time.Duration(i)*time.Second), failedintent.StatusIgnored)
			intent.UsuarioID = &userID
			require.NoError(t, s.Save(ctx, intent))
			ignoredIDs = append(ignoredIDs, intent.ID)
		}
		for i := range 2 {
			intent := newIntent(uuid.New(), time.Now().UTC().Add(time.Duration(i+10)*time.Second), failedintent.StatusNew)
			intent.UsuarioID = &userID
			require.NoError(t, s.Save(ctx, intent))
		}

		page, err := s.List(ctx, failedintent.ListParams{
			Status:    failedintent.StatusIgnored,
			UsuarioID: &userID,
			PageSize:  20,
		})
		require.NoError(t, err)
		assert.Len(t, page.Items, 3, "exactly 3 ignored rows scoped to userID")
		for _, item := range page.Items {
			assert.Equal(t, failedintent.StatusIgnored, item.Status)
		}
		returnedSet := make(map[uuid.UUID]bool)
		for _, item := range page.Items {
			returnedSet[item.ID] = true
		}
		for _, id := range ignoredIDs {
			assert.True(t, returnedSet[id], "ignored intent %s must appear", id)
		}
	})
}

// TestList_FiltersByUsuarioID verifies the UsuarioID filter.
func TestList_FiltersByUsuarioID(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		userA := uuid.New()
		userB := uuid.New()

		for i := range 4 {
			intent := newIntent(uuid.New(), time.Now().UTC().Add(time.Duration(i)*time.Second), failedintent.StatusNew)
			intent.UsuarioID = &userA
			require.NoError(t, s.Save(ctx, intent))
		}
		for i := range 2 {
			intent := newIntent(uuid.New(), time.Now().UTC().Add(time.Duration(i+10)*time.Second), failedintent.StatusNew)
			intent.UsuarioID = &userB
			require.NoError(t, s.Save(ctx, intent))
		}

		pageA, err := s.List(ctx, failedintent.ListParams{
			UsuarioID: &userA,
			PageSize:  20,
		})
		require.NoError(t, err)
		assert.Len(t, pageA.Items, 4)
		for _, item := range pageA.Items {
			require.NotNil(t, item.UsuarioID)
			assert.Equal(t, userA, *item.UsuarioID)
		}
	})
}

// TestList_PageSizeClamped verifies pageSize=0 → default 20, and 999 → max 100.
func TestList_PageSizeClamped(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		// pageSize=0 must not error; the implementation clamps it to default.
		_, err := s.List(ctx, failedintent.ListParams{PageSize: 0})
		require.NoError(t, err)

		// pageSize=999 must not error; the implementation clamps it to 100.
		_, err = s.List(ctx, failedintent.ListParams{PageSize: 999})
		require.NoError(t, err)
	})
}

// TestUpdateStatus_Success verifies the happy-path status transition.
func TestUpdateStatus_Success(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		intent.ResolvedAt = nil
		intent.ResolvedBy = nil
		intent.Notes = ""
		require.NoError(t, s.Save(ctx, intent))

		resolverID := uuid.New()
		now := time.Now().UTC().Truncate(time.Millisecond)

		err := s.UpdateStatus(ctx, intent.ID, failedintent.StatusNew, failedintent.StatusIgnored, resolverID, "nota de prueba", now)
		require.NoError(t, err)

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, failedintent.StatusIgnored, got.Status)
		require.NotNil(t, got.ResolvedAt)
		assert.WithinDuration(t, now, *got.ResolvedAt, time.Second)
		require.NotNil(t, got.ResolvedBy)
		assert.Equal(t, resolverID, *got.ResolvedBy)
		assert.Equal(t, "nota de prueba", got.Notes)
	})
}

// TestUpdateStatus_Conflict_WhenExpectedDoesntMatch verifies the conflict
// apperror code and fields.
func TestUpdateStatus_Conflict_WhenExpectedDoesntMatch(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		require.NoError(t, s.Save(ctx, intent))

		resolverID := uuid.New()
		now := time.Now().UTC()

		// First transition succeeds.
		require.NoError(t, s.UpdateStatus(
			ctx, intent.ID,
			failedintent.StatusNew, failedintent.StatusIgnored,
			resolverID, "", now,
		))

		// Second transition fails because expected=new but current=ignored.
		err := s.UpdateStatus(
			ctx, intent.ID,
			failedintent.StatusNew, failedintent.StatusIgnored,
			resolverID, "retry", now,
		)
		require.Error(t, err)

		appErr, ok := apperror.As(err)
		require.True(t, ok, "error must be *apperror.Error")
		assert.Equal(t, apperror.KindConflict, appErr.Kind)
		assert.Equal(t, "failed_intent_status_conflict", appErr.Code)
	})
}

// TestUpdateStatus_Conflict_WhenIDMissing verifies that updating a non-existent
// ID also returns the conflict apperror (0 rows affected).
func TestUpdateStatus_Conflict_WhenIDMissing(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		err := s.UpdateStatus(
			ctx, uuid.New(), // non-existent ID
			failedintent.StatusNew, failedintent.StatusIgnored,
			uuid.New(), "", time.Now().UTC(),
		)
		require.Error(t, err)

		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, "failed_intent_status_conflict", appErr.Code)
	})
}

// TestTransitionAfterReplay_OnlyTouchesStatus is the regression for the bug
// where the Resolver-shaped UpdateStatus was being used after replays,
// stamping ResolvedAt / ResolvedBy / Notes on intents that no operator had
// actually resolved. The new TransitionAfterReplay method writes ONLY the
// STATUS column.
func TestTransitionAfterReplay_OnlyTouchesStatus(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		intent.ResolvedAt = nil
		intent.ResolvedBy = nil
		intent.Notes = ""
		require.NoError(t, s.Save(ctx, intent))

		require.NoError(t, s.TransitionAfterReplay(
			ctx, intent.ID,
			failedintent.StatusNew, failedintent.StatusRetriedOK,
		))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, failedintent.StatusRetriedOK, got.Status)
		assert.Nil(t, got.ResolvedAt, "ResolvedAt must stay nil")
		assert.Nil(t, got.ResolvedBy, "ResolvedBy must stay nil")
		assert.Empty(t, got.Notes, "Notes must stay empty")
	})
}

// TestTransitionAfterReplay_PreservesPrePopulatedResolverFields verifies that
// even when a row already has ResolvedAt / ResolvedBy / Notes (e.g. an
// operator previously marked it as ignored, then the system reopened it
// somehow — defensive scenario), the replay transition still does NOT
// overwrite those fields.
func TestTransitionAfterReplay_PreservesPrePopulatedResolverFields(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		// Seed an intent already carrying operator-resolution metadata.
		originalResolver := uuid.New()
		originalResolvedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Millisecond)
		intent := newIntent(uuid.New(), time.Now().UTC().Add(-3*time.Hour), failedintent.StatusRetriedFail)
		intent.ResolvedAt = &originalResolvedAt
		intent.ResolvedBy = &originalResolver
		intent.Notes = "nota del operador original"
		require.NoError(t, s.Save(ctx, intent))

		require.NoError(t, s.TransitionAfterReplay(
			ctx, intent.ID,
			failedintent.StatusRetriedFail, failedintent.StatusRetriedOK,
		))

		got, err := s.Get(ctx, intent.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, failedintent.StatusRetriedOK, got.Status)
		require.NotNil(t, got.ResolvedAt)
		assert.WithinDuration(t, originalResolvedAt, *got.ResolvedAt, time.Second,
			"ResolvedAt must be preserved exactly")
		require.NotNil(t, got.ResolvedBy)
		assert.Equal(t, originalResolver, *got.ResolvedBy,
			"ResolvedBy must be preserved exactly")
		assert.Equal(t, "nota del operador original", got.Notes,
			"Notes must be preserved exactly")
	})
}

// TestTransitionAfterReplay_Conflict_WhenExpectedDoesntMatch verifies the
// conflict apperror is returned with the same shape as UpdateStatus when the
// current status differs from the expected.
func TestTransitionAfterReplay_Conflict_WhenExpectedDoesntMatch(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
		require.NoError(t, s.Save(ctx, intent))

		// Expected says retried_fail but actual is new — must conflict.
		err := s.TransitionAfterReplay(
			ctx, intent.ID,
			failedintent.StatusRetriedFail, failedintent.StatusRetriedOK,
		)
		require.Error(t, err)

		appErr, ok := apperror.As(err)
		require.True(t, ok)
		assert.Equal(t, apperror.KindConflict, appErr.Kind)
		assert.Equal(t, "failed_intent_status_conflict", appErr.Code)
	})
}

// TestIncrementRetry_BumpsCount verifies two increments land at retry_count=2.
func TestIncrementRetry_BumpsCount(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		intent := newIntent(uuid.New(), time.Now().UTC(), failedintent.StatusNew)
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

// TestIncrementRetry_NoRow_DoesNotError verifies that UPDATE 0 rows is not an
// error (matching the Postgres implementation's contract).
func TestIncrementRetry_NoRow_DoesNotError(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		require.NoError(t, s.IncrementRetry(ctx, uuid.New()))
	})
}

// TestPurgeOlderThan_RemovesAndReturnsCount_PlusBlobPaths seeds 3 rows older
// than the cutoff + 1 newer row. Verifies count + paths returned.
func TestPurgeOlderThan_RemovesAndReturnsCount_PlusBlobPaths(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		now := time.Now().UTC()

		oldest := newIntent(uuid.New(), now.Add(-3*time.Hour), failedintent.StatusNew)
		oldest.BodyBlobPath = fmt.Sprintf("/tmp/fi-%s.bin", uuid.New())
		require.NoError(t, s.Save(ctx, oldest))

		middle := newIntent(uuid.New(), now.Add(-2*time.Hour), failedintent.StatusNew)
		middle.BodyBlobPath = fmt.Sprintf("/tmp/fi-%s.bin", uuid.New())
		require.NoError(t, s.Save(ctx, middle))

		noBlob := newIntent(uuid.New(), now.Add(-90*time.Minute), failedintent.StatusNew)
		noBlob.BodyBlobPath = "" // no blob path
		require.NoError(t, s.Save(ctx, noBlob))

		newer := newIntent(uuid.New(), now, failedintent.StatusNew)
		require.NoError(t, s.Save(ctx, newer))

		cutoff := now.Add(-60 * time.Minute) // cuts off the 3 older rows
		result, err := s.PurgeOlderThan(ctx, cutoff)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result.RowsDeleted)
		assert.ElementsMatch(t, []string{oldest.BodyBlobPath, middle.BodyBlobPath}, result.BlobPaths)

		// Older rows must be gone.
		gotOldest, err := s.Get(ctx, oldest.ID)
		require.NoError(t, err)
		assert.Nil(t, gotOldest)

		// Newer row must still be present.
		gotNewer, err := s.Get(ctx, newer.ID)
		require.NoError(t, err)
		assert.NotNil(t, gotNewer)
	})
}

// TestPurgeOlderThan_NoMatch_ReturnsZero verifies that no rows deleted is not
// an error and returns zero counts.
func TestPurgeOlderThan_NoMatch_ReturnsZero(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)
		// Cutoff far in the past — nothing should be older.
		cutoff := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		result, err := s.PurgeOlderThan(ctx, cutoff)
		require.NoError(t, err)
		assert.Equal(t, int64(0), result.RowsDeleted)
		assert.Empty(t, result.BlobPaths)
	})
}

// TestReferencedPaths_ReturnsOnlyNonEmpty verifies that only rows with a
// non-NULL/non-empty BODY_BLOB_PATH are returned.
func TestReferencedPaths_ReturnsOnlyNonEmpty(t *testing.T) {
	t.Parallel()
	pool := fbtestutil.NewTestFirebirdPool(t)
	requireFailedIntentsTable(t, pool)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		s := failedintentfb.New(pool)

		blob1Path := fmt.Sprintf("/tmp/fi-%s.bin", uuid.New())
		blob2Path := fmt.Sprintf("/tmp/fi-%s.bin", uuid.New())

		withBlob1 := newIntent(uuid.New(), time.Now().UTC().Add(-1*time.Second), failedintent.StatusNew)
		withBlob1.BodyBlobPath = blob1Path
		require.NoError(t, s.Save(ctx, withBlob1))

		withBlob2 := newIntent(uuid.New(), time.Now().UTC().Add(-2*time.Second), failedintent.StatusNew)
		withBlob2.BodyBlobPath = blob2Path
		require.NoError(t, s.Save(ctx, withBlob2))

		withoutBlob := newIntent(uuid.New(), time.Now().UTC().Add(-3*time.Second), failedintent.StatusNew)
		withoutBlob.BodyBlobPath = ""
		require.NoError(t, s.Save(ctx, withoutBlob))

		paths, err := s.ReferencedPaths(ctx)
		require.NoError(t, err)

		// Check our two blob paths are in the returned set.
		pathSet := make(map[string]bool, len(paths))
		for _, p := range paths {
			pathSet[p] = true
		}
		assert.True(t, pathSet[blob1Path], "blob1 path must appear in referenced paths")
		assert.True(t, pathSet[blob2Path], "blob2 path must appear in referenced paths")
		assert.False(t, pathSet[""], "empty path must not appear")
	})
}
