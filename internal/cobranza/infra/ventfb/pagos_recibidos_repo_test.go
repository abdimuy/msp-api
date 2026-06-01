//nolint:misspell // Spanish vocabulary (pago, cobrador, importe, folio, etc.) by project convention.
package ventfb_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	cobranzaventfb "github.com/abdimuy/msp-api/internal/cobranza/infra/ventfb"
	"github.com/abdimuy/msp-api/internal/platform/fbtestutil"
	"github.com/abdimuy/msp-api/internal/platform/firebird"
)

// ─── Test helpers ─────────────────────────────────────────────────────────────

// buildValidPagoRecibido constructs a fresh, valid PagoRecibido using
// NewPagoRecibido with realistic Mexican Spanish data. Each call creates a new
// random UUID so multiple instances never collide within the same transaction.
func buildValidPagoRecibido(t *testing.T) *domain.PagoRecibido {
	t.Helper()
	now := time.Now().UTC()
	p, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             uuid.New(),
		CargoDoctoCCID: 1001,
		ClienteID:      11486,
		CobradorID:     42,
		Cobrador:       "Ramírez García, Jorge",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   87327, // cobranza ruta
		FechaHoraPago:  now,
		Lat:            nil,
		Lon:            nil,
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	require.NoError(t, err, "buildValidPagoRecibido: NewPagoRecibido must not fail")
	return p
}

// buildPagoRecibidoWithCoords constructs a PagoRecibido with non-nil lat/lon.
func buildPagoRecibidoWithCoords(t *testing.T) *domain.PagoRecibido {
	t.Helper()
	lat := "19.432608"
	lon := "-99.133209"
	now := time.Now().UTC()
	p, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             uuid.New(),
		CargoDoctoCCID: 1001,
		ClienteID:      11486,
		CobradorID:     42,
		Cobrador:       "Hernández López, Ana",
		Importe:        decimal.NewFromInt(2000),
		FormaCobroID:   87327,
		FechaHoraPago:  now,
		Lat:            &lat,
		Lon:            &lon,
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	require.NoError(t, err, "buildPagoRecibidoWithCoords: NewPagoRecibido must not fail")
	return p
}

// buildImagen constructs a fresh domain.Imagen attached to the given pago.
// Returns the Imagen so callers can assert on it.
func buildAndAttachImagen(t *testing.T, pago *domain.PagoRecibido, storageKey string) *domain.Imagen {
	t.Helper()
	storage, err := domain.NewImagenStorage(domain.StorageKindFilesystem, storageKey)
	require.NoError(t, err, "buildImagen: NewImagenStorage")
	desc := "Comprobante de pago"
	img, err := pago.AdjuntarImagen(domain.AdjuntarImagenParams{
		ID:          uuid.New(),
		Storage:     storage,
		Mime:        domain.MimeJPEG,
		SizeBytes:   102400,
		Descripcion: &desc,
		By:          uuid.New(),
		Now:         time.Now().UTC(),
	})
	require.NoError(t, err, "buildImagen: AdjuntarImagen")
	return img
}

// directCountPago returns the number of rows in MSP_PAGOS_RECIBIDOS with the
// given ID. Used for raw-SQL state verification.
func directCountPago(ctx context.Context, t *testing.T, q firebird.Querier, id uuid.UUID) int {
	t.Helper()
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`,
		id.String(),
	).Scan(&n)
	require.NoError(t, err, "directCountPago: SELECT COUNT")
	return n
}

// directCountImagen returns the number of rows in MSP_PAGOS_IMAGENES with the
// given ID.
func directCountImagen(ctx context.Context, t *testing.T, q firebird.Querier, id uuid.UUID) int {
	t.Helper()
	var n int
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM MSP_PAGOS_IMAGENES WHERE ID = ?`,
		id.String(),
	).Scan(&n)
	require.NoError(t, err, "directCountImagen: SELECT COUNT")
	return n
}

// newPagosRecibidosRepo is a convenience helper that builds the repo under test.
func newPagosRecibidosRepo(pool *firebird.Pool) *cobranzaventfb.PagosRecibidosRepo {
	return cobranzaventfb.NewPagosRecibidosRepo(pool)
}

