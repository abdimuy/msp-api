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

	api := humachi.New(r, config)
	registerOperations(api, NewHandlers(svc))
	return api
}

// registerOperations declares every Huma operation the ventas module
// exposes. The list mirrors the routing table documented in the module's
// HTTP layer plan: ventas CRUD plus imagen sub-resources.
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
