package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/abdimuy/msp-api/internal/auth"
	authdomain "github.com/abdimuy/msp-api/internal/auth/domain"
	"github.com/abdimuy/msp-api/internal/auth/infra/authhttp"
	"github.com/abdimuy/msp-api/internal/garantias"
	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/infra/garhttp"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/idempotency"
)

func TestGarantiasWiring_ModuloEnGrafo(t *testing.T) {
	t.Parallel()

	opts := append(
		appOptions(),
		fx.Invoke(func(garhttp.Deps) {}),
	)

	require.NoError(t, fx.ValidateApp(opts...))
}

func TestGarantiasWiring_UsuarioDesdeAuth(t *testing.T) {
	t.Parallel()

	usuarioID := uuid.MustParse("00000000-0000-0000-0000-000000000123")
	ctx := auth.PlantCurrentUser(context.Background(), auth.CurrentUser{
		ID:       usuarioID,
		Nombre:   "Rubén",
		Permisos: []string{"garantias:leer"},
	})

	id, nombre, ok := provideGarantiasUsuarioFn()(ctx)

	require.True(t, ok)
	require.Equal(t, usuarioID.String(), id)
	require.Equal(t, "Rubén", nombre)
}

func TestGarantiasWiring_UsuarioAnonimo(t *testing.T) {
	t.Parallel()

	id, nombre, ok := provideGarantiasUsuarioFn()(context.Background())

	require.False(t, ok)
	require.Empty(t, id)
	require.Empty(t, nombre)
}

func TestGarantiasWiring_PermisosDesdeAuth(t *testing.T) {
	t.Parallel()

	ctx := auth.PlantCurrentUser(context.Background(), auth.CurrentUser{
		ID:       uuid.New(),
		Nombre:   "Rubén",
		Permisos: []string{"garantias:leer", "garantias:actualizar"},
	})

	permiso := provideGarantiasPermisoFn()

	tiene, err := permiso(ctx, "garantias:leer")
	require.NoError(t, err)
	require.True(t, tiene)

	tiene, err = permiso(ctx, "garantias:actualizar")
	require.NoError(t, err)
	require.True(t, tiene)

	tiene, err = permiso(ctx, "garantias:cerrar")
	require.NoError(t, err)
	require.False(t, tiene)
}

func TestGarantiasWiring_PermisoSinUsuario(t *testing.T) {
	t.Parallel()

	tiene, err := provideGarantiasPermisoFn()(
		context.Background(),
		"garantias:leer",
	)

	require.NoError(t, err)
	require.False(t, tiene)
}

func TestGarantiasWiring_PermisosCoincidenConCatalogoAuth(t *testing.T) {
	t.Parallel()

	esperados := garantias.Permisos()
	actuales := make(map[string]authdomain.PermissionMeta)

	for _, permiso := range authdomain.AllPermissions() {
		if permiso.Categoria == "garantias" {
			actuales[permiso.Code.String()] = permiso
		}
	}

	require.Len(t, actuales, len(esperados))

	for _, esperado := range esperados {
		actual, ok := actuales[esperado.Codigo]
		require.Truef(
			t,
			ok,
			"falta permiso de garantías %q en auth",
			esperado.Codigo,
		)
		require.Equal(t, esperado.Description, actual.Description)
		require.Equal(t, "garantias", actual.Categoria)
	}
}

type identidadRouterPrueba struct{}

func (identidadRouterPrueba) UsuarioActual(context.Context) (outbound.Usuario, error) {
	return outbound.Usuario{
		ID:     "usuario-prueba",
		Nombre: "Operador",
	}, nil
}

func (identidadRouterPrueba) TienePermiso(context.Context, domain.Permiso) (bool, error) {
	return false, nil
}

func routerGarantiasPrueba() http.Handler {
	r := chi.NewRouter()

	authn := authhttp.NewAuthnMiddleware(nil, nil, nil)

	deps := garhttp.Deps{
		Consultas: app.NewConsultas(app.ConsultasDeps{}),
		Comandos:  app.NewService(app.Deps{Identity: identidadRouterPrueba{}}),
	}

	r.Route("/v2", func(r chi.Router) {
		montarGarantias(r, authn.Handler, deps)
	})

	return r
}

func TestGarantiasRouter_SinToken401(t *testing.T) {
	t.Parallel()
	router := routerGarantiasPrueba()

	req := httptest.NewRequest(
		http.MethodGet,
		"/v2/garantias",
		nil,
	)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestGarantiasRouter_OpenAPI19Operaciones(t *testing.T) {
	t.Parallel()
	router := routerGarantiasPrueba()

	req := httptest.NewRequest(
		http.MethodGet,
		"/v2/openapi.json",
		nil,
	)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))

	ids := make(map[string]bool)

	for _, metodos := range doc.Paths {
		for metodo, raw := range metodos {
			switch metodo {
			case "get", "post", "put", "patch", "delete":
			default:
				continue
			}

			var op struct {
				OperationID string `json:"operationId"` //nolint:tagliatelle // OpenAPI exige operationId.
			}

			require.NoError(t, json.Unmarshal(raw, &op))
			require.NotEmpty(t, op.OperationID)
			require.False(t, ids[op.OperationID],
				"operationId duplicado: %s", op.OperationID)

			ids[op.OperationID] = true
		}
	}

	require.Len(t, ids, 19)
	require.True(t, ids["obtener-catalogos"])
	require.True(t, ids["listar-garantias"])
	require.True(t, ids["obtener-garantia"])
}

