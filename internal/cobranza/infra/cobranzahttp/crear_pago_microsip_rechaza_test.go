//nolint:misspell // Spanish domain vocabulary (pago, cobranza) by project convention.
package cobranzahttp_test

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
)

// errMicrosipRejected is what the Microsip writer double returns when it
// refuses the pago. A plain error on purpose: it is what the port promises,
// and it pins the status these tests expect — mapAppError turns anything that
// is not an *apperror.Error into a 500 through its default branch
// (auth.go:64). The contract under test is "not 2xx"; the exact code is
// asserted so a change in that mapping is deliberate rather than silent.
var errMicrosipRejected = errors.New("microsip: DOCTO_CC rechazado por el servidor")

// ─── Rollback-modelling TxRunner ─────────────────────────────────────────────

// rollbackFakeTxRunner is fakeTxRunner plus the one thing that matters here:
// a real transaction boundary. When fn fails, everything the closure wrote to
// the in-memory repos is undone, exactly as Firebird undoes it in production.
//
// fakeTxRunner cannot stand in for these tests. It runs fn straight through,
// so a rejected pago would keep its row in the fake repo and a census taken
// afterwards would report the bug as if it were the fix.
//
// A shallow clone of the maps is enough: the writer rejects BEFORE
// MarcarAplicada mutates the aggregate, and these tests assert row counts,
// not field state.
type rollbackFakeTxRunner struct {
	pagos    *fakePagosRecibidosRepo
	imagenes *fakePagosImagenesRepo
}

func (r rollbackFakeTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	pagosBefore := maps.Clone(r.pagos.rows)
	imagenesBefore := maps.Clone(r.imagenes.images)
	byPagoBefore := maps.Clone(r.imagenes.byPago)
	if err := fn(ctx); err != nil {
		r.pagos.rows = pagosBefore
		r.imagenes.images = imagenesBefore
		r.imagenes.byPago = byPagoBefore
		return err
	}
	return nil
}

// HasTx satisfies cobranzaapp.TxRunner. Like fakeTxRunner, this double runs
// fn on the caller's context and never plants a transaction key on it.
func (r rollbackFakeTxRunner) HasTx(context.Context) bool { return false }

var _ cobranzaapp.TxRunner = rollbackFakeTxRunner{}

// ─── Census ──────────────────────────────────────────────────────────────────

// fakeStoreCounts counts, store by store, everything a pago creation can
// write. Named after the real tables so a failure message points at the row
// that should not be there.
type fakeStoreCounts struct {
	pagosRecibidos int
	pagosImagenes  int
	blobs          int
}

func snapshotFakeCounts(
	pagos *fakePagosRecibidosRepo, imagenes *fakePagosImagenesRepo, store *fakeStorageProvider,
) fakeStoreCounts {
	return fakeStoreCounts{
		pagosRecibidos: len(pagos.rows),
		pagosImagenes:  len(imagenes.images),
		blobs:          len(store.objects),
	}
}

