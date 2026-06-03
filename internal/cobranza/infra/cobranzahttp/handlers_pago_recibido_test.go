//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
)

// ─── Compile-time interface checks ────────────────────────────────────────────

var (
	_ outbound.PagosRecibidosRepo = (*fakePagosRecibidosRepo)(nil)
	_ outbound.PagosImagenesRepo  = (*fakePagosImagenesRepo)(nil)
	_ outbound.MicrosipPagoWriter = (*fakeMicrosipPagoWriter)(nil)
	_ outbound.StorageProvider    = (*fakeStorageProvider)(nil)
	_ cobranzaapp.TxRunner        = fakeTxRunner{}
)

// ─── In-memory fakes ──────────────────────────────────────────────────────────

// fakePagosRecibidosRepo is an in-memory PagosRecibidosRepo for HTTP tests.
type fakePagosRecibidosRepo struct {
	rows      map[uuid.UUID]*domain.PagoRecibido
	insertErr error
	findErr   error
	lockErr   error
	updateErr error
	listErr   error
}

func newFakePagosRecibidosRepo() *fakePagosRecibidosRepo {
	return &fakePagosRecibidosRepo{rows: map[uuid.UUID]*domain.PagoRecibido{}}
}

func (f *fakePagosRecibidosRepo) Insert(_ context.Context, p *domain.PagoRecibido) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	if _, exists := f.rows[p.ID()]; exists {
		return domain.ErrPagoYaExiste
	}
	f.rows[p.ID()] = p
	return nil
}

func (f *fakePagosRecibidosRepo) Update(_ context.Context, p *domain.PagoRecibido) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	if _, exists := f.rows[p.ID()]; !exists {
		return domain.ErrPagoNoEncontrado
	}
	f.rows[p.ID()] = p
	return nil
}

func (f *fakePagosRecibidosRepo) FindByID(_ context.Context, id uuid.UUID) (*domain.PagoRecibido, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	p, ok := f.rows[id]
	if !ok {
		return nil, domain.ErrPagoNoEncontrado
	}
	return p, nil
}

func (f *fakePagosRecibidosRepo) LockByID(_ context.Context, id uuid.UUID) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	if _, ok := f.rows[id]; !ok {
		return domain.ErrPagoNoEncontrado
	}
	return nil
}

