//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/platform/imageprocessor"
	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
	ventasdomain "github.com/abdimuy/msp-api/internal/ventas/domain"
	"github.com/abdimuy/msp-api/internal/ventas/infra/venthttp"
	ventasoutbound "github.com/abdimuy/msp-api/internal/ventas/ports/outbound"
)

// fixedClock returns a constant time for deterministic timestamps.
type fixedClock struct{ T time.Time }

func (c fixedClock) Now() time.Time { return c.T }

// fakeStorage is an in-memory StorageProvider that captures Store/Delete.
type fakeStorage struct {
	mu    sync.Mutex
	blobs map[string][]byte
}

func newFakeStorage() *fakeStorage { return &fakeStorage{blobs: map[string][]byte{}} }

func (f *fakeStorage) Store(_ context.Context, key, _ string, _ int64, body io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	f.blobs[key] = b
	return nil
}

func (f *fakeStorage) Get(_ context.Context, key string) (ventasoutbound.StorageObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.blobs[key]
	if !ok {
		return ventasoutbound.StorageObject{}, http.ErrMissingFile
	}
	return ventasoutbound.StorageObject{Body: io.NopCloser(bytes.NewReader(b)), ContentType: "application/octet-stream", SizeBytes: int64(len(b))}, nil
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.blobs, key)
	return nil
}

func (f *fakeStorage) has(key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.blobs[key]
	return ok
}

// fakeRepo is the smallest VentaRepo that supports the handler-test scenarios.
type fakeRepo struct {
	mu    sync.Mutex
	store map[uuid.UUID]*ventasdomain.Venta
}

func newFakeRepo() *fakeRepo { return &fakeRepo{store: map[uuid.UUID]*ventasdomain.Venta{}} }

func (r *fakeRepo) Save(_ context.Context, v *ventasdomain.Venta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[v.ID()] = v
	return nil
}

func (r *fakeRepo) Update(_ context.Context, v *ventasdomain.Venta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store[v.ID()] = v
	return nil
}

func (r *fakeRepo) FindByID(_ context.Context, id uuid.UUID) (*ventasdomain.Venta, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.store[id]
	if !ok {
		return nil, ventasdomain.ErrVentaNotFound
	}
	return v, nil
}

func (r *fakeRepo) LockByID(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.store[id]; !ok {
		return ventasdomain.ErrVentaNotFound
	}
	return nil
}

func (r *fakeRepo) List(_ context.Context, _ ventasoutbound.ListParams, _ ventasoutbound.ListVentasFilters) (ventasoutbound.Page[*ventasdomain.Venta], error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]*ventasdomain.Venta, 0, len(r.store))
	for _, v := range r.store {
		items = append(items, v)
	}
	return ventasoutbound.Page[*ventasdomain.Venta]{Items: items}, nil
}

func (r *fakeRepo) InsertImagen(_ context.Context, _ uuid.UUID, _ *ventasdomain.Imagen) error {
	return nil
}

func (r *fakeRepo) DeleteImagen(_ context.Context, _, _ uuid.UUID) error { return nil }

func (r *fakeRepo) UpdateHeader(_ context.Context, v *ventasdomain.Venta) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.store[v.ID()]; !ok {
		return ventasdomain.ErrVentaNotFound
	}
	r.store[v.ID()] = v
	return nil
}

func (r *fakeRepo) UpdateCliente(ctx context.Context, v *ventasdomain.Venta) error {
	return r.UpdateHeader(ctx, v)
}

func (r *fakeRepo) ReplaceProductos(ctx context.Context, v *ventasdomain.Venta) error {
	return r.UpdateHeader(ctx, v)
}

func (r *fakeRepo) ReplaceCombos(ctx context.Context, v *ventasdomain.Venta) error {
	return r.UpdateHeader(ctx, v)
}

func (r *fakeRepo) ReplaceVendedores(ctx context.Context, v *ventasdomain.Venta) error {
	return r.UpdateHeader(ctx, v)
}

// noopOutbox swallows every Enqueue call.
type noopOutbox struct{}

func (noopOutbox) Enqueue(_ context.Context, _ string, _ uuid.UUID, _ string, _ any) error {
	return nil
}

