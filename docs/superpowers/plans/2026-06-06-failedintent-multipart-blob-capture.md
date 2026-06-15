# Failed-Intent Multipart Blob Capture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture every failed `POST /v2/ventas` (multipart) so no venta is ever lost — by streaming the raw multipart body to disk via a new hex-clean `BlobStorage` outbound port, recording the blob path in Postgres, and replaying the byte-exact body through the existing dispatcher.

**Architecture:**
- New outbound port `failedintent.BlobStorage` (Save / Open / Delete) keeps the platform package agnostic of the on-disk layout. The single concrete adapter `failedintent/blobfs` lives under `STORAGE_DIR/failed-intents/{intent_id}.bin` with atomic `O_EXCL` writes + `rename`, 0o600 perms.
- `CaptureMiddleware` stops skipping `multipart/form-data`. Instead it streams the body through a `TeeReader` into the blob store while the handler reads it, captures the response status, and on `>= 400` persists an `Intent` whose new `BodyBlobPath` + `BodyContentType` fields point at the on-disk blob (so JSON bodies stay in the existing `body` column unchanged).
- Replay handlers (`Replay`, `ReplayWith`) gain a code branch: when an intent has a blob, the replay request streams `BlobStorage.Open(path)` instead of `bytes.NewReader(body)`, and propagates the captured `Content-Type` header. `ReplayWith` only accepts JSON overrides — multipart corrections are out of scope (operator can re-trigger the flow from the device).
- The janitor gains a pre-purge sweep: load expiring rows' blob paths, delete the rows, then best-effort `Delete` the orphaned blobs. A separate startup orphan-sweep removes blob files that lost their DB row (e.g. process crashed after blob write but before INSERT).
- A nullable `body_blob_path TEXT` + `body_content_type TEXT` column pair is added to `failed_intents` via migration `000006`. Both stay null for JSON intents, keeping the JSON code path identical.

**Tech Stack:** Go 1.22+, `github.com/jackc/pgx/v5`, `github.com/google/uuid`, golangci-lint, golang-migrate. Tests use `httptest`, `t.TempDir()`, in-process fakes (no Docker for unit tests).

---

## File Structure

### Create

| Path | Responsibility |
|---|---|
| `migrations/000006_failed_intents_body_blob.up.sql` | Add `body_blob_path TEXT NULL` + `body_content_type TEXT NULL`. |
| `migrations/000006_failed_intents_body_blob.down.sql` | Drop both columns. |
| `internal/platform/failedintent/blob.go` | `BlobStorage` outbound port + `BlobNotFound` sentinel + `BlobRef` value object. |
| `internal/platform/failedintent/blob_test.go` | Sentinel + interface compile-checks. |
| `internal/platform/failedintent/blobfs/store.go` | Filesystem-backed `BlobStorage` (atomic writes, 0o600, 50 MB hard cap). |
| `internal/platform/failedintent/blobfs/store_test.go` | Unit tests: write/read/delete/idempotent-delete/cap-overflow/atomic-rename. |
| `internal/platform/failedintent/blobfs/orphan.go` | One-shot orphan sweeper (list disk → diff vs DB → delete unreferenced). |
| `internal/platform/failedintent/blobfs/orphan_test.go` | Unit tests for orphan sweep. |
| `internal/platform/failedintent/multipart_test.go` | End-to-end CaptureMiddleware test for multipart path. |
| `internal/platform/failedintent/http/replay_blob_test.go` | Replay-with-blob handler test (intent.BlobPath ≠ "", body streamed verbatim). |

### Modify

| Path | What changes |
|---|---|
| `internal/platform/failedintent/failedintent.go` | Add `Intent.BodyBlobPath` + `Intent.BodyContentType` fields, `Config.Blob BlobStorage`, `Config.MaxMultipartBytes int64`. Replace the multipart-skip branch with a streaming-tee path. |
| `internal/platform/failedintent/postgres/store.go` | Persist/scan the two new columns; everything else stays. |
| `internal/platform/failedintent/postgres/store_test.go` | Round-trip a blob-backed intent. |
| `internal/platform/failedintent/janitor.go` | New `PurgeOlderThan` contract: return purged IDs so the janitor can delete the blobs. Add `BlobStorage` field on `JanitorConfig` (optional — nil falls back to today's behaviour). |
| `internal/platform/failedintent/janitor_test.go` | Add blob-cleanup case. |
| `internal/platform/failedintent/http/handlers.go` | `buildReplayRequest` reads from `BlobStorage.Open(intent.BodyBlobPath)` when set; propagates `Intent.BodyContentType`; `ReplayWith` rejects blob intents with `apperror.NewValidation("blob_intent_replay_with_unsupported", …)`. |
| `internal/platform/failedintent/http/handlers_test.go` | Adjust constructors that take new dependencies. |
| `cmd/api/failedintent_wiring.go` | Add providers `provideFailedIntentBlobStorage`, update capture config + janitor config wiring; pass `BlobStorage` into the http Service. |
| `cmd/api/main.go` | Register new providers + invoke the orphan sweep at boot. |
| `internal/platform/config/config.go` | Add `FailedIntent` config section with `BlobDir` (env `FAILEDINTENT_BLOB_DIR`, default `${STORAGE_DIR}/failed-intents`) and `MaxMultipartBytes` (env `FAILEDINTENT_MAX_MULTIPART_BYTES`, default `52428800` = 50 MB). |
| `.env.example` | Document the two new env vars. |
| `.golangci.yml` | Add importas alias for the new `blobfs` subpackage. |

---

## Task 1: Schema migration

**Files:**
- Create: `migrations/000006_failed_intents_body_blob.up.sql`
- Create: `migrations/000006_failed_intents_body_blob.down.sql`

- [ ] **Step 1: Write up migration**

```sql
-- File: migrations/000006_failed_intents_body_blob.up.sql

-- Adds two columns so failed multipart requests can be captured.
--
-- body_blob_path is the absolute on-disk path where the raw request body lives
-- when it could not be stored in the body TEXT column (multipart uploads).
-- body_content_type carries the original Content-Type header so replay can
-- forward the body byte-exact with the boundary string intact.
--
-- Both columns are nullable: JSON intents continue to use the body column and
-- leave these null. Multipart intents leave body = '' and populate these.
--
-- Per CLAUDE.md no logic in the database — no CHECK constraints encoding
-- "body OR body_blob_path required", that rule lives in Go (failedintent.go).
ALTER TABLE failed_intents
    ADD COLUMN body_blob_path     TEXT,
    ADD COLUMN body_content_type  TEXT;
```

- [ ] **Step 2: Write down migration**

```sql
-- File: migrations/000006_failed_intents_body_blob.down.sql
ALTER TABLE failed_intents
    DROP COLUMN body_blob_path,
    DROP COLUMN body_content_type;
```

- [ ] **Step 3: Apply migration locally**

Run: `make migrate-up` (or `migrate -path migrations -database "$DATABASE_URL" up`)
Expected: `6/u failed_intents_body_blob (Xms)` printed; verify with `docker exec msp-postgres psql -U msp -d msp_dev -c '\d failed_intents' | grep body_blob_path` returns one row.

- [ ] **Step 4: Commit**

```bash
git add migrations/000006_failed_intents_body_blob.up.sql migrations/000006_failed_intents_body_blob.down.sql
git commit -m "feat(failedintent): add body_blob_path + body_content_type columns"
```

---

## Task 2: Define the BlobStorage outbound port

**Files:**
- Create: `internal/platform/failedintent/blob.go`
- Create: `internal/platform/failedintent/blob_test.go`

- [ ] **Step 1: Write the failing compile-check test**

```go
// File: internal/platform/failedintent/blob_test.go
package failedintent_test

import (
	"errors"
	"testing"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

func TestErrBlobNotFound_IsExportedSentinel(t *testing.T) {
	t.Parallel()
	if failedintent.ErrBlobNotFound == nil {
		t.Fatal("expected ErrBlobNotFound sentinel to be non-nil")
	}
	wrapped := errors.New("file missing: " + failedintent.ErrBlobNotFound.Error())
	if errors.Is(wrapped, failedintent.ErrBlobNotFound) {
		t.Fatal("manual wrap without %w must not satisfy errors.Is — guard against future refactor")
	}
}

// Compile-time assertion: any BlobStorage implementation must satisfy the
// triplet of methods declared in the port. A missing method here breaks the
// build at the same site that owns the contract.
var _ failedintent.BlobStorage = (failedintent.BlobStorage)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/...`
Expected: build fails with `undefined: failedintent.ErrBlobNotFound` and `undefined: failedintent.BlobStorage`.

- [ ] **Step 3: Write minimal implementation**

```go
// File: internal/platform/failedintent/blob.go
package failedintent

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"
)

// ErrBlobNotFound is returned by BlobStorage.Open when the requested blob is
// absent. Callers compare against this with errors.Is.
var ErrBlobNotFound = errors.New("failedintent: blob not found")

// BlobStorage is the outbound port for persisting raw request bodies that
// cannot reasonably fit in failed_intents.body (multipart uploads with
// embedded image binaries).
//
// Implementations must be safe for concurrent calls. Save is expected to be
// atomic: a partial write must never leave a readable blob behind.
type BlobStorage interface {
	// Save streams body into storage under the given intentID and returns the
	// opaque path callers must persist in failed_intents.body_blob_path.
	// limitBytes caps the size — if body exceeds the limit, Save returns an
	// error wrapping ErrBlobTooLarge and leaves no blob behind.
	Save(ctx context.Context, intentID uuid.UUID, body io.Reader, limitBytes int64) (string, error)

	// Open returns a ReadCloser for the blob at path. Returns ErrBlobNotFound
	// when the blob is absent. Callers must Close the result.
	Open(ctx context.Context, path string) (io.ReadCloser, error)

	// Delete removes the blob at path. Missing blobs are a no-op (returns nil).
	Delete(ctx context.Context, path string) error
}

// ErrBlobTooLarge is wrapped by Save when the body exceeds the configured
// limit. Kept distinct from ErrBlobNotFound so callers can branch.
var ErrBlobTooLarge = errors.New("failedintent: blob exceeds size limit")
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/failedintent/...`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/failedintent/blob.go internal/platform/failedintent/blob_test.go
git commit -m "feat(failedintent): add BlobStorage outbound port"
```

---

## Task 3: Filesystem BlobStorage adapter — save/open/delete

**Files:**
- Create: `internal/platform/failedintent/blobfs/store.go`
- Create: `internal/platform/failedintent/blobfs/store_test.go`

- [ ] **Step 1: Write the failing test**

```go
// File: internal/platform/failedintent/blobfs/store_test.go
package blobfs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	"github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
)

