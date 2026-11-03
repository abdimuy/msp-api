//nolint:misspell // vocabulario español (comando, Comandos) por convención del proyecto.
package garhttp

import "time"

// ComandoBase carries the fields every command body shares: the device clock
// that stamps the event and the coordinates the agent captured. It is embedded
// so the JSON stays flat and Huma documents the fields once per operation.
type ComandoBase struct {
	DeviceCreatedAt time.Time `json:"device_created_at" format:"date-time" doc:"Momento en que el dispositivo registró la operación (RFC3339)"`
	GPSLat          *float64  `json:"gps_lat,omitempty"  minimum:"-90"  maximum:"90"  doc:"Latitud del agente; opcional salvo al entregar"`
	GPSLon          *float64  `json:"gps_lon,omitempty"  minimum:"-180" maximum:"180" doc:"Longitud del agente; opcional salvo al entregar"`
}

// AbrirGarantiaBody es el cuerpo de POST /garantias. Los datos de domicilio
// sólo aplican al origen cliente; el dominio los rechaza en un folio de piso.
type AbrirGarantiaBody struct {
	ComandoBase
	Origen         string     `json:"origen"                    enum:"piso,cliente"`
	ClienteID      *int       `json:"cliente_id,omitempty"      minimum:"1"`
	VentaID        *int       `json:"venta_id,omitempty"        minimum:"1"`
	EstadoCuenta   *string    `json:"estado_cuenta,omitempty"   enum:"liquidada,saldo_pendiente"`
	Description    string     `json:"description,omitempty"     maxLength:"500"`
	VigenciaHasta  *time.Time `json:"vigencia_hasta,omitempty"  format:"date-time"`
	Calle          string     `json:"calle,omitempty"           maxLength:"100"`
	NumeroExterior string     `json:"numero_exterior,omitempty" maxLength:"20"`
	Colonia        string     `json:"colonia,omitempty"         maxLength:"100"`
	Localidad      string     `json:"localidad,omitempty"       maxLength:"100"`
	Ciudad         string     `json:"ciudad,omitempty"          maxLength:"100"`
	CodigoPostal   string     `json:"codigo_postal,omitempty"   maxLength:"10"`
}

// IniciarProcesoBody es el cuerpo de POST /garantias/{id}/iniciar-proceso.
type IniciarProcesoBody struct {
	ComandoBase
}

// MarcarListoEntregaBody es el cuerpo de POST /garantias/{id}/listo-entrega.
type MarcarListoEntregaBody struct {
	ComandoBase
}

// CerrarBody es el cuerpo de POST /garantias/{id}/cerrar.
type CerrarBody struct {
	ComandoBase
	JustificacionDesvio string `json:"justificacion_desvio,omitempty" maxLength:"500" doc:"Explicación obligatoria cuando la entrega se desvió del punto de recolección"`
}

// CancelarBody es el cuerpo de POST /garantias/{id}/cancelar.
type CancelarBody struct {
	ComandoBase
	Motivo string `json:"motivo" maxLength:"500" doc:"Razón de la cancelación"`
}

// AgregarArticuloBody es el cuerpo de POST /garantias/{id}/articulos.
type AgregarArticuloBody struct {
	ComandoBase
	ArticuloID  *int   `json:"articulo_id,omitempty"  minimum:"1" doc:"ID del artículo en Microsip, si lo tiene"`
	Clave       string `json:"clave"                  maxLength:"50" doc:"Clave o identificador físico del artículo"`
	Description string `json:"description,omitempty"  maxLength:"500"`
}

// AvanzarArticuloBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/avanzar.
type AvanzarArticuloBody struct {
	ComandoBase
	EtapaDestino string `json:"etapa_destino" enum:"registrado,pendiente_recoleccion,recolectado,en_revision,orden_generada,enviado_proveedor,dictamen_recibido,reparado_proveedor,espera_respuesta_cliente,en_taller,reparado_taller,cambio_autorizado,listo_entrega,entregado,reingresado_inventario,standby,segunda_mano,desarmado,merma"`
}

// RegistrarDiagnosticoBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/diagnostico.
type RegistrarDiagnosticoBody struct {
	ComandoBase
	Ruta       string `json:"ruta"                  enum:"proveedor,taller"`
	RolDecisor string `json:"rol_decisor,omitempty" enum:"carpinteria,oficina,tecnica"`
}

// RegistrarDictamenBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/dictamen.
type RegistrarDictamenBody struct {
	ComandoBase
	Dictamen string `json:"dictamen" enum:"aceptada,rechazada,sin_falla"`
}

// MoverArticuloBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/mover.
type MoverArticuloBody struct {
	ComandoBase
	Ubicacion string `json:"ubicacion" enum:"domicilio_cliente,en_transito,almacen_revision,taller,proveedor,almacen_segunda_mano,entregado,baja"`
}

// AutorizarCambioFisicoBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/cambio-fisico.
type AutorizarCambioFisicoBody struct {
	ComandoBase
	RolDecisor string `json:"rol_decisor,omitempty" enum:"carpinteria,oficina,tecnica"`
}

// MarcarNoDevueltoBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/no-devuelto.
type MarcarNoDevueltoBody struct {
	ComandoBase
	RolDecisor string `json:"rol_decisor,omitempty" enum:"carpinteria,oficina,tecnica"`
}

// RegistrarDesenlaceBody es el cuerpo de POST /garantias/{id}/articulos/{aid}/desenlace.
type RegistrarDesenlaceBody struct {
	ComandoBase
	Desenlace  string `json:"desenlace"             enum:"reparado,reemplazado,devuelto,segunda_mano,desarmado,merma"`
	RolDecisor string `json:"rol_decisor,omitempty" enum:"carpinteria,oficina,tecnica"`
}

// Las siguientes entradas embeben EncabezadoIdempotencia y el path, que es lo
// que Huma lee para validar antes de entrar al handler.

// AbrirGarantiaRequest es la entrada de POST /garantias.
type AbrirGarantiaRequest struct {
	EncabezadoIdempotencia
	Body AbrirGarantiaBody
}

// IniciarProcesoRequest es la entrada de POST /garantias/{id}/iniciar-proceso.
type IniciarProcesoRequest struct {
	EncabezadoIdempotencia
	ID   string `path:"id" format:"uuid"`
	Body IniciarProcesoBody
}

// MarcarListoEntregaRequest es la entrada de POST /garantias/{id}/listo-entrega.
type MarcarListoEntregaRequest struct {
	EncabezadoIdempotencia
	ID   string `path:"id" format:"uuid"`
	Body MarcarListoEntregaBody
}

// CerrarRequest es la entrada de POST /garantias/{id}/cerrar.
type CerrarRequest struct {
	EncabezadoIdempotencia
	ID   string `path:"id" format:"uuid"`
	Body CerrarBody
}

// CancelarRequest es la entrada de POST /garantias/{id}/cancelar.
type CancelarRequest struct {
	EncabezadoIdempotencia
	ID   string `path:"id" format:"uuid"`
	Body CancelarBody
}

// AgregarArticuloRequest es la entrada de POST /garantias/{id}/articulos.
type AgregarArticuloRequest struct {
	EncabezadoIdempotencia
	ID   string `path:"id" format:"uuid"`
	Body AgregarArticuloBody
}

// AvanzarArticuloRequest es la entrada de POST /garantias/{id}/articulos/{aid}/avanzar.
type AvanzarArticuloRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       AvanzarArticuloBody
}

// RegistrarDiagnosticoRequest es la entrada de POST /garantias/{id}/articulos/{aid}/diagnostico.
type RegistrarDiagnosticoRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       RegistrarDiagnosticoBody
}

// RegistrarDictamenRequest es la entrada de POST /garantias/{id}/articulos/{aid}/dictamen.
type RegistrarDictamenRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       RegistrarDictamenBody
}

// MoverArticuloRequest es la entrada de POST /garantias/{id}/articulos/{aid}/mover.
type MoverArticuloRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       MoverArticuloBody
}

// AutorizarCambioFisicoRequest es la entrada de POST /garantias/{id}/articulos/{aid}/cambio-fisico.
type AutorizarCambioFisicoRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       AutorizarCambioFisicoBody
}

// MarcarNoDevueltoRequest es la entrada de POST /garantias/{id}/articulos/{aid}/no-devuelto.
type MarcarNoDevueltoRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       MarcarNoDevueltoBody
}

// RegistrarDesenlaceRequest es la entrada de POST /garantias/{id}/articulos/{aid}/desenlace.
type RegistrarDesenlaceRequest struct {
	EncabezadoIdempotencia
	ID         string `path:"id"  format:"uuid"`
	ArticuloID string `path:"aid" format:"uuid"`
	Body       RegistrarDesenlaceBody
}

// GarantiaRespuesta es la respuesta común de las rutas de comandos: el folio
// completo, sin la línea de tiempo.
type GarantiaRespuesta struct {
	Body GarantiaDTO
}
