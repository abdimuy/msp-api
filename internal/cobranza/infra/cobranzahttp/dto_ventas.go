//nolint:misspell // Spanish vocabulary (venta, zona, cargo, cobrador, parcialidad, enganche, vendedor) by convention.
package cobranzahttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// VentaDTO is the JSON projection of a domain.Venta — combines the
// MSP_SALDOS_VENTAS row with the static cliente/dirección/contrato fields the
// mobile cobranza app needs to render a route in one round-trip.
type VentaDTO struct {
	// MSP_SALDOS_VENTAS fields.
	DoctoCCID      int     `json:"docto_cc_id"     doc:"PK del cargo en DOCTOS_CC"`
	DoctoPVID      *int    `json:"docto_pv_id"     doc:"ID del documento PV origen, null si no aplica"`
	ClienteID      int     `json:"cliente_id"      doc:"ID del cliente en CLIENTES"`
	ZonaClienteID  *int    `json:"zona_cliente_id" doc:"ID de la zona del cliente"`
	Folio          string  `json:"folio"           doc:"Folio del cargo en Microsip"`
	FechaCargo     string  `json:"fecha_cargo"     doc:"Fecha del cargo (RFC3339 UTC)"`
	FechaVenta     *string `json:"fecha_venta"     doc:"Fecha del documento PV origen (RFC3339 UTC), null si no hay PV"`
	PrecioTotal    string  `json:"precio_total"    doc:"Importe total del cargo"`
	TotalImporte   string  `json:"total_importe"   doc:"Suma de cobros aplicados"`
	ImpteRest      string  `json:"impte_rest"      doc:"Otros descuentos (enganches viejos, condonaciones)"`
	Saldo          string  `json:"saldo"           doc:"Saldo pendiente"`
	NumPagos       int     `json:"num_pagos"       doc:"Número de pagos aplicados"`
	FechaUltPago   *string `json:"fecha_ult_pago"  doc:"Fecha del último pago (RFC3339 UTC)"`
	CargoCancelado bool    `json:"cargo_cancelado" doc:"true si el cargo fue cancelado (tombstone)"`
	UpdatedAt      string  `json:"updated_at"      doc:"Timestamp del último refresco del caché (RFC3339 UTC)"`

	// CLIENTES fields.
	ClienteNombre  string  `json:"cliente_nombre"   doc:"Nombre del cliente"`
	LimiteCredito  *string `json:"limite_credito"   doc:"Límite de crédito autorizado"`
	ClienteNotas   string  `json:"cliente_notas"    doc:"Notas libres del cliente"`
	CobradorID     *int    `json:"cobrador_id"      doc:"ID del cobrador asignado al cliente"`
	NombreCobrador string  `json:"nombre_cobrador"  doc:"Nombre del cobrador"`

	// ZONAS_CLIENTES.
	ZonaNombre string `json:"zona_nombre" doc:"Nombre de la zona"`

	// DIRS_CLIENTES (dirección principal).
	Calle    string `json:"calle"    doc:"Calle y número de la dirección principal"`
	Ciudad   string `json:"ciudad"   doc:"Ciudad o población"`
	Estado   string `json:"estado"   doc:"Estado"`
	Telefono string `json:"telefono" doc:"Teléfono principal"`

	// LIBRES_CARGOS_CC (contrato).
	Parcialidad           *int    `json:"parcialidad"               doc:"Parcialidad acordada (importe por pago)"`
	Enganche              *string `json:"enganche"                  doc:"Enganche acordado"`
	TiempoCortoPlazoMeses *int    `json:"tiempo_corto_plazo_meses"  doc:"Plazo corto en meses"`
	MontoCortoPlazo       *string `json:"monto_corto_plazo"         doc:"Monto a corto plazo"`
	PrecioDeContado       *string `json:"precio_de_contado"         doc:"Precio de contado equivalente"`
	AvalOResponsable      string  `json:"aval_o_responsable"        doc:"Aval o responsable solidario"`
	Vendedor1ID           *int    `json:"vendedor_1_id"             doc:"Vendedor 1 (display-only)"`
	Vendedor2ID           *int    `json:"vendedor_2_id"             doc:"Vendedor 2 (display-only)"`
	Vendedor3ID           *int    `json:"vendedor_3_id"             doc:"Vendedor 3 (display-only)"`
}