func (f *fakePagosRecibidosRepo) ListPendientes(_ context.Context, maxIntentos, limit int) ([]*domain.PagoRecibido, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []*domain.PagoRecibido
	for _, p := range f.rows {
		if p.IsPendiente() && p.Intentos() < maxIntentos {
			out = append(out, p)
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// fakePagosImagenesRepo is an in-memory PagosImagenesRepo for HTTP tests.
type fakePagosImagenesRepo struct {
	images    map[uuid.UUID]*domain.Imagen
	byPago    map[uuid.UUID][]uuid.UUID
	insertErr error
	deleteErr error
	findErr   error
}

func newFakePagosImagenesRepo() *fakePagosImagenesRepo {
	return &fakePagosImagenesRepo{
		images: map[uuid.UUID]*domain.Imagen{},
		byPago: map[uuid.UUID][]uuid.UUID{},
	}
}

func (f *fakePagosImagenesRepo) InsertImagen(_ context.Context, pagoID uuid.UUID, img *domain.Imagen) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.images[img.ID()] = img
	f.byPago[pagoID] = append(f.byPago[pagoID], img.ID())
	return nil
}

func (f *fakePagosImagenesRepo) DeleteImagen(_ context.Context, imagenID uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	img, ok := f.images[imagenID]
	if !ok {
		return domain.ErrImagenNoEncontrada
	}
	for pagoID, ids := range f.byPago {
		for i, id := range ids {
			if id == imagenID {
				f.byPago[pagoID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
	}
	delete(f.images, img.ID())
	return nil
}

func (f *fakePagosImagenesRepo) FindImagenByID(_ context.Context, imagenID uuid.UUID) (*domain.Imagen, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	img, ok := f.images[imagenID]
	if !ok {
		return nil, domain.ErrImagenNoEncontrada
	}
	return img, nil
}

func (f *fakePagosImagenesRepo) ListImagenes(_ context.Context, pagoID uuid.UUID) ([]*domain.Imagen, error) {
	ids := f.byPago[pagoID]
	out := make([]*domain.Imagen, 0, len(ids))
	for _, id := range ids {
		if img, ok := f.images[id]; ok {
			out = append(out, img)
		}
	}
	return out, nil
}

// fakeMicrosipPagoWriter satisfies outbound.MicrosipPagoWriter for tests.
type fakeMicrosipPagoWriter struct {
	result outbound.MicrosipPagoResult
	err    error
}

func (f *fakeMicrosipPagoWriter) Aplicar(_ context.Context, _ outbound.MicrosipPagoInput) (outbound.MicrosipPagoResult, error) {
	return f.result, f.err
}

// fakeStorageProvider is an in-memory StorageProvider for HTTP tests.
type fakeStorageProvider struct {
	objects     map[string][]byte
	mimes       map[string]string
	storeErr    error
	getErr      error
	deleteErr   error
	storeCalls  int
	deleteCalls int
}

func newFakeStorageProvider() *fakeStorageProvider {
	return &fakeStorageProvider{
		objects: map[string][]byte{},
		mimes:   map[string]string{},
	}
}

func (f *fakeStorageProvider) Store(_ context.Context, key, contentType string, _ int64, body io.Reader) error {
	f.storeCalls++
	if f.storeErr != nil {
		return f.storeErr
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.objects[key] = data
	f.mimes[key] = contentType
	return nil
}

func (f *fakeStorageProvider) Get(_ context.Context, key string) (outbound.StorageObject, error) {
	if f.getErr != nil {
		return outbound.StorageObject{}, f.getErr
	}
	data, ok := f.objects[key]
	if !ok {
		return outbound.StorageObject{}, fmt.Errorf("key not found: %s", key)
	}
	return outbound.StorageObject{
		Body:        nopCloser{bytes.NewReader(data)},
		ContentType: f.mimes[key],
		SizeBytes:   int64(len(data)),
	}, nil
}

func (f *fakeStorageProvider) Delete(_ context.Context, key string) error {
	f.deleteCalls++
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.objects, key)
	delete(f.mimes, key)
	return nil
}

// nopCloser wraps a reader so it satisfies io.ReadCloser.
type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

// fakeTxRunner is a synchronous TxRunner that executes fn in-process.
type fakeTxRunner struct{ err error }

func (f fakeTxRunner) RunInTx(ctx context.Context, fn func(context.Context) error) error {
	if f.err != nil {
		return f.err
	}
	return fn(ctx)
}

// fakeSaldosRepoHTTP is a minimal SaldosRepo for HTTP handler tests.
type fakeSaldosRepoHTTP struct {
	byCargo map[int]*domain.Saldo
}

func newFakeSaldosRepoHTTP() *fakeSaldosRepoHTTP {
	return &fakeSaldosRepoHTTP{byCargo: map[int]*domain.Saldo{}}
}

func (f *fakeSaldosRepoHTTP) PorCargo(_ context.Context, id int) (*domain.Saldo, error) {
	s, ok := f.byCargo[id]
	if !ok {
		return nil, domain.ErrSaldoNoEncontrado
	}
	return s, nil
}

func (f *fakeSaldosRepoHTTP) PorVenta(_ context.Context, _ int) (*domain.Saldo, error) {
	return nil, domain.ErrSaldoNoEncontrado
}

func (f *fakeSaldosRepoHTTP) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Saldo, error) {
	return nil, nil
}

func (f *fakeSaldosRepoHTTP) AbiertasPorCliente(_ context.Context, _ int) ([]domain.Saldo, error) {
	return nil, nil
}

func (f *fakeSaldosRepoHTTP) ResumenZonas(_ context.Context) ([]domain.ResumenZona, error) {
	return nil, nil
}

func (f *fakeSaldosRepoHTTP) SyncPorZona(_ context.Context, _ int, _ time.Time, _, _ int) (outbound.SyncPage[domain.Saldo], error) {
	return outbound.SyncPage[domain.Saldo]{}, nil
}

func (f *fakeSaldosRepoHTTP) ByIDs(_ context.Context, _ int, _ []int) ([]domain.Saldo, error) {
	return nil, nil
}

// fakePagosRepoHTTP is a minimal PagosRepo for HTTP handler tests.
type fakePagosRepoHTTP struct{}

func (fakePagosRepoHTTP) PorVenta(_ context.Context, _ int) ([]domain.Pago, error) { return nil, nil }

func (fakePagosRepoHTTP) PorCliente(_ context.Context, _ int) ([]domain.Pago, error) {
	return nil, nil
}

func (fakePagosRepoHTTP) EnRutaPorZona(_ context.Context, _ int, _ time.Time) ([]domain.Pago, error) {
	return nil, nil
}

func (fakePagosRepoHTTP) SyncPorZona(_ context.Context, _ int, _ time.Time, _, _ int, _ time.Time) (outbound.SyncPage[domain.Pago], error) {
	return outbound.SyncPage[domain.Pago]{}, nil
}

func (fakePagosRepoHTTP) ByIDs(_ context.Context, _ int, _ []int) ([]domain.Pago, error) {
	return nil, nil
}

// fixedClockHTTP returns a fixed time for the service.
type fixedClockHTTP struct{ t time.Time }

func (c fixedClockHTTP) Now() time.Time { return c.t }

// ─── Router helpers ───────────────────────────────────────────────────────────

// pagoUser builds a CurrentUser with PermCobranzaVerPagos.
func pagoUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-pago",
		Email:       "cobrador@muebleriamsp.mx",
		Nombre:      "Cobrador Test",
		Permisos:    []string{string(authdomain.PermCobranzaVerPagos)},
	}
}

// adminUser builds a CurrentUser with both PermCobranzaVerPagos and PermCobranzaReconciliar.
func adminUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-admin",
		Email:       "admin@muebleriamsp.mx",
		Nombre:      "Admin Test",
		Permisos: []string{
			string(authdomain.PermCobranzaVerPagos),
			string(authdomain.PermCobranzaReconciliar),
		},
	}
}

