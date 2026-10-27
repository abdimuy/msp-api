//nolint:misspell // vocabulario español por convención del proyecto.
package desempenohttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	desempenoapp "github.com/abdimuy/msp-api/internal/desempeno/app"
)

const securitySchemeName = "bearerAuth"

// MountRouter registra las cuatro operaciones sobre r. r ya trae el
// middleware de autenticación (cmd/api/server.go); el permiso se revisa en
// cada handler. Rutas finales: /v2/desempeno/usuarios[/{uid}/jornada|actividad|cuentas].
func MountRouter(r chi.Router, svc *desempenoapp.Service, opts ...Option) huma.API {
	cfg := huma.DefaultConfig("MSP API · Desempeño de cobradores", "v2")
	cfg.DocsRenderer = huma.DocsRendererScalar
	// Sin el "$schema" que Huma agrega a cada respuesta: el contrato fija las
	// claves byte a byte (contrato §0) y TestContrato_* lo vigila.
	cfg.CreateHooks = nil
	if cfg.Components == nil {
		cfg.Components = &huma.Components{}
	}
	if cfg.Components.SecuritySchemes == nil {
		cfg.Components.SecuritySchemes = map[string]*huma.SecurityScheme{}
	}
	cfg.Components.SecuritySchemes[securitySchemeName] = &huma.SecurityScheme{
		Type: "http", Scheme: "bearer", BearerFormat: "JWT",
		Description: "Token de Firebase ID propagado al backend como Bearer.",
	}
	if cfg.OpenAPI != nil {
		cfg.Servers = append(cfg.Servers, &huma.Server{URL: "/v2"})
	}
	api := humachi.New(r, cfg)
	h := &Handlers{svc: svc}
	for _, o := range opts {
		o(h)
	}
	security := []map[string][]string{{securitySchemeName: {}}}
	tags := []string{"desempeno"}

	huma.Register(api, huma.Operation{
		OperationID: "desempeno-tablero", Method: http.MethodGet, Path: "/desempeno/usuarios",
		Summary: "Tablero de desempeño por usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Tablero)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-catalogos", Method: http.MethodGet, Path: "/desempeno/catalogos",
		Summary: "Catálogos para los filtros", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Catalogos)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-jornada", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}/jornada",
		Summary: "Jornada de un usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Jornada)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-actividad", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}/actividad",
		Summary: "Actividad de la zona del usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Actividad)
	huma.Register(api, huma.Operation{
		OperationID: "desempeno-cuentas", Method: http.MethodGet, Path: "/desempeno/usuarios/{uid}/cuentas",
		Summary: "Cuentas de la semana del usuario", Tags: tags, Security: security, DefaultStatus: http.StatusOK,
	}, h.Cuentas)
	return api
}
