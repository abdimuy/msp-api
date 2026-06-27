//nolint:misspell // Spanish domain vocabulary (cartera, zona, cobrador, saldo, etc.) by project convention.
package analyticsfb_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/analytics/domain"
	"github.com/abdimuy/msp-api/internal/analytics/infra/analyticsfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── shared fixtures ──────────────────────────────────────────────────────────

// syntheticZona is a large negative ZONA_CLIENTE_ID that cannot exist in real
// Microsip data, ensuring test rows are isolated from production data. The base
// is deliberately far from any value a prior test run might have left committed
// in the shared dev DB so aggregation queries filtered by this zona observe
// ONLY rows this test inserts inside its rollback-only transaction.
const syntheticZona = -970001

// syntheticCliente is a large negative CLIENTE_ID for test rows.
const syntheticCliente = -970001

// insertSaldoRow inserts one MSP_SALDOS_VENTAS row inside the ambient tx.
// doctoID must be unique per test (use large negatives).
// fechaUltPago may be zero (→ SQL NULL = never paid → bucket "90+").
func insertSaldoRow(
	t *testing.T,
	ctx context.Context, //nolint:revive // t first is Go test helper convention
	pool *firebird.Pool,
	doctoID int,
	zonaID int,
	clienteID int,
	fechaCargo time.Time,
	saldo float64,
	fechaUltPago time.Time,
) {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)

	var fupArg any
	if !fechaUltPago.IsZero() {
		fupArg = firebird.ToWallClock(fechaUltPago)
	}

	_, err := q.ExecContext(
		ctx, `
INSERT INTO MSP_SALDOS_VENTAS
  (DOCTO_CC_ID, DOCTO_PV_ID, CLIENTE_ID, ZONA_CLIENTE_ID, FOLIO,
   FECHA_CARGO, PRECIO_TOTAL, TOTAL_IMPORTE, IMPTE_REST, NUM_PAGOS,
   FECHA_ULT_PAGO, SALDO, CARGO_CANCELADO, UPDATED_AT)
VALUES (?, NULL, ?, ?, NULL, ?, ?, 0, 0, 0, ?, ?, 'N', ?)`,
		doctoID,
		clienteID,
		zonaID,
		firebird.ToWallClock(fechaCargo),
		decimal.NewFromFloat(saldo),
		fupArg,
		decimal.NewFromFloat(saldo),
		firebird.ToWallClock(time.Now().UTC()),
	)
	if err != nil {
		t.Skipf("INSERT MSP_SALDOS_VENTAS failed (migration may not be applied): %v", err)
	}
}

// insertPagoRow inserts one MSP_PAGOS_VENTAS row inside the ambient tx.
func insertPagoRow(
	t *testing.T,
	ctx context.Context, //nolint:revive // t first is Go test helper convention
	pool *firebird.Pool,
	impteID int,
	doctoID int,
	acrID int,
	clienteID int,
	zonaID int,
	conceptoID int,
	fecha time.Time,
	importe float64,
) {
	t.Helper()
	q := firebird.GetQuerier(ctx, pool.DB)
	_, err := q.ExecContext(
		ctx, `
INSERT INTO MSP_PAGOS_VENTAS
  (IMPTE_DOCTO_CC_ID, DOCTO_CC_ID, DOCTO_CC_ACR_ID, CLIENTE_ID, ZONA_CLIENTE_ID,
   FOLIO, CONCEPTO_CC_ID, FECHA, IMPORTE, IMPUESTO, LAT, LON,
   CANCELADO, APLICADO, UPDATED_AT)
VALUES (?, ?, ?, ?, ?, NULL, ?, ?, ?, 0,
        NULL, -- LAT
        NULL, -- LON
        'N', 'S', ?)`,
		impteID, doctoID, acrID, clienteID, zonaID,
		conceptoID,
		firebird.ToWallClock(fecha),
		decimal.NewFromFloat(importe),
		firebird.ToWallClock(time.Now().UTC()),
	)
	if err != nil {
		t.Skipf("INSERT MSP_PAGOS_VENTAS failed (migration may not be applied): %v", err)
	}
}

// ─── AgingSaldosByZona ────────────────────────────────────────────────────────