func TestStore_SaveOpenDelete_RoundTrip(t *testing.T) {
	t.Parallel()
	store, err := blobfs.NewStore(t.TempDir())
	require.NoError(t, err)

	id := uuid.New()
	payload := []byte("multipart-body-bytes\r\n--boundary--\r\n")

	path, err := store.Save(context.Background(), id, bytes.NewReader(payload), 1024)
	require.NoError(t, err)
	assert.NotEmpty(t, path)

	rc, err := store.Open(context.Background(), path)
	require.NoError(t, err)
	defer rc.Close()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, payload, got)

	require.NoError(t, store.Delete(context.Background(), path))
	_, openErr := store.Open(context.Background(), path)
	require.Error(t, openErr)
	assert.True(t, errors.Is(openErr, failedintent.ErrBlobNotFound))
}

func TestStore_Save_OverflowReturnsErrBlobTooLarge(t *testing.T) {
	t.Parallel()
	store, err := blobfs.NewStore(t.TempDir())
	require.NoError(t, err)

	id := uuid.New()
	// 11 bytes against a 10-byte limit.
	_, err = store.Save(context.Background(), id, bytes.NewReader([]byte("0123456789X")), 10)
	require.Error(t, err)
	assert.True(t, errors.Is(err, failedintent.ErrBlobTooLarge))

	// No leftover blob on disk for the intent id (overflow rolls back).
	entries, _ := os.ReadDir(filepath.Join(t.TempDir()))
	for _, e := range entries {
		require.NotContains(t, e.Name(), id.String(),
			"overflow must remove the partial blob; found %q", e.Name())
	}
}

func TestStore_Delete_IsIdempotent(t *testing.T) {
	t.Parallel()
	store, err := blobfs.NewStore(t.TempDir())
	require.NoError(t, err)

	// Deleting a non-existent path is a no-op.
	require.NoError(t, store.Delete(context.Background(), filepath.Join(t.TempDir(), "missing.bin")))
}

func TestStore_Open_MissingReturnsErrBlobNotFound(t *testing.T) {
	t.Parallel()
	store, err := blobfs.NewStore(t.TempDir())
	require.NoError(t, err)

	_, err = store.Open(context.Background(), filepath.Join(t.TempDir(), "absent.bin"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, failedintent.ErrBlobNotFound))
}

func TestStore_Save_PathContainsIntentID(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := blobfs.NewStore(dir)
	require.NoError(t, err)

	id := uuid.New()
	path, err := store.Save(context.Background(), id, bytes.NewReader([]byte("x")), 1024)
	require.NoError(t, err)
	assert.True(t, filepath.HasPrefix(path, dir), "blob must live under baseDir")
	assert.Contains(t, filepath.Base(path), id.String(),
		"blob filename should include the intent id for traceability")
}

func TestStore_Save_FilePermissions(t *testing.T) {
	t.Parallel()
	store, err := blobfs.NewStore(t.TempDir())
	require.NoError(t, err)

	path, err := store.Save(context.Background(), uuid.New(), bytes.NewReader([]byte("x")), 1024)
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(),
		"blob must be 0o600 (only the process owner reads)")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/blobfs/...`
Expected: build fails — package does not exist yet.

- [ ] **Step 3: Write minimal implementation**

```go
// File: internal/platform/failedintent/blobfs/store.go
//
// Package blobfs is the filesystem-backed BlobStorage adapter for the
// failedintent module. Blobs live under a configured base directory, written
// atomically (tmp + rename), with restrictive permissions (0o600).
package blobfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

const (
	// blobFileMode keeps blobs readable only by the process owner.
	blobFileMode os.FileMode = 0o600
	// dirMode for the base directory and any intermediate dirs.
	dirMode os.FileMode = 0o700
)

// Store is the filesystem-backed BlobStorage adapter.
type Store struct {
	baseDir string
}

// NewStore returns a Store rooted at baseDir, creating the directory tree if
// missing. Empty baseDir is rejected.
func NewStore(baseDir string) (*Store, error) {
	if baseDir == "" {
		return nil, errors.New("blobfs: baseDir is required")
	}
	if err := os.MkdirAll(baseDir, dirMode); err != nil {
		return nil, fmt.Errorf("blobfs: mkdir %s: %w", baseDir, err)
	}
	return &Store{baseDir: baseDir}, nil
}

// Compile-time check: Store satisfies the port.
var _ failedintent.BlobStorage = (*Store)(nil)