// ─── Insert tests ─────────────────────────────────────────────────────────────

// TestE2E_PagosRecibidos_Insert_HappyPath verifies that Insert creates a row
// in MSP_PAGOS_RECIBIDOS with ESTADO='P' and INTENTOS=0.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_Insert_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)

		err := repo.Insert(ctx, pago)
		require.NoError(t, err, "Insert must succeed for a valid new pago")

		q := firebird.GetQuerier(ctx, pool.DB)
		n := directCountPago(ctx, t, q, pago.ID())
		assert.Equal(t, 1, n, "exactly one row must exist after Insert")

		// Verify ESTADO and INTENTOS via direct SELECT.
		var estado string
		var intentos int
		err = q.QueryRowContext(ctx,
			`SELECT ESTADO, INTENTOS FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`,
			pago.ID().String(),
		).Scan(&estado, &intentos)
		require.NoError(t, err)
		assert.Equal(t, "P", estado, "newly inserted pago must have ESTADO='P'")
		assert.Equal(t, 0, intentos, "newly inserted pago must have INTENTOS=0")
	})
}

// TestE2E_PagosRecibidos_Insert_DuplicateKey verifies that inserting the same
// pago twice returns domain.ErrPagoYaExiste.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_Insert_DuplicateKey(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)

		require.NoError(t, repo.Insert(ctx, pago), "first Insert must succeed")

		err := repo.Insert(ctx, pago)
		require.Error(t, err, "second Insert with same UUID must fail")
		require.ErrorIs(t, err, domain.ErrPagoYaExiste,
			"duplicate Insert must return ErrPagoYaExiste; got: %v", err)
	})
}

// TestE2E_PagosRecibidos_Insert_AllNullableColumnsNULL verifies that a pago
// built without Lat/Lon (nil) is persisted with those columns as SQL NULL.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_Insert_AllNullableColumnsNULL(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t) // lat=nil, lon=nil by default

		require.NoError(t, repo.Insert(ctx, pago))

		q := firebird.GetQuerier(ctx, pool.DB)
		var lat, lon *string
		var aplicadoAt *string // raw scan as nullable string to confirm NULL
		err := q.QueryRowContext(ctx,
			`SELECT LAT, LON, APLICADO_AT FROM MSP_PAGOS_RECIBIDOS WHERE ID = ?`,
			pago.ID().String(),
		).Scan(&lat, &lon, &aplicadoAt)
		require.NoError(t, err)
		assert.Nil(t, lat, "LAT must be NULL when not provided")
		assert.Nil(t, lon, "LON must be NULL when not provided")
		assert.Nil(t, aplicadoAt, "APLICADO_AT must be NULL for a pendiente pago")
	})
}

// ─── Update tests ─────────────────────────────────────────────────────────────

// TestE2E_PagosRecibidos_Update_HappyPath_MarcarAplicada inserts a pago then
// calls MarcarAplicada + Update and verifies the persisted state via FindByID.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_Update_HappyPath_MarcarAplicada(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		now := time.Now().UTC()
		by := uuid.New()
		const wantDoctoCCID = 123456
		const wantImpteDoctoCCID = 789012
		const wantFolio = "PG-0001"
		require.NoError(t, pago.MarcarAplicada(wantDoctoCCID, wantImpteDoctoCCID, wantFolio, now, by))

		require.NoError(t, repo.Update(ctx, pago), "Update after MarcarAplicada must succeed")

		found, err := repo.FindByID(ctx, pago.ID())
		require.NoError(t, err)

		assert.Equal(t, domain.SincronizacionAplicada, found.Sincronizacion(),
			"state must be aplicada after MarcarAplicada")
		require.NotNil(t, found.DoctoCCID(), "DoctoCCID must not be nil after aplicar")
		assert.Equal(t, wantDoctoCCID, *found.DoctoCCID())
		require.NotNil(t, found.ImpteDoctoCCID(), "ImpteDoctoCCID must not be nil after aplicar")
		assert.Equal(t, wantImpteDoctoCCID, *found.ImpteDoctoCCID())
		require.NotNil(t, found.Folio(), "Folio must not be nil after aplicar")
		assert.Equal(t, wantFolio, *found.Folio())
		assert.NotNil(t, found.AplicadoAt(), "AplicadoAt must not be nil after aplicar")
	})
}

