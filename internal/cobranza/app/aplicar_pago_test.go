//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// TestAplicarPago_MissingDeps verifies that AplicarPago returns an internal
// error (not a nil-deref panic) when the Service is constructed without the
// write-side dependencies. This is the defensive wiring-bug guard that runs
// before any Firebird interaction.
func TestAplicarPago_MissingDeps(t *testing.T) {
	t.Parallel()

	// txMgr is nil → runInTx returns errWriteDepsMissing.
	svc := app.NewService(
		newFakeSaldosRepo(),
		newFakePagosRepo(),
		nil,
		fixedClock{T: time.Now()},
		newFakePagosRecibidosRepo(),
		nil,
		&fakeMicrosipPagoWriter{},
		nil, nil,
		nil, // txMgr nil
	)

	_, err := svc.AplicarPago(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cobranza_write_deps_missing",
		"error should surface as a wiring error")
}

// ─── shared test helpers ──────────────────────────────────────────────────────

// fixedNow is the deterministic clock value used across all AplicarPago tests.
var fixedNow = time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

// pendingPagoID returns a new PagoRecibido pre-inserted into repo. The pago is
// in SincronizacionPendiente state with a valid importe/cargo/cobrador so it
// can proceed through the full AplicarPago flow.
func pendingPagoInRepo(t *testing.T, repo *fakePagosRecibidosRepo) *domain.PagoRecibido {
	t.Helper()
	pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             uuid.New(),
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  fixedNow.Add(-30 * time.Minute),
		CreatedBy:      uuid.New(),
		Now:            fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Insert(context.Background(), pago))
	return pago
}

// validWriterResult returns a MicrosipPagoResult that will pass all
// MarcarAplicada validations.
func validWriterResult() outbound.MicrosipPagoResult {
	return outbound.MicrosipPagoResult{
		DoctoCCID:      9001,
		ImpteDoctoCCID: 9002,
		Folio:          "AB-2026-001",
	}
}

// ─── TestAplicarPago_HappyPath ────────────────────────────────────────────────

func TestAplicarPago_HappyPath(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	writer := &fakeMicrosipPagoWriter{result: validWriterResult()}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	got, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.NoError(t, err)
	require.NotNil(t, got)

	assert.True(t, got.IsAplicada(), "pago should be aplicada after happy path")
	require.NotNil(t, got.DoctoCCID())
	assert.Equal(t, 9001, *got.DoctoCCID())
	require.NotNil(t, got.ImpteDoctoCCID())
	assert.Equal(t, 9002, *got.ImpteDoctoCCID())
	require.NotNil(t, got.Folio())
	assert.Equal(t, "AB-2026-001", *got.Folio())

	assert.Equal(t, 1, writer.callCount, "writer should be called exactly once")
	assert.Equal(t, 1, repo.updateCnt, "repo.Update should be called exactly once")
}

// ─── TestAplicarPago_LockFails ────────────────────────────────────────────────

func TestAplicarPago_LockFails(t *testing.T) {
	t.Parallel()

	lockErr := errors.New("lock_failed")
	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	repo.lockErr = lockErr

	writer := &fakeMicrosipPagoWriter{}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, lockErr)
	assert.Equal(t, 0, writer.callCount, "writer must not be called when lock fails")
}

// ─── TestAplicarPago_FindByIDFails ────────────────────────────────────────────

func TestAplicarPago_FindByIDFails(t *testing.T) {
	t.Parallel()

	findErr := errors.New("find_by_id_failed")
	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	// Lock succeeds (row exists); Find fails.
	repo.findErr = findErr

	writer := &fakeMicrosipPagoWriter{}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, findErr)
	assert.Equal(t, 0, writer.callCount, "writer must not be called when FindByID fails")
}

// ─── TestAplicarPago_IdempotentFastPath ───────────────────────────────────────