// planter is a chi middleware that plants the supplied CurrentUser on the
// request context so handlers can read it directly.
func planter(cu auth.CurrentUser) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := auth.PlantCurrentUser(r.Context(), cu)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// testService builds a Service with in-memory fakes wired together. The
// NoOp image processor is used so existing handler tests see the upload
// bytes verbatim.
func testService() (*ventasapp.Service, *fakeRepo, *fakeStorage) {
	return testServiceWith(imageprocessor.NoOpProcessor{})
}

// testServiceWith is testService but lets the caller inject a specific
// image processor — used by the end-to-end pipeline tests that exercise
// the real StandardProcessor through the HTTP layer.
func testServiceWith(proc ventasoutbound.ImageProcessor) (*ventasapp.Service, *fakeRepo, *fakeStorage) {
	repo := newFakeRepo()
	store := newFakeStorage()
	clock := fixedClock{T: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
	svc := ventasapp.NewService(repo, nil, nil, store, clock, noopOutbox{}, proc, nil, nil, nil, nil)
	return svc, repo, store
}

// buildRouter wires the ventas Huma routes onto a chi router with a context
// planter that authenticates every request as cu.
func buildRouter(t *testing.T, svc *ventasapp.Service, cu auth.CurrentUser) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Use(planter(cu))
	venthttp.MountRouter(r, svc)
	return r
}

// buildRouterNoAuth mounts the ventas routes WITHOUT planting a
// CurrentUser, so every request lands at the handler anonymously. Used
// by negative-path tests that pin the 401 unauthenticated response on
// endpoints that bypass Huma's own auth model (eg. the streaming GET
// imagen handler).
func buildRouterNoAuth(t *testing.T, svc *ventasapp.Service) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	venthttp.MountRouter(r, svc)
	return r
}

// fullPerms returns a CurrentUser with every ventas permission code so tests
// pass the authorization checks by default.
func fullPerms(id uuid.UUID) auth.CurrentUser {
	return auth.CurrentUser{
		ID:          id,
		FirebaseUID: "fb-1",
		Email:       "tester@example.com",
		Nombre:      "Tester",
		Permisos: []string{
			string(authdomain.PermVentasListar),
			string(authdomain.PermVentasVer),
			string(authdomain.PermVentasCrear),
			string(authdomain.PermVentasCancelar),
			string(authdomain.PermVentasEditar),
			string(authdomain.PermVentasSubirImagenes),
			string(authdomain.PermVentasEliminarImagenes),
			string(authdomain.PermVentasRevisar),
			string(authdomain.PermVentasAprobar),
			string(authdomain.PermVentasAplicar),
		},
	}
}

// jsonRequest builds an httptest request with a JSON body.
func jsonRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(method, target, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// intPtr returns a pointer to v. Test helper.
func intPtr(v int) *int { return &v }

// validCreateBody returns a CrearVentaBody that satisfies every domain rule.
func validCreateBody() venthttp.CrearVentaBody {
	return venthttp.CrearVentaBody{
		ID: uuid.NewString(),
		Cliente: venthttp.ClienteSnapshotDTO{
			Nombre: "Cliente Demo",
		},
		Direccion: venthttp.DireccionDTO{
			Calle:     "Av. Reforma 100",
			Colonia:   "Centro",
			Poblacion: "Mérida",
			Ciudad:    "Mérida",
		},
		GPS:        venthttp.GPSDTO{Latitud: 20.97, Longitud: -89.62},
		FechaVenta: time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
		TipoVenta:  "CONTADO",
		Montos:     venthttp.MontosDTO{Anual: "1000", CortoPlazo: "900", Contado: "800"},
		Productos: []venthttp.ProductoDTO{{
			ID:               uuid.NewString(),
			ArticuloID:       42,
			Articulo:         "Refrigerador 10ft",
			Cantidad:         "1",
			PrecioAnual:      "1000",
			PrecioCorto:      "900",
			PrecioContado:    "800",
			AlmacenOrigenID:  intPtr(1),
			AlmacenDestinoID: intPtr(2),
		}},
		Vendedores: []venthttp.VendedorDTO{{
			ID:        uuid.NewString(),
			UsuarioID: uuid.NewString(),
			Email:     "vendedor@example.com",
			Nombre:    "Vendedor Uno",
		}},
	}
}

func TestCrearVenta_OK(t *testing.T) {
	t.Parallel()

	svc, repo, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code, "body=%s", rec.Body.String())
	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, body.ID, out.ID)
	assert.Equal(t, "CONTADO", out.TipoVenta)
	// The domain folds user-captured text to ALL CAPS (Microsip convention).
	assert.Equal(t, strings.ToUpper(body.Cliente.Nombre), out.Cliente.Nombre)
	require.Len(t, out.Productos, 1)

	// The aggregate must be persisted in the fake repo.
	id, _ := uuid.Parse(body.ID)
	_, err := repo.FindByID(context.Background(), id)
	require.NoError(t, err)
}