// TestE2E_PagosRecibidos_Update_HappyPath_RegistrarFallo inserts a pago,
// registers one failure, and verifies the persisted INTENTOS and ULTIMO_ERROR.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_Update_HappyPath_RegistrarFallo(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		const errMsg = "microsip_timeout"
		now := time.Now().UTC()
		by := uuid.New()
		pago.RegistrarFallo(errMsg, now, by)

		require.NoError(t, repo.Update(ctx, pago), "Update after RegistrarFallo must succeed")

		found, err := repo.FindByID(ctx, pago.ID())
		require.NoError(t, err)

		assert.Equal(t, domain.SincronizacionPendiente, found.Sincronizacion(),
			"state must stay pendiente after RegistrarFallo")
		assert.Equal(t, 1, found.Intentos(), "INTENTOS must be 1 after first failure")
		require.NotNil(t, found.UltimoError(), "ULTIMO_ERROR must not be nil after RegistrarFallo")
		assert.Equal(t, errMsg, *found.UltimoError())
	})
}

// TestE2E_PagosRecibidos_Update_NotFound verifies that updating a pago that
// was never inserted returns ErrPagoNoEncontrado.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_Update_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		// Build a pago but do NOT insert it.
		pago := buildValidPagoRecibido(t)

		// Dirty the domain object so Update produces a real SQL UPDATE.
		now := time.Now().UTC()
		by := uuid.New()
		require.NoError(t, pago.MarcarAplicada(1, 2, "PG-GHOST", now, by))

		err := repo.Update(ctx, pago)
		require.ErrorIs(t, err, domain.ErrPagoNoEncontrado,
			"Update on a non-existent pago must return ErrPagoNoEncontrado; got: %v", err)
	})
}

// ─── LockByID tests ───────────────────────────────────────────────────────────

// TestE2E_PagosRecibidos_LockByID_HappyPath verifies that locking an existing
// pago inside the rollback-only tx does not error.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_LockByID_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		err := repo.LockByID(ctx, pago.ID())
		require.NoError(t, err, "LockByID on an existing pago must succeed")
	})
}

// TestE2E_PagosRecibidos_LockByID_NotFound verifies that locking a non-existent
// pago returns ErrPagoNoEncontrado.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_LockByID_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		err := repo.LockByID(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrPagoNoEncontrado,
			"LockByID for unknown UUID must return ErrPagoNoEncontrado; got: %v", err)
	})
}

// ─── FindByID tests ───────────────────────────────────────────────────────────

// TestE2E_PagosRecibidos_FindByID_HappyPath_NoImagenes inserts a pago and
// reads it back, asserting field round-trips.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_FindByID_HappyPath_NoImagenes(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildPagoRecibidoWithCoords(t)
		require.NoError(t, repo.Insert(ctx, pago))

		found, err := repo.FindByID(ctx, pago.ID())
		require.NoError(t, err)

		assert.Equal(t, pago.ID(), found.ID())
		assert.Equal(t, pago.CargoDoctoCCID(), found.CargoDoctoCCID())
		assert.Equal(t, pago.ClienteID(), found.ClienteID())
		assert.Equal(t, pago.CobradorID(), found.CobradorID())
		assert.Equal(t, pago.Cobrador(), found.Cobrador())
		assert.True(t, pago.Importe().Equal(found.Importe()),
			"Importe mismatch: want=%s got=%s", pago.Importe(), found.Importe())
		assert.Equal(t, pago.FormaCobroID(), found.FormaCobroID())
		assert.Equal(t, domain.SincronizacionPendiente, found.Sincronizacion())
		assert.Equal(t, 0, found.Intentos())
		assert.Equal(t, 0, found.ImagenesCount())

		// lat/lon round-trip.
		require.NotNil(t, found.Lat())
		require.NotNil(t, found.Lon())
		assert.Equal(t, *pago.Lat(), *found.Lat())
		assert.Equal(t, *pago.Lon(), *found.Lon())

		// FechaHoraPago round-trip: Firebird truncates to milliseconds.
		delta := found.FechaHoraPago().Sub(pago.FechaHoraPago())
		if delta < 0 {
			delta = -delta
		}
		assert.Less(t, delta, 2*time.Second, "FechaHoraPago must round-trip within 2s tolerance")
	})
}

