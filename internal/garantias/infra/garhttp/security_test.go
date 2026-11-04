//nolint:misspell // vocabulario español del módulo.
package garhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/abdimuy/msp-api/internal/garantias/domain"
	"github.com/abdimuy/msp-api/internal/garantias/ports/outbound"
)

type seguridadOperacion struct {
	ejemplo ejemploOperacion
	permiso domain.Permiso
}

type seguridadIdentity struct {
	sinUsuario  bool
	permitidos  map[domain.Permiso]bool
	consultados []domain.Permiso
}

func (s *seguridadIdentity) UsuarioActual(
	context.Context,
) (outbound.Usuario, error) {
	if s.sinUsuario {
		return outbound.Usuario{}, domain.ErrUsuarioNoAutenticado
	}

	return outbound.Usuario{
		ID:     "usuario-seguridad",
		Nombre: "Ruben",
	}, nil
}

func (s *seguridadIdentity) TienePermiso(
	_ context.Context,
	p domain.Permiso,
) (bool, error) {
	s.consultados = append(s.consultados, p)
	return s.permitidos[p], nil
}

var permisosSeguridad = []domain.Permiso{
	domain.PermisoLeer,
	domain.PermisoCrear,
	domain.PermisoActualizar,
	domain.PermisoAutorizar,
	domain.PermisoCerrar,
}

func tablaSeguridad(t *testing.T) []seguridadOperacion {
	t.Helper()

	permisosComandos := map[string]domain.Permiso{
		"abrir-garantia":          domain.PermisoCrear,
		"agregar-articulo":        domain.PermisoCrear,
		"avanzar-articulo":        domain.PermisoActualizar,
		"iniciar-proceso":         domain.PermisoActualizar,
		"marcar-listo-entrega":    domain.PermisoActualizar,
		"registrar-diagnostico":   domain.PermisoActualizar,
		"registrar-dictamen":      domain.PermisoActualizar,
		"mover-articulo":          domain.PermisoActualizar,
		"autorizar-cambio-fisico": domain.PermisoAutorizar,
		"marcar-no-devuelto":      domain.PermisoAutorizar,
		"registrar-desenlace":     domain.PermisoAutorizar,
		"entregar-garantia":       domain.PermisoCerrar,
		"cerrar-garantia":         domain.PermisoCerrar,
		"cancelar-garantia":       domain.PermisoCerrar,
	}

	operaciones := []seguridadOperacion{
		{
			ejemplo: ejemploOperacion{
				OperationID: "listar-garantias",
				Method:      http.MethodGet,
				Path:        "/garantias",
			},
			permiso: domain.PermisoLeer,
		},
		{
			ejemplo: ejemploOperacion{
				OperationID: "obtener-garantia",
				Method:      http.MethodGet,
				Path: "/garantias/" +
					idGarantiaEjemplo,
			},
			permiso: domain.PermisoLeer,
		},
		{
			ejemplo: ejemploOperacion{
				OperationID: "obtener-imagen",
				Method:      http.MethodGet,
				Path: "/garantias/" + idGarantiaEjemplo +
					"/eventos/33333333-3333-3333-3333-333333333333" +
					"/imagenes/55555555-5555-5555-5555-555555555555",
			},
			permiso: domain.PermisoLeer,
		},
		{
			ejemplo: ejemploAdjuntarSeguridad(t),
			permiso: domain.PermisoActualizar,
		},
	}

	for _, ejemplo := range ejemplosComandos() {
		permiso, ok := permisosComandos[ejemplo.OperationID]
		require.True(
			t,
			ok,
			"comando sin permiso en tabla: %s",
			ejemplo.OperationID,
		)

		operaciones = append(operaciones, seguridadOperacion{
			ejemplo: ejemplo,
			permiso: permiso,
		})
	}

	return operaciones
}

func ejemploAdjuntarSeguridad(t *testing.T) ejemploOperacion {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	require.NoError(
		t,
		writer.WriteField("descripcion", "evidencia de seguridad"),
	)

	parte, err := writer.CreateFormFile(
		"archivo",
		"evidencia.png",
	)
	require.NoError(t, err)

	_, err = parte.Write(append(
		[]byte("\x89PNG\r\n\x1a\n"),
		[]byte("evidencia")...,
	))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return ejemploOperacion{
		OperationID: "adjuntar-imagen",
		Method:      http.MethodPost,
		Path: "/garantias/" + idGarantiaEjemplo +
			"/eventos/33333333-3333-3333-3333-333333333333/imagenes",
		ContentType: writer.FormDataContentType(),
		Body:        body.Bytes(),
	}
}

func nuevoRouterSeguridad(
	identity outbound.Identity,
) (
	http.Handler,
	*fakeComandoGarantiaRepo,
	*fakeComandoImagenRepo,
	*fakeComandoStorage,
) {
	imagenes := &fakeComandoImagenRepo{
		porID: make(map[uuid.UUID]*domain.Imagen),
	}
	storage := &fakeComandoStorage{}
	eventos := &fakeComandoEventoRepo{}

	garantias := &fakeComandoGarantiaRepo{
		imagenes: imagenes,
		eventos:  eventos,
	}

	router := buildCommandRouterCon(
		identity,
		garantias,
		eventos,
		imagenes,
		storage,
	)

	return router, garantias, imagenes, storage
}

