//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/abdimuy/msp-api/internal/garantias/app"
	"github.com/abdimuy/msp-api/internal/garantias/domain"
)

// commandHandlers traduce las peticiones HTTP de los comandos a llamadas del
// servicio de aplicación. No decide permisos ni idempotencia: el encabezado se
// copia tal cual al comando y app resuelve lo demás (spec §6 y §7).
type commandHandlers struct {
	servicio *app.Service
}

// nuevoCommandHandlers construye los handlers de comandos sobre el servicio.
func nuevoCommandHandlers(servicio *app.Service) *commandHandlers {
	return &commandHandlers{servicio: servicio}
}

// responder convierte el resultado de un comando en la respuesta HTTP común.
func (h *commandHandlers) responder(
	ctx context.Context,
	garantia *domain.Garantia,
	err error,
) (*GarantiaRespuesta, error) {
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	out := &GarantiaRespuesta{}
	out.Body = garantiaDTO(garantia)

	return out, nil
}

// AbrirGarantia atiende POST /garantias.
func (h *commandHandlers) AbrirGarantia(
	ctx context.Context,
	in *AbrirGarantiaRequest,
) (*GarantiaRespuesta, error) {
	origen, err := domain.ParseOrigenFolio(in.Body.Origen)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	estadoCuenta, err := parseEstadoCuentaOpcional(in.Body.EstadoCuenta)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.AbrirGarantia(ctx, app.AbrirGarantiaCmd{
		Origen:            origen,
		ClienteID:         in.Body.ClienteID,
		VentaID:           in.Body.VentaID,
		EstadoCuenta:      estadoCuenta,
		Description:       in.Body.Description,
		VigenciaHasta:     in.Body.VigenciaHasta,
		Calle:             in.Body.Calle,
		NumeroExterior:    in.Body.NumeroExterior,
		Colonia:           in.Body.Colonia,
		Localidad:         in.Body.Localidad,
		Ciudad:            in.Body.Ciudad,
		CodigoPostal:      in.Body.CodigoPostal,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
		DomicilioGPSLat:   in.Body.GPSLat,
		DomicilioGPSLon:   in.Body.GPSLon,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
	})
	return h.responder(ctx, garantia, err)
}