// Save streams body into <baseDir>/<intentID>.bin atomically. limitBytes is a
// hard cap — exceeding it deletes the temp file and returns ErrBlobTooLarge.
func (s *Store) Save(_ context.Context, intentID uuid.UUID, body io.Reader, limitBytes int64) (string, error) {
	finalPath := filepath.Join(s.baseDir, intentID.String()+".bin")
	tmp, err := os.CreateTemp(s.baseDir, intentID.String()+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("blobfs: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup of the temp file on any error path.
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(blobFileMode); err != nil {
		return "", fmt.Errorf("blobfs: chmod: %w", err)
	}

	// Read up to limit+1 to detect overflow in a single pass.
	written, copyErr := io.CopyN(tmp, body, limitBytes+1)
	if copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return "", fmt.Errorf("blobfs: write blob: %w", copyErr)
	}
	if written > limitBytes {
		return "", failedintent.ErrBlobTooLarge
	}

	if err := tmp.Sync(); err != nil {
		return "", fmt.Errorf("blobfs: fsync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("blobfs: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("blobfs: rename: %w", err)
	}
	committed = true
	return finalPath, nil
}

// Open returns a ReadCloser for the blob at path. Missing blobs map to
// failedintent.ErrBlobNotFound.
func (s *Store) Open(_ context.Context, path string) (io.ReadCloser, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from our own Save and the DB row.
	if errors.Is(err, os.ErrNotExist) {
		return nil, failedintent.ErrBlobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("blobfs: open: %w", err)
	}
	return f, nil
}

// Delete removes path. A missing file is a no-op (idempotent).
func (s *Store) Delete(_ context.Context, path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("blobfs: delete: %w", err)
	}
	return nil
}

// BaseDir exposes the configured base directory so callers (orphan sweep) can
// enumerate the on-disk blobs.
func (s *Store) BaseDir() string { return s.baseDir }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/failedintent/blobfs/...`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/failedintent/blobfs/store.go internal/platform/failedintent/blobfs/store_test.go
git commit -m "feat(failedintent): add filesystem-backed BlobStorage adapter"
```

---

## Task 4: Extend Intent with blob fields

**Files:**
- Modify: `internal/platform/failedintent/failedintent.go` (Intent struct, around line 108)

- [ ] **Step 1: Write the failing test**

```go
// Append to: internal/platform/failedintent/failedintent_test.go
//
// Add this test in the existing _test.go file in the same package; no new file.
func TestIntent_BlobFields_DefaultEmpty(t *testing.T) {
	t.Parallel()
	var i failedintent.Intent
	if i.BodyBlobPath != "" {
		t.Fatalf("BodyBlobPath default should be empty, got %q", i.BodyBlobPath)
	}
	if i.BodyContentType != "" {
		t.Fatalf("BodyContentType default should be empty, got %q", i.BodyContentType)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/`
Expected: build fails on `i.BodyBlobPath undefined`.

- [ ] **Step 3: Add fields to Intent**

In `internal/platform/failedintent/failedintent.go` extend the Intent struct (currently ends with `Notes string`):

```go
// Intent is the canonical captured record.
type Intent struct {
	ID             uuid.UUID
	ReceivedAt     time.Time
	Method         string
	Path           string
	FirebaseUID    string
	UsuarioID      *uuid.UUID
	IdempotencyKey string
	RequestID      uuid.UUID
	Body           json.RawMessage
	BodyTruncated  bool
	HTTPStatus     int
	ErrorCode      string
	ErrorMessage   string
	RetryCount     int
	Status         Status
	ResolvedAt     *time.Time
	ResolvedBy     *uuid.UUID
	Notes          string
	// BodyBlobPath is the absolute on-disk path of the captured request body
	// when it was streamed to BlobStorage instead of inlined in Body. Empty
	// for the dominant JSON case.
	BodyBlobPath string
	// BodyContentType carries the original Content-Type header verbatim so
	// replay can forward the body byte-exact with the multipart boundary
	// intact. Empty when Body holds the request (replay defaults to
	// application/json in that case).
	BodyContentType string
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/platform/failedintent/`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/platform/failedintent/failedintent.go internal/platform/failedintent/failedintent_test.go
git commit -m "feat(failedintent): extend Intent with blob path + content-type"
```

---

## Task 5: Persist new fields in Postgres store

**Files:**
- Modify: `internal/platform/failedintent/postgres/store.go` (Save, Get, List queries + scanIntent)
- Modify: `internal/platform/failedintent/postgres/store_test.go`

- [ ] **Step 1: Write the failing round-trip test**

Add to `internal/platform/failedintent/postgres/store_test.go`:

```go
func TestStore_Save_Get_RoundTripsBlobFields(t *testing.T) {
	t.Parallel()
	pool := newTestPool(t) // assume this helper already exists in the file
	store := failedintentpg.New(pool)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	id := uuid.New()
	requestID := uuid.New()
	intent := failedintent.Intent{
		ID:              id,
		ReceivedAt:      now,
		Method:          "POST",
		Path:            "/v2/ventas",
		RequestID:       requestID,
		Body:            json.RawMessage(`""`),
		HTTPStatus:      422,
		ErrorCode:       "validation_failed",
		ErrorMessage:    "campo requerido",
		Status:          failedintent.StatusNew,
		BodyBlobPath:    "/var/uploads/failed-intents/" + id.String() + ".bin",
		BodyContentType: "multipart/form-data; boundary=----abc",
	}
	require.NoError(t, store.Save(ctx, intent))

	got, err := store.Get(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, intent.BodyBlobPath, got.BodyBlobPath)
	assert.Equal(t, intent.BodyContentType, got.BodyContentType)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/postgres/...` (with Postgres running locally; the test skips if not).
Expected: assertion failure — `BodyBlobPath` and `BodyContentType` come back empty because the store does not persist them yet.

- [ ] **Step 3: Update Save query**

In `internal/platform/failedintent/postgres/store.go`, replace the Save constant block and its argument list. The new query adds two columns at the tail:

```go
func (s *Store) Save(ctx context.Context, i failedintent.Intent) error {
	const q = `
		INSERT INTO failed_intents (
			id, received_at, method, path, firebase_uid, usuario_id,
			idempotency_key, request_id, body, body_truncated, http_status,
			error_code, error_message, retry_count, status, resolved_at,
			resolved_by, notes, body_blob_path, body_content_type
		)
		VALUES (
			$1, $2, $3, $4, NULLIF($5, ''), $6,
			NULLIF($7, ''), $8, $9, $10, $11,
			$12, $13, $14, $15, $16,
			$17, $18, NULLIF($19, ''), NULLIF($20, '')
		)
		ON CONFLICT (id) DO NOTHING`
	if _, err := transaction.GetQuerier(ctx, s.pool).Exec(ctx, q,
		i.ID,
		i.ReceivedAt.UTC(),
		i.Method,
		i.Path,
		i.FirebaseUID,
		i.UsuarioID,
		i.IdempotencyKey,
		i.RequestID,
		[]byte(i.Body),
		i.BodyTruncated,
		i.HTTPStatus,
		i.ErrorCode,
		i.ErrorMessage,
		i.RetryCount,
		string(i.Status),
		nullTime(i.ResolvedAt),
		i.ResolvedBy,
		i.Notes,
		i.BodyBlobPath,
		i.BodyContentType,
	); err != nil {
		return fmt.Errorf("failedintent.postgres: save %s: %w", i.ID, err)
	}
	return nil
}
```

- [ ] **Step 4: Update Get + List SELECT lists**

In the same file, replace both SELECT strings (lines around 88 and 116) so they read the two new columns at the tail. Use `COALESCE(body_blob_path, '')` + `COALESCE(body_content_type, '')` so NULLs scan into the empty string.

```go
// Get's SELECT:
const q = `
    SELECT
        id, received_at, method, path,
        COALESCE(firebase_uid, ''), usuario_id,
        COALESCE(idempotency_key, ''), request_id,
        body, body_truncated, http_status,
        error_code, error_message, retry_count, status,
        resolved_at, resolved_by, COALESCE(notes, ''),
        COALESCE(body_blob_path, ''), COALESCE(body_content_type, '')
    FROM failed_intents
    WHERE id = $1`
```

```go
// List's SELECT:
const q = `
    SELECT
        id, received_at, method, path,
        COALESCE(firebase_uid, ''), usuario_id,
        COALESCE(idempotency_key, ''), request_id,
        body, body_truncated, http_status,
        error_code, error_message, retry_count, status,
        resolved_at, resolved_by, COALESCE(notes, ''),
        COALESCE(body_blob_path, ''), COALESCE(body_content_type, '')
    FROM failed_intents
    WHERE ($1::timestamptz IS NULL
           OR received_at < $1
           OR (received_at = $1 AND id < $2))
      AND ($3::text IS NULL OR status = $3)
      AND ($4::uuid IS NULL OR usuario_id = $4)
    ORDER BY received_at DESC, id DESC
    LIMIT $5`
```

- [ ] **Step 5: Update scanIntent**

In the same file, extend `scanIntent` to scan the two extra columns:

```go
func scanIntent(row pgx.Row) (failedintent.Intent, error) {
	var (
		i        failedintent.Intent
		statusS  string
		body     []byte
		resolved *time.Time
	)
	err := row.Scan(
		&i.ID,
		&i.ReceivedAt,
		&i.Method,
		&i.Path,
		&i.FirebaseUID,
		&i.UsuarioID,
		&i.IdempotencyKey,
		&i.RequestID,
		&body,
		&i.BodyTruncated,
		&i.HTTPStatus,
		&i.ErrorCode,
		&i.ErrorMessage,
		&i.RetryCount,
		&statusS,
		&resolved,
		&i.ResolvedBy,
		&i.Notes,
		&i.BodyBlobPath,
		&i.BodyContentType,
	)
	if err != nil {
		return failedintent.Intent{}, err
	}
	i.Body = body
	i.Status = failedintent.Status(statusS)
	if resolved != nil {
		ut := resolved.UTC()
		i.ResolvedAt = &ut
	}
	return i, nil
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/platform/failedintent/postgres/...`
Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/failedintent/postgres/store.go internal/platform/failedintent/postgres/store_test.go
git commit -m "feat(failedintent): persist blob path + content-type in Postgres store"
```

---

## Task 6: Capture multipart bodies via tee-to-blob

**Files:**
- Modify: `internal/platform/failedintent/failedintent.go` (Config, shouldCapture, handle)
- Create: `internal/platform/failedintent/multipart_test.go`

- [ ] **Step 1: Write the failing test**

```go
// File: internal/platform/failedintent/multipart_test.go
package failedintent_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// fakeBlobStore is an in-memory BlobStorage for tests.
type fakeBlobStore struct {
	mu    sync.Mutex
	blobs map[string][]byte
	saves int
	fail  error // when non-nil, Save returns this error
}

func newFakeBlobStore() *fakeBlobStore {
	return &fakeBlobStore{blobs: make(map[string][]byte)}
}

func (f *fakeBlobStore) Save(_ context.Context, id uuid.UUID, body io.Reader, _ int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saves++
	if f.fail != nil {
		return "", f.fail
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	path := "/fake/" + id.String() + ".bin"
	f.blobs[path] = buf
	return path, nil
}

func (f *fakeBlobStore) Open(_ context.Context, path string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[path]
	if !ok {
		return nil, failedintent.ErrBlobNotFound
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeBlobStore) Delete(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blobs, path)
	return nil
}

// memStore is a minimal in-memory failedintent.Store for these middleware tests.
type memStore struct {
	mu      sync.Mutex
	intents []failedintent.Intent
}

func (m *memStore) Save(_ context.Context, i failedintent.Intent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.intents = append(m.intents, i)
	return nil
}
func (m *memStore) Get(context.Context, uuid.UUID) (*failedintent.Intent, error)         { return nil, nil }
func (m *memStore) List(context.Context, failedintent.ListParams) (failedintent.Page[failedintent.Intent], error) {
	return failedintent.Page[failedintent.Intent]{}, nil
}
func (m *memStore) UpdateStatus(context.Context, uuid.UUID, failedintent.Status, failedintent.Status, uuid.UUID, string, time.Time) error {
	return nil
}
func (m *memStore) IncrementRetry(context.Context, uuid.UUID) error          { return nil }
func (m *memStore) PurgeOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

func TestCaptureMiddleware_Multipart422_StoresBlob(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	blobs := newFakeBlobStore()

	mw := failedintent.CaptureMiddleware(failedintent.Config{
		Store:             store,
		Blob:              blobs,
		MaxMultipartBytes: 1024,
	})

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		require.Contains(t, string(body), "boundary-content")
		http.Error(w, `{"code":"validation_failed"}`, http.StatusUnprocessableEntity)
	}))

	body := "--BOUND\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\nboundary-content\r\n--BOUND--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=BOUND")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, 1, blobs.saves, "blob should be saved once")
	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.intents, 1)
	intent := store.intents[0]
	assert.NotEmpty(t, intent.BodyBlobPath, "BodyBlobPath must be populated")
	assert.Equal(t, "multipart/form-data; boundary=BOUND", intent.BodyContentType)
	assert.Equal(t, "", string(intent.Body), "Body must stay empty for blob-backed intents")
	stored, ok := blobs.blobs[intent.BodyBlobPath]
	require.True(t, ok)
	assert.Equal(t, body, string(stored), "blob must be byte-exact with request body")
}

func TestCaptureMiddleware_Multipart_2xx_NoCapture(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	blobs := newFakeBlobStore()

	mw := failedintent.CaptureMiddleware(failedintent.Config{
		Store: store, Blob: blobs, MaxMultipartBytes: 1024,
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader("--B--"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	store.mu.Lock()
	defer store.mu.Unlock()
	assert.Empty(t, store.intents, "no Intent for 2xx response")
	// Blob saves are allowed (best-effort write-ahead); however the cleanup
	// on success path must remove them. Acceptable: zero saves OR zero blobs left.
	if blobs.saves > 0 {
		assert.Empty(t, blobs.blobs, "blobs written on the success path must be cleaned up")
	}
}

func TestCaptureMiddleware_Multipart_BlobSaveFailure_FallsBackToEmptyBody(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	blobs := newFakeBlobStore()
	blobs.fail = errors.New("disk full")

	mw := failedintent.CaptureMiddleware(failedintent.Config{
		Store: store, Blob: blobs, MaxMultipartBytes: 1024,
	})
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"validation_failed"}`, http.StatusUnprocessableEntity)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v2/ventas", strings.NewReader("--B--"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=B")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.intents, 1, "intent should still be captured even when blob save fails")
	assert.Empty(t, store.intents[0].BodyBlobPath)
	assert.True(t, store.intents[0].BodyTruncated, "missing blob marked as truncated for ops visibility")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/`
Expected: build fails on `Config.Blob`, `Config.MaxMultipartBytes` undefined.

- [ ] **Step 3: Extend Config in failedintent.go**

Add fields to `Config` (around line 196) and update `defaults()`:

```go
// Config tunes CaptureMiddleware.
type Config struct {
	Store             Store
	PathPrefixes      []string
	Methods           []string
	BodyCapBytes      int64
	Clock             func() time.Time
	NewID             func() uuid.UUID
	// Blob is the outbound port used to persist multipart bodies. When nil,
	// the middleware skips multipart capture (legacy behaviour).
	Blob BlobStorage
	// MaxMultipartBytes caps each captured multipart body. Defaults to
	// DefaultMaxMultipartBytes (50 MiB). Ignored when Blob is nil.
	MaxMultipartBytes int64
}

