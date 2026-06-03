//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── Integration tests — real Firebird ────────────────────────────────────────
//
// These tests skip when FB_DATABASE is unset (CI without Firebird).
// The key property under test: by-ids does NOT apply watermark filtering
// (cursor sync does; applying it here races with the tx that inserted the rows).

// requireByIDsMigrations checks that the MSP_PAGOS_VENTAS and MSP_SALDOS_VENTAS
// tables exist (migrations 000009/000010). If not, the test is skipped.
func requireByIDsMigrations(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$RELATIONS WHERE RDB$RELATION_NAME = 'MSP_PAGOS_VENTAS'`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skip("MSP_PAGOS_VENTAS not found; run 'make fb-migrate-up'")
	}
}

// requireTXIDColumn checks that TX_ID exists on MSP_PAGOS_VENTAS (migration 000022).
func requireTXIDColumn(t *testing.T, q firebird.Querier) {
	t.Helper()
	var n int
	err := q.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM RDB$RELATION_FIELDS WHERE RDB$RELATION_NAME = 'MSP_PAGOS_VENTAS' AND RDB$FIELD_NAME = 'TX_ID'`,
	).Scan(&n)
	if err != nil || n == 0 {
		t.Skip("TX_ID column not found; run 'make fb-migrate-up' (≥000022)")
	}
}

// e2eByIDsNextID claims one ID from the Firebird generator.
func e2eByIDsNextID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var id int
	err := q.QueryRowContext(context.Background(), `SELECT GEN_ID(ID_DOCTOS, 1) FROM RDB$DATABASE`).Scan(&id)
	require.NoError(t, err)
	return id
}

// e2eByIDsZonaID returns the first ZONA_ID from ZONAS_CLIENTES, or skips.
func e2eByIDsZonaID(t *testing.T, q firebird.Querier) int {
	t.Helper()
	var zonaID int
	err := q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 ZONA_ID FROM ZONAS_CLIENTES ORDER BY ZONA_ID`).Scan(&zonaID)
	if err != nil {
		t.Skip("no ZONAS_CLIENTES rows; cannot run by-ids integration test")
	}
	return zonaID
}

// e2eByIDsAltZonaID returns a zone different from exclude, or skips.
func e2eByIDsAltZonaID(t *testing.T, q firebird.Querier, exclude int) int {
	t.Helper()
	var zonaID int
	err := q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 ZONA_ID FROM ZONAS_CLIENTES WHERE ZONA_ID <> ? ORDER BY ZONA_ID`, exclude).Scan(&zonaID)
	if err != nil {
		t.Skip("only one zone exists; cannot run zona-isolation test")
	}
	return zonaID
}

// e2eByIDsClienteInZona returns the first cliente in the given zona, or skips.
func e2eByIDsClienteInZona(t *testing.T, q firebird.Querier, zonaID int) int {
	t.Helper()
	var clienteID int
	err := q.QueryRowContext(context.Background(),
		`SELECT FIRST 1 CLIENTE_ID FROM CLIENTES WHERE ZONA_CLIENTE_ID = ?`, zonaID).Scan(&clienteID)
	if err != nil {
		t.Skipf("no client in zona %d; cannot run by-ids integration test", zonaID)
	}
	return clienteID
}

