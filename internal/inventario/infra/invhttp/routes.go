//nolint:misspell // inventario vocabulary is Spanish (traspaso, almacén, artículo, etc.) per project convention.
package invhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	inventarioapp "github.com/abdimuy/msp-api/internal/inventario/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the inventario module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication and
// idempotency chi middlewares so handlers can read auth.CurrentUser from
// the request context directly.
//
// The function builds a fresh Huma API rooted at r, declares the bearer
// security scheme used by every operation, and registers each operation
// with its OperationID, path, method, summary, and default status code.
// It returns the constructed huma.API so callers can introspect it (for
// example, to assert that the right routes are mounted in tests).
func MountRouter(r chi.Router, svc *inventarioapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Inventario", "v2")
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

// registerOperations declares every Huma operation the inventario module
// exposes.
//
//nolint:funlen // one Huma.Register call per operation; mechanical wiring.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"inventario"}

	huma.Register(api, huma.Operation{
		OperationID:   "obtener-traspaso",
		Method:        http.MethodGet,
		Path:          "/traspasos/{id}",
		Summary:       "Obtener traspaso",
		Description:   "Devuelve el traspaso identificado por su DOCTO_IN_ID de Microsip con sus líneas de artículos.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ObtenerTraspaso)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-traspasos-por-venta",
		Method:        http.MethodGet,
		Path:          "/traspasos",
		Summary:       "Listar traspasos por venta",
		Description:   "Devuelve todos los traspasos vinculados a la venta especificada por venta_id, ordenados cronológicamente.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarTraspasosPorVenta)

	huma.Register(api, huma.Operation{
		OperationID:   "consultar-stock",
		Method:        http.MethodGet,
		Path:          "/inventario/stock",
		Summary:       "Consultar stock",
		Description:   "Devuelve la existencia actual de un artículo en un almacén específico.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ConsultarStock)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-almacenes",
		Method:        http.MethodGet,
		Path:          "/inventario/almacenes",
		Summary:       "Listar almacenes",
		Description:   "Devuelve el catálogo completo de almacenes registrados en Microsip.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarAlmacenes)
}