func TestCrearVenta_PermissionDenied(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := auth.CurrentUser{ID: uuid.New(), Permisos: []string{string(authdomain.PermVentasListar)}}
	r := buildRouter(t, svc, cu)

	req := crearVentaMultipartRequest(t, validCreateBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCrearVenta_Unauthenticated(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := chi.NewRouter()
	venthttp.MountRouter(r, svc) // no planter — no CurrentUser

	req := crearVentaMultipartRequest(t, validCreateBody())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestObtenerVenta_NotFound(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	req := httptest.NewRequest(http.MethodGet, "/ventas/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestListarVentas_ReturnsItems(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	// Seed one venta via the create endpoint.
	createReq := crearVentaMultipartRequest(t, validCreateBody())
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	listReq := httptest.NewRequest(http.MethodGet, "/ventas", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	require.Equal(t, http.StatusOK, listRec.Code, listRec.Body.String())

	var page venthttp.ListResponse[venthttp.VentaDTO]
	require.NoError(t, json.Unmarshal(listRec.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
}

func TestCancelarVenta_OK(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cancel", venthttp.CancelarVentaBody{Reason: "cliente no localizable"})
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)

	require.Equal(t, http.StatusOK, cancelRec.Code, cancelRec.Body.String())
	assert.Contains(t, cancelRec.Body.String(), "\"cancelacion\"")
}

func TestCrearVenta_InvalidBody_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	// productos is empty — minItems:"1" should trip Huma validation.
	body := validCreateBody()
	body.Productos = nil
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// TestCancelarVenta_AlreadyCanceled_Returns409 verifies the handler refuses a
// second cancel call on the same venta with a stable conflict response. A
// silently-accepted second cancel would leak audit data (cancel_at would be
// the second timestamp, not the first one) — that's lawsuit-grade.
func TestCancelarVenta_AlreadyCanceled_Returns409(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	cancelOnce := jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cancel",
		venthttp.CancelarVentaBody{Reason: "first cancel"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, cancelOnce)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	cancelTwice := jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cancel",
		venthttp.CancelarVentaBody{Reason: "second cancel"})
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, cancelTwice)
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

// TestCancelarVenta_NotFound_Returns404 verifies cancellation of a venta that
// doesn't exist returns 404 (not 500 — that would leak DB error shape).
func TestCancelarVenta_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	req := jsonRequest(t, http.MethodPatch, "/ventas/"+uuid.NewString()+"/cancel",
		venthttp.CancelarVentaBody{Reason: "ghost"})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestEliminarImagen_NotFound_Returns404 verifies deletion of a non-existent
// imagen returns 404, not 500 or a silent success.
func TestEliminarImagen_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// Delete an imagen id that was never attached.
	req := httptest.NewRequest(http.MethodDelete,
		"/ventas/"+body.ID+"/imagenes/"+uuid.NewString(), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestAdjuntarImagen_VentaNotFound_Returns404 verifies the handler rejects an
// upload targeting a venta id that does not exist with 404 rather than
// silently writing an orphan row.
func TestAdjuntarImagen_VentaNotFound_Returns404(t *testing.T) {
	t.Parallel()

	svc, _, store := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="x.jpg"`)
	hdr.Set("Content-Type", "image/jpeg")
	part, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, _ = part.Write([]byte("ghost-jpeg"))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/ventas/"+uuid.NewString()+"/imagenes", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	// Lawsuit-grade: an upload that hit a non-existent venta must NOT have
	// persisted a blob — the storage rollback must have run.
	assert.Empty(t, store.blobs, "no blob should have leaked through after a 404")
}

// TestAdjuntarImagen_OnCanceledVenta_Returns409 verifies the domain rule
// that no imagen can be attached to a cancelled venta is enforced end-to-end.
// A regression that allowed late attachments would break compliance.
func TestAdjuntarImagen_OnCanceledVenta_Returns409(t *testing.T) {
	t.Parallel()

	svc, _, store := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	// Snapshot blob count after creation — the creation stores the evidencia
	// imagen that crearVentaMultipartRequest includes. Any blob added by a
	// refused late upload would increment this count.
	store.mu.Lock()
	blobsAfterCreate := len(store.blobs)
	store.mu.Unlock()

	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cancel",
		venthttp.CancelarVentaBody{Reason: "cancel-for-immutability-test"})
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)
	require.Equal(t, http.StatusOK, cancelRec.Code)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="late.jpg"`)
	hdr.Set("Content-Type", "image/jpeg")
	part, err := mw.CreatePart(hdr)
	require.NoError(t, err)
	_, _ = part.Write([]byte("late-upload"))
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/ventas/"+body.ID+"/imagenes", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	// Storage must NOT have grown — the late blob (best-effort rollback ran)
	// must not have been persisted; only the creation-time blob remains.
	store.mu.Lock()
	blobsAfterRefusal := len(store.blobs)
	store.mu.Unlock()
	assert.Equal(t, blobsAfterCreate, blobsAfterRefusal,
		"no blob should be added to storage for a refused upload")
}

// TestObtenerVenta_AfterCancel_PreservesAudit verifies the cancellation
// envelope round-trips through GET — the response includes the full
// cancelacion record (at, by, reason). A missing field would prevent
// downstream auditing.
func TestObtenerVenta_AfterCancel_PreservesAudit(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cancel",
		venthttp.CancelarVentaBody{Reason: "audit-roundtrip"})
	cancelRec := httptest.NewRecorder()
	r.ServeHTTP(cancelRec, cancelReq)
	require.Equal(t, http.StatusOK, cancelRec.Code)

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	require.NotNil(t, got.Cancelacion, "GET after cancel must include the cancelacion record")
	assert.Equal(t, "audit-roundtrip", got.Cancelacion.Reason)
	assert.NotEmpty(t, got.Cancelacion.At)
	assert.NotEmpty(t, got.Cancelacion.By)
}

// fakeNombreResolver maps usuario ids to display names for the audit-panel
// resolution exercised by GET /ventas/{id}.
type fakeNombreResolver struct{ nombres map[uuid.UUID]string }

func (f fakeNombreResolver) NombresPorID(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if n, ok := f.nombres[id]; ok {
			out[id] = n
		}
	}
	return out, nil
}

// TestObtenerVenta_ResolvesAuditActorNombres verifies GET resolves the audit
// actors (created_by / updated_by) to display names so the detail panel shows
// people, not raw UUIDs. The created venta's created_by and updated_by are the
// creating user, so both *_nombre fields carry that user's name.
func TestObtenerVenta_ResolvesAuditActorNombres(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	creador := uuid.New()
	cu := fullPerms(creador)
	svc.WithUsuarioResolver(fakeNombreResolver{nombres: map[uuid.UUID]string{
		creador: "Ana Vendedora",
	}})
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	assert.Equal(t, creador.String(), got.CreatedBy)
	assert.Equal(t, "Ana Vendedora", got.CreatedByNombre)
	assert.Equal(t, "Ana Vendedora", got.UpdatedByNombre)
}

// TestObtenerVenta_OmitsActorNombresWithoutResolver verifies the *_nombre
// fields stay absent (omitempty) when no resolver is wired — the panel falls
// back to the raw UUIDs rather than breaking.
func TestObtenerVenta_OmitsActorNombresWithoutResolver(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code, getRec.Body.String())

	assert.NotContains(t, getRec.Body.String(), "created_by_nombre")

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	assert.Empty(t, got.CreatedByNombre)
	assert.NotEmpty(t, got.CreatedBy)
}

// TestCrearVenta_WithCombo_RoundTrip verifies the combo + producto-with-combo
// path serializes/deserializes through the handler. Combos are uncommon in
// the dev DB, so without this test the combo-projection code path is
// effectively zero-coverage.
func TestCrearVenta_WithCombo_RoundTrip(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	comboID := uuid.NewString()
	body.Combos = []venthttp.ComboDTO{{
		ID:               comboID,
		Nombre:           "Combo Demo",
		PrecioAnual:      "1500.00",
		PrecioCorto:      "1400.00",
		PrecioContado:    "1300.00",
		Cantidad:         "1.0000",
		AlmacenOrigenID:  1,
		AlmacenDestinoID: 2,
	}}
	// Link the existing producto to the new combo. Productos in a combo
	// must NOT carry their own almacenes.
	body.Productos[0].ComboID = &comboID
	body.Productos[0].AlmacenOrigenID = nil
	body.Productos[0].AlmacenDestinoID = nil

	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Combos, 1, "response must include the combo")
	assert.Equal(t, "Combo Demo", out.Combos[0].Nombre)
	assert.Equal(t, "1500.00", out.Combos[0].PrecioAnual, "combo precio must use fixed-scale serialization")
	require.NotNil(t, out.Productos[0].ComboID, "producto must reference the combo")
	assert.Equal(t, comboID, *out.Productos[0].ComboID)
}

// TestCrearVenta_OptionalClienteFields_RoundTrip verifies telefono and aval
// (optional cliente snapshot fields) survive the round-trip through DTO
// mapper. Without this test, toClienteSnapshotDTO's branches for these
// pointers are not exercised.
func TestCrearVenta_OptionalClienteFields_RoundTrip(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	tel := "4491234567"
	aval := "Avalista Test"
	body.Cliente.Telefono = &tel
	body.Cliente.Aval = &aval

	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var out venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.NotNil(t, out.Cliente.Telefono)
	assert.Equal(t, tel, *out.Cliente.Telefono)
	require.NotNil(t, out.Cliente.Aval)
	// Aval is a person name → folded to ALL CAPS by the domain.
	assert.Equal(t, strings.ToUpper(aval), *out.Cliente.Aval)
}

// TestListarVentas_InvalidFilterDate_Returns422 verifies that a malformed
// `desde` query parameter yields a typed 422 with a stable code, not a 500.
// This guards the buildListarFilters error paths.
func TestListarVentas_InvalidFilterDate_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	cases := []struct{ name, query string }{
		{"bad-desde", "/ventas?desde=not-a-date"},
		{"bad-hasta", "/ventas?hasta=2026-13-99T99:99:99Z"},
		{"bad-vendedor", "/ventas?vendedor_usuario_id=not-a-uuid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, tc.query, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"want 422 for %s, got %d body=%s", tc.query, rec.Code, rec.Body.String())
		})
	}
}

