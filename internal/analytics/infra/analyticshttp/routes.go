//nolint:misspell // analytics vocabulary is Spanish per project convention.
package analyticshttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	analyticsapp "github.com/abdimuy/msp-api/internal/analytics/app"
)

// securitySchemeName is the OpenAPI security-scheme identifier referenced
// by every operation that requires a Firebase bearer token.
const securitySchemeName = "bearerAuth"

// MountRouter mounts the analytics module's HTTP routes onto r. The supplied
// chi router is expected to have ALREADY applied the authentication chi
// middleware so handlers can read auth.CurrentUser from the request context.
//
// The function builds a fresh Huma API rooted at r, declares the bearer
// security scheme used by every operation, and registers each operation
// with its OperationID, path, method, summary, and default status code.
// It returns the constructed huma.API so callers can introspect it (for
// example, to assert that the right routes are mounted in tests).
func MountRouter(r chi.Router, svc *analyticsapp.Service) huma.API {
	config := huma.DefaultConfig("MSP API · Analytics", "v2")
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

// registerOperations declares every Huma operation the analytics module
// exposes: winback list, attribution query, candidatos refresh, and all six
// cartera dashboard endpoints.
func registerOperations(api huma.API, h *Handlers) {
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"analytics"}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-winback",
		Method:        http.MethodGet,
		Path:          "/winback",
		Summary:       "Listar candidatos winback",
		Description:   "Devuelve candidatos de reenganche ordenados por score descendente, con filtros opcionales de segmento y zona.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.ListarWinback)

	huma.Register(api, huma.Operation{
		OperationID:   "atribucion-winback",
		Method:        http.MethodGet,
		Path:          "/winback/attribution",
		Summary:       "Atribución de campaña winback",
		Description:   "Mide el impacto incremental de la campaña comparando tasas de conversión entre grupos de tratamiento y control.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.Atribucion)

	huma.Register(api, huma.Operation{
		OperationID:   "refrescar-candidatos-winback",
		Method:        http.MethodPost,
		Path:          "/winback/refresh",
		Summary:       "Refrescar candidatos winback",
		Description:   "Dispara la reconstrucción de la proyección de candidatos winback en segundo plano y devuelve 202 Accepted inmediatamente. Incremental por defecto; full=true fuerza reconstrucción completa. Si ya hay un refresco en curso, el cuerpo de respuesta indica ya_en_progreso.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusAccepted,
	}, h.RefrescarCandidatos)

	registerCarteraOperations(api, h, security, tags)
}

// registerCarteraOperations registers the six cartera dashboard endpoints.
func registerCarteraOperations(api huma.API, h *Handlers, security []map[string][]string, tags []string) {
	huma.Register(api, huma.Operation{
		OperationID:   "cartera-salud",
		Method:        http.MethodGet,
		Path:          "/cartera/salud",
		Summary:       "Salud de la cartera",
		Description:   "KPIs ejecutivos de la cartera de crédito: PAR, CEI, saldo moroso, cuentas en mora y margen real proxy.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.SaludCartera)

	huma.Register(api, huma.Operation{
		OperationID:   "cartera-aging",
		Method:        http.MethodGet,
		Path:          "/cartera/aging",
		Summary:       "Distribución de aging",
		Description:   "Distribución del saldo por cubeta de antigüedad de la deuda: 0-30, 31-60, 61-90, 90+ días.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.AgingCartera)

	huma.Register(api, huma.Operation{
		OperationID:   "cartera-cosechas",
		Method:        http.MethodGet,
		Path:          "/cartera/cosechas",
		Summary:       "Cosechas de crédito",
		Description:   "Saldo vigente agrupado por cohorte de originación (vintage). Identifica qué generación de créditos concentra mayor riesgo.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.CosechasCartera)

	huma.Register(api, huma.Operation{
		OperationID:   "cartera-cobradores",
		Method:        http.MethodGet,
		Path:          "/cartera/cobradores",
		Summary:       "Ranking de cobradores",
		Description:   "Métricas de desempeño por cobrador: CEI, PAR, porcentaje al corriente, saldo total y saldo moroso.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.CobradorRanking)

	huma.Register(api, huma.Operation{
		OperationID:   "cartera-cuentas-riesgo",
		Method:        http.MethodGet,
		Path:          "/cartera/cuentas-riesgo",
		Summary:       "Cuentas en riesgo",
		Description:   "Listado accionable de clientes con saldo vigente, clasificados por tier de riesgo (CRITICO, EN_RIESGO, VIGILANCIA, AL_DIA) y segmento RFM.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.CuentasRiesgo)

	huma.Register(api, huma.Operation{
		OperationID:   "cartera-roll-rate",
		Method:        http.MethodGet,
		Path:          "/cartera/roll-rate",
		Summary:       "Roll-rate de la cartera",
		Description:   "Escalar de migración neta de la deuda entre los dos cortes de snapshot más recientes. Disponible=false indica que el sistema está acumulando datos iniciales.",
		Tags:          tags,
		Security:      security,
		DefaultStatus: http.StatusOK,
	}, h.RollRate)
}
