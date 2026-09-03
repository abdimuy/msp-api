//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the visitas module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// registerOperations declares operations at the bare paths "/visitas",
// "/visitas/{id}", and "/visitas/by-ids" — mount this on a bare chi Group
// (like rutas/clientes/config), NOT via r.Route("/visitas"), which would
// double the prefix to /v2/visitas/visitas. Final paths: POST /v2/visitas,
// GET /v2/visitas/{id}, and GET /v2/visitas/by-ids.
//
// "by-ids" is a literal path segment sibling to the "{id}" parameter at the
// same level. chi's router matches static segments before parameterized
// ones regardless of registration order, so GET /visitas/by-ids always
// resolves to VisitasExistentesPorIDs, never to ObtenerVisita with id=
// "by-ids" — TestMountRouter_ByIDsDoesNotShadowObtenerVisita in routes_test.go
// pins this down explicitly.
func MountRouter(r chi.Router, svc *visitasapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Visitas", "v2")
	config.DocsRenderer = huma.DocsRendererScalar
	if config.Components == nil {
		config.Components = &huma.Components{}
	}
	if config.Components.SecuritySchemes == nil {
		config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	config.Components.SecuritySchemes[securitySchemeName] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Token de Firebase ID propagado al backend como Bearer.",
	}
	if config.OpenAPI != nil {
		config.Servers = append(config.Servers, &huma.Server{URL: "/v2"})
	}

	api := humachi.New(r, config)
	handlers := NewHandlers(svc)
	registerOperations(api, handlers)
	return api
}

// registerOperations declares every Huma operation the visitas module exposes.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"visitas"}

	huma.Register(api, huma.Operation{
		OperationID:   "crear-visita",
		Method:        http.MethodPost,
		Path:          "/visitas",
		Summary:       "Registrar visita de cobranza",
		Description:   "Registra que un cobrador visitó a un cliente en su ruta, haya generado pago o no. Idempotente en el id enviado por el cliente.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusCreated,
	}, h.CrearVisita)

	huma.Register(api, huma.Operation{
		OperationID: "obtener-visita",
		Method:      http.MethodGet,
		Path:        "/visitas/{id}",
		Summary:     "Obtener visita por id",
		Description: "Devuelve una visita de MSP_VISITAS por UUID. Lee filas legacy tal cual: el tipo_visita devuelto puede quedar fuera del catálogo cerrado que valida la escritura.",
		Tags:        tags,
		Security:    security,
	}, h.ObtenerVisita)

	huma.Register(api, huma.Operation{
		OperationID: "visitas-existentes-por-ids",
		Method:      http.MethodGet,
		Path:        "/visitas/by-ids",
		Summary:     "Confirmar visitas existentes por ids",
		Description: "Canal del reconciliador de visitas: de una lista de UUIDs generados y subidos por el propio teléfono, devuelve el subconjunto que el servidor ya tiene. No es un canal de descarga — un id desconocido simplemente no aparece en la respuesta, nunca es un error. Máximo " + strconv.Itoa(maxIDsPorRequest) + " ids por request.",
		Tags:        tags,
		Security:    security,
	}, h.VisitasExistentesPorIDs)
}