// noPermUser builds a CurrentUser with no cobranza permissions.
func noPermUser() auth.CurrentUser {
	return auth.CurrentUser{
		ID:          uuid.New(),
		FirebaseUID: "fb-test-noperm",
		Email:       "vendedor@muebleriamsp.mx",
		Nombre:      "Sin Permiso",
		Permisos:    []string{},
	}
}

// buildTestService builds a Service wired with the supplied fakes for HTTP tests.
func buildTestService(
	now time.Time,
	saldos outbound.SaldosRepo,
	pagosRecibidos outbound.PagosRecibidosRepo,
	pagosImagenes outbound.PagosImagenesRepo,
	writer outbound.MicrosipPagoWriter,
	storage outbound.StorageProvider,
	imageProc outbound.ImageProcessor,
	txRunner cobranzaapp.TxRunner,
) *cobranzaapp.Service {
	return cobranzaapp.NewService(
		saldos,
		fakePagosRepoHTTP{},
		nil,
		fixedClockHTTP{t: now},
		pagosRecibidos,
		pagosImagenes,
		writer,
		storage,
		imageProc,
		txRunner,
	)
}

// mountReadWithUser wires a read router with a planted CurrentUser.
// SSE is disabled (default config) so the SSE routes return 503; tests for
// SSE behaviour live in handlers_sse_test.go.
func mountReadWithUser(cu auth.CurrentUser, svc *cobranzaapp.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	cobranzahttp.MountReadRouter(r, svc, eventbus.New(), config.Cobranza{}, slog.Default(), nil, nil)
	return r
}

// mountAdminWithUser wires an admin router with a planted CurrentUser.
func mountAdminWithUser(cu auth.CurrentUser, svc *cobranzaapp.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	cobranzahttp.MountAdminRouter(r, svc, nil, nil)
	return r
}

