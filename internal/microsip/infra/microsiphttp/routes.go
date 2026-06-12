//nolint:misspell // Spanish vocabulary (clientes) per project convention.
package microsiphttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	microsipapp "github.com/abdimuy/msp-api/internal/microsip/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation in this module.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the microsip module's read-only catalog endpoints
// onto r. The supplied chi router is expected to have already applied the
// authentication middleware so handlers can trust the calling identity;
// the module itself enforces no extra permission gates (any authenticated
// user may read catalogs).
//
// The returned huma.API is exposed so callers (tests, /v2/openapi) can
// introspect the operation table.
func MountRouter(r chi.Router, svc *microsipapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Microsip", "v2")
	// The ventas Huma API already owns /v2/docs, /v2/openapi.* and
	// /v2/schemas/* on this same chi router; disable them here to avoid
	// duplicate route registrations. The microsip operations still
	// contribute their schemas to the ventas-rooted spec via the shared
	// chi router (humachi attaches at the chi level), and the operations
	// themselves are registered below. Clients introspect the API via the
	// ventas docs entry-point at /v2/docs.
	config.DocsPath = ""
	config.OpenAPIPath = ""
	config.SchemasPath = ""
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
	registerOperations(api, NewHandlers(svc))
	return api
}

// registerOperations declares every Huma operation the microsip module
// exposes. All four are GET-only reads, secured by bearer token.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"microsip"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-almacenes",
		Method:        http.MethodGet,
		Path:          "/almacenes",
		Summary:       "Listar almacenes",
		Description:   "Devuelve la lista de almacenes visibles de Microsip con su total de existencias (entradas - salidas) en orden descendente.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarAlmacenes)

	huma.Register(api, huma.Operation{
		OperationID:   "obtener-almacen",
		Method:        http.MethodGet,
		Path:          "/almacenes/{id}",
		Summary:       "Obtener almacén",
		Description:   "Devuelve los datos básicos del almacén (id, nombre, existencias totales).",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ObtenerAlmacen)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-articulos-almacen",
		Method:        http.MethodGet,
		Path:          "/almacenes/{id}/articulos",
		Summary:       "Listar artículos del almacén",
		Description:   "Devuelve los artículos con existencias positivas en el almacén, filtrados opcionalmente por un substring en el nombre. La cadena 'precios' es la concatenación legacy <lista>:<precio> separada por comas.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarArticulosDelAlmacen)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-zonas-cliente",
		Method:        http.MethodGet,
		Path:          "/zonas-cliente",
		Summary:       "Listar zonas de cliente",
		Description:   "Devuelve las zonas de cliente de Microsip con el cobrador principal (mayor cantidad de clientes) concatenado al nombre.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarZonasCliente)
}