// TestCarteraRepo_AgingSaldosByZona inserts 4 saldo rows covering all four
// aging buckets for a synthetic zone, calls AgingSaldosByZona, and asserts
// each bucket has the expected saldo and count.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_AgingSaldosByZona(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		today := time.Now().UTC().Truncate(24 * time.Hour)

		// Row 1: paid 10 days ago → bucket "0-30"
		insertSaldoRow(t, ctx, pool, -99801, syntheticZona, syntheticCliente,
			today.AddDate(-1, 0, 0), 500.00, today.AddDate(0, 0, -10))
		// Row 2: paid 45 days ago → bucket "31-60"
		insertSaldoRow(t, ctx, pool, -99802, syntheticZona, syntheticCliente,
			today.AddDate(-1, 0, 0), 600.00, today.AddDate(0, 0, -45))
		// Row 3: paid 75 days ago → bucket "61-90"
		insertSaldoRow(t, ctx, pool, -99803, syntheticZona, syntheticCliente,
			today.AddDate(-1, 0, 0), 700.00, today.AddDate(0, 0, -75))
		// Row 4: NULL FECHA_ULT_PAGO (never paid) → bucket "90+"
		insertSaldoRow(t, ctx, pool, -99804, syntheticZona, syntheticCliente,
			today.AddDate(-1, 0, 0), 800.00, time.Time{})

		agingRows, err := repo.AgingSaldosByZona(ctx, today)
		require.NoError(t, err)

		// Extract only rows for our synthetic zone.
		got := make(map[string]struct {
			saldo  decimal.Decimal
			conteo int
		})
		for _, r := range agingRows {
			if r.ZonaClienteID == syntheticZona {
				got[r.Bucket] = struct {
					saldo  decimal.Decimal
					conteo int
				}{r.Saldo, r.Conteo}
				assert.Nil(t, r.CobradorID, "AgingSaldosByZona must not populate CobradorID")
			}
		}

		require.Contains(t, got, "0-30", "bucket '0-30' must appear")
		require.Contains(t, got, "31-60", "bucket '31-60' must appear")
		require.Contains(t, got, "61-90", "bucket '61-90' must appear")
		require.Contains(t, got, "90+", "bucket '90+' must appear")

		assert.True(t, decimal.NewFromFloat(500.00).Equal(got["0-30"].saldo),
			"0-30 saldo: want 500.00 got %s", got["0-30"].saldo)
		assert.Equal(t, 1, got["0-30"].conteo, "0-30 conteo must be 1")

		assert.True(t, decimal.NewFromFloat(600.00).Equal(got["31-60"].saldo),
			"31-60 saldo: want 600.00 got %s", got["31-60"].saldo)
		assert.Equal(t, 1, got["31-60"].conteo, "31-60 conteo must be 1")

		assert.True(t, decimal.NewFromFloat(700.00).Equal(got["61-90"].saldo),
			"61-90 saldo: want 700.00 got %s", got["61-90"].saldo)
		assert.Equal(t, 1, got["61-90"].conteo, "61-90 conteo must be 1")

		assert.True(t, decimal.NewFromFloat(800.00).Equal(got["90+"].saldo),
			"90+ saldo: want 800.00 got %s", got["90+"].saldo)
		assert.Equal(t, 1, got["90+"].conteo, "90+ conteo must be 1 (NULL pays)")

		t.Logf("AgingSaldosByZona ok: zona=%d buckets=%v", syntheticZona, got)
	})
}

// TestCarteraRepo_AgingSaldosByZona_MorosoThreshold verifies the boundary at
// exactly 30 and 31 days: day 30 is "0-30", day 31 is "31-60".
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_AgingSaldosByZona_MorosoThreshold(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		today := time.Now().UTC().Truncate(24 * time.Hour)

		// Exactly 30 days → "0-30"
		insertSaldoRow(t, ctx, pool, -99810, syntheticZona-1, syntheticCliente,
			today.AddDate(-1, 0, 0), 300.00, today.AddDate(0, 0, -30))
		// Exactly 31 days → "31-60"
		insertSaldoRow(t, ctx, pool, -99811, syntheticZona-1, syntheticCliente,
			today.AddDate(-1, 0, 0), 310.00, today.AddDate(0, 0, -31))

		agingRows, err := repo.AgingSaldosByZona(ctx, today)
		require.NoError(t, err)

		got := make(map[string]string)
		for _, r := range agingRows {
			if r.ZonaClienteID == syntheticZona-1 {
				got[r.Bucket] = r.Saldo.String()
			}
		}

		assert.Contains(t, got, "0-30", "30 days ago must be bucket '0-30'")
		assert.Contains(t, got, "31-60", "31 days ago must be bucket '31-60'")
		t.Logf("boundary ok: got=%v", got)
	})
}

