package confighttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	configapp "github.com/abdimuy/msp-api/internal/config/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the config module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// The function builds a fresh Huma API rooted at r, declares the bearer
// security scheme used by every operation, and registers each operation
// with its OperationID, path, method, summary, and default status code.
// It returns the constructed huma.API so callers can introspect it.
func MountRouter(r chi.Router, svc *configapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Config", "v2")
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

// registerOperations declares every Huma operation the config module exposes.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"config"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-config-vendedores",
		Method:        http.MethodGet,
		Path:          "/config/vendedores",
		Summary:       "Listar mapeo vendedor → Microsip",
		Description:   "Devuelve, por cada usuario de la aplicación, su mapeo a los tres slots de vendedor de crédito de Microsip (VENDEDOR_1/2/3).",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarVendedores)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-config-vendedores-opciones",
		Method:        http.MethodGet,
		Path:          "/config/vendedores/opciones",
		Summary:       "Listar identidades de vendedor disponibles en Microsip",
		Description:   "Devuelve las identidades de vendedor de crédito del catálogo LISTAS_ATRIBUTOS de Microsip, agrupadas por nombre, para poblar el selector de asignación.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarOpciones)

	huma.Register(api, huma.Operation{
		OperationID:   "asignar-config-vendedor",
		Method:        http.MethodPut,
		Path:          "/config/vendedores/{usuarioId}",
		Summary:       "Asignar mapeo vendedor → Microsip",
		Description:   "Crea o actualiza el mapeo de un usuario a los tres slots de vendedor de crédito de Microsip.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.AsignarVendedor)

	huma.Register(api, huma.Operation{
		OperationID:   "eliminar-config-vendedor",
		Method:        http.MethodDelete,
		Path:          "/config/vendedores/{usuarioId}",
		Summary:       "Eliminar mapeo vendedor → Microsip",
		Description:   "Elimina por completo el mapeo de un usuario a los slots de vendedor de crédito de Microsip.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.EliminarVendedor)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-config-zonas-cajas",
		Method:        http.MethodGet,
		Path:          "/config/zonas-cajas",
		Summary:       "Listar mapeo zona → caja/cajero/vendedor/cobrador",
		Description:   "Devuelve, por cada zona de cliente, su mapeo a caja, cajero, vendedor y cobrador de Microsip.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarZonasCajas)

	huma.Register(api, huma.Operation{
		OperationID:   "listar-config-zonas-cajas-opciones",
		Method:        http.MethodGet,
		Path:          "/config/zonas-cajas/opciones",
		Summary:       "Listar catálogos de zonas/cajas",
		Description:   "Devuelve los catálogos de Microsip (zonas, cajas, cajeros, vendedores, cobradores) para poblar los selectores de asignación.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarOpcionesZonasCajas)

	huma.Register(api, huma.Operation{
		OperationID:   "asignar-config-zona-caja",
		Method:        http.MethodPut,
		Path:          "/config/zonas-cajas/{zonaClienteId}",
		Summary:       "Asignar mapeo zona → caja/cajero/vendedor/cobrador",
		Description:   "Crea o actualiza el mapeo de una zona de cliente a caja, cajero, vendedor y cobrador de Microsip.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.AsignarZonaCaja)
}