func TestAplicarPago_IdempotentFastPath(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()

	// Build a pago that is already aplicada via Hydrate so we bypass domain
	// validation (HydratePagoRecibido is the repo's reconstruction path).
	id := uuid.New()
	doctoCCID := 7001
	impteDoctoCCID := 7002
	folio := "PREV-FOLIO"
	aplicadoAt := fixedNow.Add(-1 * time.Minute)

	pago := domain.HydratePagoRecibido(domain.HydratePagoRecibidoParams{
		ID:             id,
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		ConceptoCCID:   87327,
		FechaHoraPago:  fixedNow.Add(-30 * time.Minute),
		Sincronizacion: domain.SincronizacionAplicada,
		DoctoCCID:      &doctoCCID,
		ImpteDoctoCCID: &impteDoctoCCID,
		Folio:          &folio,
		ReceivedAt:     fixedNow.Add(-2 * time.Minute),
		AplicadoAt:     &aplicadoAt,
		CreatedAt:      fixedNow.Add(-2 * time.Minute),
		UpdatedAt:      aplicadoAt,
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	require.NoError(t, repo.Insert(context.Background(), pago))

	writer := &fakeMicrosipPagoWriter{}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	got, err := svc.AplicarPago(context.Background(), id, by)

	require.NoError(t, err)
	require.NotNil(t, got)
	assert.True(t, got.IsAplicada())
	assert.Equal(t, 0, writer.callCount, "writer must not be called for already-aplicada pago")
	assert.Equal(t, 0, repo.updateCnt, "Update must not be called for already-aplicada pago")
}

// ─── TestAplicarPago_PreconditionFails ────────────────────────────────────────

func TestAplicarPago_PreconditionFails(t *testing.T) {
	t.Parallel()

	// PreconditionForAplicar returns ErrPagoYaAplicado when IsAplicada() is true.
	// We build a pago that IsAplicada via Hydrate, and put it in the repo.
	// Because IsAplicada() is checked first in AplicarPago (idempotent fast-path
	// returns early), to hit PreconditionForAplicar we need an invalid
	// Sincronizacion that is neither pendiente nor aplicada.
	//
	// However, Sincronizacion is a string type — we can Hydrate with an
	// unrecognized value. The idempotent check (IsAplicada = sincronizacion=="aplicada")
	// is false, so execution falls through to PreconditionForAplicar, which
	// calls sincronizacion.IsValid() and returns ErrSincronizacionInvalida.

	repo := newFakePagosRecibidosRepo()
	id := uuid.New()

	pago := domain.HydratePagoRecibido(domain.HydratePagoRecibidoParams{
		ID:             id,
		CargoDoctoCCID: 5000,
		ClienteID:      100,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		ConceptoCCID:   87327,
		FechaHoraPago:  fixedNow.Add(-30 * time.Minute),
		Sincronizacion: domain.Sincronizacion("invalida"), // neither pendiente nor aplicada
		ReceivedAt:     fixedNow,
		CreatedAt:      fixedNow,
		UpdatedAt:      fixedNow,
		CreatedBy:      uuid.New(),
		UpdatedBy:      uuid.New(),
	})
	require.NoError(t, repo.Insert(context.Background(), pago))

	writer := &fakeMicrosipPagoWriter{}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), id, by)

	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrSincronizacionInvalida)
	assert.Equal(t, 0, writer.callCount, "writer must not be called when precondition fails")
}

// ─── TestAplicarPago_WriterFails_PersistOK ────────────────────────────────────

func TestAplicarPago_WriterFails_PersistOK(t *testing.T) {
	t.Parallel()

	writerErr := errors.New("microsip_down")
	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)

	writer := &fakeMicrosipPagoWriter{err: writerErr}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	// AplicarPago propagates the original writer error.
	require.Error(t, err)
	require.ErrorIs(t, err, writerErr)

	// Update was called once to persist the failure record.
	assert.Equal(t, 1, repo.updateCnt, "Update must be called once to persist the failure")

	// The stored pago should reflect RegistrarFallo: still pendiente, intentos=1.
	stored, findErr := repo.FindByID(context.Background(), pago.ID())
	require.NoError(t, findErr)
	assert.True(t, stored.IsPendiente(), "pago must stay pendiente after writer failure")
	assert.Equal(t, 1, stored.Intentos(), "intentos must be incremented to 1")
	require.NotNil(t, stored.UltimoError())
	assert.Contains(t, *stored.UltimoError(), "microsip_down")
}

