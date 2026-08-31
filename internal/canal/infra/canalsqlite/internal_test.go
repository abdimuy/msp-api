// White-box tests: same package as the production code, so they can reach
// the unexported *sql.DB to verify raw column content that
// outbound.BuzonRepo has no method to read back (there is no lookup-by-id
// for a row that has already left EstadoReenvioPendiente — see
// RecibirMensaje's doc comment in internal/canal/app/service.go for why
// that surface is deliberately absent). Everything reachable through the
// port itself belongs in the black-box repo_test.go instead.
package canalsqlite

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/platform/apperror"
)

// TestMarcarFallido_PersistsMotivoFalloVerbatim proves MarcarFallido writes
// the exact reason given, not merely the estado transition — a repo that
// flipped estado but dropped or truncated motivo would still pass every
// black-box assertion in repo_test.go.
func TestMarcarFallido_PersistsMotivoFalloVerbatim(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	ctx := context.Background()

	recibidoEn := time.Date(2026, 8, 31, 10, 30, 0, 0, time.UTC)
	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         "wamid-motivo-verbatim",
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "text",
		Contenido:     "hola",
		TimestampMeta: recibidoEn,
		RecibidoEn:    recibidoEn,
	})
	require.NoError(t, err)
	inserted, err := repo.Guardar(ctx, m)
	require.NoError(t, err)
	require.True(t, inserted)

	const motivo = "el servidor de la tienda respondió 503 al reenviar"
	markedAt := recibidoEn.Add(5 * time.Minute)
	require.NoError(t, repo.MarcarFallido(ctx, m.ID(), motivo, markedAt))

	var gotMotivo, gotEstado, gotUpdatedAt string
	err = repo.db.QueryRowContext(ctx,
		"SELECT motivo_fallo, estado, updated_at FROM mensajes_entrantes WHERE id = ?",
		m.ID().String(),
	).Scan(&gotMotivo, &gotEstado, &gotUpdatedAt)
	require.NoError(t, err)

	assert.Equal(t, motivo, gotMotivo, "motivo_fallo must be persisted verbatim")
	assert.Equal(t, domain.EstadoReenvioFallido.String(), gotEstado)
	assert.Equal(t, formatUTC(markedAt), gotUpdatedAt, "updated_at must be the now the caller passed in, not time.Now()")
}

// TestMarcarReenviado_PersistsGivenNowNotWallClock proves MarcarReenviado
// writes the exact now argument to updated_at instead of calling
// time.Now() internally — required by the brief's ambiguity resolution #4.
func TestMarcarReenviado_PersistsGivenNowNotWallClock(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	ctx := context.Background()

	recibidoEn := time.Date(2026, 8, 31, 10, 30, 0, 0, time.UTC)
	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         "wamid-now-check",
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "text",
		Contenido:     "hola",
		TimestampMeta: recibidoEn,
		RecibidoEn:    recibidoEn,
	})
	require.NoError(t, err)
	inserted, err := repo.Guardar(ctx, m)
	require.NoError(t, err)
	require.True(t, inserted)

	// A deliberately implausible timestamp: if MarcarReenviado ever called
	// time.Now() instead of using the parameter, this would never match.
	explicitNow := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.MarcarReenviado(ctx, m.ID(), explicitNow))

	var gotUpdatedAt string
	err = repo.db.QueryRowContext(ctx,
		"SELECT updated_at FROM mensajes_entrantes WHERE id = ?", m.ID().String(),
	).Scan(&gotUpdatedAt)
	require.NoError(t, err)
	assert.Equal(t, formatUTC(explicitNow), gotUpdatedAt)
}

// TestFormatUTC_TrimsToUTCEvenFromANonUTCInput is a narrow unit test of the
// helper the timestamp round-trip test in repo_test.go depends on:
// formatUTC must normalize to UTC before formatting, not merely format
// whatever zone it was given.
func TestFormatUTC_TrimsToUTCEvenFromANonUTCInput(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	in := time.Date(2026, 8, 31, 4, 30, 0, 987654321, loc)

	got := formatUTC(in)

	assert.Equal(t, "2026-08-31T10:30:00.987654321Z", got)
}

