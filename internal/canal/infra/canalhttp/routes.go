package canalhttp

import (
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	canalapp "github.com/abdimuy/msp-api/internal/canal/app"
	canaloutbound "github.com/abdimuy/msp-api/internal/canal/ports/outbound"
)

// Deps groups every dependency canal's HTTP surface needs. Task 7
// (internal/canal/module.go) builds one of these at composition root.
// Svc alone now covers both the internal Huma API and the webhook: the
// mailbox backlog count and the outbound-send path both moved onto
// canalapp.Service (see that package's saliente.go) so Handlers could go
// back to the gold-standard "holds only svc" shape.
type Deps struct {
	// Svc backs both the webhook's POST (RecibirMensaje) and the internal
	// Huma API (EnviarSaliente, ContarPendientes).
	Svc *canalapp.Service
	// Clock timestamps when the VPS received each webhook — see
	// domain.MensajeEntrante's doc comment on RecibidoEn vs TimestampMeta.
	Clock canaloutbound.Clock
	// Cfg carries the webhook and internal-API secrets.
	Cfg Config
	// Logger receives the webhook's parse/persistence failure logs. Nil
	// falls back to slog.Default().
	Logger *slog.Logger
}

// MountRouter mounts canal's whole HTTP surface onto r: Meta's raw webhook
// (GET/POST /canal/v1/webhook/whatsapp) and the internal Huma API (POST
// /canal/v1/salientes, GET /canal/v1/salud). It returns the huma.API so
// callers (and tests) can introspect the internal API's registered
// operations; the webhook is chi-native and does not appear in it — see
// the "Endpoints binarios" escape hatch in
// docs/module-standards/08-handlers-routes.md.
//
// 🔴 r must not already have (and must never gain, from anything mounted
// above it in the chain) middleware that reads and replaces the request
// body — see mountWebhook's doc comment. Task 7/8 wiring this at
// cmd/api/server.go is the thing most likely to get this silently wrong.
func MountRouter(r chi.Router, deps Deps) huma.API {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	config := huma.DefaultConfig("MSP API · Canal", "v1")
	config.DocsRenderer = huma.DocsRendererScalar
	api := humachi.New(r, config)

	h := NewHandlers(deps.Svc)
	registerSalud(api, h)
	registerSalientes(api, h, deps.Cfg.SharedToken)

	mountWebhook(r, &webhookHandler{
		svc:    deps.Svc,
		clock:  deps.Clock,
		cfg:    deps.Cfg,
		logger: logger,
	})

	return api
}

// registerSalud registers GET /canal/v1/salud with no authentication.
//
// This is a deliberate choice, not an oversight: /salud is the liveness and
// backlog probe an operator reaches for exactly when something is already
// wrong with the VPS, and gating it behind the same shared token used for
// store traffic would make it useless at the moment it is needed most —
// e.g. investigating from a browser or curl without pulling the secret out
// of the store's config. SaludDTO is shaped to make this safe: it carries
// only a status string and a pending count, never a token, a phone number,
// or a message body.
func registerSalud(api huma.API, h *Handlers) {
	huma.Register(api, huma.Operation{
		OperationID:   "canal-salud",
		Method:        http.MethodGet,
		Path:          "/canal/v1/salud",
		Summary:       "Liveness y pendientes del buzón",
		Description:   "Reporta si el proceso está vivo y cuántos entrantes esperan reenvío. Sin autenticación (ver comentario de registerSalud): no expone nada sensible.",
		Tags:          []string{"canal"},
		DefaultStatus: http.StatusOK,
	}, h.Salud)
}

// registerSalientes registers POST /canal/v1/salientes behind
// sharedTokenMiddleware, attached as a per-operation huma.Operation
// middleware — the "middleware de token compartido" the plan calls for —
// rather than a chi-level middleware, so it applies to exactly this
// operation and not to salud on the same router.
func registerSalientes(api huma.API, h *Handlers, sharedToken string) {
	huma.Register(api, huma.Operation{
		OperationID:   "canal-crear-saliente",
		Method:        http.MethodPost,
		Path:          "/canal/v1/salientes",
		Summary:       "Enviar un mensaje saliente de WhatsApp",
		Description:   "Envía, en nombre de la tienda, un mensaje de texto libre o de plantilla a través del canal de WhatsApp de la VPS. Requiere el token compartido interno.",
		Tags:          []string{"canal"},
		DefaultStatus: http.StatusOK,
		Middlewares:   huma.Middlewares{sharedTokenMiddleware(api, sharedToken)},
	}, h.CrearSaliente)
}
