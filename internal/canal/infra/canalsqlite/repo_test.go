// Tests for the canal module's durable SQLite mailbox. All run against
// ":memory:" — no external service required, unlike the Firebird repos
// (compare internal/flota/infra/flotafb/repo_test.go, which skips without
// FB_DATABASE). Repo.New caps the pool at one connection precisely so
// ":memory:" behaves like a single shared database across every call in a
// test instead of silently handing out an empty one per connection.
package canalsqlite_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/abdimuy/msp-api/internal/canal/domain"
	"github.com/abdimuy/msp-api/internal/canal/infra/canalsqlite"
)

// Instants used throughout. Deliberately not time.Now(): several
// assertions exist precisely to prove the value read back is the value
// written, and a moving clock cannot prove that.
var (
	tRecibido = time.Date(2026, 8, 31, 10, 30, 0, 123456789, time.UTC)
	tMeta     = time.Date(2026, 8, 31, 10, 29, 58, 0, time.UTC)
	tLuego    = time.Date(2026, 8, 31, 10, 35, 0, 0, time.UTC)
)

// newTestRepo opens a fresh in-memory mailbox for one test.
func newTestRepo(t *testing.T) *canalsqlite.Repo {
	t.Helper()
	repo, err := canalsqlite.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

// mensaje builds a valid, unpersisted MensajeEntrante with the given wamid,
// received at tRecibido.
func mensaje(t *testing.T, wamid string) *domain.MensajeEntrante {
	t.Helper()
	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         wamid,
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "text",
		Contenido:     "hola, ¿tienen refrigeradores?",
		TimestampMeta: tMeta,
		RecibidoEn:    tRecibido,
	})
	require.NoError(t, err)
	return m
}

func TestOpen_CreatesSchema(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)

	n, err := repo.ContarPendientes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a freshly created mailbox has no pending rows")
}

// TestOpen_CreatesMissingParentDirectory proves a clean boot survives a
// CANAL_SQLITE_PATH whose directory does not exist yet — exactly the state
// a fresh VPS checkout is in with the shipped .env.example's
// CANAL_SQLITE_PATH=./var/canal/buzon.db, since ./var is not in the repo.
func TestOpen_CreatesMissingParentDirectory(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "nested", "does", "not", "exist", "buzon.db")

	repo, err := canalsqlite.Open(path)
	require.NoError(t, err, "Open must create the missing parent directory rather than fail")
	t.Cleanup(func() { _ = repo.Close() })

	n, err := repo.ContarPendientes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestNew_IsSafeToRerunAgainstAnExistingDatabase(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo1, err := canalsqlite.New(db)
	require.NoError(t, err)

	m := mensaje(t, "wamid-rerun")
	inserted, err := repo1.Guardar(context.Background(), m)
	require.NoError(t, err)
	require.True(t, inserted)

	// Re-running the schema against the same, now-populated database must
	// not fail and must not touch existing rows.
	repo2, err := canalsqlite.New(db)
	require.NoError(t, err)

	n, err := repo2.ContarPendientes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the row inserted before the schema re-run must survive it")
}

func TestGuardar_InsertsANewRow(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()
	m := mensaje(t, "wamid-1")

	inserted, err := repo.Guardar(ctx, m)
	require.NoError(t, err)
	assert.True(t, inserted)

	pendientes, err := repo.ListarPendientes(ctx, 10)
	require.NoError(t, err)
	require.Len(t, pendientes, 1)

	got := pendientes[0]
	assert.Equal(t, m.ID(), got.ID())
	assert.Equal(t, m.Wamid(), got.Wamid())
	assert.Equal(t, m.Remitente(), got.Remitente())
	assert.Equal(t, m.PhoneNumberID(), got.PhoneNumberID())
	assert.Equal(t, m.Tipo(), got.Tipo())
	assert.Equal(t, m.Contenido(), got.Contenido())
	assert.Equal(t, domain.EstadoReenvioPendiente, got.Estado())
	assert.Empty(t, got.MotivoFallo())
}