// ─── TestAplicarPago_WriterFails_PersistFails ─────────────────────────────────

func TestAplicarPago_WriterFails_PersistFails(t *testing.T) {
	t.Parallel()

	writerErr := errors.New("microsip_down")
	updateErr := errors.New("db_write_failed")

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	// updateErr is set so that the persist-failure Update also fails.
	repo.updateErr = updateErr

	writer := &fakeMicrosipPagoWriter{err: writerErr}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	// Both errors must be reachable via errors.Is (wrapped via errors.Join).
	require.ErrorIs(t, err, writerErr)
	require.ErrorIs(t, err, updateErr)
}

// ─── TestAplicarPago_MarcarAplicada_FolioEmpty ────────────────────────────────

func TestAplicarPago_MarcarAplicada_FolioEmpty(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)

	// Writer returns an empty folio — MarcarAplicada must reject it.
	writer := &fakeMicrosipPagoWriter{result: outbound.MicrosipPagoResult{
		DoctoCCID:      9001,
		ImpteDoctoCCID: 9002,
		Folio:          "",
	}}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrPagoFolioRequerido)
	// Update must NOT be called — failure happens in MarcarAplicada, before Update.
	assert.Equal(t, 0, repo.updateCnt, "Update must not be called when MarcarAplicada fails")
}

// ─── TestAplicarPago_MarcarAplicada_FolioTooLong ──────────────────────────────

func TestAplicarPago_MarcarAplicada_FolioTooLong(t *testing.T) {
	t.Parallel()

	// maxFolioLength is 20 runes (from pago_recibido.go); 21 runes must fail.
	longFolio := strings.Repeat("X", 21)

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)

	writer := &fakeMicrosipPagoWriter{result: outbound.MicrosipPagoResult{
		DoctoCCID:      9001,
		ImpteDoctoCCID: 9002,
		Folio:          longFolio,
	}}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrPagoFolioDemasiadoLargo)
	assert.Equal(t, 0, repo.updateCnt, "Update must not be called when MarcarAplicada fails")
}

// ─── TestAplicarPago_MarcarAplicada_DoctoCCIDNonPositive ──────────────────────

func TestAplicarPago_MarcarAplicada_DoctoCCIDNonPositive(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)

	writer := &fakeMicrosipPagoWriter{result: outbound.MicrosipPagoResult{
		DoctoCCID:      0, // non-positive → invalid
		ImpteDoctoCCID: 9002,
		Folio:          "AB-2026-001",
	}}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrPagoDoctoCCIDInvalido)
	assert.Equal(t, 0, repo.updateCnt)
}

// ─── TestAplicarPago_MarcarAplicada_ImpteDoctoCCIDNonPositive ─────────────────

func TestAplicarPago_MarcarAplicada_ImpteDoctoCCIDNonPositive(t *testing.T) {
	t.Parallel()

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)

	writer := &fakeMicrosipPagoWriter{result: outbound.MicrosipPagoResult{
		DoctoCCID:      9001,
		ImpteDoctoCCID: 0, // non-positive → invalid
		Folio:          "AB-2026-001",
	}}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, domain.ErrPagoImpteDoctoCCIDInvalido)
	assert.Equal(t, 0, repo.updateCnt)
}

// ─── TestAplicarPago_PostMarcarUpdateFails ────────────────────────────────────

func TestAplicarPago_PostMarcarUpdateFails(t *testing.T) {
	t.Parallel()

	updateErr := errors.New("db_write_failed_post_marcar")

	repo := newFakePagosRecibidosRepo()
	pago := pendingPagoInRepo(t, repo)
	// updateErr is set after Insert so the final persist-success Update fails.
	repo.updateErr = updateErr

	writer := &fakeMicrosipPagoWriter{result: validWriterResult()}
	by := uuid.New()

	svc := newAplicarSvc(t, fakeTxRunner{}, repo, writer, fixedNow)

	_, err := svc.AplicarPago(context.Background(), pago.ID(), by)

	require.Error(t, err)
	require.ErrorIs(t, err, updateErr)
	// Writer was called (flow reached that point successfully).
	assert.Equal(t, 1, writer.callCount)
}
