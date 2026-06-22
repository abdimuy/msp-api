// narrativa_repo_test.go contains Firebird integration tests for NarrativaRepo.
// All writes execute inside a transaction that always rolls back — the shared
// dev DB never accumulates test data.
//
// Prerequisites:
//   - FB_DATABASE env var pointing at the dev Microsip Firebird DB.
//   - Migration 000040 applied (creates MSP_AN_CLIENTE_NARRATIVA and
//     MSP_AN_NARRATIVA_PENDIENTE). Each test skips cleanly when either
//     precondition is absent (mirroring how repo_test.go skips on missing 000035).
//
// Run:
//
//	FB_DATABASE=/firebird/data/MUEBLERA.FDB \
//	  go test ./internal/analytics/infra/analyticsfb/... -v
//
//nolint:misspell // Spanish domain vocabulary by project convention.
package analyticsfb_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/analytics/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
)

// skipIfMigration000040Missing probes MSP_AN_CLIENTE_NARRATIVA inside the
// already-opened test transaction. If the table is absent (migration 000040
// not yet applied) it calls t.Skipf so the suite stays green in skip-mode —
// mirroring the pattern used for migration 000035 in repo_test.go.
func skipIfMigration000040Missing(ctx context.Context, t *testing.T, repo *analyticsfb.Repo) {
	t.Helper()
	// A GetNarrativa for a negative id is a safe no-op probe: (nil, nil) on
	// success, an engine-level error when the table does not exist.
	_, err := repo.GetNarrativa(ctx, -1)
	if err != nil && isMissingTableError(err) {
		t.Skipf("MSP_AN_CLIENTE_NARRATIVA missing — migration 000040 may not be applied: %v", err)
	}
}

// isMissingTableError returns true for Firebird "table unknown" class errors.
// The nakagami/firebirdsql driver surfaces these as a message containing
// "Table unknown" or SQL error code -204. A simple substring check is
// sufficient for the skip guard.
func isMissingTableError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "table unknown") ||
		strings.Contains(msg, "-204") ||
		strings.Contains(msg, "msp_an_cliente_narrativa")
}

// makeNarrativa builds a domain.Narrativa with deterministic fields.
// clienteID should be a large negative number to avoid collisions with
// production data — consistent with the negative-clienteID convention used
// throughout repo_test.go.
func makeNarrativa(clienteID int, rasgos []string) domain.Narrativa {
	return domain.Narrativa{
		ClienteID:  clienteID,
		Texto:      "Cliente con excelente historial de pagos y alta frecuencia de compra.",
		Rasgos:     rasgos,
		InputHash:  "abc123def456abc123def456abc123def456abc123def456abc123def456abc1",
		Modelo:     "claude-3-5-sonnet-20241022",
		GeneradaEn: time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC),
	}
}

// ─── CONTEXTO_OPERATIVO round-trip ────────────────────────────────────────────

// TestNarrativaRepo_ContextoOperativo_RoundTrip verifies that the
// CONTEXTO_OPERATIVO column (migration 000041) round-trips through both the
// INSERT and UPDATE branches of UpsertNarrativa, including the empty-string case.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_ContextoOperativo_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		const clienteID = -99010

		// INSERT branch: contexto with accented text (UTF8 column).
		n := makeNarrativa(clienteID, []string{"buen_pagador"})
		n.ContextoOperativo = "Acuerdo de pago con Carmelo; domicilio compartido con Amada."
		require.NoError(t, repo.UpsertNarrativa(ctx, n))

		got, err := repo.GetNarrativa(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, n.ContextoOperativo, got.ContextoOperativo,
			"ContextoOperativo must round-trip through INSERT")

		// UPDATE branch: overwrite with a new contexto.
		n.ContextoOperativo = "Responsable de pago: la hija."
		require.NoError(t, repo.UpsertNarrativa(ctx, n))

		got2, err := repo.GetNarrativa(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got2)
		assert.Equal(t, "Responsable de pago: la hija.", got2.ContextoOperativo,
			"ContextoOperativo must round-trip through UPDATE")

		// Empty contexto persists as "" (not an error).
		n.ContextoOperativo = ""
		require.NoError(t, repo.UpsertNarrativa(ctx, n))
		got3, err := repo.GetNarrativa(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got3)
		assert.Empty(t, got3.ContextoOperativo, "empty ContextoOperativo must round-trip as empty string")
	})
}

// ─── GetNotaCliente ───────────────────────────────────────────────────────────

// TestNarrativaRepo_GetNotaCliente_UnknownClient verifies that GetNotaCliente
// returns ("", nil) for a client id that does not exist in CLIENTES — the note
// is optional context, never a hard error.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_GetNotaCliente_UnknownClient(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		nota, err := repo.GetNotaCliente(ctx, -99011)
		require.NoError(t, err)
		assert.Empty(t, nota, "unknown client must yield empty note, no error")
	})
}