// seedHTTPPago inserts a minimal PagoRecibido into the repo and returns its ID.
func seedHTTPPago(t *testing.T, repo *fakePagosRecibidosRepo) uuid.UUID {
	t.Helper()
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	pagoID := uuid.New()
	pago, err := domain.NewPagoRecibido(domain.CrearPagoRecibidoParams{
		ID:             pagoID,
		CargoDoctoCCID: 5000,
		ClienteID:      11486,
		CobradorID:     200,
		Cobrador:       "Mendoza Torres, Ana",
		Importe:        decimal.NewFromInt(1500),
		FormaCobroID:   1,
		FechaHoraPago:  now.Add(-30 * time.Minute),
		CreatedBy:      uuid.New(),
		Now:            now,
	})
	require.NoError(t, err)
	require.NoError(t, repo.Insert(context.Background(), pago))
	return pagoID
}

// makeSaldoHTTP builds a Saldo for cargo validation.
func makeSaldoHTTP(doctoCCID int, saldo decimal.Decimal) domain.Saldo {
	return domain.HydrateSaldo(domain.HydrateSaldoParams{
		DoctoCCID:   doctoCCID,
		ClienteID:   11486,
		Folio:       "CV-2026-001",
		FechaCargo:  time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		PrecioTotal: saldo,
		Saldo:       saldo,
		UpdatedAt:   time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC),
	})
}

// humaErrorCode extracts the "code=..." string from a Huma error body. Huma
// serialises detail messages as `errors[0].message = "code=<code>"`.
func humaErrorCode(t *testing.T, body *bytes.Buffer) string {
	t.Helper()
	var envelope struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body.Bytes(), &envelope); err != nil || len(envelope.Errors) == 0 {
		return body.String()
	}
	return envelope.Errors[0].Message
}

// ─── CrearPago (multipart) ───────────────────────────────────────────────────

// happyPathSvc wires the standard happy-path Service for CrearPago: saldo 2000
// in cargo 5000, working microsip writer, in-memory repos + storage.
func happyPathSvc(t *testing.T, now time.Time) (
	*cobranzaapp.Service, *fakePagosRecibidosRepo, *fakePagosImagenesRepo, *fakeStorageProvider,
) {
	t.Helper()
	saldos := newFakeSaldosRepoHTTP()
	s := makeSaldoHTTP(5000, decimal.NewFromInt(2000))
	saldos.byCargo[5000] = &s
	pagosRepo := newFakePagosRecibidosRepo()
	imagenes := newFakePagosImagenesRepo()
	store := newFakeStorageProvider()
	writer := &fakeMicrosipPagoWriter{
		result: outbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "Z001"},
	}
	svc := buildTestService(now, saldos, pagosRepo, imagenes, writer, store, nil, fakeTxRunner{})
	return svc, pagosRepo, imagenes, store
}

func TestHTTP_CrearPago_Multipart_HappyPath_SinImagenes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var dto cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, pagoID.String(), dto.ID)
	assert.Len(t, pagosRepo.rows, 1)
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 0, store.storeCalls)
}

func TestHTTP_CrearPago_Multipart_HappyPath_1Imagen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	imgID := uuid.New().String()
	body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
		{
			Filename:    "recibo.pdf",
			Mime:        "application/pdf",
			Body:        bytes.Repeat([]byte("P"), 512),
			ID:          imgID,
			Descripcion: "Recibo de transferencia",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Len(t, pagosRepo.rows, 1)
	assert.Len(t, imagenes.images, 1)
	assert.Equal(t, 1, store.storeCalls)
	assert.Equal(t, 0, store.deleteCalls)
}

func TestHTTP_CrearPago_Multipart_HappyPath_3Imagenes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
		{Filename: "a.pdf", Mime: "application/pdf", Body: []byte("AAA"), ID: uuid.New().String(), Descripcion: "uno"},
		{Filename: "b.pdf", Mime: "application/pdf", Body: []byte("BBB"), ID: uuid.New().String(), Descripcion: "dos"},
		{Filename: "c.pdf", Mime: "application/pdf", Body: []byte("CCC"), ID: uuid.New().String(), Descripcion: "tres"},
	})

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Len(t, pagosRepo.rows, 1)
	assert.Len(t, imagenes.images, 3)
	assert.Equal(t, 3, store.storeCalls)
	assert.Equal(t, 0, store.deleteCalls)
}