// TestListarVentas_ValidDateFilters_Accepted verifies that well-formed RFC
// 3339 date filters are accepted by buildListarFilters and produce a 200
// (even if the result set is empty). Exercises the happy-path of every
// branch of buildListarFilters.
func TestListarVentas_ValidDateFilters_Accepted(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	url := "/ventas?desde=2026-01-01T00:00:00Z&hasta=2026-12-31T23:59:59Z" +
		"&vendedor_usuario_id=" + uuid.NewString() +
		"&tipo_venta=CONTADO&incluir_canceladas=true&limit=10"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestListarVentas_ClienteIDFilter verifies the cliente_id query param is
// parsed and accepted. The actual repository-level filtering is exercised by
// TestVentaRepo_List_FilterByClienteID against the real DB.
func TestListarVentas_ClienteIDFilter(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))

	req := httptest.NewRequest(http.MethodGet, "/ventas?cliente_id=12345", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// TestListarVentas_InvalidClienteID_Returns422 verifies the cliente_id query
// param is rejected when malformed (non-numeric, zero, negative).
func TestListarVentas_InvalidClienteID_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := buildRouter(t, svc, fullPerms(uuid.New()))

	cases := []string{"not-a-number", "0", "-5", "1.5"}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/ventas?cliente_id="+raw, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnprocessableEntity, rec.Code,
				"want 422 for cliente_id=%q, got %d body=%s", raw, rec.Code, rec.Body.String())
		})
	}
}

