//nolint:misspell // ventas vocabulary is Spanish per project convention.
package venthttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	ventasapp "github.com/abdimuy/msp-api/internal/ventas/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the ventas module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication and
// idempotency chi middlewares so handlers can read auth.CurrentUser from
// the request context directly.
//
// The function builds a fresh Huma API rooted at r, declares the bearer
// security scheme used by every operation, and registers each operation
// with its OperationID, path, method, summary, and default status code.
// It returns the constructed huma.API so callers can introspect it (for
// example, to assert that the right routes are mounted in tests).
func MountRouter(r chi.Router, svc *ventasapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Ventas", "v2")
	// Scalar — modern docs UI with built-in dark mode, a richer "Try it" panel,
	// and a cleaner sidebar than the default Stoplight Elements renderer.
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
	// The Huma API is mounted on a chi sub-router rooted at /v2 (see
	// cmd/api/server.go). The OpenAPI servers entry advertises that prefix so
	// the generated docs HTML loads /v2/openapi.yaml instead of /openapi.yaml.
	if config.OpenAPI != nil {
		config.OpenAPI.Servers = append(config.OpenAPI.Servers, &huma.Server{URL: "/v2"})
	}

	api := humachi.New(r, config)
	handlers := NewHandlers(svc)
	registerOperations(api, handlers)
	// GET imagen is a raw chi route because Huma's response model assumes
	// structured payloads; streaming arbitrary-size binary blobs (with
	// ETag + Cache-Control) is cleaner with the standard http.ResponseWriter.
	// Documented manually in api/openapi.yaml.
	mountObtenerImagen(r, handlers)
	return api
}

// registerOperations declares every Huma operation the ventas module
// exposes. The list mirrors the routing table documented in the module's
// HTTP layer plan: ventas CRUD plus imagen sub-resources.
//
//nolint:funlen // one Huma.Register call per operation; mechanical wiring.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"ventas"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-ventas",
		Method:        http.MethodGet,
		Path:          "/ventas",
		Summary:       "Listar ventas",
		Description:   "Devuelve una página cursor-paginada de ventas, filtrable por rango de fechas, vendedor, tipo de venta y estado de cancelación.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarVentas)

	huma.Register(api, huma.Operation{
		OperationID:   "obtener-venta",
		Method:        http.MethodGet,
		Path:          "/ventas/{id}",
		Summary:       "Obtener venta",
		Description:   "Devuelve la venta identificada por id con sus colecciones hijas (combos, productos, vendedores, imágenes).",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ObtenerVenta)

	huma.Register(api, huma.Operation{
		OperationID:   "crear-venta",
		Method:        http.MethodPost,
		Path:          "/ventas",
		Summary:       "Crear venta",
		Description:   "Crea una venta nueva con sus combos, productos y vendedores. Persiste el agregado en Firebird en una sola transacción.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusCreated,
	}, h.CrearVenta)

	huma.Register(api, huma.Operation{
		OperationID:   "cancelar-venta",
		Method:        http.MethodPatch,
		Path:          "/ventas/{id}/cancel",
		Summary:       "Cancelar venta",
		Description:   "Marca la venta como cancelada (soft-delete). Las ventas canceladas dejan de aceptar mutaciones.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.CancelarVenta)

	huma.Register(api, huma.Operation{
		OperationID:   "actualizar-header-venta",
		Method:        http.MethodPatch,
		Path:          "/ventas/{id}",
		Summary:       "Actualizar header de venta",
		Description:   "Reemplaza los campos editables del header (dirección, GPS, fecha, montos, plan, dia_cobranza, nota). Solo permitido mientras la venta esté en status 'borrador'.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ActualizarHeader)

	huma.Register(api, huma.Operation{
		OperationID:   "actualizar-cliente-venta",
		Method:        http.MethodPatch,
		Path:          "/ventas/{id}/cliente",
		Summary:       "Actualizar cliente de venta",
		Description:   "Reemplaza el snapshot del cliente (nombre, teléfono, aval) y opcionalmente el link cliente_id a CLIENTES de Microsip.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ActualizarCliente)

	huma.Register(api, huma.Operation{
		OperationID:   "reemplazar-productos-venta",
		Method:        http.MethodPut,
		Path:          "/ventas/{id}/productos",
		Summary:       "Reemplazar productos de venta",
		Description:   "Reemplaza completamente la colección de productos de la venta. Requiere status 'borrador'.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ReemplazarProductos)

	huma.Register(api, huma.Operation{
		OperationID:   "reemplazar-combos-venta",
		Method:        http.MethodPut,
		Path:          "/ventas/{id}/combos",
		Summary:       "Reemplazar combos de venta",
		Description:   "Reemplaza completamente la colección de combos de la venta.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ReemplazarCombos)

	huma.Register(api, huma.Operation{
		OperationID:   "reemplazar-vendedores-venta",
		Method:        http.MethodPut,
		Path:          "/ventas/{id}/vendedores",
		Summary:       "Reemplazar vendedores de venta",
		Description:   "Reemplaza completamente la colección de vendedores asignados a la venta.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ReemplazarVendedores)

	huma.Register(api, huma.Operation{
		OperationID:   "adjuntar-imagen",
		Method:        http.MethodPost,
		Path:          "/ventas/{id}/imagenes",
		Summary:       "Adjuntar imagen",
		Description:   "Sube una imagen de evidencia (multipart/form-data). El archivo se almacena vía el proveedor configurado (filesystem por defecto) antes de persistir la fila.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusCreated,
	}, h.AdjuntarImagen)

	huma.Register(api, huma.Operation{
		OperationID:   "eliminar-imagen",
		Method:        http.MethodDelete,
		Path:          "/ventas/{id}/imagenes/{img_id}",
		Summary:       "Eliminar imagen",
		Description:   "Elimina la fila de imagen y, en mejor esfuerzo, borra el objeto del almacén configurado.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusNoContent,
	}, h.EliminarImagen)
}