func TestHTTP_CrearPago_Multipart_DatosMissing(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, _ := happyPathSvc(t, now)

	// Build a multipart body with NO `datos` part.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/pagos", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "datos")
}

func TestHTTP_CrearPago_Multipart_DatosNoEsJSON(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, store := happyPathSvc(t, now)

	body, ct := buildCrearPagoMultipart(t, "not-a-json-blob{{{", nil)
	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "datos_json_invalido")
	assert.Equal(t, 0, store.storeCalls)
}

func TestHTTP_CrearPago_Multipart_InvalidUUIDInDatos(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, _ := happyPathSvc(t, now)

	datos := crearPagoDatosJSON("not-a-uuid", now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)
	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "pago_id_invalido")
}

func TestHTTP_CrearPago_Multipart_IdempotencyKeyMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, _ := happyPathSvc(t, now)

	pagoID := uuid.New()
	other := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Idempotency-Key", other.String())
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "idempotency_key_mismatch")
}

func TestHTTP_CrearPago_Multipart_InvalidFecha(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, _ := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), "not a date")
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "fecha_hora_pago_invalida")
}

func TestHTTP_CrearPago_Multipart_InvalidImporte(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, _ := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := `{"id":"` + pagoID.String() + `",` +
		`"cargo_docto_cc_id":5000,"cliente_id":11486,"cobrador_id":200,` +
		`"cobrador":"Mendoza Torres, Ana",` +
		`"importe":"not a decimal",` +
		`"forma_cobro_id":1,"fecha_hora_pago":"` +
		now.Add(-30*time.Minute).Format(time.RFC3339) + `"}`
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "importe_invalido")
}

func TestHTTP_CrearPago_Multipart_PermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, _ := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, nil)

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(noPermUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code, "body: %s", rec.Body.String())
}

func TestHTTP_CrearPago_Multipart_Idempotent_RepeatedRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, _, _ := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	router := mountReadWithUser(pagoUser(), svc)

	// First.
	body1, ct1 := buildCrearPagoMultipart(t, datos, nil)
	req1 := httptest.NewRequest(http.MethodPost, "/pagos", body1)
	req1.Header.Set("Content-Type", ct1)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code, "first: %s", rec1.Body.String())
	var dto1 cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &dto1))

	// Second.
	body2, ct2 := buildCrearPagoMultipart(t, datos, nil)
	req2 := httptest.NewRequest(http.MethodPost, "/pagos", body2)
	req2.Header.Set("Content-Type", ct2)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "second: %s", rec2.Body.String())
	var dto2 cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &dto2))

	assert.Equal(t, dto1.ID, dto2.ID, "idempotent: same pago returned")
	assert.Len(t, pagosRepo.rows, 1, "exactly one row stored")
}

func TestHTTP_CrearPago_Multipart_RejectsBMP_NoSideEffects(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
		{Filename: "bad.bmp", Mime: "image/bmp", Body: []byte("BM data"), ID: uuid.New().String()},
	})

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	// Huma rejects the part on contentType validation — 422.
	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	// Nothing persisted.
	assert.Empty(t, pagosRepo.rows)
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 0, store.storeCalls)
}

func TestHTTP_CrearPago_Multipart_OneBadMime_NadaPersiste(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, imagenes, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
		{Filename: "a.pdf", Mime: "application/pdf", Body: []byte("AAA"), ID: uuid.New().String()},
		{Filename: "b.pdf", Mime: "application/pdf", Body: []byte("BBB"), ID: uuid.New().String()},
		{Filename: "bad.bmp", Mime: "image/bmp", Body: []byte("BM data"), ID: uuid.New().String()},
	})

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	// Tx never opens — nothing in the repos or storage.
	assert.Empty(t, pagosRepo.rows)
	assert.Empty(t, imagenes.images)
	assert.Equal(t, 0, store.storeCalls)
}