// TestParseUTC_RoundTripsFormatUTCExactly is a narrow unit test of the pair
// formatUTC/parseUTC in isolation, complementing the full Guardar ->
// ListarPendientes round trip in repo_test.go.
func TestParseUTC_RoundTripsFormatUTCExactly(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	original := time.Date(2026, 8, 31, 4, 30, 0, 987654321, loc)

	written := formatUTC(original)
	got, err := parseUTC("recibido_en", written)
	require.NoError(t, err)

	assert.True(t, got.Equal(original))
	assert.Equal(t, original.UnixNano(), got.UnixNano())
	assert.Equal(t, time.UTC, got.Location())
}

// ── mapError ─────────────────────────────────────────────────────────────
//
// Guardar's own use of ON CONFLICT DO NOTHING means the happy paths never
// hand mapError a real constraint violation, so these tests deliberately
// go around the port — via raw SQL against the repo's own *sql.DB, or by
// calling mapError directly — to prove the classification itself is
// correct, not just that it compiles.

func TestMapError_Nil(t *testing.T) {
	t.Parallel()
	assert.NoError(t, mapError(nil))
}

func TestMapError_ContextDeadlineExceededIsServiceUnavailable(t *testing.T) {
	t.Parallel()
	err := mapError(context.DeadlineExceeded)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindServiceUnavailable, appErr.Kind)
	assert.Equal(t, "canal_mailbox_timeout", appErr.Code)
}

func TestMapError_ContextCanceledIsServiceUnavailable(t *testing.T) {
	t.Parallel()
	err := mapError(context.Canceled)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindServiceUnavailable, appErr.Kind)
	assert.Equal(t, "canal_mailbox_timeout", appErr.Code)
}

func TestMapError_EOFIsConnectionLost(t *testing.T) {
	t.Parallel()
	err := mapError(io.EOF)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
	assert.Equal(t, "canal_mailbox_connection_lost", appErr.Code)
}

func TestMapError_UnrecognizedErrorPassesThroughUnchanged(t *testing.T) {
	t.Parallel()
	original := errors.New("some unrelated failure")
	assert.Same(t, original, mapError(original))
}