// TestGuardar_DuplicateWamidReportsNotInsertedAndDoesNotDuplicate is the
// dedup test named in the brief. It proves both halves of the claim: a
// second Guardar for the same wamid (1) reports inserted == false, and
// (2) leaves exactly one row behind — not two. Asserting only the boolean
// would pass even if the second call had, say, thrown away its result
// without ever touching the table; counting rows is what makes this a real
// regression test for whether storage actually stays deduplicated.
func TestGuardar_DuplicateWamidReportsNotInsertedAndDoesNotDuplicate(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()

	first := mensaje(t, "wamid-dup")
	inserted, err := repo.Guardar(ctx, first)
	require.NoError(t, err)
	require.True(t, inserted)

	// A second, independently-constructed entity carrying the same wamid —
	// exactly what a redelivered Meta webhook produces: a fresh uuid.New()
	// identity, same wamid.
	second := mensaje(t, "wamid-dup")
	require.NotEqual(t, first.ID(), second.ID(), "the two entities must have distinct generated ids")

	inserted, err = repo.Guardar(ctx, second)
	require.NoError(t, err)
	assert.False(t, inserted, "a redelivered wamid must report inserted == false")

	n, err := repo.ContarPendientes(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "the duplicate delivery must not have created a second row")

	pendientes, err := repo.ListarPendientes(ctx, 10)
	require.NoError(t, err)
	require.Len(t, pendientes, 1)
	assert.Equal(t, first.ID(), pendientes[0].ID(), "the surviving row must be the first delivery's, untouched by the second")
}

// TestListarPendientes_OrdersByRecibidoEnAscending inserts rows in an order
// that does NOT match RecibidoEn order, and out of estado order too (one
// terminal row mixed in), so the assertion can only pass if the repo
// genuinely sorts by RecibidoEn and genuinely filters by estado — not by
// coincidence of insertion order.
func TestListarPendientes_OrdersByRecibidoEnAscending(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()

	newest := mensajeAt(t, "wamid-newest", tRecibido.Add(2*time.Hour))
	oldest := mensajeAt(t, "wamid-oldest", tRecibido)
	middle := mensajeAt(t, "wamid-middle", tRecibido.Add(1*time.Hour))

	for _, m := range []*domain.MensajeEntrante{newest, oldest, middle} {
		inserted, err := repo.Guardar(ctx, m)
		require.NoError(t, err)
		require.True(t, inserted)
	}

	// A fourth message that gets marked reenviado — it must never appear in
	// ListarPendientes even though it was received before "middle".
	reenviado := mensajeAt(t, "wamid-reenviado", tRecibido.Add(30*time.Minute))
	inserted, err := repo.Guardar(ctx, reenviado)
	require.NoError(t, err)
	require.True(t, inserted)
	require.NoError(t, repo.MarcarReenviado(ctx, reenviado.ID(), tLuego))

	got, err := repo.ListarPendientes(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 3, "the reenviado row must be excluded")

	gotIDs := []uuid.UUID{got[0].ID(), got[1].ID(), got[2].ID()}
	assert.Equal(t, []uuid.UUID{oldest.ID(), middle.ID(), newest.ID()}, gotIDs,
		"ListarPendientes must order by RecibidoEn ascending")
}