// TestE2E_PagosRecibidos_FindByID_WithImagenes inserts a pago + 2 imagenes
// and verifies the aggregate is returned with both imagenes in insert order.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_FindByID_WithImagenes(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		img1 := buildAndAttachImagen(t, pago, "pagos/2026/img1.jpg")
		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img1))

		// Small sleep to ensure CREATED_AT differs between the two images.
		time.Sleep(10 * time.Millisecond)

		img2 := buildAndAttachImagen(t, pago, "pagos/2026/img2.jpg")
		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img2))

		found, err := repo.FindByID(ctx, pago.ID())
		require.NoError(t, err)

		assert.Equal(t, 2, found.ImagenesCount(), "pago must have 2 imagenes")
		imgs := slices.Collect(found.Imagenes())
		require.Len(t, imgs, 2)
		// Order by CREATED_AT ASC: img1 was inserted first.
		assert.Equal(t, img1.ID(), imgs[0].ID(), "first imagen must be img1 (inserted first)")
		assert.Equal(t, img2.ID(), imgs[1].ID(), "second imagen must be img2 (inserted second)")
	})
}

// TestE2E_PagosRecibidos_FindByID_NotFound verifies that looking up a random
// UUID returns ErrPagoNoEncontrado.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_FindByID_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		_, err := repo.FindByID(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrPagoNoEncontrado,
			"FindByID for unknown UUID must return ErrPagoNoEncontrado; got: %v", err)
	})
}

// TestE2E_PagosRecibidos_FindByID_NullableColumnsScanCorrectly inserts a pago
// without lat/lon and verifies all nullable fields come back nil.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_FindByID_NullableColumnsScanCorrectly(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t) // lat=nil, lon=nil
		require.NoError(t, repo.Insert(ctx, pago))

		found, err := repo.FindByID(ctx, pago.ID())
		require.NoError(t, err)

		assert.Nil(t, found.Lat(), "Lat must be nil when not set")
		assert.Nil(t, found.Lon(), "Lon must be nil when not set")
		assert.Nil(t, found.AplicadoAt(), "AplicadoAt must be nil for a pendiente pago")
		assert.Nil(t, found.UltimoError(), "UltimoError must be nil before any failure")
		assert.Nil(t, found.Folio(), "Folio must be nil before apply")
		assert.Nil(t, found.DoctoCCID(), "DoctoCCID must be nil before apply")
		assert.Nil(t, found.ImpteDoctoCCID(), "ImpteDoctoCCID must be nil before apply")
	})
}

// ─── ListPendientes tests ─────────────────────────────────────────────────────

// TestE2E_PagosRecibidos_ListPendientes_Empty verifies that ListPendientes
// returns an empty slice when no rows match.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_ListPendientes_Empty(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		// Use a very restrictive limit/maxIntentos. Any pre-existing rows with
		// INTENTOS >= 0 would be returned so we use maxIntentos=0, which only
		// returns rows with INTENTOS < 0 — impossible, so result is always empty.
		results, err := repo.ListPendientes(ctx, 0, 100)
		require.NoError(t, err)
		assert.Empty(t, results, "ListPendientes with maxIntentos=0 must return empty slice")
	})
}

// TestE2E_PagosRecibidos_ListPendientes_RespectsLimit inserts 5 pendientes and
// verifies that a limit of 3 returns exactly 3 results.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_ListPendientes_RespectsLimit(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		const total = 5
		for i := range total {
			pago := buildValidPagoRecibido(t)
			// Use HydratePagoRecibido to set distinct ReceivedAt timestamps.
			_ = i // iteration var for clarity; UUID uniqueness guarantees no collision
			require.NoError(t, repo.Insert(ctx, pago))
			time.Sleep(5 * time.Millisecond) // ensure distinct RECEIVED_AT
		}

		results, err := repo.ListPendientes(ctx, 10, 3)
		require.NoError(t, err)
		// There may be pre-existing pendiente rows in the DB (other tests' data
		// that share this transaction, though each test creates its own tx). The
		// limit must cap results at 3.
		assert.LessOrEqual(t, len(results), 3, "limit=3 must not return more than 3 rows")
		assert.GreaterOrEqual(t, len(results), 1, "at least 1 pendiente row must exist")
	})
}