// ─── AgingSaldosByCobrador ────────────────────────────────────────────────────

// TestCarteraRepo_AgingSaldosByCobrador verifies that the CLIENTES JOIN in
// AgingSaldosByCobrador correctly populates CobradorID and that the COBRADORES
// JOIN populates CobradorNombre with a non-empty display name.
//
// Strategy: inside the rollback-only tx, discover a real stable CLIENTES row
// with a non-null COBRADOR_ID, then insert a fixture MSP_SALDOS_VENTAS row
// referencing that real CLIENTE_ID under a synthetic zona. AgingSaldosByCobrador
// must return a row for that synthetic zona whose CobradorID matches the
// discovered cobrador AND whose CobradorNombre is non-empty. A JOIN on the
// wrong key would leave CobradorID nil and the assertion would fail — making
// any JOIN regression observable.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_AgingSaldosByCobrador(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)
		q := firebird.GetQuerier(ctx, pool.DB)

		// Step 1: discover a real CLIENTES row with a non-null COBRADOR_ID.
		var realClienteID, realCobradorID int
		err := q.QueryRowContext(
			ctx,
			`SELECT FIRST 1 CLIENTE_ID, COBRADOR_ID FROM CLIENTES WHERE COBRADOR_ID IS NOT NULL`,
		).Scan(&realClienteID, &realCobradorID)
		if err != nil {
			t.Skipf("no CLIENTES row with non-null COBRADOR_ID found: %v", err)
		}

		// Step 1b: resolve the cobrador NOMBRE so we can assert the JOIN returns it.
		var realCobradorNombre string
		err = q.QueryRowContext(
			ctx,
			`SELECT NOMBRE FROM COBRADORES WHERE COBRADOR_ID = ?`, realCobradorID,
		).Scan(&realCobradorNombre)
		if err != nil {
			t.Skipf("COBRADORES row not found for COBRADOR_ID=%d: %v", realCobradorID, err)
		}

		// Step 2: insert a fixture saldo row referencing the real CLIENTE_ID,
		// under a synthetic zona that cannot exist in production data.
		// FECHA_ULT_PAGO = 10 days ago → aging bucket "0-30".
		const zonaCob = syntheticZona - 5 // -970006; unique, never in real data
		today := time.Now().UTC().Truncate(24 * time.Hour)
		insertSaldoRow(t, ctx, pool, -99890, zonaCob, realClienteID,
			today.AddDate(-1, 0, 0), 770.00, today.AddDate(0, 0, -10))

		// Step 3: call AgingSaldosByCobrador and isolate the row for our zona.
		agingRows, err := repo.AgingSaldosByCobrador(ctx, today)
		require.NoError(t, err)

		var (
			found          bool
			foundCobID     *int
			foundCobNombre string
			foundSaldo     decimal.Decimal
			foundBucket    string
			foundConteo    int
		)
		for _, r := range agingRows {
			if r.ZonaClienteID == zonaCob {
				found = true
				foundCobID = r.CobradorID
				foundCobNombre = r.CobradorNombre
				foundSaldo = r.Saldo
				foundBucket = r.Bucket
				foundConteo = r.Conteo
				break
			}
		}

		require.True(t, found,
			"AgingSaldosByCobrador must return a row for synthetic zona %d "+
				"(realClienteID=%d); got %d total rows",
			zonaCob, realClienteID, len(agingRows))

		// Step 4: assert the CLIENTES JOIN carried the cobrador through correctly.
		require.NotNil(t, foundCobID,
			"CobradorID must be non-nil: the CLIENTES JOIN on CLIENTE_ID must "+
				"populate it for realClienteID=%d", realClienteID)
		assert.Equal(t, realCobradorID, *foundCobID,
			"CobradorID must equal the one discovered from CLIENTES")

		// Step 5: assert the COBRADORES JOIN returned a non-empty nombre.
		assert.NotEmpty(t, foundCobNombre,
			"CobradorNombre must be non-empty: the COBRADORES JOIN must populate NOMBRE "+
				"for cobrador_id=%d", realCobradorID)
		assert.Equal(t, realCobradorNombre, foundCobNombre,
			"CobradorNombre must match COBRADORES.NOMBRE for cobrador_id=%d", realCobradorID)

		// Step 6: assert the fixture saldo and bucket are correct.
		assert.Equal(t, "0-30", foundBucket,
			"FECHA_ULT_PAGO 10 days ago must land in bucket '0-30'")
		assert.True(t, decimal.NewFromFloat(770.00).Equal(foundSaldo),
			"saldo must equal the inserted fixture: want 770.00 got %s", foundSaldo)
		assert.Equal(t, 1, foundConteo, "conteo must be 1 for the single fixture row")

		t.Logf("AgingSaldosByCobrador JOIN ok: realClienteID=%d realCobradorID=%d "+
			"cobradorNombre=%q zona=%d saldo=%s bucket=%s",
			realClienteID, realCobradorID, foundCobNombre, zonaCob, foundSaldo, foundBucket)
	})
}