// TestMapError_RealUniqueConstraintViolationIsConflict inserts two rows
// sharing a wamid THROUGH RAW SQL (bypassing Guardar's own
// ON CONFLICT DO NOTHING, which would just swallow it), so the driver
// itself raises SQLITE_CONSTRAINT_UNIQUE — proving mapError classifies a
// real driver error, not a hand-built stand-in.
func TestMapError_RealUniqueConstraintViolationIsConflict(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	ctx := context.Background()

	const insertRaw = `
INSERT INTO mensajes_entrantes (
	id, wamid, remitente, phone_number_id, tipo, contenido,
	timestamp_meta, recibido_en, estado, motivo_fallo, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []any{
		"11111111-1111-1111-1111-111111111111", "wamid-collide", "5219981234567",
		"1", "text", "hola", "2026-08-31T10:30:00Z", "2026-08-31T10:30:00Z",
		"pendiente", "", "2026-08-31T10:30:00Z", "2026-08-31T10:30:00Z",
	}
	_, err = repo.db.ExecContext(ctx, insertRaw, args...)
	require.NoError(t, err)

	args[0] = "22222222-2222-2222-2222-222222222222" // distinct id, same wamid.
	_, rawErr := repo.db.ExecContext(ctx, insertRaw, args...)
	require.Error(t, rawErr)

	mapped := mapError(rawErr)
	appErr, ok := apperror.As(mapped)
	require.True(t, ok, "expected an *apperror.Error, got %T: %v", mapped, mapped)
	assert.Equal(t, apperror.KindConflict, appErr.Kind)
	assert.Equal(t, "canal_mailbox_duplicate", appErr.Code)
}

// TestMapError_RealNotNullViolationIsValidation triggers a genuine
// SQLITE_CONSTRAINT_NOTNULL by omitting a NOT NULL column, landing in
// mapError's generic-constraint branch (distinct from the unique-violation
// one above).
func TestMapError_RealNotNullViolationIsValidation(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	_, rawErr := repo.db.ExecContext(context.Background(),
		`INSERT INTO mensajes_entrantes (id) VALUES ('only-id-no-other-columns')`)
	require.Error(t, rawErr)

	mapped := mapError(rawErr)
	appErr, ok := apperror.As(mapped)
	require.True(t, ok, "expected an *apperror.Error, got %T: %v", mapped, mapped)
	assert.Equal(t, apperror.KindValidation, appErr.Kind)
	assert.Equal(t, "canal_mailbox_constraint_violation", appErr.Code)
}

// TestMapError_RealUnclassifiedDriverErrorFallsToDefault triggers a plain
// SQLITE_ERROR (querying a table that does not exist), landing in
// mapError's catch-all default branch.
func TestMapError_RealUnclassifiedDriverErrorFallsToDefault(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	_, rawErr := repo.db.ExecContext(context.Background(), `SELECT * FROM tabla_que_no_existe`)
	require.Error(t, rawErr)

	mapped := mapError(rawErr)
	appErr, ok := apperror.As(mapped)
	require.True(t, ok, "expected an *apperror.Error, got %T: %v", mapped, mapped)
	assert.Equal(t, apperror.KindInternal, appErr.Kind)
	assert.Equal(t, "canal_mailbox_error", appErr.Code)
}

// ── constructor failure paths ───────────────────────────────────────────

// TestNew_ClosedDBReturnsError proves New routes its schema-creation
// failure through mapError rather than swallowing it — sql.ErrConnDone
// itself is not a *sqlite.Error, so mapError's own documented behaviour
// (mirroring firebird.MapError) is to pass it through unchanged, exactly
// like it does for any other unrecognized error.
func TestNew_ClosedDBReturnsError(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = New(db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

// TestOpen_UnwritableParentReturnsPathNamingError proves Open fails loudly
// and names the path when its parent directory cannot be created (here:
// permission denied creating a directory under /, which no test process
// may write to) — the papercut this guards against is a silent or
// unhelpful failure on a clean VPS boot, not a specific error taxonomy.
//
// This used to assert the failure came back through mapError as an
// apperror (Open just handed path straight to sql.Open, so "file cannot be
// created" was a SQLite driver error). Now Open calls os.MkdirAll first,
// and that call is what fails here — a boot-time directory-creation
// error, not a SQLite driver error, so it is wrapped directly (fmt.Errorf
// %w) rather than routed through mapError, mirroring how module.go's
// provideBuzonRepo already wraps Open's own error for the same reason: a
// fatal fx-graph-construction failure, never surfaced over HTTP, has no
// need for apperror.Kind classification.
func TestOpen_UnwritableParentReturnsPathNamingError(t *testing.T) {
	t.Parallel()
	_, err := Open("/this/directory/does/not/exist/mailbox.db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/this/directory/does/not/exist",
		"the error must name the path that could not be created, for an operator reading a boot failure")
}

// ── row-mapper failure paths ────────────────────────────────────────────
//
// These insert deliberately corrupted rows via raw SQL — data no domain
// constructor would ever produce — to exercise scanMensaje/assembleMensaje's
// error branches, which the happy-path black-box tests never reach because
// everything they insert went through domain.NewMensajeEntrante first.

// insertRawPendiente inserts one row bypassing domain validation entirely,
// so the test can corrupt exactly one column while keeping the rest valid.
func insertRawPendiente(t *testing.T, repo *Repo, id, recibidoEn string) {
	t.Helper()
	const insertRaw = `
INSERT INTO mensajes_entrantes (
	id, wamid, remitente, phone_number_id, tipo, contenido,
	timestamp_meta, recibido_en, estado, motivo_fallo, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := repo.db.ExecContext(context.Background(), insertRaw,
		id, "wamid-raw-"+id, "5219981234567", "1", "text", "hola",
		"2026-08-31T10:30:00Z", recibidoEn, "pendiente", "",
		"2026-08-31T10:30:00Z", "2026-08-31T10:30:00Z",
	)
	require.NoError(t, err)
}

func TestListarPendientes_CorruptIDColumnReturnsError(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	insertRawPendiente(t, repo, "not-a-valid-uuid", "2026-08-31T10:30:00Z")

	_, err = repo.ListarPendientes(context.Background(), 10)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "canal_mailbox_uuid_invalid", appErr.Code)
}

func TestListarPendientes_CorruptTimestampColumnReturnsError(t *testing.T) {
	t.Parallel()
	repo, err := Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })

	insertRawPendiente(t, repo, "33333333-3333-3333-3333-333333333333", "not-a-timestamp")

	_, err = repo.ListarPendientes(context.Background(), 10)
	require.Error(t, err)
	appErr, ok := apperror.As(err)
	require.True(t, ok)
	assert.Equal(t, "canal_mailbox_timestamp_invalid", appErr.Code)
}
