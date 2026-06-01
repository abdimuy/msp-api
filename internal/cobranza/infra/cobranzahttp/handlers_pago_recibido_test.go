//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/infra/cobranzahttp"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
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
func mountReadWithUser(cu auth.CurrentUser, svc *cobranzaapp.Service) http.Handler {
	r := chi.NewRouter()
	r.Use(planter(cu))
	cobranzahttp.MountReadRouter(r, svc)
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

// ─── CrearPago ────────────────────────────────────────────────────────────────

func TestHTTP_CrearPago_HappyPath(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	saldos := newFakeSaldosRepoHTTP()
	s := makeSaldoHTTP(5000, decimal.NewFromInt(2000))
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	writer := &fakeMicrosipPagoWriter{
		result: outbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "Z001"},
	}
	svc := buildTestService(now, saldos, pagosRepo, newFakePagosImagenesRepo(), writer, newFakeStorageProvider(), nil, fakeTxRunner{})

	pagoID := uuid.New()
	body := fmt.Sprintf(`{
		"id": %q,
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "1500.00",
		"forma_cobro_id": 1,
		"fecha_hora_pago": %q
	}`, pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var dto cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, pagoID.String(), dto.ID)
	assert.Equal(t, 5000, dto.CargoDoctoCCID)
	assert.Equal(t, "1500.00", dto.Importe)
}

func TestHTTP_CrearPago_InvalidUUID(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	body := fmt.Sprintf(`{
		"id": "not-a-uuid",
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "1500.00",
		"forma_cobro_id": 1,
		"fecha_hora_pago": %q
	}`, now.Add(-30*time.Minute).Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	// Huma may reject at schema validation (422) or handler (422) — either is correct.
	assert.GreaterOrEqual(t, rec.Code, 400, "expected 4xx for invalid UUID")
	assert.Less(t, rec.Code, 500)
	// When our handler runs, the code is pago_id_invalido; Huma schema error has a different shape.
	bodyStr := rec.Body.String()
	assert.NotEmpty(t, bodyStr)
}

func TestHTTP_CrearPago_IdempotencyKeyMismatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	pagoID := uuid.New()
	otherID := uuid.New()

	body := fmt.Sprintf(`{
		"id": %q,
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "1500.00",
		"forma_cobro_id": 1,
		"fecha_hora_pago": %q
	}`, pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", otherID.String()) // mismatch!
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "idempotency_key_mismatch")
}

func TestHTTP_CrearPago_InvalidFecha(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	pagoID := uuid.New()
	body := fmt.Sprintf(`{
		"id": %q,
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "1500.00",
		"forma_cobro_id": 1,
		"fecha_hora_pago": "not a date"
	}`, pagoID.String())

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	// Huma schema validation or handler validation — either surfaces an error.
	assert.NotEmpty(t, rec.Body.String())
}

func TestHTTP_CrearPago_InvalidImporte(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	pagoID := uuid.New()
	body := fmt.Sprintf(`{
		"id": %q,
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "not a decimal",
		"forma_cobro_id": 1,
		"fecha_hora_pago": %q
	}`, pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountReadWithUser(pagoUser(), svc).ServeHTTP(rec, req)

	assert.GreaterOrEqual(t, rec.Code, 400)
	assert.Less(t, rec.Code, 500)
	assert.Contains(t, rec.Body.String(), "importe_invalido")
}

func TestHTTP_CrearPago_PermDenied(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc := buildTestService(now, newFakeSaldosRepoHTTP(), newFakePagosRecibidosRepo(), newFakePagosImagenesRepo(), nil, newFakeStorageProvider(), nil, fakeTxRunner{})

	pagoID := uuid.New()
	body := fmt.Sprintf(`{
		"id": %q,
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "1500.00",
		"forma_cobro_id": 1,
		"fecha_hora_pago": %q
	}`, pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))

	req := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mountReadWithUser(noPermUser(), svc).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestHTTP_CrearPago_Idempotent_RepeatedRequest(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	saldos := newFakeSaldosRepoHTTP()
	s := makeSaldoHTTP(5000, decimal.NewFromInt(2000))
	saldos.byCargo[5000] = &s

	pagosRepo := newFakePagosRecibidosRepo()
	writer := &fakeMicrosipPagoWriter{
		result: outbound.MicrosipPagoResult{DoctoCCID: 1, ImpteDoctoCCID: 2, Folio: "Z001"},
	}
	svc := buildTestService(now, saldos, pagosRepo, newFakePagosImagenesRepo(), writer, newFakeStorageProvider(), nil, fakeTxRunner{})

	pagoID := uuid.New()
	bodyStr := fmt.Sprintf(`{
		"id": %q,
		"cargo_docto_cc_id": 5000,
		"cliente_id": 11486,
		"cobrador_id": 200,
		"cobrador": "Mendoza Torres, Ana",
		"importe": "1500.00",
		"forma_cobro_id": 1,
		"fecha_hora_pago": %q
	}`, pagoID.String(), now.Add(-30*time.Minute).Format(time.RFC3339))

	cu := pagoUser()
	router := mountReadWithUser(cu, svc)

	// First request.
	req1 := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(bodyStr))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code, "first: %s", rec1.Body.String())

	var dto1 cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &dto1))

	// Second request with same body.
	req2 := httptest.NewRequest(http.MethodPost, "/pagos", bytes.NewBufferString(bodyStr))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code, "second: %s", rec2.Body.String())

	var dto2 cobranzahttp.PagoRecibidoDTO
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &dto2))

	assert.Equal(t, dto1.ID, dto2.ID, "idempotent: both must return same pago ID")
	assert.Len(t, pagosRepo.rows, 1, "exactly one row must be stored")
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
