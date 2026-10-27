//nolint:misspell // visitas vocabulary is Spanish per project convention.
package visitashttp

import (
	"log/slog"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/platform/config"
	"github.com/abdimuy/msp-api/internal/platform/eventbus"
	visitasapp "github.com/abdimuy/msp-api/internal/visitas/app"
	"github.com/abdimuy/msp-api/internal/visitas/domain"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountOption customizes what MountRouter mounts.
//
// Variadic rather than positional parameters so the eighteen existing call
// sites — almost all of them tests that have no interest in streaming — keep
// compiling untouched. It mirrors visitasapp.Option, the same shape the
// module already uses for its own optional dependencies.
type MountOption func(*mountConfig)

type mountConfig struct {
	bus    *eventbus.Bus
	sseCfg config.Visitas
	logger *slog.Logger
}

// WithSSE mounts the visitas SSE stream at
// GET /visitas/sync/visitas/zona/{zona_id}/stream.
//
// Supplying a nil bus is treated as "no SSE" rather than mounting a route that
// would panic on the first connection: a half-wired stream must not be
// representable. The composition root always supplies it; see
// TestMountRouter_SSERouteEstaMontada for the test that keeps it that way.
func WithSSE(bus *eventbus.Bus, sseCfg config.Visitas, logger *slog.Logger) MountOption {
	return func(c *mountConfig) {
		c.bus = bus
		c.sseCfg = sseCfg
		c.logger = logger
	}
}

// MountRouter mounts the visitas module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// registerOperations declares operations at the bare paths "/visitas",
// "/visitas/{id}", and "/visitas/by-ids" — mount this on a bare chi Group
// (like rutas/clientes/config), NOT via r.Route("/visitas"), which would
// double the prefix to /v2/visitas/visitas. Final paths: POST /v2/visitas,
// GET /v2/visitas/{id}, and GET /v2/visitas/by-ids.
//
// "by-ids" is a literal path segment sibling to the "{id}" parameter at the
// same level. chi's router matches static segments before parameterized
// ones regardless of registration order, so GET /visitas/by-ids always
// resolves to VisitasExistentesPorIDs, never to ObtenerVisita with id=
// "by-ids" — TestMountRouter_ByIDsDoesNotShadowObtenerVisita in routes_test.go
// pins this down explicitly.
func MountRouter(r chi.Router, svc *visitasapp.Service, opts ...MountOption) huma.API {
	var mc mountConfig
	for _, opt := range opts {
		opt(&mc)
	}
	humaCfg := huma.DefaultConfig("MSP API · Visitas", "v2")
	humaCfg.DocsRenderer = huma.DocsRendererScalar
	if humaCfg.Components == nil {
		humaCfg.Components = &huma.Components{}
	}
	if humaCfg.Components.SecuritySchemes == nil {
		humaCfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	humaCfg.Components.SecuritySchemes[securitySchemeName] = &huma.SecurityScheme{
		Type:         "http",
		Scheme:       "bearer",
		BearerFormat: "JWT",
		Description:  "Token de Firebase ID propagado al backend como Bearer.",
	}
	if humaCfg.OpenAPI != nil {
		humaCfg.Servers = append(humaCfg.Servers, &huma.Server{URL: "/v2"})
	}

	api := humachi.New(r, humaCfg)
	handlers := NewHandlers(svc)
	registerOperations(api, handlers)
	mountImagenesVisita(r, handlers)
	describeCrearVisitaBody(api)
	// SSE push endpoint — raw chi, because Huma has no streaming response
	// model. Registered unconditionally when a bus is wired: the feature flag
	// is checked INSIDE the handler so a disabled stream answers 503 rather
	// than 404, which is what lets the client latch "off" and stop retrying
	// instead of backing off against a route it thinks is missing.
	if mc.bus != nil {
		logger := mc.logger
		if logger == nil {
			logger = slog.Default()
		}
		mountVisitasSSE(r, newSSEHandler(mc.bus, mc.sseCfg, logger))
	}
	return api
}

// describeCrearVisitaBody rewrites POST /visitas' documented request body.
//
// The handler reads raw bytes so it can serve application/json and
// multipart/form-data on the same route (see CrearVisitaInput). Huma cannot
// infer that: a []byte RawBody makes it publish a single opaque
// application/octet-stream binary body, which would tell every reader of the
// spec the wrong thing about both formats.
//
// So the operation is patched after registration with the two media types it
// actually accepts — the JSON document generated from CrearVisitaBody, and
// the multipart envelope that wraps that same document in `datos` alongside
// the repeated `imagen` parts. Doing it here, rather than hand-maintaining a
// literal schema, keeps the JSON half generated from the Go struct: add a
// field to CrearVisitaBody and the published contract follows.
func describeCrearVisitaBody(api huma.API) {
	spec := api.OpenAPI()
	if spec == nil || spec.Paths == nil {
		return
	}
	item := spec.Paths["/visitas"]
	if item == nil || item.Post == nil {
		return
	}
	registry := spec.Components.Schemas
	jsonSchema := registry.Schema(reflect.TypeOf(CrearVisitaBody{}), true, "CrearVisitaBody")

	item.Post.RequestBody = &huma.RequestBody{
		Required: true,
		Description: "Dos formatos sobre la misma ruta. `application/json` es el contrato " +
			"legacy y sigue vigente sin cambios. `multipart/form-data` lleva ese mismo " +
			"documento JSON en el campo `datos`, más 0..N archivos `imagen` y, por cada " +
			"uno, los opcionales `id_<n>` (UUID estable, necesario para que un reintento " +
			"no duplique la foto) y `descripcion_<n>`.",
		Content: map[string]*huma.MediaType{
			mediaTypeJSON:      {Schema: jsonSchema},
			mediaTypeMultipart: {Schema: crearVisitaMultipartSchema()},
		},
	}
}

// crearVisitaMultipartSchema describes the multipart envelope: `datos` is the
// JSON document as a string, `imagen` is 0..N binary parts. The per-image
// `id_<n>` / `descripcion_<n>` fields cannot be expressed in JSON Schema —
// their names depend on position — so they live in the request body's
// description instead.
func crearVisitaMultipartSchema() *huma.Schema {
	return &huma.Schema{
		Type:     "object",
		Required: []string{"datos"},
		Properties: map[string]*huma.Schema{
			"datos": {
				Type: "string",
				Description: "El documento JSON de la visita — los mismos campos que el cuerpo " +
					"application/json de esta misma ruta, serializados como texto.",
			},
			"imagen": {
				Type: "array",
				Description: "0..N fotos. Cada parte DEBE declarar su propio Content-Type, y solo " +
					"se aceptan: " + strings.Join(mimesPermitidos(), ", ") + ".",
				Items: &huma.Schema{
					Type:   "string",
					Format: "binary",
				},
			},
		},
	}
}

// mimesPermitidos lists the accepted image MIME types, read from the domain
// so the published contract and the runtime check cannot drift.
func mimesPermitidos() []string {
	return []string{
		domain.MimeJPEG, domain.MimePNG, domain.MimeGIF, domain.MimeWebP, domain.MimePDF,
	}
}

// registerOperations declares every Huma operation the visitas module exposes.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"visitas"}

	huma.Register(api, huma.Operation{
		OperationID: "crear-visita",
		Method:      http.MethodPost,
		Path:        "/visitas",
		Summary:     "Registrar visita de cobranza",
		Description: "Registra que un cobrador visitó a un cliente en su ruta, haya generado pago o no. " +
			"Idempotente en el id enviado por el cliente, incluso cuando la request trae fotos. " +
			"Acepta application/json (contrato legacy, sin cambios) y multipart/form-data " +
			"(el mismo JSON en `datos` más 0..N `imagen`); visita e imágenes se persisten en " +
			"una sola transacción, de modo que nunca queda una foto huérfana ni una visita a medias.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusCreated,
	}, h.CrearVisita)

	huma.Register(api, huma.Operation{
		OperationID: "obtener-visita",
		Method:      http.MethodGet,
		Path:        "/visitas/{id}",
		Summary:     "Obtener visita por id",
		Description: "Devuelve una visita de MSP_VISITAS por UUID. Lee filas legacy tal cual: el tipo_visita devuelto puede quedar fuera del catálogo cerrado que valida la escritura.",
		Tags:        tags,
		Security:    security,
	}, h.ObtenerVisita)

	huma.Register(api, huma.Operation{
		OperationID: "listar-imagenes-visita",
		Method:      http.MethodGet,
		Path:        "/visitas/{id}/imagenes",
		Summary:     "Listar fotos de una visita",
		Description: "Devuelve las fotos de la visita (misma forma que las de pagos). El binario se pide en /visitas/{id}/imagenes/{img_id}.",
		Tags:        tags,
		Security:    security,
	}, h.ListarImagenesVisita)

	huma.Register(api, huma.Operation{
		OperationID: "visitas-existentes-por-ids",
		Method:      http.MethodGet,
		Path:        "/visitas/by-ids",
		Summary:     "Confirmar visitas existentes por ids",
		Description: "Canal del reconciliador de visitas: de una lista de UUIDs generados y subidos por el propio teléfono, devuelve el subconjunto que el servidor ya tiene. No es un canal de descarga — un id desconocido simplemente no aparece en la respuesta, nunca es un error. Máximo " + strconv.Itoa(maxIDsPorRequest) + " ids por request.",
		Tags:        tags,
		Security:    security,
	}, h.VisitasExistentesPorIDs)
}