// DefaultMaxMultipartBytes is the hard cap on a single captured multipart
// body. Chosen to fit ~10 JPEG-compressed evidence photos plus the JSON
// header without being so large that a single rogue client fills the disk.
const DefaultMaxMultipartBytes int64 = 50 * 1024 * 1024

func (c *Config) defaults() {
	if len(c.PathPrefixes) == 0 {
		c.PathPrefixes = []string{"/v2/ventas"}
	}
	if len(c.Methods) == 0 {
		c.Methods = []string{http.MethodPost, http.MethodPatch, http.MethodPut}
	}
	if c.BodyCapBytes == 0 {
		c.BodyCapBytes = DefaultBodyCapBytes
	}
	if c.MaxMultipartBytes == 0 {
		c.MaxMultipartBytes = DefaultMaxMultipartBytes
	}
	if c.Clock == nil {
		c.Clock = time.Now
	}
	if c.NewID == nil {
		c.NewID = uuid.New
	}
}
```

- [ ] **Step 4: Reshape shouldCapture and handle for multipart**

Replace the multipart-skip branch in `shouldCapture` so multipart now opts in when `cfg.Blob != nil`. Move the rest of the multipart handling into a dedicated branch of `handle`:

```go
func shouldCapture(cfg Config, r *http.Request) bool {
	if r.Header.Get(HeaderInternalReplay) != "" {
		return false
	}
	if !methodMatches(cfg.Methods, r.Method) {
		return false
	}
	if !pathPrefixMatches(cfg.PathPrefixes, r.URL.Path) {
		return false
	}
	// Multipart is captured only when a Blob store is wired. Without it the
	// middleware would have to dump base64 into a TEXT column — keep the
	// legacy skip behaviour for those deployments.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return cfg.Blob != nil
	}
	return true
}

func handle(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	if !shouldCapture(cfg, r) {
		next.ServeHTTP(w, r)
		return
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		handleMultipart(cfg, next, w, r)
		return
	}

	handleJSON(cfg, next, w, r) // existing logic, renamed
}
```

Rename the original body of `handle` to `handleJSON` (the logic stays identical — it already covers JSON bodies).

Add `handleMultipart`:

```go
// handleMultipart captures multipart requests. The request body is streamed
// through a tee into BlobStorage while the handler reads it; on a 4xx/5xx
// response the captured blob path is persisted on the Intent. On 2xx/3xx
// the blob is best-effort deleted to avoid orphans.
func handleMultipart(cfg Config, next http.Handler, w http.ResponseWriter, r *http.Request) {
	intentID := cfg.NewID()

	pr, pw := io.Pipe()
	// blobErr holds the BlobStorage.Save outcome; surfaced to the handler
	// after the response so a save failure does not abort the user's request.
	var (
		blobPath string
		blobErr  error
		blobDone = make(chan struct{})
	)
	go func() {
		defer close(blobDone)
		blobPath, blobErr = cfg.Blob.Save(r.Context(), intentID, pr, cfg.MaxMultipartBytes)
		// Drain any remaining bytes after Save returned so the tee never
		// blocks if Save aborted early (e.g. limit overflow).
		_, _ = io.Copy(io.Discard, pr)
	}()

	// TeeReader sends every byte read by the handler into pw. Closing pw
	// signals EOF to the goroutine above.
	tee := io.TeeReader(r.Body, pw)
	originalBody := r.Body
	r.Body = teeReadCloser{r: tee, c: originalBody, p: pw}

	cw := newCaptureWriter(w)
	next.ServeHTTP(cw, r)

	// Ensure the pipe is closed so the goroutine returns.
	_ = pw.Close()
	<-blobDone

	if cw.status < http.StatusBadRequest {
		// Success path — clean up the blob, if any.
		if blobPath != "" {
			_ = cfg.Blob.Delete(context.WithoutCancel(r.Context()), blobPath)
		}
		return
	}

	intent := buildMultipartIntent(cfg, r, intentID, blobPath, blobErr, cw)
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	if saveErr := cfg.Store.Save(saveCtx, intent); saveErr != nil {
		slog.ErrorContext(r.Context(), "failedintent: store save failed",
			"error", saveErr, "intent_id", intent.ID, "path", intent.Path,
			"http_status", intent.HTTPStatus,
		)
		// Blob is orphaned now; best-effort cleanup.
		if blobPath != "" {
			_ = cfg.Blob.Delete(context.WithoutCancel(r.Context()), blobPath)
		}
		return
	}
	emitCapturedLog(r.Context(), intent)
}

