// Package cobranzahttp hosts the cobranza module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount points.
//
//nolint:misspell // Spanish domain vocabulary by project convention.
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

// op builds a huma.Operation with the cobranza defaults (Tags, Security,
// DefaultStatus) baked in so each huma.Register call stays a single line.
func op(tag, id, method, path, summary, description string) huma.Operation {
	return huma.Operation{
		OperationID:   id,
		Method:        method,
		Path:          path,
		Summary:       summary,
		Description:   description,
		Tags:          []string{tag},
		Security:      []map[string][]string{{securitySchemeName: {}}},
		DefaultStatus: http.StatusOK,
	}
}

// registerReadOperations declares every cobranza read endpoint.
func registerReadOperations(api huma.API, h *Handlers) {
	const tag = "cobranza"

	huma.Register(api, op(tag, "cobranza-saldo-por-venta", http.MethodGet, "/saldos/venta/{id}",
		"Saldo por venta (DOCTO_PV)",
		"Devuelve el saldo materializado en MSP_SALDOS_VENTAS para el DOCTO_PV_ID indicado."), h.PorVenta)

	huma.Register(api, op(tag, "cobranza-saldos-por-cliente", http.MethodGet, "/saldos/cliente/{cliente_id}",
		"Saldos abiertos por cliente",
		"Devuelve todos los saldos con balance positivo para el cliente indicado."), h.PorCliente)

	huma.Register(api, op(tag, "cobranza-saldos-por-zona", http.MethodGet, "/saldos/zona/{zona_id}",
		"Saldos en ruta por zona",
		"Devuelve ventas abiertas y recientemente pagadas (dentro de ventana_dias) para la zona indicada."), h.PorZona)

	huma.Register(api, op(tag, "cobranza-resumen-zonas", http.MethodGet, "/resumen-zonas",
		"Resumen de saldos por zona",
		"Devuelve un resumen agregado de saldos pendientes agrupados por zona."), h.ResumenZonas)

	huma.Register(api, op(tag, "cobranza-pagos-por-venta", http.MethodGet, "/pagos/venta/{docto_cc_id}",
		"Pagos acreditados a un cargo",
		"Devuelve todos los pagos materializados en MSP_PAGOS_VENTAS cuyo DOCTO_CC_ACR_ID coincide con el cargo indicado."), h.PagosPorVenta)

	huma.Register(api, op(tag, "cobranza-pagos-por-cliente", http.MethodGet, "/pagos/cliente/{cliente_id}",
		"Historial de pagos por cliente",
		"Devuelve todos los pagos hechos por el cliente, ordenados por fecha descendente."), h.PagosPorCliente)

	huma.Register(api, op(tag, "cobranza-pagos-por-zona", http.MethodGet, "/pagos/zona/{zona_id}",
		"Pagos por zona y ventana de fecha",
		"Devuelve los pagos hechos en la zona con FECHA >= cutoff (semántica de cobranza)."), h.PagosPorZona)

	huma.Register(api, op(tag, "cobranza-sync-saldos-por-zona", http.MethodGet, "/sync/saldos/zona/{zona_id}",
		"Sync incremental de saldos por zona",
		"Devuelve un page de saldos modificados desde el cursor server_ts, con max_updated_at y server_now para que el cliente avance sin usar su reloj. Incluye tombstones (cargo_cancelado=true) para propagar cancelaciones."), h.SyncSaldosPorZona)

	huma.Register(api, op(tag, "cobranza-sync-pagos-por-zona", http.MethodGet, "/sync/pagos/zona/{zona_id}",
		"Sync incremental de pagos por zona",
		"Devuelve un page de pagos modificados desde el cursor server_ts. Misma mecánica que sync/saldos pero a nivel de MSP_PAGOS_VENTAS."), h.SyncPagosPorZona)
}

// registerAdminOperations declares the cobranza admin endpoints.
func registerAdminOperations(api huma.API, h *Handlers) {
	const tag = "cobranza-admin"

	huma.Register(api, op(tag, "cobranza-admin-reconcile", http.MethodPost, "/reconcile",
		"Reconciliar saldos",
		"Ejecuta un paso completo de reconciliación: compara el caché con los datos en Microsip y corrige divergencias."), h.Reconcile)

	huma.Register(api, op(tag, "cobranza-admin-backfill", http.MethodPost, "/backfill",
		"Backfill de saldos",
		"Recomputa incondicionalmente todos los saldos en MSP_SALDOS_VENTAS. Equivalente al bloque EXECUTE BLOCK de la migración 000010."), h.Backfill)

	huma.Register(api, op(tag, "cobranza-admin-errors", http.MethodGet, "/errors",
		"Errores del caché de saldos",
		"Devuelve los errores más recientes registrados en MSP_SALDOS_ERRORS por los triggers y el procedimiento de recompute."), h.Errors)
}