// ─── VintageSaldos ────────────────────────────────────────────────────────────

// TestCarteraRepo_VintageSaldos inserts 2 saldo rows in different FECHA_CARGO
// months and verifies the correct cohort months are computed and returned.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_VintageSaldos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const zonaV = syntheticZona - 2

		// Use noon UTC to stay within the target calendar month after ToWallClock converts to CDMX (UTC-6).
		cargo1 := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC) // cohort 2024*12+3 = 24291
		cargo2 := time.Date(2024, 9, 15, 12, 0, 0, 0, time.UTC) // cohort 2024*12+9 = 24297

		insertSaldoRow(t, ctx, pool, -99820, zonaV, syntheticCliente, cargo1, 1000.00, time.Time{})
		insertSaldoRow(t, ctx, pool, -99821, zonaV, syntheticCliente, cargo2, 2000.00, time.Time{})

		vintageRows, err := repo.VintageSaldos(ctx)
		require.NoError(t, err)

		got := make(map[int]struct {
			saldo  decimal.Decimal
			conteo int
		})
		for _, r := range vintageRows {
			if r.ZonaClienteID == zonaV {
				got[r.CohortMonth] = struct {
					saldo  decimal.Decimal
					conteo int
				}{r.Saldo, r.Conteo}
			}
		}

		wantCohort1 := domain.VintageCohort(cargo1) // 24291
		wantCohort2 := domain.VintageCohort(cargo2) // 24297

		require.Contains(t, got, wantCohort1, "cohort month %d must appear", wantCohort1)
		require.Contains(t, got, wantCohort2, "cohort month %d must appear", wantCohort2)

		assert.True(t, decimal.NewFromFloat(1000.00).Equal(got[wantCohort1].saldo),
			"cohort %d saldo: want 1000.00 got %s", wantCohort1, got[wantCohort1].saldo)
		assert.Equal(t, 1, got[wantCohort1].conteo)

		assert.True(t, decimal.NewFromFloat(2000.00).Equal(got[wantCohort2].saldo),
			"cohort %d saldo: want 2000.00 got %s", wantCohort2, got[wantCohort2].saldo)
		assert.Equal(t, 1, got[wantCohort2].conteo)

		t.Logf("VintageSaldos ok: cohort1=%d saldo1=%s cohort2=%d saldo2=%s",
			wantCohort1, got[wantCohort1].saldo, wantCohort2, got[wantCohort2].saldo)
	})
}

// ─── ColeccionCEI ─────────────────────────────────────────────────────────────