func enviarSeguridad(
	router http.Handler,
	ejemplo ejemploOperacion,
) *httptest.ResponseRecorder {
	var body *bytes.Reader
	if ejemplo.Body == nil {
		body = bytes.NewReader(nil)
	} else {
		body = bytes.NewReader(ejemplo.Body)
	}

	req := httptest.NewRequest(
		ejemplo.Method,
		ejemplo.Path,
		body,
	)

	if ejemplo.ContentType != "" {
		req.Header.Set("Content-Type", ejemplo.ContentType)
	}

	if ejemplo.Method == http.MethodPost {
		req.Header.Set("Idempotency-Key", claveEjemplo)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	return rec
}

func assertSinEscriturasSeguridad(
	t *testing.T,
	garantias *fakeComandoGarantiaRepo,
	imagenes *fakeComandoImagenRepo,
	storage *fakeComandoStorage,
) {
	t.Helper()

	require.Empty(t, garantias.creados)
	require.Zero(t, garantias.guardados)
	require.Empty(t, imagenes.registradas)

	// Entregar escribe la firma temporal antes de llegar a autorización,
	// pero el defer debe eliminarla. No puede quedar ningún blob huérfano.
	for _, key := range storage.guardados {
		require.Contains(t, storage.borrados, key)
	}
}

func plantillaOpenAPI(path string) string {
	reemplazos := []struct {
		valor     string
		plantilla string
	}{
		{idGarantiaEjemplo, "{id}"},
		{idArticuloEjemplo, "{aid}"},
		{"33333333-3333-3333-3333-333333333333", "{eid}"},
		{"55555555-5555-5555-5555-555555555555", "{iid}"},
	}

	for _, reemplazo := range reemplazos {
		path = strings.ReplaceAll(
			path,
			reemplazo.valor,
			reemplazo.plantilla,
		)
	}

	return path
}

func TestSeguridad_TablaCoincideConOpenAPI(t *testing.T) {
	t.Parallel()

	identity := &seguridadIdentity{
		permitidos: map[domain.Permiso]bool{},
	}

	router, _, _, _ := nuevoRouterSeguridad(identity)

	req := httptest.NewRequest(
		http.MethodGet,
		"/openapi.json",
		nil,
	)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &doc))

	type ruta struct {
		method string
		path   string
	}

	openAPI := make(map[string]ruta)

	for path, item := range doc.Paths {
		for method, raw := range item {
			switch strings.ToLower(method) {
			case "get", "post", "put", "patch", "delete":
			default:
				continue
			}

			var op struct {
				OperationID string `json:"operationId"`
			}
			require.NoError(t, json.Unmarshal(raw, &op))

			if op.OperationID == "" {
				continue
			}

			_, repetida := openAPI[op.OperationID]
			require.False(
				t,
				repetida,
				"operationId duplicado: %s",
				op.OperationID,
			)

			openAPI[op.OperationID] = ruta{
				method: strings.ToUpper(method),
				path:   path,
			}
		}
	}

	tabla := tablaSeguridad(t)
	require.Len(t, openAPI, len(tabla))

	vistas := make(map[string]bool, len(tabla))

	for _, fila := range tabla {
		id := fila.ejemplo.OperationID

		require.False(
			t,
			vistas[id],
			"fila de seguridad duplicada: %s",
			id,
		)
		vistas[id] = true

		got, ok := openAPI[id]
		require.True(
			t,
			ok,
			"OpenAPI tiene que contener %s",
			id,
		)

		require.Equal(t, fila.ejemplo.Method, got.method, id)
		require.Equal(
			t,
			plantillaOpenAPI(fila.ejemplo.Path),
			got.path,
			id,
		)
	}
}

func TestSeguridad_TodasLasOperacionesSinUsuario_401(t *testing.T) {
	t.Parallel()

	for _, fila := range tablaSeguridad(t) {
		fila := fila

		t.Run(fila.ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			identity := &seguridadIdentity{
				sinUsuario: true,
				permitidos: map[domain.Permiso]bool{},
			}

			router, garantias, imagenes, storage :=
				nuevoRouterSeguridad(identity)

			rec := enviarSeguridad(router, fila.ejemplo)

			require.Equal(
				t,
				http.StatusUnauthorized,
				rec.Code,
				rec.Body.String(),
			)

			require.Empty(t, identity.consultados)
			assertSinEscriturasSeguridad(
				t,
				garantias,
				imagenes,
				storage,
			)
		})
	}
}

func TestSeguridad_PermisosIncorrectos_403(t *testing.T) {
	t.Parallel()

	for _, fila := range tablaSeguridad(t) {
		fila := fila

		t.Run(fila.ejemplo.OperationID, func(t *testing.T) {
			t.Parallel()

			casos := []struct {
				nombre     string
				permitidos map[domain.Permiso]bool
			}{
				{
					nombre:     "sin_permiso",
					permitidos: map[domain.Permiso]bool{},
				},
			}

			for _, permiso := range permisosSeguridad {
				if permiso == fila.permiso {
					continue
				}

				casos = append(casos, struct {
					nombre     string
					permitidos map[domain.Permiso]bool
				}{
					nombre: strings.TrimPrefix(
						permiso.String(),
						"garantias:",
					),
					permitidos: map[domain.Permiso]bool{
						permiso: true,
					},
				})
			}

			for _, caso := range casos {
				caso := caso

				t.Run(caso.nombre, func(t *testing.T) {
					t.Parallel()

					identity := &seguridadIdentity{
						permitidos: caso.permitidos,
					}

					router, garantias, imagenes, storage :=
						nuevoRouterSeguridad(identity)

					rec := enviarSeguridad(
						router,
						fila.ejemplo,
					)

					require.Equal(
						t,
						http.StatusForbidden,
						rec.Code,
						rec.Body.String(),
					)

					require.Equal(
						t,
						[]domain.Permiso{fila.permiso},
						identity.consultados,
						"%s debe pedir exactamente %s",
						fila.ejemplo.OperationID,
						fila.permiso,
					)

					assertSinEscriturasSeguridad(
						t,
						garantias,
						imagenes,
						storage,
					)
				})
			}
		})
	}
}
