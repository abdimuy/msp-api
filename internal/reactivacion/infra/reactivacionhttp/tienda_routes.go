//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
)

// TiendaConfig carries the machine-to-machine secrets MountTiendaRouter
// checks. Both come from internal/platform/config's Canal section — see
// config.Canal.SharedToken and config.Canal.TiendaAllowedIPs' doc comments.
type TiendaConfig struct {
	// SharedToken authenticates POST /reactivacion/tienda/mensaje-entrante.
	// Empty means the route rejects every request — secureCompareTienda
	// fails closed when want is empty.
	SharedToken string
	// AllowedIPs, when non-empty, additionally restricts callers to this
	// set of remote IPs. Empty (the default) disables the check — see
	// tiendaIPMiddleware's doc comment for why this is best-effort under
	// the store's current tunnel topology, and required to be off by
	// default so local development and the test suite need no
	// configuration to reach this route.
	AllowedIPs []string
}

// MountTiendaRouter mounts the ONE store-side machine-to-machine route the
// VPS's ForwarderClient pushes an entrante to: POST
// /reactivacion/tienda/mensaje-entrante.
//
// 🔴 r must NOT have the Firebase authn chi middleware applied — the VPS
// has no Firebase user. This is why cmd/api/server.go mounts this on its
// own chi.Group, a SIBLING of (never nested inside) the Group that wraps
// MountRouter's Firebase surface in authn.Handler. Authentication here is
// entirely the shared token (plus, if configured, the IP allowlist),
// checked as per-operation Huma middlewares — see registerTiendaMensajeEntrante.
func MountTiendaRouter(r chi.Router, svc *reactivacionapp.Service, cfg TiendaConfig) huma.API {
	config := huma.DefaultConfig("MSP API · Reactivación · Tienda", "v2")
	config.DocsRenderer = huma.DocsRendererScalar
	// This is the one no-authn route group on the internet-facing box
	// (tiendaTokenMiddleware is the only gate, and IP allowlisting is
	// best-effort at most — see its own doc comment). Not new exposure —
	// Docs/OpenAPI carry no secrets — but there is no reason to publish
	// them here either. Mirrors
	// internal/microsip/infra/microsiphttp/routes.go's own DocsPath/
	// OpenAPIPath disabling.
	config.DocsPath = ""
	config.OpenAPIPath = ""
	api := humachi.New(r, config)

	h := &tiendaHandlers{svc: svc}
	registerTiendaMensajeEntrante(api, h, cfg)
	return api
}

// registerTiendaMensajeEntrante registers the single store-side route
// behind the shared token and, when configured, the IP allowlist —
// mirroring internal/canal/infra/canalhttp's registerSalientes: a
// per-operation huma.Operation middleware rather than a chi-level one, so
// the gate applies to exactly this one path.
func registerTiendaMensajeEntrante(api huma.API, h *tiendaHandlers, cfg TiendaConfig) {
	middlewares := huma.Middlewares{tiendaTokenMiddleware(api, cfg.SharedToken)}
	if len(cfg.AllowedIPs) > 0 {
		middlewares = append(middlewares, tiendaIPMiddleware(api, cfg.AllowedIPs))
	}
	huma.Register(api, huma.Operation{
		OperationID:   "reactivacion-tienda-mensaje-entrante",
		Method:        http.MethodPost,
		Path:          "/reactivacion/tienda/mensaje-entrante",
		Summary:       "Recibir un entrante de WhatsApp reenviado por la VPS",
		Description:   "Endpoint máquina-a-máquina: recibe el entrante que internal/canal (VPS) reenvía, resuelve el cliente por teléfono contra la cohorte del piloto y lo entrega al mismo procesamiento del copiloto que usa el simulador de operador. Autenticado por token compartido — nunca por Firebase.",
		Tags:          []string{"reactivacion"},
		DefaultStatus: http.StatusOK,
		Middlewares:   middlewares,
	}, h.MensajeEntrante)
}
