//nolint:misspell // cobranza vocabulary is Spanish per project convention.
package cobranzahttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced by
// every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// newHumaConfig builds a base Huma config with the bearer security scheme.
func newHumaConfig(title string) huma.Config {
	cfg := huma.DefaultConfig(title, "v2")
	cfg.DocsRenderer = huma.DocsRendererScalar
	if cfg.Components == nil {
		cfg.Components = &huma.Components{}
	}
	if cfg.Components.SecuritySchemes == nil {
		cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	cfg.Components.SecuritySchemes[securitySchemeName] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Token de Firebase ID propagado al backend como Bearer.",
	}
	return cfg
}

// MountReadRouter mounts the read endpoints under the supplied chi router.
// The router is expected to have authn already applied upstream.
// Returns the constructed huma.API for testing / introspection.
func MountReadRouter(r chi.Router, svc *cobranzaapp.Service) huma.API {
	cfg := newHumaConfig("MSP API · Cobranza")
	api := humachi.New(r, cfg)
	h := NewHandlers(svc, nil, nil)
	registerReadOperations(api, h)
	return api
}

// MountAdminRouter mounts the admin endpoints under the supplied chi router.
// The router is expected to have authn already applied upstream.
// Returns the constructed huma.API for testing / introspection.
func MountAdminRouter(r chi.Router, svc *cobranzaapp.Service, reconciler *cobranzaapp.Reconciler, errorsRepo outbound.ErrorsRepo) huma.API {
	cfg := newHumaConfig("MSP API · Cobranza Admin")
	api := humachi.New(r, cfg)
	h := NewHandlers(svc, reconciler, errorsRepo)
	registerAdminOperations(api, h)
	return api
}

// registerReadOperations declares the cobranza read endpoints.
func registerReadOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"cobranza"}

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-saldo-por-venta",
		Method:        http.MethodGet,
		Path:          "/saldos/venta/{id}",
		Summary:       "Saldo por venta (DOCTO_PV)",
		Description:   "Devuelve el saldo materializado en MSP_SALDOS_VENTAS para el DOCTO_PV_ID indicado.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.PorVenta)

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-saldos-por-cliente",
		Method:        http.MethodGet,
		Path:          "/saldos/cliente/{cliente_id}",
		Summary:       "Saldos abiertos por cliente",
		Description:   "Devuelve todos los saldos con balance positivo para el cliente indicado.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.PorCliente)

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-saldos-por-zona",
		Method:        http.MethodGet,
		Path:          "/saldos/zona/{zona_id}",
		Summary:       "Saldos en ruta por zona",
		Description:   "Devuelve ventas abiertas y recientemente pagadas (dentro de ventana_dias) para la zona indicada.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.PorZona)

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-resumen-zonas",
		Method:        http.MethodGet,
		Path:          "/resumen-zonas",
		Summary:       "Resumen de saldos por zona",
		Description:   "Devuelve un resumen agregado de saldos pendientes agrupados por zona.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ResumenZonas)
}

// registerAdminOperations declares the cobranza admin endpoints.
func registerAdminOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"cobranza-admin"}

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-admin-reconcile",
		Method:        http.MethodPost,
		Path:          "/reconcile",
		Summary:       "Reconciliar saldos",
		Description:   "Ejecuta un paso completo de reconciliación: compara el caché con los datos en Microsip y corrige divergencias.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.Reconcile)

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-admin-backfill",
		Method:        http.MethodPost,
		Path:          "/backfill",
		Summary:       "Backfill de saldos",
		Description:   "Recomputa incondicionalmente todos los saldos en MSP_SALDOS_VENTAS. Equivalente al bloque EXECUTE BLOCK de la migración 000010.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.Backfill)

	huma.Register(api, huma.Operation{
		OperationID:   "cobranza-admin-errors",
		Method:        http.MethodGet,
		Path:          "/errors",
		Summary:       "Errores del caché de saldos",
		Description:   "Devuelve los errores más recientes registrados en MSP_SALDOS_ERRORS por los triggers y el procedimiento de recompute.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.Errors)
}