// TestCrearVenta_InvalidCombo_UUID_Returns422 verifies parseCombosDTO error
// paths: a combo with a malformed id is rejected with a typed 422.
func TestCrearVenta_InvalidCombo_UUID_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	body.Combos = []venthttp.ComboDTO{{
		ID:            "not-a-uuid",
		Nombre:        "Bad combo",
		PrecioAnual:   "100.00",
		PrecioCorto:   "90.00",
		PrecioContado: "80.00",
	}}
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// TestCrearVenta_InvalidCombo_Decimal_Returns422 verifies that a combo with
// a malformed decimal price is rejected with 422 (not 500). Each parse step
// inside parseCombosDTO must surface a typed error.
func TestCrearVenta_InvalidCombo_Decimal_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	body.Combos = []venthttp.ComboDTO{{
		ID:            uuid.NewString(),
		Nombre:        "Bad price",
		PrecioAnual:   "abc",
		PrecioCorto:   "90.00",
		PrecioContado: "80.00",
	}}
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// ─── B4: trailing slash / method not allowed ──────────────────────────────

// TestRouter_TrailingSlash_Behavior documents how the router treats trailing
// slashes. chi by default does NOT treat /ventas and /ventas/ as the same
// path; we pin the current behavior so a future routing change is loud.
func TestRouter_TrailingSlash_Behavior(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	cases := []struct {
		name       string
		path       string
		methodList []string
	}{
		{"list with trailing slash", "/ventas/", []string{http.MethodGet}},
		{"obtener with trailing slash", "/ventas/" + uuid.NewString() + "/", []string{http.MethodGet}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, m := range tc.methodList {
				req := httptest.NewRequest(m, tc.path, nil)
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				// Either 404, 200/2xx, or 3xx redirect — all are valid
				// router contracts. The point is "no 5xx crash".
				t.Logf("%s %s → %d", m, tc.path, rec.Code)
				assert.Less(t, rec.Code, 500,
					"%s %s must NOT crash the router (got %d)", m, tc.path, rec.Code)
			}
		})
	}
}

