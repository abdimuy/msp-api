//nolint:misspell // Spanish vocabulary (pago, zona, cargo) by convention.
package cobranzahttp

import (
	"time"

	"github.com/abdimuy/msp-api/internal/cobranza/domain"
	"github.com/abdimuy/msp-api/internal/cobranza/ports/outbound"
)

// ─── DTOs ─────────────────────────────────────────────────────────────────────

// PagoDTO is the JSON projection of a domain.Pago.
type PagoDTO struct {
	ImpteDoctoCCID int     `json:"impte_docto_cc_id" doc:"PK del importe en IMPORTES_DOCTOS_CC"`
	DoctoCCID      int     `json:"docto_cc_id"       doc:"ID del documento header del abono"`
	DoctoCCAcrID   int     `json:"docto_cc_acr_id"   doc:"ID del cargo acreditado (= MSP_SALDOS_VENTAS.DOCTO_CC_ID)"`
	ClienteID      int     `json:"cliente_id"        doc:"ID del cliente en Microsip CLIENTES"`
	ZonaClienteID  *int    `json:"zona_cliente_id"   doc:"Zona del cliente, null si no tiene"`
	Folio          string  `json:"folio"             doc:"Folio del documento abono"`
	ConceptoCCID   int     `json:"concepto_cc_id"    doc:"Concepto de cobranza (87327, 155, 11, etc.)"`
	Fecha          string  `json:"fecha"             doc:"Timestamp del pago (RFC3339 UTC); precisión de hora cuando proviene de la app móvil, sino día"`
	Importe        string  `json:"importe"           doc:"Monto del pago"`
	Impuesto       string  `json:"impuesto"          doc:"Impuesto incluido en el pago"`
	Lat            *string `json:"lat"               doc:"Latitud GPS donde se registró el pago (reservado para futuro)"`
	Lon            *string `json:"lon"               doc:"Longitud GPS donde se registró el pago (reservado para futuro)"`
	Cancelado      bool    `json:"cancelado"         doc:"true si el importe fue cancelado en Microsip"`
	Aplicado       bool    `json:"aplicado"          doc:"true si el importe está aplicado (IMPORTES_DOCTOS_CC.APLICADO='S')"`
	UpdatedAt      string  `json:"updated_at"        doc:"Timestamp del último refresco del caché (RFC3339 UTC)"`
}

// SyncSaldosBody envuelve un page de saldos para sync incremental.
type SyncSaldosBody struct {
	Items        []SaldoDTO `json:"items"          doc:"Saldos modificados en la ventana del cursor"`
	MaxUpdatedAt string     `json:"max_updated_at" doc:"Cursor para la próxima llamada (RFC3339 UTC)"`
	ServerNow    string     `json:"server_now"     doc:"Reloj del servidor al momento de la consulta (RFC3339 UTC)"`
	HasMore      bool       `json:"has_more"       doc:"true si quedan más items para el mismo cursor (paginar con after_id)"`
}

// SyncPagosBody envuelve un page de pagos para sync incremental.
type SyncPagosBody struct {
	Items        []PagoDTO `json:"items"          doc:"Pagos modificados en la ventana del cursor"`
	MaxUpdatedAt string    `json:"max_updated_at" doc:"Cursor para la próxima llamada (RFC3339 UTC)"`
	ServerNow    string    `json:"server_now"     doc:"Reloj del servidor al momento de la consulta (RFC3339 UTC)"`
	HasMore      bool      `json:"has_more"       doc:"true si quedan más items para el mismo cursor (paginar con after_id)"`
}

// ─── Input DTOs ───────────────────────────────────────────────────────────────

// PagosPorVentaInput is the path parameter for GET /cobranza/pagos/venta/{docto_cc_id}.
type PagosPorVentaInput struct {
	DoctoCCID int `path:"docto_cc_id" doc:"DOCTO_CC_ID del cargo acreditado"`
}

// PagosPorClienteInput is the path parameter for GET /cobranza/pagos/cliente/{cliente_id}.
type PagosPorClienteInput struct {
	ClienteID int `path:"cliente_id" doc:"ID del cliente en Microsip CLIENTES"`
}

// PagosPorZonaInput contains the path parameter and optional query params for
// GET /cobranza/pagos/zona/{zona_id}.
//
// Mismas reglas que /saldos/zona/{zona_id}: desde y ventana_dias son
// excluyentes; ambos ausentes → ventana_dias=7.
type PagosPorZonaInput struct {
	ZonaID      int    `path:"zona_id"                                              doc:"ID de la zona"`
	Desde       string `query:"desde"                                               doc:"Fecha absoluta (YYYY-MM-DD o RFC3339). Excluyente con ventana_dias"`
	VentanaDias int    `query:"ventana_dias" minimum:"-1" maximum:"90" default:"-1" doc:"Días hacia atrás desde hoy. -1 = no supplied (usa default 7). Excluyente con desde"`
}

