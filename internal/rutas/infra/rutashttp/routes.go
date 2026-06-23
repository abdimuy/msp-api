//nolint:misspell // rutas vocabulary is Spanish per project convention.
package rutashttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	rutasapp "github.com/abdimuy/msp-api/internal/rutas/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the rutas module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// The function builds a fresh Huma API rooted at r, declares the bearer
// security scheme used by every operation, and registers each operation
// with its OperationID, path, method, summary, and default status code.
// It returns the constructed huma.API so callers can introspect it.
func MountRouter(r chi.Router, svc *rutasapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Rutas", "v2")
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

// registerOperations declares every Huma operation the rutas module exposes.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"rutas"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-rutas",
		Method:        http.MethodGet,
		Path:          "/rutas",
		Summary:       "Listar zonas (rutas)",
		Description:   "Devuelve todas las zonas de ventas con cobrador asignado, número de clientes activos y saldo pendiente total.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarRutas)

	huma.Register(api, huma.Operation{
		OperationID:   "desglose-cobranza-por-zona",
		Method:        http.MethodGet,
		Path:          "/rutas/{zona_id}/cobranza",
		Summary:       "Desglose de cobranza semanal por zona",
		Description:   "Devuelve el detalle por venta del reporte semanal de cobranza para una zona: abono, cuotas vencidas, aporte y saldo.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.DesglosePorZona)
}