func TestHTTP_CrearPago_Multipart_ImagenIDInvalid_Rejected(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, pagosRepo, _, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))
	body, ct := buildCrearPagoMultipart(t, datos, []crearPagoImagen{
		{Filename: "x.pdf", Mime: "application/pdf", Body: []byte("X"), ID: "not-a-uuid"},
	})

	req := httptest.NewRequest(http.MethodPost, "/pagos", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "imagen_id_invalido")
	assert.Empty(t, pagosRepo.rows)
	assert.Equal(t, 0, store.storeCalls)
}

func TestHTTP_CrearPago_Multipart_RejectsJSONContentType(t *testing.T) {
	t.Parallel()

	// Legacy JSON-only callers must migrate. Sending Content-Type: application/json
	// to the new endpoint is rejected — no graceful fallback.
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc, _, _, store := happyPathSvc(t, now)

	pagoID := uuid.New()
	datos := crearPagoDatosJSON(pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(datos))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400, "JSON-only requests must fail")
	assert.Less(t, rec.Code, 500)
	assert.Equal(t, 0, store.storeCalls)
}

// ─── ObtenerPagoRecibido ──────────────────────────────────────────────────────

func TestHTTP_ObtenerPagoRecibido_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	pagoID := seedHTTPPago(t, pagosRepo)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/"+pagoID.String(), nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dto cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, pagoID.String(), dto.ID)
	assert.Equal(t, 5000, dto.CargoDoctoCCID)
}

func TestHTTP_ObtenerPagoRecibido_NotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "pago_no_encontrado")
}

func TestHTTP_ObtenerPagoRecibido_InvalidUUIDInPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/not-a-uuid", nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
}

func TestHTTP_ObtenerPagoRecibido_PermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/"+uuid.New().String(), nil)
	rec := httptest.NewRecorder()

	mountReadWithUser(noPermUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// ─── ListarPendientes ─────────────────────────────────────────────────────────

func TestHTTP_ListarPendientes_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	// Seed 3 pendiente pagos.
	for range 3 {
		seedHTTPPago(t, pagosRepo)
	}
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/pendientes", nil)
	rec := httptest.NewRecorder()

	mountAdminWithUser(adminUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var items []cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
	assert.Len(t, items, 3)
}

func TestHTTP_ListarPendientes_AdminPermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/pendientes", nil)
	rec := httptest.NewRecorder()

	// pagoUser has VerPagos but NOT Reconciliar.
	mountAdminWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHTTP_ListarPendientes_CustomLimit(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	// Seed 5 pagos.
	for range 5 {
		seedHTTPPago(t, pagosRepo)
	}
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodGet, "/pagos/pendientes?limit=2&max_intentos=5", nil)
	rec := httptest.NewRecorder()

	mountAdminWithUser(adminUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var items []cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
	assert.LessOrEqual(t, len(items), 2, "limit=2 must cap results to at most 2")
}

// ─── AplicarPagoForzar ────────────────────────────────────────────────────────

func TestHTTP_AplicarPagoForzar_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	pagosRepo := newFakePagosRecibidosRepo()
	pagoID := seedHTTPPago(t, pagosRepo)

	writer := &fakeMicrosipPagoWriter{
		result: outbound.MicrosipPagoResult{DoctoCCID: 42, ImpteDoctoCCID: 43, Folio: "Z999"},
	}
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), pagosRepo, newFakePagosImagenesRepo(), writer, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+pagoID.String()+"/aplicar", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountAdminWithUser(adminUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dto cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, pagoID.String(), dto.ID)
	assert.Equal(t, "aplicada", dto.Sincronizacion)
	require.NotNil(t, dto.Folio)
	assert.Equal(t, "Z999", *dto.Folio)
}

func TestHTTP_AplicarPagoForzar_NotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	writer := &fakeMicrosipPagoWriter{}
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), writer, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+uuid.New().String()+"/aplicar", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountAdminWithUser(adminUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestHTTP_AplicarPagoForzar_AdminPermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	writer := &fakeMicrosipPagoWriter{}
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), writer, newFakeStorageProvider(), nil, fakeTxRunner{})

	req := httptest.NewRequest(http.MethodPost, "/pagos/"+uuid.New().String()+"/aplicar", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// pagoUser has VerPagos but NOT Reconciliar.
	mountAdminWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}