// TestByIDs_Pagos_NoWatermarkFiltering verifies that the by-ids endpoint
// returns a row even when its TX_ID would be excluded by cursor-sync
// watermark logic (TX_ID = math.MaxInt64-1 is well above any real watermark).
//
// Watermark filtering is intentionally absent from ByIDs: the caller already
// obtained these PKs from the SSE listener which only publishes committed rows.
func TestByIDs_Pagos_NoWatermarkFiltering(t *testing.T) {
	t.Parallel()
	e2eRequireFBEnv(t)
	pool := e2eTestPool(t)

	ctx := context.Background()
	q := pool
	requireByIDsMigrations(t, q)
	requireTXIDColumn(t, q)

	zonaID := e2eByIDsZonaID(t, q)
	clienteID := e2eByIDsClienteInZona(t, q, zonaID)

	impteID := e2eByIDsNextID(t, q)
	doctoCCID := e2eByIDsNextID(t, q)

	now := time.Now().UTC().Truncate(time.Second)
	txID := int64(math.MaxInt64 - 1)

	insertPagosSQL := "INSERT INTO MSP_PAGOS_VENTAS (" +
		"IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, DOCTO_CC_ACR_ID, CLIENTE_ID, ZONA_CLIENTE_ID, " +
		"FOLIO, CONCEPTO_CC_ID, FECHA, IMPORTE, IMPUESTO, LAT, LON, " +
		"CANCELADO, APLICADO, UPDATED_AT, TX_ID" +
		") VALUES (?, ?, 'CV-TEST-BYIDS-NWF', 87327, ?, 1500, 0, NULL, 'N', 'S', ?, ?)"
	_, err := pool.ExecContext(
		ctx, insertPagosSQL,
		impteID, doctoCCID, doctoCCID, clienteID, zonaID,
		firebird.ToWallClock(now), firebird.ToWallClock(now), txID,
	)
	require.NoError(t, err, "INSERT into MSP_PAGOS_VENTAS")
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_PAGOS_VENTAS WHERE IMPTE_DOCTO_CC_ID = ?`, impteID)
	})

	pagosRepo := cobranzaventfb.NewPagosRepo(pool)
	saldosRepo := cobranzaventfb.NewSaldosRepo(pool)

	// byIDsUser() from handlers_sync_by_ids_test.go — same _test package.
	handler := mountByIDsRouter(byIDsUser(), pagosRepo, saldosRepo)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/sync/pagos/by-ids?zona_id=%d&ids=%d", zonaID, impteID), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dtos []cobranzahttp.PagoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	require.NotEmpty(t, dtos, "by-ids must return the row even with TX_ID=%d (no watermark filter applied)", txID)
	assert.Equal(t, impteID, dtos[0].ImpteDoctoCCID)
}

// TestByIDs_Saldos_ZonaIsolation verifies that rows belonging to a different
// zona are excluded even when the caller submits their PKs explicitly.
//
// Two saldo rows are inserted in different zones (A and B). The request asks
// for both PKs restricted to zona A. Only the zona-A row must be returned.
func TestByIDs_Saldos_ZonaIsolation(t *testing.T) {
	t.Parallel()
	e2eRequireFBEnv(t)
	pool := e2eTestPool(t)

	ctx := context.Background()
	q := pool
	requireByIDsMigrations(t, q)

	zonaA := e2eByIDsZonaID(t, q)
	zonaB := e2eByIDsAltZonaID(t, q, zonaA)

	clienteA := e2eByIDsClienteInZona(t, q, zonaA)
	clienteB := e2eByIDsClienteInZona(t, q, zonaB)

	doctoCCIDA := e2eByIDsNextID(t, q)
	doctoCCIDB := e2eByIDsNextID(t, q)
	doctoPVIDA := e2eByIDsNextID(t, q)
	doctoPVIDB := e2eByIDsNextID(t, q)

	now := time.Now().UTC().Truncate(time.Second)
	insertSaldo := func(doctoCCID, doctoPVID, clienteID, zonaIDVal int) {
		t.Helper()
		_, err := pool.ExecContext(
			ctx, `
INSERT INTO MSP_SALDOS_VENTAS (
    DOCTO_CC_ID, DOCTO_PV_ID, CLIENTE_ID, ZONA_CLIENTE_ID, FOLIO,
    FECHA_CARGO, PRECIO_TOTAL, TOTAL_IMPORTE, IMPTE_REST, SALDO,
    NUM_PAGOS, FECHA_ULT_PAGO, CARGO_CANCELADO, UPDATED_AT
) VALUES (
    ?, ?, ?, ?, 'TEST-ZONA-ISO',
    ?, 5000, 0, 0, 5000,
    0, NULL, 'N', ?
)`,
			doctoCCID, doctoPVID, clienteID, zonaIDVal,
			firebird.ToWallClock(now), firebird.ToWallClock(now),
		)
		require.NoError(t, err)
	}

	insertSaldo(doctoCCIDA, doctoPVIDA, clienteA, zonaA)
	insertSaldo(doctoCCIDB, doctoPVIDB, clienteB, zonaB)
	t.Cleanup(func() {
		_, _ = pool.ExecContext(ctx,
			`DELETE FROM MSP_SALDOS_VENTAS WHERE DOCTO_CC_ID IN (?, ?)`, doctoCCIDA, doctoCCIDB)
	})

	pagosRepo := cobranzaventfb.NewPagosRepo(pool)
	saldosRepo := cobranzaventfb.NewSaldosRepo(pool)

	handler := mountByIDsRouter(byIDsUser(), pagosRepo, saldosRepo)

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/sync/saldos/by-ids?zona_id=%d&ids=%d,%d", zonaA, doctoCCIDA, doctoCCIDB), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dtos []cobranzahttp.SaldoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dtos))
	require.Len(t, dtos, 1, "only the zona-A row must be returned; zona isolation must hold")
	assert.Equal(t, doctoCCIDA, dtos[0].DoctoCCID)
}