// TestRouter_MethodNotAllowed verifies that requests with the wrong HTTP
// method on a registered path produce a typed 4xx (404 or 405) rather than
// crashing or silently succeeding.
func TestRouter_MethodNotAllowed(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	// Seed a venta so the id-bearing paths have a real target.
	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	cases := []struct {
		name   string
		method string
		path   string
	}{
		// /ventas accepts GET (list) + POST (create); DELETE/PATCH are not.
		{"DELETE on collection", http.MethodDelete, "/ventas"},
		// /ventas/{id}/productos accepts PUT; POST should not be registered.
		{"POST on productos", http.MethodPost, "/ventas/" + body.ID + "/productos"},
		// /ventas/{id} accepts GET + PATCH; PUT is not.
		{"PUT on venta header", http.MethodPut, "/ventas/" + body.ID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			t.Logf("%s %s → %d", tc.method, tc.path, rec.Code)
			// 404 or 405 are both fine — anything in 4xx is sane.
			assert.GreaterOrEqual(t, rec.Code, 400, "must be 4xx, got %d", rec.Code)
			assert.Less(t, rec.Code, 500, "must NOT crash, got %d body=%s", rec.Code, rec.Body.String())
		})
	}
}

// ─── B2: malformed / enormous JSON ─────────────────────────────────────────

// TestCrearVenta_MalformedJSON_Returns422 pins how the handler reacts to
// payloads that are not valid JSON. Huma's body parser must surface 4xx
// (415/422/400) rather than 5xx — we don't care which of those, but a 500
// would indicate the parser blew up unrecovered.
func TestCrearVenta_MalformedJSON_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	cases := []struct {
		name string
		body string
	}{
		{"trailing comma", `{"id":"00000000-0000-0000-0000-000000000000",}`},
		{"empty body", ``},
		{"null literal", `null`},
		{"unclosed brace", `{"id":"00000000-0000-0000-0000-000000000000"`},
		{"truncated string", `{"id":"00000000-0000-0000-0000-0`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/ventas", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			assert.GreaterOrEqual(t, rec.Code, 400, "must be 4xx: body=%s", rec.Body.String())
			assert.Less(t, rec.Code, 500, "must NOT be 5xx (parser should recover): body=%s", rec.Body.String())
		})
	}
}