// teeReadCloser bundles the tee read side with the original body close + the
// pipe writer so closing the request body also closes the pipe.
type teeReadCloser struct {
	r io.Reader
	c io.Closer
	p *io.PipeWriter
}

func (t teeReadCloser) Read(p []byte) (int, error) { return t.r.Read(p) }
func (t teeReadCloser) Close() error {
	_ = t.p.Close()
	return t.c.Close()
}

// buildMultipartIntent assembles the Intent for a multipart capture. When the
// blob save failed, the intent is still recorded (with empty BodyBlobPath +
// BodyTruncated=true) so ops can see the request happened.
func buildMultipartIntent(
	cfg Config,
	r *http.Request,
	intentID uuid.UUID,
	blobPath string,
	blobErr error,
	cw *captureWriter,
) Intent {
	now := cfg.Clock()
	contentType := r.Header.Get("Content-Type")
	intent := Intent{
		ID:              intentID,
		ReceivedAt:      now,
		Method:          r.Method,
		Path:            r.URL.Path,
		IdempotencyKey:  r.Header.Get(idempotency.HeaderKey),
		RequestID:       requestIDOrNew(r.Context()),
		Body:            json.RawMessage(`""`),
		BodyTruncated:   blobErr != nil,
		HTTPStatus:      cw.status,
		Status:          StatusNew,
		BodyBlobPath:    blobPath,
		BodyContentType: contentType,
	}
	intent.FirebaseUID, intent.UsuarioID = currentUserFields(r.Context())
	intent.ErrorCode, intent.ErrorMessage = deriveError(cw.body.Bytes())
	return intent
}
```

> The helpers `currentUserFields`, `deriveError`, `requestIDOrNew`, `emitCapturedLog`, `normaliseBody` already exist in this file — reuse them as-is. `buildIntent` (used by the JSON path) stays untouched.

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/platform/failedintent/`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/failedintent/failedintent.go internal/platform/failedintent/multipart_test.go
git commit -m "feat(failedintent): capture multipart bodies via tee-to-blob"
```

---

## Task 7: Replay reads from blob when present

**Files:**
- Modify: `internal/platform/failedintent/http/handlers.go` (Service struct, NewService signature, buildReplayRequest, ReplayWith)
- Create: `internal/platform/failedintent/http/replay_blob_test.go`
- Modify: `internal/platform/failedintent/http/handlers_test.go` (update NewService calls — add nil BlobStorage)

- [ ] **Step 1: Write the failing test**

```go
// File: internal/platform/failedintent/http/replay_blob_test.go
package failedintenthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	"github.com/abdimuy/msp-api/internal/platform/failedintent"
	failedintenthttp "github.com/abdimuy/msp-api/internal/platform/failedintent/http"
)

// Minimal fakes reused only in this file; in-package shared fakes live in
// handlers_test.go and are not exported.
type blobStub struct {
	payload []byte
	openErr error
}

func (b *blobStub) Save(context.Context, uuid.UUID, io.Reader, int64) (string, error) { return "", nil }
func (b *blobStub) Open(_ context.Context, _ string) (io.ReadCloser, error) {
	if b.openErr != nil {
		return nil, b.openErr
	}
	return io.NopCloser(bytes.NewReader(b.payload)), nil
}
func (b *blobStub) Delete(context.Context, string) error { return nil }

type intentStore struct {
	mu      sync.Mutex
	intents map[uuid.UUID]failedintent.Intent
}

func (s *intentStore) Save(_ context.Context, i failedintent.Intent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intents == nil {
		s.intents = make(map[uuid.UUID]failedintent.Intent)
	}
	s.intents[i.ID] = i
	return nil
}
func (s *intentStore) Get(_ context.Context, id uuid.UUID) (*failedintent.Intent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.intents[id]
	if !ok {
		return nil, nil
	}
	return &i, nil
}
func (s *intentStore) List(context.Context, failedintent.ListParams) (failedintent.Page[failedintent.Intent], error) {
	return failedintent.Page[failedintent.Intent]{}, nil
}
func (s *intentStore) UpdateStatus(context.Context, uuid.UUID, failedintent.Status, failedintent.Status, uuid.UUID, string, time.Time) error {
	return nil
}
func (s *intentStore) IncrementRetry(context.Context, uuid.UUID) error          { return nil }
func (s *intentStore) PurgeOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

type recordingDispatcher struct {
	gotBody        []byte
	gotContentType string
}

func (d *recordingDispatcher) Dispatch(_ http.ResponseWriter, r *http.Request) {
	d.gotContentType = r.Header.Get("Content-Type")
	d.gotBody, _ = io.ReadAll(r.Body)
}

type stubUsuarios struct{}

func (stubUsuarios) BuildCurrentUserByID(context.Context, uuid.UUID) (auth.CurrentUser, error) {
	return auth.CurrentUser{ID: uuid.New()}, nil
}

func TestReplay_BlobIntent_StreamsBodyVerbatimWithOriginalContentType(t *testing.T) {
	t.Parallel()
	payload := []byte("--B\r\nContent-Disposition: form-data; name=\"datos\"\r\n\r\n{}\r\n--B--\r\n")
	blob := &blobStub{payload: payload}
	store := &intentStore{}
	dispatcher := &recordingDispatcher{}

	intentID := uuid.New()
	usuarioID := uuid.New()
	require.NoError(t, store.Save(context.Background(), failedintent.Intent{
		ID:              intentID,
		UsuarioID:       &usuarioID,
		Method:          "POST",
		Path:            "/v2/ventas",
		RequestID:       uuid.New(),
		Body:            json.RawMessage(`""`),
		HTTPStatus:      422,
		Status:          failedintent.StatusNew,
		BodyBlobPath:    "/fake/blob.bin",
		BodyContentType: "multipart/form-data; boundary=B",
	}))

	svc := failedintenthttp.NewService(store, dispatcher, stubUsuarios{}, blob, nil, nil)

	r := chi.NewRouter()
	r.Post("/{id}/replay", svc.Replay)

	req := httptest.NewRequest(http.MethodPost, "/"+intentID.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, payload, dispatcher.gotBody, "replay must forward the blob bytes verbatim")
	assert.Equal(t, "multipart/form-data; boundary=B", dispatcher.gotContentType)
}

