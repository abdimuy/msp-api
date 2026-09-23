// Package visitasfb_test contains Firebird integration tests for the visitas
// repo. Tests skip cleanly when FB_DATABASE is not set; every write runs
// inside fbtestutil.WithTestTransaction (rollback-only) so the shared dev DB
// (MSP_VISITAS holds ~226k real legacy rows) never accumulates state.
//
// Run: FB_DATABASE=/firebird/data/MUEBLERA.FDB go test ./internal/visitas/infra/visitasfb/...
package visitasfb_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
	"github.com/abdimuy/msp-api/internal/visitas/infra/visitasfb"
)

// requireFBEnv skips the test when FB_DATABASE is not set.
func requireFBEnv(t *testing.T) {
	t.Helper()
	if os.Getenv("FB_DATABASE") == "" {
		t.Skip("FB_DATABASE not set; skipping Firebird integration tests")
	}
}

// testCobradorMarker tags every visita this test file creates so the
// verification query in the task brief ("residual rows after the run") can
// select on a single, unambiguous value:
//
//	SELECT COUNT(*) FROM MSP_VISITAS WHERE COBRADOR LIKE '%test_task2_visitas_repo%'
//
// The marker used to live in TIPO_VISITA. It moved to COBRADOR when the
// TipoVisita catalogue closed (domain/tipo_visita.go): NewVisita now rejects
// anything outside the catalogue, so an arbitrary marker string can no longer
// be written there. COBRADOR is still free text, and a marker there is in
// fact MORE selective — every one of the 226k legacy rows carries one of the
// 14 catalogue values in TIPO_VISITA, so that column could never have
// isolated this file's rows anyway.
const testCobradorMarker = "test_task2_visitas_repo"

// testTipoVisita is the catalogue member these tests write. Any member would
// do; this is the most common one in production.
const testTipoVisita = domain.TipoVisitaNoSeEncontraba

// markedCobrador appends the residual-row marker to a cobrador name, so the
// verification query can find this file's rows without relying on
// TIPO_VISITA (see testCobradorMarker).
func markedCobrador(base string) string { return base + " (" + testCobradorMarker + ")" }

// buildValidVisita constructs a fresh, valid Visita using domain.NewVisita
// with realistic Mexican Spanish data. Each call mints a new random UUID so
// multiple instances never collide within the same transaction.
func buildValidVisita(t *testing.T) *domain.Visita {
	t.Helper()
	now := time.Now().UTC()
	v, err := domain.NewVisita(domain.CrearVisitaParams{
		ID:            uuid.New(),
		Cobrador:      markedCobrador("Ramírez García, Jorge"),
		CobradorID:    42,
		Fecha:         now.Add(-time.Hour),
		FormaCobroID:  87327,
		Lat:           19.432608,
		Lng:           -99.133209,
		Nota:          "",
		TipoVisita:    string(testTipoVisita),
		ZonaClienteID: 7,
		ClienteID:     11486,
		CreatedBy:     uuid.New(),
		Now:           now,
	})
	require.NoError(t, err, "buildValidVisita: NewVisita must not fail")
	return v
}

// newVisitasRepo is a convenience helper that builds the repo under test.
func newVisitasRepo(pool *firebird.Pool) *visitasfb.Repo {
	return visitasfb.New(pool)
}

// ─── Insert + FindByID round-trip ──────────────────────────────────────────