// SyncVentasBody envuelve un page de ventas enriquecidas para sync incremental.
type SyncVentasBody struct {
	Items        []VentaDTO `json:"items"          doc:"Ventas modificadas en la ventana del cursor"`
	MaxUpdatedAt string     `json:"max_updated_at" doc:"Cursor para la próxima llamada (RFC3339 UTC)"`
	ServerNow    string     `json:"server_now"     doc:"Reloj del servidor al momento de la consulta (RFC3339 UTC)"`
	HasMore      bool       `json:"has_more"       doc:"true si quedan más items para el mismo cursor (paginar con after_id)"`
}

// SyncVentasInput contains the parameters for GET /cobranza/sync/ventas/zona/{zona_id}.
type SyncVentasInput struct {
	ZonaID  int    `path:"zona_id"                              doc:"ID de la zona"`
	Cursor  string `query:"cursor"                              doc:"Cursor server_ts (RFC3339 UTC). Vacío para sync inicial"`
	AfterID int    `query:"after_id" minimum:"0"                doc:"PK de la última fila recibida para paginar dentro del mismo cursor"`
	Limit   int    `query:"limit"    minimum:"0" maximum:"5000" doc:"Tamaño máximo de página. Default 1000, máximo 5000"`
}

// SyncVentasOutput wraps a ventas sync page.
type SyncVentasOutput struct {
	Body SyncVentasBody
}

// ─── Mapping helpers ──────────────────────────────────────────────────────────

// toVentaDTO projects a domain.Venta into a VentaDTO. Decimal pointers are
// rendered with 2 decimal places when non-nil; nil columns surface as JSON
// null.
func toVentaDTO(v domain.Venta) VentaDTO {
	dto := VentaDTO{
		DoctoCCID:             v.DoctoCCID(),
		DoctoPVID:             v.DoctoPVID(),
		ClienteID:             v.ClienteID(),
		ZonaClienteID:         v.ZonaClienteID(),
		Folio:                 v.Folio(),
		FechaCargo:            v.FechaCargo().UTC().Format(time.RFC3339Nano),
		PrecioTotal:           v.PrecioTotal().StringFixed(2),
		TotalImporte:          v.TotalImporte().StringFixed(2),
		ImpteRest:             v.ImpteRest().StringFixed(2),
		Saldo:                 v.Saldo().StringFixed(2),
		NumPagos:              v.NumPagos(),
		CargoCancelado:        v.CargoCancelado(),
		UpdatedAt:             v.UpdatedAt().UTC().Format(time.RFC3339Nano),
		ClienteNombre:         v.ClienteNombre(),
		ClienteNotas:          v.ClienteNotas(),
		CobradorID:            v.CobradorID(),
		NombreCobrador:        v.NombreCobrador(),
		ZonaNombre:            v.ZonaNombre(),
		Calle:                 v.Calle(),
		Ciudad:                v.Ciudad(),
		Estado:                v.Estado(),
		Telefono:              v.Telefono(),
		Parcialidad:           v.Parcialidad(),
		TiempoCortoPlazoMeses: v.TiempoCortoPlazoMeses(),
		AvalOResponsable:      v.AvalOResponsable(),
		Vendedor1ID:           v.Vendedor1ID(),
		Vendedor2ID:           v.Vendedor2ID(),
		Vendedor3ID:           v.Vendedor3ID(),
	}
	if fup := v.FechaUltPago(); fup != nil {
		s := fup.UTC().Format(time.RFC3339Nano)
		dto.FechaUltPago = &s
	}
	if fv := v.FechaVenta(); fv != nil {
		s := fv.UTC().Format(time.RFC3339Nano)
		dto.FechaVenta = &s
	}
	if lc := v.LimiteCredito(); lc != nil {
		s := lc.StringFixed(2)
		dto.LimiteCredito = &s
	}
	if e := v.Enganche(); e != nil {
		s := e.StringFixed(2)
		dto.Enganche = &s
	}
	if m := v.MontoCortoPlazo(); m != nil {
		s := m.StringFixed(2)
		dto.MontoCortoPlazo = &s
	}
	if p := v.PrecioDeContado(); p != nil {
		s := p.StringFixed(2)
		dto.PrecioDeContado = &s
	}
	return dto
}

// toSyncVentasBody projects an outbound.SyncPage[domain.Venta] into the DTO.
func toSyncVentasBody(page outbound.SyncPage[domain.Venta]) SyncVentasBody {
	items := make([]VentaDTO, 0, len(page.Items))
	for _, v := range page.Items {
		items = append(items, toVentaDTO(v))
	}
	return SyncVentasBody{
		Items:        items,
		MaxUpdatedAt: page.MaxUpdatedAt.UTC().Format(time.RFC3339Nano),
		ServerNow:    page.ServerNow.UTC().Format(time.RFC3339Nano),
		HasMore:      page.HasMore,
	}
}
