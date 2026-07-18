// Package confighttp is the config module's HTTP transport: handlers, DTOs,
// and the Huma-over-chi router mount point for the vendedores administration
// screen (task 3 will add zonas/cajas endpoints to this same package).
package confighttp

// ListVendedoresInput has no query parameters — the endpoint returns every
// application usuario.
type ListVendedoresInput struct{}

// ListVendedoresOutput wraps the response body for GET /config/vendedores.
type ListVendedoresOutput struct {
	Body struct {
		Items []VendedorAsignacionDTO `json:"items"`
	}
}

// ListOpcionesInput has no query parameters — the endpoint returns every
// Microsip credit-vendor identity.
type ListOpcionesInput struct{}

// ListOpcionesOutput wraps the response body for GET /config/vendedores/opciones.
type ListOpcionesOutput struct {
	Body struct {
		Items []IdentidadMicrosipDTO `json:"items"`
	}
}

// AsignarVendedorInput is the payload for PUT /config/vendedores/{usuarioId}.
type AsignarVendedorInput struct {
	UsuarioID string `path:"usuarioId" doc:"ID (uuid) del usuario de la aplicación"`
	Body      struct {
		VendedorListaID1 *int `json:"vendedor_lista_id_1,omitempty" doc:"LISTA_ATRIB_ID del atributo 19985 (VENDEDOR_1); nulo/ausente para dejar el slot sin asignar"`
		VendedorListaID2 *int `json:"vendedor_lista_id_2,omitempty" doc:"LISTA_ATRIB_ID del atributo 19986 (VENDEDOR_2); nulo/ausente para dejar el slot sin asignar"`
		VendedorListaID3 *int `json:"vendedor_lista_id_3,omitempty" doc:"LISTA_ATRIB_ID del atributo 19987 (VENDEDOR_3); nulo/ausente para dejar el slot sin asignar"`
	}
}

// AsignarVendedorOutput wraps the response body for PUT /config/vendedores/{usuarioId}.
// Returns the resulting VendedorAsignacion so the caller can refresh without
// a second GET.
type AsignarVendedorOutput struct {
	Body struct {
		Item VendedorAsignacionDTO `json:"item"`
	}
}

// EliminarVendedorInput is the payload for DELETE /config/vendedores/{usuarioId}.
type EliminarVendedorInput struct {
	UsuarioID string `path:"usuarioId" doc:"ID (uuid) del usuario de la aplicación"`
}

// EliminarVendedorOutput wraps the response body for DELETE /config/vendedores/{usuarioId}.
type EliminarVendedorOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

// VendedorSlotDTO is one resolved VENDEDOR_LISTA_ID_n slot.
type VendedorSlotDTO struct {
	ListaID int    `json:"lista_id" doc:"LISTA_ATRIB_ID de Microsip"`
	Nombre  string `json:"nombre"   doc:"VALOR_DESPLEGADO del vendedor en Microsip"`
}

// VendedorMappingDTO wraps the three (possibly nil) vendedor slots.
type VendedorMappingDTO struct {
	V1 *VendedorSlotDTO `json:"v1" doc:"Slot VENDEDOR_1 (atributo 19985); nulo si no está asignado"`
	V2 *VendedorSlotDTO `json:"v2" doc:"Slot VENDEDOR_2 (atributo 19986); nulo si no está asignado"`
	V3 *VendedorSlotDTO `json:"v3" doc:"Slot VENDEDOR_3 (atributo 19987); nulo si no está asignado"`
}

// VendedorAsignacionDTO is one row of the vendedores administration screen.
type VendedorAsignacionDTO struct {
	UsuarioID string             `json:"usuario_id" doc:"ID (uuid) del usuario de la aplicación"`
	Nombre    string             `json:"nombre"     doc:"Nombre del usuario"`
	Email     string             `json:"email"      doc:"Correo del usuario"`
	Mapping   VendedorMappingDTO `json:"mapping"    doc:"Mapeo a los tres slots de vendedor de Microsip"`
	Estado    string             `json:"estado"     doc:"Resumen del mapeo: sin asignar, 1/3, 2/3, 3/3"`
}

// IdentidadMicrosipDTO is one Microsip credit-vendor identity available to
// pick from when assigning a VendedorMapping.
type IdentidadMicrosipDTO struct {
	Nombre     string `json:"nombre"        doc:"VALOR_DESPLEGADO compartido por los slots disponibles"`
	V1ListaID  *int   `json:"v1_lista_id"   doc:"LISTA_ATRIB_ID bajo el atributo 19985 (VENDEDOR_1); nulo si no existe"`
	V2ListaID  *int   `json:"v2_lista_id"   doc:"LISTA_ATRIB_ID bajo el atributo 19986 (VENDEDOR_2); nulo si no existe"`
	V3ListaID  *int   `json:"v3_lista_id"   doc:"LISTA_ATRIB_ID bajo el atributo 19987 (VENDEDOR_3); nulo si no existe"`
	MatchCount int    `json:"match_count"   doc:"Cuántos de los tres atributos tienen fila para este nombre (3 = identidad completa)"`
}