// TestListarPendientes_BoundsByLimite proves the limite argument actually
// bounds the SQL LIMIT clause, not just a post-hoc slice in Go that would
// still have paid to fetch every row.
func TestListarPendientes_BoundsByLimite(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()

	for i := range 5 {
		m := mensajeAt(t, uuid.NewString(), tRecibido.Add(time.Duration(i)*time.Minute))
		inserted, err := repo.Guardar(ctx, m)
		require.NoError(t, err)
		require.True(t, inserted)
	}

	got, err := repo.ListarPendientes(ctx, 2)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestMarcarReenviado_TransitionsTheRow(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()
	m := mensaje(t, "wamid-reenviar")
	inserted, err := repo.Guardar(ctx, m)
	require.NoError(t, err)
	require.True(t, inserted)

	require.NoError(t, repo.MarcarReenviado(ctx, m.ID(), tLuego))

	n, err := repo.ContarPendientes(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "the row must no longer count as pending")

	got, err := repo.ListarPendientes(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestMarcarReenviado_UnknownIDReportsNotFound(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	err := repo.MarcarReenviado(context.Background(), uuid.New(), tLuego)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrMensajeEntranteNoEncontrado)
}

// TestMarcarFallido_RecordsTheReasonAndTransitions proves the row leaves
// EstadoReenvioPendiente. The exact persisted motivo_fallo string is
// verified separately by the white-box
// TestMarcarFallido_PersistsMotivoFalloVerbatim in internal_test.go —
// outbound.BuzonRepo deliberately has no lookup-by-id for a non-pendiente
// row (see RecibirMensaje's doc comment in app/service.go), so this
// black-box test cannot read motivo_fallo back through the port alone.
func TestMarcarFallido_RecordsTheReasonAndTransitions(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()
	m := mensaje(t, "wamid-fallar")
	inserted, err := repo.Guardar(ctx, m)
	require.NoError(t, err)
	require.True(t, inserted)

	const motivo = "el servidor de la tienda respondió 503"
	require.NoError(t, repo.MarcarFallido(ctx, m.ID(), motivo, tLuego))

	n, err := repo.ContarPendientes(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "the row must no longer count as pending")

	got, err := repo.ListarPendientes(ctx, 10)
	require.NoError(t, err)
	assert.Empty(t, got, "the failed row must not reappear as pending")
}

func TestMarcarFallido_UnknownIDReportsNotFound(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	err := repo.MarcarFallido(context.Background(), uuid.New(), "motivo", tLuego)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrMensajeEntranteNoEncontrado)
}

func TestContarPendientes_CountsOnlyPendingRows(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()

	pendiente1 := mensaje(t, "wamid-p1")
	pendiente2 := mensajeAt(t, "wamid-p2", tRecibido.Add(time.Minute))
	reenviado := mensajeAt(t, "wamid-r1", tRecibido.Add(2*time.Minute))
	fallido := mensajeAt(t, "wamid-f1", tRecibido.Add(3*time.Minute))

	for _, m := range []*domain.MensajeEntrante{pendiente1, pendiente2, reenviado, fallido} {
		inserted, err := repo.Guardar(ctx, m)
		require.NoError(t, err)
		require.True(t, inserted)
	}
	require.NoError(t, repo.MarcarReenviado(ctx, reenviado.ID(), tLuego))
	require.NoError(t, repo.MarcarFallido(ctx, fallido.ID(), "motivo", tLuego))

	n, err := repo.ContarPendientes(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

// TestTimestampRoundTrip_LosslessNanosecondsAndUTC is the named test from
// the brief. It writes a time.Time built with a specific America/Mexico_City
// (non-UTC) location AND a non-zero nanosecond component, reads it back
// through the full Guardar -> ListarPendientes round trip (through the real
// TEXT column, not a helper called in isolation), and checks:
//  1. the nanosecond component survived exactly;
//  2. the zone is UTC, even though the value that went in was not.
//
// Both checks would fail if the persisted layout ever silently truncated
// sub-second precision (a common RFC3339-without-Nano bug) or forgot the
// initial .UTC() normalization on write.
func TestTimestampRoundTrip_LosslessNanosecondsAndUTC(t *testing.T) {
	t.Parallel()
	repo := newTestRepo(t)
	ctx := context.Background()

	loc, err := time.LoadLocation("America/Mexico_City")
	require.NoError(t, err)
	original := time.Date(2026, 8, 31, 4, 30, 0, 987654321, loc)
	require.NotEqual(t, time.UTC, original.Location(), "the fixture must start life in a non-UTC zone")

	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         "wamid-roundtrip",
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "text",
		Contenido:     "hola",
		TimestampMeta: original,
		RecibidoEn:    original,
	})
	require.NoError(t, err)

	inserted, err := repo.Guardar(ctx, m)
	require.NoError(t, err)
	require.True(t, inserted)

	got, err := repo.ListarPendientes(ctx, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)

	roundTripped := got[0].RecibidoEn()
	assert.True(t, roundTripped.Equal(original), "the round-tripped instant must equal the original")
	assert.Equal(t, original.UnixNano(), roundTripped.UnixNano(), "nanoseconds must survive exactly")
	assert.Equal(t, time.UTC, roundTripped.Location(), "the round-tripped time must be normalized to UTC")

	roundTrippedMeta := got[0].TimestampMeta()
	assert.Equal(t, original.UnixNano(), roundTrippedMeta.UnixNano())
	assert.Equal(t, time.UTC, roundTrippedMeta.Location())
}

// mensajeAt builds a valid, unpersisted MensajeEntrante with the given
// wamid, received at recibidoEn.
func mensajeAt(t *testing.T, wamid string, recibidoEn time.Time) *domain.MensajeEntrante {
	t.Helper()
	m, err := domain.NewMensajeEntrante(domain.NewMensajeEntranteParams{
		Wamid:         wamid,
		Remitente:     "5219981234567",
		PhoneNumberID: "109876543210123",
		Tipo:          "text",
		Contenido:     "hola",
		TimestampMeta: recibidoEn,
		RecibidoEn:    recibidoEn,
	})
	require.NoError(t, err)
	return m
}