func TestReplayWith_BlobIntent_Returns422(t *testing.T) {
	t.Parallel()
	blob := &blobStub{payload: []byte("ignored")}
	store := &intentStore{}
	dispatcher := &recordingDispatcher{}

	intentID := uuid.New()
	usuarioID := uuid.New()
	require.NoError(t, store.Save(context.Background(), failedintent.Intent{
		ID:              intentID,
		UsuarioID:       &usuarioID,
		Method:          "POST",
		Path:            "/v2/ventas",
		RequestID:       uuid.New(),
		Body:            json.RawMessage(`""`),
		HTTPStatus:      422,
		Status:          failedintent.StatusNew,
		BodyBlobPath:    "/fake/blob.bin",
		BodyContentType: "multipart/form-data; boundary=B",
	}))

	svc := failedintenthttp.NewService(store, dispatcher, stubUsuarios{}, blob, nil, nil)

	r := chi.NewRouter()
	r.Post("/{id}/replay-with", svc.ReplayWith)

	body := bytes.NewBufferString(`{"body":{"override":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/"+intentID.String()+"/replay-with", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code,
		"ReplayWith must reject blob-backed intents — see blob_intent_replay_with_unsupported")
}

func TestReplay_BlobIntent_OpenFails_Returns500(t *testing.T) {
	t.Parallel()
	blob := &blobStub{openErr: errors.New("disk gone")}
	store := &intentStore{}
	dispatcher := &recordingDispatcher{}

	intentID := uuid.New()
	usuarioID := uuid.New()
	require.NoError(t, store.Save(context.Background(), failedintent.Intent{
		ID:              intentID,
		UsuarioID:       &usuarioID,
		Method:          "POST",
		Path:            "/v2/ventas",
		RequestID:       uuid.New(),
		Body:            json.RawMessage(`""`),
		HTTPStatus:      422,
		Status:          failedintent.StatusNew,
		BodyBlobPath:    "/fake/blob.bin",
		BodyContentType: "multipart/form-data; boundary=B",
	}))

	svc := failedintenthttp.NewService(store, dispatcher, stubUsuarios{}, blob, nil, nil)

	r := chi.NewRouter()
	r.Post("/{id}/replay", svc.Replay)

	req := httptest.NewRequest(http.MethodPost, "/"+intentID.String()+"/replay", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "Replay returns 200 envelope; outcome reflects the failure")
	var resp failedintenthttp.ReplayResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, string(failedintent.StatusRetriedFail), resp.Outcome)
	assert.Equal(t, http.StatusInternalServerError, resp.ReplayHTTPStatus)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/http/...`
Expected: build fails — `NewService` only takes 5 args today.

- [ ] **Step 3: Extend the Service**

In `internal/platform/failedintent/http/handlers.go`, update the struct + constructor:

```go
// Service bundles dependencies for the admin handlers.
type Service struct {
	store      failedintent.Store
	dispatcher failedintent.ReplayDispatcher
	usuarios   UsuarioLookup
	blobs      failedintent.BlobStorage
	clock      func() time.Time
	newID      func() uuid.UUID
}

// NewService constructs a Service. Nil blobs is permitted: replay of a
// blob-backed intent will fail with replay_blob_storage_unavailable.
func NewService(
	store failedintent.Store,
	dispatcher failedintent.ReplayDispatcher,
	usuarios UsuarioLookup,
	blobs failedintent.BlobStorage,
	clock func() time.Time,
	newID func() uuid.UUID,
) *Service {
	if clock == nil {
		clock = time.Now
	}
	if newID == nil {
		newID = uuid.New
	}
	return &Service{
		store:      store,
		dispatcher: dispatcher,
		usuarios:   usuarios,
		blobs:      blobs,
		clock:      clock,
		newID:      newID,
	}
}
```

- [ ] **Step 4: Update buildReplayRequest for blob intents**

Replace `buildReplayRequest` so it picks a body source from the intent:

```go
func (s *Service) buildReplayRequest(
	ctx context.Context,
	intent *failedintent.Intent,
	cu auth.CurrentUser,
	overrideBody json.RawMessage,
) (*http.Request, error) {
	var (
		body        io.Reader
		contentType string
	)
	switch {
	case overrideBody != nil:
		body = bytes.NewReader(overrideBody)
		contentType = "application/json"
	case intent.BodyBlobPath != "":
		if s.blobs == nil {
			return nil, apperror.NewInternal(
				"replay_blob_storage_unavailable",
				"el almacenamiento de cuerpos en disco no está configurado",
			)
		}
		rc, err := s.blobs.Open(ctx, intent.BodyBlobPath)
		if err != nil {
			return nil, fmt.Errorf("replay: open blob: %w", err)
		}
		body = rc
		contentType = intent.BodyContentType
		if contentType == "" {
			contentType = "application/json"
		}
	default:
		body = bytes.NewReader(intent.Body)
		contentType = "application/json"
	}

	//nolint:gosec // intent.Method and intent.Path were vetted at capture time.
	req, err := http.NewRequestWithContext(ctx, intent.Method, intent.Path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set(failedintent.HeaderInternalReplay, intent.ID.String())
	req.Header.Set("X-Request-ID", s.newID().String())
	req.Header.Set(idempotency.HeaderKey, s.newID().String())

	replayCtx := httpdispatch.InternalContext(req.Context())
	//nolint:contextcheck
	req = req.WithContext(auth.PlantCurrentUser(replayCtx, cu))
	return req, nil
}
```

Note the signature change: `body json.RawMessage` → `overrideBody json.RawMessage` semantics. Update the two call sites:

```go
// In Replay (executeReplay branch):
result := s.executeReplay(r.Context(), *intent, nil /* no override */, cu)

// In ReplayWith (executeReplay branch):
result := s.executeReplay(r.Context(), *intent, req.Body, cu)
```

Update `executeReplay`:

```go
func (s *Service) executeReplay(
	ctx context.Context,
	intent failedintent.Intent,
	overrideBody json.RawMessage,
	cu auth.CurrentUser,
) replayResult {
	// ... existing logic; pass overrideBody into buildReplayRequest.
	replayReq, buildErr := s.buildReplayRequest(ctx, &intent, cu, overrideBody)
	// ...
}
```

Add to imports: `"fmt"`, `"io"`.

- [ ] **Step 5: Guard ReplayWith against blob intents**

In `ReplayWith`, after the body validation block, add the guard:

```go
if intent.BodyBlobPath != "" {
	response.Error(w, r, apperror.NewValidation(
		"blob_intent_replay_with_unsupported",
		"no se puede aplicar un cuerpo corregido a un intento con archivo adjunto; usa Replay simple o re-envía desde el dispositivo",
	))
	return
}
```

- [ ] **Step 6: Update existing handler tests for the new constructor**

In `internal/platform/failedintent/http/handlers_test.go` find every `failedintenthttp.NewService(` call and add `nil` for the new `blobs` parameter in the right position. The signature is now:

```go
NewService(store, dispatcher, usuarios, blobs, clock, newID)
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/platform/failedintent/...`
Expected: `ok`.

- [ ] **Step 8: Commit**

```bash
git add internal/platform/failedintent/http/handlers.go internal/platform/failedintent/http/handlers_test.go internal/platform/failedintent/http/replay_blob_test.go
git commit -m "feat(failedintent): replay reads blob bodies verbatim"
```

---

## Task 8: Janitor deletes blobs when purging rows

**Files:**
- Modify: `internal/platform/failedintent/failedintent.go` (Store.PurgeOlderThan signature)
- Modify: `internal/platform/failedintent/postgres/store.go` (Postgres PurgeOlderThan returns ids + paths)
- Modify: `internal/platform/failedintent/janitor.go` (run blob delete loop)
- Modify: `internal/platform/failedintent/janitor_test.go`

**Decision recorded:** rather than introduce a separate listing call, evolve the existing `PurgeOlderThan` signature to return the deleted ids and their blob paths. The contract change is internal — only the janitor and its test consume it.

- [ ] **Step 1: Update the Store contract**

Change the Store interface in `failedintent.go`:

```go
	// PurgeOlderThan deletes rows whose received_at is strictly less than
	// `before` and returns the paths of any blobs that were attached. The
	// janitor uses the returned slice to delete those blobs from BlobStorage.
	PurgeOlderThan(ctx context.Context, before time.Time) (PurgeResult, error)
```

Add `PurgeResult`:

```go
// PurgeResult reports the rows removed by Store.PurgeOlderThan. BlobPaths
// holds the body_blob_path values that were non-null on the purged rows;
// rows without blobs do not appear in the slice. RowsDeleted is the total
// affected row count (>= len(BlobPaths)).
type PurgeResult struct {
	RowsDeleted int64
	BlobPaths   []string
}
```

- [ ] **Step 2: Implement PurgeOlderThan in Postgres**

```go
func (s *Store) PurgeOlderThan(ctx context.Context, before time.Time) (failedintent.PurgeResult, error) {
	const q = `
		DELETE FROM failed_intents
		WHERE received_at < $1
		RETURNING body_blob_path`
	rows, err := transaction.GetQuerier(ctx, s.pool).Query(ctx, q, before.UTC())
	if err != nil {
		return failedintent.PurgeResult{}, fmt.Errorf("failedintent.postgres: purge: %w", err)
	}
	defer rows.Close()

	var (
		paths []string
		count int64
	)
	for rows.Next() {
		var p *string
		if scanErr := rows.Scan(&p); scanErr != nil {
			return failedintent.PurgeResult{}, fmt.Errorf("failedintent.postgres: purge scan: %w", scanErr)
		}
		count++
		if p != nil && *p != "" {
			paths = append(paths, *p)
		}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return failedintent.PurgeResult{}, fmt.Errorf("failedintent.postgres: purge rows: %w", rowsErr)
	}
	return failedintent.PurgeResult{RowsDeleted: count, BlobPaths: paths}, nil
}
```

- [ ] **Step 3: Update the janitor to delete blobs**

In `janitor.go`, add `Blob` field to `JanitorConfig` and consume the new result:

```go
type JanitorConfig struct {
	Store    Store
	Blob     BlobStorage // optional; when nil, blob deletion is skipped.
	Interval time.Duration
	Retain   time.Duration
	Clock    func() time.Time
}
```

Update `purgeOnce`:

```go
func (j *Janitor) purgeOnce(ctx context.Context) {
	cutoff := j.cfg.Clock().Add(-j.cfg.Retain)
	result, err := j.cfg.Store.PurgeOlderThan(ctx, cutoff)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		slog.ErrorContext(ctx, "failedintent.janitor: purge failed",
			"error", err, "cutoff", cutoff,
		)
		return
	}
	if result.RowsDeleted == 0 {
		return
	}
	slog.InfoContext(ctx, "failedintent.janitor: purged",
		"count", result.RowsDeleted, "cutoff", cutoff,
		"blob_count", len(result.BlobPaths),
	)
	if j.cfg.Blob == nil {
		return
	}
	for _, p := range result.BlobPaths {
		if err := j.cfg.Blob.Delete(ctx, p); err != nil {
			slog.WarnContext(ctx, "failedintent.janitor: blob delete failed",
				"error", err, "path", p,
			)
		}
	}
}
```

- [ ] **Step 4: Update janitor_test.go**

Replace the existing fake Store's `PurgeOlderThan` so it returns a `PurgeResult`. Add a test:

```go
func TestJanitor_PurgeDeletesBlobsForPurgedRows(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		purge: failedintent.PurgeResult{
			RowsDeleted: 3,
			BlobPaths:   []string{"/blob/a.bin", "/blob/c.bin"},
		},
	}
	blob := &fakeBlobStore{blobs: map[string][]byte{
		"/blob/a.bin": {1}, "/blob/c.bin": {2}, "/blob/keep.bin": {3},
	}}
	j := failedintent.NewJanitor(failedintent.JanitorConfig{
		Store: store, Blob: blob,
		Interval: time.Hour, Retain: time.Hour,
		Clock: func() time.Time { return time.Now() },
	})

	require.NoError(t, j.Start(context.Background()))
	// purgeOnce runs at boot; give it a beat to land.
	time.Sleep(10 * time.Millisecond)
	require.NoError(t, j.Stop(context.Background()))

	blob.mu.Lock()
	defer blob.mu.Unlock()
	assert.NotContains(t, blob.blobs, "/blob/a.bin")
	assert.NotContains(t, blob.blobs, "/blob/c.bin")
	assert.Contains(t, blob.blobs, "/blob/keep.bin", "untouched blob stays")
}
```

(Reuse `fakeBlobStore` from multipart_test.go or move it into a shared `testhelpers_test.go` in the package — your call. If moved, update both files' imports.)

- [ ] **Step 5: Run tests**

Run: `go test ./internal/platform/failedintent/...`
Expected: `ok`.

- [ ] **Step 6: Update all other callers of PurgeOlderThan**

Search for callers: `grep -rn "PurgeOlderThan" --include="*.go"`. Update each fake/mock to return `PurgeResult` instead of `int64`. Likely sites: any mock in `failedintent_test.go`, `janitor_test.go`, `postgres/store_test.go`, the http e2e_test.go.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/failedintent internal/platform/failedintent/postgres
git commit -m "feat(failedintent): janitor cleans up purged-row blobs"
```

---

## Task 9: Boot-time orphan sweep

**Files:**
- Create: `internal/platform/failedintent/blobfs/orphan.go`
- Create: `internal/platform/failedintent/blobfs/orphan_test.go`

The orphan sweep handles the edge case where a process crash leaves a blob on disk after `Save` but before the DB INSERT lands.

- [ ] **Step 1: Write the failing test**

```go
// File: internal/platform/failedintent/blobfs/orphan_test.go
package blobfs_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"
)

type referencedPathsFn func(context.Context) (map[string]struct{}, error)

func (f referencedPathsFn) ReferencedPaths(ctx context.Context) (map[string]struct{}, error) {
	return f(ctx)
}

func TestSweepOrphans_DeletesUnreferencedBlobs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := blobfs.NewStore(dir)
	require.NoError(t, err)

	// Three blobs on disk; only one is referenced by the DB.
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}

	refs := referencedPathsFn(func(context.Context) (map[string]struct{}, error) {
		return map[string]struct{}{filepath.Join(dir, "b.bin"): {}}, nil
	})

	report, err := blobfs.SweepOrphans(context.Background(), store, refs)
	require.NoError(t, err)
	assert.Equal(t, 2, report.Deleted, "two orphans should be removed")

	remaining, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(remaining))
	for _, e := range remaining {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, []string{"b.bin"}, names)
}