// TestCarteraRepo_ColeccionCEI inserts a payment row and verifies it appears
// in ColeccionCEI with the expected importe and count. The synthetic ZONA
// (syntheticZona-3) isolates results from real Microsip data. Because CLIENTES
// has no row for the synthetic CLIENTE_ID, the LEFT JOIN returns COBRADOR_ID=NULL.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_ColeccionCEI(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const zonaC = syntheticZona - 3
		const clienteC = syntheticCliente - 3
		const pagoFecha = "2099-06-15T10:00:00Z" // far future: no real rows overlap
		fechaPago, _ := time.Parse(time.RFC3339, pagoFecha)

		// Insert one pago: concepto 87327 (cobranza en ruta), CANCELADO='N', APLICADO='S'.
		insertPagoRow(
			t, ctx, pool,
			-99830, -99830, -99830, // impteID, doctoID, acrID
			clienteC, zonaC,
			87327, // conceptoCobranzaRuta
			fechaPago, 1500.00,
		)

		desde := fechaPago.Add(-24 * time.Hour)
		hasta := fechaPago.Add(24 * time.Hour)

		ceiRows, err := repo.ColeccionCEI(ctx, desde, hasta)
		require.NoError(t, err)

		var found bool
		for _, r := range ceiRows {
			if r.ZonaClienteID == zonaC {
				found = true
				assert.True(t, decimal.NewFromFloat(1500.00).Equal(r.Importe),
					"CEI importe: want 1500.00 got %s", r.Importe)
				assert.Equal(t, 1, r.Conteo, "CEI conteo must be 1 distinct client")
				assert.Nil(t, r.CobradorID, "synthetic client has no CLIENTES row → COBRADOR_ID must be nil")
			}
		}
		assert.True(t, found, "CEI result must contain the synthetic zona %d", zonaC)
		t.Logf("ColeccionCEI ok: zona=%d importe=%s", zonaC, decimal.NewFromFloat(1500.00))
	})
}

// TestCarteraRepo_ColeccionCEI_ExcludesOutOfRange verifies that pagos outside
// the date range are NOT included in the CEI result.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_ColeccionCEI_ExcludesOutOfRange(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		const zonaC4 = syntheticZona - 4
		const clienteC4 = syntheticCliente - 4

		insideFecha := time.Date(2099, 7, 15, 10, 0, 0, 0, time.UTC)
		outsideFecha := time.Date(2099, 7, 20, 10, 0, 0, 0, time.UTC)

		// Inside the range.
		insertPagoRow(t, ctx, pool, -99840, -99840, -99840,
			clienteC4, zonaC4, 87327, insideFecha, 200.00)
		// Outside the range (> hasta).
		insertPagoRow(t, ctx, pool, -99841, -99841, -99841,
			clienteC4, zonaC4, 87327, outsideFecha, 999.00)

		desde := insideFecha.Add(-time.Hour)
		hasta := insideFecha.Add(time.Hour)

		ceiRows, err := repo.ColeccionCEI(ctx, desde, hasta)
		require.NoError(t, err)

		for _, r := range ceiRows {
			if r.ZonaClienteID == zonaC4 {
				assert.True(t, decimal.NewFromFloat(200.00).Equal(r.Importe),
					"only inside pago must be counted: want 200.00 got %s", r.Importe)
			}
		}
		t.Logf("ColeccionCEI range filter ok: insideFecha=%s hasta=%s", insideFecha, hasta)
	})
}

// ─── SaveCarteraSnapshot / ListRecentSnapshots ────────────────────────────────

// makeSnapshot is a test helper that creates a valid CarteraSnapshot.
func makeSnapshot(
	t *testing.T,
	fechaCorte time.Time,
	zonaID int,
	cobradorID *int,
	bucket string,
	saldo float64,
	conteo int,
) domain.CarteraSnapshot {
	t.Helper()
	s, err := domain.NewCarteraSnapshot(domain.NewCarteraSnapshotParams{
		FechaCorte:    fechaCorte,
		ZonaClienteID: zonaID,
		CobradorID:    cobradorID,
		Bucket:        bucket,
		Saldo:         decimal.NewFromFloat(saldo),
		Conteo:        conteo,
		Now:           time.Now().UTC(),
	})
	require.NoError(t, err, "makeSnapshot: NewCarteraSnapshot must succeed")
	return *s
}