// IniciarProceso atiende POST /garantias/{id}/iniciar-proceso.
func (h *commandHandlers) IniciarProceso(
	ctx context.Context,
	in *IniciarProcesoRequest,
) (*GarantiaRespuesta, error) {
	id, err := parseUUIDDeRuta(in.ID, "la garantía")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.IniciarProceso(ctx, app.IniciarProcesoCmd{
		GarantiaID:        id,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// MarcarListoEntrega atiende POST /garantias/{id}/listo-entrega.
func (h *commandHandlers) MarcarListoEntrega(
	ctx context.Context,
	in *MarcarListoEntregaRequest,
) (*GarantiaRespuesta, error) {
	id, err := parseUUIDDeRuta(in.ID, "la garantía")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.MarcarListoEntrega(ctx, app.MarcarListoEntregaCmd{
		GarantiaID:        id,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// Cerrar atiende POST /garantias/{id}/cerrar.
func (h *commandHandlers) Cerrar(
	ctx context.Context,
	in *CerrarRequest,
) (*GarantiaRespuesta, error) {
	id, err := parseUUIDDeRuta(in.ID, "la garantía")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.Cerrar(ctx, app.CerrarCmd{
		GarantiaID:          id,
		JustificacionDesvio: in.Body.JustificacionDesvio,
		ClaveIdempotencia:   in.IdempotencyKey,
		DeviceCreatedAt:     in.Body.DeviceCreatedAt,
		GPSLat:              in.Body.GPSLat,
		GPSLon:              in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// Cancelar atiende POST /garantias/{id}/cancelar.
func (h *commandHandlers) Cancelar(
	ctx context.Context,
	in *CancelarRequest,
) (*GarantiaRespuesta, error) {
	id, err := parseUUIDDeRuta(in.ID, "la garantía")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.Cancelar(ctx, app.CancelarCmd{
		GarantiaID:        id,
		Motivo:            in.Body.Motivo,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// AgregarArticulo atiende POST /garantias/{id}/articulos.
func (h *commandHandlers) AgregarArticulo(
	ctx context.Context,
	in *AgregarArticuloRequest,
) (*GarantiaRespuesta, error) {
	id, err := parseUUIDDeRuta(in.ID, "la garantía")
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.AgregarArticulo(ctx, app.AgregarArticuloCmd{
		GarantiaID:        id,
		ArticuloID:        in.Body.ArticuloID,
		Clave:             in.Body.Clave,
		Description:       in.Body.Description,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// AvanzarArticulo atiende POST /garantias/{id}/articulos/{aid}/avanzar.
func (h *commandHandlers) AvanzarArticulo(
	ctx context.Context,
	in *AvanzarArticuloRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	etapa, err := domain.ParseEtapa(in.Body.EtapaDestino)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.AvanzarArticulo(ctx, app.AvanzarArticuloCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		EtapaDestino:      etapa,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// RegistrarDiagnostico atiende POST /garantias/{id}/articulos/{aid}/diagnostico.
//
//nolint:dupl // cada handler valida su enum y arma su propio comando; duplicar la forma es más claro que un genérico.
func (h *commandHandlers) RegistrarDiagnostico(
	ctx context.Context,
	in *RegistrarDiagnosticoRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	ruta, err := domain.ParseRutaReparacion(in.Body.Ruta)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	if err := validarRolDecisor(in.Body.RolDecisor); err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.RegistrarDiagnostico(ctx, app.RegistrarDiagnosticoCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		Ruta:              ruta.String(),
		RolDecisor:        in.Body.RolDecisor,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// RegistrarDictamen atiende POST /garantias/{id}/articulos/{aid}/dictamen.
func (h *commandHandlers) RegistrarDictamen(
	ctx context.Context,
	in *RegistrarDictamenRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	dictamen, err := domain.ParseDictamen(in.Body.Dictamen)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.RegistrarDictamen(ctx, app.RegistrarDictamenCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		Dictamen:          dictamen.String(),
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// MoverArticulo atiende POST /garantias/{id}/articulos/{aid}/mover.
func (h *commandHandlers) MoverArticulo(
	ctx context.Context,
	in *MoverArticuloRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	ubicacion, err := domain.ParseUbicacion(in.Body.Ubicacion)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.MoverArticulo(ctx, app.MoverArticuloCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		Ubicacion:         ubicacion.String(),
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// AutorizarCambioFisico atiende POST /garantias/{id}/articulos/{aid}/cambio-fisico.
func (h *commandHandlers) AutorizarCambioFisico(
	ctx context.Context,
	in *AutorizarCambioFisicoRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	if err := validarRolDecisor(in.Body.RolDecisor); err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.AutorizarCambioFisico(ctx, app.AutorizarCambioFisicoCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		RolDecisor:        in.Body.RolDecisor,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// MarcarNoDevuelto atiende POST /garantias/{id}/articulos/{aid}/no-devuelto.
func (h *commandHandlers) MarcarNoDevuelto(
	ctx context.Context,
	in *MarcarNoDevueltoRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	if err := validarRolDecisor(in.Body.RolDecisor); err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.MarcarNoDevuelto(ctx, app.MarcarNoDevueltoCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		RolDecisor:        in.Body.RolDecisor,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// RegistrarDesenlace atiende POST /garantias/{id}/articulos/{aid}/desenlace.
//
//nolint:dupl // espejo de RegistrarDiagnostico: enum propio y comando propio.
func (h *commandHandlers) RegistrarDesenlace(
	ctx context.Context,
	in *RegistrarDesenlaceRequest,
) (*GarantiaRespuesta, error) {
	garantiaID, articuloID, err := parseUUIDsDeRuta(ctx, in.ID, in.ArticuloID)
	if err != nil {
		return nil, err
	}

	desenlace, err := domain.ParseDesenlace(in.Body.Desenlace)
	if err != nil {
		return nil, mapAppError(ctx, err)
	}

	if err := validarRolDecisor(in.Body.RolDecisor); err != nil {
		return nil, mapAppError(ctx, err)
	}

	garantia, err := h.servicio.RegistrarDesenlace(ctx, app.RegistrarDesenlaceCmd{
		GarantiaID:        garantiaID,
		ArticuloID:        articuloID,
		Desenlace:         desenlace.String(),
		RolDecisor:        in.Body.RolDecisor,
		ClaveIdempotencia: in.IdempotencyKey,
		DeviceCreatedAt:   in.Body.DeviceCreatedAt,
		GPSLat:            in.Body.GPSLat,
		GPSLon:            in.Body.GPSLon,
	})
	return h.responder(ctx, garantia, err)
}

// parseUUIDDeRuta valida un UUID que llegó por la ruta. Huma ya lo hace por el
// format del campo; repetirlo deja el contrato en el handler y da un error con
// el mismo formato que el resto del módulo si alguna ruta lo omitiera.
func parseUUIDDeRuta(raw, nombre string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, errorValidacion(
			"invalid_uuid",
			"el identificador de "+nombre+" no es un UUID válido",
		)
	}

	return id, nil
}

// parseUUIDsDeRuta valida los dos UUID de las rutas de artículo.
func parseUUIDsDeRuta(ctx context.Context, garantiaID, articuloID string) (uuid.UUID, uuid.UUID, error) {
	gid, err := parseUUIDDeRuta(garantiaID, "la garantía")
	if err != nil {
		return uuid.Nil, uuid.Nil, mapAppError(ctx, err)
	}

	aid, err := parseUUIDDeRuta(articuloID, "el artículo")
	if err != nil {
		return uuid.Nil, uuid.Nil, mapAppError(ctx, err)
	}

	return gid, aid, nil
}

// parseEstadoCuentaOpcional parsea el enum cuando viene y devuelve nil cuando
// el cliente no lo envió.
func parseEstadoCuentaOpcional(raw *string) (*domain.EstadoCuenta, error) {
	if raw == nil {
		//nolint:nilnil // nil indica que el campo opcional no fue enviado.
		return nil, nil
	}

	estado, err := domain.ParseEstadoCuenta(*raw)
	if err != nil {
		return nil, err
	}

	return &estado, nil
}

// validarRolDecisor adelanta en HTTP el error que app ya produce: un rol
// vacío significa que el cliente no lo envió y lo decide el dominio; uno
// desconocido es 422 con el centinela del dominio.
func validarRolDecisor(raw string) error {
	rol := strings.TrimSpace(raw)
	if rol == "" {
		return nil
	}

	_, err := domain.ParseRolDecisor(rol)

	return err
}