// TestE2E_Visitas_InsertThenFindByID_RoundTripsEveryField inserts a visita
// with every field set (including a non-nil ImpteDoctoCCID and a non-empty
// Nota) and verifies FindByID returns each field exactly.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_InsertThenFindByID_RoundTripsEveryField(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		now := time.Now().UTC()
		impte := 789012
		v, err := domain.NewVisita(domain.CrearVisitaParams{
			ID:             uuid.New(),
			Cobrador:       markedCobrador("Hernández López, Ana"),
			CobradorID:     42,
			Fecha:          now.Add(-2 * time.Hour),
			FormaCobroID:   87327,
			Lat:            19.432608,
			Lng:            -99.133209,
			Nota:           "Pagó completo, cliente satisfecho",
			TipoVisita:     string(testTipoVisita),
			ZonaClienteID:  7,
			ClienteID:      11486,
			ImpteDoctoCCID: &impte,
			CreatedBy:      uuid.New(),
			Now:            now,
		})
		require.NoError(t, err)

		require.NoError(t, repo.Insert(ctx, v), "Insert must succeed for a valid new visita")

		found, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)

		assert.Equal(t, v.ID(), found.ID())
		assert.Equal(t, v.Cobrador(), found.Cobrador())
		assert.Equal(t, v.CobradorID(), found.CobradorID())
		assert.Equal(t, v.FormaCobroID(), found.FormaCobroID())
		assert.InDelta(t, v.Lat(), found.Lat(), 0)
		assert.InDelta(t, v.Lng(), found.Lng(), 0)
		assert.Equal(t, v.Nota(), found.Nota())
		assert.Equal(t, v.TipoVisita(), found.TipoVisita())
		assert.Equal(t, v.ZonaClienteID(), found.ZonaClienteID())
		assert.Equal(t, v.ClienteID(), found.ClienteID())
		require.NotNil(t, found.ImpteDoctoCCID())
		assert.Equal(t, impte, *found.ImpteDoctoCCID())

		// FECHA round-trip: Firebird TIMESTAMP truncates to millisecond
		// precision — tolerate a small delta rather than asserting exact
		// equality.
		delta := found.Fecha().Sub(v.Fecha())
		if delta < 0 {
			delta = -delta
		}
		assert.Less(t, delta, 2*time.Second, "Fecha must round-trip within 2s tolerance")
	})
}

// TestE2E_Visitas_InsertThenFindByID_NilImpteDoctoCCIDAndEmptyNota verifies
// that a visita built with no ImpteDoctoCCID and no Nota round-trips those
// nullable columns as nil / empty string, not some zero-value sentinel.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_InsertThenFindByID_NilImpteDoctoCCIDAndEmptyNota(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t) // Nota == "", ImpteDoctoCCID == nil

		require.NoError(t, repo.Insert(ctx, v))

		found, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)

		assert.Empty(t, found.Nota(), "Nota must round-trip as empty string when not set")
		assert.Nil(t, found.ImpteDoctoCCID(), "ImpteDoctoCCID must round-trip as nil when not set")

		// Verify NULL at the SQL level too, not just the domain default.
		q := firebird.GetQuerier(ctx, pool.DB)
		var nota *string
		var impte *int
		err = q.QueryRowContext(ctx,
			`SELECT NOTA, IMPTE_DOCTO_CC_ID FROM MSP_VISITAS WHERE ID = ?`,
			v.ID().String(),
		).Scan(&nota, &impte)
		require.NoError(t, err)
		assert.Nil(t, nota, "NOTA column must be SQL NULL, not empty string")
		assert.Nil(t, impte, "IMPTE_DOCTO_CC_ID column must be SQL NULL")
	})
}

// TestE2E_Visitas_InsertThenFindByID_AccentRoundTrip verifies that Nota and
// Cobrador containing ñ/ó/á come back byte-identical — the documented risk
// for MSP_VISITAS' CHARACTER SET NONE text columns holding UTF-8 bytes.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_InsertThenFindByID_AccentRoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		wantCobrador := markedCobrador("José Muñoz Peña")
		const wantNota = "María salió, vuelve mañana"
		now := time.Now().UTC()
		v, err := domain.NewVisita(domain.CrearVisitaParams{
			ID:            uuid.New(),
			Cobrador:      wantCobrador,
			CobradorID:    42,
			Fecha:         now.Add(-time.Hour),
			FormaCobroID:  87327,
			Lat:           19.432608,
			Lng:           -99.133209,
			Nota:          wantNota,
			TipoVisita:    string(testTipoVisita),
			ZonaClienteID: 7,
			ClienteID:     11486,
			CreatedBy:     uuid.New(),
			Now:           now,
		})
		require.NoError(t, err)

		require.NoError(t, repo.Insert(ctx, v))

		found, err := repo.FindByID(ctx, v.ID())
		require.NoError(t, err)

		assert.Equal(t, wantCobrador, found.Cobrador(),
			"Cobrador must round-trip byte-identical (accents intact)")
		assert.Equal(t, wantNota, found.Nota(),
			"Nota must round-trip byte-identical (accents intact)")
		assert.Equal(t, []byte(wantCobrador), []byte(found.Cobrador()),
			"Cobrador must round-trip byte-identical at the UTF-8 byte level")
		assert.Equal(t, []byte(wantNota), []byte(found.Nota()),
			"Nota must round-trip byte-identical at the UTF-8 byte level")
	})
}