// TestCarteraRepo_SnapshotRoundTrip creates snapshot rows with and without a
// cobrador, saves them, retrieves them, and asserts all fields round-trip.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_SnapshotRoundTrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		fechaCorte := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		const zonaSnap = 9901
		cobradorID := 42

		s1 := makeSnapshot(t, fechaCorte, zonaSnap, nil, domain.BucketAgingDias0_30, 1000.00, 5)
		s2 := makeSnapshot(t, fechaCorte, zonaSnap, &cobradorID, domain.BucketAgingDias31_60, 2000.00, 3)

		err := repo.SaveCarteraSnapshot(ctx, []domain.CarteraSnapshot{s1, s2})
		if err != nil {
			t.Skipf("SaveCarteraSnapshot failed — migration 000042 may not be applied: %v", err)
		}

		rows, err := repo.ListRecentSnapshots(ctx, 100)
		require.NoError(t, err)

		// Find our two rows.
		found := make(map[string]domain.CarteraSnapshot)
		for _, r := range rows {
			if r.ZonaClienteID() == zonaSnap && r.FechaCorte().Equal(fechaCorte) {
				found[r.Bucket()] = r
			}
		}

		require.Contains(t, found, domain.BucketAgingDias0_30, "snapshot bucket '0-30' must appear")
		require.Contains(t, found, domain.BucketAgingDias31_60, "snapshot bucket '31-60' must appear")

		r1 := found[domain.BucketAgingDias0_30]
		assert.Equal(t, s1.ID(), r1.ID(), "ID must round-trip")
		assert.Nil(t, r1.CobradorID(), "zone-level row must have nil CobradorID")
		assert.True(t, decimal.NewFromFloat(1000.00).Equal(r1.Saldo()),
			"saldo must round-trip: want 1000.00 got %s", r1.Saldo())
		assert.Equal(t, 5, r1.Conteo())
		assert.WithinDuration(t, fechaCorte, r1.FechaCorte(), time.Second)

		r2 := found[domain.BucketAgingDias31_60]
		require.NotNil(t, r2.CobradorID(), "cobrador row must have non-nil CobradorID")
		assert.Equal(t, cobradorID, *r2.CobradorID())
		assert.True(t, decimal.NewFromFloat(2000.00).Equal(r2.Saldo()),
			"saldo must round-trip: want 2000.00 got %s", r2.Saldo())
		assert.Equal(t, 3, r2.Conteo())

		t.Logf("SnapshotRoundTrip ok: zona=%d buckets=%v", zonaSnap, []string{r1.Bucket(), r2.Bucket()})
	})
}

// TestCarteraRepo_SnapshotNullCobradorIdempotency saves a zone-level (null
// cobrador) snapshot row twice with a different saldo on the second call, then
// asserts only one row exists (UPDATE, not INSERT) and the saldo reflects the
// second write. This validates the explicit IS NULL matching in the WHERE clause.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_SnapshotNullCobradorIdempotency(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		fechaCorte := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
		const zonaSnap2 = 9902

		s1 := makeSnapshot(t, fechaCorte, zonaSnap2, nil, domain.BucketAgingDias90Plus, 5000.00, 10)
		err := repo.SaveCarteraSnapshot(ctx, []domain.CarteraSnapshot{s1})
		if err != nil {
			t.Skipf("SaveCarteraSnapshot failed — migration 000042 may not be applied: %v", err)
		}

		// Save again with different saldo — must UPDATE the existing row.
		s2 := makeSnapshot(t, fechaCorte, zonaSnap2, nil, domain.BucketAgingDias90Plus, 9999.00, 20)
		err = repo.SaveCarteraSnapshot(ctx, []domain.CarteraSnapshot{s2})
		require.NoError(t, err)

		rows, err := repo.ListRecentSnapshots(ctx, 100)
		require.NoError(t, err)

		var matchingRows []domain.CarteraSnapshot
		for _, r := range rows {
			if r.ZonaClienteID() == zonaSnap2 &&
				r.FechaCorte().Equal(fechaCorte) &&
				r.Bucket() == domain.BucketAgingDias90Plus {
				matchingRows = append(matchingRows, r)
			}
		}

		assert.Len(t, matchingRows, 1, "null-cobrador upsert must produce exactly 1 row, not %d", len(matchingRows))
		if len(matchingRows) == 1 {
			assert.True(t, decimal.NewFromFloat(9999.00).Equal(matchingRows[0].Saldo()),
				"second save must update saldo: want 9999.00 got %s", matchingRows[0].Saldo())
			assert.Equal(t, 20, matchingRows[0].Conteo(),
				"second save must update conteo")
			assert.Nil(t, matchingRows[0].CobradorID(), "CobradorID must remain nil")
		}
		t.Logf("NullCobradorIdempotency ok: 1 row after 2 saves, saldo=%s", matchingRows[0].Saldo())
	})
}