// assertFakeCountsUnchanged compares two censuses store by store, naming the
// store in the failure message.
func assertFakeCountsUnchanged(t *testing.T, before, after fakeStoreCounts) {
	t.Helper()
	assert.Equal(t, before.pagosRecibidos, after.pagosRecibidos,
		"MSP_PAGOS_RECIBIDOS: antes=%d despues=%d", before.pagosRecibidos, after.pagosRecibidos)
	assert.Equal(t, before.pagosImagenes, after.pagosImagenes,
		"MSP_PAGOS_IMAGENES: antes=%d despues=%d", before.pagosImagenes, after.pagosImagenes)
	assert.Equal(t, before.blobs, after.blobs,
		"blobs en disco: antes=%d despues=%d", before.blobs, after.blobs)
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestHTTP_CrearPago_MicrosipRechaza_SinImagenes_NoDevuelve2xx locks the HTTP
// half of the contract on the path the phone actually uses: POST /pagos with
// no `imagen` parts (len(imgs)==0 — there is no second, non-multipart route).
//
// What it detects: a Microsip rejection answered with 2xx. That is the
// incident this branch exists for. The phone hears "cobrado", the row is
// nowhere Microsip can see it, and the next cobrador charges the client
// again. The response must be >= 400 and MSP_PAGOS_RECIBIDOS must be left
// exactly as it was found.
func TestHTTP_CrearPago_MicrosipRechaza_SinImagenes_NoDevuelve2xx(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := buildPagoSvc(t, now, pagoSvcOpts{
		writerErr: errMicrosipRejected, rollback: true,
	})

	before := snapshotFakeCounts(pagosRepo, imagenes, store)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest,
		"un rechazo de Microsip no puede contestar 2xx; body=%s", rec.Body.String())
	assert.Equal(t, http.StatusInternalServerError, rec.Code, "ver errMicrosipRejected")

	assertFakeCountsUnchanged(t, before, snapshotFakeCounts(pagosRepo, imagenes, store))
	assert.Empty(t, pagosRepo.rows, "el pago rechazado no puede sobrevivir en MSP_PAGOS_RECIBIDOS")

	_, err := pagosRepo.FindByID(context.Background(), pagoID)
	assert.ErrorIs(t, err, domain.ErrPagoNoEncontrado,
		"el id del pago rechazado no debe existir")
}

// TestHTTP_CrearPago_MicrosipRechaza_ConImagenes_NoDevuelve2xx is the same
// contract with comprobantes attached, and adds what only this path can lose:
// the blobs. They are written to disk BEFORE the transaction opens, so a
// rejection that forgot to clean them up would leave orphan files pointing at
// a pago that never existed.
func TestHTTP_CrearPago_MicrosipRechaza_ConImagenes_NoDevuelve2xx(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := buildPagoSvc(t, now, pagoSvcOpts{
		writerErr: errMicrosipRejected, rollback: true,
	})

	before := snapshotFakeCounts(pagosRepo, imagenes, store)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
		{Filename: "a.pdf", Mime: "application/pdf", Body: []byte("AAA"), ID: uuid.New().String(), Descripcion: "uno"},
		{Filename: "b.pdf", Mime: "application/pdf", Body: []byte("BBB"), ID: uuid.New().String(), Descripcion: "dos"},
	})

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest,
		"un rechazo de Microsip no puede contestar 2xx; body=%s", rec.Body.String())
	assert.Equal(t, http.StatusInternalServerError, rec.Code, "ver errMicrosipRejected")

	assertFakeCountsUnchanged(t, before, snapshotFakeCounts(pagosRepo, imagenes, store))
	assert.Empty(t, pagosRepo.rows, "el pago rechazado no puede sobrevivir en MSP_PAGOS_RECIBIDOS")
	assert.Empty(t, imagenes.images, "sin pago no puede quedar ninguna imagen")
	assert.Equal(t, 2, store.storeCalls, "los dos blobs se escriben antes de abrir la transacción")
	assert.Equal(t, 2, store.deleteCalls, "y los dos se borran al deshacerse la creación")
}

// TestHTTP_CrearPago_MicrosipAcepta_ConRollbackRunner_Sigue200 is the control
// for the two above. It is NOT a second copy of
// TestHTTP_CrearPago_Multipart_HappyPath_SinImagenes: what it adds is that
// rollbackFakeTxRunner — the double those two depend on — does not undo a
// creation that SUCCEEDED, and that the pago lands applied rather than
// pending. Without it, a runner that restored its snapshot unconditionally
// would leave both rejection tests green while quietly proving nothing.
func TestHTTP_CrearPago_MicrosipAcepta_ConRollbackRunner_Sigue200(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, _, _ := buildPagoSvc(t, now, pagoSvcOpts{rollback: true})

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "el éxito sigue siendo 200; body=%s", rec.Body.String())
	require.Len(t, pagosRepo.rows, 1, "el runner no puede deshacer una creación exitosa")

	stored, err := pagosRepo.FindByID(context.Background(), pagoID)
	require.NoError(t, err)
	assert.True(t, stored.IsAplicada(), "el pago aceptado queda aplicado, no pendiente")
}