// TestE2E_PagosRecibidos_ListPendientes_FiltersByMaxIntentos verifies the
// INTENTOS < maxIntentos filter.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_ListPendientes_FiltersByMaxIntentos(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		// Insert 3 pendientes: 2 will accumulate 2 intentos each.
		pagoFresh := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pagoFresh))

		pagoFailed1 := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pagoFailed1))

		pagoFailed2 := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pagoFailed2))

		// Register 2 failures on each of the "failed" pagos.
		by := uuid.New()
		now := time.Now().UTC()
		for range 2 {
			pagoFailed1.RegistrarFallo("connection refused", now, by)
			pagoFailed2.RegistrarFallo("timeout", now, by)
		}
		require.NoError(t, repo.Update(ctx, pagoFailed1))
		require.NoError(t, repo.Update(ctx, pagoFailed2))

		// maxIntentos=2 → only rows with INTENTOS < 2 qualify → only pagoFresh.
		results2, err := repo.ListPendientes(ctx, 2, 100)
		require.NoError(t, err)
		ids2 := make(map[uuid.UUID]bool)
		for _, p := range results2 {
			ids2[p.ID()] = true
		}
		assert.True(t, ids2[pagoFresh.ID()],
			"pagoFresh (intentos=0) must appear when maxIntentos=2")
		assert.False(t, ids2[pagoFailed1.ID()],
			"pagoFailed1 (intentos=2) must NOT appear when maxIntentos=2")
		assert.False(t, ids2[pagoFailed2.ID()],
			"pagoFailed2 (intentos=2) must NOT appear when maxIntentos=2")

		// maxIntentos=3 → all three qualify (intentos=0 < 3, intentos=2 < 3).
		results3, err := repo.ListPendientes(ctx, 3, 100)
		require.NoError(t, err)
		ids3 := make(map[uuid.UUID]bool)
		for _, p := range results3 {
			ids3[p.ID()] = true
		}
		assert.True(t, ids3[pagoFresh.ID()], "pagoFresh must appear when maxIntentos=3")
		assert.True(t, ids3[pagoFailed1.ID()], "pagoFailed1 must appear when maxIntentos=3")
		assert.True(t, ids3[pagoFailed2.ID()], "pagoFailed2 must appear when maxIntentos=3")
	})
}

// TestE2E_PagosRecibidos_ListPendientes_ExcludesAplicadas verifies that
// ListPendientes never returns aplicadas pagos.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_ListPendientes_ExcludesAplicadas(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		pagoPendiente := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pagoPendiente))

		pagoAplicado := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pagoAplicado))

		// Transition pagoAplicado to state=A.
		now := time.Now().UTC()
		by := uuid.New()
		require.NoError(t, pagoAplicado.MarcarAplicada(555, 666, "PG-APLD", now, by))
		require.NoError(t, repo.Update(ctx, pagoAplicado))

		results, err := repo.ListPendientes(ctx, 10, 100)
		require.NoError(t, err)

		ids := make(map[uuid.UUID]bool)
		for _, p := range results {
			ids[p.ID()] = true
		}
		assert.True(t, ids[pagoPendiente.ID()],
			"pendiente pago must appear in ListPendientes")
		assert.False(t, ids[pagoAplicado.ID()],
			"aplicada pago must NOT appear in ListPendientes")
	})
}

