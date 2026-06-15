//nolint:misspell // clientes vocabulary is Spanish per project convention.
package clienteshttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	clientesapp "github.com/abdimuy/msp-api/internal/clientes/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the clientes module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// The function builds a fresh Huma API rooted at r, declares the bearer
// security scheme used by every operation, and registers each operation with
// its OperationID, path, method, summary, and default status code.
// It returns the constructed huma.API so callers can introspect it (for
// example, to assert that the right routes are mounted in tests).
func MountRouter(r chi.Router, svc *clientesapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Clientes", "v2")
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

// registerOperations declares every Huma operation the clientes module exposes.
//
//nolint:funlen // five endpoints — keep together for reviewability.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"clientes"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-clientes",
		Method:        http.MethodGet,
		Path:          "/clientes",
		Summary:       "Listar directorio de clientes",
		Description:   "Devuelve una página del directorio de clientes con enriquecimiento opcional de pulso analítico. Soporta búsqueda de texto completo, filtros de zona/cobrador/saldo y filtros de segmento/estado-pago/score.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarClientes)

	huma.Register(api, huma.Operation{
		OperationID:   "obtener-cliente",
		Method:        http.MethodGet,
		Path:          "/clientes/{id}",
		Summary:       "Obtener ficha de cliente",
		Description:   "Devuelve la vista 360 de un cliente: identidad, resumen financiero, series de tiempo y pulso analítico (si está disponible).",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ObtenerFicha)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-ventas-cliente",
		Method:        http.MethodGet,
		Path:          "/clientes/{id}/ventas",
		Summary:       "Listar ventas de un cliente",
		Description:   "Devuelve una página con los encabezados de venta del cliente ordenados por fecha descendente.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarVentasCliente)

	huma.Register(api, huma.Operation{
		OperationID:   "obtener-venta-detalle",
		Method:        http.MethodGet,
		Path:          "/clientes/{id}/ventas/{doctoPvId}",
		Summary:       "Obtener detalle de venta",
		Description:   "Devuelve el detalle completo de una venta: encabezado, líneas de productos, contrato de crédito (si aplica) e historial de pagos.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ObtenerVentaDetalle)

	huma.Register(api, huma.Operation{
		OperationID:   "refrescar-busqueda-clientes",
		Method:        http.MethodPost,
		Path:          "/clientes/_search/refresh",
		Summary:       "Refrescar índice de búsqueda de clientes",
		Description:   "Reconstruye el índice de texto completo desde el directorio de clientes actual. Operación sincrónica — devuelve 200 con el número de documentos indexados.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.RefrescarBusqueda)
}
