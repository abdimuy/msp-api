//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp_test

import (
	"context"
	"encoding/json"
	"math/rand"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/auth"
	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
	"github.com/abdimuy/msp-api/internal/reactivacion/domain"
	"github.com/abdimuy/msp-api/internal/reactivacion/infra/reactivacionhttp"
	"github.com/abdimuy/msp-api/internal/reactivacion/ports/outbound"
)

// ─── fakes for the canal ports ──────────────────────────────────────────────

// fakeMensajeRepo is a minimal in-memory outbound.MensajeRepo for the HTTP
// layer's own test suite (kept separate from the app package's fakes, which
// are unexported to that package).
type fakeMensajeRepo struct {
	mu         sync.Mutex
	insertados []*domain.Mensaje
}

func (r *fakeMensajeRepo) Insertar(_ context.Context, mensajes []*domain.Mensaje) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.insertados = append(r.insertados, mensajes...)
	return nil
}

func (r *fakeMensajeRepo) ListarPendientes(_ context.Context, limit int) ([]*domain.Mensaje, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Mensaje
	for _, m := range r.insertados {
		if m.Estado() == domain.EstadoEncolado {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (r *fakeMensajeRepo) Actualizar(_ context.Context, m *domain.Mensaje) error {
	return nil
}

func (r *fakeMensajeRepo) Listar(_ context.Context, p outbound.ListarMensajesParams) ([]*domain.Mensaje, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Mensaje
	for _, m := range r.insertados {
		if p.Estado != "" && m.Estado() != p.Estado {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *fakeMensajeRepo) ContarEnviadosHoy(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func (r *fakeMensajeRepo) ClientesConMensaje(_ context.Context) (map[int]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[int]bool, len(r.insertados))
	for _, m := range r.insertados {
		out[m.ClienteID()] = true
	}
	return out, nil
}

// insertadosCount returns the number of Insertar calls captured so far.
func (r *fakeMensajeRepo) insertadosCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.insertados)
}

// fakeSender always succeeds and reports SenderSimulado.
type fakeSender struct{}

func (fakeSender) Enviar(_ context.Context, _ outbound.Destino, _ string) error { return nil }
func (fakeSender) Kind() domain.SenderKind                                      { return domain.SenderSimulado }

// buildServiceWithCanal wires a Service with the Fase 2 channel dependencies
// on top of buildService, using the demo profile so the gobernador never
// blocks a test on wall-clock pacing.
func buildServiceWithCanal(
	reader outbound.UniversoReader,
	repo outbound.CohorteRepo,
	mensajeRepo outbound.MensajeRepo,
	sender outbound.MessageSender,
	autoSend bool,
) *reactivacionapp.Service {
	gobernador := reactivacionapp.NewGobernador(reactivacionapp.PerfilConfig(reactivacionapp.PerfilDemo), rand.New(rand.NewSource(1)))
	return buildService(reader, repo).WithCanal(mensajeRepo, sender, reactivacionapp.NewOpener(), gobernador, autoSend)
}

// ─── POST /reactivacion/envios/encolar ──────────────────────────────────────

func TestEncolar_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	rec := do(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/envios/encolar")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestEncolar_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	// reactivacion:leer is NOT enough for the admin encolado.
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/envios/encolar")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestEncolar_AdminPerm_202(t *testing.T) {
	t.Parallel()
	repo := &fakeRepo{list: []*domain.CohorteCliente{cohorteRow(301, domain.SegmentoRecienLiquidado, false)}}
	mensajeRepo := &fakeMensajeRepo{}
	svc := buildServiceWithCanal(&fakeReader{}, repo, mensajeRepo, fakeSender{}, true)

	rec := do(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/envios/encolar")
	require.Equal(t, http.StatusAccepted, rec.Code)

	var body struct {
		Status  string `json:"status"`
		Mensaje string `json:"mensaje"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, []string{"aceptado", "en_progreso"}, body.Status)
}

// ─── POST /reactivacion/envios/drenar ───────────────────────────────────────

func TestDrenar_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	rec := do(buildRouterNoAuth(svc), http.MethodPost, "/reactivacion/envios/drenar")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestDrenar_LeerPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodPost, "/reactivacion/envios/drenar")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDrenar_AdminPerm_200_ReportaEnviados(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{}
	m, err := domain.CrearMensaje(domain.CrearMensajeParams{
		ClienteID: 401, Segmento: domain.SegmentoRecienLiquidado,
		Telefono: "238 111 2222", Cuerpo: "cuerpo de prueba", Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, mensajeRepo.Insertar(context.Background(), []*domain.Mensaje{m}))

	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, mensajeRepo, fakeSender{}, true)

	rec := do(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/envios/drenar")
	require.Equal(t, http.StatusOK, rec.Code)

	var dto reactivacionhttp.DrenarResultDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, 1, dto.Enviados)
	assert.Equal(t, 0, dto.Fallidos)
	assert.Equal(t, 0, dto.Saltados)
}

func TestDrenar_AutoSendOff_TodoSaltado(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{}
	m, err := domain.CrearMensaje(domain.CrearMensajeParams{
		ClienteID: 402, Segmento: domain.SegmentoRecienLiquidado,
		Telefono: "238 111 2222", Cuerpo: "cuerpo de prueba", Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, mensajeRepo.Insertar(context.Background(), []*domain.Mensaje{m}))

	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, mensajeRepo, fakeSender{}, false)

	rec := do(buildRouter(svc, userWith(auth.PermReactivacionAdministrar)), http.MethodPost, "/reactivacion/envios/drenar")
	require.Equal(t, http.StatusOK, rec.Code)

	var dto reactivacionhttp.DrenarResultDTO
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dto))
	assert.Equal(t, 0, dto.Enviados)
	assert.Equal(t, 1, dto.Saltados)
}

// ─── GET /reactivacion/envios ────────────────────────────────────────────────

func TestListEnvios_NoAuth_401(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	rec := do(buildRouterNoAuth(svc), http.MethodGet, "/reactivacion/envios")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListEnvios_NoPerm_403(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	rec := do(buildRouter(svc, userWith()), http.MethodGet, "/reactivacion/envios")
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestListEnvios_HappyPath_200(t *testing.T) {
	t.Parallel()
	mensajeRepo := &fakeMensajeRepo{}
	m, err := domain.CrearMensaje(domain.CrearMensajeParams{
		ClienteID: 501, Segmento: domain.SegmentoPorLiquidarHueco,
		Telefono: "238 111 2222", Cuerpo: "cuerpo de prueba", Now: fixedNow,
	})
	require.NoError(t, err)
	require.NoError(t, mensajeRepo.Insertar(context.Background(), []*domain.Mensaje{m}))

	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, mensajeRepo, fakeSender{}, true)

	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/envios")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Items []reactivacionhttp.MensajeDTO `json:"items"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, 501, body.Items[0].ClienteID)
	assert.Equal(t, "por_liquidar_hueco", body.Items[0].Segmento)
	assert.Equal(t, "encolado", body.Items[0].Estado)
	assert.Nil(t, body.Items[0].EnviadoEn)
}

func TestListEnvios_FiltraPorEstadoInvalido_400(t *testing.T) {
	t.Parallel()
	svc := buildServiceWithCanal(&fakeReader{}, &fakeRepo{}, &fakeMensajeRepo{}, fakeSender{}, true)
	rec := do(buildRouter(svc, userWith(auth.PermReactivacionLeer)), http.MethodGet, "/reactivacion/envios?estado=no_existe")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}