// ─── Insert duplicate key ──────────────────────────────────────────────────

// TestE2E_Visitas_Insert_DuplicateKey verifies that inserting the same visita
// ID twice returns domain.ErrVisitaYaExiste.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_Insert_DuplicateKey(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)

		require.NoError(t, repo.Insert(ctx, v), "first Insert must succeed")

		err := repo.Insert(ctx, v)
		require.Error(t, err, "second Insert with same UUID must fail")
		require.ErrorIs(t, err, domain.ErrVisitaYaExiste,
			"duplicate Insert must return ErrVisitaYaExiste; got: %v", err)
	})
}

// ─── FindByID not found ─────────────────────────────────────────────────────

// TestE2E_Visitas_FindByID_NotFound verifies that looking up a random UUID
// returns domain.ErrVisitaNoEncontrada.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_FindByID_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)

		_, err := repo.FindByID(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrVisitaNoEncontrada,
			"FindByID for unknown UUID must return ErrVisitaNoEncontrada; got: %v", err)
	})
}

// ─── FindExistingIDs ────────────────────────────────────────────────────────

// TestE2E_Visitas_FindExistingIDs_MixOfKnownAndUnknown inserts two visitas,
// then asks FindExistingIDs about those two ids plus a third one it never
// saw. The unknown id must be silently absent — never an error.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_FindExistingIDs_MixOfKnownAndUnknown(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v1, v2 := buildValidVisita(t), buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v1))
		require.NoError(t, repo.Insert(ctx, v2))
		unknown := uuid.New()

		got, err := repo.FindExistingIDs(ctx, []uuid.UUID{v1.ID(), unknown, v2.ID()})

		require.NoError(t, err)
		assert.ElementsMatch(t, []uuid.UUID{v1.ID(), v2.ID()}, got,
			"the unknown id must be absent from the result, not an error")
	})
}

// TestE2E_Visitas_FindExistingIDs_NoneKnown_ReturnsEmpty verifies a request
// entirely of unknown ids returns an empty slice, not an error.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_FindExistingIDs_NoneKnown_ReturnsEmpty(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)

		got, err := repo.FindExistingIDs(ctx, []uuid.UUID{uuid.New(), uuid.New()})

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

// TestE2E_Visitas_FindExistingIDs_EmptyInput_NoQuery verifies an empty ids
// slice short-circuits to an empty result without issuing a query.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_FindExistingIDs_EmptyInput_NoQuery(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)

		got, err := repo.FindExistingIDs(ctx, []uuid.UUID{})

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

// TestE2E_Visitas_FindExistingIDs_DuplicateInput_NoDuplicatesInResult
// verifies that a "WHERE ID IN (...)" built with the same id repeated
// several times still answers that id exactly once.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_FindExistingIDs_DuplicateInput_NoDuplicatesInResult(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		got, err := repo.FindExistingIDs(ctx, []uuid.UUID{v.ID(), v.ID(), v.ID()})

		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{v.ID()}, got)
	})
}

// TestE2E_Visitas_FindExistingIDs_AtCap_SingleQuerySucceeds proves a single
// "WHERE ID IN (...)" round-trip built with maxIDsPorRequest (100) bound
// parameters — the visitashttp cap — succeeds against real Firebird without
// chunking.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_Visitas_FindExistingIDs_AtCap_SingleQuerySucceeds(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newVisitasRepo(pool)
		v := buildValidVisita(t)
		require.NoError(t, repo.Insert(ctx, v))

		ids := make([]uuid.UUID, 100)
		ids[0] = v.ID()
		for i := 1; i < len(ids); i++ {
			ids[i] = uuid.New()
		}

		got, err := repo.FindExistingIDs(ctx, ids)

		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{v.ID()}, got)
	})
}