// TestE2E_PagosRecibidos_ListPendientes_OrderByReceivedAtAsc inserts 3 pagos
// and verifies the result is ordered by RECEIVED_AT ascending.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_ListPendientes_OrderByReceivedAtAsc(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		// Insert 3 pagos with a brief delay between each so RECEIVED_AT differs.
		var inserted []*domain.PagoRecibido
		for range 3 {
			pago := buildValidPagoRecibido(t)
			require.NoError(t, repo.Insert(ctx, pago))
			inserted = append(inserted, pago)
			time.Sleep(20 * time.Millisecond)
		}

		results, err := repo.ListPendientes(ctx, 10, 100)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(results), 3,
			"at least 3 results expected; got %d", len(results))

		// Find the 3 inserted pagos in the result slice and verify ascending order.
		findIdx := func(id uuid.UUID) int {
			for i, p := range results {
				if p.ID() == id {
					return i
				}
			}
			return -1
		}
		i0 := findIdx(inserted[0].ID())
		i1 := findIdx(inserted[1].ID())
		i2 := findIdx(inserted[2].ID())
		require.NotEqual(t, -1, i0, "inserted[0] not found in results")
		require.NotEqual(t, -1, i1, "inserted[1] not found in results")
		require.NotEqual(t, -1, i2, "inserted[2] not found in results")
		assert.Less(t, i0, i1, "oldest pago must come before second pago")
		assert.Less(t, i1, i2, "second pago must come before newest pago")
	})
}

// ─── PagosImagenesRepo tests ──────────────────────────────────────────────────

// TestE2E_PagosImagenes_Insert_HappyPath verifies that InsertImagen creates a
// row in MSP_PAGOS_IMAGENES with the correct fields.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_Insert_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		img := buildAndAttachImagen(t, pago, "pagos/2026/comprobante.jpg")
		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img))

		q := firebird.GetQuerier(ctx, pool.DB)
		n := directCountImagen(ctx, t, q, img.ID())
		assert.Equal(t, 1, n, "exactly one imagen row must exist after InsertImagen")

		// Verify columns directly.
		var storageKind, storageKey, mime string
		var sizeBytes int64
		err := q.QueryRowContext(ctx,
			`SELECT STORAGE_KIND, STORAGE_KEY, MIME, SIZE_BYTES FROM MSP_PAGOS_IMAGENES WHERE ID = ?`,
			img.ID().String(),
		).Scan(&storageKind, &storageKey, &mime, &sizeBytes)
		require.NoError(t, err)
		assert.Equal(t, domain.StorageKindFilesystem.String(), storageKind)
		assert.Equal(t, "pagos/2026/comprobante.jpg", storageKey)
		assert.Equal(t, domain.MimeJPEG, mime)
		assert.Equal(t, int64(102400), sizeBytes)
	})
}

// TestE2E_PagosImagenes_FindImagenByID_HappyPath verifies that FindImagenByID
// returns the correct imagen with all fields bit-exact.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_FindImagenByID_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		img := buildAndAttachImagen(t, pago, "pagos/2026/recibo.jpg")
		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img))

		found, err := repo.FindImagenByID(ctx, img.ID())
		require.NoError(t, err)

		assert.Equal(t, img.ID(), found.ID())
		assert.Equal(t, img.Mime(), found.Mime())
		assert.Equal(t, img.SizeBytes(), found.SizeBytes())
		assert.True(t, img.Storage().Equals(found.Storage()),
			"Storage must round-trip: want=%v got=%v", img.Storage(), found.Storage())
		// descripcion round-trip.
		require.NotNil(t, found.Descripcion())
		require.NotNil(t, img.Descripcion())
		assert.Equal(t, *img.Descripcion(), *found.Descripcion())
	})
}

// TestE2E_PagosImagenes_FindImagenByID_NotFound verifies that looking up a
// random UUID returns ErrImagenNoEncontrada.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_FindImagenByID_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		_, err := repo.FindImagenByID(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada,
			"FindImagenByID for unknown UUID must return ErrImagenNoEncontrada; got: %v", err)
	})
}

// TestE2E_PagosImagenes_DeleteImagen_HappyPath verifies that DeleteImagen
// removes the row and a subsequent FindImagenByID returns ErrImagenNoEncontrada.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_DeleteImagen_HappyPath(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		img := buildAndAttachImagen(t, pago, "pagos/2026/a-borrar.jpg")
		require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img))

		require.NoError(t, repo.DeleteImagen(ctx, img.ID()), "DeleteImagen must succeed")

		_, err := repo.FindImagenByID(ctx, img.ID())
		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada,
			"FindImagenByID after DeleteImagen must return ErrImagenNoEncontrada; got: %v", err)
	})
}

