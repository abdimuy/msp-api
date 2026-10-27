//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/abdimuy/msp-api/internal/garantias/app"
)

const securitySchemeName = "bearerAuth"

// Deps contiene las dependencias HTTP del módulo de garantías.
//
// Consultas atiende las lecturas; Comandos, las rutas POST. Las dos son
// obligatorias: un módulo montado a medias respondería 404 a la mitad de su
// contrato sin que nada truene en el arranque.
type Deps struct {
	Consultas *app.Consultas
	Comandos  *app.Service
}

// MountRouter monta las rutas HTTP de garantías sobre el router recibido.
func MountRouter(r chi.Router, d Deps) huma.API {
	if d.Consultas == nil {
		panic("garhttp: Consultas es requerida")
	}

	if d.Comandos == nil {
		panic("garhttp: Comandos es requerido")
	}

	config := huma.DefaultConfig("MSP API · Garantías", "v2")
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
		Description:  "Token de autenticación Bearer.",
	}

	if config.OpenAPI != nil {
		config.Servers = append(
			config.Servers,
			&huma.Server{URL: "/v2"},
		)
	}

	api := humachi.New(r, config)
	handlers := NewHandlers(d.Consultas)

	api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
		if ctx.Operation() != nil &&
			ctx.Operation().OperationID == "listar-garantias" {
			u := ctx.URL()
			query := u.Query()

			for _, nombre := range []string{
				"cursor",
				"limit",
				"estado",
				"origen",
				"cliente_id",
				"etapa",
				"ubicacion",
				"desde",
				"hasta",
			} {
				if len(query[nombre]) > 1 {
					_ = huma.WriteErr(
						api,
						ctx,
						http.StatusUnprocessableEntity,
						"el parámetro "+nombre+" no puede repetirse",
					)
					return
				}
			}
		}

		next(ctx)
	})

	registerOperations(api, handlers)
	registrarComandos(api, d.Comandos)

	return api
}

func registerOperations(api huma.API, handlers *Handlers) {
	security := []map[string][]string{
		{securitySchemeName: {}},
	}

	huma.Register(api, huma.Operation{
		OperationID:   "listar-garantias",
		Method:        http.MethodGet,
		Path:          "/garantias",
		Summary:       "Listar garantías",
		Description:   "Devuelve la bandeja paginada de garantías con sus artículos.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusUnprocessableEntity,
		},
	}, handlers.ListarGarantias)

	huma.Register(api, huma.Operation{
		OperationID:   "obtener-garantia",
		Method:        http.MethodGet,
		Path:          "/garantias/{id}",
		Summary:       "Obtener garantía",
		Description:   "Devuelve un folio de garantía con sus artículos y línea de tiempo.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
			http.StatusUnprocessableEntity,
		},
	}, handlers.ObtenerGarantia)
}
