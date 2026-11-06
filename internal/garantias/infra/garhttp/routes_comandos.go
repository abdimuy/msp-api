//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/abdimuy/msp-api/internal/garantias/app"
)

// registrarComandos registra las rutas POST de los comandos del folio.
//
// Contrato de las tareas 9 y 10: este archivo, handlers_comandos.go y
// dto_comandos.go son de la tarea 9. routes.go lo llama y no vuelve a
// cambiar por ellos. Cada operación que se registre aquí embebe
// EncabezadoIdempotencia en su entrada.
//
//nolint:funlen // una entrada de huma.Register por ruta: el contrato se lee como una tabla y no reparte OperationIDs.
func registrarComandos(api huma.API, servicio *app.Service) {
	handlers := nuevoCommandHandlers(servicio)
	security := []map[string][]string{
		{securitySchemeName: {}},
	}
	errores := []int{
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusUnprocessableEntity,
	}

	huma.Register(api, huma.Operation{
		OperationID:   "abrir-garantia",
		Method:        http.MethodPost,
		Path:          "/garantias",
		Summary:       "Abrir garantía",
		Description:   "Abre un folio de garantía de piso o de cliente y devuelve el folio creado.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.AbrirGarantia)

	huma.Register(api, huma.Operation{
		OperationID:   "iniciar-proceso",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/iniciar-proceso",
		Summary:       "Iniciar proceso",
		Description:   "Mueve el folio de abierto a en_proceso.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.IniciarProceso)

	huma.Register(api, huma.Operation{
		OperationID:   "marcar-listo-entrega",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/listo-entrega",
		Summary:       "Marcar listo para entrega",
		Description:   "Mueve el folio a listo_entrega cuando todos sus artículos están listos.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.MarcarListoEntrega)

	huma.Register(api, huma.Operation{
		OperationID:   "entregar-garantia",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/entregar",
		Summary:       "Entregar garantía",
		Description:   "Entrega el folio al cliente con su firma de conformidad y el GPS del agente.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.Entregar)

	huma.Register(api, huma.Operation{
		OperationID:   "cerrar-garantia",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/cerrar",
		Summary:       "Cerrar garantía",
		Description:   "Cierra un folio entregado y audita el GPS de la entrega contra los puntos de recolección.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.Cerrar)

	huma.Register(api, huma.Operation{
		OperationID:   "cancelar-garantia",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/cancelar",
		Summary:       "Cancelar garantía",
		Description:   "Cancela un folio abierto o en proceso con el motivo indicado.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.Cancelar)

	huma.Register(api, huma.Operation{
		OperationID:   "agregar-articulo",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos",
		Summary:       "Agregar artículo",
		Description:   "Agrega un artículo al folio de garantía.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.AgregarArticulo)

	huma.Register(api, huma.Operation{
		OperationID:   "avanzar-articulo",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/avanzar",
		Summary:       "Avanzar artículo",
		Description:   "Mueve un artículo a la etapa indicada, si la máquina de estados lo permite.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.AvanzarArticulo)

	huma.Register(api, huma.Operation{
		OperationID:   "registrar-diagnostico",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/diagnostico",
		Summary:       "Registrar diagnóstico",
		Description:   "Fija la ruta de reparación del artículo y lo avanza en la misma llamada.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.RegistrarDiagnostico)

	huma.Register(api, huma.Operation{
		OperationID:   "registrar-dictamen",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/dictamen",
		Summary:       "Registrar dictamen",
		Description:   "Aplica el dictamen del proveedor y avanza el artículo en la misma llamada.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.RegistrarDictamen)

	huma.Register(api, huma.Operation{
		OperationID:   "mover-articulo",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/mover",
		Summary:       "Mover artículo",
		Description:   "Registra la ubicación física manual del artículo.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.MoverArticulo)

	huma.Register(api, huma.Operation{
		OperationID:   "autorizar-cambio-fisico",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/cambio-fisico",
		Summary:       "Autorizar cambio físico",
		Description:   "Autoriza el reemplazo del artículo: el original pasa a standby y se crea el reemplazo.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.AutorizarCambioFisico)

	huma.Register(api, huma.Operation{
		OperationID:   "marcar-no-devuelto",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/no-devuelto",
		Summary:       "Marcar no devuelto",
		Description:   "Mueve a standby un artículo que el cliente no devolvió, sin crear reemplazo.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.MarcarNoDevuelto)

	huma.Register(api, huma.Operation{
		OperationID:   "registrar-desenlace",
		Method:        http.MethodPost,
		Path:          "/garantias/{id}/articulos/{aid}/desenlace",
		Summary:       "Registrar desenlace",
		Description:   "Registra el destino final de un artículo en standby.",
		Tags:          []string{"garantias"},
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors:        errores,
	}, handlers.RegistrarDesenlace)
}