// TestCrearVenta_EnormousBody_Rejected verifies the handler rejects a body
// that is large but still structurally valid (10MB+ of productos). The
// rejection should be a clean 4xx (likely 413), never a 5xx or a hang.
// Requires the BodyLimit middleware (10 MB cap) to be wired — without it the
// domain has no upper bound on productos count and would return 201.
func TestCrearVenta_EnormousBody_Rejected(t *testing.T) {
	t.Parallel()

	// Use bodyLimitRouter so the 10 MB middleware is active.
	r := buildRouterWithBodyLimit(t)

	body := validCreateBody()
	// ~70k productos × ~200 bytes per producto ≈ 14MB of JSON in the datos
	// field — the full multipart body clearly exceeds the 10 MB cap.
	body.Productos = make([]venthttp.ProductoDTO, 0, 70000)
	for i := range 70000 {
		body.Productos = append(body.Productos, venthttp.ProductoDTO{
			ID: uuid.NewString(), ArticuloID: i + 1, Articulo: "Articulo",
			Cantidad: "1", PrecioAnual: "1", PrecioCorto: "1", PrecioContado: "1",
			AlmacenOrigenID: intPtr(1), AlmacenDestinoID: intPtr(2),
		})
	}
	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.GreaterOrEqual(t, rec.Code, 400, "enormous body must be 4xx: %d", rec.Code)
	assert.Less(t, rec.Code, 500, "enormous body must NOT 5xx: %d body=%s", rec.Code, rec.Body.String())
}

// TestCrearVenta_ClienteReferencia_RoundTrip verifies that a venta created with
// cliente.referencia echoes the value back in the POST response and then again
// in the subsequent GET. This exercises the full HTTP → app → domain → (fake)
// repo → DTO mapper round-trip without touching Firebird.
func TestCrearVenta_ClienteReferencia_RoundTrip(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	ref := "CASA AZUL ESQUINA"
	body.Cliente.Referencia = &ref

	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, "create body=%s", rec.Body.String())

	var created venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.NotNil(t, created.Cliente.Referencia, "referencia must be present in POST response")
	assert.Equal(t, ref, *created.Cliente.Referencia)

	// GET the same venta — referencia must survive the repo round-trip.
	getReq := httptest.NewRequest(http.MethodGet, "/ventas/"+body.ID, nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusOK, getRec.Code, "get body=%s", getRec.Body.String())

	var got venthttp.VentaDTO
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &got))
	require.NotNil(t, got.Cliente.Referencia)
	assert.Equal(t, ref, *got.Cliente.Referencia)
}

// TestCrearVenta_ClienteReferencia_TooLong_Returns422 verifies that a
// referencia exceeding 99 characters is rejected with 422 before it reaches
// persistence. The domain validates length via trimOptionalBounded.
func TestCrearVenta_ClienteReferencia_TooLong_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	// 100 ASCII chars — one over the 99-rune limit.
	s := strings.Repeat("A", 100)
	body.Cliente.Referencia = &s

	req := crearVentaMultipartRequest(t, body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body=%s", rec.Body.String())
}

// TestOpenAPI_PathsRegistered verifies that the Huma API publishes the six
// expected ventas endpoints. The /openapi.json document is served by Huma
// at /openapi.json on the chi router rooted at MountRouter's argument.
func TestOpenAPI_PathsRegistered(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	r := chi.NewRouter()
	venthttp.MountRouter(r, svc)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	doc := rec.Body.String()
	for _, want := range []string{
		"/ventas",
		"/ventas/{id}",
		"/ventas/{id}/cancel",
		"/ventas/{id}/imagenes",
		"/ventas/{id}/imagenes/{img_id}",
		"bearerAuth",
	} {
		assert.Contains(t, doc, want, "expected openapi to contain %q", want)
	}
}

// ─── RegresarBorradorVenta ─────────────────────────────────────────────────
//
// These tests pin the HTTP surface of the extended transition
// (revisada OR aprobada → borrador, NEVER aplicada → borrador). The "aplicada"
// rejection is the dollar-stakes one: a successful regress on an aplicada venta
// would orphan the DOCTOS_PV row in Microsip.

// seedRegresable drives a venta from create → revisar (→ aprobar). Used by the
// regresar-borrador handler tests to set up the prereq situación through the
// real HTTP surface.
func seedRegresable(t *testing.T, r http.Handler, untilAprobada bool) string {
	t.Helper()
	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code, createRec.Body.String())

	revisarReq := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/revisar", struct{}{})
	revisarRec := httptest.NewRecorder()
	r.ServeHTTP(revisarRec, revisarReq)
	require.Equal(t, http.StatusOK, revisarRec.Code, revisarRec.Body.String())

	if untilAprobada {
		aprobarReq := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/aprobar", struct{}{})
		aprobarRec := httptest.NewRecorder()
		r.ServeHTTP(aprobarRec, aprobarReq)
		require.Equal(t, http.StatusOK, aprobarRec.Code, aprobarRec.Body.String())
	}
	return body.ID
}