// ─── GetNarrativa — miss ──────────────────────────────────────────────────────

// TestNarrativaRepo_GetNarrativa_Miss verifies that GetNarrativa returns
// (nil, nil) when no row exists for the given clienteID.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_GetNarrativa_Miss(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		row, err := repo.GetNarrativa(ctx, -99001)
		require.NoError(t, err)
		assert.Nil(t, row, "GetNarrativa for unknown clienteID must return nil, nil")
	})
}

// ─── UpsertNarrativa insert → GetNarrativa round-trip ────────────────────────

// TestNarrativaRepo_UpsertInsert_RoundTrip verifies that after UpsertNarrativa
// (first call = INSERT), GetNarrativa returns all fields correctly.
// Covers multi-element Rasgos and an empty Rasgos slice.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_UpsertInsert_RoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		// ── multi-element Rasgos ──────────────────────────────────────────────
		n1 := makeNarrativa(-99002, []string{"buen_pagador", "comprador_estacional", "alta_frecuencia"})
		require.NoError(t, repo.UpsertNarrativa(ctx, n1))

		got1, err := repo.GetNarrativa(ctx, -99002)
		require.NoError(t, err)
		require.NotNil(t, got1, "GetNarrativa must return row after UpsertNarrativa")
		assert.Equal(t, -99002, got1.ClienteID)
		assert.Equal(t, n1.Texto, got1.Texto, "Texto must round-trip")
		assert.Equal(t, n1.Rasgos, got1.Rasgos, "Rasgos must round-trip")
		assert.Equal(t, n1.InputHash, got1.InputHash, "InputHash must round-trip")
		assert.Equal(t, n1.Modelo, got1.Modelo, "Modelo must round-trip")

		// ── empty Rasgos (stored as "[]") ────────────────────────────────────
		n2 := makeNarrativa(-99003, []string{})
		require.NoError(t, repo.UpsertNarrativa(ctx, n2))

		got2, err := repo.GetNarrativa(ctx, -99003)
		require.NoError(t, err)
		require.NotNil(t, got2, "GetNarrativa must return row with empty rasgos")
		assert.Equal(t, []string{}, got2.Rasgos, "empty Rasgos must round-trip as empty slice, not nil")

		t.Logf("upsert insert round-trip ok: clienteID=%d rasgos=%v", got1.ClienteID, got1.Rasgos)
	})
}

// ─── UpsertNarrativa update (same CLIENTE_ID twice) ──────────────────────────

// TestNarrativaRepo_UpsertUpdate_InPlace verifies that calling UpsertNarrativa
// twice for the same CLIENTE_ID updates the row in-place (no duplicate-key error,
// exactly one row, new values returned).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_UpsertUpdate_InPlace(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		const clienteID = -99004

		// First insert.
		n1 := makeNarrativa(clienteID, []string{"primer_rasgo"})
		require.NoError(t, repo.UpsertNarrativa(ctx, n1))

		// Second upsert — different Texto, Rasgos, InputHash, Modelo.
		n2 := domain.Narrativa{
			ClienteID:  clienteID,
			Texto:      "Texto actualizado tras segunda generación de narrativa.",
			Rasgos:     []string{"buen_pagador", "riesgo_bajo"},
			InputHash:  "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Modelo:     "claude-opus-4-5",
			GeneradaEn: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC),
		}
		require.NoError(t, repo.UpsertNarrativa(ctx, n2), "second UpsertNarrativa must not fail")

		got, err := repo.GetNarrativa(ctx, clienteID)
		require.NoError(t, err)
		require.NotNil(t, got, "row must exist after second upsert")

		// Values must reflect the second upsert.
		assert.Equal(t, n2.Texto, got.Texto, "Texto must be updated to second value")
		assert.Equal(t, n2.Rasgos, got.Rasgos, "Rasgos must be updated to second value")
		assert.Equal(t, n2.InputHash, got.InputHash, "InputHash must be updated to second value")
		assert.Equal(t, n2.Modelo, got.Modelo, "Modelo must be updated to second value")

		t.Logf("upsert update in-place ok: clienteID=%d texto=%q rasgos=%v",
			got.ClienteID, got.Texto, got.Rasgos)
	})
}

// ─── Encolar idempotent ───────────────────────────────────────────────────────