// TestGarantiasRouter_PostRepetidoSinReplay comprueba que el
// montaje HTTP no agregue una respuesta de replay a un POST.
func TestGarantiasRouter_PostRepetidoSinReplay(t *testing.T) {
	t.Parallel()
	router := routerGarantiasPrueba()

	clave := "11111111-2222-4333-8444-555555555555"
	body := `{"device_created_at":"2026-11-11T14:00:00Z"}`

	for intento := 1; intento <= 2; intento++ {
		req := httptest.NewRequest(
			http.MethodPost,
			"/v2/garantias/11111111-1111-4111-8111-111111111111/iniciar-proceso",
			strings.NewReader(body),
		)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", clave)

		ctx := auth.PlantCurrentUser(req.Context(), auth.CurrentUser{
			ID:       uuid.New(),
			Nombre:   "Operador",
			Permisos: []string{},
		})
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code,
			"intento %d: %s", intento, rec.Body.String())
		require.Empty(t, rec.Header().Get("Idempotent-Replay"))
	}
}

// almacenReplayPrueba simula una respuesta guardada por el middleware HTTP.
type almacenReplayPrueba struct {
	registro  idempotency.Record
	consultas int
}

func (a *almacenReplayPrueba) Get(
	_ context.Context, key string,
) (*idempotency.Record, error) {
	a.consultas++
	if key != a.registro.Key {
		return nil, nil
	}
	return &a.registro, nil
}

func (a *almacenReplayPrueba) Save(
	_ context.Context, _ idempotency.Record,
) error {
	return nil
}

func TestGarantiasRouter_ControlPositivoMiddlewareReplay(t *testing.T) {
	t.Parallel()
	body := []byte(`{"device_created_at":"2026-11-11T14:00:00Z"}`)
	clave := "11111111-2222-4333-8444-555555555555"
	ruta := "/v2/garantias/11111111-1111-4111-8111-111111111111/iniciar-proceso"

	hash := sha256.Sum256(body)
	almacen := &almacenReplayPrueba{
		registro: idempotency.Record{
			Key:            clave,
			Method:         http.MethodPost,
			Path:           ruta,
			RequestHash:    hex.EncodeToString(hash[:]),
			ResponseStatus: http.StatusOK,
			ResponseBody:   json.RawMessage(`{"replay":true}`),
		},
	}

	// Simula expresamente el middleware que NO debe estar en Garantías.
	conMiddleware := idempotency.Middleware(idempotency.Config{
		Store: almacen,
	})(routerGarantiasPrueba())

	req := httptest.NewRequest(
		http.MethodPost, ruta, bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", clave)

	rec := httptest.NewRecorder()
	conMiddleware.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", rec.Header().Get("Idempotent-Replay"))
	require.Equal(t, 1, almacen.consultas)
	require.JSONEq(t, `{"replay":true}`, rec.Body.String())

	// Sin el middleware general, debe responder la aplicación.
	sinMiddleware := routerGarantiasPrueba()

	req2 := httptest.NewRequest(
		http.MethodPost, ruta, bytes.NewReader(body),
	)
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", clave)

	rec2 := httptest.NewRecorder()
	sinMiddleware.ServeHTTP(rec2, req2)

	require.Equal(t, http.StatusUnauthorized, rec2.Code, rec2.Body.String())
	require.Empty(t, rec2.Header().Get("Idempotent-Replay"))
	require.Equal(t, 1, almacen.consultas)
}

// TestGarantiasWiring_MontajeSinMiddlewareIdem comprueba que el
// bloque de Garantías no incorpore el middleware HTTP general.
// Complementa las pruebas HTTP sobre montarGarantias.
func TestGarantiasWiring_MontajeSinMiddlewareIdem(t *testing.T) {
	t.Parallel()
	fuente, err := os.ReadFile("server.go")
	require.NoError(t, err)

	s := string(fuente)

	inicio := strings.Index(s, "// Garantías — autenticación común")
	require.NotEqual(t, -1, inicio)

	fin := strings.Index(s[inicio:], "// Desempeño de cobradores")
	require.NotEqual(t, -1, fin)

	bloque := s[inicio : inicio+fin]

	require.Contains(t, bloque,
		"montarGarantias(r, authn.Handler, garantiasHTTP)")

	require.NotContains(t, bloque, "r.Use(idem)")
	require.NotContains(t, bloque, "r.With(idem)")
	require.NotContains(t, bloque, "idempotency.Middleware(")

	// También protege el montaje compartido.
	inicioHelper := strings.Index(s, "func montarGarantias(")
	require.NotEqual(t, -1, inicioHelper)

	finHelper := strings.Index(s[inicioHelper:], "// provideHTTPServer")
	require.NotEqual(t, -1, finHelper)

	helper := s[inicioHelper : inicioHelper+finHelper]

	require.Contains(t, helper, "garhttp.MountRouter(r, deps)")
	require.Contains(t, helper, "skipAuthForPublicDocs(authn)")
	require.NotContains(t, helper, "idempotency.Middleware(")
	require.NotContains(t, helper, "r.Use(idem)")
	require.NotContains(t, helper, "r.With(idem)")
}