// TestRegresarBorradorVenta_FromAprobada_OK is the new happy path: an aprobada
// venta can regress to borrador and the response surfaces situacion=borrador
// with a cleared aprobacion (rendered as JSON null).
func TestRegresarBorradorVenta_FromAprobada_OK(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID := seedRegresable(t, r, true)

	req := jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"situacion":"borrador"`)
	// The DTO marks Aprobacion as omitempty, so a cleared aprobacion is absent
	// from the response (NOT rendered as "aprobacion":null).
	assert.NotContains(t, rec.Body.String(), `"aprobacion"`)
}

// TestRegresarBorradorVenta_FromRevisada_OK is the regression coverage for the
// original (pre-extension) path.
func TestRegresarBorradorVenta_FromRevisada_OK(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	ventaID := seedRegresable(t, r, false)

	req := jsonRequest(t, http.MethodPost, "/ventas/"+ventaID+"/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"situacion":"borrador"`)
}

// TestRegresarBorradorVenta_FromBorrador_Returns409 verifies the handler
// refuses a regress on a venta still in borrador.
func TestRegresarBorradorVenta_FromBorrador_Returns409(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	req := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_no_regresable_a_borrador")
}

// TestRegresarBorradorVenta_FromAprobadaAplicada_Returns409 is the critical
// guardrail: a venta already materialized in Microsip must never regress. The
// fakeRepo doesn't expose MarcarAplicada via HTTP (no /aplicar in unit fakes),
// so we hydrate the venta into the "aplicada" state directly and verify the
// handler returns 409.
func TestRegresarBorradorVenta_FromAprobadaAplicada_Returns409(t *testing.T) {
	t.Parallel()

	svc, repo, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	ventaUUID, err := uuid.Parse(body.ID)
	require.NoError(t, err)

	// Drive the venta to aprobada through the public surface...
	revisar := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/revisar", struct{}{})
	require.Equal(t, http.StatusOK, do(t, r, revisar).Code)
	aprobar := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/aprobar", struct{}{})
	require.Equal(t, http.StatusOK, do(t, r, aprobar).Code)

	// ... then forcibly mark it aplicada via the domain (the fake repo handler
	// stack does not wire the AplicarVenta machinery).
	repo.mu.Lock()
	v := repo.store[ventaUUID]
	repo.mu.Unlock()
	require.NotNil(t, v)
	require.NoError(t, v.MarcarAplicada(15239197, "Y00099999", time.Date(2026, 5, 2, 12, 0, 0, 0, time.UTC), cu.ID))

	req := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_no_regresable_a_borrador")
}

// TestRegresarBorradorVenta_FromCancelada_Returns409 verifies a cancelled
// venta cannot regress.
func TestRegresarBorradorVenta_FromCancelada_Returns409(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	body := validCreateBody()
	createReq := crearVentaMultipartRequest(t, body)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	require.Equal(t, http.StatusCreated, createRec.Code)

	cancelReq := jsonRequest(t, http.MethodPatch, "/ventas/"+body.ID+"/cancel",
		venthttp.CancelarVentaBody{Reason: "cliente no localizable"})
	require.Equal(t, http.StatusOK, do(t, r, cancelReq).Code)

	req := jsonRequest(t, http.MethodPost, "/ventas/"+body.ID+"/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "venta_no_regresable_a_borrador")
}

// TestRegresarBorradorVenta_NotFound_Returns404 verifies the handler returns
// 404 on a missing venta — not 500 (leaks DB shape) or 409 (wrong semantic).
func TestRegresarBorradorVenta_NotFound_Returns404(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	req := jsonRequest(t, http.MethodPost, "/ventas/"+uuid.NewString()+"/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// TestRegresarBorradorVenta_InvalidUUID_Returns422 verifies Huma's path-param
// validation rejects a non-UUID id before it reaches the handler.
func TestRegresarBorradorVenta_InvalidUUID_Returns422(t *testing.T) {
	t.Parallel()

	svc, _, _ := testService()
	cu := fullPerms(uuid.New())
	r := buildRouter(t, svc, cu)

	req := jsonRequest(t, http.MethodPost, "/ventas/not-a-uuid/regresar-borrador", struct{}{})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
}

// do executes an HTTP request against the router and returns the recorder.
func do(t *testing.T, r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}