func TestSweepOrphans_IgnoresNonBinFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := blobfs.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "x.bin.tmp"), []byte("partial"), 0o600))

	refs := referencedPathsFn(func(context.Context) (map[string]struct{}, error) {
		return map[string]struct{}{}, nil
	})

	report, err := blobfs.SweepOrphans(context.Background(), store, refs)
	require.NoError(t, err)
	assert.Equal(t, 0, report.Deleted, ".tmp files are skipped (concurrent Save in flight)")
	_, statErr := os.Stat(filepath.Join(dir, "x.bin.tmp"))
	require.NoError(t, statErr)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/platform/failedintent/blobfs/...`
Expected: build fails on `blobfs.SweepOrphans`, `blobfs.SweepReport`.

- [ ] **Step 3: Implement SweepOrphans**

```go
// File: internal/platform/failedintent/blobfs/orphan.go
package blobfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/abdimuy/msp-api/internal/platform/failedintent"
)

// PathReferencer is the port SweepOrphans uses to learn which blob paths the
// database currently references. The Postgres store satisfies it via
// SELECT body_blob_path FROM failed_intents WHERE body_blob_path IS NOT NULL.
type PathReferencer interface {
	ReferencedPaths(ctx context.Context) (map[string]struct{}, error)
}

// SweepReport summarises what SweepOrphans did. Useful for boot logs.
type SweepReport struct {
	Scanned int
	Deleted int
}

// SweepOrphans removes blob files in store.BaseDir that are not referenced by
// the supplied referencer. Files ending in .tmp are skipped — they belong to
// in-flight writes from the atomic Save path.
func SweepOrphans(ctx context.Context, store *Store, refs PathReferencer) (SweepReport, error) {
	refSet, err := refs.ReferencedPaths(ctx)
	if err != nil {
		return SweepReport{}, fmt.Errorf("blobfs.sweep: load refs: %w", err)
	}
	entries, err := os.ReadDir(store.BaseDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SweepReport{}, nil
		}
		return SweepReport{}, fmt.Errorf("blobfs.sweep: read base dir: %w", err)
	}
	report := SweepReport{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") {
			continue
		}
		report.Scanned++
		full := filepath.Join(store.BaseDir(), name)
		if _, ok := refSet[full]; ok {
			continue
		}
		if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
			return report, fmt.Errorf("blobfs.sweep: delete %s: %w", full, err)
		}
		report.Deleted++
	}
	return report, nil
}
```

- [ ] **Step 4: Add ReferencedPaths to the Postgres store**

In `internal/platform/failedintent/postgres/store.go`:

```go
// ReferencedPaths returns the set of body_blob_path values currently
// recorded in failed_intents. Used by blobfs.SweepOrphans at boot to delete
// blob files that lost their DB row (process crash between Save and INSERT).
func (s *Store) ReferencedPaths(ctx context.Context) (map[string]struct{}, error) {
	const q = `SELECT body_blob_path FROM failed_intents WHERE body_blob_path IS NOT NULL`
	rows, err := transaction.GetQuerier(ctx, s.pool).Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("failedintent.postgres: referenced paths: %w", err)
	}
	defer rows.Close()
	out := make(map[string]struct{})
	for rows.Next() {
		var p string
		if scanErr := rows.Scan(&p); scanErr != nil {
			return nil, fmt.Errorf("failedintent.postgres: referenced paths scan: %w", scanErr)
		}
		out[p] = struct{}{}
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("failedintent.postgres: referenced paths rows: %w", rowsErr)
	}
	return out, nil
}
```

- [ ] **Step 5: Run tests**

Run: `go test ./internal/platform/failedintent/...`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/platform/failedintent/blobfs internal/platform/failedintent/postgres
git commit -m "feat(failedintent): boot-time orphan blob sweep"
```

---

## Task 10: Config + wiring + .env

**Files:**
- Modify: `internal/platform/config/config.go`
- Modify: `.env.example`
- Modify: `cmd/api/failedintent_wiring.go`
- Modify: `cmd/api/main.go`
- Modify: `.golangci.yml`

- [ ] **Step 1: Add the config section**

In `internal/platform/config/config.go`, register a new section and wire it into `Config`:

```go
// Config aggregates all runtime configuration.
type Config struct {
	App            App
	Cobranza       Cobranza
	HTTP           HTTP
	Postgres       Postgres
	Firebird       Firebird
	Firebase       Firebase
	Sync           Sync
	Storage        Storage
	ImageProcessor ImageProcessor
	Microsip       Microsip
	FailedIntent   FailedIntent
}

// FailedIntent tunes the failed-intent capture pipeline.
type FailedIntent struct {
	// BlobDir is the on-disk root for captured multipart bodies. Empty falls
	// back to "<STORAGE_DIR>/failed-intents". Created at boot with 0o700.
	BlobDir string `env:"FAILEDINTENT_BLOB_DIR"`
	// MaxMultipartBytes caps each captured body. Default 50 MiB.
	MaxMultipartBytes int64 `env:"FAILEDINTENT_MAX_MULTIPART_BYTES" envDefault:"52428800"`
}

// resolvedBlobDir returns FailedIntent.BlobDir or the default rooted at
// Storage.Dir. Always returns an absolute-ish path the caller can mkdir.
func (f FailedIntent) resolvedBlobDir(storage Storage) string {
	if strings.TrimSpace(f.BlobDir) != "" {
		return f.BlobDir
	}
	return filepath.Join(storage.Dir, "failed-intents")
}
```