// TestCarteraRepo_SnapshotBatchIdempotency verifies that saving the same batch
// twice leaves only one row per (fechaCorte, zona, cobrador, bucket) key.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_SnapshotBatchIdempotency(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		fechaCorte := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
		const zonaSnap3 = 9903
		cob1 := 11
		cob2 := 22

		batch := []domain.CarteraSnapshot{
			makeSnapshot(t, fechaCorte, zonaSnap3, nil, domain.BucketAgingDias0_30, 100.00, 1),
			makeSnapshot(t, fechaCorte, zonaSnap3, &cob1, domain.BucketAgingDias0_30, 200.00, 2),
			makeSnapshot(t, fechaCorte, zonaSnap3, &cob2, domain.BucketAgingDias31_60, 300.00, 3),
		}

		err := repo.SaveCarteraSnapshot(ctx, batch)
		if err != nil {
			t.Skipf("SaveCarteraSnapshot failed — migration 000042 may not be applied: %v", err)
		}

		// Save again with updated saldos — each row must UPDATE in place.
		batch2 := []domain.CarteraSnapshot{
			makeSnapshot(t, fechaCorte, zonaSnap3, nil, domain.BucketAgingDias0_30, 150.00, 1),
			makeSnapshot(t, fechaCorte, zonaSnap3, &cob1, domain.BucketAgingDias0_30, 250.00, 2),
			makeSnapshot(t, fechaCorte, zonaSnap3, &cob2, domain.BucketAgingDias31_60, 350.00, 3),
		}
		err = repo.SaveCarteraSnapshot(ctx, batch2)
		require.NoError(t, err)

		rows, err := repo.ListRecentSnapshots(ctx, 100)
		require.NoError(t, err)

		var matchingRows []domain.CarteraSnapshot
		for _, r := range rows {
			if r.ZonaClienteID() == zonaSnap3 && r.FechaCorte().Equal(fechaCorte) {
				matchingRows = append(matchingRows, r)
			}
		}

		assert.Len(t, matchingRows, 3,
			"3 distinct keys → exactly 3 rows after 2 saves, got %d", len(matchingRows))
		t.Logf("BatchIdempotency ok: %d rows after 2 saves for zona=%d", len(matchingRows), zonaSnap3)
	})
}

// TestCarteraRepo_ListRecentSnapshots_Limit verifies that the limit parameter
// caps the number of rows returned.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestCarteraRepo_ListRecentSnapshots_Limit(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := analyticsfb.NewRepo(pool)

		fechaCorte := time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC)
		const zonaSnap4 = 9904

		batch := []domain.CarteraSnapshot{
			makeSnapshot(t, fechaCorte, zonaSnap4, nil, domain.BucketAgingDias0_30, 1.00, 1),
			makeSnapshot(t, fechaCorte, zonaSnap4, nil, domain.BucketAgingDias31_60, 2.00, 1),
			makeSnapshot(t, fechaCorte, zonaSnap4, nil, domain.BucketAgingDias61_90, 3.00, 1),
		}
		err := repo.SaveCarteraSnapshot(ctx, batch)
		if err != nil {
			t.Skipf("SaveCarteraSnapshot failed — migration 000042 may not be applied: %v", err)
		}

		rowsLimited, err := repo.ListRecentSnapshots(ctx, 2)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(rowsLimited), 2, "limit=2 must return at most 2 rows")

		rowsAll, err := repo.ListRecentSnapshots(ctx, 0)
		require.NoError(t, err)

		var countAll int
		for _, r := range rowsAll {
			if r.ZonaClienteID() == zonaSnap4 && r.FechaCorte().Equal(fechaCorte) {
				countAll++
			}
		}
		assert.Equal(t, 3, countAll, "limit=0 must return all 3 rows for the test zona")
		t.Logf("ListRecentSnapshots limit ok: limited=%d all_for_zona=%d", len(rowsLimited), countAll)
	})
}