// TestE2E_PagosImagenes_DeleteImagen_NotFound verifies that deleting a
// non-existent imagen returns ErrImagenNoEncontrada.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_DeleteImagen_NotFound(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)

		err := repo.DeleteImagen(ctx, uuid.New())
		require.ErrorIs(t, err, domain.ErrImagenNoEncontrada,
			"DeleteImagen for unknown UUID must return ErrImagenNoEncontrada; got: %v", err)
	})
}

// TestE2E_PagosImagenes_ListImagenes_OrderByCreatedAt inserts 3 imagenes with
// sequential CreatedAt values and verifies they are returned in ASC order.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_ListImagenes_OrderByCreatedAt(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		keys := []string{
			"pagos/2026/first.jpg",
			"pagos/2026/second.jpg",
			"pagos/2026/third.jpg",
		}
		var imgIDs []uuid.UUID
		for _, key := range keys {
			img := buildAndAttachImagen(t, pago, key)
			require.NoError(t, repo.InsertImagen(ctx, pago.ID(), img))
			imgIDs = append(imgIDs, img.ID())
			time.Sleep(10 * time.Millisecond) // ensure distinct CREATED_AT
		}

		imgs, err := repo.ListImagenes(ctx, pago.ID())
		require.NoError(t, err)
		require.Len(t, imgs, 3, "ListImagenes must return all 3 inserted imagenes")

		assert.Equal(t, imgIDs[0], imgs[0].ID(), "first imagen (oldest CREATED_AT) must be first")
		assert.Equal(t, imgIDs[1], imgs[1].ID(), "second imagen must be second")
		assert.Equal(t, imgIDs[2], imgs[2].ID(), "third imagen (newest CREATED_AT) must be last")
	})
}

// TestE2E_PagosImagenes_ListImagenes_Empty verifies that ListImagenes returns
// an empty slice when no imagenes have been attached to the pago.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosImagenes_ListImagenes_Empty(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		repo := newPagosRecibidosRepo(pool)
		pago := buildValidPagoRecibido(t)
		require.NoError(t, repo.Insert(ctx, pago))

		imgs, err := repo.ListImagenes(ctx, pago.ID())
		require.NoError(t, err)
		assert.Empty(t, imgs, "ListImagenes must return empty slice when no imagenes exist")
	})
}

// ─── Utility tests ────────────────────────────────────────────────────────────

// TestE2E_PagosRecibidos_UTC_Roundtrip verifies that a FechaHoraPago stored as
// UTC comes back as UTC with no DST drift and sub-second precision within 1ms.
//
//nolint:paralleltest // serial: shares rollback-only tx.
func TestE2E_PagosRecibidos_UTC_Roundtrip(t *testing.T) {
	requireFBEnv(t)
	pool := fbtestutil.NewTestFirebirdPool(t)

	fbtestutil.WithTestTransaction(t, pool, func(ctx context.Context) {
		input := time.Date(2026, 6, 1, 14, 30, 0, 0, time.UTC)

		now := time.Now().UTC()
		pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
			ID:             uuid.New(),
			CargoDoctoCCID: 1001,
			ClienteID:      11486,
			CobradorID:     42,
			Cobrador:       "Martínez Orozco, Luis",
			Importe:        decimal.NewFromInt(750),
			FormaCobroID:   87327,
			FechaHoraPago:  input,
			CreatedBy:      uuid.New(),
			Now:            now,
		})
		require.NoError(t, err)

		repo := newPagosRecibidosRepo(pool)
		require.NoError(t, repo.Insert(ctx, pago))

		found, err := repo.FindByID(ctx, pago.ID())
		require.NoError(t, err)

		got := found.FechaHoraPago().UTC()
		// Firebird stores timestamps at millisecond precision; tolerate 1ms drift.
		delta := got.Sub(input)
		if delta < 0 {
			delta = -delta
		}
		assert.Less(t, delta, time.Millisecond,
			"FechaHoraPago must round-trip within 1ms; want=%v got=%v delta=%v",
			input, got, delta)
		assert.Equal(t, time.UTC, got.Location(),
			"FechaHoraPago must be in UTC after round-trip")
	})
}
