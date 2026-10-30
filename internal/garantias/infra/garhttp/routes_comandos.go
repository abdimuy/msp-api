//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/garantias/app"
)

// registrarComandos registra las rutas POST de los comandos del folio.
//
// Contrato de las tareas 9 y 10: este archivo, handlers_comandos.go y
// dto_comandos.go son de la tarea 9. routes.go lo llama y no vuelve a
// cambiar por ellos. Cada operación que se registre aquí embebe
// EncabezadoIdempotencia en su entrada.
func registrarComandos(_ huma.API, _ *app.Service) {}
