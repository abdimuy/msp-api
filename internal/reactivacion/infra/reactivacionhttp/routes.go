//nolint:misspell // reactivación vocabulary is Spanish per project convention.
package reactivacionhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	reactivacionapp "github.com/abdimuy/msp-api/internal/reactivacion/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced by
// every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the reactivación module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// The function builds a fresh Huma API rooted at r, declares the bearer security
// scheme used by every operation, and registers each operation. It returns the
// constructed huma.API so callers can introspect it.
func MountRouter(r chi.Router, svc *reactivacionapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Reactivación", "v2")
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

// registerOperations declares every Huma operation the reactivación module exposes.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"reactivacion"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-cohorte-reactivacion",
		Method:        http.MethodGet,
		Path:          "/reactivacion/cohorte",
		Summary:       "Listar la cohorte del piloto de reactivación",
		Description:   "Devuelve los clientes de la cohorte de Tehuacán con su segmento, grupo de control y baseline de compra. Filtrable por segmento y solo tratamiento.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListCohorte)

	huma.Register(api, huma.Operation{
		OperationID:   "atribucion-reactivacion",
		Method:        http.MethodGet,
		Path:          "/reactivacion/atribucion",
		Summary:       "Atribución tratamiento vs control",
		Description:   "Compara la tasa de enganche (recompra tras la cohorte) entre contactados y grupo de control, con el uplift resultante.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.Atribucion)

	huma.Register(api, huma.Operation{
		OperationID:   "construir-cohorte-reactivacion",
		Method:        http.MethodPost,
		Path:          "/reactivacion/cohorte/construir",
		Summary:       "Construir (o reconstruir) la cohorte del piloto",
		Description:   "Materializa el snapshot MSP_RX_COHORTE desde el universo tratable de Tehuacán en segundo plano. Preserva grupo de control, flag de contacto y fecha de cohorte de los clientes ya existentes.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusAccepted,
	}, h.Construir)

	huma.Register(api, huma.Operation{
		OperationID:   "encolar-envios-reactivacion",
		Method:        http.MethodPost,
		Path:          "/reactivacion/envios/encolar",
		Summary:       "Encolar los mensajes de apertura del canal",
		Description:   "Genera y encola el mensaje de apertura (MSP_RX_MENSAJES) para cada cliente de la cohorte de tratamiento que aún no tiene mensaje, en segundo plano. Idempotente.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusAccepted,
	}, h.Encolar)

	huma.Register(api, huma.Operation{
		OperationID: "drenar-envios-reactivacion",
		Method:      http.MethodPost,
		Path:        "/reactivacion/envios/drenar",
		Summary:     "Drenar una tanda de la cola de envíos",
		Description: "Procesa hasta el tope de la tanda de mensajes pendientes respetando el gobernador anti-baneo y el flag auto_send. Útil para demo/operación manual — el EnvioWorker automático corre esta misma lógica en el ciclo de vida de la app.",
		Tags:        tags,
		Security:    security,
	}, h.Drenar)

	huma.Register(api, huma.Operation{
		OperationID: "listar-envios-reactivacion",
		Method:      http.MethodGet,
		Path:        "/reactivacion/envios",
		Summary:     "Listar los mensajes del canal de reactivación",
		Description: "Devuelve los mensajes de MSP_RX_MENSAJES, filtrables por estado y segmento, para inspeccionar el flujo del canal.",
		Tags:        tags,
		Security:    security,
	}, h.ListEnvios)
}