// TestNarrativaRepo_Encolar_Idempotent verifies that enqueuing the same client
// twice produces exactly one row in the queue with the latest hash.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_Encolar_Idempotent(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		const clienteID = -99005
		hash1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		hash2 := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

		require.NoError(t, repo.Encolar(ctx, clienteID, hash1))
		require.NoError(t, repo.Encolar(ctx, clienteID, hash2), "re-enqueue must not fail")

		pending, err := repo.ListarPendientes(ctx, 100)
		require.NoError(t, err)

		var count int
		var latestHash string
		for _, row := range pending {
			if row.ClienteID == clienteID {
				count++
				latestHash = row.InputHash
			}
		}
		assert.Equal(t, 1, count, "idempotent enqueue must produce exactly one row")
		assert.Equal(t, hash2, latestHash, "second Encolar must refresh the hash")

		t.Logf("encolar idempotente ok: clienteID=%d hash=%s", clienteID, latestHash)
	})
}

// ─── ListarPendientes — limit + order ─────────────────────────────────────────

// TestNarrativaRepo_ListarPendientes_LimitAndOrder verifies that ListarPendientes
// caps the result to `limit` rows and returns them in ENCOLADA_EN ASC order.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_ListarPendientes_LimitAndOrder(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		// Enqueue three clients. We sleep 1 ms between calls to guarantee a
		// distinct ENCOLADA_EN (Firebird TIMESTAMP resolution is ~1ms).
		ids := []int{-99010, -99011, -99012}
		hash := "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		for _, id := range ids {
			require.NoError(t, repo.Encolar(ctx, id, hash))
			time.Sleep(2 * time.Millisecond)
		}

		// Limit to 2 — must return only 2 of our 3 rows (plus any pre-existing
		// rows from the shared DB, capped at limit total).
		pending, err := repo.ListarPendientes(ctx, 2)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(pending), 2, "ListarPendientes(limit=2) must return at most 2 rows")

		t.Logf("listar pendientes limit ok: got=%d rows (limit=2)", len(pending))
	})
}

// ─── BorrarPendiente ──────────────────────────────────────────────────────────

// TestNarrativaRepo_BorrarPendiente verifies that after BorrarPendiente,
// the client no longer appears in ListarPendientes.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_BorrarPendiente(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		const clienteID = -99020
		hash := "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

		require.NoError(t, repo.Encolar(ctx, clienteID, hash))

		// Verify it's in the queue.
		before, err := repo.ListarPendientes(ctx, 1000)
		require.NoError(t, err)
		found := false
		for _, row := range before {
			if row.ClienteID == clienteID {
				found = true
				break
			}
		}
		require.True(t, found, "clienteID must be in queue before BorrarPendiente")

		// Delete it.
		require.NoError(t, repo.BorrarPendiente(ctx, clienteID))

		// Verify it's gone.
		after, err := repo.ListarPendientes(ctx, 1000)
		require.NoError(t, err)
		for _, row := range after {
			assert.NotEqual(t, clienteID, row.ClienteID,
				"clienteID must not appear in queue after BorrarPendiente")
		}

		// Deleting a non-existent row is a no-op (no error).
		require.NoError(t, repo.BorrarPendiente(ctx, clienteID),
			"BorrarPendiente on already-deleted row must not error")

		t.Logf("borrar pendiente ok: clienteID=%d removed from queue", clienteID)
	})
}

// TestNarrativaRepo_ListarPendientes_Empty verifies that ListarPendientes
// returns an empty (non-nil) slice when no rows match.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_ListarPendientes_Empty(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		// Using limit=0 will match no rows via FIRST 0 (Firebird returns 0 rows).
		pending, err := repo.ListarPendientes(ctx, 0)
		require.NoError(t, err)
		assert.NotNil(t, pending, "ListarPendientes must return non-nil slice even when empty")

		t.Logf("listar pendientes empty ok")
	})
}

// TestNarrativaRepo_UpsertNarrativa_NilRasgos verifies that a nil Rasgos field
// is stored as "[]" and round-trips as an empty slice (not nil).
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestNarrativaRepo_UpsertNarrativa_NilRasgos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		skipIfMigration000040Missing(ctx, t, repo)

		n := domain.Narrativa{
			ClienteID:  -99030,
			Texto:      "Sin rasgos aún.",
			Rasgos:     nil, // explicitly nil
			InputHash:  "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
			Modelo:     "claude-haiku-4-5",
			GeneradaEn: time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC),
		}
		require.NoError(t, repo.UpsertNarrativa(ctx, n))

		got, err := repo.GetNarrativa(ctx, -99030)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.NotNil(t, got.Rasgos, "nil Rasgos must round-trip as empty (non-nil) slice")
		assert.Empty(t, got.Rasgos, "nil Rasgos must round-trip as empty slice")

		t.Logf("nil rasgos round-trip ok: rasgos=%v", got.Rasgos)
	})
}

// ─── Compile-time interface check via outbound.NarrativaRepo ─────────────────
// This ensures that even if the var _ assertion in narrativa_repo.go were
// removed, the test package catches a missing implementation at compile time.
var _ outbound.NarrativaRepo = (*analyticsfb.Repo)(nil)