// SyncSaldosInput contains the parameters for GET /cobranza/sync/saldos/zona/{zona_id}.
type SyncSaldosInput struct {
	ZonaID  int    `path:"zona_id"                              doc:"ID de la zona"`
	Cursor  string `query:"cursor"                              doc:"Cursor server_ts (RFC3339 UTC). Vacío para sync inicial"`
	AfterID int    `query:"after_id" minimum:"0"                doc:"PK de la última fila recibida para paginar dentro del mismo cursor"`
	Limit   int    `query:"limit"    minimum:"0" maximum:"5000" doc:"Tamaño máximo de página. Default 1000, máximo 5000"`
}

// SyncPagosInput contains the parameters for GET /cobranza/sync/pagos/zona/{zona_id}.
type SyncPagosInput struct {
	ZonaID  int    `path:"zona_id"                              doc:"ID de la zona"`
	Cursor  string `query:"cursor"                              doc:"Cursor server_ts (RFC3339 UTC). Vacío para sync inicial"`
	AfterID int    `query:"after_id" minimum:"0"                doc:"PK de la última fila recibida para paginar dentro del mismo cursor"`
	Limit   int    `query:"limit"    minimum:"0" maximum:"5000" doc:"Tamaño máximo de página. Default 1000, máximo 5000"`
}

// ─── Output DTOs ──────────────────────────────────────────────────────────────

// PagoOutput wraps a single PagoDTO.
type PagoOutput struct {
	Body PagoDTO
}

// PagosOutput wraps a slice of PagoDTO.
type PagosOutput struct {
	Body []PagoDTO
}

// SyncSaldosOutput wraps a saldos sync page.
type SyncSaldosOutput struct {
	Body SyncSaldosBody
}

// SyncPagosOutput wraps a pagos sync page.
type SyncPagosOutput struct {
	Body SyncPagosBody
}

// ─── Mapping helpers ──────────────────────────────────────────────────────────

// toPagoDTO projects a domain.Pago into a PagoDTO.
func toPagoDTO(p domain.Pago) PagoDTO {
	dto := PagoDTO{
		ImpteDoctoCCID: p.ImpteDoctoCCID(),
		DoctoCCID:      p.DoctoCCID(),
		DoctoCCAcrID:   p.DoctoCCAcrID(),
		ClienteID:      p.ClienteID(),
		ZonaClienteID:  p.ZonaClienteID(),
		Folio:          p.Folio(),
		ConceptoCCID:   p.ConceptoCCID(),
		Fecha:          p.Fecha().UTC().Format(time.RFC3339Nano),
		Importe:        p.Importe().StringFixed(2),
		Impuesto:       p.Impuesto().StringFixed(2),
		Cancelado:      p.Cancelado(),
		Aplicado:       p.Aplicado(),
		UpdatedAt:      p.UpdatedAt().UTC().Format(time.RFC3339Nano),
	}
	if lat := p.Lat(); lat != nil {
		s := lat.StringFixed(8)
		dto.Lat = &s
	}
	if lon := p.Lon(); lon != nil {
		s := lon.StringFixed(8)
		dto.Lon = &s
	}
	return dto
}

// toSyncSaldosBody projects an outbound.SyncPage[domain.Saldo] into the DTO.
func toSyncSaldosBody(page outbound.SyncPage[domain.Saldo]) SyncSaldosBody {
	items := make([]SaldoDTO, 0, len(page.Items))
	for _, s := range page.Items {
		items = append(items, toSaldoDTO(s))
	}
	return SyncSaldosBody{
		Items:        items,
		MaxUpdatedAt: page.MaxUpdatedAt.UTC().Format(time.RFC3339Nano),
		ServerNow:    page.ServerNow.UTC().Format(time.RFC3339Nano),
		HasMore:      page.HasMore,
	}
}

// toSyncPagosBody projects an outbound.SyncPage[domain.Pago] into the DTO.
func toSyncPagosBody(page outbound.SyncPage[domain.Pago]) SyncPagosBody {
	items := make([]PagoDTO, 0, len(page.Items))
	for _, p := range page.Items {
		items = append(items, toPagoDTO(p))
	}
	return SyncPagosBody{
		Items:        items,
		MaxUpdatedAt: page.MaxUpdatedAt.UTC().Format(time.RFC3339Nano),
		ServerNow:    page.ServerNow.UTC().Format(time.RFC3339Nano),
		HasMore:      page.HasMore,
	}
}
