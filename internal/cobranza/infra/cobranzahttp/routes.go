// Package cobranzahttp hosts the cobranza module's HTTP transport: handlers,
// DTOs, and the Huma-over-chi router mount points.
//
//nolint:misspell // Spanish domain vocabulary by project convention.
package cobranzahttp

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	cobranzaapp "github.com/abdimuy/msp-api/internal/cobranza/app"
	"github.com/abdimuy/msp-api/internal/cobranza/app/eventbus"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
	"github.com/abdimuy/msp-api/internal/platform/config"
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

// MountReadRouter mounts the read + write endpoints (saldos, pagos, sync, plus
// pago creation/imágenes) under the supplied chi router. The router is
// expected to have authn already applied upstream.
//
// The streaming GET imagen endpoint and the SSE push endpoints are wired as
// raw chi handlers (not Huma) to support binary streaming and chunked
// transfer encoding respectively.
//
// bus, sseCfg, and logger are required for the SSE endpoints. Pass a
// non-nil *eventbus.Bus obtained from eventbus.New(). The SSE routes are
// always registered; the feature flag inside sseCfg.SSEEnabled gates whether
// they actually stream or return 503.
//
// Returns the constructed huma.API for testing / introspection.
func MountReadRouter(
	r chi.Router,
	svc *cobranzaapp.Service,
	bus *eventbus.Bus,
	sseCfg config.Cobranza,
	logger *slog.Logger,
) huma.API {
	cfg := newHumaConfig("MSP API · Cobranza")
	api := humachi.New(r, cfg)
	h := NewHandlers(svc, nil, nil)
	registerReadOperations(api, h)
	registerPagoWriteOperations(api, h)
	// GET imagen is a raw chi route: Huma's response model assumes structured
	// payloads; streaming arbitrary-size binary blobs with ETag + Cache-Control
	// is cleaner via http.ResponseWriter.
	mountObtenerImagenPago(r, h)
	// SSE push endpoints — raw chi because Huma does not support streaming.
	// Feature flag (sseCfg.SSEEnabled) is checked inside each handler; the
	// routes are always registered so 503 is returned when the flag is off.
	sse := newSSEHandler(bus, sseCfg, logger)
	mountCobranzaSSE(r, sse)
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

	huma.Register(api, op(tag, "cobranza-sync-ventas-por-zona", http.MethodGet, "/sync/ventas/zona/{zona_id}",
		"Sync incremental de ventas enriquecidas por zona",
		"Devuelve un page de ventas (saldo + cliente + dirección + contrato) modificadas desde el cursor server_ts. Pensado para alimentar la app móvil de cobranza en una sola request por zona."), h.SyncVentasPorZona)

	// Reconcile digest + IDs endpoints — point-in-time correctness contract for
	// the mobile app cache. SSE (commit 7) is latency-only; these are the source
	// of truth for drift detection.
	huma.Register(api, op(tag, "cobranza-sync-pagos-digest", http.MethodGet, "/sync/pagos/zona/{zona_id}/digest",
		"Digest de pagos activos por zona",
		"Devuelve la huella digital (count, xor, sum, max_updated_at) de los pagos activos de la zona, calculada dentro de una transacción snapshot para garantizar consistencia punto-en-tiempo."), h.SyncPagosDigest)

	huma.Register(api, op(tag, "cobranza-sync-pagos-ids", http.MethodGet, "/sync/pagos/zona/{zona_id}/ids",
		"IDs de pagos activos por zona",
		"Devuelve la lista de IDs de pagos activos, paginada por after. Usar para reconciliar la caché local del cliente contra el servidor."), h.SyncPagosIDs)

	huma.Register(api, op(tag, "cobranza-sync-saldos-digest", http.MethodGet, "/sync/saldos/zona/{zona_id}/digest",
		"Digest de saldos activos por zona",
		"Devuelve la huella digital (count, xor, sum, max_updated_at) de los saldos activos de la zona, calculada dentro de una transacción snapshot."), h.SyncSaldosDigest)

	huma.Register(api, op(tag, "cobranza-sync-saldos-ids", http.MethodGet, "/sync/saldos/zona/{zona_id}/ids",
		"IDs de saldos activos por zona",
		"Devuelve la lista de IDs de saldos activos, paginada por after."), h.SyncSaldosIDs)
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

	huma.Register(api, op(tag, "cobranza-admin-listar-pendientes", http.MethodGet, "/pagos/pendientes",
		"Listar pagos pendientes",
		"Lista pagos con ESTADO='P' que el retry worker está drenando."), h.ListarPendientes)

	huma.Register(api, op(tag, "cobranza-admin-aplicar-pago", http.MethodPost, "/pagos/{id}/aplicar",
		"Forzar aplicación de un pago",
		"Ejecuta AplicarPago manualmente sobre un pago pendiente."), h.AplicarPagoForzar)
}

// registerPagoWriteOperations declares the pago write + imagen endpoints.
func registerPagoWriteOperations(api huma.API, h *Handlers) {
	const tag = "cobranza"

	huma.Register(api, op(tag, "cobranza-crear-pago", http.MethodPost, "/pagos",
		"Crear pago de cobranza",
		"multipart/form-data: campo `datos` (JSON con el pago) + N campos `imagen` (archivos) "+
			"+ opcionales `id_<n>` / `descripcion_<n>` por imagen. Persiste pago + comprobantes "+
			"en una sola tx Firebird; aplica a Microsip best-effort después del commit. ID del "+
			"cliente (datos.id) es la idempotency key end-to-end."), h.CrearPago)

	huma.Register(api, op(tag, "cobranza-obtener-pago-recibido", http.MethodGet, "/pagos/{id}",
		"Obtener pago",
		"Devuelve un pago de la outbox por UUID."), h.ObtenerPagoRecibido)

	huma.Register(api, op(tag, "cobranza-listar-imagenes-pago", http.MethodGet, "/pagos/{id}/imagenes",
		"Listar comprobantes",
		"Lista los comprobantes adjuntos al pago."), h.ListarImagenesPago)

	huma.Register(api, op(tag, "cobranza-adjuntar-imagen-pago", http.MethodPost, "/pagos/{id}/imagenes",
		"Adjuntar comprobante",
		"Multipart upload de un comprobante (PDF o imagen) al pago."), h.AdjuntarImagenPago)

	huma.Register(api, op(tag, "cobranza-eliminar-imagen-pago", http.MethodDelete, "/pagos/{id}/imagenes/{img_id}",
		"Eliminar comprobante",
		"Borra un comprobante del pago."), h.EliminarImagenPago)
}
