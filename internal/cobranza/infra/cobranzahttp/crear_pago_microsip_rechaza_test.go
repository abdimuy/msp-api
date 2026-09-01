//nolint:misspell // Spanish domain vocabulary (pago, cobranza, censo) by project convention.
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
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// errMicrosipRechaza is what the Microsip writer double returns when it
// refuses the pago. A plain error on purpose: it is what the port promises,
// and mapAppError turns anything that is not an *apperror.Error into a 500.
var errMicrosipRechaza = errors.New("microsip: DOCTO_CC rechazado por el servidor")

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
	pagosAntes := maps.Clone(r.pagos.rows)
	imagenesAntes := maps.Clone(r.imagenes.images)
	porPagoAntes := maps.Clone(r.imagenes.byPago)
	if err := fn(ctx); err != nil {
		r.pagos.rows = pagosAntes
		r.imagenes.images = imagenesAntes
		r.imagenes.byPago = porPagoAntes
		return err
	}
	return nil
}

// HasTx satisfies cobranzaapp.TxRunner. Like fakeTxRunner, this double runs
// fn on the caller's context and never plants a transaction key on it.
func (r rollbackFakeTxRunner) HasTx(context.Context) bool { return false }

var _ cobranzaapp.TxRunner = rollbackFakeTxRunner{}

// ─── Census ──────────────────────────────────────────────────────────────────

// censoFakes counts, store by store, everything a pago creation can write.
// Named after the real tables so a failure message points at the row that
// should not be there.
type censoFakes struct {
	pagosRecibidos int
	pagosImagenes  int
	blobs          int
}

func tomarCensoFakes(
	pagos *fakePagosRecibidosRepo, imagenes *fakePagosImagenesRepo, store *fakeStorageProvider,
) censoFakes {
	return censoFakes{
		pagosRecibidos: len(pagos.rows),
		pagosImagenes:  len(imagenes.images),
		blobs:          len(store.objects),
	}
}

// assertCensoFakesIgual compares two censuses store by store, naming the
// store in the failure message.
func assertCensoFakesIgual(t *testing.T, antes, despues censoFakes) {
	t.Helper()
	assert.Equal(t, antes.pagosRecibidos, despues.pagosRecibidos,
		"MSP_PAGOS_RECIBIDOS: antes=%d despues=%d", antes.pagosRecibidos, despues.pagosRecibidos)
	assert.Equal(t, antes.pagosImagenes, despues.pagosImagenes,
		"MSP_PAGOS_IMAGENES: antes=%d despues=%d", antes.pagosImagenes, despues.pagosImagenes)
	assert.Equal(t, antes.blobs, despues.blobs,
		"blobs en disco: antes=%d despues=%d", antes.blobs, despues.blobs)
}

// ─── Service wiring ──────────────────────────────────────────────────────────

// rechazoSvc wires the same Service happyPathSvc builds, with two changes:
// the Microsip writer rejects, and the TxRunner honours the rollback.
func rechazoSvc(t *testing.T, now time.Time) (
	*cobranzaapp.Service, *fakePagosRecibidosRepo, *fakePagosImagenesRepo, *fakeStorageProvider,
) {
	t.Helper()
	saldos := newFakeSaldosRepoHTTP()
	s := makeSaldoHTTP(5000, decimal.NewFromInt(2000))
	saldos.byCargo[5000] = &s
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	writer := &fakeMicrosipPagoWriter{err: errMicrosipRechaza}
	svc := buildTestService(
		now, saldos, pagosRepo, imagenes, writer, store, nil,
		rollbackFakeTxRunner{pagos: pagosRepo, imagenes: imagenes},
	)
	return svc, pagosRepo, imagenes, store
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
	svc, pagosRepo, imagenes, store := rechazoSvc(t, now)

	antes := tomarCensoFakes(pagosRepo, imagenes, store)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.GreaterOrEqual(t, rec.Code, http.StatusBadRequest,
		"un rechazo de Microsip no puede contestar 2xx; body=%s", rec.Body.String())
	assert.Equal(t, http.StatusInternalServerError, rec.Code,
		"un error liso del escritor cae en la rama por defecto de mapAppError")

	assertCensoFakesIgual(t, antes, tomarCensoFakes(pagosRepo, imagenes, store))
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
	svc, pagosRepo, imagenes, store := rechazoSvc(t, now)

	antes := tomarCensoFakes(pagosRepo, imagenes, store)

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

	assertCensoFakesIgual(t, antes, tomarCensoFakes(pagosRepo, imagenes, store))
	assert.Empty(t, pagosRepo.rows, "el pago rechazado no puede sobrevivir en MSP_PAGOS_RECIBIDOS")
	assert.Empty(t, imagenes.images, "sin pago no puede quedar ninguna imagen")
	assert.Equal(t, 2, store.storeCalls, "los dos blobs se escriben antes de abrir la transacción")
	assert.Equal(t, 2, store.deleteCalls, "y los dos se borran al deshacerse la creación")
}

// TestHTTP_CrearPago_MicrosipAcepta_Sigue200 is the control for the two
// above: with the very same wiring and a writer that accepts, the endpoint
// still answers 200 (not 201 — routes.go pins DefaultStatus to OK) and the
// pago lands applied. Without it, a change that made CrearPago fail for any
// reason would leave the rejection tests green.
func TestHTTP_CrearPago_MicrosipAcepta_Sigue200(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	saldos := newFakeSaldosRepoHTTP()
	s := makeSaldoHTTP(5000, decimal.NewFromInt(2000))
	saldos.byCargo[5000] = &s
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	writer := &fakeMicrosipPagoWriter{
		result: outbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "Z001"},
	}
	svc := buildTestService(
		now, saldos, pagosRepo, imagenes, writer, store, nil,
		rollbackFakeTxRunner{pagos: pagosRepo, imagenes: imagenes},
	)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "el éxito sigue siendo 200; body=%s", rec.Body.String())
	require.Len(t, pagosRepo.rows, 1)

	stored, err := pagosRepo.FindByID(context.Background(), pagoID)
	require.NoError(t, err)
	assert.True(t, stored.IsAplicada(), "el pago aceptado queda aplicado, no pendiente")
}