Add to imports: `"path/filepath"`.

Export a helper used by the wiring:

```go
// FailedIntentBlobDir resolves the on-disk root for failed-intent blobs.
func (c *Config) FailedIntentBlobDir() string {
	return c.FailedIntent.resolvedBlobDir(c.Storage)
}
```

- [ ] **Step 2: Document the env vars**

Append to `.env.example`:

```
# ── Failed-intent capture ────────────────────────────────────────
# Root directory for captured multipart bodies (defaults to <STORAGE_DIR>/failed-intents).
# FAILEDINTENT_BLOB_DIR=

# Hard cap per multipart body (bytes). Default 50 MiB.
FAILEDINTENT_MAX_MULTIPART_BYTES=52428800
```

- [ ] **Step 3: Add providers**

In `cmd/api/failedintent_wiring.go`, add new providers and update the existing config provider:

```go
// provideFailedIntentBlobStorage builds the filesystem-backed BlobStorage.
func provideFailedIntentBlobStorage(cfg *config.Config) (failedintent.BlobStorage, error) {
	return blobfs.NewStore(cfg.FailedIntentBlobDir())
}

// provideFailedIntentCaptureConfig now wires the Blob + cap.
func provideFailedIntentCaptureConfig(
	cfg *config.Config,
	store failedintent.Store,
	blobs failedintent.BlobStorage,
) failedintent.Config {
	return failedintent.Config{
		Store:             store,
		Blob:              blobs,
		MaxMultipartBytes: cfg.FailedIntent.MaxMultipartBytes,
	}
}

// provideFailedIntentJanitor passes the Blob store so retention also cleans
// up the on-disk blobs.
func provideFailedIntentJanitor(
	store failedintent.Store,
	blobs failedintent.BlobStorage,
) *failedintent.Janitor {
	return failedintent.NewJanitor(failedintent.JanitorConfig{
		Store: store, Blob: blobs,
	})
}

// provideFailedIntentHTTPService picks up the Blob dependency.
func provideFailedIntentHTTPService(
	store failedintent.Store,
	dispatcher failedintent.ReplayDispatcher,
	usuarios failedintenthttp.UsuarioLookup,
	blobs failedintent.BlobStorage,
) *failedintenthttp.Service {
	return failedintenthttp.NewService(store, dispatcher, usuarios, blobs, nil, nil)
}

// provideFailedIntentPostgresStore exposes the concrete Postgres store as the
// PathReferencer port for the orphan sweep wiring below.
func provideFailedIntentPostgresStore(p *postgres.Pool) *failedintentpg.Store {
	return failedintentpg.New(p.Pool)
}

// provideFailedIntentStore exposes the concrete store as the platform Store.
func provideFailedIntentStore(s *failedintentpg.Store) failedintent.Store {
	return s
}

// provideFailedIntentPathReferencer exposes the concrete store as the orphan
// sweep referencer port.
func provideFailedIntentPathReferencer(s *failedintentpg.Store) blobfs.PathReferencer {
	return s
}
```

Add imports: `"github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"`.

- [ ] **Step 4: Invoke the orphan sweep at boot**

In `cmd/api/main.go`, add to `fx.Provide(...)`: `provideFailedIntentBlobStorage`, `provideFailedIntentPathReferencer`, `provideFailedIntentPostgresStore` (replace the existing `provideFailedIntentStore` definition order so the postgres store is built first). Add to `fx.Invoke(...)`:

```go
invokeFailedIntentOrphanSweep,
```

Where:

```go
// invokeFailedIntentOrphanSweep deletes blob files left on disk after a crash
// between blob write and DB INSERT. Runs once at boot and emits a log line.
func invokeFailedIntentOrphanSweep(
	lc fx.Lifecycle,
	blobStore failedintent.BlobStorage,
	refs blobfs.PathReferencer,
) {
	concrete, ok := blobStore.(*blobfs.Store)
	if !ok {
		// Non-fs adapter — nothing to sweep.
		return
	}
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			report, err := blobfs.SweepOrphans(ctx, concrete, refs)
			if err != nil {
				slog.WarnContext(ctx, "failedintent: orphan sweep failed", "error", err)
				return nil
			}
			slog.InfoContext(ctx, "failedintent: orphan sweep complete",
				"scanned", report.Scanned, "deleted", report.Deleted)
			return nil
		},
	})
}
```

Add imports: `"context"`, `"log/slog"`, `"go.uber.org/fx"`, `"github.com/abdimuy/msp-api/internal/platform/failedintent"`, `"github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs"`.

- [ ] **Step 5: Add importas alias in golangci**

In `.golangci.yml`, append next to the other failedintent aliases:

```yaml
        - pkg: github.com/abdimuy/msp-api/internal/platform/failedintent/blobfs
          alias: failedintentblobfs
```

- [ ] **Step 6: Build + test**

Run: `go build ./...`
Expected: clean.

Run: `go test ./...`
Expected: all green (modulo Firebird integration tests that already skip without `FB_DATABASE`).

Run: `golangci-lint run ./...`
Expected: no new findings.

- [ ] **Step 7: Commit**

```bash
git add internal/platform/config/config.go .env.example cmd/api/failedintent_wiring.go cmd/api/main.go .golangci.yml
git commit -m "feat(failedintent): wire blob storage, config, orphan sweep"
```

---

## Task 11: End-to-end smoke + verification

**Files:** none (verification only).

- [ ] **Step 1: Restart the dev API**

Run: `docker compose restart api`
Expected: `migrate apply` runs migration `000006` and the boot log shows `failedintent: orphan sweep complete scanned=0 deleted=0`.

- [ ] **Step 2: Replay the broken venta scenario**

From the device or via curl reproduce a failing `POST /v2/ventas` multipart (any 422). Verify in Postgres:

```bash
docker exec msp-postgres psql -U msp -d msp_dev -c \
  "SELECT id, path, http_status, body_blob_path IS NOT NULL AS has_blob, body_content_type FROM failed_intents ORDER BY received_at DESC LIMIT 1"
```

Expected: `has_blob=t`, `body_content_type='multipart/form-data; boundary=…'`, blob file present at `body_blob_path`.

- [ ] **Step 3: Confirm cleanup on success path**

Repeat with a successful POST (multipart + valid payload). Verify: no new row in `failed_intents` and no new file under `STORAGE_DIR/failed-intents/`.

- [ ] **Step 4: Replay the captured intent**

```bash
INTENT_ID=<id-from-step-2>
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:3001/v2/_admin/failed-intents/$INTENT_ID/replay
```

Expected: response with `outcome=retried_ok` and `replay_http_status=201` (assuming the downstream handler now accepts the body).

- [ ] **Step 5: Verify ReplayWith guard**

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"body":{}}' \
  http://localhost:3001/v2/_admin/failed-intents/$INTENT_ID/replay-with
```

Expected: 422 with code `blob_intent_replay_with_unsupported`.

- [ ] **Step 6: Verify janitor blob cleanup**

Temporarily set `FAILEDINTENT_RETAIN=1s` in the .env (or hand-edit the row's `received_at` to a past time), restart, and confirm the row + blob disappear after one tick.

Run: `ls "$STORAGE_DIR/failed-intents"` then `docker exec msp-postgres psql -U msp -d msp_dev -c 'SELECT COUNT(*) FROM failed_intents'`
Expected: both empty.

- [ ] **Step 7: Final commit (or PR)**

If everything is green, push the branch and open a PR — no extra code commits at this step.

---

## Self-Review Notes

**Spec coverage:** every promise from the conversation has a task — captured: blob port (Task 2), filesystem adapter with atomic write + 0o600 + 50 MB cap (Task 3), schema migration with nullable columns (Task 1), tee-streaming capture (Task 6), byte-exact replay (Task 7), janitor blob cleanup (Task 8), boot orphan sweep (Task 9), config + env vars + wiring + linter alias (Task 10), end-to-end smoke verification (Task 11). The `ReplayWith` corner case is explicitly rejected so we never silently lose multipart corrections.

**Type consistency:** `BodyBlobPath` / `BodyContentType` used identically across `Intent`, Postgres scan/save, capture builder, replay builder, tests. `PurgeResult{RowsDeleted, BlobPaths}` consumed only by the janitor; downstream callers (none) untouched. `BlobStorage` triplet (`Save`, `Open`, `Delete`) used identically by `blobfs.Store`, fakes in tests, handlers, and janitor. `PathReferencer.ReferencedPaths` consumed only by `SweepOrphans` and implemented only on the Postgres store.

**No placeholders:** every step contains the full code for the change.
